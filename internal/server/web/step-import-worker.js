/* One import per disposable worker. Download and OCCT stay off the UI thread. */
const STEP_LIMIT = 384 * 1024 * 1024;
let busy = false, waiting = null;
const report = (phase, values = {}) => self.postMessage({kind:'progress', phase, ...values});
async function downloadStep(url, limit=STEP_LIMIT) {
  const limitError=limit<STEP_LIMIT?'当前环境没有原生 CAD 服务，浏览器兼容预览仅支持 64 MiB。请通过新版 start.command 启动。':'STEP 文件超过 384 MiB 预览上限。';
  const target = new URL(url, self.location.origin);
  if (target.origin !== self.location.origin) throw new Error('STEP 文件必须来自当前工作台。');
  const response = await fetch(target.href);
  if (!response.ok) {
    const text = (await response.text()).slice(0, 1024);
    let message; try { message=JSON.parse(text).error; } catch (_) {}
    throw new Error(message || ('HTTP '+response.status));
  }
  const total = Number(response.headers.get('Content-Length')) || 0;
  if (total > limit) { await response.body?.cancel(); throw new Error(limitError); }
  if (!response.body?.getReader) {
    if(!total)throw new Error('当前浏览器不支持有界 CAD 读取，请使用更新的浏览器。');
    const buffer=await response.arrayBuffer();
    if(buffer.byteLength>limit) throw new Error(limitError);
    return new Uint8Array(buffer);
  }
  const reader=response.body.getReader(), chunks=[];
  let buffer=total>0?new Uint8Array(total):null, loaded=0, last=0;
  try {
    while(true) {
      const {done,value}=await reader.read(); if(done)break;
      loaded+=value.byteLength;
      if(loaded>limit || (total && loaded>total)) {await reader.cancel();throw new Error('STEP 文件超过预览上限或传输长度异常。');}
      if(buffer)buffer.set(value,loaded-value.byteLength);else chunks.push(value);
      if(Date.now()-last>150) {report('download',{loaded,total});last=Date.now();}
    }
  } finally {reader.releaseLock();}
  if(total && loaded!==total) throw new Error('STEP 文件传输未完成，请重新加载。');
  if(!buffer) {buffer=new Uint8Array(loaded);let offset=0;for(const chunk of chunks){buffer.set(chunk,offset);offset+=chunk.byteLength;}}
  report('download',{loaded,total:loaded});
  return buffer;
}
// Native conversion streams only generated mesh batches to the browser. The
// original 200+ MiB STEP never enters the browser or WASM heap.
function validatedParts(parts, count, stride, registry) {
  if(parts===undefined)return undefined; // Older CAD services remain previewable.
  if(!Array.isArray(parts)||!parts.length||parts.length>16000)throw Error('CAD 零件范围无效。');
  let next=0;for(const p of parts){if(typeof p.id!=='string'||!p.id||p.id.length>128||typeof p.name!=='string'||p.name.length>512||p.start!==next||!Number.isSafeInteger(p.count)||p.count<=0||p.count%stride)throw Error('CAD 零件范围无效。');next+=p.count;if(registry){if(registry.has(p.id)&&registry.get(p.id)!==p.name)throw Error('CAD 零件名称不一致。');registry.set(p.id,p.name);if(registry.size>50000)throw Error('模型超过五万个零件实例预算。');}}
  if(next!==count)throw Error('CAD 零件范围不完整。');return parts;
}
async function importNative(rawUrl) {
  const target=new URL(rawUrl,self.location.origin);
  if(target.origin!==self.location.origin)throw new Error('STEP 文件必须来自当前工作台。');
  const token=target.searchParams.get('access_token');target.searchParams.delete('access_token');target.pathname='/api/file/step-preview';
  report('initialize');
  const response=await fetch(target.href,{headers:token?{Authorization:'Bearer '+token}:{}});
  if(response.status===501){await response.body?.cancel();return false;}
  if(!response.ok){let message;try{message=(await response.json()).error;}catch(_){}throw new Error(message||('HTTP '+response.status));}
  if(!response.body?.getReader)throw new Error('当前浏览器不支持 CAD 流式预览。');
  const reader=response.body.getReader(),decoder=new TextDecoder();let pending='',id=0,triangles=0,segments=0,points=0,complete=false;const partNames=new Map();
  const decode=text=>{const raw=atob(text);if(raw.length%4)throw new Error('CAD 网格块格式无效。');const bytes=Uint8Array.from(raw,c=>c.charCodeAt(0));return new Float32Array(bytes.buffer);};
  try {
    while(true){
      const {value,done}=await reader.read();pending+=decoder.decode(value||new Uint8Array(),{stream:!done});
      let newline;
      while((newline=pending.indexOf('\n'))>=0){
        const line=pending.slice(0,newline);pending=pending.slice(newline+1);if(!line)continue;
        if(line.length>4*1024*1024)throw new Error('CAD 输出块超过安全预算。');
        const item=JSON.parse(line);if(item.error)throw new Error(item.error);
        if(item.kind==='native-mesh'){
          const p=decode(item.positions),n=decode(item.normals),c=decode(item.colors);
          if(!p.length||p.length%9||p.length!==n.length||p.length!==c.length||!Array.isArray(item.origin)||item.origin.length!==3||!item.origin.every(Number.isFinite)||!p.every(Number.isFinite)||!n.every(Number.isFinite)||!c.every(Number.isFinite))throw new Error('CAD 网格包含无效几何。');
          triangles+=p.length/9;if(triangles>3000000)throw new Error('模型超过预览几何预算。');
          const indices=Uint32Array.from({length:p.length/3},(_,i)=>i),current=id++;
          const acknowledged=new Promise(resolve=>{waiting={id:current,resolve};});
          self.postMessage({kind:'meshes',id:current,meshes:[{attributes:{position:{array:p},normal:{array:n},color:{array:c}},index:{array:indices},origin:item.origin,sourceCount:item.sourceCount,parts:validatedParts(item.parts,indices.length,3,partNames)}],total:0},[p.buffer,n.buffer,c.buffer,indices.buffer]);
          await acknowledged;
        } else if(item.kind==='native-lines'||item.kind==='native-points'){
          const p=decode(item.positions),c=decode(item.colors),isLine=item.kind==='native-lines',stride=isLine?6:3;
          if(!p.length||p.length%stride||p.length!==c.length||!Array.isArray(item.origin)||item.origin.length!==3||!item.origin.every(Number.isFinite)||!p.every(Number.isFinite)||!c.every(Number.isFinite))throw new Error('CAD 网格包含无效几何。');
          if(isLine)segments+=p.length/6;else points+=p.length/3;
          if(segments>500000||points>200000)throw new Error('模型超过预览几何预算。');
          const current=id++,acknowledged=new Promise(resolve=>{waiting={id:current,resolve};});
          self.postMessage({kind:'meshes',id:current,meshes:[{primitive:isLine?'lines':'points',attributes:{position:{array:p},color:{array:c}},origin:item.origin,sourceCount:item.sourceCount,parts:validatedParts(item.parts,p.length/3,isLine?2:1,partNames)}],total:0},[p.buffer,c.buffer]);
          await acknowledged;
        } else if(item.kind==='complete'){
          if(complete||!(triangles||segments||points)||item.triangles!==triangles||(item.segments||0)!==segments||(item.points||0)!==points||item.unit!=='mm')throw new Error('CAD 曲面转换未完整结束。');
          complete=true;self.postMessage({...item,coarse:false});
        } else if(item.kind==='progress')self.postMessage({...item,engine:'occt-native'});
      }
      if(pending.length>4*1024*1024)throw new Error('CAD 输出块超过安全预算。');
      if(done)break;
    }
    if(pending.trim()||!complete)throw new Error('CAD 曲面转换未完整结束。');
  } finally {await reader.cancel().catch(()=>{});reader.releaseLock();}
  return true;
}
// Large assemblies otherwise create tens of thousands of GPU draw calls.
// Flatten only one acknowledged batch at a time; vertex colours preserve faces.
function mergeBatch(meshes) {
  const count=meshes.reduce((n,m)=>n+m.index.array.length,0),positions=new Float32Array(count*3),normals=new Float32Array(count*3),colors=new Float32Array(count*3),indices=new Uint32Array(count);
  let offset=0,hasNormals=true;const parts=[];
  for(const mesh of meshes) {
    const p=mesh.attributes.position.array,n=mesh.attributes.normal?.array,ix=mesh.index.array,faces=mesh.brep_faces||[];let face=0;
    if(!n)hasNormals=false;
    for(let j=0;j<ix.length;j++) {
      const triangle=Math.floor(j/3);while(face<faces.length && faces[face].last<triangle)face++;
      const color=(faces[face] && faces[face].first<=triangle && faces[face].color)||mesh.color||[.615,.749,.89];
      const target=(offset+j)*3,index=ix[j]*3;
      for(let axis=0;axis<3;axis++){positions[target+axis]=p[index+axis];if(n)normals[target+axis]=n[index+axis];colors[target+axis]=color[axis];}
      indices[offset+j]=offset+j;
    }
    for(const part of mesh.parts||[])parts.push({...part,start:part.start+offset});
    offset+=ix.length;
  }
  const attributes={position:{array:positions},color:{array:colors}};
  if(hasNormals)attributes.normal={array:normals};
  return {attributes,index:{array:indices},sourceCount:meshes.length,parts};
}
self.onmessage = async ({data}) => {
  if(data?.kind==='ack') {if(waiting && data.id===waiting.id){const resolve=waiting.resolve;waiting=null;resolve();}return;}
  if(busy)return; busy=true;
  try {
    if(data.native && await importNative(data.url))return;
    let bytes=data.buffer?new Uint8Array(data.buffer):await downloadStep(data.url,data.native?64*1024*1024:STEP_LIMIT);
    if(bytes.byteLength>STEP_LIMIT)throw new Error('STEP 文件超过 384 MiB 预览上限。');
    const large=bytes.byteLength>64*1024*1024;
    report('initialize');
    if(!self.occtimportjs)importScripts('/vendor/occt-import-js/occt-import-js.js');
    const occt=await self.occtimportjs({locateFile:path=>new URL('/vendor/occt-import-js/'+path,self.location.origin).href});
    report('parse',{coarse:large});
    const result=occt.ReadFile('step',bytes,{linearUnit:'millimeter',linearDeflectionType:'absolute_value',linearDeflection:large?0.5:0.1,angularDeflection:large?0.8:0.45});
    bytes=null;
    if(!result?.success || !Array.isArray(result.meshes) || !result.meshes.length)throw new Error('STEP 文件无法解析，请确认文件完整且格式有效。');
    let triangles=0,vertices=0;
    for(const mesh of result.meshes) {
      triangles+=(mesh.index?.array?.length||0)/3;vertices+=(mesh.attributes?.position?.array?.length||0)/3;
    }
    if(triangles===0)throw new Error('文件中没有可显示的 STEP 实体。');
    if(triangles>3000000 || vertices>9000000 || result.meshes.length>50000)throw new Error('模型超过预览几何预算，请降低模型复杂度后重试。');
    const count=result.meshes.length;
    let batch=[],transfers=[],batchTriangles=0,id=0;
    const flush=async()=>{
      const current=id++;
      const acknowledged=new Promise(resolve=>{waiting={id:current,resolve};});
      if(large){const merged=mergeBatch(batch);batch=[merged];transfers=Object.values(merged.attributes).map(a=>a.array.buffer).concat(merged.index.array.buffer);}
      self.postMessage({kind:'meshes',id:current,meshes:batch,total:count},transfers);
      batch=[];transfers=[];batchTriangles=0;
      await acknowledged;
    };
    for(let i=0;i<count;i++) {
      const mesh=result.meshes[i]; result.meshes[i]=null;
      const position=mesh.attributes?.position?.array,indices=mesh.index?.array;
      if(!position?.length || !indices?.length)continue;
      mesh.parts=[{id:'mesh-'+i,name:String(mesh.name||('Part '+(i+1))).slice(0,160),start:0,count:indices.length}];
      mesh.attributes.position.array=Float32Array.from(position);
      mesh.index.array=Uint32Array.from(indices);
      transfers.push(mesh.attributes.position.array.buffer,mesh.index.array.buffer);
      if(mesh.attributes.normal?.array) {mesh.attributes.normal.array=Float32Array.from(mesh.attributes.normal.array);transfers.push(mesh.attributes.normal.array.buffer);}
      batch.push(mesh);batchTriangles+=indices.length/3;
      if(batch.length>=(large?128:16) || batchTriangles>=100000)await flush();
    }
    if(batch.length)await flush();
    self.postMessage({kind:'complete',triangles,meshes:count,coarse:large});
  } catch(error) {
    const message=error?.message||String(error||'STEP 解析失败。');
    self.postMessage({error:/memory|out of bounds|allocation|abort/i.test(message)?'解析内存不足。请拆分装配或简化几何后重试。':message});
  }
};
