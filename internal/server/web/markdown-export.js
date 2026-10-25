/* Local Markdown downloads. Rendering libraries are bundled, loaded only on demand. */
(() => {
 'use strict';
 const LIMIT=32*1024*1024, WIDTH=794, PAGE=987, PDF_SCALE=1.25, PDF_JPEG_QUALITY=0.82;
 let active=null, menu=null;
 const css=`*{box-sizing:border-box}body{margin:0;width:794px;padding:48px 60px;color:#202630;background:white;font:15px/1.7 -apple-system,BlinkMacSystemFont,"Segoe UI","Microsoft YaHei","PingFang SC",sans-serif}main{overflow-wrap:anywhere}h1,h2,h3,h4{line-height:1.3}h1{font-size:32px}h2{font-size:25px}h3{font-size:20px}img,svg{max-width:100%;height:auto}pre{white-space:pre-wrap;overflow-wrap:anywhere;font:12px/1.5 ui-monospace,Consolas,monospace;padding:14px;background:#f3f5f7;border:1px solid #dde2e8;border-radius:6px}table{border-collapse:collapse;width:100%;font-size:14px}th,td{border:1px solid #cdd4dd;padding:8px;text-align:left;overflow-wrap:anywhere}blockquote{margin:16px 0;padding-left:16px;border-left:3px solid #8295ab;color:#46505e}a{color:#355f86;text-decoration:none}hr{border:0;border-top:1px solid #cdd4dd}button,nav,.md-outline{display:none!important}`;
 function closeMenu(){if(!menu)return;const {host,button,dispose}=menu;menu=null;dispose();host.remove();button.setAttribute('aria-expanded','false');button.focus();}
 function close(){closeMenu();active?.controller.abort();active?.frame?.remove();active?.renderFrame?.remove();active=null;}
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
  if(!response.ok)throw Error('资源读取失败（'+response.status+'）');if(Number(response.headers.get('content-length'))>LIMIT)throw Error(uiTextMarkdownExport('单个资源超过 32 MiB'));
  const reader=response.body?.getReader();if(!reader){const b=await response.blob();if(b.size>LIMIT)throw Error(uiTextMarkdownExport('单个资源超过 32 MiB'));return b;}
  const chunks=[];let size=0;try{while(true){if(signal.aborted)throw new DOMException('Cancelled','AbortError');const {done,value}=await reader.read();if(done)break;size+=value.length;if(size>LIMIT)throw Error(uiTextMarkdownExport('单个资源超过 32 MiB'));chunks.push(value);}}catch(e){await reader.cancel();throw e;}finally{reader.releaseLock();}
  return new Blob(chunks,{type:response.headers.get('content-type')||'application/octet-stream'});
 }
 async function exportZIP(o,job){
  await job.wait(o.load('/vendor/jszip.min.js',()=>!!window.JSZip));const zip=new window.JSZip(),refs=references(o.markdown),paths=new Map(),external=new Set();let total=0,source=o.markdown;
  for(const ref of refs){
   if(!ref.value||ref.value.startsWith('#')||/^data:/i.test(ref.value))continue;
   if(/^(?:[a-z][a-z0-9+.-]*:|\/\/)/i.test(ref.value)){external.add(ref.value.replace(/([?&])access_token=[^&#]*/gi,'$1'));continue;}
   const resolved=o.resolve(ref.value.replace(/\\([() ])/g,'$1'));if(!resolved)throw Error(uiTextMarkdownExport('资源路径无效'));
   if(!paths.has(resolved)){
    if(paths.size>=64)throw Error(uiTextMarkdownExport('资源超过 64 项，请拆分文档'));const blob=await readBounded(await o.fetchResource(resolved,job.controller.signal),job.controller.signal);total+=blob.size;if(total>LIMIT)throw Error(uiTextMarkdownExport('资源总量超过 32 MiB，请拆分文档'));
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
   const url=img.getAttribute('src');if(!url)throw Error(uiTextMarkdownExport('图片地址为空'));
   const blob=await readBounded(await fetch(url,{signal:job.controller.signal,credentials:new URL(url,location.href).origin===location.origin?'same-origin':'omit'}),job.controller.signal);total+=blob.size;if(total>LIMIT)throw Error(uiTextMarkdownExport('图片资源总量超过 32 MiB'));
   const data=await new Promise((resolve,reject)=>{const reader=new FileReader();reader.onload=()=>resolve(reader.result);reader.onerror=()=>reject(reader.error);reader.readAsDataURL(blob);});
   await new Promise((resolve,reject)=>{const signal=job.controller.signal;const finish=(err)=>{clearTimeout(timer);signal.removeEventListener('abort',cancel);img.onload=img.onerror=null;err?reject(err):resolve();};const cancel=()=>finish(new DOMException('Cancelled','AbortError')),timer=setTimeout(()=>finish(Error(uiTextMarkdownExport('图片渲染超时'))),15000);signal.addEventListener('abort',cancel,{once:true});img.onload=()=>finish();img.onerror=()=>finish(Error(uiTextMarkdownExport('图片渲染失败')));if(signal.aborted)cancel();else img.src=data;});
  }
  await job.wait(doc.fonts?.ready);
  for(const img of body.querySelectorAll('img'))if(img.naturalWidth*img.naturalHeight>24000000)throw Error(uiTextMarkdownExport('图片像素过大，请缩小后导出'));
  return doc.body;
 }
 async function run(format,o,progress=()=>{}){
  close();if(!o.valid())return;const job={controller:new AbortController()};job.wait=p=>new Promise((resolve,reject)=>{const signal=job.controller.signal,cancel=()=>{signal.removeEventListener('abort',cancel);reject(new DOMException('Cancelled','AbortError'));};signal.addEventListener('abort',cancel,{once:true});if(signal.aborted)cancel();Promise.resolve(p).then(v=>{signal.removeEventListener('abort',cancel);resolve(v);},e=>{signal.removeEventListener('abort',cancel);reject(e);});});active=job;const alive=()=>active===job&&!job.controller.signal.aborted&&o.valid();
  // Abort stale/locked documents; bound network operations independently of page lifetime.
  const guard=setInterval(()=>{if(!alive())job.controller.abort();},250);let timeout=setTimeout(()=>{job.timedOut=true;job.controller.abort();},120000);
  const name=nameOf(o.title),stem=name.replace(/\.(md|markdown)$/i,'');
  try{
   let blob,extension;
   if(format==='md'){blob=new Blob([o.markdown],{type:'text/markdown;charset=utf-8'});extension='md';}
   else if(format==='zip'){blob=await exportZIP(o,job);extension='zip';}
   else{
    if(format==='pdf'){
     const estimatedPages=Math.max(1,Math.ceil(String(o.markdown||'').length/2500));
     clearTimeout(timeout);timeout=setTimeout(()=>{job.timedOut=true;job.controller.abort();},Math.min(15*60*1000,120000+estimatedPages*2000));
    }
    await job.wait(o.load('/vendor/markdown-export/html2canvas.min.js',()=>!!window.html2canvas));
    if(format==='pdf')await job.wait(o.load('/vendor/markdown-export/jspdf.umd.min.js',()=>!!window.jspdf?.jsPDF));
    const body=await snapshot(o,job),height=Math.ceil(body.scrollHeight);
    // Long PDFs are rendered a page at a time. Give them a proportional
    // deadline instead of rejecting at an arbitrary document-height limit.
    if(format!=='pdf'&&height>60000)throw Error(uiTextMarkdownExport('文档过长，请拆分后导出'));
    // Rasterize only the blocks visible on this page. Passing the full document
    // to html2canvas for every page makes its cloned DOM grow with the entire
    // document and can exhaust Safari's memory on long Markdown files.
    const sourceRoot=body.children[0]||body,sourceBox=sourceRoot.getBoundingClientRect();
    const blocks=[...sourceRoot.children].map(node=>{const r=node.getBoundingClientRect();return{node,top:r.top-sourceBox.top,bottom:r.bottom-sourceBox.top,left:r.left-sourceBox.left,width:r.width,height:r.height};});
    const measured=new WeakMap(),childCursor=new WeakMap();
    for(const block of blocks)if(block.height>PAGE){const pending=[block.node];while(pending.length){const node=pending.pop();if(node.nodeType!==1)continue;measured.set(node,node.getBoundingClientRect());for(const child of node.children)pending.push(child);}}
    const pageRanges=[],breakSpans=[];
    if(format==='pdf'){
     // Avoid cutting a line/short table row/list item/image in half. Measure
     // once, then advance through the intervals with the page cursor.
     const addBreak=r=>{if(r.height>0&&r.height<=PAGE)breakSpans.push({top:r.top-sourceBox.top,bottom:r.bottom-sourceBox.top});};
     for(const node of sourceRoot.querySelectorAll('tr,li,img,svg'))addBreak(node.getBoundingClientRect());
     const walker=sourceRoot.ownerDocument.createTreeWalker(sourceRoot,4);
     while(walker.nextNode()){const node=walker.currentNode;if(!node.nodeValue.trim()||node.parentElement?.closest('svg'))continue;const range=node.ownerDocument.createRange();range.selectNodeContents(node);for(const r of range.getClientRects())addBreak(r);}
     breakSpans.sort((a,b)=>a.top-b.top||a.bottom-b.bottom);
     let out=0;for(const span of breakSpans){const last=breakSpans[out-1];if(last&&span.top<last.bottom-.25)last.bottom=Math.max(last.bottom,span.bottom);else breakSpans[out++]=span;}breakSpans.length=out;
    }
    if(format==='pdf'){
     const contentHeight=blocks.length?Math.max(1,blocks[blocks.length-1].bottom):Math.max(1,height);
     let y=0,scanBlock=0,scanBreak=0;
     while(y<contentHeight){while(scanBlock<blocks.length&&blocks[scanBlock].bottom<=y)scanBlock++;let end=Math.min(y+PAGE,contentHeight);
      // Match the render pass: keep nearby short blocks together when possible.
      for(let i=scanBlock;i<blocks.length&&blocks[i].top<end;i++){const b=blocks[i];if(b.top>y+PAGE*.65&&b.top<end&&b.bottom>end&&b.height<PAGE)end=Math.floor(b.top);}
      while(scanBreak<breakSpans.length&&breakSpans[scanBreak].bottom<=y)scanBreak++;
      for(let i=scanBreak;i<breakSpans.length&&breakSpans[i].top<end;i++){const span=breakSpans[i];if(span.bottom>end&&span.top>y+PAGE*.65)end=span.top;}
      end=Math.max(y+1,end);pageRanges.push({y,height:end===contentHeight?Math.max(end-y,PAGE):end-y});y=end;
     }
     clearTimeout(timeout);timeout=setTimeout(()=>{job.timedOut=true;job.controller.abort();},Math.min(15*60*1000,120000+pageRanges.length*2000));
    }
    let firstBlock=0;
    const renderFrame=document.createElement('iframe');renderFrame.className='markdown-print-frame';renderFrame.setAttribute('aria-hidden','true');renderFrame.tabIndex=-1;document.body.append(renderFrame);job.renderFrame=renderFrame;
    const renderDoc=renderFrame.contentDocument;renderDoc.open();renderDoc.write('<!doctype html><html><head><meta charset="utf-8"></head><body></body></html>');renderDoc.close();const renderStyle=renderDoc.createElement('style');renderStyle.textContent=css+'html,body{margin:0!important;padding:0!important;width:'+WIDTH+'px!important;min-height:0!important}';renderDoc.head.append(renderStyle);
    const pageSlice=(y,h)=>{
     while(firstBlock<blocks.length&&blocks[firstBlock].bottom<=y)firstBlock++;
     const page=renderDoc.createElement('main');page.className='markdown-export-page';page.style.cssText=`position:relative;box-sizing:border-box;width:${WIDTH}px;height:${h}px;overflow:hidden;background:#fff;margin:0;padding:0`;
     // Every retained box uses source coordinates. Mixing normal-flow clones
     // with absolute oversized wrappers moves later headings back over the list.
     const place=(clone,r,parent)=>{
      clone.style.position='absolute';clone.style.boxSizing='border-box';
      clone.style.left=(r.left-parent.left)+'px';clone.style.top=(r.top-parent.top)+'px';
      clone.style.width=r.width+'px';clone.style.height=r.height+'px';clone.style.margin='0';
      return clone;
     };
     const sliceText=(node,parent)=>{
      const text=node.nodeValue||'';if(!text)return null;
      const range=node.ownerDocument.createRange(),style=node.ownerDocument.defaultView.getComputedStyle(node.parentElement);
      const rectAt=index=>{range.setStart(node,index);range.setEnd(node,index+1);return range.getBoundingClientRect();};
      const boundary=(target,after)=>{let lo=0,hi=text.length;while(lo<hi){const mid=(lo+hi)>>1,r=rectAt(mid);if(after?r.top<target:r.bottom<=target)lo=mid+1;else hi=mid;}return lo;};
      let from=boundary(sourceBox.top+y,false);const to=boundary(sourceBox.top+y+h,true),fragment=renderDoc.createDocumentFragment();
      while(from<to){
       const first=rectAt(from);let lo=from+1,hi=to;
       while(lo<hi){const mid=(lo+hi)>>1;if(Math.abs(rectAt(mid).top-first.top)<.5)lo=mid+1;else hi=mid;}
       const end=lo;range.setStart(node,from);range.setEnd(node,end);const r=range.getBoundingClientRect();
       if(r.width&&r.height){
        const span=renderDoc.createElement('span');span.textContent=text.slice(from,end);
        if(!/^(pre|pre-wrap|break-spaces)$/.test(style.whiteSpace))span.textContent=span.textContent.replace(/\s+/g,' ');
        place(span,r,parent);span.style.width='max-content';span.style.whiteSpace='pre';span.style.font=style.font;
        span.style.lineHeight=r.height+'px';span.style.color=style.color;span.style.letterSpacing=style.letterSpacing;span.style.textDecoration=style.textDecoration;span.style.wordSpacing=style.wordSpacing;span.style.direction=style.direction;
        fragment.append(span);
       }
       from=end;
      }
      return fragment;
     };
     const sliceNode=(node,parent)=>{
      if(node.nodeType===3)return sliceText(node,parent);
      if(node.nodeType!==1)return null;
      const r=measured.get(node)||node.getBoundingClientRect(),top=r.top-sourceBox.top,bottom=r.bottom-sourceBox.top;if(bottom<=y||top>=y+h)return null;
      const style=node.ownerDocument.defaultView.getComputedStyle(node),inline=style.display==='inline';
      const children=[...node.childNodes||[]];
      // Preserve small formatting subtrees even across the page boundary. The
      // page viewport clips them; rebuilding inline children would rewrap them.
      if((!inline&&r.height<=PAGE)||!children.length||node.tagName==='svg'||node.tagName==='SVG'){
       const clone=place(node.cloneNode(true),r,parent);
       if(['TABLE','THEAD','TBODY','TFOOT','TR'].includes(node.tagName)){
        // A pruned table must not recalculate column widths from this page's rows.
        const sourceCells=node.querySelectorAll('th,td');clone.querySelectorAll('th,td').forEach((cell,i)=>{cell.style.width=sourceCells[i].getBoundingClientRect().width+'px';cell.style.minWidth=cell.style.width;});
       }
       return clone;
      }
      const clone=inline?renderDoc.createElement('span'):node.cloneNode(false);place(clone,r,parent);
      if(inline){clone.style.font=style.font;clone.style.color=style.color;clone.style.lineHeight=style.lineHeight;clone.style.textDecoration=style.textDecoration;}
      // Absolute offsets refer to the padding box, not the source border box.
      const origin={left:r.left+(parseFloat(style.borderLeftWidth)||0),top:r.top+(parseFloat(style.borderTopWidth)||0)};
      let state=childCursor.get(node)||{index:0,ordinal:0},childIndex=state.index,ordinal=state.ordinal,resume=null;
      while(childIndex<children.length){const child=children[childIndex];let childRect;
       if(child.nodeType===1)childRect=measured.get(child)||child.getBoundingClientRect();
       else if(child.nodeType===3&&child.nodeValue){const range=node.ownerDocument.createRange();range.selectNodeContents(child);childRect=range.getBoundingClientRect();}
       if(!childRect||!childRect.height){childIndex++;continue;}
       const childTop=childRect.top-sourceBox.top,childBottom=childRect.bottom-sourceBox.top;if(childTop>=y+h)break;
       if(childBottom>y){const copy=sliceNode(child,origin);if(copy){
        if(node.tagName==='OL'&&child.nodeType===1&&child.tagName==='LI')copy.setAttribute('value',child.getAttribute('value')||Number(node.getAttribute('start')||1)+ordinal);
        clone.append(copy);
       }}
       // Parallel table cells may all cross the page bottom. Keep scanning
       // visible siblings, but resume at the first unfinished child next page.
       if(childBottom>y+h&&!resume)resume={index:childIndex,ordinal};
       if(node.tagName==='OL'&&child.nodeType===1&&child.tagName==='LI')ordinal++;childIndex++;
      }
      childCursor.set(node,resume||{index:childIndex,ordinal});return clone;
     };
     for(let i=firstBlock;i<blocks.length&&blocks[i].top<y+h;i++){
      const b=blocks[i];if(b.bottom<=y)continue;const clone=sliceNode(b.node,{left:sourceBox.left,top:sourceBox.top+y});if(clone)page.append(clone);
     }
     renderDoc.body.replaceChildren(page);return page;
    };
    const render=async(page,h)=>{if(!alive())throw new DOMException('Cancelled','AbortError');return window.html2canvas(page,{backgroundColor:'#fff',scale:PDF_SCALE,width:WIDTH,height:h,windowWidth:WIDTH,windowHeight:1123,logging:false,imageTimeout:15000});};
    if(format==='png'){
     if(height>10000)throw Error(uiTextMarkdownExport('长图超过 10000 像素，请选择分页 PDF'));if(!alive())throw new DOMException('Cancelled','AbortError');const canvas=await window.html2canvas(body,{backgroundColor:'#fff',scale:1.5,width:WIDTH,height,y:0,windowWidth:WIDTH,windowHeight:1123,logging:false,imageTimeout:15000});blob=await new Promise((resolve,reject)=>canvas.toBlob(b=>b?resolve(b):reject(Error(uiTextMarkdownExport('图片生成失败'))),'image/png'));canvas.width=canvas.height=0;extension='png';
    }else{
     const pdf=new window.jspdf.jsPDF({unit:'mm',format:'a4',compress:true});pdf.setProperties({title:stem,creator:'aide'});
     let pageNo=0;
     for(const range of pageRanges){const {y,height:h}=range,slice=pageSlice(y,h);
      progress(pageNo+1,pageRanges.length);
      // Let the progress state paint before starting another expensive canvas render.
      await new Promise(resolve=>requestAnimationFrame(()=>resolve()));
      // html2canvas has no AbortSignal support. Race its promise against the
      // export job so timeout, lock, or editor close can settle the UI even
      // when a large page render itself stalls.
      const canvas=await job.wait(render(slice,h));slice.remove();if(pageNo++)pdf.addPage();pdf.addImage(canvas.toDataURL('image/jpeg',PDF_JPEG_QUALITY),'JPEG',12,12,186,h/WIDTH*186,undefined,'FAST');canvas.width=canvas.height=0;
     }
     blob=pdf.output('blob');extension='pdf';
    }
   }
   if(alive())download(blob,format==='md'?name:stem+'.'+extension);
  }catch(e){if(job.timedOut)throw Error(uiTextMarkdownExport('导出超时，请检查资源连接或拆分文档'));throw e;}finally{clearInterval(guard);clearTimeout(timeout);job.frame?.remove();job.renderFrame?.remove();if(active===job)active=null;}
 }
 function bind({button,options,t=s=>s,error=()=>{}}){
  button.setAttribute('aria-haspopup','menu');button.setAttribute('aria-expanded','false');
  button.onclick=()=>{
   if(menu?.button===button){closeMenu();return;}closeMenu();
   const host=document.createElement('div');host.className='markdown-export-menu';host.setAttribute('role','menu');host.setAttribute('aria-label',t('导出'));button.parentNode.append(host);
   const formats=[['pdf','PDF','直接下载 · 分页文档'],['md','Markdown','下载原文 · .md'],['zip','Markdown + 资源','图片与附件 · .zip'],['png','PNG 图片','渲染为一张长图']];
   for(const [format,label,description] of formats){const item=document.createElement('button');item.type='button';item.setAttribute('role','menuitem');const badge=document.createElement('span');badge.className='markdown-export-format';badge.textContent=format.toUpperCase();const text=document.createElement('span'),strong=document.createElement('strong'),small=document.createElement('small');strong.textContent=t(label);small.textContent=t(description);text.append(strong,small);item.append(badge,text);host.append(item);
    item.onclick=async()=>{const o=options();closeMenu();if(!o)return;button.disabled=true;button.setAttribute('aria-busy','true');const progress=document.createElement('span');progress.className='markdown-export-status';progress.setAttribute('role','status');progress.setAttribute('aria-live','polite');progress.textContent=t('准备导出…');button.insertAdjacentElement('afterend',progress);try{await run(format,o,(current,total)=>{progress.textContent=t('正在渲染第 {current}/{total} 页').replace('{current}',String(current)).replace('{total}',String(total));button.setAttribute('aria-valuetext',progress.textContent);});}catch(e){if(e.name!=='AbortError')error(e);}finally{button.disabled=false;button.removeAttribute('aria-busy');button.removeAttribute('aria-valuetext');progress.remove();}};
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

function uiTextMarkdownExport(key, ...values) { return globalThis.window?.aideI18n?.t(key, ...values) ?? String(key).replace(/\{(\d+)\}/g, (match, index) => index < values.length ? String(values[index]) : match); }
