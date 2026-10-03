#!/usr/bin/env node
'use strict';
// Stabilize v4: standing = bottom+headX fixed; lying = centroid centered. No smoothing.
const fs = require('node:fs/promises');
const path = require('node:path');
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

async function main() {
  const dir = path.join(root, 'internal/server/web/avatars/xiaomi-enhanced');
  const manifest = JSON.parse(await fs.readFile(path.join(dir, 'manifest.json'), 'utf8'));

  for (const [id, sheet] of Object.entries(manifest.sheets)) {
    const atlasPath = path.join(dir, sheet.src);
    const { data, info } = await sharp(atlasPath).ensureAlpha().raw().toBuffer({ resolveWithObject: true });
    const cols = sheet.columns, rows = sheet.rows;
    const frames = [];
    for (let i = 0; i < cols * rows; i++) {
      const cx = (i % cols) * size, cy = Math.floor(i / cols) * size;
      const f = Buffer.alloc(size * size * 4);
      for (let y = 0; y < size; y++) data.copy(f, (y * size) * 4, ((cy + y) * info.width + cx) * 4, ((cy + y) * info.width + cx + size) * 4);
      frames.push(f);
    }

    const meas = frames.map(f => {
      let minX=size, maxX=0, minY=size, maxY=0;
      for (let y=0;y<size;y++) for (let x=0;x<size;x++) {
        const a = f[(y*size+x)*4+3];
        if (a > 20) { if(x<minX)minX=x; if(x>maxX)maxX=x; if(y<minY)minY=y; if(y>maxY)maxY=y; }
      }
      return { minX, maxX, minY, maxY, w:maxX-minX+1, h:maxY-minY+1 };
    });
    const maxW = Math.max(...meas.map(m=>m.w)), maxH = Math.max(...meas.map(m=>m.h));
    const scene = id.split('-')[0];
    const lying = scene === 'sleeping' || (scene === 'idle' && id === 'idle-v2') || (scene === 'reading') || maxW/maxH > 1.1;

    const scale = lying ? Math.min(190/maxW, 180/maxH) : Math.min(180/maxH, 200/maxW);
    const out = [];
    for (let i = 0; i < frames.length; i++) {
      const m = meas[i];
      if (m.w < 2) { out.push(frames[i]); continue; }
      const w = m.w, h = m.h;
      const cropped = Buffer.alloc(w*h*4);
      for (let y=0;y<h;y++) frames[i].copy(cropped, y*w*4, ((m.minY+y)*size+m.minX)*4, ((m.minY+y)*size+m.minX+w)*4);
      const nw = Math.round(w*scale), nh = Math.round(h*scale);
      const scaled = await sharp(cropped, { raw: { width: w, height: h, channels: 4 } })
        .resize({ width: nw, height: nh, kernel: 'lanczos3' }).png().toBuffer();

      let left, top;
      if (lying) {
        // centroid center
        let sx=0, sy=0, n=0;
        for (let y=0;y<h;y++) for (let x=0;x<w;x++) {
          const a = cropped[(y*w+x)*4+3];
          if (a>20) { sx+=x; sy+=y; n++; }
        }
        const ccx = n?sx/n:w/2, ccy = n?sy/n:h/2;
        left = Math.max(0, Math.min(size-nw, Math.round(128 - ccx*scale)));
        top = Math.max(0, Math.min(size-nh, Math.round(140 - ccy*scale)));
      } else {
        // standing: bottom fixed at 236, head-X anchored
        let hx=0, hn=0;
        for (let y=0;y<Math.min(100,h);y++) for (let x=0;x<w;x++) {
          const a = cropped[(y*w+x)*4+3];
          if (a>20) { hx+=x; hn++; }
        }
        const headCX = hn?hx/hn:w/2;
        left = Math.max(0, Math.min(size-nw, Math.round(128 - headCX*scale)));
        top = Math.max(0, Math.round(236 - nh));
      }
      const canvas = Buffer.alloc(size*size*4);
      await sharp(canvas, { raw: { width: size, height: size, channels: 4 } })
        .composite([{ input: scaled, left, top }]).raw().toBuffer({ resolveWithObject: true })
        .then(r => r.data.copy(canvas));
      out.push(canvas);
    }

    const atlasBuf = Buffer.alloc(size*columns*size*rows*4);
    for (let idx=0; idx<out.length; idx++) {
      const x=idx%columns*size, y=Math.floor(idx/columns)*size;
      for (let r=0;r<size;r++) out[idx].copy(atlasBuf, ((y+r)*size*columns+x)*4, r*size*4, (r+1)*size*4);
    }
    await sharp(atlasBuf, { raw: { width: columns*size, height: rows*size, channels: 4 } })
      .webp({ quality: 85, alphaQuality: 90, effort: 4 }).toFile(atlasPath);
    const clip = manifest.clips[id];
    const delay = clip.durations[0] || 75;
    await sharp(Buffer.concat(out), { raw: { width: size, height: size*out.length, channels: 4, pageHeight: size } })
      .webp({ quality: 80, alphaQuality: 90, effort: 4, loop: clip.loop?0:1, delay: Array(out.length).fill(delay) })
      .toFile(path.join(dir, `${id}.webp`));
    await sharp(out[0], { raw: { width: size, height: size, channels: 4 } })
      .webp({ quality: 90 }).toFile(path.join(dir, `${id}-poster.webp`));
    console.log(`${id}: ${lying?'LIE':'STAND'}`);
  }
  console.log('Done.');
}
main().catch(e=>{console.error(e);process.exit(1);});
