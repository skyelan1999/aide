'use strict';
/* 锁屏集群：跨标签页（主界面 ↔ 从界面）锁定状态层级联动。
 *
 * 纯前端零后端改动，基于 BroadcastChannel（频道 aide-lock-v1）做 leader election。
 * 三条硬规则：
 *   ① master 未锁 → 所有 slave 绝不锁
 *   ② master 锁   → 所有 slave 自动跟随锁
 *   ③ slave 单独解锁 → 只解锁该 slave（localDismiss），不回传 master，master 与其他 slave 仍锁
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
  const JOIN_WINDOW_MS = 600;    // 加入窗口期：显示中性“正在确认安全状态…”
  const HEARTBEAT_MS = 1500;     // master 心跳（assert）间隔
  const WATCHDOG_MS = 3000;      // slave 收不到心跳 → 重选
  const ELECTION_MS = 400;       // 选举发出后等待反超的时间
  const PING_THROTTLE_MS = 1000; // slave 活动 ping 节流

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
  let masterGen = 0;               // 当前 master 代际：slave 只接受单调递增
  let masterFrom = null;
  let joinTimer = null, watchdogTimer = null, heartbeatTimer = null, electionTimer = null;
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
    scheduleWatchdog();
    emit('effective', effectiveLocked());
    settleReady();
  }
  function startElection() {
    clearT(joinTimer); joinTimer = null;
    clearT(electionTimer);
    send('election');
    electionTimer = setTimeout(() => {
      // 无更强者反超 → 当选。继承上次集群锁态；从未见过 master（首个 tab/刷新主 tab）默认锁。
      const inherit = lastSeenMasterLocked === null ? true : !!lastSeenMasterLocked;
      becomeMaster(inherit);
    }, ELECTION_MS);
  }
  function onWatchdog() {
    if (phase !== 'slave') return;
    phase = 'joining'; // 临时回到竞选态
    startElection();
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
    joinTimer = setTimeout(startElection, JOIN_WINDOW_MS);
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
    get tabId() { return tabId; },
    get role() { return role; },
    get supported() { return supported; }
  };
  boot();
})();
