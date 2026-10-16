#!/usr/bin/env node
/* Opt-in native importer stress check. Use an isolated, bounded Docker runner. */
'use strict';
const fs=require('node:fs'),path=require('node:path'),assert=require('node:assert/strict');
(async()=>{
 const file=process.argv[2];if(!file)throw new Error('Usage: node test_step_stress.cjs fixture.step [expectedTriangles]');
 const vendor=process.env.AIDE_OCCT_VENDOR||path.resolve(__dirname,'../internal/server/web/vendor/occt-import-js');
 const data=fs.readFileSync(file),start=Date.now();assert.ok(data.length<=384*1048576,'fixture exceeds preview budget');
 const occt=await require(path.join(vendor,'occt-import-js.js'))({locateFile:n=>path.join(vendor,n)});
 const result=occt.ReadFile('step',new Uint8Array(data.buffer,data.byteOffset,data.byteLength),{linearUnit:'millimeter',linearDeflectionType:'absolute_value',linearDeflection:data.length>64*1048576?.5:.1,angularDeflection:data.length>64*1048576?.8:.45});
 assert.equal(result.success,true);
 let triangles=0;const min=[Infinity,Infinity,Infinity],max=[-Infinity,-Infinity,-Infinity];
 for(const mesh of result.meshes||[]) {
  triangles+=(mesh.index?.array?.length||0)/3;
  for(let i=0;i<(mesh.attributes?.position?.array?.length||0);i++){const value=mesh.attributes.position.array[i],axis=i%3;assert.ok(Number.isFinite(value));min[axis]=Math.min(min[axis],value);max[axis]=Math.max(max[axis],value);}
 }
 assert.ok(triangles>0,'success flag without geometry is a failure');
 if(process.argv[3])assert.equal(triangles,Number(process.argv[3]),'every generated solid must be triangulated');
 console.log(JSON.stringify({status:'pass',bytes:data.length,meshes:result.meshes.length,triangles,bounds:{min,max,size:max.map((v,i)=>v-min[i])},elapsedMs:Date.now()-start,rss:process.memoryUsage().rss,precisionMm:data.length>64*1048576?.5:.1}));
})().catch(error=>{console.error(error.message);process.exitCode=1;});
