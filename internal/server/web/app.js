'use strict';
const t = (key, ...args) => window.aideI18n ? window.aideI18n.t(key, ...args) : String(key).replace(/\{(\d+)\}/g, (m, i) => args[i] ?? m);
const $ = id => document.getElementById(id);
function updateFavicon() {
  const icon = $('app-favicon');
  if (!icon) return;
  const styles = getComputedStyle(document.documentElement);
  const brand = styles.getPropertyValue('--brand').trim() || '#007aff';
  const foreground = styles.getPropertyValue('--on-brand').trim() || '#ffffff';
  const svg = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 64 64"><rect width="64" height="64" rx="15" fill="${brand}"/><text x="32" y="49" text-anchor="middle" font-family="Inter,-apple-system,BlinkMacSystemFont,Segoe UI,sans-serif" font-size="53" font-weight="800" fill="${foreground}">a</text></svg>`;
  icon.href = 'data:image/svg+xml;charset=utf-8,' + encodeURIComponent(svg);
}
updateFavicon();
if (window.aideUI?.subscribe) window.aideUI.subscribe(updateFavicon);
const state = { token: localStorage.getItem('aide-token') || '', session: null, sessionJSON: '', historyLimit: 30, historyScroll: false, pendingSessionId: '', submitting: false, mode: 'chat', root: 'workspace', dir: '.', fileDirs: {}, fileEntries: [], fileSelection: new Set(), fileSelectionLocation: '', fileSelectionAnchor: -1, fileSearch: '', fileSearchScope: 'folder', fileSearchMatch: 'fuzzy', commandHistory: [], commandHistoryIndex: 0, commandHistoryDraft: '', attachments: [], file: null, busy: false, poll: null, config: null, commandAbort: null, profiles: null, modelDraft: null, plugins: [], panel: 'files', sources: [], source: '', stream: null, live: {}, liveStable: {}, liveTool: {}, liveReasoning: {}, runPhase: {}, streamRetryAt: 0, queueMode: false, autoScroll: true, jumpAnimating: false };
const fragment = new URLSearchParams(location.hash.slice(1));
state.liveRound = {}; // Keep per-run streaming rounds initialized on the first session.
// 文件面板的上传/搜索控件保持为脚本生成，避免与嵌入式页面模板的单行结构耦合。
// 它们在后续的事件绑定和首次 loadFiles 前已经存在。
(() => {
  const tools = document.querySelector('.file-tools');
  if (!tools) return;
  tools.insertAdjacentHTML('afterend', '<div class="file-filter"><div class="file-search-box"><input id="file-search" type="search" autocomplete="off" placeholder="搜索文件" aria-label="搜索文件" data-i18n-placeholder="搜索文件" data-i18n-aria-label="搜索文件"><button type="button" id="file-search-scope" aria-pressed="false" title="包含子文件夹" aria-label="包含子文件夹" data-i18n-title="包含子文件夹" data-i18n-aria-label="包含子文件夹">↳</button><button type="button" id="file-search-match" aria-pressed="false" title="精确匹配" aria-label="精确匹配" data-i18n-title="精确匹配" data-i18n-aria-label="精确匹配">＝</button></div></div>');
  const list = $('files');
  list.tabIndex = 0;
  list.setAttribute('aria-label', '文件列表');
  list.dataset.i18nAriaLabel = '文件列表';
})();
// 纯前端「忽略此提案」集合（无后端拒绝端点）：仅在内存折叠卡片，不写盘、不影响会话
const ignoredProposals = new Set();
if (fragment.has('token')) { state.token = fragment.get('token'); localStorage.setItem('aide-token', state.token); history.replaceState(null, '', location.pathname); }
function el(tag, cls, text) { const e = document.createElement(tag); if (cls) e.className = cls; if (text != null) e.textContent = text; return e; }
function toast(text) { const host = document.querySelector('dialog[open]') || document.body; host.append($('toast')); $('toast').textContent = text; $('toast').classList.remove('hidden'); clearTimeout(toast.timer); toast.timer = setTimeout(() => $('toast').classList.add('hidden'), 5000); }
// passwordPrompt(title) 通用密文密码弹窗：替代浏览器原生 prompt（输入时密码在屏幕上明文可见）。
// 用 <dialog> + <input type=password>，回车确认 / Esc 或点遮罩取消。
// 返回 Promise<string|null>：确定为输入串（不 trim，由调用方决定），取消为 null。样式复用全局 dialog/label/button 主题变量。
function passwordPrompt(title) {
  return new Promise(resolve => {
    const dlg = el('dialog', 'pw-prompt');
    const row = el('label', 'pw-prompt-label', title);
    const input = el('input');
    input.type = 'password';
    input.autocomplete = 'off';
    input.spellcheck = false;
    row.append(input);
    dlg.append(row);
    const actions = el('div', 'editor-footer');
    const cancel = el('button', 'quiet', t('取消')); cancel.type = 'button';
    const ok = el('button', 'primary', t('确定')); ok.type = 'button';
    actions.append(cancel, ok);
    dlg.append(actions);
    let settled = false;
    const done = val => { if (settled) return; settled = true; dlg.close(); dlg.remove(); resolve(val); };
    cancel.onclick = () => done(null);
    ok.onclick = () => done(input.value);
    input.addEventListener('keydown', e => {
      if (e.key === 'Enter') { e.preventDefault(); done(input.value); }
      else if (e.key === 'Escape') { e.preventDefault(); done(null); }
    });
    dlg.addEventListener('click', e => { if (e.target === dlg) done(null); }); // 点遮罩取消
    dlg.addEventListener('cancel', () => done(null)); // 原生 Esc
    document.body.append(dlg);
    if (typeof dlg.showModal === 'function') dlg.showModal(); else dlg.setAttribute('open', '');
    input.focus();
  });
}
// requestMasterAuth({reason}) 统一主身份认证弹窗（#43）：密码输入 + 指纹授权按钮。
// 已注册指纹（hasPlatformCredential=true）则按钮可点并自动发起 WebAuthn assertion；未注册置灰。
// 返回 Promise<string|null>：输入密码且验证通过→密码串；指纹通过→'success'；取消→null（失败已 toast/内联提示，不锁死）。
function requestMasterAuth({ reason, assistantSessionId } = {}) {
  return new Promise(resolve => {
    const dlg = el('dialog', 'master-auth');
    const box = el('div', 'master-auth-box');
    box.append(el('div', 'master-auth-title', t('身份验证')));
    if (reason) box.append(el('p', 'muted', reason));
    const input = el('input'); input.type = 'password'; input.autocomplete = 'off'; input.spellcheck = false; input.placeholder = t('输入账户密码');
    const err = el('div', 'master-auth-error', '');
    const actions = el('div', 'editor-footer');
    const cancel = el('button', 'quiet', t('取消')); cancel.type = 'button';
    const fp = el('button', 'master-auth-fp', t('指纹授权')); fp.type = 'button';
    const ok = el('button', 'primary', t('确认')); ok.type = 'button';
    actions.append(cancel, fp, ok);
    box.append(input, err, actions);
    dlg.append(box);
    let settled = false;
    const done = v => { if (settled) return; settled = true; dlg.close(); dlg.remove(); resolve(v); };
    cancel.onclick = () => done(null);
    dlg.addEventListener('cancel', () => done(null));
    dlg.addEventListener('click', e => { if (e.target === dlg) done(null); });
    const doPassword = async () => {
      const pw = input.value;
      if (!pw) { err.textContent = t('请输入密码'); input.focus(); return; }
      try {
        const body = { password: pw };
        if (assistantSessionId) body.assistantSessionId = assistantSessionId;
        await api('/auth/verify', { method: 'POST', body: JSON.stringify(body) });
        done(pw);
      } catch (e) { err.textContent = t('密码错误'); input.value = ''; input.focus(); }
    };
    ok.onclick = () => action(doPassword)();
    input.addEventListener('keydown', e => {
      if (e.key === 'Enter') { e.preventDefault(); action(doPassword)(); }
      else if (e.key === 'Escape') { e.preventDefault(); done(null); }
    });
    const hasCred = !!(state.config && state.config.hasPlatformCredential);
    const waReady = !!(state.config && state.config.webAuthnReady);
    const canFp = hasCred && waReady && location.hostname === 'localhost';
    fp.disabled = !canFp; fp.classList.toggle('disabled', !canFp);
    if (!canFp && !hasCred) fp.title = t('未注册指纹，可在设置 → 账号中绑定');
    fp.onclick = action(async () => {
      fp.disabled = true; err.textContent = '';
      try {
        const start = await api('/webauthn/assertion/start', { method: 'POST', body: '{}' });
        if (!start.allowCredentials || !start.allowCredentials.length) { err.textContent = t('未注册指纹设备'); fp.disabled = false; return; }
        const options = {
          challenge: b64uToBuf(start.challenge), rpId: start.rpId,
          allowCredentials: start.allowCredentials.map(c => ({ type: c.type, id: b64uToBuf(c.id), transports: c.transports })),
          userVerification: start.userVerification || 'preferred', timeout: 120000,
        };
        const assertion = await navigator.credentials.get({ publicKey: options });
        if (!assertion) { fp.disabled = false; return; }
        const body = { challenge: start.challenge, assertion: assertionToJSON(assertion) };
        if (assistantSessionId) body.assistantSessionId = assistantSessionId;
        await api('/auth/verify', { method: 'POST', body: JSON.stringify(body) });
        done('success');
      } catch (e) {
        if (e && e.name === 'NotAllowedError') { fp.disabled = false; return; }
        err.textContent = t('指纹验证失败，请重试'); fp.disabled = false;
      }
    });
    document.body.append(dlg);
    if (typeof dlg.showModal === 'function') dlg.showModal(); else dlg.setAttribute('open', '');
    input.focus();
  });
}
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
  if (typeof updatePhaseHint === 'function') updatePhaseHint();
  if (typeof scheduleContextPreview === 'function') scheduleContextPreview();
}
function syncAssistantModeControls(isAssistantSess) {
  if (isAssistantSess) {
    if (state.mode !== 'chat') setMode('chat');
    state.workflowPhase = '';
    document.querySelectorAll('#workflow-phases .phase-btn').forEach(b => b.classList.remove('selected'));
    $('workflow-phases')?.classList.add('hidden');
    state.queueMode = false;
    $('queue-toggle')?.classList.remove('active');
  }
  const controls = [...document.querySelectorAll('.mode-switch button'), $('queue-toggle')];
  controls.forEach(button => {
    if (!button) return;
    button.disabled = isAssistantSess;
    button.setAttribute('aria-disabled', String(isAssistantSess));
  });
}
// AI 工作流阶段
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
    updatePhaseHint();
  };
});
// 自动编排模式：AI 工作流下未选任何阶段时，由后端 autoModePrompt 处理，前端不显示描述

function updateAutoModeUI() {}

/* ── 问题分析历史报告：读索引、渲染 Markdown，并校验 draw.io 图存在 ── */
const RCA_DIR = '.cache/system-docs/problem-reports';
const RCA_INDEX = RCA_DIR + '/problem-reports-index.md';
function updatePhaseHint() {
  const el = $('phase-hint');
  if (!el) return;
  if (!state.workflowPhase || state.mode !== 'workflow') { el.classList.add('hidden'); el.innerHTML = ''; return; }
  if (state.workflowPhase === 'problem-solving') {
    el.classList.remove('hidden');
    el.innerHTML = '<b>' + t('问题分析阶段') + '</b>：' + t('围绕用户描述的问题收集背景、分析原因并核验证据') + ' → <button type="button" id="rca-reports-btn" class="phase-report-link" title="' + t('查看已保存的问题分析报告') + '">' + t('历史问题报告') + '</button>';
    $('rca-reports-btn').onclick = action(openRcaReports);
  } else { el.classList.add('hidden'); el.innerHTML = ''; }
}
function rcaParseIndex(md) {
  const rows = [];
  (md || '').split('\n').forEach(line => {
    const t = line.trim();
    if (!t.startsWith('|') || /^\|[\s:\-|]+\|$/.test(t) || t.indexOf('编号') >= 0) return;
    const parts = t.replace(/^\||\|$/g, '').split('|').map(c => c.trim());
    if (parts.length < 1 || !/^RCA-\d+/i.test(parts[0])) return;
    rows.push({ id: parts[0], name: parts[1] || parts[0], status: parts[2] || '', created: parts[3] || '', related: parts[4] || '' });
  });
  return rows;
}
function rcaExtractDiagramPath(content) {
  let m = (content || '').match(/关联[：:]\s*draw\.io[：:]\s*([^\s]+)/);
  if (m) return m[1].trim();
  m = (content || '').match(/##\s*RCA\s*图\s*\n+([^\n#]+)/);
  if (m) return m[1].trim();
  return '';
}
function rcaResolveDiagram(ref) {
  ref = (ref || '').trim();
  if (!ref) return null;
  let drawio = ref, exportRef = null;
  if (/\.drawio\.(svg|png)$/i.test(ref)) { drawio = ref.replace(/\.(svg|png)$/i, ''); exportRef = ref; }
  return { drawio, exportRef };
}
async function rcaPathExists(path) {
  try { await api('/file?root=workspace&path=' + encodeURIComponent(path)); return true; }
  catch (e) { return false; }
}
function rcaFriendlyRender(content) {
  let src = String(content || '');
  const trimmed = src.trim();
  if (trimmed.startsWith('{') || trimmed.startsWith('[')) {
    try { return renderMarkdown('```json\n' + JSON.stringify(JSON.parse(trimmed), null, 2) + '\n```', false, ''); }
    catch (e) { /* 非合法 JSON，按 Markdown 原样渲染 */ }
  }
  return renderMarkdown(src, false, '');
}
let rcaState = { entries: [], active: '' };
async function openRcaReports() {
  const dlg = $('rca-dialog');
  if (typeof dlg.showModal === 'function' && !dlg.open) dlg.showModal(); else dlg.setAttribute('open', '');
  await loadRcaList();
}
async function loadRcaList() {
  const listEl = $('rca-report-list');
  listEl.innerHTML = '<div class="rca-empty">' + t('正在加载…') + '</div>';
  let indexRows = [];
  try { const d = await api('/file?root=workspace&path=' + encodeURIComponent(RCA_INDEX)); indexRows = rcaParseIndex(d.content || ''); }
  catch (e) { indexRows = []; }
  let files = [];
  try { files = await api('/files?root=workspace&path=' + encodeURIComponent(RCA_DIR)); }
  catch (e) { files = []; }
  const reportFiles = (files || []).filter(f => !f.dir && /^RCA-.*\.md$/i.test(f.name) && f.name !== 'problem-reports-index.md');
  const byId = {};
  indexRows.forEach(r => { byId[r.id.toUpperCase()] = r; });
  reportFiles.forEach(f => {
    const m = f.name.match(/^(RCA-\d+)/i); if (!m) return;
    const id = m[1].toUpperCase();
    if (!byId[id]) byId[id] = { id, name: f.name.replace(/\.md$/, ''), status: '', created: '', related: '' };
    byId[id].path = f.path;
  });
  rcaState.entries = Object.keys(byId).map(k => byId[k]).filter(e => e.path).sort((a, b) => a.id < b.id ? -1 : (a.id > b.id ? 1 : 0));
  listEl.replaceChildren();
  if (!rcaState.entries.length) {
    listEl.innerHTML = '<div class="rca-empty">' + t('暂无问题分析报告。选择「AI 工作流 → 问题分析」并运行后，可在这里查看。') + '</div>';
    $('rca-view-title').textContent = '—'; $('rca-view-body').innerHTML = '';
    $('rca-open-diagram').classList.add('hidden'); $('rca-warn').classList.add('hidden');
    return;
  }
  rcaState.entries.forEach(e => {
    const b = el('button', 'rca-item' + (rcaState.active === e.path ? ' active' : ''));
    b.type = 'button';
    b.append(el('div', 'rca-item-id', e.id));
    b.append(el('div', 'rca-item-title', e.name || e.id));
    const meta = [e.status, e.created].filter(Boolean).join(' · ');
    if (meta) b.append(el('div', 'rca-item-meta', meta));
    b.onclick = () => openRcaReport(e);
    listEl.append(b);
  });
  if (!rcaState.active || !rcaState.entries.some(e => e.path === rcaState.active)) openRcaReport(rcaState.entries[0]);
}
async function openRcaReport(entry) {
  rcaState.active = entry.path;
  document.querySelectorAll('.rca-item').forEach(b => b.classList.toggle('active', b.textContent.indexOf(entry.id) >= 0));
  $('rca-view-title').textContent = entry.id + (entry.name ? ' · ' + entry.name : '');
  const warn = $('rca-warn'); warn.classList.add('hidden'); warn.innerHTML = '';
  $('rca-open-diagram').classList.add('hidden');
  const body = $('rca-view-body');
  body.innerHTML = '<div class="rca-empty">' + t('正在加载…') + '</div>';
  let data;
  try { data = await api('/file?root=workspace&path=' + encodeURIComponent(entry.path)); }
  catch (e) { body.innerHTML = '<div class="rca-empty">' + t('读取报告失败：{0}', e.message || entry.path) + '</div>'; return; }
  body.innerHTML = rcaFriendlyRender(data.content || '');
  // renderMarkdown 包装层已在渲染后自动触发 renderMermaid，此处无需再调用（renderMermaid 为闭包内定义）
  let ref = rcaExtractDiagramPath(data.content || '') || (entry.related || '').replace(/^draw\.io[：:]\s*/i, '');
  const diag = rcaResolveDiagram(ref);
  const warnings = [];
  if (!diag) {
    warnings.push(t('报告未关联 draw.io 图（缺少 RCA 图 / draw.io 路径）。'));
  } else {
    const ok = await rcaPathExists(diag.drawio);
    if (!ok) {
      warnings.push(t('关联的 draw.io 图不存在：{0}', diag.drawio));
    } else {
      const btn = $('rca-open-diagram');
      btn.classList.remove('hidden');
      btn.onclick = () => openFile(diag.drawio);
      if (diag.exportRef && !(await rcaPathExists(diag.exportRef)))
        warnings.push(t('导出图缺失（{0} 未生成），已改为打开可编辑的 .drawio 源图。', diag.exportRef));
    }
  }
  if (warnings.length) { warn.innerHTML = warnings.join('<br>'); warn.classList.remove('hidden'); }
}
async function refreshConfig() {
  state.config = await api('/config');
  $('connection').textContent = t("● 本地服务已连接"); $('connection').classList.add('ready');
  const versionText = state.config.version ? 'v' + state.config.version : 'dev';
  $('app-version').textContent = versionText;
  $('settings-sheet-version').textContent = ' · aide ' + versionText;
  $('model-status').textContent = state.config.configured ? t("已配置") : t("未配置");
  $('model-name').textContent = state.config.configured ? t("{0} · API 已配置", state.config.model) : t("先配置模型，即可开始真实 AI 对话");
  if (typeof resetIdleTimer === "function") resetIdleTimer();
  // 左下角 Docker 块锁定蒙版：仅已设密码时启用，点击即锁屏（不依赖是否打开过设置面板）
  const rcLock = $('runtime-card');
  if (rcLock) rcLock.classList.toggle('lock-enabled', !!(state.config && state.config.hasPassword));
  const ovLock = $('runtime-lock-overlay');
  if (ovLock && !ovLock.dataset.lockBound) {
    ovLock.dataset.lockBound = '1';
    ovLock.addEventListener('click', (e) => { e.stopPropagation(); if (state.config && state.config.hasPassword) lockScreenNow(); });
  }
  estimateContext();
  if (typeof scheduleContextPreview === 'function') scheduleContextPreview();
}
const subGroupState = {}; // 主会话 id -> { collapsed, expandAll }：已归档子会话折叠组状态，跨 loadSessions 重渲染保留
let assistantEntrySession = null; // #62：小秘单例会话引用（独立侧栏槽位）
function renderAssistantEntry(s) {
  assistantEntrySession = s;
  const entry = $('assistant-entry');
  if (!entry) return;
  entry.dataset.sessionId = s.id;
  entry.dataset.sessionTitle = s.title;
  entry.querySelector('.assistant-entry-name').textContent = (state.config && state.config.assistantName) || t('小秘');
  entry.classList.toggle('active', (state.pendingSessionId || state.session?.id) === s.id);
  entry.classList.toggle('loading', state.pendingSessionId === s.id);
  const running = s.status === 'running';
  entry.querySelector('.assistant-entry-dot').classList.toggle('live', running);
  entry.onclick = action(() => openAssistantGate(s.id, s.title));
}
async function loadSessions() {
  const [sessions, archived] = await Promise.all([api('/sessions'), api('/sessions?archived=1')]);
  $('sessions').replaceChildren();
  // 层级：无 parentId 为主会话；有 parentId 为子会话。active 列表里的子会话=运行中（未归档），archived 列表里的=完成后自动归档。
  const childrenOf = {}, doneChildrenOf = {}, mains = [];
  sessions.forEach(s => {
    if (s.kind === 'assistant') { renderAssistantEntry(s); return; } // 小秘走独立槽位，不进普通列表
    if (s.parentId) (childrenOf[s.parentId] = childrenOf[s.parentId] || []).push(s); else mains.push(s);
  });
  archived.forEach(s => { if (s.parentId) (doneChildrenOf[s.parentId] = doneChildrenOf[s.parentId] || []).push(s); });
  if (!sessions.length) $('sessions').append(el('p', 'sessions-empty', t("还没有会话。\n从一个想法开始吧。")));

  // 单个会话条目（主/子共用）：主会话原样；子会话加 sub-session 缩进类与 ↳ 前缀
  const buildItem = (s, isSub) => {
    const isActive = (state.pendingSessionId || state.session?.id) === s.id;
    // 高亮（蓝点+加粗）只给“完成且未被查看”的会话；查看后由后端 checked 持久化清除
    const highlight = s.status === 'completed' && !s.checked;
    const item = el('div', 'session-item' + (isActive ? ' active' : '') + (s.pinned ? ' pinned' : '') + (highlight ? ' status-completed' : '') + (isSub ? ' sub-session' : ''));
    item.dataset.sessionId = s.id;
    item.dataset.sessionTitle = s.title;
    item.classList.toggle('loading', state.pendingSessionId === s.id);
    item.title = s.title;
    // 状态机：运行中=荧光绿闪烁、等待审批=黄常亮、失败=红常亮、完成=蓝；其余无点。
    const dotClass = { running: 'dot-running', failed: 'dot-failed', awaiting_approval: 'dot-await', completed: 'dot-done' }[s.status] || '';
    // 完成且已查看：不显示蓝点；已自动归档的子会话用灰标签替代蓝点
    if (dotClass && !(s.status === 'completed' && s.checked) && !(isSub && s.autoArchived)) item.append(el('span', 'session-dot ' + dotClass, ''));
    const label = el('span', 'session-label' + (isSub ? ' sub-session-label' : ''));
    label.textContent = s.number > 0 ? '#' + s.number + ' ' + s.title : s.title;
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
      toggle.append(el('span', 'sub-group-title', t("子会话 ({0})", done.length)));
      $('sessions').append(toggle);
      if (!st.collapsed) {
        const shown = st.expandAll ? done : done.slice(0, 3);
        shown.forEach(c => $('sessions').append(buildItem(c, true)));
        if (done.length > 3) {
          const more = el('div', 'sub-group-toggle sub-group-more');
          more.onclick = e => { e.stopPropagation(); st.expandAll = !st.expandAll; loadSessions(); };
          more.append(el('span', '', st.expandAll ? t("收起") : t("展开全部 (+{0})", done.length - 3)));
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
// 全局 SSE（#60）：后端在会话创建/归档/置顶/状态变更时广播 sessions-changed，前端自动刷新侧栏。
// - 300ms 节流合并；subGroupState 跨重渲染保留（折叠/展开不丢）；
// - 仅刷新侧栏列表，不抢输入焦点、不打断正在流式查看的会话；
// - 运行中的 run 完成由各自 SSE 处理本会话视图，这里只同步列表。
let sessionsEv = null, sessionsEvTimer = 0;
function scheduleSessionsRefresh() {
  if (sessionsEvTimer) return; // 300ms 内多次事件合并为一次
  sessionsEvTimer = setTimeout(() => {
    sessionsEvTimer = 0;
    const focused = document.activeElement === $('prompt'); // 输入框聚焦时跳过本次，避免打断打字；下个事件再刷新
    if (focused) return;
    action(loadSessions)().catch(() => {});
  }, 300);
}
function setupGlobalEvents() {
  if (sessionsEv || typeof EventSource !== 'function') return;
  try {
    sessionsEv = new EventSource('/api/events?access_token=' + encodeURIComponent(state.token));
    sessionsEv.addEventListener('sessions-changed', () => scheduleSessionsRefresh());
    sessionsEv.onerror = () => { /* 浏览器自动重连；不关闭 */ };
  } catch (_) { sessionsEv = null; }
}

/* ── 小秘系统会话密码门（#30）：点击小秘会话先验证账户密码，通过后才进入；锁屏后需重新验证 ── */
function openAssistantGate(id, title) {
  action(async () => {
    // 后端的内存锁才是授权依据。localStorage 只作体验缓存：服务重启/锁屏后
    // 它可能过期，不能因此跳过身份验证框。
    try {
      const r = await api('/sessions/' + id + '/unlock-assistant', { method: 'POST', body: JSON.stringify({ password: '' }) });
      if (r && (r.noPassword || r.unlocked)) { localStorage.setItem('assistantUnlocked_' + id, '1'); selectSession(id); return; }
    } catch (_) {}
    // 需要密码：弹身份验证框
    const res = await requestMasterAuth({ reason: t('进入小秘会话需要验证身份'), assistantSessionId: id });
    if (res === null) return; // 取消
    localStorage.setItem('assistantUnlocked_' + id, '1');
    selectSession(id);
  })();
}
// 锁屏时清空小秘会话内存解锁态
function clearAssistantGate() { Object.keys(localStorage).filter(k => k.indexOf('assistantUnlocked_') === 0).forEach(k => localStorage.removeItem(k)); }
const sessionSeq = { value: 0 }; // R07：递增请求序号，旧响应不得覆盖新选择
const SESSION_HISTORY_PAGE = 30;
function sessionRevision(s) {
  const runs = s.runs || [], messages = s.messages || [];
  const lastRun = runs[runs.length - 1];
  return [s.id, s.updated, s.runsTotal ?? runs.length, s.messagesTotal ?? messages.length, state.historyLimit,
    runs.length, messages.length, lastRun?.id, lastRun?.status, lastRun?.steps?.map(step => step.name + ':' + step.status + ':' + (step.content || '').length).join(',')].join('|');
}
function getSessionWindow(id) { return api('/sessions/' + id + '?limit=' + state.historyLimit); }
function paintSessionSelection(id, loading) {
  const items = [...document.querySelectorAll('.session-item[data-session-id]'), $('assistant-entry')].filter(Boolean);
  for (const item of items) {
    const selected = item.dataset.sessionId === id;
    item.classList.toggle('active', selected);
    item.classList.toggle('loading', loading && selected);
  }
  $('conversation').classList.toggle('session-switching', loading);
  $('conversation').setAttribute('aria-busy', String(loading));
  if (loading) {
    const selected = items.find(item => item.dataset.sessionId === id);
    if (selected?.dataset.sessionTitle) $('session-title').textContent = selected.dataset.sessionTitle;
  }
}
async function selectSession(id) {
  const seq = ++sessionSeq.value;
  const sameSession = state.session?.id === id;
  if (!sameSession) { state.historyLimit = SESSION_HISTORY_PAGE; state.historyScroll = false; }
  if (typeof cancelContextPreview === 'function') cancelContextPreview();
  state.pendingSessionId = id;
  paintSessionSelection(id, true); // 首帧反馈不等待 GET 或「已读」磁盘写入
  if (!sameSession && typeof stopXiaomiDictation === 'function') stopXiaomiDictation();
  clearTimeout(state.poll); closeStream();
  ttsCancel();
  if (!sameSession) {
    // live 文本按 run 归属：切换会话才失效；同会话刷新（排队/插话等）保留流式状态，
    // 避免打断正在流式渲染的回答（closeStream 后 schedulePoll 会重连，live 丢失会造成文本回退）
    state.live = {}; state.liveStable = {}; state.liveRound = {}; state.liveTool = {}; state.liveReasoning = {}; state.runPhase = {}; state.streamRetryAt = 0;
  }
  try {
    const loaded = await getSessionWindow(id);
    if (seq !== sessionSeq.value) return; // 已有更新的选择，丢弃本次过期响应
    const json = sessionRevision(loaded);
    const changed = json !== state.sessionJSON;
    state.session = loaded;
    state.sessionJSON = json;
    if (loaded.pendingPrompt && !$('prompt').value.trim()) $('prompt').value = loaded.pendingPrompt;
    if (changed || !sameSession) renderSession(); // 数据未变时跳过重渲染，点击更轻快
    refreshCompactInfo();
    schedulePoll();
    $('prompt').focus(); // 不等待侧栏刷新即可继续输入
    if (typeof scheduleContextPreview === 'function') scheduleContextPreview();
    // 「已读」写入可能串行占用服务端锁；首屏读取及渲染完成后再执行。
    api(`/sessions/${id}`, { method: 'PATCH', body: JSON.stringify({ check: true }) })
      .then(() => loadSessions()).catch(() => {});
  } finally {
    if (seq === sessionSeq.value) {
      state.pendingSessionId = '';
      paintSessionSelection(state.session?.id || '', false);
      if (state.session?.id !== id) {
        $('session-title').textContent = state.session?.title || t('开始新的探索');
        if (state.session?.id) schedulePoll(); // GET 失败时恢复原会话轮询
      }
    }
  }
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
  es.addEventListener('heartbeat', e => {
    let d; try { d = JSON.parse(e.data); } catch (_) { return; }
    const ph = state.runPhase[run.id];
    if (!ph) return;
    ph.lastHeartbeat = Date.now();
    const row = ph.tools.find(x => x.callId === d.callId) || ph.tools[ph.tools.length - 1];
    if (row) row.lastHeartbeat = ph.lastHeartbeat;
    touchRunActivity(run.id);
    renderRunStatus(run.id);
  }); // 长命令心跳：可见地更新连接状态与计时，而非只重置看门狗
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
    delete state.live[run.id]; delete state.liveStable[run.id]; delete state.liveRound[run.id]; delete state.liveTool[run.id]; delete state.liveReasoning[run.id];
    delete state.runPhase[run.id]; // run 终态，释放 phase 残留（看门狗/锁屏均跳过 done 项）
    closeStream(); state.streamRetryAt = 0;
    action(async () => {
      const id = state.session?.id; if (!id) return;
      const s = await getSessionWindow(id); if (state.session?.id !== id) return;
      if (adoptSessionIfChanged(s)) renderSession();
      // 小秘语音发起的 run 完成且开启语音回复 → 朗读最后一条 assistant 回复
      if (voice.awaitingReply) {
        voice.awaitingReply = false;
        if (state.config && state.config.voiceReplyEnabled) {
          // 选最终完整结论：排除工具过渡/过短消息（<30 字多为工具确认）；找不到再退回最后一条非空 assistant
          const rev = [...(s.messages || [])].reverse();
          let last = rev.find(m => m.role === 'assistant' && m.content && m.content.trim().length >= 30);
          if (!last) last = rev.find(m => m.role === 'assistant' && m.content && m.content.trim());
          if (last) speakReply(last.content);
        }
      }
      // 正在查看的会话完成 → 视为已检查，直接清除高亮；后台完成的会话保持蓝点+加粗待点击
      if (s.runs.some(r => r.status === 'completed')) {
        try { await api(`/sessions/${id}`, { method: 'PATCH', body: JSON.stringify({ check: true }) }); } catch (err) {}
      }
      await loadSessions(); // 任务完成：AI 已更新标题，同步侧栏会话列表
      loadFiles(true).catch(()=>{}); // AI 可能写入了新文件，自动刷新右侧项目文件
      schedulePoll();
      scheduleTitleSync(id); // 主题总结是后台异步调用：稍后补一次同步标题
      maybeAutoNarrate(run.id); // 无障碍/讲解模式：输出完成后由小秘自动讲解（aide 本身不发声）
    })();
  });
  // 流断开：保留已积累的 live 文本。服务重启或一次网络失败不能让
  // 轮询链就此消失，否则后端已进入终态时页面会永久停在“运行中”。
  es.onerror = () => {
    closeStream();
    state.streamRetryAt = Date.now() + 5000;
    refreshSessionSoon(); // 立即向持久化会话状态对账，避免只等下一轮 SSE。
    schedulePoll();       // 确保意外断流后的轮询兜底仍在运行。
  };
}
// 流式渲染节流：delta 先累积，按动画帧批量渲染（每帧最多一次解析）。
// 自适应兜底：活动块异常大（如超长未闭合代码块）时隔帧渲染，给主线程喘息，
// 防止低端机长代码块掉帧；正常大小活动块每帧渲染保持顺滑。
const liveRenderScheduled = new Set();
const liveActiveBytes = {};
const liveSkipFrame = {};
function scheduleLiveRender(runId) {
  if (liveRenderScheduled.has(runId)) return;
  liveRenderScheduled.add(runId);
  const tick = () => requestAnimationFrame(() => {
    liveRenderScheduled.delete(runId);
    if ((liveActiveBytes[runId] || 0) > 16384 && !liveSkipFrame[runId]) {
      liveSkipFrame[runId] = true; // 本帧跳过，下一帧强制渲染
      liveRenderScheduled.add(runId);
      tick();
      return;
    }
    liveSkipFrame[runId] = false;
    renderLiveAnswer(runId);
  });
  tick();
}
// lineCharOffset 返回前 k 行（含各自换行符）的字符总长度，即第 k 行的起始偏移。
function lineCharOffset(lines, k) {
  let n = 0;
  for (let i = 0; i < k; i++) n += lines[i].length + 1; // +1：'\n'
  return n;
}
// liveSplitPoint 返回「已稳定块 / 活动块」的字符分割点。
// 已稳定块 = 已闭合的 markdown 顶层块（完整段落 / 代码块 / 列表 / 表格），
// 只需解析一次；活动块 = 最后一个仍在增长的块，逐帧只解析它，让单帧渲染
// 代价近似 O(活动块) 而非 O(全文)，长回答也不掉帧。
function liveSplitPoint(src) {
  const lines = src.split('\n');
  let fenceOpen = false, fenceStart = -1, lastFenceEnd = -1;
  for (let i = 0; i < lines.length; i++) {
    // 围栏标记：行首 0-3 空格后紧跟 ``` 或 ~~~（含四反引号围栏）
    if (/^ {0,3}(```|~~~)/.test(lines[i])) {
      if (!fenceOpen) { fenceOpen = true; fenceStart = i; }
      else { fenceOpen = false; lastFenceEnd = i; }
    }
  }
  // 未闭合围栏：活动块从开启围栏的行开始，整段未完成代码块都在活动区。
  if (fenceOpen) return lineCharOffset(lines, fenceStart);
  // 围栏已闭合：分割点不得早于最后一个闭合围栏行，避免把代码块【内部】的
  // 空行误判成顶层块边界而拆开代码块。
  const lower = lastFenceEnd >= 0 ? lastFenceEnd : 0;
  for (let i = lines.length - 1; i >= lower; i--) {
    if (lines[i].trim() === '') return lineCharOffset(lines, i);
  }
  return lineCharOffset(lines, lower); // 围栏刚闭合 / 尚无空行：保守把最后块留在活动区
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
  if (!full) { ans.innerHTML = ''; return; }
  const split = liveSplitPoint(full);
  const stableText = full.slice(0, split);
  const activeText = full.slice(split);
  liveActiveBytes[runId] = activeText.length;
  // 双分区：stable 只在新块闭合时重建，active 每帧重建（但只含活动小块）
  let stableEl = ans.querySelector('.live-stable');
  let activeEl = ans.querySelector('.live-active');
  if (!stableEl || !activeEl) {
    ans.innerHTML = '';
    stableEl = document.createElement('div');
    stableEl.className = 'live-stable';
    activeEl = document.createElement('div');
    activeEl.className = 'live-active';
    ans.append(stableEl, activeEl);
    state.liveStable[runId] = null;
  }
  const cache = state.liveStable[runId];
  if (!cache || cache.text !== stableText) {
    stableEl.innerHTML = stableText ? renderMarkdown(stableText, true) : '';
    state.liveStable[runId] = { text: stableText };
  }
  // live 渲染跳过代码高亮（每帧高亮代价高），完成态由 renderSession 全量补全
  activeEl.innerHTML = (activeText ? renderMarkdown(activeText, true) : '') +
    '<span class="stream-cursor" aria-hidden="true">▍</span>';
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
  const next = sessionRevision(s);
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
  if (ph.phase === 'tool') return ph.lastHeartbeat ? t('正在调用 {0}（连接正常）', ph.toolName || t('工具')) : t('正在调用 {0}', ph.toolName || t('工具'));
  if (ph.phase === 'generating') return t('正在生成回答');
  return t('等待模型响应');
}
function formatElapsed(startedAt) {
  const sec = Math.max(0, Math.floor((Date.now() - startedAt) / 1000));
  const m = Math.floor(sec / 60);
  return m > 0 ? m + ':' + String(sec % 60).padStart(2, '0') : sec + 's';
}
// 思考框是否已贴近底部（流式自动跟随 / 手动上翻不吸回的阈值判断）
function thinkAtBottom(body) {
  return body.scrollHeight - body.scrollTop - body.clientHeight < 24;
}
function renderToolRow(tool) {
  const row = el('div', 'rsp-tool' + (tool.status === 'running' ? ' running' : tool.status === 'err' ? ' err' : ''));
  const icon = tool.status === 'running' ? '◌' : tool.status === 'err' ? '✗' : '✓';
  row.append(el('span', 'rsp-tool-icon', icon));
  row.append(el('span', 'rsp-tool-name', tool.tool));
  if (tool.args) row.append(el('span', 'rsp-tool-args', tool.args));
  row.append(el('span', 'rsp-tool-dur', tool.endedAt ? Math.round((tool.endedAt - tool.startedAt) / 1000) + 's' : Math.max(0, Math.round((Date.now() - tool.startedAt) / 1000)) + 's'));
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
  // 头部：spinner + 阶段 + 计时。子节点只创建一次并永久复用，高频流式事件仅更新文本；
  // 否则每次 replaceChildren 都会销毁重建 .rsp-spinner，使 CSS 旋转动画从 0° 重启 → 转圈抖动/卡顿
  let head = panel.querySelector('.rsp-head');
  if (!head) {
    head = el('div', 'rsp-head');
    head.append(el('span', 'rsp-spinner'), el('span', 'rsp-phase'), el('span', 'rsp-elapsed'));
    panel.append(head);
  }
  head.querySelector('.rsp-phase').textContent = phaseLabel(ph);
  head.querySelector('.rsp-elapsed').textContent = formatElapsed(ph.startedAt);
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
  // 思考折叠区（流式增量追加；运行中默认展开窥测；不重建节点以保留展开态与滚动位置）
  const reasoning = state.liveReasoning[runId] || '';
  let det = panel.querySelector('.rsp-thinking');
  if (reasoning) {
    if (!det) {
      det = el('details', 'rsp-thinking');
      det.open = true;
      const sum = el('summary', '');
      const body = el('div', 'rsp-think-body');
      const follow = el('button', 'rsp-follow', t('↓ 最新'));
      follow.type = 'button';
      follow.onclick = () => { body.scrollTop = body.scrollHeight; };
      const wrap = el('div', 'rsp-think-scroll');
      wrap.append(body, follow);
      det.append(sum, wrap);
      panel.append(det);
      follow.classList.add('hidden');
      body.addEventListener('scroll', () => follow.classList.toggle('hidden', thinkAtBottom(body)));
    }
    det.querySelector('summary').textContent = ph.done ? t('思考过程') : t('思考中…');
    const body = det.querySelector('.rsp-think-body');
    const follow = det.querySelector('.rsp-follow');
    // 增量追加：只把新增片段 append 为文本节点，避免全量 textContent 重置 scrollTop
    const rendered = Number(body.dataset.len || 0);
    if (reasoning.length > rendered) {
      const atBottom = thinkAtBottom(body);
      body.append(document.createTextNode(reasoning.slice(rendered)));
      body.dataset.len = String(reasoning.length);
      if (atBottom) body.scrollTop = body.scrollHeight; // 未上翻时跟随最新；手动上翻则保持位置不吸回
    }
    follow.classList.toggle('hidden', thinkAtBottom(body) || ph.done);
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
    const runningTool = box.querySelector('.rsp-tool.running .rsp-tool-dur');
    const activeTool = ph.tools.find(tool => tool.status === 'running');
    if (runningTool && activeTool) runningTool.textContent = Math.max(0, Math.round((now - activeTool.startedAt) / 1000)) + 's';
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
      const s2 = await getSessionWindow(id);
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
    const s = await getSessionWindow(id); if (state.session?.id !== id) return;
    if (adoptSessionIfChanged(s)) renderSession();
  }), 250);
}
function schedulePoll() {
  clearTimeout(state.poll);
  const running = state.session?.runs?.find(r => r.status === 'running');
  if (running) {
    if ((!state.stream || state.stream._runId !== running.id) && (!state.streamRetryAt || Date.now() >= state.streamRetryAt)) openStream(running);
    state.poll = setTimeout(async () => {
      const id = state.session?.id;
      if (!id) return;
      try {
        const s = await getSessionWindow(id);
        if (state.session?.id !== id) return;
        const hadRunning = !!state.session?.runs?.find(r => r.status === 'running');
        if (adoptSessionIfChanged(s)) renderSession();
        if (hadRunning && !s.runs.some(r => r.status === 'running')) {
          if (s.runs.some(r => r.status === 'completed')) {
            try { await api(`/sessions/${id}`, { method: 'PATCH', body: JSON.stringify({ check: true }) }); } catch (err) {}
          }
          await loadSessions(); // 轮询兜底路径：完成时同步列表
          loadFiles(true).catch(()=>{}); // 自动刷新右侧项目文件
          scheduleTitleSync(id);
          maybeAutoNarrate(running.id); // 轮询兜底：输出完成自动讲解
        }
      } catch (e) {
        // 轮询兜底：后端暂不可达时静默退避，避免每 1.5s 弹错误 toast 刷屏
      } finally {
        // 重排必须在 finally 中，避免一次请求短暂失败后永远不再同步运行状态
        if (state.session?.id === id) schedulePoll();
      }
    }, 1500);
  } else {
    closeStream();
  }
}
async function newSession() {
  ++sessionSeq.value; state.pendingSessionId = '';
  state.historyLimit = SESSION_HISTORY_PAGE; state.historyScroll = false;
  cancelContextPreview();
  paintSessionSelection('', false);
  clearTimeout(state.poll); closeStream(); state.live = {}; state.liveStable = {}; state.liveRound = {}; state.liveTool = {}; state.liveReasoning = {}; state.runPhase = {}; state.streamRetryAt = 0; state.sessionJSON = ''; state.session = null; state.attachments = []; renderAttachments(); renderSession(); $('prompt').focus(); if (typeof hideContextPreview === 'function') hideContextPreview();
  loadSessions().catch(() => {});
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
function toolIconSVG(tool) {
  const p = '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">';
  if (tool === 'run_shell') return p + '<path d="M4 17l6-5-6-5"/><path d="M12 19h8"/></svg>';
  if (tool === 'read_file') return p + '<path d="M14 3H7a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h10a2 2 0 0 0 2-2V8Z"/><path d="M9 12h6M9 16h4"/></svg>';
  if (tool === 'list_files') return p + '<path d="M3 7a2 2 0 0 1 2-2h5l2 3h7a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2Z"/></svg>';
  if (tool === 'write_file' || tool === 'edit_file') return p + '<path d="M12 20h9"/><path d="M16.5 3.5a2.1 2.1 0 0 1 3 3L7 19l-4 1 1-4Z"/></svg>';
  if (tool === 'web_search') return p + '<circle cx="11" cy="11" r="7"/><path d="m20 20-3.2-3.2"/></svg>';
  return p + '<circle cx="12" cy="12" r="3.2"/><path d="M12 2.5v3M12 18.5v3M2.5 12h3M18.5 12h3M5.6 5.6l2.1 2.1M16.3 16.3l2.1 2.1M18.4 5.6l-2.1 2.1M7.7 16.3l-2.1 2.1"/></svg>';
}
function toolDisplayName(tool) {
  const map = { read_file: '读取文件', list_files: '列出文件', run_shell: '执行命令', write_file: '写入文件', edit_file: '编辑文件', web_search: '在线搜索', search_text: '搜索资料' };
  return map[tool] || tool;
}
function prettyPath(p) {
  if (p === '.' || p === './') return t('当前目录');
  if (p === '..' || p === '../') return t('上一级目录');
  return p;
}
function toolArgText(use) {
  try {
    const a = JSON.parse(use.args || '{}');
    if (use.tool === 'run_shell') return a.command || '';
    // 搜索类：query 才是主体，path 仅为搜索范围（path 为当前目录时省略）
    if (use.tool === 'search_text' || use.tool === 'web_search') {
      let q = a.query || '';
      if (use.tool === 'search_text' && a.path && a.path !== '.' && a.path !== './') q += '  ·  ' + prettyPath(a.path);
      return q;
    }
    if (a.path) return (a.source ? '[' + a.source + '] ' : '') + prettyPath(a.path);
    if (a.url) return a.url;
    const v = Object.values(a).find(x => typeof x === 'string');
    return v || '';
  } catch (e) { return ''; }
}
function buildToolUses(run) {
  const group = el('details', 'tool-group');
  group.dataset.key = run.id + ':tools';
  const uses = run.toolUses || [];
  const firstText = toolArgText(uses[0]) || toolDisplayName(uses[0].tool);
  const head = el('summary', 'tg-head');
  head.insertAdjacentHTML('beforeend', '<svg class="tg-caret" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.4" stroke-linecap="round" stroke-linejoin="round"><path d="M9 6l6 6-6 6"/></svg>');
  head.insertAdjacentHTML('beforeend', '<span class="tg-ico">' + toolIconSVG('run_shell') + '</span>');
  head.append(el('span', 'tg-title', t('工具调用')));
  head.append(el('span', 'tg-count', String(uses.length)));
  const firstEl = el('span', 'tg-first', firstText); firstEl.title = firstText; head.append(firstEl);
  const body = el('div', 'tg-body');
  uses.forEach(use => {
    const item = el('details', 'tl');
    const h = el('summary', 'tl-head');
    h.insertAdjacentHTML('beforeend', '<span class="tl-ico">' + toolIconSVG(use.tool) + '</span>');
    h.append(el('span', 'tl-name', toolDisplayName(use.tool)));
    const argsEl = el('code', 'tl-args', toolArgText(use)); argsEl.title = argsEl.textContent; h.append(argsEl);
    h.insertAdjacentHTML('beforeend', '<svg class="tl-caret" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.4" stroke-linecap="round" stroke-linejoin="round"><path d="M9 6l6 6-6 6"/></svg>');
    const b = el('div', 'tl-body');
    const isCommand = use.tool === 'run_shell';
    b.append(el('div', 'tl-label', isCommand ? t('命令') : t('参数')));
    const headText = isCommand ? toolArgText(use) : (use.args || '');
    b.append(el('pre', 'tl-pre tl-cmd', headText || t('（无）')));
    b.append(el('div', 'tl-label', t('结果')));
    b.append(el('pre', 'tl-pre tl-result', use.result || t('（无结果）')));
    item.append(h, b);
    body.append(item);
  });
  group.append(head, body);
  return group;
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
    const send = el('button', 'primary', t('发送')); send.onclick = () => answer(input.value.trim() || t('(空)'));
    input.onkeydown = e => { if (e.key === 'Enter') answer(input.value.trim() || t('(空)')); };
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
  const conversation = $('conversation');
  const previousScroll = conversation.scrollTop;
  const previousHeight = conversation.scrollHeight;
  const preserveHistoryScroll = state.historyScroll;
  state.historyScroll = false;
  const nearBottom = conversation.scrollHeight - previousScroll - conversation.clientHeight < 100;
  const openDetails = new Set([...$('timeline').querySelectorAll('details[open][data-key]')].map(d => d.dataset.key));
  const isAssistantSess = state.session?.kind === 'assistant';
  document.body.classList.toggle('assistant-mode', !!isAssistantSess);
  syncAssistantModeControls(!!isAssistantSess);
  $('prompt').placeholder = isAssistantSess ? t('对小蜜说点什么…') : '';
  $('session-title').textContent = state.session?.title || t("开始新的探索");
  // #62：小秘会话始终隐藏通用 welcome（及其 4 个快捷入口），改渲染小蜜专属时间线/空状态
  $('welcome').classList.toggle('hidden', isAssistantSess || !!state.session?.runs.length);
  $('timeline').replaceChildren(); state.busy = false;
  if (state.session?.hasOlder) {
    const older = el('button', 'quiet load-older-history', t('加载更早的会话历史'));
    older.type = 'button';
    older.onclick = action(async () => {
      const id = state.session?.id; if (!id || !state.session?.hasOlder) return;
      state.historyLimit = Math.min(2000, state.historyLimit + SESSION_HISTORY_PAGE);
      state.historyScroll = true;
      await selectSession(id);
    });
    $('timeline').append(older);
  }
  // #62：小蜜空状态——历史为空时显示专属引导，绝不显示通用欢迎页
  if (isAssistantSess && !(state.session?.runs || []).length && !(state.session?.messages || []).length) {
    const es = el('div', 'assistant-empty');
    es.append(el('div', 'assistant-empty-icon'));
    es.append(el('p', 'assistant-empty-title', state.session?.title || t('小秘')));
    es.append(el('p', 'assistant-empty-sub', t('还没有对话，点麦克风开始')));
    $('timeline').append(es);
    return;
  }

  // #62扩展：小蜜会话渲染 messages 时间线（voice-in/text-in/voice-note/voice-ask）
  if (isAssistantSess && (state.session?.messages || []).length) {
    for (const msg of state.session.messages) {
      const type = msg.type || '';
      const row = el('div', 'assistant-msg am-' + (type || 'chat'));
      const iconMap = { 'voice-in': '🎤', 'text-in': '⌨️', 'voice-note': '💬', 'voice-ask': '📋' };
      const icon = iconMap[type] || '💬';
      row.append(el('span', 'am-icon', icon));
      const body = el('div', 'am-body');
      const content = msg.content || msg.text || '';
      if (content) body.append(el('div', msg.role === 'assistant' ? 'am-reply' : 'am-text', content));
      if (msg.reply) body.append(el('div', 'am-reply', msg.reply));
      if (msg.dispatched) {
        const card = el('button', 'am-dispatched');
        card.textContent = t('已创建会话 #{0}：{1}', msg.dispatched.number || '?', msg.dispatched.title || '');
        card.onclick = action(() => selectSession(msg.dispatched.sessionId));
        body.append(card);
      }
      row.append(body);
      $('timeline').append(row);
    }
  }
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
    const meta = el('div', 'run-meta'); meta.append(el('span', '', run.mode === 'workflow' ? t("◈ AIDE WORKFLOW · 规划 → 方案 → 审查") : '◌ AIDE ASSISTANT'), el('span', 'run-model', run.model || ''), el('span', 'run-status', t(statuses[run.status] || run.status))); if (run.strategy) meta.append(el('span', 'run-strategy', run.strategy === 'auto' ? t("策略: 自动 → {0}", profileName(run.profile)) : t("策略: 手动 · {0}", profileName(run.profile)))); box.append(meta);
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
          ans.textContent = t("正在思考");
          for (let _di = 1; _di <= 3; _di++) { const _dot = el('span', 'thinking-dot', '.'); _dot.setAttribute('aria-hidden', 'true'); ans.append(_dot); }
        } else if (run.error) {
          // 真·失败：显示具体原因（上游错误/超时/取消），不再是光秃秃的“未返回回答”
          ans.classList.add('chat-answer-error');
          ans.textContent = '⚠ ' + run.error;
        } else {
          ans.classList.add('chat-answer-empty');
          ans.textContent = run.toolUses?.length ? t("未返回回答（已完成 {0} 次工具调用）", run.toolUses.length) : t("未返回回答（模型未生成正文）");
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
          if (text) {
            actions.append(mk(t("复制"), () => { navigator.clipboard.writeText(text).then(() => toast(t("已复制"))).catch(() => toast(t("复制失败"))); }));
            const rb = mk(t("朗读"), () => toggleMechanicalRead(rb, text));
            rb.classList.add('msg-btn-mech');
            actions.append(rb);
          }
          actions.append(mk(t("重试"), () => { api(`/sessions/${state.session.id}/runs/${run.id}/retry`, { method: 'POST', body: '{}' }).then(() => selectSession(state.session.id)); }));
          actions.append(mk(t("继续"), () => { $('prompt').value = t("继续"); $('task-form').requestSubmit(); }));
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
      if (run.status === 'awaiting_approval') {
        if (ignoredProposals.has(run.id)) {
          proposal.append(el('p', 'muted', t("已忽略此提案，未写入磁盘。")));
        } else {
          const apply = el('button', 'primary', t("应用这些文件修改"));
          apply.onclick = async () => {
            if (apply.disabled) return;
            apply.disabled = true;
            try {
              await api(`/sessions/${state.session.id}/runs/${run.id}/apply`, { method: 'POST', body: '{}' });
              toast(t("文件修改已写入本地挂载目录"));
              await selectSession(state.session.id);
              await loadFiles();
            } catch (e) {
              toast(e.message || t("应用失败，请稍后重试")); // 409 冲突等：把后端 error 透传给用户
            } finally {
              apply.disabled = false;
            }
          };
          const ignore = el('button', 'quiet', t("忽略此提案"));
          ignore.onclick = () => { ignoredProposals.add(run.id); renderSession(); };
          proposal.append(el('p', 'muted', t("请展开检查文件内容。应用后会写入本地工作目录；验证命令需要单独运行。")), apply, ignore);
        }
      }
      else if (run.applied) proposal.append(el('p', 'muted', t("✓ 已应用文件修改。命令验证结果以命令面板为准。")));
      box.append(proposal);
    }
    if (run.commands?.length) {
      box.append(el('p', 'muted', t("建议验证命令（尚未运行）")));
      run.commands.forEach(command => { const row = el('div', 'suggested-command'); const button = el('button', 'quiet', t("填入命令面板")); button.onclick = () => { $('terminal-body').classList.remove('hidden'); $('terminal-state').textContent = t("收起 −"); $('command').value = command; $('command').focus(); }; row.append(el('code', '', command), button); box.append(row); });
    }
    if (run.toolUses?.length) {
      box.append(buildToolUses(run));
    }
    if (run.error) box.append(el('p', 'task-error', run.error)); $('timeline').append(box);
  }
  // 运行中：发送箭头原位切换为停止图标（插话/排队仍可用 Enter 或「排队」+Enter 提交）
  setSendMode(state.busy);
  const c = $('conversation');
  const expandedHistoryDelta = Math.max(0, c.scrollHeight - previousHeight);
  c.scrollTo({ top: preserveHistoryScroll ? previousScroll + expandedHistoryDelta : (nearBottom ? c.scrollHeight : previousScroll), behavior: 'instant' });
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
  state.attachments.forEach((a, index) => { const chip = el('span', 'chip', a.root === 'context' ? t("参考 · {0}", a.path) : a.path); const b = el('button', '', '×'); b.setAttribute('aria-label', t("移除附件 {0}", a.path)); b.onclick = () => { state.attachments.splice(index, 1); renderAttachments(); }; chip.append(b); $('attachment-chips').append(chip); });
}
async function loadFiles(auto) {
  if (auto) { const _r = document.querySelector('#files input.file-rename'); if (_r && document.activeElement === _r) return; } // 自动刷新且正在重命名 → 跳过，不打断
  const query = state.root === 'context' && state.source ? '/files?source=' + encodeURIComponent(state.source) + '&path=' : '/files?root=' + state.root + '&path=';
  const search = state.fileSearch.trim();
  const searchParams = search ? '&search=' + encodeURIComponent(search) + '&scope=' + encodeURIComponent(state.fileSearchScope) + '&match=' + encodeURIComponent(state.fileSearchMatch) : '';
  // 直接 fetch 以便读取搜索截断响应头（api() 只返回 body）。
  const filesResp = await fetch('/api' + query + encodeURIComponent(state.dir) + searchParams, { headers: { 'Authorization': 'Bearer ' + state.token } });
  const files = await filesResp.json();
  if (!filesResp.ok) { if (filesResp.status === 401 && !$('login-dialog').open) $('login-dialog').showModal(); throw new Error(t(files && files.error) || t("请求失败")); }
  state.fileSearchTruncated = filesResp.headers.get('X-Search-Truncated') === '1';
  state.fileSearchDirLimit = filesResp.headers.get('X-Search-Dir-Limit') || '200';
  state.fileSearchResultLimit = filesResp.headers.get('X-Search-Result-Limit') || '500';
  const selectionLocation = fileLocationKey() + ':' + state.dir;
  if (state.fileSelectionLocation !== selectionLocation) { state.fileSelection.clear(); state.fileSelectionAnchor = -1; state.fileSelectionLocation = selectionLocation; }
  const visible = new Set(files.map(f => f.path));
  for (const p of state.fileSelection) if (!visible.has(p)) state.fileSelection.delete(p);
  state.fileDirs[fileLocationKey()] = state.dir;
  const label = state.root === 'context' && state.source ? 'sources/' + (state.sources.find(x => x.id === state.source)?.name || state.source) : state.root;
  $('file-path').textContent = '/' + label + (state.dir === '.' ? '' : '/' + state.dir); $('file-path').title = $('file-path').textContent;
  const writable = currentFileTarget().writable;
  $('new-file').disabled = !writable;
  state.fileEntries = files;
  renderFileEntries();
}
function fileLocationKey() { return state.root === 'context' ? 'context:' + (state.source || '') : 'workspace'; }
function rememberFileLocation() { state.fileDirs[fileLocationKey()] = state.dir; }
function restoreFileLocation() { state.dir = state.fileDirs[fileLocationKey()] || '.'; }
function currentFileTarget() {
  if (state.root !== 'context') return { writable: true, source: '' };
  const src = state.sources.find(x => x.id === state.source);
  return { writable: !!src?.rw, source: state.source || '' };
}
const fileNavigationGesture = { current: null, suppressClickUntil: 0 };
function clearFileNavigationHighlights() {
  document.querySelectorAll('.file-nav-source,.file-nav-target').forEach(node => node.classList.remove('file-nav-source', 'file-nav-target'));
  document.body.classList.remove('file-nav-dragging');
}
function resetFileNavigationGesture() {
  if (fileNavigationGesture.current) clearTimeout(fileNavigationGesture.current.timer);
  fileNavigationGesture.current = null;
  clearFileNavigationHighlights();
}
function fileNavigationDropTarget(x, y) {
  const node = document.elementFromPoint(x, y);
  const row = node?.closest('#files .file-item[data-file-dir="true"]');
  if (row) return { type: 'folder', path: row.dataset.filePath, element: row };
  const parent = node?.closest('#parent-dir');
  if (parent && state.dir !== '.') return { type: 'parent', path: state.dir.includes('/') ? state.dir.slice(0, state.dir.lastIndexOf('/')) : '.', element: parent };
  return null;
}
document.addEventListener('pointermove', event => {
  const gesture = fileNavigationGesture.current;
  if (!gesture || event.pointerId !== gesture.pointerId) return;
  const dx = event.clientX - gesture.x, dy = event.clientY - gesture.y;
  if (!gesture.active) {
    if (Math.hypot(dx, dy) > 8) resetFileNavigationGesture();
    return;
  }
  if (Math.hypot(dx, dy) > 8) gesture.moved = true;
  clearFileNavigationHighlights();
  if (gesture.moved) {
    gesture.row.classList.add('file-nav-source');
    gesture.target = fileNavigationDropTarget(event.clientX, event.clientY);
    if (gesture.target) gesture.target.element.classList.add('file-nav-target');
    document.body.classList.add('file-nav-dragging');
  }
});
document.addEventListener('pointerup', event => {
  const gesture = fileNavigationGesture.current;
  if (!gesture || event.pointerId !== gesture.pointerId) return;
  clearTimeout(gesture.timer);
  if (!gesture.active || !gesture.moved) { resetFileNavigationGesture(); return; }
  event.preventDefault();
  fileNavigationGesture.suppressClickUntil = Date.now() + 500;
  const target = gesture.target;
  resetFileNavigationGesture();
  if (!target) return;
  state.dir = target.path;
  loadFiles().catch(error => toast(error.message || String(error)));
});
document.addEventListener('pointercancel', resetFileNavigationGesture);
window.addEventListener('blur', resetFileNavigationGesture);
function bindFileNavigationGesture(row) {
  row.draggable = false;
  row.onpointerdown = event => {
    if (event.pointerType !== 'mouse' || event.button !== 0 || event.ctrlKey || event.metaKey || event.shiftKey) return;
    resetFileNavigationGesture();
    const gesture = { pointerId: event.pointerId, x: event.clientX, y: event.clientY, row, active: false, moved: false, target: null, timer: 0 };
    gesture.timer = setTimeout(() => {
      if (fileNavigationGesture.current !== gesture) return;
      gesture.active = true;
      row.classList.add('file-nav-source');
      document.body.classList.add('file-nav-dragging');
      toast(t('拖到文件夹或上级目录后松开即可进入'));
    }, 420);
    fileNavigationGesture.current = gesture;
  };
}
function renderFileEntries() {
  const files = state.fileEntries;
  $('files').replaceChildren();
  if (!files.length) $('files').append(el('p', 'muted', state.fileSearch.trim() ? t("未找到匹配文件") : t("目录为空")));
  files.forEach((file, index) => {
    const b = el('button', 'file-item');
    b.dataset.filePath = file.path;
    b.dataset.fileDir = String(!!file.dir);
    bindFileNavigationGesture(b);
    b.classList.toggle('selected', state.fileSelection.has(file.path));
    b.setAttribute('aria-pressed', String(state.fileSelection.has(file.path)));
    const nameSpan = el('span', 'file-name', file.name);
    b.append(el('span', 'file-icon', file.dir ? '▱' : '≡'), nameSpan);
    if (file.dir) b.append(el('small', '', '›'));
    b.title = file.path;
    b._last = 0;
    // 单击打开（文件→当前标签查看，文件夹→进入）；快速双击文件→新标签打开；右键→菜单（重命名）
    b._clickTimer = 0;
    b.onclick = (ev) => {
      if (Date.now() < fileNavigationGesture.suppressClickUntil) { ev.preventDefault(); ev.stopPropagation(); return; }
      if (ev.ctrlKey || ev.metaKey || ev.shiftKey) {
        clearFileOpenTimers(); b._last = 0;
        if (ev.shiftKey) {
          const anchor = state.fileSelectionAnchor < 0 ? index : state.fileSelectionAnchor;
          if (!ev.ctrlKey && !ev.metaKey) state.fileSelection.clear();
          for (let i = Math.min(anchor, index); i <= Math.max(anchor, index); i++) state.fileSelection.add(files[i].path);
        } else {
          if (state.fileSelection.has(file.path)) state.fileSelection.delete(file.path); else state.fileSelection.add(file.path);
          state.fileSelectionAnchor = index;
        }
        syncFileSelection(); return;
      }
      const now = Date.now(), prev = b._last || 0; b._last = now;
      clearFileOpenTimers();
      if (prev && now - prev <= FILE_DBLCLICK_MS) {
        b._last = 0; selectFileRow(b, file.path, index);
        if (file.dir) { state.dir = file.path; loadFiles().catch(e => toast(e.message)); }
        else openFileInNewTab(file);
        return;
      }
      selectFileRow(b, file.path, index);
      b._clickTimer = setTimeout(() => { // 短延迟以区分双击；随后打开/进入
        if (file.dir) { state.dir = file.path; loadFiles().catch(e => toast(e.message)); }
        else openFile(file.path).catch(e => toast(e.message || String(e)));
      }, FILE_CLICK_OPEN_MS);
    };
    b.oncontextmenu = (ev) => {
      ev.preventDefault(); clearFileOpenTimers(); b._last = 0;
      if (!state.fileSelection.has(file.path)) selectFileRow(b, file.path, index);
      openFileContextMenu(ev, b, nameSpan, file);
    };
    $('files').append(b);
  });
  if (state.fileSearchTruncated) {
    $('files').append(el('p', 'file-search-truncated', t("结果过多，已按上限截断：最多搜索 {0} 个文件夹、显示前 {1} 项，请缩小范围或改用精确匹配", state.fileSearchDirLimit, state.fileSearchResultLimit)));
  }
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
const FILE_DBLCLICK_MS = 450, FILE_RENAME_MS = 1600, FILE_CLICK_OPEN_MS = 240;
function clearFileOpenTimers() {
  document.querySelectorAll('#files .file-item').forEach(row => clearTimeout(row._clickTimer));
}
function syncFileSelection() {
  document.querySelectorAll('#files .file-item').forEach((row, i) => {
    const selected = state.fileSelection.has(state.fileEntries[i]?.path);
    row.classList.toggle('selected', selected);
    row.setAttribute('aria-pressed', String(selected));
  });
}
function selectFileRow(b, filePath, index){
  state.fileSelection.clear(); state.fileSelection.add(filePath);
  state.fileSelectionAnchor = index;
  syncFileSelection();
}
function openFileInNewTab(file){
  const source = state.root === 'context' ? state.source : '';
  const spec = { root: source ? 'source' : state.root, source, path: file.path };
  window.open(location.pathname + '#file=' + encodeURIComponent(JSON.stringify(spec)), '_blank', 'noopener');
}
let fileCtxMenuEl = null;
function closeFileContextMenu() {
  if (!fileCtxMenuEl) return;
  fileCtxMenuEl.remove(); fileCtxMenuEl = null;
  document.removeEventListener('click', closeFileContextMenu);
  document.removeEventListener('keydown', onFileCtxKey);
}
function onFileCtxKey(ev) { if (ev.key === 'Escape') closeFileContextMenu(); }
function fileRawUrl(filePath, root, source) {
  const token = state.token || (state.config && state.config.accessToken) || '';
  const qp = new URLSearchParams(); qp.set('path', filePath);
  if (source) qp.set('source', source); else qp.set('root', root || 'workspace');
  if (token) qp.set('access_token', token);
  return '/api/file/raw?' + qp.toString();
}
function downloadFileEntry(file, archive) {
  const token = state.token || (state.config && state.config.accessToken) || '';
  const qp = new URLSearchParams(); qp.set('path', file.path);
  if (state.root === 'context' && state.source) qp.set('source', state.source); else qp.set('root', state.root || 'workspace');
  if (archive) qp.set('archive', '1'); if (token) qp.set('access_token', token);
  const link = document.createElement('a'); link.href = '/api/file/download?' + qp.toString(); link.download = archive ? file.name + '.zip' : file.name;
  link.style.display = 'none'; document.body.append(link); link.click(); setTimeout(() => link.remove(), 0);
}
function openFileTransferPicker(operation, selectedPaths) {
  const candidates = [{ id: 'workspace', name: t('工作目录') }, ...state.sources.filter(s => s.enabled && s.rw && ['local', 'skill', 'sftp', 'workspace-sftp'].includes(s.type)).map(s => ({ id: s.id, name: s.name }))];
  const dlg = el('dialog', 'file-transfer-dialog');
  const heading = el('h2', '', operation === 'copy' ? t('复制到') : t('移动到'));
  const summary = el('p', '', t('已选择 {0} 项', selectedPaths.length));
  const location = el('select', 'file-transfer-source');
  candidates.forEach(s => { const opt = el('option', '', s.name); opt.value = s.id; location.append(opt); });
  location.value = state.root === 'context' && candidates.some(s => s.id === state.source) ? state.source : 'workspace';
  const breadcrumb = el('div', 'file-transfer-breadcrumb');
  const dirs = el('div', 'file-transfer-dirs');
  const err = el('p', 'file-transfer-error');
  const footer = el('div', 'file-transfer-footer');
  const cancel = el('button', 'quiet', t('取消'));
  const submit = el('button', 'primary', operation === 'copy' ? t('复制到此处') : t('移动到此处'));
  let dir = '.';
  const render = async () => {
    breadcrumb.textContent = '/' + location.selectedOptions[0].textContent + (dir === '.' ? '' : '/' + dir);
    dirs.replaceChildren(); err.textContent = ''; submit.disabled = true;
    try {
      const query = location.value === 'workspace' ? '/files?root=workspace&path=' : '/files?source=' + encodeURIComponent(location.value) + '&path=';
      const entries = await api(query + encodeURIComponent(dir));
      if (dir !== '.') {
        const up = el('button', 'file-transfer-dir', '↑  ' + t('上一级'));
        up.onclick = () => { dir = dir.includes('/') ? dir.slice(0, dir.lastIndexOf('/')) : '.'; render(); };
        dirs.append(up);
      }
      entries.filter(e => e.dir).forEach(e => {
        const b = el('button', 'file-transfer-dir', '▱  ' + e.name + '  ›');
        b.onclick = () => { dir = e.path; render(); };
        dirs.append(b);
      });
      if (!dirs.childElementCount) dirs.append(el('p', 'muted', t('没有子文件夹')));
      submit.disabled = false;
    } catch (e) { err.textContent = e.message || String(e); }
  };
  location.onchange = () => { dir = '.'; render(); };
  cancel.onclick = () => dlg.close();
  submit.onclick = action(async () => {
    submit.disabled = true; err.textContent = '';
    try {
      const source = state.root === 'context' ? state.source : 'workspace';
      const result = await api('/file/transfer', { method: 'POST', body: JSON.stringify({ operation, source, paths: selectedPaths, destination: location.value, destinationPath: dir }) });
      dlg.close(); state.fileSelection.clear(); state.fileSelectionAnchor = -1;
      await loadFiles();
      toast((operation === 'copy' ? t('已复制') : t('已移动')) + ' ' + result.count + ' ' + t('项'));
    } catch (e) { err.textContent = e.message || String(e); submit.disabled = false; }
  });
  footer.append(cancel, submit); dlg.append(heading, summary, location, breadcrumb, dirs, err, footer);
  document.body.append(dlg); dlg.onclose = () => dlg.remove();
  dlg.showModal(); render();
}
function openFileContextMenu(ev, rowBtn, nameSpan, file) {
  closeFileContextMenu();
  const selectedPaths = state.fileEntries.filter(f => state.fileSelection.has(f.path)).map(f => f.path);
  const multiple = selectedPaths.length > 1;
  const m = el('div', 'file-ctx-menu');
  const copyTo = el('button', 'file-ctx-item', t('复制到…') + (multiple ? ' (' + selectedPaths.length + ')' : ''));
  const moveTo = el('button', 'file-ctx-item', t('移动到…') + (multiple ? ' (' + selectedPaths.length + ')' : ''));
  const download = el('button', 'file-ctx-item', t('下载'));
  const compress = el('button', 'file-ctx-item', t('压缩为 ZIP'));
  const extract = el('button', 'file-ctx-item', t('解压到新文件夹'));
  const ren = el('button', 'file-ctx-item', t('重命名'));
  const props = el('button', 'file-ctx-item', t('属性'));
  const del = el('button', 'file-ctx-item danger-item', t('删除'));
  if (!currentFileTarget().writable) { moveTo.disabled = true; moveTo.title = t('只读引用不能移动'); }
  if (multiple) [download, compress, extract, ren, props, del].forEach(x => { x.disabled = true; x.title = t('此操作仅支持单个文件'); });
  if (state.root === 'context') [compress, ren, props, del, extract].forEach(x => { x.classList.add('disabled'); x.disabled = true; x.title = t('引用为只读'); });
  if (state.root !== 'context' && isZipPath(file.path)) { compress.classList.add('disabled'); compress.disabled = true; compress.title = t('不能重复压缩 ZIP 文件'); }
  if (state.root !== 'context' && !isZipPath(file.path)) { extract.classList.add('disabled'); extract.disabled = true; extract.title = t('仅支持 ZIP 文件'); }
  download.onclick = (e) => { e.stopPropagation(); closeFileContextMenu(); downloadFileEntry(file, !!file.dir); };
  copyTo.onclick = (e) => { e.stopPropagation(); closeFileContextMenu(); openFileTransferPicker('copy', selectedPaths); };
  moveTo.onclick = (e) => { e.stopPropagation(); closeFileContextMenu(); openFileTransferPicker('move', selectedPaths); };
  compress.onclick = async (e) => { e.stopPropagation(); closeFileContextMenu(); try { const out = await api('/file/archive', { method: 'POST', body: JSON.stringify({ root: 'workspace', path: file.path }) }); toast(t('已压缩到：') + out.path); await loadFiles(); } catch (err) { toast(err.message || String(err)); } };
  extract.onclick = async (e) => { e.stopPropagation(); closeFileContextMenu(); try { const out = await api('/file/extract', { method: 'POST', body: JSON.stringify({ root: 'workspace', path: file.path }) }); toast(t('已解压到：') + out.path); loadFiles(); } catch (err) { toast(err.message || String(err)); } };
  ren.onclick = (e) => { e.stopPropagation(); closeFileContextMenu(); selectFileRow(rowBtn, file.path, state.fileEntries.indexOf(file)); beginInlineRename(rowBtn, nameSpan, file); };
  props.onclick = (e) => { e.stopPropagation(); closeFileContextMenu(); showFileProperties(file); };
  del.onclick = (e) => { e.stopPropagation(); closeFileContextMenu(); deleteFileEntry(file); };
  m.append(copyTo, moveTo, download, compress, extract, ren, props, del);
  document.body.append(m);
  fileCtxMenuEl = m;
  m.style.left = Math.max(8, Math.min(ev.clientX, innerWidth - 198)) + 'px';
  m.style.top = Math.max(8, Math.min(ev.clientY, innerHeight - 300)) + 'px';
  setTimeout(() => { document.addEventListener('click', closeFileContextMenu); document.addEventListener('keydown', onFileCtxKey); }, 0);
}

async function openFile(path) {
  // 图片 / STL / PDF 走独立 raw 端点的可视化查看器，不经过只支持文本、会拒绝二进制的 /api/file
  if (isImagePath(path) || isStlPath(path) || isPdfPath(path) || isDxfPath(path) || isDocxPath(path) || isXlsxPath(path) || isSqlitePath(path) || isZipPath(path)) {
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
function isPdfPath(path) { return /\.pdf$/i.test(path || ''); }
function isZipPath(path) { return /\.zip$/i.test(path || ''); }
function isXlsxPath(path) { return /\.xlsx$/i.test(path || ''); }
function isSqlitePath(path) { return /\.(db|sqlite|sqlite3)$/i.test(path || ''); }
function setEditorMode(mode) {
  const preview = mode === 'preview';
  $('editor').classList.toggle('hidden', preview);
  if ($('editor').parentNode.classList.contains('code-wrapper')) $('editor').parentNode.classList.toggle('hidden', preview);
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
  const isPdf = isPdfPath(state.file.path);
  const isDxf = isDxfPath(state.file.path);
  const isDocx = isDocxPath(state.file.path);
  const isXlsx = isXlsxPath(state.file.path);
  const isSqlite = isSqlitePath(state.file.path);
  const isZip = isZipPath(state.file.path);
  $('editor').readOnly = readOnly;
  // #64: code syntax highlighting
  teardownCodeHighlight($('editor'));
  teardownViewer($('editor-preview')); // 切文件前回收上一个可视化查看器（WebGL/PDF/Observer）
  var _cl = codeLang(state.file.path);
  if (_cl) setupCodeHighlight($('editor'), _cl);
  // 图片 / STL / PDF 为只读可视化查看器，无文本可保存，禁用保存（避免空内容覆盖原文件）；drawio 可保存
  // 可视化查看器（图片/STL/PDF/DXF）无文本可保存 → 隐藏保存按钮；只读来源的文本文件 → 禁用
  $("save-file").classList.toggle("hidden", isImg || isStl || isPdf || isDxf || isDocx || isXlsx || isSqlite || isZip);
  $("save-file").disabled = readOnly;
  $('attach-file').disabled = state.file.fresh;
  $("editor-ro-badge").classList.toggle("hidden", !(readOnly || isImg || isStl || isPdf || isDxf || isDocx || isSqlite || isZip));
  $("editor-ro-badge").title = (readOnly && state.file.root === "context") ? (sourceIsRW() ? t("引用 · 读写来源") : t("引用 · 只读")) : (isDocx ? t("DOCX 正文预览 · 批注可写入文档") : (isImg || isStl || isPdf || isDxf || isSqlite || isZip ? t("只读 · 可视化查看器") : t("工作目录 · 保存后同步到主机")));
  $('editor-mode-switch').classList.toggle('hidden', !md);
  if (isZip) {
    $('editor').classList.add('hidden');
    $('editor-preview').classList.remove('hidden');
    setupZipPreview($('editor-preview'), state.file.path, state.file.root, state.file.source || '');
  } else if (isStl) {
    $('editor').classList.add('hidden');
    $('editor-preview').classList.remove('hidden');
    setupStlPreview($('editor-preview'), state.file.path, state.file.root, state.file.source || '');
  } else if (isPdf) {
    $('editor').classList.add('hidden');
    $('editor-preview').classList.remove('hidden');
    setupPdfPreview($('editor-preview'), state.file.path, state.file.root, state.file.source || '');
  } else if (isDxf) {
    $('editor').classList.add('hidden');
    $('editor-preview').classList.remove('hidden');
    setupDxfPreview($('editor-preview'), state.file.path, state.file.root, state.file.source || '');
  } else if (isDocx) {
    $('editor').classList.add('hidden');
    $('editor-preview').classList.remove('hidden');
    setupDocxPreview($('editor-preview'), state.file.path, state.file.root, state.file.source || '');
  } else if (isXlsx) {
    $('editor').classList.add('hidden');
    $('editor-preview').classList.remove('hidden');
    setupXlsxPreview($('editor-preview'), state.file.path, state.file.root, state.file.source || '');
  } else if (isSqlite) {
    $('editor').classList.add('hidden');
    $('editor-preview').classList.remove('hidden');
    setupSqliteViewer($('editor-preview'), state.file.path, state.file.root);
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
document.querySelectorAll('.starter').forEach(b => b.onclick = () => {
    $('prompt').value = b.dataset.prompt;
    setMode(b.dataset.mode || 'chat');
    if (b.dataset.phase) {
      // 问题解决等阶段入口：自动切到工作流并选中对应阶段按钮
      state.workflowPhase = b.dataset.phase;
      document.querySelectorAll('#workflow-phases .phase-btn').forEach(x => x.classList.toggle('selected', x.dataset.phase === b.dataset.phase));
      updatePhaseHint();
    }
    $('prompt').focus();
  });
document.querySelectorAll('[data-close]').forEach(b => b.onclick = () => $(b.dataset.close).close());
function applyReferenceTerminology() {
  const setText = (selector, key) => document.querySelectorAll(selector).forEach(node => { node.dataset.i18n = key; node.textContent = t(key); });
  setText('[data-root="context"] i18n-text', '引用');
  setText('.context-guide i18n-text[data-i18n="也可以从辅助资料中选择参考文档。"]', '也可以从引用中选择参考文档。');
  setText('#source-dialog h2 i18n-text', '添加引用');
  const add = $('source-add');
  add.setAttribute('aria-label', t('新增引用')); add.title = t('新增引用');
  const label = add.querySelector('span'); if (label) label.textContent = t('引用');
}
applyReferenceTerminology();
// 点击 dialog 遮罩关闭弹窗（事件委托，覆盖所有静态及动态 dialog）
document.addEventListener('click', e => { if (e.target.tagName === 'DIALOG' && e.target.open) e.target.close(); });
document.querySelectorAll('[data-root]').forEach(b => b.onclick = action(async () => { rememberFileLocation(); state.root = b.dataset.root; if (state.root === 'context' && !state.source) state.source = defaultContextSourceID(); restoreFileLocation(); document.querySelectorAll('[data-root]').forEach(x => x.classList.toggle('active', x === b)); renderSourceChips(); await loadFiles(); }));
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
async function navigateParentDirectory() {
  state.dir = state.dir.includes('/') ? state.dir.slice(0, state.dir.lastIndexOf('/')) : '.';
  await loadFiles();
}
$('parent-dir').onclick = action(async event => {
  if (Date.now() < fileNavigationGesture.suppressClickUntil) { event?.preventDefault(); event?.stopPropagation(); return; }
  await navigateParentDirectory();
});
$('file-search').addEventListener('input', () => { state.fileSearch = $('file-search').value; clearTimeout(state.fileSearchTimer); state.fileSearchTimer = setTimeout(() => loadFiles().catch(error => toast(error.message || String(error))), 180); });
$('file-search-scope').addEventListener('click', () => { state.fileSearchScope = state.fileSearchScope === 'folder' ? 'recursive' : 'folder'; $('file-search-scope').setAttribute('aria-pressed', String(state.fileSearchScope === 'recursive')); loadFiles().catch(error => toast(error.message || String(error))); });
$('file-search-match').addEventListener('click', () => { state.fileSearchMatch = state.fileSearchMatch === 'fuzzy' ? 'exact' : 'fuzzy'; $('file-search-match').setAttribute('aria-pressed', String(state.fileSearchMatch === 'exact')); loadFiles().catch(error => toast(error.message || String(error))); });
// ── 批量 / 文件夹拖拽上传 ───────────────────────────────────────────────────
// 与后端 internal/server/file_upload_batch.go 对齐的限额（仅前端分块用）。
const BATCH_MAX_FILES = 1000;
const BATCH_MAX_BYTES = 256 * 1024 * 1024;

// collectEntry 递归遍历 FileSystemEntry（文件夹拖拽），输出 {file, relativePath}。
// prefix 累积到当前目录为止的相对路径（含顶层拖拽项自身的名字）。目录 reader 分多批
// 返回条目，必须循环 readEntries 直到空批次。挂到 window 便于 Playwright 用合成 entry 单测。
async function collectEntry(entry, prefix, out) {
  if (entry.isFile) {
    const file = await new Promise((resolve, reject) => entry.file(resolve, reject));
    out.push({ file, relativePath: prefix ? prefix + '/' + file.name : file.name });
    return;
  }
  if (!entry.isDirectory) return;
  const reader = entry.createReader();
  for (;;) {
    const batch = await new Promise((resolve, reject) => reader.readEntries(resolve, reject));
    if (!batch.length) break;
    for (const child of batch) {
      const childPrefix = prefix ? prefix + '/' + entry.name : entry.name;
      await collectEntry(child, childPrefix, out);
    }
  }
}

// collectDroppedItems 优先用 DataTransferItem.webkitGetAsEntry() 递归文件夹；
// 该 API 不可用时回退到扁平 FileList，保证单文件拖拽仍可用。
async function collectDroppedItems(dataTransfer) {
  if (dataTransfer && dataTransfer.items) {
    const entries = [];
    for (const it of dataTransfer.items) {
      const entry = it.webkitGetAsEntry && it.webkitGetAsEntry();
      if (entry) entries.push(entry);
    }
    if (entries.length) {
      const out = [];
      for (const entry of entries) await collectEntry(entry, '', out);
      return out;
    }
  }
  return Array.from((dataTransfer && dataTransfer.files) || []).map(f => ({ file: f, relativePath: f.name }));
}

// showUploadProgress 上传进度/结果覆盖层。纯外部 CSS 类切换（CSP style-src 'self'
// 禁止内联样式），完成后自动隐藏；失败清单通过 up-detail 展示，绝不静默。
function showUploadProgress(total) {
  let host = $('upload-progress');
  if (!host) {
    host = el('div', 'upload-progress');
    host.id = 'upload-progress';
    host.innerHTML = '<div class="up-text"></div><div class="up-detail"></div>';
    document.body.appendChild(host);
  }
  host.classList.remove('hidden', 'up-fail', 'up-done');
  const text = host.querySelector('.up-text');
  const detail = host.querySelector('.up-detail');
  detail.textContent = ''; // 清掉上一次上传遗留的失败明细
  const api = {
    update(processed, current) {
      text.textContent = '上传中 ' + processed + '/' + total + (current ? ' · ' + current : '');
    },
    finish(processed, failedList) {
      const ok = processed - failedList.length;
      if (!failedList.length) {
        text.textContent = '已上传 ' + processed + ' 个文件';
        detail.textContent = '';
        host.classList.add('up-done');
      } else {
        text.textContent = '上传完成：成功 ' + ok + '，失败 ' + failedList.length;
        detail.textContent = failedList.slice(0, 10).map(f => f.path + ' — ' + f.error).join('\n');
        host.classList.add('up-fail');
      }
      clearTimeout(host._t);
      host._t = setTimeout(() => host.classList.add('hidden'), failedList.length ? 12000 : 2500);
    },
  };
  api.update(0, '');
  return api;
}

// uploadCollected 上传已收集的 {file, relativePath} 列表。单文件走原有 raw
// /api/file/upload；多文件走 multipart 批量端点并按后端限额分块。每个失败（冲突/校验/
// 限额/网络）都计入 failed 并展示，网络错误整块计为失败。
async function uploadCollected(items, target) {
  if (!items.length) return { succeeded: 0, failed: [] };
  const overlay = showUploadProgress(items.length);
  const destBase = state.dir === '.' ? '' : state.dir;
  const qsBase = destBase ? encodeURIComponent(destBase) : encodeURIComponent('.');
  const srcQS = target.source ? '&source=' + encodeURIComponent(target.source) : '';
  let processed = 0;
  const failed = [];
  try {
    if (items.length === 1) {
      const it = items[0];
      overlay.update(0, it.relativePath);
      const dest = destBase ? destBase + '/' + it.relativePath : it.relativePath;
      try {
        const resp = await fetch('/api/file/upload?path=' + encodeURIComponent(dest) + srcQS, {
          method: 'POST',
          headers: { Authorization: 'Bearer ' + state.token, 'Content-Type': it.file.type || 'application/octet-stream' },
          body: it.file,
        });
        const data = await resp.json().catch(() => ({}));
        if (!resp.ok) throw new Error(data.error || ('HTTP ' + resp.status));
      } catch (e) { failed.push({ path: it.relativePath, error: e.message }); }
      processed++;
      overlay.update(processed, it.relativePath);
    } else {
      let chunk = [];
      let chunkBytes = 0;
      const flush = async () => {
        if (!chunk.length) return;
        const form = new FormData();
        for (const it of chunk) form.append('files', it.file, it.relativePath);
        const url = '/api/file/upload-batch?path=' + qsBase + srcQS;
        let resp, data;
        try {
          resp = await fetch(url, { method: 'POST', headers: { Authorization: 'Bearer ' + state.token }, body: form });
          data = await resp.json().catch(() => ({}));
        } catch (e) {
          for (const it of chunk) failed.push({ path: it.relativePath, error: String(e) });
          processed += chunk.length;
          overlay.update(processed, '');
          chunk = []; chunkBytes = 0;
          return;
        }
        const results = data.results || [];
        for (const r of results) {
          processed++;
          if (!r.ok) failed.push({ path: r.path || '?', error: r.error || ('HTTP ' + resp.status) });
        }
        if (!results.length) {
          const errMsg = (data && data.error) || ('HTTP ' + resp.status);
          for (const it of chunk) failed.push({ path: it.relativePath, error: errMsg });
          processed += chunk.length;
        }
        overlay.update(processed, '');
        chunk = []; chunkBytes = 0;
      };
      for (const it of items) {
        const size = it.file.size || 0;
        if (chunk.length && (chunk.length >= BATCH_MAX_FILES || chunkBytes + size > BATCH_MAX_BYTES)) await flush();
        chunk.push(it);
        chunkBytes += size;
      }
      await flush();
    }
  } finally {
    await loadFiles();
    overlay.finish(processed, failed);
  }
  return { succeeded: processed - failed.length, failed };
}

// uploadDroppedFiles 兼容旧的扁平 FileList 调用（单文件拖拽入口）。
async function uploadDroppedFiles(files) {
  const target = currentFileTarget();
  if (!target.writable) { toast('当前目录为只读，无法上传'); return; }
  const items = Array.from(files || []).map(f => ({ file: f, relativePath: f.name }));
  if (!items.length) return;
  await uploadCollected(items, target);
}
['dragenter', 'dragover'].forEach(type => $('files').addEventListener(type, event => { event.preventDefault(); if (currentFileTarget().writable) $('files').classList.add('drop-ready'); }));
['dragleave', 'drop'].forEach(type => $('files').addEventListener(type, event => { event.preventDefault(); $('files').classList.remove('drop-ready'); }));
$('files').addEventListener('drop', action(async event => {
  event.preventDefault();
  $('files').classList.remove('drop-ready');
  const items = await collectDroppedItems(event.dataTransfer);
  if (items.length) await uploadCollected(items, currentFileTarget());
}));
$('task-form').onsubmit = action(async event => {
  event.preventDefault();
  if (state.submitting) return; // Enter 连击与点击不可重复创建 run
  if (xiaomiDictation.active || xiaomiDictation.starting) { toast(t('请先停止语音转写，再检查并发送文字')); return; }
  const prompt = $('prompt').value.trim(); if (!prompt) return;
  if (!state.config?.configured) { openSettings(); return; }
  if (state.previewOverLimit) { toast(t("上下文预算超限：请缩短任务或减少附件后再发送")); return; }
  state.submitting = true;
  updateSendEnabled(); // 先显示提交态，再等待创建会话/启动任务请求
  cancelContextPreview(); // 输入防抖请求不再和正式发送争用服务端会话锁
  const draftSession = state.session; // R07：捕获发送时对象，后续等待不得覆盖新选择
  let created = null;
  try {
    if (!draftSession) {
      created = await api('/sessions', { method: 'POST', body: JSON.stringify({ title: t("新会话") }) });
      if (!state.session) state.session = created; // 仅当用户仍停留在空白页时接管；点击已切走的会话不被空壳抢占
    }
    const target = draftSession || created;
    // 小秘会话键盘输入走 assistant-message，由后端按直接文字消息处理，不经过语音环境过滤。
    if (target.kind === 'assistant') {
      try {
        const resp = await api(`/sessions/${target.id}/assistant-message`, { method: 'POST', body: JSON.stringify({ text: prompt }) });
        // locked：弹密码门，不清空输入，解锁后重发
        if (resp.action === 'locked') {
          toast(resp.reason || t('小秘已锁定，请在小秘会话中解锁'));
          openAssistantGate(target.id, target.title || '小秘');
          return;
        }
        $('prompt').value = ''; state.attachments = []; renderAttachments();
        // 转交成功后立即打开后端刚创建的 aide 会话。此前固定回到小秘，
        // 导致新会话虽已出现在侧栏，主区域仍停留在欢迎页或小秘历史。
        await selectSession(resp.dispatched?.sessionId || target.id);
        // 按 action 分流提示
        if (resp.action === 'dispatch' && resp.dispatched) {
          toast(t('已创建会话 #{0}，任务已就绪；检查后发送才会运行', resp.dispatched.number || '?'));
        } else if (resp.action === 'ask') {
          toast(resp.reply || t('小秘想追问'));
        } else if (resp.action === 'silent') {
          toast(t('（已忽略）'));
        } else if (resp.reply) {
          toast(resp.reply);
        } else {
          toast(t('已发送'));
        }
        return;
      } catch (err) { toast(err.message); return; }
    }
    const strategy = state.profiles?.strategy || 'auto';
    // #41：小秘语音经 typeIntoPrompt 提交时，用 analyze 判定的 mode 一次性覆盖手动排队开关
    let queued = state.queueMode;
    if (voice.queuedOverride != null) { queued = voice.queuedOverride; voice.queuedOverride = null; }
    await api(`/sessions/${target.id}/runs`, { method: 'POST', body: JSON.stringify({ prompt, mode: state.mode, attachments: state.attachments, strategy, profile: strategy === 'auto' ? '' : (state.profiles?.activeProfile || 'default'), queued, workflowPhase: state.workflowPhase || '' }) });
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
  } finally { state.submitting = false; updateSendEnabled(); setSendMode(state.busy); }
});
  $('queue-toggle')?.addEventListener('click', () => { state.queueMode = !state.queueMode; $('queue-toggle').classList.toggle('active', state.queueMode); });
  $('prompt').addEventListener('keydown', event => { if (event.key === 'Enter' && event.ctrlKey && !event.isComposing) { event.preventDefault(); $('task-form').requestSubmit(); } });

/* ── R08-04 上下文预览：与真实请求共用服务端构建器，口径如实标注为估算 ── */
state.previewSeq = { value: 0 };
state.previewTimer = 0;
state.previewController = null;
state.previewFingerprint = '';
state.previewOverLimit = false;
function cancelContextPreview() {
  clearTimeout(state.previewTimer);
  ++state.previewSeq.value;
  state.previewController?.abort();
  state.previewController = null;
}
function updateSendEnabled() {
  $('send').disabled = !!state.previewOverLimit || !!state.submitting;
  $('send').classList.toggle('submitting', !!state.submitting);
  $('send').setAttribute('aria-busy', String(!!state.submitting));
  if (state.submitting) {
    $('composer-hint').textContent = t('正在提交…');
  } else if (state.previewOverLimit) {
    $('composer-hint').textContent = t("⚠ 上下文预算超限：请缩短任务或减少附件");
  } else {
    $('composer-hint').textContent = t("Ctrl + Enter 发送 · Enter 换行");
  }
}
function hideContextPreview() {
  state.previewOverLimit = false;
  state.previewFingerprint = '';
  $('context-preview').classList.add('hidden');
  state.contextMeterDraft = false;
  estimateContext();
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
  _ctxTooltip.replaceChildren();
  const _ttStrong = el('strong', '', label);
  _ttStrong.style.color = color;
  _ctxTooltip.append(_ttStrong, document.createElement('br'), document.createTextNode(tokens + ' tokens · ' + pct + '%'));
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

function contextParts(bd) {
  return [
    [t("系统/策略提示"), bd.system, "#6366f1"],
    [t("历史摘要"), bd.summary, "#8b5cf6"],
    [t("历史用户消息"), bd.historyUser, "#0ea5e9"],
    [t("历史助手回复"), bd.historyAssistant, "#06b6d4"],
    [t("工具调用参数"), bd.toolCalls, "#64748b"],
    [t("工具结果"), bd.toolResults, "#f97316"],
    [t("本次用户输入"), bd.prompt, "#22c55e"],
    [t("附件/引用"), bd.attachments, "#f59e0b"],
    [t("阶段指令"), bd.instruction, "#ec4899"],
    [t("工具定义"), bd.toolSchemas, "#64748b"],
    [t("图片附件（保守上界）"), bd.images, "#a855f7"],
    [t("请求协议开销"), bd.protocol, "#94a3b8"],
    [t("上游实际 usage 校准"), bd.usageAdjustment, "#14b8a6"],
  ].filter(([, part]) => part && part.tokens > 0);
}
function contextPartText(part) {
  if (!part) return '';
  return part.bytes > 0
    ? t("{0} 字节 · ≈ {1} tokens", part.bytes, part.tokens)
    : t("≈ {0} tokens", part.tokens);
}

function renderContextPreview(data) {
  if (!data || !data.breakdown) return;
  state.contextPreview = data;
  state.previewFingerprint = data.fingerprint || '';
  state.previewOverLimit = !!data.overLimit;
  const bd = data.breakdown;
  const overText = data.overLimit ? ' · ' + t("⚠ 超限 {0}", Math.max(0, data.totalEstimate - data.contextWindow)) : '';
  $('cp-summary').textContent = t("输入估算 {0} tokens + 输出预留 {1} = {2} / 窗口 {3}", data.inputEstimate, data.outputReserve, data.totalEstimate, data.contextWindow) + overText;
  const detail = $('cp-detail');
  detail.replaceChildren();
  const parts = contextParts(bd);
  parts.forEach(([label, part]) => {
    const row = el('div', 'cp-row');
    row.append(el('span', '', label), el('span', '', contextPartText(part)));
    detail.append(row);
  });
  // 堆叠条按分项 token（而不是字符）显示；每段和标题的输入估算严格同口径。
  const total = parts.reduce((sum, [, part]) => sum + part.tokens, 0) || 1;
  const bar = el('div', 'ctx-bar');
  const cells = [];
  for (const [label, part, color] of parts) {
    const pct = ((part.tokens / total) * 100).toFixed(1);
    const cell = el('div', 'ctx-bar-cell');
    cell.style.width = pct + '%';
    cell.style.background = color;
    cell.dataset.label = label;
    cell.dataset.tokens = String(part.tokens);
    cell.dataset.pct = pct;
    cell.dataset.color = color;
    cell.addEventListener('mouseenter', (e) => {
      cells.forEach(c => { if (c !== cell) c.classList.add('dimmed'); });
      showCtxTooltip(e, label, part.tokens, pct, color);
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
  parts.forEach(([label, part, color], index) => {
    const item = el('div', 'ctx-legend-item');
    item.append(el('span', 'ctx-legend-dot', ''), el('span', '', label + ' · ' + part.tokens + 't'));
    item.querySelector('.ctx-legend-dot').style.background = color;
    item.onclick = () => {
      item.classList.toggle('hidden');
      if (cells[index]) cells[index].style.display = item.classList.contains('hidden') ? 'none' : '';
    };
    legend.append(item);
  });
  detail.append(legend);
  detail.append(el('p', 'cp-note', data.estimationNote || ''));
  $('context-preview').classList.remove('hidden');
  renderContextMeter(data, true);
  updateSendEnabled();
}
async function refreshContextPreview() {
  const seq = ++state.previewSeq.value;
  state.previewController?.abort();
  const controller = new AbortController();
  state.previewController = controller;
  const prompt = $('prompt').value.trim();
  if (!prompt || !state.config?.configured) { hideContextPreview(); return; }
  $('context-preview').classList.remove('hidden');
  $('cp-summary').textContent = t("上下文预算计算中…（估算）");
  try {
    const strategy = state.profiles?.strategy || 'manual';
    const profile = strategy === 'auto' ? '' : (state.profiles?.activeProfile || 'default');
    const data = await api('/context-preview', { method: 'POST', signal: controller.signal, body: JSON.stringify({ sessionId: state.session?.id || '', prompt, mode: state.mode, attachments: state.attachments, strategy, profile, workflowPhase: state.workflowPhase || '' }) });
    if (seq !== state.previewSeq.value) return; // 过期响应不得覆盖新预览（R08-04 草稿失效）
    renderContextPreview(data);
  } catch (error) {
    if (seq !== state.previewSeq.value) return;
    if (error?.name === 'AbortError') return;
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
  } finally {
    if (state.previewController === controller) state.previewController = null;
  }
}
function scheduleContextPreview() {
  clearTimeout(state.previewTimer);
  state.previewController?.abort();
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
function renderApiKeyStatus() {
  const cfg = state.config || {};
  const status = $('apikey-status'); const edit = $('apikey-edit');
  if (!status || !edit) return;
  $('api-key').value = ''; $('clear-key').checked = false;
  status.replaceChildren();
  const hasKey = !!cfg.hasKey; const unlocked = !!cfg.vaultUnlocked; const hasPw = !!cfg.hasPassword;
  if (!hasKey) {
    status.append(el('span', 'ak-badge ak-none', t('未配置')));
    edit.classList.remove('hidden');
    return;
  }
  if (!unlocked && hasPw) {
    status.append(el('span', 'ak-badge ak-locked', t('需解锁')));
    const un = el('button', 'quiet ak-unlock', t('解锁')); un.type = 'button';
    un.onclick = action(async () => {
      const res = await requestMasterAuth({ reason: t('解锁模型 API 密钥') });
      if (res === 'cancel') return;
      if (res !== 'success') { toast(t('验证失败')); return; }
      // 后端 /api/auth/verify 通过后已解锁 vault
      await refreshConfig(); renderApiKeyStatus(); toast(t('保险库已解锁'));
    });
    status.append(un);
    edit.classList.add('hidden');
    return;
  }
  status.append(el('span', 'ak-badge ak-ok', t('已配置')));
  const chg = el('button', 'quiet ak-change', t('更换')); chg.type = 'button';
  chg.onclick = () => { edit.classList.toggle('hidden'); if (!edit.classList.contains('hidden')) $('api-key').focus(); };
  status.append(chg);
  edit.classList.add('hidden');
}
function openSettings() { $('base-url').value = state.config?.baseURL || 'https://api.deepseek.com'; state.modelDraft = { models: JSON.parse(JSON.stringify(state.config?.models || [])), activeModel: state.config?.activeModel || '' }; renderModelList(); renderApiKeyStatus(); $('settings-dialog').showModal(); }
$('settings-button').onclick = openSettings;
$('settings-form').onsubmit = action(async event => { event.preventDefault(); if (!state.modelDraft.models.length) { toast(t("请至少添加一个模型")); return; } await api('/settings', { method: 'PUT', body: JSON.stringify({ baseURL: $('base-url').value.trim(), apiKey: $('api-key').value.trim(), clearKey: $('clear-key').checked, models: state.modelDraft.models, activeModel: state.modelDraft.activeModel }) }); $('api-key').value = ''; $('settings-dialog').close(); await refreshConfig(); toast(t("模型设置已保存，发送任务时会调用当前模型")); if (typeof scheduleContextPreview === 'function') scheduleContextPreview(); });
$('save-file').onclick = action(async () => { const body = { path: state.file.path, content: $('editor').value, hash: state.file.hash }; if (state.file.source) body.source = state.file.source; if (state.file.wsId) body.workspaceId = state.file.wsId; const data = await api('/file', { method: 'PUT', body: JSON.stringify(body) }); state.file.hash = data.hash; state.file.content = $('editor').value; state.file.fresh = false; $('attach-file').disabled = false; toast(t("✓ 已保存")); await loadFiles(); });
$('attach-file').onclick = () => {
  if (state.file.content !== $('editor').value) { toast(t("请先保存修改，再附加到任务")); return; }
  const fp = state.file.path || '';
  const ext = fp.split('.').pop().toLowerCase();
  const att = { root: state.file.root, path: fp }; if (state.file.source) { att.root = 'source'; att.source = state.file.source; }
  // 图片附件：检查模型视觉能力
  if (isImagePath(fp)) {
    const vision = state.config && state.config.vision;
    if (!vision) {
      const rec = (state.config && state.config.visionRecommend || []).join('、');
      toast(t("当前模型不支持图片输入，请切换到支持视觉的模型") + (rec ? "（推荐：" + rec + "）" : ""));
      return;
    }
  }
  if (!state.attachments.some(a => a.root === att.root && a.path === att.path && (a.source || '') === (att.source || ''))) {
    if (state.attachments.length >= 8) { toast(t("最多附加 8 个文件")); return; }
    state.attachments.push(att);
  }
  renderAttachments(); $('editor-dialog').close(); $('prompt').focus();
};
$('new-file').onclick = openNewItemMenu;
$('new-file-form').onsubmit = action(async event => { event.preventDefault(); state.file = { path: $('new-file-path').value.trim(), root: 'workspace', hash: '', content: '', fresh: true }; $('new-file-dialog').close(); showEditor(); });
$('new-folder-form').onsubmit = action(async event => { event.preventDefault(); await api('/directory', { method: 'POST', body: JSON.stringify({ root: 'workspace', parentPath: state.dir, name: $('new-folder-name').value }) }); $('new-folder-dialog').close(); await loadFiles(); });
$('terminal-toggle').onclick = () => { const hidden = $('terminal-body').classList.toggle('hidden'); $('terminal-state').textContent = hidden ? t("展开 ＋") : t("收起 −"); };
$('command').addEventListener('keydown', event => {
  if (event.key !== 'ArrowUp' && event.key !== 'ArrowDown' || !state.commandHistory.length) return;
  event.preventDefault();
  if (event.key === 'ArrowUp') {
    if (state.commandHistoryIndex === state.commandHistory.length) state.commandHistoryDraft = $('command').value;
    state.commandHistoryIndex = Math.max(0, state.commandHistoryIndex - 1);
    $('command').value = state.commandHistory[state.commandHistoryIndex];
  } else {
    state.commandHistoryIndex = Math.min(state.commandHistory.length, state.commandHistoryIndex + 1);
    $('command').value = state.commandHistoryIndex === state.commandHistory.length ? state.commandHistoryDraft : state.commandHistory[state.commandHistoryIndex];
  }
  $('command').setSelectionRange($('command').value.length, $('command').value.length);
});
$('command-form').onsubmit = action(async event => {
  event.preventDefault(); if (state.commandAbort) return; const command = $('command').value.trim(); if (!command) return;
  if (state.commandHistory.at(-1) !== command) state.commandHistory.push(command);
  if (state.commandHistory.length > 100) state.commandHistory.shift();
  state.commandHistoryIndex = state.commandHistory.length;
  state.commandHistoryDraft = '';
  $('command').value = '';
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
    // 不在头部右侧重复显示当前选项文字：分段按钮 + 滑块已表达选中态，
    // 否则「跟随系统」会同时出现在头部右上与激活按钮上，造成重复且遮挡预览。
    value.textContent = '';
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
  if (window.aideUI) {
    const unsub = window.aideUI.subscribe(apply);
    window.addEventListener('resize', apply);
    settingsPanel.refreshers.push({ apply: apply, unsub: unsub });
  }
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

// 通用 number 渲染器：label + <input type=number> + 可选单位/描述；防抖 PUT /settings 自动保存
function renderNumberControl(control) {
  const wrap = el('div', 'settings-control number-control');
  const head = el('div', 'control-label');
  head.append(el('span', '', control.label));
  if (control.unit) head.append(el('span', 'control-value', control.unit));
  const input = el('input', 'number-input');
  input.type = 'number';
  if (control.min != null) input.min = control.min;
  if (control.max != null) input.max = control.max;
  if (control.step != null) input.step = control.step;
  if (control.placeholder != null) input.placeholder = control.placeholder;
  input.setAttribute('aria-label', control.label);
  const fallback = parseInt(control.placeholder, 10);
  const defaults = Number.isFinite(fallback) ? fallback : 60;
  const current = state.config && Number.isFinite(Number(state.config[control.id])) ? Number(state.config[control.id]) : defaults;
  input.value = current;
  let timer = null;
  const clampValue = (raw) => {
    let n = Number(raw);
    if (!Number.isFinite(n)) n = defaults;
    if (control.min != null && n < control.min) n = control.min;
    if (control.max != null && n > control.max) n = control.max;
    const step = Number(control.step);
    if (control.step != null && Number.isFinite(step) && step !== 0) n = Math.round(n / step) * step;
    return n;
  };
  const scheduleSave = () => {
    clearTimeout(timer);
    timer = setTimeout(action(async () => {
      const text = input.value.trim();
      let value;
      if (text === '') {
        value = defaults;
        input.value = value;
        toast(t('已恢复默认值 {0}', value));
      } else {
        const clamped = clampValue(text);
        if (clamped !== Number(text)) input.value = clamped;
        value = clamped;
      }
      await api('/settings', { method: 'PUT', body: JSON.stringify({ [control.id]: value, activeModel: state.config ? state.config.activeModel : '' }) });
      await refreshConfig();
      toast(t('已保存'));
    }), 600);
  };
  input.addEventListener('input', scheduleSave);
  input.addEventListener('change', scheduleSave);
  wrap.append(head, input);
  if (control.description) wrap.append(el('small', '', control.description));
  return wrap;
}
const controlRenderers = { language: renderLanguageControl, 'about-project': renderAboutProject, segmented: renderSegmentedControl, 'profiles-manager': renderProfilesManager, 'token-stats': renderTokenStats, 'sessions-manage': renderSessionsManage, 'permission-manager': renderPermissionManager, number: renderNumberControl, 'system-logs': renderSystemLogsControl };

function renderSystemLogsControl() {
  const wrap = el('div', 'settings-control system-logs-control');
  const toolbar = el('div', 'system-logs-toolbar');
  const filter = el('select');
  filter.setAttribute('aria-label', t('日志级别'));
  [['all', '全部级别'], ['debug', 'DEBUG'], ['info', 'INFO'], ['warn', 'WARN'], ['error', 'ERROR']].forEach(([value, label]) => {
    const option = el('option', '', t(label)); option.value = value; filter.append(option);
  });
  const refresh = el('button', 'quiet', t('刷新'));
  const download = el('button', 'primary', t('下载日志'));
  toolbar.append(filter, refresh, download);
  const status = el('small', 'system-logs-status', t('正在加载日志…'));
  const list = el('div', 'system-logs-list');
  wrap.append(toolbar, status, list);
  let loading = false;
  const load = async () => {
    if (loading) return;
    loading = true; refresh.disabled = true;
    try {
      const data = await api('/system-logs?level=' + encodeURIComponent(filter.value) + '&limit=500');
      list.replaceChildren();
      for (const entry of data.entries || []) {
        const row = el('article', 'system-log-entry level-' + entry.level);
        const meta = el('div', 'system-log-meta');
        meta.append(el('time', '', new Date(entry.time).toLocaleString()), el('span', 'system-log-level', entry.level.toUpperCase()));
        row.append(meta, el('pre', 'system-log-message', entry.message));
        list.append(row);
      }
      status.textContent = data.count ? t('显示 {0} 条日志（最多保留 {1} 条）', data.count, data.capacity) : t('当前筛选下没有日志');
    } catch (error) { status.textContent = t('日志加载失败：{0}', error.message); }
    finally { loading = false; refresh.disabled = false; }
  };
  refresh.onclick = () => action(load)();
  filter.onchange = () => action(load)();
  download.onclick = action(async () => {
    const response = await fetch('/api/system-logs?level=' + encodeURIComponent(filter.value) + '&limit=5000&download=1', { headers: { Authorization: 'Bearer ' + state.token } });
    if (!response.ok) throw new Error(t('日志下载失败'));
    const blob = await response.blob();
    const url = URL.createObjectURL(blob);
    const anchor = el('a'); anchor.href = url; anchor.download = 'aide-system-logs.jsonl'; document.body.append(anchor); anchor.click(); anchor.remove();
    setTimeout(() => URL.revokeObjectURL(url), 1000);
    toast(t('日志已下载'));
  });
  action(load)();
  return wrap;
}

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
    let node;
    try {
      const renderer = controlRenderers[control && control.type];
      if (!renderer) {
        node = el('div', 'settings-control control-unsupported', t('暂不支持的设置项（type={0}）', (control && control.type) || 'unknown'));
      } else {
        node = renderer(control);
      }
    } catch (e) {
      console.warn('render control failed', control && control.id, e);
      node = el('div', 'settings-control control-error', t('该设置项渲染失败（{0}）', (control && (control.id || control.type)) || '?'));
    }
    host.append(node);
  }
}
function renderSettingsSheet() {
  const nav = $('settings-nav');
  const content = $('settings-content');
  // 每次重渲染前退订上一批 segmented 控制的 aideUI 订阅与 resize 监听，避免反复打开累积
  for (const r of settingsPanel.refreshers) {
    try { if (r.unsub) r.unsub(); } catch (_) {}
    window.removeEventListener('resize', r.apply);
  }
  settingsPanel.refreshers = [];
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
  requestAnimationFrame(() => settingsPanel.refreshers.forEach(r => r.apply()));
}
function closeSettingsSheet() {
  setSettingsOpen(false);
  // 鼠标点击打开设置后，关闭时不要再 focus() 触发按钮，否则品牌按钮会残留
  // 3px 蓝色焦点环；blur 让焦点回到 body，键盘 Tab 的 :focus-visible 不受影响。
  if (settingsPanel.trigger) settingsPanel.trigger.blur();
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
    strategy: source?.strategy || 'auto',
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
  { key: 'max_tokens', label: '最大 Tokens', min: 1, max: 65536, step: 1, placeholder: '8192', integer: true },
  { key: 'frequency_penalty', label: '频率惩罚', min: -2, max: 2, step: 0.1, placeholder: '0' },
  { key: 'presence_penalty', label: '存在惩罚', min: -2, max: 2, step: 0.1, placeholder: '0' }
];
profilesManager.refresh = function () {
  this.local = JSON.parse(JSON.stringify(state.profiles || { strategy: 'auto', activeProfile: 'default', profiles: [] }));
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
    del.type = 'button'; del.title = t("删除配置"); del.setAttribute('aria-label', t("删除配置 {0}", profile.name));
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
  const label = (p.strategy === 'auto' ? t("策略 · 自动") : t("策略 · {0}", profileName(p.activeProfile))) + (modelName ? ' · ' + modelName : '');
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
    right.append(strategyMenuOption('model', m.id, m.name, t("{0} · {1}K 上下文", m.id, (m.contextWindow || 65536) / 1024), selected));
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
      toast(t("已切换模型：{0}", modelName));
      return;
    }
    if (kind === 'reasoning') {
      await api('/settings', { method: 'PUT', body: JSON.stringify({ reasoningEffort: value }) });
      closeStrategyMenu();
      await refreshConfig();
      const eff = ({auto:t("自动"),off:t("关闭"),low:t("低"),medium:t("中"),high:t("高")}[value] || value);
      toast(t("已切换推理强度：{0}", eff));
      return;
    }
    if (kind === 'auto') source.strategy = 'auto';
    else { source.strategy = 'manual'; source.activeProfile = value; }
    await saveProfilesFrom(source);
    closeStrategyMenu();
    toast(kind === 'auto' ? t("已切换为自动路由策略") : t("已切换为手动策略 · {0}", profileName(value)));
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
  const priceFields = el('div', 'token-price-fields');
  const priceNote = el('small', 'token-price-note', '');
  wrap.append(head, chips, priceRow, grid, legend, tip, detail);
  const pieWrap = el('div', 'model-pie-wrap');
  pieWrap.innerHTML = `<div class="model-pie-title">${t('模型 Token 占比')}</div><div class="model-pie-body"><svg class="model-pie-svg" viewBox="-82 -12 384 240"></svg></div><div class="model-pie-empty hidden">${t('暂无模型调用数据')}</div>`;
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
    priceRow.classList.toggle('official-pricing', !!pricing.official);
    priceNote.textContent = pricing.official
      ? t("DeepSeek 官方自动计价：输入缓存命中 ¥{0}/百万、未命中 ¥{1}/百万，输出 ¥{2}/百万；按调用时北京时间高峰/空闲费率快照", pricing.priceInCacheHit, pricing.priceInCacheMiss, pricing.priceOut)
      : pricing.deepSeekProvider
        ? t("当前模型未列入 DeepSeek 官网现行价目表，按该模型自定义费率或默认费率估算；历史费用不会重算")
        : t("自定义费率按调用快照计算；未配置模型使用默认刊例估算。历史费用不会重算");
    const cost = data.cost ?? 0;
    // 服务端费率加载完成后同步输入框显示（未聚焦时），避免停留在初始默认值
    const inEl = wrap.querySelector('input[data-price="priceIn"]');
    const outEl = wrap.querySelector('input[data-price="priceOut"]');
    if (inEl && document.activeElement !== inEl) inEl.value = pricing.priceIn;
    if (outEl && document.activeElement !== outEl) outEl.value = pricing.priceOut;
    const callRecords = data.callRecords || [];
    const dayCost = {};
    const dayEstimated = {};
    const dayCache = {};
    callRecords.forEach(c => {
      const k = (c.time || '').slice(0, 10);
      dayCost[k] = (dayCost[k] || 0) + (c.cost || 0);
      if (c.defaulted || c.costEstimated) dayEstimated[k] = (dayEstimated[k] || 0) + (c.cost || 0);
      if (c.cacheKnown) {
        const entry = dayCache[k] || { hit: 0, miss: 0, knownCalls: 0 };
        entry.hit += c.cacheHit || 0; entry.miss += c.cacheMiss || 0; entry.knownCalls++;
        dayCache[k] = entry;
      }
    });
    // R08：费用仅由服务端逐调用记录汇总；未计价历史（旧版汇总）单独提示，不并入费用
    const estimatedCost = data.estimatedCost ?? 0;
    const metrics = head.querySelector('.control-value');
    metrics.replaceChildren();
    const metric = (label, value, caption) => {
      const card = el('div', 'usage-metric');
      card.append(el('span', 'usage-label', label), el('strong', 'usage-value', value), el('small', 'usage-caption', caption));
      return card;
    };
    metrics.append(metric(t("累计用量"), fmtStatTokens(totalsObj.total || 0), t("tokens · {0} 次调用", totalsObj.calls || 0)),
      metric(t("已计价费用"), '¥' + cost.toFixed(2), t("按调用时刻的费率快照")));
    if (estimatedCost > 0) metrics.append(metric(t("刊例价估算"), '¥' + estimatedCost.toFixed(2), t("与已计价费用分开统计")));
    chips.replaceChildren();
    chips.append(
      el('span', 'token-chip', todayStats.priced !== false ? t("今日 {0} tokens · ¥{1}", fmtStatTokens(todayStats.total || 0), (dayCost[Object.keys(days).sort().pop()] || 0).toFixed(2)) : t("今日 {0} tokens · 未计价", fmtStatTokens(todayStats.total || 0))),
      el('span', 'token-chip', t("调用 {0} 次", totalsObj.calls || 0))
    );
    Object.entries(data.modelCost || {}).forEach(([model, mc]) => {
      const chip = el('span', 'token-chip', model + ' ¥' + mc.toFixed(2));
      chip.title = t("该模型逐调用计价快照合计");
      chips.append(chip);
    });
    if (unpriced.total) {
      const chip = el('span', 'token-chip', t("未计价历史 {0} tokens · {1} 次", fmtStatTokens(unpriced.total), unpriced.calls || 0));
      chip.title = t("旧版统计没有逐调用与计价证据，费用未知；未按当前费率冒充已发生费用");
      chips.append(chip);
    }
    // 模型 Token 占比饼图
    const pm = data.perModel || {};
    const pmEntries = Object.entries(pm).sort((x, y) => (y[1].total || 0) - (x[1].total || 0));
    const pmTotal = pmEntries.reduce((s, e) => s + (e[1].total || 0), 0);
    const pieSvg = pieWrap.querySelector('.model-pie-svg');
    const pieEmpty = pieWrap.querySelector('.model-pie-empty');
    pieSvg.innerHTML = '';
    if (pmEntries.length === 0 || pmTotal === 0) {
      pieSvg.classList.add('hidden');
      pieEmpty.classList.remove('hidden');
    } else {
      pieSvg.classList.remove('hidden');
      pieEmpty.classList.add('hidden');
      const colors = ['#3b82f6','#f59e0b','#8b5cf6','#10b981','#ef4444','#06b6d4','#ec4899','#84cc16','#f97316','#6366f1'];
      const NS = 'http://www.w3.org/2000/svg';
      const cx = 112, cy = 116, r = 56;   // 饼心与半径
      const r2 = r + 6;                    // 引导线径向外延(短)
      const railR = cx + r + 18;           // 右侧标签列 rail x
      const railL = cx - r - 18;           // 左侧标签列 rail x
      // <1% 的极小扇区合并为“其他”，tooltip 保留明细
      const MIN_PCT = 0.01;
      const rows = [];
      let otherDetail = null, otherTotal = 0, otherCalls = 0;
      pmEntries.forEach(([model, stats]) => {
        const frac = (stats.total || 0) / pmTotal;
        if (frac < MIN_PCT) {
          otherTotal += stats.total || 0;
          otherCalls += stats.calls || 0;
          (otherDetail = otherDetail || []).push({ model, total: stats.total || 0, calls: stats.calls || 0, pct: frac * 100 });
        } else {
          rows.push({ model, stats, frac });
        }
      });
      if (otherDetail) rows.push({ model: t("其他"), stats: { total: otherTotal, calls: otherCalls, _detail: otherDetail }, frac: otherTotal / pmTotal, _other: true });
      const slices = [];
      let angle = -Math.PI / 2;
      rows.forEach((row, i) => {
        const frac = row.frac;
        const sweep = frac * Math.PI * 2;
        const color = colors[i % colors.length];
        const isFull = frac >= 0.999;
        let sliceEl;
        if (isFull) {
          // 单模型 100%：画整圆
          sliceEl = document.createElementNS(NS, 'circle');
          sliceEl.setAttribute('cx', cx); sliceEl.setAttribute('cy', cy); sliceEl.setAttribute('r', r);
        } else {
          const x1 = cx + r * Math.cos(angle), y1 = cy + r * Math.sin(angle);
          const x2 = cx + r * Math.cos(angle + sweep), y2 = cy + r * Math.sin(angle + sweep);
          const large = sweep > Math.PI ? 1 : 0;
          sliceEl = document.createElementNS(NS, 'path');
          sliceEl.setAttribute('d', `M${cx},${cy} L${x1},${y1} A${r},${r} 0 ${large} 1 ${x2},${y2} Z`);
        }
        sliceEl.setAttribute('fill', color);
        sliceEl.setAttribute('class', 'pie-slice');
        sliceEl.setAttribute('data-model', row.model);
        pieSvg.appendChild(sliceEl);
        // 引导线中点角度：整圆固定取正右(0rad)
        const mid = isFull ? 0 : angle + sweep / 2;
        const side = Math.cos(mid) >= 0 ? 1 : -1;   // 1=右 -1=左
        const ax = cx + r * Math.cos(mid), ay = cy + r * Math.sin(mid);        // 扇区边缘点
        const bx = cx + r2 * Math.cos(mid), by = cy + r2 * Math.sin(mid);     // 径向短线端点
        slices.push({ ...row, color, side, sliceEl, ax, ay, bx, by,
          railX: side === 1 ? railR : railL, labelY: by });
        angle += sweep;
      });
      // 同侧标签纵向碰撞消解：保持角度序(即自然y序)，前向下压+后向上抬拉开最小间距，
      // 再 clamp 到上下边界并整体垂直居中；序不变 => 径向/rail 两端同序 => 引导线不交叉
      const minGap = 18;
      const topBound = 4, bottomBound = 222;
      [1, -1].forEach(sd => {
        const g = slices.filter(s => s.side === sd).sort((a, b) => a.labelY - b.labelY);
        if (!g.length) return;
        for (let k = 1; k < g.length; k++) {
          if (g[k].labelY - g[k-1].labelY < minGap) g[k].labelY = g[k-1].labelY + minGap;
        }
        for (let k = g.length - 2; k >= 0; k--) {
          if (g[k+1].labelY - g[k].labelY < minGap) g[k].labelY = g[k+1].labelY - minGap;
        }
        if (g[0].labelY < topBound) g.forEach(s => s.labelY += topBound - g[0].labelY);
        if (g[g.length-1].labelY > bottomBound) g.forEach(s => s.labelY -= g[g.length-1].labelY - bottomBound);
        const shift = cy - (g[0].labelY + g[g.length-1].labelY) / 2;
        g.forEach(s => s.labelY += shift);
        if (g[0].labelY < topBound) g.forEach(s => s.labelY += topBound - g[0].labelY);
        if (g[g.length-1].labelY > bottomBound) g.forEach(s => s.labelY -= g[g.length-1].labelY - bottomBound);
      });
      // 绘制引导线(polyline: 边缘点->径向短线->标签rail) + 边缘圆点 + 两行文字标签
      slices.forEach(s => {
        const grp = document.createElementNS(NS, 'g');
        grp.setAttribute('class', 'pie-label-group');
        grp.setAttribute('data-model', s.model);
        const poly = document.createElementNS(NS, 'polyline');
        poly.setAttribute('points', `${s.ax},${s.ay} ${s.bx},${s.by} ${s.railX},${s.labelY}`);
        poly.setAttribute('fill', 'none');
        poly.setAttribute('stroke', s.color);
        poly.setAttribute('stroke-width', '1');
        poly.setAttribute('class', 'pie-leader');
        grp.appendChild(poly);
        const dot = document.createElementNS(NS, 'circle');
        dot.setAttribute('cx', s.ax); dot.setAttribute('cy', s.ay);
        dot.setAttribute('r', '1.6'); dot.setAttribute('fill', s.color);
        grp.appendChild(dot);
        const anchor = s.side === 1 ? 'start' : 'end';
        const tx = s.railX + s.side * 4;
        const name = document.createElementNS(NS, 'text');
        name.setAttribute('x', tx); name.setAttribute('y', s.labelY - 2);
        name.setAttribute('text-anchor', anchor);
        name.setAttribute('class', 'pie-label-name');
        const label = s.model.length > 20 ? s.model.slice(0, 18) + '…' : s.model;
        name.textContent = label;
        grp.appendChild(name);
        const sub = document.createElementNS(NS, 'text');
        sub.setAttribute('x', tx); sub.setAttribute('y', s.labelY + 10);
        sub.setAttribute('text-anchor', anchor);
        sub.setAttribute('class', 'pie-label-sub');
        sub.textContent = fmtStatTokens(s.stats.total || 0) + ' · ' + (s.frac * 100).toFixed(1) + '%';
        sub.title = s._other && s.stats._detail
          ? t("其他 {0} 个模型", s.stats._detail.length) + '\n' + s.stats._detail.map(d => `${d.model} · ${fmtStatTokens(d.total)} · ${d.pct.toFixed(1)}%`).join('\n')
          : t("{0}: {1} tokens · {2} 次调用 · {3}%", s.model, s.stats.total || 0, s.stats.calls || 0, (s.frac * 100).toFixed(1));
        name.title = sub.title;
        grp.appendChild(sub);
        s.grp = grp;
        pieSvg.appendChild(grp);
      });
      // hover 高亮：扇区 + 引导线 + 标签联动
      slices.forEach(s => {
        const on = () => { s.sliceEl.setAttribute('opacity', '0.8'); if (s.grp) s.grp.classList.add('active'); };
        const off = () => { s.sliceEl.setAttribute('opacity', '1'); if (s.grp) s.grp.classList.remove('active'); };
        s.sliceEl.addEventListener('mouseenter', on);
        s.sliceEl.addEventListener('mouseleave', off);
        if (s.grp) {
          s.grp.addEventListener('mouseenter', on);
          s.grp.addEventListener('mouseleave', off);
        }
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
        const chip = el('span', 'token-chip balance', t("余额 {0}", parts));
        chip.title = t("来自 API 的账户余额");
        chips.append(chip);
      } catch (error) {
        const chip = el('span', 'token-chip', t("余额不可查"));
        chip.title = t("查询失败: {0}", error.message);
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
      const fee = pricedDay ? t("费用 ¥{0}", (dayCost[date] || 0).toFixed(4)) : t("费用未知（旧数据未计价）");
      tip.append(
        el('strong', '', date + ' · ' + fmtStatTokens(day.total || 0) + ' tokens'),
        el('br'),
        el('span', '', t("输入 {0} · 输出 {1}", fmtStatTokens(day.prompt || 0), fmtStatTokens(day.completion || 0))),
        el('br'),
        el('span', '', t("调用 {0} 次 · {1}{2}", day.calls || 0, fee, day.estimated ? t("（用量为估算）") : '')),
        el('br'),
        el('span', '', t("所在周合计 {0} tokens", fmtStatTokens(weekTotal)))
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
        cell.setAttribute('aria-label', t("{0} · {1} tokens，查看当日明细", key, fmtStatTokens(day.total || 0)));
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
          const fee = pricedDay ? t("费用 ¥{0}（调用快照；其中估算 ¥{1}）", (dayCost[key] || 0).toFixed(4), (dayEstimated[key] || 0).toFixed(4)) : t("费用未知：旧数据没有逐调用与计价证据，未按当前费率冒充");
          detail.append(
            el('strong', '', key),
            el('span', '', t("输入 {0} tokens · 输出 {1} tokens", fmtStatTokens(day.prompt || 0), fmtStatTokens(day.completion || 0))),
            el('span', '', t("调用 {0} 次 · 合计 {1} tokens{2}", day.calls || 0, fmtStatTokens(day.total || 0), day.estimated ? t("（用量为估算）") : '')),
            el('span', '', fee)
          );
          if (dayCache[key]) detail.append(el('span', '', t("缓存命中 {0} · 未命中 {1} tokens（{2} 次调用有缓存明细）", fmtStatTokens(dayCache[key].hit), fmtStatTokens(dayCache[key].miss), dayCache[key].knownCalls)));
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
    legend.append(el('span', '', t("多")), el('small', '', pricing.official ? t("按 DeepSeek 官方费率与调用用量计算 · 悬停查看明细") : t("计价可配置 · 悬停查看明细")));
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
          toast(t("费率保存失败: {0}", error.message));
          action(loadStats).call(null);
        }
      })();
    });
    lab.append(input);
    return lab;
  };
  priceFields.append(mkPrice('priceIn', t("输入 ¥/百万")), mkPrice('priceOut', t("输出 ¥/百万")));
  priceRow.append(priceFields, priceNote);
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
      btn.title = t("{0} 上下文", p.label);
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
    // Preset values like 1,000,000 are valid integer windows but are not
    // multiples of 1024; a 1024 step makes the browser block form submission.
    windowInput.step = 1;
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
    toast(t("已获取 {0} 个可用模型，在输入框中选择即可", (data.models || []).length));
  } finally {
    button.disabled = false;
    button.textContent = t("⟳ 自动获取");
  }
});
/* ── 上下文统计：与 /context-preview 共用服务端请求构造，避免浏览器自行猜测。 ── */
function fmtTokens(n) { return n < 1000 ? String(n) : (n / 1024).toFixed(1) + 'K'; }
state.contextMeterSeq = state.contextMeterSeq || { value: 0 };
state.contextMeterKey = state.contextMeterKey || '';
state.contextMeterDraft = state.contextMeterDraft || false;

function ensureContextMeterControls() {
  const card = $('context-card');
  if (!card) return;
  const bar = $('context-bar');
  if (bar && !$('context-card-detail')) {
    const detail = el('div', 'context-card-detail');
    detail.id = 'context-card-detail';
    card.append(detail);
  }
}
function contextMeterKey() {
  const messages = state.session?.messages || [];
  const signature = messages.map(m => [m.role, (m.content || '').length, (m.toolCalls || []).map(c => (c.function?.arguments || '').length).join(',')].join(':')).join('|');
  return [state.session?.id || 'new', state.config?.model || '', state.mode || 'chat', state.workflowPhase || '', state.profiles?.strategy || 'auto', state.profiles?.activeProfile || 'default', signature].join('~');
}
function renderContextMeter(data, isDraft) {
  if (!data || !data.breakdown) return;
  ensureContextMeterControls();
  state.contextMeter = data;
  state.contextMeterDraft = !!isDraft;
  const limit = data.contextWindow || activeModel()?.contextWindow || 65536;
  const pressure = data.totalEstimate || data.inputEstimate || 0;
  const pct = Math.min(100, Math.round((pressure / limit) * 100));
  $('context-stat').textContent = fmtTokens(data.inputEstimate || 0) + ' / ' + fmtTokens(limit);
  const bar = $('context-bar');
  bar.replaceChildren();
  bar.setAttribute('aria-label', t("下一次请求的上下文估算：{0} / 窗口 {1}", pressure, limit));
  const detail = $('context-card-detail');
  if (!detail) return;
  detail.replaceChildren();
  const parts = contextParts(data.breakdown);
  parts.forEach(([label, part, color]) => {
    const segment = el('span', 'context-segment');
    const share = Math.max(0, part.tokens) / limit * 100;
    segment.style.width = share + '%';
    segment.style.backgroundColor = color;
    segment.tabIndex = 0;
    segment.setAttribute('role', 'img');
    const pctOfWindow = (part.tokens / limit * 100).toFixed(1);
    const tooltip = (event) => showCtxTooltip(event, label + (isDraft ? ' · ' + t("当前草稿 · 估算") : ' · ' + t("估算")), part.tokens.toLocaleString(), pctOfWindow, color);
    segment.setAttribute('aria-label', t("{0}：估算 {1} tokens，占窗口 {2}%", label, part.tokens, pctOfWindow));
    segment.title = t("{0} · 估算 {1} tokens · 窗口 {2}%", label, part.tokens, pctOfWindow);
    segment.addEventListener('mouseenter', tooltip);
    segment.addEventListener('mousemove', moveCtxTooltip);
    segment.addEventListener('mouseleave', hideCtxTooltip);
    segment.addEventListener('focus', (event) => {
      const rect = event.currentTarget.getBoundingClientRect();
      tooltip({ clientX: rect.left + rect.width / 2, clientY: rect.top + rect.height / 2 });
    });
    segment.addEventListener('blur', hideCtxTooltip);
    bar.append(segment);
  });
  if ((data.outputReserve || 0) > 0) {
    const reserve = el('span', 'context-segment context-segment-reserve');
    reserve.style.width = Math.max(0, data.outputReserve) / limit * 100 + '%';
    reserve.tabIndex = 0;
    reserve.setAttribute('role', 'img');
    reserve.setAttribute('aria-label', t("输出预留：估算 {0} tokens", data.outputReserve));
    reserve.title = t("输出预留 · {0} tokens", data.outputReserve);
    const showReserve = (event) => showCtxTooltip(event, t("输出预留"), data.outputReserve.toLocaleString(), (data.outputReserve / limit * 100).toFixed(1), '#475569');
    reserve.addEventListener('mouseenter', showReserve);
    reserve.addEventListener('mousemove', moveCtxTooltip);
    reserve.addEventListener('mouseleave', hideCtxTooltip);
    reserve.addEventListener('focus', (event) => { const rect = event.currentTarget.getBoundingClientRect(); showReserve({ clientX: rect.left + rect.width / 2, clientY: rect.top + rect.height / 2 }); });
    reserve.addEventListener('blur', hideCtxTooltip);
    bar.append(reserve);
  }
  bar.classList.toggle('warn', pct > 90);
  const runs = state.session?.runs || [];
  const lastRun = [...runs].reverse().find(r => r.usage && r.usage.total > 0);
  if (lastRun) {
    const usage = lastRun.usage;
    const source = usage.estimated ? t("最近任务累计（估算）") : t("最近任务累计（上游实际）");
    const promptTokens = usage.estimated ? fmtTokens(usage.prompt || 0) : (usage.prompt || 0).toLocaleString();
    const completionTokens = usage.estimated ? fmtTokens(usage.completion || 0) : (usage.completion || 0).toLocaleString();
    const actual = el('p', 'context-card-actual', t("{0}：输入 {1} · 输出 {2}", source, promptTokens, completionTokens));
    actual.title = t("{0}：输入 {1} tokens · 输出 {2} tokens", source, usage.prompt || 0, usage.completion || 0);
    detail.append(actual);
  }
  detail.title = data.estimationNote || '';
}
async function refreshContextMeter() {
  if (!state.config?.configured || state.contextMeterDraft) return;
  const key = contextMeterKey();
  if (key === state.contextMeterKey && state.contextMeter) { renderContextMeter(state.contextMeter, false); return; }
  state.contextMeterKey = key;
  const seq = ++state.contextMeterSeq.value;
  const strategy = state.profiles?.strategy || 'auto';
  const profile = strategy === 'auto' ? '' : (state.profiles?.activeProfile || 'default');
  try {
    const data = await api('/context-preview', { method: 'POST', body: JSON.stringify({ sessionId: state.session?.id || '', prompt: '', mode: state.mode || 'chat', strategy, profile, workflowPhase: state.workflowPhase || '', baseline: true }) });
    if (seq !== state.contextMeterSeq.value || state.contextMeterKey !== key || state.contextMeterDraft) return;
    renderContextMeter(data, false);
  } catch (_) {
    // 统计失败不影响正常对话；不要用旧的浏览器猜测值冒充服务端口径。
  }
}
function estimateContext() {
  if (!state.config?.configured) {
    $('context-stat').textContent = '—';
    $('context-bar').replaceChildren();
    $('context-card-detail')?.replaceChildren();
    return;
  }
  refreshContextMeter();
}
/* ── 插件系统（FR-72~75，协议 docs/plugin-protocol.md）：右侧面板 + 上传/搜索/启停/删除/surface ── */
async function loadPluginsPanel() {
  const data = await api('/plugins');
  state.plugins = data.plugins || [];
  renderPluginList();
  const surface = await api('/plugin-surface');
  renderPluginSurface(surface.plugins || []);
}
const PLUGIN_I18N = {
  "技能": { en: "Skill" },
  "目标": { en: "Goal" },
  "计划": { en: "Plan" },
  "任务清单": { en: "Todo" },
  "反馈": { en: "Feedback" },
  "子代理": { en: "Sub-agent" },
  "终端": { en: "Terminal" },
  "工作流": { en: "Workflow" },
  "环境说明": { en: "Environment guide" },
  "SQLite 数据库": { en: "SQLite database" },
  "TCP 通讯": { en: "TCP communication" },
  "UDP 通讯": { en: "UDP communication" }
};
function trPlugin(text) {
  if (!text) return text;
  return t(text);
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
    head.append(el('span', 'plugin-name', trPlugin(p.name)), el('span', 'plugin-badge', p.id + (p.version ? ' · v' + p.version : '')));
    const del = el('button', 'plugin-delete', '－');
    del.type = 'button'; del.title = t("删除插件");
    del.onclick = () => { if (confirm(t("删除插件「{0}」？", trPlugin(p.name)))) action(async () => { await api('/plugins/' + encodeURIComponent(p.id), { method: 'DELETE' }); await loadPluginsPanel(); toast(t("插件已删除")); })(); };
    head.append(del);
    card.append(head);
    if (p.description) card.append(el('p', 'plugin-desc', trPlugin(p.description)));
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
    box.append(el('strong', '', t(e.name)));
    if (e.error) { box.append(el('p', 'task-error', '⚠ ' + e.error)); host.append(box); continue; }
    const chips = el('div', 'surface-chips');
    for (const t of e.tools || []) chips.append(el('span', 'surface-chip tool', '⚒ ' + t.name));
    for (const sl of e.slots || []) chips.append(el('span', 'surface-chip slot', '▦ ' + t(sl.name)));
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
  $('workspace-summary').textContent = w.mode === 'ssh' ? (w.host || t("远程")) + ' · SSH/SFTP' : t("{0} · 本地", state.config?.workspaceDisplay || '/workspace');
  $('command-mode').textContent = w.mode === 'ssh' ? 'SSH · ' + (w.host || t("未配置主机")) : t("本地");
}
function setWsMode(mode) {
  document.querySelectorAll('.ws-seg:not(.ws-auth) [data-mode]').forEach(b => { const on = b.dataset.mode === mode; b.classList.toggle('active', on); b.setAttribute('aria-pressed', String(on)); });
  $('ws-local-fields').classList.toggle('hidden', mode !== 'local');
  $('ws-ssh-fields').classList.toggle('hidden', mode !== 'ssh');
  $('ws-test-row').classList.toggle('hidden', mode !== 'ssh');
  if (mode !== 'ssh') $('ws-test-status').textContent = '';
  syncWorkspaceLocationControls();
}

async function showFileProperties(file) {
  const data = await api('/file/properties?root=workspace&path=' + encodeURIComponent(file.path));
  const list = $('file-properties-list'); list.replaceChildren();
  [[t('名称'), data.name], [t('类型'), data.type], [t('相对路径'), data.path], [t('大小'), data.dir ? t('—') : formatFileSize(Number(data.size || 0))], [t('修改时间'), data.modified || t('未知')]].forEach(([key, value]) => {
    list.append(el('dt', '', key), el('dd', '', String(value)));
  });
  $('file-properties-dialog').showModal();
}
function formatFileSize(bytes) {
  if (!Number.isFinite(bytes) || bytes < 1024) return Math.max(0, bytes || 0) + ' B';
  const units = ['KB', 'MB', 'GB', 'TB']; let value = bytes / 1024, index = 0;
  while (value >= 1024 && index < units.length - 1) { value /= 1024; index++; }
  return value.toFixed(value >= 10 ? 0 : 1) + ' ' + units[index];
}
async function deleteFileEntry(file) {
  const kind = file.dir ? t('文件夹') : t('文件');
  if (!window.confirm(t('确定永久删除{0}“{1}”？空文件夹才能删除。', kind, file.name))) return;
  await api('/file/delete', { method: 'POST', body: JSON.stringify({ root: 'workspace', path: file.path }) });
  if (state.file?.path === file.path) $('editor-dialog').close();
  toast(t('已删除：{0}', file.name));
  await loadFiles();
}
function openNewItemMenu() {
  closeFileContextMenu();
  const anchor = $('new-file'), rect = anchor.getBoundingClientRect();
  const m = el('div', 'file-ctx-menu');
  const file = el('button', 'file-ctx-item', t('新建文件'));
  const folder = el('button', 'file-ctx-item', t('新建文件夹'));
  file.onclick = () => { closeFileContextMenu(); $('new-file-path').value = state.dir === '.' ? '' : state.dir + '/'; $('new-file-dialog').showModal(); };
  folder.onclick = () => { closeFileContextMenu(); $('new-folder-name').value = ''; $('new-folder-dialog').showModal(); $('new-folder-name').focus(); };
  m.append(file, folder); document.body.append(m); fileCtxMenuEl = m;
  m.style.left = Math.min(rect.left, innerWidth - 160) + 'px'; m.style.top = (rect.bottom + 4) + 'px';
  setTimeout(() => { document.addEventListener('click', closeFileContextMenu); document.addEventListener('keydown', onFileCtxKey); }, 0);
}
function setWsAuth(auth) {
  document.querySelectorAll('.ws-auth [data-auth]').forEach(b => { const on = b.dataset.auth === auth; b.classList.toggle('active', on); b.setAttribute('aria-pressed', String(on)); });
  $('ws-password-field').classList.toggle('hidden', auth !== 'password');
  $('ws-key-field').classList.toggle('hidden', auth !== 'key');
}
function syncWorkspaceLocationControls() {
  const ssh = document.querySelector('.ws-seg:not(.ws-auth) [data-mode].active')?.dataset.mode === 'ssh';
  for (const [field, location] of [['docs-path', 'docs-location'], ['cache-path', 'cache-location']]) {
    if (!ssh && $(location).value === 'workspace') $(location).value = 'local';
    const remote = ssh && $(location).value === 'workspace';
    $(field).placeholder = remote ? (field === 'cache-path' ? t('相对远程工作区的缓存目录；留空 = .cache') : t('相对远程工作区的目录；留空 = 工作区根目录')) : (field === 'docs-path' ? '/context' : '.cache');
  }
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
  $('ws-password').value = ''; $('ws-key').value = ''; $('ws-passphrase').value = ''; $('ws-vault-password').value = '';
  $('ws-password').placeholder = c.hasPassword ? t("已保存密码；留空保留") : t("设置远程密码");
  $('ws-key').placeholder = c.hasKey ? t("已保存私钥；留空保留") : t("粘贴私钥内容");
  $('ws-key-refpath').value = c.workspace?.keyRefPath || '';
  const km = c.workspace?.keyMode || 'paste';
  setWsKeyInput(km === 'ref' || km === 'copy' ? 'file' : 'paste');
  setWsKeyMode(km === 'copy' ? 'copy' : 'ref');
  const fpEl = $('ws-key-fingerprint');
  if (c.keyFingerprint) { fpEl.textContent = t("公钥指纹：") + c.keyFingerprint; fpEl.classList.remove('hidden'); }
  else { fpEl.textContent = ''; fpEl.classList.add('hidden'); }
  const hint = $('ws-vault-hint');
  const vpField = $('ws-vault-password-field');
  if (!c.hasAccountPassword) { hint.textContent = ''; hint.classList.add('hidden'); vpField.classList.add('hidden'); }
  else if (c.vaultLocked) {
    hint.textContent = t("凭证保险库已锁定：保存前请验证身份解锁"); hint.classList.remove('hidden'); vpField.classList.remove('hidden');
    vpField.querySelector('.ws-vault-fp')?.remove();
    if (state.config && state.config.hasPlatformCredential) {
      const fp = el('button', 'quiet ws-vault-fp'); fp.type = 'button'; fp.textContent = t('指纹解锁');
      fp.onclick = action(async () => {
        const res = await requestMasterAuth({ reason: t('解锁凭证保险库') });
        if (res === null) return;
        if (typeof res === 'string' && res !== 'success') { $('ws-vault-password').value = res; }
        await refreshConfig(); await fillWorkspaceSheet();
      });
      vpField.append(fp);
    }
  }
  else { hint.textContent = ''; hint.classList.add('hidden'); vpField.classList.add('hidden'); }
  $('docs-path').value = c.docs?.path || '';
  $('docs-location').value = c.docs?.location || 'local';
  $('cache-path').value = c.cache?.path || '';
  $('cache-location').value = c.cache?.location || 'local';
  syncWorkspaceLocationControls();
  $('ws-clear-secrets').checked = false;
  renderWsRecent('ws-recent', c.recent?.workspace || []);
  renderWsRecent('docs-recent', c.recent?.docs || []);
  renderWsRecent('cache-recent', c.recent?.cache || []);
  $('ws-test-status').textContent = '';
}
function collectWsConfig() {
  const mode = document.querySelector('.ws-seg:not(.ws-auth) [data-mode].active')?.dataset.mode || 'local';
  const auth = document.querySelector('.ws-auth [data-auth].active')?.dataset.auth || 'password';
  const keyAuth = auth === 'key';
  return {
    workspace: { mode, path: mode === 'local' ? $('ws-path').value.trim() : $('ws-remote-path').value.trim(), host: $('ws-host').value.trim(), port: parseInt($('ws-port').value, 10) || 22, username: $('ws-user').value.trim(), auth },
    docs: { path: $('docs-path').value.trim(), location: $('docs-location').value },
    cache: { path: $('cache-path').value.trim(), location: $('cache-location').value },
    password: $('ws-password').value,
    key: $('ws-key').value,
    passphrase: $('ws-passphrase').value,
    keyMode: keyAuth ? (document.querySelector('#ws-key-file [data-keymode].active')?.dataset.keymode || 'ref') : '',
    keyRefPath: keyAuth ? $('ws-key-refpath').value.trim() : '',
    vaultPassword: $('ws-vault-password').value,
    clearPassword: $('ws-clear-secrets').checked,
    clearKey: $('ws-clear-secrets').checked,
    clearPassphrase: $('ws-clear-secrets').checked
  };
}
async function saveWorkspaceConfig() {
  wsState.config = await api('/workspace-config', { method: 'PUT', body: JSON.stringify(collectWsConfig()) });
  renderWorkspaceSummary();
  fillWorkspaceSheet();
  state.dir = '.';
  await refreshConfig();
  await loadSourcesList();
  try {
    await loadFiles();
  } catch (error) {
    // 系统文档是可选的叠加来源。它的本地路径失效时，不能把保存工作空间
    // （尤其是随后的 SSH/SFTP 连通性测试）整体判为失败；退回恒定可用的 /context，
    // 但只处理这一种确定情形，其他文件面板错误仍照常上抛。
    if (state.root !== 'context' || state.source !== 'system-docs') throw error;
    state.source = 'context';
    state.dir = '.';
    await loadFiles();
    toast(t('自动系统文档路径不可访问：{0}；已切换到内置引用。', error.message));
  }
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
$('ws-test').onclick = action(async () => {
  const status = $('ws-test-status');
  if ((document.querySelector('.ws-seg:not(.ws-auth) [data-mode].active')?.dataset.mode || 'local') !== 'ssh') {
    status.textContent = t('仅 SSH/SFTP 工作空间需要测试连接');
    return;
  }
  $('ws-test').disabled = true; status.textContent = t('正在保存配置并测试 SSH/SFTP 连接…');
  try {
    // 测试使用当前输入的主机、认证和目录，而非上一次保存的旧配置。保存后仍留在
    // 弹窗中，便于根据失败原因立刻修改重试；测试本身不会写入远端。
    await saveWorkspaceConfig();
    const result = await api('/workspace-config/test', { method: 'POST', body: '{}' });
    status.textContent = t('✓ SSH/SFTP 连接成功：{0}', result.path || t('远程目录'));
  } catch (error) {
    status.textContent = t('连接失败：{0}', error.message);
  } finally { $('ws-test').disabled = false; }
});
document.querySelectorAll('.ws-seg:not(.ws-auth) [data-mode]').forEach(b => b.onclick = () => setWsMode(b.dataset.mode));
document.querySelectorAll('.ws-auth [data-auth]').forEach(b => b.onclick = () => setWsAuth(b.dataset.auth));
$('docs-location').onchange = syncWorkspaceLocationControls;
$('cache-location').onchange = syncWorkspaceLocationControls;
function setWsKeyInput(which) {
  document.querySelectorAll('.ws-keyinput [data-keyinput]').forEach(b => { const on = b.dataset.keyinput === which; b.classList.toggle('active', on); b.setAttribute('aria-pressed', String(on)); });
  $('ws-key-paste').classList.toggle('hidden', which !== 'paste');
  $('ws-key-file').classList.toggle('hidden', which !== 'file');
}
function setWsKeyMode(mode) {
  document.querySelectorAll('#ws-key-file [data-keymode]').forEach(b => { const on = b.dataset.keymode === mode; b.classList.toggle('active', on); b.setAttribute('aria-pressed', String(on)); });
}
document.querySelectorAll('.ws-keyinput [data-keyinput]').forEach(b => b.onclick = () => setWsKeyInput(b.dataset.keyinput));
document.querySelectorAll('#ws-key-file [data-keymode]').forEach(b => b.onclick = () => setWsKeyMode(b.dataset.keymode));
$('ws-key-browse').onclick = () => openBrowse('ws-key-refpath');
/* 目录选择器：按宿主机真实目录浏览（root=local），显示与回填均为本机路径 */
function hostPathOf(dir) {
  const base = state.config?.hostLocal || '/local';
  if (dir === '.' || dir === '') return base;
  // Windows 的宿主机路径不能与容器内部的 POSIX 路径混用斜杠；否则回填的
  // C:\\Users\\… 无法作为下一次浏览的起点。
  if (/^[a-z]:[\\/]/i.test(base)) return base.replace(/[\\/]+$/, '') + '\\' + dir.replace(/\//g, '\\');
  return base.replace(/\/$/, '') + '/' + dir;
}
/* 从输入值推导浏览起点：宿主机路径 → 本地挂载相对路径；空值/其他前缀回退根 */
function browseDirFromValue(value) {
  value = (value || '').trim();
  const base = (state.config?.hostLocal || '/local').replace(/\/$/, '');
  const windowsHost = /^[a-z]:[\\/]/i.test(base);
  const normalize = input => String(input || '').replace(/\\/g, '/').replace(/\/+$/, '');
  const sameHostPath = (left, right) => windowsHost ? left.toLowerCase() === right.toLowerCase() : left === right;
  const hostStartsWith = (candidate, prefix) => windowsHost ? candidate.toLowerCase().startsWith(prefix.toLowerCase() + '/') : candidate.startsWith(prefix + '/');
  if (value === '~') value = base;
  else if (value.startsWith('~/')) value = base + value.slice(1);
  const normalizedValue = normalize(value);
  for (const prefix of [normalize(base), '/local']) {
    if (sameHostPath(normalizedValue, prefix)) return '.';
    if (hostStartsWith(normalizedValue, prefix)) {
      const parts = normalizedValue.slice(prefix.length + 1).split('/');
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

function isWorkspaceBrowseField(field) {
  return (field === 'docs-path' && $('docs-location').value === 'workspace') ||
    (field === 'cache-path' && $('cache-location').value === 'workspace');
}
function isRemoteWorkspaceBrowseField(field) { return field === 'ws-remote-path'; }
function browseWorkspaceDirFromValue(value) {
  const clean = String(value || '').trim().replace(/\\/g, '/').replace(/^\.\//, '').replace(/\/+$/, '');
  if (!clean || clean === '.') return '.';
  if (clean.startsWith('/')) throw new Error(t('远程绝对路径可直接输入；目录浏览仅显示当前工作空间范围'));
  const parts = [];
  for (const part of clean.split('/')) {
    if (!part || part === '.') continue;
    if (part === '..') { if (!parts.length) throw new Error(t('目录超出远程工作空间')); parts.pop(); }
    else parts.push(part);
  }
  return parts.join('/') || '.';
}
function browseRemoteDirFromValue(value) {
  const remote = String(value || '').trim().replace(/\\/g, '/');
  if (!remote || remote === '~' || remote === '.') return '.';
  if (remote.startsWith('~/')) return remote.slice(2) || '.';
  if (/[\u0000\r\n]/.test(remote)) throw new Error(t('远程目录不能包含换行或空字符'));
  return remote;
}
function browseRootForField(field) {
  if (isRemoteWorkspaceBrowseField(field)) return 'remote';
  return isWorkspaceBrowseField(field) ? 'workspace' : 'local';
}

/* 当前路径不可用时逐级向上找到可用目录 */
async function resolveExistingDir(dir, root = 'local') {
  for (;;) {
    try {
      await api('/files?root=' + root + '&path=' + encodeURIComponent(dir));
      return dir;
    } catch (error) {
      if (dir === '.' || !dir.includes('/')) return '.';
      dir = dir.slice(0, dir.lastIndexOf('/'));
    }
  }
}
function openBrowse(field) {
  action(async () => {
    const root = browseRootForField(field);
    const workspace = root === 'workspace';
    const remote = root === 'remote';
    const start = remote ? browseRemoteDirFromValue($(field).value) : workspace ? browseWorkspaceDirFromValue($(field).value) : browseDirFromValue($(field).value);
    const dir = await resolveExistingDir(start, root);
    wsState.browse = { field, dir, root, workspace, remote };
    $('ws-browse-dialog').showModal();
    await loadBrowseDir();
  })();
}
async function loadBrowseDir() {
  const b = wsState.browse;
  const files = await api('/files?root=' + b.root + '&path=' + encodeURIComponent(b.dir));
  $('ws-browse-path').textContent = b.remote ? t('可访问范围：远程账户目录') : b.workspace ? t('可访问范围：远程工作空间') : t('可访问范围：{0}', hostPathOf('.'));
  $('ws-browse-address').value = b.workspace || b.remote ? b.dir : hostPathOf(b.dir);
  $('ws-browse-parent').disabled = !b.remote && b.dir === '.';
  $('ws-browse-status').textContent = '';
  const list = $('ws-browse-list');
  list.replaceChildren();
  const dirs = files.filter(f => f.dir);
  if (!dirs.length) list.append(el('p', 'muted', t("没有子目录")));
  dirs.forEach(d => {
    const row = el('div', 'ws-browse-dir-row');
    const open = el('button', 'ws-browse-item', '▱ ' + d.name);
    open.type = 'button'; open.onclick = () => { b.dir = d.path; action(loadBrowseDir)(); };
    const rename = el('button', 'quiet ws-browse-rename', t('重命名'));
    rename.type = 'button'; rename.title = t('重命名文件夹');
    rename.onclick = () => renameBrowseDirectory(d);
    row.append(open, rename); list.append(row);
  });
}
async function browseAddress() {
  const b = wsState.browse, previous = b.dir;
  try {
    b.dir = b.remote ? browseRemoteDirFromValue($('ws-browse-address').value) : b.workspace ? browseWorkspaceDirFromValue($('ws-browse-address').value) : browseDirFromValue($('ws-browse-address').value);
    await loadBrowseDir();
  } catch (error) {
    b.dir = previous;
    $('ws-browse-status').textContent = error.message;
  }
}
$('ws-browse-go').onclick = browseAddress;
$('ws-browse-address').onkeydown = event => { if (event.key === 'Enter') { event.preventDefault(); browseAddress(); } };
$('ws-browse').onclick = () => openBrowse('ws-path');
function setupRemoteWorkspaceBrowse() {
  const input = $('ws-remote-path');
  if (!input || $('ws-remote-browse')) return;
  const row = el('div', 'ws-row');
  input.replaceWith(row); row.append(input);
  const browse = el('button', 'quiet', t('浏览…'));
  browse.id = 'ws-remote-browse'; browse.type = 'button';
  browse.onclick = () => openBrowse('ws-remote-path');
  row.append(browse);
}
setupRemoteWorkspaceBrowse();
$('docs-browse').onclick = () => openBrowse('docs-path');
$('cache-browse').onclick = () => openBrowse('cache-path');
function browseParent(dir, remote) {
  if (!remote) return dir.includes('/') ? dir.slice(0, dir.lastIndexOf('/')) : '.';
  if (dir === '/') return '/';
  if (dir === '.') return '..';
  const trim = dir.replace(/\/+$/, '');
  const parent = trim.slice(0, trim.lastIndexOf('/'));
  return parent || (trim.startsWith('/') ? '/' : '.');
}
async function createBrowseDirectory() {
  const b = wsState.browse;
  const name = window.prompt(t('请输入新文件夹名称'));
  if (name === null) return;
  await api('/directory', { method: 'POST', body: JSON.stringify({ root: b.root, parentPath: b.dir, name }) });
  await loadBrowseDir();
}
async function renameBrowseDirectory(dir) {
  const b = wsState.browse;
  const newName = window.prompt(t('请输入新的文件夹名称'), dir.name);
  if (newName === null || newName.trim() === dir.name) return;
  await api('/directory/rename', { method: 'POST', body: JSON.stringify({ root: b.root, path: dir.path, newName }) });
  await loadBrowseDir();
}
function setupBrowseDirectoryActions() {
  const actions = $('ws-browse-select').parentElement;
  if ($('ws-browse-new')) return;
  const create = el('button', 'quiet', t('新建文件夹'));
  create.id = 'ws-browse-new'; create.type = 'button';
  create.onclick = () => action(createBrowseDirectory)();
  actions.prepend(create);
}
setupBrowseDirectoryActions();
$('ws-browse-parent').onclick = () => { const b = wsState.browse; b.dir = browseParent(b.dir, b.remote); action(loadBrowseDir)(); };
$('ws-browse-select').onclick = () => { const b = wsState.browse; $(b.field).value = b.remote ? (b.dir === '.' ? '' : b.dir) : b.workspace ? (b.dir === '.' ? '' : b.dir) : b.field === 'ws-key-refpath' ? ('/local/' + b.dir).replace(/\/+/g,'/') : hostPathOf(b.dir); $('ws-browse-dialog').close(); };

/* ── 引用多来源（FR-82~84） ── */
let editingSourceID = '';
function defaultContextSourceID() {
  const enabled = state.sources.filter(x => x.enabled);
  return (enabled.find(x => x.id === 'system-docs') || enabled.find(x => x.id === 'context') || enabled[0] || {}).id || '';
}
function sourceUpdatePayload(change) {
  return state.sources.filter(x => !x.builtin).map(src => src.id === change.id ? { ...src, ...change } : src);
}
function sourceCanSetRW(src) { return ['local', 'skill', 'sftp'].includes(src.type); }
async function loadSourcesList() {
  state.sources = (await api('/sources')).sources || [];
  if (state.root === 'context' && !state.source) state.source = defaultContextSourceID();
  renderSourceChips();
}
function sourceIconSVG(type) {
  const p = '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">';
  if (type === 'local' || type === 'skill') return p + '<path d="M3 7a2 2 0 0 1 2-2h5l2 3h7a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2Z"/></svg>';
  if (type === 'sftp' || type === 'workspace-sftp' || type === 'ftp' || type === 'ftps') return p + '<rect x="3" y="4" width="18" height="7" rx="2"/><rect x="3" y="13" width="18" height="7" rx="2"/><path d="M7 7.5h.01M7 16.5h.01"/></svg>';
  if (type === 'link') return p + '<path d="M10 13a5 5 0 0 0 7.07.5l2-2a5 5 0 0 0-7.07-7.07l-1 1"/><path d="M14 11a5 5 0 0 0-7.07-.5l-2 2a5 5 0 0 0 7.07 7.07l1-1"/></svg>';
  if (type === 'smb') return p + '<rect x="2" y="9" width="20" height="11" rx="2"/><path d="M6 9V6a2 2 0 0 1 2-2h8a2 2 0 0 1 2 2v3"/><path d="M6 13h.01"/></svg>';
  if (type === 'mcp') return p + '<path d="M9 3v4M15 3v4M7 7h10v4a5 5 0 0 1-10 0Z"/><path d="M12 16v5"/></svg>';
  return p + '<path d="M14 3H7a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h10a2 2 0 0 0 2-2V8Z"/><path d="M14 3v5h5"/></svg>';
}
function sourceLockSVG(locked) {
  const body = locked
    ? '<rect x="5" y="11" width="14" height="9" rx="2"/><path d="M8 11V8a4 4 0 0 1 8 0v3"/>'
    : '<rect x="5" y="11" width="14" height="9" rx="2"/><path d="M8 11V8a4 4 0 0 1 7.5-3.2"/>';
  return '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">' + body + '</svg>';
}
function renderSourceChips() {
  const host = $('source-chips');
  const visible = state.root === 'context';
  host.classList.toggle('hidden', !visible);
  if (!visible) return;
  const track = $('source-track');
  track.replaceChildren();
  const enabled = state.sources.filter(x => x.enabled);
  const systemDocs = enabled.find(x => x.id === 'system-docs');
  // /context remains a backend reference root, but its generic built-in chip is
  // redundant beside the configured system-docs source and is hidden from this list.
  const ordered = [systemDocs, ...enabled.filter(x => !x.builtin && x.id !== 'context')].filter(Boolean);
  ordered.forEach(src => {
    const displayName = src.builtin ? t(src.name) : src.name;
    const loc = src.config.path || src.config.command || src.config.url || (src.config.host ? ('//' + src.config.host + (src.config.path || '')) : '');
    const wrap = el('div', 'src-chip' + (state.source === src.id ? ' active' : '') + (src.id === 'system-docs' ? ' system-docs' : ''));
    const main = el('button', 'src-main');
    main.type = 'button';
    main.title = t(src.rw ? '{0} · 读写' : '{0} · 只读', (src.type ? src.type + ' · ' : '') + (loc || displayName));
    main.setAttribute('aria-pressed', state.source === src.id ? 'true' : 'false');
    main.onclick = () => { rememberFileLocation(); state.source = src.id; restoreFileLocation(); state.attachments = []; renderAttachments(); renderSourceChips(); action(loadFiles)(); };
    if (!src.builtin) {
      const edit = el('button', 'src-edit');
      edit.type = 'button'; edit.title = t('编辑引用配置'); edit.setAttribute('aria-label', t('编辑引用配置：{0}', displayName));
      edit.insertAdjacentHTML('beforeend', '<span class="src-ico">' + sourceIconSVG(src.type) + '</span>');
      edit.onclick = ev => { ev.stopPropagation(); openSourceEditor(src); };
      wrap.append(edit);
    } else {
      main.insertAdjacentHTML('beforeend', '<span class="src-ico">' + sourceIconSVG(src.type) + '</span>');
    }
    main.append(el('span', 'src-name', displayName));
    if (src.builtin) main.append(el('span', 'src-fixed', src.id === 'system-docs' ? t('固定') : t('内置')));
    wrap.append(main);
    if (!src.builtin) {
      if (src.type === 'mcp') {
        const test = el('button', 'src-test', t('测试 MCP'));
        test.type = 'button';
        test.title = t('测试 MCP 并发现工具');
        test.setAttribute('aria-label', t('测试 MCP 并发现工具'));
        test.onclick = ev => { ev.stopPropagation(); action(async () => {
          const result = await api('/sources/' + encodeURIComponent(src.id) + '/test', { method: 'POST' });
          await loadSourcesList();
          if (state.source === src.id) await loadFiles();
          const total = Array.isArray(result.tools) ? result.tools.length : 0;
          const readOnly = Array.isArray(result.tools) ? result.tools.filter(x => x.readOnly).length : 0;
          toast(t('MCP 已发现 {0} 个工具，其中 {1} 个可只读调用', total, readOnly));
        })(); };
        wrap.append(test);
      }
      const lock = el('button', 'src-lock');
      lock.type = 'button';
      const canSetRW = sourceCanSetRW(src);
      const nextMode = canSetRW ? (src.rw ? t('切换为只读') : t('切换为读写')) : t('此来源只支持只读');
      lock.title = nextMode;
      lock.setAttribute('aria-label', nextMode);
      lock.setAttribute('aria-pressed', src.rw ? 'false' : 'true');
      lock.disabled = !canSetRW;
      lock.innerHTML = sourceLockSVG(!src.rw);
      lock.onclick = ev => { if (!canSetRW) return; ev.stopPropagation(); action(async () => {
        await api('/sources', { method: 'PUT', body: JSON.stringify({ sources: sourceUpdatePayload({ id: src.id, rw: !src.rw }) }) });
        await loadSourcesList();
        if (state.source === src.id) await loadFiles();
        toast(src.rw ? t('已切换为只读') : t('已切换为读写'));
      })(); };
      wrap.append(lock);
      const del = el('button', 'src-del');
      del.type = 'button';
      del.title = t("删除来源 {0}", displayName);
      del.setAttribute('aria-label', t("删除来源 {0}", displayName));
      del.innerHTML = '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><path d="M6 6l12 12M18 6 6 18"/></svg>';
      del.onclick = ev => { ev.stopPropagation(); if (confirm(t('删除来源「{0}」？', displayName))) action(async () => { await api('/sources', { method: 'PUT', body: JSON.stringify({ sources: state.sources.filter(x => !x.builtin && x.id !== src.id) }) }); await loadSourcesList(); if (state.source === src.id) { state.source = defaultContextSourceID(); state.dir = '.'; await loadFiles(); } })(); };
      wrap.append(del);
    }
    track.append(wrap);
  });
}
$('source-add').onclick = () => {
  editingSourceID = '';
  $('source-form').reset(); $('src-type').disabled = false;
  setSourceDialogMode(false); renderSourceFields(); $('source-dialog').showModal();
};

function setSourceDialogMode(editing) {
  const title = document.querySelector('#source-dialog h2 i18n-text');
  const titleKey = editing ? '编辑引用配置' : '添加辅助资料来源';
  title.dataset.i18n = titleKey; title.textContent = t(titleKey);
  const submit = document.querySelector('#source-submit i18n-text');
  const submitKey = editing ? '保存来源配置' : '添加来源';
  submit.dataset.i18n = submitKey; submit.textContent = t(submitKey);
}

function openSourceEditor(src) {
  if (!src || src.builtin) return;
  editingSourceID = src.id;
  $('source-form').reset();
  $('src-type').value = src.type;
  renderSourceFields();
  $('src-type').disabled = true;
  $('src-name').value = src.name || '';
  $('src-rw').checked = !!src.rw;
  if (src.type === 'local' || src.type === 'skill') $('src-path').value = src.config.path || '';
  else if (src.type === 'sftp') {
    $('src-host').value = src.config.host || '';
    $('src-port').value = src.config.port || 22;
    $('src-user').value = src.config.username || '';
    $('src-remote').value = src.config.path || '';
  } else if (src.type === 'mcp') {
    $('src-command').value = src.config.command || '';
    $('src-mcp-args').value = (src.config.args || []).join('\n');
  } else {
    $('src-url').value = src.config.url || '';
    if ($('src-user')) $('src-user').value = src.config.username || '';
  }
  const keepSecretHint = src.hasSecret ? t('留空 = 保留现有凭据') : '';
  ['src-password', 'src-key'].forEach(id => { if ($(id) && keepSecretHint) $(id).placeholder = keepSecretHint; });
  setSourceDialogMode(true);
  $('source-dialog').showModal();
}

function renderSourceFields() {
  const type = $('src-type').value;
  const mcpOption = [...$('src-type').options].find(option => option.value === 'mcp');
  if (mcpOption) { mcpOption.textContent = t('MCP 工具服务'); mcpOption.dataset.i18n = 'MCP 工具服务'; }
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
    if (type !== 'link') { addField(t('用户名'), 'src-user', ''); addField(t('密码（可选）'), 'src-password', editingSourceID ? t('留空 = 保留现有凭据') : '').type = 'password'; }
  }
  else if (type === 'mcp') {
    addField(t('启动程序'), 'src-command', 'npx');
    const argsLabel = el('label', '', t('参数（每行一个）'));
    const args = el('textarea', ''); args.id = 'src-mcp-args'; args.rows = 4; args.placeholder = '-y\n@wisflux/docmost-local-mcp\n--base-url=http://docs.example.com'; args.autocomplete = 'off'; argsLabel.append(args); host.append(argsLabel);
    const template = el('button', 'quiet', t('填入 Docmost 模板')); template.type = 'button';
    template.onclick = () => { $('src-command').value = 'npx'; args.value = '-y\n@wisflux/docmost-local-mcp\n--base-url=http://docs.example.com'; };
    host.append(template);
    host.append(el('p', 'muted', t('MCP 程序由 AIDE 运行时启动。测试会发现工具；AI 仅能调用服务声明为只读的工具。首次认证由该 MCP 服务自行处理。')));
  }
  else if (type === 'sftp') {
    addField(t("主机"), 'src-host', '192.168.1.10');
    addField(t("端口"), 'src-port', '22').type = 'number';
    addField(t("用户名"), 'src-user', 'root');
    addField(t("远程目录"), 'src-remote', '/srv/refs');
    addField(t("密码（可选）"), 'src-password', editingSourceID ? t('留空 = 保留现有凭据') : t("留空 = 无密码认证")).type = 'password';
    addField(t("私钥（可选，优先于密码）"), 'src-key', editingSourceID ? t('留空 = 保留现有凭据') : t("粘贴私钥内容")).type = 'password';
  }
}
$('src-type').addEventListener('change', renderSourceFields);
function suggestedSourceName(type) {
  if (type === 'local' || type === 'skill') {
    const parts = ($('src-path')?.value || '').trim().split(/[\\/]+/).filter(Boolean);
    return parts.pop() || (type === 'skill' ? 'Skill' : '本机');
  }
  if (type === 'sftp') return ($('src-host')?.value || '').trim() || 'SFTP';
  if (type === 'link' || type === 'ftp' || type === 'ftps' || type === 'smb') {
    const value = ($('src-url')?.value || '').trim();
    try { return new URL(value).hostname || type.toUpperCase(); } catch { return type.toUpperCase(); }
  }
  if (type === 'mcp') {
    const args = ($('src-mcp-args')?.value || '').split(/\r?\n/).map(x => x.trim()).filter(x => x && !x.startsWith('-'));
    const command = ($('src-command')?.value || '').trim();
    return args.pop()?.split(/[\\/]/).filter(Boolean).pop() || command.split(/[\\/]/).filter(Boolean).pop() || 'MCP';
  }
  return type;
}
$('source-form').onsubmit = action(async event => {
  event.preventDefault();
  const type = $('src-type').value;
  const name = $('src-name').value.trim() || suggestedSourceName(type);
  const previous = editingSourceID ? state.sources.find(x => x.id === editingSourceID) : null;
  const id = previous?.id || 's-' + Math.random().toString(36).slice(2, 8);
  const src = { id, name, type, enabled: previous?.enabled ?? true, rw: $('src-rw').checked, config: {} };
  const secrets = {};
  if (type === 'local' || type === 'skill') src.config.path = $('src-path').value.trim();
  else if (type === 'mcp') { src.config = { transport: 'stdio', command: ($('src-command')?.value || '').trim(), args: ($('src-mcp-args')?.value || '').split(/\r?\n/).filter(value => value.length > 0) }; }
  else if (type === 'sftp') { const pw = $('src-password').value, key = $('src-key').value; src.config = { path: $('src-remote').value.trim(), host: $('src-host').value.trim(), port: parseInt($('src-port').value, 10) || 22, username: $('src-user').value.trim(), auth: key ? 'key' : pw ? 'password' : previous?.config.auth || 'none' }; if (pw || key) secrets[id] = { password: pw, key }; }
  else { src.config.url = $('src-url').value.trim(); if ($('src-user')) src.config.username = $('src-user').value.trim(); if ($('src-password')?.value) secrets[id] = { password: $('src-password').value }; }
  const payload = previous
    ? { sources: sourceUpdatePayload(src) }
    : { sources: [...state.sources.filter(x => !x.builtin), src] };
  if (Object.keys(secrets).length) payload.secrets = secrets;
  await api('/sources', { method: 'PUT', body: JSON.stringify(payload) });
  $('source-dialog').close();
  editingSourceID = '';
  await loadSourcesList();
  state.source = id; state.dir = '.'; await loadFiles();
  toast(previous ? t('来源配置已保存') : t("已添加来源：{0}", name));
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
      if (name === 'href' || name === 'src') {
        // 去掉 scheme 内的 tab/换行/空白等控制字符，堵住 java\tscript: / java\nscript: 绕过
        const norm = (attr.value || '').replace(/[\x00-\x20]/g, '');
        if (/^(javascript|vbscript|data:text\/html)/i.test(norm)) el.removeAttribute(attr.name);
      }
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
      container.innerHTML = `<div style="padding:40px;text-align:center;color:var(--text-dim);"><p>📐 ${t('draw.io 加载超时')}</p><p style="font-size:12px;margin-top:8px;">${t('本地 draw.io 加载失败，请刷新页面重试')}</p></div>`;
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
  const btnFit = el('button', 'img-ctrl', t('适应'));
  const btnOrig = el('button', 'img-ctrl', '1:1');
  const btnClose = el('button', 'img-ctrl', '✕');
  btnZoomIn.title = t('放大'); btnZoomOut.title = t('缩小'); btnFit.title = t('适应窗口'); btnOrig.title = t('原始大小'); btnClose.title = t('关闭');
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
  img.onerror = () => { canvas.innerHTML = `<div style="color:var(--warn);padding:40px;text-align:center;">${t('图片加载失败')}</div>`; };
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
  const onImgMove = (e) => { if (dragging) { tx = e.clientX - startX; ty = e.clientY - startY; apply(); } };
  const onImgUp = () => { dragging = false; canvas.style.cursor = 'grab'; };
  window.addEventListener('mousemove', onImgMove);
  window.addEventListener('mouseup', onImgUp);
  bindViewerTeardown(container, () => {
    window.removeEventListener('mousemove', onImgMove);
    window.removeEventListener('mouseup', onImgUp);
  });
  canvas.style.cursor = 'grab';
}

/* STL 3D 模型预览器：Three.js + STLLoader + OrbitControls */
/* 动态确保 vendor 脚本加载（兜底 defer 未生效 / 缓存失败） */
function ensureVendorScript(src, check) {
  return new Promise((resolve, reject) => {
    if (check()) return resolve();
    const sc = document.createElement('script');
    sc.src = src; sc.async = false;
    sc.onload = () => (check() ? resolve() : reject(new Error(t('{0} 加载后仍不可用', src))));
    sc.onerror = () => reject(new Error(t('无法加载 {0}', src)));
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
/* ── 查看器资源回收：切换文件/关闭页签/卸载页面时，统一释放 WebGL/Observer/全局监听 ── */
function teardownViewer(container) {
  try { if (container && container._aideViewerTeardown) container._aideViewerTeardown(); } catch (_) {}
  try { if (container) container._aideViewerTeardown = null; } catch (_) {}
}
function bindViewerTeardown(container, teardown) {
  if (!container || typeof teardown !== 'function') return;
  container._aideViewerTeardown = teardown;
  const dlg = container.closest('dialog');
  if (dlg) dlg.addEventListener('close', teardown, { once: true });
  window.addEventListener('pagehide', teardown, { once: true });
}
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
  let animId = 0, resizeObs = null, stlGeometry = null, stlMaterial = null;
  let disposed = false;
  const teardown = () => {
    if (disposed) return; disposed = true;
    if (animId) { cancelAnimationFrame(animId); animId = 0; }
    if (resizeObs) { resizeObs.disconnect(); resizeObs = null; }
    try { controls.dispose(); } catch (_) {}
    if (stlGeometry) { try { stlGeometry.dispose(); } catch (_) {} }
    if (stlMaterial) { try { stlMaterial.dispose(); } catch (_) {} }
    try { renderer.dispose(); } catch (_) {}
  };
  bindViewerTeardown(container, teardown);

  fetch(rawUrl).then(r => { if (!r.ok) throw new Error('HTTP ' + r.status); return r.arrayBuffer(); })
    .then(buf => {
      stlGeometry = new THREE.STLLoader().parse(buf);
      stlGeometry.computeVertexNormals(); stlGeometry.computeBoundingBox();
      const mesh = new THREE.Mesh(stlGeometry, new THREE.MeshPhongMaterial({ color: 0x60a5fa, specular: 0x111111, shininess: 80 }));
      stlMaterial = mesh.material;
      const bb = stlGeometry.boundingBox; const center = new THREE.Vector3(); bb.getCenter(center);
      mesh.position.sub(center); scene.add(mesh);
      grid.position.y = bb.min.y - center.y;
      const size = new THREE.Vector3(); bb.getSize(size);
      const maxDim = Math.max(size.x, size.y, size.z);
      const camDist = Math.abs(maxDim / 2 / Math.tan(camera.fov * Math.PI / 360)) * 1.8;
      camera.position.set(camDist, camDist * 0.7, camDist);
      camera.near = camDist / 100; camera.far = camDist * 100; camera.updateProjectionMatrix();
      controls.target.set(0, 0, 0); controls.update();
      meta.textContent = Math.round(stlGeometry.attributes.position.count / 3) + ' ' + t('三角面') + ' · ' + size.x.toFixed(2) + '×' + size.y.toFixed(2) + '×' + size.z.toFixed(2);
      btnReset.onclick = () => { camera.position.set(camDist, camDist * 0.7, camDist); controls.target.set(0, 0, 0); controls.update(); };
      (function animate() { if (disposed) return; animId = requestAnimationFrame(animate); controls.update(); renderer.render(scene, camera); })();
      const resize = () => { if (disposed) return; const w = canvasWrap.clientWidth, h = canvasWrap.clientHeight; if (w > 0 && h > 0) { camera.aspect = w / h; camera.updateProjectionMatrix(); renderer.setSize(w, h); } };
      resize(); resizeObs = new ResizeObserver(resize); resizeObs.observe(canvasWrap);
    }).catch(err => { canvasWrap.innerHTML = '<div style="padding:40px;text-align:center;color:var(--warn);">' + t('STL 加载失败：') + escapeHtml(err.message) + '</div>'; meta.textContent = t('解析失败'); });
}
/* ── ZIP 查看器：只列出目录，不会在浏览器中解压或执行归档内容。 ── */
async function setupZipPreview(container, filePath, root, source) {
  container.innerHTML = '';
  const viewer = el('div', 'zip-viewer');
  const toolbar = el('div', 'zip-toolbar');
  const title = el('strong', '', filePath.split('/').pop());
  const meta = el('span', 'zip-meta', t('正在读取压缩包…'));
  const download = el('a', 'zip-download', t('下载 ZIP')); download.href = fileRawUrl(filePath, root, source); download.download = filePath.split('/').pop();
  toolbar.append(title, meta, download);
  const list = el('div', 'zip-list'); viewer.append(toolbar, list); container.append(viewer);
  try {
    if (!window.JSZip) throw new Error('JSZip unavailable');
    const response = await fetch(fileRawUrl(filePath, root, source));
    if (!response.ok) throw new Error('HTTP ' + response.status);
    const zip = await window.JSZip.loadAsync(await response.arrayBuffer(), { createFolders: true });
    const entries = Object.values(zip.files).sort((a, b) => a.name.localeCompare(b.name, undefined, { numeric: true }));
    let files = 0, dirs = 0;
    for (const entry of entries) {
      if (entry.dir) dirs++; else files++;
      const row = el('div', 'zip-entry' + (entry.dir ? ' zip-entry-dir' : ''));
      row.append(el('span', 'zip-entry-icon', entry.dir ? '▱' : '≡'), el('span', 'zip-entry-name', entry.name));
      list.append(row);
    }
    meta.textContent = t('{0} 个文件 · {1} 个文件夹', files, dirs);
    if (!entries.length) list.append(el('div', 'zip-empty', t('压缩包为空')));
  } catch (err) {
    meta.textContent = t('加载失败');
    list.append(el('div', 'zip-error', t('压缩包加载失败：') + (err.message || String(err))));
  }
}

/* ── PDF.js 内联预览器：离线 vendor、连续滚动、逐页 canvas、IntersectionObserver 懒渲染 ── */
/* 动态 import 同源 ES 模块；仅在打开 PDF 时加载一次并缓存 Promise */
let _pdfjsPromise = null;
function ensurePdfJs() {
  if (_pdfjsPromise) return _pdfjsPromise;
  _pdfjsPromise = (async () => {
    const pdfjs = await import('/vendor/pdfjs/pdf.min.mjs');
    pdfjs.GlobalWorkerOptions.workerSrc = '/vendor/pdfjs/pdf.worker.min.mjs';
    return pdfjs;
  })();
  return _pdfjsPromise;
}
async function setupPdfPreview(container, filePath, root, source) {
  container.innerHTML = '';
  const token = state.token || (state.config && state.config.accessToken) || '';
  const qp = new URLSearchParams();
  qp.set('path', filePath);
  if (source) qp.set('source', source); else qp.set('root', root || 'workspace');
  if (token) qp.set('access_token', token);
  const rawUrl = '/api/file/raw?' + qp.toString();

  const viewer = el('div', 'pdf-viewer');
  const toolbar = el('div', 'pdf-toolbar');
  const info = el('span', 'pdf-info', filePath.split('/').pop());
  const meta = el('span', 'pdf-meta', t('加载中…'));
  const btnPrev = el('button', 'pdf-ctrl', '‹');
  const pageInput = el('input', 'pdf-page-input'); pageInput.type = 'text'; pageInput.inputMode = 'numeric'; pageInput.value = '–';
  const pageTotal = el('span', 'pdf-page-total', '');
  const btnNext = el('button', 'pdf-ctrl', '›');
  const btnZoomOut = el('button', 'pdf-ctrl', '－');
  const btnZoomIn = el('button', 'pdf-ctrl', '＋');
  const btnFit = el('button', 'pdf-ctrl', t('适应宽度'));
  const btnClose = el('button', 'pdf-ctrl', '✕');
  btnPrev.title = t('上一页'); btnNext.title = t('下一页');
  btnZoomOut.title = t('缩小'); btnZoomIn.title = t('放大'); btnFit.title = t('适应宽度'); btnClose.title = t('关闭');
  toolbar.append(info, meta, btnPrev, pageInput, pageTotal, btnNext, btnZoomOut, btnZoomIn, btnFit, btnClose);
  const scroll = el('div', 'pdf-scroll');
  /* 预留：后续批注覆盖层（本次不实现） */
  const overlay = el('div', 'pdf-annot-overlay'); overlay.hidden = true;
  viewer.append(toolbar, scroll, overlay);
  container.append(viewer);

  btnClose.onclick = () => {
    const dlg = container.closest('dialog');
    if (dlg) dlg.close();
    else if (document.body.classList.contains('file-view-mode')) history.back();
  };

  let cancelled = false, pdfDoc = null, io = null;
  const pageList = [], holders = [];
  const dpr = window.devicePixelRatio || 1;
  let scale = 1, baseWidth = 0, numPages = 0;
  let renderGeneration = 0; // 渲染代次：layoutPages/缩放使其递增，旧 renderVisible 循环发现过期即退出

  const setMeta = (txt) => { meta.textContent = txt; };
  const fail = (msg) => { scroll.innerHTML = ''; scroll.append(el('div', 'pdf-error', msg)); setMeta(t('加载失败')); };
  const teardown = () => {
    cancelled = true;
    renderGeneration++;
    for (const h of holders) { if (h && h.renderTask) { try { h.renderTask.cancel(); } catch (e) {} h.renderTask = null; } }
    if (io) { io.disconnect(); io = null; }
    if (pdfDoc) { try { pdfDoc.destroy(); } catch (e) {} pdfDoc = null; }
  };
  bindViewerTeardown(container, teardown);

  // 1) 取字节（复用图片/STL 的 raw 端点，带 access_token 查询参数）
  let buf;
  try {
    const r = await fetch(rawUrl);
    if (!r.ok) throw new Error('HTTP ' + r.status);
    buf = await r.arrayBuffer();
  } catch (e) { fail(t('无法读取 PDF：') + escapeHtml(e.message)); return; }

  // 2) 加载本地 pdf.js 模块
  let pdfjs;
  try { pdfjs = await ensurePdfJs(); }
  catch (e) { fail(t('PDF 渲染组件加载失败（离线 vendor 缺失？）：') + escapeHtml(e.message)); return; }

  // 3) 打开文档（加密 PDF 走 onPassword 回调）
  const task = pdfjs.getDocument({ data: buf });
  task.onPassword = (updatePassword, reason) => {
    // reason: 0 = NEED_PASSWORD，1 = INCORRECT_PASSWORD
    passwordPrompt(reason === 1 ? t('密码不正确，请重试') : t('该 PDF 已加密，请输入打开密码'))
      .then(pw => {
        if (pw == null) { task.destroy(); fail(t('已取消：需要密码才能打开该 PDF')); return; }
        updatePassword(pw);
      });
  };
  try { pdfDoc = await task.promise; }
  catch (e) {
    if (e && e.name === 'PasswordException') return; // 取消/错误已由 onPassword 给出提示
    fail(t('PDF 已损坏或无法解析：') + escapeHtml(e.message)); return;
  }
  if (cancelled || !pdfDoc) return;

  numPages = pdfDoc.numPages;
  pageTotal.textContent = '/ ' + numPages;
  pageInput.value = '1';

  async function getPage(n) { if (!pageList[n]) pageList[n] = await pdfDoc.getPage(n); return pageList[n]; }
  const fitWidthScale = () => { const cw = scroll.clientWidth - 24; return baseWidth ? Math.max(0.4, Math.min(3, cw / baseWidth)) : 1; };

  // 4) 逐页占位 + IntersectionObserver 懒渲染
  await getPage(1); baseWidth = pageList[1].getViewport({ scale: 1 }).width;
  scale = fitWidthScale();
  for (let i = 1; i <= numPages; i++) {
    const holder = el('div', 'pdf-page');
    holder.append(el('canvas', 'pdf-canvas'));
    scroll.append(holder);
    holders[i] = { el: holder, canvas: holder.firstChild, page: i, renderedScale: 0, visible: false, renderTask: null };
  }
  await layoutPages();

  io = new IntersectionObserver(entries => {
    for (const en of entries) {
      const h = holders.find(x => x && x.el === en.target);
      if (h) h.visible = en.isIntersecting;
    }
    renderVisible();
  }, { root: scroll, rootMargin: '400px 0px', threshold: 0 });
  holders.forEach(h => io.observe(h.el));

  setMeta(numPages + ' ' + t('页'));

  // 取消某 holder 上在途的渲染任务（与新渲染/拆构竞态安全：用身份判断避免误清新任务）
  function cancelHolderRender(h) {
    if (h && h.renderTask) { try { h.renderTask.cancel(); } catch (e) {} h.renderTask = null; }
  }
  async function layoutPages() {
    renderGeneration++; // 代次递增：旧 renderVisible 循环在下次检查时立即退出
    for (const h of holders) cancelHolderRender(h);
    for (let i = 1; i <= numPages; i++) {
      if (cancelled) return;
      const p = await getPage(i);
      if (cancelled) return;
      const vp = p.getViewport({ scale });
      const h = holders[i];
      h.el.style.width = Math.floor(vp.width) + 'px';
      h.el.style.height = Math.floor(vp.height) + 'px';
      h.renderedScale = 0;
    }
    renderVisible();
  }
  // 渲染单页：开始前取消同 holder 旧任务；带超时兜底；区分取消异常与真实错误
  async function renderPage(h, gen) {
    cancelHolderRender(h);
    if (gen !== renderGeneration || cancelled) return;
    const p = await getPage(h.page);
    if (gen !== renderGeneration || cancelled) return;
    const vp = p.getViewport({ scale });
    const c = h.canvas;
    c.width = Math.floor(vp.width * dpr);
    c.height = Math.floor(vp.height * dpr);
    c.style.width = Math.floor(vp.width) + 'px';
    c.style.height = Math.floor(vp.height) + 'px';
    const task = p.render({ canvasContext: c.getContext('2d'), viewport: vp, transform: [dpr, 0, 0, dpr, 0, 0] });
    h.renderTask = task;
    h.renderedScale = scale;
    let timer = 0;
    const timeout = new Promise((_, rej) => {
      timer = setTimeout(() => { try { task.cancel(); } catch (e) {} rej(new Error('render-timeout')); }, 20000);
    });
    try {
      await Promise.race([task.promise, timeout]);
    } catch (e) {
      // 取消（缩放/翻页/teardown）或超时：按失败处理，重置 renderedScale 以便后续重试
      if ((e && e.name === 'RenderingCancelledException') || (e && e.message === 'render-timeout')) {
        if (h.renderedScale === scale) h.renderedScale = 0;
      }
    } finally {
      clearTimeout(timer);
      if (h.renderTask === task) h.renderTask = null;
    }
  }
  async function renderVisible() {
    const gen = renderGeneration;
    for (let i = 1; i <= numPages; i++) {
      if (gen !== renderGeneration || cancelled) return; // 代次过期/已拆构：立即退出，不再渲染旧比例
      const h = holders[i];
      if (!h.visible || h.renderedScale === scale) continue;
      await renderPage(h, gen);
    }
  }

  let scrollTimer = 0, _gotoLock = false;
  scroll.onscroll = () => { if (_gotoLock) return; clearTimeout(scrollTimer); scrollTimer = setTimeout(updateCurrentPage, 120); };
  function updateCurrentPage() {
    const top = scroll.scrollTop;
    let best = 1, dist = Infinity;
    for (let i = 1; i <= numPages; i++) { const d = Math.abs(holders[i].el.offsetTop - top); if (d < dist) { dist = d; best = i; } }
    pageInput.value = best;
  }
  function gotoPage(i) { i = Math.max(1, Math.min(numPages, i | 0)); _gotoLock = true; clearTimeout(scrollTimer); scroll.scrollTop = holders[i].el.offsetTop; pageInput.value = i; setTimeout(() => { _gotoLock = false; }, 200); }
  btnPrev.onclick = () => gotoPage((parseInt(pageInput.value, 10) || 1) - 1);
  btnNext.onclick = () => gotoPage((parseInt(pageInput.value, 10) || 1) + 1);
  pageInput.onchange = () => gotoPage(parseInt(pageInput.value, 10) || 1);
  btnZoomIn.onclick = () => { scale = Math.min(3, scale * 1.2); layoutPages(); };
  btnZoomOut.onclick = () => { scale = Math.max(0.4, scale / 1.2); layoutPages(); };
  btnFit.onclick = () => { scale = fitWidthScale(); layoutPages(); };
}

/* ── DXF 矢量渲染器：dxf-parser + SVG 离线渲染 ── */
function isDxfPath(path) { return /\.dxf$/i.test(path || ''); }
function isDocxPath(path) { return /\.docx$/i.test(path || ''); }
/* XLSX 查看和单元格修改走服务端 openpyxl：仅传可见页，保留原工作簿。 */
async function setupXlsxPreview(container, filePath, root, source) {
  container.replaceChildren();
  const wrap = el('div', 'xlsx-viewer');
  const toolbar = el('div', 'xlsx-toolbar');
  const sheetSelect = document.createElement('select'); sheetSelect.setAttribute('aria-label', t('工作表'));
  const previous = el('button', 'quiet quiet-sm', '←');
  const next = el('button', 'quiet quiet-sm', '→');
  const previousCol = el('button', 'quiet quiet-sm', '⇤'); previousCol.title = t('前 26 列');
  const nextCol = el('button', 'quiet quiet-sm', '⇥'); nextCol.title = t('后 26 列');
  const position = el('span', 'xlsx-position', '');
  const save = el('button', 'primary primary-sm', t('保存文件'));
  const grid = el('div', 'xlsx-grid');
  toolbar.append(sheetSelect, previous, next, previousCol, nextCol, position, save);
  wrap.append(toolbar, grid); container.append(wrap);
  let current = null, startRow = 1, startCol = 1, changes = new Map(), loading = false, saveHash = '';
  async function load(sheet) {
    if (loading) return;
    loading = true;
    try {
      const query = new URLSearchParams({path:filePath, row:String(startRow), col:String(startCol)});
      if (source) query.set('source', source);
      if (sheet) query.set('sheet', sheet);
      current = await api('/office/xlsx?' + query.toString());
      if (!saveHash) saveHash = current.hash;
      if (!sheetSelect.options.length) current.sheets.forEach(name => { const opt = document.createElement('option'); opt.value = name; opt.textContent = name; sheetSelect.append(opt); });
      sheetSelect.value = current.sheet;
      position.textContent = `${startRow}–${Math.min(startRow + 99, current.maxRow)} / ${current.maxRow} · ${startCol}–${Math.min(startCol + 25, current.maxCol)} / ${current.maxCol}`;
      save.disabled = current.readOnly || !changes.size;
      save.classList.toggle('hidden', current.readOnly);
      previous.disabled = startRow <= 1; next.disabled = startRow + 100 > current.maxRow;
      previousCol.disabled = startCol <= 1; nextCol.disabled = startCol + 26 > current.maxCol;
      const table = el('table', 'xlsx-table');
      const header = document.createElement('tr'); header.append(el('th', '', '#'));
      const count = Math.min(26, Math.max(1, current.maxCol - startCol + 1));
      const firstRow = current.rows[0] || [];
      for (let i=0;i<count;i++) header.append(el('th', '', (firstRow[i]?.ref || '').replace(/[0-9]+$/, '')));
      table.append(header);
      for (const row of current.rows) {
        const tr = document.createElement('tr');
        const rowNum = row.length ? Number((row[0].ref.match(/[0-9]+$/)||[])[0]) : 0;
        tr.append(el('th', '', String(rowNum)));
        for (const cell of row) {
          const td = document.createElement('td');
          const input = document.createElement('input'); input.type = 'text';
          const key = current.sheet + '!' + cell.ref;
          input.value = changes.has(key) ? changes.get(key).display : String(cell.value ?? '');
          input.title = cell.ref; input.readOnly = !!current.readOnly;
          input.addEventListener('input', () => {
            changes.set(key, {sheet: current.sheet, ref: cell.ref, display: input.value});
            save.disabled = false;
          });
          td.append(input); tr.append(td);
        }
        table.append(tr);
      }
      grid.replaceChildren(table);
    } catch (err) { grid.textContent = err.message || String(err); }
    finally { loading = false; }
  }
  sheetSelect.onchange = () => { startRow = 1; load(sheetSelect.value); };
  previous.onclick = () => { startRow = Math.max(1, startRow - 100); load(sheetSelect.value); };
  next.onclick = () => { startRow += 100; load(sheetSelect.value); };
  previousCol.onclick = () => { startCol = Math.max(1, startCol - 26); load(sheetSelect.value); };
  nextCol.onclick = () => { startCol += 26; load(sheetSelect.value); };
  save.onclick = async () => {
    if (!current || !changes.size) return;
    const parsed = [...changes.values()].map(c => {
      const s = c.display;
      const value = s === '' ? null : (/^-?(?:0|[1-9]\d*)(?:\.\d+)?$/.test(s) && Number.isFinite(Number(s)) ? Number(s) : s);
      return {sheet:c.sheet, ref:c.ref, value};
    });
    try {
      save.disabled = true;
      const result = await api('/office/xlsx', {method:'PUT', body:JSON.stringify({path:filePath, source, hash:saveHash, workspaceId:current.workspaceId, changes:parsed})});
      saveHash = result.hash; changes = new Map(); toast(t('✓ 已保存'));
      await load(sheetSelect.value);
    } catch (err) { toast(err.message || String(err)); save.disabled = false; }
  };
  await load('');
}
function isDocPath(path) { return /\.doc$/i.test(path || ''); }
function codeLang(path) {
  var ext = (path || '').split('.').pop().toLowerCase();
  var map = {
    // Python
    py:'python', pyw:'python', py3:'python', pyx:'python',
    // Go
    go:'go',
    // JavaScript / TypeScript
    js:'javascript', mjs:'javascript', cjs:'javascript', jsx:'javascript',
    ts:'typescript', tsx:'typescript',
    // JSON
    json:'json', jsonc:'json',
    // Shell
    sh:'bash', bash:'bash', zsh:'bash', fish:'bash',
    // YAML / TOML / INI
    yaml:'yaml', yml:'yaml', toml:'ini', ini:'ini', cfg:'ini', conf:'ini',
    // SQL
    sql:'sql', pgsql:'sql', mysql:'sql',
    // Markdown
    md:'markdown', mkd:'markdown', markdown:'markdown',
    // C / C++
    c:'c', h:'c', cc:'cpp', cpp:'cpp', cxx:'cpp', hpp:'cpp', hh:'cpp',
    // Java
    java:'java',
    // Kotlin
    kt:'kotlin', kts:'kotlin',
    // Swift
    swift:'swift',
    // Rust
    rs:'rust',
    // Ruby
    rb:'ruby', erb:'ruby',
    // PHP
    php:'php', phtml:'php',
    // C#
    cs:'csharp',
    // Scala
    scala:'scala', sc:'scala',
    // Lua
    lua:'lua',
    // Dart
    dart:'dart',
    // R
    r:'r', R:'r',
    // MATLAB
    m:'matlab',
    // Haskell
    hs:'haskell',
    // Clojure
    clj:'clojure', cljs:'clojurescript',
    // Elixir
    ex:'elixir', exs:'elixir',
    // Erlang
    erl:'erlang', hrl:'erlang',
    // F#
    fs:'fsharp',
    // Groovy
    groovy:'groovy', gvy:'groovy',
    // Perl
    pl:'perl', pm:'perl',
    // Vue
    vue:'xml',
    // HTML / XML
    html:'xml', htm:'xml', xml:'xml', svg:'xml',
    // CSS / SCSS / Less
    css:'css', scss:'scss', less:'less',
    // GraphQL
    gql:'graphql', graphql:'graphql',
    // Protocol Buffers
    proto:'protobuf',
    // Dockerfile
    dockerfile:'dockerfile',
    // Makefile
    makefile:'makefile', mk:'makefile',
    // Vim script
    vim:'vim',
    // Nginx
    nginx:'nginx',
    // Apache
    htaccess:'apache',
  };
  return map[ext] || '';
}
function setupCodeHighlight(textarea, lang) {
  if (!window.hljs || !lang) return;
  var wrapper = document.createElement('div');
  wrapper.className = 'code-wrapper';
  textarea.parentNode.insertBefore(wrapper, textarea);
  wrapper.appendChild(textarea);
  var pre = document.createElement('pre');
  pre.className = 'code-highlight-overlay';
  pre.setAttribute('aria-hidden', 'true');
  var code = document.createElement('code');
  code.className = 'language-' + lang;
  pre.appendChild(code);
  wrapper.insertBefore(pre, textarea);
  textarea.classList.add('code-editable');
  function positionOverlay() {
    var style = window.getComputedStyle(textarea);
    pre.style.top = '0';
    pre.style.left = '0';
    pre.style.width = '100%';
    pre.style.height = '100%';
    pre.style.font = style.font;
    pre.style.lineHeight = style.lineHeight;
    pre.style.letterSpacing = style.letterSpacing;
    pre.style.padding = style.padding;
    pre.style.tabSize = style.tabSize;
    pre.style.whiteSpace = style.whiteSpace;
    pre.style.wordBreak = style.wordBreak;
    pre.style.boxSizing = style.boxSizing;
  }
  function render() {
    positionOverlay();
    var text = textarea.value;
    if (text.length > 500000) { code.textContent = text; return; }
    try {
      var res = window.hljs.highlight(text, { language: lang, ignoreIllegals: true });
      code.innerHTML = res.value;
    } catch (e) { code.textContent = text; }
    pre.scrollTop = textarea.scrollTop;
    pre.scrollLeft = textarea.scrollLeft;
  }
  textarea._aideResizeOverlay = positionOverlay;
  window.addEventListener('resize', positionOverlay);
  textarea.addEventListener('scroll', render);
  textarea.addEventListener('input', render);
  render();
}
function teardownCodeHighlight(textarea) {
  if (textarea._aideResizeOverlay) { window.removeEventListener('resize', textarea._aideResizeOverlay); textarea._aideResizeOverlay = null; }
  textarea.classList.remove('code-editable');
  var wrapper = textarea.parentNode;
  if (wrapper && wrapper.classList.contains('code-wrapper')) {
    var pre = wrapper.querySelector('.code-highlight-overlay');
    if (pre) pre.remove();
    // Unwrap: move textarea back to original parent
    wrapper.parentNode.insertBefore(textarea, wrapper);
    wrapper.remove();
  }
}
let _dxfParserPromise = null;
function ensureDxfParser() {
  if (_dxfParserPromise) return _dxfParserPromise;
  _dxfParserPromise = ensureVendorScript('/vendor/dxf-parser/dxf-parser.js', () => typeof DxfParser !== 'undefined');
  return _dxfParserPromise;
}
const DXF_SVG_NS = 'http://www.w3.org/2000/svg';
function dxfSvgEl(tag, attrs) {
  const e = document.createElementNS(DXF_SVG_NS, tag);
  if (attrs) for (const k in attrs) e.setAttribute(k, attrs[k]);
  return e;
}
// ACI colorIndex → CSS 颜色；7(白/黑 auto) 与 byLayer 回退到主题变量
function dxfEntityColor(ent, layers) {
  const ci = ent.colorIndex;
  if (ci != null && ci !== 0 && ci !== 256 && ci !== 7 && ent.color != null) {
    return '#' + (ent.color >>> 0).toString(16).padStart(6, '0');
  }
  if (ci === 7) return 'var(--dxf-fg)'; // auto 色：随主题明暗
  if (ent.layer && layers && layers[ent.layer] && layers[ent.layer].color != null) {
    const lc = layers[ent.layer].color;
    return '#' + (lc >>> 0).toString(16).padStart(6, '0');
  }
  return 'var(--dxf-fg)';
}
// 多边形顶点 bulge 弧段 → 插值点序列
function bulgePts(x1, y1, x2, y2, bulge) {
  if (!bulge || Math.abs(bulge) < 1e-9) return [[x1, y1], [x2, y2]];
  const dx = x2 - x1, dy = y2 - y1, chord = Math.hypot(dx, dy);
  if (chord < 1e-9) return [[x1, y1], [x2, y2]];
  const angle = 4 * Math.atan(Math.abs(bulge));
  const R = chord / (2 * Math.sin(angle / 2));
  const mx = (x1 + x2) / 2, my = (y1 + y2) / 2;
  const ux = -dy / chord, uy = dx / chord; // 弦的单位法向
  const side = bulge > 0 ? 1 : -1;
  const cd = R * Math.cos(angle / 2);
  const cx = mx - ux * cd * side, cy = my - uy * cd * side;
  const steps = Math.max(4, Math.min(48, Math.ceil(angle * 14)));
  const a0 = Math.atan2(y1 - cy, x1 - cx), a1 = Math.atan2(y2 - cy, x2 - cx);
  let da = a1 - a0;
  while (bulge > 0 && da < 0) da += 2 * Math.PI;
  while (bulge > 0 && da > 2 * Math.PI) da -= 2 * Math.PI;
  while (bulge < 0 && da > 0) da -= 2 * Math.PI;
  while (bulge < 0 && da < -2 * Math.PI) da += 2 * Math.PI;
  const pts = [];
  for (let i = 0; i <= steps; i++) { const a = a0 + da * i / steps; pts.push([cx + R * Math.cos(a), cy + R * Math.sin(a)]); }
  return pts;
}

async function setupDxfPreview(container, filePath, root, source) {
  container.innerHTML = '';
  const token = state.token || (state.config && state.config.accessToken) || '';
  const qp = new URLSearchParams();
  qp.set('path', filePath);
  if (source) qp.set('source', source); else qp.set('root', root || 'workspace');
  if (token) qp.set('access_token', token);
  const rawUrl = '/api/file/raw?' + qp.toString();

  const viewer = el('div', 'dxf-viewer');
  const toolbar = el('div', 'dxf-toolbar');
  const info = el('span', 'dxf-info', filePath.split('/').pop());
  const meta = el('span', 'dxf-meta', t('加载中…'));
  const btnZoomOut = el('button', 'dxf-ctrl', '－');
  const btnZoomIn = el('button', 'dxf-ctrl', '＋');
  const btnFit = el('button', 'dxf-ctrl', t('适应'));
  const btnClose = el('button', 'dxf-ctrl', '✕');
  btnZoomOut.title = t('缩小'); btnZoomIn.title = t('放大'); btnFit.title = t('适应窗口'); btnClose.title = t('关闭');
  toolbar.append(info, meta, btnZoomOut, btnZoomIn, btnFit, btnClose);
  const canvas = el('div', 'dxf-canvas');
  viewer.append(toolbar, canvas);
  container.append(viewer);

  const close = () => {
    const dlg = container.closest('dialog');
    if (dlg) dlg.close();
    else if (document.body.classList.contains('file-view-mode')) history.back();
  };
  btnClose.onclick = close;

  let text;
  try {
    await ensureDxfParser();
    const r = await fetch(rawUrl);
    if (!r.ok) throw new Error('HTTP ' + r.status);
    text = await r.text();
  } catch (e) {
    meta.textContent = t('加载失败');
    canvas.innerHTML = '';
    canvas.append(el('div', 'dxf-error', t('DXF 加载失败：') + ' ' + escapeHtml(e.message)));
    return;
  }

  let doc;
  try {
    doc = new DxfParser().parseSync(text);
  } catch (e) {
    meta.textContent = t('解析失败');
    canvas.innerHTML = '';
    canvas.append(el('div', 'dxf-error', t('DXF 解析失败：') + ' ' + escapeHtml(e.message)));
    return;
  }
  const entities = (doc && doc.entities) || [];
  const blocks = (doc && doc.blocks) || {};
  const layers = (doc && doc.tables && doc.tables.layers) || {};
  if (!entities.length) {
    meta.textContent = t('无实体');
    canvas.innerHTML = '';
    canvas.append(el('div', 'dxf-error', t('该 DXF 文件没有可显示的实体')));
    return;
  }

  // 包围盒（DXF Y-up 坐标）
  const bb = { minX: Infinity, minY: Infinity, maxX: -Infinity, maxY: -Infinity };
  const grow = (x, y) => { if (!isFinite(x) || !isFinite(y)) return; if (x < bb.minX) bb.minX = x; if (x > bb.maxX) bb.maxX = x; if (y < bb.minY) bb.minY = y; if (y > bb.maxY) bb.maxY = y; };

  const svg = dxfSvgEl('svg', { class: 'dxf-svg', preserveAspectRatio: 'xMidYMid meet' });
  const content = dxfSvgEl('g', { transform: 'scale(1,-1)' }); // DXF Y-up → SVG Y-down
  svg.appendChild(content);
  canvas.appendChild(svg);

  const penWidth = 0.35; // 模型单位下的默认线宽
  function strokeAttrs(ent) {
    return { stroke: dxfEntityColor(ent, layers), 'stroke-width': penWidth, fill: 'none', 'stroke-linecap': 'round', 'stroke-linejoin': 'round' };
  }
  function renderEntity(ent, parent, xform) {
    const tx = (xform ? xform.dx : 0), ty = (xform ? xform.dy : 0);
    const sxp = (xform ? xform.sx : 1), syp = (xform ? xform.sy : 1);
    const rot = (xform ? (xform.rotDeg || 0) * Math.PI / 180 : 0);
    const xf = (x, y) => {
      if (!xform) return [x, y];
      const rx = x * sxp, ry = y * syp;
      return [tx + rx * Math.cos(rot) - ry * Math.sin(rot), ty + rx * Math.sin(rot) + ry * Math.cos(rot)];
    };
    try {
      switch (ent.type) {
        case 'LINE': {
          const v = ent.vertices || [];
          if (v.length >= 2) {
            const [x1, y1] = xf(v[0].x, v[0].y), [x2, y2] = xf(v[1].x, v[1].y);
            parent.appendChild(dxfSvgEl('line', { ...strokeAttrs(ent), x1, y1, x2, y2 }));
            grow(x1, y1); grow(x2, y2);
          }
          break;
        }
        case 'POINT': {
          const p = ent.position || {};
          const [x, y] = xf(p.x || 0, p.y || 0);
          parent.appendChild(dxfSvgEl('circle', { cx: x, cy: y, r: penWidth * 2, fill: dxfEntityColor(ent, layers), stroke: 'none' }));
          grow(x, y);
          break;
        }
        case 'CIRCLE': {
          const c = ent.center || {};
          const [cx, cy] = xf(c.x || 0, c.y || 0);
          const r = (ent.radius || 0) * (Math.abs(sxp + syp) / 2 || 1);
          parent.appendChild(dxfSvgEl('circle', { ...strokeAttrs(ent), cx, cy, r }));
          grow(cx - r, cy - r); grow(cx + r, cy + r);
          break;
        }
        case 'ARC': {
          const c = ent.center || {}, r = ent.radius || 0;
          const a0 = ent.startAngle || 0, a1 = ent.endAngle || 0;
          const sa = xf(c.x + r * Math.cos(a0), c.y + r * Math.sin(a0));
          const ea = xf(c.x + r * Math.cos(a1), c.y + r * Math.sin(a1));
          const large = (ent.angleLength || 0) > Math.PI ? 1 : 0;
          parent.appendChild(dxfSvgEl('path', { ...strokeAttrs(ent), d: 'M ' + sa[0] + ' ' + sa[1] + ' A ' + r + ' ' + r + ' 0 ' + large + ' 0 ' + ea[0] + ' ' + ea[1] }));
          const steps = 48;
          for (let i = 0; i <= steps; i++) { const a = a0 + (a1 - a0) * i / steps; const pt = xf(c.x + r * Math.cos(a), c.y + r * Math.sin(a)); grow(pt[0], pt[1]); }
          break;
        }
        case 'ELLIPSE': {
          const c = ent.center || {}, v = ent.majorAxisEndPoint || {};
          const vmx = v.x || 0, vmy = v.y || 0;
          const Ra = Math.hypot(vmx, vmy), Rb = Ra * (ent.axisRatio || 0);
          const theta = Math.atan2(vmy, vmx);
          const ux = Math.cos(theta), uy = Math.sin(theta);
          const wx = -Math.sin(theta), wy = Math.cos(theta);
          const t0 = ent.startAngle || 0, t1 = ent.endAngle || Math.PI * 2;
          const steps = 80;
          let d = '';
          for (let i = 0; i <= steps; i++) {
            const t = t0 + (t1 - t0) * i / steps;
            const lx = Ra * Math.cos(t), ly = Rb * Math.sin(t);
            const px = c.x + lx * ux + ly * wx, py = c.y + lx * uy + ly * wy;
            const pt = xf(px, py);
            d += (i === 0 ? 'M ' : ' L ') + pt[0] + ' ' + pt[1];
            grow(pt[0], pt[1]);
          }
          parent.appendChild(dxfSvgEl('path', { ...strokeAttrs(ent), d }));
          break;
        }
        case 'LWPOLYLINE':
        case 'POLYLINE': {
          const verts = ent.vertices || [];
          if (verts.length < 2) break;
          let d = '', started = false;
          for (let i = 0; i < verts.length; i++) {
            const a = verts[i], b = verts[(i + 1) % verts.length];
            const bulge = a.bulge || 0;
            const seg = bulgePts(a.x, a.y, b.x, b.y, bulge);
            for (let j = 0; j < seg.length; j++) {
              const pt = xf(seg[j][0], seg[j][1]);
              d += (started ? ' L ' : 'M ') + pt[0] + ' ' + pt[1];
              started = true;
              grow(pt[0], pt[1]);
            }
            if (!ent.shape && i === verts.length - 1) break;
          }
          if (ent.shape) d += ' Z';
          parent.appendChild(dxfSvgEl('path', { ...strokeAttrs(ent), d }));
          break;
        }
        case 'SPLINE': {
          const pts = ent.fitPoints && ent.fitPoints.length ? ent.fitPoints : (ent.controlPoints || []);
          if (pts.length < 2) break;
          let d = '';
          pts.forEach((p, i) => { const pt = xf(p.x || 0, p.y || 0); d += (i ? ' L ' : 'M ') + pt[0] + ' ' + pt[1]; grow(pt[0], pt[1]); });
          if (ent.closed) d += ' Z';
          parent.appendChild(dxfSvgEl('path', { ...strokeAttrs(ent), d }));
          break;
        }
        case 'TEXT':
        case 'MTEXT': {
          const p = ent.startPoint || ent.position || {};
          const pt = xf(p.x || 0, p.y || 0);
          const h = ent.textHeight || ent.height || 1;
          const rot = ent.rotation || 0;
          const g = dxfSvgEl('g', { transform: 'translate(' + pt[0] + ' ' + pt[1] + ') scale(1,-1)' });
          const tx2 = dxfSvgEl('text', { 'font-size': h, fill: dxfEntityColor(ent, layers), 'text-anchor': 'start', 'dominant-baseline': 'alphabetic', 'font-family': 'var(--dxf-font, sans-serif)', transform: 'rotate(' + rot + ')' });
          tx2.textContent = ent.text || '';
          g.appendChild(tx2);
          parent.appendChild(g);
          grow(pt[0] - h, pt[1] - h); grow(pt[0] + h * (ent.text || '').length, pt[1] + h);
          break;
        }
        case 'INSERT': {
          const blk = blocks[ent.name];
          if (!blk || !blk.entities) break;
          const p = ent.position || {};
          const pt = xf(p.x || 0, p.y || 0);
          const sub = dxfSvgEl('g', {});
          parent.appendChild(sub);
          blk.entities.forEach(e2 => renderEntity(e2, sub, { dx: pt[0], dy: pt[1], rotDeg: ent.rotation || 0, sx: ent.xScale || 1, sy: ent.yScale || 1 }));
          break;
        }
        case 'DIMENSION': {
          if (ent.block && blocks[ent.block] && blocks[ent.block].entities) {
            const p = ent.anchorPoint || ent.middleOfText || {};
            const pt = xf(p.x || 0, p.y || 0);
            const sub = dxfSvgEl('g', {});
            parent.appendChild(sub);
            blocks[ent.block].entities.forEach(e2 => renderEntity(e2, sub, { dx: pt[0], dy: pt[1], rotDeg: 0, sx: 1, sy: 1 }));
          }
          break;
        }
        case 'SOLID': {
          const pts = ent.points || [];
          if (pts.length >= 3) {
            let d = '';
            pts.slice(0, 4).forEach((p, i) => { const pt = xf(p.x || 0, p.y || 0); d += (i ? ' L ' : 'M ') + pt[0] + ' ' + pt[1]; grow(pt[0], pt[1]); });
            d += ' Z';
            parent.appendChild(dxfSvgEl('path', { d, fill: dxfEntityColor(ent, layers), stroke: 'none', opacity: '0.6' }));
          }
          break;
        }
        default:
          break;
      }
    } catch (e) { /* 单实体异常不中断整图 */ }
  }

  entities.forEach(ent => renderEntity(ent, content, null));

  if (!isFinite(bb.minX) || bb.minX === bb.maxX) { bb.minX -= 1; bb.maxX += 1; }
  if (!isFinite(bb.minY) || bb.minY === bb.maxY) { bb.minY -= 1; bb.maxY += 1; }
  const pad = Math.max((bb.maxX - bb.minX), (bb.maxY - bb.minY)) * 0.05 || 1;
  bb.minX -= pad; bb.maxX += pad; bb.minY -= pad; bb.maxY += pad;

  // viewBox：DXF Y-up → SVG Y-down（y 取负）
  const fitVb = { x: bb.minX, y: -bb.maxY, w: bb.maxX - bb.minX, h: bb.maxY - bb.minY };
  let s = 1, cxv = fitVb.x + fitVb.w / 2, cyv = fitVb.y + fitVb.h / 2;
  const applyVb = () => {
    const w = fitVb.w / s, h = fitVb.h / s;
    svg.setAttribute('viewBox', (cxv - w / 2) + ' ' + (cyv - h / 2) + ' ' + w + ' ' + h);
  };
  const fit = () => { s = 1; cxv = fitVb.x + fitVb.w / 2; cyv = fitVb.y + fitVb.h / 2; applyVb(); };
  fit();
  btnZoomIn.onclick = () => { s = Math.min(200, s * 1.3); applyVb(); };
  btnZoomOut.onclick = () => { s = Math.max(0.05, s / 1.3); applyVb(); };
  btnFit.onclick = fit;
  canvas.onwheel = (e) => { e.preventDefault(); const f = e.deltaY < 0 ? 1.15 : 1 / 1.15; s = Math.max(0.05, Math.min(200, s * f)); applyVb(); };
  let dragging = false, lastX = 0, lastY = 0;
  canvas.onmousedown = (e) => { dragging = true; lastX = e.clientX; lastY = e.clientY; canvas.classList.add('grabbing'); };
  const onDxfMove = (e) => {
    if (!dragging) return;
    const dx = e.clientX - lastX, dy = e.clientY - lastY;
    lastX = e.clientX; lastY = e.clientY;
    const vbW = fitVb.w / s, vbH = fitVb.h / s;
    const cw = canvas.clientWidth || 1, ch = canvas.clientHeight || 1;
    cxv -= dx * vbW / cw; cyv -= dy * vbH / ch;
    applyVb();
  };
  const onDxfUp = () => { dragging = false; canvas.classList.remove('grabbing'); };
  window.addEventListener('mousemove', onDxfMove);
  window.addEventListener('mouseup', onDxfUp);
  bindViewerTeardown(container, () => {
    window.removeEventListener('mousemove', onDxfMove);
    window.removeEventListener('mouseup', onDxfUp);
  });
  canvas.classList.add('grab');

  meta.textContent = entities.length + ' ' + t('个实体');
}

/* ── docx-preview 渲染器：保留标题/表格/图片/列表，支持批注 ── */
async function setupDocxPreview(container, filePath, root, source) {
  container.innerHTML = '';
  const loading = el('div', 'docx-loading', t('正在加载 Word 文档…'));
  loading.style.cssText = 'padding:40px;text-align:center;color:var(--text-dim)';
  container.append(loading);

  // .doc 旧格式提示
  if (isDocPath(filePath)) {
    loading.remove();
    container.innerHTML = '<div style="padding:40px;text-align:center;color:var(--warn);">' + t('旧版 .doc 格式不支持在线预览，建议在 Word/WPS 中另存为 .docx 后打开。') + '</div>';
    return;
  }

  const token = state.token || (state.config && state.config.accessToken) || '';
  const qp = new URLSearchParams();
  qp.set('path', filePath);
  if (source) qp.set('source', source); else qp.set('root', root || 'workspace');
  if (token) qp.set('access_token', token);
  const rawUrl = '/api/file/raw?' + qp.toString();

  try {
    const r = await fetch(rawUrl);
    if (!r.ok) throw new Error('HTTP ' + r.status);
    const buf = await r.arrayBuffer();

    const toolbar = el('div', 'docx-toolbar');
    const info = el('span', 'docx-info', filePath.split('/').pop());
    const btnToggleComments = el('button', 'docx-ctrl', t('批注'));
    btnToggleComments.title = t('切换批注面板');
    const btnClose = el('button', 'docx-ctrl', '✕');
    toolbar.append(info, btnToggleComments, btnClose);

    const docxBody = el('div', 'docx-body');
    const commentPanel = el('div', 'docx-comment-panel hidden');
    commentPanel.innerHTML = '<div class="dcp-head"><strong>' + t('批注') + '</strong><button class="dcp-close quiet">×</button></div><div class="dcp-list"></div>';

    const wrap = el('div', 'docx-wrap');
    wrap.append(toolbar, docxBody);
    container.append(wrap, commentPanel);

    btnClose.onclick = () => { const dlg = container.closest('dialog'); if (dlg) dlg.close(); else if (document.body.classList.contains('file-view-mode')) history.back(); };

    await window.docx.renderAsync(buf, docxBody, null, {
      className: 'docx-rendered',
      inWrapper: true,
      ignoreWidth: false,
      ignoreHeight: false,
      experimental: true,
    });
    loading.remove();

    // ── 批注：跨节点 Range 定位 + 高亮 + 联动 ──
    await new Promise(r => setTimeout(r, 300)); // 等 docx-preview DOM 稳定
    const listEl = commentPanel.querySelector('.dcp-list');
    let comments = [];
    let nativeVersion = null;
    // 文档内容哈希：与服务端 files.go hash() 口径一致（SHA-256(raw bytes) 小写 hex），
    // 用于批注锚点 stale 判定；非安全上下文（无 crypto.subtle）回落旧的占位值，不阻断功能。
    let docHash = btoa(String(buf.byteLength)).slice(0, 16);
    try {
      const dg = await crypto.subtle.digest('SHA-256', buf);
      docHash = [...new Uint8Array(dg)].map(b => b.toString(16).padStart(2, '0')).join('');
    } catch (e) { /* 回落占位值 */ }

    // 在容器内跨节点查找 quote 第 N 次出现，返回 Range
    function findTextRange(container, quote, wantIndex) {
      if (!quote) return null;
      // 排除 STYLE/SCRIPT（docx-preview 注入的 <style> 文本含大量被折叠空白，不可作为正文锚点）
      const walker = document.createTreeWalker(container, NodeFilter.SHOW_TEXT, {
        acceptNode(node) {
          const p = node.parentElement;
          if (p && (p.tagName === 'STYLE' || p.tagName === 'SCRIPT')) return NodeFilter.FILTER_REJECT;
          return NodeFilter.FILTER_ACCEPT;
        }
      });
      // 边遍历边构建规范化文本 norm 与偏移 map，二者严格 1:1：
      // 连续空白折叠为一个空格（映射到该空白段首个字符），被折叠的后续空白不产生条目。
      // 这样在 norm 里 indexOf 后用同一下标取 map，Range 不会错位进 CSS 文本。
      let norm = '';
      const map = []; // norm char index → {node, off}
      let prevWS = false;
      while (walker.nextNode()) {
        const node = walker.currentNode;
        const t = node.textContent;
        for (let i = 0; i < t.length; i++) {
          const ch = t[i];
          if (/\s/.test(ch)) {
            if (prevWS) continue;
            prevWS = true;
            map.push({ node, off: i });
            norm += ' ';
          } else {
            prevWS = false;
            map.push({ node, off: i });
            norm += ch;
          }
        }
      }
      const normQuote = quote.trim().replace(/\s+/g, ' ');
      let found = 0, pos = 0;
      while (true) {
        const idx = norm.indexOf(normQuote, pos);
        if (idx < 0) break;
        if (found === wantIndex) {
          const startMap = map[idx], endMap = map[idx + normQuote.length - 1];
          if (startMap && endMap) {
            const range = document.createRange();
            range.setStart(startMap.node, startMap.off);
            range.setEnd(endMap.node, endMap.off + 1);
            return range;
          }
        }
        found++;
        pos = idx + 1;
      }
      return null;
    }

    function clearHighlights() {
      docxBody.querySelectorAll('.comment-anchor-hl').forEach(el => {
        const parent = el.parentNode;
        parent.replaceChild(document.createTextNode(el.textContent), el);
        parent.normalize();
      });
    }

    function highlightComment(c) {
      clearHighlights();
      const range = findTextRange(docxBody, c.anchorQuote, c.anchorIndex || 0);
      if (!range) { c.stale = true; return; }
      c.stale = false;
      try {
        const hl = document.createElement('mark');
        hl.className = 'comment-anchor-hl';
        range.surroundContents(hl);
      } catch (e) {
        // surroundContents fails when range spans multiple nodes; extract/insert
        try {
          const frag = range.extractContents();
          const hl = document.createElement('mark');
          hl.className = 'comment-anchor-hl';
          hl.appendChild(frag);
          range.insertNode(hl);
        } catch (e2) {}
      }
    }

    async function loadComments() {
      try {
        const query = new URLSearchParams({path: filePath});
        if (source) query.set('source', source);
        nativeVersion = await api('/office/docx/comments?' + query.toString());
        comments = (nativeVersion.comments || []).map(c => ({...c, native: true, stale: !c.anchorValid}));
      } catch (e) { nativeVersion = null; comments = []; toast(t('DOCX 批注加载失败：') + e.message); }
      try {
        // Older sidecar notes are retained and labelled; they are not in the download.
        const legacy = await api('/comments?path=' + encodeURIComponent(filePath));
        comments.push(...(legacy.comments || []).map(c => ({...c, native: false})));
      } catch (e) { /* native comments remain available */ }
      renderComments();
      // 高亮所有批注锚点
      comments.forEach(c => { if (c.status !== 'resolved') highlightComment(c); });
    }

    function renderComments() {
      listEl.innerHTML = '';
      if (!comments.length) { listEl.innerHTML = '<p class="muted" style="padding:12px">' + t('暂无批注') + '</p>'; return; }
      const written = new Set(comments.filter(c => c.native).map(c => JSON.stringify([c.text, c.anchorQuote])));
      comments.forEach(c => {
        const card = el('div', 'comment-card' + (c.status === 'resolved' ? ' resolved' : '') + (c.stale ? ' stale' : ''));
        card.innerHTML = '<div class="cc-text"></div><div class="cc-meta"></div><div class="cc-actions"></div>';
        card.querySelector('.cc-text').textContent = c.text;
        const alreadyWritten = written.has(JSON.stringify([c.text, c.anchorQuote]));
        card.querySelector('.cc-meta').textContent = (c.author ? c.author : '') + (c.native ? ' · DOCX' : ' · ' + t(alreadyWritten ? '旧批注：已写入文档' : '旧批注：未写入文件')) + (c.stale ? ' · ' + t('锚点可能失效') : '');
        if (!c.native && !alreadyWritten && c.status !== 'resolved' && nativeVersion && !nativeVersion.readOnly) {
          const migrate = el('button', 'docx-ctrl', t('写入 DOCX'));
          migrate.onclick = async (event) => {
            event.stopPropagation();
            try {
              const saved = await api('/office/docx/comments', {method:'POST',body:JSON.stringify({path:filePath,source,hash:nativeVersion.hash,workspaceId:nativeVersion.workspaceId,quote:c.anchorQuote,anchorIndex:c.anchorIndex || 0,text:c.text,author:c.author || 'aide'})});
              nativeVersion.hash = saved.hash;
              toast(t('批注已写入 DOCX'));
              await loadComments();
            } catch (e) { toast(t('写入 DOCX 失败：') + e.message); }
          };
          card.querySelector('.cc-actions').append(migrate);
        }
        // 点击批注 → 滚动到高亮
        card.style.cursor = 'pointer';
        card.onclick = () => {
          highlightComment(c);
          const hl = docxBody.querySelector('.comment-anchor-hl');
          if (hl) hl.scrollIntoView({ behavior: 'smooth', block: 'center' });
        };
        listEl.append(card);
      });
    }

    btnToggleComments.onclick = () => {
      commentPanel.classList.toggle('hidden');
      if (!commentPanel.classList.contains('hidden')) loadComments();
    };
    commentPanel.querySelector('.dcp-close').onclick = () => commentPanel.classList.add('hidden');

    // 文本选中 → 添加批注
    docxBody.addEventListener('mouseup', async () => {
      const sel = window.getSelection();
      const text = sel.toString().trim();
      if (!text || text.length < 2) return;
      if (text.length > 4000) { toast(t('选中文本过长，请缩小范围')); return; }
      if (nativeVersion && nativeVersion.readOnly) { toast(t('该引用为只读，无法写入批注')); return; }
      const quote = text;
      // 计算 anchorIndex：选中片段是 quote 在规范化全文中的第几次出现（0 起始）。
      // 用覆盖 docxBody 起点到选区起点(anchorNode,anchorOffset) 的 Range 取前缀文本并做与
      // findTextRange 一致的空白规范化，统计 normQuote 在该前缀中的出现次数；选区起点处的
      // 本次出现不计入前缀，天然得到正确 0-based 序号（修复原 anchorIndex 自赋值 no-op 恒为 0）。
      const normQuote = quote.replace(/\s+/g, ' ');
      let anchorIndex = 0;
      try {
        const preRange = document.createRange();
        preRange.setStart(docxBody, 0);
        preRange.setEnd(sel.anchorNode, sel.anchorOffset);
        const prefix = preRange.toString().replace(/\s+/g, ' ');
        let pos = 0;
        while (true) {
          const idx = prefix.indexOf(normQuote, pos);
          if (idx < 0) break;
          anchorIndex++;
          pos = idx + 1;
        }
      } catch (e) { anchorIndex = 0; }
      const comment = prompt(t('添加批注：') + quote.slice(0, 40) + '…', '');
      if (!comment) { sel.removeAllRanges(); return; }
      try {
        if (!nativeVersion) throw new Error(t('批注尚未加载'));
        const saved = await api('/office/docx/comments', { method: 'POST', body: JSON.stringify({ path: filePath, source, hash: nativeVersion.hash || docHash, workspaceId: nativeVersion.workspaceId, quote, anchorIndex, text: comment }) });
        nativeVersion.hash = saved.hash;
        toast(t('批注已添加'));
        await loadComments();
        // 高亮刚加的
        const last = comments[comments.length - 1];
        if (last) highlightComment(last);
      } catch (e) { toast(e.message); }
      sel.removeAllRanges();
    });

    await loadComments();
  } catch (e) {
    loading.remove();
    container.innerHTML = '<div style="padding:40px;text-align:center;color:var(--warn);">' + t('Word 文档加载失败：') + escapeHtml(e.message) + '</div>';
  }
}


/* fixCjkEmphasis：修复 CommonMark 强调 flanking 规则在中文场景的痛点。
   当加粗/斜体内容以全角标点（如 ）。！？】》）结尾、闭合标记 ** / * 后又直接跟
   字母或汉字时，marked 会依据 flanking 规则判定该标记“不能闭合”，导致星号裸露
   （如「**安全（safe）**与」渲染成字面 **）。这里把阻碍闭合的前导标点临时替换成
   私用区占位符（非标点非空白），marked 即可正确配对；解析后再把占位符还原为原
   标点——最终文本不留任何占位/零宽字符。围栏代码块与行内代码不处理。 */
function fixCjkEmphasis(src) {
  const map = new Map();
  let ph = 0xE000;
  const take = (ch) => {
    for (const [k, v] of map) if (v === ch) return k;
    const k = String.fromCodePoint(ph++);
    map.set(k, ch);
    return k;
  };
  const lines = String(src || '').split('\n');
  let fence = null;
  for (let i = 0; i < lines.length; i++) {
    const line = lines[i];
    const fm = line.match(/^\s{0,3}(```|~~~)/);
    if (fm) { if (fence === null) fence = fm[1][0]; else fence = null; continue; }
    if (fence !== null) continue;
    const segs = line.split('`');
    for (let j = 0; j < segs.length; j += 2) {
      segs[j] = segs[j].replace(/([^\P{P}*_])(\*\*|__|\*|_)(?=[\p{L}\p{N}])/gu, (m, p, d) => take(p) + d);
    }
    lines[i] = segs.join('`');
  }
  const text = lines.join('\n');
  const restore = (html) => { for (const [k, v] of map) html = html.split(k).join(v); return html; };
  return { text, restore };
}
function renderMarkdown(src, live, basePath) {
  if (window.marked && typeof window.marked.parse === 'function') {
    const fixed = fixCjkEmphasis(String(src || ''));
    const html = fixed.restore(window.marked.parse(fixed.text, { gfm: true, breaks: false }));
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
      if (!lang || !window.hljs || typeof window.hljs.getLanguage !== 'function') return;
      // highlight.js 未单独提供 clojurescript 语法，回退到 clojure（语法超集）
      const langAlias = { clojurescript: 'clojure' };
      let hlLang = window.hljs.getLanguage(lang) ? lang : (langAlias[lang] && window.hljs.getLanguage(langAlias[lang]) ? langAlias[lang] : '');
      if (!hlLang) return; // 未注册语言保持原样，避免错误着色
      try { codeEl.innerHTML = window.hljs.highlight(codeEl.textContent, { language: hlLang, ignoreIllegals: true }).value; }
      catch (_) {}
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
        img.onerror = () => { img.style.opacity = '0.4'; img.title = t('图片加载失败: {0}', orig); };
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
  if ($('file-view-editor').parentNode.classList.contains('code-wrapper')) $('file-view-editor').parentNode.classList.toggle('hidden', preview);
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
  const isPdf = isPdfPath(spec.path);
  const isDxf = isDxfPath(spec.path);
  const isDocx = isDocxPath(spec.path);
  const isXlsx = isXlsxPath(spec.path);
  const isZip = isZipPath(spec.path);
  // 只有二进制/画布查看器需要占满剩余空间并自行处理滚动；Markdown
  // 预览必须保留外层滚动容器，避免被沉浸式查看器样式锁死。
  $('file-view-preview').classList.toggle('file-view-immersive', isImg || isStl || isPdf || isDxf || isDocx || isXlsx || isDrawio);
  // 图片 / STL / PDF / DXF 走独立 raw 查看器，跳过只支持文本、会拒绝二进制的 /api/file
  let data;
  if (isImg || isStl || isPdf || isDxf || isDocx || isXlsx || isZip) {
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
  // #64: code syntax highlighting
  teardownCodeHighlight($('file-view-editor'));
  teardownViewer($('file-view-preview')); // file-view 页签切文件前回收上一个查看器
  var _fvcl = codeLang(spec.path);
  if (_fvcl) setupCodeHighlight($('file-view-editor'), _fvcl);
  // 图片 / STL / PDF 只读查看器禁用保存；drawio 可保存
  $("file-view-save").classList.toggle("hidden", isImg || isStl || isPdf || isDxf || isDocx || isXlsx || isZip);
  $("file-view-save").disabled = readOnly;
  $("file-view-ro-badge").classList.toggle("hidden", !(readOnly || isImg || isStl || isPdf || isDxf || isDocx || isZip));
  if (isZip) {
    $('file-view-editor').classList.add('hidden');
    $('file-view-preview').classList.remove('hidden');
    setupZipPreview($('file-view-preview'), spec.path, spec.root, spec.source || '');
  } else if (isStl) {
    $('file-view-editor').classList.add('hidden');
    $('file-view-preview').classList.remove('hidden');
    setupStlPreview($('file-view-preview'), spec.path, spec.root, spec.source || '');
  } else if (isPdf) {
    $('file-view-editor').classList.add('hidden');
    $('file-view-preview').classList.remove('hidden');
    setupPdfPreview($('file-view-preview'), spec.path, spec.root, spec.source || '');
  } else if (isDxf) {
    $('file-view-editor').classList.add('hidden');
    $('file-view-preview').classList.remove('hidden');
    setupDxfPreview($('file-view-preview'), spec.path, spec.root, spec.source || '');
  } else if (isDocx) {
    $('file-view-editor').classList.add('hidden');
    $('file-view-preview').classList.remove('hidden');
    setupDocxPreview($('file-view-preview'), spec.path, spec.root, spec.source || '');
  } else if (isXlsx) {
    $('file-view-editor').classList.add('hidden');
    $('file-view-preview').classList.remove('hidden');
    setupXlsxPreview($('file-view-preview'), spec.path, spec.root, spec.source || '');
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
  $('file-view-content').replaceChildren();
}
$('fv-edit').onclick = () => setFileViewMode('edit');
$('fv-preview').onclick = () => setFileViewMode('preview');
$('file-view-save').onclick = action(async () => {
  const body = { path: fileView.spec.path, content: $('file-view-editor').value, hash: fileView.hash };
  if (fileView.spec.source) body.source = fileView.spec.source; if (fileView.wsId) body.workspaceId = fileView.wsId;
  const res = await api('/file', { method: 'PUT', body: JSON.stringify(body) });
  fileView.hash = res.hash;
  toast(t("✓ 已保存"));
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
  if (!session || (session.kind === 'assistant' ? !session.messages?.length : !session.runs?.length)) {
    host.append(el('p', 'muted', t("当前会话还没有任务。发送任务后，这里会按事件时间线记录完整轨迹。")));
    return;
  }
  const fmtSeg = $('traj-fmt-seg');
  if (trajView === 'calls') { if (fmtSeg) fmtSeg.classList.add('hidden'); if (session.kind === 'assistant') host.append(el('p', 'muted', t('小秘会话记录的是对话历史，没有工作台工具调用记录。'))); else renderCallsAnalysis(host, session); return; }
  if (fmtSeg) fmtSeg.classList.remove('hidden');
  if (trajFmt === 'json') {
    const pre = el('pre', 'traj-raw-view json-view');
    pre.innerHTML = highlightJSON(buildTrajectoryJSON(session)); // JSON 语法高亮
    host.append(pre);
  } else {
    const md = el('div', 'traj-md-view md-body');
    md.innerHTML = renderMarkdown(buildTrajectoryMarkdown(session)); // Markdown 渲染为 HTML（含 mermaid）
    host.append(md);
  }
}
function buildTrajectoryMarkdown(s) {
  const subs = (state.sessions || []).filter(x => x.parentId === s.id);
  let md = "# " + (s.title || t("未命名会话")) + "\n\n";
  md += t("> 导出时间：{0} · 会话 ID：{1}\n\n---\n\n", new Date().toLocaleString(), s.id);
  if (s.kind === 'assistant') {
    for (const m of (s.messages || [])) md += `## ${m.role === 'user' ? t('用户') : t('小秘')}\n\n${m.content || ''}\n\n---\n\n`;
    return md;
  }
  for (const r of (s.runs || [])) {
    md += t("## 任务 · {0}\n\n", r.created || "");
    md += t("**用户：** {0}\n\n", r.prompt || "");
    if (r.usage?.total) md += t("**Token 消耗：** {0}{1}\n\n", r.usage.total, r.usage.estimated ? t("（估）") : "");
    if (r.steps) for (const st of r.steps) md += "- " + st.name + " · " + st.status + (st.content ? "\n  > " + String(st.content).slice(0,500) : "") + "\n";
    if (r.toolUses) for (const tu of r.toolUses) {
      md += t("**工具 {0}：**\n```\n", tu.tool) + String(tu.preview || tu.result || "") + "\n```\n\n";
    }
    if (r.error) md += t("**错误：** {0}\n\n", r.error);
    md += "\n---\n\n";
  }
  for (const sub of subs) {
    md += t("## 子会话：{0}\n\n", sub.title || sub.id);
    for (const r of (sub.runs || [])) {
      md += "### " + (r.created || "") + "\n\n";
      md += t("**用户：** {0}\n\n", r.prompt || "");
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
    session: { id: s.id, title: s.title, created: s.created, updated: s.updated, parentId: s.parentId, kind: s.kind },
    messages: (s.messages || []).map(m => ({ role: m.role, type: m.type, content: m.content })),
    runs: (s.runs || []).map(r => ({
      id: r.id, created: r.created, prompt: r.prompt, mode: r.mode, model: r.model,
      status: r.status, error: r.error, usage: r.usage,
      steps: r.steps, toolUses: r.toolUses, files: r.files, commands: r.commands
    })),
    subSessions: subs.map(sub => ({ id: sub.id, title: sub.title, created: sub.created, runs: sub.runs })),
    timeline: []
  };
  if (s.kind === 'assistant') data.timeline = (s.messages || []).map((m, index) => ({ index, type: m.role, messageType: m.type, content: m.content }));
  for (const r of (s.runs || [])) {
    data.timeline.push({ time: r.created, type: 'user', content: r.prompt });
    for (const st of (r.steps || [])) data.timeline.push({ time: r.created, type: 'step', name: st.name, status: st.status });
    for (const tu of (r.toolUses || [])) data.timeline.push({ time: r.created, type: 'tool', tool: tu.tool, args: tu.args, result: tu.preview || tu.result });
  }
  return JSON.stringify(data, null, 2);
}
// JSON 语法高亮：先转义防 XMS/破坏，再给 key/string/number/boolean/null 上色
function highlightJSON(json) {
  const esc = String(json).replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;');
  return esc.replace(
    /("(?:\u[a-fA-F0-9]{4}|\[^u]|[^\\"])*"(?:\s*:)?|\b(?:true|false|null)\b|-?\d+(?:\.\d+)?(?:[eE][+\-]?\d+)?)/g,
    (m) => {
      let cls = 'json-number';
      if (m.startsWith('"')) cls = /:\s*$/.test(m) ? 'json-key' : 'json-string';
      else if (/true|false/.test(m)) cls = 'json-boolean';
      else if (/null/.test(m)) cls = 'json-null';
      return '<span class="' + cls + '">' + m + '</span>';
    });
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
  host.replaceChildren();
  host.append(el('p', 'muted', t('加载调用记录…')));
  (async () => {
    try {
      const resp = await api('/sessions/' + session.id + '/tool-calls?who=all');
      const calls = (resp.calls || []).map(c => ({
        agent: c.who === 'sub' ? 'sub' : 'main',
        agentName: c.who === 'sub' ? (t('子 Agent · #{0}', c.childNumber || '?') + (c.childTitle ? ' ' + c.childTitle : '')) : t('主 Agent'),
        time: c.time,
        tool: c.tool,
        args: c.args,
        result: c.result,
        ok: c.ok
      }));
      renderCallsTable(host, calls, resp.stats);
    } catch(e) {
      host.replaceChildren();
      host.append(el('p', 'muted', t('加载失败：{0}', e.message)));
    }
  })();
}
function renderCallsTable(host, calls, stats) {
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
  const now = Date.now();
  const tms = { '1h': 3600000, '24h': 86400000, '7d': 604800000 };
  const filtered = calls.filter(c => {
    if (callFilter.tool && c.tool !== callFilter.tool) return false;
    if (callFilter.agent !== 'all' && c.agent !== callFilter.agent) return false;
    if (callFilter.type !== 'all' && callTypeOf(c.tool) !== callFilter.type) return false;
    if (callFilter.time !== 'all' && c.time && now - new Date(c.time).getTime() > tms[callFilter.time]) return false;
    return true;
  });
  // 统计：优先用后端 stats，否则本地计算
  const mainN = stats ? stats.mainCount : filtered.filter(c => c.agent === 'main').length;
  const subN = stats ? stats.childCount : filtered.filter(c => c.agent === 'sub').length;
  const failN = stats ? stats.failCount : filtered.filter(c => c.ok === false || /失败|error|拒绝|fail/i.test(String(c.result || ''))).length;
  host.append(el('div', 'call-summary', t('共 {0} 次 · 主 {1} 子 {2} 失败 {3}', filtered.length, mainN, subN, failN)));
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
  if (s.kind === 'assistant' && !s.messages?.length) return toast(t('小秘历史当前不可用，请先解锁会话'));
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
let searchSeq = 0;
$('global-search').addEventListener('input', () => {
  clearTimeout(searchTimer);
  const q = $('global-search').value.trim();
  if (!q) { $('search-results').classList.add('hidden'); return; }
  searchTimer = setTimeout(action(async () => {
    const mySeq = ++searchSeq;
    const data = await api('/search?q=' + encodeURIComponent(q));
    if (mySeq !== searchSeq) return; // 已有更新的查询，丢弃旧响应
    const host = $('search-results');
    host.replaceChildren();
    if (!data.results?.length) { host.append(el('p', 'muted', t("没有匹配的聊天"))); }
    data.results.forEach(res => {
      const row = el('button', 'search-result', '');
      const badge = res.archived ? el('span', 'archived-badge', t('已归档')) : null;
      row.append(el('strong', '', (res.number > 0 ? '#' + res.number + ' ' : '') + res.title), badge, el('span', '', res.snippet));
      row.onclick = action(async () => {
        $('search-results').classList.add('hidden'); $('global-search').value = '';
        if (res.archived) {
          await api(`/sessions/${res.sessionId}`, { method: 'PATCH', body: JSON.stringify({ archived: false }) });
          await loadSessions();
        }
        await selectSession(res.sessionId);
      });
      host.append(row);
    });
    host.classList.remove('hidden');
  }), 300);
});
// 聚焦即展开搜索面板：空输入时给出提示，消除「可输入但无反应」的假可点观感。
$('global-search').addEventListener('focus', () => {
  if ($('global-search').value.trim()) return;
  const host = $('search-results');
  host.replaceChildren(el('p', 'muted', t("输入关键字搜索聊天记录")));
  host.classList.remove('hidden');
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
    if (path) openFile(path).catch(() => toast(t("打不开文件: {0}", path)));
    return;
  }
  if (!event.target.closest('.global-search')) $('search-results').classList.add('hidden');
});
/* ── 手动压缩（FR-93） ── */
function refreshCompactInfo() {
  const btn = $('compact-button');
  if (!btn) return;
  btn.classList.toggle('has-compacted', (state.session?.compactedMessages || 0) > 0);
}
$('compact-button').onclick = action(async () => {
  if (!state.session) { toast(t("请先选择会话")); return; }
  const res = await api('/sessions/' + state.session.id + '/compact', { method: 'POST', body: '{}' });
  toast(res.folded ? t("已压缩 {0} 条历史消息", res.folded) : t("历史未超阈值，无需压缩"));
  await selectSession(state.session.id);
  refreshCompactInfo();
});
async function initialize() {
  syncPanelButtons();
  await refreshConfig();
  const fragment = new URLSearchParams(location.hash.slice(1));
  if (fragment.has('file')) { await Promise.all([loadWorkspaceConfig(), loadSourcesList()]); await openFileViewMode(); return; }
  await Promise.all([loadSessions(), loadFiles(), loadProfiles(), loadWorkspaceConfig(), loadSourcesList()]);
  setupGlobalEvents(); // #60
}
initialize()
  .then(() => {
    // 已配置密码：先入集群，加入窗口期显示中性面纱，等选举结果再决定遮罩。
    // ① master 未锁 → slave 不锁；② master 锁 → slave 跟随；③ slave 单解不回传。
    if (state.config && state.config.hasPassword && !lockScreen.locked) {
      if (!lockScreenActive()) {
        // 文件只读查看标签：不卡 joining 面纱、不参与选举；一次 1s 短超时后端权威核对即可。
        checkFileViewLock();
      } else if (typeof LockCluster !== 'undefined') {
        // 后端权威已明确且选举已收敛：权威“未锁”直接放行，尽量不闪 joining 面纱；
        // 收敛中或权威“锁定”才显示中性面纱，等 onReady 决定遮罩。
        const snap = LockCluster.snapshot();
        if (snap.settled && !snap.locked) {
          // 权威未锁：跳过面纱，直接进入界面
        } else {
          showJoiningVeil();
          LockCluster.onReady().then(effective => {
            if (effective) applyLockVisual(); else releaseLockVisual();
          });
        }
      } else {
        lockScreenNow(); // 降级（无 LockCluster）：旧行为本地即锁
      }
    }
  })
  .catch(error => { if (!$('login-dialog').open) $('login-dialog').showModal(); $('login-error').textContent = state.token ? error.message : ''; });

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
if (window.mermaid) mermaid.initialize({ startOnLoad: false, theme: 'neutral', securityLevel: 'strict' });
async function renderMermaid() {
  if (!window.mermaid) return;
  document.querySelectorAll('div.mermaid:not([data-processed])').forEach(async el => {
    try {
      const { svg } = await mermaid.render('m' + Math.random().toString(36).slice(2), el.textContent);
      el.innerHTML = svg; el.dataset.processed = '1';
    } catch (e) { el.innerHTML = `<pre style="color:#f87171">${t('流程图渲染失败')}</pre>`; el.dataset.processed = '1'; }
  });
}
if (!document.body.classList.contains('file-view-mode') && state.config) { action(loadSessions)(); action(loadFiles)(); }
// 每次 renderMarkdown 后触发 mermaid 渲染
const _origRender = renderMarkdown;
renderMarkdown = function(src, live) { const html = _origRender(src, live); setTimeout(renderMermaid, 50); return html; };
  if (fileView.spec) $('file-view-ro-badge').classList.toggle('hidden', !$('file-view-editor').readOnly);
  if (!$('strategy-menu').classList.contains('hidden')) openStrategyMenu();
  if (state.contextPreview) renderContextPreview(state.contextPreview);
});

/* ── 语音小秘（Web Speech API 实时断句 + AI 甄别 + 直接发送）──────────
   边说边断：按句末标点/停顿自动成句 → 后端甄别 → 判定为指令的句子直接
   发送到当前会话（与手动提交同一 run 入口，兼容排队/工作流模式）；
   背景声自动忽略；与他人闲聊自动退下。语音框记录「已发送/已忽略」。 */
const voice = {
  recognition: null, listening: false, starting: false, startToken: 0, standby: false, awaitingReply: false, halted: false,
  buffer: '', interim: '', baseStatus: '', timer: null, sending: false, queue: [], log: [], micStream: null,
  queuedOverride: null // #41：小秘 analyze 判定的本次发送排队/插队（一次性，onsubmit 消费后清零）
};
voice.name = () => (state.config && state.config.voiceAssistantName) || t('小秘');
voice.supported = ('SpeechRecognition' in window) || ('webkitSpeechRecognition' in window);
try {
  if ('speechSynthesis' in window) {
    window.speechSynthesis.getVoices();
    window.speechSynthesis.onvoiceschanged = () => window.speechSynthesis.getVoices();
  }
} catch (_) {}

function voiceSetStatus(mode, text) {
  const box = $('voice-status');
  box.classList.remove('listening', 'ignored', 'standby', 'requesting');
  if (mode) box.classList.add(mode);
  voice.baseStatus = text;
  // 镜像 mode 到面板根：供左侧 8px 状态圆点按模式着色（仅视觉，不改行为）
  const panel = $('voice-panel');
  panel.classList.remove('listening', 'ignored', 'standby', 'requesting');
  if (mode) panel.classList.add(mode);
  voiceRenderStatus();
}
// 状态文字单行内联：正在识别的 interim 以“… ”前缀显示于此，否则恢复基础状态；超长由 CSS ellipsis 截断
function voiceRenderStatus() {
  $('voice-status-text').textContent = voice.interim ? '… ' + voice.interim : (voice.baseStatus || '');
  const pending = voice.queue.length + (voice.sending ? 1 : 0);
  $('voice-queue-status').textContent = pending ? t('待处理') + ' ' + pending : '';
  $('voice-queue-status').classList.toggle('hidden', !pending);
}
function voiceRenderLog() {
  const host = $('voice-text');
  host.replaceChildren();
  for (const item of voice.log) {
    const line = el('div', 'voice-log-line ' + (item.type === 'sent' ? 'is-sent' : item.type === 'ignored' ? 'is-ignored' : 'is-standby'));
    const label = item.type === 'sent' ? t('已发送') : item.type === 'ignored' ? t('已忽略') : item.type === 'ask' ? t('追问') : t('已退下');
    const tag = el('span', 'voice-log-tag', label);
    const chips = el('span', 'voice-log-chips');
    if (item.type === 'sent' && item.mode === 'insert') chips.append(el('span', 'voice-log-mode is-insert', t('插队')));
    else if (item.type === 'sent' && item.mode === 'queue') chips.append(el('span', 'voice-log-mode is-queue', t('排队')));
    const right = el('span', 'voice-log-text');
    if (item.text) right.append(el('span', '', item.text));
    if (item.reason) right.append(el('small', 'voice-log-reason', item.reason));
    line.append(tag, chips, right);
    host.append(line);
  }
  if (voice.interim) host.append(el('div', 'voice-log-interim', '… ' + voice.interim));
  host.scrollTop = host.scrollHeight;
  voiceRenderStatus();
}
function voiceLog(type, text, reason, mode) {
  voice.log.push({ type, text, reason, mode });
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
async function voiceSend(text, queued) {
  if (!state.config?.configured) throw new Error(t('请先配置模型'));
  let target = state.session, created = null;
  if (!target) {
    created = await api('/sessions', { method: 'POST', body: JSON.stringify({ title: t('新会话') }) });
    if (!state.session) state.session = created;
    target = created;
  }
  const strategy = state.profiles?.strategy || 'auto';
  // #41：小秘 analyze 判定的 mode 优先；未给出时回退手动排队开关
  const q = (queued != null) ? queued : state.queueMode;
  await api(`/sessions/${target.id}/runs`, { method: 'POST', body: JSON.stringify({ prompt: text, mode: state.mode, attachments: [], strategy, profile: strategy === 'auto' ? '' : (state.profiles?.activeProfile || 'default'), queued: q, workflowPhase: state.workflowPhase || '' }) });
  if (state.session?.id === target.id) await selectSession(target.id);
}
// 提取主会话近期对话（供小秘对话/讲解感知 aide 内容）
function buildAideContextText(){
  const s=state.session; if(!s) return '';
  const out=[]; let len=0;
  for(const m of (s.messages||[])){
    const c=String(m.content||'').trim(); if(!c) continue;
    const line = m.role==='user' ? '用户：'+c : m.role==='assistant' ? 'aide：'+c : '';
    if(!line) continue;
    out.push(line); len+=line.length;
    if(len>2600) break;
  }
  return out.slice(-10).join('\n').slice(-2600);
}
async function voiceFilterOne(sentence) {
  let result = { action: 'ignore', text: sentence, reason: '' };
  try { result = await api('/voice-filter', { method: 'POST', body: JSON.stringify({ text: sentence, context: buildAideContextText() }) }); }
  catch (_) { result = { action: 'ignore', text: sentence, reason: t('甄别失败') }; }
  if (result.action === 'locked') {
    voiceLog('ignored', sentence, result.reason);
    if (voice.recognition) { try { voice.recognition.onend = null; voice.recognition.stop(); } catch (_) {} }
    $('voice-panel').classList.add('hidden');
    toast(result.reason || t('小秘已锁定，请在小秘会话中解锁'));
    return;
  }
  if (result.action === 'send') {
    if (voice.halted) return; // 已硬停止：不发送、不触发新 run
    const text = (result.text || sentence).trim();
    // #41：小秘自行判断 queue/insert。insert→queued=false（插话打断当前 run）；queue/缺省→queued=true（排队）
    const queued = (result.mode !== 'insert');
    voice.awaitingReply = true; // 标记本次由小秘语音发起，run 完成后据开关朗读回复
    try {
      if (state.config && state.config.voiceReplyEnabled) {
        voice.queuedOverride = queued; // 打字机路径经表单 onsubmit 消费
        await typeIntoPrompt(text);
        $('task-form').requestSubmit();
      } else {
        await voiceSend(text, queued);
      }
      voiceLog('sent', text, result.reason, result.mode);
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
  voiceRenderStatus();
  try {
    while (voice.queue.length && !voice.standby && !voice.halted) {
      const sentence = voice.queue.shift();
      voiceRenderStatus();
      await voiceFilterOne(sentence);
    }
  } finally { voice.sending = false; voiceRenderStatus(); }
}

/* ── 双向语音：小蜜朗读。优先后端 edge-tts 神经音（TTSPlayer），失败/超时自动降级浏览器 Web Speech ── */
function ttsCancel() {
  try { if ('speechSynthesis' in window) window.speechSynthesis.cancel(); } catch (_) {}
  ttsPlayer.halt();
}
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
/* TTSPlayer：逐句 fetch /api/tts/synthesize → Audio 队列顺序播放；可中断/暂停/继续 */
const ttsPlayer = {
  cancelled: false, paused: false,
  _ctrl: null, _audio: null, _wake: null,
  halt() {
    this.cancelled = true; this.paused = false;
    if (this._ctrl) { try { this._ctrl.abort(); } catch (_) {} this._ctrl = null; }
    if (this._audio) { try { this._audio.pause(); } catch (_) {} this._audio = null; }
    if (this._wake) { const w = this._wake; this._wake = null; w(); }
  },
  setPaused(p) {
    this.paused = p;
    if (p) { if (this._audio) try { this._audio.pause(); } catch (_) {} }
    else { if (this._audio) this._audio.play().catch(() => {}); if (this._wake) { const w = this._wake; this._wake = null; w(); } }
  },
};
function ttsWantsEdge(overrideProvider) {
  const p = overrideProvider != null ? overrideProvider : ((state.config && state.config.ttsProvider) || 'auto');
  return p !== 'webspeech'; // auto/edge 都先走 edge，失败自动降级
}
async function ttsEdgeSynthOne(seg, overrideVoice) {
  const ctrl = new AbortController();
  ttsPlayer._ctrl = ctrl;
  // 客户端 20s 硬超时：后端 sherpa 冷启动/edge 挂起时主动 abort，避免 fetch 永不返回卡死播放队列。
  const timer = setTimeout(() => { try { ctrl.abort(); } catch (_) {} }, 20000);
  const voice = overrideVoice != null ? overrideVoice : ((state.config && state.config.ttsVoice) || '');
  let res;
  try {
    res = await fetch('/api/tts/synthesize', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', 'Authorization': 'Bearer ' + state.token },
      body: JSON.stringify({ text: seg, voice: voice, gender: (state.config && state.config.voiceReplyGender) || 'female' }),
      signal: ctrl.signal,
    });
  } finally {
    clearTimeout(timer);
  }
  ttsPlayer._ctrl = null;
  if (!res.ok) throw new Error('edge-tts http ' + res.status);
  const blob = await res.blob();
  return URL.createObjectURL(blob);
}
function ttsPlayOne(url) {
  return new Promise((resolve) => {
    const a = new Audio(url);
    ttsPlayer._audio = a;
    let done = false;
    const finish = () => { if (done) return; done = true; ttsPlayer._audio = null; URL.revokeObjectURL(url); resolve(); };
    a.onended = finish;
    a.onerror = finish;
    const timer = setInterval(() => {
      if (done) { clearInterval(timer); return; }
      if (ttsPlayer.cancelled) { a.pause(); clearInterval(timer); finish(); return; }
      if (ttsPlayer.paused) { if (!a.paused) a.pause(); }
      else if (a.paused && !a.ended) a.play().catch(() => {});
    }, 150);
    a.play().catch(finish);
  });
}
// 朗读入口：awaitMode=true 返回 Promise（导览讲解），否则即发即忘（对话回复）。
// colloquial=true 时先经后端 LLM 口语化改写。任何 edge 失败都降级浏览器 Web Speech。
let _lastDegradeToastAt = 0;
function notifyDegraded() {
  // 降级到浏览器机械音时显著提示（5s 内只弹一次，避免每句刷屏）
  const now = Date.now();
  if (now - _lastDegradeToastAt < 5000) return;
  _lastDegradeToastAt = now;
  toast(t('神经音暂不可用，当前为浏览器机械音（联网恢复后自动切回）'));
}
async function ttsSpeak(text, { awaitMode = false, colloquial = false, voice = null, provider = null } = {}) {
  ttsPlayer.cancelled = false;
  ttsPlayer.paused = false;
  if (!text) return;
  if (!ttsWantsEdge(provider)) { return awaitMode ? webSpeakAwait(text) : webSpeakReply(text); }
  let spoken = text;
  if (colloquial) {
    try {
      const mode = (state.config && state.config.voiceReplyVerbosity) || 'brief';
      const r = await api('/tts/colloquialize', { method: 'POST', body: JSON.stringify({ text, mode }) });
      if (r && r.spoken) spoken = r.spoken;
    } catch (_) { spoken = text; }
  }
  const segs = ttsSegments(spoken);
  if (!segs.length) return;
  try {
    for (let i = 0; i < segs.length; i++) {
      if (ttsPlayer.cancelled) return;
      if (awaitMode && narration.paused) await new Promise(res => { ttsPlayer._wake = res; });
      if (ttsPlayer.cancelled) return;
      const url = await ttsEdgeSynthOne(segs[i], voice);
      if (ttsPlayer.cancelled) { URL.revokeObjectURL(url); return; }
      await ttsPlayOne(url);
    }
  } catch (_) {
    // edge 不可用/超时 → 降级浏览器 Web Speech（用口语化后的文本），并显著提示
    notifyDegraded();
    if (awaitMode) return webSpeakAwait(spoken);
    return webSpeakReply(spoken);
  }
}

/* ── 浏览器 Web Speech 兜底（macOS 常回落 Ting-Ting 机械音，仅作降级） ── */
function webSpeakReply(text) {
  if (!text || !('speechSynthesis' in window)) return;
  const segs = ttsSegments(text);
  if (!segs.length) return;
  const synth = window.speechSynthesis;
  synth.cancel();
  const voice = pickVoiceForGender(state.config && state.config.voiceReplyGender);
  const isMale = (state.config && state.config.voiceReplyGender) === 'male';
  const basePitch = isMale ? 0.99 : 1.1;
  const baseRate = 1.04;
  let i = 0;
  let stopped = false; // ttsCancel()/锁屏/停止按钮 → 终止整条朗读链
  function next() {
    if (stopped || i >= segs.length) return;
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
    u.onend = () => { if (stopped) return; const pause = ask ? 200 : clause ? 95 : 175; i++; setTimeout(next, pause); };
    u.onerror = (ev) => {
      // canceled/interrupted/aborted 是主动取消，必须停链，不再排队下一句
      const kind = ev && ev.error;
      if (kind === 'canceled' || kind === 'interrupted' || kind === 'aborted') { stopped = true; return; }
      i++; next();
    };
    synth.speak(u);
  }
  next();
}
// 对话回复入口（调用点零改动）：先口语化改写，再 edge 神经音，失败降级 Web Speech。
function speakReply(text) {
  if (!state.config || !state.config.voiceReplyEnabled) return;
  ttsSpeak(text, { awaitMode: false, colloquial: true });
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

// 小秘专属会话使用听写模式：识别结果先进入可编辑输入框，用户显式点击发送后才提交。
const xiaomiDictation = { token: 0, recognition: null, stream: null, active: false, starting: false, finalText: '', interim: '', baseText: '', lastRendered: '' };
function renderXiaomiDictationText() {
  const prompt = $('prompt');
  const recognized = xiaomiDictation.finalText + xiaomiDictation.interim;
  const separator = xiaomiDictation.baseText && recognized && !/[\s\n]$/.test(xiaomiDictation.baseText) ? '\n' : '';
  prompt.value = xiaomiDictation.baseText + separator + recognized;
  xiaomiDictation.lastRendered = prompt.value;
  const host = $('voice-text');
  host.replaceChildren();
  if (xiaomiDictation.finalText) host.append(el('div', 'voice-log-line', xiaomiDictation.finalText));
  if (xiaomiDictation.interim) host.append(el('div', 'voice-log-interim', '… ' + xiaomiDictation.interim));
  host.scrollTop = host.scrollHeight;
}
function releaseXiaomiDictationStream() {
  if (xiaomiDictation.stream) {
    try { xiaomiDictation.stream.getTracks().forEach(track => track.stop()); } catch (_) {}
    xiaomiDictation.stream = null;
  }
}
function finishXiaomiDictation(token, showResult = true) {
  if (token !== xiaomiDictation.token) return;
  xiaomiDictation.active = false;
  xiaomiDictation.starting = false;
  if (showResult && xiaomiDictation.interim.trim()) xiaomiDictation.finalText += xiaomiDictation.interim;
  xiaomiDictation.interim = '';
  if (xiaomiDictation.recognition) {
    try { xiaomiDictation.recognition.onend = null; xiaomiDictation.recognition.stop(); } catch (_) {}
    xiaomiDictation.recognition = null;
  }
  releaseXiaomiDictationStream();
  xiaomiDictation.token++;
  $('prompt').disabled = false;
  $('voice-btn').classList.remove('recording');
  $('voice-panel').classList.add('hidden');
  renderXiaomiDictationText();
  if (showResult && xiaomiDictation.finalText.trim()) toast(t('语音已转写，可编辑后发送'));
}
function stopXiaomiDictation() {
  if (!xiaomiDictation.active && !xiaomiDictation.starting) return;
  const token = xiaomiDictation.token;
  xiaomiDictation.active = false;
  xiaomiDictation.starting = false;
  $('voice-btn').classList.remove('recording');
  voiceSetStatus('standby', t('说完后可编辑并发送'));
  if (xiaomiDictation.recognition) {
    try { xiaomiDictation.recognition.stop(); } catch (_) {}
    // 某些 Web Speech 实现不触发 onend；最终转写应保留，麦克风也要及时释放。
    setTimeout(() => finishXiaomiDictation(token), 900);
  } else finishXiaomiDictation(token);
}
async function startXiaomiDictation() {
  if (xiaomiDictation.active || xiaomiDictation.starting) return;
  const Ctor = window.SpeechRecognition || window.webkitSpeechRecognition;
  if (!voice.supported || !Ctor) {
    const message = t('当前浏览器不支持语音识别，请用 Chrome/Edge，并通过 HTTPS 或 localhost 访问');
    toast(message); return;
  }
  const token = ++xiaomiDictation.token;
  xiaomiDictation.starting = true;
  xiaomiDictation.active = false;
  xiaomiDictation.finalText = '';
  xiaomiDictation.interim = '';
  xiaomiDictation.baseText = $('prompt').value;
  $('prompt').disabled = true;
  $('voice-title').textContent = voice.name();
  $('voice-panel').classList.remove('hidden');
  $('voice-btn').classList.remove('recording');
  voiceSetStatus('requesting', t('正在请求麦克风…'));
  renderXiaomiDictationText();
  try {
    const deviceId = state.config && state.config.voiceInputDevice;
    if (deviceId && navigator.mediaDevices?.getUserMedia) {
      try { xiaomiDictation.stream = await navigator.mediaDevices.getUserMedia({ audio: { deviceId: { exact: deviceId } } }); }
      catch (_) { toast(t('无法使用所选麦克风，将使用系统默认')); }
    }
    if (token !== xiaomiDictation.token || !xiaomiDictation.starting) {
      releaseXiaomiDictationStream();
      return;
    }
    const rec = new Ctor();
    rec.lang = 'zh-CN'; rec.continuous = true; rec.interimResults = true;
    xiaomiDictation.recognition = rec;
    rec.onstart = () => {
      if (token !== xiaomiDictation.token || xiaomiDictation.recognition !== rec) return;
      if (!xiaomiDictation.starting) { finishXiaomiDictation(token); return; }
      xiaomiDictation.starting = false; xiaomiDictation.active = true;
      $('voice-btn').classList.add('recording');
      voiceSetStatus('listening', t('说完后可编辑并发送'));
    };
    rec.onresult = event => {
      if (token !== xiaomiDictation.token) return;
      let interim = '';
      for (let i = event.resultIndex; i < event.results.length; i++) {
        const result = event.results[i];
        if (result.isFinal) xiaomiDictation.finalText += result[0].transcript;
        else interim += result[0].transcript;
      }
      xiaomiDictation.interim = interim;
      renderXiaomiDictationText();
    };
    rec.onerror = event => {
      if (token !== xiaomiDictation.token) return;
      const denied = event.error === 'not-allowed' || event.error === 'service-not-allowed';
      const message = denied ? t('麦克风权限被拒绝，请在浏览器地址栏允许麦克风访问后重试') : t('语音识别暂不可用，请检查麦克风后重试');
      finishXiaomiDictation(token, false);
      toast(message);
    };
    rec.onend = () => {
      if (token !== xiaomiDictation.token) return;
      if (xiaomiDictation.active) { try { rec.start(); } catch (_) {} }
      else finishXiaomiDictation(token);
    };
    ttsCancel();
    const audioTrack = xiaomiDictation.stream?.getAudioTracks?.()[0];
    if (audioTrack) {
      try { rec.start(audioTrack); }
      catch (_) { releaseXiaomiDictationStream(); toast(t('当前浏览器不支持所选麦克风，将使用系统默认')); rec.start(); }
    } else rec.start();
  } catch (_) {
    finishXiaomiDictation(token, false);
    toast(t('无法启动语音识别，请检查麦克风后重试'));
  }
}

async function voiceStart() {
  if (voice.starting || voice.listening) return;
  const Ctor = window.SpeechRecognition || window.webkitSpeechRecognition;
  voice.starting = true;
  const startToken = ++voice.startToken;
  voice.listening = false;
  voice.standby = false;
  voice.halted = false;
  voice.buffer = ''; voice.interim = ''; voice.queue = []; voice.log = [];
  $('voice-title').textContent = voice.name();
  $('voice-panel').classList.remove('hidden');
  voiceRenderLog();
  voiceSetStatus('requesting', t('正在请求麦克风…'));
  $('voice-btn').classList.remove('recording');
  if (!voice.supported || !Ctor) {
    voice.starting = false;
    const message = t('当前浏览器不支持语音识别，请用 Chrome/Edge，并通过 HTTPS 或 localhost 访问');
    voiceSetStatus('standby', message);
    toast(message);
    return;
  }
  try {
    const micReady = await voiceOpenMicStream(startToken);
    if (!micReady || !voice.starting || startToken !== voice.startToken) return;
  } catch (_) {
    voice.starting = false;
    voiceSetStatus('standby', t('无法访问麦克风，请检查权限后重试'));
    toast(t('无法访问麦克风，请检查权限后重试'));
    return;
  }
  const rec = new Ctor();
  rec.lang = 'zh-CN'; rec.continuous = true; rec.interimResults = true;
  voice.recognition = rec;
  ttsCancel();
  rec.onstart = () => {
    if (startToken !== voice.startToken || voice.recognition !== rec) return;
    voice.starting = false;
    voice.listening = true;
    $('voice-btn').classList.add('recording');
    voiceSetStatus('listening', t('聆听中…说完一句会自动发送'));
  };
  rec.onresult = (e) => {
    if (startToken !== voice.startToken) return;
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
    if (startToken !== voice.startToken) return;
    voice.starting = false;
    voice.listening = false;
    voiceReleaseMicStream();
    $('voice-btn').classList.remove('recording');
    const denied = e.error === 'not-allowed' || e.error === 'service-not-allowed';
    const message = denied ? t('麦克风权限被拒绝，请在浏览器地址栏允许麦克风访问后重试') : t('语音识别暂不可用，请检查麦克风后重试');
    voiceSetStatus('standby', message);
    toast(message);
  };
  rec.onend = () => {
    if (startToken !== voice.startToken) return;
    // 不自动重启监听：说完一句后停止，需要用户手动点麦克风再次开始
    voice.listening = false;
    voiceReleaseMicStream();
    $('voice-btn').classList.remove('recording');
    voiceSetStatus('standby', t('聆听结束，点击麦克风再次开始'));
  };
  try {
    const audioTrack = voice.micStream?.getAudioTracks?.()[0];
    if (audioTrack) {
      try { rec.start(audioTrack); }
      catch (_) {
        voiceReleaseMicStream();
        toast(t('当前浏览器不支持所选麦克风，将使用系统默认'));
        rec.start();
      }
    } else rec.start();
  }
  catch (_) {
    voice.starting = false;
    voice.listening = false;
    voiceReleaseMicStream();
    $('voice-btn').classList.remove('recording');
    const message = t('无法启动语音识别，请检查麦克风后重试');
    voiceSetStatus('standby', message);
    toast(message);
  }
}
function voiceStopAndFlush() {
  clearTimeout(voice.timer);
  voice.starting = false;
  voice.startToken++;
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
// 硬停止：取消一切——停朗读、清空队列/缓冲、不再发送、不触发新 run。绑定面板"停止"按钮。
function voiceHardStop() {
  clearTimeout(voice.timer);
  voice.starting = false;
  voice.startToken++;
  voiceReleaseMicStream();
  ttsCancel(); // 立即停神经音 + 浏览器机械音朗读
  voice.halted = true;
  voice.queue = []; voice.buffer = ''; voice.interim = ''; voice.awaitingReply = false;
  if (voice.recognition) { try { voice.recognition.onend = null; voice.recognition.abort(); } catch (_) {} }
  voice.listening = false; voice.standby = false;
  $('voice-btn').classList.remove('recording');
  $('voice-panel').classList.add('hidden');
}
function voiceClose() {
  clearTimeout(voice.timer);
  voice.starting = false;
  voice.startToken++;
  voiceReleaseMicStream();
  ttsCancel();
  voice.listening = false; voice.standby = false;
  if (voice.recognition) { try { voice.recognition.onend = null; voice.recognition.abort(); } catch (_) {} }
  $('voice-btn').classList.remove('recording');
  $('voice-panel').classList.add('hidden');
}

// 选定麦克风：获取 exact deviceId 的音频流；支持 start(audioTrack) 的浏览器会将所选轨道传给识别器。
async function voiceOpenMicStream(startToken) {
  voiceReleaseMicStream();
  const dev = state.config && state.config.voiceInputDevice;
  if (!dev || !navigator.mediaDevices || !navigator.mediaDevices.getUserMedia) return true;
  try {
    const stream = await navigator.mediaDevices.getUserMedia({ audio: { deviceId: { exact: dev } } });
    if (startToken !== voice.startToken || !voice.starting) {
      try { stream.getTracks().forEach(tk => tk.stop()); } catch (_) {}
      return false;
    }
    voice.micStream = stream;
  } catch (_) {
    voice.micStream = null;
    toast(t('无法使用所选麦克风，将使用系统默认'));
  }
  return true;
}
function voiceReleaseMicStream() {
  if (voice.micStream) { try { voice.micStream.getTracks().forEach(tk => tk.stop()); } catch (_) {} voice.micStream = null; }
}
$('voice-btn').onclick = action(() => {
  if (state.session?.kind === 'assistant') {
    (xiaomiDictation.active || xiaomiDictation.starting) ? stopXiaomiDictation() : startXiaomiDictation();
    return;
  }
  // 主会话和小秘会话统一进入小秘语音：实时断句、AI 甄别并直接发送。
  (voice.listening || voice.starting) ? voiceStopAndFlush() : voiceStart();
});
$('voice-stop').onclick = action(() => state.session?.kind === 'assistant' ? stopXiaomiDictation() : voiceHardStop());
// ===== 小秘语音导览：朗读 AI 输出并自动滚动跟随；讲方案时先打开产物文件再讲解 =====
const narration = { active:false, steps:[], index:0, paused:false, cancelled:false, jump:0 };
function narrSleep(ms){ return new Promise(r=>setTimeout(r,ms)); }
function stripMarkdownForSpeech(src){
  let s = String(src||'');
  s = s.replace(/```mermaid[\s\S]*?```/gi, t('，流程图如下，'));
  s = s.replace(/```[a-zA-Z]*[\s\S]*?```/g, t('，相关代码见屏幕，'));
  s = s.replace(/!\[[^\]]*\]\([^)]*\)/g, '');
  s = s.replace(/\[([^\]]+)\]\([^)]*\)/g, '$1');
  s = s.replace(/^\s{0,3}#{1,6}\s*/gm, '');
  s = s.replace(/^\s*> ?/gm, '');
  s = s.replace(/[*_~|`]/g, '');
  s = s.replace(/^\s*[-+]\s+/gm, '');
  s = s.replace(/<\/?[a-zA-Z][^>]*>/g, '');
  s = s.replace(/[ \t]+\n/g,'\n').replace(/\n{2,}/g,'。').replace(/\n/g,'，');
  return s.replace(/，{2,}/g,'，').replace(/。{2,}/g,'。').replace(/\s+/g,' ');
}
function toolPathOf(tu){
  try { const a = JSON.parse(tu.args||'{}'); return { path:a.path, source:a.source, root:a.root }; }
  catch(_){ return { path:'' }; }
}
function buildNarrationSteps(scopeRunId){
  const s = state.session; const steps=[];
  if(!s) return steps;
  for(const run of (s.runs||[])){
    if(scopeRunId && run.id !== scopeRunId) continue;
    let md='';
    for(const st of (run.steps||[])) if(st.content) md += st.content+'\n';
    const text = stripMarkdownForSpeech(md).trim();
    const opened = new Set();
    for(const tu of (run.toolUses||[])){
      if(/create_diagram|write_file/.test(tu.tool||'')){
        const p = toolPathOf(tu);
        if(p.path && !opened.has(p.path)){ opened.add(p.path); steps.push({kind:'openFile', path:p.path, source:p.source||'', root:p.root||'workspace'}); }
      }
    }
    if(text) steps.push({kind:'speak', runId:run.id, text:text.slice(0,4000)});
  }
  return steps;
}
function narrationGate(){
  return new Promise(res=>{
    (function chk(){
      if(narration.cancelled) return res();
      if(!narration.paused) return res();
      setTimeout(chk,180);
    })();
  });
}
// 浏览器 Web Speech 导览讲解兜底（await 版）
function webSpeakAwait(text){
  return new Promise(resolve=>{
    if(!('speechSynthesis' in window)){ narrSleep(500).then(res); return; }
    const segs = ttsSegments(text);
    if(!segs.length) return res();
    const synth = window.speechSynthesis; synth.cancel();
    const vc = pickVoiceForGender(state.config && state.config.voiceReplyGender);
    const isMale = (state.config && state.config.voiceReplyGender)==='male';
    const basePitch = isMale?0.99:1.1, baseRate=1.04;
    let i=0;
    function step(){
      if(narration.cancelled){ try{synth.cancel();}catch(_){} return res(); }
      if(i>=segs.length) return res();
      const seg=segs[i];
      const u=new SpeechSynthesisUtterance(seg);
      u.lang='zh-CN'; if(vc)u.voice=vc;
      const ask=/[?？]\s*$/.test(seg), ex=/[!！]\s*$/.test(seg), clause=/[，,、；;：:]\s*$/.test(seg);
      u.pitch=ask?basePitch+0.14:ex?basePitch+0.06:basePitch;
      u.rate=ex?baseRate+0.07:ask?baseRate-0.04:baseRate;
      u.onend=()=>{ const pause=ask?200:clause?95:175; i++; setTimeout(next,pause); };
      u.onerror=()=>{ i++; next(); };
      synth.speak(u);
    }
    function next(){
      if(narration.cancelled){ try{synth.cancel();}catch(_){} return res(); }
      if(narration.paused){
        try{ synth.pause(); }catch(_){}
        (function w(){
          if(narration.cancelled){ try{synth.cancel();}catch(_){} return res(); }
          if(narration.paused) return setTimeout(w,180);
          try{ synth.resume(); }catch(_){}
          step();
        })();
        return;
      }
      step();
    }
    next();
  });
}
// 导览讲解入口（调用点零改动）：edge 神经音，文本已由 voice-narrate 口语化，不再改写；失败降级 Web Speech。
function speakAwait(text){
  return ttsSpeak(text, { awaitMode: true, colloquial: false });
}
async function focusRunInChat(runId){
  if($('editor-dialog').open){ try{ $('editor-dialog').close(); }catch(_){} await narrSleep(260); }
  document.querySelectorAll('.narration-highlight').forEach(x=>x.classList.remove('narration-highlight'));
  const art = document.querySelector('article.run[data-run="'+CSS.escape(runId)+'"]');
  if(art){
    art.classList.add('narration-highlight');
    art.scrollIntoView({behavior:'smooth', block:'center'});
    await narrSleep(480);
  } else {
    $('conversation').scrollTo({top:$('conversation').scrollHeight, behavior:'smooth'});
  }
}
async function doOpenForNarration(st){
  state.root = st.root || 'workspace';
  state.source = st.source || '';
  try{ await openFile(st.path); }
  catch(e){ toast(t('打开文件失败：')+st.path); }
  await narrSleep(650);
}
function updateNarrationBar(){
  $('narr-progress').textContent = (narration.index+1)+' / '+narration.steps.length;
  $('narr-play').textContent = narration.paused ? '▶' : '❚❚';
}
function showNarrationBar(){ $('narration-bar').classList.remove('hidden'); }
function hideNarrationBar(){ $('narration-bar').classList.add('hidden'); }
async function startNarration(scopeRunId){
  if(narration.active){ narration.cancelled=true; ttsCancel(); await narrSleep(120); }
  let steps = buildNarrationSteps(scopeRunId);
  if(!steps.length){ toast(t('当前会话暂无可讲解的内容')); return false; }
  let speaks = null;
  try {
    const r = await api('/voice-narrate', { method:'POST', body: JSON.stringify({ steps: steps.map(st=>({ kind:st.kind, path:st.path||'', text:st.text||'' })) }) });
    speaks = r.speaks;
  } catch(_) { speaks = null; }
  steps = steps.map((st,i)=>({ ...st, speak: (speaks && speaks[i]) ? speaks[i] : (st.text || (t('我们先来看：')+(st.path||''))) }));
  narration.active=true;
  Object.assign(narration,{steps,index:0,paused:false,cancelled:false,jump:0,userStopped:false});
  $('voice-panel').classList.remove('hidden');
  showNarrationBar(); updateNarrationBar();
  runLoop(0);
  return true;
}
async function runLoop(from){
  let i=from;
  while(i<narration.steps.length){
    narration.index=i; updateNarrationBar();
    await narrationGate();
    if(narration.cancelled) break;
    const st=narration.steps[i];
    if(st.kind==='openFile'){ await doOpenForNarration(st); await narrationGate(); if(narration.cancelled)break; if(st.speak) await speakAwait(st.speak); await narrSleep(200); }
    else { await focusRunInChat(st.runId); await narrationGate(); if(narration.cancelled)break; await speakAwait(st.speak); }
    if(narration.cancelled) break;
    if(narration.jump){ i=narration.jump; narration.jump=0; continue; }
    i++;
  }
  endNarration(!narration.userStopped); // 自然播完保持讲解模式；用户停止则取消
}
function endNarration(keepMode){
  narration.active=false; narration.paused=false; narration.jump=0;
  ttsCancel();
  document.querySelectorAll('.narration-highlight').forEach(x=>x.classList.remove('narration-highlight'));
  if($('editor-dialog').open){ try{ $('editor-dialog').close(); }catch(_){} }
  hideNarrationBar();
  if(!keepMode) setNarrateModeUI(false);
  if(!voice.listening && !narrateModeOn()) $('voice-panel').classList.add('hidden');
}
function narrationJump(d){
  if(!narration.active) return;
  const t=Math.max(0,Math.min(narration.steps.length-1, narration.index+d));
  narration.paused=false; narration.jump=t;
  try{ window.speechSynthesis.resume(); }catch(_){}
  ttsCancel();
}
$('voice-narrate').onclick = action(toggleNarrateMode);
$('narr-play').onclick = action(()=>{
  if(!narration.active) return;
  narration.paused = !narration.paused;
  try{ narration.paused ? window.speechSynthesis.pause() : window.speechSynthesis.resume(); }catch(_){}
  ttsPlayer.setPaused(narration.paused);
  updateNarrationBar();
});
$('narr-prev').onclick = action(()=>narrationJump(-1));
$('narr-next').onclick = action(()=>narrationJump(1));
$('narr-stop').onclick = action(()=>{
  narration.cancelled=true; narration.userStopped=true; narration.paused=false; narration.jump=0;
  try{ window.speechSynthesis.resume(); }catch(_){}
  ttsCancel();
});

// ===== 讲解模式（勾选）：勾选后小秘主动滚动/开文件并口语讲解，输出完成自动朗读 =====
function narrateModeOn(){ return $('voice-narrate').classList.contains('active'); }
function setNarrateModeUI(on){
  const b=$('voice-narrate');
  b.classList.toggle('active',on);
  b.setAttribute('aria-pressed',on?'true':'false');
  // 手动“讲解”是当前页面内的临时模式，不写入跨会话持久设置。
}
async function toggleNarrateMode(){
  if(narrateModeOn()){ narration.cancelled=true; narration.userStopped=true; ttsCancel(); }
  else { setNarrateModeUI(true); const ok=await startNarration(); if(!ok) setNarrateModeUI(false); }
}
// 输出完成后自动讲解（无障碍自动朗读 或 讲解模式开启）；取消的会话不讲
function maybeAutoNarrate(runId){
  const want = state.config?.accessibilityAutoRead || narrateModeOn();
  if(!want || !runId) return;
  setTimeout(action(async()=>{
    if(state.session?.runs?.find(r=>r.id===runId)?.status==='cancelled') return;
    startNarration(runId);
  }),650);
}
try{ localStorage.removeItem('aide.narrateMode'); }catch(_){} // 清除旧版遗留的跨会话讲解状态。

// ===== 主聊天消息「朗读」：显式点击触发，平直机械音（区别于小秘的灵动韵律 TTS）=====
const mech = { speaking:false, btn:null };
function mechanicalParts(text){
  const flat = String(text||'').replace(/```[\s\S]*?```/g,t('，代码，')).replace(/[#*`>_~|]/g,'');
  const out=[]; const re=/[^。！？.!?\n]+[。！？.!?\n]?/g; let m;
  while((m=re.exec(flat))){ const x=m[0].trim(); if(x) out.push(x.slice(0,200)); }
  return out.slice(0,60);
}
function resetMechButtons(){ document.querySelectorAll('.msg-btn-mech').forEach(b=>b.textContent=t('朗读')); }
function toggleMechanicalRead(btn, text){
  if(mech.speaking && mech.btn===btn){ ttsCancel(); mech.speaking=false; btn.textContent=t('朗读'); return; }
  if(!('speechSynthesis' in window)){ toast(t('当前浏览器不支持语音合成')); return; }
  resetMechButtons();
  ttsPlayer.halt(); // 只停神经音播放器，避免与机械朗读互相打断
  if ('speechSynthesis' in window) window.speechSynthesis.cancel();
  const parts = mechanicalParts(text);
  if(!parts.length){ toast(t('没有可朗读的文本')); return; }
  mech.btn=btn; mech.speaking=true; btn.textContent=t('停止');
  let i=0; let stopped=false;
  function next(){
    if(stopped) return;
    if(i>=parts.length){ mech.speaking=false; btn.textContent=t('朗读'); mech.btn=null; return; }
    const u=new SpeechSynthesisUtterance(parts[i]);
    u.lang='zh-CN'; u.rate=1; u.pitch=1; u.volume=1; // 固定参数、无停顿无语气 → 机械
    u.onend=()=>{ if(stopped) return; i++; next(); };
    u.onerror=(ev)=>{ const kind=ev&&ev.error; if(kind==='canceled'||kind==='interrupted'||kind==='aborted'){ stopped=true; return; } i++; next(); };
    window.speechSynthesis.speak(u);
  }
  // speechSynthesis.cancel() 是异步的，立即 speak 首句会被吞掉，延迟 120ms 再开读
  setTimeout(next, 120);
}

// 设置面板：语音小秘名字输入
function renderVoiceNameControl() {
  const wrap = el('div', 'settings-control');
  const head = el('div', 'control-label');
  head.append(el('span', '', t('小秘名字')));
  const input = el('input');
  input.type = 'text';
  input.maxLength = 12;
  input.placeholder = t('小秘');
  input.value = (state.config && state.config.voiceAssistantName) || t('小秘');
  input.onkeydown = (e) => { if (e.key === 'Enter') { e.preventDefault(); saveBtn.click(); } };
  const saveBtn = el('button', 'primary', t('保存'));
  saveBtn.type = 'button';
  saveBtn.onclick = action(async () => {
    const name = input.value.trim() || t('小秘');
    await api('/settings', { method: 'PUT', body: JSON.stringify({ voiceAssistantName: name, activeModel: state.config ? state.config.activeModel : '' }) });
    await refreshConfig();
    toast(t('小秘名字已保存'));
  });
  const row = el('div', 'voice-name-row');
  row.append(input, saveBtn);
  wrap.append(head, row, el('small', '', t('语音弹框标题使用这个名字，默认「小秘」。')));
  return wrap;
}
function renderXiaomiModelControl() {
  const wrap = el('div', 'settings-control');
  const head = el('div', 'control-label'); head.append(el('span', '', t('小秘模型来源')));
  const source = el('select', 'input');
  [['inherit', t('复用工作台模型设置')], ['custom', t('使用独立模型来源')]].forEach(([v, label]) => { const o = el('option', '', label); o.value = v; source.append(o); });
  const base = el('input', 'input'); base.placeholder = 'https://api.example.com/v1'; base.autocomplete = 'url';
  const model = el('input', 'input'); model.placeholder = t('模型名称，例如 gpt-4o-mini'); model.autocomplete = 'off';
  const key = el('input', 'input'); key.type = 'password'; key.placeholder = t('API Key（留空表示不更改）'); key.autocomplete = 'new-password';
  const clearKey = el('input'); clearKey.type = 'checkbox';
  const clearKeyLabel = el('label', 'xiaomi-clear-key'); clearKeyLabel.append(clearKey, el('span', '', t('清除已保存的独立 API Key')));
  const status = el('small', 'muted', '');
  const fields = el('div', 'xiaomi-model-fields'); fields.append(base, model, key, clearKeyLabel);
  const save = el('button', 'primary', t('保存小秘模型设置')); save.type = 'button';
  const toggleFields = () => fields.classList.toggle('hidden', source.value !== 'custom');
  source.onchange = toggleFields;
  wrap.append(head, source, fields, status, save, el('small', 'muted', t('独立 API Key 使用加密保险库存储；复用模式跟随工作台当前模型和密钥。')));
  api('/xiaomi/model').then(cfg => { source.value = cfg.source || 'inherit'; base.value = cfg.baseURL || ''; model.value = cfg.model || ''; status.textContent = cfg.hasKey ? t('独立 API Key 已配置') : t('独立 API Key 尚未配置'); toggleFields(); }).catch(e => { status.textContent = e.message; });
  save.onclick = action(async () => {
    const cfg = await api('/xiaomi/model', { method: 'PUT', body: JSON.stringify({ source: source.value, baseURL: base.value.trim(), model: model.value.trim(), apiKey: key.value.trim(), clearKey: clearKey.checked }) });
    key.value = ''; clearKey.checked = false; status.textContent = cfg.hasKey ? t('独立 API Key 已配置') : t('独立 API Key 尚未配置'); toast(t('小秘模型设置已保存'));
  });
  return wrap;
}
controlRenderers['xiaomi-model'] = renderXiaomiModelControl;
// voice-history 控制项已移除：小秘对话入口从设置面板删除
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
  const verb = el('select');
  const vb = el('option', '', t('简要概括（默认）')); vb.value = 'brief';
  const vf = el('option', '', t('完整朗读')); vf.value = 'full';
  verb.append(vb, vf);
  verb.value = (state.config && state.config.voiceReplyVerbosity) || 'brief';
  const save = el('button', 'primary', t('保存'));
  save.type = 'button';
  save.onclick = action(async () => {
    await api('/settings', { method: 'PUT', body: JSON.stringify({ voiceReplyEnabled: toggle.checked, voiceReplyGender: gender.value, voiceReplyVerbosity: verb.value, activeModel: state.config ? state.config.activeModel : '' }) });
    await refreshConfig();
    if (toggle.checked && !('speechSynthesis' in window)) toast(t('当前浏览器不支持语音朗读'));
    else toast(t('语音回复设置已保存'));
  });
  row.append(toggle, el('span', '', t('语音回复')), gender, verb, save);
  wrap.append(head, row, el('small', '', t('开启后：口述总结的意图以打字机效果填入输入框并自动发送；模型回复会被朗读（音色可选男女）。简要概括=抓结论数字，完整朗读=不删减。')));
  return wrap;
}
controlRenderers['voice-reply'] = renderVoiceReplyControl;

// 设置 → 小秘发送调度（#41）：默认排队/插队 + 插队敏感度（冷却窗口）
function renderVoiceDispatchControl() {
  const wrap = el('div', 'settings-control');
  const head = el('div', 'control-label');
  head.append(el('span', '', t('发送调度')));
  const cfg = state.config || {};
  const row = el('div', 'voice-reply-row');

  row.append(el('span', '', t('默认发送模式')));
  const mode = el('select');
  [['queue', t('排队（默认）')], ['insert', t('插队')]].forEach(([v, label]) => {
    const o = el('option', '', label); o.value = v; mode.append(o);
  });
  mode.value = cfg.voiceDefaultSendMode || 'queue';
  row.append(mode);

  row.append(el('span', '', t('插队敏感度')));
  const sens = el('select');
  [['conservative', t('保守（默认）')], ['normal', t('适中')], ['aggressive', t('激进')]].forEach(([v, label]) => {
    const o = el('option', '', label); o.value = v; sens.append(o);
  });
  sens.value = cfg.voiceInsertSensitivity || 'conservative';
  row.append(sens);

  const save = el('button', 'primary', t('保存')); save.type = 'button';
  save.onclick = action(async () => {
    await api('/settings', { method: 'PUT', body: JSON.stringify({
      voiceDefaultSendMode: mode.value, voiceInsertSensitivity: sens.value,
      activeModel: state.config ? state.config.activeModel : ''
    }) });
    await refreshConfig();
    toast(t('发送调度已保存'));
  });
  row.append(save);
  wrap.append(head, row, el('small', '', t('小秘把识别意图发给 aide 时自行判断排队还是插队：紧急/中止类立即插队，新任务/不紧急排队。保守=5秒内不重复插队，中止指令不受限。')));
  return wrap;
}
controlRenderers['voice-dispatch'] = renderVoiceDispatchControl;

// 设置 → 语音引擎：edge-tts 神经音 / 浏览器 Web Speech 兜底
function renderTTSEngineControl() {
  const wrap = el('div', 'settings-control');
  const head = el('div', 'control-label');
  head.append(el('span', '', t('语音引擎')));
  const cfg = state.config || {};

  // 引擎下拉
  const engRow = el('div', 'voice-reply-row');
  engRow.append(el('span', '', t('TTS引擎')));
  const eng = el('select');
  [['auto', t('自动（本地离线优先）')], ['sherpa', t('本地离线')], ['edge', t('edge-tts 联网')], ['clone', t('克隆音色（自托管）')], ['webspeech', t('浏览器合成')]].forEach(([v, label]) => {
    const o = el('option', '', label); o.value = v; eng.append(o);
  });
  eng.value = cfg.ttsProvider || 'auto';
  engRow.append(eng);

  // 音色下拉（edge 音色列表）
  const voiceRow = el('div', 'voice-reply-row');
  voiceRow.append(el('span', '', t('音色')));
  const voice = el('select');
  const voices = cfg.ttsVoices || [];
  const autoOpt = el('option', '', t('默认（按性别）')); autoOpt.value = ''; voice.append(autoOpt);
  // 按性别分组（女声/男声），未保存的选择只改下拉，不写 state.config
  [['female', t('女声')], ['male', t('男声')]].forEach(([g, label]) => {
    const og = el('optgroup'); og.label = label;
    voices.filter(v => v.gender === g).forEach(v => {
      const o = el('option', '', v.name); o.value = v.id; og.append(o);
    });
    if (og.children.length) voice.append(og);
  });
  voice.value = cfg.ttsVoice || ''; // 回显已保存的音色；空值选中"默认（按性别）"
  voiceRow.append(voice);

  // 语速滑块
  const rateRow = el('div', 'voice-reply-row');
  rateRow.append(el('span', '', t('语速')));
  const rate = el('input'); rate.type = 'range'; rate.min = '0.8'; rate.max = '1.3'; rate.step = '0.05';
  rate.value = cfg.ttsRate || 1.0;
  const rateVal = el('span', '', Number(rate.value).toFixed(2) + 'x');
  rate.oninput = () => { rateVal.textContent = Number(rate.value).toFixed(2) + 'x'; };
  rateRow.append(rate, rateVal);

  // 表现力滑块
  const expRow = el('div', 'voice-reply-row');
  expRow.append(el('span', '', t('表现力')));
  const exp = el('input'); exp.type = 'range'; exp.min = '0'; exp.max = '1'; exp.step = '0.1';
  exp.value = cfg.ttsExpressiveness || 0.5;
  const expVal = el('span', '', Number(exp.value).toFixed(1));
  exp.oninput = () => { expVal.textContent = Number(exp.value).toFixed(1); };
  expRow.append(exp, expVal);

  // 保存 + 试听
  const btnRow = el('div', 'voice-reply-row');
  const save = el('button', 'primary', t('保存')); save.type = 'button';
  const preview = el('button', 'quiet', t('试听')); preview.type = 'button';
  save.onclick = action(async () => {
    await api('/settings', { method: 'PUT', body: JSON.stringify({
      ttsProvider: eng.value, ttsVoice: voice.value,
      ttsRate: parseFloat(rate.value), ttsExpressiveness: parseFloat(exp.value),
      activeModel: state.config ? state.config.activeModel : '',
    }) });
    await refreshConfig();
    toast(t('语音引擎设置已保存'));
  });
  preview.onclick = action(async () => {
    ttsCancel();
    const savedLabel = preview.textContent;
    preview.disabled = true; preview.textContent = t('播放中…');
    try {
      // 试听严格用下拉当前所选（voice/provider 覆盖），不污染 state.config。
      // 8s 硬超时：即使后端/播放链路卡死也强制恢复按钮，禁止停在"播放中…"。
      await Promise.race([
        ttsSpeak('你好，我是小秘，这是试听效果。', {
          awaitMode: true, colloquial: false,
          voice: voice.value, provider: eng.value,
        }),
        new Promise(res => setTimeout(res, 8000)),
      ]);
    } finally {
      ttsCancel();
      preview.disabled = false; preview.textContent = savedLabel;
    }
  });
  btnRow.append(save, preview);

  // edge 不可用时的持久提示（随 config 刷新；edge 恢复后自动消失）
  const warn = el('small', '', '');
  const prov = cfg.ttsProvider || 'auto';
  // 显式选「本地离线」但未安装模型 → 明确引导（不静默降级 edge）
  if (prov === 'sherpa' && !cfg.sherpaAvailable) {
    warn.textContent = '⚠ ' + t('本地离线模型未安装') + '。' + t('请在容器内运行 scripts/tts-setup 下载模型到 /data/tts/，或手动放入后重启；当前回退联网/浏览器。');
    warn.style.color = '#c0392b';
  } else if (prov !== 'webspeech' && cfg.edgeAvailable === false && !cfg.sherpaAvailable) {
    warn.textContent = '⚠ ' + t('神经音当前不可用') + (cfg.edgeLastError ? '（' + cfg.edgeLastError + '）' : '') + '，' + t('已临时使用浏览器机械音；联网恢复后自动切回神经音。');
    warn.style.color = '#c0392b';
  }
  // 当前实际生效引擎（后端 /api/config 回显）——不再静默降级，明确告知用户
  const engStatus = el('small', '');
  const cur = cfg.currentTTSEngine;
  if (prov === 'webspeech') {
    engStatus.textContent = t('当前引擎') + ': ' + t('浏览器合成（机械音）');
    engStatus.style.color = '#888';
  } else if (cur === 'sherpa') {
    const nv = (cfg.sherpaVoices && cfg.sherpaVoices.length) ? cfg.sherpaVoices.map(v=>v.name||v.dir).join('、') : '';
    engStatus.textContent = t('当前引擎') + ': 本地离线 sherpa-onnx ✓' + (nv ? '（' + nv + '）' : '');
    engStatus.style.color = '#27ae60';
  } else if (cur === 'edge' || cur === 'azure') {
    engStatus.textContent = t('当前引擎') + ': ' + (cur === 'edge' ? 'edge-tts 神经音' : 'Azure 神经音') + ' ✓' + (cfg.sherpaAvailable ? '（本地已装，下次优先本地）' : '');
    engStatus.style.color = '#27ae60';
  } else if (cur === 'unavailable') {
    engStatus.textContent = t('当前引擎') + ': ⚠ ' + t('神经音不可用，已降级浏览器机械音');
    engStatus.style.color = '#c0392b';
  } else {
    engStatus.textContent = t('当前引擎') + ': ' + (cfg.sherpaAvailable ? t('本地离线已就绪，待首次合成确认') : t('待检测（合成一次后显示）'));
    engStatus.style.color = '#888';
  }
  wrap.append(head, engRow, voiceRow, engStatus, rateRow, expRow, btnRow, warn,
    el('small', '', t('默认优先本地离线 sherpa-onnx（完全离线、文本不出本机）；未装模型时自动回退 edge-tts 联网神经音，再不行降级浏览器合成。保密环境可装本地模型或选「浏览器合成」。主聊天的机械朗读按钮不受此设置影响。')));
  return wrap;
}
controlRenderers['tts-engine'] = renderTTSEngineControl;

// 设置 → 个性化音色（#36）：克隆服务配置 + 录音样本 + 创建音色 + 性格推断
function renderVoiceCloneControl() {
  const wrap = el('div', 'settings-control');
  const head = el('div', 'control-label'); head.append(el('span', '', t('个性化音色')));
  wrap.append(head);
  const cfg = state.config || {};

  // ── 克隆服务配置 ──
  const baseRow = el('div', 'voice-reply-row');
  baseRow.append(el('span', '', t('服务地址')));
  const baseURL = el('input'); baseURL.type = 'text'; baseURL.placeholder = 'http://127.0.0.1:9880';
  baseURL.value = cfg.cloneBaseURL || ''; baseURL.style.flex = '1';
  baseRow.append(baseURL);

  const beRow = el('div', 'voice-reply-row');
  beRow.append(el('span', '', t('后端')));
  const be = el('select');
  [['openai','OpenAI 兼容'],['gpt-sovits','GPT-SoVITS'],['indextts2','IndexTTS2'],['cosyvoice2','CosyVoice2'],['openvoice','OpenVoice v2']]
    .forEach(([v,l]) => { const o=el('option','',l); o.value=v; be.append(o); });
  be.value = cfg.cloneBackend || 'openai';
  beRow.append(be);

  const keyRow = el('div', 'voice-reply-row');
  keyRow.append(el('span', '', t('API Key')));
  const apiKey = el('input'); apiKey.type = 'password'; apiKey.placeholder = cfg.hasCloneKey ? t('已设置（留空不改动）') : t('可选');
  apiKey.style.flex = '1';
  keyRow.append(apiKey);

  const saveCfg = el('button','primary',t('保存克隆配置')); saveCfg.type='button';
  saveCfg.onclick = action(async () => {
    await api('/settings', { method:'PUT', body: JSON.stringify({
      cloneTTSBaseURL: baseURL.value.trim(),
      cloneTTSBackend: be.value,
      cloneTTSAPIKey: apiKey.value,
      activeModel: state.config ? state.config.activeModel : '',
    })});
    await refreshConfig();
    toast(t('克隆配置已保存'));
  });
  wrap.append(baseRow, beRow, keyRow, saveCfg);

  if (cfg.cloneConfigured) {
    wrap.append(el('small','', t('已绑定音色')+': '+(cfg.cloneVoiceID||t('未创建'))));
  } else {
    wrap.append(el('small','', t('未配置克隆服务：样本仍可加密录制，但需配置服务后才能创建音色。')));
  }

  // ── 合规告知与同意 ──
  const consentRow = el('div','voice-reply-row');
  const consent = el('input'); consent.type='checkbox';
  consentRow.append(consent, el('span','', t('本人或已获被录制者同意录制（GDPR Art.9 生物特征）')));
  wrap.append(consentRow);
  wrap.append(el('small','', t('样本在本机用 AES-256-GCM 加密存储，不上传第三方云；删除样本即无残留。真实效果需自托管克隆模型。')));

  // ── 引导朗读文本 ──
  const guide = el('small','', t('请朗读以下文本（30 秒-3 分钟，覆盖常见音素与韵律）：'));
  const guideText = el('div','', t('今天天气不错，我们一起去公园散步吧。我喜欢清晨的咖啡和安静的街道，工作时专注、生活中温和。'));
  guideText.style.cssText = 'background:#f5f5f5;padding:8px;border-radius:6px;margin:6px 0;';
  wrap.append(guide, guideText);

  // ── 录音 ──
  let mediaRec = null, chunks = [];
  const recBtn = el('button','quiet',t('开始录音')); recBtn.type='button';
  const stopBtn = el('button','quiet',t('停止并上传')); stopBtn.type='button'; stopBtn.disabled=true;
  recBtn.onclick = action(async () => {
    if (!consent.checked) { toast(t('请先勾选同意')); return; }
    try {
      const stream = await navigator.mediaDevices.getUserMedia({audio:true});
      mediaRec = new MediaRecorder(stream);
      chunks = [];
      mediaRec.ondataavailable = e => { if (e.data.size) chunks.push(e.data); };
      mediaRec.onstop = async () => {
        stream.getTracks().forEach(t=>t.stop());
        const blob = new Blob(chunks, {type: mediaRec.mimeType || 'audio/webm'});
        await uploadSample(blob);
        recBtn.disabled=false; stopBtn.disabled=true;
        loadSamples();
      };
      mediaRec.start();
      recBtn.disabled=true; stopBtn.disabled=false;
    } catch(e) { toast(t('无法访问麦克风：')+e.message); }
  });
  stopBtn.onclick = () => { if (mediaRec && mediaRec.state !== 'inactive') mediaRec.stop(); };
  wrap.append(recBtn, stopBtn);

  async function uploadSample(blob) {
    const fd = new FormData();
    try {
      const res = await fetch('/api/voice-sample/upload?consented=1&durationSec=5', {
        method:'POST', headers:{Authorization:'Bearer '+(state.token||'')}, body: blob
      });
      if (!res.ok) { const e=await res.json().catch(()=>({})); toast(t('上传失败：')+(e.error||res.status)); return; }
      toast(t('样本已加密保存'));
    } catch(e) { toast(t('上传失败：')+e.message); }
  }

  // ── 样本列表 ──
  const listEl = el('div','voice-history-list');
  async function loadSamples() {
    listEl.innerHTML='';
    try {
      const res = await api('/voice-samples');
      (res.samples||[]).forEach(s => {
        const row = el('div','voice-history-item');
        row.append(el('span','', new Date(s.createdAt).toLocaleString()+' · '+(s.durationSec||'?')+'s · '+Math.round(s.bytes/1024)+'KB'));
        const play = el('button','quiet',t('试听')); play.type='button';
        play.onclick = () => { const a=new Audio('/api/voice-sample/'+s.id+'/audio'); a.play(); };
        const del = el('button','quiet',t('删除')); del.type='button';
        del.onclick = action(async () => { await api('/voice-sample/'+s.id,{method:'DELETE'}); loadSamples(); });
        row.append(play, del);
        listEl.append(row);
      });
    } catch(e) {}
  }
  wrap.append(listEl);

  // ── 创建音色 ──
  const createBtn = el('button','primary',t('用所选样本创建音色')); createBtn.type='button';
  createBtn.onclick = action(async () => {
    try {
      const res = await api('/voice-samples');
      const ids = (res.samples||[]).map(s=>s.id);
      if (!ids.length) { toast(t('请先录制样本')); return; }
      const out = await api('/voice-clone/create',{method:'POST',body:JSON.stringify({sampleIds:ids})});
      toast(t('音色已创建：')+out.voiceID);
      await refreshConfig();
    } catch(e) { toast(t('创建失败：')+e.message); }
  });
  wrap.append(createBtn);

  // ── 性格推断 ──
  const inferBtn = el('button','quiet',t('从声音推断性格')); inferBtn.type='button';
  const personaBox = el('div','', '');
  personaBox.style.cssText='margin-top:8px;';
  inferBtn.onclick = action(async () => {
    try {
      const res = await api('/voice-samples');
      const ids = (res.samples||[]).map(s=>s.id);
      if (!ids.length) { toast(t('请先录制样本')); return; }
      const out = await api('/voice-personality/infer',{method:'POST',body:JSON.stringify({sampleIds:ids})});
      personaBox.innerHTML='';
      if (out.error) toast(t('推断失败：')+out.error);
      const p = out.profile||{};
      personaBox.append(el('small','', (p.summary||'')+'（'+t('AI 推断仅供参考')+'）'));
      const ta = el('textarea'); ta.rows=6; ta.style.width='100%';
      ta.value = out.promptDraft||'';
      const adopt = el('button','primary',t('采纳为小秘性格')); adopt.type='button';
      adopt.onclick = action(async () => {
        await api('/voice-personality/adopt',{method:'POST',body:JSON.stringify({prompt:ta.value})});
        toast(t('已采纳小秘性格'));
      });
      personaBox.append(ta, adopt);
    } catch(e) { toast(t('推断失败：')+e.message); }
  });
  wrap.append(inferBtn, personaBox);

  loadSamples();
  return wrap;
}
controlRenderers['voice-clone'] = renderVoiceCloneControl;
// 设置 → 无障碍：输出完成后由小秘自动朗读讲解
function renderAccessibilityControl() {
  const wrap = el('div', 'settings-control');
  const head = el('div', 'control-label'); head.append(el('span', '', t('无障碍')));
  const row = el('div', 'voice-reply-row');
  const toggle = el('input'); toggle.type = 'checkbox';
  toggle.checked = !!(state.config && state.config.accessibilityAutoRead);
  let busy = false;
  // 勾选/取消即自动保存，无需再点保存按钮
  toggle.onchange = action(async () => {
    if (busy) return; busy = true;
    try {
      await api('/settings', { method: 'PUT', body: JSON.stringify({ accessibilityAutoRead: toggle.checked, activeModel: state.config ? state.config.activeModel : '' }) });
      await refreshConfig();
      toast(toggle.checked ? t('已开启：输出完成后自动朗读') : t('已关闭自动朗读'));
    } finally { busy = false; }
  });
  row.append(toggle, el('span', '', t('输出完成后自动朗读')));
  wrap.append(head, row, el('small', '', t('勾选即自动保存。开启后：每次模型输出完成，由小秘自动滚动、打开相关文件并口头讲解本次输出；aide 主会话本身不发声。')));
  return wrap;
}
controlRenderers['accessibility-read'] = renderAccessibilityControl;
// 设置：外部 AI 机器可读诊断接口（/api/debug）——默认关、只读、独立令牌、审计脱敏
function renderDebugAccessControl() {
  const wrap = el('div', 'settings-control');
  const head = el('div', 'control-label'); head.append(el('span', '', t('允许外部 AI 接入调试')));
  const row = el('div', 'voice-reply-row');
  const toggle = el('input'); toggle.type = 'checkbox';
  toggle.checked = !!(state.config && state.config.debugAccessEnabled);
  let busy = false;
  toggle.onchange = action(async () => {
    if (busy) return; busy = true;
    try {
      await api('/settings', { method: 'PUT', body: JSON.stringify({ debugAccessEnabled: toggle.checked, activeModel: state.config ? state.config.activeModel : '' }) });
      await refreshConfig();
      toast(toggle.checked ? t('已开启外部 AI 调试') : t('已关闭外部 AI 调试'));
      renderBody();
    } finally { busy = false; }
  });
  row.append(toggle, el('span', '', t('只读诊断')));
  const warn = el('div', 'dbg-warn', t('高危开关：开启后，持有调试令牌的外部程序可只读查看会话、错误、用量与 Provider 连通性。默认关闭；关闭时本接口整体不可达，并立即清空已发令牌。'));

  const body = el('div');
  async function renderBody() {
    body.replaceChildren();
    const enabled = !!(state.config && state.config.debugAccessEnabled);
    if (!enabled) { body.append(el('small', '', t('当前已关闭'))); return; }

    // ── 令牌子卡片 ──
    const tokCard = el('div', 'dbg-card');
    tokCard.append(el('div', 'dbg-card-head', t('调试令牌')));
    const tokRow = el('div', 'dbg-row');
    const hasTok = !!(state.config && state.config.hasDebugToken);
    tokRow.append(el('span', '', hasTok ? t('状态：已生成') : t('状态：未生成')));
    const genBtn = el('button', 'quiet', hasTok ? t('重新生成') : t('生成调试令牌')); genBtn.type = 'button';
    const revBtn = el('button', 'quiet', t('立即吊销')); revBtn.type = 'button';
    revBtn.style.display = hasTok ? '' : 'none';
    tokRow.append(genBtn, revBtn);
    tokCard.append(tokRow);
    const tokenBox = el('div');
    tokCard.append(tokenBox);
    body.append(tokCard);
    genBtn.onclick = action(async () => {
      const r = await api('/debug/admin/token', { method: 'POST', body: '{}' });
      tokenBox.replaceChildren();
      tokenBox.append(el('small', '', t('明文令牌仅此一次显示，请立即复制保存：')));
      const code = el('div', 'dbg-token', r.token);
      const copy = el('button', 'quiet', t('复制')); copy.type = 'button';
      copy.onclick = () => { navigator.clipboard && navigator.clipboard.writeText(r.token); toast(t('已复制')); };
      tokenBox.append(code, copy, el('small', '', t('有效期至 {0}', r.expiresAt)));
      await refreshConfig();
    });
    revBtn.onclick = action(async () => {
      await api('/debug/admin/revoke', { method: 'POST', body: '{}' });
      await refreshConfig(); toast(t('已吊销调试令牌')); renderBody();
    });

    // ── 来源白名单子卡片 ──
    const wlCard = el('div', 'dbg-card');
    wlCard.append(el('div', 'dbg-card-head', t('来源白名单')));
    const wlRow = el('div', 'dbg-row');
    const wl = el('input'); wl.type = 'text'; wl.placeholder = t('来源白名单（可选，逗号分隔）');
    wl.value = (state.config && state.config.debugAllowOrigins || []).join(', ');
    const wlSave = el('button', 'quiet', t('保存')); wlSave.type = 'button';
    wlSave.onclick = action(async () => {
      const origins = wl.value.split(',').map(s => s.trim()).filter(Boolean);
      await api('/settings', { method: 'PUT', body: JSON.stringify({ debugAllowOrigins: origins, activeModel: state.config ? state.config.activeModel : '' }) });
      await refreshConfig(); toast(t('已保存来源白名单'));
    });
    wlRow.append(wl, wlSave);
    wlCard.append(wlRow, el('small', '', t('仅校验浏览器 Origin；纯 curl 请求不带 Origin，不受此限制。留空表示不限制来源。')));
    body.append(wlCard);

    // ── 接入审计子卡片（表格化 + 筛选 + 下载）──
    const audCard = el('div', 'dbg-card');
    audCard.append(el('div', 'dbg-card-head', t('接入审计')));
    const bar = el('div', 'dbg-filterbar');
    const seg = el('div', 'dbg-seg');
    const filterDefs = [['all', t('全部')], ['external', t('仅外部')], ['failed', t('仅失败')]];
    let filter = 'all';
    const kw = el('input'); kw.type = 'text'; kw.placeholder = t('路径关键字');
    const loadBtn = el('button', 'quiet', t('刷新')); loadBtn.type = 'button';
    const dlBtn = el('button', 'quiet', t('下载审计日志')); dlBtn.type = 'button';
    bar.append(seg, kw, loadBtn, dlBtn);
    const tableBox = el('div', 'dbg-table');
    audCard.append(bar, tableBox);
    body.append(audCard);

    let rows = [];
    const segBtns = filterDefs.map(([v, label]) => {
      const b = el('button', 'quiet', label); b.type = 'button';
      b.onclick = () => { filter = v; syncSeg(); renderTable(); };
      return b;
    });
    seg.append(...segBtns);
    function syncSeg() { segBtns.forEach((b, i) => b.classList.toggle('active', filterDefs[i][0] === filter)); }
    function resultTag(result) {
      const tag = el('span', 'dbg-tag', result);
      if (/^200/.test(result)) tag.classList.add('ok');
      else if (/^40[13]/.test(result)) tag.classList.add('bad');
      else tag.classList.add('warn');
      return tag;
    }
    function renderTable() {
      tableBox.replaceChildren();
      const kwv = kw.value.trim().toLowerCase();
      const shown = rows.filter(e => {
        if (filter === 'external' && e.owner) return false;
        if (filter === 'failed' && /^200/.test(e.result)) return false;
        if (kwv && !(e.path || '').toLowerCase().includes(kwv)) return false;
        return true;
      });
      if (!shown.length) { tableBox.append(el('small', '', t('暂无审计记录'))); return; }
      const table = el('table');
      const thead = el('thead'); const trh = el('tr');
      ['时间', '来源', '方法', '路径', '结果'].forEach(h => trh.append(el('th', '', t(h))));
      thead.append(trh); table.append(thead);
      const tbody = el('tbody');
      shown.slice().reverse().forEach(e => {
        const tr = el('tr');
        const src = e.owner ? t('owner') : (t('令牌') + ' ' + (e.tokenFp || '?'));
        tr.append(
          el('td', '', (e.time || '').replace('T', ' ').replace(/\.\d+Z?$/, '')),
          el('td', '', src + (e.ip ? ' · ' + e.ip : '')),
          el('td', '', e.method || ''),
          el('td', '', e.path || ''));
        const td = el('td'); td.append(resultTag(e.result)); tr.append(td);
        tbody.append(tr);
      });
      table.append(tbody);
      tableBox.append(table);
    }
    kw.oninput = () => renderTable();
    loadBtn.onclick = action(async () => { rows = await api('/debug/audit', {}); renderTable(); });
    dlBtn.onclick = action(async () => {
      const res = await fetch('/api/debug/audit/export?format=jsonl', { headers: { 'Authorization': 'Bearer ' + state.token } });
      if (!res.ok) throw new Error(t('下载失败'));
      const blob = await res.blob();
      const url = URL.createObjectURL(blob);
      const a = document.createElement('a');
      a.href = url; a.download = 'debug-audit.jsonl'; a.click();
      setTimeout(() => URL.revokeObjectURL(url), 5000);
      toast(t('已下载审计日志'));
    });
    rows = await api('/debug/audit', {});
    syncSeg(); renderTable();
  }
  renderBody();
  wrap.append(head, row, warn, body);
  return wrap;
}
controlRenderers['debug-access'] = renderDebugAccessControl;

// 设置：小秘麦克风输入源选择
function renderVoiceInputSourceControl() {
  const wrap = el('div', 'settings-control');
  const head = el('div', 'control-label'); head.append(el('span', '', t('麦克风输入源')));
  const row = el('div', 'voice-input-row');
  const sel = el('select', 'voice-input-select');
  const refresh = el('button', 'quiet', t('刷新')); refresh.type = 'button';
  row.append(sel, refresh);
  const note = el('small', '', t('默认用系统麦克风。浏览器支持 SpeechRecognition.start(audioTrack) 时使用所选设备；不支持时回退系统默认。'));
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
  if (pid === 'xiaomi') {
    wrap.append(el('small', 'muted', t('性格演化会使用小秘会话中的语音与文字历史，并从整段历史取样；加密语音历史需先在历史设置中解锁。')));
  }

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
// 文件只读查看标签（#file=… / body.file-view-mode）：锁屏系统被动化——不参与选举、
// 不卡 joining 面纱、不响应后续集群锁定与空闲升锁。仅打开时做一次短超时后端权威核对。
function lockScreenActive() {
  return !document.body.classList.contains('file-view-mode');
}
// file-view 打开时一次短超时后端权威核对：明确锁定才罩屏；未锁/失败超时直接显示文件。
function checkFileViewLock() {
  const ctrl = (typeof AbortController !== 'undefined') ? new AbortController() : null;
  const timer = setTimeout(() => { try { ctrl && ctrl.abort(); } catch (_) {} }, 1000);
  fetch('/api/lock-state', { headers: { 'Authorization': 'Bearer ' + (state.token || '') }, signal: ctrl ? ctrl.signal : undefined })
    .then(r => { clearTimeout(timer); if (!r.ok) return null; return r.json(); })
    .then(d => { if (d && d.locked) applyLockVisual(); })
    .catch(() => { clearTimeout(timer); /* 失败默认放行：双击只可能来自已解锁主界面 */ });
}
// 空闲定时器只在 master 持有；slave 的活动经 ping 续 master 的表。
function resetIdleTimer() {
  clearTimeout(lockScreen.timer);
  lockScreen.timer = null;
  if (!lockScreenActive()) return; // 文件只读查看标签不参与空闲升锁
  if (typeof LockCluster !== 'undefined' && !LockCluster.isMaster()) return;
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
    ? t('运行中 · {0} · {1}', phaseLabel(running), formatElapsed(running.startedAt))
    : t('空闲 · 后台任务不受锁屏影响');
}
/* 视觉层：把 effectiveLocked=true 落到本地遮罩 + 小秘退下（幂等，可重复调用）。 */
function applyLockVisual() {
  if (!state.config || !state.config.hasPassword) return;
  const veil = $('lock-screen');
  if (lockScreen.locked) { refreshLockStatus(); return; }
  lockScreen.locked = true;
  clearTimeout(lockScreen.timer); lockScreen.timer = null;
  // 小秘退下：停止听写 + 取消朗读（解锁后按原状态恢复）
  lockScreen.wasVoiceListening = !!voice.listening;
  if (voice.listening) voiceClose();
  if (xiaomiDictation.active || xiaomiDictation.starting) stopXiaomiDictation();
  ttsCancel();
  veil.hidden = false;
  veil.classList.remove('joining');
  $('lock-password').value = '';
  $('lock-error').textContent = '';
  refreshLockStatus();
  setTimeout(() => { try { $('lock-password').focus(); } catch (_) {} }, 60);
  updateTouchIdButton();
}
/* 视觉层：把 effectiveLocked=false 落到本地（仅藏遮罩；欢迎语/麦克风恢复只在输密码的 tab）。 */
function releaseLockVisual() {
  lockScreen.locked = false;
  const veil = $('lock-screen');
  veil.hidden = true;
  veil.classList.remove('joining');
}
/* 加入窗口期中性面纱：不露内容、不抢密码框。 */
function showJoiningVeil() {
  const veil = $('lock-screen');
  veil.hidden = false;
  veil.classList.add('joining');
  $('lock-error').textContent = '';
  $('lock-status').textContent = t('正在确认安全状态…');
}
/* 升锁入口：master 写 masterLocked 并广播；slave 转 req-lock 给 master。 */
function lockScreenNow() {
  if (!state.config || !state.config.hasPassword) return;
  clearAssistantGate(); // #30：锁屏后小秘会话需重新验证密码
  if (typeof LockCluster !== 'undefined') LockCluster.requestLock('manual');
  else applyLockVisual();
}
/* dismissAfterUnlock：密码与触控 ID 解锁共用的唯一收尾出口——保证两路径行为逐字节一致。 */
function dismissAfterUnlock() {
  // 本 tab 本地恢复（欢迎语、麦克风只在解锁的那个 tab，避免多 tab 合唱）
  lockScreen.locked = false;
  $('lock-screen').hidden = true;
  $('lock-screen').classList.remove('joining');
  const name = (state.config && state.config.userName) || '';
  const xm = (state.config && state.config.voiceAssistantName) || t('小秘');
  const welcome = name ? t('欢迎回来，{0}，我是{1}。', name, xm) : t('欢迎回来，我是{0}。', xm);
  toast(welcome);
  speakReply(welcome);
  if (lockScreen.wasVoiceListening) { lockScreen.wasVoiceListening = false; voiceStart(); }
  if (typeof LockCluster !== 'undefined') LockCluster.handleUnlockSuccess();
  resetIdleTimer();
}
function unlockScreen(pw) {
  return api('/account/verify-password', { method: 'POST', body: JSON.stringify({ password: pw }) }).then(dismissAfterUnlock);
}

/* ── 触控 ID / WebAuthn 解锁 ── */
function b64uToBuf(b64url) {
  const pad = '='.repeat((4 - (b64url.length % 4)) % 4);
  const bin = atob(b64url.replace(/-/g, '+').replace(/_/g, '/') + pad);
  return Uint8Array.from(bin, c => c.charCodeAt(0)).buffer;
}
function bufToB64u(buf) {
  const bytes = new Uint8Array(buf);
  let bin = '';
  bytes.forEach(b => bin += String.fromCharCode(b));
  return btoa(bin).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '');
}
async function touchIdAvailable() {
  if (!state.config || !state.config.webAuthnReady) return { ok: false, reason: 'not-ready' };
  if (location.hostname !== 'localhost') return { ok: false, reason: 'use-localhost' };
  if (!window.PublicKeyCredential) return { ok: false, reason: 'unsupported' };
  try {
    const ok = await PublicKeyCredential.isUserVerifyingPlatformAuthenticatorAvailable();
    return ok ? { ok: true } : { ok: false, reason: 'no-touchid' };
  } catch { return { ok: false, reason: 'unsupported' }; }
}
/* 锁屏时根据探测结果显示/隐藏触控 ID 按钮。 */
async function updateTouchIdButton() {
  const btn = $('touchid-unlock-btn');
  const hint = $('touchid-hint');
  if (!btn) return;
  const res = await touchIdAvailable();
  if (!res.ok) {
    btn.hidden = true;
    if (hint) { hint.hidden = res.reason !== 'use-localhost'; }
    return;
  }
  // 平台 authenticator 可用：按是否已注册凭证决定可点 / 置灰
  btn.hidden = false;
  const hasCred = !!(state.config && state.config.hasPlatformCredential);
  btn.disabled = !hasCred;
  btn.classList.toggle('disabled', !hasCred);
  btn.setAttribute('aria-disabled', String(!hasCred));
  if (hint) {
    hint.hidden = hasCred;
    hint.textContent = t('未注册触控 ID，可在设置 → 账号中绑定');
  }
}
function assertionToJSON(a) {
  return {
    id: a.id, rawId: bufToB64u(a.rawId), type: a.type,
    response: {
      clientDataJSON: bufToB64u(a.response.clientDataJSON),
      authenticatorData: bufToB64u(a.response.authenticatorData),
      signature: bufToB64u(a.response.signature),
      userHandle: a.response.userHandle ? bufToB64u(a.response.userHandle) : null,
    },
  };
}
async function unlockByTouchId() {
  const btn = $('touchid-unlock-btn');
  if (btn) btn.disabled = true;
  try {
    const start = await api('/webauthn/assertion/start', { method: 'POST', body: '{}' });
    if (!start.allowCredentials || !start.allowCredentials.length) {
      $('lock-error').textContent = t('未注册触控 ID 设备，请先在设置中绑定');
      return;
    }
    const options = {
      challenge: b64uToBuf(start.challenge),
      rpId: start.rpId,
      allowCredentials: start.allowCredentials.map(c => ({ type: c.type, id: b64uToBuf(c.id), transports: c.transports })),
      userVerification: start.userVerification || 'preferred',
      timeout: 120000,
    };
    const assertion = await navigator.credentials.get({ publicKey: options });
    if (!assertion) { $('lock-error').textContent = ''; return; }
    const finishRes = await fetch('/api/webauthn/assertion/finish?challenge=' + encodeURIComponent(start.challenge), {
      method: 'POST',
      headers: { 'Authorization': 'Bearer ' + state.token, 'Content-Type': 'application/json' },
      body: JSON.stringify(assertionToJSON(assertion)),
    });
    if (!finishRes.ok) {
      const err = await finishRes.json().catch(() => ({}));
      throw new Error(t(err.error || '验证失败，请重试'));
    }
    dismissAfterUnlock();
  } catch (e) {
    if (e && e.name === 'NotAllowedError') { $('lock-error').textContent = ''; return; }
    $('lock-error').textContent = t('验证失败，请重试');
  } finally {
    if (btn) btn.disabled = false;
  }
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
const touchidBtn = $('touchid-unlock-btn');
if (touchidBtn) touchidBtn.onclick = action(unlockByTouchId);
// 集群把 effectiveLocked 映射到本地视觉（master 自身、slave 收到 lock/unlock 均走这里）
if (typeof LockCluster !== 'undefined') {
  LockCluster.on('effective', locked => { if (!lockScreenActive()) return; if (locked) applyLockVisual(); else releaseLockVisual(); });
  LockCluster.setRemoteActivityHook(() => resetIdleTimer()); // master 续表
}
['mousemove', 'keydown', 'click', 'scroll', 'touchstart'].forEach(ev =>
  window.addEventListener(ev, () => {
    // master 自己活动即续表；slave 活动节流发 ping，由 master 续表
    if (typeof LockCluster !== 'undefined') LockCluster.noteActivity();
    resetIdleTimer();
  }, { passive: true }));
setInterval(() => { if (lockScreen.locked) refreshLockStatus(); }, 1000);

// 设置面板「账户」：用户名 / 锁屏密码 / 锁屏时间 / 立即锁屏
function renderAccountControl() {
  const wrap = el('div', 'settings-control account-control');
  const cfg = state.config || {};
  const hasPw = !!cfg.hasPassword;
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
  actions.append(save, lockBtn);
  wrap.append(uRow, tRow, oldRow, newRow, actions);

  // ── 触控 ID / Passkey 管理 ──
  if (cfg.webAuthnReady) {
    const waSection = el('div', 'wa-section');
    waSection.append(el('div', 'control-label', t('触控 ID / Passkey')));
    const waList = el('div', 'wa-cred-list');
    const regBtn = el('button', 'quiet', t('注册新设备')); regBtn.type = 'button';

    async function refreshWaList() {
      waList.replaceChildren();
      try {
        const creds = await api('/webauthn/credentials');
        if (state.config) state.config.hasPlatformCredential = creds.length > 0;
        if (!creds.length) waList.append(el('div', 'wa-cred-meta', t('未注册设备')));
        creds.forEach(c => {
          const item = el('div', 'wa-cred-item');
          const nameEl = el('span', 'wa-cred-name', t(c.name));
          const meta = el('span', 'wa-cred-meta', new Date((c.createdAt || 0) * 1000).toLocaleDateString());
          const delBtn = el('button', 'quiet', t('删除')); delBtn.type = 'button';
          delBtn.onclick = action(async () => {
            const pw = prompt(t('删除设备请输入原密码'));
            if (!pw) return;
            await api('/webauthn/credentials/' + encodeURIComponent(c.id), { method: 'DELETE', body: JSON.stringify({ oldPassword: pw }) });
            toast(t('设备已删除'));
            refreshWaList();
          });
          item.append(nameEl, meta, delBtn);
          waList.append(item);
        });
      } catch (_) {}
    }
    refreshWaList();

    regBtn.onclick = action(async () => {
      if (location.hostname !== 'localhost') { toast(t('请用 localhost 打开以使用 Touch ID')); return; }
      const pw = await passwordPrompt(t('注册触控 ID 需验证原密码'));
      if (!pw) return;
      const start = await api('/webauthn/register/start', { method: 'POST', body: JSON.stringify({ oldPassword: pw }) });
      const options = {
        challenge: b64uToBuf(start.challenge),
        rp: { id: start.rp.id, name: start.rp.name },
        user: { id: b64uToBuf(start.user.id), name: start.user.name, displayName: start.user.displayName },
        pubKeyCredParams: start.pubKeyCredParams,
        authenticatorSelection: start.authenticatorSelection,
        excludeCredentials: (start.excludeCredentials || []).map(c => ({ type: c.type, id: b64uToBuf(c.id), transports: c.transports })),
        timeout: 120000,
        attestation: start.attestation || 'none',
      };
      const cred = await navigator.credentials.create({ publicKey: options });
      if (!cred) return;
      const credJSON = {
        id: cred.id, rawId: bufToB64u(cred.rawId), type: cred.type,
        response: {
          attestationObject: bufToB64u(cred.response.attestationObject),
          clientDataJSON: bufToB64u(cred.response.clientDataJSON),
        },
      };
      const finishRes = await fetch('/api/webauthn/register/finish?challenge=' + encodeURIComponent(start.challenge), {
        method: 'POST',
        headers: { 'Authorization': 'Bearer ' + state.token, 'Content-Type': 'application/json' },
        body: JSON.stringify(credJSON),
      });
      if (!finishRes.ok) { const e = await finishRes.json().catch(()=>({})); toast(t(e.error || '注册失败')); return; }
      toast(t('触控 ID 已绑定'));
      refreshWaList();
    });

    waSection.append(waList, regBtn);
    wrap.append(waSection);
  }

  wrap.append(
    el('small', '', t('不设密码且锁屏时间为 0 时不锁屏。密码同时作为小秘对话历史的 AES-256-GCM 加密密钥，只存哈希、不明文回显。')));
  return wrap;
}
controlRenderers['account'] = renderAccountControl;

// ===== 配置备份：导出 / 导入引导 =====
function renderBackupControl() {
  const wrap = el('div', 'settings-control backup-control');

  // ---------- 导出 ----------
  const exp = el('div', 'backup-block');
  exp.append(el('div', 'backup-block-title', t('导出配置')));
  exp.append(el('p', 'backup-desc', t('将模型、沙箱、性格、语音、账户等全部配置导出为一个 JSON 备份文件，便于迁移或恢复。')));
  const secBox = el('label', 'backup-check');
  const sec = el('input'); sec.type = 'checkbox';
  secBox.append(sec, el('span', '', t('包含敏感凭据（API Key、登录密码哈希、性格密文、来源密钥）')));
  exp.append(secBox);
  exp.append(el('p', 'backup-warn', t('注意：API Key 等密钥以加密信封导出、不含明文；仍请妥善保管备份文件，不要分享或上传到公共位置。')));
  const voiceBox = el('label', 'backup-check');
  const vc = el('input'); vc.type = 'checkbox';
  voiceBox.append(vc, el('span', '', t('包含语音小秘的对话历史')));
  exp.append(voiceBox);
  const expBtn = el('button', 'primary', t('导出配置'));
  exp.append(expBtn);
  expBtn.onclick = action(async () => {
    expBtn.disabled = true;
    try {
      const res = await fetch('/api/config/export', { method: 'POST', headers: { Authorization: 'Bearer ' + state.token, 'Content-Type': 'application/json' }, body: JSON.stringify({ includeSecrets: sec.checked, includeVoiceData: vc.checked }) });
      if (!res.ok) { toast(t('导出失败：') + res.status); return; }
      const blob = await res.blob();
      let fn = 'aide-config.json';
      const m = (res.headers.get('Content-Disposition') || '').match(/filename="?([^"]+)"?/);
      if (m) fn = m[1];
      const url = URL.createObjectURL(blob);
      const a = el('a'); a.href = url; a.download = fn;
      document.body.appendChild(a); a.click(); a.remove();
      setTimeout(() => URL.revokeObjectURL(url), 1500);
      toast(t('配置已导出：') + fn);
    } finally { expBtn.disabled = false; }
  });

  // ---------- 导入 ----------
  const imp = el('div', 'backup-block');
  imp.append(el('div', 'backup-block-title', t('导入配置')));
  imp.append(el('p', 'backup-desc', t('选择此前导出的 aide 配置备份文件，确认内容后导入。')));
  const fileInput = el('input'); fileInput.type = 'file'; fileInput.accept = '.json,application/json'; fileInput.style.display = 'none';
  const chooseBtn = el('button', 'quiet', t('选择备份文件'));
  imp.append(chooseBtn, fileInput);
  const info = el('div', 'backup-info hidden');
  const impSecBox = el('label', 'backup-check');
  const impSec = el('input'); impSec.type = 'checkbox';
  impSecBox.append(impSec, el('span', '', t('导入敏感凭据')));
  const impVoiceBox = el('label', 'backup-check');
  const impVoice = el('input'); impVoice.type = 'checkbox';
  impVoiceBox.append(impVoice, el('span', '', t('导入小秘对话历史')));
  const impNote = el('p', 'backup-note', t('导入前会自动把当前配置另存为回滚点 settings.json.pre-import。'));
  const pwNote = el('p', 'backup-warn', t('若导入含密码哈希的备份，登录密码将变为备份时的密码，之后需用该密码解锁。'));
  const impBtn = el('button', 'primary', t('确认导入')); impBtn.disabled = true;
  info.append(impSecBox, impVoiceBox, impNote, pwNote, impBtn);
  imp.append(info);
  let parsed = null, armTimer = null;

  chooseBtn.onclick = () => fileInput.click();
  fileInput.onchange = () => {
    const f = fileInput.files[0];
    if (!f) return;
    const rd = new FileReader();
    rd.onload = () => {
      try {
        parsed = JSON.parse(rd.result);
        if (parsed.format !== 'aide-config-backup') { toast(t('不是有效的 aide 配置备份文件')); parsed = null; return; }
        info.classList.remove('hidden');
        info.querySelector('.backup-meta')?.remove();
        const meta = el('div', 'backup-meta');
        meta.append(el('div', '', t('来源版本') + '：' + (parsed.appVersion || '?')));
        meta.append(el('div', '', t('导出时间') + '：' + fmtBackupTime(parsed.exportedAt)));
        const tags = el('div', 'backup-tags');
        if (parsed.includeSecrets) tags.append(el('span', 'backup-tag tag-secret', t('含敏感凭据')));
        if (parsed.includeVoiceData) tags.append(el('span', 'backup-tag tag-voice', t('含小秘历史')));
        if (!tags.children.length) tags.append(el('span', 'backup-tag', t('仅配置')));
        meta.append(tags);
        info.prepend(meta);
        impSec.disabled = !parsed.includeSecrets; impSec.checked = !!parsed.includeSecrets;
        impVoice.disabled = !parsed.includeVoiceData; impVoice.checked = !!parsed.includeVoiceData;
        impBtn.disabled = false;
      } catch (e) { toast(t('文件解析失败，请选择有效的 JSON 备份')); parsed = null; }
    };
    rd.readAsText(f);
  };

  impBtn.onclick = action(async () => {
    if (!impBtn.dataset.armed) {
      impBtn.dataset.armed = '1'; impBtn.textContent = t('再次点击以确认导入');
      armTimer = setTimeout(() => { delete impBtn.dataset.armed; impBtn.textContent = t('确认导入'); }, 3500);
      return;
    }
    clearTimeout(armTimer);
    impBtn.disabled = true;
    try {
      const res = await api('/config/import', { method: 'POST', body: JSON.stringify({ backup: parsed, importSecrets: impSec.checked, importVoice: impVoice.checked }) });
      await refreshConfig();
      if (res.passwordChanged) toast(t('导入完成：登录密码已变更为备份时的密码，请使用该密码解锁'));
      else if (res.migratedKey) toast(t('配置导入完成，API Key 已安全迁移至加密保险库'));
      else toast(t('配置导入完成'));
      parsed = null; fileInput.value = ''; info.classList.add('hidden');
      delete impBtn.dataset.armed; impBtn.textContent = t('确认导入');
    } catch (e) { toast(t('导入失败：') + (e.message || e)); impBtn.disabled = false; }
  });

  wrap.append(exp, el('div', 'backup-divider'), imp);
  return wrap;
}
controlRenderers['config-backup'] = renderBackupControl;
function fmtBackupTime(iso) { try { return new Date(iso).toLocaleString(); } catch { return iso || ''; } }

// ===== 危险操作：恢复出厂设置（#40）=====
// 范围可选、默认只勾"设置项"；强确认（勾选不可恢复 + 输入"重置"）；
// 涉及凭据/小秘历史时要求重输登录密码；执行前自动备份并告知备份路径；
// /workspace、/context 里的用户代码与文件永不删除。
function renderFactoryResetControl() {
  const wrap = el('div', 'settings-control');
  const zone = el('div', 'danger-zone');
  zone.append(el('div', 'backup-block-title', t('恢复出厂设置')));
  zone.append(el('p', 'backup-desc', t('以下操作会改动 aide 的配置与本地数据。/workspace 与 /context 里的你的代码和文件永远不会被删除。')));
  const btn = el('button', 'danger-outline', t('开始重置…'));
  btn.type = 'button';
  zone.append(btn);
  wrap.append(zone);

  btn.onclick = action(() => openFactoryResetDialog());
  return wrap;
}
controlRenderers['factory-reset'] = renderFactoryResetControl;

let frDialog = null;
function openFactoryResetDialog() {
  if (frDialog) { frDialog.remove(); frDialog = null; }
  const dlg = el('dialog', 'fr-dialog');
  frDialog = dlg;

  const head = el('div', 'dialog-heading');
  head.append(el('h2', '', t('恢复出厂设置')));
  const closeX = el('button', 'icon-button', '×'); closeX.type = 'button';
  closeX.onclick = () => dlg.close();
  head.append(closeX);
  dlg.append(head);

  dlg.append(el('p', '', t('选择要重置的范围。默认只重置设置项；更具破坏性的项需要你主动勾选。执行前会自动生成完整备份（含加密密钥与小秘历史）。')));

  // 范围分组复选框
  const groups = [
    { key: 'settings', label: t('设置项'), desc: t('模型 / TTS / 主题 / 权限 / 工具轮数 / 推理强度 / 沙箱 / 工作流 / 无障碍'), checked: true },
    { key: 'sessionsAndMemory', label: t('会话与记忆'), desc: t('全部会话 + aide 核心记忆 + 小秘历史/记忆 + 性格恢复默认'), checked: false },
    { key: 'credentialsAndKeys', label: t('凭据与密钥'), desc: t('登录密码 / API Key / SSH·vault 凭据 / 来源密钥 / 调试令牌 / WebAuthn / KDF salt / access-token'), checked: false },
    { key: 'workspaceConfig', label: t('工作空间配置'), desc: t('工作空间连接配置回到本地默认（不删除任何用户文件）'), checked: false },
  ];
  const boxes = {};
  for (const g of groups) {
    const row = el('label', 'fr-row');
    const cb = el('input'); cb.type = 'checkbox'; cb.checked = g.checked;
    boxes[g.key] = cb;
    const txt = el('span');
    txt.append(el('strong', '', g.label));
    txt.append(el('div', 'muted', g.desc));
    row.append(cb, txt);
    dlg.append(row);
  }

  // 强确认：不可恢复勾选
  const ackBox = el('label', 'checkbox');
  const ack = el('input'); ack.type = 'checkbox';
  ackBox.append(ack, el('span', '', t('我了解此操作不可恢复')));
  dlg.append(ackBox);

  // 输入"重置"二字
  const confirmLabel = el('label', '', t('输入"重置"以确认'));
  const confirmInput = el('input'); confirmInput.type = 'text'; confirmInput.autocomplete = 'off';
  confirmInput.placeholder = '重置';
  confirmLabel.append(confirmInput);
  dlg.append(confirmLabel);

  // 密码框：涉及凭据/小秘历史且已设密码时显示
  const pwRow = el('label', 'fr-pw-row hidden', t('登录密码（确认本人）'));
  const pwInput = el('input'); pwInput.type = 'password'; pwInput.autocomplete = 'off';
  pwRow.append(pwInput);
  dlg.append(pwRow);

  const preserve = el('p', 'backup-warn', t('永不删除：/workspace 与 /context 里的用户代码与文件。备份位于 /data/config/backups/。'));
  dlg.append(preserve);
  const resultNote = el('p', 'fr-result hidden');
  dlg.append(resultNote);

  const actions = el('div', 'editor-footer');
  const cancel = el('button', 'quiet', t('取消')); cancel.type = 'button';
  cancel.onclick = () => dlg.close();
  const exec = el('button', 'danger-outline', t('执行重置')); exec.type = 'button'; exec.disabled = true;
  actions.append(cancel, exec);
  dlg.append(actions);

  document.body.append(dlg);

  const needPw = () => (boxes.credentialsAndKeys.checked || boxes.sessionsAndMemory.checked) && state.config?.hasPassword;
  const refresh = () => {
    pwRow.classList.toggle('hidden', !needPw());
    const anyScope = Object.values(boxes).some(b => b.checked);
    exec.disabled = !(anyScope && ack.checked && confirmInput.value.trim() === '重置');
  };
  for (const b of Object.values(boxes)) b.onchange = refresh;
  ack.onchange = refresh;
  confirmInput.oninput = refresh;

  exec.onclick = action(async () => {
    exec.disabled = true;
    exec.textContent = t('正在重置…');
    try {
      const res = await api('/factory-reset', { method: 'POST', body: JSON.stringify({
        scope: {
          settings: boxes.settings.checked,
          sessionsAndMemory: boxes.sessionsAndMemory.checked,
          credentialsAndKeys: boxes.credentialsAndKeys.checked,
          workspaceConfig: boxes.workspaceConfig.checked,
        },
        confirmText: '重置',
        password: needPw() ? pwInput.value : '',
      })});
      resultNote.classList.remove('hidden');
      resultNote.style.color = 'var(--danger)';
      resultNote.textContent = t('重置完成。备份：') + (res.backupPath || '');
      toast(t('恢复出厂设置完成，即将返回登录页'));
      setTimeout(() => window.location.reload(), 1600);
    } catch (e) {
      resultNote.classList.remove('hidden');
      resultNote.style.color = 'var(--danger)';
      resultNote.textContent = t('失败：') + (e.message || e);
      exec.disabled = false;
      exec.textContent = t('执行重置');
    }
  });

  if (typeof dlg.showModal === 'function') dlg.showModal(); else dlg.setAttribute('open', '');
}

// ===== SQLite 查看器 =====
async function setupSqliteViewer(container, path, root) {
  container.innerHTML = '<div class="sqlite-empty">正在加载数据库…</div>';
  try {
    // 获取表列表
    const tables = await api('/sqlite/tables?root=' + root + '&path=' + encodeURIComponent(path));
    if (!tables.length) {
      container.innerHTML = '<div class="sqlite-empty-center">数据库中没有表</div>';
      return;
    }
    let html = '<div class="sqlite-layout">';
    // 左侧表列表
    html += '<div class="sqlite-table-list">';
    tables.forEach((t, i) => {
      html += `<div class="sqlite-table-item" data-table="${escapeHtml(t.name)}">${escapeHtml(t.name)} <span class="sqlite-table-count">(${t.rows} rows)</span></div>`;
    });
    html += '</div>';
    // 右侧数据区
    html += '<div class="sqlite-data-pane"><div class="sqlite-data-area">点击左侧表查看数据</div></div>';
    html += '</div>';
    container.innerHTML = html;
    // 绑定表点击事件
    container.querySelectorAll('.sqlite-table-item').forEach(item => {
      item.onclick = async () => {
        container.querySelectorAll('.sqlite-table-item').forEach(x => x.style.background = '');
        item.style.background = 'var(--surface-hover)';
        const table = item.dataset.table;
        const dataArea = container.querySelector('.sqlite-data-area');
        dataArea.innerHTML = '<div class="sqlite-empty">加载中…</div>';
        try {
          const data = await api('/sqlite/data?root=' + root + '&path=' + encodeURIComponent(path) + '&table=' + encodeURIComponent(table) + '&limit=100&offset=0');
          let tableHtml = `<div class="sqlite-meta">共 ${data.total} 行，显示前 ${data.rows.length} 行</div>`;
          tableHtml += '<div class="sqlite-table-wrap"><table class="sqlite-table">';
          tableHtml += '<thead><tr>';
          data.columns.forEach(col => {
            tableHtml += `<th>${escapeHtml(col.name)}<div class="sqlite-col-type">${escapeHtml(col.type)}</div></th>`;
          });
          tableHtml += '</tr></thead><tbody>';
          data.rows.forEach(row => {
            tableHtml += '<tr>';
            data.columns.forEach(col => {
              const val = row[col.name];
              tableHtml += `<td>${val === null ? '<span class="sqlite-null">NULL</span>' : escapeHtml(val)}</td>`;
            });
            tableHtml += '</tr>';
          });
          tableHtml += '</tbody></table></div>';
          dataArea.innerHTML = tableHtml;
        } catch (e) {
          dataArea.innerHTML = '<div class="sqlite-error">加载失败: ' + escapeHtml(e.message || '') + '</div>';
        }
      };
    });
    // 默认点击第一个表
    const firstTable = container.querySelector('.sqlite-table-item');
    if (firstTable) firstTable.click();
  } catch (e) {
    container.innerHTML = '<div class="sqlite-error">加载失败: ' + escapeHtml(e.message || '') + '</div>';
  }
}
