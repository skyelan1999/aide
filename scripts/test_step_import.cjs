#!/usr/bin/env node
'use strict';

const assert = require('node:assert/strict');
const { execFileSync } = require('node:child_process');
const path = require('node:path');

const vendor = path.resolve(__dirname, '../internal/server/web/vendor/occt-import-js');
const archive = path.join(vendor, 'occt-import-js-0.0.23-source.tgz');
const sample = execFileSync('tar', ['-xOf', archive, 'package/test/testfiles/simple-basic-cube/cube.stp'], { maxBuffer: 1024 * 1024 });
const factory = require(path.join(vendor, 'occt-import-js.js'));

(async () => {
  const occt = await factory({ locateFile: (file) => path.join(vendor, file) });
  const result = occt.ReadFile('step', new Uint8Array(sample), {
    linearDeflectionType: 'bounding_box_ratio', linearDeflection: 0.003, angularDeflection: 0.45,
  });
  assert.equal(result.success, true, 'fixture should parse');
  assert.ok(result.meshes?.length, 'fixture should contain meshes');
  const triangles = result.meshes.reduce((sum, mesh) => {
    const positions = mesh.attributes?.position?.array;
    const indices = mesh.index?.array;
    assert.ok(positions?.length >= 9, 'mesh should have vertices');
    assert.ok(indices?.length >= 3 && indices.length % 3 === 0, 'mesh should have triangle indices');
    return sum + indices.length / 3;
  }, 0);
  assert.ok(triangles > 0, 'fixture should produce triangles');
  process.stdout.write(`STEP importer: ${result.meshes.length} mesh(es), ${triangles} triangle(s)\n`);
})().catch((error) => { console.error(error); process.exitCode = 1; });
