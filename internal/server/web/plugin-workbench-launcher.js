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
        const updateButton = document.getElementById('plugin-workbench-updates');
        const updateCount = document.getElementById('plugin-workbench-update-count');
        const autostartButton = document.getElementById('plugin-workbench-autostart');
        const autostartDialog = document.getElementById('plugin-autostart-dialog');
        const autostartList = document.getElementById('plugin-autostart-list');
        const autostartEmpty = document.getElementById('plugin-autostart-empty');
        if (!entry || !dialog || !grid || !rail || !preview || !close || !updateButton || !updateCount || !autostartButton || !autostartDialog || !autostartList || !autostartEmpty || entry.dataset.mounted)
            return;
        entry.dataset.mounted = 'true';
        const node = services.el;
        const t = services.t;
        const letters = 'ABCDEFGHIJKLMNOPQRSTUVWXYZ#'.split('');
        const letterButtons = {};
        let activeLetter = '';
        let renderedKey = '';
        let renderedAlphabetical = false;
        let previewTimer = 0;
        let previewToken = 0;
        let updateBusy = false, autostartBusy = false, updateCheckedAt = 0;
        let updates = [];
        const updateServices = { api: services.api, refresh: services.refreshPlugins, t, toast: services.toast };
        function hidePreview() {
            previewToken++;
            if (previewTimer)
                clearTimeout(previewTimer);
            previewTimer = 0;
            if (!preview.hidden)
                preview.hidden = true;
        }
        function positionPreview(item) {
            if (typeof item.getBoundingClientRect !== 'function')
                return;
            const rect = item.getBoundingClientRect(), host = dialog.getBoundingClientRect(), content = grid.getBoundingClientRect(), size = preview.getBoundingClientRect();
            const width = size.width || 264, height = size.height || 190, margin = 12, gap = 10;
            const preferredLeft = rect.right + gap + width <= host.right - margin ? rect.right - host.left + gap : rect.left - host.left - width - gap;
            const maxLeft = Math.max(margin, host.width - width - margin);
            const minTop = content.top - host.top + 8, maxTop = Math.max(minTop, content.bottom - host.top - height - 8);
            preview.style.left = Math.max(margin, Math.min(preferredLeft, maxLeft)) + 'px';
            preview.style.top = Math.max(minTop, Math.min(rect.top - host.top - 8, maxTop)) + 'px';
        }
        function showPreview(plugin, item) {
            if (item.disabled)
                return;
            hidePreview();
            const token = previewToken;
            previewTimer = setTimeout(() => {
                previewTimer = 0;
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
                // Hover only renders metadata. Starting an iframe/daemon here made a
                // pointer crossing several cards repeatedly create and destroy full
                // workbenches, which was the launcher's main source of intermittent jank.
            }, 180);
        }
        function updateRail(items) {
            const available = new Set(items.map(letterOf));
            for (const letter of letters) {
                const button = letterButtons[letter];
                const disabled = !available.has(letter);
                const pressed = String(activeLetter === letter);
                if (button.disabled !== disabled)
                    button.disabled = disabled;
                if (button.getAttribute('aria-pressed') !== pressed)
                    button.setAttribute('aria-pressed', pressed);
            }
        }
        function scrollToActiveLetter() {
            if (!activeLetter || typeof grid.querySelector !== 'function')
                return;
            const target = grid.querySelector('[data-letter-heading="' + activeLetter + '"]');
            target?.scrollIntoView?.({ block: 'start', behavior: 'auto' });
        }
        function refresh(scrollToLetter = false) {
            const items = supported(services.plugins());
            const alphabetical = !!activeLetter;
            const ordered = alphabetical ? [...items].sort((a, b) => letterOf(a).localeCompare(letterOf(b)) || String(services.displayName(a.name)).localeCompare(String(services.displayName(b.name)), 'zh-Hans-CN-u-co-pinyin') || a.id.localeCompare(b.id)) : items;
            const key = JSON.stringify(ordered.map(plugin => [plugin.id, services.displayName(plugin.name), !!plugin.enabled, !!plugin.error, plugin.version, services.fileFirst(plugin.id)]));
            dialog.dataset.pluginCount = String(items.length);
            dialog.dataset.order = alphabetical ? 'alphabetical' : 'default';
            updateRail(items);
            if (key === renderedKey && alphabetical === renderedAlphabetical) {
                if (scrollToLetter)
                    scrollToActiveLetter();
                return;
            }
            hidePreview();
            renderedKey = key;
            renderedAlphabetical = alphabetical;
            grid.replaceChildren();
            if (!items.length) {
                grid.append(node('p', 'workbench-launcher-empty', t('暂无支持工作台的插件。')));
                return;
            }
            let group = '';
            for (const plugin of ordered) {
                const letter = letterOf(plugin);
                if (alphabetical && letter !== group) {
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
            if (scrollToLetter)
                scrollToActiveLetter();
        }
        function paintUpdates() {
            updateButton.disabled = updateBusy;
            updateButton.classList.toggle('has-updates', updates.length > 0);
            updateButton.dataset.state = updateBusy ? 'busy' : updates.length ? 'available' : 'idle';
            updateCount.hidden = updates.length === 0;
            updateCount.textContent = String(updates.length);
            updateButton.title = updateBusy ? t('正在处理插件升级…') : updates.length ? t('一键升级 {0} 个插件', updates.length) : t('检查插件升级');
            updateButton.setAttribute('aria-label', updateButton.title);
            const autostartCount = services.plugins().filter((plugin) => plugin.daemon && plugin.enabled && plugin.autoStart !== false).length;
            autostartButton.dataset.count = String(autostartCount);
        }
        async function checkUpdates(force = false) {
            if (updateBusy || services.blocked() || (!force && updateCheckedAt && Date.now() - updateCheckedAt < 300000))
                return;
            updateBusy = true;
            updates = [];
            paintUpdates();
            try {
                const result = await window.AidePluginUpgrade.checkAll(services.plugins(), updateServices);
                updates = result.updates;
                updateCheckedAt = Date.now();
                if (result.failures.length)
                    services.toast(t('{0} 个插件升级检查失败，可稍后重试', result.failures.length));
            }
            catch (error) {
                services.toast(error.message || t('插件升级检查失败'));
            }
            finally {
                updateBusy = false;
                paintUpdates();
            }
        }
        async function applyUpdates() {
            if (updateBusy || services.blocked())
                return;
            if (!updates.length) {
                await checkUpdates(true);
                if (!updates.length)
                    return;
            }
            updateBusy = true;
            paintUpdates();
            try {
                const result = await window.AidePluginUpgrade.upgradeAll(updates, updateServices);
                updates = result.failures;
                updateCheckedAt = Date.now();
                if (result.succeeded.length)
                    services.toast(t('已升级 {0} 个插件', result.succeeded.length));
                if (result.refreshError)
                    services.toast(t('插件已升级，列表刷新失败，请手动刷新'));
                if (result.failures.length)
                    services.toast(t('{0} 个插件升级失败，请单独重试', result.failures.length));
            }
            catch (error) {
                services.toast(error.message || t('插件升级失败'));
            }
            finally {
                updateBusy = false;
                paintUpdates();
            }
        }
        function renderAutostart() {
            const plugins = services.plugins().filter((plugin) => plugin.daemon);
            autostartList.replaceChildren();
            autostartEmpty.hidden = plugins.length > 0;
            for (const plugin of plugins) {
                const row = node('div', 'plugin-autostart-row');
                const text = node('div', 'plugin-autostart-copy');
                text.append(node('strong', '', services.displayName(plugin.name)), node('small', '', plugin.id + ' · ' + (plugin.enabled ? t('插件已启用') : t('插件已停用'))));
                const toggle = node('button', 'plugin-autostart-toggle' + (plugin.autoStart === false ? '' : ' active'), plugin.autoStart === false ? t('已关闭') : t('已开启'));
                toggle.type = 'button';
                toggle.disabled = autostartBusy;
                toggle.setAttribute('aria-pressed', String(plugin.autoStart !== false));
                toggle.setAttribute('aria-label', t('{0} 后台自启动', services.displayName(plugin.name)));
                toggle.onclick = async () => {
                    if (autostartBusy)
                        return;
                    autostartBusy = true;
                    renderAutostart();
                    try {
                        const enabled = plugin.autoStart === false;
                        await services.api('/plugins/' + encodeURIComponent(plugin.id) + '/autostart', { method: 'PUT', body: JSON.stringify({ enabled }) });
                        plugin.autoStart = enabled;
                        services.toast(enabled ? t('已开启 {0} 后台自启动', services.displayName(plugin.name)) : t('已关闭 {0} 后台自启动', services.displayName(plugin.name)));
                    }
                    catch (error) {
                        services.toast(error.message || t('后台自启动设置失败'));
                    }
                    finally {
                        autostartBusy = false;
                        renderAutostart();
                        paintUpdates();
                    }
                };
                row.append(text, toggle);
                autostartList.append(row);
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
                refresh(true);
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
            paintUpdates();
            checkUpdates().catch(() => { });
        };
        updateButton.onclick = () => { applyUpdates().catch(() => { }); };
        autostartButton.onclick = () => { renderAutostart(); if (!autostartDialog.open)
            autostartDialog.showModal(); };
        const autostartClose = document.getElementById('plugin-autostart-close');
        autostartClose.onclick = () => autostartDialog.close();
        autostartDialog.addEventListener('click', (event) => { if (event.target === autostartDialog)
            autostartDialog.close(); });
        close.onclick = () => dialog.close();
        grid.addEventListener('scroll', () => { if (!preview.hidden || previewTimer)
            hidePreview(); });
        dialog.addEventListener('close', hidePreview);
        dialog.addEventListener('click', (event) => {
            if (event.target === dialog)
                dialog.close();
        });
        refreshPanel = () => refresh();
        closePanel = () => { hidePreview(); if (autostartDialog.open)
            autostartDialog.close(); if (dialog.open)
            dialog.close(); };
        refresh();
        paintUpdates();
    }
    window.AidePluginWorkbenchLauncher = {
        mount,
        supported,
        letterOf,
        refresh: () => refreshPanel(),
        close: () => closePanel(),
    };
})();
