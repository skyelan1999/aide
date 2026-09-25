'use strict';
const t = (key, ...args) => window.aideI18n ? window.aideI18n.t(key, ...args) : String(key).replace(/\{(\d+)\}/g, (m, i) => args[i] ?? m);
const $ = id => document.getElementById(id);
const state = { token: localStorage.getItem('aide-token') || '', session: null, sessionJSON: '', mode: 'chat', root: 'workspace', dir: '.', attachments: [], file: null, busy: false, poll: null, config: null, commandAbort: null, profiles: null, modelDraft: null, plugins: [], panel: 'files', sources: [], source: '', stream: null, live: {}, liveRound: {}, liveTool: {}, liveReasoning: {}, runPhase: {}, streamRetryAt: 0, queueMode: true, autoScroll: true, jumpAnimating: false };
const fragment = new URLSearchParams(location.hash.slice(1));
if (fragment.has('token')) { state.token = fragment.get('token'); localStorage.setItem('aide-token', state.token); history.replaceState(null, '', location.pathname); }
function el(tag, cls, text) { const e = document.createElement(tag); if (cls) e.className = cls; if (text != null) e.textContent = text; return e; }
function toast(text) { const host = document.querySelector('dialog[open]') || document.body; host.append($('toast')); $('toast').textContent = text; $('toast').classList.remove('hidden'); clearTimeout(toast.timer); toast.timer = setTimeout(() => $('toast').classList.add('hidden'), 5000); }
async function api(path, options = {}) {
  const response = await fetch('/api' + path, { ...options, headers: { 'Authorization': 'Bearer ' + state.token, 'Content-Type': 'application/json', ...options.headers } });
  const data = await response.json();
  if (!response.ok) { if (response.status === 401 && !$('login-dialog').open) $('login-dialog').showModal(); throw new Error(t(data.error) || t("请求失败")); }
  return data;
}
function action(fn) { return async (...args) => { try { await fn(...args); } catch (e) { toast(e.message); } }; }
function setMode(mode) {
  state.mode = mode;
  document.querySelectorAll('.mode-switch button').forEach(b => b.classList.toggle('active', b.dataset.mode === mode));
  const phaseBar = $('workflow-phases');
  if (phaseBar) {
    if (mode === 'workflow') {
      phaseBar.classList.remove('hidden');
      phaseBar.querySelectorAll('.phase-btn').forEach((btn, i) => {
        btn.style.animation = 'none';
        btn.offsetHeight; // reflow
        btn.style.animation = 'phaseIn 0.35s ease forwards ' + (i * 70) + 'ms';
      });
    } else {
      phaseBar.querySelectorAll('.phase-btn').forEach((btn, i) => {
        btn.style.animation = 'phaseOut 0.25s ease forwards ' + ((3 - i) * 50) + 'ms';
      });
      setTimeout(() => { if (state.mode !== 'workflow') phaseBar.classList.add('hidden'); }, 400);
    }
  }
  updateAutoModeUI();
  if (typeof scheduleContextPreview === 'function') scheduleContextPreview();
}
// AI 工作流四阶段
state.workflowPhase = state.workflowPhase || '';
document.querySelectorAll('.phase-btn').forEach(btn => {
  btn.onclick = () => {
    const phase = btn.dataset.phase;
    if (state.workflowPhase === phase) {
      state.workflowPhase = '';
      btn.classList.remove('selected');
    } else {
      state.workflowPhase = phase;
      document.querySelectorAll('.phase-btn').forEach(b => b.classList.remove('selected'));
      btn.classList.add('selected');
    }
    updateAutoModeUI();
  };
});
// 自动编排模式：AI 工作流下未选任何阶段时，由后端 autoModePrompt 处理，前端不显示描述
function updateAutoModeUI() {}
async function refreshConfig() {
  state.config = await api('/config');
  $('connection').textContent = t("● 本地服务已连接"); $('connection').classList.add('ready');
  const versionText = state.config.version ? 'v' + state.config.version : 'dev';
  $('app-version').textContent = versionText;
  $('settings-sheet-version').textContent = ' · aide ' + versionText;
  $('model-status').textContent = state.config.configured ? t("已配置") : t("未配置");
  $('model-name').textContent = state.config.configured ? state.config.model + t(" · API 已配置") : t("先配置模型，即可开始真实 AI 对话");
  if (typeof resetIdleTimer === "function") resetIdleTimer();
  estimateContext();
  if (typeof scheduleContextPreview === 'function') scheduleContextPreview();
}
const subGroupState = {}; // 主会话 id -> { collapsed, expandAll }：已归档子会话折叠组状态，跨 loadSessions 重渲染保留
async function loadSessions() {
  const [sessions, archived] = await Promise.all([api('/sessions'), api('/sessions?archived=1')]);
  $('sessions').replaceChildren();
  // 层级：无 parentId 为主会话；有 parentId 为子会话。active 列表里的子会话=运行中（未归档），archived 列表里的=完成后自动归档。
  const childrenOf = {}, doneChildrenOf = {}, mains = [];
  sessions.forEach(s => { if (s.parentId) (childrenOf[s.parentId] = childrenOf[s.parentId] || []).push(s); else mains.push(s); });
  archived.forEach(s => { if (s.parentId) (doneChildrenOf[s.parentId] = doneChildrenOf[s.parentId] || []).push(s); });
  if (!sessions.length) $('sessions').append(el('p', 'sessions-empty', t("还没有会话。\n从一个想法开始吧。")));

  // 单个会话条目（主/子共用）：主会话原样；子会话加 sub-session 缩进类与 ↳ 前缀
  const buildItem = (s, isSub) => {
    const isActive = state.session?.id === s.id;
    // 高亮（蓝点+加粗）只给“完成且未被查看”的会话；查看后由后端 checked 持久化清除
    const highlight = s.status === 'completed' && !s.checked;
    const item = el('div', 'session-item' + (isActive ? ' active' : '') + (s.pinned ? ' pinned' : '') + (highlight ? ' status-completed' : '') + (isSub ? ' sub-session' : ''));
    item.title = s.title;
    // 状态机：运行中=荧光绿闪烁、等待审批=黄常亮、失败=红常亮、完成=蓝；其余无点。
    const dotClass = { running: 'dot-running', failed: 'dot-failed', awaiting_approval: 'dot-await', completed: 'dot-done' }[s.status] || '';
    // 完成且已查看：不显示蓝点；已自动归档的子会话用灰标签替代蓝点
    if (dotClass && !(s.status === 'completed' && s.checked) && !(isSub && s.autoArchived)) item.append(el('span', 'session-dot ' + dotClass, ''));
    const label = el('span', 'session-label' + (isSub ? ' sub-session-label' : ''), s.title);
    label.onclick = action(() => selectSession(s.id));
    if (isSub && s.autoArchived) label.append(el('span', 'sub-session-badge', t("已完成")));
    const more = el('button', 'session-more', '⋯');
    more.setAttribute('aria-label', t("会话操作"));
    more.onclick = e => {
      e.stopPropagation();
      document.querySelectorAll('.session-menu').forEach(m => m.remove());
      const menu = el('div', 'session-menu');
      const closeMenu = () => { menu.remove(); };
      const pinBtn = el('button', 'menu-item', s.pinned ? t("取消置顶") : t("置顶"));
      pinBtn.onclick = action(async () => { await api(`/sessions/${s.id}`, { method: 'PATCH', body: JSON.stringify({ pinned: !s.pinned }) }); closeMenu(); await loadSessions(); });
      const archBtn = el('button', 'menu-item', s.archived ? t("取消归档") : t("归档"));
      archBtn.onclick = action(async () => {
        const willArchive = !s.archived;
        await api(`/sessions/${s.id}`, { method: 'PATCH', body: JSON.stringify({ archived: willArchive }) });
        closeMenu();
        // 归档当前正在查看的会话后，自动回到新会话输入界面（与删除当前会话行为一致）；取消归档不打断当前视图
        if (willArchive && state.session?.id === s.id) { await newSession(); return; }
        await loadSessions();
      });
      const delBtn = el('button', 'menu-item danger', t("删除"));
      delBtn.onclick = action(async () => {
        if (!confirm(t("确定删除这个会话？此操作不可撤销。"))) return;
        await api(`/sessions/${s.id}`, { method: 'DELETE' });
        closeMenu();
        if (state.session?.id === s.id) { clearTimeout(state.poll); closeStream(); state.session = null; state.sessionJSON = ''; renderSession(); }
        await loadSessions();
      });
      menu.append(pinBtn, archBtn, delBtn);
      // 挂到 body 用 fixed 定位：测量尺寸后贴 ⋯ 按钮，空间不足则向上翻，不受侧栏滚动裁切
      document.body.append(menu);
      const btnRect = more.getBoundingClientRect();
      const mw = menu.offsetWidth, mh = menu.offsetHeight;
      menu.style.left = Math.min(Math.max(8, btnRect.right - mw), window.innerWidth - mw - 8) + 'px';
      menu.style.top = (btnRect.bottom + mh + 8 <= window.innerHeight ? btnRect.bottom + 4 : Math.max(8, btnRect.top - mh - 4)) + 'px';
      setTimeout(() => { const close = () => { closeMenu(); document.removeEventListener('click', close); }; document.addEventListener('click', close); }, 0);
    };
    item.append(label, more);
    return item;
  };

  const renderedMains = new Set(mains.map(m => m.id));
  mains.forEach(m => {
    $('sessions').append(buildItem(m, false));
    // 运行中子会话（未归档）：直接缩进列出，运行灯复用 dot-running
    (childrenOf[m.id] || []).forEach(c => $('sessions').append(buildItem(c, true)));
    // 完成后自动归档的子会话：折叠组，默认展开最近 3 个
    const done = doneChildrenOf[m.id] || [];
    if (done.length) {
      const st = subGroupState[m.id] = subGroupState[m.id] || { collapsed: false, expandAll: false };
      const toggle = el('div', 'sub-group-toggle');
      toggle.onclick = () => { st.collapsed = !st.collapsed; loadSessions(); };
      toggle.append(el('span', 'sub-group-caret', st.collapsed ? '▸' : '▾'));
      toggle.append(el('span', 'sub-group-title', t("子会话") + ' (' + done.length + ')'));
      $('sessions').append(toggle);
      if (!st.collapsed) {
        const shown = st.expandAll ? done : done.slice(0, 3);
        shown.forEach(c => $('sessions').append(buildItem(c, true)));
        if (done.length > 3) {
          const more = el('div', 'sub-group-toggle sub-group-more');
          more.onclick = e => { e.stopPropagation(); st.expandAll = !st.expandAll; loadSessions(); };
          more.append(el('span', '', st.expandAll ? t("收起") : t("展开全部 (+" + (done.length - 3) + ")")));
          $('sessions').append(more);
        }
      }
    }
  });
  // 父会话已归档而子会话仍在活动列表中的孤儿子会话：顶层兜底渲染，避免丢失
  Object.keys(childrenOf).forEach(pid => {
    if (!renderedMains.has(pid)) childrenOf[pid].forEach(c => $('sessions').append(buildItem(c, true)));
  });
  return sessions;
}
const sessionSeq = { value: 0 }; // R07：递增请求序号，旧响应不得覆盖新选择
async function selectSession(id) {
  const seq = ++sessionSeq.value;
  const sameSession = state.session?.id === id;
  clearTimeout(state.poll); closeStream();
  ttsCancel();
  // 查看完成会话：清除“蓝点+加粗”高亮（持久化；不阻塞会话加载，失败静默）
  api(`/sessions/${id}`, { method: 'PATCH', body: JSON.stringify({ check: true }) }).catch(() => {});
  if (!sameSession) {
    // live 文本按 run 归属：切换会话才失效；同会话刷新（排队/插话等）保留流式状态，
    // 避免打断正在流式渲染的回答（closeStream 后 schedulePoll 会重连，live 丢失会造成文本回退）
    state.live = {}; state.liveRound = {}; state.liveTool = {}; state.liveReasoning = {}; state.runPhase = {}; state.streamRetryAt = 0;
  }
  const loaded = await api('/sessions/' + id);
  if (seq !== sessionSeq.value) return; // 已有更新的选择，丢弃本次过期响应
  const changed = JSON.stringify(loaded) !== state.sessionJSON;
  state.session = loaded;
  state.sessionJSON = JSON.stringify(loaded);
  if (changed || !sameSession) renderSession(); // 数据未变时跳过重渲染，点击更轻快
  refreshCompactInfo();
  await loadSessions();
  schedulePoll();
  $('prompt').focus(); // 点击会话后直接可输入；焦点离开 body 也避免误触全局快捷键
  if (typeof scheduleContextPreview === 'function') scheduleContextPreview();
}
function closeStream() {
  if (state.stream) { try { state.stream.close(); } catch (e) {} state.stream = null; }
}
function openStream(run) {
  closeStream();
  const es = new EventSource('/api/sessions/' + state.session.id + '/runs/' + run.id + '/events?access_token=' + encodeURIComponent(state.token));
  es._runId = run.id;
  state.stream = es;
  state.streamRetryAt = 0;
  ensureRunPhase(run.id);
  es.addEventListener('step', () => { touchRunActivity(run.id); refreshSessionSoon(); });
  es.addEventListener('tool', e => {
    let d; try { d = JSON.parse(e.data); } catch (err) { return; }
    state.liveTool[run.id] = d;
    const ph = state.runPhase[run.id];
    if (ph) {
      const row = ph.tools.find(x => x.callId === d.callId) || ph.tools[ph.tools.length - 1];
      if (row) { row.endedAt = Date.now(); row.ok = !!d.ok; row.status = d.ok ? 'done' : 'err'; }
      ph.phase = 'reasoning'; ph.toolName = '';
      touchRunActivity(run.id); renderRunStatus(run.id);
    }
    refreshSessionSoon();
  });
  es.addEventListener('intent', e => {
    let d; try { d = JSON.parse(e.data); } catch (err) { return; }
    const ph = state.runPhase[run.id];
    if (ph) {
      ph.tools.push({ callId: d.callId || ('c'+Date.now()+Math.random()), tool: d.tool, args: d.args || '', startedAt: Date.now(), status: 'running' });
      ph.phase = 'tool'; ph.toolName = d.tool;
      touchRunActivity(run.id); renderRunStatus(run.id);
    }
  });
  es.addEventListener('reasoning', e => {
    let d; try { d = JSON.parse(e.data); } catch (err) { return; }
    state.liveReasoning[run.id] = (state.liveReasoning[run.id] || '') + (d.reasoning || '');
    const ph = state.runPhase[run.id];
    if (ph) { ph.phase = 'reasoning'; touchRunActivity(run.id); renderRunStatus(run.id); }
  });
  es.addEventListener('heartbeat', () => touchRunActivity(run.id)); // 长命令心跳：证明活着，看门狗复位
  es.addEventListener('note', e => {
    let d; try { d = JSON.parse(e.data); } catch (err) { return; }
    const ph = state.runPhase[run.id];
    if (ph) { ph.note = d.text || ''; touchRunActivity(run.id); renderRunStatus(run.id); }
  }); // 空响应自动续接提示
  es.addEventListener('clarification', () => refreshSessionSoon());
  es.addEventListener('delta', e => {
    let d; try { d = JSON.parse(e.data); } catch (err) { return; }
    const ph = state.runPhase[run.id];
    if (ph) { ph.phase = 'generating'; touchRunActivity(run.id); }
    const target = state.session?.runs?.find(r => r.id === run.id);
    if (target?.mode !== 'chat') return; // workflow 步骤不渲染 live 文本
    // 同一步骤内每轮 toolLoop 会重开一次模型请求：轮次变化时重置，避免拼接上一轮的叙述
    if (typeof d.round === 'number' && state.liveRound[run.id] !== d.round) {
      state.live[run.id] = ''; state.liveRound[run.id] = d.round;
    }
    state.live[run.id] = (state.live[run.id] || '') + (d.text || '');
    scheduleLiveRender(run.id); // 按动画帧批量渲染，避免逐 token 全量 markdown 解析
  });
  es.addEventListener('done', () => {
    delete state.live[run.id]; delete state.liveRound[run.id]; delete state.liveTool[run.id]; delete state.liveReasoning[run.id];
    if (state.runPhase[run.id]) state.runPhase[run.id].done = true;
    closeStream(); state.streamRetryAt = 0;
    action(async () => {
      const id = state.session?.id; if (!id) return;
      const s = await api('/sessions/' + id); if (state.session?.id !== id) return;
      if (adoptSessionIfChanged(s)) renderSession();
      // 小秘语音发起的 run 完成且开启语音回复 → 朗读最后一条 assistant 回复
      if (voice.awaitingReply) {
        voice.awaitingReply = false;
        if (state.config && state.config.voiceReplyEnabled) {
          const last = [...(s.messages || [])].reverse().find(m => m.role === 'assistant' && m.content && m.content.trim());
          if (last) speakReply(last.content);
        }
      }
      // 正在查看的会话完成 → 视为已检查，直接清除高亮；后台完成的会话保持蓝点+加粗待点击
      if (s.runs.some(r => r.status === 'completed')) {
        try { await api(`/sessions/${id}`, { method: 'PATCH', body: JSON.stringify({ check: true }) }); } catch (err) {}
      }
      await loadSessions(); // 任务完成：AI 已更新标题，同步侧栏会话列表
      schedulePoll();
      scheduleTitleSync(id); // 主题总结是后台异步调用：稍后补一次同步标题
    })();
  });
  // 流断开：保留已积累的 live 文本，短暂退避后由轮询兜底重开；最终状态仍以会话接口为准
  es.onerror = () => { closeStream(); state.streamRetryAt = Date.now() + 5000; };
}
// 流式渲染节流：delta 先累积，按动画帧批量渲染（每帧最多一次全量解析）
const liveRenderScheduled = new Set();
function scheduleLiveRender(runId) {
  if (liveRenderScheduled.has(runId)) return;
  liveRenderScheduled.add(runId);
  requestAnimationFrame(() => {
    liveRenderScheduled.delete(runId);
    renderLiveAnswer(runId);
  });
}
function renderLiveAnswer(runId) {
  const box = document.querySelector('#timeline .run[data-run="' + runId + '"]');
  const all = document.querySelectorAll('#timeline .chat-answer');
  const ans = box?.querySelector('.chat-answer') || (all.length ? all[all.length - 1] : null);
  if (!ans) return;
  // 运行中的 chat 步骤以 live 文本为准（step.content 可能还是“工具调用中”或尚未提交）
  const run = state.session?.runs?.find(r => r.id === runId);
  const step = run?.steps?.[run.steps.length - 1];
  const live = state.live[runId] || '';
  const full = live || (step?.content || '');
  // live 渲染跳过代码高亮（每帧高亮大段代码代价高），完成态由 renderSession 全量渲染
  ans.innerHTML = (full ? renderMarkdown(full, true) : '') + '<span class="stream-cursor" aria-hidden="true">▍</span>';
  const c = $('conversation');
  if (state.autoScroll) c.scrollTo({ top: c.scrollHeight, behavior: 'instant' });
  updateJumpBtn();
}
function updateJumpBtn() {
  const c = $('conversation');
  const dist = c.scrollHeight - c.scrollTop - c.clientHeight;
  const nearBottom = dist < 80;
  const btn = $('jump-bottom');
  if (state.jumpAnimating) {
    // 平滑回底动画期间：按钮立即隐藏、保持自动跟随；动画到底后由 scroll 事件自然收尾
    state.autoScroll = true;
    if (btn) btn.classList.add('hidden');
    if (nearBottom) state.jumpAnimating = false;
    return;
  }
  state.autoScroll = nearBottom;
  // 按钮仅在离开底部约一个自然页后才出现
  if (btn) btn.classList.toggle('hidden', dist < c.clientHeight * 0.8);
}
$('conversation')?.addEventListener('scroll', updateJumpBtn);
$('conversation')?.addEventListener('wheel', () => { state.jumpAnimating = false; }, { passive: true });
$('conversation')?.addEventListener('touchstart', () => { state.jumpAnimating = false; }, { passive: true });
$('jump-bottom')?.addEventListener('click', () => {
  const c = $('conversation');
  state.autoScroll = true;
  state.jumpAnimating = true;
  c.scrollTo({ top: c.scrollHeight, behavior: 'smooth' });
  setTimeout(() => { state.jumpAnimating = false; }, 1200); // 兜底：动画未触发滚动事件时复位
});
// 会话快照未变化时跳过整页重渲染（流式期间 step.content/usage 未变，轮询只做轻量校验）
function adoptSessionIfChanged(s) {
  const next = JSON.stringify(s);
  if (next === state.sessionJSON) return false;
  state.sessionJSON = next;
  state.session = s;
  return true;
}
function renderLiveTool(runId) {
  const t = state.liveTool[runId];
  const box = document.querySelector('#timeline .run[data-run="' + runId + '"]');
  if (!box || !t) return;
  let row = box.querySelector('.live-tool');
  if (!row) {
    const ans = box.querySelector('.chat-answer');
    row = el('div', 'live-tool');
    if (ans) ans.insertAdjacentElement('afterend', row); else box.append(row);
  }
  row.textContent = '⚒ ' + t.tool + (t.preview ? ' · ' + String(t.preview).slice(0, 80) : '');
}

// ── 统一运行状态面板：阶段指示 / 秒表 / 工具逐项 / 思考折叠 / 卡死看门狗 ──
const STALL_MS = 90000; // 完全无事件超时阈值（可在此调整；工具执行期间有 heartbeat 不算超时）
function ensureRunPhase(runId) {
  if (!state.runPhase[runId]) {
    state.runPhase[runId] = { startedAt: Date.now(), phase: 'waiting', toolName: '', tools: [], lastActivity: Date.now(), stalled: false, done: false };
  }
  return state.runPhase[runId];
}
function touchRunActivity(runId) {
  const ph = state.runPhase[runId];
  if (!ph) return;
  ph.lastActivity = Date.now(); ph.stalled = false;
}
function phaseLabel(ph) {
  if (ph.phase === 'reasoning') return t('模型思考中');
  if (ph.phase === 'tool') return t('正在调用 ') + (ph.toolName || t('工具'));
  if (ph.phase === 'generating') return t('正在生成回答');
  return t('等待模型响应');
}
function formatElapsed(startedAt) {
  const sec = Math.max(0, Math.floor((Date.now() - startedAt) / 1000));
  const m = Math.floor(sec / 60);
  return m > 0 ? m + ':' + String(sec % 60).padStart(2, '0') : sec + 's';
}
function renderToolRow(tool) {
  const row = el('div', 'rsp-tool' + (tool.status === 'running' ? ' running' : tool.status === 'err' ? ' err' : ''));
  const icon = tool.status === 'running' ? '◌' : tool.status === 'err' ? '✗' : '✓';
  row.append(el('span', 'rsp-tool-icon', icon));
  row.append(el('span', 'rsp-tool-name', tool.tool));
  if (tool.args) row.append(el('span', 'rsp-tool-args', tool.args));
  row.append(el('span', 'rsp-tool-dur', tool.endedAt ? Math.round((tool.endedAt - tool.startedAt) / 1000) + 's' : ''));
  return row;
}
function stopRunById(runId) {
  api('/sessions/' + state.session.id + '/runs/' + runId + '/cancel', { method: 'POST', body: '{}' }).then(() => toast(t('已请求停止'))).catch(() => {});
}
function retryRunById(runId) {
  api('/sessions/' + state.session.id + '/runs/' + runId + '/retry', { method: 'POST', body: '{}' }).then(() => selectSession(state.session.id)).catch(() => {});
}
function renderRunStatusInto(box, runId) {
  const ph = state.runPhase[runId];
  if (!box || !ph) return;
  let panel = box.querySelector('.run-status-panel');
  if (!panel) {
    panel = el('div', 'run-status-panel');
    const meta = box.querySelector('.run-meta');
    if (meta) meta.insertAdjacentElement('afterend', panel); else box.prepend(panel);
  }
  // 头部：spinner + 阶段 + 计时
  let head = panel.querySelector('.rsp-head');
  if (!head) { head = el('div', 'rsp-head'); panel.append(head); }
  head.replaceChildren(el('span', 'rsp-spinner'), el('span', 'rsp-phase', phaseLabel(ph)), el('span', 'rsp-elapsed', formatElapsed(ph.startedAt)));
  let noteEl = panel.querySelector('.run-note');
  if (ph.note) {
    if (!noteEl) { noteEl = el('div', 'run-note'); panel.append(noteEl); }
    noteEl.textContent = ph.note;
  } else if (noteEl) { noteEl.remove(); }
  // 卡死横幅
  let banner = panel.querySelector('.rsp-banner');
  if (ph.stalled) {
    if (!banner) { banner = el('div', 'rsp-banner'); panel.append(banner); }
    banner.replaceChildren();
    banner.append(el('div', 'rsp-banner-msg', t('长时间无响应，可能已卡住')));
    const actions = el('div', 'rsp-banner-actions');
    const stop = el('button', 'quiet', t('停止')); stop.onclick = () => stopRunById(runId);
    const wait = el('button', 'quiet', t('继续等待')); wait.onclick = () => { ph.stalled = false; ph.lastActivity = Date.now(); renderRunStatus(runId); };
    const retry = el('button', 'quiet', t('重试')); retry.onclick = () => retryRunById(runId);
    actions.append(stop, wait, retry);
    banner.append(actions);
  } else if (banner) { banner.remove(); }
  // 思考折叠区（流式实时追加；运行中默认展开窥测）
  const reasoning = state.liveReasoning[runId] || '';
  let det = panel.querySelector('.rsp-thinking');
  if (reasoning) {
    if (!det) { det = el('details', 'rsp-thinking'); det.open = true; panel.append(det); }
    const sum = el('summary', '', (ph.done ? t('思考过程') : t('思考中…')));
    const body = el('div', 'rsp-think-body'); body.textContent = reasoning;
    det.replaceChildren(sum, body);
  } else if (det) { det.remove(); }
  // 工具逐项列表
  let list = panel.querySelector('.rsp-tools');
  if (ph.tools.length) {
    if (!list) { list = el('div', 'rsp-tools'); panel.append(list); }
    list.replaceChildren();
    ph.tools.forEach(tool => list.append(renderToolRow(tool)));
  } else if (list) { list.remove(); }
}
function renderRunStatus(runId) {
  const box = document.querySelector('#timeline .run[data-run="' + runId + '"]');
  renderRunStatusInto(box, runId);
}
// 1s 心跳：刷新计时数字 + 看门狗超时检测
setInterval(() => {
  const now = Date.now();
  for (const runId in state.runPhase) {
    const ph = state.runPhase[runId];
    if (!ph || ph.done) continue;
    const box = document.querySelector('#timeline .run[data-run="' + runId + '"]');
    if (!box) continue;
    const el2 = box.querySelector('.rsp-elapsed');
    if (el2) el2.textContent = formatElapsed(ph.startedAt);
    if (!ph.stalled && now - ph.lastActivity > STALL_MS) { ph.stalled = true; renderRunStatus(runId); }
  }
}, 1000);
let titleSyncTimers = [];
function scheduleTitleSync(id) {
  titleSyncTimers.forEach(clearTimeout); titleSyncTimers = [];
  // 主题总结是后台异步调用，可能在任务完成之后才落库：分两轮补同步（无变化时不会重渲染）
  for (const delay of [2000, 5000, 9000]) {
    titleSyncTimers.push(setTimeout(action(async () => {
      if (state.session?.id !== id) return;
      const s2 = await api('/sessions/' + id);
      if (state.session?.id !== id) return;
      if (adoptSessionIfChanged(s2)) renderSession();
      await loadSessions();
    }), delay));
  }
}
let refreshSoonTimer = 0;
function refreshSessionSoon() {
  clearTimeout(refreshSoonTimer);
  refreshSoonTimer = setTimeout(action(async () => {
    const id = state.session?.id; if (!id) return;
    const s = await api('/sessions/' + id); if (state.session?.id !== id) return;
    if (adoptSessionIfChanged(s)) renderSession();
  }), 250);
}
function schedulePoll() {
  clearTimeout(state.poll);
  const running = state.session?.runs?.find(r => r.status === 'running');
  if (running) {
    if ((!state.stream || state.stream._runId !== running.id) && (!state.streamRetryAt || Date.now() >= state.streamRetryAt)) openStream(running);
    state.poll = setTimeout(action(async () => {
      const id = state.session.id; const s = await api('/sessions/' + id); if (state.session?.id !== id) return;
      const hadRunning = !!state.session?.runs?.find(r => r.status === 'running');
      if (adoptSessionIfChanged(s)) renderSession();
      if (hadRunning && !s.runs.some(r => r.status === 'running')) {
        if (s.runs.some(r => r.status === 'completed')) {
          try { await api(`/sessions/${id}`, { method: 'PATCH', body: JSON.stringify({ check: true }) }); } catch (err) {}
        }
        await loadSessions(); // 轮询兜底路径：完成时同步列表
        scheduleTitleSync(id);
      }
      schedulePoll();
    }), 1500);
  } else {
    closeStream();
  }
}
async function newSession() {
  clearTimeout(state.poll); closeStream(); state.live = {}; state.liveRound = {}; state.liveTool = {}; state.liveReasoning = {}; state.runPhase = {}; state.streamRetryAt = 0; state.sessionJSON = ''; state.session = null; state.attachments = []; renderAttachments(); renderSession(); await loadSessions(); $('prompt').focus(); if (typeof hideContextPreview === 'function') hideContextPreview();
}
const labels = { plan: '01 · 规划', propose: '02 · 生成方案', review: '03 · 审查', chat: 'aide' };
function toolSummaryBrief(use) {
  try {
    const args = JSON.parse(use.args || '{}');
    const brief = Object.values(args)[0];
    if (typeof brief === 'string') return ' · ' + brief.slice(0, 60);
  } catch (error) { /* 非 JSON 参数直接忽略 */ }
  return '';
}
const statuses = { running: '运行中', completed: '已完成', failed: '失败', cancelled: '已停止', interrupted: '已中断', awaiting_approval: '等待应用', awaiting_clarification: '等待澄清' };
// 澄清卡片：在会话流中渲染单个交互问题（选项/输入/确认条），点击即作为应答
function renderClarification(run, box) {
  if (!run.pendingQuestion) return;
  let q; try { q = typeof run.pendingQuestion === 'string' ? JSON.parse(run.pendingQuestion) : run.pendingQuestion; } catch (e) { return; }
  const card = el('div', 'clarify-card');
  if (q.progressTotal) card.append(el('div', 'clarify-progress', t('澄清 {0}/{1}', q.progressCurrent || 1, q.progressTotal)));
  card.append(el('div', 'clarify-question', q.question));
  const answer = async (text) => {
    card.querySelectorAll('button,input').forEach(x => x.disabled = true);
    try { await api(`/sessions/${state.session.id}/runs/${run.id}/answer`, { method: 'POST', body: JSON.stringify({ answer: text }) }); await selectSession(state.session.id); schedulePoll(); }
    catch (e) { toast(e.message); card.querySelectorAll('button,input').forEach(x => x.disabled = false); }
  };
  if (q.type === 'confirm') {
    const row = el('div', 'clarify-actions');
    const ok = el('button', 'primary', t('确认，继续')); ok.onclick = () => answer('确认');
    const adj = el('button', 'quiet', t('需要调整')); adj.onclick = () => answer('需要调整');
    row.append(ok, adj); card.append(row);
  } else if (q.type === 'input') {
    const row = el('div', 'clarify-actions');
    const input = el('input', 'clarify-input'); input.placeholder = t('输入你的回答…');
    const send = el('button', 'primary', t('发送')); send.onclick = () => answer(input.value.trim() || '(空)');
    input.onkeydown = e => { if (e.key === 'Enter') answer(input.value.trim() || '(空)'); };
    row.append(input, send); card.append(row);
  } else {
    (q.options || []).forEach(o => { const b = el('button', 'clarify-option', o); b.onclick = () => answer(o); card.append(b); });
    const other = el('button', 'clarify-option clarify-other', t('其他 / 我自己说'));
    other.onclick = () => { $('prompt').focus(); toast(t('直接在输入框回答后发送即可')); };
    card.append(other);
  }
  box.append(card);
}
function renderSession() {
  const previousScroll = $('conversation').scrollTop;
  const nearBottom = $('conversation').scrollHeight - previousScroll - $('conversation').clientHeight < 100;
  const openDetails = new Set([...$('timeline').querySelectorAll('details[open][data-key]')].map(d => d.dataset.key));
  $('session-title').textContent = state.session?.title || t("开始新的探索");
  $('welcome').classList.toggle('hidden', !!state.session?.runs.length);
  $('timeline').replaceChildren(); state.busy = false;
  for (const run of state.session?.runs || []) {
    if (run.status === 'running') state.busy = true;
    const box = el('article', 'run'); box.dataset.run = run.id; box.append(el('div', 'user-message', run.prompt));
    // 运行中插话（steered）的消息渲染进时间线；排队中的消息由队列条展示
    for (const st of run.steers || []) {
      if (st.queued) continue;
      const msg = el('div', 'steer-msg');
      msg.append(el('span', 'steer-tag', t("插话")), document.createTextNode(st.content));
      box.append(msg);
    }
    const meta = el('div', 'run-meta'); meta.append(el('span', '', run.mode === 'workflow' ? t("◈ AIDE WORKFLOW · 规划 → 方案 → 审查") : '◌ AIDE ASSISTANT'), el('span', 'run-model', run.model || ''), el('span', 'run-status', t(statuses[run.status] || run.status))); if (run.strategy) meta.append(el('span', 'run-strategy', t("策略: ") + (run.strategy === 'auto' ? t("自动 → ") + profileName(run.profile) : t("手动 · ") + profileName(run.profile)))); box.append(meta);
    if (run.status === 'running' && state.runPhase[run.id]) renderRunStatusInto(box, run.id);
    if (run.attachments?.length) box.append(el('p', 'muted', t("已附加：") + run.attachments.map(a => a.root + '/' + a.path).join('、')));
    renderClarification(run, box);
    if (!run.steps.length) box.append(el('p', 'muted', t("正在准备模型请求…")));
    run.steps.forEach((step, index) => {
      if (run.mode === 'chat') {
        if (step.reasoning) { const rd = el('details', 'rsp-thinking rsp-thinking-done'); rd.append(el('summary', '', t("思考过程")), el('div', 'rsp-think-body', step.reasoning)); box.append(rd); }
        const ans = el('div', 'chat-answer md-body');
        const running = run.status === 'running';
        const liveText = state.live[run.id] || '';
        // 运行中优先显示实时增量（step.content 尚未提交，可能还是“工具调用中”），
        // 结束后只用持久化内容，避免 live 与 content 重复拼接
        const text = running && liveText ? liveText : (step.content || '');
        if (text) {
          ans.innerHTML = renderMarkdown(text) + (running ? '<span class="stream-cursor" aria-hidden="true">▍</span>' : '');
        } else if (running) {
          // DSH/Codex 风格：首 token 前显示呼吸的思考点
          ans.innerHTML = t("正在思考") + '<span class="thinking-dot" aria-hidden="true">.</span><span class="thinking-dot" aria-hidden="true" style="animation-delay:.2s">.</span><span class="thinking-dot" aria-hidden="true" style="animation-delay:.4s">.</span>';
        } else if (run.error) {
          // 真·失败：显示具体原因（上游错误/超时/取消），不再是光秃秃的“未返回回答”
          ans.classList.add('chat-answer-error');
          ans.textContent = '⚠ ' + run.error;
        } else {
          ans.classList.add('chat-answer-empty');
          ans.textContent = t("未返回回答") + '（' + (run.toolUses?.length ? t("已完成 {0} 次工具调用", run.toolUses.length) : t("模型未生成正文")) + '）';
        }
        box.append(ans);
        // 消息操作按钮：结束后（无论有无正文/是否失败）都给「重试 / 继续」，
        // 让用户在“做了一堆工具却没结论”时能直接续接；复制/好/坏仅在有正文时可用
        if (!running) {
          const actions = el('div', 'msg-actions');
          const mk = (label, fn) => {
            const b = el('button', 'msg-btn', label);
            b.type = 'button';
            b.onclick = fn;
            return b;
          };
          if (text) actions.append(mk(t("复制"), () => { navigator.clipboard.writeText(text).then(() => toast(t("已复制"))); }));
          actions.append(mk(t("重试"), () => { api(`/sessions/${state.session.id}/runs/${run.id}/retry`, { method: 'POST', body: '{}' }).then(() => selectSession(state.session.id)); }));
          actions.append(mk(t("继续"), () => { $('prompt').value = ''; sendPrompt(t("继续")); }));
          if (text) {
            actions.append(mk(t("好"), () => {
              api('/feedback', { method: 'POST', body: JSON.stringify({ runId: run.id, prompt: run.prompt, answer: text, rating: 'good' }) })
                .then(() => toast(t("已记录到记忆")))
                .catch(() => toast(t("记录失败")));
            }));
            actions.append(mk(t("有问题"), () => {
              api('/feedback', { method: 'POST', body: JSON.stringify({ runId: run.id, prompt: run.prompt, answer: text, rating: 'bad' }) })
                .then(() => toast(t("已记录到记忆")))
                .catch(() => toast(t("记录失败")));
            }));
          }
          box.append(actions);
        }
        return;
      }
      const details = el('details', 'step'); details.dataset.key = run.id + ':' + step.name;
      details.open = openDetails.has(details.dataset.key) || (index === run.steps.length - 1 && step.name !== 'propose');
      const summary = el('summary', '', t(labels[step.name])); summary.append(el('span', '', t(statuses[step.status])));
      if (step.name === 'propose') {
        details.append(summary, el('pre', 'step-content', step.content || t("正在调用模型…")));
      } else {
        const md = el('div', 'step-content md-body');
        md.innerHTML = renderMarkdown(step.content || t("正在调用模型…"));
        details.append(summary, md);
      }
      box.append(details);
    });
    if (run.files?.length) {
      const proposal = el('div', 'proposal'); proposal.append(el('h4', '', t("文件修改 · {0} 个文件", run.files.length)));
      for (const file of run.files) {
        const details = el('details'); details.dataset.key = run.id + ':' + file.path; details.open = openDetails.has(details.dataset.key);
        details.append(el('summary', '', (file.applied ? '✓ ' : '+ ') + file.path));
        const diff = el('div', 'diff-columns'); const before = el('div'); before.append(el('small', '', t("原内容")), el('pre', '', file.before || t("（新文件）")));
        const after = el('div'); after.append(el('small', '', t("建议内容")), el('pre', '', file.content)); diff.append(before, after); details.append(diff); proposal.append(details);
      }
      if (run.status === 'awaiting_approval') { const apply = el('button', 'primary', t("应用这些文件修改")); apply.onclick = action(async () => { apply.disabled = true; try { await api(`/sessions/${state.session.id}/runs/${run.id}/apply`, { method: 'POST', body: '{}' }); toast(t("文件修改已写入本地挂载目录")); await selectSession(state.session.id); await loadFiles(); } finally { apply.disabled = false; } }); proposal.append(el('p', 'muted', t("请展开检查文件内容。应用后会写入本地工作目录；验证命令需要单独运行。")), apply); }
      else if (run.applied) proposal.append(el('p', 'muted', t("✓ 已应用文件修改。命令验证结果以命令面板为准。")));
      box.append(proposal);
    }
    if (run.commands?.length) {
      box.append(el('p', 'muted', t("建议验证命令（尚未运行）")));
      run.commands.forEach(command => { const row = el('div', 'suggested-command'); const button = el('button', 'quiet', t("填入命令面板")); button.onclick = () => { $('terminal-body').classList.remove('hidden'); $('terminal-state').textContent = t("收起 −"); $('command').value = command; $('command').focus(); }; row.append(el('code', '', command), button); box.append(row); });
    }
    if (run.toolUses?.length) {
      // 文件类调用合并展示（只显示路径，轻量）；命令与其他工具单独折叠显示细节
      const fileUses = run.toolUses.filter(u => u.tool === 'read_file' || u.tool === 'list_files');
      const otherUses = run.toolUses.filter(u => u.tool !== 'read_file' && u.tool !== 'list_files');
      if (fileUses.length) {
        const details = el('details', 'tool-use');
        details.dataset.key = run.id + ':files';
        const summary = el('summary', '', t("⚒ 文件查看 · ") + fileUses.length + t(" 次"));
        const paths = fileUses.map(u => { try { const a = JSON.parse(u.args || '{}'); return (a.source ? 'sources/' + a.source + ' · ' : '') + (a.path || '.'); } catch (e) { return '.'; } }).join('\n');
        details.append(summary, el('pre', 'tool-use-detail', paths));
        box.append(details);
      }
      otherUses.forEach((use, toolIndex) => {
        const details = el('details', 'tool-use');
        details.dataset.key = run.id + ':tool:' + toolIndex;
        details.open = false;
        const isCommand = use.tool === 'run_shell';
        const summary = el('summary', '', isCommand ? t("⚒ 建议命令") : t("⚒ 工具调用 · ") + use.tool);
        summary.append(el('span', '', toolSummaryBrief(use)));
        const detail = isCommand
          ? t("命令：\n") + toolSummaryBrief(use) + t("\n\n结果：\n") + (use.result || t("（无）"))
          : t("参数：") + (use.args || t("无")) + t("\n\n结果：\n") + (use.result || t("（无）"));
        details.append(summary, el('pre', 'tool-use-detail', detail));
        box.append(details);
      });
    }
    if (run.error) box.append(el('p', 'task-error', run.error)); $('timeline').append(box);
  }
  // 运行中：发送箭头原位切换为停止图标（插话/排队仍可用 Enter 或「排队」+Enter 提交）
  setSendMode(state.busy);
  const c = $('conversation');
  c.scrollTo({ top: nearBottom ? c.scrollHeight : previousScroll, behavior: 'instant' });
  estimateContext();
  renderQueueBar();
  updateJumpBtn();
}
function renderQueueBar() {
  const bar = $('queue-bar');
  if (!bar) return;
  bar.replaceChildren();
  const running = state.session?.runs?.find(r => r.status === 'running');
  const queued = (running?.steers || []).filter(st => st.queued);
  if (!running || !queued.length) { bar.classList.add('hidden'); return; }
  bar.classList.remove('hidden');
  // Codex 风格（pending_input_preview）：分区标题 + ↳ 条目 + 底部提示行，全部弱化样式
  bar.append(el('div', 'queue-head', '• ' + t("排队消息 · 当前回答结束后按顺序处理")));
  let qi = 0;
  for (const st of running.steers) {
    if (!st.queued) continue;
    const idx = qi++;
    const item = el('div', 'queue-item');
    const arrow = el('span', 'q-arrow', '↳');
    const content = el('span', 'q-content', st.content);
    const editBtn = el('button', 'q-btn', t('修改'));
    const delBtn = el('button', 'q-btn', t('删除'));
    const steerBtn = el('button', 'q-btn primary', t('插话'));
    editBtn.onclick = () => {
      const input = el('input', 'q-edit'); input.value = st.content;
      content.replaceWith(input); input.focus();
      let committed = false;
      const commit = action(async () => {
        if (committed) return;
        committed = true;
        const v = input.value.trim();
        if (!v) { await selectSession(state.session.id); return; }
        await api(`/sessions/${state.session.id}/runs/${running.id}/queue/${idx}`, { method: 'POST', body: JSON.stringify({ action: 'edit', content: v }) });
        await selectSession(state.session.id);
      });
      input.onkeydown = e => {
        if (e.key === 'Escape') { committed = true; selectSession(state.session.id); return; }
        if (e.key !== 'Enter' || e.isComposing) return;
        e.preventDefault();
        commit();
      };
      input.onblur = () => commit();
    };
    delBtn.onclick = action(async () => {
      await api(`/sessions/${state.session.id}/runs/${running.id}/queue/${idx}`, { method: 'POST', body: JSON.stringify({ action: 'delete' }) });
      await selectSession(state.session.id);
    });
    steerBtn.onclick = action(async () => {
      await api(`/sessions/${state.session.id}/runs/${running.id}/queue/${idx}`, { method: 'POST', body: JSON.stringify({ action: 'steer' }) });
      await selectSession(state.session.id);
    });
    item.append(arrow, content, editBtn, delBtn, steerBtn);
    bar.append(item);
  }
  bar.append(el('div', 'queue-hint', t("提示：点「插话」提升到下一轮立即处理；修改 / 删除即时生效")));
}
function renderAttachments() {
  $('attachment-chips').replaceChildren();
  if (typeof scheduleContextPreview === 'function') scheduleContextPreview();
  state.attachments.forEach((a, index) => { const chip = el('span', 'chip', (a.root === 'context' ? t("参考 · ") : '') + a.path); const b = el('button', '', '×'); b.setAttribute('aria-label', t("移除附件 ") + a.path); b.onclick = () => { state.attachments.splice(index, 1); renderAttachments(); }; chip.append(b); $('attachment-chips').append(chip); });
}
async function loadFiles() {
  const query = state.root === 'context' && state.source ? '/files?source=' + encodeURIComponent(state.source) + '&path=' : '/files?root=' + state.root + '&path=';
  const files = await api(query + encodeURIComponent(state.dir));
  const label = state.root === 'context' && state.source ? 'sources/' + (state.sources.find(x => x.id === state.source)?.name || state.source) : state.root;
  $('file-path').textContent = '/' + label + (state.dir === '.' ? '' : '/' + state.dir); $('file-path').title = $('file-path').textContent;
  $('new-file').disabled = state.root === 'context'; $('files').replaceChildren();
  if (!files.length) $('files').append(el('p', 'muted', t("目录为空")));
  files.forEach(file => {
    const b = el('button', 'file-item');
    const nameSpan = el('span', 'file-name', file.name);
    b.append(el('span', 'file-icon', file.dir ? '▱' : '≡'), nameSpan);
    if (file.dir) b.append(el('small', '', '›'));
    b.title = file.path;
    b.onclick = action(async () => { if (file.dir) { state.dir = file.path; await loadFiles(); } else await openFile(file.path); });
    // 点击文件名文字 → 内联重命名（阻止冒泡触发打开）；失焦或回车自动保存，Esc 取消
    nameSpan.onclick = (ev) => { ev.stopPropagation(); beginInlineRename(b, nameSpan, file); };
    $('files').append(b);
  });
}
/* 文件名内联重命名：点击名称进入编辑，失焦/回车保存，Esc 取消 */
function beginInlineRename(rowBtn, nameSpan, file) {
  if (rowBtn.querySelector('input.file-rename')) return;
  const original = file.name;
  const input = el('input', 'file-rename');
  input.value = original;
  nameSpan.replaceWith(input);
  input.focus();
  const dot = file.dir ? -1 : original.lastIndexOf('.');
  if (!file.dir && dot > 0) input.setSelectionRange(0, dot); else input.select();
  let settled = false;
  const restore = () => input.replaceWith(nameSpan);
  const commit = action(async () => {
    if (settled) return; settled = true;
    const nn = input.value.trim();
    if (nn === '' || nn === original) { restore(); return; }
    if (nn.includes('/') || nn.includes('\\')) { toast(t('文件名不能包含 / 或 \\')); restore(); return; }
    try {
      await api('/file/rename', { method: 'POST', body: JSON.stringify({ root: state.root, path: file.path, newName: nn }) });
      await loadFiles();
    } catch (e) { restore(); toast(e.message || String(e)); }
  });
  input.onblur = commit;
  input.onkeydown = (ev) => {
    if (ev.key === 'Enter') { ev.preventDefault(); input.blur(); }
    else if (ev.key === 'Escape') { settled = true; restore(); }
  };
}

async function openFile(path) {
  // 图片 / STL 走独立 raw 端点的可视化查看器，不经过只支持文本、会拒绝二进制的 /api/file
  if (isImagePath(path) || isStlPath(path)) {
    state.file = { path, root: state.root, source: state.root === 'context' ? state.source : '', content: '', editable: false, fresh: false, wsId: state.workspaceId || '' };
    showEditor();
    return;
  }
  const query = state.root === 'context' && state.source ? '/file?source=' + encodeURIComponent(state.source) + '&path=' : '/file?root=' + state.root + '&path=';
  const data = await api(query + encodeURIComponent(path)); state.file = { ...data, path, root: state.root, source: state.root === 'context' ? state.source : '', wsId: data.workspaceId || data.wsId || '', fresh: false }; showEditor();
}
function sourceIsRW() {
  if (state.file.root !== 'context' || !state.file.source) return false;
  return state.sources.find(x => x.id === state.file.source)?.rw === true;
}
function isMarkdownPath(path) { return /\.(md|markdown)$/i.test(path || ''); }
function isImagePath(path) { return /\.(png|jpe?g|gif|webp|svg|bmp|ico)$/i.test(path || ''); }
function isStlPath(path) { return /\.stl$/i.test(path || ''); }
function setEditorMode(mode) {
  const preview = mode === 'preview';
  $('editor').classList.toggle('hidden', preview);
  $('editor-preview').classList.toggle('hidden', !preview);
  $('editor-mode-edit').classList.toggle('active', !preview);
  $('editor-mode-preview').classList.toggle('active', preview);
  if (preview) { $('editor-preview').innerHTML = renderMarkdown($('editor').value, false, state.file ? state.file.path : ''); $('editor-preview').scrollTop = 0; }
}
function showEditor() {
  $('editor-title').textContent = state.file.path; $('editor').value = state.file.content;
  const readOnly = state.file.root === 'context' && !sourceIsRW();
  const md = isMarkdownPath(state.file.path);
  const isDrawio = /\.drawio$/i.test(state.file.path || '');
  const isImg = isImagePath(state.file.path);
  const isStl = isStlPath(state.file.path);
  $('editor').readOnly = readOnly;
  // 图片 / STL 为只读可视化查看器，无文本可保存，禁用保存（避免空内容覆盖原文件）；drawio 可保存
  $('save-file').disabled = readOnly || isImg || isStl;
  $('attach-file').disabled = state.file.fresh;
  $('editor-status').textContent = state.file.root === 'context' ? (sourceIsRW() ? t("辅助资料 · 读写来源") : t("辅助资料 · 只读")) : t("工作目录 · 保存后同步到主机");
  $('editor-mode-switch').classList.toggle('hidden', !md);
  if (isStl) {
    $('editor').classList.add('hidden');
    $('editor-preview').classList.remove('hidden');
    setupStlPreview($('editor-preview'), state.file.path, state.file.root, state.file.source || '');
  } else if (isImg) {
    $('editor').classList.add('hidden');
    $('editor-preview').classList.remove('hidden');
    setupImagePreview($('editor-preview'), state.file.path, state.file.root, state.file.source || '');
  } else if (isDrawio) {
    $('editor').classList.add('hidden');
    $('editor-preview').classList.remove('hidden');
    setupDrawioFrame($('editor-preview'), state.file.content, (xml) => {
      $('editor').value = xml;
      $('save-file').click();
      toast(t('draw.io 已保存'));
    }, '_editorDrawioHandler');
  } else {
    setEditorMode(md ? 'preview' : 'edit');
  }
  $('editor-dialog').showModal();
}
$('editor-mode-edit').onclick = () => setEditorMode('edit');
$('editor-mode-preview').onclick = () => setEditorMode('preview');
$('open-new-tab').onclick = () => {
  const spec = { root: state.file.source ? 'source' : state.file.root, source: state.file.source || '', path: state.file.path };
  window.open(location.pathname + '#file=' + encodeURIComponent(JSON.stringify(spec)), '_blank', 'noopener');
};
$('new-session').onclick = action(newSession); $('refresh-sessions').onclick = action(loadSessions); $('refresh-files').onclick = action(loadFiles);
document.querySelectorAll('.mode-switch button').forEach(b => b.onclick = () => setMode(b.dataset.mode));
document.querySelectorAll('.starter').forEach(b => b.onclick = () => { $('prompt').value = b.dataset.prompt; setMode(b.dataset.mode || 'chat'); $('prompt').focus(); });
document.querySelectorAll('[data-close]').forEach(b => b.onclick = () => $(b.dataset.close).close());
// 点击 dialog 遮罩关闭弹窗（事件委托，覆盖所有静态及动态 dialog）
document.addEventListener('click', e => { if (e.target.tagName === 'DIALOG' && e.target.open) e.target.close(); });
document.querySelectorAll('[data-root]').forEach(b => b.onclick = action(async () => { state.root = b.dataset.root; state.dir = '.'; if (state.root === 'context') state.source = ''; document.querySelectorAll('[data-root]').forEach(x => x.classList.toggle('active', x === b)); renderSourceChips(); await loadFiles(); }));
function syncPanelButtons() {
  const plugins = document.body.classList.contains('plugins-mode');
  const files = !plugins && (innerWidth <= 950 ? $('file-panel').classList.contains('mobile-open') : !document.body.classList.contains('files-hidden'));
  for (const [id, selected] of [['files-toggle', files], ['plugins-toggle', plugins]]) {
    $(id).classList.toggle('active', selected);
    $(id).setAttribute('aria-pressed', String(selected));
  }
}
function closeSidePanels() {
  document.body.classList.remove('plugins-mode');
  document.body.classList.add('files-hidden');
  $('file-panel').classList.remove('mobile-open');
  syncPanelButtons();
}
$('files-toggle').onclick = () => {
  const fromPlugins = document.body.classList.contains('plugins-mode');
  document.body.classList.remove('plugins-mode');
  state.panel = 'files';
  if (innerWidth <= 950) {
    document.body.classList.remove('files-hidden');
    $('file-panel').classList.toggle('mobile-open', fromPlugins || !$('file-panel').classList.contains('mobile-open'));
  } else if (fromPlugins) document.body.classList.remove('files-hidden');
  else document.body.classList.toggle('files-hidden');
  syncPanelButtons();
};
$('file-panel-close').onclick = () => { closeSidePanels(); $('files-toggle').focus(); };
$('plugin-panel-close').onclick = () => { closeSidePanels(); $('plugins-toggle').focus(); };
window.addEventListener('resize', syncPanelButtons);
$('parent-dir').onclick = action(async () => { state.dir = state.dir.includes('/') ? state.dir.slice(0, state.dir.lastIndexOf('/')) : '.'; await loadFiles(); });
$('task-form').onsubmit = action(async event => {
  event.preventDefault(); const prompt = $('prompt').value.trim(); if (!prompt) return;
  if (!state.config?.configured) { openSettings(); return; }
  if (state.previewOverLimit) { toast(t("上下文预算超限：请缩短任务或减少附件后再发送")); return; }
  $('send').disabled = true;
  const draftSession = state.session; // R07：捕获发送时对象，后续等待不得覆盖新选择
  let created = null;
  try {
    if (!draftSession) {
      created = await api('/sessions', { method: 'POST', body: JSON.stringify({ title: t("新会话") }) });
      if (!state.session) state.session = created; // 仅当用户仍停留在空白页时接管；点击已切走的会话不被空壳抢占
    }
    const target = draftSession || created;
    const strategy = state.profiles?.strategy || 'manual';
    await api(`/sessions/${target.id}/runs`, { method: 'POST', body: JSON.stringify({ prompt, mode: state.mode, attachments: state.attachments, strategy, profile: strategy === 'auto' ? '' : (state.profiles?.activeProfile || 'default'), queued: state.queueMode, workflowPhase: state.workflowPhase || '' }) });
    if (state.session?.id === target.id) { // 仅当用户仍停留在发送会话时清空草稿
      $('prompt').value = ''; state.attachments = []; renderAttachments();
    }
    if (state.session?.id === target.id) { // R07：提交完成后不得抢走用户已切换到的会话
      await selectSession(target.id);
      $('conversation').scrollTo({ top: $('conversation').scrollHeight, behavior: 'instant' });
    }
  } catch (err) {
    // 任务未启动成功：删掉刚创建的空壳会话，避免侧栏残留“新会话”空项；正展示时退回空白页
    if (created) {
      if (state.session?.id === created.id) { state.session = null; state.sessionJSON = ''; renderSession(); }
      await api(`/sessions/${created.id}`, { method: 'DELETE' }).catch(() => {});
      await loadSessions().catch(() => {});
    }
    throw err;
  } finally { updateSendEnabled(); }
});
  $('queue-toggle')?.addEventListener('click', () => { state.queueMode = !state.queueMode; $('queue-toggle').classList.toggle('active', state.queueMode); });
  $('prompt').addEventListener('keydown', event => { if (event.key === 'Enter' && !event.shiftKey && !event.isComposing) { event.preventDefault(); $('task-form').requestSubmit(); } });

/* ── R08-04 上下文预览：与真实请求共用服务端构建器，口径如实标注为估算 ── */
state.previewSeq = { value: 0 };
state.previewTimer = 0;
state.previewFingerprint = '';
state.previewOverLimit = false;
function updateSendEnabled() {
  $('send').disabled = !!state.previewOverLimit;
  if (state.previewOverLimit) {
    $('composer-hint').textContent = t("⚠ 上下文预算超限：请缩短任务或减少附件");
  } else {
    $('composer-hint').textContent = t("Enter 发送 · Shift + Enter 换行");
  }
}
function hideContextPreview() {
  state.previewOverLimit = false;
  state.previewFingerprint = '';
  $('context-preview').classList.add('hidden');
  updateSendEnabled();
}
// ctx-bar 浮动 tooltip
let _ctxTooltip = null;
function showCtxTooltip(e, label, tokens, pct, color) {
  if (!_ctxTooltip) {
    _ctxTooltip = document.createElement('div');
    _ctxTooltip.className = 'ctx-tooltip';
    document.body.append(_ctxTooltip);
  }
  _ctxTooltip.innerHTML = '<strong style="color:' + color + '">' + label + '</strong><br>' + tokens + ' tokens · ' + pct + '%';
  _ctxTooltip.style.display = 'block';
  moveCtxTooltip(e);
}
function moveCtxTooltip(e) {
  if (!_ctxTooltip) return;
  const x = Math.min(e.clientX + 12, window.innerWidth - 160);
  const y = Math.min(e.clientY + 12, window.innerHeight - 60);
  _ctxTooltip.style.left = x + 'px';
  _ctxTooltip.style.top = y + 'px';
}
function hideCtxTooltip() {
  if (_ctxTooltip) _ctxTooltip.style.display = 'none';
}

function renderContextPreview(data) {
  if (!data || !data.breakdown) return;
  state.contextPreview = data;
  state.previewFingerprint = data.fingerprint || '';
  state.previewOverLimit = !!data.overLimit;
  const bd = data.breakdown;
  const overText = data.overLimit ? t(" · ⚠ 超限 ") + Math.max(0, data.totalEstimate - data.contextWindow) : '';
  $('cp-summary').textContent = t("输入估算 ") + data.inputEstimate + t(" tokens + 输出预留 ") + data.outputReserve + ' = ' + data.totalEstimate + t(" / 窗口 ") + data.contextWindow + overText;
  const detail = $('cp-detail');
  detail.replaceChildren();
  const rows = [
    [t("系统指令"), bd.systemChars],
    [t("历史摘要"), bd.summaryChars],
    [t("历史消息 ") + (bd.historyMessages || 0) + t(" 条"), bd.historyChars],
    [t("任务输入"), bd.promptChars],
    [t("附件 ") + (bd.attachmentFiles || 0) + t(" 个"), bd.attachmentChars],
    [t("阶段指令"), bd.instructionChars],
    [t("工具定义 ") + (bd.toolCount || 0) + t(" 个"), bd.toolSchemaChars]
  ];
  rows.forEach(([label, chars]) => {
    if (chars) {
      const row = el('div', 'cp-row');
      row.append(el('span', '', label), el('span', '', chars + t(" 字符 ≈ ") + Math.floor(chars / 4) + ' tokens'));
      detail.append(row);
    }
  });
  // 堆叠条形图：按 token 占比着色，hover 高亮显示详情
  const segments = [
    { label: t("系统指令"), chars: bd.systemChars, color: "#6366f1" },
    { label: t("历史摘要"), chars: bd.summaryChars, color: "#8b5cf6" },
    { label: t("历史消息"), chars: bd.historyChars, color: "#06b6d4" },
    { label: t("任务输入"), chars: bd.promptChars, color: "#22c55e" },
    { label: t("附件"), chars: bd.attachmentChars, color: "#f59e0b" },
    { label: t("阶段指令"), chars: bd.instructionChars, color: "#ec4899" },
    { label: t("工具定义"), chars: bd.toolSchemaChars, color: "#64748b" },
  ].filter(x => x.chars > 0);
  const total = segments.reduce((sum, x) => sum + x.chars, 0) || 1;
  const bar = el('div', 'ctx-bar');
  const cells = [];
  for (const seg of segments) {
    const pct = ((seg.chars / total) * 100).toFixed(1);
    const cell = el('div', 'ctx-bar-cell');
    cell.style.width = pct + '%';
    cell.style.background = seg.color;
    const tokens = Math.floor(seg.chars / 4);
    cell.dataset.label = seg.label;
    cell.dataset.tokens = tokens;
    cell.dataset.pct = pct;
    cell.dataset.color = seg.color;
    cell.addEventListener('mouseenter', (e) => {
      cells.forEach(c => { if (c !== cell) c.classList.add('dimmed'); });
      showCtxTooltip(e, seg.label, tokens, pct, seg.color);
    });
    cell.addEventListener('mousemove', (e) => moveCtxTooltip(e));
    cell.addEventListener('mouseleave', () => {
      cells.forEach(c => c.classList.remove('dimmed'));
      hideCtxTooltip();
    });
    cells.push(cell);
    bar.append(cell);
  }
  detail.append(bar);
  // 图例：可点击切换显示/隐藏
  const legend = el('div', 'ctx-legend');
  segments.forEach(seg => {
    const tokens = Math.floor(seg.chars / 4);
    const item = el('div', 'ctx-legend-item');
    item.append(el('span', 'ctx-legend-dot', ''), el('span', '', seg.label + ' · ' + tokens + 't'));
    item.querySelector('.ctx-legend-dot').style.background = seg.color;
    item.onclick = () => {
      item.classList.toggle('hidden');
      const idx = segments.indexOf(seg);
      if (cells[idx]) cells[idx].style.display = item.classList.contains('hidden') ? 'none' : '';
    };
    legend.append(item);
  });
  detail.append(legend);
  detail.append(el('p', 'cp-note', data.estimationNote || ''));
  $('context-preview').classList.remove('hidden');
  updateSendEnabled();
}
async function refreshContextPreview() {
  const seq = ++state.previewSeq.value;
  const prompt = $('prompt').value.trim();
  if (!prompt || !state.config?.configured) { hideContextPreview(); return; }
  $('context-preview').classList.remove('hidden');
  $('cp-summary').textContent = t("上下文预算计算中…（估算）");
  try {
    const data = await api('/context-preview', { method: 'POST', body: JSON.stringify({ sessionId: state.session?.id || '', prompt, mode: state.mode, attachments: state.attachments }) });
    if (seq !== state.previewSeq.value) return; // 过期响应不得覆盖新预览（R08-04 草稿失效）
    renderContextPreview(data);
  } catch (error) {
    if (seq !== state.previewSeq.value) return;
    if (error && String(error.message).includes(t("上下文预算超限"))) {
      const m = String(error.message);
      state.previewOverLimit = true;
      $('cp-summary').textContent = '⚠ ' + m;
      $('context-preview').classList.add('over');
      $('context-preview').classList.remove('hidden');
      updateSendEnabled();
      return;
    }
    hideContextPreview();
  }
}
function scheduleContextPreview() {
  clearTimeout(state.previewTimer);
  state.previewFingerprint = '';
  state.previewTimer = setTimeout(() => action(refreshContextPreview).call(null), 300);
}
$('prompt').addEventListener('input', scheduleContextPreview);
$('cp-toggle').onclick = () => {
  const detail = $('cp-detail');
  const open = detail.classList.toggle('hidden');
  $('cp-toggle').textContent = open ? t("组成明细 ▾") : t("组成明细 ▴");
  $('cp-toggle').setAttribute('aria-expanded', String(!open));
};
// 发送按钮双态：空闲 = 发送（↑），运行中 = 停止（■，点击取消任务；插话/排队用 Enter 提交）
function setSendMode(running) {
  const btn = $('send');
  if (!btn) return;
  if (running) {
    btn.textContent = '■';
    btn.classList.add('stop-mode');
    btn.title = t("停止当前任务");
    btn.setAttribute('aria-label', t("停止当前任务"));
  } else {
    btn.textContent = '↑';
    btn.classList.remove('stop-mode');
    btn.title = t("发送");
    btn.setAttribute('aria-label', t("发送任务"));
  }
}
$('send').addEventListener('click', event => {
  if (!state.busy) return; // 空闲：走默认 submit
  event.preventDefault();
  action(async () => {
    const run = state.session?.runs.find(r => r.status === 'running');
    if (run) {
      await api(`/sessions/${state.session.id}/runs/${run.id}/cancel`, { method: 'POST', body: '{}' });
      toast(t("已请求停止"));
    }
  })();
});
function openSettings() { $('base-url').value = state.config?.baseURL || 'https://api.deepseek.com'; $('api-key').value = ''; $('api-key').placeholder = state.config?.hasKey ? t("已保存密钥；留空保留") : t("云端 API 通常需要密钥；本地模型可不填"); $('clear-key').checked = false; state.modelDraft = { models: JSON.parse(JSON.stringify(state.config?.models || [])), activeModel: state.config?.activeModel || '' }; renderModelList(); $('settings-dialog').showModal(); }
$('settings-button').onclick = openSettings;
$('settings-form').onsubmit = action(async event => { event.preventDefault(); if (!state.modelDraft.models.length) { toast(t("请至少添加一个模型")); return; } await api('/settings', { method: 'PUT', body: JSON.stringify({ baseURL: $('base-url').value.trim(), apiKey: $('api-key').value.trim(), clearKey: $('clear-key').checked, models: state.modelDraft.models, activeModel: state.modelDraft.activeModel }) }); $('api-key').value = ''; $('settings-dialog').close(); await refreshConfig(); toast(t("模型设置已保存，发送任务时会调用当前模型")); if (typeof scheduleContextPreview === 'function') scheduleContextPreview(); });
$('save-file').onclick = action(async () => { const body = { path: state.file.path, content: $('editor').value, hash: state.file.hash }; if (state.file.source) body.source = state.file.source; if (state.file.wsId) body.workspaceId = state.file.wsId; const data = await api('/file', { method: 'PUT', body: JSON.stringify(body) }); state.file.hash = data.hash; state.file.content = $('editor').value; state.file.fresh = false; $('attach-file').disabled = false; $('editor-status').textContent = t("✓ 已保存"); await loadFiles(); });
$('attach-file').onclick = () => {
  if (state.file.content !== $('editor').value) { toast(t("请先保存修改，再附加到任务")); return; }
  const att = { root: state.file.root, path: state.file.path }; if (state.file.source) { att.root = 'source'; att.source = state.file.source; } if (!state.attachments.some(a => a.root === att.root && a.path === att.path && (a.source || '') === (att.source || ''))) { if (state.attachments.length >= 8) { toast(t("最多附加 8 个文件")); return; } state.attachments.push(att); }
  renderAttachments(); $('editor-dialog').close(); $('prompt').focus();
};
$('new-file').onclick = () => { $('new-file-path').value = state.dir === '.' ? '' : state.dir + '/'; $('new-file-dialog').showModal(); };
$('new-file-form').onsubmit = action(async event => { event.preventDefault(); state.file = { path: $('new-file-path').value.trim(), root: 'workspace', hash: '', content: '', fresh: true }; $('new-file-dialog').close(); showEditor(); });
$('terminal-toggle').onclick = () => { const hidden = $('terminal-body').classList.toggle('hidden'); $('terminal-state').textContent = hidden ? t("展开 ＋") : t("收起 −"); };
$('command-form').onsubmit = action(async event => {
  event.preventDefault(); if (state.commandAbort) return; const command = $('command').value.trim(); if (!command) return;
  const abort = new AbortController(); state.commandAbort = abort; $('command-run').disabled = true; $('command-stop').classList.remove('hidden'); $('terminal-output').textContent += '\n\n❯ ' + command + '\n';
  try {
    const response = await fetch('/api/command', { method: 'POST', signal: abort.signal, headers: { Authorization: 'Bearer ' + state.token, 'Content-Type': 'application/json' }, body: JSON.stringify({ command, cwd: '.' }) });
    if (!response.ok) throw new Error((await response.json()).error);
    const reader = response.body.getReader(); const decoder = new TextDecoder(); let pending = '';
    while (true) {
      const { value, done } = await reader.read(); if (done) break; pending += decoder.decode(value, { stream: true });
      let index; while ((index = pending.indexOf('\n')) >= 0) {
        const line = pending.slice(0, index); pending = pending.slice(index + 1); if (!line) continue; const item = JSON.parse(line);
        $('terminal-output').textContent += item.type === 'output' ? item.text : t("\n[退出码 {0} · {1} ms] {2}\n", item.code, item.elapsedMS, item.error || '');
        if ($('terminal-output').textContent.length > 180000) $('terminal-output').textContent = $('terminal-output').textContent.slice(-160000);
        $('terminal-output').scrollTop = $('terminal-output').scrollHeight;
      }
    }
  } catch (error) { if (error.name === 'AbortError') $('terminal-output').textContent += t("\n[已停止命令]\n"); else throw error; }
  finally { state.commandAbort = null; $('command-run').disabled = false; $('command-stop').classList.add('hidden'); }
});
$('command-stop').onclick = () => state.commandAbort?.abort();
$('login-dialog').addEventListener('cancel', event => event.preventDefault());
$('login-form').onsubmit = async event => { event.preventDefault(); state.token = $('access-token').value.trim(); try { await initialize(); localStorage.setItem('aide-token', state.token); $('access-token').value = ''; $('login-dialog').close(); } catch (error) { $('login-error').textContent = error.message; } };
document.addEventListener('keydown', event => { if (event.key.toLowerCase() === 'n' && !event.metaKey && !event.ctrlKey && ['BODY', 'HTML'].includes(document.activeElement.tagName) && !document.querySelector('dialog[open]') && !$('settings-sheet').classList.contains('open')) action(newSession)(); });
/* ── 设置面板（FR-58~FR-60）：品牌 logo 入口；结构由 /settings-schema.json 数据驱动；
   设置值一律经 window.aideUI 的 JSON 文档管理。必须位于 initialize() 之外：
   未登录时 initialize() 会抛错返回，设置面板仍需可用。 ── */
const settingsPanel = { schema: null, rendered: false, trigger: null, refreshers: [], active: '', activeChild: '' };
async function loadSettingsSchema() {
  if (!settingsPanel.schema) {
    const response = await fetch('/settings-schema.json', { cache: 'no-store' });
    if (!response.ok) throw new Error(t("设置面板定义加载失败"));
    settingsPanel.schema = await response.json();
  }
  return settingsPanel.schema;
}
function renderSegmentedControl(control) {
  const wrap = el('div', 'settings-control');
  const head = el('div', 'control-label');
  const value = el('span', 'control-value');
  head.append(el('span', '', control.label), value);
  const track = el('div', 'segmented');
  track.setAttribute('data-control', control.id);
  if (control.palette) track.dataset.palette = control.palette;
  track.setAttribute('role', 'group');
  track.setAttribute('aria-label', control.label);
  const thumb = el('span', 'segmented-thumb');
  thumb.setAttribute('aria-hidden', 'true');
  const buttons = (control.options || []).map(option => {
    const b = el('button', '', option.label);
    b.type = 'button';
    b.dataset.value = option.value;
    b.setAttribute('aria-pressed', 'false');
    return b;
  });
  const apply = () => {
    const current = window.aideUI ? window.aideUI.get(control.id) : (control.options[0] || {}).value;
    const rowActive = !control.palette || window.aideUI?.get('palette') === control.palette;
    const active = rowActive ? (buttons.find(b => b.dataset.value === current) || buttons[0]) : null;
    buttons.forEach(b => b.setAttribute('aria-pressed', b === active ? 'true' : 'false'));
    const match = (control.options || []).find(o => o.value === current);
    value.textContent = rowActive ? (match ? match.label : (current || '')) : '';
    thumb.style.width = (active ? active.offsetWidth : 0) + 'px';
    thumb.style.transform = 'translateX(' + (active ? active.offsetLeft : 0) + 'px)';
  };
  track.addEventListener('click', event => {
    const b = event.target.closest('button[data-value]');
    if (b && window.aideUI) {
      if (control.palette) window.aideUI.setAppearance(control.palette, b.dataset.value);
      else window.aideUI.set(control.id, b.dataset.value);
    }
  });
  if (window.aideUI) { window.aideUI.subscribe(apply); window.addEventListener('resize', apply); settingsPanel.refreshers.push(apply); }
  track.append(thumb, ...buttons);
  wrap.append(head, track);
  requestAnimationFrame(apply);
  return wrap;
}
function renderAboutProject(control) {
  const card = el('div', 'about-project');
  const hero = el('div', 'about-identity');
  const mark = el('span', 'about-mark', 'a');
  mark.setAttribute('aria-hidden', 'true');
  const identity = el('div');
  identity.append(el('h4', 'about-name', 'aide'), el('p', 'about-description', t("AI+IDE，让想法成为下一步")));
  hero.append(mark, identity);
  const version = el('div', 'about-version');
  version.append(el('span', '', t("当前版本")), el('span', '', state.config?.version ? 'v' + state.config.version : t("开发版本")));
  const link = el('a', 'about-repository');
  link.href = control.repository;
  link.target = '_blank';
  link.rel = 'noopener noreferrer';
  link.setAttribute('aria-label', t("在新标签页打开 aide 的 GitHub 仓库"));
  const text = el('span');
  text.append(el('strong', '', 'GitHub'), el('span', 'about-repository-path', 'skyelan1999 / aide'));
  link.append(text, el('span', 'about-external', '↗'));
  card.append(hero, version, link);
  // 第三方开源软件许可
  const deps = el('div', 'about-deps');
  deps.append(el('h5', '', t("第三方开源软件")));
  const depList = el('div', 'about-dep-list');
  const deps_data = [
    {name: "draw.io / diagrams.net", ver: "v31.5.2", license: "Apache-2.0", url: "https://github.com/jgraph/drawio", note: t("内嵌静态 webapp，离线图表编辑")},
    {name: "Three.js", ver: "0.128.0", license: "MIT", url: "https://github.com/mrdoob/three.js", note: t("STL 3D 模型预览")},
    {name: "marked.js", ver: "bundled", license: "MIT", url: "https://github.com/markedjs/marked", note: t("Markdown 渲染")},
    {name: "mermaid.js", ver: "bundled", license: "MIT", url: "https://github.com/mermaid-js/mermaid", note: t("流程图渲染")},
  ];
  for (const d of deps_data) {
    const row = el('div', 'about-dep-row');
    const left = el('div', 'about-dep-left');
    left.append(el('strong', '', d.name), el('span', 'about-dep-ver', d.ver));
    const right = el('div', 'about-dep-right');
    const lic = el('span', 'about-dep-license', d.license);
    const a_link = el('a', 'about-dep-url', d.url);
    a_link.href = d.url; a_link.target = '_blank'; a_link.rel = 'noopener noreferrer';
    right.append(lic, a_link);
    row.append(left, right);
    if (d.note) row.append(el('p', 'about-dep-note', d.note));
    depList.append(row);
  }
  deps.append(depList);
  card.append(deps);
  return card;
}
function renderLanguageControl() {
  const wrap = el('div', 'settings-control language-control');
  wrap.append(el('span', '', t('界面语言')));
  const group = el('div', 'language-buttons');
  group.setAttribute('role', 'group');
  group.setAttribute('aria-label', t('界面语言'));
  for (const [value, name] of [['zh-CN', '中文'], ['en', 'English']]) {
    const button = el('button', '', name);
    button.type = 'button';
    button.dataset.languageButton = value;
    button.setAttribute('aria-pressed', String(window.aideI18n.language() === value));
    button.onclick = () => window.aideUI.set('language', value);
    group.append(button);
  }
  wrap.append(group, el('small', '', t('仅切换界面语言，不翻译聊天、文件或模型回答。')));
  return wrap;
}

function renderPermissionManager() {
  const wrap = el('div', 'settings-control permission-panel');
  const tools = [
    ['run_shell', t('执行 shell 命令'), t('沙箱内自动执行（60s 超时、受限环境），危险命令自动拦截')],
    ['write_file', t('写入文件'), t('生成修改提案，需你手动批准后才落盘')],
    ['list_files', t('列目录'), t('列出工作目录内容，结果返回给模型')],
    ['read_file', t('读文件'), t('读取文件内容返回给模型')],
    ['search_text', t('关键字搜索'), t('工作区文件正则关键字搜索')],
    ['semantic_search', t('语义搜索'), t('本地 TF-IDF 语义检索文件片段')],
    ['web_search', t('网页搜索'), t('DuckDuckGo 在线搜索当前信息')],
    ['spawn_subagent', t('子代理'), t('派生独立子会话处理子任务')],
    ['read_memory', t('读记忆'), t('读取持久化记忆文件')],
    ['write_memory', t('写记忆'), t('追加持久化记忆')],
    ['create_diagram', t('建图表'), t('创建 draw.io 图表文件')],
  ];
  const disabled = new Set(state.config && state.config.disabledTools ? state.config.disabledTools : []);
  for (const [tool, label, desc] of tools) {
    const row = el('div', 'perm-row');
    const head = el('div', 'perm-head');
    const code = el('code', 'perm-tool', tool);
    const toggle = el('input');
    toggle.type = 'checkbox';
    toggle.checked = !disabled.has(tool);
    toggle.title = t('取消勾选即禁用该工具');
    toggle.onchange = action(async () => {
      const set = new Set(state.config && state.config.disabledTools ? state.config.disabledTools : []);
      if (toggle.checked) set.delete(tool); else set.add(tool);
      await api('/settings', { method: 'PUT', body: JSON.stringify({ disabledTools: Array.from(set), activeModel: state.config ? state.config.activeModel : '' }) });
      await refreshConfig();
      toast(t('工具权限已更新'));
    });
    head.append(code, toggle, el('span', 'perm-badge', label));
    row.append(head, el('p', 'perm-desc', desc));
    wrap.append(row);
  }
  wrap.append(el('p', 'section-desc', t('取消勾选后模型将看不到该工具，即使调用也会被拒绝。破坏性命令仍会被自动拦截。')));
  return wrap;
}

const controlRenderers = { language: renderLanguageControl, 'about-project': renderAboutProject, segmented: renderSegmentedControl, 'profiles-manager': renderProfilesManager, 'token-stats': renderTokenStats, 'sessions-manage': renderSessionsManage, 'permission-manager': renderPermissionManager };

// 设置面板「会话与数据」：归档会话列表（恢复/删除）+ 全部导出按钮
function renderSessionsManage() {
  const wrap = el('div', 'settings-control sessions-manage');
  const head = el('div', 'sessions-manage-head');
  const exportBtn = el('button', 'primary', t("全部导出"));
  exportBtn.title = t("导出全部会话数据为 JSON 文件（含归档）");
  exportBtn.onclick = action(async () => {
    const res = await fetch('/api/export', { headers: { 'Authorization': 'Bearer ' + state.token } });
    if (!res.ok) throw new Error(t("导出失败"));
    const blob = await res.blob();
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    const cd = res.headers.get('Content-Disposition') || '';
    const m = cd.match(/filename="([^"]+)"/);
    a.download = m ? m[1] : 'aide-sessions.json';
    a.click();
    setTimeout(() => URL.revokeObjectURL(url), 5000);
    toast(t("已导出全部会话数据"));
  });
  const refresh = el('button', 'quiet', t("刷新"));
  refresh.onclick = action(renderArchivedList);
  const delAll = el('button', 'danger-outline', t("全部删除"));
  delAll.title = t("永久删除全部归档会话");
  delAll.onclick = action(async () => {
    const items = await api('/sessions?archived=1');
    const count = items.length;
    if (count === 0) { toast(t("没有已归档会话")); return; }
    if (!confirm(t("将永久删除全部 {0} 个归档会话，此操作不可恢复，是否继续？", count))) return;
    const res = await api('/sessions/archived/all', { method: 'DELETE' });
    if (res.failed > 0) { toast(t("已删除 {0} 个，{1} 个失败", res.deleted, res.failed)); }
    else { toast(t("已删除全部 {0} 个归档会话", res.deleted)); }
    await renderArchivedList();
    await loadSessions();
  });
  head.append(exportBtn, refresh, delAll);
  const list = el('div', 'archived-list');
  async function renderArchivedList() {
    const items = await api('/sessions?archived=1');
    list.replaceChildren();
    if (!items.length) { list.append(el('p', 'muted', t("没有已归档会话。"))); delAll.disabled = true; delAll.style.opacity = '0.4'; delAll.style.cursor = 'not-allowed'; return; } else { delAll.disabled = false; delAll.style.opacity = ''; delAll.style.cursor = ''; }
    for (const it of items) {
      const row = el('div', 'archived-item');
      const label = el('span', 'archived-title', it.title);
      label.title = it.title;
      const restore = el('button', 'quiet', t("恢复"));
      restore.onclick = action(async () => { await api(`/sessions/${it.id}`, { method: 'PATCH', body: JSON.stringify({ archived: false }) }); await renderArchivedList(); await loadSessions(); });
      const del = el('button', 'quiet danger-text', t("删除"));
      del.onclick = action(async () => {
        if (!confirm(t("确定删除这个会话？此操作不可撤销。"))) return;
        await api(`/sessions/${it.id}`, { method: 'DELETE' });
        await renderArchivedList();
        await loadSessions();
        if (state.session?.id === it.id) { clearTimeout(state.poll); closeStream(); state.session = null; state.sessionJSON = ''; renderSession(); }
      });
      row.append(label, restore, del);
      list.append(row);
    }
  }
  wrap.append(head, list);
  renderArchivedList();
  return wrap;
}
function renderControlsInto(host, controls, description) {
  if (description) host.append(el('p', 'section-desc', description));
  for (const control of controls || []) {
    const renderer = controlRenderers[control.type];
    if (renderer) host.append(renderer(control));
  }
}
function renderSettingsSheet() {
  const nav = $('settings-nav');
  const content = $('settings-content');
  nav.replaceChildren();
  content.replaceChildren();
  const sections = window.aideI18n ? window.aideI18n.schema(settingsPanel.schema?.sections || []) : (settingsPanel.schema?.sections || []);
  if (!sections.length) return;
  if (!sections.some(x => x.id === settingsPanel.active)) settingsPanel.active = sections[0].id;
  for (const section of sections) {
    const b = el('button', 'settings-nav-item' + (section.id === settingsPanel.active ? ' active' : ''), section.title);
    b.type = 'button';
    b.onclick = () => { settingsPanel.active = section.id; settingsPanel.activeChild = ''; renderSettingsSheet(); };
    nav.append(b);
  }
  const section = sections.find(x => x.id === settingsPanel.active);
  if (!section) return;
  content.append(el('h3', 'settings-page-title', section.title));
  if (section.children?.length) {
    const sub = el('div', 'settings-subnav');
    const children = section.children;
    if (!children.some(c => c.id === settingsPanel.activeChild)) settingsPanel.activeChild = children[0].id;
    for (const child of children) {
      const cb = el('button', 'settings-subnav-item' + (child.id === settingsPanel.activeChild ? ' active' : ''), child.title);
      cb.type = 'button';
      cb.onclick = () => { settingsPanel.activeChild = child.id; renderSettingsSheet(); };
      sub.append(cb);
    }
    content.append(sub);
    const child = children.find(c => c.id === settingsPanel.activeChild);
    renderControlsInto(content, child?.controls || [], child?.description || '');
  } else {
    renderControlsInto(content, section.controls || [], section.description);
  }
  settingsPanel.rendered = true;
}
function setSettingsOpen(open) {
  $('settings-backdrop').classList.toggle('open', open);
  $('settings-sheet').classList.toggle('open', open);
  document.querySelectorAll('[aria-controls="settings-sheet"]').forEach(b => b.setAttribute('aria-expanded', String(open)));
  if (open) $('settings-sheet-close').focus();
}
async function openSettingsSheet() {
  closeTrajectory();
  await loadSettingsSchema();
  renderSettingsSheet(); // 每次打开强制重渲染，保证数据新鲜
  setSettingsOpen(true);
  // 面板可见后重测 thumb 几何（关闭状态下 offsetWidth 为 0）
  requestAnimationFrame(() => settingsPanel.refreshers.forEach(fn => fn()));
}
function closeSettingsSheet() {
  setSettingsOpen(false);
  if (settingsPanel.trigger) settingsPanel.trigger.focus();
  settingsPanel.trigger = null;
}
function bindSettingsTrigger(button) {
  if (!button) return;
  button.onclick = action(async () => { settingsPanel.trigger = button; await openSettingsSheet(); });
}
bindSettingsTrigger($('brand-button'));
bindSettingsTrigger($('brand-mini'));
$('settings-sheet-close').onclick = closeSettingsSheet;
$('settings-backdrop').onclick = () => { if ($('trajectory-sheet').classList.contains('open')) closeTrajectory(); else if ($('workspace-sheet').classList.contains('open')) closeWorkspaceSheet(); else closeSettingsSheet(); };
document.addEventListener('keydown', event => { if (event.key === 'Escape' && !document.querySelector('dialog[open]')) { if ($('trajectory-sheet').classList.contains('open')) closeTrajectory(); else if ($('workspace-sheet').classList.contains('open')) closeWorkspaceSheet(); else if ($('settings-sheet').classList.contains('open')) closeSettingsSheet(); } });
/* ── 模型参数 Profile 与策略路由（FR-61~FR-64）：数据经 GET/PUT /api/profiles，
   持久化于工程目录 profiles.json；聊天栏策略按钮可选 auto 或手动 profile。 ── */
async function loadProfiles() { state.profiles = await api('/profiles'); refreshStrategyUI(); }
function profileName(id) { const p = state.profiles?.profiles.find(p => p.id === id); return p?.system ? t(p.name) : (p?.name || id); }
function activeModel() { return state.config?.models?.find(m => m.id === state.config.activeModel); }
function profilesPayloadFrom(source) {
  return {
    strategy: source?.strategy || 'manual',
    activeProfile: source?.activeProfile || 'default',
    profiles: (source?.profiles || []).filter(p => !p.system)
  };
}
async function saveProfilesFrom(source) {
  const saved = await api('/profiles', { method: 'PUT', body: JSON.stringify(profilesPayloadFrom(source)) });
  state.profiles = saved;
  refreshStrategyUI();
  return saved;
}
/* 设置面板里的 Profile 管理器：本地编辑副本 + 防抖保存；系统配置只读（FR-62） */
const profilesManager = { local: null, timer: null, host: null };
const paramDefs = [
  { key: 'temperature', label: '温度 temperature', min: 0, max: 2, step: 0.1, placeholder: '1' },
  { key: 'top_p', label: 'Top P', min: 0, max: 1, step: 0.05, placeholder: '1' },
  { key: 'max_tokens', label: '最大 Tokens', min: 1, max: 8192, step: 1, placeholder: '4096', integer: true },
  { key: 'frequency_penalty', label: '频率惩罚', min: -2, max: 2, step: 0.1, placeholder: '0' },
  { key: 'presence_penalty', label: '存在惩罚', min: -2, max: 2, step: 0.1, placeholder: '0' }
];
profilesManager.refresh = function () {
  this.local = JSON.parse(JSON.stringify(state.profiles || { strategy: 'manual', activeProfile: 'default', profiles: [] }));
  if (this.host) this.render();
};
profilesManager.scheduleSave = function () {
  clearTimeout(this.timer);
  this.timer = setTimeout(action(async () => { try { await saveProfilesFrom(this.local); } catch (error) { this.refresh(); } }), 700);
};
profilesManager.save = function () {
  clearTimeout(this.timer);
  return saveProfilesFrom(this.local).catch(error => { this.refresh(); throw error; });
};
profilesManager.flush = async function () {
  clearTimeout(this.timer);
  if (this.local) await saveProfilesFrom(this.local);
};
profilesManager.render = function () {
  const host = this.host;
  if (!host || !this.local) return;
  host.replaceChildren();
  for (const profile of this.local.profiles || []) host.append(this.card(profile));
};
profilesManager.card = function (profile) {
  const isSystem = !!profile.system;
  const card = el('div', 'profile-card' + (isSystem ? ' system' : ''));
  const head = el('div', 'profile-card-head');
  if (isSystem) {
    head.append(el('span', 'profile-name', profileName(profile.id)), el('span', 'profile-badge', t("🔒 系统配置 · 不可修改")));
  } else {
    const nameInput = el('input', 'profile-name-input');
    nameInput.value = profile.name || ''; nameInput.maxLength = 32; nameInput.setAttribute('aria-label', t("配置名称"));
    nameInput.addEventListener('input', () => { profile.name = nameInput.value.trim(); this.scheduleSave(); });
    head.append(nameInput, el('span', 'profile-badge', profile.id));
    const del = el('button', 'profile-delete', '－');
    del.type = 'button'; del.title = t("删除配置"); del.setAttribute('aria-label', t("删除配置 ") + profile.name);
    del.onclick = () => { if (confirm(t("删除配置「{0}」？", profile.name))) { const index = this.local.profiles.indexOf(profile); if (index >= 0) { this.local.profiles.splice(index, 1); if (this.local.activeProfile === profile.id) this.local.activeProfile = 'default'; } this.render(); this.save(); } };
    head.append(del);
  }
  const grid = el('div', 'profile-params');
  for (const def of paramDefs) {
    const label = el('label', 'param-field');
    label.append(el('span', '', t(def.label)));
    const input = el('input');
    input.type = 'number'; input.min = def.min; input.max = def.max; input.step = def.step; input.placeholder = def.placeholder;
    input.disabled = isSystem;
    const raw = profile.params[def.key];
    input.value = (raw === undefined || raw === null) ? '' : raw;
    input.addEventListener('input', () => {
      const text = input.value.trim();
      if (text === '') { delete profile.params[def.key]; }
      else { const value = def.integer ? parseInt(text, 10) : parseFloat(text); if (!Number.isNaN(value)) profile.params[def.key] = value; }
      this.scheduleSave();
    });
    label.append(input);
    grid.append(label);
  }
  const rfLabel = el('label', 'param-field');
  rfLabel.append(el('span', '', t("输出格式")));
  const select = el('select');
  for (const v of ['text', 'json_object']) { const o = el('option', '', v === 'text' ? t("文本 text") : t("JSON 对象 json_object")); o.value = v; select.append(o); }
  select.value = profile.params.response_format || 'text';
  select.disabled = isSystem;
  select.addEventListener('change', () => { profile.params.response_format = select.value; this.scheduleSave(); });
  rfLabel.append(select);
  grid.append(rfLabel);
  const stopLabel = el('label', 'param-field wide');
  stopLabel.append(el('span', '', t("停止词 stop（逗号分隔）")));
  const stopInput = el('input');
  stopInput.type = 'text'; stopInput.placeholder = t("无"); stopInput.disabled = isSystem;
  stopInput.value = (profile.params.stop || []).join(', ');
  stopInput.addEventListener('input', () => {
    const parts = stopInput.value.split(/[,，]/).map(s => s.trim()).filter(Boolean).slice(0, 16);
    if (parts.length) profile.params.stop = parts; else delete profile.params.stop;
    this.scheduleSave();
  });
  stopLabel.append(stopInput);
  grid.append(stopLabel);
  card.append(head, grid);
  return card;
};
function renderProfilesManager(control) {
  const wrap = el('div', 'settings-control profiles-manager');
  const head = el('div', 'control-label');
  head.append(el('span', '', control.label));
  const add = el('button', 'profiles-add', t("＋ 新建配置"));
  add.type = 'button';
  add.onclick = action(async () => {
    await profilesManager.load();
    const def = (state.profiles.profiles.find(p => p.id === 'default') || {}).params || {};
    profilesManager.local.profiles.push({ id: 'u-' + Math.random().toString(36).slice(2, 8), name: t("自定义配置"), params: JSON.parse(JSON.stringify(def)) });
    profilesManager.render();
    await profilesManager.save();
    toast(t("已添加配置，可修改名称与参数"));
  });
  head.append(add);
  const list = el('div', 'profile-list');
  wrap.append(head, list);
  profilesManager.host = list;
  profilesManager.load = async function () { if (!state.profiles) await loadProfiles(); };
  profilesManager.load().then(() => profilesManager.refresh()).catch(error => toast(error.message));
  return wrap;
}
/* 聊天栏策略按钮（FR-63） */
function refreshStrategyUI() {
  const p = state.profiles;
  if (!p) return;
  const modelName = state.config?.models?.find(m => m.id === state.config.activeModel)?.name || state.config?.model || '';
  const label = (p.strategy === 'auto' ? t("策略 · 自动") : t("策略 · ") + profileName(p.activeProfile)) + (modelName ? ' · ' + modelName : '');
  $('strategy-label').textContent = label;
  $('strategy-label').title = label;
  const menu = $('strategy-menu');
  menu.replaceChildren();
  // 左栏：策略；右栏：模型（各自独立滚动，互不挤压）
  const left = el('div', 'strategy-menu-col');
  left.append(el('div', 'strategy-menu-sep', t("策略")));
  left.append(strategyMenuOption('auto', '', t("自动路由"), t("按 routing-policy.json 规则匹配"), p.strategy === 'auto'));
  left.append(el('div', 'strategy-menu-sep', t("手动")));
  for (const profile of p.profiles) {
    const selected = p.strategy === 'manual' && p.activeProfile === profile.id;
    left.append(strategyMenuOption('profile', profile.id, profileName(profile.id), profile.system ? t("系统配置") : t("自定义配置"), selected));
  }
  const right = el('div', 'strategy-menu-col');
  right.append(el('div', 'strategy-menu-sep', t("模型")));
  for (const m of state.config?.models || []) {
    const selected = state.config.activeModel === m.id;
    right.append(strategyMenuOption('model', m.id, m.name, m.id + ' · ' + (m.contextWindow || 65536) / 1024 + t("K 上下文"), selected));
  }
  const manageModels = el('button', 'model-picker-manage', t("⚙ 管理模型…"));
  manageModels.type = 'button';
  manageModels.onclick = () => { closeStrategyMenu(); openSettings(); };
  right.append(manageModels);
  const reasoningCol = el('div', 'strategy-menu-col');
  reasoningCol.append(el('div', 'strategy-menu-sep', t("推理强度")));
  const curEffort = state.config?.reasoningEffort || 'auto';
  for (const [val, name, desc] of [['auto', t("自动"), t("按任务自动选择")], ['off', t("关闭"), t("不启用推理")], ['low', t("低"), t("快速响应")], ['medium', t("中"), t("均衡")], ['high', t("高"), t("深度推理")]]) {
    reasoningCol.append(strategyMenuOption('reasoning', val, name, desc, curEffort === val));
  }
  menu.append(left, right, reasoningCol);
}
function strategyMenuOption(kind, value, name, desc, selected) {
  const b = el('button', 'strategy-option' + (selected ? ' selected' : ''));
  b.type = 'button';
  b.setAttribute('role', 'menuitemradio');
  b.setAttribute('aria-checked', String(selected));
  b.append(el('span', 'strategy-option-check', selected ? '✓' : ''), el('span', '', name), el('small', '', desc));
  b.onclick = action(async () => {
    await profilesManager.flush();
    const source = profilesManager.local || state.profiles;
    if (kind === 'model') {
      await api('/settings', { method: 'PUT', body: JSON.stringify({ activeModel: value }) });
      closeStrategyMenu();
      await refreshConfig();
      const modelName = state.config.models?.find(m => m.id === value)?.name || value;
      toast(t("已切换模型：") + modelName);
      return;
    }
    if (kind === 'reasoning') {
      await api('/settings', { method: 'PUT', body: JSON.stringify({ reasoningEffort: value }) });
      closeStrategyMenu();
      await refreshConfig();
      toast(t("已切换推理强度：") + ({auto:t("自动"),off:t("关闭"),low:t("低"),medium:t("中"),high:t("高")}[value] || value));
      return;
    }
    if (kind === 'auto') source.strategy = 'auto';
    else { source.strategy = 'manual'; source.activeProfile = value; }
    await saveProfilesFrom(source);
    closeStrategyMenu();
    toast(kind === 'auto' ? t("已切换为自动路由策略") : t("已切换为手动策略 · ") + profileName(value));
  });
  return b;
}
function openStrategyMenu() {
  $('strategy-menu').classList.remove('hidden');
  $('strategy-button').setAttribute('aria-expanded', 'true');
}
function closeStrategyMenu() {
  $('strategy-menu').classList.add('hidden');
  $('strategy-button').setAttribute('aria-expanded', 'false');
}
$('strategy-button').onclick = async () => {
  if (!$('strategy-menu').classList.contains('hidden')) { closeStrategyMenu(); return; }
  try { if (!state.profiles) await loadProfiles(); refreshStrategyUI(); openStrategyMenu(); } catch (error) { toast(error.message); }
};
document.addEventListener('click', event => { if (!$('strategy-menu').classList.contains('hidden') && !event.target.closest('.strategy-picker')) closeStrategyMenu(); });
document.addEventListener('keydown', event => { if (event.key === 'Escape' && !$('strategy-menu').classList.contains('hidden')) closeStrategyMenu(); });
/* ── Token 消耗统计（FR-90）：git 提交热力图样式 ── */
function fmtStatTokens(n) { return n < 1000 ? String(n) : (n / 1000).toFixed(1) + 'K'; }
function renderTokenStats(control) {
  // R08：计价与费用是服务端事实源（/api/token-pricing + 逐调用快照），前端只展示
  let pricing = { priceIn: 2, priceOut: 8 };
  const wrap = el('div', 'settings-control token-stats');
  const head = el('div', 'token-head');
  head.append(el('span', 'token-title', t("Token 消耗")), el('span', 'control-value', ''));
  const chips = el('div', 'token-chips');
  const grid = el('div', 'token-heatmap');
  const legend = el('div', 'token-legend');
  const tip = el('div', 'token-tip');
  const detail = el('div', 'token-day-detail hidden');
  const priceRow = el('div', 'token-price-row');
  wrap.append(head, chips, priceRow, grid, legend, tip, detail);
  const pieWrap = el('div', 'model-pie-wrap');
  pieWrap.innerHTML = '<div class="model-pie-title">模型 Token 占比</div><div class="model-pie-body"><svg class="model-pie-svg" viewBox="0 0 200 200"></svg><div class="model-pie-legend"></div></div><div class="model-pie-empty hidden">暂无模型调用数据</div>';
  wrap.append(pieWrap);
  const failBox = el('p', 'task-error', '');
  wrap.append(failBox);
  const loadStats = async () => {
    failBox.textContent = '';
    const data = await api('/token-stats');
    const days = data.days || {};
    const totalsObj = data.totals || {};
    const todayStats = data.today || {};
    const unpriced = data.unpricedTotals || {};
    pricing = data.pricing || pricing;
    const cost = data.cost ?? 0;
    // 服务端费率加载完成后同步输入框显示（未聚焦时），避免停留在初始默认值
    const inEl = wrap.querySelector('input[data-price="priceIn"]');
    const outEl = wrap.querySelector('input[data-price="priceOut"]');
    if (inEl && document.activeElement !== inEl) inEl.value = pricing.priceIn;
    if (outEl && document.activeElement !== outEl) outEl.value = pricing.priceOut;
    const callRecords = data.callRecords || [];
    const dayCost = {};
    callRecords.forEach(c => { const k = (c.time || '').slice(0, 10); dayCost[k] = (dayCost[k] || 0) + (c.cost || 0); });
    // R08：费用仅由服务端逐调用记录汇总；未计价历史（旧版汇总）单独提示，不并入费用
    const estimatedCost = data.estimatedCost ?? 0;
    const metrics = head.querySelector('.control-value');
    metrics.replaceChildren();
    const metric = (label, value, caption) => {
      const card = el('div', 'usage-metric');
      card.append(el('span', 'usage-label', label), el('strong', 'usage-value', value), el('small', 'usage-caption', caption));
      return card;
    };
    metrics.append(metric(t("累计用量"), fmtStatTokens(totalsObj.total || 0), 'tokens · ' + (totalsObj.calls || 0) + t(" 次调用")),
      metric(t("已计价费用"), '¥' + cost.toFixed(2), t("按调用时刻的费率快照")));
    if (estimatedCost > 0) metrics.append(metric(t("刊例价估算"), '¥' + estimatedCost.toFixed(2), t("与已计价费用分开统计")));
    chips.replaceChildren();
    chips.append(
      el('span', 'token-chip', t("今日 ") + fmtStatTokens(todayStats.total || 0) + ' tokens' + (todayStats.priced !== false ? ' · ¥' + (dayCost[Object.keys(days).sort().pop()] || 0).toFixed(2) : t(" · 未计价"))),
      el('span', 'token-chip', t("调用 ") + (totalsObj.calls || 0) + t(" 次"))
    );
    Object.entries(data.modelCost || {}).forEach(([model, mc]) => {
      const chip = el('span', 'token-chip', model + ' ¥' + mc.toFixed(2));
      chip.title = t("该模型逐调用计价快照合计");
      chips.append(chip);
    });
    if (unpriced.total) {
      const chip = el('span', 'token-chip', t("未计价历史 ") + fmtStatTokens(unpriced.total) + ' tokens · ' + (unpriced.calls || 0) + t(" 次"));
      chip.title = t("旧版统计没有逐调用与计价证据，费用未知；未按当前费率冒充已发生费用");
      chips.append(chip);
    }
    // 模型 Token 占比饼图
    const pm = data.perModel || {};
    const pmEntries = Object.entries(pm).sort((x, y) => (y[1].total || 0) - (x[1].total || 0));
    const pmTotal = pmEntries.reduce((s, e) => s + (e[1].total || 0), 0);
    const pieSvg = pieWrap.querySelector('.model-pie-svg');
    const pieLegend = pieWrap.querySelector('.model-pie-legend');
    const pieEmpty = pieWrap.querySelector('.model-pie-empty');
    pieSvg.innerHTML = '';
    pieLegend.innerHTML = '';
    if (pmEntries.length === 0 || pmTotal === 0) {
      pieSvg.classList.add('hidden');
      pieLegend.classList.add('hidden');
      pieEmpty.classList.remove('hidden');
    } else {
      pieSvg.classList.remove('hidden');
      pieLegend.classList.remove('hidden');
      pieEmpty.classList.add('hidden');
      const colors = ['#3b82f6','#f59e0b','#8b5cf6','#10b981','#ef4444','#06b6d4','#ec4899','#84cc16','#f97316','#6366f1'];
      const cx = 100, cy = 100, r = 70;
      let angle = -Math.PI / 2;
      pmEntries.forEach(([model, stats], i) => {
        const frac = (stats.total || 0) / pmTotal;
        const sweep = frac * Math.PI * 2;
        const x1 = cx + r * Math.cos(angle), y1 = cy + r * Math.sin(angle);
        const x2 = cx + r * Math.cos(angle + sweep), y2 = cy + r * Math.sin(angle + sweep);
        const large = sweep > Math.PI ? 1 : 0;
        const color = colors[i % colors.length];
        if (frac >= 0.999) {
          // 单模型 100%：画整圆
          const c = document.createElementNS('http://www.w3.org/2000/svg','circle');
          c.setAttribute('cx', cx); c.setAttribute('cy', cy); c.setAttribute('r', r);
          c.setAttribute('fill', color); c.setAttribute('class', 'pie-slice');
          c.setAttribute('data-model', model);
          pieSvg.appendChild(c);
        } else {
          const path = document.createElementNS('http://www.w3.org/2000/svg','path');
          path.setAttribute('d', `M${cx},${cy} L${x1},${y1} A${r},${r} 0 ${large} 1 ${x2},${y2} Z`);
          path.setAttribute('fill', color); path.setAttribute('class', 'pie-slice');
          path.setAttribute('data-model', model);
          pieSvg.appendChild(path);
        }
        angle += sweep;
        // 图例
        const item = el('div', 'pie-legend-item');
        item.innerHTML = `<span class="pie-dot" style="background:${color}"></span><span class="pie-model">${model}</span><span class="pie-tokens">${fmtStatTokens(stats.total || 0)}</span><span class="pie-pct">${(frac*100).toFixed(1)}%</span>`;
        item.title = `${model}: ${stats.total || 0} tokens · ${stats.calls || 0} 次调用 · ${(frac*100).toFixed(1)}%`;
        pieLegend.appendChild(item);
      });
      // hover 高亮
      pieSvg.querySelectorAll('.pie-slice').forEach(slice => {
        slice.addEventListener('mouseenter', () => { slice.setAttribute('opacity', '0.8'); });
        slice.addEventListener('mouseleave', () => { slice.setAttribute('opacity', '1'); });
      });
    }
    action(async () => {
      try {
        const bal = await api('/balance');
        const infos = (bal && bal.balance_infos) || [];
        if (!infos.length) {
          const chip = el('span', 'token-chip', t("余额不可查"));
          chip.title = t("接口未返回余额信息（部分账户/服务不支持）");
          chips.append(chip);
          return;
        }
        const parts = infos.map(i => (i.total_balance ?? '?') + ' ' + (i.currency || '')).join(' · ');
        const chip = el('span', 'token-chip balance', t("余额 ") + parts);
        chip.title = t("来自 API 的账户余额");
        chips.append(chip);
      } catch (error) {
        const chip = el('span', 'token-chip', t("余额不可查"));
        chip.title = t("查询失败: ") + error.message;
        chips.append(chip);
      }
    })();
    grid.replaceChildren();
    const today = new Date();
    const start = new Date(today);
    start.setDate(start.getDate() - 111);
    start.setDate(start.getDate() - ((start.getDay() + 6) % 7));
    const cols = [];
    let cursor = new Date(start);
    while (cursor <= today) {
      const col = [];
      for (let dow = 0; dow < 7; dow++) {
        const d = new Date(cursor);
        d.setDate(d.getDate() + dow);
        col.push(d);
      }
      cols.push(col);
      cursor.setDate(cursor.getDate() + 7);
    }
    const maxVal = Math.max(1, ...Object.values(days).map(d => d.total || 0));
    const level = v => v <= 0 ? 0 : v <= maxVal * 0.25 ? 1 : v <= maxVal * 0.5 ? 2 : v <= maxVal * 0.75 ? 3 : 4;
    const showTip = (cell, date, day, weekTotal) => {
      tip.replaceChildren();
      const pricedDay = day.priced !== false;
      const fee = pricedDay ? t("费用 ¥") + (dayCost[date] || 0).toFixed(4) : t("费用未知（旧数据未计价）");
      tip.append(
        el('strong', '', date + ' · ' + fmtStatTokens(day.total || 0) + ' tokens'),
        el('br'),
        el('span', '', t("输入 ") + fmtStatTokens(day.prompt || 0) + t(" · 输出 ") + fmtStatTokens(day.completion || 0)),
        el('br'),
        el('span', '', t("调用 ") + (day.calls || 0) + t(" 次 · ") + fee + (day.estimated ? t("（用量为估算）") : '')),
        el('br'),
        el('span', '', t("所在周合计 ") + fmtStatTokens(weekTotal) + ' tokens')
      );
      tip.classList.add('show');
      const rect = cell.getBoundingClientRect();
      // 浮窗的定位上下文是 .token-stats（position:relative），坐标必须相对它而非 settings-sheet
      const parentRect = tip.offsetParent.getBoundingClientRect();
      const tipW = tip.offsetWidth;
      const tipH = tip.offsetHeight;
      const centerX = rect.left - parentRect.left + rect.width / 2;
      const left = Math.min(Math.max(centerX - tipW / 2, 0), Math.max(parentRect.width - tipW, 0));
      let top = rect.top - parentRect.top - tipH - 8;
      if (top < 0) top = rect.bottom - parentRect.top + 8; // 上方放不下时翻到格子下方
      tip.style.left = left + 'px';
      tip.style.top = top + 'px';
    };
    for (const col of cols) {
      const colEl = el('div', 'token-week');
      const weekTotal = col.reduce((sum, d) => sum + ((days[d.toISOString().slice(0, 10)] || {}).total || 0), 0);
      for (const d of col) {
        const key = d.toISOString().slice(0, 10);
        const day = days[key] || {};
        const cell = el('button', 'token-cell tk-' + level(day.total || 0));
        cell.type = 'button';
        cell.setAttribute('aria-label', key + ' · ' + fmtStatTokens(day.total || 0) + t(" tokens，查看当日明细"));
        cell.addEventListener('focus', () => showTip(cell, key, day, weekTotal));
        cell.addEventListener('blur', () => tip.classList.remove('show'));
        if (d > today) { cell.classList.add('future'); cell.disabled = true; }
        cell.tabIndex = key === today.toISOString().slice(0, 10) ? 0 : -1;
        cell.addEventListener('keydown', event => {
          const delta = { ArrowUp: -1, ArrowDown: 1, ArrowLeft: -7, ArrowRight: 7 }[event.key];
          if (delta === undefined) return;
          event.preventDefault();
          const cells = [...grid.querySelectorAll('button:not(:disabled)')];
          const next = cells[Math.max(0, Math.min(cells.length - 1, cells.indexOf(cell) + delta))];
          cells.forEach(item => { item.tabIndex = item === next ? 0 : -1; });
          next.focus();
        });
        cell.addEventListener('mouseenter', () => showTip(cell, key, day, weekTotal));
        cell.addEventListener('mouseleave', () => tip.classList.remove('show'));
        cell.addEventListener('click', () => {
          grid.querySelectorAll('button').forEach(item => { item.tabIndex = item === cell ? 0 : -1; });
          detail.classList.remove('hidden');
          detail.replaceChildren();
          const pricedDay = day.priced !== false;
          const fee = pricedDay ? t("费用 ¥") + (dayCost[key] || 0).toFixed(4) + t("（按调用时刻计价快照）") : t("费用未知：旧数据没有逐调用与计价证据，未按当前费率冒充");
          detail.append(
            el('strong', '', key),
            el('span', '', t("输入 ") + fmtStatTokens(day.prompt || 0) + t(" tokens · 输出 ") + fmtStatTokens(day.completion || 0) + ' tokens'),
            el('span', '', t("调用 ") + (day.calls || 0) + t(" 次 · 合计 ") + fmtStatTokens(day.total || 0) + ' tokens' + (day.estimated ? t("（用量为估算）") : '')),
            el('span', '', fee)
          );
          tip.classList.remove('show');
          detail.scrollIntoView({ block: 'nearest' });
        });
        colEl.append(cell);
      }
      grid.append(colEl);
    }
    legend.replaceChildren();
    legend.append(el('span', '', t("少")));
    for (let i = 1; i <= 4; i++) legend.append(el('span', 'token-cell tk-' + i));
    legend.append(el('span', '', t("多")), el('small', '', t("计价可配置 · 悬停查看明细")));
  };
  action(loadStats).call(null);
  const mkPrice = (key, label) => {
    const lab = el('label', '', label);
    const input = el('input', '');
    input.type = 'number';
    input.min = 0;
    input.step = 0.1;
    input.value = key === 'priceIn' ? pricing.priceIn : pricing.priceOut;
    input.setAttribute('aria-label', label);
    input.dataset.price = key;
    input.addEventListener('change', () => {
      // R08：费率是服务端事实源；留空/非法必须显式拒绝（0 是合法免费，不等于留空）
      if (input.value.trim() === '') {
        toast(t("费率不能留空：0 表示免费，请输入明确的数字"));
        input.value = key === 'priceIn' ? pricing.priceIn : pricing.priceOut;
        return;
      }
      const v = parseFloat(input.value);
      if (Number.isNaN(v) || v < 0) {
        toast(t("费率必须是 ≥ 0 的数字"));
        input.value = key === 'priceIn' ? pricing.priceIn : pricing.priceOut;
        return;
      }
      // 以两个输入框的当前值为准（避免第二次修改用过期的模块缓存覆盖第一次的值）
      const readOther = key => { const raw = wrap.querySelector('input[data-price="' + key + '"]')?.value; const n = parseFloat(raw); return Number.isFinite(n) && n >= 0 ? n : pricing[key]; };
      const next = { priceIn: readOther('priceIn'), priceOut: readOther('priceOut') };
      next[key] = v;
      action(async () => {
        try {
          pricing = await api('/token-pricing', { method: 'PUT', body: JSON.stringify(next) });
          action(loadStats).call(null);
        } catch (error) {
          toast(t("费率保存失败: ") + error.message);
          action(loadStats).call(null);
        }
      })();
    });
    lab.append(input);
    return lab;
  };
  priceRow.append(mkPrice('priceIn', t("输入 ¥/百万")), mkPrice('priceOut', t("输出 ¥/百万")), el('small', '', t("0 = 免费；留空无效。费率由服务端保存，历史费用按调用时刻快照不变")));
  return wrap;
}
/* ── 模型列表管理（FR-67 / FR-68）：设置弹窗内增删、标记当前、自动获取候选 ── */
function renderModelList() {
  const host = $('model-list');
  host.replaceChildren();
  for (const m of state.modelDraft?.models || []) {
    const isActive = m.id === state.modelDraft.activeModel;
    const row = el('div', 'model-row' + (isActive ? ' active-model' : ''));
    const radio = el('button', 'model-active' + (isActive ? ' active' : ''));
    radio.type = 'button';
    radio.title = t("设为当前模型");
    radio.setAttribute('aria-pressed', String(isActive));
    radio.textContent = isActive ? '●' : '○';
    radio.onclick = () => { state.modelDraft.activeModel = m.id; renderModelList(); };
    const nameInput = el('input', 'model-name-input');
    nameInput.value = m.name || m.id;
    nameInput.maxLength = 32;
    nameInput.setAttribute('aria-label', t("模型名称"));
    nameInput.addEventListener('input', () => { m.name = nameInput.value.trim() || m.id; });
    const idText = el('span', 'model-id-text', m.id);
    const windowLabel = el('span', 'model-window-label', t("上下文窗口"));
    const presets = [
      {k: 32768, label: '32K'},
      {k: 65536, label: '64K'},
      {k: 128000, label: '128K'},
      {k: 200000, label: '200K'},
      {k: 256000, label: '256K'},
      {k: 1000000, label: '1M'},
    ];
    const presetRow = el('span', 'win-preset-row');
    presets.forEach(p => {
      const btn = el('button', 'win-preset', p.label);
      btn.type = 'button';
      btn.dataset.k = p.k;
      btn.title = p.label + t(" 上下文");
      if (m.contextWindow === p.k) btn.classList.add('active');
      btn.onclick = () => {
        m.contextWindow = p.k;
        windowInput.value = p.k;
        presetRow.querySelectorAll('.win-preset').forEach(b => b.classList.remove('active'));
        btn.classList.add('active');
      };
      presetRow.append(btn);
    });
    const windowInput = el('input', 'model-window-input');
    windowInput.type = 'number';
    windowInput.min = 1024;
    windowInput.max = 2097152;
    windowInput.step = 1024;
    windowInput.value = m.contextWindow || 65536;
    windowInput.setAttribute('aria-label', t("上下文窗口"));
    windowInput.addEventListener('input', () => {
      const v = parseInt(windowInput.value, 10);
      if (!Number.isNaN(v)) {
        m.contextWindow = v;
        // 高亮匹配的预设
        presetRow.querySelectorAll('.win-preset').forEach(b => {
          b.classList.toggle('active', parseInt(b.dataset.k, 10) === v);
        });
      }
    });
    const del = el('button', 'model-delete', '－');
    del.type = 'button';
    del.title = t("删除模型");
    del.onclick = () => {
      const index = state.modelDraft.models.indexOf(m);
      if (index >= 0) state.modelDraft.models.splice(index, 1);
      if (state.modelDraft.activeModel === m.id) state.modelDraft.activeModel = state.modelDraft.models[0]?.id || '';
      renderModelList();
    };
    const rowHead = el('div', 'model-row-head');
    rowHead.append(radio, nameInput, idText, del);
    const rowWin = el('div', 'model-row-window');
    rowWin.append(windowLabel, presetRow, windowInput);
    row.append(rowHead, rowWin);
    host.append(row);
  }
  if (!(state.modelDraft?.models || []).length) host.append(el('p', 'muted', t("尚未添加模型。可输入模型 ID 添加，或用「自动获取」从 API 拉取候选。")));
}
$('add-model').onclick = () => {
  const id = $('new-model-id').value.trim();
  if (!id) { toast(t("请输入模型 ID")); return; }
  if (state.modelDraft.models.some(m => m.id === id)) { toast(t("该模型已存在")); return; }
  state.modelDraft.models.push({ id, name: id, contextWindow: 65536 });
  if (!state.modelDraft.activeModel) state.modelDraft.activeModel = id;
  $('new-model-id').value = '';
  renderModelList();
};
$('fetch-models').onclick = action(async () => {
  const button = $('fetch-models');
  button.disabled = true;
  button.textContent = t("⟳ 获取中…");
  try {
    const data = await api('/models', { method: 'POST', body: JSON.stringify({ baseURL: $('base-url').value.trim(), apiKey: $('api-key').value.trim(), clearKey: $('clear-key').checked }) });
    const list = $('model-datalist');
    list.replaceChildren();
    (data.models || []).forEach(id => list.append(new Option(id, id)));
    toast(t("已获取 ") + (data.models || []).length + t(" 个可用模型，在输入框中选择即可"));
  } finally {
    button.disabled = false;
    button.textContent = t("⟳ 自动获取");
  }
});
/* ── 上下文统计（FR-70，参考 DSH token-meter：4 字符/词 + 每消息 4 开销） ── */
function fmtTokens(n) { return n < 1000 ? String(n) : (n / 1024).toFixed(1) + 'K'; }
function estimateContext() {
  const model = activeModel();
  const limit = model?.contextWindow || 65536;
  let used = 0;
  let included = 0;
  const messages = state.session?.messages || [];
  let budget = 0;
  for (let i = messages.length - 1; i >= 0; i--) {
    const size = (messages[i].content || '').length;
    if (budget > 0 && budget + size >= 60000) break; // 与后端 60,000 字符回放预算一致（近似）
    budget += size;
    used += Math.ceil(size / 4) + 4;
    included++;
  }
  const pct = Math.min(100, Math.round((used / limit) * 100));
  $('context-stat').textContent = fmtTokens(used) + ' / ' + fmtTokens(limit);
  $('context-fill').style.width = pct + '%';
  $('context-fill').classList.toggle('warn', pct > 90);
  $('context-card').title = (included ? t("最近 ") + included + t(" 条消息") : t("当前会话暂无内容")) + t(" · 4 字符/词估算 · tokens 已用/窗口");
}
/* ── 插件系统（FR-72~75，协议 docs/plugin-protocol.md）：右侧面板 + 上传/搜索/启停/删除/surface ── */
async function loadPluginsPanel() {
  const data = await api('/plugins');
  state.plugins = data.plugins || [];
  renderPluginList();
  const surface = await api('/plugin-surface');
  renderPluginSurface(surface.plugins || []);
}
function renderPluginList() {
  const query = $('plugin-search').value.trim().toLowerCase();
  const host = $('plugin-list');
  host.replaceChildren();
  const matched = state.plugins.filter(p => !query || (p.name + ' ' + (p.description || '') + ' ' + p.id).toLowerCase().includes(query));
  $('plugin-count').textContent = matched.length + ' / ' + state.plugins.length;
  if (!matched.length) { host.append(el('p', 'muted', query ? t("没有匹配的插件") : t("还没有插件。点击「＋ 上传」添加（协议 v1，DSH 形态）。"))); return; }
  for (const p of matched) {
    const card = el('div', 'plugin-card' + (p.enabled ? '' : ' disabled'));
    const head = el('div', 'plugin-card-head');
    head.append(el('span', 'plugin-name', p.name), el('span', 'plugin-badge', p.id + (p.version ? ' · v' + p.version : '')));
    const del = el('button', 'plugin-delete', '－');
    del.type = 'button'; del.title = t("删除插件");
    del.onclick = () => { if (confirm(t("删除插件「{0}」？", p.name))) action(async () => { await api('/plugins/' + encodeURIComponent(p.id), { method: 'DELETE' }); await loadPluginsPanel(); toast(t("插件已删除")); })(); };
    head.append(del);
    card.append(head);
    if (p.description) card.append(el('p', 'plugin-desc', p.description));
    if (p.error) card.append(el('p', 'task-error', '⚠ ' + p.error));
    const foot = el('div', 'plugin-card-foot');
    const toggle = el('button', 'plugin-toggle' + (p.enabled ? ' on' : ''), p.enabled ? t("✓ 使用中") : t("停用"));
    toggle.type = 'button';
    toggle.onclick = action(async () => {
      await api('/plugins/' + encodeURIComponent(p.id), { method: 'PUT', body: JSON.stringify({ enabled: !p.enabled }) });
      await loadPluginsPanel();
      toast(p.enabled ? t("已停用插件：") + p.name : t("已启用插件：") + p.name);
    });
    foot.append(el('small', '', p.enabled ? t("启用") : t("停用")), toggle);
    card.append(foot);
    host.append(card);
  }
}
function renderPluginSurface(entries) {
  const host = $('plugin-surface');
  host.replaceChildren();
  const active = entries.filter(e => !e.error);
  if (!entries.length) { host.append(el('p', 'muted', t("暂无启用的插件。"))); return; }
  for (const e of entries) {
    const box = el('div', 'surface-item');
    box.append(el('strong', '', e.name));
    if (e.error) { box.append(el('p', 'task-error', '⚠ ' + e.error)); host.append(box); continue; }
    const chips = el('div', 'surface-chips');
    for (const t of e.tools || []) chips.append(el('span', 'surface-chip tool', '⚒ ' + t.name));
    for (const sl of e.slots || []) chips.append(el('span', 'surface-chip slot', '▦ ' + sl.name));
    for (const sv of e.provided || []) chips.append(el('span', 'surface-chip service', '◈ ' + sv));
    box.append(chips);
    host.append(box);
  }
  void active;
}
$('plugins-toggle').onclick = action(async () => {
  if (document.body.classList.contains('plugins-mode')) { closeSidePanels(); return; }
  document.body.classList.remove('files-hidden');
  document.body.classList.add('plugins-mode');
  state.panel = 'plugins';
  $('file-panel').classList.remove('mobile-open');
  syncPanelButtons();
  await loadPluginsPanel();
});
$('refresh-plugins').onclick = action(loadPluginsPanel);
$('plugin-search').addEventListener('input', renderPluginList);
$('plugin-upload').onclick = () => { $('plugin-upload-form').reset(); $('plugin-file-name').textContent = ''; $('plugin-upload-dialog').showModal(); };
$('plugin-file-pick').addEventListener('change', () => { $('plugin-file-name').textContent = $('plugin-file-pick').files[0] ? t("已选择：") + $('plugin-file-pick').files[0].name : ''; });
$('plugin-upload-form').onsubmit = action(async event => {
  event.preventDefault();
  const file = $('plugin-file-pick').files[0];
  if (!file) { toast(t("请选择插件文件")); return; }
  if (file.size > 256 * 1024) { toast(t("插件文件超过 256 KiB 限制")); return; }
  const code = await new Promise((resolve, reject) => { const reader = new FileReader(); reader.onload = () => resolve(reader.result); reader.onerror = () => reject(new Error(t("读取文件失败"))); reader.readAsText(file); });
  await api('/plugins', { method: 'POST', body: JSON.stringify({ name: $('plugin-name').value.trim() || file.name.replace(/\.js$/, ''), description: $('plugin-desc').value.trim(), code }) });
  $('plugin-upload-dialog').close();
  await loadPluginsPanel();
  toast(t("插件已上传并启用"));
});

/* ── 工作空间配置（FR-76~80）：点侧栏工作空间卡片弹出；本地/SSH·SFTP、文档、缓存、最近路径 ── */
const wsState = { config: null, browse: { field: '', root: 'workspace', dir: '.' } };
async function loadWorkspaceConfig() { wsState.config = await api('/workspace-config'); renderWorkspaceSummary(); }
function renderWorkspaceSummary() {
  const w = wsState.config?.workspace || {};
  $('workspace-summary').textContent = w.mode === 'ssh' ? (w.host || t("远程")) + ' · SSH/SFTP' : (state.config?.workspaceDisplay || '/workspace') + t(" · 本地");
  $('command-mode').textContent = w.mode === 'ssh' ? 'SSH · ' + (w.host || t("未配置主机")) : t("本地");
}
function setWsMode(mode) {
  document.querySelectorAll('.ws-seg:not(.ws-auth) [data-mode]').forEach(b => { const on = b.dataset.mode === mode; b.classList.toggle('active', on); b.setAttribute('aria-pressed', String(on)); });
  $('ws-local-fields').classList.toggle('hidden', mode !== 'local');
  $('ws-ssh-fields').classList.toggle('hidden', mode !== 'ssh');
}
function setWsAuth(auth) {
  document.querySelectorAll('.ws-auth [data-auth]').forEach(b => { const on = b.dataset.auth === auth; b.classList.toggle('active', on); b.setAttribute('aria-pressed', String(on)); });
  $('ws-password-field').classList.toggle('hidden', auth !== 'password');
  $('ws-key-field').classList.toggle('hidden', auth !== 'key');
}
function renderWsRecent(id, list) {
  const host = $(id);
  host.replaceChildren();
  if (!list.length) { host.append(el('p', 'muted', t("暂无最近路径"))); return; }
  list.forEach(value => {
    const chip = el('button', 'ws-recent-chip', value);
    chip.type = 'button';
    chip.title = t("点击挂载：") + value;
    chip.onclick = action(async () => {
      const field = id === 'ws-recent' ? 'ws-path' : id === 'docs-recent' ? 'docs-path' : 'cache-path';
      $(field).value = value;
      await saveWorkspaceConfig();
      toast(t("已挂载路径：") + value);
    });
    host.append(chip);
  });
}
function fillWorkspaceSheet() {
  const c = wsState.config;
  const w = c.workspace || {};
  setWsMode(w.mode || 'local');
  $('ws-path').value = w.mode === 'local' ? (w.path || '') : '';
  $('ws-host').value = w.host || '';
  $('ws-port').value = w.port || 22;
  $('ws-user').value = w.username || '';
  $('ws-remote-path').value = w.mode === 'ssh' ? (w.path || '') : '';
  setWsAuth(w.auth || 'password');
  $('ws-password').value = ''; $('ws-key').value = '';
  $('ws-password').placeholder = c.hasPassword ? t("已保存密码；留空保留") : t("设置远程密码");
  $('ws-key').placeholder = c.hasKey ? t("已保存私钥；留空保留") : t("粘贴私钥内容");
  $('docs-path').value = c.docs?.path || '';
  $('cache-path').value = c.cache?.path || '';
  $('ws-clear-secrets').checked = false;
  renderWsRecent('ws-recent', c.recent?.workspace || []);
  renderWsRecent('docs-recent', c.recent?.docs || []);
  renderWsRecent('cache-recent', c.recent?.cache || []);
}
function collectWsConfig() {
  const mode = document.querySelector('.ws-seg:not(.ws-auth) [data-mode].active')?.dataset.mode || 'local';
  const auth = document.querySelector('.ws-auth [data-auth].active')?.dataset.auth || 'password';
  return {
    workspace: { mode, path: mode === 'local' ? $('ws-path').value.trim() : $('ws-remote-path').value.trim(), host: $('ws-host').value.trim(), port: parseInt($('ws-port').value, 10) || 22, username: $('ws-user').value.trim(), auth },
    docs: { path: $('docs-path').value.trim() },
    cache: { path: $('cache-path').value.trim() },
    password: $('ws-password').value,
    key: $('ws-key').value,
    clearPassword: $('ws-clear-secrets').checked,
    clearKey: $('ws-clear-secrets').checked
  };
}
async function saveWorkspaceConfig() {
  wsState.config = await api('/workspace-config', { method: 'PUT', body: JSON.stringify(collectWsConfig()) });
  renderWorkspaceSummary();
  fillWorkspaceSheet();
  state.dir = '.';
  await refreshConfig();
  await loadSourcesList();
  await loadFiles();
}
function openWorkspaceSheet() {
  closeTrajectory();
  closeSettingsSheet();
  fillWorkspaceSheet();
  $('workspace-sheet').classList.add('open');
  $('settings-backdrop').classList.add('open');
}
function closeWorkspaceSheet() {
  $('workspace-sheet').classList.remove('open');
  $('settings-backdrop').classList.remove('open');
}
$('workspace-config-button').onclick = () => { action(async () => { await loadWorkspaceConfig(); openWorkspaceSheet(); })(); };
$('workspace-sheet-close').onclick = closeWorkspaceSheet;
$('ws-save').onclick = action(async () => { await saveWorkspaceConfig(); closeWorkspaceSheet(); toast(t("工作空间配置已保存")); });
document.querySelectorAll('.ws-seg:not(.ws-auth) [data-mode]').forEach(b => b.onclick = () => setWsMode(b.dataset.mode));
document.querySelectorAll('.ws-auth [data-auth]').forEach(b => b.onclick = () => setWsAuth(b.dataset.auth));
/* 目录选择器：按宿主机真实目录浏览（root=local），显示与回填均为本机路径 */
function hostPathOf(dir) {
  const base = state.config?.hostLocal || '/local';
  if (dir === '.' || dir === '') return base;
  return base.replace(/\/$/, '') + '/' + dir;
}
/* 从输入值推导浏览起点：宿主机路径 → 本地挂载相对路径；空值/其他前缀回退根 */
function browseDirFromValue(value) {
  value = (value || '').trim();
  const base = (state.config?.hostLocal || '/local').replace(/\/$/, '');
  if (value === '~') value = base;
  else if (value.startsWith('~/')) value = base + value.slice(1);
  for (const prefix of [base, '/local']) {
    if (value === prefix) return '.';
    if (value.startsWith(prefix + '/')) {
      const parts = value.slice(prefix.length + 1).split('/');
      const clean = [];
      for (const part of parts) {
        if (!part || part === '.') continue;
        if (part === '..') { if (!clean.length) return '.'; clean.pop(); }
        else clean.push(part);
      }
      return clean.join('/') || '.';
    }
  }
  if (value && value !== '/workspace' && value !== '/context') throw new Error(t('该路径不在 Docker 挂载范围内，请先配置 AIDE_LOCAL_ROOT 并重新创建容器。'));
  return '.';
}

/* 当前路径不可用时逐级向上找到可用目录 */
async function resolveExistingDir(dir) {
  for (;;) {
    try {
      await api('/files?root=local&path=' + encodeURIComponent(dir));
      return dir;
    } catch (error) {
      if (dir === '.' || !dir.includes('/')) return '.';
      dir = dir.slice(0, dir.lastIndexOf('/'));
    }
  }
}
function openBrowse(field) {
  action(async () => {
    const start = browseDirFromValue($(field).value);
    const dir = await resolveExistingDir(start);
    wsState.browse = { field, dir };
    $('ws-browse-dialog').showModal();
    await loadBrowseDir();
  })();
}
async function loadBrowseDir() {
  const b = wsState.browse;
  const files = await api('/files?root=local&path=' + encodeURIComponent(b.dir));
  $('ws-browse-path').textContent = t('可访问范围：{0}', hostPathOf('.'));
  $('ws-browse-address').value = hostPathOf(b.dir);
  $('ws-browse-parent').disabled = b.dir === '.';
  $('ws-browse-status').textContent = '';
  const list = $('ws-browse-list');
  list.replaceChildren();
  const dirs = files.filter(f => f.dir);
  if (!dirs.length) list.append(el('p', 'muted', t("没有子目录")));
  dirs.forEach(d => { const row = el('button', 'ws-browse-item', '▱ ' + d.name); row.onclick = () => { b.dir = d.path; action(loadBrowseDir)(); }; list.append(row); });
}
async function browseAddress() {
  const b = wsState.browse, previous = b.dir;
  try {
    b.dir = browseDirFromValue($('ws-browse-address').value);
    await loadBrowseDir();
  } catch (error) {
    b.dir = previous;
    $('ws-browse-status').textContent = error.message;
  }
}
$('ws-browse-go').onclick = browseAddress;
$('ws-browse-address').onkeydown = event => { if (event.key === 'Enter') { event.preventDefault(); browseAddress(); } };
$('ws-browse').onclick = () => openBrowse('ws-path');
$('docs-browse').onclick = () => openBrowse('docs-path');
$('cache-browse').onclick = () => openBrowse('cache-path');
$('ws-browse-parent').onclick = () => { const b = wsState.browse; b.dir = b.dir.includes('/') ? b.dir.slice(0, b.dir.lastIndexOf('/')) : '.'; action(loadBrowseDir)(); };
$('ws-browse-select').onclick = () => { const b = wsState.browse; $(b.field).value = hostPathOf(b.dir); $('ws-browse-dialog').close(); };

/* ── 辅助资料多来源（FR-82~84） ── */
async function loadSourcesList() { state.sources = (await api('/sources')).sources || []; renderSourceChips(); }
function renderSourceChips() {
  const host = $('source-chips');
  const visible = state.root === 'context';
  host.classList.toggle('hidden', !visible);
  if (!visible) return;
  host.replaceChildren();
  state.sources.filter(x => x.enabled).forEach(src => {
    const chip = el('button', 'source-chip' + (state.source === src.id ? ' active' : ''), (src.rw ? '✎ ' : '') + (src.builtin ? t(src.name) : src.name) + (src.builtin ? ' 🔒' : ''));
    chip.title = src.type + (src.config.path || src.config.url || src.config.host || '') + (src.rw ? t(" · 读写") : t(" · 只读"));
    chip.onclick = () => { state.source = src.id; state.dir = '.'; state.attachments = []; renderAttachments(); renderSourceChips(); action(loadFiles)(); };
    host.append(chip);
    if (!src.builtin) {
      const del = el('button', 'source-chip-x', '×');
      del.title = t("删除来源 ") + src.name;
      del.onclick = () => { if (confirm(t('删除来源「{0}」？', src.name))) action(async () => { await api('/sources', { method: 'PUT', body: JSON.stringify({ sources: state.sources.filter(x => x.id !== src.id) }) }); await loadSourcesList(); if (state.source === src.id) { state.source = ''; state.dir = '.'; await loadFiles(); } })(); };
      host.append(del);
    }
  });
  const add = el('button', 'source-chip-add', t("＋ 来源"));
  add.onclick = () => { $('source-form').reset(); renderSourceFields(); $('source-dialog').showModal(); };
  host.append(add);
}
function renderSourceFields() {
  const type = $('src-type').value;
  $('src-rw').disabled = !['local', 'skill', 'sftp'].includes(type);
  if ($('src-rw').disabled) $('src-rw').checked = false;
  const host = $('src-fields');
  host.replaceChildren();
  const addField = (labelText, id, placeholder) => { const label = el('label', '', labelText); const input = el('input', ''); input.id = id; input.placeholder = placeholder || ''; input.autocomplete = 'off'; label.append(input); host.append(label); return input; };
  if (type === 'local' || type === 'skill') {
    const input = addField(t("本机路径（绝对路径）"), 'src-path', state.config?.hostLocal || '/local');
    const row = el('div', 'source-path-row'); input.parentNode.append(row); row.append(input);
    const browse = el('button', 'quiet', t('浏览…')); browse.type = 'button'; browse.onclick = () => openBrowse('src-path'); row.append(browse);
  }
  else if (type === 'link' || type === 'ftp' || type === 'ftps' || type === 'smb') {
    addField(t("URL（如 ftp://host/dir 或 https://…）"), 'src-url', type === 'link' ? 'https://' : type + '://');
    if (type !== 'link') { addField(t('用户名'), 'src-user', ''); addField(t('密码（可选）'), 'src-password', '').type = 'password'; }
  }
  else if (type === 'mcp') { addField(t("启动命令"), 'src-command', 'npx -y @modelcontextprotocol/server-…'); addField(t("或 URL"), 'src-url', ''); }
  else if (type === 'sftp') {
    addField(t("主机"), 'src-host', '192.168.1.10');
    addField(t("端口"), 'src-port', '22').type = 'number';
    addField(t("用户名"), 'src-user', 'root');
    addField(t("远程目录"), 'src-remote', '/srv/refs');
    addField(t("密码（可选）"), 'src-password', t("留空 = 无密码认证")).type = 'password';
    addField(t("私钥（可选，优先于密码）"), 'src-key', t("粘贴私钥内容")).type = 'password';
  }
}
$('src-type').addEventListener('change', renderSourceFields);
$('source-form').onsubmit = action(async event => {
  event.preventDefault();
  const type = $('src-type').value;
  const name = $('src-name').value.trim();
  const id = 's-' + Math.random().toString(36).slice(2, 8);
  const src = { id, name, type, enabled: true, rw: $('src-rw').checked, config: {} };
  const secrets = {};
  if (type === 'local' || type === 'skill') src.config.path = $('src-path').value.trim();
  else if (type === 'mcp') { src.config.command = ($('src-command')?.value || '').trim(); src.config.url = ($('src-url')?.value || '').trim(); }
  else if (type === 'sftp') { src.config = { path: $('src-remote').value.trim(), host: $('src-host').value.trim(), port: parseInt($('src-port').value, 10) || 22, username: $('src-user').value.trim(), auth: $('src-key').value ? 'key' : $('src-password').value ? 'password' : 'none' }; const pw = $('src-password').value, key = $('src-key').value; if (pw || key) secrets[id] = { password: pw, key }; }
  else { src.config.url = $('src-url').value.trim(); if ($('src-user')) src.config.username = $('src-user').value.trim(); if ($('src-password')?.value) secrets[id] = { password: $('src-password').value }; }
  const payload = { sources: [...state.sources.filter(x => !x.builtin), src] };
  if (Object.keys(secrets).length) payload.secrets = secrets;
  await api('/sources', { method: 'PUT', body: JSON.stringify(payload) });
  $('source-dialog').close();
  await loadSourcesList();
  state.source = id; state.dir = '.'; if (type !== 'mcp') await loadFiles(); else $('files').replaceChildren(el('p', 'muted', t('MCP 仅支持登记，尚未接入协议调用。')));
  toast(t("已添加来源：") + name);
});
/* ── Markdown 渲染：基于 marked v12（MIT，vendor/marked.min.js，GFM 全特性）
     输出经 DOM 消毒（去 script/style/iframe/事件属性/javascript: 链接），
     JS/TS 代码块后处理语法高亮；marked 不可用时回退纯文本转义。 ── */
function escapeHtml(str) { return String(str).replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;'); }
const JS_KEYWORDS = 'const|let|var|function|return|if|else|for|while|do|switch|case|break|continue|class|extends|new|try|catch|finally|throw|async|await|import|export|from|default|typeof|instanceof|in|of|yield|delete|void|this|super|null|undefined|true|false|static|get|set';
function highlightCode(code, lang) {
  let esc = escapeHtml(code);
  if (!/^(js|javascript|jsx|ts|typescript|mjs)$/i.test(lang || '')) return esc;
  esc = esc.replace(new RegExp('\\b(' + JS_KEYWORDS + ')\\b', 'g'), '<span class="tok-k">$1</span>');
  esc = esc.replace(/\b(\d+(?:\.\d+)?)\b/g, '<span class="tok-n">$1</span>');
  const protectedSegments = [];
  esc = esc.replace(/(\/\/[^\n]*)|(\/\*[\s\S]*?\*\/)|(&quot;(?:[^&]|&(?!quot;))*?&quot;|&#39;(?:[^&]|&(?!#39;))*?&#39;|`[^`]*`)/g, (m, comment) => {
    const index = protectedSegments.length;
    protectedSegments.push('<span class="' + (comment ? 'tok-c' : 'tok-s') + '">' + m + '</span>');
    return '\u0001' + index + '\u0002';
  });
  esc = esc.replace(/\u0001(\d+)\u0002/g, (_, i) => protectedSegments[+i]);
  return esc;
}
const BLOCKED_TAGS = new Set(['script', 'style', 'iframe', 'object', 'embed', 'form', 'meta', 'link', 'base', 'svg', 'math']);
function sanitizeHtml(html) {
  const doc = new DOMParser().parseFromString(html, 'text/html');
  doc.body.querySelectorAll('*').forEach(el => {
    if (BLOCKED_TAGS.has(el.tagName.toLowerCase())) { el.remove(); return; }
    for (const attr of [...el.attributes]) {
      const name = attr.name.toLowerCase();
      if (name.startsWith('on')) { el.removeAttribute(attr.name); continue; }
      if ((name === 'href' || name === 'src') && /^\s*(javascript|vbscript|data:text\/html)/i.test(attr.value)) el.removeAttribute(attr.name);
    }
  });
  return doc.body;
}
/* draw.io embed 正确协议：等 init 事件后再 load，save 事件回写 */
function setupDrawioFrame(container, xml, onSave, handlerKey) {
  container.innerHTML = '';
  const iframe = document.createElement('iframe');
  iframe.src = '/vendor/drawio/?embed=1&proto=json&spin=1';
  const immersive = document.body.classList.contains('file-view-mode');
  iframe.style.cssText = immersive
    ? 'width:100%;height:100%;border:0;'
    : 'width:100%;height:75vh;min-height:400px;border:0;border-radius:8px';
  iframe.setAttribute('allow', 'fullscreen');
  container.appendChild(iframe);
  let loaded = false;
  let timedOut = false;
  const timeoutId = setTimeout(() => {
    if (!loaded) {
      timedOut = true;
      container.innerHTML = '<div style="padding:40px;text-align:center;color:var(--text-dim);"><p>📐 draw.io 加载超时</p><p style="font-size:12px;margin-top:8px;">本地 draw.io 加载失败，请刷新页面重试</p></div>';
    }
  }, 15000);
  if (window[handlerKey]) window.removeEventListener('message', window[handlerKey]);
  window[handlerKey] = (ev) => {
    if (ev.source !== iframe.contentWindow) return;
    let msg;
    try { msg = JSON.parse(ev.data); } catch (e) { return; }
    if (msg.event === 'init') {
      clearTimeout(timeoutId);
      loaded = true;
      iframe.contentWindow.postMessage(JSON.stringify({ action: 'load', xml: xml || '<mxfile host="aide-local"><diagram></diagram></mxfile>' }), '*');
    } else if (msg.event === 'save') {
      const newXml = msg.xml;
      if (newXml && onSave) {
        onSave(newXml);
        iframe.contentWindow.postMessage(JSON.stringify({ action: 'status', message: '已保存' }), '*');
      }
    } else if (msg.event === 'exit') {
      // draw.io 请求关闭，忽略（由用户控制弹窗）
    }
  };
  window.addEventListener('message', window[handlerKey]);
}

/* 图片预览器：缩放/平移/适应窗口/原始大小 */
function setupImagePreview(container, filePath, root, source) {
  container.innerHTML = '';
  const token = state.token || (state.config && state.config.accessToken) || '';
  const params = new URLSearchParams();
  params.set('path', filePath);
  if (source) params.set('source', source);
  else params.set('root', root || 'workspace');
  if (token) params.set('access_token', token);
  const imgUrl = '/api/file/raw?' + params.toString();
  const viewer = el('div', 'img-viewer');
  const toolbar = el('div', 'img-toolbar');
  const info = el('span', 'img-info', filePath.split('/').pop());
  const dims = el('span', 'img-dims', '');
  const btnZoomIn = el('button', 'img-ctrl', '＋');
  const btnZoomOut = el('button', 'img-ctrl', '－');
  const btnFit = el('button', 'img-ctrl', '适应');
  const btnOrig = el('button', 'img-ctrl', '1:1');
  const btnClose = el('button', 'img-ctrl', '✕');
  btnZoomIn.title = '放大'; btnZoomOut.title = '缩小'; btnFit.title = '适应窗口'; btnOrig.title = '原始大小'; btnClose.title = '关闭';
  toolbar.append(info, dims, btnZoomOut, btnZoomIn, btnFit, btnOrig, btnClose);
  const canvas = el('div', 'img-canvas');
  const img = el('img', 'img-preview');
  img.alt = filePath;
  img.src = imgUrl;
  canvas.append(img);
  viewer.append(toolbar, canvas);
  container.append(viewer);
  let scale = 1, tx = 0, ty = 0, dragging = false, startX = 0, startY = 0;
  const apply = () => { img.style.transform = `translate(${tx}px,${ty}px) scale(${scale})`; };
  const fit = () => {
    const cw = canvas.clientWidth, ch = canvas.clientHeight;
    const iw = img.naturalWidth || img.width, ih = img.naturalHeight || img.height;
    if (iw && ih) { scale = Math.min(cw / iw, ch / ih, 1); tx = 0; ty = 0; apply(); }
  };
  img.onload = () => { dims.textContent = img.naturalWidth + '×' + img.naturalHeight; fit(); };
  img.onerror = () => { canvas.innerHTML = '<div style="color:var(--warn);padding:40px;text-align:center;">图片加载失败</div>'; };
  btnZoomIn.onclick = () => { scale = Math.min(scale * 1.25, 8); apply(); };
  btnZoomOut.onclick = () => { scale = Math.max(scale / 1.25, 0.1); apply(); };
  btnFit.onclick = fit;
  btnOrig.onclick = () => { scale = 1; tx = 0; ty = 0; apply(); };
  btnClose.onclick = () => {
    const dlg = container.closest('dialog');
    if (dlg) dlg.close();
    else if (document.body.classList.contains('file-view-mode')) { history.back(); }
  };
  canvas.onwheel = (e) => { e.preventDefault(); const f = e.deltaY < 0 ? 1.1 : 0.9; scale = Math.max(0.1, Math.min(8, scale * f)); apply(); };
  canvas.onmousedown = (e) => { dragging = true; startX = e.clientX - tx; startY = e.clientY - ty; canvas.style.cursor = 'grabbing'; };
  window.addEventListener('mousemove', (e) => { if (dragging) { tx = e.clientX - startX; ty = e.clientY - startY; apply(); } });
  window.addEventListener('mouseup', () => { dragging = false; canvas.style.cursor = 'grab'; });
  canvas.style.cursor = 'grab';
}

/* STL 3D 模型预览器：Three.js + STLLoader + OrbitControls */
/* 动态确保 vendor 脚本加载（兜底 defer 未生效 / 缓存失败） */
function ensureVendorScript(src, check) {
  return new Promise((resolve, reject) => {
    if (check()) return resolve();
    const sc = document.createElement('script');
    sc.src = src; sc.async = false;
    sc.onload = () => (check() ? resolve() : reject(new Error(src + ' 加载后仍不可用')));
    sc.onerror = () => reject(new Error('无法加载 ' + src));
    document.head.appendChild(sc);
  });
}
async function ensureThreeStack() {
  await ensureVendorScript('/vendor/three.min.js', () => typeof THREE !== 'undefined');
  await ensureVendorScript('/vendor/STLLoader.js', () => typeof THREE !== 'undefined' && !!THREE.STLLoader);
  await ensureVendorScript('/vendor/OrbitControls.js', () => typeof THREE !== 'undefined' && !!THREE.OrbitControls);
}
/* 探测可用 WebGL 上下文（允许软件渲染降级），返回类型名或 null */
function detectWebGLContext() {
  const c = document.createElement('canvas');
  const opts = { failIfMajorPerformanceCaveat: false, antialias: true };
  for (const ty of ['webgl2', 'webgl', 'experimental-webgl']) {
    try { if (c.getContext(ty, opts)) return ty; } catch (e) {}
  }
  return null;
}
/* STL 3D 模型预览器：Three.js + STLLoader + OrbitControls */
async function setupStlPreview(container, filePath, root, source) {
  container.innerHTML = '';
  const loading = el('div', 'stl-loading', t('正在加载 3D 预览组件…'));
  loading.style.cssText = 'padding:40px;text-align:center;color:var(--text-dim)';
  container.append(loading);

  let glType = null;
  try {
    await ensureThreeStack();
    glType = detectWebGLContext();
  } catch (e) {
    loading.remove();
    container.innerHTML = '<div style="padding:40px;text-align:center;color:var(--warn);">3D ' + t('预览组件加载失败：') + escapeHtml(e.message) + '</div>';
    return;
  }

  const token = state.token || (state.config && state.config.accessToken) || '';
  const qp = new URLSearchParams();
  qp.set('path', filePath);
  if (source) qp.set('source', source); else qp.set('root', root || 'workspace');
  if (token) qp.set('access_token', token);
  const rawUrl = '/api/file/raw?' + qp.toString();

  if (!glType) {
    loading.remove();
    const box = el('div', 'stl-nowebgl');
    box.style.cssText = 'padding:36px 32px;text-align:center;';
    box.innerHTML =
      '<div style="font-size:14px;font-weight:600;color:var(--warn);margin-bottom:8px;">' + t('当前浏览器未启用 WebGL，无法渲染 3D 模型') + '</div>' +
      '<div style="font-size:12px;color:var(--text-dim);margin-bottom:18px;line-height:1.7;">' + t('可改用系统浏览器打开，或在浏览器设置中开启硬件加速（GPU）。') + '</div>';
    const dl = el('a', 'stl-download', t('下载该 STL 文件'));
    dl.href = rawUrl;
    box.append(dl);
    container.append(box);
    return;
  }

  const viewer = el('div', 'stl-viewer');
  const toolbar = el('div', 'stl-toolbar');
  const info = el('span', 'stl-info', filePath.split('/').pop());
  const meta = el('span', 'stl-meta', t('加载中…'));
  const btnReset = el('button', 'stl-ctrl', t('重置视角'));
  const btnClose = el('button', 'stl-ctrl', '✕');
  toolbar.append(info, meta, btnReset, btnClose);
  const canvasWrap = el('div', 'stl-canvas');
  viewer.append(toolbar, canvasWrap);
  loading.remove();
  container.append(viewer);
  btnClose.onclick = () => {
    const dlg = container.closest('dialog');
    if (dlg) dlg.close();
    else if (document.body.classList.contains('file-view-mode')) history.back();
  };

  const scene = new THREE.Scene();
  scene.background = new THREE.Color(0x1a1a2e);
  const camera = new THREE.PerspectiveCamera(45, 1, 0.1, 10000);
  const renderer = new THREE.WebGLRenderer({ antialias: true, failIfMajorPerformanceCaveat: false });
  renderer.setPixelRatio(window.devicePixelRatio);
  canvasWrap.appendChild(renderer.domElement);
  scene.add(new THREE.AmbientLight(0xffffff, 0.5));
  const dl1 = new THREE.DirectionalLight(0xffffff, 0.8); dl1.position.set(5, 10, 7); scene.add(dl1);
  const dl2 = new THREE.DirectionalLight(0xffffff, 0.3); dl2.position.set(-5, -3, -5); scene.add(dl2);
  const grid = new THREE.GridHelper(20, 20, 0x444466, 0x333355); scene.add(grid);
  const controls = new THREE.OrbitControls(camera, renderer.domElement);
  controls.enableDamping = true; controls.dampingFactor = 0.08;

  fetch(rawUrl).then(r => { if (!r.ok) throw new Error('HTTP ' + r.status); return r.arrayBuffer(); })
    .then(buf => {
      const geometry = new THREE.STLLoader().parse(buf);
      geometry.computeVertexNormals(); geometry.computeBoundingBox();
      const mesh = new THREE.Mesh(geometry, new THREE.MeshPhongMaterial({ color: 0x60a5fa, specular: 0x111111, shininess: 80 }));
      const bb = geometry.boundingBox; const center = new THREE.Vector3(); bb.getCenter(center);
      mesh.position.sub(center); scene.add(mesh);
      grid.position.y = bb.min.y - center.y;
      const size = new THREE.Vector3(); bb.getSize(size);
      const maxDim = Math.max(size.x, size.y, size.z);
      const camDist = Math.abs(maxDim / 2 / Math.tan(camera.fov * Math.PI / 360)) * 1.8;
      camera.position.set(camDist, camDist * 0.7, camDist);
      camera.near = camDist / 100; camera.far = camDist * 100; camera.updateProjectionMatrix();
      controls.target.set(0, 0, 0); controls.update();
      meta.textContent = Math.round(geometry.attributes.position.count / 3) + ' ' + t('三角面') + ' · ' + size.x.toFixed(2) + '×' + size.y.toFixed(2) + '×' + size.z.toFixed(2);
      btnReset.onclick = () => { camera.position.set(camDist, camDist * 0.7, camDist); controls.target.set(0, 0, 0); controls.update(); };
      (function animate() { requestAnimationFrame(animate); controls.update(); renderer.render(scene, camera); })();
      const resize = () => { const w = canvasWrap.clientWidth, h = canvasWrap.clientHeight; if (w > 0 && h > 0) { camera.aspect = w / h; camera.updateProjectionMatrix(); renderer.setSize(w, h); } };
      resize(); new ResizeObserver(resize).observe(canvasWrap);
    }).catch(err => { canvasWrap.innerHTML = '<div style="padding:40px;text-align:center;color:var(--warn);">' + t('STL 加载失败：') + escapeHtml(err.message) + '</div>'; meta.textContent = t('解析失败'); });
}
function renderMarkdown(src, live, basePath) {
  if (window.marked && typeof window.marked.parse === 'function') {
    const html = window.marked.parse(String(src || ''), { gfm: true, breaks: false });
    const body = sanitizeHtml(html);
    // mermaid 流程图：把 ```mermaid 代码块替换成 <div class="mermaid"> 供后续渲染
    body.querySelectorAll('pre > code.language-mermaid').forEach(codeEl => {
      const pre = codeEl.parentElement;
      const div = el('div', 'mermaid', codeEl.textContent);
      pre.replaceWith(div);
    });
    // live（流式渲染）跳过代码高亮：每帧全量高亮代价高，完成态由 renderSession 补全
    if (!live) body.querySelectorAll('pre > code[class*="language-"]').forEach(codeEl => {
      const lang = (codeEl.className.match(/language-([\w+-]+)/) || [])[1] || '';
      if (/^(js|javascript|jsx|ts|typescript|mjs)$/i.test(lang)) codeEl.innerHTML = highlightCode(codeEl.textContent, lang);
    });
    // 标记相对路径链接（事件委托在 timeline 上统一处理）
    body.querySelectorAll('a[href]').forEach(a => {
      const href = a.getAttribute('href') || '';
      if (/^(https?:|mailto:|#|data:)/.test(href)) return;
      a.dataset.internalLink = href;
    });
    // 相对路径图片：改写为 /api/file/raw 原始字节端点（img 无法带 Authorization 头，用 access_token 查询参数）
    if (basePath) {
      const baseDir = basePath.includes('/') ? basePath.slice(0, basePath.lastIndexOf('/')) : '';
      body.querySelectorAll('img[src]').forEach(img => {
        const orig = img.getAttribute('src') || '';
        if (/^(https?:|data:|blob:|\/\/)/.test(orig)) return;
        let resolved = orig;
        if (baseDir) {
          const parts = [];
          for (const seg of (baseDir + '/' + orig).split('/')) {
            if (seg === '' || seg === '.') continue;
            if (seg === '..') { parts.pop(); continue; }
            parts.push(seg);
          }
          resolved = parts.join('/');
        }
        img.src = '/api/file/raw?root=workspace&path=' + encodeURIComponent(resolved) + '&access_token=' + encodeURIComponent(state.token);
        img.onerror = () => { img.style.opacity = '0.4'; img.title = '图片加载失败: ' + orig; };
      });
    }
    return body.innerHTML;
  }
  return '<pre>' + escapeHtml(String(src || '')) + '</pre>';
}
/* ── 单文件视图（新标签页）：路径 + 可编辑内容（保存带哈希校验）+ md 渲染预览 ── */
const fileView = { spec: null, hash: '' };
function setFileViewMode(mode) {
  const preview = mode === 'preview';
  $('file-view-editor').classList.toggle('hidden', preview);
  $('file-view-preview').classList.toggle('hidden', !preview);
  $('fv-edit').classList.toggle('active', !preview);
  $('fv-preview').classList.toggle('active', preview);
  if (preview) { $('file-view-preview').innerHTML = renderMarkdown($('file-view-editor').value, false, fileView.spec ? fileView.spec.path : ''); $('file-view-preview').scrollTop = 0; }
}
async function openFileViewMode() {
  let spec = null;
  try { spec = JSON.parse(decodeURIComponent(new URLSearchParams(location.hash.slice(1)).get('file') || '')); } catch (e) { spec = null; }
  if (!spec) return;
  document.body.classList.add('file-view-mode');
  $('file-view').classList.remove('hidden');
  fileView.spec = spec; fileView.wsId = '';
  $('file-view-path').textContent = (spec.source ? 'sources/' + spec.source : spec.root) + ' · ' + spec.path;
  const md = isMarkdownPath(spec.path);
  const isDrawio = /\.drawio$/i.test(spec.path || '');
  const isImg = isImagePath(spec.path);
  const isStl = isStlPath(spec.path);
  // 图片 / STL 走独立 raw 查看器，跳过只支持文本、会拒绝二进制的 /api/file
  let data;
  if (isImg || isStl) {
    data = { content: '', hash: '', workspaceId: '', wsId: '' };
  } else {
    const query = spec.source
      ? '/file?source=' + encodeURIComponent(spec.source) + '&path=' + encodeURIComponent(spec.path)
      : '/file?root=' + encodeURIComponent(spec.root) + '&path=' + encodeURIComponent(spec.path);
    data = await api(query);
  }
  fileView.hash = data.hash; fileView.wsId = data.workspaceId || data.wsId || '';
  $('file-view-mode-switch').classList.toggle('hidden', !md);
  const readOnly = spec.root !== 'workspace' && !(spec.source && state.sources.find(x => x.id === spec.source)?.rw === true);
  $('file-view-editor').value = data.content;
  $('file-view-editor').readOnly = readOnly;
  // 图片 / STL 只读查看器禁用保存；drawio 可保存
  $('file-view-save').disabled = readOnly || isImg || isStl;
  $('file-view-status').textContent = readOnly ? t("只读") : t("可编辑 · 保存后同步");
  if (isStl) {
    $('file-view-editor').classList.add('hidden');
    $('file-view-preview').classList.remove('hidden');
    setupStlPreview($('file-view-preview'), spec.path, spec.root, spec.source || '');
  } else if (isImg) {
    $('file-view-editor').classList.add('hidden');
    $('file-view-preview').classList.remove('hidden');
    setupImagePreview($('file-view-preview'), spec.path, spec.root, spec.source || '');
  } else if (isDrawio) {
    $('file-view-editor').classList.add('hidden');
    $('file-view-preview').classList.remove('hidden');
    setupDrawioFrame($('file-view-preview'), data.content, (xml) => {
      $('file-view-editor').value = xml;
      $('file-view-save').click();
      toast(t('draw.io 已保存'));
    }, '_fileViewDrawioHandler');
  } else {
    setFileViewMode(md ? 'preview' : 'edit');
  }
  $('file-view-toolbar').classList.remove('hidden');
  $('file-view-content').replaceChildren();
}
$('fv-edit').onclick = () => setFileViewMode('edit');
$('fv-preview').onclick = () => setFileViewMode('preview');
$('file-view-save').onclick = action(async () => {
  const body = { path: fileView.spec.path, content: $('file-view-editor').value, hash: fileView.hash };
  if (fileView.spec.source) body.source = fileView.spec.source; if (fileView.wsId) body.workspaceId = fileView.wsId;
  const res = await api('/file', { method: 'PUT', body: JSON.stringify(body) });
  fileView.hash = res.hash;
  $('file-view-status').textContent = t("✓ 已保存");
});
/* ── 会话轨迹（DSH TrajectoryView 风格：turn-aware 事件时间线） ── */
function trajectoryEvent(dot, title, bodyNode, kind) {
  const ev = el('div', 'traj-event ' + (kind || ''));
  const head = el('div', 'traj-event-head');
  head.append(el('span', 'traj-dot', dot), el('strong', '', title));
  ev.append(head);
  if (bodyNode) ev.append(bodyNode);
  return ev;
}
let trajView = "history"; // history | calls（一级）
let trajFmt = "md"; // md | json（历史视图格式，二级）
function renderTrajectory() {
  const host = $('trajectory-content');
  host.replaceChildren();
  const session = state.session;
  if (!session || !session.runs?.length) {
    host.append(el('p', 'muted', t("当前会话还没有任务。发送任务后，这里会按事件时间线记录完整轨迹。")));
    return;
  }
  const fmtSeg = $('traj-fmt-seg');
  if (trajView === 'calls') { if (fmtSeg) fmtSeg.classList.add('hidden'); renderCallsAnalysis(host, session); return; }
  if (fmtSeg) fmtSeg.classList.remove('hidden');
  const pre = el('pre', 'traj-raw-view');
  pre.textContent = trajFmt === 'json' ? buildTrajectoryJSON(session) : buildTrajectoryMarkdown(session);
  host.append(pre);
}
function buildTrajectoryMarkdown(s) {
  const subs = (state.sessions || []).filter(x => x.parentId === s.id);
  let md = "# " + (s.title || "未命名会话") + "\n\n";
  md += "> 导出时间：" + new Date().toLocaleString() + " · 会话 ID：" + s.id + "\n\n---\n\n";
  for (const r of (s.runs || [])) {
    md += "## 任务 · " + (r.created || "") + "\n\n";
    md += "**用户：** " + (r.prompt || "") + "\n\n";
    if (r.usage?.total) md += "**Token 消耗：** " + r.usage.total + (r.usage.estimated ? "（估）" : "") + "\n\n";
    if (r.steps) for (const st of r.steps) md += "- " + st.name + " · " + st.status + (st.content ? "\n  > " + String(st.content).slice(0,500) : "") + "\n";
    if (r.toolUses) for (const tu of r.toolUses) {
      md += "**工具 " + tu.tool + "：**\n```\n" + String(tu.preview || tu.result || "") + "\n```\n\n";
    }
    if (r.error) md += "**错误：** " + r.error + "\n\n";
    md += "\n---\n\n";
  }
  for (const sub of subs) {
    md += "## 子会话：" + (sub.title || sub.id) + "\n\n";
    for (const r of (sub.runs || [])) {
      md += "### " + (r.created || "") + "\n\n";
      md += "**用户：** " + (r.prompt || "") + "\n\n";
      if (r.toolUses) for (const tu of r.toolUses) md += "- " + tu.tool + ": " + String(tu.preview || tu.result || "").slice(0, 200) + "\n";
      md += "\n";
    }
    md += "---\n\n";
  }
  return md;
}
function buildTrajectoryJSON(s) {
  const subs = (state.sessions || []).filter(x => x.parentId === s.id);
  const data = {
    exportedAt: new Date().toISOString(),
    session: { id: s.id, title: s.title, created: s.created, updated: s.updated, parentId: s.parentId },
    runs: (s.runs || []).map(r => ({
      id: r.id, created: r.created, prompt: r.prompt, mode: r.mode, model: r.model,
      status: r.status, error: r.error, usage: r.usage,
      steps: r.steps, toolUses: r.toolUses, files: r.files, commands: r.commands
    })),
    subSessions: subs.map(sub => ({ id: sub.id, title: sub.title, created: sub.created, runs: sub.runs })),
    timeline: []
  };
  for (const r of (s.runs || [])) {
    data.timeline.push({ time: r.created, type: 'user', content: r.prompt });
    for (const st of (r.steps || [])) data.timeline.push({ time: r.created, type: 'step', name: st.name, status: st.status });
    for (const tu of (r.toolUses || [])) data.timeline.push({ time: r.created, type: 'tool', tool: tu.tool, args: tu.args, result: tu.preview || tu.result });
  }
  return JSON.stringify(data, null, 2);
}
let callFilter = { tool: '', agent: 'all', type: 'all', time: 'all' };
const CALL_TYPES = {
  file: ['list_files','read_file','write_file','create_diagram'],
  shell: ['run_shell'],
  search: ['search_text','semantic_search','web_search'],
  memory: ['read_memory','write_memory'],
  subagent: ['spawn_subagent'],
};
function callTypeOf(tool) {
  for (const [t, tools] of Object.entries(CALL_TYPES)) { if (tools.includes(tool)) return t; }
  return 'other';
}
function renderCallsAnalysis(host, session) {
  const calls = [];
  for (const run of (session.runs || [])) {
    for (const tu of (run.toolUses || [])) {
      calls.push({ agent: 'main', agentName: t('主 Agent'), time: run.created, tool: tu.tool, args: tu.args, result: tu.preview || tu.result });
    }
  }
  const subs = (state.sessions || []).filter(x => x.parentId === session.id);
  for (const sub of subs) {
    calls.push({ agent: 'sub', agentName: sub.title || t('子 Agent'), time: sub.created, tool: '(子会话启动)', args: sub.id, result: sub.title });
    (async () => {
      try {
        const detail = await api('/sessions/' + sub.id);
        for (const r of (detail.runs || [])) {
          for (const tu of (r.toolUses || [])) {
            calls.push({ agent: 'sub', agentName: sub.title || t('子 Agent'), time: r.created, tool: tu.tool, args: tu.args, result: tu.preview || tu.result });
          }
        }
        renderCallsTable(host, calls);
      } catch(e) {}
    })();
  }
  renderCallsTable(host, calls);
}
function renderCallsTable(host, calls) {
  host.replaceChildren();
  const toolNames = [...new Set(calls.map(c => c.tool))].sort();
  const bar = el('div', 'call-filter-bar');
  bar.append(el('span', 'muted', t('筛选：')));
  const mkSel = (opts, val, onChange) => {
    const sel = el('select', 'call-filter-select');
    opts.forEach(([v,l]) => { const o = el('option', '', l); o.value = v; if (v === val) o.selected = true; sel.append(o); });
    sel.onchange = onChange;
    return sel;
  };
  bar.append(mkSel([['',t('全部工具')], ...toolNames.map(n => [n,n])], callFilter.tool, () => { callFilter.tool = bar.children[1].value; renderCallsTable(host, calls); }));
  bar.append(mkSel([['all',t('全部类型')],['file',t('文件')],['shell',t('命令')],['search',t('搜索')],['memory',t('记忆')],['subagent',t('子Agent')],['other',t('其他')]], callFilter.type, () => { callFilter.type = bar.children[2].value; renderCallsTable(host, calls); }));
  bar.append(mkSel([['all',t('全部时间')],['1h',t('1小时')],['24h',t('24小时')],['7d',t('7天')]], callFilter.time, () => { callFilter.time = bar.children[3].value; renderCallsTable(host, calls); }));
  bar.append(mkSel([['all',t('全部')],['main',t('主Agent')],['sub',t('子Agent')]], callFilter.agent, () => { callFilter.agent = bar.children[4].value; renderCallsTable(host, calls); }));
  host.append(bar);
  const mainN = calls.filter(c => c.agent === 'main').length;
  const subN = calls.filter(c => c.agent === 'sub').length;
  const failN = calls.filter(c => /失败|error|拒绝|fail/i.test(String(c.result || ''))).length;
  host.append(el('div', 'call-summary', t('共 ') + calls.length + t(' 次 · 主') + mainN + t(' 子') + subN + t(' 失败') + failN));
  const now = Date.now();
  const tms = { '1h': 3600000, '24h': 86400000, '7d': 604800000 };
  const filtered = calls.filter(c => {
    if (callFilter.tool && c.tool !== callFilter.tool) return false;
    if (callFilter.agent !== 'all' && c.agent !== callFilter.agent) return false;
    if (callFilter.type !== 'all' && callTypeOf(c.tool) !== callFilter.type) return false;
    if (callFilter.time !== 'all' && c.time && now - new Date(c.time).getTime() > tms[callFilter.time]) return false;
    return true;
  });
  if (!filtered.length) { host.append(el('p', 'muted', t("没有匹配的调用记录"))); return; }
  const table = el('table', 'call-table');
  const headRow = el('tr', '');
  headRow.append(
    el('th', '', t("时间")), el('th', '', t("谁")), el('th', '', t("工具")), el('th', '', t("类型")), el('th', '', t("状态")), el('th', '', t("结果"))
  );
  const thead = el('thead', ''); thead.append(headRow); table.append(thead);
  const tbody = el('tbody', '');
  const typeLbl = { file: t('文件'), shell: t('命令'), search: t('搜索'), memory: t('记忆'), subagent: t('子Agent'), other: t('其他') };
  for (const c of filtered) {
    const fail = /失败|error|拒绝|fail/i.test(String(c.result || ''));
    const tr = el('tr', c.agent === 'sub' ? 'call-sub' : '');
    tr.append(
      el('td', 'call-time', (c.time || '').slice(11, 19)),
      el('td', '', c.agentName || (c.agent === 'sub' ? t("子 Agent") : t("主 Agent"))),
      el('td', 'call-tool', c.tool),
      el('td', '', typeLbl[callTypeOf(c.tool)] || callTypeOf(c.tool)),
      el('td', fail ? 'call-fail' : 'call-ok', fail ? '✖' : '✓'),
      el('td', 'call-result', String(c.result || '').slice(0, 120))
    );
    tbody.append(tr);
  }
  table.append(tbody);
  host.append(table);
}

function openTrajectory() {
  closeSettingsSheet();
  closeWorkspaceSheet();
  ensureTrajectoryTabs();
  renderTrajectory();
  $('trajectory-sheet').classList.add('open');
  $('settings-backdrop').classList.add('open');
}
function closeTrajectory() {
  $('trajectory-sheet').classList.remove('open');
  $('settings-backdrop').classList.remove('open');
}
$('trajectory-toggle').onclick = () => { if ($('trajectory-sheet').classList.contains('open')) closeTrajectory(); else openTrajectory(); };
$('trajectory-sheet-close').onclick = closeTrajectory;
let exportFormat = 'md';
$('trajectory-export').onclick = action(() => {
  if (!state.session) return toast(t("没有可导出的会话"));
  const s = state.session;
  const ts = new Date().toISOString().slice(0,19).replace(/[:T]/g,'-');
  const base = (s.title || "session").slice(0, 30).replace(/[\/:*?"<>|]/g, "_");
  const fmt = (trajView === 'history' && trajFmt === 'json') ? 'json' : 'md';
  if (fmt === 'json') {
    const blob = new Blob([buildTrajectoryJSON(s)], {type: "application/json;charset=utf-8"});
    const a = document.createElement("a");
    a.href = URL.createObjectURL(blob);
    a.download = base + "_" + ts + ".json";
    a.click();
    URL.revokeObjectURL(a.href);
    toast(t("已导出会话为 JSON"));
  } else {
    const blob = new Blob([buildTrajectoryMarkdown(s)], {type: "text/markdown;charset=utf-8"});
    const a = document.createElement("a");
    a.href = URL.createObjectURL(blob);
    a.download = base + "_" + ts + ".md";
    a.click();
    URL.revokeObjectURL(a.href);
    toast(t("已导出会话为 Markdown"));
  }
});
// 轨迹一级视图：历史 / 调用记录；历史二级格式：Markdown / JSON
function ensureTrajectoryTabs() {
  const mainSeg = document.querySelector('.traj-main-seg');
  if (mainSeg && !mainSeg.dataset.bound) {
    mainSeg.dataset.bound = '1';
    mainSeg.querySelectorAll('.traj-seg-btn').forEach(btn => {
      btn.onclick = () => {
        mainSeg.querySelectorAll('.traj-seg-btn').forEach(b => b.classList.remove('active'));
        btn.classList.add('active');
        trajView = btn.dataset.view;
        renderTrajectory();
      };
    });
  }
  const fmtSeg = $('traj-fmt-seg');
  if (fmtSeg && !fmtSeg.dataset.bound) {
    fmtSeg.dataset.bound = '1';
    fmtSeg.querySelectorAll('.traj-seg-btn').forEach(btn => {
      btn.onclick = () => {
        fmtSeg.querySelectorAll('.traj-seg-btn').forEach(b => b.classList.remove('active'));
        btn.classList.add('active');
        trajFmt = btn.dataset.fmt;
        renderTrajectory();
      };
    });
  }
}

/* ── 全局搜索（FR-92）：⌘K 聚焦，防抖检索会话缓存 ── */
let searchTimer = null;
$('global-search').addEventListener('input', () => {
  clearTimeout(searchTimer);
  const q = $('global-search').value.trim();
  if (!q) { $('search-results').classList.add('hidden'); return; }
  searchTimer = setTimeout(action(async () => {
    const data = await api('/search?q=' + encodeURIComponent(q));
    const host = $('search-results');
    host.replaceChildren();
    if (!data.results?.length) { host.append(el('p', 'muted', t("没有匹配的聊天"))); }
    data.results.forEach(res => {
      const row = el('button', 'search-result', '');
      const badge = res.archived ? el('span', 'archived-badge', t('已归档')) : null;
      row.append(el('strong', '', res.title), badge, el('span', '', res.snippet));
      row.onclick = () => { $('search-results').classList.add('hidden'); $('global-search').value = ''; action(() => selectSession(res.sessionId))(); };
      host.append(row);
    });
    host.classList.remove('hidden');
  }), 300);
});
document.addEventListener('keydown', event => {
  if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === 'k') { event.preventDefault(); $('global-search').focus(); $('global-search').select(); }
  if (event.key === 'Escape') $('search-results').classList.add('hidden');
});
document.addEventListener('click', event => {
  // md 相对路径链接：在 aide 内部打开
  const link = event.target.closest('a[data-internal-link]');
  if (link) {
    event.preventDefault();
    const href = link.dataset.internalLink;
    const path = href.replace(/^\.\//, '').split('#')[0];
    if (path) openFile(path).catch(() => toast(t("打不开文件: ") + path));
    return;
  }
  if (!event.target.closest('.global-search')) $('search-results').classList.add('hidden');
});
/* ── 手动压缩（FR-93） ── */
function refreshCompactInfo() {
  const sess = state.session;
  $('compact-info').textContent = sess?.compactedMessages ? t("已折叠 ") + sess.compactedMessages + t(" 条消息") : '';
}
$('compact-button').onclick = action(async () => {
  if (!state.session) { toast(t("请先选择会话")); return; }
  const res = await api('/sessions/' + state.session.id + '/compact', { method: 'POST', body: '{}' });
  toast(res.folded ? t("已压缩 ") + res.folded + t(" 条历史消息") : t("历史未超阈值，无需压缩"));
  await selectSession(state.session.id);
  refreshCompactInfo();
});
async function initialize() {
  syncPanelButtons();
  await refreshConfig();
  const fragment = new URLSearchParams(location.hash.slice(1));
  if (fragment.has('file')) { await Promise.all([loadWorkspaceConfig(), loadSourcesList()]); await openFileViewMode(); return; }
  await Promise.all([loadSessions(), loadFiles(), loadProfiles(), loadWorkspaceConfig(), loadSourcesList()]);
}
initialize().catch(error => { if (!$('login-dialog').open) $('login-dialog').showModal(); $('login-error').textContent = state.token ? error.message : ''; });

/* Compact-window navigation. Keeps session navigation reachable on iPhone/iPad. */
function closeSidebarNavigation() {
  document.body.classList.remove('sidebar-open');
  $('sidebar-scrim').hidden = true;
  $('sidebar-toggle').setAttribute('aria-expanded', 'false');
  $('sidebar-toggle').setAttribute('aria-label', t("展开会话导航"));
}
$('sidebar-toggle').onclick = () => {
  const open = !document.body.classList.contains('sidebar-open');
  document.body.classList.toggle('sidebar-open', open);
  $('sidebar-scrim').hidden = !open;
  $('sidebar-toggle').setAttribute('aria-expanded', String(open));
  $('sidebar-toggle').setAttribute('aria-label', open ? t("收起会话导航") : t("展开会话导航"));
  if (open) $('new-session').focus();
};
$('sidebar-scrim').onclick = () => { closeSidebarNavigation(); $('sidebar-toggle').focus(); };
$('sessions').addEventListener('click', event => { if (event.target.closest('button')) closeSidebarNavigation(); });
$('new-session').addEventListener('click', closeSidebarNavigation);
window.addEventListener('resize', () => { if (innerWidth > 700) closeSidebarNavigation(); });
// Utility windows are modal for keyboard users as well as pointer users.
document.addEventListener('keydown', event => {
  if (document.querySelector('dialog[open]')) return;
  const sheet = document.querySelector('.settings-sheet.open');
  const sidebar = document.body.classList.contains('sidebar-open') ? $('sidebar') : null;
  if (event.key === 'Escape' && sidebar && !sheet) {
    closeSidebarNavigation(); $('sidebar-toggle').focus(); return;
  }
  if (event.key !== 'Tab' || !(sheet || sidebar)) return;
  const controls = [...(sheet || sidebar).querySelectorAll('button, input, select, textarea, a[href], [tabindex="0"]')]
    .filter(node => !node.disabled && node.tabIndex >= 0 && node.getClientRects().length);
  if (!controls.length) return;
  const first = controls[0], last = controls[controls.length - 1];
  if (event.shiftKey && (document.activeElement === first || !(sheet || sidebar).contains(document.activeElement))) {
    event.preventDefault(); last.focus();
  } else if (!event.shiftKey && (document.activeElement === last || !(sheet || sidebar).contains(document.activeElement))) {
    event.preventDefault(); first.focus();
  }
});

window.addEventListener('aide:language', () => {
  const languageFocused = document.activeElement?.hasAttribute('data-language-button');
  if (settingsPanel.schema) renderSettingsSheet();
  if (languageFocused && $('settings-sheet').classList.contains('open')) $('settings-content').querySelector('[data-language-button][aria-pressed="true"]')?.focus();
  if (state.config) action(refreshConfig)();
  renderSession(); renderAttachments(); renderTrajectory(); renderSourceChips();
  refreshStrategyUI(); refreshCompactInfo(); estimateContext(); renderWorkspaceSummary(); updateSendEnabled();
  // mermaid 初始化
if (window.mermaid) mermaid.initialize({ startOnLoad: false, theme: 'neutral', securityLevel: 'loose' });
async function renderMermaid() {
  if (!window.mermaid) return;
  document.querySelectorAll('div.mermaid:not([data-processed])').forEach(async el => {
    try {
      const { svg } = await mermaid.render('m' + Math.random().toString(36).slice(2), el.textContent);
      el.innerHTML = svg; el.dataset.processed = '1';
    } catch (e) { el.innerHTML = '<pre style="color:#f87171">流程图渲染失败</pre>'; el.dataset.processed = '1'; }
  });
}
if (!document.body.classList.contains('file-view-mode') && state.config) { action(loadSessions)(); action(loadFiles)(); }
// 每次 renderMarkdown 后触发 mermaid 渲染
const _origRender = renderMarkdown;
renderMarkdown = function(src, live) { const html = _origRender(src, live); setTimeout(renderMermaid, 50); return html; };
  if (fileView.spec) $('file-view-status').textContent = $('file-view-editor').readOnly ? t('只读') : t('可编辑 · 保存后同步');
  if (!$('strategy-menu').classList.contains('hidden')) openStrategyMenu();
  if (state.contextPreview) renderContextPreview(state.contextPreview);
});

/* ── 语音小秘（Web Speech API 实时断句 + AI 甄别 + 直接发送）──────────
   边说边断：按句末标点/停顿自动成句 → 后端甄别 → 判定为指令的句子直接
   发送到当前会话（与手动提交同一 run 入口，兼容排队/工作流模式）；
   背景声自动忽略；与他人闲聊自动退下。语音框记录「已发送/已忽略」。 */
const voice = {
  recognition: null, listening: false, standby: false, awaitingReply: false,
  buffer: '', interim: '', timer: null, sending: false, queue: [], log: [], micStream: null
};
voice.name = () => (state.config && state.config.voiceAssistantName) || '小秘';
voice.supported = ('SpeechRecognition' in window) || ('webkitSpeechRecognition' in window);
try {
  if ('speechSynthesis' in window) {
    window.speechSynthesis.getVoices();
    window.speechSynthesis.onvoiceschanged = () => window.speechSynthesis.getVoices();
  }
} catch (_) {}

function voiceSetStatus(mode, text) {
  const box = $('voice-status');
  box.classList.remove('listening', 'ignored', 'standby');
  if (mode) box.classList.add(mode);
  $('voice-status-text').textContent = text;
}
function voiceRenderLog() {
  const host = $('voice-text');
  host.replaceChildren();
  for (const item of voice.log) {
    const line = el('div', 'voice-log-line ' + (item.type === 'sent' ? 'is-sent' : item.type === 'ignored' ? 'is-ignored' : 'is-standby'));
    const label = item.type === 'sent' ? t('已发送') : item.type === 'ignored' ? t('已忽略') : item.type === 'ask' ? t('追问') : t('已退下');
    const tag = el('span', 'voice-log-tag', label);
    const right = el('span', 'voice-log-text');
    if (item.text) right.append(el('span', '', item.text));
    if (item.reason) right.append(el('small', 'voice-log-reason', item.reason));
    line.append(tag, right);
    host.append(line);
  }
  if (voice.interim) host.append(el('div', 'voice-log-interim', '… ' + voice.interim));
  host.scrollTop = host.scrollHeight;
}
function voiceLog(type, text, reason) {
  voice.log.push({ type, text, reason });
  if (voice.log.length > 40) voice.log.shift();
  voiceRenderLog();
}
// 从未处理 buffer 中按句末标点切出完整句子，残句留在 buffer
function voiceExtractSentences() {
  const re = /[^。！？!?；;\n]+[。！？!?；;\n]+/g;
  const out = [];
  let m;
  while ((m = re.exec(voice.buffer)) !== null) out.push(m[0]);
  if (out.length) voice.buffer = voice.buffer.slice(out.join('').length);
  return out.map(x => x.trim()).filter(Boolean);
}
// 停顿成句：1.1s 无新结果且残句足够长，按完整一句话处理
function voiceArmPauseFlush() {
  clearTimeout(voice.timer);
  voice.timer = setTimeout(() => {
    const rest = voice.buffer.trim();
    voice.buffer = '';
    if (rest.length >= 6) { voice.queue.push(rest); voiceDrainQueue(); }
  }, 1100);
}
// 与手动提交同一 run 入口，尊重排队模式/工作流模式
async function voiceSend(text) {
  if (!state.config?.configured) throw new Error(t('请先配置模型'));
  let target = state.session, created = null;
  if (!target) {
    created = await api('/sessions', { method: 'POST', body: JSON.stringify({ title: t('新会话') }) });
    if (!state.session) state.session = created;
    target = created;
  }
  const strategy = state.profiles?.strategy || 'manual';
  await api(`/sessions/${target.id}/runs`, { method: 'POST', body: JSON.stringify({ prompt: text, mode: state.mode, attachments: [], strategy, profile: strategy === 'auto' ? '' : (state.profiles?.activeProfile || 'default'), queued: state.queueMode, workflowPhase: state.workflowPhase || '' }) });
  if (state.session?.id === target.id) await selectSession(target.id);
}
async function voiceFilterOne(sentence) {
  let result = { action: 'ignore', text: sentence, reason: '' };
  try { result = await api('/voice-filter', { method: 'POST', body: JSON.stringify({ text: sentence }) }); }
  catch (_) { result = { action: 'ignore', text: sentence, reason: t('甄别失败') }; }
  if (result.action === 'send') {
    const text = (result.text || sentence).trim();
    voice.awaitingReply = true; // 标记本次由小秘语音发起，run 完成后据开关朗读回复
    try {
      if (state.config && state.config.voiceReplyEnabled) {
        await typeIntoPrompt(text);
        $('task-form').requestSubmit();
      } else {
        await voiceSend(text);
      }
      voiceLog('sent', text, result.reason);
    }
    catch (e) { voiceLog('ignored', text + '（' + e.message + '）'); }
  } else if (result.action === 'ask') {
    voiceLog('ask', result.ask || result.text, result.reason);
    voiceSetStatus('standby', (result.ask || t('请补充说明你想做什么')));
    // 保持聆听，用户口头补充后进入下一轮分析，不发送
  } else if (result.action === 'standby') {
    voice.standby = true;
    voiceLog('standby', result.reason || t('和他人闲聊，已退下'));
    if (voice.recognition) { try { voice.recognition.onend = null; voice.recognition.stop(); } catch (_) {} }
    // 退下即收起面板，仅短暂 toast 提示
    $('voice-panel').classList.add('hidden');
    toast(result.reason || t('小秘已退下，点麦克风可重新唤起'));
  } else {
    voiceLog('ignored', sentence, result.reason);
  }
}
async function voiceDrainQueue() {
  if (voice.sending) return;
  voice.sending = true;
  try {
    while (voice.queue.length && !voice.standby) {
      const sentence = voice.queue.shift();
      await voiceFilterOne(sentence);
    }
  } finally { voice.sending = false; }
}

/* ── 双向语音：打字机输入 + TTS 朗读（浏览器原生 SpeechSynthesis，预留后端 TTS 替换） ── */
function ttsCancel() { try { if ('speechSynthesis' in window) window.speechSynthesis.cancel(); } catch (_) {} }
function pickVoiceForGender(gender) {
  if (!('speechSynthesis' in window)) return null;
  const voices = window.speechSynthesis.getVoices() || [];
  if (!voices.length) return null;
  const zh = voices.filter(x => /zh|cmn|chinese|mandarin/i.test((x.lang || '') + ' ' + (x.name || '')));
  const pool = zh.length ? zh : voices;
  const femaleKw = ['xiaoxiao','xiaoyi','xiaomei','huihui','yaoyao','tingting','mei-jia','sinji','female','女'];
  const maleKw = ['yunxi','yunjian','yunyang','yunxia','kangkang','male','男'];
  const kws = gender === 'male' ? maleKw : femaleKw;
  const isQuality = v => /neural|online|google|natural|云|网络/i.test(v.name || '');
  // 质量优先：神经网络/在线/Google 音色远比本地老式合成自然
  let v = pool.find(x => isQuality(x) && kws.some(k => (x.name || '').toLowerCase().includes(k)));
  if (!v) v = pool.find(x => kws.some(k => (x.name || '').toLowerCase().includes(k)));
  if (!v) v = pool.find(x => isQuality(x));
  if (!v) v = zh[0];
  return v || pool[0] || null;
}
// 韵律分句：按标点切成短段（同时规避 Chrome 长 utterance ~15s 卡死）
function ttsSegments(text) {
  const flat = text.replace(/[#*`>\[\]]/g, ' ').replace(/\s+/g, ' ').trim();
  const out = [];
  const re = /[^，。！？；：、,.!?;:…]+[，。！？；：、,.!?;:…]?/g;
  let m; while ((m = re.exec(flat))) { const t = m[0].trim(); if (t) out.push(t.slice(0, 120)); }
  return out.slice(0, 40);
}
function speakReply(text) {
  if (!state.config || !state.config.voiceReplyEnabled) return;
  if (!text || !('speechSynthesis' in window)) return;
  const segs = ttsSegments(text);
  if (!segs.length) return;
  const synth = window.speechSynthesis;
  synth.cancel();
  const voice = pickVoiceForGender(state.config.voiceReplyGender);
  const isMale = state.config.voiceReplyGender === 'male';
  const basePitch = isMale ? 0.99 : 1.1;   // 女声略提音调，更明亮灵动
  const baseRate = 1.04;
  let i = 0;
  function next() {
    if (i >= segs.length) return;
    const seg = segs[i];
    const u = new SpeechSynthesisUtterance(seg);
    u.lang = 'zh-CN';
    if (voice) u.voice = voice;
    const ask = /[?？]\s*$/.test(seg);
    const exclaim = /[!！]\s*$/.test(seg);
    const clause = /[，,、；;：:]\s*$/.test(seg);
    u.pitch = ask ? basePitch + 0.14 : exclaim ? basePitch + 0.06 : basePitch;
    u.rate = exclaim ? baseRate + 0.07 : ask ? baseRate - 0.04 : baseRate;
    u.volume = 1;
    u.onend = () => { const pause = ask ? 200 : clause ? 95 : 175; i++; setTimeout(next, pause); };
    u.onerror = () => { i++; next(); };
    synth.speak(u);
  }
  next();
}
async function typeIntoPrompt(text) {
  const prompt = $('prompt');
  prompt.value = '';
  prompt.focus();
  for (let i = 1; i <= text.length; i++) {
    prompt.value = text.slice(0, i);
    prompt.scrollTop = prompt.scrollHeight;
    await new Promise(r => setTimeout(r, 22));
  }
}

async function voiceStart() {
  if (!voice.supported) { toast(t("当前浏览器不支持语音识别，请用 Chrome/Edge，并通过 HTTPS 或 localhost 访问")); return; }
  await voiceOpenMicStream();
  const Ctor = window.SpeechRecognition || window.webkitSpeechRecognition;
  const rec = new Ctor();
  rec.lang = 'zh-CN'; rec.continuous = true; rec.interimResults = true;
  voice.recognition = rec;
  ttsCancel();
  voice.buffer = ''; voice.interim = ''; voice.standby = false; voice.queue = []; voice.log = [];
  $('voice-title').textContent = voice.name();
  voiceRenderLog();
  voiceSetStatus('listening', t("聆听中…说完一句会自动发送"));
  $('voice-panel').classList.remove('hidden');
  rec.onresult = (e) => {
    let interim = '';
    for (let i = e.resultIndex; i < e.results.length; i++) {
      const r = e.results[i];
      if (r.isFinal) voice.buffer += r[0].transcript;
      else interim += r[0].transcript;
    }
    voice.interim = interim;
    voice.queue.push(...voiceExtractSentences());
    voiceRenderLog();
    voiceDrainQueue();
    voiceArmPauseFlush();
  };
  rec.onerror = (e) => {
    if (e.error === 'not-allowed' || e.error === 'service-not-allowed') {
      toast(t("麦克风权限被拒绝，请在浏览器地址栏允许麦克风访问"));
      voiceClose();
    }
  };
  rec.onend = () => {
    if (voice.listening && !voice.standby) { try { rec.start(); } catch (_) {} }
  };
  try { rec.start(); voice.listening = true; $('voice-btn').classList.add('recording'); }
  catch (_) { toast(t("无法启动语音识别，请检查麦克风")); }
}
function voiceStopAndFlush() {
  clearTimeout(voice.timer);
  voiceReleaseMicStream();
  voice.listening = false;
  if (voice.recognition) { try { voice.recognition.onend = null; voice.recognition.stop(); } catch (_) {} }
  $('voice-btn').classList.remove('recording');
  voice.interim = '';
  const rest = voice.buffer.trim();
  voice.buffer = '';
  if (rest.length >= 2) { voice.queue.push(rest); voiceDrainQueue(); }
  // 停止即收起：自动隐藏小秘面板（剩余句子仍会异步发送），下次点麦克风重新唤起
  $('voice-panel').classList.add('hidden');
}
function voiceClose() {
  clearTimeout(voice.timer);
  voiceReleaseMicStream();
  ttsCancel();
  voice.listening = false; voice.standby = false;
  if (voice.recognition) { try { voice.recognition.onend = null; voice.recognition.abort(); } catch (_) {} }
  $('voice-btn').classList.remove('recording');
  $('voice-panel').classList.add('hidden');
}

// 选定麦克风：getUserMedia 以 exact deviceId 建立并持有音频流，把音频路由锁定到该设备
async function voiceOpenMicStream() {
  voiceReleaseMicStream();
  const dev = state.config && state.config.voiceInputDevice;
  if (!dev || !navigator.mediaDevices || !navigator.mediaDevices.getUserMedia) return;
  try {
    voice.micStream = await navigator.mediaDevices.getUserMedia({ audio: { deviceId: { exact: dev } } });
  } catch (_) {
    voice.micStream = null;
    toast(t('无法使用所选麦克风，将使用系统默认'));
  }
}
function voiceReleaseMicStream() {
  if (voice.micStream) { try { voice.micStream.getTracks().forEach(tk => tk.stop()); } catch (_) {} voice.micStream = null; }
}
$('voice-btn').onclick = action(() => { voice.listening ? voiceStopAndFlush() : voiceStart(); });
$('voice-stop').onclick = action(voiceStopAndFlush);

// 设置面板：语音小秘名字输入
function renderVoiceNameControl() {
  const wrap = el('div', 'settings-control');
  const head = el('div', 'control-label');
  head.append(el('span', '', t('小秘名字')));
  const input = el('input');
  input.type = 'text';
  input.maxLength = 12;
  input.placeholder = '小秘';
  input.value = (state.config && state.config.voiceAssistantName) || '小秘';
  input.onkeydown = (e) => { if (e.key === 'Enter') { e.preventDefault(); saveBtn.click(); } };
  const saveBtn = el('button', 'primary', t('保存'));
  saveBtn.type = 'button';
  saveBtn.onclick = action(async () => {
    const name = input.value.trim() || '小秘';
    await api('/settings', { method: 'PUT', body: JSON.stringify({ voiceAssistantName: name, activeModel: state.config ? state.config.activeModel : '' }) });
    await refreshConfig();
    toast(t('小秘名字已保存'));
  });
  const row = el('div', 'voice-name-row');
  row.append(input, saveBtn);
  wrap.append(head, row, el('small', '', t('语音弹框标题使用这个名字，默认「小秘」。')));
  return wrap;
}
function renderVoiceHistoryControl() {
  const wrap = el('div', 'settings-control');
  const head = el('div', 'control-label');
  head.append(el('span', '', t('小秘对话历史')));
  const list = el('div', 'voice-history-list');
  async function load() {
    list.replaceChildren();
    list.append(el('p', 'muted', t('加载中…')));
    let res;
    try { res = await api('/voice-history'); }
    catch (e) { list.replaceChildren(el('p', 'muted', e.message)); return; }
    list.replaceChildren();

    // 隐私二次校验：即使系统已解锁，查看加密小秘历史也要再输一次账户密码
    if (res.encrypted && !list._authPassed && state.config && state.config.hasPassword) {
      list.append(el('p', 'muted', t('查看小秘对话历史需再次输入账户密码确认身份。')));
      const pw = el('input'); pw.type = 'password'; pw.placeholder = t('账户密码');
      const go = el('button', 'primary', t('查看历史')); go.type = 'button';
      go.onclick = action(async () => {
        if (!pw.value) return toast(t('请输入密码'));
        try {
          await api('/account/verify-password', { method: 'POST', body: JSON.stringify({ password: pw.value }) });
          try { await api('/voice-history/unlock', { method: 'POST', body: JSON.stringify({ password: pw.value }) }); } catch (_) {}
          list._authPassed = true;
          load();
        } catch (e) { toast(t('密码错误')); }
      });
      const row = el('div', 'voice-lock-row'); row.append(pw, go);
      list.append(row);
      return;
    }

    // 加密但未解锁：只显示锁定占位 + 解锁
    if (res.encrypted && !res.unlocked) {
      list.append(el('p', 'muted', t('历史已加密，需输入密钥解锁。密钥丢失无法恢复，只能清空重置。')));
      const pw = el('input'); pw.type = 'password'; pw.placeholder = t('密钥');
      const un = el('button', 'primary', t('解锁')); un.type = 'button';
      un.onclick = action(async () => {
        if (!pw.value) return toast(t('请输入密钥'));
        try { await api('/voice-history/unlock', { method: 'POST', body: JSON.stringify({ password: pw.value }) }); toast(t('已解锁')); load(); }
        catch (e) { toast(t('密钥错误')); }
      });
      const row = el('div', 'voice-lock-row'); row.append(pw, un);
      list.append(row);
      return;
    }

    // 未加密：提供启用加密
    if (!res.encrypted) {
      list.append(el('p', 'muted', t('历史当前为明文。可启用 AES-256-GCM 加密，密钥只留内存、不落盘。')));
      const pw0 = el('input'); pw0.type = 'password'; pw0.placeholder = t('设置加密密钥');
      const en = el('button', 'quiet', t('启用加密')); en.type = 'button';
      en.onclick = action(async () => {
        if (!pw0.value) return toast(t('请设置密钥'));
        await api('/voice-history/enable', { method: 'POST', body: JSON.stringify({ password: pw0.value }) });
        toast(t('已启用加密')); load();
      });
      const row = el('div', 'voice-lock-row'); row.append(pw0, en);
      list.append(row);
    }

    // 工具条
    const tool = el('div', 'voice-history-bar');
    const refreshBtn = el('button', 'quiet', t('刷新')); refreshBtn.type = 'button';
    refreshBtn.onclick = action(load);
    tool.append(refreshBtn);
    if (res.encrypted) {
      const lockBtn = el('button', 'quiet', t('锁定')); lockBtn.type = 'button';
      lockBtn.onclick = action(async () => { await api('/voice-history/lock', { method: 'POST' }); toast(t('已锁定')); load(); });
      const chgBtn = el('button', 'quiet', t('修改密钥')); chgBtn.type = 'button';
      chgBtn.onclick = action(async () => {
        const oldPw = prompt(t('原密钥')); if (oldPw == null) return;
        const newPw = prompt(t('新密钥')); if (!newPw) return toast(t('请输入新密钥'));
        try { await api('/voice-history/change-password', { method: 'POST', body: JSON.stringify({ oldPassword: oldPw, newPassword: newPw }) }); toast(t('密钥已修改')); }
        catch (e) { toast(t('修改失败：' + e.message)); }
      });
      const disBtn = el('button', 'quiet', t('关闭加密')); disBtn.type = 'button';
      disBtn.onclick = action(async () => {
        const pw = prompt(t('输入密钥以解密回明文')); if (pw == null) return;
        try { await api('/voice-history/disable', { method: 'POST', body: JSON.stringify({ password: pw }) }); toast(t('已关闭加密')); load(); }
        catch (e) { toast(t('失败：' + e.message)); }
      });
      tool.append(lockBtn, chgBtn, disBtn);
    }
    const clearBtn = el('button', 'quiet', t('清空')); clearBtn.type = 'button';
    clearBtn.onclick = action(async () => {
      if (!confirm(t('确定清空小秘的全部历史记录？'))) return;
      await api('/voice-history', { method: 'DELETE' }); toast(t('已清空')); load();
    });
    tool.append(clearBtn);
    list.append(tool);

    const items = res.history || [];
    if (!items.length) { list.append(el('p', 'muted', t('暂无记录。点麦克风说话后，小秘的判断会记录在这里。'))); return; }
    for (const it of items) {
      const row = el('div', 'voice-history-item');
      const meta = el('div', 'vh-meta');
      const label = it.action === 'send' ? t('已发送') : it.action === 'standby' ? t('退下') : it.action === 'ask' ? t('追问') : t('忽略');
      const tag = el('span', 'vh-tag ' + (it.action === 'send' ? 'is-sent' : it.action === 'standby' ? 'is-standby' : 'is-ignored'), label);
      meta.append(tag, el('span', 'vh-time', it.time || ''));
      row.append(meta, el('div', 'vh-heard', t('听到：') + (it.heard || '')));
      if (it.action === 'ask' && it.ask) row.append(el('div', 'vh-text', t('追问：') + it.ask));
      else if (it.text) row.append(el('div', 'vh-text', t('总结发送：') + it.text));
      if (it.reason) row.append(el('div', 'vh-reason', it.reason));
      list.append(row);
    }
  }
  wrap.append(head, list, el('small', '', t('小秘听到了什么、如何判断、发送了什么，按时间线记录；重启后仍保留。')));
  load();
  return wrap;
}
controlRenderers['voice-history'] = renderVoiceHistoryControl;
function renderVoiceReplyControl() {
  const wrap = el('div', 'settings-control');
  const head = el('div', 'control-label');
  head.append(el('span', '', t('语音回复')));
  const row = el('div', 'voice-reply-row');
  const toggle = el('input'); toggle.type = 'checkbox';
  toggle.checked = !!(state.config && state.config.voiceReplyEnabled);
  const gender = el('select');
  const optF = el('option', '', t('女声')); optF.value = 'female';
  const optM = el('option', '', t('男声')); optM.value = 'male';
  gender.append(optF, optM);
  gender.value = (state.config && state.config.voiceReplyGender) || 'female';
  const save = el('button', 'primary', t('保存'));
  save.type = 'button';
  save.onclick = action(async () => {
    await api('/settings', { method: 'PUT', body: JSON.stringify({ voiceReplyEnabled: toggle.checked, voiceReplyGender: gender.value, activeModel: state.config ? state.config.activeModel : '' }) });
    await refreshConfig();
    if (toggle.checked && !('speechSynthesis' in window)) toast(t('当前浏览器不支持语音朗读'));
    else toast(t('语音回复设置已保存'));
  });
  row.append(toggle, el('span', '', t('语音回复')), gender, save);
  wrap.append(head, row, el('small', '', t('开启后：口述总结的意图以打字机效果填入输入框并自动发送；模型回复会被朗读（音色可选男女）。')));
  return wrap;
}
controlRenderers['voice-reply'] = renderVoiceReplyControl;
// 设置：小秘麦克风输入源选择
function renderVoiceInputSourceControl() {
  const wrap = el('div', 'settings-control');
  const head = el('div', 'control-label'); head.append(el('span', '', t('麦克风输入源')));
  const row = el('div', 'voice-input-row');
  const sel = el('select', 'voice-input-select');
  const refresh = el('button', 'quiet', t('刷新')); refresh.type = 'button';
  row.append(sel, refresh);
  const note = el('small', '', t('默认用系统麦克风。Web Speech 标准不直接支持指定设备，这里通过 getUserMedia 锁定该设备的音频路由。'));
  wrap.append(head, row, note);
  async function populate() {
    sel.replaceChildren();
    const def = el('option'); def.value = ''; def.textContent = t('系统默认'); sel.append(def);
    try {
      let mics = (await navigator.mediaDevices.enumerateDevices()).filter(d => d.kind === 'audioinput');
      if (mics.length && !mics.some(d => d.label)) {
        try { const st = await navigator.mediaDevices.getUserMedia({ audio: true }); st.getTracks().forEach(tk => tk.stop());
          mics = (await navigator.mediaDevices.enumerateDevices()).filter(d => d.kind === 'audioinput'); } catch (_) {}
      }
      mics.forEach((d, i) => { const o = el('option'); o.value = d.deviceId; o.textContent = d.label || (t('麦克风') + (i + 1)); sel.append(o); });
    } catch (_) {}
    sel.value = (state.config && state.config.voiceInputDevice) || '';
  }
  sel.onchange = action(async () => {
    await api('/settings', { method: 'PUT', body: JSON.stringify({ voiceInputDevice: sel.value, activeModel: state.config ? state.config.activeModel : '' }) });
    await refreshConfig();
    toast(sel.value ? t('已选择麦克风，下次语音生效') : t('已切回系统默认麦克风'));
  });
  refresh.onclick = action(populate);
  populate();
  return wrap;
}
controlRenderers['voice-input-source'] = renderVoiceInputSourceControl;
controlRenderers['voice-name'] = renderVoiceNameControl;

// 设置面板：人格切换（aide 工作 / 小秘 生活）
function renderPersonaSwitchControl() {
  const wrap = el('div', 'settings-control');
  const head = el('div', 'control-label');
  head.append(el('span', '', t('活动人格')));
  const list = el('div', 'persona-switch-list');
  const personas = (state.config && state.config.personas) || [];
  const activeId = (state.config && state.config.activePersona) || 'aide';
  const cards = [];
  for (const p of personas) {
    const card = el('button', 'persona-card' + (p.id === activeId ? ' active' : ''));
    card.type = 'button';
    const nm = el('strong', '', p.name);
    const sub = el('small', '', p.role === 'life' ? t('生活向 · 私人秘书') : t('工作向 · 开发助手'));
    card.append(nm, sub);
    card.onclick = action(async () => {
      await api('/personas/active', { method: 'POST', body: JSON.stringify({ id: p.id }) });
      await refreshConfig();
      cards.forEach(c => c.classList.toggle('active', c.dataset.pid === p.id));
      const greet = p.role === 'life'
        ? t('已切到 {0}，有什么生活上的事想聊聊、记下或提醒吗？', p.name)
        : t('已切回 {0}，专注工作。', p.name);
      toast(greet);
      speakReply(greet);
    });
    card.dataset.pid = p.id;
    cards.push(card);
    list.append(card);
  }
  if (!personas.length) list.append(el('p', 'muted', t('加载中…')));
  wrap.append(head, list, el('small', '', t('主会话默认 aide 工作人格；语音听写与锁屏解锁后自动进入小秘生活人格。')));
  return wrap;
}
controlRenderers['persona-switch'] = renderPersonaSwitchControl;

// 设置：可演化性格系统（aide 基本聊天 / 小秘语音页），开关 + 提示词 + 保存/重置/演化
function renderPersonalityControl(control) {
  const pid = control.persona || 'aide';
  const wrap = el('div', 'settings-control personality-panel');
  const head = el('div', 'control-label');
  head.append(el('span', '', t(control.label || (pid === 'xiaomi' ? '小秘性格系统' : 'aide 性格系统'))));

  const top = el('div', 'personality-top');
  const toggle = el('label', 'switch');
  const cb = el('input'); cb.type = 'checkbox';
  toggle.append(cb, el('span', 'switch-slider'));
  const stateLbl = el('span', 'personality-state', t('已关闭'));
  top.append(toggle, stateLbl);

  const ta = el('textarea', 'personality-prompt');
  ta.rows = 8;
  ta.placeholder = t('性格提示词：定义语气、风格与做事方式…');

  const btns = el('div', 'personality-btns');
  const mk = (cls, txt) => { const b = el('button', cls, txt); b.type = 'button'; return b; };
  const saveBtn = mk('primary', t('保存'));
  const resetBtn = mk('quiet', t('重置默认'));
  const evolveBtn = mk('quiet', t('立即演化'));
  btns.append(saveBtn, resetBtn, evolveBtn);

  const meta = el('div', 'personality-meta');
  wrap.append(head, top, ta, btns, meta);

  const renderMeta = (p) => {
    meta.replaceChildren();
    meta.append(el('span', '', t('已演化 {0} 次 · 约 {1} tokens', p.evolutions ?? 0, p.estTokens ?? 0)));
    if (p.updatedAt) meta.append(el('span', '', ' · ' + new Date(p.updatedAt).toLocaleString()));
  };
  const apply = (p) => {
    cb.checked = !!p.enabled; ta.value = p.prompt || '';
    stateLbl.textContent = p.enabled ? t('已启用') : t('已关闭');
    renderMeta(p);
  };

  api('/personality?id=' + pid).then(apply).catch((e) => toast(e.message || String(e)));

  cb.onchange = action(async () => {
    const p = await api('/personality', { method: 'PUT', body: JSON.stringify({ id: pid, enabled: cb.checked, prompt: ta.value }) });
    stateLbl.textContent = p.enabled ? t('已启用') : t('已关闭');
    toast(p.enabled ? t('性格已启用') : t('性格已关闭'));
  });
  saveBtn.onclick = action(async () => {
    const p = await api('/personality', { method: 'PUT', body: JSON.stringify({ id: pid, enabled: cb.checked, prompt: ta.value }) });
    apply({ ...p, estTokens: Math.ceil((p.prompt || '').length / 4), evolutions: p.evolutions });
    toast(t('已保存'));
  });
  resetBtn.onclick = action(async () => {
    if (!confirm(t('确定重置为默认性格？自定义与演化结果会被清除。'))) return;
    const p = await api('/personality/reset', { method: 'POST', body: JSON.stringify({ id: pid }) });
    apply({ ...p, estTokens: Math.ceil((p.prompt || '').length / 4), evolutions: 0 });
    toast(t('已重置'));
  });
  evolveBtn.onclick = action(async () => {
    evolveBtn.disabled = true; const orig = evolveBtn.textContent; evolveBtn.textContent = t('演化中…');
    try {
      const p = await api('/personality/evolve', { method: 'POST', body: JSON.stringify({ id: pid }) });
      apply({ ...p, estTokens: Math.ceil((p.prompt || '').length / 4) });
      toast(t('性格已精简演化'));
    } catch (e) { toast(e.message || String(e)); }
    finally { evolveBtn.disabled = false; evolveBtn.textContent = orig; }
  });
  return wrap;
}
controlRenderers['personality'] = renderPersonalityControl;


/* ── 账户锁屏：空闲糊化遮罩（纯视觉层，不停止后端任务；小秘暂停听写/朗读） ── */
const lockScreen = { timer: null, locked: false, wasVoiceListening: false };
function lockTimeoutActive() {
  return !!(state.config && state.config.hasPassword && (state.config.lockTimeoutSec || 0) > 0);
}
function resetIdleTimer() {
  clearTimeout(lockScreen.timer);
  lockScreen.timer = null;
  if (lockScreen.locked) return;
  if (!lockTimeoutActive()) return;
  lockScreen.timer = setTimeout(lockScreenNow, (state.config.lockTimeoutSec || 0) * 1000);
}
function refreshLockStatus() {
  const host = $('lock-status');
  if (!host) return;
  let running = null;
  for (const rid in state.runPhase) {
    const ph = state.runPhase[rid];
    if (ph && !ph.done) { running = ph; break; }
  }
  host.textContent = running
    ? t('运行中 · ') + phaseLabel(running) + ' · ' + formatElapsed(running.startedAt)
    : t('空闲 · 后台任务不受锁屏影响');
}
function lockScreenNow() {
  if (lockScreen.locked) return;
  if (!state.config || !state.config.hasPassword) return;
  lockScreen.locked = true;
  clearTimeout(lockScreen.timer); lockScreen.timer = null;
  // 小秘退下：停止听写 + 取消朗读（解锁后按原状态恢复）
  lockScreen.wasVoiceListening = !!voice.listening;
  if (voice.listening) voiceClose();
  ttsCancel();
  $('lock-screen').hidden = false;
  $('lock-password').value = '';
  $('lock-error').textContent = '';
  refreshLockStatus();
  setTimeout(() => { try { $('lock-password').focus(); } catch (_) {} }, 60);
}
function unlockScreen(pw) {
  return api('/account/verify-password', { method: 'POST', body: JSON.stringify({ password: pw }) }).then(() => {
    lockScreen.locked = false;
    $('lock-screen').hidden = true;
    const name = (state.config && state.config.userName) || '';
    const xm = (state.config && state.config.voiceAssistantName) || '小秘';
    // 解锁后由小秘人格亲切欢迎
    const welcome = name ? t('欢迎回来，{0}，我是{1}。', name, xm) : t('欢迎回来，我是{0}。', xm);
    toast(welcome);
    speakReply(welcome); // 内部按 voiceReplyEnabled 判断是否朗读
    if (lockScreen.wasVoiceListening) { lockScreen.wasVoiceListening = false; voiceStart(); }
    resetIdleTimer();
  });
}
$('lock-form').onsubmit = action(async e => {
  e.preventDefault();
  try {
    await unlockScreen($('lock-password').value);
    $('lock-password').value = '';
  } catch (err) {
    $('lock-error').textContent = t('密码错误');
    try { $('lock-password').select(); } catch (_) {}
  }
});
['mousemove', 'keydown', 'click', 'scroll', 'touchstart'].forEach(ev =>
  window.addEventListener(ev, resetIdleTimer, { passive: true }));
setInterval(() => { if (lockScreen.locked) refreshLockStatus(); }, 1000);

// 设置面板「账户」：用户名 / 锁屏密码 / 锁屏时间 / 立即锁屏
function renderAccountControl() {
  const wrap = el('div', 'settings-control account-control');
  const cfg = state.config || {};
  const hasPw = !!cfg.hasPassword;
  // Docker 环境块手动锁屏蒙版：仅已设密码时启用
  const rc = $('runtime-card');
  if (rc) { rc.classList.toggle('lock-enabled', hasPw); }
  const uRow = el('div', 'account-row');
  uRow.append(el('span', '', t('用户名')));
  const uInput = el('input'); uInput.type = 'text'; uInput.maxLength = 24; uInput.placeholder = t('可选，用于欢迎语'); uInput.value = cfg.userName || '';
  uRow.append(uInput);
  const tRow = el('div', 'account-row');
  tRow.append(el('span', '', t('锁屏时间(秒)')));
  const tInput = el('input'); tInput.type = 'number'; tInput.min = 0; tInput.max = 86400; tInput.placeholder = '0'; tInput.value = cfg.lockTimeoutSec || 0;
  tRow.append(tInput);
  const oldRow = el('div', 'account-row');
  oldRow.append(el('span', '', t('原密码')));
  const oldPw = el('input'); oldPw.type = 'password'; oldPw.autocomplete = 'off';
  oldPw.placeholder = hasPw ? t('已设置，改密需输入') : t('未设置'); oldPw.disabled = !hasPw;
  oldRow.append(oldPw);
  const newRow = el('div', 'account-row');
  newRow.append(el('span', '', t('新密码')));
  const newPw = el('input'); newPw.type = 'password'; newPw.autocomplete = 'off';
  newPw.placeholder = hasPw ? t('留空不修改') : t('设置后用于解锁');
  newRow.append(newPw);
  const actions = el('div', 'account-actions');
  const save = el('button', 'primary', t('保存')); save.type = 'button';
  save.onclick = action(async () => {
    const body = { userName: uInput.value.trim(), lockTimeoutSec: parseInt(tInput.value, 10) || 0, activeModel: cfg.activeModel || '' };
    if (newPw.value) {
      body.newPassword = newPw.value;
      if (hasPw) body.oldPassword = oldPw.value;
    }
    await api('/settings', { method: 'PUT', body: JSON.stringify(body) });
    await refreshConfig();
    newPw.value = ''; oldPw.value = '';
    toast(t('账户设置已保存'));
    resetIdleTimer();
  });
  const lockBtn = el('button', 'quiet', t('立即锁屏')); lockBtn.type = 'button';
  lockBtn.onclick = action(lockScreenNow);
  // Docker 环境块蒙版点击锁屏
  const ov = $('runtime-lock-overlay');
  if (ov) { ov.addEventListener('click', (e) => { e.stopPropagation(); if (state.config && state.config.hasPassword) lockScreenNow(); }); }
  actions.append(save, lockBtn);
  wrap.append(uRow, tRow, oldRow, newRow, actions,
    el('small', '', t('不设密码且锁屏时间为 0 时不锁屏。密码同时作为小秘对话历史的 AES-256-GCM 加密密钥，只存哈希、不明文回显。')));
  return wrap;
}
controlRenderers['account'] = renderAccountControl;
