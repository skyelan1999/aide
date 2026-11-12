'use strict';
// Cross-check generated JS against the separately compiled native CLI raster.
// This is a frontend consistency check, not an independent conformance oracle.
const fs=require('node:fs'),path=require('node:path'),assert=require('node:assert/strict'),crypto=require('node:crypto');
(async()=>{
 const root=process.argv[2];assert(root,'pass the verified official fixture directory from test-native.py');
 const rows=JSON.parse(fs.readFileSync('scripts/openjpeg/official-vectors.json'));
 const expected=['daabbcf4c3cb3b2036821b41244439a04a848c86b872b10400ece97039ba98ed','41cae35ed1905f8f07ea3b0e76b608f40f053e68a360defd2d9a98bd95916b05'];
 const sha=b=>crypto.createHash('sha256').update(b).digest('hex');
 const source=fs.readFileSync('internal/server/web/vendor/pdfjs/pdf.worker.min.mjs','utf8');
 const {oi}=await import('data:text/javascript;base64,'+Buffer.from(source+';export {oi};').toString('base64'));
 const decoder=oi({warn:()=>{}}),results=[];
 for(let i=0;i<rows.length;i++){const row=rows[i],input=fs.readFileSync(path.join(root,row.name));assert.equal(sha(input),row.sha256);const output=decoder.decode(input,{numComponents:3});assert(output instanceof Uint8ClampedArray);assert.equal(output.length,1179648);assert.equal(sha(output),expected[i]);results.push({name:row.name,bytes:output.length,sha256:sha(output)});}
 console.log(JSON.stringify({status:'PASS',results}));
})().catch(e=>{console.error(e);process.exitCode=1});
