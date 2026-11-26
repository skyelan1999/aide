/* ── 问题分析历史报告：读索引、渲染 Markdown，并校验 draw.io 图存在 ── */
const RCA_DIR = '.cache/system-docs/problem-reports';
const RCA_INDEX = RCA_DIR + '/problem-reports-index.md';
let rcaLocation = { query: 'root=workspace', dir: RCA_DIR, index: RCA_INDEX };
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
function rcaParseIndex(md: any) {
  const rows: any[] = [];
  (md || '').split('\n').forEach((line: any) => {
    const t = line.trim();
    if (!t.startsWith('|') || /^\|[\s:\-|]+\|$/.test(t) || t.indexOf('编号') >= 0) return;
    const parts = t.replace(/^\||\|$/g, '').split('|').map((c: any) => c.trim());
    if (parts.length < 1 || !/^RCA-\d+/i.test(parts[0])) return;
    rows.push({ id: parts[0], name: parts[1] || parts[0], status: parts[2] || '', created: parts[3] || '', related: parts[4] || '' });
  });
  return rows;
}
function rcaExtractDiagramPath(content: any) {
  let m = (content || '').match(/关联[：:]\s*draw\.io[：:]\s*([^\s]+)/);
  if (m) return m[1].trim();
  m = (content || '').match(/##\s*RCA\s*图\s*\n+([^\n#]+)/);
  if (m) return m[1].trim();
  return '';
}
function rcaResolveDiagram(ref: any) {
  ref = (ref || '').trim();
  if (!ref) return null;
  let drawio = ref, exportRef = null;
  if (/\.drawio\.(svg|png)$/i.test(ref)) { drawio = ref.replace(/\.(svg|png)$/i, ''); exportRef = ref; }
  return { drawio, exportRef };
}
async function rcaPathExists(path: any) {
  try { await api('/file?root=workspace&path=' + encodeURIComponent(path)); return true; }
  catch (e: any) { return false; }
}
function rcaFriendlyRender(content: any) {
  let src = String(content || '');
  const trimmed = src.trim();
  if (trimmed.startsWith('{') || trimmed.startsWith('[')) {
    try { return renderMarkdown('```json\n' + JSON.stringify(JSON.parse(trimmed), null, 2) + '\n```', false, ''); }
    catch (e: any) { /* 非合法 JSON，按 Markdown 原样渲染 */ }
  }
  return renderMarkdown(src, false, '');
}
let rcaState: any = { entries: [], active: '' };
async function openRcaReports() {
  const dlg = $('rca-dialog');
  if (typeof dlg.showModal === 'function' && !dlg.open) dlg.showModal(); else dlg.setAttribute('open', '');
  const config = await api('/workspace-config');
  const bound = !!String(config.docs?.path || '').trim();
  rcaLocation = bound ? { query: 'source=system-docs', dir: 'problem-reports', index: 'problem-reports/problem-reports-index.md' } : { query: 'root=workspace', dir: RCA_DIR, index: RCA_INDEX };
  rcaState.active = '';
  await loadRcaList();
}
async function loadRcaList() {
  const listEl = $('rca-report-list');
  listEl.innerHTML = '<div class="rca-empty">' + t('正在加载…') + '</div>';
  let indexRows: any[] = [];
  try { const d = await api('/file?' + rcaLocation.query + '&path=' + encodeURIComponent(rcaLocation.index)); indexRows = rcaParseIndex(d.content || ''); }
  catch (e: any) { indexRows = []; }
  let files: any[] = [];
  try { files = await api('/files?' + rcaLocation.query + '&path=' + encodeURIComponent(rcaLocation.dir)); }
  catch (e: any) { files = []; }
  const reportFiles = (files || []).filter((f: any) => !f.dir && /^RCA-.*\.md$/i.test(f.name) && f.name !== 'problem-reports-index.md');
  const byId: any = {};
  indexRows.forEach((r: any) => { byId[r.id.toUpperCase()] = r; });
  reportFiles.forEach((f: any) => {
    const m = f.name.match(/^(RCA-\d+)/i); if (!m) return;
    const id = m[1].toUpperCase();
    if (!byId[id]) byId[id] = { id, name: f.name.replace(/\.md$/, ''), status: '', created: '', related: '' };
    byId[id].path = f.path;
  });
  rcaState.entries = Object.keys(byId).map((k: any) => byId[k]).filter((e: any) => e.path).sort((a: any, b: any) => a.id < b.id ? -1 : (a.id > b.id ? 1 : 0));
  listEl.replaceChildren();
  if (!rcaState.entries.length) {
    listEl.innerHTML = '<div class="rca-empty">' + t('暂无问题分析报告。选择「AI 工作流 → 问题分析」并运行后，可在这里查看。') + '</div>';
    $('rca-view-title').textContent = '—'; $('rca-view-body').innerHTML = '';
    $('rca-open-diagram').classList.add('hidden'); $('rca-warn').classList.add('hidden');
    return;
  }
  rcaState.entries.forEach((e: any) => {
    const b = el('button', 'rca-item' + (rcaState.active === e.path ? ' active' : ''));
    b.type = 'button';
    b.append(el('div', 'rca-item-id', e.id));
    b.append(el('div', 'rca-item-title', e.name || e.id));
    const meta = [e.status, e.created].filter(Boolean).join(' · ');
    if (meta) b.append(el('div', 'rca-item-meta', meta));
    b.onclick = () => openRcaReport(e);
    listEl.append(b);
  });
  if (!rcaState.active || !rcaState.entries.some((e: any) => e.path === rcaState.active)) openRcaReport(rcaState.entries[0]);
}
async function openRcaReport(entry: any) {
  rcaState.active = entry.path;
  document.querySelectorAll('.rca-item').forEach((b: any) => b.classList.toggle('active', b.textContent.indexOf(entry.id) >= 0));
  $('rca-view-title').textContent = entry.id + (entry.name ? ' · ' + entry.name : '');
  const warn = $('rca-warn'); warn.classList.add('hidden'); warn.innerHTML = '';
  $('rca-open-diagram').classList.add('hidden');
  const body = $('rca-view-body');
  body.innerHTML = '<div class="rca-empty">' + t('正在加载…') + '</div>';
  let data;
  try { data = await api('/file?' + rcaLocation.query + '&path=' + encodeURIComponent(entry.path)); }
  catch (e: any) { body.innerHTML = '<div class="rca-empty">' + t('读取报告失败：{0}', e.message || entry.path) + '</div>'; return; }
  body.innerHTML = rcaFriendlyRender(data.content || '');
  // renderMarkdown 包装层已在渲染后自动触发 renderMermaid，此处无需再调用（renderMermaid 为闭包内定义）
  let ref = rcaExtractDiagramPath(data.content || '') || (entry.related || '').replace(/^draw\.io[：:]\s*/i, '');
  const diag = rcaResolveDiagram(ref);
  const warnings: any[] = [];
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
// @ts-expect-error -- independent settings pages expose their own config refresh helper.
async function refreshConfig({ modelConnectionsSaved = false } = {}) {
  const previousScope=recoveryScope(),[nextConfig,assistantModel]=await Promise.all([api('/config'),api('/xiaomi/model').catch(()=>null)]);
  const workspaceChanged=!!previousScope&&previousScope!==nextConfig.workspaceId;
  if(workspaceChanged){await persistChatDraft();recoveryReady=false;clearTimeout(recoveryTimer);++sessionSeq.value;}
  state.config = nextConfig;
  state.xiaomiModelSettings = assistantModel;
  // Keep config-backed controls current without rebuilding the whole settings
  // sheet or disturbing unrelated controls the user may be editing.
  for(const refresher of settingsPanel.refreshers)refresher.onConfig?.(nextConfig, modelConnectionsSaved);
  void window.AidePluginMarket?.initRouting?.();
  renderCommandWorkspaceStatus();
  window.AideManagement?.refresh();
  await refreshApprovalPolicy();
  const versionText = state.config.version ? 'v' + state.config.version : 'dev';
  $('app-version').textContent = versionText;
  $('settings-sheet-version').textContent = ' · aide ' + versionText;
  syncVoiceAssistantDisplayName();
  $('model-status').textContent = state.config.configured ? t("已配置") : t("未配置");
  refreshStrategyUI();
  if (typeof resetIdleTimer === "function") resetIdleTimer();
  // 左下角 Docker 块锁定蒙版：仅已设密码时启用，点击即锁屏（不依赖是否打开过设置面板）
  const rcLock = $('runtime-card');
  if (rcLock) rcLock.classList.toggle('lock-enabled', !!(state.config && state.config.hasPassword));
  const ovLock = $('runtime-lock-overlay');
  if (ovLock) {
    const enabled = !!state.config?.hasPassword;
    ovLock.tabIndex = enabled ? 0 : -1;
    ovLock.setAttribute('aria-disabled', String(!enabled));
  }
  if (ovLock && !ovLock.dataset.lockBound) {
    ovLock.dataset.lockBound = '1';
    ovLock.addEventListener('click', (e: any) => { e.stopPropagation(); if (state.config && state.config.hasPassword) lockScreenNow(); });
    ovLock.addEventListener('keydown', (e: any) => {
      if (e.key !== 'Enter' && e.key !== ' ') return;
      e.preventDefault();
      e.stopPropagation();
      if (state.config?.hasPassword) lockScreenNow();
    });
  }
  if(workspaceChanged)await restoreWorkspaceScene();
  estimateContext();
  if (typeof scheduleContextPreview === 'function') scheduleContextPreview();
}
function syncVoiceAssistantDisplayName() {
  const name = voiceAssistantDisplayName();
  const entry = $('assistant-entry');
  if (entry) {
    const label = entry.querySelector('.assistant-entry-name');
    if (label) label.textContent = name;
    entry.dataset.sessionTitle = name;
    entry.setAttribute('aria-label', name);
    entry.title = name;
  }
  if (assistantEntrySession) assistantEntrySession.title = name;
  const lockName = $('lock-assistant-name');
  if (lockName) lockName.textContent = name;
  if (state.session?.kind === 'assistant') {
    state.session.title = name;
    const title = $('session-title');
    if (title) title.textContent = name;
  }
}
