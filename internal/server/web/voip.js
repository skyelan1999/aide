"use strict";
(() => {
    'use strict';
    function open({ mode, token, permission, context, onStatus, onEnd }) {
        const ownsContext = !context;
        const AudioContext = window.AudioContext || window.webkitAudioContext;
        context ||= new AudioContext();
        let socket, node, microphone, clock, closed = false, answered = false;
        const sources = new Map();
        let resolveReady, rejectReady;
        const ready = new Promise((resolve, reject) => { resolveReady = resolve; rejectReady = reject; });
        function close(message = '') {
            if (closed)
                return;
            closed = true;
            if (socket) {
                socket.onmessage = socket.onclose = socket.onerror = null;
                if (socket.readyState === WebSocket.OPEN) {
                    try {
                        socket.send(JSON.stringify({ op: 'hangup' }));
                    }
                    catch (_) { }
                }
                socket.close();
            }
            for (const source of sources.values())
                source.disconnect();
            sources.clear();
            if (node) {
                node.port.onmessage = null;
                node.port.close();
                node.disconnect();
            }
            clock?.disconnect();
            microphone?.stream.getTracks().forEach((track) => track.stop());
            if (ownsContext)
                void context.close().catch(() => { });
            if (answered)
                onEnd?.(message);
            else
                rejectReady(new Error(message || '话机通话已取消'));
        }
        async function connect() {
            try {
                await Promise.all([context.resume(), context.audioWorklet.addModule('/voip-pcm-worklet.js?v=2')]);
                if (closed)
                    return;
                microphone = context.createMediaStreamDestination();
                node = new AudioWorkletNode(context, 'aide-voip-audio', { channelCount: 1, outputChannelCount: [1] });
                node.connect(microphone);
                // Keep the graph ticking during ringing without playing through the Mac.
                clock = context.createGain();
                clock.gain.value = 0;
                node.connect(clock).connect(context.destination);
                node.port.onmessage = ({ data }) => {
                    if (!answered || socket.readyState !== WebSocket.OPEN || socket.bufferedAmount > 8192)
                        return;
                    const pcm = btoa(String.fromCharCode(...new Uint8Array(data)));
                    socket.send(JSON.stringify({ op: 'audio', pcm }));
                };
                const url = new URL('/api/voip/socket', location.href);
                url.protocol = url.protocol === 'https:' ? 'wss:' : 'ws:';
                url.search = new URLSearchParams({ mode, ...permission }).toString();
                socket = new WebSocket(url, ['aide-voip', 'aide-auth.' + token]);
                socket.onmessage = ({ data }) => {
                    try {
                        const e = JSON.parse(data);
                        switch (e.op) {
                            case 'dialing':
                                onStatus?.('正在呼叫局域网话机');
                                break;
                            case 'ringing':
                                onStatus?.('话机响铃，请摘机');
                                break;
                            case 'answered':
                                if (!answered) {
                                    answered = true;
                                    resolveReady();
                                }
                                break;
                            case 'audio': {
                                if (!answered)
                                    break;
                                const frame = Uint8Array.from(atob(e.pcm), (c) => c.charCodeAt(0)).buffer;
                                node.port.postMessage(frame, [frame]);
                                break;
                            }
                            case 'ended':
                                close(e.error || '');
                                break;
                        }
                    }
                    catch (_) {
                        close('话机音频连接异常');
                    }
                };
                socket.onerror = () => close('无法连接本机 VoIP 组件');
                socket.onclose = () => close('话机连接已结束');
            }
            catch (error) {
                close(error.message || '无法初始化话机音频');
            }
        }
        void connect();
        return {
            ready,
            close,
            get stream() { return microphone.stream; },
            get speaker() { return node; },
            attach(peer, stream) {
                this.detach(peer);
                const source = context.createMediaStreamSource(stream);
                source.connect(node);
                sources.set(peer, source);
            },
            detach(peer) {
                sources.get(peer)?.disconnect();
                sources.delete(peer);
            }
        };
    }
    function settings(api, t, gateway) {
        const form = document.createElement('form');
        form.className = 'settings-control voip-control';
        form.noValidate = true;
        const make = (tag, className, text = '') => {
            const node = document.createElement(tag);
            if (className)
                node.className = className;
            if (text)
                node.textContent = t(text);
            return node;
        };
        const header = make('header', 'voip-header');
        const titleRow = make('div', 'voip-title-row');
        const titleGroup = make('div', 'voip-title-group');
        const title = make('h4', '', 'VoIP 话机接管');
        title.id = 'voip-settings-title';
        titleGroup.append(title, make('p', 'section-desc', '同一局域网内直拨话机。打开对应通话后，话机会响铃，摘机后使用话机收发语音。'));
        const badge = make('span', 'voip-runtime-badge', '正在检测');
        badge.dataset.state = 'loading';
        badge.setAttribute('role', 'status');
        titleRow.append(titleGroup, badge);
        const runtime = make('p', 'voip-runtime-desc', '正在读取本机 VoIP 组件状态…');
        header.append(titleRow, runtime);
        form.setAttribute('aria-labelledby', title.id);
        const section = (label, description) => {
            const panel = make('section', 'voip-section');
            const heading = make('div', 'voip-section-heading');
            heading.append(make('h5', '', label), make('p', '', description));
            panel.append(heading);
            return panel;
        };
        const access = section('接管范围', '分别选择需要转移到话机的通话类型，关闭后仍在 AIDE R&D 中使用音频。');
        const box = () => Object.assign(document.createElement('input'), { type: 'checkbox' });
        const messages = box(), assistant = box();
        const toggle = (label, description, field) => {
            const row = make('label', 'voip-toggle-row');
            const copy = make('span', 'voip-toggle-copy');
            copy.append(make('strong', '', label), make('small', '', description));
            const control = make('span', 'switch');
            const slider = make('span', 'switch-slider');
            slider.setAttribute('aria-hidden', 'true');
            control.append(field, slider);
            row.append(copy, control);
            return row;
        };
        access.append(toggle('接管消息语音', '消息语音通话响起时，使用局域网话机接听和说话。', messages), toggle('接管助理电话', '助理实时通话建立后，使用话机完成双向语音。', assistant));
        const network = section('话机连接', '填写话机的局域网 SIP 地址。电脑与话机需连接到同一网络。');
        const grid = make('div', 'voip-network-grid');
        const ip = Object.assign(document.createElement('input'), { placeholder: '192.168.1.50', inputMode: 'decimal', maxLength: 15, autocomplete: 'off', spellcheck: false });
        const port = Object.assign(document.createElement('input'), { type: 'number', value: 5060, min: 1, max: 65535, step: 1, required: true, inputMode: 'numeric' });
        const user = Object.assign(document.createElement('input'), { maxLength: 64, placeholder: t('可留空'), autocomplete: 'off', spellcheck: false, pattern: '[A-Za-z0-9_.+-]{0,64}' });
        const field = (label, input, hint, className = '') => {
            const wrap = make('label', 'voip-field ' + className);
            wrap.append(make('span', 'voip-field-label', label), input, make('small', '', hint));
            return wrap;
        };
        grid.append(field('话机 IPv4 地址', ip, '仅支持局域网、回环或链路本地地址。', 'voip-ip-field'), field('SIP 端口', port, '默认 5060，范围 1–65535。', 'voip-port-field'), field('SIP 用户', user, '可留空；支持字母、数字和 . _ + -。', 'voip-user-field'));
        network.append(grid);
        const useGateway = make('button', 'quiet', '带入网关当前的话机地址和账号');
        useGateway.type = 'button';
        if (gateway)
            network.append(useGateway);
        useGateway.onclick = () => {
            if (loading || saving || !local)
                return;
            const info = gateway?.();
            if (!info?.running || !info.phone) {
                setStatus(t('请先刷新并确认电话网关运行中'), 'error');
                return;
            }
            ip.value = info.phone;
            port.value = String(info.phonePort || 5060);
            user.value = info.account;
            changed();
        };
        const footer = make('div', 'voip-footer');
        const status = make('p', 'voip-save-status', '正在读取话机设置…');
        status.setAttribute('role', 'status');
        status.setAttribute('aria-live', 'polite');
        const actions = make('div', 'voip-actions');
        const reset = Object.assign(document.createElement('button'), { type: 'button', className: 'quiet', textContent: t('恢复已保存') });
        const save = Object.assign(document.createElement('button'), { type: 'submit', className: 'primary', textContent: t('保存话机设置') });
        actions.append(reset, save);
        footer.append(status, actions);
        form.append(header, access, network, footer);
        const controls = [messages, assistant, ip, port, user];
        let savedConfig = null, local = false, loading = true, saving = false, dirty = false;
        const snapshot = () => ({ messages: messages.checked, assistant: assistant.checked,
            target: { ip: ip.value.trim(), port: Number(port.value), user: user.value.trim() } });
        const setStatus = (message, state = '') => {
            status.textContent = message;
            status.dataset.state = state;
        };
        const refreshControls = () => {
            const unavailable = loading || saving || !local;
            useGateway.disabled = unavailable;
            controls.forEach((field) => { field.disabled = unavailable; });
            reset.disabled = unavailable || !dirty;
            save.disabled = unavailable || !dirty;
            save.textContent = t(saving ? '正在保存…' : '保存话机设置');
            form.setAttribute('aria-busy', String(loading || saving));
        };
        const setRuntime = (config) => {
            const state = !config.local ? 'remote' : config.ready ? 'ready' : 'offline';
            badge.dataset.state = state;
            badge.textContent = t(state === 'remote' ? '仅本机可设置' : state === 'ready' ? '组件已连接' : '组件未连接');
            runtime.textContent = t(state === 'remote' ? '当前设备仅可查看，需在运行 AIDE R&D 的本机修改。' : state === 'ready' ? '本机 VoIP 组件已连接，新通话将使用此处保存的话机配置。' : '本机 VoIP 组件未连接，请用启动器重新启动 AIDE R&D');
        };
        const apply = (config) => {
            savedConfig = { messages: !!config.messages, assistant: !!config.assistant,
                target: { ip: config.target?.ip || '', port: config.target?.port || 5060, user: config.target?.user || '' }, local: !!config.local, ready: !!config.ready };
            messages.checked = savedConfig.messages;
            assistant.checked = savedConfig.assistant;
            ip.value = savedConfig.target.ip;
            port.value = savedConfig.target.port;
            user.value = savedConfig.target.user;
            local = savedConfig.local;
            dirty = false;
            setRuntime(savedConfig);
            refreshControls();
        };
        const changed = () => {
            controls.forEach((field) => field.setCustomValidity(''));
            dirty = !!savedConfig && JSON.stringify(snapshot()) !== JSON.stringify({ messages: savedConfig.messages, assistant: savedConfig.assistant, target: savedConfig.target });
            refreshControls();
            if (!loading && !saving && local)
                setStatus(t(dirty ? '有未保存的修改' : '所有更改已保存'), dirty ? 'dirty' : 'saved');
        };
        const validLocalIPv4 = (value) => {
            const parts = value.split('.');
            if (parts.length !== 4 || parts.some((part) => !/^(0|[1-9]\d{0,2})$/.test(part) || Number(part) > 255))
                return false;
            const numbers = parts.map(Number);
            return numbers[0] === 10 || numbers[0] === 127 || (numbers[0] === 169 && numbers[1] === 254) ||
                (numbers[0] === 172 && numbers[1] >= 16 && numbers[1] <= 31) || (numbers[0] === 192 && numbers[1] === 168);
        };
        const validate = () => {
            controls.forEach((field) => field.setCustomValidity(''));
            const value = snapshot();
            const invalid = (field, message) => {
                const text = t(message);
                field.setCustomValidity(text);
                field.reportValidity();
                field.focus();
                setStatus(text, 'error');
                return false;
            };
            if (!value.target.ip && (value.messages || value.assistant))
                return invalid(ip, '启用接管前请填写话机 IPv4 地址');
            if (value.target.ip && !validLocalIPv4(value.target.ip))
                return invalid(ip, '请输入局域网 IPv4 地址');
            if (!Number.isInteger(value.target.port) || value.target.port < 1 || value.target.port > 65535)
                return invalid(port, 'SIP 端口需为 1–65535 的整数');
            if (!/^[A-Za-z0-9_.+-]{0,64}$/.test(value.target.user))
                return invalid(user, 'SIP 用户只能使用字母、数字和 . _ + -');
            return true;
        };
        form.addEventListener('input', changed);
        form.addEventListener('change', (event) => { if (event.target.type === 'checkbox')
            changed(); });
        reset.addEventListener('click', () => {
            if (!savedConfig || saving || !local)
                return;
            apply(savedConfig);
            setStatus(t('已恢复为上次保存的设置'), 'saved');
        });
        form.addEventListener('submit', async (event) => {
            event.preventDefault();
            if (loading || saving || !local || !dirty || !validate())
                return;
            saving = true;
            refreshControls();
            setStatus(t('正在保存话机设置…'), 'saving');
            try {
                const body = JSON.stringify(snapshot());
                const config = await api('/voip/settings', { method: 'PUT', body });
                apply(config);
                setStatus(t('已保存，话机配置已生效'), 'success');
            }
            catch (error) {
                setStatus(t(error.message), 'error');
            }
            finally {
                saving = false;
                refreshControls();
            }
        });
        refreshControls();
        api('/voip/settings').then((config) => {
            loading = false;
            apply(config);
            setStatus(t(config.local ? '所有更改已保存' : '当前设备为只读'), config.local ? 'saved' : 'remote');
        }).catch((error) => {
            loading = false;
            local = false;
            badge.dataset.state = 'error';
            badge.textContent = t('读取失败');
            runtime.textContent = t('无法获取 VoIP 组件状态，请重新打开设置后重试。');
            setStatus(t(error.message), 'error');
            refreshControls();
        });
        return form;
    }
    window.AideVoIP = Object.freeze({ open, settings });
})();
