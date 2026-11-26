"use strict";
// State changes and listener cleanup remain immediate; experience retains only
// short-lived inert paint, and falls back to immediate cleanup without motion.
function leaveUISurface(node, close, remove = false) {
    if (window.aideExperience?.leave)
        window.aideExperience.leave(node, close, { remove });
    else {
        close?.();
        if (remove)
            node?.remove();
    }
}
function changeSidePanelState(update) { if (window.aideExperience?.changePanels)
    window.aideExperience.changePanels(update);
else
    update(); }
// passwordPrompt(title) 通用密文密码弹窗：替代浏览器原生 prompt（输入时密码在屏幕上明文可见）。
// 用 <dialog> + <input type=password>，回车确认 / Esc 或点遮罩取消。
// 返回 Promise<string|null>：确定为输入串（不 trim，由调用方决定），取消为 null。样式复用全局 dialog/label/button 主题变量。
// @ts-expect-error -- independent classic pages keep their own password prompt.
function passwordPrompt(title) {
    return new Promise((resolve) => {
        const dlg = el('dialog', 'pw-prompt');
        const row = el('label', 'pw-prompt-label', title);
        const input = el('input');
        input.type = 'password';
        input.autocomplete = 'current-password';
        input.spellcheck = false;
        const field = el('div', 'pw-prompt-field');
        const reveal = el('button', 'quiet quiet-sm pw-prompt-reveal', t('显示'));
        reveal.type = 'button';
        reveal.setAttribute('aria-label', t('显示密码'));
        reveal.setAttribute('aria-pressed', 'false');
        reveal.onclick = () => {
            const visible = input.type === 'password';
            input.type = visible ? 'text' : 'password';
            reveal.textContent = t(visible ? '隐藏' : '显示');
            reveal.setAttribute('aria-label', t(visible ? '隐藏密码' : '显示密码'));
            reveal.setAttribute('aria-pressed', String(visible));
            input.focus();
        };
        field.append(input, reveal);
        row.append(field);
        dlg.append(row);
        const actions = el('div', 'editor-footer');
        const cancel = el('button', 'quiet', t('取消'));
        cancel.type = 'button';
        const ok = el('button', 'primary', t('确定'));
        ok.type = 'button';
        actions.append(cancel, ok);
        dlg.append(actions);
        let settled = false;
        const done = (val) => { if (settled)
            return; settled = true; input.value = ''; dlg.close(); dlg.remove(); resolve(val); };
        cancel.onclick = () => done(null);
        ok.onclick = () => done(input.value);
        input.addEventListener('keydown', (e) => {
            if (e.key === 'Enter') {
                e.preventDefault();
                done(input.value);
            }
            else if (e.key === 'Escape') {
                e.preventDefault();
                done(null);
            }
        });
        dlg.addEventListener('click', (e) => { if (e.target === dlg)
            done(null); }); // 点遮罩取消
        dlg.addEventListener('cancel', () => done(null)); // 原生 Esc
        document.body.append(dlg);
        if (typeof dlg.showModal === 'function')
            dlg.showModal();
        else
            dlg.setAttribute('open', '');
        input.focus();
    });
}
// requestMasterAuth({reason}) 统一主身份认证弹窗（#43）：密码输入 + 指纹授权按钮。
// 已注册指纹（hasPlatformCredential=true）则按钮可点并自动发起 WebAuthn assertion；未注册置灰。
// 返回 Promise<string|null>：输入密码且验证通过→密码串；指纹通过→'success'；取消→null（失败已 toast/内联提示，不锁死）。
function requestMasterAuth({ reason, assistantSessionId, authBody } = {}) {
    return new Promise((resolve) => {
        const dlg = el('dialog', 'master-auth');
        const box = el('div', 'master-auth-box');
        box.append(el('div', 'master-auth-title', t('身份验证')));
        if (reason)
            box.append(el('p', 'muted', reason));
        const input = el('input');
        input.type = 'password';
        input.autocomplete = 'off';
        input.spellcheck = false;
        input.placeholder = t('输入账户密码');
        const err = el('div', 'master-auth-error', '');
        const hasCred = !!(state.config && state.config.hasPlatformCredential);
        const approvalCredentialMissing = !!authBody?.approvalMode && !state.config?.hasPassword && !hasCred;
        if (approvalCredentialMissing) {
            input.disabled = true;
            err.textContent = t('高风险审批需要先设置账户密码或绑定指纹，请前往设置 → 账户');
        }
        const actions = el('div', 'editor-footer');
        const cancel = el('button', 'quiet', t('取消'));
        cancel.type = 'button';
        const fp = el('button', 'master-auth-fp', t(window.AideNativeBiometrics?.platform === 'windows' ? 'Windows Hello 授权' : '指纹授权'));
        fp.type = 'button';
        const ok = el('button', 'primary', t('确认'));
        ok.type = 'button';
        ok.disabled = approvalCredentialMissing;
        actions.append(cancel, fp, ok);
        box.append(input, err, actions);
        dlg.append(box);
        let settled = false;
        const done = (v) => { if (settled)
            return; settled = true; dlg.close(); dlg.remove(); resolve(v); };
        cancel.onclick = () => done(null);
        dlg.addEventListener('cancel', () => done(null));
        dlg.addEventListener('click', (e) => { if (e.target === dlg)
            done(null); });
        const doPassword = async () => {
            const pw = input.value;
            if (!pw) {
                err.textContent = t('请输入密码');
                input.focus();
                return;
            }
            try {
                const body = { ...(authBody || {}), password: pw };
                if (assistantSessionId)
                    body.assistantSessionId = assistantSessionId;
                const result = await api('/auth/verify', { method: 'POST', body: JSON.stringify(body) });
                done(authBody ? result : pw);
            }
            catch (e) {
                err.textContent = e?.status === 403 && authBody?.approvalMode ? t(e.message) : t('密码错误');
                if (!approvalCredentialMissing) {
                    input.value = '';
                    input.focus();
                }
            }
        };
        ok.onclick = action(doPassword);
        input.addEventListener('keydown', (e) => {
            if (e.key === 'Enter') {
                e.preventDefault();
                ok.click();
            }
            else if (e.key === 'Escape') {
                e.preventDefault();
                done(null);
            }
        });
        const waReady = !!(state.config && state.config.webAuthnReady);
        const canFp = hasCred && waReady && location.hostname === 'localhost' && !window.AideNativeBiometrics;
        fp.disabled = !canFp;
        fp.classList.toggle('disabled', !canFp);
        if (!canFp && !hasCred)
            fp.title = t('未注册指纹，可在设置 → 账号中绑定');
        if (window.AideNativeBiometrics)
            touchIdAvailable().then((res) => {
                if (settled)
                    return;
                const enabled = hasCred && res.ok && res.enrolled;
                fp.disabled = !enabled;
                fp.classList.toggle('disabled', !enabled);
                if (!enabled)
                    fp.title = t(window.AideNativeBiometrics?.platform === 'windows' ? '请先用密码解锁，在设置 → 账户中绑定 Windows Hello' : '请先用密码解锁，在设置 → 账户中为 Aide 桌面绑定触控 ID');
            }).catch(() => { });
        fp.onclick = action(async () => {
            fp.disabled = true;
            err.textContent = '';
            try {
                const start = await api('/webauthn/assertion/start', { method: 'POST', body: '{}' });
                if (!start.allowCredentials || !start.allowCredentials.length) {
                    err.textContent = t('未注册指纹设备');
                    fp.disabled = false;
                    return;
                }
                const assertion = await requestPlatformAssertion(start);
                if (!assertion) {
                    fp.disabled = false;
                    return;
                }
                const body = { ...(authBody || {}), challenge: start.challenge, assertion };
                if (assistantSessionId)
                    body.assistantSessionId = assistantSessionId;
                const result = await api('/auth/verify', { method: 'POST', body: JSON.stringify(body) });
                done(authBody ? result : 'success');
            }
            catch (e) {
                if (e && e.name === 'NotAllowedError') {
                    fp.disabled = false;
                    return;
                }
                err.textContent = t('指纹验证失败，请重试');
                fp.disabled = false;
            }
        });
        document.body.append(dlg);
        if (typeof dlg.showModal === 'function')
            dlg.showModal();
        else
            dlg.setAttribute('open', '');
        input.focus();
    });
}
// @ts-expect-error -- independent classic pages keep their own API helper.
async function api(path, options = {}) {
    const { raw = false, ...requestOptions } = options;
    const isForm = typeof FormData !== 'undefined' && options.body instanceof FormData;
    const headers = { 'Authorization': 'Bearer ' + state.token, ...(isForm ? {} : { 'Content-Type': 'application/json' }), ...options.headers };
    const method = String(options.method || 'GET').toUpperCase();
    let response;
    // Safari/WKWebView reports a dropped connection as TypeError "Load failed".
    // The service restarts after an update or an OOM kill, so read-only polling
    // gets two short retries before surfacing a localized error. Writes are not
    // retried: a replay could duplicate a run or a file change.
    for (let attempt = 0;; attempt++) {
        try {
            response = await fetch('/api' + path, { ...requestOptions, headers });
            break;
        }
        catch (cause) {
            if (cause?.name === 'AbortError' || cause?.name === 'TimeoutError')
                throw cause;
            if ((method === 'GET' || method === 'HEAD') && attempt < 2) {
                await new Promise((resolve) => setTimeout(resolve, 800 * (attempt + 1)));
                continue;
            }
            const error = new Error(t('工作台连接中断，请稍后重试'));
            error.network = true;
            error.cause = cause;
            throw error;
        }
    }
    if (raw && response.ok)
        return response;
    let data;
    try {
        data = await response.json();
    }
    catch (cause) {
        const error = new Error(response.ok ? t('工作台返回了无法解析的响应，请稍后重试') : t('工作台连接中断，请稍后重试'));
        error.status = response.status;
        error.cause = cause;
        throw error;
    }
    if (!response.ok) {
        window.AideCrash?.reportServer(data);
        // Password/Passkey reauthentication failures also return 401. Only the
        // access-token middleware can mark those paths as requiring a new login;
        // otherwise keep the current identity dialog and its local error visible.
        const identityCheck = path === '/remote-access' || path === '/account/verify-password' || path === '/auth/verify' || path.startsWith('/webauthn/') || /^\/sessions\/[^/]+\/unlock-assistant$/.test(path);
        if ((response.status === 401 || response.status === 403) && response.headers?.get('X-Aide-Auth-Required') === 'lan-password') {
            location.replace('/lan-login.html');
            throw new Error(t('请重新输入账户密码登录'));
        }
        const tokenRequired = response.headers?.get('X-Aide-Auth-Required') === 'token';
        if (response.status === 401 && (!identityCheck || tokenRequired) && !$('login-dialog').open)
            $('login-dialog').showModal();
        const error = new Error(t(data?.error) || t("请求失败"));
        error.status = response.status;
        error.data = data;
        throw error;
    }
    return data;
}
// @ts-expect-error -- independent classic pages keep their own action wrapper.
function action(fn, options) { return window.AideAsyncAction.wrap(fn, (e) => toast(e.message), options); }
function setMode(mode) {
    if (mode === 'pw' && (!state.config?.professionalWorkflow || state.session?.kind === 'assistant'))
        return;
    state.mode = mode;
    document.querySelectorAll('.mode-switch button').forEach((b) => b.classList.toggle('active', b.dataset.mode === mode));
    const phaseBar = $('workflow-phases');
    if (phaseBar) {
        if (mode === 'workflow') {
            phaseBar.classList.remove('hidden');
            phaseBar.querySelectorAll('.phase-btn').forEach((btn, i) => {
                btn.style.animation = 'none';
                btn.offsetHeight; // reflow
                btn.style.animation = 'phaseIn 0.35s ease forwards ' + (i * 70) + 'ms';
            });
        }
        else {
            phaseBar.querySelectorAll('.phase-btn').forEach((btn, i) => {
                btn.style.animation = 'phaseOut 0.25s ease forwards ' + ((3 - i) * 50) + 'ms';
            });
            setTimeout(() => { if (state.mode !== 'workflow')
                phaseBar.classList.add('hidden'); }, 400);
        }
    }
    window.AideParallelChat?.sync();
    updateAutoModeUI();
    if (typeof updatePhaseHint === 'function')
        updatePhaseHint();
    if (typeof scheduleContextPreview === 'function')
        scheduleContextPreview();
}
function syncAssistantModeControls(isAssistantSess) {
    window.AideParallelChat?.sync();
    if (isAssistantSess) {
        if (state.mode !== 'chat')
            setMode('chat');
        state.workflowPhase = '';
        document.querySelectorAll('#workflow-phases .phase-btn').forEach((b) => b.classList.remove('selected'));
        $('workflow-phases')?.classList.add('hidden');
    }
    // The trajectory view also renders assistant-session message history. Keep it
    // available in 小秘; only workbench execution controls are incompatible there.
    const trajectoryToggle = $('trajectory-toggle');
    if (trajectoryToggle) {
        trajectoryToggle.disabled = false;
        trajectoryToggle.setAttribute('aria-disabled', 'false');
    }
    const controls = [...document.querySelectorAll('.mode-switch button'), $('queue-toggle')];
    controls.forEach((button) => {
        if (!button)
            return;
        button.disabled = isAssistantSess;
        button.setAttribute('aria-disabled', String(isAssistantSess));
    });
}
// AI 工作流阶段
state.workflowPhase = state.workflowPhase || '';
document.querySelectorAll('.phase-btn').forEach((btn) => {
    btn.onclick = () => {
        const phase = btn.dataset.phase;
        if (state.workflowPhase === phase) {
            state.workflowPhase = '';
            btn.classList.remove('selected');
        }
        else {
            state.workflowPhase = phase;
            document.querySelectorAll('.phase-btn').forEach((b) => b.classList.remove('selected'));
            btn.classList.add('selected');
        }
        updateAutoModeUI();
        updatePhaseHint();
    };
});
// 自动编排模式：AI 工作流下未选任何阶段时，由后端 autoModePrompt 处理，前端不显示描述
function updateAutoModeUI() { }
