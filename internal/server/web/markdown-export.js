/* Local Markdown downloads. Rendering libraries are bundled, loaded only on demand. */
(() => {
 'use strict';
 const LIMIT=32*1024*1024, PDF_ATTACHMENT_LIMIT=64*1024*1024, PDF_ATTACHMENT_TOTAL=256*1024*1024, PDF_ATTACHMENT_COUNT=256;
 const PDF_CHUNK_CHARS=192*1024, WIDTH=794, PAGE=987;
 let active=null, menu=null;
 function closeMenu(){if(!menu)return;const {host,button,dispose}=menu;menu=null;dispose();host.remove();button.setAttribute('aria-expanded','false');button.focus();}
 function close(){closeMenu();active?.controller.abort();active?.frame?.remove();active?.renderFrame?.remove();active=null;}
 window.LockCluster?.on('effective',value=>{if(value)close();});window.addEventListener('pagehide',close);
 function download(blob,name){const url=URL.createObjectURL(blob),a=document.createElement('a');a.href=url;a.download=name;a.hidden=true;document.body.append(a);a.click();a.remove();setTimeout(()=>URL.revokeObjectURL(url),60000);}
 function nameOf(title){return String(title||'document.md').split('/').pop().replace(/[\\:*?"<>|\x00-\x1f]/g,'_')||'document.md';}
 // Match local inline, reference-definition and HTML resources, ignoring code literals.
 function forEachReference(markdown,visit){
  const patterns=[/!?\[[^\]\n]*\]\(\s*(<[^>\n]+>|(?:\\.|[^\s()\\]|\([^()]*\))+)/g,/^\s{0,3}\[[^\]\n]+\]:\s*(<[^>\n]+>|[^\s]+)/gm,/(?:src|href)\s*=\s*["']([^"']+)["']/gi];
  let offset=0,fence=null;
  while(offset<markdown.length){let end=markdown.indexOf('\n',offset);if(end<0)end=markdown.length;const line=markdown.slice(offset,end),marker=line.match(/^\s{0,3}(`{3,}|~{3,})(.*)$/);
   if(fence){if(marker&&marker[1][0]===fence.char&&marker[1].length>=fence.length)fence=null;offset=end+1;continue;}
   if(marker){fence={char:marker[1][0],length:marker[1].length};offset=end+1;continue;}
   const masked=line.replace(/(`+)[^\n]*?\1/g,m=>' '.repeat(m.length));
   for(const [index,pattern] of patterns.entries())for(const match of masked.matchAll(pattern)){const raw=match[1],wrapped=raw.startsWith('<'),value=wrapped?raw.slice(1,-1):raw;visit({value,start:offset+match.index+match[0].lastIndexOf(raw)+(wrapped?1:0),length:value.length,type:index===0?(match[0].startsWith('!')?'image':'link'):index===1?'reference':/^\s*src\s*=/i.test(match[0])?'image':'link'});}
   offset=end+1;
  }
 }
 function references(markdown){const found=[];forEachReference(markdown,ref=>found.push(ref));return found.sort((a,b)=>a.start-b.start).filter((r,i,a)=>!i||r.start!==a[i-1].start);}
 function* markdownFragments(markdown,limit=PDF_CHUNK_CHARS){
  const definitions=[],definitionSet=new Set();let definitionSize=0;forEachReference(markdown,ref=>{const lineStart=markdown.lastIndexOf('\n',ref.start)+1,lineEnd=markdown.indexOf('\n',ref.start);if(lineStart<ref.start&&/^\s{0,3}\[[^\]]+\]:/.test(markdown.slice(lineStart,lineEnd<0?markdown.length:lineEnd))){const line=markdown.slice(lineStart,lineEnd<0?markdown.length:lineEnd);if(!definitionSet.has(line)&&definitionSize+line.length<65536){definitionSet.add(line);definitions.push(line);definitionSize+=line.length+1;}}});
  const definitionPrefix=definitions.length?definitions.join('\n')+'\n\n':'';let offset=0,parts=[],size=0,fence=null,tableHeader='',tableActive=false;
  const flush=()=>{if(!parts.length)return null;let value=parts.join('');if(fence)value+='\n'+fence.marker;parts=[];size=0;if(fence){parts.push(fence.marker+fence.info+'\n');size=parts[0].length;}if(tableActive&&tableHeader){parts.unshift(tableHeader+'\n');size+=tableHeader.length+1;}return value;};
  while(offset<markdown.length){let end=markdown.indexOf('\n',offset),hasNL=end>=0;if(!hasNL)end=markdown.length;const line=markdown.slice(offset,end),whole=(hasNL?line+'\n':line),marker=line.match(/^\s{0,3}(`{3,}|~{3,})(.*)$/);let cursor=0;
   if(whole.length>limit&& !marker){while(cursor<whole.length){const take=Math.min(limit-size,whole.length-cursor);parts.push(whole.slice(cursor,cursor+take));size+=take;cursor+=take;if(size>=limit&&cursor<whole.length){const ready=flush();if(ready)yield definitionPrefix+ready;}}
   }else{parts.push(whole);size+=whole.length;cursor=whole.length;}
   if(fence){if(marker&&marker[1][0]===fence.char&&marker[1].length>=fence.length)fence=null;}
   else if(marker)fence={marker:marker[1],info:marker[2]};
   if(!fence){if(!tableActive&&line.includes('|')){const previous=parts.length>1?parts[parts.length-2].replace(/\n$/,''):'';if(previous.includes('|')&&/^\s*\|?\s*:?-{3,}/.test(line)){tableHeader=previous+'\n'+line;tableActive=true;}}
    else if(tableActive&&!line.includes('|')){tableActive=false;tableHeader='';}}
   offset=end+1;
   if(size>=limit&&cursor>=whole.length){const ready=flush();if(ready)yield definitionPrefix+ready;}
  }
  if(parts.length){const ready=flush();if(ready)yield definitionPrefix+ready;}
 }
 async function readBounded(response,signal,limit=LIMIT){
  if(!response.ok)throw Error('资源读取失败（'+response.status+'）');if(Number(response.headers.get('content-length'))>limit)throw Error(uiTextMarkdownExport('单个资源超过 {0} MiB',Math.floor(limit/1048576)));
  const reader=response.body?.getReader();if(!reader){const b=await response.blob();if(b.size>limit)throw Error(uiTextMarkdownExport('单个资源超过 {0} MiB',Math.floor(limit/1048576)));return b;}
  const chunks=[];let size=0;try{while(true){if(signal.aborted)throw new DOMException('Cancelled','AbortError');const {done,value}=await reader.read();if(done)break;size+=value.length;if(size>limit)throw Error(uiTextMarkdownExport('单个资源超过 {0} MiB',Math.floor(limit/1048576)));chunks.push(value);}}catch(e){await reader.cancel();throw e;}finally{reader.releaseLock();}
  return new Blob(chunks,{type:response.headers.get('content-type')||'application/octet-stream'});
 }
 function uniquePdfAttachments(markdown,o){
  const paths=new Map();forEachReference(markdown,ref=>{if(ref.type==='image'||!ref.value||ref.value.startsWith('#')||/^data:/i.test(ref.value)||/^(?:[a-z][a-z0-9+.-]*:|\/\/)/i.test(ref.value))return;const path=o.resolve(ref.value.replace(/\\([() ])/g,'$1'));if(path&&!paths.has(path))paths.set(path,nameOf(path));});return [...paths].map(([path,name])=>({path,name}));
 }
 function pdfTextHex(value){let hex='FEFF';for(const char of String(value)){const cp=char.codePointAt(0);if(cp<=0xffff)hex+=cp.toString(16).padStart(4,'0');else{const n=cp-0x10000;hex+=(0xd800+(n>>10)).toString(16)+(0xdc00+(n&1023)).toString(16);}}return '<'+hex.toUpperCase()+'>';}
 function pdfLiteral(value){return String(value).replace(/[^\x20-\x7e]/g,'_').replace(/[\\()]/g,'\\$&');}
 async function preparePdfAttachments(markdown,o,job){
  const items=uniquePdfAttachments(markdown,o);if(items.length>PDF_ATTACHMENT_COUNT)throw Error(uiTextMarkdownExport('PDF 附件超过 256 项'));let total=0;
  const usedNames=new Set();for(let i=0;i<items.length;i++){if(job.controller.signal.aborted)throw new DOMException('Cancelled','AbortError');const item=items[i];item.blob=await readBounded(await o.fetchResource(item.path,job.controller.signal),job.controller.signal,PDF_ATTACHMENT_LIMIT);item.size=item.blob.size;total+=item.size;if(total>PDF_ATTACHMENT_TOTAL)throw Error(uiTextMarkdownExport('PDF 附件总量超过 256 MiB'));let name=item.name,version=2;while(usedNames.has(name.toLocaleLowerCase())){const dot=name.lastIndexOf('.'),suffix=' ('+version+')';version++;name=dot>0?name.slice(0,dot)+suffix+name.slice(dot):name+suffix;}item.name=name;usedNames.add(name.toLocaleLowerCase());item.index=i;}
  return items;
 }
 async function attachmentBytes(blob){const bytes=new Uint8Array(await blob.arrayBuffer());let binary='';for(let i=0;i<bytes.length;i+=0x8000)binary+=String.fromCharCode(...bytes.subarray(i,Math.min(i+0x8000,bytes.length)));return binary;}
 function registerPdfAttachments(pdf,attachments,shared){
  if(!attachments.length)return;const i=pdf.internal,files=[];let page=0,created=false;
  i.events.subscribe('putPage',({pageContext})=>{page++;if(!created){created=true;for(const item of attachments){const stream=i.newAdditionalObject(),spec=i.newAdditionalObject(),binary=item.binary;stream.content='<< /Type /EmbeddedFile /Length '+item.size+' /Params << /Size '+item.size+' >> >>\nstream\n'+binary+'\nendstream';const ascii=pdfLiteral(item.name);spec.content='<< /Type /Filespec /F ('+ascii+') /UF '+pdfTextHex(item.name)+' /EF << /F '+stream.objId+' 0 R /UF '+stream.objId+' 0 R >> /AFRelationship /Data >>';files.push({item,spec});}}
   const rects=shared.attachmentRects?.get(page)||[];for(const rect of rects){const file=files[rect.index];if(!file)continue;const x=12+rect.left*186/WIDTH,y=12+rect.top*186/WIDTH,w=Math.max(3,rect.width*186/WIDTH),h=Math.max(3,rect.height*186/WIDTH),coords=[i.getCoordinateString(x),i.getVerticalCoordinateString(y+h),i.getCoordinateString(x+w),i.getVerticalCoordinateString(y)];const annotation=i.newAdditionalObject();annotation.content='<< /Type /Annot /Subtype /Link /Rect ['+coords.join(' ')+'] /Border [0 0 0] /Contents (Open embedded '+pdfLiteral(file.item.name)+') /A << /S /GoToE /F '+file.spec.objId+' 0 R /D [0 /Fit] /NewWindow true >> >>';pageContext.annotations.push({type:'reference',object:annotation});}
  });
  i.events.subscribe('putCatalog',()=>{if(!files.length)return;const ordered=[...files].sort((a,b)=>pdfLiteral(a.item.name).localeCompare(pdfLiteral(b.item.name),'en'));const names=ordered.map(({item,spec})=>'('+pdfLiteral(item.name)+') '+spec.objId+' 0 R').join(' ');i.write('/Names << /EmbeddedFiles << /Names ['+names+'] >> >>');i.write('/AF ['+ordered.map(({spec})=>spec.objId+' 0 R').join(' ')+']');});
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
 // Preflight before rendering or converting untrusted document HTML.
 function validateDOM(root){
  const pending=[{node:root,depth:0}];let count=0;
  while(pending.length){
   const {node,depth}=pending.pop();
   if(depth>128)throw Error(uiTextMarkdownExport('文档结构过深，请简化后导出'));
   if(++count>50000||pending.length+node.childNodes.length>50000)throw Error(uiTextMarkdownExport('文档节点过多，请拆分后导出'));
   for(let i=0;i<node.childNodes.length;i++)pending.push({node:node.childNodes[i],depth:depth+1});
  }
 }
 // Decode image data locally: connect-src intentionally excludes data: fetches.
 function inlineImageBlob(url,budget){
  const invalid=()=>Error(uiTextMarkdownExport('图片渲染失败'));
  const limit=()=>Error(uiTextMarkdownExport('图片资源总量超过 32 MiB'));
  if(url.protocol!=='data:')throw invalid();
  if(url.href.length>budget*4+512)throw limit();
  let content=url.href.slice(5),fragment=content.indexOf('#');if(fragment>=0)content=content.slice(0,fragment);
  const comma=content.indexOf(',');if(comma<0||comma>512)throw invalid();
  const header=content.slice(0,comma).trim(),base64=/;[ \t]*base64$/i.test(header);
  const media=(base64?header.replace(/;[ \t]*base64$/i,''):header).trim();
  if(!/^image\/[a-z0-9][a-z0-9.+-]*$/i.test(media.split(';',1)[0].trim()))throw invalid();
  const body=content.slice(comma+1);let bytes;
  if(base64){
   let text;try{text=decodeURIComponent(body).replace(/[\t\n\f\r ]/g,'');}catch(_){throw invalid();}
   const padding=text.endsWith('==')?2:text.endsWith('=')?1:0;
   if(Math.floor(text.length*3/4)-padding>budget)throw limit();
   let binary;try{binary=atob(text);}catch(_){throw invalid();}
   if(binary.length>budget)throw limit();bytes=new Uint8Array(binary.length);
   for(let i=0;i<binary.length;i++)bytes[i]=binary.charCodeAt(i);
  }else{
   bytes=new Uint8Array(Math.min(body.length*3,budget));let used=0;
   const write=value=>{if(used>=budget)throw limit();bytes[used++]=value;};
   const hex=c=>c>=48&&c<=57?c-48:c>=65&&c<=70?c-55:c>=97&&c<=102?c-87:-1;
   for(let i=0;i<body.length;i++){
    const c=body.charCodeAt(i),a=c===37?hex(body.charCodeAt(i+1)):-1,b=c===37?hex(body.charCodeAt(i+2)):-1;
    if(a>=0&&b>=0){write((a<<4)|b);i+=2;}
    else if(c<128)write(c);
    else{const cp=body.codePointAt(i);for(const byte of new TextEncoder().encode(String.fromCodePoint(cp)))write(byte);if(cp>65535)i++;}
   }
   bytes=bytes.subarray(0,used);
  }
  return new Blob([bytes],{type:media});
 }
 function imageDataURL(blob,signal){
  return new Promise((resolve,reject)=>{
   const reader=new FileReader();let done=false;
   const finish=(error,value)=>{if(done)return;done=true;signal.removeEventListener('abort',cancel);reader.onload=reader.onerror=reader.onabort=null;error?reject(error):resolve(value);};
   const cancel=()=>{reader.abort();finish(new DOMException('Cancelled','AbortError'));};
   reader.onload=()=>finish(null,reader.result);reader.onerror=()=>finish(reader.error||Error(uiTextMarkdownExport('图片渲染失败')));reader.onabort=()=>finish(new DOMException('Cancelled','AbortError'));
   signal.addEventListener('abort',cancel,{once:true});if(signal.aborted)cancel();else try{reader.readAsDataURL(blob);}catch(error){finish(error);}
  });
 }
 async function pdfImageBlob(blob,img,job){
  const signature=new Uint8Array(await job.wait(blob.slice(0,8).arrayBuffer()));
  if(signature[0]===255&&signature[1]===216)return blob;
  const pngSignature=[137,80,78,71,13,10,26,10];
  if(pngSignature.every((byte,index)=>signature[index]===byte))return blob;

  // PDFMake only accepts PNG and JPEG for image nodes. Use the browser's
  // decoded image at up to twice its print size, keeping alpha and aspect ratio.
  const canvas=img.ownerDocument.createElement('canvas');
  const {width,height}=img.getBoundingClientRect();
  const scale=width&&height?Math.min(1,2*width/img.naturalWidth,2*height/img.naturalHeight):1;
  canvas.width=Math.max(1,Math.round(img.naturalWidth*scale));
  canvas.height=Math.max(1,Math.round(img.naturalHeight*scale));
  try{
   canvas.getContext('2d').drawImage(img,0,0,canvas.width,canvas.height);
   const converted=await job.wait(new Promise(resolve=>canvas.toBlob(resolve,'image/png')));
   if(converted)return converted;
  }catch(error){
   if(error?.name==='AbortError')throw error;
  }finally{canvas.width=canvas.height=0;}
  throw Error(uiTextMarkdownExport('图片渲染失败'));
 }
 async function snapshot(o,job,html=o.html,pdfImages=null){
  if(job.controller.signal.aborted)throw new DOMException('Cancelled','AbortError');
  const frame=document.createElement('iframe');frame.className='markdown-print-frame';frame.setAttribute('aria-hidden','true');frame.tabIndex=-1;document.body.append(frame);job.frame=frame;
  const doc=frame.contentDocument;doc.open();doc.write('<!doctype html><html><head><meta charset="utf-8"></head><body></body></html>');doc.close();
  const style=doc.createElement('link');
  style.rel='stylesheet';
  style.href='/markdown-print.css?v=1';
  await job.wait(new Promise((resolve,reject)=>{
   style.onload=resolve;
   style.onerror=()=>reject(Error(uiTextMarkdownExport('无法加载导出样式')));
   doc.head.append(style);
  }));
  const body=doc.createElement('main');body.innerHTML=html;validateDOM(body);
  body.querySelectorAll('script,iframe,object,embed,form,input,button,textarea,select').forEach(e=>e.remove());
  for(const e of body.querySelectorAll('*'))for(const a of [...e.attributes])if(/^on/i.test(a.name)||['srcdoc','style','srcset','data-pdfmake','data-pdfmake-type'].includes(a.name))e.removeAttribute(a.name);
  doc.body.append(body);if(o.renderDiagrams)await job.wait(o.renderDiagrams(body));validateDOM(body);
  for(const img of body.querySelectorAll('img')){img.style.setProperty('display','block','important');img.style.setProperty('width','auto','important');img.style.setProperty('height','auto','important');img.style.setProperty('max-width','100%','important');img.style.setProperty('max-height',PAGE+'px','important');img.style.setProperty('object-fit','contain','important');}
  // Diagrams should stay together on a page when their natural portrait size
  // is larger than the printable area. Preserve their viewBox aspect ratio.
  for(const svg of body.querySelectorAll('svg')){svg.style.setProperty('width','auto','important');svg.style.setProperty('height','auto','important');svg.style.setProperty('max-width','100%','important');svg.style.setProperty('max-height',pdfImages?PAGE+'px':'560px','important');svg.style.setProperty('object-fit','contain','important');}
  const budget=o.imageBudget||{total:0,pdfTotal:0};
  for(const img of body.querySelectorAll('img')){
   const url=img.getAttribute('src');if(!url)throw Error(uiTextMarkdownExport('图片地址为空'));
   const signal=job.controller.signal;if(signal.aborted)throw new DOMException('Cancelled','AbortError');
   const parsed=new URL(url,location.href),blob=parsed.protocol==='data:'?inlineImageBlob(parsed,LIMIT-budget.total):await readBounded(await fetch(url,{signal,credentials:parsed.origin===location.origin?'same-origin':'omit'}),signal);budget.total+=blob.size;if(budget.total>LIMIT)throw Error(uiTextMarkdownExport('图片资源总量超过 32 MiB'));
   const data=await imageDataURL(blob,signal);
   await new Promise((resolve,reject)=>{const signal=job.controller.signal;const finish=(err)=>{clearTimeout(timer);signal.removeEventListener('abort',cancel);img.onload=img.onerror=null;err?reject(err):resolve();};const cancel=()=>finish(new DOMException('Cancelled','AbortError')),timer=setTimeout(()=>finish(Error(uiTextMarkdownExport('图片渲染超时'))),15000);signal.addEventListener('abort',cancel,{once:true});img.onload=()=>finish();img.onerror=()=>finish(Error(uiTextMarkdownExport('图片渲染失败')));if(signal.aborted)cancel();else img.src=data;});
   if(img.naturalWidth*img.naturalHeight>24000000)throw Error(uiTextMarkdownExport('图片像素过大，请缩小后导出'));
   if(pdfImages){
    const pdfBlob=await pdfImageBlob(blob,img,job);budget.pdfTotal+=pdfBlob.size;
    if(budget.pdfTotal>LIMIT)throw Error(uiTextMarkdownExport('图片资源总量超过 32 MiB'));
    if(pdfBlob!==blob)pdfImages.set(img,await imageDataURL(pdfBlob,signal));
   }
  }
  await job.wait(doc.fonts?.ready);
  return doc.body;
 }
 let vectorPdfReady=null;
 function base64FromBytes(bytes){let out='';for(let i=0;i<bytes.length;i+=0x6000)out+=String.fromCharCode(...bytes.subarray(i,Math.min(i+0x6000,bytes.length)));return btoa(out);}
 async function loadVectorPdf(job,o){
  if(!vectorPdfReady)vectorPdfReady=(async()=>{
   await job.wait(o.load('/vendor/markdown-export/pdfmake.min.js?v=2',()=>!!window.pdfMake?.createPdf));
   await job.wait(o.load('/vendor/markdown-export/html-to-pdfmake.js',()=>!!window.htmlToPdfmake));
   await job.wait(o.load('/vendor/markdown-export/pdf-lib.min.js',()=>!!window.PDFLib?.PDFDocument));
   const regular='NotoSansSC-Regular.ttf',semibold='NotoSansSC-SemiBold.ttf';
   const files=await Promise.all([regular,semibold].map(async name=>{
    const response=await job.wait(fetch('/vendor/markdown-export/'+name,{signal:job.controller.signal}));
    if(!response.ok)throw Error('中文字库加载失败');
    const bytes=new Uint8Array(await job.wait(response.arrayBuffer()));
    return [name,base64FromBytes(bytes)];
   }));
   window.pdfMake.addVirtualFileSystem(Object.fromEntries(files));
   window.pdfMake.addFonts({NotoSansSC:{normal:regular,bold:semibold,italics:regular,bolditalics:semibold}});
  })().catch(error=>{vectorPdfReady=null;throw error;});
  await job.wait(vectorPdfReady);
 }
 function compactPdfCssColors(value){return value.replace(/\b(rgba?)\(\s*(\d+)\s*,\s*(\d+)\s*,\s*(\d+)(?:\s*,\s*([\d.]+))?\s*\)/gi,(_,fn,r,g,b,a)=>fn.toLowerCase()==='rgba'&&a!==undefined?`rgba(${r},${g},${b},${a})`:`rgb(${r},${g},${b})`);}
 const PDF_MARGIN = 48;
 const PDF_PT_PER_PX = 0.75;
 const PDF_A4 = {width:595.28,height:841.89};
 const PDF_TABLE_LAYOUT = {
  hLineWidth:(row,node)=>row===node.table.headerRows?0.9:0.45,
  hLineColor:(row,node)=>row===node.table.headerRows?'#9aaabd':'#d8e0e8',
  vLineWidth:()=>0,
  paddingLeft:()=>6,paddingRight:()=>6,paddingTop:()=>5,paddingBottom:()=>5
 };
 function pdfTableColumns(table){
  return Array.from(table.rows).reduce((max,row)=>Math.max(max,Array.from(row.cells).reduce((total,cell)=>total+cell.colSpan,0)),0);
 }
 function fitPdfMedia(width,height,maxWidth,maxHeight){
  if(!(width>0&&height>0))return null;
  const scale=Math.min(PDF_PT_PER_PX,maxWidth/width,maxHeight/height);
  return {width:width*scale,height:height*scale};
 }
 function pdfElementWidth(element,printableWidth){
  const cell=element.closest('td,th');
  if(!cell)return printableWidth;
  const width=cell.getBoundingClientRect().width*PDF_PT_PER_PX;
  // Reserve the actual PDFMake cell padding and room for rounding.
  return Math.max(1,Math.min(printableWidth,width-14));
 }
 function stylePdfMedia(element,printable,pdfImages){
  const svg=element.tagName.toLowerCase()==='svg';
  const cell=element.closest('td,th');
  const box=element.getBoundingClientRect();
  const view=svg?element.viewBox?.baseVal:null;
  const width=svg?(view?.width||element.width?.baseVal.value||box.width):element.naturalWidth;
  const height=svg?(view?.height||element.height?.baseVal.value||box.height):element.naturalHeight;
  if(svg&&!element.hasAttribute('viewBox')&&width&&height){
   element.setAttribute('viewBox','0 0 '+width+' '+height);
  }
  const head=cell?.closest('table')?.tHead?.getBoundingClientRect().height||0;
  const headerReserve=Math.min(printable.height/3,head*PDF_PT_PER_PX);
  const boundHeight=cell?.closest('thead')?printable.height/3:
   cell?Math.min(printable.height*0.62,printable.height-headerReserve-34):
    printable.height-Math.min(126,printable.height*0.27);
  const boundWidth=Math.min(box.width*PDF_PT_PER_PX||printable.width,pdfElementWidth(element,printable.width));
  const dimensions=fitPdfMedia(width,height,boundWidth,boundHeight);
  if(dimensions){
   element.style.setProperty('width',dimensions.width/PDF_PT_PER_PX+'px','important');
   element.style.setProperty('height',dimensions.height/PDF_PT_PER_PX+'px','important');
   element.dataset.pdfmake=JSON.stringify({...dimensions,margin:cell?[0,2,0,2]:[0,6,0,12],alignment:cell?'left':'center'});
  }
  const data=pdfImages?.get(element);
  if(data)element.src=data;
 }
 function trimPdfBlockWhitespace(body){
  const block=/^(?:ADDRESS|ARTICLE|ASIDE|BLOCKQUOTE|DETAILS|DIV|FIGURE|H[1-6]|HR|LI|MAIN|OL|P|PRE|SECTION|TABLE|TBODY|THEAD|TFOOT|TR|UL)$/;
  for(const parent of body.querySelectorAll('main,article,section,div,figure,blockquote,ul,ol,table,thead,tbody,tfoot,tr')){
   for(const node of Array.from(parent.childNodes)){
    if(node.nodeType===3&&!node.textContent.trim()&&
      (block.test(node.previousSibling?.nodeName||'')||block.test(node.nextSibling?.nodeName||'')))node.remove();
   }
  }
 }
 function vectorPdfDefinition(body,attachments=[],pdfImages){
  trimPdfBlockWhitespace(body);
  const tables=[...body.querySelectorAll('table')];
  const landscape=tables.some(table=>pdfTableColumns(table)>5);
  const width=landscape?PDF_A4.height:PDF_A4.width;
  const height=landscape?PDF_A4.width:PDF_A4.height;
  const printable={width:width-2*PDF_MARGIN,height:height-2*PDF_MARGIN};
  if(body.style){
   body.style.width=width/PDF_PT_PER_PX+'px';
   body.style.maxWidth='none';
   body.style.paddingInline=PDF_MARGIN/PDF_PT_PER_PX+'px';
  }
  for(const heading of body.querySelectorAll('h1,h2,h3,h4,h5,h6')){
   heading.dataset.pdfmake=JSON.stringify({headlineLevel:Number(heading.tagName.slice(1))});
  }
  for(const rule of body.querySelectorAll('hr')){
   rule.dataset.pdfmake=JSON.stringify({width:pdfElementWidth(rule,printable.width),color:'#c4ced9',thickness:0.6});
  }
  for(const element of body.querySelectorAll('img,svg'))stylePdfMedia(element,printable,pdfImages);
  for(const element of body.querySelectorAll('*')){
   const style=element.ownerDocument.defaultView.getComputedStyle(element);
   for(const [source,target] of [['color','color'],['backgroundColor','background-color'],['fontSize','font-size'],['fontWeight','font-weight'],['fontStyle','font-style'],['textAlign','text-align'],['textDecoration','text-decoration'],['verticalAlign','vertical-align'],['whiteSpace','white-space']]){
    const value=style[source];
    if(value&&value!=='none'&&value!=='normal'&&value!=='rgba(0, 0, 0, 0)')element.style.setProperty(target,compactPdfCssColors(value));
   }
  }
  const converted=window.htmlToPdfmake(compactPdfCssColors(body.innerHTML),{
   window:body.ownerDocument.defaultView,
   tableAutoSize:true,
   defaultStyles:{
    h1:{fontSize:29,bold:true,margin:[0,6,0,18]},h2:{fontSize:21,bold:true,margin:[0,18,0,9]},
    h3:{fontSize:16,bold:true,margin:[0,13,0,7]},h4:{fontSize:13,bold:true,margin:[0,10,0,5]},
    h5:{fontSize:11,bold:true,margin:[0,8,0,4]},h6:{fontSize:10,bold:true,margin:[0,8,0,4]},
    p:{margin:[0,0,0,8]},pre:{fontSize:8,margin:[0,6,0,10],wordBreak:'break-all',fillColor:'#f2f5f8'},
    blockquote:{color:'#4e6073',margin:[14,7,12,10],italics:true},
    th:{bold:true,fillColor:'#eaf0f6'},td:{margin:0},table:{margin:[0,9,0,13]},
    a:{color:'#24598a',decoration:'underline'}
   }
  });
  const content=Array.isArray(converted)?converted:converted?.content||[converted];
  let tableIndex=0;
  const pending=[content];
  while(pending.length){
   const value=pending.pop();
   if(!value||typeof value!=='object')continue;
   if(Array.isArray(value)){
    for(let i=value.length-1;i>=0;i--)pending.push(value[i]);
    continue;
   }
   if(value.table?.body){
    const source=tables[tableIndex++];
    const cells=Array.from(source?.rows[0]?.cells||[]);
    const count=source?pdfTableColumns(source):0;
    if(cells.length&&count){
     const widths=cells.flatMap(cell=>Array(cell.colSpan).fill(cell.getBoundingClientRect().width/cell.colSpan)).slice(0,count);
     while(widths.length<count)widths.push(widths.at(-1)||1);
     const total=widths.reduce((sum,width)=>sum+width,0)||1;
     value.table.widths=widths.map(width=>(width/total*100).toFixed(3)+'%');
    }
    const headerRows=source?.querySelectorAll('thead tr').length||0;
    if(headerRows)value.table.headerRows=headerRows;
    if(source?.rows[0]?.getBoundingClientRect){
     const headerHeight=source.tHead?.getBoundingClientRect().height||0;
     value.table.dontBreakRows=Array.from(source.rows).every(row=>
      (row.getBoundingClientRect().height+headerHeight)*PDF_PT_PER_PX < printable.height*0.76);
    }
    value.layout=PDF_TABLE_LAYOUT;
    for(const [rowIndex,row] of value.table.body.entries()){
     const sourceCells=source?.rows[rowIndex]?.cells;
     for(const [index,cell] of row.entries()){
      const original=sourceCells?.[index];
      if(!original||!cell||typeof cell!=='object')continue;
      const width=original.getBoundingClientRect().width*PDF_PT_PER_PX-14;
      const maxWord=Math.max(5,Math.floor(width/7));
      // CJK already has natural breaks. Let ordinary words wrap between words
      // and allow a break inside longer IDs, URLs, and other unspaced runs.
      if((original.textContent||'').match(/[!-~]+/g)?.some(word=>word.length>maxWord))cell.wordBreak='break-all';
     }
    }
   }
   // Traverse only document children, so any layout callbacks stay outside
   // the walk and nested tables are processed in source order.
   const children=value.table?.body||value.stack||value.columns||value.ul||value.ol||value.text;
   if(Array.isArray(children))pending.push(children);
  }
  if(attachments.length){
   content.push({text:'附件',fontSize:17,bold:true,margin:[0,18,0,9]});
   for(const item of attachments)content.push({columns:[{text:item.name,width:'*',fontSize:10,id:'aide-attachment-'+item.index},{attachment:{src:item.dataUrl,name:item.name,description:item.name,hidden:false,relationship:'Data'},icon:'Paperclip',width:24}],columnGap:8,margin:[0,0,0,6]});
  }
  return {
   pageSize:'A4',pageOrientation:landscape?'landscape':'portrait',pageMargins:[PDF_MARGIN,PDF_MARGIN,PDF_MARGIN,PDF_MARGIN],
   defaultStyle:{font:'NotoSansSC',fontSize:10.5,lineHeight:1.24,color:'#243448'},
   pageBreakBefore:(heading,context)=>!!heading.headlineLevel&&heading.pageNumbers?.length===1&&!context.getFollowingNodesOnPage().length&&context.getNodesOnNextPage().length>0,
   content
  };
 }
 async function renderVectorChunk(html,o,job,attachments=[]){
  const pdfImages=new Map(),body=await snapshot(o,job,html,pdfImages);
  return job.wait(window.pdfMake.createPdf(vectorPdfDefinition(body,attachments,pdfImages)).getBlob());
 }
 async function mergeVectorPdf(parts,last,stem,job){
  const {PDFDocument,StandardFonts,rgb}=window.PDFLib,target=await job.wait(PDFDocument.load(await job.wait(last.arrayBuffer())));let index=0;
  for(const bytes of parts){if(job.controller.signal.aborted)throw new DOMException('Cancelled','AbortError');const source=await job.wait(PDFDocument.load(bytes)),pages=await job.wait(target.copyPages(source,source.getPageIndices()));for(const page of pages)target.insertPage(index++,page);}
  const pages=target.getPages(),font=await job.wait(target.embedFont(StandardFonts.Helvetica));
  for(const [index,page] of pages.entries()){
   const right=page.getWidth()-PDF_MARGIN;
   page.drawLine({start:{x:PDF_MARGIN,y:34},end:{x:right,y:34},thickness:0.45,color:rgb(0.81,0.85,0.89)});
   const label=(index+1)+' / '+pages.length,size=8;
   page.drawText(label,{x:right-font.widthOfTextAtSize(label,size),y:20,size,font,color:rgb(0.32,0.38,0.46)});
  }
  target.setTitle(stem);target.setCreator('aide');return new Blob([await job.wait(target.save({useObjectStreams:true}))],{type:'application/pdf'});
 }
 async function run(format,o,progress=()=>{},shared=null){
  if(!shared)close();if(!o.valid())return;const job=shared?.job||{controller:new AbortController()};if(!job.wait)job.wait=p=>new Promise((resolve,reject)=>{const signal=job.controller.signal,cancel=()=>{signal.removeEventListener('abort',cancel);reject(new DOMException('Cancelled','AbortError'));};signal.addEventListener('abort',cancel,{once:true});if(signal.aborted)cancel();Promise.resolve(p).then(v=>{signal.removeEventListener('abort',cancel);resolve(v);},e=>{signal.removeEventListener('abort',cancel);reject(e);});});active=job;const alive=()=>active===job&&!job.controller.signal.aborted&&o.valid();
  // Abort stale/locked documents; bound network operations independently of page lifetime.
  const guard=shared?null:setInterval(()=>{if(!alive())job.controller.abort();},250);let timeout=shared?null:setTimeout(()=>{job.timedOut=true;job.controller.abort();},120000);
  const name=nameOf(o.title),stem=name.replace(/\.(md|markdown)$/i,'');
  try{
   if(format==='pdf'&&!shared){
    await loadVectorPdf(job,o);
    clearTimeout(timeout);timeout=setTimeout(()=>{job.timedOut=true;job.controller.abort();},6*60*60*1000);
    const attachments=await preparePdfAttachments(String(o.markdown),o,job);for(const item of attachments){item.dataUrl=await imageDataURL(item.blob,job.controller.signal);item.blob=null;}
    const prior=[],imageBudget={total:0,pdfTotal:0};let pending=null,chunk=0,last=null;
    const emit=async(fragment,isLast)=>{if(!alive())throw new DOMException('Cancelled','AbortError');const html=o.renderMarkdown?o.renderMarkdown(fragment):o.html;last=await renderVectorChunk(html,{...o,markdown:fragment,renderMarkdown:null,imageBudget},job,isLast?attachments:[]);chunk++;progress(chunk,chunk,chunk);};
    for(const fragment of markdownFragments(String(o.markdown),PDF_CHUNK_CHARS)){if(pending!==null){await emit(pending,false);prior.push(new Uint8Array(await job.wait(last.arrayBuffer())));}pending=fragment;}
    if(pending!==null)await emit(pending,true);else await emit('',true);
    const blob=await mergeVectorPdf(prior,last,stem,job);if(alive())download(blob,stem+'.pdf');return;
   }
   let blob,extension;
   if(format==='md'){blob=new Blob([o.markdown],{type:'text/markdown;charset=utf-8'});extension='md';}
   else if(format==='zip'){blob=await exportZIP(o,job);extension='zip';}
   else if(format==='png'){
    if(shared)throw Error('PNG export cannot use a shared PDF renderer');
    await job.wait(o.load('/vendor/markdown-export/html2canvas.min.js?v=2',()=>!!window.html2canvas));
    const body=await snapshot(o,job,o.renderMarkdown?o.renderMarkdown(o.markdown):o.html),height=Math.ceil(body.scrollHeight);
    if(height>10000)throw Error(uiTextMarkdownExport('长图超过 10000 像素，请选择分页 PDF'));
    if(!alive())throw new DOMException('Cancelled','AbortError');
    const canvas=await job.wait(window.html2canvas(body,{backgroundColor:'#fff',scale:1.5,width:WIDTH,height,y:0,windowWidth:WIDTH,windowHeight:1123,logging:false,imageTimeout:15000,signal:job.controller.signal}));
    try{blob=await job.wait(new Promise((resolve,reject)=>canvas.toBlob(b=>b?resolve(b):reject(Error(uiTextMarkdownExport('图片生成失败'))),'image/png')));}finally{canvas.width=canvas.height=0;}
    extension='png';
   }else throw Error('Unsupported export format');
   if(alive())download(blob,format==='md'?name:stem+'.'+extension);
  }catch(e){if(job.timedOut)throw Error(uiTextMarkdownExport('导出超时，请检查资源连接或拆分文档'));throw e;}finally{if(!shared){clearInterval(guard);clearTimeout(timeout);job.controller.abort();job.frame?.remove();job.renderFrame?.remove();if(active===job)active=null;}else{job.frame?.remove();job.renderFrame?.remove();job.frame=job.renderFrame=null;}}
 }
 function bind({button,options,t=s=>s,error=()=>{}}){
  button.setAttribute('aria-haspopup','menu');button.setAttribute('aria-expanded','false');
  button.onclick=()=>{
   if(menu?.button===button){closeMenu();return;}closeMenu();
   const host=document.createElement('div');host.className='markdown-export-menu';host.setAttribute('role','menu');host.setAttribute('aria-label',t('导出'));button.parentNode.append(host);
   const formats=[['pdf','PDF','直接下载 · 分页文档'],['md','Markdown','下载原文 · .md'],['zip','Markdown + 资源','图片与附件 · .zip'],['png','PNG 图片','渲染为一张长图']];
   for(const [format,label,description] of formats){const item=document.createElement('button');item.type='button';item.setAttribute('role','menuitem');const badge=document.createElement('span');badge.className='markdown-export-format';badge.textContent=format.toUpperCase();const text=document.createElement('span'),strong=document.createElement('strong'),small=document.createElement('small');strong.textContent=t(label);small.textContent=t(description);text.append(strong,small);item.append(badge,text);host.append(item);
    item.onclick=async()=>{const o=options();closeMenu();if(!o)return;button.disabled=true;button.setAttribute('aria-busy','true');const progress=document.createElement('span');progress.className='markdown-export-status';progress.setAttribute('role','status');progress.setAttribute('aria-live','polite');progress.textContent=t('准备导出…');button.insertAdjacentElement('afterend',progress);try{await run(format,o,(current,total,chunk)=>{progress.textContent=chunk?t('正在转换 Markdown 片段 {chunk}…').replace('{chunk}',String(chunk)).replace('{current}',String(current)).replace('{total}',String(total)):t('正在渲染第 {current}/{total} 页').replace('{current}',String(current)).replace('{total}',String(total));button.setAttribute('aria-valuetext',progress.textContent);});}catch(e){if(e.name!=='AbortError')error(e);}finally{button.disabled=false;button.removeAttribute('aria-busy');button.removeAttribute('aria-valuetext');progress.remove();}};
   }
   const outside=e=>{if(!host.contains(e.target)&&e.target!==button)closeMenu();};const key=e=>{if(e.key==='Escape'){e.preventDefault();closeMenu();}else if(['ArrowDown','ArrowUp'].includes(e.key)){e.preventDefault();const items=[...host.children],i=items.indexOf(document.activeElement);items[(i+(e.key==='ArrowDown'?1:items.length-1)+items.length)%items.length].focus();}else if(e.key==='Tab')closeMenu();};
   const blur=()=>closeMenu();document.addEventListener('pointerdown',outside);host.addEventListener('keydown',key);window.addEventListener('resize',blur);
   button.setAttribute('aria-expanded','true');
   menu={host,button,dispose:()=>{document.removeEventListener('pointerdown',outside);window.removeEventListener('resize',blur);}};
   if(host.showPopover){host.setAttribute('popover','manual');host.showPopover();}
   const r=button.getBoundingClientRect();host.style.left=Math.max(8,Math.min(r.right-280,window.innerWidth-288))+'px';host.style.top=Math.max(8,Math.min(r.bottom+8,window.innerHeight-host.offsetHeight-8))+'px';host.children[0].focus();
  };
 }
 window.AideMarkdownExport={bind,run,close,references,fragments:markdownFragments};
 if(window.__AIDE_MARKDOWN_EXPORT_TEST__)window.AideMarkdownExport.test={registerPdfAttachments,uniquePdfAttachments};
})();

function uiTextMarkdownExport(key, ...values) { return globalThis.window?.aideI18n?.t(key, ...values) ?? String(key).replace(/\{(\d+)\}/g, (match, index) => index < values.length ? String(values[index]) : match); }
