'use strict';
/* 锁屏集群：跨标签页（主界面 ↔ 从界面）锁定状态层级联动。
 *
 * 三条硬规则（不变）：
 *   ① master 未锁 → 所有 slave 绝不锁
 *   ② master 锁   → 所有 slave 自动跟随锁
 *   ③ slave 单独解锁 → 只解锁该 slave（localDismiss），不回传 master，master 与其他 slave 仍锁
 *
 * 后端权威优先（修复“刷新即误锁”）：
 *   主 tab 刷新时旧 master 消失，新 tab 在加入窗口期内见不到任何 BroadcastChannel 心跳，
 *   旧逻辑以 lastSeenMasterLocked===null ? true 硬默认“锁定”当选——无法区分“从未锁”与
 *   “锁了、master 重启”。现改为：选举/当选 inherit、veil 硬超时、看门狗重选前，先读后端
 *   持久权威状态 GET /api/lock-state：
 *     - 明确未锁 → becomeMaster(false)
 *     - 明确锁定 → becomeMaster(true)
 *   仅当后端不可达/无法判断时才回退前端保守默认 DEFAULT_LOCK_WHEN_UNKNOWN。
 *   master 执行锁定/解锁/空闲升锁时 PUT /api/lock-state 持久化。
 *
 * 状态：
 *   master 侧唯一写入点 masterLocked；slave 侧维护 localDismiss。
 *   effectiveLocked: master → masterLocked；slave → masterLocked && !localDismiss。
 *
 * 选举优先级元组（小者胜，先比 role，再比 bootTs，最后 tabId）：
 *   P = (role==='ws' ? 0 : 1), bootTs, tabId  —— ws 主界面优先当 master。
 *
 * 降级：typeof BroadcastChannel === 'undefined' 时退化为现状（各 tab 独立锁），
 *       onReady 直接 resolve true，其余消息/定时均为空操作。
 */
(function () {
  const CHANNEL = 'aide-lock-v1';
  const JOIN_WINDOW_MS = 600;     // 加入窗口期：显示中性“正在确认安全状态…”
  const HEARTBEAT_MS = 1500;      // master 心跳（assert）间隔
  const WATCHDOG_MS = 3000;       // slave 收不到心跳 → 重选
  const ELECTION_MS = 400;        // 选举发出后等待反超的时间
  const PING_THROTTLE_MS = 1000;  // slave 活动 ping 节流
  const JOIN_HARD_TIMEOUT_MS = 2500; // joining veil 硬上限：不得卡死在“正在确认安全状态”
  const BACKEND_FETCH_TIMEOUT_MS = 1500; // 后端权威状态读取超时
  // 后端不可达且集群内也无任何已知锁态时的保守默认。建议 false（不锁）：
  // 宁可短暂放行，也不让一次后端抖动把用户锁在门外；随后后台继续确认，确认真实锁定再补锁。
  const DEFAULT_LOCK_WHEN_UNKNOWN = false;

  // 每 tab 启动生成稳定身份
  const tabId = (typeof crypto !== 'undefined' && crypto.randomUUID)
    ? crypto.randomUUID()
    : 'tab-' + Date.now().toString(36) + '-' + Math.random().toString(36).slice(2, 10);
  const bootTs = Date.now();
  // role：文件查看器（#file=…）为 slave 优先角色；其余 ws 为主界面
  const role = (location.hash || '').indexOf('#file=') === 0 ? 'file' : 'ws';
  const ROLE_PRI = role === 'ws' ? 0 : 1;

  const handlers = Object.create(null);
  let bc = null;
  const supported = (typeof BroadcastChannel !== 'undefined');
  let phase = 'joining';            // joining | master | slave
  let masterLocked = false;         // 仅 master 写入
  let localDismiss = false;         // 仅 slave：本 tab 已单独解锁
  let lastSeenMasterLocked = null;  // slave 从 welcome/assert 学到的集群锁态
  let masterGen = 0;                // 当前 master 代际：slave 只接受单调递增
  let masterFrom = null;
  // 后端权威状态：null = 未取到/不可达；{ locked:bool, gen:number } = 已知
  let backendState = null;
  let joinTimer = null, watchdogTimer = null, heartbeatTimer = null, electionTimer = null, hardJoinTimer = null;
  let lastPingAt = 0;
  let onRemoteActivity = null;     // master 收到 slave ping 时回调（app.js 注入重置空闲表）
  let readyResolve = null;
  let readySettled = false;
  const readyPromise = new Promise(res => { readyResolve = res; });

  function emit(ev, data) {
    const list = handlers[ev];
    if (!list) return;
    for (const fn of list) { try { fn(data); } catch (_) {} }
  }
  function on(ev, fn) { (handlers[ev] = handlers[ev] || []).push(fn); }

  function myPri() { return [ROLE_PRI, bootTs, tabId]; }
  // 元组 a 是否比 b 优先级高（小者胜）
  function priBeats(a, b) {
    for (let i = 0; i < 3; i++) if (a[i] !== b[i]) return a[i] < b[i];
    return false;
  }
  function send(type, extra) {
    if (!bc) return;
    const msg = Object.assign({ type, gen: masterGen, from: tabId, role, bootTs, pri: myPri() }, extra);
    try { bc.postMessage(msg); } catch (_) {}
  }

  /* ── 后端权威状态 ── */
  function authToken() {
    try { return localStorage.getItem('aide-token') || ''; } catch (_) { return ''; }
  }
  // 读后端权威锁定状态。成功填 backendState；任何失败/非 2xx 置 null（=无法判断）。
  function fetchBackendState() {
    if (typeof fetch !== 'function') { backendState = null; return Promise.resolve(null); }
    const ctrl = (typeof AbortController !== 'undefined') ? new AbortController() : null;
    const timer = setTimeout(() => { try { ctrl && ctrl.abort(); } catch (_) {} }, BACKEND_FETCH_TIMEOUT_MS);
    return fetch('/api/lock-state', {
      headers: { 'Authorization': 'Bearer ' + authToken() },
      signal: ctrl ? ctrl.signal : undefined
    }).then(resp => {
      clearTimeout(timer);
      if (!resp.ok) { backendState = null; return null; }
      return resp.json();
    }).then(data => {
      if (!data) return null;
      backendState = { locked: !!data.locked, gen: (data.gen | 0) };
      return backendState;
    }).catch(_ => { clearTimeout(timer); backendState = null; return null; });
  }
  // master 锁定/解锁/升锁后持久化权威状态（best-effort；失败由下次读取自愈）。
  function persistBackend(locked) {
    if (typeof fetch !== 'function') return;
    fetch('/api/lock-state', {
      method: 'PUT',
      headers: { 'Authorization': 'Bearer ' + authToken(), 'Content-Type': 'application/json' },
      body: JSON.stringify({ locked: !!locked })
    }).then(() => { backendState = { locked: !!locked, gen: backendState ? backendState.gen + 1 : 1 }; })
      .catch(() => {});
  }
  // 后端曾不可达：后台继续确认。确认锁定则补锁；确认未锁则维持现状。
  function backgroundReconfirm() {
    setTimeout(() => {
      fetchBackendState().then(() => {
        if (backendState && phase === 'master' && backendState.locked !== masterLocked) {
          masterLocked = backendState.locked;
          masterGen++;
          send('assert', { masterLocked });
          emit('effective', effectiveLocked());
        }
      });
    }, 800);
  }

  function effectiveLocked() {
    if (phase === 'master') return masterLocked;
    if (phase === 'slave') return !!lastSeenMasterLocked && !localDismiss;
    return false; // joining 期间由 app 显示中性面纱
  }
  function settleReady() {
    if (readySettled) return;
    readySettled = true;
    readyResolve(effectiveLocked());
  }
  function clearT(t) { if (t) { clearTimeout(t); } }

  function scheduleHeartbeat() {
    clearT(heartbeatTimer);
    heartbeatTimer = setInterval(() => { send('assert', { masterLocked }); }, HEARTBEAT_MS);
  }
  function scheduleWatchdog() {
    clearT(watchdogTimer);
    watchdogTimer = setTimeout(onWatchdog, WATCHDOG_MS);
  }

  function becomeMaster(inheritLocked) {
    phase = 'master';
    masterGen++;
    masterLocked = !!inheritLocked;
    localDismiss = false;
    masterFrom = tabId;
    clearT(joinTimer); joinTimer = null;
    clearT(electionTimer); electionTimer = null;
    clearT(watchdogTimer); watchdogTimer = null;
    clearT(hardJoinTimer); hardJoinTimer = null;
    scheduleHeartbeat();
    send('assert', { masterLocked });
    emit('effective', effectiveLocked());
    settleReady();
  }
  function becomeSlave(from, mLocked, gen) {
    phase = 'slave';
    masterFrom = from;
    masterGen = gen | 0;
    lastSeenMasterLocked = !!mLocked;
    localDismiss = false;
    clearT(joinTimer); joinTimer = null;
    clearT(electionTimer); electionTimer = null;
    clearT(hardJoinTimer); hardJoinTimer = null;
    scheduleWatchdog();
    emit('effective', effectiveLocked());
    settleReady();
  }
  // 当选时继承锁态：后端权威优先，其次集群内见过的锁态，最后回退保守默认。
  function inheritLockedDecision() {
    if (backendState) return !!backendState.locked;
    if (lastSeenMasterLocked !== null) return !!lastSeenMasterLocked;
    return DEFAULT_LOCK_WHEN_UNKNOWN;
  }
  function startElection() {
    clearT(joinTimer); joinTimer = null;
    clearT(electionTimer);
    send('election');
    electionTimer = setTimeout(() => {
      // 无更强者反超 → 当选。以后端持久状态为准（修复刷新误锁）；后端不可达才回退默认。
      becomeMaster(inheritLockedDecision());
    }, ELECTION_MS);
  }
  function onWatchdog() {
    if (phase !== 'slave') return;
    phase = 'joining'; // 临时回到竞选态
    // 心跳短暂丢失（后台 tab 被节流/断连恢复）不应误判锁态：重选前先刷新后端权威。
    fetchBackendState();
    startElection();
  }
  // joining veil 硬超时：面纱不得卡死在“正在确认安全状态”。
  function onJoinHardTimeout() {
    if (readySettled) return;
    if (phase === 'joining') {
      becomeMaster(inheritLockedDecision());
    } else {
      settleReady();
    }
    // 后端此前一直不可达：后台继续确认，确认真实锁定再补锁。
    if (!backendState) backgroundReconfirm();
  }

  function onMessage(ev) {
    const m = ev.data || {};
    if (typeof m.type !== 'string' || m.from === tabId) return;

    switch (m.type) {
      case 'hello':
        if (phase === 'master') send('welcome', { masterLocked, gen: masterGen });
        break;

      case 'welcome':
        if (phase === 'master') {
          if (priBeats(m.pri, myPri())) becomeSlave(m.from, m.masterLocked, m.gen); // 分裂脑，强者上
        } else {
          becomeSlave(m.from, m.masterLocked, m.gen);
        }
        break;

      case 'assert': // master 心跳
        if (phase === 'master') {
          if (priBeats(m.pri, myPri())) becomeSlave(m.from, m.masterLocked, m.gen);
          break;
        }
        if (phase === 'joining') {
          // 并发启动：自封 master 的心跳到达，认它为主（不等 welcome）
          becomeSlave(m.from, m.masterLocked, m.gen);
        } else if (phase === 'slave' && m.from === masterFrom && m.gen >= masterGen) {
          masterGen = Math.max(masterGen, m.gen | 0);
          lastSeenMasterLocked = !!m.masterLocked;
          scheduleWatchdog();
          emit('effective', effectiveLocked());
        }
        break;

      case 'lock':
        if (phase === 'slave' && m.from === masterFrom && m.gen >= masterGen) {
          masterGen = m.gen | 0;
          lastSeenMasterLocked = true;
          localDismiss = false; // master 重新升锁 → slave 本地解锁标记失效
          scheduleWatchdog();
          emit('effective', true);
        }
        break;

      case 'unlock':
        if (phase === 'slave' && m.from === masterFrom && m.gen >= masterGen) {
          masterGen = m.gen | 0;
          lastSeenMasterLocked = false;
          scheduleWatchdog();
          emit('effective', false);
        }
        break;

      case 'election':
        if (priBeats(m.pri, myPri())) {
          // 对方更强：退让，等对方 assert
          if (phase === 'master') { phase = 'slave'; masterFrom = m.from; }
          clearT(electionTimer); electionTimer = null;
          scheduleWatchdog();
        } else {
          send('election'); // 我更强：反发
        }
        break;

      case 'ping':
        if (phase === 'master' && onRemoteActivity) { try { onRemoteActivity(); } catch (_) {} }
        break;

      case 'req-lock':
        if (phase === 'master') requestLock('remote');
        break;

      case 'bye':
        if (phase === 'slave' && m.from === masterFrom) onWatchdog();
        break;
    }
  }

  /* ── 对外 API ── */
  function requestLock(reason) {
    if (!supported) { emit('effective', true); return; } // 降级：仅本 tab
    if (phase === 'master') {
      masterLocked = true;
      masterGen++;
      send('lock', { masterLocked: true, reason: reason || 'manual' });
      persistBackend(true); // 持久化权威：刷新/新开 tab 保持锁定
      emit('effective', true);
    } else {
      send('req-lock'); // slave 请 master 升锁
    }
  }
  function handleUnlockSuccess() {
    if (!supported) { emit('effective', false); return; }
    if (phase === 'master') {
      masterLocked = false;
      masterGen++;
      send('unlock', { masterLocked: false });
      persistBackend(false); // 持久化权威：刷新/新开 tab 不再误锁
      emit('effective', false);
    } else if (phase === 'slave') {
      localDismiss = true; // 只解本 tab，不广播
      emit('effective', effectiveLocked());
    }
  }
  function noteActivity() {
    if (!supported) return;
    if (phase === 'master') {
      if (onRemoteActivity) { try { onRemoteActivity(); } catch (_) {} }
    } else if (phase === 'slave') {
      const now = Date.now();
      if (now - lastPingAt >= PING_THROTTLE_MS) {
        lastPingAt = now;
        send('ping');
      }
    }
  }

  function boot() {
    // 降级：无 BroadcastChannel 时退化为现状（各 tab 独立锁）。
    // onReady 直接 resolve true，让 app 走“加载即锁”的旧本地路径。
    if (!supported) { readySettled = true; readyResolve(true); return; }
    try { bc = new BroadcastChannel(CHANNEL); }
    catch (_) { bc = null; settleReady(); return; }
    bc.onmessage = onMessage;
    send('hello');
    // 并发拉取后端权威状态：选举/veil 超时决策时通常已就绪；不阻塞选举。
    fetchBackendState();
    joinTimer = setTimeout(startElection, JOIN_WINDOW_MS);
    hardJoinTimer = setTimeout(onJoinHardTimeout, JOIN_HARD_TIMEOUT_MS);
    window.addEventListener('beforeunload', () => { try { send('bye'); } catch (_) {} });
  }

  window.LockCluster = {
    on,
    onReady: () => readyPromise,
    requestLock,
    handleUnlockSuccess,
    noteActivity,
    setRemoteActivityHook: fn => { onRemoteActivity = fn; },
    isMaster: () => phase === 'master',
    isSlave: () => phase === 'slave',
    effectiveLocked,
    // 供 app.js initialize 预判：已收敛且明确未锁时跳过 joining 面纱，尽量不闪。
    snapshot: () => (readySettled ? { settled: true, locked: effectiveLocked() } : { settled: false, locked: false }),
    get tabId() { return tabId; },
    get role() { return role; },
    get supported() { return supported; }
  };
  boot();
})();
