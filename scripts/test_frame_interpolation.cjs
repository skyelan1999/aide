'use strict';
const assert = require('node:assert/strict'), fs = require('node:fs'), os = require('node:os'), path = require('node:path');
const {spawnSync} = require('node:child_process'), crypto = require('node:crypto');
function fixture(marked = false) {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), 'aide-interpolation-test-'));
  const dir = path.join(root, 'internal/server/web/avatars/xiaomi-enhanced');fs.mkdirSync(dir, {recursive: true});
  fs.mkdirSync(path.join(root, 'scripts'));fs.copyFileSync('scripts/interpolate_frames_x3.cjs', path.join(root, 'scripts/interpolate_frames_x3.cjs'));
  if(fs.existsSync('scripts/avatar_asset_audit.cjs'))fs.copyFileSync('scripts/avatar_asset_audit.cjs', path.join(root, 'scripts/avatar_asset_audit.cjs'));
  const manifest = {sheets: {owned: {src: 'owned-atlas.webp', columns: 6, rows: 1}},
    clips: {owned: {sheet: 'owned', frames: [0, 2, 4], durations: [90, 120, 150], loop: true}},
    variants: {owned: {scene: 'idle',poster:'owned-poster.webp',animation:'owned.webp',frames:3,uniqueFrames:3}}, provenance: marked ? {interpolation: 'already expanded'} : {format: 'owned original'}};
  const write = () => fs.writeFileSync(path.join(dir, 'manifest.json'), JSON.stringify(manifest));write();
  fs.writeFileSync(path.join(dir, 'owned-atlas.webp'), 'owned original atlas');fs.writeFileSync(path.join(dir, 'owned.webp'), 'owned original animation');
  fs.writeFileSync(path.join(dir, 'owned-poster.webp'), 'owned original poster');
  const inputFiles=Object.fromEntries(['owned-atlas.webp','owned.webp','owned-poster.webp'].map(file=>[file,crypto.createHash('sha256').update(fs.readFileSync(path.join(dir,file))).digest('hex')]));
  fs.writeFileSync(path.join(dir,'art-audit.json'),JSON.stringify([{id:'owned',scene:'idle',sourceSHA256:'a'.repeat(64),frameCount:3,uniqueFrames:3,frameHashes:['b'.repeat(64),'c'.repeat(64),'d'.repeat(64)],files:inputFiles}]));
  const preload = path.join(root, 'fake-sharp.cjs');
  fs.writeFileSync(preload, `const Module=require('node:module'),fs=require('node:fs'),path=require('node:path');
const afs=require('node:fs/promises'),rename=afs.rename;afs.rename=async function(from,to){if(process.env.AIDE_INTERPOLATION_FIXTURE_AUDIT_FAIL==='1'&&path.basename(to)==='art-audit.json')throw Error('owned audit publication failure');return rename.call(this,from,to);};
const load=Module._load;Module._load=function(id,...args){if(id!=='sharp')return load.call(this,id,...args);
return function(input,options){return {ensureAlpha(){return this},raw(){return this},webp(value){this.encoding=value;return this},
async toBuffer(){fs.appendFileSync(path.join(${JSON.stringify(root)},'processor-reads'),'read\\n');const data=Buffer.alloc(6*256*256*4);for(let y=0;y<256;y++)for(let x=0;x<1536;x++){const offset=(y*1536+x)*4;data[offset]=Math.floor(x/256);data[offset+3]=255;}return {data,info:{width:1536,height:256}}},
async toFile(file){if(process.env.AIDE_INTERPOLATION_FIXTURE_FAIL==='1')throw Error('owned image encoding failure');fs.writeFileSync(file,JSON.stringify({raw:options.raw,encoding:this.encoding,first:input[0],last:input[(options.raw.height-1)*options.raw.width*4]}));}}}};`);
  return {root, dir, manifest, write, run(fail = false, auditFail = false) {const result=spawnSync(process.execPath, ['--require', preload, path.join(root, 'scripts/interpolate_frames_x3.cjs')],
    {cwd: root, encoding: 'utf8', timeout: 10000, env: {...process.env, AIDE_INTERPOLATION_FIXTURE_FAIL: fail ? '1' : '0', AIDE_INTERPOLATION_FIXTURE_AUDIT_FAIL: auditFail ? '1' : '0'}});assert.ifError(result.error);return result;},
    verify() {const result=spawnSync(process.execPath,[path.join(root,'scripts/avatar_asset_audit.cjs')],{cwd:root,encoding:'utf8',timeout:10000});assert.ifError(result.error);return result;},
    snapshot() {return Object.fromEntries(fs.readdirSync(dir).map(file => [file, crypto.createHash('sha256').update(fs.readFileSync(path.join(dir, file))).digest('hex')]));},
    close() {fs.rmSync(root, {recursive: true, force: true});}};
}
function scenario(marked, callback) {const f = fixture(marked);try {callback(f);} finally {f.close();}}
scenario(true, f => {
  const before = f.snapshot(), result = f.run();assert.notEqual(result.status, 0, 'already interpolated input must refuse before mutation');
  assert.deepEqual(f.snapshot(), before);assert.equal(fs.existsSync(path.join(f.root, 'processor-reads')), false);
});
scenario(false, f => {
  const result = f.run();assert.equal(result.status, 0, result.stderr);
  const out = JSON.parse(fs.readFileSync(path.join(f.dir, 'manifest.json'))), clip = out.clips.owned;
  assert.equal(clip.frames.length, 7, 'three actual selected frames yield7; atlas padding is not input');
  assert.equal(clip.durations.reduce((sum, value) => sum + value, 0), 360, 'total playback duration must remain unchanged');
  assert.ok(clip.durations.every(value => Number.isInteger(value) && value > 0));
  const animation = JSON.parse(fs.readFileSync(path.join(f.dir, 'owned.webp')));
  assert.equal(animation.raw.height, 7 * 256);assert.equal(animation.first, 0);assert.equal(animation.last, 4);
  assert.equal(out.provenance.interpolationInProgress, undefined);
  const art=JSON.parse(fs.readFileSync(path.join(f.dir,'art-audit.json')));
  for(const [file,digest] of Object.entries(art[0].files))assert.equal(digest,crypto.createHash('sha256').update(fs.readFileSync(path.join(f.dir,file))).digest('hex'),'postprocessing audit hash must match '+file);
  assert.equal(art[0].frameCount,7);assert.equal(art[0].frameHashes.length,7);
  assert.equal(art[0].sourceExtraction.frameCount,3,'original extraction evidence must be retained');
  const audit=JSON.parse(fs.readFileSync(path.join(f.dir,'frame-interpolation-audit.json')));
  assert.deepEqual(audit.map(x=>x.id),Object.keys(out.variants));assert.deepEqual(audit[0].files,art[0].files);
  assert.deepEqual(audit[0].sourceFiles,art[0].sourceExtraction.files);
  const validBefore=f.snapshot();assert.equal(f.verify().status,0);assert.deepEqual(f.snapshot(),validBefore,'verification is read-only');
  const atlasFile=path.join(f.dir,'owned-atlas.webp'),originalAtlas=fs.readFileSync(atlasFile);
  fs.appendFileSync(atlasFile,'unrecorded byte');let tampered=f.snapshot();assert.notEqual(f.verify().status,0);assert.deepEqual(f.snapshot(),tampered);fs.writeFileSync(atlasFile,originalAtlas);
  const auditFile=path.join(f.dir,'frame-interpolation-audit.json'),originalAudit=fs.readFileSync(auditFile);
  audit[0].newFrames++;fs.writeFileSync(auditFile,JSON.stringify(audit));tampered=f.snapshot();assert.notEqual(f.verify().status,0);assert.deepEqual(f.snapshot(),tampered);fs.writeFileSync(auditFile,originalAudit);
  const before = f.snapshot();assert.notEqual(f.run().status, 0);assert.deepEqual(f.snapshot(), before);
});
scenario(false, f => {
  f.manifest.clips.owned.frames = [0, 9];f.write();const before = f.snapshot();assert.notEqual(f.run().status, 0);
  assert.deepEqual(f.snapshot(), before, 'invalid frame metadata must reject before any write');
});
scenario(false, f => {
  assert.notEqual(f.run(true).status, 0);const before = f.snapshot();
  assert.ok(JSON.parse(fs.readFileSync(path.join(f.dir, 'manifest.json'))).provenance.interpolationInProgress);
  assert.notEqual(f.run().status, 0);assert.deepEqual(f.snapshot(), before, 'partial mutation requires original restoration before retry');
});
for(const kind of ['changed-input','wrong-audit-id','duplicate-audit-id','stale-interpolation','asset-path'])scenario(false,f=>{
  if(kind==='changed-input')fs.appendFileSync(path.join(f.dir,'owned-atlas.webp'),'unrecorded change');
  const art=JSON.parse(fs.readFileSync(path.join(f.dir,'art-audit.json')));
  if(kind==='wrong-audit-id')art[0].id='missing';
  if(kind==='duplicate-audit-id')art.push({...art[0]});
  fs.writeFileSync(path.join(f.dir,'art-audit.json'),JSON.stringify(art));
  if(kind==='stale-interpolation')fs.writeFileSync(path.join(f.dir,'frame-interpolation-audit.json'),'[]');
  if(kind==='asset-path'){f.manifest.variants.owned.animation='../foreign.webp';f.write();}
  const before=f.snapshot(),result=f.run();assert.notEqual(result.status,0,kind+' must fail before mutation');
  assert.deepEqual(f.snapshot(),before);assert.equal(fs.existsSync(path.join(f.root,'processor-reads')),false);
});
scenario(false,f=>{
  const original=fs.readFileSync(path.join(f.dir,'art-audit.json'));
  const result=f.run(false,true);assert.notEqual(result.status,0);assert.match(result.stderr,/owned audit publication failure/,JSON.stringify({status:result.status,signal:result.signal,error:result.error?.message,stdout:result.stdout}));
  assert.deepEqual(fs.readFileSync(path.join(f.dir,'art-audit.json')),original,'failed atomic audit write retains previous record');
  const out=JSON.parse(fs.readFileSync(path.join(f.dir,'manifest.json')));assert.ok(out.provenance.interpolationInProgress);
  assert.equal(out.provenance.interpolation,undefined);assert.equal(fs.readdirSync(f.dir).some(x=>x.startsWith('.avatar-audit-')),false);
  const before=f.snapshot();assert.notEqual(f.run().status,0);assert.deepEqual(f.snapshot(),before);
});
console.log('PASS interpolation rerun refusal, actual frame indices/no padding, total duration, malformed metadata preflight and failed-run marker, input/output hashes and audit publication failure (mock Sharp)');
