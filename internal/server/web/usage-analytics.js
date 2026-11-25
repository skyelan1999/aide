"use strict";
'use strict';
(function () {
    let translate = key => key;
    const t = (key, ...args) => translate(key, ...args);
    let requestSequence = 0;
    function formatBytes(value) {
        const bytes = Math.max(0, Number(value) || 0);
        const units = ['B', 'KB', 'MB', 'GB', 'TB', 'PB'];
        let scaled = bytes;
        let index = 0;
        while (scaled >= 1000 && index < units.length - 1) {
            scaled /= 1000;
            index += 1;
        }
        const digits = index === 0 ? 0 : scaled < 10 ? 2 : scaled < 100 ? 1 : 0;
        return `${scaled.toFixed(digits)} ${units[index]}`;
    }
    function formatTime(value) {
        const date = new Date(value);
        return Number.isNaN(date.getTime()) ? '' : date.toLocaleString(window.aideI18n?.language() === 'en' ? 'en-US' : 'zh-CN', { hour12: false });
    }
    function utcDay(offset) {
        const date = new Date();
        date.setUTCDate(date.getUTCDate() + offset);
        return date.toISOString().slice(0, 10);
    }
    function selectNetworkDays(days, range) {
        const source = days && typeof days === 'object' ? days : {};
        if (range === 'all')
            return Object.keys(source).sort().map(key => ({ day: key, ...(source[key] || {}) }));
        const count = range === '30' ? 30 : 7;
        return Array.from({ length: count }, (_, index) => {
            const day = utcDay(index - count + 1);
            return { day, ...(source[day] || {}) };
        });
    }
    function totalBytes(item) { return (Number(item?.uploadBytes) || 0) + (Number(item?.downloadBytes) || 0); }
    function sumNetwork(entries) {
        const interfaces = {};
        let uploadBytes = 0;
        let downloadBytes = 0;
        let localUploadBytes = 0;
        let localDownloadBytes = 0;
        entries.forEach(entry => {
            uploadBytes += Number(entry.uploadBytes) || 0;
            downloadBytes += Number(entry.downloadBytes) || 0;
            localUploadBytes += Number(entry.localUploadBytes) || 0;
            localDownloadBytes += Number(entry.localDownloadBytes) || 0;
            Object.entries(entry.interfaces || {}).forEach(([name, value]) => {
                interfaces[name] ||= { uploadBytes: 0, downloadBytes: 0 };
                interfaces[name].uploadBytes += Number(value.uploadBytes) || 0;
                interfaces[name].downloadBytes += Number(value.downloadBytes) || 0;
            });
        });
        return { uploadBytes, downloadBytes, localUploadBytes, localDownloadBytes, interfaces };
    }
    function metric(label, value, caption, className = '') {
        const card = document.createElement('div');
        card.className = `analytics-metric ${className}`;
        const name = document.createElement('span');
        name.className = 'analytics-metric-label';
        name.textContent = label;
        const strong = document.createElement('strong');
        strong.className = 'analytics-metric-value';
        strong.textContent = value;
        const small = document.createElement('small');
        small.className = 'analytics-metric-caption';
        small.textContent = caption;
        card.append(name, strong, small);
        return card;
    }
    function sectionTitle(text) {
        const title = document.createElement('h3');
        title.className = 'analytics-section-title';
        title.textContent = text;
        return title;
    }
    function toolbar(onRange, onRefresh) {
        const element = document.createElement('div');
        element.className = 'analytics-toolbar';
        const ranges = document.createElement('div');
        ranges.className = 'analytics-range';
        ranges.setAttribute('role', 'group');
        ranges.setAttribute('aria-label', t('统计范围'));
        let selected = '7';
        [['7', t('近 7 天')], ['30', t('近 30 天')], ['all', t('全部')]].forEach(([value, label]) => {
            const button = document.createElement('button');
            button.type = 'button';
            button.textContent = label;
            button.dataset.range = value;
            button.setAttribute('aria-pressed', String(value === selected));
            button.addEventListener('click', () => {
                selected = value;
                ranges.querySelectorAll('button').forEach(item => item.setAttribute('aria-pressed', String(item.dataset.range === value)));
                onRange(value);
            });
            ranges.append(button);
        });
        const refresh = document.createElement('button');
        refresh.type = 'button';
        refresh.className = 'analytics-refresh';
        refresh.textContent = t('刷新');
        refresh.addEventListener('click', onRefresh);
        element.append(ranges, refresh);
        return { element, range: () => selected };
    }
    function statusNode() { const node = document.createElement('p'); node.className = 'analytics-status'; node.setAttribute('role', 'status'); return node; }
    function networkChart(entries) {
        const wrap = document.createElement('div');
        wrap.className = 'network-chart';
        const width = 760;
        const height = 240;
        const left = 64;
        const right = 18;
        const top = 18;
        const bottom = 40;
        const plotWidth = width - left - right;
        const plotHeight = height - top - bottom;
        const maximum = Math.max(1, ...entries.map(totalBytes));
        const step = plotWidth / Math.max(1, entries.length);
        const barWidth = Math.max(2, Math.min(18, step * 0.62));
        const svg = document.createElementNS('http://www.w3.org/2000/svg', 'svg');
        svg.setAttribute('viewBox', `0 0 ${width} ${height}`);
        svg.classList.add('network-chart-svg');
        svg.setAttribute('role', 'img');
        svg.setAttribute('aria-label', t('按日上传与下载网络流量图'));
        const title = document.createElementNS(svg.namespaceURI, 'title');
        title.textContent = t('按日上传与下载网络流量图');
        svg.append(title);
        [0, .5, 1].forEach(ratio => {
            const y = top + plotHeight * (1 - ratio);
            const line = document.createElementNS(svg.namespaceURI, 'line');
            line.setAttribute('x1', String(left));
            line.setAttribute('x2', String(width - right));
            line.setAttribute('y1', String(y));
            line.setAttribute('y2', String(y));
            line.classList.add('analytics-grid-line');
            svg.append(line);
            const text = document.createElementNS(svg.namespaceURI, 'text');
            text.setAttribute('x', String(left - 9));
            text.setAttribute('y', String(y + 4));
            text.setAttribute('text-anchor', 'end');
            text.classList.add('analytics-axis-label');
            text.textContent = formatBytes(maximum * ratio);
            svg.append(text);
        });
        entries.forEach((entry, index) => {
            const upload = Number(entry.uploadBytes) || 0;
            const download = Number(entry.downloadBytes) || 0;
            const total = upload + download;
            const x = left + step * index + (step - barWidth) / 2;
            const uploadHeight = upload ? Math.max(1, plotHeight * upload / maximum) : 0;
            const downloadHeight = download ? Math.max(1, plotHeight * download / maximum) : 0;
            const group = document.createElementNS(svg.namespaceURI, 'g');
            const tooltip = document.createElementNS(svg.namespaceURI, 'title');
            tooltip.textContent = `${entry.day}\n${t('上传')} ${formatBytes(upload)}\n${t('下载')} ${formatBytes(download)}\n${t('外部总流量')} ${formatBytes(total)}`;
            group.append(tooltip);
            if (downloadHeight) {
                const rect = document.createElementNS(svg.namespaceURI, 'rect');
                rect.setAttribute('x', String(x));
                rect.setAttribute('width', String(barWidth));
                rect.setAttribute('y', String(top + plotHeight - downloadHeight));
                rect.setAttribute('height', String(downloadHeight));
                rect.setAttribute('rx', '2');
                rect.classList.add('network-bar-download');
                group.append(rect);
            }
            if (uploadHeight) {
                const rect = document.createElementNS(svg.namespaceURI, 'rect');
                rect.setAttribute('x', String(x));
                rect.setAttribute('width', String(barWidth));
                rect.setAttribute('y', String(top + plotHeight - downloadHeight - uploadHeight));
                rect.setAttribute('height', String(uploadHeight));
                rect.setAttribute('rx', '2');
                rect.classList.add('network-bar-upload');
                group.append(rect);
            }
            svg.append(group);
            const every = Math.max(1, Math.ceil(entries.length / 7));
            if (index % every === 0 || index === entries.length - 1) {
                const text = document.createElementNS(svg.namespaceURI, 'text');
                text.setAttribute('x', String(left + step * index + step / 2));
                text.setAttribute('y', String(height - 13));
                text.setAttribute('text-anchor', 'middle');
                text.classList.add('analytics-axis-label');
                text.textContent = entry.day.slice(5);
                svg.append(text);
            }
        });
        wrap.append(svg);
        const legend = document.createElement('div');
        legend.className = 'analytics-legend';
        const upload = document.createElement('span');
        upload.className = 'is-upload';
        upload.textContent = t('上传');
        const download = document.createElement('span');
        download.className = 'is-download';
        download.textContent = t('下载');
        legend.append(upload, download);
        wrap.append(legend);
        return wrap;
    }
    function renderNetwork(options) {
        translate = options.t || translate;
        const api = options.api;
        const control = document.createElement('div');
        control.className = 'control-value usage-analytics network-usage-analytics';
        const tools = toolbar(() => { if (lastData)
            paint(lastData); }, () => { void load(); });
        const status = statusNode();
        const content = document.createElement('div');
        content.className = 'analytics-content';
        control.append(tools.element, status, content);
        let lastData = null;
        const serial = ++requestSequence;
        function paint(data) {
            lastData = data;
            const entries = selectNetworkDays(data.days, tools.range());
            const totals = sumNetwork(entries);
            content.replaceChildren();
            const metrics = document.createElement('div');
            metrics.className = 'analytics-metrics';
            metrics.append(metric(t('外部总流量'), formatBytes(totalBytes(totals)), tools.range() === 'all' ? t('全部记录') : t('近 {0} 天', tools.range()), 'is-total'), metric(t('上传'), formatBytes(totals.uploadBytes), t('Aide 对外发送（已扣除页面连接）')), metric(t('下载'), formatBytes(totals.downloadBytes), t('Aide 从外部接收（已扣除页面连接）')), metric(t('页面连接（已排除）'), formatBytes(totals.localUploadBytes + totals.localDownloadBytes), t('工作台 HTTP 收发，不计入外部流量')));
            content.append(metrics, sectionTitle(t('每日流量趋势')), networkChart(entries));
            content.append(sectionTitle(t('网络接口')));
            const interfaces = Object.entries(totals.interfaces || {}).sort((a, b) => totalBytes(b[1]) - totalBytes(a[1]));
            if (!interfaces.length) {
                const empty = document.createElement('p');
                empty.className = 'analytics-empty';
                empty.textContent = data.available ? t('当前范围还没有网络流量记录。') : t('正在等待可用的网络计数。');
                content.append(empty);
            }
            else {
                const list = document.createElement('div');
                list.className = 'network-interface-list';
                interfaces.forEach(([name, value]) => {
                    const row = document.createElement('div');
                    row.className = 'network-interface-row';
                    const label = document.createElement('strong');
                    label.textContent = name;
                    const upload = document.createElement('span');
                    upload.textContent = `${t('上传')} ${formatBytes(value.uploadBytes)}`;
                    const download = document.createElement('span');
                    download.textContent = `${t('下载')} ${formatBytes(value.downloadBytes)}`;
                    const total = document.createElement('b');
                    total.textContent = formatBytes(totalBytes(value));
                    row.append(label, upload, download, total);
                    list.append(row);
                });
                content.append(list);
            }
            const note = document.createElement('p');
            note.className = 'analytics-note';
            note.textContent = t('按网卡增量估算 Aide 对外通信，并扣除工作台页面 HTTP 收发；包含模型、插件、媒体、实时连接和 SSH。每 5 秒采样，按 UTC 日期归档，不等于运营商计费。');
            content.append(note);
            if (data.legacyExcluded) {
                const warning = document.createElement('p');
                warning.className = 'analytics-warning';
                warning.textContent = t('旧版本混合了页面本机流量，已从当前趋势中排除；原始账本仍保留。');
                content.append(warning);
            }
            if (data.persistenceWarning) {
                const warning = document.createElement('p');
                warning.className = 'analytics-warning';
                warning.textContent = t('历史统计文件暂时无法保存，当前新增数据仅保留在内存中。');
                content.append(warning);
            }
            status.textContent = data.supported && data.available ? `${t('更新于')} ${formatTime(data.sampledAt)}` : t('当前运行环境无法读取 Aide 网络接口计数，未使用 Token 估算替代。');
        }
        async function load() {
            control.setAttribute('aria-busy', 'true');
            status.textContent = t('正在读取网络流量…');
            try {
                const data = await api('/network-usage');
                if (serial !== requestSequence && !control.isConnected)
                    return;
                paint(data);
            }
            catch (error) {
                status.textContent = `${t('网络流量读取失败')}：${error instanceof Error ? error.message : String(error)}`;
            }
            finally {
                control.removeAttribute('aria-busy');
            }
        }
        void load();
        const timer = window.setInterval(() => {
            if (!control.isConnected) {
                window.clearInterval(timer);
                return;
            }
            if (!document.hidden)
                void load();
        }, 30000);
        return control;
    }
    const storageLabels = {
        sessions: '会话与运行记录', memory: '记忆数据', knowledge: '知识索引', config: '配置', stats: '统计数据', audit: '审计日志', plugins: '插件', messages: '消息与协作', market: '插件市场数据', assistant: '小秘数据', '_root': '根目录文件',
    };
    function storageGroup(id) { const source = storageLabels[id] || id; return t(source); }
    function renderStorage(options) {
        translate = options.t || translate;
        const api = options.api;
        const control = document.createElement('div');
        control.className = 'control-value usage-analytics storage-usage-analytics';
        const actions = document.createElement('div');
        actions.className = 'analytics-toolbar is-end';
        const refresh = document.createElement('button');
        refresh.type = 'button';
        refresh.className = 'analytics-refresh';
        refresh.textContent = t('重新扫描');
        refresh.addEventListener('click', () => { void load(); });
        actions.append(refresh);
        const status = statusNode();
        const content = document.createElement('div');
        content.className = 'analytics-content';
        control.append(actions, status, content);
        async function load() {
            control.setAttribute('aria-busy', 'true');
            refresh.disabled = true;
            status.textContent = t('正在扫描存储占用，大型工作区可能需要一些时间…');
            try {
                const data = await api('/storage-usage');
                content.replaceChildren();
                const capacity = data.capacity || {};
                const metrics = document.createElement('div');
                metrics.className = 'analytics-metrics';
                metrics.append(metric(t('Aide 数据'), formatBytes(data.data?.logicalBytes), t('{0} 个文件', Number(data.data?.files) || 0), 'is-total'), metric(t('当前工作区'), formatBytes(data.workspace?.logicalBytes), t('{0} 个文件', Number(data.workspace?.files) || 0)), metric(t('磁盘可用空间'), Number(capacity.dataAvailableBytes) >= 0 ? formatBytes(capacity.dataAvailableBytes) : '—', t('Aide 数据所在磁盘')));
                content.append(metrics, sectionTitle(t('Aide 数据构成')));
                const children = Array.isArray(data.data?.children) ? data.data.children : [];
                if (!children.length) {
                    const empty = document.createElement('p');
                    empty.className = 'analytics-empty';
                    empty.textContent = t('还没有可统计的 Aide 数据。');
                    content.append(empty);
                }
                else {
                    const list = document.createElement('div');
                    list.className = 'storage-category-list';
                    const maximum = Math.max(1, ...children.map((item) => Number(item.logicalBytes) || 0));
                    children.forEach((item) => {
                        const row = document.createElement('div');
                        row.className = 'storage-category-row';
                        const head = document.createElement('div');
                        head.className = 'storage-category-head';
                        const label = document.createElement('strong');
                        label.textContent = storageGroup(String(item.id));
                        const files = document.createElement('span');
                        files.textContent = t('{0} 个文件', Number(item.files) || 0);
                        const size = document.createElement('b');
                        size.textContent = formatBytes(item.logicalBytes);
                        head.append(label, files, size);
                        const track = document.createElement('div');
                        track.className = 'storage-category-track';
                        track.setAttribute('aria-hidden', 'true');
                        const bar = document.createElement('span');
                        bar.style.width = `${Math.max(1, (Number(item.logicalBytes) || 0) / maximum * 100)}%`;
                        track.append(bar);
                        row.append(head, track);
                        list.append(row);
                    });
                    content.append(list);
                }
                content.append(sectionTitle(t('工作区与容量')));
                const workspace = document.createElement('div');
                workspace.className = 'storage-workspace-card';
                const row = document.createElement('div');
                row.className = 'storage-workspace-row';
                const name = document.createElement('strong');
                name.textContent = t('当前工作区文件');
                const available = document.createElement('span');
                available.textContent = Number(capacity.workspaceAvailableBytes) >= 0 ? `${t('可用')} ${formatBytes(capacity.workspaceAvailableBytes)}` : '';
                const size = document.createElement('b');
                size.textContent = formatBytes(data.workspace?.logicalBytes);
                row.append(name, available, size);
                workspace.append(row);
                content.append(workspace);
                const note = document.createElement('p');
                note.className = 'analytics-note';
                note.textContent = t('按文件逻辑大小统计，不跟随符号链接，不读取文件内容，也不会自动清理任何数据。工作区不重复计入 .aide-data；可用空间来自对应文件系统。');
                content.append(note);
                const errors = (Number(data.data?.errors) || 0) + (Number(data.workspace?.errors) || 0);
                if (errors) {
                    const warning = document.createElement('p');
                    warning.className = 'analytics-warning';
                    warning.textContent = t('有 {0} 个项目无法读取，未计入上述总量。', errors);
                    content.append(warning);
                }
                status.textContent = `${t('扫描完成于')} ${formatTime(data.generatedAt)}`;
            }
            catch (error) {
                status.textContent = `${t('存储扫描失败')}：${error instanceof Error ? error.message : String(error)}`;
            }
            finally {
                control.removeAttribute('aria-busy');
                refresh.disabled = false;
            }
        }
        void load();
        return control;
    }
    window.AideUsageAnalytics = { renderNetwork, renderStorage, formatBytes, selectNetworkDays };
})();
