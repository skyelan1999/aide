/* Local, per-document PDF appearance. HTML is read in an inert template. */
(() => {
 'use strict';

 const PREFIX='aide.markdown-pdf.appearance.v1:';
 const MAX_HTML=4096;
 const MAX_LOGO=512*1024;
 const DROP_TAGS=new Set(['SCRIPT','STYLE','TEMPLATE','IFRAME','OBJECT','EMBED','SVG','MATH','AUDIO','VIDEO','PICTURE','IMG','INPUT','TEXTAREA','SELECT','BUTTON']);
 const BLOCK_TAGS=new Set(['P','DIV','H1','H2','H3','H4','H5','H6']);
 let activeDialog=null;
 const logoSide=value=>value==='left'?'left':'right';

 function colorValue(value){
  const hex=String(value||'').trim().toLowerCase();
  if(/^#[0-9a-f]{6}$/.test(hex))return hex;
  return /^#[0-9a-f]{3}$/.test(hex)?'#'+Array.from(hex.slice(1),n=>n+n).join(''):'';
 }

 function headerContent(html,doc){
  const template=doc.createElement('template');
  template.innerHTML=html;
  const rows=[[]];
  const finishRow=()=>{if(rows.at(-1).length)rows.push([]);};
  const append=(raw,format)=>{
   const row=rows.at(-1);
   let text=raw.replace(/\s+/g,' ');
   if(!row.length||row.at(-1).text.endsWith(' '))text=text.trimStart();
   if(text)row.push({text,...format});
  };
  // Iterative traversal also handles pasted deeply nested HTML without
  // recursing into a user-controlled document tree.
  const pending=Array.from(template.content.childNodes,node=>({node,format:{}})).reverse();
  while(pending.length){
   const {node,format}=pending.pop();
   if(!node){finishRow();continue;}
   if(node.nodeType===3){append(node.data,format);continue;}
   if(node.nodeType!==1)continue;
   const tag=node.tagName.toUpperCase();
   if(DROP_TAGS.has(tag))continue;
   if(tag==='BR'){finishRow();continue;}
   const block=BLOCK_TAGS.has(tag);
   if(block){
    finishRow();
    pending.push({node:null});
   }
   const next={...format};
   if(tag==='B'||tag==='STRONG'||/^H[1-6]$/.test(tag))next.bold=true;
   if(tag==='EM'||tag==='I')next.italics=true;
   if(tag==='U')next.decoration='underline';
   if(tag==='S'||tag==='DEL')next.decoration='lineThrough';
   const color=colorValue(/(?:^|;)\s*color\s*:\s*(#[\da-f]+)\s*(?:;|$)/i.exec(node.getAttribute('style')||'')?.[1]);
   if(color)next.color=color;
   for(let i=node.childNodes.length-1;i>=0;i--)pending.push({node:node.childNodes[i],format:next});
  }
  return rows.filter(row=>row.some(run=>run.text.trim()));
 }

 function compile(profile,doc=document){
  const rows=headerContent(profile.header||'',doc);
  // Forty-two glyphs is a conservative line width at 9 pt in A4 with a logo.
  // Reserve room for line wraps before page layout and keep the body below it.
  const lineCount=rows.reduce((total,row)=>total+Math.ceil(Array.from(row.map(run=>run.text).join('').trim()).length/42),0);
  return {color:profile.color,logo:profile.logo,logoSide:logoSide(profile.logoSide),rows,lineCount};
 }

 function read(key){
  if(!key)return {};
  try{
   const saved=JSON.parse(localStorage.getItem(PREFIX+key)||'{}');
   if(!saved||typeof saved!=='object')return {};
   return {
    color:colorValue(saved.color),
    header:typeof saved.header==='string'&&saved.header.length<=MAX_HTML?saved.header:'',
    logo:typeof saved.logo==='string'&&saved.logo.length<=MAX_LOGO&&/^data:image\/png;base64,[A-Za-z0-9+/]+=*$/.test(saved.logo)?saved.logo:'',
    logoSide:logoSide(saved.logoSide)
   };
  }catch{return {};}
 }

 async function loadLogo(file){
  if(!/^image\/(png|jpeg|webp)$/.test(file.type)||file.size>8*1024*1024)throw new Error('请选择 8 MiB 以内的 PNG、JPEG 或 WebP 图片。');
  const url=URL.createObjectURL(file);
  try{
   const image=new Image();
   image.src=url;
   try{await image.decode();}catch{throw new Error('无法读取 Logo 图片。');}
   if(!image.naturalWidth||!image.naturalHeight||image.naturalWidth*image.naturalHeight>20000000)throw new Error('Logo 图片尺寸过大。');
   const scale=Math.min(1,600/image.naturalWidth,240/image.naturalHeight);
   const canvas=document.createElement('canvas');
   canvas.width=Math.max(1,Math.round(image.naturalWidth*scale));
   canvas.height=Math.max(1,Math.round(image.naturalHeight*scale));
   const context=canvas.getContext('2d');
   context.drawImage(image,0,0,canvas.width,canvas.height);
   const data=canvas.toDataURL('image/png');
   canvas.width=canvas.height=0;
   if(data.length>MAX_LOGO)throw new Error('Logo 文件过大，请选用较小的图片。');
   return data;
  }finally{URL.revokeObjectURL(url);}
 }

 function close(){activeDialog?.close();}

 function open({key,valid,button,t}){
  close();
  if(!key||!valid())return;
  const saved=read(key);
  let logo=saved.logo||'';
  let logoRequest=0;
  const make=(tag,text)=>{
   const node=document.createElement(tag);
   if(text)node.textContent=t(text);
   return node;
  };
  const action=(label,cssClass='quiet')=>{
   const item=make('button',label);
   item.type='button';item.className=cssClass;
   return item;
  };
  const dialog=make('dialog');
  dialog.className='dialog markdown-pdf-appearance';
  dialog.setAttribute('aria-label',t('PDF 版式设置'));
  const title=make('h2','PDF 版式设置');
  const form=make('form');
  const color=make('input');color.type='text';color.maxLength=7;color.placeholder='#24598A';color.value=saved.color||'';color.spellcheck=false;
  const colorLabel=make('label','文档主题色');colorLabel.append(color);
  const picker=make('input');picker.type='color';picker.value=saved.color||'#24598a';
  picker.setAttribute('aria-label',t('选择主题色'));
  const colorField=make('div');colorField.className='markdown-pdf-color';colorField.append(colorLabel,picker);
  const header=make('textarea');header.value=saved.header||'';header.maxLength=MAX_HTML;header.rows=4;header.spellcheck=false;
  const headerLabel=make('label','HTML 页眉');headerLabel.append(header);
  const help=make('p','可用段落、换行、加粗、下划线、删除线和文字颜色。内容留空时不生成页眉。');
  help.className='muted';
  const file=make('input');file.type='file';file.accept='image/png,image/jpeg,image/webp';
  const logoLabel=make('label','页眉 Logo');logoLabel.append(file);
  const remove=action('移除 Logo');
  const logoField=make('div');logoField.className='markdown-pdf-logo-setting';logoField.append(logoLabel,remove);
  const position=make('select');
  position.setAttribute('aria-label',t('Logo 位置'));
  for(const [value,label] of [['left','左上角'],['right','右上角']]){
   const option=make('option',label);option.value=value;position.append(option);
  }
  position.value=logoSide(saved.logoSide);
  const positionLabel=make('label','Logo 位置');positionLabel.append(position);
  const preview=make('div');preview.className='markdown-pdf-header-preview';
  preview.setAttribute('aria-label',t('页眉预览'));
  const error=make('p');error.className='task-error';error.setAttribute('role','status');
  const buttons=make('div');buttons.className='markdown-pdf-appearance-actions';
  const cancel=action('取消');
  const save=action('保存','primary');save.type='submit';
  buttons.append(cancel,save);
  form.append(colorField,headerLabel,help,logoField,positionLabel,preview,error,buttons);
  dialog.append(title,form);
  document.body.append(dialog);

  function refresh(){
   preview.replaceChildren();
   const colorHex=colorValue(color.value);
   if(colorHex)picker.value=colorHex;
   const content=compile({header:header.value,color:colorHex,logo});
   const text=make('div');text.style.color=colorHex||'';
   for(const row of content.rows){
    const line=make('div');
    for(const run of row){
     const span=make('span');
     span.textContent=run.text;
     if(run.bold)span.style.fontWeight='bold';
     if(run.italics)span.style.fontStyle='italic';
     if(run.decoration)span.style.textDecoration=run.decoration==='lineThrough'?'line-through':'underline';
     if(run.color)span.style.color=run.color;
     line.append(span);
    }
    text.append(line);
   }
   preview.append(text);
   if(logo){
    const image=make('img');image.alt=t('页眉 Logo');image.src=logo;
    if(position.value==='left')preview.prepend(image);else preview.append(image);
   }
   if(!logo&&!content.rows.length)text.textContent=t('页眉为空');
   remove.hidden=!logo;
   position.disabled=!logo;
  }

  color.oninput=header.oninput=()=>{error.textContent='';refresh();};
  picker.oninput=()=>{color.value=picker.value;color.oninput();};
  position.onchange=refresh;
  file.onchange=async()=>{
   const chosen=file.files?.[0];if(!chosen)return;
   const request=++logoRequest;
   save.disabled=true;error.textContent='';
   try{
    const data=await loadLogo(chosen);
    if(dialog.open&&valid()&&request===logoRequest){logo=data;refresh();}
   }catch(cause){
    if(dialog.open&&request===logoRequest)error.textContent=t(cause.message);
   }finally{
    if(request===logoRequest){file.value='';save.disabled=false;}
   }
  };
  remove.onclick=()=>{logoRequest++;save.disabled=false;file.value='';logo='';refresh();};
  cancel.onclick=()=>dialog.close();
  form.onsubmit=event=>{
   event.preventDefault();
   if(save.disabled)return;
   if(!valid()){dialog.close();return;}
   const theme=colorValue(color.value);
   if(color.value.trim()&&!theme){error.textContent=t('请输入有效的十六进制主题色。');color.focus();return;}
   const profile={color:theme,header:header.value.trim(),logo,logoSide:logoSide(position.value)};
   const content=compile(profile);
   if(content.lineCount>4){error.textContent=t('页眉超过 4 行，请缩短内容。');header.focus();return;}
   if(!content.rows.length)profile.header='';
   try{
    if(profile.color||profile.header||profile.logo)localStorage.setItem(PREFIX+key,JSON.stringify(profile));
    else localStorage.removeItem(PREFIX+key);
   }catch{error.textContent=t('无法保存 PDF 版式设置，请检查浏览器存储空间。');return;}
   dialog.close();
  };
  dialog.addEventListener('close',()=>{
   if(activeDialog===dialog)activeDialog=null;
   dialog.remove();
   if(button.isConnected)button.focus();
  },{once:true});
  activeDialog=dialog;
  refresh();
  dialog.showModal();
 }

 window.AideMarkdownPdfAppearance={close,open,read,compile};
})();
