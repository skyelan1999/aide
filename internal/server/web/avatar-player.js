/* Sprite clips share one clock; a hidden or removed avatar never keeps its own loop. */
(() => {
  'use strict';
  const players = new Set();
  const slots = new WeakMap();
  const manifests = new Map();
  const images = new Map();
  const reducedMotion = window.matchMedia?.('(prefers-reduced-motion: reduce)');
  const MAX_CACHED_IMAGES = 4;
  const MAX_CACHED_PIXELS = 8 * 1024 * 1024;
  let raf = 0, intersectionObserver, resizeObserver, removalObserver, observing = false;
  let sweepPending = false;

  function assetURL(source, base) {
    const url = new URL(source, base);
    if (!['http:', 'https:', 'blob:', 'data:'].includes(url.protocol)) throw new Error('Unsupported avatar asset URL');
    return url.href;
  }

  function discard(cache, key, record) {
    if (cache.get(key) === record) cache.delete(key);
    record.dispose?.();
  }

  function trimCache(cache, maxEntries, pixelBudget = Infinity) {
    let pixels = [...cache.values()].reduce((sum, item) => sum + (item.pixels || 0), 0);
    for (const [key, item] of cache) {
      if (cache.size <= maxEntries && pixels <= pixelBudget) break;
      if (item.refs || !item.settled) continue;
      discard(cache, key, item);
      pixels -= item.pixels || 0;
    }
  }

  function acquire(cache, key, create) {
    let record = cache.get(key);
    if (record) { cache.delete(key); cache.set(key, record); }
    else {
      record = { key, refs: 0, settled: false, pixels: 0 };
      record.promise = Promise.resolve().then(() => create(record)).then(value => {
        record.settled = true;
        record.pixels = value?.naturalWidth ? value.naturalWidth * value.naturalHeight : 0;
        trimCache(cache, cache === images ? MAX_CACHED_IMAGES : 8, cache === images ? MAX_CACHED_PIXELS : Infinity);
        return value;
      }, error => {
        record.settled = true;
        discard(cache, key, record);
        throw error;
      });
      cache.set(key, record);
    }
    record.refs++;
    return record;
  }

  function release(cache, record) {
    if (!record) return;
    record.refs = Math.max(0, record.refs - 1);
    if (cache === images && !record.refs && !record.settled) discard(cache, record.key, record);
    trimCache(cache, cache === images ? MAX_CACHED_IMAGES : 8, cache === images ? MAX_CACHED_PIXELS : Infinity);
  }

  function loadImage(url) {
    return acquire(images, url, record => new Promise((resolve, reject) => {
      if (!record.refs) { reject(new Error('Avatar image no longer requested')); return; }
      const image = new Image();
      image.decoding = 'async';
      record.dispose = () => {
        image.onload = image.onerror = null;
        image.src = '';
        if (!record.settled) reject(new Error('Avatar image no longer requested'));
      };
      image.onload = () => {
        image.onload = image.onerror = null;
        const finish = () => {
          if (!record.refs) { reject(new Error('Avatar image no longer requested')); return; }
          if (!image.naturalWidth || !image.naturalHeight || image.naturalWidth * image.naturalHeight > 64 * 1024 * 1024) {
            reject(new Error('Invalid avatar sheet dimensions'));
          } else resolve(image);
        };
        // Decode speculative sheets before their first visible paint instead of on the boundary.
        if (typeof image.decode === 'function') image.decode().then(finish, reject);
        else finish();
      };
      image.onerror = () => { image.onload = image.onerror = null; reject(new Error('Avatar sheet unavailable')); };
      image.src = url;
    }));
  }

  function normalizeManifest(raw, base) {
    if (!raw || raw.version !== 1 || !raw.sheets || !raw.clips) throw new Error('Invalid avatar manifest');
    const sheets = Object.create(null), clips = Object.create(null), scenes = Object.create(null);
    for (const [id, sheet] of Object.entries(raw.sheets).slice(0, 256)) {
      if (!sheet?.src) continue;
      const columns = Math.max(1, Math.min(128, Math.floor(Number(sheet.columns) || 1)));
      const rows = Math.max(1, Math.min(128, Math.floor(Number(sheet.rows) || 1)));
      const rects = sheet.frameRects || sheet.frames;
      const frameRects = Array.isArray(rects) ? rects.slice(0, 4096).map(rect => {
        const values = Array.isArray(rect) ? rect : [rect?.x, rect?.y, rect?.width, rect?.height];
        if (values.length !== 4 || !values.every(Number.isFinite) || values[0] < 0 || values[1] < 0 || values[2] <= 0 || values[3] <= 0) throw new Error('Invalid avatar frame rectangle');
        return values;
      }) : null;
      sheets[id] = { src: assetURL(sheet.src, base), columns, rows, frameRects };
    }
    for (const [name, spec] of Object.entries(raw.clips).slice(0, 512)) {
      const sheet = sheets[spec?.sheet];
      if (!sheet || !Array.isArray(spec.frames)) continue;
      const count = sheet.frameRects ? sheet.frameRects.length : sheet.columns * sheet.rows;
      if (!spec.frames.length || spec.frames.length > 4096 || spec.frames.some(frame => !Number.isInteger(frame) || frame < 0 || frame >= count)) continue;
      const durations = spec.frames.map((_, i) => Math.min(10000, Math.max(40, Number(spec.durations?.[i]) || Number(spec.duration) || 160)));
      clips[name] = { id: name, sheet, frames: spec.frames.slice(), durations, total: durations.reduce((a, b) => a + b, 0), loop: spec.loop !== false };
    }
    if (!Object.keys(clips).length) throw new Error('Avatar manifest has no playable clips');
    for (const [name, variants] of Object.entries(raw.scenes || {}).slice(0, 128)) {
      if (!Array.isArray(variants)) continue;
      const ids = [...new Set(variants.filter(id => typeof id === 'string' && clips[id]))];
      if (ids.length) scenes[name] = ids;
    }
    return { label: String(raw.label || '虚拟形象').slice(0, 200), poster: raw.poster ? assetURL(raw.poster, base) : '', clips, scenes };
  }

  function candidatesFor(manifest, motion) {
    const { clips, scenes } = manifest;
    const scene = scenes[motion] || Object.values(scenes).find(ids => ids.includes(motion)) || [];
    const ids = [...(clips[motion] ? [motion] : []), ...scene,
      ...(clips.idle ? ['idle'] : []), ...(scenes.idle || []), Object.keys(clips)[0]];
    return [...new Set(ids)].map(id => clips[id]).filter(Boolean);
  }

  function nextVariant(player) {
    if (!player.clip || !player.manifest) return null;
    const { clips, scenes } = player.manifest;
    if (player.deferredMotion) return candidatesFor(player.manifest, player.deferredMotion).find(clip => !player.failedSheets.has(clip.sheet.src));
    if (player.preloadMotion) return candidatesFor(player.manifest, player.preloadMotion).find(clip => !player.failedSheets.has(clip.sheet.src));
    const variants = scenes[player.motion] || Object.values(scenes).find(ids => ids.includes(player.clip.id));
    if (!variants || variants.length < 2) return null;
    const current = variants.indexOf(player.clip.id);
    for (let step = 1; step < variants.length; step++) {
      const clip = clips[variants[(current + step + variants.length) % variants.length]];
      if (!player.failedSheets.has(clip.sheet.src)) return clip;
    }
    return null;
  }

  function releasePreload(player) {
    release(images, player.preloadRecord);
    player.preloadRecord = null;
  }

  async function warmNext(player) {
    if (player.destroyed || !player.manifest || !player.image || player.pendingRecord || document.hidden || (reducedMotion?.matches || document.documentElement.dataset.motion === 'reduced') || !visible(player)) {
      releasePreload(player);
      return false;
    }
    const clip = nextVariant(player);
    if (!clip || clip.sheet.src === player.clip.sheet.src) {
      releasePreload(player);
      if (clip && !player.clip.loop && player.elapsed >= player.clip.total) commitDeferred(player);
      return false;
    }
    if (player.preloadRecord?.key !== clip.sheet.src) {
      releasePreload(player);
      player.preloadRecord = loadImage(clip.sheet.src);
    }
    const record = player.preloadRecord;
    try {
      await record.promise;
      const ready = !player.destroyed && player.preloadRecord === record;
      if (ready && !player.clip.loop && player.elapsed >= player.clip.total) commitDeferred(player);
      return ready;
    }
    catch {
      if (player.preloadRecord === record) {
        player.failedSheets.add(record.key); releasePreload(player);
        if (player.deferredMotion) void warmNext(player);
      }
      return false;
    }
  }

  function frameAt(clip, elapsed) {
    let remaining = clip.loop ? elapsed % clip.total : Math.min(elapsed, clip.total - 1);
    for (let i = 0; i < clip.frames.length; i++) {
      if (remaining < clip.durations[i]) {
        const next = i + 1 < clip.frames.length ? i + 1 : clip.loop ? 0 : i;
        return { frame: clip.frames[i], next: clip.frames[next], mix: next === i ? 0 : remaining / clip.durations[i] };
      }
      remaining -= clip.durations[i];
    }
    const frame = clip.frames[clip.frames.length - 1];
    return { frame, next: frame, mix: 0 };
  }

  function playbackRate(value) {
    const number = typeof value === 'number' || (typeof value === 'string' && value.trim()) ? Number(value) : NaN;
    return Number.isFinite(number) ? Math.max(0.25, Math.min(4, number)) : 1;
  }

  function connected(player) {
    return player.slot.isConnected && player.element.parentNode === player.slot;
  }

  function visible(player) {
    // IntersectionObserver supplies visibility without forcing a layout on each tick.
    return connected(player) && player.inView && (intersectionObserver || player.slot.getClientRects().length > 0);
  }

  function canAnimate(player) {
    return !player.destroyed && player.image && player.clip?.frames.length > 1 &&
      (player.clip.loop || player.elapsed < player.clip.total) && !document.hidden &&
      !(reducedMotion?.matches || document.documentElement.dataset.motion === 'reduced') && visible(player);
  }

  function schedule() {
    const runnable = [...players].some(canAnimate);
    if (runnable && !raf) raf = requestAnimationFrame(tick);
    else if (!runnable && raf) { cancelAnimationFrame(raf); raf = 0; }
  }

  function commitDeferred(player) {
    if (!player.deferredMotion || player.destroyed || !player.manifest || document.hidden || (reducedMotion?.matches || document.documentElement.dataset.motion === 'reduced') || !visible(player)) return false;
    const clip = nextVariant(player);
    if (!clip || (clip.sheet.src !== player.clip?.sheet.src && (player.preloadRecord?.key !== clip.sheet.src || !player.preloadRecord.settled))) return false;
    const motion = player.deferredMotion;
    player.deferredMotion = null;
    player.controller.setMotion(motion);
    return true;
  }

  function tick(now) {
    raf = 0;
    for (const player of players) {
      if (!connected(player) && player.wasConnected) { destroy(player); continue; }
      if (!canAnimate(player)) { player.lastTick = null; continue; }
      const previousElapsed = player.elapsed;
      if (player.lastTick !== null) player.elapsed += Math.max(0, now - player.lastTick) * player.playbackRate;
      player.lastTick = now;
      const ended = player.clip.loop ? Math.floor(player.elapsed / player.clip.total) > Math.floor(previousElapsed / player.clip.total) : player.elapsed >= player.clip.total;
      if (ended && commitDeferred(player)) continue;
      draw(player);
    }
    schedule();
  }

  function draw(player, force = false) {
    if (!player.image || !player.clip || player.destroyed) return;
    const { frame, next, mix: progress } = frameAt(player.clip, player.elapsed);
    const mix = player.interpolationEnabled && next !== frame ? progress : 0;
    if (!force && frame === player.drawnFrame && next === player.drawnNext && mix === player.drawnMix) return;
    try {
      const { sheet } = player.clip;
      const sourceWidth = player.image.naturalWidth / sheet.columns;
      const sourceHeight = player.image.naturalHeight / sheet.rows;
      const rectangle = index => {
        const rect = sheet.frameRects?.[index] || [index % sheet.columns * sourceWidth, Math.floor(index / sheet.columns) * sourceHeight, sourceWidth, sourceHeight];
        if (rect[0] + rect[2] > player.image.naturalWidth + 0.5 || rect[1] + rect[3] > player.image.naturalHeight + 0.5) throw new Error('Avatar frame outside sheet');
        return rect;
      };
      const rect = rectangle(frame);
      const ratio = Math.min(2, Math.max(1, window.devicePixelRatio || 1));
      if (!player.layout || player.layout.ratio !== ratio) player.layout = { width: player.slot.clientWidth, height: player.slot.clientHeight, ratio };
      const width = Math.min(2048, Math.max(1, Math.round((player.layout.width || rect[2]) * ratio)));
      const height = Math.min(2048, Math.max(1, Math.round((player.layout.height || rect[3]) * ratio)));
      const canvas = player.canvas;
      if (canvas.width !== width) canvas.width = width;
      if (canvas.height !== height) canvas.height = height;
      const context = player.context || (player.context = canvas.getContext('2d'));
      if (!context) throw new Error('Canvas unavailable');
      context.clearRect(0, 0, width, height);
      context.imageSmoothingEnabled = true;
      context.imageSmoothingQuality = 'high';
      const paint = (source, weight) => {
        const scale = Math.min(width / source[2], height / source[3]);
        context.globalAlpha = weight;
        context.drawImage(player.image, ...source, (width - source[2] * scale) / 2, (height - source[3] * scale) / 2, source[2] * scale, source[3] * scale);
      };
      try {
        // Weighted addition on a cleared canvas interpolates premultiplied color AND alpha.
        // source-over would attenuate the first pose twice and darken transparent edges.
        context.globalCompositeOperation = mix > 0 ? 'lighter' : 'source-over';
        paint(rect, 1 - mix);
        if (mix > 0) paint(rectangle(next), mix);
      } finally {
        context.globalAlpha = 1;
        context.globalCompositeOperation = 'source-over';
      }
      player.drawnFrame = frame; player.drawnNext = next; player.drawnMix = mix;
    } catch { fallback(player); }
  }

  function settle(player, status) {
    if (!player.resolveReady) return;
    player.resolveReady({ status });
    player.resolveReady = null;
  }

  function fallback(player) {
    if (player.destroyed) return;
    ++player.generation;
    release(images, player.pendingRecord); player.pendingRecord = null;
    releasePreload(player);
    player.image = null;
    player.clip = null;
    release(images, player.imageRecord);
    player.imageRecord = null;
    const poster = player.manifest?.poster || player.poster;
    if (poster) {
      const image = document.createElement('img');
      image.className = 'avatar-character avatar-poster';
      image.alt = player.manifest?.label || '虚拟形象';
      image.decoding = 'async';
      image.onerror = () => { image.onerror = null; player.slot.dataset.avatarStatus = 'error'; };
      image.src = poster;
      player.element = image;
      player.slot.replaceChildren(image);
      player.slot.dataset.avatarStatus = 'fallback';
      settle(player, 'fallback');
    } else {
      player.slot.dataset.avatarStatus = 'error';
      player.canvas.setAttribute('aria-label', '虚拟形象暂时不可用');
      settle(player, 'error');
    }
    schedule();
  }

  async function selectClip(player) {
    if (player.destroyed || !player.manifest) return;
    const candidates = candidatesFor(player.manifest, player.motion);
    if (player.clip === candidates[0] && player.image && !player.pendingRecord) { void warmNext(player); return; }
    const generation = ++player.generation;
    release(images, player.pendingRecord); player.pendingRecord = null;
    const failedSheets = player.failedSheets;
    for (const clip of candidates) {
      if (player.destroyed || generation !== player.generation) return;
      if (failedSheets.has(clip.sheet.src)) continue;
      let record;
      if (player.preloadRecord?.key === clip.sheet.src) {
        record = player.preloadRecord; player.preloadRecord = null;
      } else { releasePreload(player); record = loadImage(clip.sheet.src); }
      player.pendingRecord = record;
      try {
        const image = await record.promise;
        if (player.destroyed || generation !== player.generation) return;
        if (clip.sheet.frameRects && clip.frames.some(frame => {
          const [x, y, width, height] = clip.sheet.frameRects[frame];
          return x + width > image.naturalWidth + 0.5 || y + height > image.naturalHeight + 0.5;
        })) throw new Error('Avatar frame outside sheet');
        release(images, player.imageRecord);
        player.imageRecord = record; player.pendingRecord = null;
        player.image = image; player.clip = clip;
        player.elapsed = 0; player.lastTick = null; player.drawnFrame = -1;
        if (player.element !== player.canvas) { player.element = player.canvas; player.slot.replaceChildren(player.canvas); }
        player.slot.dataset.avatarClip = clip.id;
        draw(player, true);
        if (player.image) {
          player.slot.dataset.avatarStatus = 'ready';
          settle(player, 'ready');
          schedule(); void warmNext(player);
        }
        return;
      } catch {
        if (player.destroyed || generation !== player.generation) return;
        failedSheets.add(clip.sheet.src);
        release(images, record); player.pendingRecord = null;
      }
    }
    if (!player.destroyed && generation === player.generation) fallback(player);
  }

  function refreshVisibility() {
    for (const player of players) { player.lastTick = null; void warmNext(player); }
    schedule();
  }

  function sweep() {
    sweepPending = false;
    for (const player of players) {
      if (connected(player)) {
        player.wasConnected = true;
        clearTimeout(player.orphanTimer);
        player.orphanTimer = 0;
        if (!player.preloadRecord) void warmNext(player);
      } else if (player.wasConnected) destroy(player);
    }
    schedule();
  }

  function startObservers() {
    if (observing) return;
    observing = true;
    document.addEventListener('visibilitychange', refreshVisibility);
    window.addEventListener('aide:motion',refreshVisibility);
    if (reducedMotion?.addEventListener) reducedMotion.addEventListener('change', refreshVisibility);
    else reducedMotion?.addListener?.(refreshVisibility);
    if (window.IntersectionObserver) intersectionObserver = new IntersectionObserver(entries => {
      for (const entry of entries) {
        const player = slots.get(entry.target);
        if (player) { player.inView = entry.isIntersecting; player.lastTick = null; void warmNext(player); }
      }
      sweep();
    });
    if (window.ResizeObserver) resizeObserver = new ResizeObserver(entries => {
      for (const entry of entries) {
        const player = slots.get(entry.target);
        if (player) { player.layout = null; draw(player, true); player.lastTick = null; }
      }
      schedule();
    });
    removalObserver = new MutationObserver(() => {
      if (!sweepPending) { sweepPending = true; queueMicrotask(sweep); }
    });
    removalObserver.observe(document.documentElement, {
      childList: true, subtree: true,
      ...(intersectionObserver ? {} : { attributes: true, attributeFilter: ['hidden', 'open', 'class', 'style'] })
    });
  }

  function stopObservers() {
    if (players.size || !observing) return;
    observing = false;
    if (raf) { cancelAnimationFrame(raf); raf = 0; }
    intersectionObserver?.disconnect(); resizeObserver?.disconnect(); removalObserver?.disconnect();
    intersectionObserver = resizeObserver = removalObserver = undefined;
    document.removeEventListener('visibilitychange', refreshVisibility);
    window.removeEventListener('aide:motion',refreshVisibility);
    if (reducedMotion?.removeEventListener) reducedMotion.removeEventListener('change', refreshVisibility);
    else reducedMotion?.removeListener?.(refreshVisibility);
    for (const [key, record] of images) if (!record.refs) discard(images, key, record);
  }

  function destroy(player) {
    if (player.destroyed) return;
    player.destroyed = true;
    ++player.generation;
    clearTimeout(player.orphanTimer);
    intersectionObserver?.unobserve(player.slot);
    resizeObserver?.unobserve(player.slot);
    if (player.element.parentNode === player.slot) player.element.remove();
    if (player.element.tagName === 'IMG') player.element.onerror = null;
    players.delete(player);
    slots.delete(player.slot);
    release(images, player.imageRecord);
    release(images, player.pendingRecord); player.pendingRecord = null;
    releasePreload(player);
    release(manifests, player.manifestRecord);
    player.image = player.imageRecord = player.manifest = player.manifestRecord = player.clip = null;
    delete player.slot.dataset.avatarStatus;
    delete player.slot.dataset.avatarMotion;
    delete player.slot.dataset.avatarClip;
    // Canvas backing stores otherwise survive as long as a caller retains the controller.
    player.canvas.width = player.canvas.height = 1;
    settle(player, 'destroyed');
    stopObservers();
    schedule();
  }

  function mount(slot, manifestURL, motion = 'idle', options = {}) {
    if (!slot?.replaceChildren) throw new TypeError('Avatar slot must be an element');
    const url = assetURL(manifestURL, document.baseURI);
    const previous = slots.get(slot);
    if (previous?.url === url) { previous.controller.setMotion(motion, options); return previous.controller; }
    if (previous) destroy(previous);
    const canvas = document.createElement('canvas');
    canvas.className = 'avatar-character avatar-sprite';
    canvas.setAttribute('role', 'img');
    canvas.setAttribute('aria-label', '虚拟形象');
    canvas.textContent = '虚拟形象';
    const player = { slot, url, canvas, element: canvas, motion, generation: 0, elapsed: 0, playbackRate: 1, interpolationEnabled: true, drawnFrame: -1, drawnNext: -1, drawnMix: -1, failedSheets: new Set(),
      lastTick: null, inView: !slot.isConnected || slot.getClientRects().length > 0, wasConnected: slot.isConnected, destroyed: false, orphanTimer: 0 };
    const ready = new Promise(resolve => { player.resolveReady = resolve; });
    player.controller = Object.freeze({
      ready,
      setMotion(nextMotion = 'idle', options = {}) {
        if (player.destroyed) return;
        if (player.motion === nextMotion) {
          if (player.deferredMotion) { player.deferredMotion = null; void warmNext(player); }
          return;
        }
        if (options?.deferUntilLoop && player.clip?.frames.length > 1 && player.image && !player.pendingRecord) {
          if (player.deferredMotion !== nextMotion) { player.deferredMotion = nextMotion; player.preloadMotion = null; }
          void warmNext(player);
          return;
        }
        player.deferredMotion = null;
        player.motion = nextMotion; player.preloadMotion = null;
        slot.dataset.avatarMotion = nextMotion;
        void selectClip(player);
      },
      setPlaybackRate(rate) {
        // Change the clock multiplier without restarting the clip or its current hold.
        if (!player.destroyed) player.playbackRate = playbackRate(rate);
        return player.playbackRate;
      },
      setInterpolationEnabled(enabled = true) {
        const next = !!enabled;
        if (!player.destroyed && player.interpolationEnabled !== next) {
          player.interpolationEnabled = next;
          if (!document.hidden && visible(player)) draw(player, true);
        }
        return player.interpolationEnabled;
      },
      preload(nextMotion) {
        if (player.destroyed) return Promise.resolve(false);
        player.preloadMotion = typeof nextMotion === 'string' ? nextMotion : null;
        return warmNext(player);
      },
      destroy() { destroy(player); }
    });
    slots.set(slot, player);
    players.add(player);
    slot.replaceChildren(canvas);
    slot.dataset.avatarStatus = 'loading';
    slot.dataset.avatarMotion = motion;
    startObservers();
    intersectionObserver?.observe(slot);
    resizeObserver?.observe(slot);
    // Settings constructs previews before attaching them. Allow that synchronous render,
    // but release a preview which was abandoned before it ever reached the document.
    if (!player.wasConnected) player.orphanTimer = setTimeout(() => { if (!connected(player)) destroy(player); }, 10000);
    player.manifestRecord = acquire(manifests, url, async () => {
      const response = await fetch(url, { credentials: 'same-origin' });
      if (!response.ok) throw new Error('Avatar manifest unavailable');
      return response.json();
    });
    player.manifestRecord.promise.then(raw => {
      if (player.destroyed) return;
      // Retain a usable poster even when a malformed animation manifest cannot play.
      if (raw?.poster) player.poster = assetURL(raw.poster, url);
      player.manifest = normalizeManifest(raw, url);
      canvas.setAttribute('aria-label', player.manifest.label);
      canvas.textContent = player.manifest.label;
      return selectClip(player);
    }).catch(() => { if (!player.destroyed) fallback(player); });
    queueMicrotask(sweep);
    return player.controller;
  }

  window.AideAvatarPlayer = Object.freeze({ mount, unmount(slot) { const player = slots.get(slot); if (player) destroy(player); } });
})();
