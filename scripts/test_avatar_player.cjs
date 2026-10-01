'use strict';
// Verify observable canvas frames and lifecycle using a deterministic browser clock.
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const crypto = require('node:crypto');

function environment(manifestOverrides = {}, imageSize = { width: 400, height: 200 }) {
  let nextRAF = 1, nextTimer = 1;
  const rafs = new Map(), timers = new Map(), events = new Map(), mediaEvents = new Map();
  const imageLoads = [], imageInstances = [], pendingImages = [], fetches = [], observers = { intersection: [], resize: [], mutation: [] };
  const manifest = {
    version: 1, label: 'Little whale', poster: 'poster.png',
    sheets: { main: { src: 'sheet.png', columns: 4, rows: 2 } },
    clips: {
      idle: { sheet: 'main', frames: [0, 1, 2], durations: [100, 200, 300], loop: true },
      eating: { sheet: 'main', frames: [4, 5], durations: [50, 80], loop: true },
      done: { sheet: 'main', frames: [6, 7], durations: [100, 100], loop: false }
    }, ...manifestOverrides
  };
  class Element {
    constructor(tagName) { this.tagName = tagName.toUpperCase(); this.dataset = {}; this.attributes = {}; this.children = []; this.parentNode = null; this.clientWidth = 100; this.clientHeight = 150; this.width = 0; this.height = 0; this.draws = []; this.currentDraws = []; this.paints = 0; this.layoutVisible = true; }
    get isConnected() { return this.root || !!this.parentNode?.isConnected; }
    append(child) { child.remove(); this.children.push(child); child.parentNode = this; }
    replaceChildren(...children) { for (const child of this.children) child.parentNode = null; this.children = []; children.forEach(child => this.append(child)); }
    remove() { if (this.parentNode) this.parentNode.children = this.parentNode.children.filter(child => child !== this); this.parentNode = null; }
    getClientRects() { return this.layoutVisible && this.isConnected ? [{}] : []; }
    setAttribute(key, value) { this.attributes[key] = value; }
    getContext() {
      this.contextRequests = (this.contextRequests || 0) + 1;
      if (!this.context) {
        const surface = this;
        this.context = {
          globalAlpha: 1, globalCompositeOperation: 'source-over', pixel: [0, 0, 0, 0],
          clearRect() { this.pixel = [0, 0, 0, 0]; surface.currentDraws = []; surface.paints++; },
          drawImage(...args) {
            surface.draws.push(args);
            surface.currentDraws.push({ args, alpha: this.globalAlpha, operation: this.globalCompositeOperation });
            const sample = args[0].pixelAt?.(args[1], args[2]) || [0, 0, 0, 0];
            const alpha = sample[3] * this.globalAlpha, source = sample.map((value, channel) => channel === 3 ? alpha : value * alpha);
            this.pixel = source.map((value, channel) => this.globalCompositeOperation === 'lighter'
              ? Math.min(1, value + this.pixel[channel]) : value + this.pixel[channel] * (1 - alpha));
          }
        };
      }
      return this.context;
    }
  }
  class Observer {
    constructor(callback, kind) { this.callback = callback; this.targets = new Set(); this.disconnected = false; observers[kind].push(this); }
    observe(target) { this.targets.add(target); }
    unobserve(target) { this.targets.delete(target); }
    disconnect() { this.targets.clear(); this.disconnected = true; }
  }
  const documentElement = new Element('html'); documentElement.root = true;
  const document = {
    baseURI: 'https://localhost:8097/', documentElement, hidden: false,
    createElement: tag => new Element(tag),
    addEventListener: (name, fn) => events.set(name, fn),
    removeEventListener: (name, fn) => { if (events.get(name) === fn) events.delete(name); }
  };
  const media = { matches: false, addEventListener: (name, fn) => mediaEvents.set(name, fn), removeEventListener: (name, fn) => { if (mediaEvents.get(name) === fn) mediaEvents.delete(name); } };
  const context = {
    document, URL, console, queueMicrotask, Promise,
    devicePixelRatio: 1, matchMedia: () => media,
    fetch: async url => { fetches.push(url); return { ok: !url.includes('missing-manifest'), json: async () => manifest }; },
    Image: class {
      constructor() { imageInstances.push(this); }
      decode() { this.decodeCalls = (this.decodeCalls || 0) + 1; return Promise.resolve(); }
      get src() { return this._src; }
      set src(url) {
        this._src = url;
        if (!url) { this.naturalWidth = this.naturalHeight = 0; return; }
        const size = typeof imageSize === 'function' ? imageSize(url) : imageSize;
        this.naturalWidth = size.width; this.naturalHeight = size.height;
        this.pixelAt = size.pixelAt;
        imageLoads.push(url);
        const finish = () => { if (this._src !== url) return; url.includes('missing.png') ? this.onerror?.() : this.onload?.(); };
        if (url.includes('deferred')) pendingImages.push({ url, finish }); else queueMicrotask(finish);
      }
    },
    IntersectionObserver: class extends Observer { constructor(callback) { super(callback, 'intersection'); } },
    ResizeObserver: class extends Observer { constructor(callback) { super(callback, 'resize'); } },
    MutationObserver: class extends Observer { constructor(callback) { super(callback, 'mutation'); } },
    requestAnimationFrame: fn => { const id = nextRAF++; rafs.set(id, fn); return id; },
    cancelAnimationFrame: id => rafs.delete(id),
    setTimeout: fn => { const id = nextTimer++; timers.set(id, fn); return id; }, clearTimeout: id => timers.delete(id)
  };
  context.window = context;
  vm.runInNewContext(fs.readFileSync(path.join(__dirname, '../internal/server/web/avatar-player.js'), 'utf8'), context);
  return {
    api: context.AideAvatarPlayer, manifest, document, media, imageLoads, imageInstances, fetches, observers, rafs, timers,
    completeImages() { pendingImages.splice(0).forEach(pending => pending.finish()); },
    slot(attached = true) { const slot = new Element('div'); if (attached) documentElement.append(slot); return slot; },
    tick(now) { const callbacks = [...rafs.values()]; rafs.clear(); callbacks.forEach(callback => callback(now)); assert.ok(rafs.size <= 1, 'one shared RAF at most'); },
    visibility(hidden) { document.hidden = hidden; events.get('visibilitychange')?.(); },
    reduce(value) { media.matches = value; mediaEvents.get('change')?.(); },
    intersection(slot, visible) { observers.intersection.at(-1).callback([{ target: slot, isIntersecting: visible }]); },
    mutate() { observers.mutation.at(-1).callback([]); },
    events, mediaEvents
  };
}
const flush = async () => { for (let i = 0; i < 12; i++) await Promise.resolve(); };
const lastSource = slot => slot.children[0].currentDraws[0]?.args.slice(1, 5);

(async () => {
  const env = environment(), slot = env.slot();
  const controller = env.api.mount(slot, '/avatars/whale/manifest.json');
  assert.equal((await controller.ready).status, 'ready');
  assert.equal(slot.children[0].attributes['aria-label'], 'Little whale');
  assert.deepEqual(lastSource(slot), [0, 0, 100, 100]);
  // Variable holds are honored at exact frame boundaries, and a loop wraps exactly once.
  env.tick(0); env.tick(99); assert.deepEqual(lastSource(slot), [0, 0, 100, 100]);
  env.tick(100); assert.deepEqual(lastSource(slot), [100, 0, 100, 100]);
  env.tick(299); assert.deepEqual(lastSource(slot), [100, 0, 100, 100]);
  assert.equal(env.api.mount(slot, '/avatars/whale/manifest.json'), controller);
  env.tick(300); assert.deepEqual(lastSource(slot), [200, 0, 100, 100]);
  env.tick(600); assert.deepEqual(lastSource(slot), [0, 0, 100, 100]);
  // Two visible characters still share one clock and decode their shared sheet once.
  const second = env.slot(); const controller2 = env.api.mount(second, '/avatars/whale/manifest.json');
  await controller2.ready;
  assert.equal(env.fetches.length, 1); assert.equal(env.imageLoads.length, 1); assert.equal(env.rafs.size, 1);
  // Pauses preserve frame time rather than fast-forwarding after a background interval.
  env.tick(700); assert.deepEqual(lastSource(slot), [100, 0, 100, 100]);
  env.visibility(true); assert.equal(env.rafs.size, 0);
  env.visibility(false); env.tick(5000); assert.deepEqual(lastSource(slot), [100, 0, 100, 100]);
  env.tick(5199); assert.deepEqual(lastSource(slot), [100, 0, 100, 100]);
  env.tick(5200); assert.deepEqual(lastSource(slot), [200, 0, 100, 100]);
  env.intersection(slot, false); env.intersection(second, false); assert.equal(env.rafs.size, 0);
  env.intersection(slot, true); assert.equal(env.rafs.size, 1);
  env.reduce(true); assert.equal(env.rafs.size, 0);
  env.reduce(false); assert.equal(env.rafs.size, 1);
  controller.setMotion('eating'); await flush();
  assert.deepEqual(lastSource(slot), [0, 100, 100, 100]);
  env.tick(10000); env.tick(10050); assert.deepEqual(lastSource(slot), [100, 100, 100, 100]);
  controller.setMotion('eating'); env.tick(10100); assert.deepEqual(lastSource(slot), [100, 100, 100, 100]);
  controller.setMotion('unknown'); await flush(); assert.deepEqual(lastSource(slot), [0, 0, 100, 100]);
  controller.setMotion('done'); await flush(); env.tick(11000); env.tick(11200);
  assert.deepEqual(lastSource(slot), [300, 100, 100, 100]); assert.equal(env.rafs.size, 0, 'non-loop clip sleeps on its final frame');
  // Detached settings previews are automatically disposed, including listeners and backing store.
  slot.remove(); second.remove(); env.mutate(); await flush();
  assert.equal(env.rafs.size, 0); assert.equal(env.events.size, 0); assert.equal(env.mediaEvents.size, 0);
  assert.ok(Object.values(env.observers).flat().every(observer => observer.disconnected));
  controller.destroy(); controller2.destroy(); // Idempotent.
  const detached = env.slot(false); const orphan = env.api.mount(detached, '/avatars/whale/manifest.json');
  for (const timer of [...env.timers.values()]) timer();
  assert.equal((await orphan.ready).status, 'destroyed'); assert.equal(env.rafs.size, 0);

  const failure = environment({ sheets: { main: { src: 'missing.png', columns: 4, rows: 2 } } });
  const failedSlot = failure.slot(); const failed = failure.api.mount(failedSlot, '/avatars/whale/manifest.json');
  assert.equal((await failed.ready).status, 'fallback');
  assert.equal(failedSlot.children[0].tagName, 'IMG');
  assert.equal(failedSlot.children[0].src, 'https://localhost:8097/avatars/whale/poster.png');
  assert.equal(failure.rafs.size, 0); failed.destroy();

  const unavailable = environment(); const unavailableSlot = unavailable.slot();
  const unavailablePlayer = unavailable.api.mount(unavailableSlot, '/missing-manifest.json');
  assert.equal((await unavailablePlayer.ready).status, 'error'); unavailablePlayer.destroy();

  // Real-time interpolation uses two premultiplied weighted draws, never source-over fades.
  const smoothManifest = { clips: {
    idle: { sheet: 'main', frames: [0, 1], durations: [100, 100], loop: true },
    once: { sheet: 'main', frames: [2, 3], durations: [100, 100], loop: false }
  } };
  const smooth = environment(smoothManifest, { width: 400, height: 200, pixelAt: () => [1, 0, 0, 1] });
  const smoothSlot = smooth.slot(); let widthReads = 0;
  Object.defineProperty(smoothSlot, 'clientWidth', { get() { widthReads++; return 100; } });
  const smoothPlayer = smooth.api.mount(smoothSlot, '/smooth.json'); await smoothPlayer.ready;
  const surface = smoothSlot.children[0], sample = () => surface.currentDraws;
  const opacity = () => surface.context.pixel[3];
  assert.ok(smooth.imageInstances.every(image => image.decodeCalls === 1), 'atlas is decoded before it becomes playable');
  smooth.tick(0); const initialPaints = surface.paints;
  smooth.tick(25); assert.deepEqual(sample().map(draw => draw.alpha), [0.75, 0.25]);
  smooth.tick(50); assert.deepEqual(sample().map(draw => draw.alpha), [0.5, 0.5]);
  assert.ok(sample().every(draw => draw.operation === 'lighter'), 'both premultiplied terms are added on a cleared surface');
  assert.equal(opacity(), 1, 'overlapping opaque pixels retain full opacity, rather than dipping to 0.75');
  assert.deepEqual(surface.context.pixel, [1, 0, 0, 1], 'identical colors do not darken during blending');
  const halfPaints = surface.paints;smooth.tick(50);assert.equal(surface.paints, halfPaints, 'unchanged fractional progress does not repaint');
  smooth.tick(75);assert.equal(surface.paints, initialPaints + 3, 'three intermediate samples render inside one source-frame interval');
  assert.equal(surface.contextRequests, 1, 'intermediate frames reuse the canvas context');
  assert.equal(widthReads, 1, 'intermediate frames use cached layout dimensions');
  smooth.observers.resize.at(-1).callback([{ target: smoothSlot }]);
  assert.equal(widthReads, 2, 'ResizeObserver refreshes cached dimensions');
  assert.equal(surface.context.globalAlpha, 1);assert.equal(surface.context.globalCompositeOperation, 'source-over', 'blend state is restored after each paint');
  smooth.tick(75);smooth.tick(100);assert.equal(sample().length, 1);assert.deepEqual(lastSource(smoothSlot), [100, 0, 100, 100]);
  smooth.tick(150);assert.deepEqual(sample().map(draw => draw.args[1]), [100, 0], 'loop end blends toward the first source frame');
  assert.deepEqual(sample().map(draw => draw.alpha), [0.5, 0.5]);
  smooth.tick(200);smoothPlayer.setPlaybackRate(2);smooth.tick(225);
  assert.deepEqual(sample().map(draw => draw.alpha), [0.5, 0.5], 'playback multiplier also scales interpolation progress');
  const smoothLoads = smooth.imageLoads.length;
  assert.equal(smoothPlayer.setInterpolationEnabled(false), false);assert.equal(sample().length, 1);
  const discretePaints = surface.paints;smooth.tick(235);assert.equal(surface.paints, discretePaints, 'disabled interpolation paints only source-frame changes');
  smoothPlayer.setInterpolationEnabled(true);
  assert.ok(Math.abs(sample()[1].alpha - 0.7) < 1e-9, 'reenabling keeps the exact accumulated fractional position');
  const enabledPaints = surface.paints;smoothPlayer.setInterpolationEnabled(true);assert.equal(surface.paints, enabledPaints, 'same-value toggles do not repaint');
  smooth.visibility(true);smoothPlayer.setInterpolationEnabled(false);smoothPlayer.setInterpolationEnabled(true);
  assert.equal(surface.paints, enabledPaints);assert.equal(smooth.rafs.size, 0, 'hidden interpolation has no frame work');
  smooth.visibility(false);smooth.tick(5000);assert.equal(surface.paints, enabledPaints, 'resume keeps the paused blend weight');
  smooth.reduce(true);assert.equal(smooth.rafs.size, 0);smooth.reduce(false);smooth.tick(5500);
  assert.equal(surface.paints, enabledPaints, 'reduced-motion pause also preserves the sample');
  smooth.intersection(smoothSlot, false);assert.equal(smooth.rafs.size, 0);smooth.intersection(smoothSlot, true);smooth.tick(5600);
  assert.equal(surface.paints, enabledPaints, 'offscreen pause does not interpolate hidden elapsed time');
  smoothPlayer.setPlaybackRate(1);smoothPlayer.setMotion('once');await flush();smooth.tick(6000);smooth.tick(6050);
  assert.deepEqual(sample().map(draw => draw.args[1]), [200, 300]);
  smooth.tick(6100);assert.equal(sample().length, 1);assert.deepEqual(lastSource(smoothSlot), [300, 0, 100, 100]);
  const finalPaints = surface.paints;smooth.tick(6150);smooth.tick(6200);
  assert.equal(surface.paints, finalPaints, 'one-shot last frame is held without blending back to frame zero');
  assert.equal(smooth.rafs.size, 0);assert.equal(smooth.imageLoads.length, smoothLoads);
  smoothPlayer.destroy();assert.equal(surface.width, 1);assert.equal(surface.height, 1);

  for (const [firstPixel, nextPixel, expected] of [
    [[1, 0, 0, 0.5], [0, 0, 1, 0.25], [0.25, 0, 0.125, 0.375]],
    [[0, 0, 0, 0], [0, 1, 0, 1], [0, 0.5, 0, 0.5]]
  ]) {
    const alphaEnv = environment(smoothManifest, { width: 400, height: 200, pixelAt: x => x === 0 ? firstPixel : nextPixel });
    const alphaSlot = alphaEnv.slot(), alphaPlayer = alphaEnv.api.mount(alphaSlot, '/alpha.json');await alphaPlayer.ready;
    alphaEnv.tick(0);alphaEnv.tick(50);
    assert.deepEqual(alphaSlot.children[0].context.pixel, expected, 'transparent edges and newly revealed pixels use the premultiplied weighted sum');
    alphaPlayer.destroy();
  }

  // Playback rate scales elapsed time without resetting a frame hold or loading artwork.
  const speed = environment(), speedSlot = speed.slot();
  const speedPlayer = speed.api.mount(speedSlot, '/speed.json'); await speedPlayer.ready;
  speed.tick(0); speed.tick(80);
  assert.deepEqual(lastSource(speedSlot), [0, 0, 100, 100], 'new players start at normal speed');
  const rateLoads = speed.imageLoads.length, rateDraws = speedSlot.children[0].draws.length;
  assert.equal(speedPlayer.setPlaybackRate(2), 2);
  assert.equal(speedSlot.children[0].draws.length, rateDraws, 'rate changes preserve the visible frame');
  speed.tick(90); assert.deepEqual(lastSource(speedSlot), [100, 0, 100, 100], '2x advances the remaining twenty milliseconds in ten');
  speedPlayer.setPlaybackRate(2);
  speed.tick(189); assert.deepEqual(lastSource(speedSlot), [100, 0, 100, 100]);
  speed.tick(190); assert.deepEqual(lastSource(speedSlot), [200, 0, 100, 100], 'setting the same rate preserves the clock cadence');
  assert.equal(speedPlayer.setPlaybackRate('0.5'), 0.5);
  speed.tick(788); assert.deepEqual(lastSource(speedSlot), [200, 0, 100, 100]);
  speed.tick(790); assert.deepEqual(lastSource(speedSlot), [0, 0, 100, 100], 'slowing down retains the accumulated loop position');
  assert.equal(speed.api.mount(speedSlot, '/speed.json'), speedPlayer, 'mount reuse preserves the rate');
  speed.tick(989); assert.deepEqual(lastSource(speedSlot), [0, 0, 100, 100]);
  speed.tick(990); assert.deepEqual(lastSource(speedSlot), [100, 0, 100, 100]);
  speed.visibility(true); speedPlayer.setPlaybackRate(4); assert.equal(speed.rafs.size, 0);
  speed.visibility(false); speed.tick(5000); assert.deepEqual(lastSource(speedSlot), [100, 0, 100, 100], 'background time is not multiplied into the animation');
  speed.tick(5050); assert.deepEqual(lastSource(speedSlot), [200, 0, 100, 100]);
  speed.intersection(speedSlot, false); speedPlayer.setPlaybackRate(0.25); assert.equal(speed.rafs.size, 0);
  speed.intersection(speedSlot, true); speed.tick(10000);
  speed.tick(11199); assert.deepEqual(lastSource(speedSlot), [200, 0, 100, 100]);
  speed.tick(11200); assert.deepEqual(lastSource(speedSlot), [0, 0, 100, 100], 'offscreen pause preserves progress at quarter speed');
  speed.reduce(true); speedPlayer.setPlaybackRate(3); assert.equal(speed.rafs.size, 0);
  speed.reduce(false); speed.tick(20000);
  speed.tick(20034); assert.deepEqual(lastSource(speedSlot), [100, 0, 100, 100], 'reduced-motion pause resumes at the selected rate');
  assert.equal(speedPlayer.setPlaybackRate(-5), 0.25);
  assert.equal(speedPlayer.setPlaybackRate(20), 4);
  for (const badRate of [NaN, Infinity, -Infinity, 'bad', '', '  ', null, undefined, {}, Symbol('invalid')]) {
    assert.equal(speedPlayer.setPlaybackRate(badRate), 1, 'invalid or non-finite rates normalize to 1x');
  }
  assert.equal(speed.imageLoads.length, rateLoads, 'rate changes never reload the sheet');
  speedPlayer.setPlaybackRate(4); speedPlayer.setMotion('done'); await flush();
  speed.tick(21000); speed.tick(21050);
  assert.deepEqual(lastSource(speedSlot), [300, 100, 100, 100]);
  assert.equal(speed.rafs.size, 0, 'accelerated one-shot still stops on its final frame');
  speedPlayer.setPlaybackRate(0.5); assert.equal(speed.rafs.size, 0, 'changing rate never restarts a finished clip');
  speedPlayer.destroy(); speedPlayer.setPlaybackRate(3);
  assert.equal(speed.rafs.size, 0, 'destroyed players cannot restart through a rate update');

  const independentSpeed = environment(), fastSlot = independentSpeed.slot(), normalSlot = independentSpeed.slot();
  const fastPlayer = independentSpeed.api.mount(fastSlot, '/independent-speed.json');
  fastPlayer.setPlaybackRate(2); // The settings UI applies preferences immediately after mount.
  const normalPlayer = independentSpeed.api.mount(normalSlot, '/independent-speed.json');
  await Promise.all([fastPlayer.ready, normalPlayer.ready]);
  independentSpeed.tick(0); independentSpeed.tick(50);
  assert.deepEqual(lastSource(fastSlot), [100, 0, 100, 100], 'rate configured before loading survives clip initialization');
  assert.deepEqual(lastSource(normalSlot), [0, 0, 100, 100], 'shared scheduler preserves independent player rates');
  fastPlayer.destroy(); normalPlayer.destroy();

  const rects = environment({ sheets: { main: { src: 'sheet.png', frameRects: [[10, 20, 60, 70], [90, 20, 80, 100]] } }, clips: { idle: { sheet: 'main', frames: [1], durations: [100] } } });
  const rectSlot = rects.slot(); const rectPlayer = rects.api.mount(rectSlot, '/rects.json'); await rectPlayer.ready;
  assert.deepEqual(lastSource(rectSlot), [90, 20, 80, 100]); assert.equal(rects.rafs.size, 0);
  assert.equal(rectSlot.children[0].draws.at(-1)[8], 125, 'frame fits its slot without distortion');
  rects.api.unmount(rectSlot);
  // A 38-variant pack decodes only the current sheet and its next scene variant.
  const sceneNames = ['idle', 'eating', 'sleeping', 'tantrum', 'thinking', 'listening', 'working', 'done', 'error', 'reading', 'writing', 'waiting', 'celebrating', 'greeting', 'stretching', 'playing'];
  const many = { version: 1, poster: 'poster.png', sheets: {}, clips: {}, scenes: {} };
  for (let i = 0; i < 38; i++) {
    many.sheets[`sheet${i}`] = { src: `variant-${i}.webp`, columns: 6, rows: 5 };
    many.clips[`variant${i}`] = { sheet: `sheet${i}`, frames: Array.from({ length: 30 }, (_, frame) => frame), durations: Array(30).fill(50), loop: true };
    (many.scenes[sceneNames[i % sceneNames.length]] ||= []).push(`variant${i}`);
  }
  const expanded = environment(many, { width: 1536, height: 1280 }), expandedSlot = expanded.slot();
  const expandedPlayer = expanded.api.mount(expandedSlot, '/expanded.json', 'idle'); await expandedPlayer.ready; await flush();
  assert.equal(expandedSlot.dataset.avatarClip, 'variant0');
  assert.deepEqual(expanded.imageLoads.map(url => path.basename(url)), ['variant-0.webp', 'variant-16.webp'], 'mount loads current and one next variant only');
  expanded.tick(0);
  const visitedFrames = new Set();
  for (let frame = 0; frame < 30; frame++) { expanded.tick(frame * 50); visitedFrames.add(lastSource(expandedSlot).join(',')); }
  assert.equal(visitedFrames.size, 30, 'all thirty distinct atlas cells actually render');
  expandedPlayer.setMotion('variant16'); await flush();
  assert.equal(expandedSlot.dataset.avatarClip, 'variant16');
  assert.equal(expanded.imageLoads.filter(url => url.endsWith('variant-16.webp')).length, 1, 'next variant reuses its preloaded decode');
  assert.ok(expanded.imageLoads.some(url => url.endsWith('variant-32.webp')), 'next preload follows the current scene variant');
  expandedPlayer.setMotion('variant37'); await flush();
  assert.equal(expandedSlot.dataset.avatarClip, 'variant37', 'sheets beyond the former thirty-two limit are playable');
  for (let variant = 0; variant < 38; variant++) {
    expandedPlayer.setMotion(`variant${variant}`); await flush();
    assert.ok(expanded.imageInstances.filter(image => image.src).length <= 4, 'decoded idle cache stays within four sheets');
  }
  expanded.visibility(true);
  const loadsBeforeHiddenPreload = expanded.imageLoads.length;
  assert.equal(await expandedPlayer.preload('variant0'), false);
  assert.equal(expanded.imageLoads.length, loadsBeforeHiddenPreload, 'hidden players do not start speculative loads');
  expanded.visibility(false); await flush();
  assert.equal(await expandedPlayer.preload('variant7'), true);
  expandedPlayer.destroy();
  assert.equal(expanded.imageInstances.filter(image => image.src).length, 0, 'last unmount releases all decoded sheet sources');

  const queuedPack = structuredClone(many);queuedPack.clips.variant1.loop = false;
  const queued = environment(queuedPack, { width: 1536, height: 1280 }), queuedSlot = queued.slot();
  const queuedPlayer = queued.api.mount(queuedSlot, '/queued.json', 'variant0');await queuedPlayer.ready;await flush();
  queued.tick(0);queued.tick(400);
  assert.equal(queued.api.mount(queuedSlot, '/queued.json', 'variant16', { deferUntilLoop: true }), queuedPlayer);
  await flush();queuedPlayer.preload('variant32');await flush();
  queued.tick(800);queued.api.mount(queuedSlot, '/queued.json', 'variant16', { deferUntilLoop: true });
  await flush();queued.tick(1499);assert.equal(queuedSlot.dataset.avatarClip, 'variant0', 'deferred transition keeps the current loop until its boundary');
  queued.tick(1500);await flush();assert.equal(queuedSlot.dataset.avatarClip, 'variant16', 'queued clip starts on the boundary without being overridden by speculative preload');
  assert.equal(queued.imageLoads.filter(url => url.endsWith('variant-16.webp')).length, 1, 'repeated pending requests reuse the prepared decode');
  queued.tick(1600);queued.tick(1700);
  queuedPlayer.setMotion('variant32', { deferUntilLoop: true });await flush();
  queuedPlayer.setMotion('variant1');await flush();
  assert.equal(queuedSlot.dataset.avatarClip, 'variant1', 'real scene changes cancel a deferred transition immediately');
  queued.tick(1800);queued.tick(3300);await flush();
  assert.equal(queuedSlot.dataset.avatarClip, 'variant1', 'cancelled targets cannot return on a later boundary');
  assert.equal(queued.rafs.size, 0, 'completed one-shot stops while no transition is pending');
  queuedPlayer.setMotion('variant2', { deferUntilLoop: true });await flush();
  assert.equal(queuedSlot.dataset.avatarClip, 'variant2', 'a completed one-shot accepts a prepared target without waiting for a nonexistent loop');
  queuedPlayer.destroy();

  for (const loop of [true, false]) {
    const stillPack = structuredClone(many);stillPack.clips.variant0.frames = [0];stillPack.clips.variant0.durations = [100];stillPack.clips.variant0.loop = loop;
    const still = environment(stillPack, { width: 1536, height: 1280 }), stillSlot = still.slot();
    const stillPlayer = still.api.mount(stillSlot, '/still.json', 'variant0');await stillPlayer.ready;await flush();
    assert.equal(still.rafs.size, 0, 'single-frame clips do not run an animation clock');
    still.api.mount(stillSlot, '/still.json', 'variant16', { deferUntilLoop: true });await flush();
    assert.equal(stillSlot.dataset.avatarClip, 'variant16', `single-frame ${loop ? 'loop' : 'one-shot'} switches without waiting for an unreachable boundary`);
    assert.equal(still.rafs.size, 1, 'the new multi-frame clip starts normally');
    stillPlayer.destroy();assert.equal(still.imageInstances.filter(image => image.src).length, 0);
  }

  for (const loop of [true, false]) {
    const pendingPack = structuredClone(many);pendingPack.sheets.sheet16.src = 'deferred-target.webp';pendingPack.clips.variant0.loop = loop;
    const pending = environment(pendingPack, { width: 1536, height: 1280 }), pendingSlot = pending.slot();
    const pendingPlayer = pending.api.mount(pendingSlot, '/pending.json', 'variant0');await pendingPlayer.ready;await flush();
    pending.tick(0);pending.tick(700);pendingPlayer.setMotion('variant16', { deferUntilLoop: true });await flush();
    pending.tick(1500);await flush();assert.equal(pendingSlot.dataset.avatarClip, 'variant0', 'an unloaded target never removes the current character');
    if (!loop) assert.equal(pending.rafs.size, 0, 'one-shot holds its last frame while the target is still loading');
    pending.tick(1700);pending.completeImages();await flush();
    if (loop) {
      assert.equal(pendingSlot.dataset.avatarClip, 'variant0', 'mid-loop decode completion does not cause a hard switch');
      pending.tick(2999);assert.equal(pendingSlot.dataset.avatarClip, 'variant0');
      pending.tick(3000);await flush();
    }
    assert.equal(pendingSlot.dataset.avatarClip, 'variant16', loop ? 'loop switch resumes at the next boundary once ready' : 'one-shot commits its pending target when decoding finally finishes');
    pendingPlayer.destroy();assert.equal(pending.imageInstances.filter(image => image.src).length, 0);
  }

  const brokenQueuePack = structuredClone(many);brokenQueuePack.sheets.sheet16.src = 'missing.png';
  const brokenQueue = environment(brokenQueuePack, { width: 1536, height: 1280 }), brokenQueueSlot = brokenQueue.slot();
  const brokenQueuePlayer = brokenQueue.api.mount(brokenQueueSlot, '/broken-queue.json', 'variant0');await brokenQueuePlayer.ready;await flush();
  brokenQueue.tick(0);brokenQueuePlayer.setMotion('variant16', { deferUntilLoop: true });await flush();
  brokenQueue.tick(1500);await flush();
  assert.equal(brokenQueueSlot.dataset.avatarClip, 'variant0', 'unavailable deferred art falls back to a usable same-scene clip');
  assert.equal(brokenQueue.imageLoads.filter(url => url.endsWith('missing.png')).length, 1);
  assert.equal(brokenQueueSlot.dataset.avatarStatus, 'ready');brokenQueuePlayer.destroy();

  const missingVariant = structuredClone(many);
  missingVariant.sheets.sheet0.src = 'missing.png';
  const alternative = environment(missingVariant, { width: 1536, height: 1280 }), alternativeSlot = alternative.slot();
  const alternativePlayer = alternative.api.mount(alternativeSlot, '/alternatives.json', 'idle');
  assert.equal((await alternativePlayer.ready).status, 'ready'); await flush();
  assert.equal(alternativeSlot.dataset.avatarClip, 'variant16', 'missing first variant falls back within the same scene');
  for (let i = 0; i < 3; i++) { alternative.mutate(); await flush(); }
  assert.equal(alternative.imageLoads.filter(url => url.endsWith('missing.png')).length, 1, 'failed variant does not reload during unrelated DOM updates');
  alternativePlayer.setMotion('undefined-scene'); await flush();
  assert.equal(alternativeSlot.dataset.avatarClip, 'variant16', 'unknown scenes resolve to usable idle variant');
  alternativePlayer.destroy();

  const delayed = structuredClone(many);
  delayed.sheets.sheet7.src = 'deferred-7.webp'; delayed.sheets.sheet8.src = 'deferred-8.webp';
  const switching = environment(delayed, { width: 1536, height: 1280 }), switchingSlot = switching.slot();
  const switchingPlayer = switching.api.mount(switchingSlot, '/switching.json'); await switchingPlayer.ready; await flush();
  switchingPlayer.setMotion('variant7'); await flush();
  assert.equal(switchingSlot.dataset.avatarClip, 'variant0', 'previous rendered clip stays until next sheet is ready');
  switchingPlayer.setMotion('variant8'); await flush();
  assert.equal(switching.imageInstances.some(image => image.src?.endsWith('deferred-7.webp')), false, 'superseded in-flight decode is cancelled');
  switchingPlayer.setMotion('variant9'); await flush();
  switching.completeImages(); await flush();
  assert.equal(switchingSlot.dataset.avatarClip, 'variant9', 'late image completions cannot overwrite a newer motion');
  switchingPlayer.destroy();
  assert.equal(switching.imageInstances.filter(image => image.src).length, 0);

  // Validate the shipped pack dimensions/references independently of its grid shape.
  function webpSize(file) {
    const bytes = fs.readFileSync(file);
    assert.equal(bytes.toString('ascii', 0, 4), 'RIFF'); assert.equal(bytes.toString('ascii', 8, 12), 'WEBP');
    const format = bytes.toString('ascii', 12, 16);
    if (format === 'VP8X') {
      assert.ok(bytes[20] & 0x10, `${file} preserves transparency`);
      return { width: bytes.readUIntLE(24, 3) + 1, height: bytes.readUIntLE(27, 3) + 1 };
    }
    if (format === 'VP8L') {
      assert.equal(bytes[20], 0x2f);
      const bits = bytes.readUInt32LE(21);
      assert.ok(bits & 0x10000000, `${file} preserves transparency`);
      return { width: (bits & 0x3fff) + 1, height: ((bits >>> 14) & 0x3fff) + 1 };
    }
    throw new Error(`Expected transparent WebP for ${file}`);
  }
  const packDirectory = path.join(__dirname, '../internal/server/web/avatars/xiaomi-original');
  const shipped = JSON.parse(fs.readFileSync(path.join(packDirectory, 'manifest.json'), 'utf8'));
  const catalogue = JSON.parse(fs.readFileSync(path.join(__dirname, '../docs/design/virtual-avatar-generation.json'), 'utf8')).jobs;
  const audit = JSON.parse(fs.readFileSync(path.join(packDirectory, 'art-audit.json'), 'utf8'));
  const expectedIDs = catalogue.map(job => job.id).sort();
  const expectedScenes = [...new Set(catalogue.map(job => job.scene))].sort();
  assert.ok(expectedIDs.length >= 38, 'catalogue retains all thirty-eight promised variants');
  assert.ok(expectedScenes.length >= 16, 'catalogue retains all sixteen promised scenes');
  assert.deepEqual(Object.keys(shipped.clips).sort(), expectedIDs, 'every promised variant ships exactly once');
  assert.deepEqual(Object.keys(shipped.scenes).sort(), expectedScenes, 'every promised scene ships');
  assert.deepEqual(audit.map(record => record.id).sort(), expectedIDs, 'every variant has durable extraction evidence');
  const dimensions = new Map(), fingerprints = new Set();
  for (const [id, sheet] of Object.entries(shipped.sheets)) {
    const file = path.join(packDirectory, sheet.src), size = webpSize(file);
    dimensions.set(id, size);
    assert.equal(size.width % sheet.columns, 0, `${id} has integral frame widths`);
    assert.equal(size.height % sheet.rows, 0, `${id} has integral frame heights`);
    fingerprints.add(crypto.createHash('sha256').update(fs.readFileSync(file)).digest('hex'));
  }
  assert.ok(fs.existsSync(path.join(packDirectory, shipped.poster)));
  const requiredMotions = Object.keys(shipped.scenes);
  assert.equal(fingerprints.size, Object.keys(shipped.sheets).length, 'different shipped sheets cannot be byte-identical substitutes');
  for (const motion of requiredMotions) {
    const ids = shipped.scenes[motion];
    assert.ok(ids.length, `${motion} has a clip`);
    assert.deepEqual(ids.slice().sort(), catalogue.filter(job => job.scene === motion).map(job => job.id).sort(), `${motion} contains its own intended variants`);
    for (const id of ids) {
      const clip = shipped.clips[id], sheet = shipped.sheets[clip?.sheet];
      assert.ok(clip && sheet, `${motion}/${id} references a real sheet`);
      assert.equal(clip.frames.length, clip.durations.length, `${id} frame holds are complete`);
      assert.ok(clip.frames.every(frame => Number.isInteger(frame) && frame >= 0 && frame < sheet.columns * sheet.rows));
      assert.ok(clip.durations.every(duration => Number.isFinite(duration) && duration >= 40));
      assert.ok(new Set(clip.frames).size >= 30, `${id} references at least thirty distinct frames`);
      const record = audit.find(record => record.id === id), variant = shipped.variants[id];
      assert.equal(record.scene, motion);
      assert.equal(record.frameCount, clip.frames.length);
      assert.equal(record.frameHashes.length, record.frameCount);
      assert.equal(new Set(record.frameHashes).size, record.uniqueFrames);
      assert.ok(record.uniqueFrames >= 30, `${id} has at least thirty distinct extracted RGBA drawings`);
      assert.ok(record.frameHashes.every(hash => /^[a-f0-9]{64}$/.test(hash)));
      assert.equal(variant.frames, record.frameCount);
      assert.equal(variant.uniqueFrames, record.uniqueFrames);
      assert.equal(variant.loop, clip.loop);
      if (motion === 'completed' || motion === 'error') assert.equal(clip.loop, false, `${motion} stops after its reaction`);
      assert.deepEqual(Object.keys(record.files).sort(), [sheet.src, variant.animation, variant.poster].sort());
      for (const [name, expectedHash] of Object.entries(record.files)) {
        assert.equal(crypto.createHash('sha256').update(fs.readFileSync(path.join(packDirectory, name))).digest('hex'), expectedHash, `${name} matches the audited artifact`);
      }
      // Read RIFF frame records directly so a static WebP cannot masquerade as an animation.
      const animatedBytes = fs.readFileSync(path.join(packDirectory, variant.animation));
      let animatedFrames = 0, animationLoop = null;
      for (let offset = 12; offset + 8 <= animatedBytes.length;) {
        const kind = animatedBytes.toString('ascii', offset, offset + 4), length = animatedBytes.readUInt32LE(offset + 4);
        assert.ok(offset + 8 + length <= animatedBytes.length, `${id} WebP chunk is complete`);
        if (kind === 'ANMF') animatedFrames++;
        if (kind === 'ANIM') animationLoop = animatedBytes.readUInt16LE(offset + 12);
        offset += 8 + length + (length & 1);
      }
      assert.equal(animatedFrames, record.frameCount, `${id} standalone WebP contains every actual frame`);
      assert.equal(animationLoop, 0, `${id} downloadable preview loops independently of the state player`);
    }
  }
  const shippedEnvironment = environment(shipped, url => {
    const name = path.basename(new URL(url).pathname);
    const sheetID = Object.keys(shipped.sheets).find(id => path.basename(shipped.sheets[id].src) === name);
    return dimensions.get(sheetID);
  });
  const shippedSlot = shippedEnvironment.slot();
  const shippedPlayer = shippedEnvironment.api.mount(shippedSlot, '/avatars/xiaomi-original/manifest.json', requiredMotions[0]);
  assert.equal((await shippedPlayer.ready).status, 'ready');
  for (const motion of requiredMotions) {
    shippedPlayer.setMotion(motion); await flush();
    const id = shipped.scenes[motion][0];
    assert.equal(shippedSlot.dataset.avatarClip, id, `${motion} resolves to its own clip`);
  }
  shippedPlayer.destroy();
  console.log('PASS avatar-player: frame interpolation/premultiplied alpha/cache, deferred loop boundaries/late decode/one-shot, frame holds, playback rate/normalization/preserved progress, shared clock, pause/resume, detached cleanup, fallback, frame rectangles, 38 variants/30 frames, lazy preload/cancellation/cache limits, shipped transparent assets');
})().catch(error => { console.error(error); process.exitCode = 1; });
