#!/usr/bin/env node
'use strict';
// Technical sprite extraction only: generated drawings are never interpolated or duplicated.
// Usage: node scripts/pack_avatar_art.cjs --help
// External dependency: Sharp. Set AIDE_SHARP_MODULE to its package directory when
// it is installed outside this repository; --check-deps checks without writing assets.
const fs = require('node:fs/promises');
const path = require('node:path');
const crypto = require('node:crypto');
const { createRequire } = require('node:module');
const root = path.resolve(__dirname, '..');
const cliArgs = process.argv.slice(2);
const usage = `Usage: node scripts/pack_avatar_art.cjs [variant-id ...] [options]

Inputs:  .agent-state/avatar-generation/{variant-id}.json
Outputs: internal/server/web/avatars/xiaomi-original/
Reviews: .agent-state/avatar-packed/ (frame hashes and contact sheets)

Options:
  --force          Re-extract selected source drawings even when cached.
  --reviewed       Mark selected drawings reviewed after visual inspection.
  --publish        Publish the manifest only when all 38 variants are reviewed.
  --allow-partial  Allow an incomplete development manifest with --publish.
  --check-deps     Check Sharp availability without reading or writing artwork.
  --help, -h       Show this help without requiring Sharp.

Sharp setup (choose an external tools directory to leave this repository unchanged):
  npm install --prefix /path/to/avatar-tools sharp
  AIDE_SHARP_MODULE=/path/to/avatar-tools/node_modules/sharp node scripts/pack_avatar_art.cjs --check-deps

Dependency lookup: explicit AIDE_SHARP_MODULE; otherwise normal require('sharp'),
then AIDE_SHARP_ROOT/node_modules/sharp when provided, or the current user's
optional Codex dependency cache. No dependencies are installed automatically.
`;
if (cliArgs.includes('--help') || cliArgs.includes('-h')) { console.log(usage); process.exit(0); }
let sharp;
try {
  if (process.env.AIDE_SHARP_MODULE) sharp = require(path.resolve(process.env.AIDE_SHARP_MODULE));
  else {
    try { sharp = require('sharp'); }
    catch {
      const dependencyRoot = process.env.AIDE_SHARP_ROOT || path.join(require('node:os').homedir(), '.cache/codex-runtimes/codex-primary-runtime/dependencies/node');
      sharp = createRequire(path.join(path.resolve(dependencyRoot), 'package.json'))('sharp');
    }
  }
  if (typeof sharp !== 'function') throw new Error('The selected module does not export the Sharp image processor.');
} catch (error) {
  console.error(`Unable to load Sharp${process.env.AIDE_SHARP_MODULE ? ' from AIDE_SHARP_MODULE' : ''}: ${error.message}\n\nInstall Sharp in an external tools directory with:\n  npm install --prefix /path/to/avatar-tools sharp\nThen set AIDE_SHARP_MODULE=/path/to/avatar-tools/node_modules/sharp.\nRun with --help for usage. No artwork was changed.`);
  process.exit(1);
}
if (cliArgs.includes('--check-deps')) { console.log(`Sharp ${sharp.versions?.sharp || 'available'}; dependency check passed. No artwork was changed.`); process.exit(0); }
const inputDir = path.join(root, '.agent-state/avatar-generation');
const outputDir = path.join(root, 'internal/server/web/avatars/xiaomi-original');
const auditDir = path.join(root, '.agent-state/avatar-packed');
const size = 256, columns = 6;
const hash = value => crypto.createHash('sha256').update(value).digest('hex');
const median = values => values.slice().sort((a, b) => a - b)[Math.floor(values.length / 2)];
const clamp = (value, min, max) => Math.min(max, Math.max(min, value));

function components(data, width, height) {
  const count = width * height, labels = new Int32Array(count), stack = new Int32Array(count), items = [];
  for (let pixel = 0; pixel < count; pixel++) {
    if (labels[pixel] || data[pixel * 4 + 3] < 48) continue;
    const id = items.length + 1;
    let top = 0, area = 0, minX = width, maxX = 0, minY = height, maxY = 0;
    stack[top++] = pixel; labels[pixel] = id;
    while (top) {
      const index = stack[--top], x = index % width, y = Math.floor(index / width);
      area++; minX = Math.min(minX, x); maxX = Math.max(maxX, x); minY = Math.min(minY, y); maxY = Math.max(maxY, y);
      for (const next of [x ? index - 1 : -1, x < width - 1 ? index + 1 : -1, y ? index - width : -1, y < height - 1 ? index + width : -1]) {
        if (next >= 0 && !labels[next] && data[next * 4 + 3] >= 48) { labels[next] = id; stack[top++] = next; }
      }
    }
    items.push({ id, area, minX, maxX, minY, maxY, cx: (minX + maxX) / 2, cy: (minY + maxY) / 2 });
  }
  return { labels, items };
}

function detectFrames(data, width, height) {
  const { labels, items } = components(data, width, height);
  const substantial = items.filter(item => item.area > width * height / 500);
  if (!substantial.length) throw new Error('No character silhouettes detected');
  const normalArea = median(substantial.map(item => item.area));
  const major = substantial.filter(item => item.area > normalArea * 0.3);
  const normalHeight = median(major.map(item => item.maxY - item.minY));
  const rows = [];
  for (const component of major.sort((a, b) => a.cy - b.cy)) {
    let row = rows.find(candidate => Math.abs(candidate.center - component.cy) < normalHeight * 0.5);
    if (!row) { row = { center: component.cy, items: [] }; rows.push(row); }
    row.items.push(component); row.center = median(row.items.map(item => item.cy));
  }
  const warnings = [], frames = [], coreMap = new Map();
  for (const [rowIndex, row] of rows.entries()) {
    const ordinaryWidth = median(row.items.map(item => item.maxX - item.minX + 1));
    const ordinaryArea = median(row.items.map(item => item.area));
    row.frames = [];
    for (const component of row.items.sort((a, b) => a.cx - b.cx)) {
      const widthRatio = (component.maxX - component.minX + 1) / ordinaryWidth;
      const parts = widthRatio > 1.6 && component.area > ordinaryArea * 1.5 ? Math.round(widthRatio) : 1;
      const cuts = [component.minX];
      for (let part = 1; part < parts; part++) {
        const expected = component.minX + (component.maxX - component.minX + 1) * part / parts;
        let best = Math.round(expected), least = Infinity;
        for (let x = Math.round(expected - ordinaryWidth * 0.12); x <= expected + ordinaryWidth * 0.12; x++) {
          let ink = 0;
          for (let y = component.minY; y <= component.maxY; y++) if (labels[y * width + x] === component.id) ink += data[(y * width + x) * 4 + 3];
          const score = ink + Math.abs(x - expected) * 8;
          if (score < least) { least = score; best = x; }
        }
        cuts.push(best);
      }
      cuts.push(component.maxX + 1);
      if (parts > 1) warnings.push(`row ${rowIndex + 1}: ${parts} touching characters separated at alpha valleys`);
      for (let part = 0; part < parts; part++) {
        const frame = { componentID: component.id, left: cuts[part], right: cuts[part + 1], top: component.minY, bottom: component.maxY,
          row: rowIndex, pixels: [], faceX: [], minX: width, maxX: 0, minY: height, maxY: 0, clippedEdges: 0 };
        row.frames.push(frame);
        if (!coreMap.has(component.id)) coreMap.set(component.id, []);
        coreMap.get(component.id).push(frame);
      }
    }
    row.frames.sort((a, b) => a.left - b.left);
    row.baseline = median(row.frames.map(frame => frame.bottom));
    for (const frame of row.frames) { frame.baseline = row.baseline; frame.index = frames.length; frames.push(frame); }
  }
  const owners = new Int32Array(items.length + 1); owners.fill(-1);
  for (const item of items) {
    if (coreMap.has(item.id) || item.area < 8) continue;
    let nearest = null, minimum = Infinity;
    for (const frame of frames) {
      const dx = Math.max(frame.left - item.cx, 0, item.cx - frame.right), dy = Math.max(frame.top - item.cy, 0, item.cy - frame.bottom);
      const centerDistance = (item.cx - (frame.left + frame.right) / 2) ** 2 + (item.cy - (frame.top + frame.bottom) / 2) ** 2;
      const score = dx * dx + dy * dy + 0.025 * centerDistance;
      if (score < minimum) { minimum = score; nearest = frame; }
    }
    if (minimum < (normalHeight * 0.4) ** 2) owners[item.id] = nearest.index;
  }
  for (let pixel = 0; pixel < labels.length; pixel++) {
    const x = pixel % width, y = Math.floor(pixel / width), alpha = data[pixel * 4 + 3];
    if (alpha < 20) continue;
    let label = labels[pixel];
    if (!label) {
      // Preserve one-pixel antialiasing attached to a real component; discard remote matte speckles.
      for (const candidate of [x ? pixel - 1 : -1, x < width - 1 ? pixel + 1 : -1, y ? pixel - width : -1, y < height - 1 ? pixel + width : -1]) {
        if (candidate >= 0 && labels[candidate]) { label = labels[candidate]; break; }
      }
    }
    if (!label) continue;
    const cores = coreMap.get(label);
    const frame = cores ? (cores.find(candidate => x >= candidate.left && x < candidate.right) || (x >= cores.at(-1).right ? cores.at(-1) : cores[0])) : frames[owners[label]];
    if (!frame) continue;
    frame.pixels.push(pixel);
    frame.minX = Math.min(frame.minX, x); frame.maxX = Math.max(frame.maxX, x);
    frame.minY = Math.min(frame.minY, y); frame.maxY = Math.max(frame.maxY, y);
    if (alpha > 160 && (x === 0 || x === width - 1 || y === 0 || y === height - 1)) frame.clippedEdges++;
    const [r, g, b] = data.subarray(pixel * 4, pixel * 4 + 3);
    if (y > frame.top + (frame.bottom - frame.top) * 0.2 && y < frame.top + (frame.bottom - frame.top) * 0.7 && r > 175 && g > 125 && b > 95 && r - g > 12 && g - b > 4) frame.faceX.push(x);
  }
  for (const frame of frames) {
    frame.anchor = frame.faceX.length > 50 ? median(frame.faceX) : (frame.left + frame.right) / 2;
    delete frame.faceX;
    if (frame.clippedEdges > 12) warnings.push(`frame ${frame.index + 1}: ${frame.clippedEdges} opaque pixels touch the source boundary`);
  }
  if (rows.some(row => row.frames.length !== 6)) warnings.push(`adaptive row counts: ${rows.map(row => row.frames.length).join('/')}`);
  return { frames, warnings, rowCounts: rows.map(row => row.frames.length) };
}

async function pack(job, options) {
  const source = await fs.readFile(job.source), fingerprint = hash(source);
  const auditPath = path.join(auditDir, job.id + '.json');
  let prior;
  try {
    prior = JSON.parse(await fs.readFile(auditPath, 'utf8'));
    if (!options.force && prior.sourceSHA256 === fingerprint && prior.pipelineVersion === 2) {
      // Descriptive metadata may be corrected after reviewing the same drawings.
      if (prior.label !== job.label || prior.scene !== job.scene) {
        prior.label = job.label; prior.scene = job.scene;
        await fs.writeFile(auditPath, JSON.stringify(prior, null, 2) + '\n');
      }
      return prior;
    }
  } catch {}
  const { data, info } = await sharp(source).ensureAlpha().raw().toBuffer({ resolveWithObject: true });
  const { frames, warnings, rowCounts } = detectFrames(data, info.width, info.height);
  if (frames.length < 30) warnings.push(`insufficient distinct drawings: detected ${frames.length}, minimum 30`);
  if (frames.length > 40) throw new Error(`Unexpected ${frames.length} character regions; manual review required`);
  const maxLeft = Math.max(...frames.map(frame => frame.anchor - frame.minX));
  const maxRight = Math.max(...frames.map(frame => frame.maxX - frame.anchor));
  const above = Math.max(...frames.map(frame => frame.baseline - frame.minY));
  const below = Math.max(0, ...frames.map(frame => frame.maxY - frame.baseline));
  const scale = Math.min(116 / maxLeft, 116 / maxRight, 232 / (above + below));
  const baseline = 244 - below * scale;
  const outputs = [], hashes = [];
  for (const frame of frames) {
    const width = frame.maxX - frame.minX + 1, height = frame.maxY - frame.minY + 1;
    const cropped = Buffer.alloc(width * height * 4);
    for (const pixel of frame.pixels) {
      const x = pixel % info.width - frame.minX, y = Math.floor(pixel / info.width) - frame.minY;
      data.copy(cropped, (y * width + x) * 4, pixel * 4, pixel * 4 + 4);
    }
    const resized = await sharp(cropped, { raw: { width, height, channels: 4 } }).resize({ width: Math.max(1, Math.round(width * scale)), kernel: 'lanczos3' }).png().toBuffer();
    const left = Math.round(128 + (frame.minX - frame.anchor) * scale), top = Math.round(baseline + (frame.minY - frame.baseline) * scale);
    if (left < 0 || top < 0) throw new Error(`${job.id} frame ${frame.index}: alignment exceeded canvas`);
    const rendered = await sharp({ create: { width: size, height: size, channels: 4, background: '#00000000' } }).composite([{ input: resized, left, top }]).raw().toBuffer();
    hashes.push(hash(rendered)); outputs.push(rendered);
  }
  const uniqueFrames = new Set(hashes).size;
  if (uniqueFrames < 30) warnings.push(`only ${uniqueFrames} unique RGBA frame hashes`);
  const rows = Math.ceil(outputs.length / columns), atlas = Buffer.alloc(size * columns * size * rows * 4);
  for (let index = 0; index < outputs.length; index++) {
    const x = index % columns * size, y = Math.floor(index / columns) * size;
    for (let row = 0; row < size; row++) outputs[index].copy(atlas, ((y + row) * size * columns + x) * 4, row * size * 4, (row + 1) * size * 4);
  }
  const atlasName = `${job.id}-atlas.webp`, animationName = `${job.id}.webp`, posterName = `${job.id}-poster.webp`;
  await sharp(atlas, { raw: { width: columns * size, height: rows * size, channels: 4 } }).webp({ quality: 91, alphaQuality: 100, effort: 5 }).toFile(path.join(outputDir, atlasName));
  const duration = job.scene === 'sleeping' || job.scene === 'locked' ? 110 : job.scene === 'idle' ? 95 : 75;
  await sharp(Buffer.concat(outputs), { raw: { width: size, height: size * outputs.length, channels: 4, pageHeight: size } }).webp({ quality: 87, alphaQuality: 100, effort: 4, loop: 0, delay: Array(outputs.length).fill(duration) }).toFile(path.join(outputDir, animationName));
  await sharp(outputs[0], { raw: { width: size, height: size, channels: 4 } }).webp({ quality: 94, alphaQuality: 100 }).toFile(path.join(outputDir, posterName));
  const animationMeta = await sharp(path.join(outputDir, animationName), { animated: true }).metadata();
  if (animationMeta.pages !== outputs.length) throw new Error(`Animated WebP exported ${animationMeta.pages} pages instead of ${outputs.length}`);
  const audit = { pipelineVersion: 2, id: job.id, scene: job.scene, label: job.label, sourceSHA256: fingerprint,
    sourceSize: { width: info.width, height: info.height }, rowCounts, frameCount: outputs.length, uniqueFrames, frameHashes: hashes,
    atlas: atlasName, animation: animationName, poster: posterName, columns, rows, cell: size, duration, scale,
    extraction: 'alpha-connected components; attached antialiasing retained; detached matte speckles discarded; uniform aspect-preserving scale and per-row floor registration',
    warnings, needsReview: warnings.length > 0, usable: uniqueFrames >= 30,
    reviewed: !!options.reviewed || !!(prior?.reviewed && prior.sourceSHA256 === fingerprint && JSON.stringify(prior.frameHashes) === JSON.stringify(hashes)) };
  await fs.writeFile(auditPath, JSON.stringify(audit, null, 2) + '\n');
  // Contact sheet is reviewer evidence, not a product asset.
  await sharp(atlas, { raw: { width: columns * size, height: rows * size, channels: 4 } }).resize({ width: 1152 }).flatten({ background: '#edf1f6' }).jpeg({ quality: 90 }).toFile(path.join(auditDir, `${job.id}-contact.jpg`));
  return audit;
}

async function publish(records, allowPartial) {
  const expected = JSON.parse(await fs.readFile(path.join(root, 'docs/design/virtual-avatar-generation.json'), 'utf8')).jobs;
  const usable = [];
  for (const job of expected) {
    const record = records.find(candidate => candidate.id === job.id);
    if (!record?.usable || !record.reviewed || record.scene !== job.scene) continue;
    const current = JSON.parse(await fs.readFile(path.join(inputDir, job.id + '.json'), 'utf8'));
    if (record.sourceSHA256 !== hash(await fs.readFile(current.source))) throw new Error(`${job.id}: reviewed artwork is stale`);
    usable.push(record);
  }
  if (!allowPartial && usable.length !== expected.length) throw new Error(`Missing reviewed variants: ${expected.filter(job => !usable.some(record => record.id === job.id)).map(job => job.id).join(', ')}`);
  const scenes = {};
  for (const record of usable) (scenes[record.scene] ||= []).push(record.id);
  if (!allowPartial && (usable.length < 38 || Object.keys(scenes).length < 16)) throw new Error(`Not publishing incomplete pack: ${usable.length}/38 reviewed variants, ${Object.keys(scenes).length}/16 scenes`);
  const sheets = {}, clips = {}, variants = {};
  for (const record of usable) {
    sheets[record.id] = { src: record.atlas, columns: record.columns, rows: record.rows };
    clips[record.id] = { sheet: record.id, frames: Array.from({ length: record.frameCount }, (_, i) => i), durations: Array(record.frameCount).fill(record.duration), loop: !['done', 'completed', 'error', 'stopped'].includes(record.scene) };
    variants[record.id] = { scene: record.scene, label: record.label, poster: record.poster, animation: record.animation, frames: record.frameCount, uniqueFrames: record.uniqueFrames, loop: clips[record.id].loop };
  }
  const poster = usable.find(record => record.id === 'idle-v1')?.poster || usable[0]?.poster;
  if (!poster) throw new Error('No reviewed material to publish');
  const manifest = { version: 1, label: '小鲸 · 原画动态伙伴', poster, sheets, clips, scenes, variants,
    provenance: { format: 'individually generated animation drawings; alpha extraction and uniform registration; no repeated-frame padding', variants: usable.length, scenes: Object.keys(scenes).length, minimumUniqueFrames: Math.min(...usable.map(record => record.uniqueFrames)) } };
  await fs.writeFile(path.join(outputDir, 'manifest.json'), JSON.stringify(manifest, null, 2) + '\n');
  const publishedAudit = [];
  for (const record of usable) {
    const { id, scene, label, sourceSHA256, frameCount, uniqueFrames, frameHashes, warnings, extraction } = record;
    const files = {};
    for (const file of [record.atlas, record.animation, record.poster]) files[file] = hash(await fs.readFile(path.join(outputDir, file)));
    publishedAudit.push({ id, scene, label, sourceSHA256, frameCount, uniqueFrames, frameHashes, files, warnings, extraction });
  }
  await fs.writeFile(path.join(outputDir, 'art-audit.json'), JSON.stringify(publishedAudit, null, 2) + '\n');
  return { variants: usable.length, scenes: Object.keys(scenes).length };
}

(async () => {
  const args = cliArgs, ids = args.filter(arg => !arg.startsWith('--'));
  await fs.mkdir(outputDir, { recursive: true }); await fs.mkdir(auditDir, { recursive: true });
  const jobs = (await fs.readdir(inputDir)).filter(name => name.endsWith('.json')).sort();
  for (const file of jobs) {
    const job = JSON.parse(await fs.readFile(path.join(inputDir, file), 'utf8'));
    if (ids.length && !ids.includes(job.id)) continue;
    try {
      const record = await pack(job, { force: args.includes('--force'), reviewed: args.includes('--reviewed') });
      if (args.includes('--reviewed') && !record.reviewed) { record.reviewed = true; await fs.writeFile(path.join(auditDir, `${record.id}.json`), JSON.stringify(record, null, 2) + '\n'); }
      console.log(JSON.stringify({ id: record.id, frames: record.frameCount, unique: record.uniqueFrames, rows: record.rowCounts, warnings: record.warnings, reviewed: record.reviewed }));
    } catch (error) { console.error(JSON.stringify({ id: job.id, error: error.message })); process.exitCode = 1; }
  }
  if (args.includes('--publish')) {
    const records = [];
    for (const file of await fs.readdir(auditDir)) if (file.endsWith('.json')) records.push(JSON.parse(await fs.readFile(path.join(auditDir, file), 'utf8')));
    console.log(JSON.stringify({ published: await publish(records, args.includes('--allow-partial')) }));
  }
})().catch(error => { console.error(error); process.exitCode = 1; });
