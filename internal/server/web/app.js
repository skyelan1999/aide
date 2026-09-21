'use strict';
const $ = id => document.getElementById(id);
const state = { token: localStorage.getItem('aide-token') || '', session: null, mode: 'chat', root: 'workspace', dir: '.', attachments: [], file: null, busy: false, poll: null, config: null, commandAbort: null, profiles: null };
const fragment = new URLSearchParams(location.hash.slice(1));
if (fragment.has('token')) { state.token = fragment.get('token'); localStorage.setItem('aide-token', state.token); history.replaceState(null, '', location.pathname); }
function el(tag, cls, text) { const e = document.createElement(tag); if (cls) e.className = cls; if (text != null) e.textContent = text; return e; }
function toast(text) { const host = document.querySelector('dialog[open]') || document.body; host.append($('toast')); $('toast').textContent = text; $('toast').classList.remove('hidden'); clearTimeout(toast.timer); toast.timer = setTimeout(() => $('toast').classList.add('hidden'), 5000); }
async function api(path, options = {}) {
  const response = await fetch('/api' + path, { ...options, headers: { 'Authorization': 'Bearer ' + state.token, 'Content-Type': 'application/json', ...options.headers } });
  const data = await response.json();
  if (!response.ok) { if (response.status === 401 && !$('login-dialog').open) $('login-dialog').showModal(); throw new Error(data.error || '请求失败'); }
  return data;
}
function action(fn) { return async (...args) => { try { await fn(...args); } catch (e) { toast(e.message); } }; }
function setMode(mode) { state.mode = mode; document.querySelectorAll('.mode-switch button').forEach(b => b.classList.toggle('active', b.dataset.mode === mode)); }
async function refreshConfig() {
  state.config = await api('/config');
  $('connection').textContent = '● 本地服务已连接'; $('connection').classList.add('ready');
  const versionText = state.config.version ? 'v' + state.config.version : 'dev';
  $('app-version').textContent = versionText;
  $('settings-sheet-version').textContent = ' · aide ' + versionText;
  $('model-status').textContent = state.config.configured ? '已配置' : '未配置';
  $('model-name').textContent = state.config.configured ? state.config.model + ' · API 已配置' : '先配置模型，即可开始真实 AI 对话';
}
async function loadSessions() {
  const sessions = await api('/sessions'); $('sessions').replaceChildren();
  if (!sessions.length) $('sessions').append(el('p', 'sessions-empty', '还没有会话。\n从一个想法开始吧。'));
  sessions.forEach(s => { const b = el('button', 'session-item' + (state.session?.id === s.id ? ' active' : ''), '◌  ' + s.title); b.title = s.title; b.onclick = action(() => selectSession(s.id)); $('sessions').append(b); });
  return sessions;
}
async function selectSession(id) {
  clearTimeout(state.poll); state.session = await api('/sessions/' + id); renderSession(); await loadSessions(); schedulePoll();
}
function schedulePoll() {
  clearTimeout(state.poll);
  if (state.session?.runs.some(r => r.status === 'running')) state.poll = setTimeout(action(async () => {
    const id = state.session.id; const s = await api('/sessions/' + id); if (state.session?.id !== id) return;
    state.session = s; renderSession(); schedulePoll();
  }), 1200);
}
async function newSession() {
  clearTimeout(state.poll); state.session = null; state.attachments = []; renderAttachments(); renderSession(); await loadSessions(); $('prompt').focus();
}
const labels = { plan: '01 · 规划', propose: '02 · 生成方案', review: '03 · 审查', chat: 'aide' };
const statuses = { running: '运行中', completed: '已完成', failed: '失败', cancelled: '已停止', interrupted: '已中断', awaiting_approval: '等待应用' };
function renderSession() {
  const previousScroll = $('conversation').scrollTop;
  const nearBottom = $('conversation').scrollHeight - previousScroll - $('conversation').clientHeight < 100;
  const openDetails = new Set([...$('timeline').querySelectorAll('details[open][data-key]')].map(d => d.dataset.key));
  $('session-title').textContent = state.session?.title || '开始新的探索';
  $('welcome').classList.toggle('hidden', !!state.session?.runs.length);
  $('timeline').replaceChildren(); state.busy = false;
  for (const run of state.session?.runs || []) {
    if (run.status === 'running') state.busy = true;
    const box = el('article', 'run'); box.append(el('div', 'user-message', run.prompt));
    const meta = el('div', 'run-meta'); meta.append(el('span', '', run.mode === 'workflow' ? '◈ AIDE WORKFLOW · 规划 → 方案 → 审查' : '◌ AIDE ASSISTANT'), el('span', 'run-status', statuses[run.status] || run.status)); if (run.strategy) meta.append(el('span', 'run-strategy', '策略: ' + (run.strategy === 'auto' ? '自动 → ' + profileName(run.profile) : '手动 · ' + profileName(run.profile)))); box.append(meta);
    if (run.attachments?.length) box.append(el('p', 'muted', '已附加：' + run.attachments.map(a => a.root + '/' + a.path).join('、')));
    if (!run.steps.length) box.append(el('p', 'muted', '正在准备模型请求…'));
    run.steps.forEach((step, index) => {
      if (run.mode === 'chat') { box.append(el('div', 'chat-answer', step.content || (step.status === 'running' ? '正在思考…' : '未返回回答'))); return; }
      const details = el('details', 'step'); details.dataset.key = run.id + ':' + step.name;
      details.open = openDetails.has(details.dataset.key) || (index === run.steps.length - 1 && step.name !== 'propose');
      const summary = el('summary', '', labels[step.name]); summary.append(el('span', '', statuses[step.status])); details.append(summary, el('pre', 'step-content', step.content || '正在调用模型…')); box.append(details);
    });
    if (run.files?.length) {
      const proposal = el('div', 'proposal'); proposal.append(el('h4', '', `文件修改 · ${run.files.length} 个文件`));
      for (const file of run.files) {
        const details = el('details'); details.dataset.key = run.id + ':' + file.path; details.open = openDetails.has(details.dataset.key);
        details.append(el('summary', '', (file.applied ? '✓ ' : '+ ') + file.path));
        const diff = el('div', 'diff-columns'); const before = el('div'); before.append(el('small', '', '原内容'), el('pre', '', file.before || '（新文件）'));
        const after = el('div'); after.append(el('small', '', '建议内容'), el('pre', '', file.content)); diff.append(before, after); details.append(diff); proposal.append(details);
      }
      if (run.status === 'awaiting_approval') { const apply = el('button', 'primary', '应用这些文件修改'); apply.onclick = action(async () => { apply.disabled = true; try { await api(`/sessions/${state.session.id}/runs/${run.id}/apply`, { method: 'POST', body: '{}' }); toast('文件修改已写入本地挂载目录'); await selectSession(state.session.id); await loadFiles(); } finally { apply.disabled = false; } }); proposal.append(el('p', 'muted', '请展开检查文件内容。应用后会写入本地工作目录；验证命令需要单独运行。'), apply); }
      else if (run.applied) proposal.append(el('p', 'muted', '✓ 已应用文件修改。命令验证结果以命令面板为准。'));
      box.append(proposal);
    }
    if (run.commands?.length) {
      box.append(el('p', 'muted', '建议验证命令（尚未运行）'));
      run.commands.forEach(command => { const row = el('div', 'suggested-command'); const button = el('button', 'quiet', '填入命令面板'); button.onclick = () => { $('terminal-body').classList.remove('hidden'); $('terminal-state').textContent = '收起 −'; $('command').value = command; $('command').focus(); }; row.append(el('code', '', command), button); box.append(row); });
    }
    if (run.error) box.append(el('p', 'task-error', run.error)); $('timeline').append(box);
  }
  $('send').classList.toggle('hidden', state.busy); $('cancel').classList.toggle('hidden', !state.busy); $('prompt').disabled = state.busy;
  if (nearBottom) $('conversation').scrollTop = $('conversation').scrollHeight; else $('conversation').scrollTop = previousScroll;
}
function renderAttachments() {
  $('attachment-chips').replaceChildren();
  state.attachments.forEach((a, index) => { const chip = el('span', 'chip', (a.root === 'context' ? '参考 · ' : '') + a.path); const b = el('button', '', '×'); b.setAttribute('aria-label', '移除附件 ' + a.path); b.onclick = () => { state.attachments.splice(index, 1); renderAttachments(); }; chip.append(b); $('attachment-chips').append(chip); });
}
async function loadFiles() {
  const files = await api('/files?root=' + state.root + '&path=' + encodeURIComponent(state.dir));
  $('file-path').textContent = '/' + state.root + (state.dir === '.' ? '' : '/' + state.dir); $('file-path').title = $('file-path').textContent;
  $('new-file').disabled = state.root === 'context'; $('files').replaceChildren();
  if (!files.length) $('files').append(el('p', 'muted', '目录为空'));
  files.forEach(file => { const b = el('button', 'file-item'); b.append(el('span', 'file-icon', file.dir ? '▱' : '≡'), el('span', 'file-name', file.name)); if (file.dir) b.append(el('small', '', '›')); b.title = file.path; b.onclick = action(async () => { if (file.dir) { state.dir = file.path; await loadFiles(); } else await openFile(file.path); }); $('files').append(b); });
}
async function openFile(path) {
  const data = await api('/file?root=' + state.root + '&path=' + encodeURIComponent(path)); state.file = { ...data, path, root: state.root, fresh: false }; showEditor();
}
function showEditor() {
  $('editor-title').textContent = state.file.path; $('editor').value = state.file.content; $('editor').readOnly = state.file.root === 'context'; $('save-file').disabled = state.file.root === 'context'; $('attach-file').disabled = state.file.fresh;
  $('editor-status').textContent = state.file.root === 'context' ? '辅助目录 · 只读' : '工作目录 · 保存后同步到主机'; $('editor-dialog').showModal();
}
$('new-session').onclick = action(newSession); $('refresh-sessions').onclick = action(loadSessions); $('refresh-files').onclick = action(loadFiles);
document.querySelectorAll('.mode-switch button').forEach(b => b.onclick = () => setMode(b.dataset.mode));
document.querySelectorAll('.starter').forEach(b => b.onclick = () => { $('prompt').value = b.dataset.prompt; setMode(b.dataset.mode || 'chat'); $('prompt').focus(); });
document.querySelectorAll('[data-close]').forEach(b => b.onclick = () => $(b.dataset.close).close());
document.querySelectorAll('[data-root]').forEach(b => b.onclick = action(async () => { state.root = b.dataset.root; state.dir = '.'; document.querySelectorAll('[data-root]').forEach(x => x.classList.toggle('active', x === b)); await loadFiles(); }));
$('files-toggle').onclick = () => { if (innerWidth <= 950) $('file-panel').classList.toggle('mobile-open'); else document.body.classList.toggle('files-hidden'); };
$('parent-dir').onclick = action(async () => { state.dir = state.dir.includes('/') ? state.dir.slice(0, state.dir.lastIndexOf('/')) : '.'; await loadFiles(); });
$('task-form').onsubmit = action(async event => {
  event.preventDefault(); const prompt = $('prompt').value.trim(); if (!prompt || state.busy) return;
  if (!state.config?.configured) { openSettings(); return; }
  $('send').disabled = true;
  try {
    if (!state.session) state.session = await api('/sessions', { method: 'POST', body: JSON.stringify({ title: '新会话' }) });
    const strategy = state.profiles?.strategy || 'manual';
    await api(`/sessions/${state.session.id}/runs`, { method: 'POST', body: JSON.stringify({ prompt, mode: state.mode, attachments: state.attachments, strategy, profile: strategy === 'auto' ? '' : (state.profiles?.activeProfile || 'default') }) });
    $('prompt').value = ''; state.attachments = []; renderAttachments(); await selectSession(state.session.id); $('conversation').scrollTop = $('conversation').scrollHeight;
  } finally { $('send').disabled = false; }
});
$('prompt').addEventListener('keydown', event => { if (event.key === 'Enter' && !event.shiftKey && !event.isComposing) { event.preventDefault(); $('task-form').requestSubmit(); } });
$('cancel').onclick = action(async () => { const run = state.session?.runs.find(r => r.status === 'running'); if (run) { await api(`/sessions/${state.session.id}/runs/${run.id}/cancel`, { method: 'POST', body: '{}' }); toast('已请求停止'); } });
function openSettings() { $('base-url').value = state.config?.baseURL || 'https://api.deepseek.com'; $('model').value = state.config?.model || ''; $('api-key').value = ''; $('api-key').placeholder = state.config?.hasKey ? '已保存密钥；留空保留' : '云端 API 通常需要密钥；本地模型可不填'; $('clear-key').checked = false; $('settings-dialog').showModal(); }
$('settings-button').onclick = openSettings;
$('settings-form').onsubmit = action(async event => { event.preventDefault(); await api('/settings', { method: 'PUT', body: JSON.stringify({ baseURL: $('base-url').value.trim(), model: $('model').value.trim(), apiKey: $('api-key').value.trim(), clearKey: $('clear-key').checked }) }); $('api-key').value = ''; $('settings-dialog').close(); await refreshConfig(); toast('模型设置已保存，发送任务时会调用该模型'); });
$('save-file').onclick = action(async () => { const data = await api('/file', { method: 'PUT', body: JSON.stringify({ path: state.file.path, content: $('editor').value, hash: state.file.hash }) }); state.file.hash = data.hash; state.file.content = $('editor').value; state.file.fresh = false; $('attach-file').disabled = false; $('editor-status').textContent = '✓ 已保存到本地工作目录'; await loadFiles(); });
$('attach-file').onclick = () => {
  if (state.file.content !== $('editor').value) { toast('请先保存修改，再附加到任务'); return; }
  if (!state.attachments.some(a => a.root === state.file.root && a.path === state.file.path)) { if (state.attachments.length >= 8) { toast('最多附加 8 个文件'); return; } state.attachments.push({ root: state.file.root, path: state.file.path }); }
  renderAttachments(); $('editor-dialog').close(); $('prompt').focus();
};
$('new-file').onclick = () => { $('new-file-path').value = state.dir === '.' ? '' : state.dir + '/'; $('new-file-dialog').showModal(); };
$('new-file-form').onsubmit = action(async event => { event.preventDefault(); state.file = { path: $('new-file-path').value.trim(), root: 'workspace', hash: '', content: '', fresh: true }; $('new-file-dialog').close(); showEditor(); });
$('terminal-toggle').onclick = () => { const hidden = $('terminal-body').classList.toggle('hidden'); $('terminal-state').textContent = hidden ? '展开 ＋' : '收起 −'; };
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
        $('terminal-output').textContent += item.type === 'output' ? item.text : `\n[退出码 ${item.code} · ${item.elapsedMS} ms] ${item.error || ''}\n`;
        if ($('terminal-output').textContent.length > 180000) $('terminal-output').textContent = $('terminal-output').textContent.slice(-160000);
        $('terminal-output').scrollTop = $('terminal-output').scrollHeight;
      }
    }
  } catch (error) { if (error.name === 'AbortError') $('terminal-output').textContent += '\n[已停止命令]\n'; else throw error; }
  finally { state.commandAbort = null; $('command-run').disabled = false; $('command-stop').classList.add('hidden'); }
});
$('command-stop').onclick = () => state.commandAbort?.abort();
$('login-dialog').addEventListener('cancel', event => event.preventDefault());
$('login-form').onsubmit = async event => { event.preventDefault(); state.token = $('access-token').value.trim(); try { await initialize(); localStorage.setItem('aide-token', state.token); $('access-token').value = ''; $('login-dialog').close(); } catch (error) { $('login-error').textContent = error.message; } };
document.addEventListener('keydown', event => { if (event.key.toLowerCase() === 'n' && !event.metaKey && !event.ctrlKey && !['INPUT', 'TEXTAREA'].includes(document.activeElement.tagName) && !document.querySelector('dialog[open]') && !$('settings-sheet').classList.contains('open')) action(newSession)(); });
/* ── 设置面板（FR-58~FR-60）：品牌 logo 入口；结构由 /settings-schema.json 数据驱动；
   设置值一律经 window.aideUI 的 JSON 文档管理。必须位于 initialize() 之外：
   未登录时 initialize() 会抛错返回，设置面板仍需可用。 ── */
const settingsPanel = { schema: null, rendered: false, trigger: null, refreshers: [] };
async function loadSettingsSchema() {
  if (!settingsPanel.schema) {
    const response = await fetch('/settings-schema.json', { cache: 'no-store' });
    if (!response.ok) throw new Error('设置面板定义加载失败');
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
    const active = buttons.find(b => b.dataset.value === current) || buttons[0];
    buttons.forEach(b => b.setAttribute('aria-pressed', b === active ? 'true' : 'false'));
    const match = (control.options || []).find(o => o.value === current);
    value.textContent = match ? match.label : (current || '');
    thumb.style.width = active.offsetWidth + 'px';
    thumb.style.transform = 'translateX(' + active.offsetLeft + 'px)';
  };
  track.addEventListener('click', event => {
    const b = event.target.closest('button[data-value]');
    if (b && window.aideUI) window.aideUI.set(control.id, b.dataset.value);
  });
  if (window.aideUI) { window.aideUI.subscribe(apply); window.addEventListener('resize', apply); settingsPanel.refreshers.push(apply); }
  track.append(thumb, ...buttons);
  wrap.append(head, track);
  requestAnimationFrame(apply);
  return wrap;
}
const controlRenderers = { segmented: renderSegmentedControl, 'profiles-manager': renderProfilesManager };
function renderSettingsSheet() {
  const host = $('settings-sections');
  host.replaceChildren();
  for (const section of settingsPanel.schema?.sections || []) {
    const box = el('section', 'settings-section');
    box.append(el('h3', '', section.title));
    if (section.description) box.append(el('p', 'section-desc', section.description));
    for (const control of section.controls || []) {
      const renderer = controlRenderers[control.type];
      if (renderer) box.append(renderer(control));
    }
    host.append(box);
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
  await loadSettingsSchema();
  if (!settingsPanel.rendered) renderSettingsSheet();
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
$('settings-backdrop').onclick = closeSettingsSheet;
document.addEventListener('keydown', event => { if (event.key === 'Escape' && $('settings-sheet').classList.contains('open') && !document.querySelector('dialog[open]')) closeSettingsSheet(); });
/* ── 模型参数 Profile 与策略路由（FR-61~FR-64）：数据经 GET/PUT /api/profiles，
   持久化于工程目录 profiles.json；聊天栏策略按钮可选 auto 或手动 profile。 ── */
async function loadProfiles() { state.profiles = await api('/profiles'); refreshStrategyUI(); }
function profileName(id) { return state.profiles?.profiles.find(p => p.id === id)?.name || id; }
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
    head.append(el('span', 'profile-name', profile.name), el('span', 'profile-badge', '🔒 系统配置 · 不可修改'));
  } else {
    const nameInput = el('input', 'profile-name-input');
    nameInput.value = profile.name || ''; nameInput.maxLength = 32; nameInput.setAttribute('aria-label', '配置名称');
    nameInput.addEventListener('input', () => { profile.name = nameInput.value.trim(); this.scheduleSave(); });
    head.append(nameInput, el('span', 'profile-badge', profile.id));
    const del = el('button', 'profile-delete', '－');
    del.type = 'button'; del.title = '删除配置'; del.setAttribute('aria-label', '删除配置 ' + profile.name);
    del.onclick = () => { if (confirm(`删除配置「${profile.name}」？`)) { const index = this.local.profiles.indexOf(profile); if (index >= 0) this.local.profiles.splice(index, 1); this.render(); this.save(); } };
    head.append(del);
  }
  const grid = el('div', 'profile-params');
  for (const def of paramDefs) {
    const label = el('label', 'param-field');
    label.append(el('span', '', def.label));
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
  rfLabel.append(el('span', '', '输出格式'));
  const select = el('select');
  for (const v of ['text', 'json_object']) { const o = el('option', '', v === 'text' ? '文本 text' : 'JSON 对象 json_object'); o.value = v; select.append(o); }
  select.value = profile.params.response_format || 'text';
  select.disabled = isSystem;
  select.addEventListener('change', () => { profile.params.response_format = select.value; this.scheduleSave(); });
  rfLabel.append(select);
  grid.append(rfLabel);
  const stopLabel = el('label', 'param-field wide');
  stopLabel.append(el('span', '', '停止词 stop（逗号分隔）'));
  const stopInput = el('input');
  stopInput.type = 'text'; stopInput.placeholder = '无'; stopInput.disabled = isSystem;
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
  const add = el('button', 'profiles-add', '＋ 新建配置');
  add.type = 'button';
  add.onclick = action(async () => {
    await profilesManager.load();
    const def = (state.profiles.profiles.find(p => p.id === 'default') || {}).params || {};
    profilesManager.local.profiles.push({ id: 'u-' + Math.random().toString(36).slice(2, 8), name: '自定义配置', params: JSON.parse(JSON.stringify(def)) });
    profilesManager.render();
    await profilesManager.save();
    toast('已添加配置，可修改名称与参数');
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
  const label = p.strategy === 'auto' ? '策略 · 自动' : '策略 · ' + profileName(p.activeProfile);
  $('strategy-label').textContent = label;
  const menu = $('strategy-menu');
  menu.replaceChildren();
  menu.append(strategyMenuOption('auto', '', '自动路由', '按 routing-policy.json 规则匹配', p.strategy === 'auto'));
  menu.append(el('div', 'strategy-menu-sep', '手动'));
  for (const profile of p.profiles) {
    const selected = p.strategy === 'manual' && p.activeProfile === profile.id;
    menu.append(strategyMenuOption('profile', profile.id, profile.name, profile.system ? '系统配置' : '自定义配置', selected));
  }
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
    if (kind === 'auto') source.strategy = 'auto';
    else { source.strategy = 'manual'; source.activeProfile = value; }
    await saveProfilesFrom(source);
    closeStrategyMenu();
    toast(kind === 'auto' ? '已切换为自动路由策略' : '已切换为手动策略 · ' + profileName(value));
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
async function initialize() { await refreshConfig(); await Promise.all([loadSessions(), loadFiles(), loadProfiles()]); }
initialize().catch(error => { if (!$('login-dialog').open) $('login-dialog').showModal(); $('login-error').textContent = state.token ? error.message : ''; });
