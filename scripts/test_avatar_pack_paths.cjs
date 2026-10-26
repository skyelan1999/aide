'use strict';
const assert = require('node:assert/strict'), fs = require('node:fs'), os = require('node:os'), path = require('node:path');
const crypto = require('node:crypto'), {spawnSync} = require('node:child_process');
const hash = bytes => crypto.createHash('sha256').update(bytes).digest('hex');
function fixture(script) {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), 'aide-avatar-path-test-'));
  const enhanced = script.includes('enhanced');
  const input = path.join(root, '.agent-state', enhanced ? 'avatar-enhanced' : 'avatar-generation');
  const audit = path.join(root, '.agent-state', enhanced ? 'avatar-enhanced-packed' : 'avatar-packed');
  const output = path.join(root, 'internal/server/web/avatars', enhanced ? 'xiaomi-enhanced' : 'xiaomi-original');
  for (const dir of [input, audit, output, path.join(root, 'scripts'), path.join(root, 'docs/design')]) fs.mkdirSync(dir, {recursive: true});
  for (const file of [script, 'avatar_pack_paths.cjs']) if (fs.existsSync('scripts/' + file)) fs.copyFileSync('scripts/' + file, path.join(root, 'scripts', file));
  fs.copyFileSync('docs/design/virtual-avatar-generation.json', path.join(root, 'docs/design/virtual-avatar-generation.json'));
  const source = path.join(input, 'owned.png');fs.writeFileSync(source, 'owned drawing fixture');
  const job = {id: 'idle-v1', scene: 'idle', label: 'updated owned label', source};
  const record = {pipelineVersion: 2, id: job.id, scene: job.scene, label: 'prior label', sourceSHA256: hash(fs.readFileSync(source)),
    atlas: 'idle-v1-atlas.webp', animation: 'idle-v1.webp', poster: 'idle-v1-poster.webp', reviewed: true, frameCount: 36};
  const write = (target, value) => fs.writeFileSync(target, JSON.stringify(value));
  const sharp = path.join(root, 'owned-sharp.cjs');fs.writeFileSync(sharp, "module.exports=function(){throw Error('image processor must not run in cached/path-only fixture');};");
  return {root, input, audit, output, job, record, write, run() {
    write(path.join(input, 'owned-job.json'), job);write(path.join(audit, 'idle-v1.json'), record);
    return spawnSync(process.execPath, [path.join(root, 'scripts', script)], {cwd: root, encoding: 'utf8', timeout: 10000,
      env: {...process.env, AIDE_SHARP_MODULE: sharp}});
  }, close() {fs.rmSync(root, {recursive: true, force: true});}};
}
function scenario(script, callback) {const f = fixture(script);try {callback(f);} finally {f.close();}}
for (const script of ['pack_avatar_art.cjs', 'pack_avatar_enhanced.cjs']) {
  scenario(script, f => {
    f.job.id = '../../escape';f.record.id = f.job.id;
    const escape = path.join(f.root, 'escape.json');f.write(escape, f.record);const before = fs.readFileSync(escape);
    const result = f.run();assert.notEqual(result.status, 0, 'traversal metadata must fail before cached audit writes');
    assert.deepEqual(fs.readFileSync(escape), before, 'escaped audit changed');
  });
  for (const kind of ['external-source', 'source-symlink', 'audit-symlink', 'output-root-symlink', 'unexpected-id', 'cached-record-id', 'cached-asset-path']) scenario(script, f => {
    const outside = path.join(f.root, 'owned-external');fs.mkdirSync(outside);
    const sentinel = path.join(outside, 'sentinel.png');fs.writeFileSync(sentinel, 'owned drawing fixture');
    if (kind === 'external-source') f.job.source = sentinel;
    if (kind === 'source-symlink') {const link = path.join(f.input, 'linked.png');fs.symlinkSync(sentinel, link);f.job.source = link;}
    if (kind === 'audit-symlink') {f.write(sentinel, f.record);fs.symlinkSync(sentinel, path.join(f.audit, 'idle-v1.json'));}
    if (kind === 'output-root-symlink') {fs.rmdirSync(f.output);fs.symlinkSync(outside, f.output);}
    if (kind === 'unexpected-id') f.job.id = 'unknown-v1';
    if (kind === 'cached-record-id') f.record.id = '../../escape';
    if (kind === 'cached-asset-path') f.record.atlas = '../../escape.webp';
    const before = fs.readFileSync(sentinel), result = f.run();
    assert.notEqual(result.status, 0, script + ': ' + kind + ' must reject');
    assert.doesNotMatch(result.stderr, /image processor must not run/, 'reject metadata before image processing');
    assert.deepEqual(fs.readFileSync(sentinel), before, script + ': ' + kind + ' changed foreign target');
  });
  for (const relative of [false, true]) scenario(script, f => {
    if (relative) f.job.source = 'owned.png';const result = f.run();assert.equal(result.status, 0, result.stderr);
    assert.equal(JSON.parse(fs.readFileSync(path.join(f.audit, 'idle-v1.json'))).label, f.job.label);
  });
}
console.log('PASS both packers reject traversal, external/symlink source, symlink audit/output roots and unknown IDs; valid cached relative artwork remains usable');
