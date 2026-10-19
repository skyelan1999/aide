'use strict';
// Keep the map in its tab and own one reusable, read-only preview tab.
// A changed query forces document navigation:
// the existing file viewer intentionally ignores file-to-file hash changes.
window.AideStarmapPreview={create({origin,workspace,locked,openWindow,note,key}){
 let viewer=null,sequence=0,pending=false;
 const owns=()=>{
  try{
   if(!viewer||viewer.closed)return false;
   if(pending&&viewer.location.href==='about:blank')return true;
   pending=false;const url=new URL(viewer.location.href);
   const spec=JSON.parse(new URLSearchParams(url.hash.slice(1)).get('file')||'null')||viewer.history?.state?.aideFileRoute;
   return url.origin===origin&&url.pathname==='/'&&url.searchParams.get('starmapPreview')===key&&spec?.readOnly===true;
  }
  catch{return false;}
 };
 return {
  open(href){
   if(locked())return false;
   let url,spec;
   try{url=new URL(href,origin);spec=JSON.parse(new URLSearchParams(url.hash.slice(1)).get('file')||'null');}catch{note('文件定位无效，请重新选择节点。');return false;}
   if(url.origin!==origin||url.pathname!=='/'||spec?.readOnly!==true||typeof spec.path!=='string'||!spec.path||typeof spec.origin!=='string'||!spec.origin||spec.knowledgeWorkspace!==workspace()){note('文件来源已变化或缺少定位信息，请刷新星图。');return false;}
   url.search='';url.searchParams.set('starmapPreview',key);url.searchParams.set('previewRequest',String(++sequence));
   try{
    if(owns()){viewer.location.replace(url.href);viewer.focus();}
    else{viewer=openWindow(url.href);pending=true;if(!viewer){note('请允许打开文件预览选项卡后重试。');return false;}try{viewer.opener=null;}catch{}}
   }catch{viewer=null;note('文件预览未能打开，请重试。');return false;}
   note('已在新选项卡打开只读文件预览；后续文件复用此预览选项卡。');return true;
  },
  close(){try{if(owns())viewer.close();}catch{}finally{viewer=null;}}
 };
}};
