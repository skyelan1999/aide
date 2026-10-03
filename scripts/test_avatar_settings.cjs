'use strict';
// Execute the shipped settings module with a deterministic clock, DOM, storage and manifest responses.
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const STORAGE_KEY = 'aide.virtual-avatars.v2';
const LEGACY_STORAGE_KEY = 'aide.virtual-avatars.v1';
const app = fs.readFileSync(path.join(__dirname, '../internal/server/web/app.js'), 'utf8');
const moduleStart = app.indexOf('(() => {', app.indexOf('// 虚拟形象：'));
const moduleEnd = app.indexOf("\n$('save-file')", moduleStart);
assert.ok(moduleStart >= 0 && moduleEnd > moduleStart);
const avatarModule = app.slice(moduleStart, moduleEnd);
// A character-wide overlay must never reserve a full-width band above the composer.
const avatarCSS = fs.readFileSync(path.join(__dirname, '../internal/server/web/style.css'), 'utf8');
const avatarComposerRules = [...avatarCSS.matchAll(/\.composer\.has-avatar\s*\{([^}]*)\}/g)];
assert.ok(avatarComposerRules.length, 'avatar composer styling exists');
for (const [, rule] of avatarComposerRules) {
  assert.match(rule, /margin-top\s*:\s*0\s*;/, 'avatar does not reduce the conversation height, including narrow screens');
  assert.doesNotMatch(rule, /(?:padding|height|margin)\s*:/, 'avatar does not reserve a full-width layout spacer');
}
assert.match(avatarCSS, /\.input-wrap \.composer-avatar-stage\s*\{[^}]*position:absolute[^}]*pointer-events:none/, 'the overlay stays local and passes pointer events through');
const plan = JSON.parse(fs.readFileSync(path.join(__dirname, '../docs/design/virtual-avatar-scenarios.json'), 'utf8'));
const fullManifest = { version: 1, scenes: {}, clips: {} };
const enhancedManifest = JSON.parse(fs.readFileSync(path.join(__dirname, '../internal/server/web/avatars/xiaomi-enhanced/manifest.json'), 'utf8'));
for (const scene of plan.scenes) {
  fullManifest.scenes[scene.id] = scene.variants.map((_, index) => `${scene.id}-v${index + 1}`);
  for (const id of fullManifest.scenes[scene.id]) fullManifest.clips[id] = { sheet: id, frames: Array.from({ length: 30 }, (_, index) => index), duration: 100 };
}
function environment(initial, manifest = fullManifest, initialKey = STORAGE_KEY) {
  const storage = new Map(initial ? [[initialKey, JSON.stringify(initial)]] : []);
  const intervals = [], events = new Map(), mounted = new Map();
  let clock = 100000, budgetRefreshes = 0;
  class FakeDate extends Date { static now() { return clock; } }
  class Element {
    constructor(tag, className = '', text = '') {
      this.tagName = tag.toUpperCase(); this.className = className; this.textContent = text;
      this.children = []; this.dataset = {}; this.attributes = {}; this.parentElement = null;
      this.style = { setProperty() {} }; this.value = ''; this.checked = false;
      this.classList = {
        contains: name => this.className.split(/\s+/).includes(name),
        toggle: (name, force) => { const parts = new Set(this.className.split(/\s+/).filter(Boolean)); const add = force ?? !parts.has(name); if (add) parts.add(name); else parts.delete(name); this.className = [...parts].join(' '); return add; }
      };
    }
    setAttribute(name, value) { this.attributes[name] = value; }
    append(...children) { for (const child of children) { child.remove(); child.parentElement = this; this.children.push(child); } }
    replaceChildren(...children) { for (const child of this.children) child.parentElement = null; this.children = []; this.append(...children); }
    remove() { if (this.parentElement) this.parentElement.children = this.parentElement.children.filter(child => child !== this); this.parentElement = null; }
  }
  const elements = new Map();
  const get = id => { if (!elements.has(id)) elements.set(id, new Element('div')); return elements.get(id); };
  const context = {
    console, Date: FakeDate, JSON, Map, Set, Object, Array, Number, String, RegExp,
    localStorage: { getItem: key => storage.get(key) ?? null, setItem: (key, value) => storage.set(key, value) },
    document: { body: get('body'), querySelector: selector => selector === '.input-wrap' ? get('input-wrap') : null },
    $: get, el: (tag, className = '', text = '') => new Element(tag, className, text),
    t: (text, ...args) => String(text).replace(/\{(\d+)\}/g, (_, index) => String(args[index])),
    state: { submitting: false, runPhase: {}, busy: false, session: null },
    MutationObserver: class { observe() {} },
    setTimeout: () => 1, clearTimeout() {},
    crypto: { randomUUID: () => 'generated-id' }, confirm: () => true, toast() {},
    FileReader: class { readAsDataURL(file) { this.result = file.content; this.onload(); } },
    fetch: async () => ({ ok: true, json: async () => manifest }),
    scheduleContextPreview: () => { budgetRefreshes++; },
    addEventListener: (name, callback) => events.set(name, callback),
    setInterval: callback => { intervals.push(callback); return intervals.length; },
    AideAvatarPlayer: { mount(slot, url, motion, options) { const record = { url, motion, options, playbackRate: 1, interpolationEnabled: true }; mounted.set(slot, record); return { preload() {}, setPlaybackRate(rate) { record.playbackRate = rate; }, setInterpolationEnabled(enabled) { record.interpolationEnabled = enabled; } }; }, unmount(slot) { mounted.delete(slot); } }
  };
  context.window = context;
  vm.runInNewContext(avatarModule, context, { filename: 'app.js:virtual-avatar-module' });
  return {
    context, get, mounted, storage,
    budgetRefreshes: () => budgetRefreshes,
    render: () => context.renderVirtualAvatarSettings(),
    stored: () => JSON.parse(storage.get(STORAGE_KEY)),
    tick(count = 1) { for (let i = 0; i < count; i++) intervals.forEach(callback => callback()); },
    elapse(ms) { clock += ms; },
    advance(ms) { clock += ms; this.tick(); },
    external(next, key = STORAGE_KEY) { storage.set(key, JSON.stringify(next)); events.get('storage')?.({ key }); },
    settled: () => new Promise(resolve => setImmediate(resolve)),
    stage: () => get('input-wrap').children[0]
  };
}
function descendants(element) { return [element, ...element.children.flatMap(descendants)]; }
function checkbox(host, label) {
  const match = descendants(host).find(element => element.tagName === 'LABEL' && element.children.some(child => child.textContent === label));
  assert.ok(match, `checkbox label exists: ${label}`);
  return match.children.find(child => child.tagName === 'INPUT' && child.type === 'checkbox');
}
function range(host) { return descendants(host).find(element => element.tagName === 'INPUT' && element.type === 'range'); }
function button(host, label) { return descendants(host).find(element => element.tagName === 'BUTTON' && element.textContent === label); }
function select(host, label) { return descendants(host).find(element => element.tagName === 'SELECT' && element.attributes['aria-label'] === label); }
const custom = { id: 'custom-whale', name: 'My drawing', builtin: false, assets: [{ id: 'my-idle', name: 'idle.png', motion: 'idle', src: 'data:image/png;base64,USER_IMAGE' }] };
const oldBuiltin = id => ({ id: 'builtin-' + id, builtin: true, name: 'Old rig', assets: [{ id: id + '-rig', src: '/avatars/xiaomi-rig.svg', motion: 'idle' }] });
const v5 = {
  version: 5,
  aide: { enabled: false, chat: false, lock: true, opacity: 55, activePack: 'builtin-aide', packs: [oldBuiltin('aide')] },
  xiaomi: { enabled: true, chat: true, lock: false, opacity: 67, activePack: custom.id, packs: [oldBuiltin('xiaomi'), custom] }
};
async function main() {
  for (const version of [2, 3, 4, 5]) {
    const migrated = environment({ ...v5, version }).stored();
    assert.equal(migrated.version, 6);
    assert.equal(migrated.avatar.packs.filter(pack => pack.builtin).length, 2, 'old builtins migrate to two independent shared packs');
    assert.ok(migrated.avatar.packs.some(pack => pack.id === 'builtin-whale'));
    assert.ok(migrated.avatar.packs.some(pack => pack.id === 'builtin-whale-original'));
    assert.equal(migrated.avatar.enabled, true); assert.equal(migrated.avatar.chat, true); assert.equal(migrated.avatar.lock, false);
    assert.equal(migrated.avatar.opacity, 67); assert.equal(migrated.avatar.activePack, custom.id);
    assert.deepEqual(migrated.avatar.packs.find(pack => pack.id === custom.id), custom, 'custom assets survive byte-for-byte');
    assert.equal(migrated.migration.preferences.aide.opacity, 55, 'original role preferences are recoverable');
    assert.deepEqual(environment(migrated).stored(), migrated, 'v6 reload keeps shared preferences');
  }
  const collision = structuredClone(v5);
  collision.aide.packs = [custom, { ...custom, name: 'Other drawing', assets: [{ ...custom.assets[0], src: 'data:image/png;base64,OTHER' }] }];
  const merged = environment(collision).stored().avatar.packs;
  assert.equal(merged.filter(pack => !pack.builtin).length, 2, 'identical custom packs deduplicate; different content with same id survives');
  assert.equal(new Set(merged.map(pack => pack.id)).size, merged.length, 'colliding IDs are renamed safely');
  const removed = structuredClone(v5); removed.aide.packs = []; removed.xiaomi.packs = [custom];
  assert.deepEqual(environment(removed).stored().avatar.packs, [custom], 'deleted builtin is never resurrected');
  const fresh = environment().stored(); assert.equal(fresh.avatar.enabled, false); assert.equal(fresh.avatar.packs.length, 2);
  const enhancedPack = fresh.avatar.packs.find(pack => pack.id === 'builtin-whale');
  const originalPack = fresh.avatar.packs.find(pack => pack.id === 'builtin-whale-original');
  assert.equal(enhancedPack.name, '小鲸鱼 - 精致增强动态包');
  assert.equal(originalPack.name, '小鲸鱼 - 原画动态包');
  assert.equal(enhancedPack.revision, 5); assert.equal(enhancedPack.assets.length, 1, 'each builtin references its own manifest, not per-frame thumbnails');
  assert.equal(enhancedPack.assets[0].manifest, '/avatars/xiaomi-enhanced/manifest.json');
  assert.equal(originalPack.assets[0].manifest, '/avatars/xiaomi-original/manifest.json');
  const savedOldBuiltin=structuredClone(fresh);savedOldBuiltin.avatar.packs[0].name='小鲸 · 原画动态包';savedOldBuiltin.avatar.packs[0].revision=2;
  savedOldBuiltin.avatar.packs = [savedOldBuiltin.avatar.packs[0]];
  const upgradedBuiltin=environment(savedOldBuiltin).stored();assert.equal(upgradedBuiltin.avatar.packs.find(pack => pack.id === 'builtin-whale').name,'小鲸鱼 - 精致增强动态包');assert.ok(upgradedBuiltin.avatar.packs.some(pack => pack.id === 'builtin-whale-original'),'upgrade registers the second builtin without changing the stable enhanced-pack identity');
  assert.equal(fresh.avatar.playbackRate, 1, 'new installs default to original speed');
  assert.equal(fresh.avatar.smoothFrames, true, 'frame interpolation defaults on without enabling a disabled avatar');
  const legacyConfig=structuredClone(fresh);legacyConfig.avatar.playbackRate=0.5;legacyConfig.avatar.smoothFrames=false;legacyConfig.avatar.packs.push(custom);
  const fromLegacy=environment(legacyConfig,fullManifest,LEGACY_STORAGE_KEY);
  assert.deepEqual(fromLegacy.stored(),legacyConfig,'v1 settings migrate once to isolated v2 key with custom packs intact');
  const savedBefore=fromLegacy.storage.get(STORAGE_KEY);
  fromLegacy.external(fresh,LEGACY_STORAGE_KEY);
  fromLegacy.tick();assert.equal(fromLegacy.storage.get(STORAGE_KEY),savedBefore,'old-tab v1 writes cannot overwrite speed/interpolation');
  const afterOldWrite=environment(JSON.parse(savedBefore)).stored();assert.equal(afterOldWrite.avatar.playbackRate,0.5);assert.equal(afterOldWrite.avatar.smoothFrames,false,'reload retains speed and smoothing despite legacy writes');
  const future=structuredClone(fresh);future.avatar.futurePreference={keep:true};assert.deepEqual(environment(future).stored().avatar.futurePreference,{keep:true},'future fields survive normalization');
  const rawExternal=structuredClone(fresh);delete rawExternal.avatar.smoothFrames;
  fromLegacy.external(rawExternal);assert.equal(fromLegacy.storage.get(STORAGE_KEY),JSON.stringify(rawExternal),'storage events read without rewriting normalized defaults');

  const noSmooth=structuredClone(fresh);noSmooth.avatar.smoothFrames=false;assert.equal(environment(noSmooth).stored().avatar.smoothFrames,false,'explicit off survives normalization');
  for (const [raw, expected] of [[undefined, 1], [0, 1], [-1, 1], ['bad', 1], [0.1, 0.25], [5, 4], ['1.25', 1.25]]) {
    const old = structuredClone(fresh); old.avatar.playbackRate = raw;
    assert.equal(environment(old).stored().avatar.playbackRate, expected, 'stored speed normalized safely');
  }
  const speedConfig = structuredClone(fresh); speedConfig.avatar.enabled = true;
  const speedEnv = environment(speedConfig), speedHost = speedEnv.render(); await speedEnv.settled();
  const speed = descendants(speedHost).find(element => element.attributes['aria-label'] === '播放速度倍率');
  assert.equal(speed.value, '1'); assert.equal(speed.disabled, false);
  speed.value = '2'; speed.oninput();
  assert.equal(speedEnv.stored().avatar.playbackRate, 2);
  assert.equal(speedEnv.mounted.size, 3, 'preview, composer and lock actors exist');
  assert.ok([...speedEnv.mounted.values()].every(player => player.playbackRate === 2), 'all three actors receive speed immediately');
  assert.equal(speedEnv.budgetRefreshes(), 0, 'playback speed never requests AI or changes token budget');
  const smooth=checkbox(speedHost,'自动平滑补帧');assert.equal(smooth.checked,true);assert.equal(smooth.disabled,false);
  smooth.checked=false;smooth.onchange();assert.equal(speedEnv.stored().avatar.smoothFrames,false);
  assert.ok([...speedEnv.mounted.values()].every(player=>player.interpolationEnabled===false),'switch immediately applies to preview/composer/lock');
  const smoothed= speedEnv.stored();smoothed.avatar.smoothFrames=true;speedEnv.external(smoothed);
  assert.equal(smooth.checked,true);assert.ok([...speedEnv.mounted.values()].every(player=>player.interpolationEnabled),'cross-tab interpolation synchronizes players and control');
  assert.equal(environment(speedEnv.stored()).stored().avatar.smoothFrames,true,'interpolation survives reload');
  assert.equal(speedEnv.budgetRefreshes(),0,'interpolation does not invoke AI or change token budget');
  speedEnv.get('lock-screen').hidden=true;speedEnv.tick();assert.equal(speedEnv.get('lock-avatars').children.length,0,'unlocked screen releases hidden lock animation');
  assert.equal(speedEnv.mounted.size,2,'only preview and composer stay mounted');
  speedEnv.get('lock-screen').hidden=false;speedEnv.get('lock-screen').classList.toggle('joining',true);speedEnv.tick();assert.equal(speedEnv.mounted.size,2,'joining veil does not mount lock avatar');
  speedEnv.get('lock-screen').classList.toggle('joining',false);speedEnv.tick();assert.equal(speedEnv.mounted.size,3,'visible lock animation is mounted on demand');

  for (const invalid of ['', '0', '-1', '5', 'invalid']) { speed.value = invalid; speed.oninput(); assert.equal(speedEnv.stored().avatar.playbackRate, 2); speed.onchange(); assert.equal(speed.value, '2'); }
  button(speedHost, '恢复 1 倍').onclick(); assert.equal(speedEnv.stored().avatar.playbackRate, 1);
  const externalSpeed = speedEnv.stored(); externalSpeed.avatar.playbackRate = 0.75; speedEnv.external(externalSpeed);
  assert.equal(speed.value, '0.75'); assert.ok([...speedEnv.mounted.values()].every(player => player.playbackRate === 0.75));
  assert.equal(environment(speedEnv.stored()).stored().avatar.playbackRate, 0.75, 'speed survives reload');
  speed.value = '0.25'; speed.oninput();
  speedEnv.context.state.session = { runs: [{ id: 'speed-run', status: 'running' }] }; speedEnv.context.state.runPhase = { 'speed-run': { phase: 'generating' } }; speedEnv.tick();
  const firstSlowClip = speedEnv.stage().children[0].dataset.clip;
  speedEnv.advance(5000); assert.equal(speedEnv.stage().children[0].dataset.clip, firstSlowClip, 'slow clips get enough time for one complete cycle');
  speedEnv.advance(7100); assert.notEqual(speedEnv.stage().children[0].dataset.clip, firstSlowClip, 'slow scene still rotates after its full duration');
  assert.equal(speedEnv.mounted.get(speedEnv.stage().children[0]).options.deferUntilLoop,true,'automatic same-scene variant changes wait for a cycle boundary');
  speedEnv.context.state.runPhase['speed-run'].phase='thinking';speedEnv.tick();assert.equal(speedEnv.mounted.get(speedEnv.stage().children[0]).options.deferUntilLoop,false,'real task scene changes take effect immediately');

  // A change of multiplier must not reinterpret the entire past dwell at the new rate.
  async function runningAt(rate) {
    const config=structuredClone(fresh);config.avatar.enabled=true;config.avatar.playbackRate=rate;
    const env=environment(config),host=env.render();await env.settled();
    env.context.state.session={runs:[{id:'rate-run',status:'running'}]};env.context.state.runPhase={'rate-run':{phase:'generating'}};env.tick();
    const input=descendants(host).find(element=>element.attributes['aria-label']==='播放速度倍率');
    return {env,input,clip:()=>env.stage().children[0].dataset.clip};
  }
  for (const channel of ['input','storage']) {
    const run=await runningAt(0.25),first=run.clip();
    for(let tick=0;tick<6;tick++)run.env.advance(800);
    run.env.elapse(200); // Change between polls: 5s real time, 1.25s animation time.
    if(channel==='input'){run.input.value='1';run.input.oninput();}
    else{const changed=run.env.stored();changed.avatar.playbackRate=1;run.env.external(changed);}
    assert.equal(run.clip(),first, `${channel}: speeding up never immediately replaces the current action`);
    run.env.advance(800);assert.equal(run.clip(),first, `${channel}: retains 1.25s already played instead of resetting`);
    run.env.advance(800);assert.equal(run.clip(),first, `${channel}: same-scene polling accumulates the new rate`);
    run.env.advance(149);assert.equal(run.clip(),first, `${channel}: time before the rate change uses the old multiplier`);
    run.env.advance(1);assert.notEqual(run.clip(),first, `${channel}: rotates after exactly the remaining 1.75s`);
  }
  const slowing=await runningAt(1),slowingFirst=slowing.clip();
  slowing.env.advance(800);slowing.env.advance(800);
  slowing.input.value='0.25';slowing.input.oninput();
  slowing.input.oninput(); // A repeated value must not reset the accumulated 1.6s.
  for(let tick=0;tick<6;tick++)slowing.env.advance(800);
  assert.equal(slowing.clip(),slowingFirst, 'slowing down keeps the remaining action visible for its full adjusted duration');
  slowing.env.advance(800);assert.notEqual(slowing.clip(),slowingFirst, 'slowing down retains progress and rotates at 1.6s + 5.6s × 0.25');
  const fast=await runningAt(4),fastFirst=fast.clip();
  for(let tick=0;tick<4;tick++)fast.env.advance(800);
  fast.input.value='1';fast.input.oninput();fast.env.advance(799);
  assert.equal(fast.clip(),fastFirst, 'even a completed fast action must dwell for at least four seconds');
  fast.env.advance(1);assert.notEqual(fast.clip(),fastFirst, 'four-second minimum remains exact after a speed change');
  const storageRefresh=await runningAt(4),storageFirst=storageRefresh.clip();
  storageRefresh.env.elapse(5000);
  storageRefresh.env.external(storageRefresh.env.stored());
  assert.equal(storageRefresh.clip(),storageFirst, 'storage refresh itself does not replace an action even if a rotation is due');
  storageRefresh.env.advance(800);assert.notEqual(storageRefresh.clip(),storageFirst, 'regular polling continues normal rotation after storage refresh');
  const earlyV6 = structuredClone(fresh); earlyV6.avatar.packs[0].revision = 1; earlyV6.avatar.packs[0].assets = [{ id: 'old-preview', src: '/old.webp' }]; earlyV6.avatar.packs.push(custom); earlyV6.avatar.opacity = 52; earlyV6.avatar.activePack = custom.id;
  const renewed = environment(earlyV6).stored(); assert.equal(renewed.avatar.packs[0].revision, 5); assert.equal(renewed.avatar.opacity, 52); assert.equal(renewed.avatar.activePack, custom.id); assert.deepEqual(renewed.avatar.packs[2], custom);
  earlyV6.avatar.packs = [custom]; assert.deepEqual(environment(earlyV6).stored().avatar.packs, [custom], 'builtin metadata refresh never resurrects a deleted pack');
  const oneBuiltin = structuredClone(fresh); oneBuiltin.avatar.packs = [enhancedPack];
  assert.deepEqual(environment(oneBuiltin).stored().avatar.packs, [enhancedPack], 'an intentionally deleted companion pack is not re-added on every read');
  const v1 = { version: 1, xiaomi: { active: custom.id, items: [{ id: custom.id, name: 'Old image', src: custom.assets[0].src }], opacity: 66 } };
  const migratedV1 = environment(v1).stored().avatar; assert.equal(migratedV1.enabled, true); assert.equal(migratedV1.activePack, custom.id); assert.equal(migratedV1.opacity, 66);

  const env = environment(v5, enhancedManifest), host = env.render(); await env.settled();
  assert.equal(button(host, '小秘'), undefined, 'no persona switch remains');
  assert.equal(descendants(host).find(element => element.attributes['aria-label'] === '播放速度倍率').disabled, true, 'native image-only packs explain fixed native playback speed');
  assert.equal(checkbox(host,'自动平滑补帧').disabled,true,'native GIF/WebP cannot use sprite interpolation');
  const enabled = checkbox(host, '启用虚拟形象'); env.tick(5); enabled.checked = false; enabled.onchange();
  assert.equal(env.stored().avatar.enabled, false, 'toggle persists after timer updates'); assert.equal(env.budgetRefreshes(), 1, 'enable state updates prompt budget');
  const opacity = range(host); env.tick(5); opacity.value = '42'; opacity.oninput(); assert.equal(env.stored().avatar.opacity, 42);
  const fromOtherTab = env.stored(); fromOtherTab.avatar.opacity = 80; fromOtherTab.avatar.lock = true;
  env.external(fromOtherTab); opacity.value = '63'; opacity.oninput(); assert.equal(env.stored().avatar.opacity, 63); assert.equal(env.stored().avatar.lock, true, 'cross-tab changes are preserved by controls already open');
  const builtinRadio = descendants(host).find(element => element.type === 'radio' && element.attributes['aria-label'] === '激活素材包 小鲸鱼 - 精致增强动态包');
  builtinRadio.checked = true; builtinRadio.onchange(); await env.settled();
  assert.equal(env.stored().avatar.activePack, 'builtin-whale'); assert.deepEqual(env.stored().avatar.packs.find(pack => pack.id === custom.id), custom);
  const on = checkbox(host, '启用虚拟形象'); on.checked = true; on.onchange(); await env.settled();
  const sceneSelect = select(host, '预览场景'); assert.equal(sceneSelect.children.length, 16);
  assert.ok(descendants(host).some(element => element.className === 'avatar-pack-count' && element.textContent === `16 个场景 · ${Object.keys(enhancedManifest.clips).length} 套动作`), 'counts derive from the selected enhanced manifest');
  const builtinThumbs = descendants(host).find(element => element.className === 'avatar-pack-assets'); assert.equal(builtinThumbs.children.length, 8); assert.ok(descendants(builtinThumbs).filter(element => element.tagName === 'IMG').every(image => image.src.endsWith('-poster.webp') && image.loading === 'lazy'), 'gallery uses at most8 lazy static posters');
  for (const scene of plan.scenes) {
    sceneSelect.value = scene.id; sceneSelect.onchange();
    const variants = select(host, '动作变体');
    const clips = enhancedManifest.scenes[scene.id];
    const highestVariant = Math.max(0, ...clips.map(id => Number(id.match(/-v(\d+)$/)?.[1] || 0)));
    assert.equal(variants.children.length, Math.max(scene.variants.length, highestVariant));
    assert.deepEqual(variants.children.map(option => !!option.disabled), variants.children.map((_, index) => !clips.some(id => Number(id.match(/-v(\d+)$/)?.[1] || 0) === index + 1)));
    const firstClip = enhancedManifest.clips[enhancedManifest.scenes[scene.id][0]];
    const status = descendants(host).find(element => element.className === 'avatar-preview-availability'); assert.ok(status.textContent.startsWith(`${firstClip.frames.length} 帧`), 'preview frame count comes from the selected manifest');
  }
  assert.equal(env.get('lock-avatars').children.length, 1, 'one lock actor');
  env.get('body').classList.toggle('assistant-mode', true); env.tick(); assert.equal(env.get('lock-avatars').children.length, 1, 'assistant mode uses same lock actor');
  assert.equal(env.stage().classList.contains('visible'), true);

  const state = env.context.state;
  for (const [tool, expected] of [['web_search', 'searching'], ['read_file', 'reading'], ['apply_patch', 'creating'], ['terminal_exec', 'executing']]) {
    state.runPhase = { r1: { done: false, phase: 'tool', toolName: tool } }; env.tick(); assert.equal(env.stage().dataset.motion, expected);
  }
  state.runPhase = { r1: { done: false, phase: 'generating' } }; env.tick(); assert.equal(env.stage().dataset.motion, 'generating');
  const char = env.stage().children[0]; const initialClip = char.dataset.clip;
  const generatedVariant = 'generating-v2';
  const completedVariant = 'completed-v3';
  assert.equal(env.context.aideAvatarCue({ scene: 'completed', variant: completedVariant, emotion: 'happy', intensity: 2 }), true); env.tick(); assert.equal(env.stage().dataset.motion, 'generating', 'AI feedback cannot claim success during generation');
  assert.equal(env.context.aideAvatarCue({ scene: 'generating', variant: generatedVariant, emotion: 'happy', intensity: 2 }), true);
  env.advance(3999); assert.equal(char.dataset.clip, initialClip, 'variant holds for minimum dwell');
  env.advance(2); assert.equal(char.dataset.clip, generatedVariant, 'valid matching scene cue selects requested variant after dwell');
  assert.equal(env.stage().dataset.emotion, 'happy'); assert.equal(env.stage().dataset.intensity, '2', 'accepted expression lasts for the selected clip');
  env.advance(4100); assert.notEqual(char.dataset.clip, generatedVariant, 'automatic selection avoids immediate repeat');
  assert.equal(env.context.aideAvatarCue({ scene: 'generating', variant: 'error-v2', emotion: 'happy', intensity: 2 }), false, 'cross-scene variant rejected');
  assert.equal(env.context.aideAvatarCue({ scene: 'generating', variant: 4, emotion: 'happy', intensity: 2 }), false);
  assert.equal(env.context.aideAvatarCue({ scene: 'generating', variant: 1, emotion: 'evil', intensity: 2 }), false);
  env.context.aideAvatarFeedback('completed', { runId: 'old-run' }); assert.equal(env.stage().dataset.motion, 'generating', 'late old-run completion does not replace current work');
  state.runPhase = {}; env.context.aideAvatarFeedback('failed', { runId: 'r1' }); assert.equal(env.stage().dataset.motion, 'error');
  env.context.aideAvatarFeedback('cancelled'); assert.equal(env.stage().dataset.motion, 'paused', 'cancel is not error');
  env.advance(6100); env.context.aideAvatarSignal('speaking', true); assert.equal(env.stage().dataset.motion, 'speaking');
  env.context.aideAvatarSignal('listening', true); assert.equal(env.stage().dataset.motion, 'listening');
  env.context.aideAvatarSignal('listening', false); env.context.aideAvatarSignal('speaking', false);
  state.session = { runs: [{ id: 'r1', status: 'running' }] }; state.runPhase = { r1: { phase: 'reasoning', status: 'awaiting_clarification' } }; env.tick(); assert.equal(env.stage().dataset.motion, 'awaiting_user', 'SSE approval state remains visible before session poll');
  state.session = { runs: [{ id: 'r2', status: 'running' }] }; state.runPhase = { r1: { phase: 'generating' }, r2: { phase: 'tool', toolName: 'read_file' } }; env.tick(); assert.equal(env.stage().dataset.motion, 'reading', 'residual phase from another session is ignored');
  state.session = { runs: [{ id: 'r2', status: 'completed' }] }; env.tick(); assert.equal(env.stage().dataset.motion, 'idle', 'finished phase cannot keep the avatar working');
  state.runPhase = {}; state.session = { runs: [{ status: 'awaiting_approval' }] }; env.tick(); assert.equal(env.stage().dataset.motion, 'awaiting_user');
  state.session = null; env.advance(46000); assert.equal(env.stage().dataset.motion, 'sleeping');
  env.context.aideAvatarFeedback('notification'); assert.equal(env.stage().dataset.motion, 'notification');
  const off = checkbox(host, '启用虚拟形象'); off.checked = false; off.onchange(); assert.equal(env.get('lock-avatars').children.length, 0); assert.equal(env.stage().classList.contains('visible'), false);
  assert.equal(env.context.aideAvatarEnabled(), false); assert.equal(environment(env.stored()).stored().avatar.enabled, false);

  const sparse = environment(fresh, { version: 1, clips: { idle: { frames: [0, 1, 2, 3, 4, 5] } } });
  const sparseHost = sparse.render(); await sparse.settled(); const sparseScene = select(sparseHost, '预览场景'); sparseScene.value = 'reading'; sparseScene.onchange();
  assert.equal(select(sparseHost, '动作变体').disabled, true, 'missing variants are not presented as implemented');
  assert.equal(descendants(sparseHost).find(element => element.className === 'avatar-preview-availability').textContent, '此素材包尚未提供该场景，展示可用形象');
  button(sparseHost, '删除').onclick(); assert.equal(sparse.stored().avatar.packs.length, 1); assert.equal(sparse.stored().avatar.packs[0].id, 'builtin-whale-original'); assert.equal(environment(sparse.stored()).stored().avatar.packs.length, 1, 'deleting one builtin remains effective after reload');


  const importEnv = environment(fresh), importHost = importEnv.render(); await importEnv.settled();
  const upload = descendants(importHost).find(element => element.type === 'file'); upload.files = [{ name: 'error-v3.webp', type: 'image/webp', size: 100, content: 'data:image/webp;base64,CUSTOM' }]; await upload.onchange();
  assert.equal(importEnv.stored().avatar.enabled, true); assert.equal(importEnv.budgetRefreshes(), 1, 'import enabling avatar refreshes budget'); assert.equal(importEnv.stored().avatar.packs.at(-1).assets[0].variant, 3);
  const externalDisabled = importEnv.stored(); externalDisabled.avatar.enabled = false; importEnv.external(externalDisabled); assert.equal(importEnv.budgetRefreshes(), 2, 'cross-tab disable refreshes budget');
  const incomplete = environment(fresh, { version: 1, scenes: { idle: ['idle-v1'], error: ['error-v3'] }, clips: { 'idle-v1': { frames: [0, 1] }, 'error-v3': { frames: Array.from({ length: 30 }, (_, i) => i) } }, variants: { 'error-v3': { label: '跺脚挥拳闹脾气再消气', poster: 'error-v3-poster.webp' } } });
  const incompleteHost = incomplete.render(); await incomplete.settled(); const incompleteScene = select(incompleteHost, '预览场景'); incompleteScene.value = 'error'; incompleteScene.onchange();
  const incompleteVariants = select(incompleteHost, '动作变体'); assert.equal(incompleteVariants.value, '2'); assert.deepEqual(incompleteVariants.children.map(option => !!option.disabled), [true, true, false], 'sparse v3 stays variant3, not mislabeled variant1');
  console.log('PASS avatar-settings: v1-v5→v6 merge, custom/dedup/deletion, shared settings persistence, 16-scene preview, state truth, cue validation/dwell/nonrepeat, single actor, fallback availability, shared playback multiplier');
}
main().catch(error => { console.error(error); process.exitCode = 1; });
