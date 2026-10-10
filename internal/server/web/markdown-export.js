/* Local Markdown downloads. Rendering libraries are bundled, loaded only on demand. */
(() => {
 'use strict';
 const LIMIT=32*1024*1024, WIDTH=794, PAGE=987;
 let active=null, menu=null;
 const css=`*{box-sizing:border-box}body{margin:0;width:794px;padding:48px 60px;color:#202630;background:white;font:15px/1.7 -apple-system,BlinkMacSystemFont,"Segoe UI","Microsoft YaHei","PingFang SC",sans-serif}main{overflow-wrap:anywhere}h1,h2,h3,h4{line-height:1.3}h1{font-size:32px}h2{font-size:25px}h3{font-size:20px}img,svg{max-width:100%;height:auto}pre{white-space:pre-wrap;overflow-wrap:anywhere;font:12px/1.5 ui-monospace,Consolas,monospace;padding:14px;background:#f3f5f7;border:1px solid #dde2e8;border-radius:6px}table{border-collapse:collapse;width:100%;font-size:14px}th,td{border:1px solid #cdd4dd;padding:8px;text-align:left;overflow-wrap:anywhere}blockquote{margin:16px 0;padding-left:16px;border-left:3px solid #8295ab;color:#46505e}a{color:#355f86;text-decoration:none}hr{border:0;border-top:1px solid #cdd4dd}button,nav,.md-outline{display:none!important}`;
 function closeMenu(){if(!menu)return;const {host,button,dispose}=menu;menu=null;dispose();host.remove();button.setAttribute('aria-expanded','false');button.focus();}
 function close(){closeMenu();active?.controller.abort();active?.frame?.remove();active=null;}
 window.LockCluster?.on('effective',value=>{if(value)close();});window.addEventListener('pagehide',close);
 function download(blob,name){const url=URL.createObjectURL(blob),a=document.createElement('a');a.href=url;a.download=name;a.hidden=true;document.body.append(a);a.click();a.remove();setTimeout(()=>URL.revokeObjectURL(url),60000);}
 function nameOf(title){return String(title||'document.md').split('/').pop().replace(/[\\:*?"<>|\x00-\x1f]/g,'_')||'document.md';}
 // Match local inline, reference-definition and HTML resources, ignoring code literals.
 function references(markdown){
  const masked=markdown.replace(/^\s{0,3}(`{3,}|~{3,})[^\n]*\n[\s\S]*?^\s{0,3}\1[^\n]*$/gm,m=>' '.repeat(m.length)).replace(/(`+)[^\n]*?\1/g,m=>' '.repeat(m.length));
  const patterns=[/!?\[[^\]\n]*\]\(\s*(<[^>\n]+>|(?:\\.|[^\s()\\]|\([^()]*\))+)/g,/^\s{0,3}\[[^\]\n]+\]:\s*(<[^>\n]+>|[^\s]+)/gm,/(?:src|href)\s*=\s*["']([^"']+)["']/gi];
  const found=[];
  for(const pattern of patterns)for(const match of masked.matchAll(pattern)){const raw=match[1],wrapped=raw.startsWith('<'),value=wrapped?raw.slice(1,-1):raw;found.push({value,start:match.index+match[0].lastIndexOf(raw)+(wrapped?1:0),length:value.length});}
  return found.sort((a,b)=>a.start-b.start).filter((r,i,a)=>!i||r.start!==a[i-1].start);
 }
 async function readBounded(response,signal){
  if(!response.ok)throw Error('资源读取失败（'+response.status+'）');if(Number(response.headers.get('content-length'))>LIMIT)throw Error('单个资源超过 32 MiB');
  const reader=response.body?.getReader();if(!reader){const b=await response.blob();if(b.size>LIMIT)throw Error('单个资源超过 32 MiB');return b;}
  const chunks=[];let size=0;try{while(true){if(signal.aborted)throw new DOMException('Cancelled','AbortError');const {done,value}=await reader.read();if(done)break;size+=value.length;if(size>LIMIT)throw Error('单个资源超过 32 MiB');chunks.push(value);}}catch(e){await reader.cancel();throw e;}finally{reader.releaseLock();}
  return new Blob(chunks,{type:response.headers.get('content-type')||'application/octet-stream'});
 }
 async function exportZIP(o,job){
  await job.wait(o.load('/vendor/jszip.min.js',()=>!!window.JSZip));const zip=new window.JSZip(),refs=references(o.markdown),paths=new Map(),external=new Set();let total=0,source=o.markdown;
  for(const ref of refs){
   if(!ref.value||ref.value.startsWith('#')||/^data:/i.test(ref.value))continue;
   if(/^(?:[a-z][a-z0-9+.-]*:|\/\/)/i.test(ref.value)){external.add(ref.value.replace(/([?&])access_token=[^&#]*/gi,'$1'));continue;}
   const resolved=o.resolve(ref.value.replace(/\\([() ])/g,'$1'));if(!resolved)throw Error('资源路径无效');
   if(!paths.has(resolved)){
    if(paths.size>=64)throw Error('资源超过 64 项，请拆分文档');const blob=await readBounded(await o.fetchResource(resolved,job.controller.signal),job.controller.signal);total+=blob.size;if(total>LIMIT)throw Error('资源总量超过 32 MiB，请拆分文档');
    const dest='resources/'+String(paths.size+1).padStart(3,'0')+'-'+nameOf(resolved);zip.file(dest,blob);paths.set(resolved,dest);
   }
   ref.replacement=paths.get(resolved)+((ref.value.match(/#[^]*$/)||[''])[0]);
  }
  for(const ref of [...refs].reverse())if(ref.replacement)source=source.slice(0,ref.start)+encodeURI(ref.replacement).replace(/[()]/g,c=>'%'+c.charCodeAt(0).toString(16))+source.slice(ref.start+ref.length);
  zip.file(nameOf(o.title),source);zip.file('export-manifest.json',JSON.stringify({format:1,resources:[...paths.values()],externalResources:[...external],note:'当前 Markdown 草稿与直接引用资源；外部链接保留，未下载；不递归打包链接文档内的资源。'},null,2));
  return job.wait(zip.generateAsync({type:'blob',compression:'DEFLATE',compressionOptions:{level:3}}));
 }
 async function snapshot(o,job){
  if(job.controller.signal.aborted)throw new DOMException('Cancelled','AbortError');
  const frame=document.createElement('iframe');frame.className='markdown-print-frame';frame.setAttribute('aria-hidden','true');frame.tabIndex=-1;document.body.append(frame);job.frame=frame;
  const doc=frame.contentDocument;doc.open();doc.write('<!doctype html><html><head><meta charset="utf-8"></head><body></body></html>');doc.close();
  const style=doc.createElement('style');style.textContent=css;doc.head.append(style);const body=doc.createElement('main');body.innerHTML=o.html;
  body.querySelectorAll('script,iframe,object,embed,form,input,button,textarea,select').forEach(e=>e.remove());
  for(const e of body.querySelectorAll('*'))for(const a of [...e.attributes])if(/^on/i.test(a.name)||['srcdoc','style','srcset'].includes(a.name))e.removeAttribute(a.name);
  doc.body.append(body);if(o.renderDiagrams)await job.wait(o.renderDiagrams(body));
  let total=0;
  for(const img of body.querySelectorAll('img')){
   const url=img.getAttribute('src');if(!url)throw Error('图片地址为空');
   const blob=await readBounded(await fetch(url,{signal:job.controller.signal,credentials:new URL(url,location.href).origin===location.origin?'same-origin':'omit'}),job.controller.signal);total+=blob.size;if(total>LIMIT)throw Error('图片资源总量超过 32 MiB');
   const data=await new Promise((resolve,reject)=>{const reader=new FileReader();reader.onload=()=>resolve(reader.result);reader.onerror=()=>reject(reader.error);reader.readAsDataURL(blob);});
   await new Promise((resolve,reject)=>{const signal=job.controller.signal;const finish=(err)=>{clearTimeout(timer);signal.removeEventListener('abort',cancel);img.onload=img.onerror=null;err?reject(err):resolve();};const cancel=()=>finish(new DOMException('Cancelled','AbortError')),timer=setTimeout(()=>finish(Error('图片渲染超时')),15000);signal.addEventListener('abort',cancel,{once:true});img.onload=()=>finish();img.onerror=()=>finish(Error('图片渲染失败'));if(signal.aborted)cancel();else img.src=data;});
  }
  await job.wait(doc.fonts?.ready);
  for(const img of body.querySelectorAll('img'))if(img.naturalWidth*img.naturalHeight>24000000)throw Error('图片像素过大，请缩小后导出');
  return doc.body;
 }
 async function run(format,o){
  close();if(!o.valid())return;const job={controller:new AbortController()};job.wait=p=>new Promise((resolve,reject)=>{const signal=job.controller.signal,cancel=()=>{signal.removeEventListener('abort',cancel);reject(new DOMException('Cancelled','AbortError'));};signal.addEventListener('abort',cancel,{once:true});if(signal.aborted)cancel();Promise.resolve(p).then(v=>{signal.removeEventListener('abort',cancel);resolve(v);},e=>{signal.removeEventListener('abort',cancel);reject(e);});});active=job;const alive=()=>active===job&&!job.controller.signal.aborted&&o.valid();
  // Abort stale/locked documents; bound network operations independently of page lifetime.
  const guard=setInterval(()=>{if(!alive())job.controller.abort();},250),timeout=setTimeout(()=>{job.timedOut=true;job.controller.abort();},120000);
  const name=nameOf(o.title),stem=name.replace(/\.(md|markdown)$/i,'');
  try{
   let blob,extension;
   if(format==='md'){blob=new Blob([o.markdown],{type:'text/markdown;charset=utf-8'});extension='md';}
   else if(format==='zip'){blob=await exportZIP(o,job);extension='zip';}
   else{
    await job.wait(o.load('/vendor/markdown-export/html2canvas.min.js',()=>!!window.html2canvas));
    if(format==='pdf')await job.wait(o.load('/vendor/markdown-export/jspdf.umd.min.js',()=>!!window.jspdf?.jsPDF));
    const body=await snapshot(o,job),height=Math.ceil(body.scrollHeight);
    if(height>60000)throw Error('文档过长，请拆分后导出');
    const render=async(y,h)=>{if(!alive())throw new DOMException('Cancelled','AbortError');return window.html2canvas(body,{backgroundColor:'#fff',scale:1.5,width:WIDTH,height:h,y,windowWidth:WIDTH,windowHeight:1123,logging:false,imageTimeout:15000});};
    if(format==='png'){
     if(height>10000)throw Error('长图超过 10000 像素，请选择分页 PDF');const canvas=await render(0,height);blob=await new Promise((resolve,reject)=>canvas.toBlob(b=>b?resolve(b):reject(Error('图片生成失败')),'image/png'));canvas.width=canvas.height=0;extension='png';
    }else{
     const pdf=new window.jspdf.jsPDF({unit:'mm',format:'a4',compress:true});pdf.setProperties({title:stem,creator:'aide'});
     let y=0,page=0;
     while(y<height){let end=Math.min(y+PAGE,height);
      // Move a cut before a nearby block, avoiding short headings/rows/images split across pages.
      for(const block of body.querySelectorAll('h1,h2,h3,h4,p,li,tr,pre,img,svg')){const r=block.getBoundingClientRect(),top=r.top+body.ownerDocument.defaultView.scrollY,next=/^H[1-4]$/.test(block.tagName)?block.nextElementSibling?.getBoundingClientRect():null,bottom=next&&next.height<PAGE/3?Math.max(top+r.height,next.bottom+body.ownerDocument.defaultView.scrollY):top+r.height;if(top>y+PAGE*.65&&top<end&&bottom>end&&r.height<PAGE){end=Math.floor(top);}}
      const h=Math.max(1,end-y),canvas=await render(y,h);if(page++)pdf.addPage();pdf.addImage(canvas,'JPEG',12,12,186,h/WIDTH*186,undefined,'FAST');canvas.width=canvas.height=0;y=end;
     }
     blob=pdf.output('blob');extension='pdf';
    }
   }
   if(alive())download(blob,format==='md'?name:stem+'.'+extension);
  }catch(e){if(job.timedOut)throw Error('导出超时，请检查资源连接或拆分文档');throw e;}finally{clearInterval(guard);clearTimeout(timeout);job.frame?.remove();if(active===job)active=null;}
 }
 function bind({button,options,t=s=>s,error=()=>{}}){
  button.setAttribute('aria-haspopup','menu');button.setAttribute('aria-expanded','false');
  button.onclick=()=>{
   if(menu?.button===button){closeMenu();return;}closeMenu();
   const host=document.createElement('div');host.className='markdown-export-menu';host.setAttribute('role','menu');host.setAttribute('aria-label',t('导出'));button.parentNode.append(host);
   const formats=[['pdf','PDF','直接下载 · 分页文档'],['md','Markdown','下载原文 · .md'],['zip','Markdown + 资源','图片与附件 · .zip'],['png','PNG 图片','渲染为一张长图']];
   for(const [format,label,description] of formats){const item=document.createElement('button');item.type='button';item.setAttribute('role','menuitem');const badge=document.createElement('span');badge.className='markdown-export-format';badge.textContent=format.toUpperCase();const text=document.createElement('span'),strong=document.createElement('strong'),small=document.createElement('small');strong.textContent=t(label);small.textContent=t(description);text.append(strong,small);item.append(badge,text);host.append(item);
    item.onclick=async()=>{const o=options();closeMenu();if(!o)return;button.disabled=true;button.setAttribute('aria-busy','true');const label=button.textContent;button.textContent=t('正在导出…');try{await run(format,o);}catch(e){if(e.name!=='AbortError')error(e);}finally{button.disabled=false;button.removeAttribute('aria-busy');button.textContent=label;}};
   }
   const outside=e=>{if(!host.contains(e.target)&&e.target!==button)closeMenu();};const key=e=>{if(e.key==='Escape'){e.preventDefault();closeMenu();}else if(['ArrowDown','ArrowUp'].includes(e.key)){e.preventDefault();const items=[...host.children],i=items.indexOf(document.activeElement);items[(i+(e.key==='ArrowDown'?1:items.length-1)+items.length)%items.length].focus();}else if(e.key==='Tab')closeMenu();};
   const blur=()=>closeMenu();document.addEventListener('pointerdown',outside);host.addEventListener('keydown',key);window.addEventListener('resize',blur);
   button.setAttribute('aria-expanded','true');
   menu={host,button,dispose:()=>{document.removeEventListener('pointerdown',outside);window.removeEventListener('resize',blur);}};
   if(host.showPopover){host.setAttribute('popover','manual');host.showPopover();}
   const r=button.getBoundingClientRect();host.style.left=Math.max(8,Math.min(r.right-280,window.innerWidth-288))+'px';host.style.top=Math.max(8,Math.min(r.bottom+8,window.innerHeight-host.offsetHeight-8))+'px';host.children[0].focus();
  };
 }
 window.AideMarkdownExport={bind,run,close,references};
})();
