interface Window {
  AideVoIP?: any;
}
(() => {
  'use strict';

  function open({mode, token, permission, context, onStatus, onEnd}: any) {
    const ownsContext = !context;
    const AudioContext = window.AudioContext || window.webkitAudioContext;
    context ||= new AudioContext();
    let socket: any, node: any, microphone: any, clock: any, closed = false, answered = false;
    const sources = new Map<any, any>();
    let resolveReady: any, rejectReady: any;
    const ready = new Promise<void>((resolve, reject) => { resolveReady = resolve; rejectReady = reject; });

    function close(message = ''): void {
      if (closed) return;
      closed = true;
      if (socket) {
        socket.onmessage = socket.onclose = socket.onerror = null;
        if (socket.readyState === WebSocket.OPEN) {
          try { socket.send(JSON.stringify({op: 'hangup'})); } catch (_) {}
        }
        socket.close();
      }
      for (const source of sources.values()) source.disconnect();
      sources.clear();
      if (node) {
        node.port.onmessage = null;
        node.port.close();
        node.disconnect();
      }
      clock?.disconnect();
      microphone?.stream.getTracks().forEach((track: any) => track.stop());
      if (ownsContext) void context.close().catch(() => {});
      if (answered) onEnd?.(message);
      else rejectReady(new Error(message || '话机通话已取消'));
    }

    async function connect(): Promise<void> {
      try {
        await Promise.all([context.resume(), context.audioWorklet.addModule('/voip-pcm-worklet.js?v=2')]);
        if (closed) return;
        microphone = context.createMediaStreamDestination();
        node = new AudioWorkletNode(context, 'aide-voip-audio', {channelCount: 1, outputChannelCount: [1]});
        node.connect(microphone);
        // Keep the graph ticking during ringing without playing through the Mac.
        clock = context.createGain();
        clock.gain.value = 0;
        node.connect(clock).connect(context.destination);
        node.port.onmessage = ({data}: any): void => {
          if (!answered || socket.readyState !== WebSocket.OPEN || socket.bufferedAmount > 8192) return;
          const pcm = btoa(String.fromCharCode(...new Uint8Array(data)));
          socket.send(JSON.stringify({op: 'audio', pcm}));
        };
        const url = new URL('/api/voip/socket', location.href);
        url.protocol = url.protocol === 'https:' ? 'wss:' : 'ws:';
        url.search = new URLSearchParams({mode, ...permission}).toString();
        socket = new WebSocket(url, ['aide-voip', 'aide-auth.' + token]);
        socket.onmessage = ({data}: any): void => {
          try {
            const e = JSON.parse(data);
            switch (e.op) {
              case 'dialing': onStatus?.('正在呼叫局域网话机'); break;
              case 'ringing': onStatus?.('话机响铃，请摘机'); break;
              case 'answered':
                if (!answered) {
                  answered = true;
                  resolveReady();
                }
                break;
              case 'audio': {
                if (!answered) break;
                const frame = Uint8Array.from(atob(e.pcm), (c: any) => c.charCodeAt(0)).buffer;
                node.port.postMessage(frame, [frame]);
                break;
              }
              case 'ended': close(e.error || ''); break;
            }
          } catch (_) {
            close('话机音频连接异常');
          }
        };
        socket.onerror = () => close('无法连接本机 VoIP 组件');
        socket.onclose = () => close('话机连接已结束');
      } catch (error: any) {
        close(error.message || '无法初始化话机音频');
      }
    }

    void connect();
    return {
      ready,
      close,
      get stream() { return microphone.stream; },
      get speaker() { return node; },
      attach(peer: any, stream: any): void {
        this.detach(peer);
        const source = context.createMediaStreamSource(stream);
        source.connect(node);
        sources.set(peer, source);
      },
      detach(peer: any): void {
        sources.get(peer)?.disconnect();
        sources.delete(peer);
      }
    };
  }

  function settings(api: any, t: any) {
    const form = document.createElement('form');
    form.className = 'settings-control voip-control';
    const title = document.createElement('h4');
    title.textContent = t('VoIP 话机接管');
    const line = (label: any, field: any) => {
      const row = document.createElement('label');
      row.className = 'voice-reply-row';
      const text = document.createElement('span');
      text.textContent = t(label);
      row.append(text, field);
      return row;
    };
    const box = () => Object.assign(document.createElement('input'), {type: 'checkbox'});
    const messages = box(), assistant = box();
    const ip = Object.assign(document.createElement('input'), {placeholder: '192.168.1.50', inputMode: 'decimal', maxLength: 15});
    const port: any = Object.assign(document.createElement('input'), {type: 'number', value: 5060, min: 1, max: 65535});
    const user = Object.assign(document.createElement('input'), {maxLength: 64, placeholder: t('可留空')});
    const save = Object.assign(document.createElement('button'), {type: 'submit', className: 'primary', textContent: t('保存话机设置')});
    const status = document.createElement('p'), help = document.createElement('p');
    status.className = help.className = 'section-desc';
    status.setAttribute('role', 'status');
    help.textContent = t('同一局域网内直拨话机。打开对应通话后，话机会响铃，摘机后使用话机收发语音。');
    form.append(title, help, line('接管消息语音', messages), line('接管助理电话', assistant), line('话机 IPv4 地址', ip),
      line('SIP 端口', port), line('SIP 用户', user), save, status);
    const controls = [messages, assistant, ip, port, user, save];
    const disabled = (value: any): void => controls.forEach((field: any) => { field.disabled = value; });
    function apply(config: any) {
      messages.checked = config.messages;
      assistant.checked = config.assistant;
      ip.value = config.target.ip;
      port.value = config.target.port || 5060;
      user.value = config.target.user;
      return t(!config.local ? '请在本机设置话机' : config.ready ? '本机 VoIP 组件已连接' : '本机 VoIP 组件未连接，请用启动器重新启动 Aide');
    }
    disabled(true);
    api('/voip/settings').then((config: any) => {
      status.textContent = apply(config);
      disabled(!config.local);
    }).catch((error: any) => { status.textContent = t(error.message); });
    form.addEventListener('submit', async (event: any): Promise<void> => {
      event.preventDefault();
      disabled(true);
      try {
        const body = JSON.stringify({messages: messages.checked, assistant: assistant.checked,
          target: {ip: ip.value.trim(), port: Number(port.value || 5060), user: user.value.trim()}});
        status.textContent = t('已保存') + ' · ' + apply(await api('/voip/settings', {method: 'PUT', body}));
      } catch (error: any) {
        status.textContent = t(error.message);
      } finally {
        disabled(false);
      }
    });
    return form;
  }

  window.AideVoIP = Object.freeze({open, settings});
})();
