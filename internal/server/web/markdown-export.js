/* Printable Markdown snapshot. No document is uploaded to an external service. */
(() => {
 'use strict';
 let active=null;
 const css=`@page{size:A4;margin:18mm}*{box-sizing:border-box}body{margin:0;color:#202630;background:white;font:11pt/1.7 -apple-system,BlinkMacSystemFont,"Segoe UI","Microsoft YaHei","PingFang SC",sans-serif}h1,h2,h3,h4{line-height:1.3;break-after:avoid}h1{font-size:25pt}h2{font-size:19pt}h3{font-size:14pt}p,li{orphans:3;widows:3}img,svg{max-width:100%;height:auto;break-inside:avoid}pre{white-space:pre-wrap;overflow-wrap:anywhere;font:9pt/1.5 ui-monospace,Consolas,monospace;padding:10pt;background:#f3f5f7;border:1px solid #dde2e8;border-radius:4pt}code{overflow-wrap:anywhere}table{border-collapse:collapse;width:100%;font-size:10pt}thead{display:table-header-group}tr{break-inside:avoid}th,td{border:1px solid #cdd4dd;padding:6pt;text-align:left;overflow-wrap:anywhere}blockquote{margin:12pt 0;padding-left:12pt;border-left:3pt solid #8295ab;color:#46505e}a{color:#355f86;text-decoration:none}hr{border:0;border-top:1px solid #cdd4dd}button,nav,.md-outline{display:none!important}`;
 function close(){active?.remove();active=null;}
 window.LockCluster?.on('effective',value=>{if(value)close();});window.addEventListener('pagehide',close);
 async function exportPDF({html,title,valid=()=>true,renderDiagrams,print}){
  if(!valid())return;close();
  const frame=document.createElement('iframe');frame.className='markdown-print-frame';frame.setAttribute('aria-hidden','true');frame.tabIndex=-1;
  document.body.append(frame);active=frame;
  const alive=()=>active===frame&&frame.isConnected&&valid();
  try{
   const doc=frame.contentDocument;doc.open();doc.write('<!doctype html><html><head><meta charset="utf-8"></head><body></body></html>');doc.close();doc.title=title.replace(/\.(md|markdown)$/i,'');
   const style=doc.createElement('style');style.textContent=css;doc.head.append(style);
   const body=doc.createElement('main');body.innerHTML=html;
   body.querySelectorAll('script,iframe,object,embed,form,input,button,textarea,select').forEach(e=>e.remove());
   for(const e of body.querySelectorAll('*'))for(const a of [...e.attributes])if(/^on/i.test(a.name)||['srcdoc','style','srcset'].includes(a.name))e.removeAttribute(a.name);
   body.querySelectorAll('a').forEach(a=>{const href=a.getAttribute('href')||'';if(!/^(https?:|mailto:|#)/i.test(href)||href.includes('access_token='))a.removeAttribute('href');});
   doc.body.append(body);
   if(renderDiagrams)await renderDiagrams(body);
   const images=[...body.querySelectorAll('img')];
   await Promise.all(images.map(image=>new Promise((resolve,reject)=>{image.loading='eager';const timer=setTimeout(()=>{clean();reject(Error('图片读取超时，请检查引用后重试'));},15000);function clean(){clearTimeout(timer);image.removeEventListener('load',loaded);image.removeEventListener('error',failed);}function loaded(){clean();resolve();}function failed(){clean();reject(Error('图片读取失败，请检查引用后重试'));}if(image.complete){image.naturalWidth?loaded():failed();return;}image.addEventListener('load',loaded,{once:true});image.addEventListener('error',failed,{once:true});})));
   await doc.fonts?.ready;
   if(!alive())return;
   // Embedded image URLs are print resources, not navigable credential-bearing links.
   frame.contentWindow.addEventListener('afterprint',()=>{if(active===frame)close();},{once:true});
   (print||(()=>frame.contentWindow.print()))(frame.contentWindow);
  }catch(e){if(active===frame)close();throw e;}
 }
 window.AideMarkdownExport={exportPDF,close};
})();
