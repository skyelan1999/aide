"use strict";
/* Launcher for installed plugins that explicitly declare workbench protocol v1. */
(() => {
    'use strict';
    let refreshPanel = () => { };
    let closePanel = () => { };
    function supported(items) {
        return (Array.isArray(items) ? items : []).filter(plugin => plugin?.workbench?.protocol === 1);
    }
    function letterOf(plugin) {
        const name = String(plugin?.name || '').trim();
        const source = /^[A-Za-z]/.test(name) ? name : String(plugin?.id || name).trim();
        const letter = source.charAt(0).toUpperCase();
        return /^[A-Z]$/.test(letter) ? letter : '#';
    }
    function mount(services) {
        const entry = document.getElementById('plugin-workbench-entry');
        const dialog = document.getElementById('plugin-workbench-panel');
        const grid = document.getElementById('plugin-workbench-grid');
        const rail = document.getElementById('plugin-workbench-letters');
        const preview = document.getElementById('plugin-workbench-preview');
        const close = document.getElementById('plugin-workbench-close');
        if (!entry || !dialog || !grid || !rail || !preview || !close || entry.dataset.mounted)
            return;
        entry.dataset.mounted = 'true';
        const node = services.el;
        const t = services.t;
        const letters = 'ABCDEFGHIJKLMNOPQRSTUVWXYZ#'.split('');
        const letterButtons = {};
        let activeLetter = '';
        let previewTimer = 0;
        let previewToken = 0;
        function hidePreview() {
            previewToken++;
            clearTimeout(previewTimer);
            preview.hidden = true;
        }
        function positionPreview(item) {
            if (typeof item.getBoundingClientRect !== 'function')
                return;
            const rect = item.getBoundingClientRect(), host = dialog.getBoundingClientRect();
            const left = rect.right + 276 < window.innerWidth ? rect.right - host.left + 10 : rect.left - host.left - 274;
            preview.style.left = Math.max(12, left) + 'px';
            preview.style.top = Math.max(76, Math.min(rect.top - host.top - 8, host.height - 205)) + 'px';
        }
        function showPreview(plugin, item) {
            if (item.disabled)
                return;
            hidePreview();
            const token = previewToken;
            previewTimer = setTimeout(async () => {
                if (token !== previewToken || !dialog.open)
                    return;
                const name = services.displayName ? services.displayName(plugin.name) : plugin.name;
                const fileFirst = services.fileFirst(plugin.id);
                const initial = Array.from(String(name || plugin.id || '?').trim())[0]?.toUpperCase() || '?';
                const glyph = node('span', 'workbench-preview-glyph', initial);
                glyph.setAttribute('aria-hidden', 'true');
                const heading = node('div', 'workbench-preview-heading');
                heading.append(glyph, node('strong', '', name));
                const description = node('p', '', plugin.description || plugin.workbench.title || name);
                const status = node('small', 'workbench-preview-status', fileFirst ? t('从项目文件打开') : t('点击打开'));
                status.setAttribute('role', 'status');
                preview.replaceChildren(heading, description, node('span', 'workbench-preview-meta', 'v' + (plugin.version || '—')), status);
                preview.hidden = false;
                positionPreview(item);
                if (!fileFirst && plugin.enabled && !plugin.error) {
                    const preloaded = await services.preloadWorkbench(plugin);
                    if (token === previewToken && preloaded) {
                        preview.dataset.preloaded = plugin.id;
                        status.textContent = t('已启动后台预加载');
                    }
                }
            }, 180);
        }
        function animateOrder(previous) {
            if (!previous.size || window.matchMedia?.('(prefers-reduced-motion: reduce)').matches || typeof grid.querySelectorAll !== 'function')
                return;
            for (const item of Array.from(grid.querySelectorAll('.workbench-plugin-item'))) {
                const before = previous.get(item.dataset.pluginId || '');
                if (!before || typeof item.animate !== 'function')
                    continue;
                const after = item.getBoundingClientRect(), x = before.left - after.left, y = before.top - after.top;
                if (Math.abs(x) < 1 && Math.abs(y) < 1)
                    continue;
                item.animate([{ transform: `translate(${x}px, ${y}px)`, opacity: .65 }, { transform: 'translate(0, 0)', opacity: 1 }], { duration: 280, easing: 'cubic-bezier(.2,.8,.2,1)' });
            }
        }
        function refresh(animate = false, scrollToLetter = false) {
            const previous = new Map();
            if (animate && typeof grid.querySelectorAll === 'function') {
                for (const item of Array.from(grid.querySelectorAll('.workbench-plugin-item')))
                    previous.set(item.dataset.pluginId || '', item.getBoundingClientRect());
            }
            hidePreview();
            const items = supported(services.plugins());
            const ordered = activeLetter ? [...items].sort((a, b) => letterOf(a).localeCompare(letterOf(b)) || String(services.displayName(a.name)).localeCompare(String(services.displayName(b.name)), 'zh-Hans-CN-u-co-pinyin') || a.id.localeCompare(b.id)) : items;
            grid.replaceChildren();
            dialog.dataset.pluginCount = String(items.length);
            dialog.dataset.order = activeLetter ? 'alphabetical' : 'default';
            for (const letter of letters) {
                const button = letterButtons[letter];
                const available = items.some(plugin => letterOf(plugin) === letter);
                button.disabled = !available;
                button.setAttribute('aria-pressed', String(activeLetter === letter));
            }
            if (!items.length) {
                grid.append(node('p', 'workbench-launcher-empty', t('暂无支持工作台的插件。')));
                return;
            }
            let group = '';
            for (const plugin of ordered) {
                const letter = letterOf(plugin);
                if (activeLetter && letter !== group) {
                    group = letter;
                    const heading = node('h3', 'workbench-letter-heading', letter);
                    heading.dataset.letterHeading = letter;
                    grid.append(heading);
                }
                const name = services.displayName ? services.displayName(plugin.name) : plugin.name;
                const fileFirst = services.fileFirst(plugin.id);
                const actionLabel = fileFirst ? t('从项目文件打开') : t('打开工作台');
                const status = plugin.error ? t('插件载入失败') : !plugin.enabled ? t('已停用') : actionLabel;
                const item = node('button', 'workbench-plugin-item');
                item.type = 'button';
                item.dataset.pluginId = plugin.id;
                item.disabled = !plugin.enabled || !!plugin.error;
                item.setAttribute('aria-label', name + ' · ' + status);
                const hash = Array.from(String(plugin.id)).reduce((sum, char) => sum + char.charCodeAt(0), 0);
                item.classList.add('tone-' + hash % 6);
                const initial = Array.from(String(name || plugin.id || '?').trim())[0]?.toUpperCase() || '?';
                const glyph = node('span', 'workbench-plugin-glyph', initial);
                glyph.setAttribute('aria-hidden', 'true');
                item.append(glyph, node('strong', '', name), node('small', '', status));
                item.onmouseenter = () => showPreview(plugin, item);
                item.onfocus = () => showPreview(plugin, item);
                item.onmouseleave = hidePreview;
                item.onblur = hidePreview;
                item.onclick = async () => {
                    if (services.blocked())
                        return;
                    hidePreview();
                    dialog.close();
                    try {
                        if (fileFirst)
                            await services.openGuide(plugin);
                        else
                            await services.openWorkbench(plugin);
                    }
                    catch (error) {
                        services.toast(error.message || t('操作失败'));
                    }
                };
                grid.append(item);
            }
            animateOrder(previous);
            if (scrollToLetter && activeLetter && typeof grid.querySelector === 'function') {
                const target = grid.querySelector('[data-letter-heading="' + activeLetter + '"]');
                target?.scrollIntoView?.({ block: 'start', behavior: window.matchMedia?.('(prefers-reduced-motion: reduce)').matches ? 'auto' : 'smooth' });
            }
        }
        for (const letter of letters) {
            const button = node('button', '', letter);
            button.type = 'button';
            button.dataset.letter = letter;
            button.setAttribute('aria-label', letter);
            button.setAttribute('aria-pressed', 'false');
            button.onclick = () => {
                activeLetter = activeLetter === letter ? '' : letter;
                refresh(true, true);
            };
            letterButtons[letter] = button;
            rail.append(button);
        }
        entry.onclick = () => {
            if (services.blocked())
                return;
            activeLetter = '';
            refresh();
            if (!dialog.open)
                dialog.showModal();
        };
        close.onclick = () => dialog.close();
        grid.addEventListener('scroll', hidePreview);
        dialog.addEventListener('close', hidePreview);
        dialog.addEventListener('click', (event) => {
            if (event.target === dialog)
                dialog.close();
        });
        refreshPanel = () => refresh();
        closePanel = () => { hidePreview(); if (dialog.open)
            dialog.close(); };
        refresh();
    }
    window.AidePluginWorkbenchLauncher = {
        mount,
        supported,
        letterOf,
        refresh: () => refreshPanel(),
        close: () => closePanel(),
    };
})();
