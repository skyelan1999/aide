/* Disk-backed, bounded-memory transfers. File contents are never kept in UI state. */
(() => {
 'use strict';
 const CHUNK=4*1024*1024;let shown=100,dbPromise;const deferred=new Set(),views=new Map();let updateSequence=0;let services,dialog,list,timer,uploadFiles=new Map(),active=new Map(),downloads=[],serverJobs=[],lastStates=new Map();
 const tr=s=>services.t(s),node=(tag,cls='',text)=>{const e=document.createElement(tag);e.className=cls;if(text!=null)e.textContent=text;return e;};
 const bytes=n=>{if(n<1024)return n+' B';const units=['KiB','MiB','GiB','TiB','PiB'];let u=-1;do{n/=1024;u++;}while(n>=1024&&u<units.length-1);return n.toFixed(1)+' '+units[u];};
 const hash=async b=>Array.from(new Uint8Array(await crypto.subtle.digest('SHA-256',b)),n=>n.toString(16).padStart(2,'0')).join('');
 const valid=scope=>!services.blocked()&&scope===services.scope();
 function database(){if(!dbPromise)dbPromise=new Promise((resolve,reject)=>{const q=indexedDB.open('aide-file-transfer-handles',2);q.onupgradeneeded=()=>{for(const name of ['files','downloads','chunks'])if(!q.result.objectStoreNames.contains(name))q.result.createObjectStore(name);};q.onsuccess=()=>resolve(q.result);q.onerror=()=>reject(q.error);});return dbPromise;}
 async function idb(name,mode,fn){const db=await database();return new Promise((resolve,reject)=>{const tx=db.transaction(name,mode),q=fn(tx.objectStore(name));let result;if(q)q.onsuccess=()=>result=q.result;tx.oncomplete=()=>resolve(result);tx.onerror=()=>reject(tx.error);tx.onabort=()=>reject(tx.error);});}
 async function save(j){const {chunks,...record}=j;await idb('downloads','readwrite',store=>store.put(record,j.id));}
 async function chunkRecords(j,remove=false){const db=await database();return new Promise((resolve,reject)=>{const tx=db.transaction('chunks',remove?'readwrite':'readonly'),q=tx.objectStore('chunks').openCursor(IDBKeyRange.bound(j.id+':',j.id+':\uffff')),chunks={};q.onsuccess=()=>{const c=q.result;if(c){if(remove)c.delete();else chunks[c.value.index]=c.value.digest;c.continue();}};tx.oncomplete=()=>resolve(chunks);tx.onerror=()=>reject(tx.error);});}
 async function removeDownload(j){await chunkRecords(j,true);await idb('files','readwrite',s=>s.delete(j.id));await idb('downloads','readwrite',s=>s.delete(j.id));downloads=downloads.filter(x=>x.id!==j.id);paint();}
 const request=async(url,options={})=>{const r=await fetch('/api'+url,{...options,headers:{Authorization:'Bearer '+services.token(),...options.headers}});if(!r.ok){let d;try{d=await r.json();}catch(_){}throw Error(d?.error||'HTTP '+r.status);}return r;};
 function button(label,fn){const b=node('button','quiet',tr(label));b.dataset.i18n=label;b.type='button';b.onclick=window.AideAsyncAction.wrap(fn,e=>{if(e.name!=='AbortError')services.notice(e.message);});return b;}
 async function dbHandle(id,value){return idb('files',value===undefined?'readonly':'readwrite',s=>value===undefined?s.get(id):s.put(value,id));}
 async function retry(fn,signal){for(let n=0;;n++){try{return await fn();}catch(e){if(signal.aborted||n===2||/41[29]|已变化/.test(e.message))throw e;await new Promise((resolve,reject)=>{const id=setTimeout(done,300*2**n);function done(){signal.removeEventListener('abort',abort);resolve();}function abort(){clearTimeout(id);reject(new DOMException('Aborted','AbortError'));}signal.addEventListener('abort',abort,{once:true});});}}}
 async function pool(count,next,work,signal){let first;await Promise.allSettled(Array.from({length:count},async()=>{while(!signal.aborted&&!first){const item=next();if(item==null)return;try{await work(item);}catch(e){first=first||e;return;}}}));if(first)throw first;}

 async function update(){if(!services||services.blocked())return;const sequence=++updateSequence,scope=services.scope();try{const pages=await Promise.all(Array.from({length:Math.ceil(shown/100)},(_,i)=>services.api('/file/transfers?offset='+i*100)));if(sequence!==updateSequence||!valid(scope))return;serverJobs=Array.from(new Map(pages.flat().map(j=>[j.id,j])).values());for(const j of serverJobs){if(j.status==='complete'){uploadFiles.delete(j.id);}if(j.status==='complete'&&lastStates.get(j.id)&&lastStates.get(j.id)!=='complete')services.refresh().catch(()=>{});lastStates.set(j.id,j.status);}paint();}catch(e){if(dialog?.open)services.notice(e.message);}}
 function schedule(){clearTimeout(timer);if(dialog?.open||active.size||serverJobs.some(j=>['running','queued','pausing','cancelling'].includes(j.status)))timer=setTimeout(async()=>{await update();schedule();},1000);}
 const states={uploading:'等待上传',queued:'排队中',running:'传输中',paused:'已暂停',pausing:'正在暂停',failed:'传输失败',complete:'已完成',cancelled:'已取消',cancelling:'正在取消',preparing:'正在准备',browser:'浏览器下载'};
 function paint(){
  if(!list)return;const scope=services.scope(),all=[...serverJobs,...downloads.filter(j=>j.workspace===scope)],wanted=new Set(all.map(j=>j.id));
  for(const [id,v] of views){if(!wanted.has(id)){v.card.remove();views.delete(id);}}
  list.querySelector('.file-transfer-empty')?.remove();list.querySelector('.file-transfer-more')?.remove();
  if(!all.length){list.append(node('p','muted file-transfer-empty',tr('暂无传输任务')));return;}
  let cursor=list.firstChild;
  for(const j of all){
   const local=j.local===true;let v=views.get(j.id);
   if(!v){v={card:node('article','file-transfer-task'),title:node('strong'),operation:node('small','muted'),status:node('span','muted'),progress:node('progress'),size:node('small','muted'),error:node('p','task-error'),controls:node('div','file-transfer-task-controls')};v.card.dataset.transferId=j.id;v.card.append(v.title,v.operation,v.status,v.progress,v.size,v.error,v.controls);views.set(j.id,v);}
   if(v.card!==cursor)list.insertBefore(v.card,cursor);cursor=v.card.nextSibling;
   v.title.textContent=local?j.name:(j.uploadName||j.tops?.map(x=>x.source||x.target).join(', ')||j.id);
   v.operation.textContent=tr(j.rawDownload?'下载':({copy:'复制',move:'移动',upload:'上传',download:'打包下载'})[j.operation]||'下载');
   v.status.textContent=tr(active.get(j.id)?.phase==='verifying'?'校验已传分块':j.operation==='upload'&&active.has(j.id)?'上传中':j.status==='running'&&j.completed>=j.size?'校验并发布':states[j.status]||j.status);
   v.progress.max=Math.max(1,j.size||0);v.progress.value=j.status==='complete'?v.progress.max:j.completed||0;v.size.textContent=bytes(j.completed||0)+' / '+bytes(j.size||0);v.error.hidden=!j.error;v.error.textContent=j.error||'';
   const key=JSON.stringify([j.status,active.has(j.id),j.uploadCommitted,document.documentElement.lang]);if(key===v.key)continue;v.key=key;v.controls.replaceChildren();const controls=v.controls;
   if(!['complete','cancelled','browser'].includes(j.status)){
    if(active.has(j.id)||['running','queued','pausing','cancelling'].includes(j.status))controls.append(button('暂停',async()=>{deferred.add(j.id);active.get(j.id)?.abort();if(local){j.status='paused';await save(j);paint();}else await services.api('/file/transfers/'+j.id+'/pause',{method:'POST',body:'{}'});await update();}));
    else controls.append(button('继续',async()=>{deferred.delete(j.id);if(local)return resumeDownload(j);if(j.operation==='upload'&&!j.uploadCommitted){const file=uploadFiles.get(j.id);if(file)return resumeUpload(j,file);const input=node('input');input.type='file';input.onchange=()=>{if(input.files[0])resumeUpload(j,input.files[0]).catch(e=>services.notice(e.message));};input.click();return;}await services.api('/file/transfers/'+j.id+'/resume',{method:'POST',body:JSON.stringify({concurrency:concurrency()})});await update();}));
    controls.append(button('取消',async()=>{deferred.add(j.id);active.get(j.id)?.abort();if(local){j.status='cancelled';await save(j);}else await services.api('/file/transfers/'+j.id+'/cancel',{method:'POST',body:'{}'});await update();paint();}));
   }
   if(!local&&j.operation==='download'&&j.status==='complete')controls.append(button('保存下载',()=>savePrepared(j)));
   if(local&&['complete','cancelled','browser'].includes(j.status))controls.append(button('移除记录',()=>removeDownload(j)));
   if(!local&&!['running','queued','pausing','cancelling'].includes(j.status))controls.append(button('移除记录',async()=>{await services.api('/file/transfers/'+j.id,{method:'DELETE'});await update();}));
  }
  if(serverJobs.length>=shown){const more=button('加载更多',async()=>{shown+=100;await update();});more.classList.add('file-transfer-more');list.append(more);}
 }

 function open(){if(!dialog){dialog=node('dialog','file-transfers-center');dialog.id='file-transfers-center';const heading=node('div','dialog-heading');heading.append(node('h2','',tr('文件传输')),button('关闭',()=>dialog.close()));const config=node('label','file-transfer-options',tr('并发数'));const select=node('select');select.id='file-transfer-concurrency';for(const n of [1,2,4,6,8]){const option=node('option','',String(n));option.value=n;select.append(option);}select.value='4';config.append(select);for(const folder of [false,true])config.append(button(folder?'上传文件夹':'上传文件',()=>{const target=services.target(),base=services.base(),scope=services.scope();if(!target.writable)throw Error(tr('当前目录为只读，无法上传'));const input=node('input');input.type='file';input.multiple=true;if(folder)input.webkitdirectory=true;input.onchange=()=>{if(!valid(scope)){services.notice(tr('工作区已切换'));return;}void upload(Array.from(input.files).map(file=>({file,relativePath:file.webkitRelativePath||file.name})),target,base).catch(e=>services.notice(e.message));};input.click();}));list=node('div','file-transfer-tasks');dialog.append(heading,config,list);dialog.addEventListener('close',()=>schedule());document.body.append(dialog);}if(!dialog.open)dialog.showModal();paint();void update();schedule();}
 const concurrency=()=>Number(document.getElementById('file-transfer-concurrency')?.value||4);
 async function create(data){const j=await services.api('/file/transfers',{method:'POST',body:JSON.stringify({...data,workspaceId:data.workspaceId||services.scope(),concurrency:concurrency()})});await update();return j;}
 async function resumeUpload(job,file){
  if(active.has(job.id))return;if(!valid(job.workspace))throw Error(tr('工作区已切换'));if(file.size!==job.size)throw Error(tr('请选择与原任务一致的文件'));
  const controller=new AbortController(),{signal}=controller;controller.phase='verifying';active.set(job.id,controller);uploadFiles.set(job.id,file);paint();schedule();
  try{
   const fresh=await services.api('/file/transfers/'+job.id);if(['cancelled','complete'].includes(fresh.status))return;
   // A reselected file is checked against every acknowledged chunk. Same name,
   // length or mtime alone must never splice different files together.
   for(const [index,digest] of Object.entries(fresh.uploadChunks||{})){if(signal.aborted)throw new DOMException('Aborted','AbortError');const start=Number(index)*CHUNK;if(await hash(await file.slice(start,Math.min(start+CHUNK,file.size)).arrayBuffer())!==digest)throw Error(tr('文件内容已变化，不能继续原上传'));}
   if(signal.aborted)throw new DOMException('Aborted','AbortError');controller.phase='uploading';paint();await services.api('/file/transfers/'+job.id+'/resume',{method:'POST',body:'{}'});let cursor=0;
   await pool(concurrency(),()=>{while(cursor*CHUNK<file.size){const n=cursor++;if(!fresh.uploadChunks?.[n])return n;}return null;},async n=>{
    if(!valid(job.workspace))throw Error(tr('工作区已切换'));const blob=file.slice(n*CHUNK,Math.min((n+1)*CHUNK,file.size));const digest=await hash(await blob.arrayBuffer());
    await retry(async()=>{const response=await request('/file/transfers/'+job.id+'/chunks/'+n,{method:'PUT',signal,headers:{'Content-Type':'application/octet-stream','X-Chunk-SHA256':digest},body:blob});const ack=await response.json();if(ack.sha256!==digest)throw Error(tr('服务器上传确认校验不匹配'));},signal);
   },signal);
   if(signal.aborted)throw new DOMException('Aborted','AbortError');await services.api('/file/transfers/'+job.id+'/commit',{method:'POST',body:'{}'});
  }catch(e){controller.abort();if(e.name!=='AbortError'){services.notice(e.message);await services.api('/file/transfers/'+job.id+'/pause',{method:'POST',body:'{}'}).catch(()=>{});throw e;}}
  finally{if(signal.aborted)await services.api('/file/transfers/'+job.id+'/pause',{method:'POST',body:'{}'}).catch(()=>{});active.delete(job.id);await update();schedule();}
 }
 async function upload(items,target,base){const scope=services.scope();open();const jobs=[],failed=[];for(const item of items){if(!valid(scope))throw Error(tr('工作区已切换'));if(!Number.isSafeInteger(item.file.size))throw Error(tr('浏览器无法准确表示文件大小'));try{const j=await create({workspaceId:scope,operation:'upload',destination:target.source||'workspace',destinationPath:base,name:item.relativePath,size:item.file.size});uploadFiles.set(j.id,item.file);jobs.push([j,item.file]);}catch(e){failed.push({path:item.relativePath,error:e.message});services.notice(e.message);}}let n=0;await Promise.all(Array.from({length:Math.min(2,jobs.length)},async()=>{while(n<jobs.length){const [j,file]=jobs[n++];try{if(!deferred.has(j.id))await resumeUpload(j,file);}catch(e){failed.push({path:j.uploadName,error:e.message});}}}));return{succeeded:Math.max(0,items.length-failed.length),failed};}
 function native(url,name){const link=node('a');link.href='/api'+url+(url.includes('?')?'&':'?')+'access_token='+encodeURIComponent(services.token());link.download=name;document.body.append(link);link.click();link.remove();}
 async function startDownload(url,name,handlePromise){
  if(!handlePromise){native(url,name);services.notice(tr('请在浏览器下载列表管理或继续此下载'));return;}
  const scope=services.scope(),handle=await handlePromise;if(!valid(scope))throw Error(tr('工作区已切换'));const j={id:'download-'+crypto.randomUUID(),local:true,workspace:scope,name,url,size:0,etag:'',chunks:{},completed:0,status:'paused'};await dbHandle(j.id,handle);downloads.push(j);await save(j);open();await resumeDownload(j,handle);
 }
 async function resumeDownload(j,provided){
  if(active.has(j.id))return;if(!valid(j.workspace))throw Error(tr('工作区已切换'));const handle=provided||await dbHandle(j.id);if(!handle)throw Error(tr('下载目标不可用，请重新下载'));
  if(handle.requestPermission&&await handle.requestPermission({mode:'readwrite'})!=='granted')throw Error(tr('未获得保存文件的权限'));
  const controller=new AbortController(),{signal}=controller;active.set(j.id,controller);j.status='preparing';paint();schedule();let writable;
  try{
   const meta=await request(j.url,{method:'HEAD',signal});const size=Number(meta.headers.get('X-Transfer-Size')),etag=meta.headers.get('ETag');if(!Number.isSafeInteger(size)||size<0||!etag)throw Error(tr('文件大小或版本信息无效'));if(j.etag&&j.etag!==etag)throw Error(tr('源文件已变化，请重新下载'));j.size=size;j.etag=etag;j.status='running';j.error='';j.chunks=await chunkRecords(j);j.completed=Object.keys(j.chunks).reduce((n,k)=>n+Math.min(CHUNK,size-Number(k)*CHUNK),0);await save(j);paint();schedule();
   const previous=await handle.getFile();for(const [index,digest] of Object.entries(j.chunks)){if(signal.aborted)throw new DOMException('Aborted','AbortError');const start=Number(index)*CHUNK;const b=await previous.slice(start,Math.min(start+CHUNK,size)).arrayBuffer();if(await hash(b)!==digest){delete j.chunks[index];j.completed-=Math.min(CHUNK,size-start);await idb('chunks','readwrite',s=>s.delete(j.id+':'+index));}}
   writable=await handle.createWritable({keepExistingData:true});let cursor=0;
   await pool(concurrency(),()=>{while(cursor*CHUNK<size){const n=cursor++;if(!j.chunks[n])return n;}return null;},async n=>{
    if(!valid(j.workspace))throw Error(tr('工作区已切换'));const start=n*CHUNK,end=Math.min(start+CHUNK,size)-1;
    await retry(async()=>{const r=await request(j.url,{signal,headers:{Range:'bytes='+start+'-'+end,'If-Match':etag}});if(r.status!==206||r.headers.get('ETag')!==etag||r.headers.get('Content-Range')!=='bytes '+start+'-'+end+'/'+size)throw Error(tr('服务器返回的下载分块版本或范围不匹配'));const b=await r.arrayBuffer();if(b.byteLength!==end-start+1)throw Error(tr('下载分块长度不匹配'));if(!valid(j.workspace))throw Error(tr('工作区已切换'));await writable.write({type:'write',position:start,data:b});const digest=await hash(b);await idb('chunks','readwrite',s=>s.put({index:n,digest},j.id+':'+n));if(!j.chunks[n])j.completed+=b.byteLength;j.chunks[n]=digest;await save(j);paint();},signal);
   },signal);
   if(signal.aborted)throw new DOMException('Aborted','AbortError');await writable.truncate(size);await writable.close();writable=null;j.status='complete';
  }catch(e){controller.abort();if(j.status!=='cancelled')j.status=e.name==='AbortError'?'paused':'failed';j.error=e.name==='AbortError'?'':e.message;}
  finally{if(writable)await writable.close().catch(()=>{});active.delete(j.id);await save(j);paint();schedule();}
 }
 function picker(name){return window.showSaveFilePicker?window.showSaveFilePicker({suggestedName:name}):null;}
 async function savePrepared(j){const name=j.tops.length===1?j.tops[0].source.split('/').pop()+(j.rawDownload?'':'.zip'):'download.zip';return startDownload('/file/stream?job='+encodeURIComponent(j.id),name,picker(name));}
 function download(file,archive,target){
  const name=file.name+(archive?'.zip':'');const handle=picker(name);const scope=services.scope();
  return(async()=>{if(handle)await handle;const j=await create({operation:'download',source:target.source||'workspace',paths:[file.path],rawDownload:!archive});open();while(valid(scope)){const current=await services.api('/file/transfers/'+j.id);if(current.status==='complete')return startDownload('/file/stream?job='+encodeURIComponent(j.id),name,handle);if(['failed','cancelled','paused'].includes(current.status))return;await new Promise(r=>setTimeout(r,500));}})().catch(e=>{if(e.name!=='AbortError')services.notice(e.message);});
 }
 window.AideTransfers={mount(s){services=s;void idb('downloads','readonly',s=>s.getAll()).then(records=>{downloads=Array.from(new Map([...(records||[]),...downloads].map(j=>[j.id,j])).values());for(const j of downloads){if(j.status==='running')j.status='paused';}paint();}).catch(e=>services.notice(e.message));const b=button('文件传输',open);b.id='file-transfers-button';delete b.dataset.i18n;b.textContent='⇅';b.title=tr('文件传输');b.dataset.i18nTitle='文件传输';b.dataset.i18nAriaLabel='文件传输';b.setAttribute('aria-label',tr('文件传输'));b.classList.add('icon-button');document.getElementById('refresh-files').before(b);},open,create,upload,download,hash};
})();
