'use strict';
const $ = id => document.getElementById(id);
const state = { token: localStorage.getItem('aide-token') || '', session: null, mode: 'chat', root: 'workspace', dir: '.', attachments: [], file: null, busy: false, poll: null, config: null, commandAbort: null, profiles: null, modelDraft: null, plugins: [], panel: 'files', sources: [], source: '' };
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
  estimateContext();
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
  $('session-title').textContent = state.session?.title || '开始新的探索';
  $('welcome').classList.toggle('hidden', !!state.session?.runs.length);
  $('timeline').replaceChildren(); state.busy = false;
  for (const run of state.session?.runs || []) {
    if (run.status === 'running') state.busy = true;
    const box = el('article', 'run'); box.append(el('div', 'user-message', run.prompt));
    const meta = el('div', 'run-meta'); meta.append(el('span', '', run.mode === 'workflow' ? '◈ AIDE WORKFLOW · 规划 → 方案 → 审查' : '◌ AIDE ASSISTANT'), el('span', 'run-model', run.model || ''), el('span', 'run-status', statuses[run.status] || run.status)); if (run.strategy) meta.append(el('span', 'run-strategy', '策略: ' + (run.strategy === 'auto' ? '自动 → ' + profileName(run.profile) : '手动 · ' + profileName(run.profile)))); box.append(meta);
    if (run.attachments?.length) box.append(el('p', 'muted', '已附加：' + run.attachments.map(a => a.root + '/' + a.path).join('、')));
    if (!run.steps.length) box.append(el('p', 'muted', '正在准备模型请求…'));
    run.steps.forEach((step, index) => {
      if (run.mode === 'chat') {
        const ans = el('div', 'chat-answer md-body');
        ans.innerHTML = renderMarkdown(step.content || (step.status === 'running' ? '正在思考…' : '未返回回答'));
        box.append(ans);
        return;
      }
      const details = el('details', 'step'); details.dataset.key = run.id + ':' + step.name;
      details.open = openDetails.has(details.dataset.key) || (index === run.steps.length - 1 && step.name !== 'propose');
      const summary = el('summary', '', labels[step.name]); summary.append(el('span', '', statuses[step.status]));
      if (step.name === 'propose') {
        details.append(summary, el('pre', 'step-content', step.content || '正在调用模型…'));
      } else {
        const md = el('div', 'step-content md-body');
        md.innerHTML = renderMarkdown(step.content || '正在调用模型…');
        details.append(summary, md);
      }
      box.append(details);
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
    if (run.toolUses?.length) {
      // 文件类调用合并展示（只显示路径，轻量）；命令与其他工具单独折叠显示细节
      const fileUses = run.toolUses.filter(u => u.tool === 'read_file' || u.tool === 'list_files');
      const otherUses = run.toolUses.filter(u => u.tool !== 'read_file' && u.tool !== 'list_files');
      if (fileUses.length) {
        const details = el('details', 'tool-use');
        details.dataset.key = run.id + ':files';
        const summary = el('summary', '', '⚒ 文件查看 · ' + fileUses.length + ' 次');
        const paths = fileUses.map(u => { try { return JSON.parse(u.args || '{}').path || '.'; } catch (e) { return '.'; } }).join('\n');
        details.append(summary, el('pre', 'tool-use-detail', paths));
        box.append(details);
      }
      otherUses.forEach((use, toolIndex) => {
        const details = el('details', 'tool-use');
        details.dataset.key = run.id + ':tool:' + toolIndex;
        details.open = false;
        const isCommand = use.tool === 'run_shell';
        const summary = el('summary', '', isCommand ? '⚒ 建议命令' : '⚒ 工具调用 · ' + use.tool);
        summary.append(el('span', '', toolSummaryBrief(use)));
        const detail = isCommand
          ? '命令：\n' + toolSummaryBrief(use) + '\n\n结果：\n' + (use.result || '（无）')
          : '参数：' + (use.args || '无') + '\n\n结果：\n' + (use.result || '（无）');
        details.append(summary, el('pre', 'tool-use-detail', detail));
        box.append(details);
      });
    }
    if (run.error) box.append(el('p', 'task-error', run.error)); $('timeline').append(box);
  }
  $('send').classList.toggle('hidden', state.busy); $('cancel').classList.toggle('hidden', !state.busy); $('prompt').disabled = state.busy;
  if (nearBottom) $('conversation').scrollTop = $('conversation').scrollHeight; else $('conversation').scrollTop = previousScroll;
  estimateContext();
}
function renderAttachments() {
  $('attachment-chips').replaceChildren();
  state.attachments.forEach((a, index) => { const chip = el('span', 'chip', (a.root === 'context' ? '参考 · ' : '') + a.path); const b = el('button', '', '×'); b.setAttribute('aria-label', '移除附件 ' + a.path); b.onclick = () => { state.attachments.splice(index, 1); renderAttachments(); }; chip.append(b); $('attachment-chips').append(chip); });
}
async function loadFiles() {
  const query = state.root === 'context' && state.source ? '/files?source=' + encodeURIComponent(state.source) + '&path=' : '/files?root=' + state.root + '&path=';
  const files = await api(query + encodeURIComponent(state.dir));
  const label = state.root === 'context' && state.source ? 'sources/' + (state.sources.find(x => x.id === state.source)?.name || state.source) : state.root;
  $('file-path').textContent = '/' + label + (state.dir === '.' ? '' : '/' + state.dir); $('file-path').title = $('file-path').textContent;
  $('new-file').disabled = state.root === 'context'; $('files').replaceChildren();
  if (!files.length) $('files').append(el('p', 'muted', '目录为空'));
  files.forEach(file => { const b = el('button', 'file-item'); b.append(el('span', 'file-icon', file.dir ? '▱' : '≡'), el('span', 'file-name', file.name)); if (file.dir) b.append(el('small', '', '›')); b.title = file.path; b.onclick = action(async () => { if (file.dir) { state.dir = file.path; await loadFiles(); } else await openFile(file.path); }); $('files').append(b); });
}
async function openFile(path) {
  const query = state.root === 'context' && state.source ? '/file?source=' + encodeURIComponent(state.source) + '&path=' : '/file?root=' + state.root + '&path=';
  const data = await api(query + encodeURIComponent(path)); state.file = { ...data, path, root: state.root, source: state.root === 'context' ? state.source : '', fresh: false }; showEditor();
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
  $('editor-status').textContent = state.file.root === 'context' ? (sourceIsRW() ? '辅助资料 · 读写来源' : '辅助资料 · 只读') : '工作目录 · 保存后同步到主机';
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
$('files-toggle').onclick = () => { document.body.classList.remove('plugins-mode'); state.panel = 'files'; $('plugins-toggle').classList.remove('active'); $('plugins-toggle').setAttribute('aria-pressed', 'false'); $('files-toggle').classList.add('active'); $('files-toggle').setAttribute('aria-pressed', 'true'); if (innerWidth <= 950) $('file-panel').classList.toggle('mobile-open'); else document.body.classList.toggle('files-hidden'); };
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
function openSettings() { $('base-url').value = state.config?.baseURL || 'https://api.deepseek.com'; $('api-key').value = ''; $('api-key').placeholder = state.config?.hasKey ? '已保存密钥；留空保留' : '云端 API 通常需要密钥；本地模型可不填'; $('clear-key').checked = false; state.modelDraft = { models: JSON.parse(JSON.stringify(state.config?.models || [])), activeModel: state.config?.activeModel || '' }; renderModelList(); $('settings-dialog').showModal(); }
$('settings-button').onclick = openSettings;
$('settings-form').onsubmit = action(async event => { event.preventDefault(); if (!state.modelDraft.models.length) { toast('请至少添加一个模型'); return; } await api('/settings', { method: 'PUT', body: JSON.stringify({ baseURL: $('base-url').value.trim(), apiKey: $('api-key').value.trim(), clearKey: $('clear-key').checked, models: state.modelDraft.models, activeModel: state.modelDraft.activeModel }) }); $('api-key').value = ''; $('settings-dialog').close(); await refreshConfig(); toast('模型设置已保存，发送任务时会调用当前模型'); });
$('save-file').onclick = action(async () => { const body = { path: state.file.path, content: $('editor').value, hash: state.file.hash }; if (state.file.source) body.source = state.file.source; const data = await api('/file', { method: 'PUT', body: JSON.stringify(body) }); state.file.hash = data.hash; state.file.content = $('editor').value; state.file.fresh = false; $('attach-file').disabled = false; $('editor-status').textContent = '✓ 已保存'; await loadFiles(); });
$('attach-file').onclick = () => {
  if (state.file.content !== $('editor').value) { toast('请先保存修改，再附加到任务'); return; }
  const att = { root: state.file.root, path: state.file.path }; if (state.file.source) { att.root = 'source'; att.source = state.file.source; } if (!state.attachments.some(a => a.root === att.root && a.path === att.path && (a.source || '') === (att.source || ''))) { if (state.attachments.length >= 8) { toast('最多附加 8 个文件'); return; } state.attachments.push(att); }
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
const controlRenderers = { segmented: renderSegmentedControl, 'profiles-manager': renderProfilesManager, 'token-stats': renderTokenStats };
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
$('settings-backdrop').onclick = () => { if ($('workspace-sheet').classList.contains('open')) closeWorkspaceSheet(); else closeSettingsSheet(); };
document.addEventListener('keydown', event => { if (event.key === 'Escape' && !document.querySelector('dialog[open]')) { if ($('workspace-sheet').classList.contains('open')) closeWorkspaceSheet(); else if ($('settings-sheet').classList.contains('open')) closeSettingsSheet(); } });
/* ── 模型参数 Profile 与策略路由（FR-61~FR-64）：数据经 GET/PUT /api/profiles，
   持久化于工程目录 profiles.json；聊天栏策略按钮可选 auto 或手动 profile。 ── */
async function loadProfiles() { state.profiles = await api('/profiles'); refreshStrategyUI(); }
function profileName(id) { return state.profiles?.profiles.find(p => p.id === id)?.name || id; }
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
  const modelName = state.config?.models?.find(m => m.id === state.config.activeModel)?.name || state.config?.model || '';
  const label = (p.strategy === 'auto' ? '策略 · 自动' : '策略 · ' + profileName(p.activeProfile)) + (modelName ? ' · ' + modelName : '');
  $('strategy-label').textContent = label;
  $('strategy-label').title = label;
  const menu = $('strategy-menu');
  menu.replaceChildren();
  // 左栏：策略；右栏：模型（各自独立滚动，互不挤压）
  const left = el('div', 'strategy-menu-col');
  left.append(el('div', 'strategy-menu-sep', '策略'));
  left.append(strategyMenuOption('auto', '', '自动路由', '按 routing-policy.json 规则匹配', p.strategy === 'auto'));
  left.append(el('div', 'strategy-menu-sep', '手动'));
  for (const profile of p.profiles) {
    const selected = p.strategy === 'manual' && p.activeProfile === profile.id;
    left.append(strategyMenuOption('profile', profile.id, profile.name, profile.system ? '系统配置' : '自定义配置', selected));
  }
  const right = el('div', 'strategy-menu-col');
  right.append(el('div', 'strategy-menu-sep', '模型'));
  for (const m of state.config?.models || []) {
    const selected = state.config.activeModel === m.id;
    right.append(strategyMenuOption('model', m.id, m.name, m.id + ' · ' + (m.contextWindow || 65536) / 1024 + 'K 上下文', selected));
  }
  const manageModels = el('button', 'model-picker-manage', '⚙ 管理模型…');
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
      toast('已切换模型：' + modelName);
      return;
    }
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
/* ── Token 消耗统计（FR-90）：git 提交热力图样式 ── */
function fmtStatTokens(n) { return n < 1000 ? String(n) : (n / 1000).toFixed(1) + 'K'; }
function renderTokenStats(control) {
  const wrap = el('div', 'settings-control token-stats');
  // 折叠菜单形式：收起为一行摘要，展开显示热力图卡片
  const menu = el('details', 'token-menu');
  const summary = el('summary', '');
  summary.append(el('span', '', control.label), el('span', 'control-value', '加载中…'));
  menu.append(summary);
  const card = el('div', 'token-card');
  const totals = el('p', 'token-totals', '加载中…');
  const grid = el('div', 'token-heatmap');
  grid.setAttribute('role', 'img');
  grid.setAttribute('aria-label', 'Token 消耗热力图');
  const legend = el('div', 'token-legend');
  card.append(totals, grid, legend);
  menu.append(card);
  wrap.append(menu);
  action(async () => {
    const data = await api('/token-stats');
    const days = data.days || {};
    const totalsObj = data.totals || {};
    summary.querySelector('.control-value').textContent = fmtStatTokens(totalsObj.total || 0) + ' tokens · 今日 ' + fmtStatTokens((data.today || {}).total || 0);
    totals.textContent = '今日 ' + fmtStatTokens((data.today || {}).total || 0) + ' · 累计 ' + fmtStatTokens(totalsObj.total || 0) + ' · ' + (totalsObj.calls || 0) + ' 次调用' + (totalsObj.estimated ? ' · 含估算' : '');
    grid.replaceChildren();
    // 最近 16 周（112 天），列=周、行=星期（周一~周日）
    const today = new Date();
    const start = new Date(today);
    start.setDate(start.getDate() - 111);
    start.setDate(start.getDate() - ((start.getDay() + 6) % 7)); // 对齐到周一
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
    for (const col of cols) {
      const colEl = el('div', 'token-week');
      for (const d of col) {
        const key = d.toISOString().slice(0, 10);
        const day = days[key] || {};
        const cell = el('span', 'token-cell tk-' + level(day.total || 0));
        cell.title = key + ' · ' + fmtStatTokens(day.total || 0) + ' tokens · ' + (day.calls || 0) + ' 次调用' + (day.estimated ? '（估算）' : '');
        if (d > today) cell.classList.add('future');
        colEl.append(cell);
      }
      grid.append(colEl);
    }
    legend.replaceChildren();
    legend.append(el('span', '', '少'));
    for (let i = 1; i <= 4; i++) { const c = el('span', 'token-cell tk-' + i); legend.append(c); }
    legend.append(el('span', '', '多'));
    if (totalsObj.estimated) legend.append(el('small', '', '· 上游未返回 usage 时按 4 字符/词估算'));
  })();
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
    radio.title = '设为当前模型';
    radio.setAttribute('aria-pressed', String(m.id === state.modelDraft.activeModel));
    radio.textContent = m.id === state.modelDraft.activeModel ? '●' : '○';
    radio.onclick = () => { state.modelDraft.activeModel = m.id; renderModelList(); };
    const nameInput = el('input', 'model-name-input');
    nameInput.value = m.name || m.id;
    nameInput.maxLength = 32;
    nameInput.setAttribute('aria-label', '模型名称');
    nameInput.addEventListener('input', () => { m.name = nameInput.value.trim() || m.id; });
    const idText = el('span', 'model-id-text', m.id);
    const windowLabel = el('label', 'model-window-label', '窗口');
    const windowInput = el('input', 'model-window-input');
    windowInput.type = 'number';
    windowInput.min = 1024;
    windowInput.max = 1048576;
    windowInput.step = 1024;
    windowInput.value = m.contextWindow || 65536;
    windowInput.setAttribute('aria-label', '上下文窗口');
    windowInput.addEventListener('input', () => { const v = parseInt(windowInput.value, 10); if (!Number.isNaN(v)) m.contextWindow = v; });
    windowLabel.append(windowInput);
    const del = el('button', 'model-delete', '－');
    del.type = 'button';
    del.title = '删除模型';
    del.onclick = () => {
      const index = state.modelDraft.models.indexOf(m);
      if (index >= 0) state.modelDraft.models.splice(index, 1);
      if (state.modelDraft.activeModel === m.id) state.modelDraft.activeModel = state.modelDraft.models[0]?.id || '';
      renderModelList();
    };
    row.append(radio, nameInput, idText, windowLabel, del);
    host.append(row);
  }
  if (!(state.modelDraft?.models || []).length) host.append(el('p', 'muted', '尚未添加模型。可输入模型 ID 添加，或用「自动获取」从 API 拉取候选。'));
}
$('add-model').onclick = () => {
  const id = $('new-model-id').value.trim();
  if (!id) { toast('请输入模型 ID'); return; }
  if (state.modelDraft.models.some(m => m.id === id)) { toast('该模型已存在'); return; }
  state.modelDraft.models.push({ id, name: id, contextWindow: 65536 });
  if (!state.modelDraft.activeModel) state.modelDraft.activeModel = id;
  $('new-model-id').value = '';
  renderModelList();
};
$('fetch-models').onclick = action(async () => {
  const button = $('fetch-models');
  button.disabled = true;
  button.textContent = '⟳ 获取中…';
  try {
    const data = await api('/models');
    const list = $('model-datalist');
    list.replaceChildren();
    (data.models || []).forEach(id => list.append(new Option(id, id)));
    toast('已获取 ' + (data.models || []).length + ' 个可用模型，在输入框中选择即可');
  } finally {
    button.disabled = false;
    button.textContent = '⟳ 自动获取';
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
  $('context-card').title = (included ? '最近 ' + included + ' 条消息' : '当前会话暂无内容') + ' · 4 字符/词估算 · tokens 已用/窗口';
}
/* ── 插件系统（FR-72~75，协议 doc/plugin-protocol.md）：右侧面板 + 上传/搜索/启停/删除/surface ── */
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
  if (!matched.length) { host.append(el('p', 'muted', query ? '没有匹配的插件' : '还没有插件。点击「＋ 上传」添加（协议 v1，DSH 形态）。')); return; }
  for (const p of matched) {
    const card = el('div', 'plugin-card' + (p.enabled ? '' : ' disabled'));
    const head = el('div', 'plugin-card-head');
    head.append(el('span', 'plugin-name', p.name), el('span', 'plugin-badge', p.id + (p.version ? ' · v' + p.version : '')));
    const del = el('button', 'plugin-delete', '－');
    del.type = 'button'; del.title = '删除插件';
    del.onclick = () => { if (confirm(`删除插件「${p.name}」？`)) action(async () => { await api('/plugins/' + encodeURIComponent(p.id), { method: 'DELETE' }); await loadPluginsPanel(); toast('插件已删除'); })(); };
    head.append(del);
    card.append(head);
    if (p.description) card.append(el('p', 'plugin-desc', p.description));
    if (p.error) card.append(el('p', 'task-error', '⚠ ' + p.error));
    const foot = el('div', 'plugin-card-foot');
    const toggle = el('button', 'plugin-toggle' + (p.enabled ? ' on' : ''), p.enabled ? '✓ 使用中' : '停用');
    toggle.type = 'button';
    toggle.onclick = action(async () => {
      await api('/plugins/' + encodeURIComponent(p.id), { method: 'PUT', body: JSON.stringify({ enabled: !p.enabled }) });
      await loadPluginsPanel();
      toast(p.enabled ? '已停用插件：' + p.name : '已启用插件：' + p.name);
    });
    foot.append(el('small', '', p.enabled ? '启用' : '停用'), toggle);
    card.append(foot);
    host.append(card);
  }
}
function renderPluginSurface(entries) {
  const host = $('plugin-surface');
  host.replaceChildren();
  const active = entries.filter(e => !e.error);
  if (!entries.length) { host.append(el('p', 'muted', '暂无启用的插件。')); return; }
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
  document.body.classList.add('plugins-mode');
  state.panel = 'plugins';
  $('plugins-toggle').classList.add('active');
  $('plugins-toggle').setAttribute('aria-pressed', 'true');
  $('files-toggle').classList.remove('active');
  $('files-toggle').setAttribute('aria-pressed', 'false');
  if (innerWidth <= 950) $('file-panel').classList.remove('mobile-open');
  await loadPluginsPanel();
});
$('refresh-plugins').onclick = action(loadPluginsPanel);
$('plugin-search').addEventListener('input', renderPluginList);
$('plugin-upload').onclick = () => { $('plugin-upload-form').reset(); $('plugin-file-name').textContent = ''; $('plugin-upload-dialog').showModal(); };
$('plugin-file-pick').addEventListener('change', () => { $('plugin-file-name').textContent = $('plugin-file-pick').files[0] ? '已选择：' + $('plugin-file-pick').files[0].name : ''; });
$('plugin-upload-form').onsubmit = action(async event => {
  event.preventDefault();
  const file = $('plugin-file-pick').files[0];
  if (!file) { toast('请选择插件文件'); return; }
  if (file.size > 256 * 1024) { toast('插件文件超过 256 KiB 限制'); return; }
  const code = await new Promise((resolve, reject) => { const reader = new FileReader(); reader.onload = () => resolve(reader.result); reader.onerror = () => reject(new Error('读取文件失败')); reader.readAsText(file); });
  await api('/plugins', { method: 'POST', body: JSON.stringify({ name: $('plugin-name').value.trim() || file.name.replace(/\.js$/, ''), description: $('plugin-desc').value.trim(), code }) });
  $('plugin-upload-dialog').close();
  await loadPluginsPanel();
  toast('插件已上传并启用');
});

/* ── 工作空间配置（FR-76~80）：点侧栏工作空间卡片弹出；本地/SSH·SFTP、文档、缓存、最近路径 ── */
const wsState = { config: null, browse: { field: '', root: 'workspace', dir: '.' } };
async function loadWorkspaceConfig() { wsState.config = await api('/workspace-config'); renderWorkspaceSummary(); }
function renderWorkspaceSummary() {
  const w = wsState.config?.workspace || {};
  $('workspace-summary').textContent = w.mode === 'ssh' ? (w.host || '远程') + ' · SSH/SFTP' : (state.config?.workspaceDisplay || '/workspace') + ' · 本地';
  $('command-mode').textContent = w.mode === 'ssh' ? 'SSH · ' + (w.host || '未配置主机') : '本地';
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
  if (!list.length) { host.append(el('p', 'muted', '暂无最近路径')); return; }
  list.forEach(value => {
    const chip = el('button', 'ws-recent-chip', value);
    chip.type = 'button';
    chip.title = '点击挂载：' + value;
    chip.onclick = action(async () => {
      const field = id === 'ws-recent' ? 'ws-path' : id === 'docs-recent' ? 'docs-path' : 'cache-path';
      $(field).value = value;
      await saveWorkspaceConfig();
      toast('已挂载路径：' + value);
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
  $('ws-password').placeholder = c.hasPassword ? '已保存密码；留空保留' : '设置远程密码';
  $('ws-key').placeholder = c.hasKey ? '已保存私钥；留空保留' : '粘贴私钥内容';
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
  await loadFiles();
}
function openWorkspaceSheet() {
  closeSettingsSheet();
  fillWorkspaceSheet();
  $('workspace-sheet').classList.add('open');
  $('settings-backdrop').classList.add('open');
}
function closeWorkspaceSheet() {
  $('workspace-sheet').classList.remove('open');
  $('settings-backdrop').classList.remove('open');
}
$('workspace-config-button').onclick = () => { action(async () => { if (!wsState.config) await loadWorkspaceConfig(); openWorkspaceSheet(); })(); };
$('workspace-sheet-close').onclick = closeWorkspaceSheet;
$('ws-save').onclick = action(async () => { await saveWorkspaceConfig(); closeWorkspaceSheet(); toast('工作空间配置已保存'); });
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
  if (!value) return '.';
  const base = state.config?.hostLocal || '/local';
  if (value.startsWith(base)) {
    const rel = value.slice(base.length).replace(/^\/+/, '');
    return rel || '.';
  }
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
  $('ws-browse-path').textContent = hostPathOf(b.dir);
  const list = $('ws-browse-list');
  list.replaceChildren();
  const dirs = files.filter(f => f.dir);
  if (!dirs.length) list.append(el('p', 'muted', '没有子目录'));
  dirs.forEach(d => { const row = el('button', 'ws-browse-item', '▱ ' + d.name); row.onclick = () => { b.dir = d.path; action(loadBrowseDir)(); }; list.append(row); });
}
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
    const chip = el('button', 'source-chip' + (state.source === src.id ? ' active' : ''), (src.rw ? '✎ ' : '') + src.name + (src.builtin ? ' 🔒' : ''));
    chip.title = src.type + (src.config.path || src.config.url || src.config.host || '') + (src.rw ? ' · 读写' : ' · 只读');
    chip.onclick = () => { state.source = src.id; state.dir = '.'; state.attachments = []; renderAttachments(); renderSourceChips(); action(loadFiles)(); };
    host.append(chip);
    if (!src.builtin) {
      const del = el('button', 'source-chip-x', '×');
      del.title = '删除来源 ' + src.name;
      del.onclick = () => { if (confirm('删除来源「' + src.name + '」？')) action(async () => { await api('/sources', { method: 'PUT', body: JSON.stringify({ sources: state.sources.filter(x => x.id !== src.id) }) }); await loadSourcesList(); if (state.source === src.id) { state.source = ''; state.dir = '.'; await loadFiles(); } })(); };
      host.append(del);
    }
  });
  const add = el('button', 'source-chip-add', '＋ 来源');
  add.onclick = () => { $('source-form').reset(); renderSourceFields(); $('source-dialog').showModal(); };
  host.append(add);
}
function renderSourceFields() {
  const type = $('src-type').value;
  const host = $('src-fields');
  host.replaceChildren();
  const addField = (labelText, id, placeholder) => { const label = el('label', '', labelText); const input = el('input', ''); input.id = id; input.placeholder = placeholder || ''; input.autocomplete = 'off'; label.append(input); host.append(label); return input; };
  if (type === 'local' || type === 'skill') addField('本机路径（绝对路径）', 'src-path', '/Users/you/…');
  else if (type === 'link' || type === 'ftp' || type === 'ftps' || type === 'smb') addField('URL（如 ftp://host/dir 或 https://…）', 'src-url', type + '://');
  else if (type === 'mcp') { addField('启动命令', 'src-command', 'npx -y @modelcontextprotocol/server-…'); addField('或 URL', 'src-url', ''); }
  else if (type === 'sftp') {
    addField('主机', 'src-host', '192.168.1.10');
    addField('端口', 'src-port', '22').type = 'number';
    addField('用户名', 'src-user', 'root');
    addField('远程目录', 'src-remote', '/srv/refs');
    addField('密码（可选）', 'src-password', '留空 = 无密码认证').type = 'password';
    addField('私钥（可选，优先于密码）', 'src-key', '粘贴私钥内容').type = 'password';
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
  else src.config.url = $('src-url').value.trim();
  const payload = { sources: [...state.sources.filter(x => !x.builtin), src] };
  if (Object.keys(secrets).length) payload.secrets = secrets;
  await api('/sources', { method: 'PUT', body: JSON.stringify(payload) });
  $('source-dialog').close();
  await loadSourcesList();
  state.source = id; state.dir = '.'; await loadFiles();
  toast('已添加来源：' + name);
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
function renderMarkdown(src) {
  if (window.marked && typeof window.marked.parse === 'function') {
    const html = window.marked.parse(String(src || ''), { gfm: true, breaks: false });
    const body = sanitizeHtml(html);
    body.querySelectorAll('pre > code[class*="language-"]').forEach(codeEl => {
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
  fileView.spec = spec;
  $('file-view-path').textContent = (spec.source ? 'sources/' + spec.source : spec.root) + ' · ' + spec.path;
  const query = spec.source
    ? '/file?source=' + encodeURIComponent(spec.source) + '&path=' + encodeURIComponent(spec.path)
    : '/file?root=' + encodeURIComponent(spec.root) + '&path=' + encodeURIComponent(spec.path);
  const data = await api(query);
  fileView.hash = data.hash;
  const md = isMarkdownPath(spec.path);
  $('file-view-mode-switch').classList.toggle('hidden', !md);
  const readOnly = spec.root !== 'workspace' && !(spec.source && state.sources.find(x => x.id === spec.source)?.rw === true);
  $('file-view-editor').value = data.content;
  $('file-view-editor').readOnly = readOnly;
  $('file-view-save').disabled = readOnly;
  $('file-view-status').textContent = readOnly ? '只读' : '可编辑 · 保存后同步';
  setFileViewMode(md ? 'preview' : 'edit'); // md 默认渲染预览
  $('file-view-toolbar').classList.remove('hidden');
  $('file-view-content').replaceChildren();
}
$('fv-edit').onclick = () => setFileViewMode('edit');
$('fv-preview').onclick = () => setFileViewMode('preview');
$('file-view-save').onclick = action(async () => {
  const body = { path: fileView.spec.path, content: $('file-view-editor').value, hash: fileView.hash };
  if (fileView.spec.source) body.source = fileView.spec.source;
  const res = await api('/file', { method: 'PUT', body: JSON.stringify(body) });
  fileView.hash = res.hash;
  $('file-view-status').textContent = '✓ 已保存';
});
async function initialize() {
  await refreshConfig();
  const fragment = new URLSearchParams(location.hash.slice(1));
  if (fragment.has('file')) { await Promise.all([loadWorkspaceConfig(), loadSourcesList()]); await openFileViewMode(); return; }
  await Promise.all([loadSessions(), loadFiles(), loadProfiles(), loadWorkspaceConfig(), loadSourcesList()]);
}
initialize().catch(error => { if (!$('login-dialog').open) $('login-dialog').showModal(); $('login-error').textContent = state.token ? error.message : ''; });
