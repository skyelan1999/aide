"use strict";
/* Optional decoration. Procedural backgrounds are rendered locally; no external requests or per-frame JavaScript. */
'use strict';
(function () {
    const root = document.documentElement;
    const layer = document.createElement('div');
    layer.className = 'workbench-background';
    layer.setAttribute('aria-hidden', 'true');
    layer.inert = true;
    document.body.prepend(layer);
    const reduced = window.matchMedia?.('(prefers-reduced-motion: reduce)');
    const forcedColors = window.matchMedia?.('(forced-colors: active)');
    const accepted = new Set(['video/mp4', 'video/webm', 'image/gif', 'image/png', 'image/jpeg', 'image/webp']);
    let mode = '', generation = 0, media = null, url = '', record = null, message = '', saving = false;
    const panels = new Set();
    const WORK_INTERVAL = 20 * 60_000;
    const SNOOZE_INTERVAL = 5 * 60_000;
    const REST_INTERVAL = 20_000;
    let database;
    let pageActive = true, eyeTimer, activeSince = null, workRemaining = WORK_INTERVAL, restEnds = 0;
    let notice, noticeStatus, noticeAction, noticeSnooze, previousFocus;
    function db() {
        if (!database)
            database = new Promise((resolve, reject) => {
                if (!window.indexedDB)
                    return reject(new Error(uiTextWorkbenchBackground('本地背景存储不可用')));
                const request = indexedDB.open('aide.workbench-background', 1);
                request.onupgradeneeded = () => request.result.createObjectStore('assets');
                request.onsuccess = () => { request.result.onversionchange = () => { request.result.close(); database = null; }; resolve(request.result); };
                request.onerror = () => reject(request.error);
                request.onblocked = () => reject(new Error(uiTextWorkbenchBackground('本地背景存储不可用')));
            }).catch((error) => { database = null; throw error; });
        return database;
    }
    async function stored(operation, value) {
        const connection = await db();
        return new Promise((resolve, reject) => {
            const transaction = connection.transaction('assets', operation === 'get' ? 'readonly' : 'readwrite');
            const store = transaction.objectStore('assets');
            const request = operation === 'get' ? store.get('current') : operation === 'put' ? store.put(value, 'current') : store.delete('current');
            transaction.oncomplete = () => resolve(request.result);
            transaction.onerror = transaction.onabort = () => reject(transaction.error || new Error(uiTextWorkbenchBackground('本地背景存储不可用')));
        });
    }
    function pageAvailable() {
        return pageActive && !document.hidden &&
            !document.querySelector('#login-dialog[open],#lock-screen:not([hidden])');
    }
    // PNG and WebP carry both still images and animation. Skip chunk payloads when
    // reading PNG metadata so a large color profile does not require decoding it.
    async function animatedImage(type, blob) {
        if (type !== 'image/png' && type !== 'image/webp')
            return type === 'image/gif';
        if (!blob?.slice)
            return true;
        if (type === 'image/webp') {
            const header = new Uint8Array(await blob.slice(0, 21).arrayBuffer());
            return String.fromCharCode(...header.subarray(12, 16)) === 'VP8X' && (header[20] & 2) !== 0;
        }
        let offset = 8;
        // Unusual metadata layouts fall back to the pausable path.
        for (let reads = 0; reads < 32 && offset + 8 <= blob.size; reads++) {
            const bytes = await blob.slice(offset, offset + 1024).arrayBuffer();
            const chunks = new DataView(bytes);
            let cursor = 0;
            while (cursor + 8 <= chunks.byteLength) {
                const tag = chunks.getUint32(cursor + 4);
                if (tag === 0x6163544c)
                    return true; // acTL precedes the first image data chunk.
                if (tag === 0x49444154 || tag === 0x49454e44)
                    return false; // IDAT or IEND.
                cursor += chunks.getUint32(cursor) + 12;
            }
            offset += cursor;
        }
        return true;
    }
    function startEyeRest() {
        if (mode !== 'eye-comfort' || !pageAvailable() || restEnds)
            return;
        activeSince = null;
        workRemaining = WORK_INTERVAL;
        restEnds = performance.now() + REST_INTERVAL;
        lifecycle();
    }
    function showEyeNotice(resting) {
        if (!notice) {
            notice = document.createElement('aside');
            notice.className = 'eye-rest-notice';
            noticeStatus = document.createElement('p');
            noticeStatus.setAttribute('role', 'status');
            noticeAction = document.createElement('button');
            noticeAction.type = 'button';
            noticeAction.className = 'primary';
            noticeAction.onclick = () => {
                if (!restEnds) {
                    startEyeRest();
                    return;
                }
                restEnds = 0;
                lifecycle();
            };
            noticeSnooze = document.createElement('button');
            noticeSnooze.type = 'button';
            noticeSnooze.className = 'quiet';
            noticeSnooze.onclick = () => {
                workRemaining = SNOOZE_INTERVAL;
                lifecycle();
            };
            const actions = document.createElement('div');
            actions.className = 'eye-rest-actions';
            actions.append(noticeAction, noticeSnooze);
            notice.append(noticeStatus, actions);
            notice.addEventListener('focusin', (event) => {
                if (!notice.contains(event.relatedTarget))
                    previousFocus = event.relatedTarget;
            });
            document.body.append(notice);
        }
        notice.hidden = false;
        const text = uiTextWorkbenchBackground(resting
            ? '休息 20 秒。放松视线，看向约 6 米外。'
            : '到远眺休息时间了。眨眨眼，看向屏幕外的远处。');
        if (noticeStatus.textContent !== text)
            noticeStatus.textContent = text;
        noticeAction.textContent = uiTextWorkbenchBackground(resting ? '结束休息' : '休息 20 秒');
        noticeSnooze.textContent = uiTextWorkbenchBackground('5 分钟后提醒');
        noticeSnooze.hidden = resting;
    }
    function hideEyeNotice() {
        if (!notice || notice.hidden)
            return;
        const hadFocus = notice.contains(document.activeElement);
        notice.hidden = true;
        if (hadFocus && pageAvailable() && previousFocus?.isConnected)
            previousFocus.focus({ preventScroll: true });
        previousFocus = null;
    }
    function syncEyeRest(visible) {
        clearTimeout(eyeTimer);
        const now = performance.now();
        if (activeSince !== null) {
            workRemaining = Math.max(0, workRemaining - (now - activeSince));
            activeSince = null;
        }
        if (mode !== 'eye-comfort') {
            workRemaining = WORK_INTERVAL;
            restEnds = 0;
            hideEyeNotice();
            return false;
        }
        if (restEnds <= now)
            restEnds = 0;
        const remind = window.aideUI?.get('eyeRestReminder') !== false;
        if (!remind)
            workRemaining = WORK_INTERVAL;
        if (!visible) {
            hideEyeNotice();
            return !!restEnds;
        }
        if (restEnds || (remind && workRemaining === 0)) {
            showEyeNotice(!!restEnds);
            if (restEnds)
                eyeTimer = setTimeout(lifecycle, Math.ceil(restEnds - now));
        }
        else {
            hideEyeNotice();
            if (remind) {
                activeSince = now;
                eyeTimer = setTimeout(lifecycle, Math.ceil(workRemaining));
            }
        }
        return !!restEnds;
    }
    function lifecycle() {
        const visible = pageAvailable();
        const resting = syncEyeRest(visible);
        const paused = !visible || root.dataset.motion === 'reduced' || root.dataset.motionProfile === 'balanced' || root.dataset.motionProfile === 'minimal' || reduced?.matches || forcedColors?.matches || resting;
        root.toggleAttribute('data-background-paused', paused);
        if (!media)
            return;
        if (media.tagName === 'VIDEO') {
            if (paused && !media.paused)
                media.pause();
            if (!paused && media.paused)
                media.play()?.catch(() => { });
        }
        else if (root.dataset.backgroundMedia === 'animated') {
            // Animated images lack playback controls. Unload while paused.
            if (paused)
                media.removeAttribute('src');
            else if (!media.hasAttribute('src'))
                media.src = url;
        }
    }
    function release() {
        if (media?.tagName === 'VIDEO') {
            media.pause();
            media.removeAttribute('src');
            media.load();
        }
        media?.remove();
        media = null;
        if (url.startsWith('blob:'))
            URL.revokeObjectURL(url);
        url = '';
        layer.replaceChildren();
        record = null;
        root.removeAttribute('data-background-media');
    }
    function refreshPanels() {
        for (const panel of panels) {
            if (!panel.node.isConnected) {
                panels.delete(panel);
                continue;
            }
            panel.update();
        }
    }
    async function apply(force = false) {
        const next = window.aideUI?.get('background') || 'studio';
        const strength = window.aideUI?.get('backgroundStrength');
        root.style?.setProperty('--wb-background-strength', String(typeof strength === 'number' && Number.isFinite(strength) ? Math.min(50, Math.max(10, strength)) / 100 : .22));
        if (next === mode && !force) {
            lifecycle();
            refreshPanels();
            return;
        }
        const ticket = ++generation;
        mode = next;
        release();
        message = '';
        root.dataset.background = mode;
        lifecycle();
        refreshPanels();
        if (mode === 'studio') {
            for (const name of ['plane', 'line', 'halo']) {
                const node = document.createElement('span');
                node.className = 'workbench-studio-' + name;
                layer.append(node);
            }
            return;
        }
        if (mode === 'modern') {
            for (const name of ['glow', 'grid', 'horizon']) {
                const node = document.createElement('span');
                node.className = 'workbench-modern-' + name;
                layer.append(node);
            }
            return;
        }
        if (mode === 'eye-comfort') {
            const glow = document.createElement('span');
            glow.className = 'workbench-eye-glow';
            layer.append(glow);
            return;
        }
        if (mode !== 'local')
            return;
        try {
            const asset = await stored('get');
            if (ticket !== generation)
                return;
            if ((!asset?.blob && !asset?.src) || !accepted.has(asset.type)) {
                message = '请先选择本地背景素材';
                refreshPanels();
                return;
            }
            const animated = await animatedImage(asset.type, asset.blob);
            if (ticket !== generation)
                return;
            record = asset;
            url = asset.src || URL.createObjectURL(asset.blob);
            const video = asset.type.startsWith('video/');
            media = document.createElement(video ? 'video' : 'img');
            if (video) {
                media.muted = true;
                media.defaultMuted = true;
                media.loop = true;
                media.playsInline = true;
                media.preload = 'metadata';
            }
            else
                media.alt = '';
            media.onerror = () => { if (ticket === generation) {
                release();
                message = '背景素材无法播放，请更换文件';
                refreshPanels();
            } };
            root.dataset.backgroundMedia = video ? 'video' : animated ? 'animated' : 'image';
            layer.append(media);
            if (video || !root.hasAttribute('data-background-paused') || root.dataset.backgroundMedia === 'image')
                media.src = url;
            lifecycle();
            refreshPanels();
        }
        catch (_) {
            if (ticket === generation) {
                message = '本地背景存储不可用';
                refreshPanels();
            }
        }
    }
    function renderSettings({ t = (text) => text } = {}) {
        refreshPanels(); // Retire detached settings nodes on every visit, even without a preference change.
        const node = document.createElement('div');
        node.className = 'settings-control background-control';
        const label = document.createElement('div');
        label.className = 'control-label';
        label.textContent = t('工作台背景');
        const choices = document.createElement('div');
        choices.className = 'background-options';
        choices.setAttribute('role', 'group');
        choices.setAttribute('aria-label', t('工作台背景'));
        const buttons = [['off', '关闭'], ['studio', '静域'], ['modern', '现代光场'], ['eye-comfort', '护眼'], ['local', '本地素材']].map(([value, text]) => {
            const button = document.createElement('button');
            button.type = 'button';
            button.dataset.value = value;
            button.textContent = t(text);
            button.onclick = () => window.aideUI?.set('background', value);
            choices.append(button);
            return button;
        });
        const picker = document.createElement('label');
        picker.className = 'background-picker';
        const text = document.createElement('span');
        text.textContent = t('加载本地背景');
        const input = document.createElement('input');
        input.type = 'file';
        input.accept = [...accepted].join(',');
        input.setAttribute('aria-label', t('加载本地背景'));
        const remove = document.createElement('button');
        remove.type = 'button';
        remove.className = 'quiet';
        remove.textContent = t('删除本地素材');
        const status = document.createElement('p');
        status.className = 'background-status';
        status.setAttribute('role', 'status');
        const note = document.createElement('p');
        note.className = 'muted';
        note.textContent = t('静域为轻量原生背景，随明暗主题自适应。自选素材支持 MP4、WebM、GIF、PNG、JPEG、WebP，最多 30 MiB，仅保存在当前浏览器。后台、锁屏及减少动态效果时暂停。');
        const intensity = document.createElement('label');
        intensity.className = 'background-intensity';
        const intensityText = document.createElement('span');
        intensityText.textContent = t('背景强度');
        const range = document.createElement('input');
        range.type = 'range';
        range.min = '10';
        range.max = '50';
        range.step = '1';
        range.setAttribute('aria-label', t('背景强度'));
        const value = document.createElement('output');
        intensity.append(intensityText, range, value);
        range.oninput = () => window.aideUI?.set('backgroundStrength', Number(range.value));
        const eyeSettings = document.createElement('div');
        eyeSettings.className = 'eye-rest-settings';
        const eyeNote = document.createElement('p');
        eyeNote.className = 'muted';
        eyeNote.textContent = t('柔光缓慢移动，跟随系统减少动态效果设置。远眺提醒只累计页面可见且未锁屏的时间。');
        const reminderLabel = document.createElement('label');
        const reminder = document.createElement('input');
        reminder.type = 'checkbox';
        reminder.onchange = () => window.aideUI?.set('eyeRestReminder', reminder.checked);
        reminderLabel.append(reminder, t('每 20 分钟提醒远眺'));
        const rest = document.createElement('button');
        rest.type = 'button';
        rest.className = 'quiet';
        rest.textContent = t('现在休息');
        rest.onclick = startEyeRest;
        eyeSettings.append(eyeNote, reminderLabel, rest);
        const panel = { node, update() {
                buttons.forEach(button => { button.setAttribute('aria-pressed', String(mode === button.dataset.value)); button.disabled = saving; });
                input.disabled = remove.disabled = saving;
                range.disabled = mode === 'off';
                eyeSettings.hidden = mode !== 'eye-comfort';
                reminder.checked = window.aideUI?.get('eyeRestReminder') !== false;
                status.hidden = !eyeSettings.hidden && !message && !saving;
                picker.hidden = remove.hidden = note.hidden = !eyeSettings.hidden;
                range.value = String(window.aideUI?.get('backgroundStrength') ?? 22);
                value.textContent = range.value + '%';
                range.setAttribute('aria-valuetext', value.textContent);
                status.textContent = saving ? t('正在保存背景…') : message ? t(message) : mode === 'studio' ? t('静域 · 柔光与几何层次') : mode === 'modern' ? t('现代光场 · 中性雾面与精密网格') : record?.name || t('选择本地素材后自动启用');
            } };
        input.onchange = async () => {
            const file = input.files?.[0];
            input.value = '';
            if (!file || saving)
                return;
            if (!accepted.has(file.type) || file.size > 30 * 1024 * 1024 || file.size === 0) {
                message = '请选择支持的素材格式，大小不超过 30 MiB';
                refreshPanels();
                return;
            }
            saving = true;
            refreshPanels();
            try {
                await stored('put', { blob: file, name: file.name, type: file.type });
                window.aideUI?.set('background', 'local');
                await apply(true);
                channel?.postMessage('changed');
            }
            catch (_) {
                message = '背景保存失败，原素材已保留';
            }
            finally {
                saving = false;
                refreshPanels();
            }
        };
        remove.onclick = async () => {
            if (saving)
                return;
            saving = true;
            refreshPanels();
            try {
                await stored('delete');
                if (mode === 'local')
                    window.aideUI?.set('background', 'off');
                channel?.postMessage('changed');
                message = '本地背景素材已删除';
            }
            catch (_) {
                message = '本地背景存储不可用';
            }
            finally {
                saving = false;
                refreshPanels();
            }
        };
        picker.append(text, input);
        node.append(label, choices, picker, remove, status, note, intensity, eyeSettings);
        panels.add(panel);
        panel.update();
        return node;
    }
    const channel = typeof BroadcastChannel === 'function' ? new BroadcastChannel('aide.background') : null;
    if (channel)
        channel.onmessage = () => { if (mode === 'local')
            apply(true); };
    document.addEventListener('visibilitychange', lifecycle);
    window.addEventListener('aide:motion', lifecycle);
    reduced?.addEventListener?.('change', lifecycle);
    forcedColors?.addEventListener?.('change', lifecycle);
    const observer = new MutationObserver(lifecycle);
    for (const node of document.querySelectorAll('#login-dialog,#lock-screen'))
        observer.observe(node, { attributes: true, attributeFilter: ['hidden', 'open', 'class'] });
    window.addEventListener('pagehide', () => { pageActive = false; lifecycle(); ++generation; release(); });
    window.addEventListener('pageshow', () => { if (!pageActive) {
        pageActive = true;
        apply(true);
    } });
    window.aideUI?.subscribe(() => apply());
    window.AideBackground = { renderSettings };
    apply();
})();
function uiTextWorkbenchBackground(key, ...values) { return globalThis.window?.aideI18n?.t(key, ...values) ?? String(key).replace(/\{(\d+)\}/g, (match, index) => index < values.length ? String(values[index]) : match); }
