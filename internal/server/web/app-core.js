'use strict';
// @ts-ignore -- independent page scripts share the historical global helper name.
const t = (key, ...args) => window.aideI18n ? window.aideI18n.t(key, ...args) : String(key).replace(/\{(\d+)\}/g, (m, i) => args[i] ?? m);
// @ts-ignore -- independent page scripts share the historical global DOM helper name.
const $ = (id) => document.getElementById(id);
function updateFavicon() {
    const icon = $('app-favicon');
    if (!icon)
        return;
    const styles = getComputedStyle(document.documentElement);
    const brand = styles.getPropertyValue('--brand').trim() || '#007aff';
    const foreground = styles.getPropertyValue('--on-brand').trim() || '#ffffff';
    const svg = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 64 64"><rect width="64" height="64" rx="15" fill="${brand}"/><text x="32" y="49" text-anchor="middle" font-family="Inter,-apple-system,BlinkMacSystemFont,Segoe UI,sans-serif" font-size="53" font-weight="800" fill="${foreground}">a</text></svg>`;
    icon.href = 'data:image/svg+xml;charset=utf-8,' + encodeURIComponent(svg);
}
updateFavicon();
if (window.aideUI?.subscribe)
    window.aideUI.subscribe(updateFavicon);
// @ts-ignore -- app state is global to the classic scripts loaded after this file.
const state = { token: localStorage.getItem('aide-token') || '', session: null, sessionJSON: '', historyLimit: 30, historyScroll: false, pendingSessionId: '', submitting: false, mode: 'chat', root: 'workspace', dir: '.', fileDirs: {}, fileEntries: [], fileSelection: new Set(), fileSelectionLocation: '', fileSelectionAnchor: -1, fileSearch: '', fileSearchScope: 'folder', fileSearchMatch: 'fuzzy', commandHistory: [], commandHistoryIndex: 0, commandHistoryDraft: '', attachments: [], file: null, busy: false, poll: null, config: null, xiaomiModelSettings: null, commandAbort: null, profiles: null, modelDraft: null, plugins: [], panel: 'files', sources: [], source: '', stream: null, live: {}, liveStable: {}, liveTool: {}, liveReasoning: {}, runPhase: {}, thinkingOpen: new Set(), streamRetryAt: 0, queueMode: true, approvalPolicy: null, approvalModeBusy: false, autoScroll: true, jumpAnimating: false, contextCompactions: new Map() };
// @ts-ignore -- starmap has an independent page-local timer with the same global name.
let recoveryReady = false, recoveryTimer = 0, recoveryNotice = false, chatRecoveryEpoch = 0;
let workspaceSceneRestoring = false;
const recoveryScope = () => state.config?.workspaceId || '';
const chatDraftKey = (id, scope = recoveryScope()) => JSON.stringify([scope, id]);
function recoveryFailure(error) { if (!recoveryNotice) {
    recoveryNotice = true;
    toast(t('本地草稿保存失败') + ' · ' + error.message);
} }
function cancelPendingRecoveryWrites() { chatRecoveryEpoch++; clearTimeout(recoveryTimer); window.AideContinuity?.abortPendingWrites?.(); window.AidePluginPlans?.closeCheck?.(); }
function persistWorkspaceScene(overrides = {}) {
    if (!recoveryReady || lockScreen.locked || workspaceSceneRestoring)
        return;
    const scope = recoveryScope(), epoch = chatRecoveryEpoch, active = () => epoch === chatRecoveryEpoch && !lockScreen.locked && scope === recoveryScope();
    const scene = { session: state.session?.id || '', root: state.root, source: state.source, dir: state.dir, fileDirs: state.fileDirs,
        openFile: $('editor-dialog')?.open && state.file ? { root: state.file.root, source: state.file.source || '', path: state.file.path } : null, ...overrides };
    return window.AideContinuity?.writeTab(scope, 'scene', 'workbench', scene, { active }).catch(recoveryFailure);
}
function persistChatDraft(strict = false) {
    if (!recoveryReady || lockScreen.locked)
        return;
    const scope = recoveryScope(), id = state.session?.id || '';
    // The immutable snapshot belongs to the captured session even after navigation.
    // Lock/workspace generations still invalidate pending writes.
    const epoch = chatRecoveryEpoch, active = () => epoch === chatRecoveryEpoch && !lockScreen.locked && scope === recoveryScope();
    if (!active())
        return;
    const failed = (e) => { recoveryFailure(e); if (strict === true)
        throw e; };
    const draft = { text: $('prompt').value, attachments: state.attachments.map((a) => ({ ...a })), start: $('prompt').selectionStart, end: $('prompt').selectionEnd, scroll: $('prompt').scrollTop };
    sessionDrafts.set(chatDraftKey(id), draft);
    return Promise.all([
        window.AideContinuity?.writeTab(scope, 'chat', id, draft, { active }).catch(failed),
        window.AideContinuity?.writeTab(scope, 'scene', 'workbench', { session: id, root: state.root, source: state.source, dir: state.dir, fileDirs: state.fileDirs, openFile: $('editor-dialog')?.open && state.file ? { root: state.file.root, source: state.file.source || '', path: state.file.path } : null }, { active }).catch(failed)
    ]);
}
function restoreChatSelection(draft) { const prompt = $('prompt'); if (!draft)
    return; const start = Math.min(prompt.value.length, Math.max(0, Number(draft.start) || 0)), end = Math.min(prompt.value.length, Math.max(start, Number(draft.end) || start)); prompt.setSelectionRange(start, end); prompt.scrollTop = Math.max(0, Number(draft.scroll) || 0); }
function cacheEmergencyChatDraft() {
    if (!recoveryReady || lockScreen.locked)
        return;
    const scope = recoveryScope(), id = state.session?.id || '';
    if (!scope || !window.AideContinuity?.enabled())
        return;
    const draft = { text: $('prompt').value, attachments: state.attachments.map((a) => ({ ...a })), start: $('prompt').selectionStart, end: $('prompt').selectionEnd, scroll: $('prompt').scrollTop };
    try {
        window.AideContinuity.cacheTab(scope, 'chat', id, draft, { active: () => !lockScreen.locked && scope === recoveryScope() && id === (state.session?.id || '') });
        sessionDrafts.set(chatDraftKey(id), draft);
    }
    catch (error) {
        recoveryFailure(error);
    }
}
function scheduleChatRecovery(cacheImmediately = true) { if (cacheImmediately)
    cacheEmergencyChatDraft(); clearTimeout(recoveryTimer); recoveryTimer = setTimeout(persistChatDraft, 180); }
let chatRecoverySeq = 0;
const chatContent = (d) => JSON.stringify([d?.text || '', d?.attachments || []]);
async function refreshChatDraftChoices() {
    const seq = ++chatRecoverySeq, scope = recoveryScope(), id = state.session?.id || '';
    let panel = $('chat-recovery');
    if (!panel) {
        panel = el('details', 'chat-recovery');
        panel.id = 'chat-recovery';
        $('task-form').prepend(panel);
    }
    panel.hidden = true;
    panel.replaceChildren();
    if (!window.AideContinuity?.listChatDrafts || lockScreen.locked)
        return;
    const drafts = await AideContinuity.listChatDrafts(scope, id).catch((e) => { recoveryFailure(e); return []; });
    if (seq !== chatRecoverySeq || scope !== recoveryScope() || id !== (state.session?.id || '') || lockScreen.locked)
        return;
    const current = () => ({ text: $('prompt').value, attachments: state.attachments });
    const choices = drafts.filter((d) => chatContent(d) !== chatContent(current()));
    if (!choices.length)
        return;
    const summary = el('summary', '', t('其他恢复草稿 · {0}', choices.length));
    const body = el('div', 'chat-recovery-body'), hint = el('p', 'muted', t('选择恢复仅替换输入区，不会发送任务；当前草稿会先保留。'));
    const select = el('select');
    select.setAttribute('aria-label', t('选择恢复草稿'));
    choices.forEach((d, i) => { const option = el('option', '', t('{0} · {1} 个附件 · {2}', String(d.text || '').replace(/\s+/g, ' ').slice(0, 60) || t('仅附件'), d.attachments?.length || 0, d.updated ? new Date(d.updated).toLocaleString() : t('较早草稿'))); option.value = String(i); select.append(option); });
    const restore = el('button', 'quiet', t('恢复到输入区')), dismiss = el('button', 'quiet', t('忽略此草稿'));
    restore.type = dismiss.type = 'button';
    const valid = () => seq === chatRecoverySeq && scope === recoveryScope() && id === (state.session?.id || '') && !lockScreen.locked && !state.submitting;
    restore.onclick = action(async () => {
        if (!valid())
            return;
        const d = choices[Number(select.value)], before = chatContent(current());
        restore.disabled = dismiss.disabled = true;
        try {
            await persistChatDraft(true);
            if (!valid() || chatContent(current()) !== before)
                return;
            await AideContinuity.adoptChatDraft(scope, id, d);
            if (!valid())
                return;
            if (chatContent(current()) !== before) {
                await persistChatDraft(true);
                return;
            }
            $('prompt').value = d.text || '';
            state.attachments = (d.attachments || []).map((a) => ({ ...a }));
            renderAttachments();
            restoreChatSelection(d);
            await persistChatDraft();
            $('prompt').focus();
            scheduleContextPreview();
        }
        finally {
            await refreshChatDraftChoices();
        }
    });
    dismiss.onclick = action(async () => { if (!valid())
        return; restore.disabled = dismiss.disabled = true; try {
        await AideContinuity.dismissChatDraft(scope, id, choices[Number(select.value)]);
    }
    finally {
        await refreshChatDraftChoices();
    } });
    body.append(hint, select, restore, dismiss);
    panel.append(summary, body);
    panel.hidden = false;
}
function voiceAssistantDisplayName() { return String(state.config?.voiceAssistantName || '小秘').trim() || '小秘'; }
function currentHostPort() { return Number(location.port || (location.protocol === 'https:' ? 443 : 80)); }
const fragment = new URLSearchParams(location.hash.slice(1));
state.liveRound = {}; // Keep per-run streaming rounds initialized on the first session.
// 文件面板的上传/搜索控件保持为脚本生成，避免与嵌入式页面模板的单行结构耦合。
// 它们在后续的事件绑定和首次 loadFiles 前已经存在。
(() => {
    const tools = document.querySelector('.file-tools');
    if (!tools)
        return;
    tools.insertAdjacentHTML('afterend', '<div class="file-filter"><div class="file-search-box"><input id="file-search" type="search" autocomplete="off" placeholder="搜索文件" aria-label="搜索文件" data-i18n-placeholder="搜索文件" data-i18n-aria-label="搜索文件"><button type="button" id="file-search-scope" aria-pressed="false" title="包含子文件夹" aria-label="包含子文件夹" data-i18n-title="包含子文件夹" data-i18n-aria-label="包含子文件夹">↳</button><button type="button" id="file-search-match" aria-pressed="false" title="精确匹配" aria-label="精确匹配" data-i18n-title="精确匹配" data-i18n-aria-label="精确匹配">＝</button></div></div>');
    const list = $('files');
    list.tabIndex = 0;
    list.setAttribute('aria-label', t('文件列表'));
    list.dataset.i18nAriaLabel = '文件列表';
})();
// 纯前端「忽略此提案」集合（无后端拒绝端点）：仅在内存折叠卡片，不写盘、不影响会话
const ignoredProposals = new Set();
if (fragment.has('token')) {
    state.token = fragment.get('token');
    localStorage.setItem('aide-token', state.token);
    fragment.delete('token');
    history.replaceState(history.state, '', location.pathname + location.search + (fragment.toString() ? '#' + fragment.toString() : ''));
}
