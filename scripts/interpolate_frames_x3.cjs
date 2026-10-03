#!/usr/bin/env node
'use strict';
// Frame interpolation: insert 2 midpoint frames between each pair of original frames,
// tripling frame count. Reads the already-packed xiaomi-enhanced atlas, re-emits.
const fs = require('node:fs/promises');
const path = require('node:path');
const crypto = require('node:crypto');
const { createRequire } = require('node:module');
const root = path.resolve(__dirname, '..');
const sharp = (() => {
  try { return require('sharp'); }
  catch {
    const d = path.join(require('os').homedir(), '.cache/codex-runtimes/codex-primary-runtime/dependencies/node');
    return createRequire(path.join(d, 'package.json'))('sharp');
  }
})();
const size = 256, columns = 6;
const hash = v => crypto.createHash('sha256').update(v).digest('hex');

async function main() {
  const dir = path.join(root, 'internal/server/web/avatars/xiaomi-enhanced');
  const manifest = JSON.parse(await fs.readFile(path.join(dir, 'manifest.json'), 'utf8'));
  const newClips = {}, newVariants = {}, newSheets = {};
  const audit = [];

  for (const [id, sheet] of Object.entries(manifest.sheets)) {
    const atlasPath = path.join(dir, sheet.src);
    const { data, info } = await sharp(atlasPath).ensureAlpha().raw().toBuffer({ resolveWithObject: true });
    const cols = sheet.columns, rows = sheet.rows, frames = [];
    for (let i = 0; i < cols * rows; i++) {
      const cx = (i % cols) * size, cy = Math.floor(i / cols) * size;
      const frame = Buffer.alloc(size * size * 4);
      for (let y = 0; y < size; y++) data.copy(frame, (y * size) * 4, ((cy + y) * info.width + cx) * 4, ((cy + y) * info.width + cx + size) * 4);
      frames.push(frame);
    }
    // Interpolate: between frame[i] and frame[i+1], insert 2 blends at t=1/3, 2/3
    const out = [];
    for (let i = 0; i < frames.length; i++) {
      out.push(frames[i]);
      if (i < frames.length - 1) {
        for (const t of [1/3, 2/3]) {
          const a = frames[i], b = frames[i + 1], m = Buffer.alloc(size * size * 4);
          for (let p = 0; p < size * size * 4; p += 4) {
            // Premultiplied-alpha blend for correct compositing
            const aa = a[p+3], ab = b[p+3];
            if (aa === 0 && ab === 0) { m[p+3]=0; continue; }
            m[p]   = Math.round((a[p]*aa*(1-t) + b[p]*ab*t) / Math.max(1, aa*(1-t)+ab*t));
            m[p+1] = Math.round((a[p+1]*aa*(1-t) + b[p+1]*ab*t) / Math.max(1, aa*(1-t)+ab*t));
            m[p+2] = Math.round((a[p+2]*aa*(1-t) + b[p+2]*ab*t) / Math.max(1, aa*(1-t)+ab*t));
            m[p+3] = Math.round(aa*(1-t) + ab*t);
          }
          out.push(m);
        }
      }
    }

    // Repack atlas
    const outRows = Math.ceil(out.length / columns);
    const atlasBuf = Buffer.alloc(size * columns * size * outRows * 4);
    for (let idx = 0; idx < out.length; idx++) {
      const x = idx % columns * size, y = Math.floor(idx / columns) * size;
      for (let r = 0; r < size; r++) out[idx].copy(atlasBuf, ((y + r) * size * columns + x) * 4, r * size * 4, (r + 1) * size * 4);
    }
    await sharp(atlasBuf, { raw: { width: columns*size, height: outRows*size, channels: 4 } })
      .webp({ quality: 91, alphaQuality: 100, effort: 5 }).toFile(atlasPath);

    // Animated webp — duration divided by 3 to keep total length
    const origDuration = manifest.clips[id].durations[0] || 75;
    const newDuration = Math.round(origDuration / 3);
    await sharp(Buffer.concat(out), { raw: { width: size, height: size*out.length, channels: 4, pageHeight: size } })
      .webp({ quality: 87, alphaQuality: 100, effort: 4, loop: manifest.clips[id].loop ? 0 : 1, delay: Array(out.length).fill(newDuration) })
      .toFile(path.join(dir, `${id}.webp`));

    const clip = manifest.clips[id];
    newClips[id] = { sheet: id, frames: Array.from({length: out.length},(_,i)=>i), durations: Array(out.length).fill(newDuration), loop: clip.loop };
    newSheets[id] = { src: sheet.src, columns, rows: outRows };
    const v = manifest.variants[id];
    newVariants[id] = { ...v, frames: out.length, uniqueFrames: out.length, duration: newDuration };
    audit.push({ id, originalFrames: frames.length, newFrames: out.length, duration: newDuration });
    console.log(`${id}: ${frames.length} -> ${out.length} frames @ ${newDuration}ms`);
  }

  manifest.sheets = newSheets;
  manifest.clips = newClips;
  manifest.variants = newVariants;
  manifest.provenance = { ...manifest.provenance, interpolation: 'alpha-blend x3 between original frames', interpolatedAt: new Date().toISOString() };
  await fs.writeFile(path.join(dir, 'manifest.json'), JSON.stringify(manifest, null, 2));
  await fs.writeFile(path.join(dir, 'frame-interpolation-audit.json'), JSON.stringify(audit, null, 2));
  console.log('Done. Total variants:', Object.keys(newVariants).length);
}
main().catch(e => { console.error(e); process.exit(1); });
