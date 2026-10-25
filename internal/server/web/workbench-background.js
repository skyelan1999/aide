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
  const accepted = new Set(['video/mp4', 'video/webm', 'image/gif', 'image/png', 'image/jpeg', 'image/webp']);
  let mode = '', generation = 0, media = null, url = '', record = null, message = '', saving = false;
  const panels = new Set();
  let database;
  function db() {
    if (!database) database = new Promise((resolve, reject) => {
      if (!window.indexedDB) return reject(new Error(uiTextWorkbenchBackground('本地背景存储不可用')));
      const request = indexedDB.open('aide.workbench-background', 1);
      request.onupgradeneeded = () => request.result.createObjectStore('assets');
      request.onsuccess = () => { request.result.onversionchange = () => { request.result.close(); database = null; }; resolve(request.result); };
      request.onerror = () => reject(request.error);
      request.onblocked = () => reject(new Error(uiTextWorkbenchBackground('本地背景存储不可用')));
    }).catch(error => { database = null; throw error; });
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
  function blocked() {
    return document.hidden || root.dataset.motion === 'reduced' || reduced?.matches ||
      !!document.querySelector('#login-dialog[open],#lock-screen:not([hidden])');
  }
  function lifecycle() {
    const paused = blocked();
    root.toggleAttribute('data-background-paused', paused);
    if (!media) return;
    if (media.tagName === 'VIDEO') {
      if (paused) media.pause();
      else { const play = media.play(); play?.catch(() => {}); }
    } else if (record?.type === 'image/gif' || record?.type === 'image/webp') {
      // GIF/WebP can animate without a playback API. Unload while paused.
      if (paused) media.removeAttribute('src');
      else if (!media.hasAttribute('src')) media.src = url;
    }
  }
  function release() {
    if (media?.tagName === 'VIDEO') { media.pause(); media.removeAttribute('src'); media.load(); }
    media?.remove(); media = null;
    if (url.startsWith('blob:')) URL.revokeObjectURL(url);
    url = ''; layer.replaceChildren(); record = null;
    root.removeAttribute('data-background-media');
  }
  function refreshPanels() {
    for (const panel of panels) {
      if (!panel.node.isConnected) { panels.delete(panel); continue; }
      panel.update();
    }
  }
  async function apply(force = false) {
    const next = window.aideUI?.get('background') || 'off';
    const strength = window.aideUI?.get('backgroundStrength');
    root.style?.setProperty('--wb-background-strength', String(typeof strength === 'number' && Number.isFinite(strength) ? Math.min(50, Math.max(10, strength)) / 100 : .22));
    if (next === mode && !force) { lifecycle(); refreshPanels(); return; }
    const ticket = ++generation;
    mode = next; release(); message = '';
    root.dataset.background = mode;
    lifecycle(); refreshPanels();
    if (mode === 'aurora') {
      for (const variant of ['primary', 'secondary']) {
        const glow = document.createElement('span');
        glow.className = 'workbench-aurora workbench-aurora-' + variant;
        layer.append(glow);
      }
      return;
    }
    if(mode==='studio'){
      for(const name of ['plane','line','halo']){const node=document.createElement('span');node.className='workbench-studio-'+name;layer.append(node);}return;
    }
    if (mode !== 'local') return;
    try {
      const asset = await stored('get');
      if (ticket !== generation) return;
      if ((!asset?.blob && !asset?.src) || !accepted.has(asset.type)) { message = '请先选择本地背景素材'; refreshPanels(); return; }
      record = asset;
      url = asset.src || URL.createObjectURL(asset.blob);
      const video = asset.type.startsWith('video/');
      media = document.createElement(video ? 'video' : 'img');
      if (video) { media.muted = true; media.defaultMuted = true; media.loop = true; media.playsInline = true; media.preload = 'metadata'; }
      else media.alt = '';
      media.onerror = () => { if (ticket === generation) { release(); message = '背景素材无法播放，请更换文件'; refreshPanels(); } };
      root.dataset.backgroundMedia = video ? 'video' : (asset.type === 'image/gif' || asset.type === 'image/webp') ? 'animated' : 'image';
      layer.append(media);
      if (video || !blocked() || root.dataset.backgroundMedia === 'image') media.src = url;
      lifecycle(); refreshPanels();
    } catch (_) {
      if (ticket === generation) { message = '本地背景存储不可用'; refreshPanels(); }
    }
  }
  function renderSettings({t = text => text} = {}) {
    refreshPanels(); // Retire detached settings nodes on every visit, even without a preference change.
    const node = document.createElement('div'); node.className = 'settings-control background-control';
    const label = document.createElement('div'); label.className = 'control-label'; label.textContent = t('工作台背景');
    const choices = document.createElement('div'); choices.className = 'background-options'; choices.setAttribute('role', 'group'); choices.setAttribute('aria-label', t('工作台背景'));
    const buttons = [['off', '关闭'], ['aurora', '流光'], ['studio', '静域'], ['local', '本地素材']].map(([value, text]) => {
      const button = document.createElement('button'); button.type = 'button'; button.dataset.value = value; button.textContent = t(text);
      button.onclick = () => window.aideUI?.set('background', value); choices.append(button); return button;
    });
    const picker = document.createElement('label'); picker.className = 'background-picker';
    const text = document.createElement('span'); text.textContent = t('加载本地背景');
    const input = document.createElement('input'); input.type = 'file'; input.accept = [...accepted].join(','); input.setAttribute('aria-label', t('加载本地背景'));
    const remove = document.createElement('button'); remove.type = 'button'; remove.className = 'quiet'; remove.textContent = t('删除本地素材');
    const status = document.createElement('p'); status.className = 'background-status'; status.setAttribute('role', 'status');
    const note = document.createElement('p'); note.className = 'muted'; note.textContent = t('静域为轻量原生背景，随明暗主题自适应。自选素材支持 MP4、WebM、GIF、PNG、JPEG、WebP，最多 30 MiB，仅保存在当前浏览器。后台、锁屏及减少动态效果时暂停。');
    const intensity = document.createElement('label'); intensity.className = 'background-intensity';
    const intensityText = document.createElement('span'); intensityText.textContent = t('背景强度');
    const range = document.createElement('input'); range.type = 'range'; range.min = '10'; range.max = '50'; range.step = '1'; range.setAttribute('aria-label', t('背景强度'));
    const value = document.createElement('output');
    intensity.append(intensityText, range, value);
    range.oninput = () => window.aideUI?.set('backgroundStrength', Number(range.value));
    const panel = {node, update() {
      buttons.forEach(button => { button.setAttribute('aria-pressed', String(mode === button.dataset.value)); button.disabled = saving; });
      input.disabled = remove.disabled = saving;
      range.disabled = mode === 'off';
      range.value = String(window.aideUI?.get('backgroundStrength') ?? 22);
      value.textContent = range.value + '%';
      range.setAttribute('aria-valuetext', value.textContent);
      status.textContent = saving ? t('正在保存背景…') : message ? t(message) : mode === 'studio' ? t('静域 · 柔光与几何层次') : record?.name || t('选择本地素材后自动启用');
    }};
    input.onchange = async () => {
      const file = input.files?.[0]; input.value = ''; if (!file || saving) return;
      if (!accepted.has(file.type) || file.size > 30 * 1024 * 1024 || file.size === 0) { message = '请选择支持的素材格式，大小不超过 30 MiB'; refreshPanels(); return; }
      saving = true; refreshPanels();
      try {
        await stored('put', {blob:file, name:file.name, type:file.type});
        window.aideUI?.set('background', 'local'); await apply(true);
        channel?.postMessage('changed');
      } catch (_) { message = '背景保存失败，原素材已保留'; }
      finally { saving = false; refreshPanels(); }
    };
    remove.onclick = async () => {
      if (saving) return; saving = true; refreshPanels();
      try { await stored('delete'); if (mode === 'local') window.aideUI?.set('background', 'off'); channel?.postMessage('changed'); message = '本地背景素材已删除'; }
      catch (_) { message = '本地背景存储不可用'; }
      finally { saving = false; refreshPanels(); }
    };
    picker.append(text, input); node.append(label, choices, picker, remove, status, note, intensity);
    panels.add(panel); panel.update(); return node;
  }
  const channel = typeof BroadcastChannel === 'function' ? new BroadcastChannel('aide.background') : null;
  if (channel) channel.onmessage = () => { if (mode === 'local') apply(true); };
  document.addEventListener('visibilitychange', lifecycle);
  window.addEventListener('aide:motion', lifecycle);
  reduced?.addEventListener?.('change', lifecycle);
  const observer = new MutationObserver(lifecycle);
  for (const node of document.querySelectorAll('#login-dialog,#lock-screen')) observer.observe(node, {attributes:true, attributeFilter:['hidden','open','class']});
  window.addEventListener('pagehide', () => { ++generation; release(); });
  window.addEventListener('pageshow', () => apply(true));
  window.aideUI?.subscribe(() => apply());
  window.AideBackground = {renderSettings};
  apply();
})();

function uiTextWorkbenchBackground(key, ...values) { return globalThis.window?.aideI18n?.t(key, ...values) ?? String(key).replace(/\{(\d+)\}/g, (match, index) => index < values.length ? String(values[index]) : match); }
