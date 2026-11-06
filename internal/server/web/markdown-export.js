/* Local Markdown downloads. Rendering libraries are bundled, loaded only on demand. */
(() => {
 'use strict';
 const LIMIT=32*1024*1024, PDF_ATTACHMENT_LIMIT=64*1024*1024, PDF_ATTACHMENT_TOTAL=256*1024*1024, PDF_ATTACHMENT_COUNT=256;
 const PDF_CHUNK_CHARS=192*1024, WIDTH=794, PAGE=987;
 let active=null, menu=null;
 const css=`*{box-sizing:border-box;max-width:100%}body{margin:0;width:794px;padding:48px 60px;color:#202630;background:#fff;font:15px/1.7 -apple-system,BlinkMacSystemFont,"Segoe UI","Microsoft YaHei","PingFang SC",sans-serif}main{min-width:0;overflow-wrap:anywhere}h1,h2,h3,h4{line-height:1.3}h1{font-size:32px}h2{font-size:25px}h3{font-size:20px}img,svg{max-width:100%!important;height:auto;object-fit:contain}pre{max-width:100%;white-space:pre-wrap;overflow-wrap:anywhere;font:12px/1.5 ui-monospace,Consolas,monospace;padding:14px;background:#f3f5f7;border:1px solid #718096;border-radius:6px}table{border-collapse:collapse;width:100%;max-width:100%;table-layout:fixed;font-size:14px;border:1.25px solid #718096}th,td{border:1.25px solid #718096!important;padding:8px;text-align:left;overflow-wrap:anywhere;word-break:break-word}blockquote{margin:16px 0;padding-left:16px;border-left:3px solid #8295ab;color:#46505e}a{color:#355f86;text-decoration:none}hr{border:0;border-top:1px solid #cdd4dd}button,nav,.md-outline{display:none!important}`;
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
 async function snapshot(o,job,html=o.html){
  if(job.controller.signal.aborted)throw new DOMException('Cancelled','AbortError');
  const frame=document.createElement('iframe');frame.className='markdown-print-frame';frame.setAttribute('aria-hidden','true');frame.tabIndex=-1;document.body.append(frame);job.frame=frame;
  const doc=frame.contentDocument;doc.open();doc.write('<!doctype html><html><head><meta charset="utf-8"></head><body></body></html>');doc.close();
  const style=doc.createElement('style');style.textContent=css;doc.head.append(style);const body=doc.createElement('main');body.innerHTML=html;validateDOM(body);
  body.querySelectorAll('script,iframe,object,embed,form,input,button,textarea,select').forEach(e=>e.remove());
  for(const e of body.querySelectorAll('*'))for(const a of [...e.attributes])if(/^on/i.test(a.name)||['srcdoc','style','srcset'].includes(a.name))e.removeAttribute(a.name);
  doc.body.append(body);if(o.renderDiagrams)await job.wait(o.renderDiagrams(body));validateDOM(body);
  // Keep print-critical paints on the snapshot as inline styles so the vector
  // HTML-to-PDF converter sees the table and code borders without app CSS.
  for(const table of body.querySelectorAll('table')){
   table.style.borderCollapse='collapse';table.style.width='100%';table.style.tableLayout='fixed';table.style.border='1px solid #718096';
   for(const cell of table.querySelectorAll('th,td')){cell.style.border='1px solid #718096';cell.style.padding='8px';cell.style.textAlign='left';cell.style.overflowWrap='anywhere';cell.style.wordBreak='break-word';}
  }
  for(const pre of body.querySelectorAll('pre')){pre.style.border='1px solid #718096';pre.style.padding='14px';pre.style.whiteSpace='pre-wrap';pre.style.overflowWrap='anywhere';}
  for(const img of body.querySelectorAll('img')){img.style.setProperty('display','block','important');img.style.setProperty('width','auto','important');img.style.setProperty('height','auto','important');img.style.setProperty('max-width','100%','important');img.style.setProperty('max-height',PAGE+'px','important');img.style.setProperty('object-fit','contain','important');}
  // Diagrams should stay together on a page when their natural portrait size
  // is larger than the printable area. Preserve their viewBox aspect ratio.
  for(const svg of body.querySelectorAll('svg')){svg.style.setProperty('width','auto','important');svg.style.setProperty('height','auto','important');svg.style.setProperty('max-width','100%','important');svg.style.setProperty('max-height','560px','important');svg.style.setProperty('object-fit','contain','important');}
  const budget=o.imageBudget||{total:0};
  for(const img of body.querySelectorAll('img')){
   const url=img.getAttribute('src');if(!url)throw Error(uiTextMarkdownExport('图片地址为空'));
   const signal=job.controller.signal;if(signal.aborted)throw new DOMException('Cancelled','AbortError');
   const parsed=new URL(url,location.href),blob=parsed.protocol==='data:'?inlineImageBlob(parsed,LIMIT-budget.total):await readBounded(await fetch(url,{signal,credentials:parsed.origin===location.origin?'same-origin':'omit'}),signal);budget.total+=blob.size;if(budget.total>LIMIT)throw Error(uiTextMarkdownExport('图片资源总量超过 32 MiB'));
   const data=await imageDataURL(blob,signal);
   await new Promise((resolve,reject)=>{const signal=job.controller.signal;const finish=(err)=>{clearTimeout(timer);signal.removeEventListener('abort',cancel);img.onload=img.onerror=null;err?reject(err):resolve();};const cancel=()=>finish(new DOMException('Cancelled','AbortError')),timer=setTimeout(()=>finish(Error(uiTextMarkdownExport('图片渲染超时'))),15000);signal.addEventListener('abort',cancel,{once:true});img.onload=()=>finish();img.onerror=()=>finish(Error(uiTextMarkdownExport('图片渲染失败')));if(signal.aborted)cancel();else img.src=data;});
  }
  await job.wait(doc.fonts?.ready);
  for(const img of body.querySelectorAll('img'))if(img.naturalWidth*img.naturalHeight>24000000)throw Error(uiTextMarkdownExport('图片像素过大，请缩小后导出'));
  return doc.body;
 }
 let vectorPdfReady=null;
 function base64FromBytes(bytes){let out='';for(let i=0;i<bytes.length;i+=0x6000)out+=String.fromCharCode(...bytes.subarray(i,Math.min(i+0x6000,bytes.length)));return btoa(out);}
 async function loadVectorPdf(job,o){
  if(!vectorPdfReady)vectorPdfReady=(async()=>{
   await job.wait(o.load('/vendor/markdown-export/pdfmake.min.js',()=>!!window.pdfMake?.createPdf));
   await job.wait(o.load('/vendor/markdown-export/html-to-pdfmake.js',()=>!!window.htmlToPdfmake));
   await job.wait(o.load('/vendor/markdown-export/pdf-lib.min.js',()=>!!window.PDFLib?.PDFDocument));
   const response=await job.wait(fetch('/vendor/markdown-export/NotoSansSC-VF.ttf',{signal:job.controller.signal}));if(!response.ok)throw Error('中文字库加载失败');
   const font=base64FromBytes(new Uint8Array(await job.wait(response.arrayBuffer())));
   window.pdfMake.addVirtualFileSystem({'NotoSansSC-VF.ttf':font});
   window.pdfMake.addFonts({NotoSansSC:{normal:'NotoSansSC-VF.ttf',bold:'NotoSansSC-VF.ttf',italics:'NotoSansSC-VF.ttf',bolditalics:'NotoSansSC-VF.ttf'}});
  })().catch(error=>{vectorPdfReady=null;throw error;});
  await job.wait(vectorPdfReady);
 }
 function vectorPdfDefinition(body,attachments=[]){
  for(const element of body.querySelectorAll('img,svg')){const box=element.getBoundingClientRect();if(box.width&&box.height){element.style.setProperty('width',box.width+'px','important');element.style.setProperty('height',box.height+'px','important');}}
  for(const element of body.querySelectorAll('*')){const style=element.ownerDocument.defaultView.getComputedStyle(element);for(const [source,target] of [['color','color'],['backgroundColor','background-color'],['fontSize','font-size'],['fontWeight','font-weight'],['fontStyle','font-style'],['textAlign','text-align'],['textDecoration','text-decoration'],['verticalAlign','vertical-align'],['whiteSpace','white-space'],['border','border'],['borderTop','border-top'],['borderRight','border-right'],['borderBottom','border-bottom'],['borderLeft','border-left'],['padding','padding']]){const value=style[source];if(value&&value!=='none'&&value!=='normal'&&value!=='rgba(0, 0, 0, 0)')element.style.setProperty(target,value);}}
  const converted=window.htmlToPdfmake(body.innerHTML,{window:body.ownerDocument.defaultView,tableAutoSize:true,defaultStyles:{h1:{fontSize:24,bold:true,margin:[0,18,0,10]},h2:{fontSize:20,bold:true,margin:[0,16,0,8]},h3:{fontSize:16,bold:true,margin:[0,12,0,6]},h4:{fontSize:13,bold:true,margin:[0,10,0,5]},h5:{fontSize:11,bold:true,margin:[0,8,0,4]},h6:{fontSize:10,bold:true,margin:[0,8,0,4]},p:{margin:[0,0,0,8]},pre:{fontSize:8,margin:[0,6,0,10],fillColor:'#f3f5f7',border:[true,true,true,true],borderColor:'#718096',padding:8},blockquote:{color:'#46505e',margin:[12,0,12,0],italics:true},th:{bold:true,fillColor:'#edf1f5',margin:[5,4,5,4]},td:{margin:[5,4,5,4]},table:{margin:[0,8,0,10]},a:{color:'#355f86',decoration:'underline'}}});
  const content=Array.isArray(converted)?converted:converted?.content||[converted];
  const tables=[...body.querySelectorAll('table')];let tableIndex=0;const pending=[content];while(pending.length){const value=pending.pop();if(!value||typeof value!=='object')continue;if(Array.isArray(value)){for(let i=value.length-1;i>=0;i--)pending.push(value[i]);continue;}if(value.table?.body){const source=tables[tableIndex++],rows=[...(source?.rows||[])],first=rows[0],cells=first?[...first.cells]:[],count=Math.max(0,...rows.map(row=>[...row.cells].reduce((sum,cell)=>sum+(cell.colSpan||1),0)));if(cells.length&&count){const widths=Array(count).fill(0);let column=0;for(const cell of cells){const span=cell.colSpan||1,each=cell.getBoundingClientRect().width/span;for(let i=0;i<span&&column<count;i++)widths[column++]=each;}for(;column<count;column++)widths[column]=widths[column-1]||1;const total=widths.reduce((sum,width)=>sum+width,0)||1;value.table.widths=widths.map(width=>(width/total*100).toFixed(3)+'%');}const head=source?.querySelectorAll('thead tr').length||0;if(head)value.table.headerRows=head;}const children=Object.values(value);for(let i=children.length-1;i>=0;i--)if(children[i]&&typeof children[i]==='object')pending.push(children[i]);}
  if(attachments.length){content.push({text:'附件',fontSize:18,bold:true,margin:[0,18,0,8]});for(const item of attachments)content.push({columns:[{text:item.name,width:'*',fontSize:10,id:'aide-attachment-'+item.index},{attachment:{src:item.dataUrl,name:item.name,description:item.name},icon:'Paperclip',width:24}],columnGap:8,margin:[0,0,0,6]});}
  const landscape=[...body.querySelectorAll('table')].some(table=>[...table.rows].some(row=>[...row.cells].reduce((sum,cell)=>sum+(cell.colSpan||1),0)>5));
  return{pageSize:'A4',pageOrientation:landscape?'landscape':'portrait',pageMargins:[42,42,42,42],defaultStyle:{font:'NotoSansSC',fontSize:10,lineHeight:1.25,color:'#202630'},styles:{'html-h1':{fontSize:24,bold:true},'html-h2':{fontSize:20,bold:true},'html-h3':{fontSize:16,bold:true},'html-pre':{fontSize:8},'html-th':{bold:true}},content};
 }
 async function renderVectorChunk(html,o,job,attachments=[]){const body=await snapshot(o,job,html);return job.wait(window.pdfMake.createPdf(vectorPdfDefinition(body,attachments)).getBlob());}
 async function mergeVectorPdf(parts,last,stem,job){
  const {PDFDocument}=window.PDFLib,target=await job.wait(PDFDocument.load(await job.wait(last.arrayBuffer())));let index=0;
  for(const bytes of parts){if(job.controller.signal.aborted)throw new DOMException('Cancelled','AbortError');const source=await job.wait(PDFDocument.load(bytes)),pages=await job.wait(target.copyPages(source,source.getPageIndices()));for(const page of pages)target.insertPage(index++,page);}
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
    const prior=[],imageBudget={total:0};let pending=null,chunk=0,last=null;
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
