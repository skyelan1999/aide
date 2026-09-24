'use strict';
const t = (key, ...args) => window.aideI18n ? window.aideI18n.t(key, ...args) : String(key).replace(/\{(\d+)\}/g, (m, i) => args[i] ?? m);
const $ = id => document.getElementById(id);
const state = { token: localStorage.getItem('aide-token') || '', session: null, sessionJSON: '', mode: 'chat', root: 'workspace', dir: '.', attachments: [], file: null, busy: false, poll: null, config: null, commandAbort: null, profiles: null, modelDraft: null, plugins: [], panel: 'files', sources: [], source: '', stream: null, live: {}, liveRound: {}, liveTool: {}, streamRetryAt: 0, queueMode: false, autoScroll: true, jumpAnimating: false };
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
function setMode(mode) { state.mode = mode; document.querySelectorAll('.mode-switch button').forEach(b => b.classList.toggle('active', b.dataset.mode === mode)); if (typeof scheduleContextPreview === 'function') scheduleContextPreview(); }
async function refreshConfig() {
  state.config = await api('/config');
  $('connection').textContent = t("● 本地服务已连接"); $('connection').classList.add('ready');
  const versionText = state.config.version ? 'v' + state.config.version : 'dev';
  $('app-version').textContent = versionText;
  $('settings-sheet-version').textContent = ' · aide ' + versionText;
  $('model-status').textContent = state.config.configured ? t("已配置") : t("未配置");
  $('model-name').textContent = state.config.configured ? state.config.model + t(" · API 已配置") : t("先配置模型，即可开始真实 AI 对话");
  estimateContext();
  if (typeof scheduleContextPreview === 'function') scheduleContextPreview();
}
async function loadSessions() {
  const sessions = await api('/sessions'); $('sessions').replaceChildren();
  if (!sessions.length) $('sessions').append(el('p', 'sessions-empty', t("还没有会话。\n从一个想法开始吧。")));
  sessions.forEach(s => {
    const isActive = state.session?.id === s.id;
    // 高亮（蓝点+加粗）只给“完成且未被查看”的会话；查看后由后端 checked 持久化清除
    const highlight = s.status === 'completed' && !s.checked;
    const item = el('div', 'session-item' + (isActive ? ' active' : '') + (s.pinned ? ' pinned' : '') + (highlight ? ' status-completed' : ''));
    item.title = s.title;
    // 状态机：运行中=荧光绿闪烁、等待审批=黄常亮、失败=红常亮、完成=蓝；其余无点。
    // 选中会话由 CSS 转为无色点 + 正常字重（点击检查后加粗与蓝点消失）
    const dotClass = { running: 'dot-running', failed: 'dot-failed', awaiting_approval: 'dot-await', completed: 'dot-done' }[s.status] || '';
    // 完成且已查看：不显示蓝点；运行/审批/失败灯始终显示（点击运行中绿灯不消失）
    if (dotClass && !(s.status === 'completed' && s.checked)) item.append(el('span', 'session-dot ' + dotClass, ''));
    const label = el('span', 'session-label', s.title);
    label.onclick = action(() => selectSession(s.id));
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
      archBtn.onclick = action(async () => { await api(`/sessions/${s.id}`, { method: 'PATCH', body: JSON.stringify({ archived: !s.archived }) }); closeMenu(); await loadSessions(); });
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
    $('sessions').append(item);
  });
  return sessions;
}
const sessionSeq = { value: 0 }; // R07：递增请求序号，旧响应不得覆盖新选择
async function selectSession(id) {
  const seq = ++sessionSeq.value;
  const sameSession = state.session?.id === id;
  clearTimeout(state.poll); closeStream();
  // 查看完成会话：清除“蓝点+加粗”高亮（持久化；不阻塞会话加载，失败静默）
  api(`/sessions/${id}`, { method: 'PATCH', body: JSON.stringify({ check: true }) }).catch(() => {});
  if (!sameSession) {
    // live 文本按 run 归属：切换会话才失效；同会话刷新（排队/插话等）保留流式状态，
    // 避免打断正在流式渲染的回答（closeStream 后 schedulePoll 会重连，live 丢失会造成文本回退）
    state.live = {}; state.liveRound = {}; state.liveTool = {}; state.streamRetryAt = 0;
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
  es.addEventListener('step', () => refreshSessionSoon());
  es.addEventListener('tool', e => {
    let d; try { d = JSON.parse(e.data); } catch (err) { return; }
    state.liveTool[run.id] = d;
    renderLiveTool(run.id);
    refreshSessionSoon();
  });
  es.addEventListener('delta', e => {
    let d; try { d = JSON.parse(e.data); } catch (err) { return; }
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
    delete state.live[run.id]; delete state.liveRound[run.id]; delete state.liveTool[run.id];
    closeStream(); state.streamRetryAt = 0;
    action(async () => {
      const id = state.session?.id; if (!id) return;
      const s = await api('/sessions/' + id); if (state.session?.id !== id) return;
      if (adoptSessionIfChanged(s)) renderSession();
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
let titleSyncTimers = [];
function scheduleTitleSync(id) {
  titleSyncTimers.forEach(clearTimeout); titleSyncTimers = [];
  // 主题总结是后台异步调用，可能在任务完成之后才落库：分两轮补同步（无变化时不会重渲染）
  for (const delay of [1500, 4000]) {
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
  clearTimeout(state.poll); closeStream(); state.live = {}; state.liveRound = {}; state.liveTool = {}; state.streamRetryAt = 0; state.sessionJSON = ''; state.session = null; state.attachments = []; renderAttachments(); renderSession(); await loadSessions(); $('prompt').focus(); if (typeof hideContextPreview === 'function') hideContextPreview();
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
const statuses = { running: '运行中', completed: '已完成', failed: '失败', cancelled: '已停止', interrupted: '已中断', awaiting_approval: '等待应用' };
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
    if (run.attachments?.length) box.append(el('p', 'muted', t("已附加：") + run.attachments.map(a => a.root + '/' + a.path).join('、')));
    if (!run.steps.length) box.append(el('p', 'muted', t("正在准备模型请求…")));
    run.steps.forEach((step, index) => {
      if (run.mode === 'chat') {
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
        } else {
          ans.textContent = t("未返回回答");
        }
        box.append(ans);
        if (running && state.liveTool[run.id]) {
          const t2 = state.liveTool[run.id];
          box.append(el('div', 'live-tool', '⚒ ' + t2.tool + (t2.preview ? ' · ' + String(t2.preview).slice(0, 80) : '')));
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
  files.forEach(file => { const b = el('button', 'file-item'); b.append(el('span', 'file-icon', file.dir ? '▱' : '≡'), el('span', 'file-name', file.name)); if (file.dir) b.append(el('small', '', '›')); b.title = file.path; b.onclick = action(async () => { if (file.dir) { state.dir = file.path; await loadFiles(); } else await openFile(file.path); }); $('files').append(b); });
}
async function openFile(path) {
  const query = state.root === 'context' && state.source ? '/file?source=' + encodeURIComponent(state.source) + '&path=' : '/file?root=' + state.root + '&path=';
  const data = await api(query + encodeURIComponent(path)); state.file = { ...data, path, root: state.root, source: state.root === 'context' ? state.source : '', wsId: data.workspaceId || data.wsId || '', fresh: false }; showEditor();
}
function sourceIsRW() {
  if (state.file.root !== 'context' || !state.file.source) return false;
  return state.sources.find(x => x.id === state.file.source)?.rw === true;
}
function isMarkdownPath(path) { return /\.(md|markdown)$/i.test(path || ''); }
function setEditorMode(mode) {
  const preview = mode === 'preview';
  $('editor').classList.toggle('hidden', preview);
  $('editor-preview').classList.toggle('hidden', !preview);
  $('editor-mode-edit').classList.toggle('active', !preview);
  $('editor-mode-preview').classList.toggle('active', preview);
  if (preview) { $('editor-preview').innerHTML = renderMarkdown($('editor').value); $('editor-preview').scrollTop = 0; }
}
function showEditor() {
  $('editor-title').textContent = state.file.path; $('editor').value = state.file.content;
  const readOnly = state.file.root === 'context' && !sourceIsRW();
  $('editor').readOnly = readOnly; $('save-file').disabled = readOnly; $('attach-file').disabled = state.file.fresh;
  $('editor-status').textContent = state.file.root === 'context' ? (sourceIsRW() ? t("辅助资料 · 读写来源") : t("辅助资料 · 只读")) : t("工作目录 · 保存后同步到主机");
  const md = isMarkdownPath(state.file.path);
  $('editor-mode-switch').classList.toggle('hidden', !md);
  setEditorMode(md ? 'preview' : 'edit'); // md 文件打开即渲染预览（含表格）
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
    await api(`/sessions/${target.id}/runs`, { method: 'POST', body: JSON.stringify({ prompt, mode: state.mode, attachments: state.attachments, strategy, profile: strategy === 'auto' ? '' : (state.profiles?.activeProfile || 'default'), queued: state.queueMode }) });
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
    if (chars) detail.append(el('div', 'cp-row', el('span', '', label), el('span', '', chars + t(" 字符 ≈ ") + Math.floor(chars / 4) + ' tokens')));
  });
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

const controlRenderers = { language: renderLanguageControl, 'about-project': renderAboutProject, segmented: renderSegmentedControl, 'profiles-manager': renderProfilesManager, 'token-stats': renderTokenStats, 'sessions-manage': renderSessionsManage };

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
  head.append(exportBtn, refresh);
  const list = el('div', 'archived-list');
  async function renderArchivedList() {
    const items = await api('/sessions?archived=1');
    list.replaceChildren();
    if (!items.length) { list.append(el('p', 'muted', t("没有已归档会话。"))); return; }
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
    del.onclick = () => { if (confirm(t("删除配置「{0}」？", profile.name))) { const index = this.local.profiles.indexOf(profile); if (index >= 0) this.local.profiles.splice(index, 1); this.render(); this.save(); } };
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
  menu.append(left, right);
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
    const row = el('div', 'model-row');
    const radio = el('button', 'model-active' + (m.id === state.modelDraft.activeModel ? ' active' : ''));
    radio.type = 'button';
    radio.title = t("设为当前模型");
    radio.setAttribute('aria-pressed', String(m.id === state.modelDraft.activeModel));
    radio.textContent = m.id === state.modelDraft.activeModel ? '●' : '○';
    radio.onclick = () => { state.modelDraft.activeModel = m.id; renderModelList(); };
    const nameInput = el('input', 'model-name-input');
    nameInput.value = m.name || m.id;
    nameInput.maxLength = 32;
    nameInput.setAttribute('aria-label', t("模型名称"));
    nameInput.addEventListener('input', () => { m.name = nameInput.value.trim() || m.id; });
    const idText = el('span', 'model-id-text', m.id);
    const windowLabel = el('label', 'model-window-label', t("窗口"));
    const windowInput = el('input', 'model-window-input');
    windowInput.type = 'number';
    windowInput.min = 1024;
    windowInput.max = 1048576;
    windowInput.step = 1024;
    windowInput.value = m.contextWindow || 65536;
    windowInput.setAttribute('aria-label', t("上下文窗口"));
    windowInput.addEventListener('input', () => { const v = parseInt(windowInput.value, 10); if (!Number.isNaN(v)) m.contextWindow = v; });
    windowLabel.append(windowInput);
    const del = el('button', 'model-delete', '－');
    del.type = 'button';
    del.title = t("删除模型");
    del.onclick = () => {
      const index = state.modelDraft.models.indexOf(m);
      if (index >= 0) state.modelDraft.models.splice(index, 1);
      if (state.modelDraft.activeModel === m.id) state.modelDraft.activeModel = state.modelDraft.models[0]?.id || '';
      renderModelList();
    };
    row.append(radio, nameInput, idText, windowLabel, del);
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
function renderMarkdown(src, live) {
  if (window.marked && typeof window.marked.parse === 'function') {
    const html = window.marked.parse(String(src || ''), { gfm: true, breaks: false });
    const body = sanitizeHtml(html);
    // live（流式渲染）跳过代码高亮：每帧全量高亮代价高，完成态由 renderSession 补全
    if (!live) body.querySelectorAll('pre > code[class*="language-"]').forEach(codeEl => {
      const lang = (codeEl.className.match(/language-([\w+-]+)/) || [])[1] || '';
      if (/^(js|javascript|jsx|ts|typescript|mjs)$/i.test(lang)) codeEl.innerHTML = highlightCode(codeEl.textContent, lang);
    });
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
  if (preview) { $('file-view-preview').innerHTML = renderMarkdown($('file-view-editor').value); $('file-view-preview').scrollTop = 0; }
}
async function openFileViewMode() {
  let spec = null;
  try { spec = JSON.parse(decodeURIComponent(new URLSearchParams(location.hash.slice(1)).get('file') || '')); } catch (e) { spec = null; }
  if (!spec) return;
  document.body.classList.add('file-view-mode');
  $('file-view').classList.remove('hidden');
  fileView.spec = spec; fileView.wsId = '';
  $('file-view-path').textContent = (spec.source ? 'sources/' + spec.source : spec.root) + ' · ' + spec.path;
  const query = spec.source
    ? '/file?source=' + encodeURIComponent(spec.source) + '&path=' + encodeURIComponent(spec.path)
    : '/file?root=' + encodeURIComponent(spec.root) + '&path=' + encodeURIComponent(spec.path);
  const data = await api(query);
  fileView.hash = data.hash; fileView.wsId = data.workspaceId || data.wsId || '';
  const md = isMarkdownPath(spec.path);
  $('file-view-mode-switch').classList.toggle('hidden', !md);
  const readOnly = spec.root !== 'workspace' && !(spec.source && state.sources.find(x => x.id === spec.source)?.rw === true);
  $('file-view-editor').value = data.content;
  $('file-view-editor').readOnly = readOnly;
  $('file-view-save').disabled = readOnly;
  $('file-view-status').textContent = readOnly ? t("只读") : t("可编辑 · 保存后同步");
  setFileViewMode(md ? 'preview' : 'edit'); // md 默认渲染预览
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
function renderTrajectory() {
  const host = $('trajectory-content');
  host.replaceChildren();
  const session = state.session;
  if (!session || !session.runs?.length) {
    host.append(el('p', 'muted', t("当前会话还没有任务。发送任务后，这里会按事件时间线记录完整轨迹。")));
    return;
  }
  for (const run of session.runs) {
    const card = el('div', 'traj-run');
    const head = el('div', 'traj-run-head');
    head.append(el('span', 'traj-run-time', (run.created || '').replace('T', ' ').slice(0, 16)));
    const meta = [];
    meta.push(run.mode === 'workflow' ? t("工作流") : t("对话"));
    if (run.strategy) meta.push(run.strategy === 'auto' ? t("自动路由 → ") + profileName(run.profile) : t("手动 · ") + profileName(run.profile));
    if (run.model) meta.push(run.model);
    meta.push(t(statuses[run.status] || run.status));
    if (run.usage?.total) meta.push(fmtStatTokens(run.usage.total) + ' tokens' + (run.usage.estimated ? t("（估）") : ''));
    head.append(el('span', 'traj-run-meta', meta.join(' · ')));
    card.append(head);
    card.append(trajectoryEvent('💬', t("用户任务"), el('div', 'traj-body', run.prompt)));
    run.steps?.forEach(step => {
      const body = el('div', 'traj-body md-body');
      if (step.name === 'propose') { const pre = el('pre', 'traj-pre', step.content || ''); body.append(pre); }
      else body.innerHTML = renderMarkdown(step.content || t("（无内容）"));
      card.append(trajectoryEvent('◈', t(labels[step.name]) || step.name + ' · ' + t(statuses[step.status]), body));
    });
    run.toolUses?.forEach(use => {
      let argsBrief = '';
      try { const a = JSON.parse(use.args || '{}'); const v = Object.values(a)[0]; if (typeof v === 'string') argsBrief = ' · ' + v.slice(0, 40); } catch (e) { /* 忽略 */ }
      const body = el('div', 'traj-body');
      body.append(el('p', '', t("参数：") + (use.args || t("无"))), el('pre', 'traj-pre', use.preview || use.result || t("（无结果）")));
      card.append(trajectoryEvent('⚒', use.tool + argsBrief, body, 'tool'));
    });
    if (run.files?.length) {
      const body = el('div', 'traj-body');
      run.files.forEach(f => body.append(el('p', '', (f.applied ? t("✓ 已应用 ") : t("→ 提案 ")) + f.path)));
      card.append(trajectoryEvent('📝', t("文件提案 · ") + run.files.length + t(" 个"), body));
    }
    if (run.commands?.length) {
      const body = el('div', 'traj-body');
      run.commands.forEach(c => body.append(el('pre', 'traj-pre', c)));
      card.append(trajectoryEvent('❯', t("建议命令 · ") + run.commands.length + t(" 条（未运行）"), body));
    }
    if (run.error) card.append(trajectoryEvent('✖', t("错误"), el('div', 'traj-body task-error', run.error), 'error'));
    host.append(card);
  }
}
function openTrajectory() {
  closeSettingsSheet();
  closeWorkspaceSheet();
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
      row.append(el('strong', '', res.title), el('span', '', res.snippet));
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
document.addEventListener('click', event => { if (!event.target.closest('.global-search')) $('search-results').classList.add('hidden'); });
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
  if (!document.body.classList.contains('file-view-mode') && state.config) { action(loadSessions)(); action(loadFiles)(); }
  if (fileView.spec) $('file-view-status').textContent = $('file-view-editor').readOnly ? t('只读') : t('可编辑 · 保存后同步');
  if (!$('strategy-menu').classList.contains('hidden')) openStrategyMenu();
  if (state.contextPreview) renderContextPreview(state.contextPreview);
});
