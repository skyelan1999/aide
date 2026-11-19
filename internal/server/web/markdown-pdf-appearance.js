/* A sanitized, document-local HTML layer and a separate PDF logo layer. */
(() => {
 'use strict';

 const PREFIX='aide.markdown-pdf.appearance.v1:';
 const MAX_HTML=64*1024;
 const MAX_LOGO=512*1024;
 const HTML_NS='http://www.w3.org/1999/xhtml';
 const TAGS=new Set('DIV SPAN SECTION ARTICLE HEADER P H1 H2 H3 H4 H5 H6 STRONG B EM I U S DEL SMALL SUB SUP BR HR UL OL LI DL DT DD BLOCKQUOTE CODE PRE TABLE CAPTION COLGROUP COL THEAD TBODY TFOOT TR TH TD'.split(' '));
 const DROP_TAGS=new Set('SCRIPT STYLE TEMPLATE IFRAME OBJECT EMBED LINK META BASE SVG MATH AUDIO VIDEO PICTURE IMG INPUT TEXTAREA SELECT BUTTON'.split(' '));
 const PAINT=new Set(('color background-color font font-size font-family font-weight font-style line-height letter-spacing '+
  'text-align text-transform text-decoration-line text-decoration-color text-decoration-style text-decoration-thickness text-indent vertical-align white-space overflow-wrap word-break '+
  'display flex flex-direction flex-wrap align-items justify-content gap row-gap column-gap grid-template-columns '+
  'width min-width max-width height min-height max-height margin margin-top margin-right margin-bottom margin-left '+
  'padding padding-top padding-right padding-bottom padding-left '+
  'border-top-color border-right-color border-bottom-color border-left-color '+
  'border-top-width border-right-width border-bottom-width border-left-width '+
  'border-top-style border-right-style border-bottom-style border-left-style '+
  'border-top-left-radius border-top-right-radius border-bottom-left-radius border-bottom-right-radius '+
  'border-collapse border-spacing table-layout list-style-type').split(' '));
 const PAGE={width:595.28,height:841.89};
 const MARGIN=48, TOP=18, GAP=14, PT_PER_PX=0.75, PT_PER_MM=72/25.4, LOGO_WIDTH=76, LOGO_HEIGHT=34;
 let activeDialog=null;

 const logoSide=value=>value==='left'||value==='custom'?value:'right';
 function coordinate(value,fallback){
  if(value===''||value==null)return fallback;
  const number=Number(value);
  return Number.isFinite(number)?Math.min(297,Math.max(0,number)):fallback;
 }
 function colorValue(value){
  const hex=String(value||'').trim().toLowerCase();
  if(/^#[0-9a-f]{6}$/.test(hex))return hex;
  return /^#[0-9a-f]{3}$/.test(hex)?'#'+Array.from(hex.slice(1),n=>n+n).join(''):'';
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
    toc:saved.toc===true,logoSide:logoSide(saved.logoSide),logoX:coordinate(saved.logoX,17),logoY:coordinate(saved.logoY,6.35)
   };
  }catch{return {};}
 }
 function buildHtml(doc,profile){
  if(!profile.header?.trim())return null;
  const template=doc.createElement('template');
  template.innerHTML=profile.header;
  const surface=doc.createElement('div');
  surface.className='markdown-pdf-rich-html';
  if(profile.color)surface.style.color=profile.color;
  const paint=doc.createElement('span').style;
  const pending=Array.from(template.content.childNodes,node=>({node,parent:surface})).reverse();
  // Only reconstructed HTML elements and these paint properties ever enter
  // the workbench. URL attributes, network paints and page rules stay inert.
  while(pending.length){
   const {node,parent}=pending.pop();
   if(node.nodeType===3){parent.append(doc.createTextNode(node.data));continue;}
   if(node.nodeType!==1||node.namespaceURI!==HTML_NS||DROP_TAGS.has(node.tagName))continue;
   const tag=node.tagName;
   let target=parent;
   if(TAGS.has(tag)){
    target=doc.createElement(tag.toLowerCase());
    if(tag==='TD'||tag==='TH'){
     target.colSpan=Math.min(node.colSpan,36);
     target.rowSpan=Math.min(node.rowSpan||1,200);
    }
    if(tag==='COL'||tag==='COLGROUP')target.span=Math.min(node.span,36);
    if(tag==='OL'&&node.hasAttribute('start'))target.start=node.start;
    const align=node.getAttribute('align');
    if(/^(left|center|right|justify)$/.test(align||''))target.style.textAlign=align;
    // The workbench CSP blocks style attributes. Parse them on a
    // detached node and apply approved paint properties through CSSOM.
    paint.cssText=node.getAttribute('style')||'';
    for(const property of Array.from(paint)){
     const value=paint.getPropertyValue(property);
     if(PAINT.has(property)&&!/(?:url\s*\(|image(?:-set)?\s*\(|var\s*\(|\\)/i.test(value)){
      target.style.setProperty(property,value);
     }
    }
    parent.append(target);
   }
   for(let i=node.childNodes.length-1;i>=0;i--)pending.push({node:node.childNodes[i],parent:target});
  }
  return surface.textContent.trim()||surface.children.length?surface:null;
 }
 function logoSize(logo){
  if(!logo)return null;
  try{
   const start=logo.indexOf(',')+1;
   const bytes=Uint8Array.from(atob(logo.slice(start,start+32)),c=>c.charCodeAt(0));
   if(bytes.length!==24||bytes[0]!==137||bytes[1]!==80||bytes[2]!==78||bytes[3]!==71)return null;
   const data=new DataView(bytes.buffer);
   const width=data.getUint32(16),height=data.getUint32(20);
   if(!width||!height)return null;
   const fit=Math.min(LOGO_WIDTH/width,LOGO_HEIGHT/height);
   return {width:width*fit,height:height*fit};
  }catch{return null;}
 }
 function pageGeometry(landscape){
  return landscape?{width:PAGE.height,height:PAGE.width}:PAGE;
 }
 function layout(profile,page,htmlHeightPx,htmlWidthPx=(page.width-2*MARGIN)/PT_PER_PX){
  const lowerLimit=page.height/2-GAP;
  const fullHeight=htmlHeightPx*PT_PER_PX;
  const fullWidth=htmlWidthPx*PT_PER_PX;
  const fit=fullHeight?Math.min(1,(lowerLimit-TOP)/fullHeight,(page.width-2*MARGIN)/fullWidth):1;
  const html=fullHeight?{x:MARGIN,y:TOP,width:fullWidth*fit,height:fullHeight*fit,fit}:null;
  const dimensions=logoSize(profile.logo);
  let logo=null;
  if(dimensions){
   const custom=profile.logoSide==='custom';
   const x=custom?coordinate(profile.logoX,17)*PT_PER_MM:
    profile.logoSide==='left'?MARGIN:page.width-MARGIN-dimensions.width;
   const y=custom?coordinate(profile.logoY,6.35)*PT_PER_MM:TOP;
   logo={...dimensions,x:Math.min(x,page.width-dimensions.width),y:Math.min(y,lowerLimit-dimensions.height)};
  }
  const bottom=Math.max(html?html.y+html.height:0,logo?logo.y+logo.height:0);
  return {html,logo,topMargin:bottom?Math.max(MARGIN,bottom+GAP):MARGIN};
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
  let logo=saved.logo||'',logoRequest=0;
  const make=(tag,text,className)=>{
   const node=document.createElement(tag);
   if(text)node.textContent=t(text);
   if(className)node.className=className;
   return node;
  };
  const action=(label,className='quiet')=>{
   const item=make('button',label,className);item.type='button';return item;
  };
  const label=(name,control)=>{
   const wrap=make('label',name);
   control.setAttribute('aria-label',t(name));wrap.append(control);return wrap;
  };
  const select=(options,value)=>{
   const control=make('select');
   for(const [key,name] of options){const option=make('option',name);option.value=key;control.append(option);}
   control.value=value;return control;
  };
  const number=value=>{
   const input=make('input');input.type='number';input.min='0';input.max='297';input.step='any';input.value=String(value);
   return input;
  };

  const dialog=make('dialog',null,'dialog markdown-pdf-appearance');
  dialog.setAttribute('aria-label',t('PDF 版式设置'));
  const heading=make('header',null,'markdown-pdf-appearance-heading');
  const copy=action('复制 AI 提示词');
  heading.append(make('h2','PDF 版式设置'),copy);

  const form=make('form');
  const columns=make('div',null,'markdown-pdf-appearance-columns');
  const editor=make('section',null,'markdown-pdf-appearance-editor');
  const previewPanel=make('section',null,'markdown-pdf-appearance-preview');
  columns.append(editor,previewPanel);

  const color=make('input');color.type='text';color.maxLength=7;color.placeholder='#24598A';color.value=saved.color||'';color.spellcheck=false;
  const picker=make('input');picker.type='color';picker.value=saved.color||'#24598a';
  picker.setAttribute('aria-label',t('选择主题色'));
  const colorField=make('div',null,'markdown-pdf-color');colorField.append(label('文档主题色',color),picker);
  const header=make('textarea');header.value=saved.header||'';header.maxLength=MAX_HTML;header.rows=11;header.spellcheck=false;
  const help=make('p','支持 HTML 表格和内联 CSS。页眉过高时会等比缩放，为正文留出空间。','muted');
  const toc=make('input');toc.type='checkbox';toc.checked=saved.toc===true;
  const tocField=make('label',null,'markdown-pdf-toc-setting');
  tocField.append(toc,make('span','按标题生成目录'));
  editor.append(colorField,label('HTML 页眉',header),help,tocField);

  const file=make('input');file.type='file';file.accept='image/png,image/jpeg,image/webp';
  file.hidden=true;file.setAttribute('aria-label',t('页眉 Logo'));
  const choose=action('选择 Logo');
  const remove=action('移除 Logo');
  const logoField=make('div',null,'markdown-pdf-logo-setting');
  const logoActions=make('div',null,'markdown-pdf-logo-actions');
  logoActions.append(choose,remove);
  logoField.append(make('div','页眉 Logo'),logoActions,file);
  choose.onclick=()=>file.click();
  const position=select([['left','左上角'],['right','右上角'],['custom','自定义坐标']],logoSide(saved.logoSide));
  const x=number(coordinate(saved.logoX,17)),y=number(coordinate(saved.logoY,6.35));
  const coords=make('div',null,'markdown-pdf-logo-coordinates');
  coords.append(label('X（毫米）',x),label('Y（毫米）',y));
  const tip=make('p','以纸张左上角为原点。超出页眉区域的 Logo 会贴边显示。','muted');
  const previewOrientation=select([['portrait','A4 竖版'],['landscape','A4 横版']],'portrait');
  const previewHeading=make('div',null,'markdown-pdf-preview-heading');
  previewOrientation.setAttribute('aria-label',t('预览页面'));
  previewHeading.append(make('strong','页眉预览'),previewOrientation);
  const preview=make('div',null,'markdown-pdf-header-preview');
  const paper=make('div',null,'markdown-pdf-preview-paper');
  preview.append(paper);
  previewPanel.append(logoField,label('Logo 位置',position),coords,tip,previewHeading,preview);
  const error=make('p',null,'task-error');error.setAttribute('role','status');
  const status=make('p',null,'muted');status.setAttribute('role','status');
  const buttons=make('div',null,'markdown-pdf-appearance-actions');
  const cancel=action('取消');
  const save=action('保存','primary');save.type='submit';
  buttons.append(status,cancel,save);
  form.append(columns,error,buttons);
  dialog.append(heading,form);
  document.body.append(dialog);

  const current=()=>({header:header.value,color:colorValue(color.value),logo,logoSide:position.value,logoX:x.value,logoY:y.value,toc:toc.checked});
  let previewFrame=0;
  function refresh(){
   cancelAnimationFrame(previewFrame);
   previewFrame=requestAnimationFrame(paint);
  }
  function paint(){
   if(!dialog.open)return;
   const profile=current();
   if(profile.color)picker.value=profile.color;
   position.disabled=!logo;remove.hidden=!logo;
   choose.textContent=t(logo?'更换 Logo':'选择 Logo');
   const custom=!!logo&&position.value==='custom';
   coords.hidden=tip.hidden=!custom;
   x.disabled=y.disabled=!custom;

   const page=pageGeometry(previewOrientation.value==='landscape');
   paper.style.width=page.width/PT_PER_PX+'px';
   paper.style.zoom='1';
   paper.replaceChildren();
   const html=buildHtml(document,profile);
   const baseWidth=(page.width-2*MARGIN)/PT_PER_PX;
   if(html){
    html.style.width=baseWidth+'px';
    html.style.left=MARGIN/PT_PER_PX+'px';
    html.style.top=TOP/PT_PER_PX+'px';
    paper.append(html);
   }
   const arrangement=layout(profile,page,html?.scrollHeight||0,html?.scrollWidth);
   if(arrangement.html)html.style.transform='scale('+arrangement.html.fit+')';
   if(arrangement.logo){
    const img=make('img',null,'markdown-pdf-preview-logo');
    img.alt=t('页眉 Logo');img.src=logo;
    for(const [property,key] of [['left','x'],['top','y'],['width','width'],['height','height']])img.style[property]=arrangement.logo[key]/PT_PER_PX+'px';
    paper.append(img);
   }
   if(!arrangement.html&&!arrangement.logo)paper.append(make('p','页眉为空','markdown-pdf-empty-preview'));
   paper.style.height=(arrangement.topMargin+25)/PT_PER_PX+'px';
   paper.style.zoom=String(Math.min(1,Math.max(0.1,(preview.clientWidth-2)/(page.width/PT_PER_PX))));
  }

  copy.onclick=async()=>{
   const guide=t('请生成可直接粘贴到 Aide 的 HTML PDF 页眉。使用 HTML 片段和内联 CSS，优先用表格排版。包含机构名称、文档编号和日期。Logo 由 Aide 单独叠放，请在需要的位置留白。只输出 HTML，不要添加 Markdown 代码围栏、脚本、外链或图片标签。');
   try{
    await navigator.clipboard.writeText(guide);
    if(dialog.open)status.textContent=t('已复制');
   }catch{
    if(!dialog.open)return;
    const fallback=make('textarea');fallback.readOnly=true;fallback.value=guide;
    status.replaceChildren(fallback);fallback.select();
    try{if(document.execCommand('copy')){status.textContent=t('已复制');return;}}catch{}
    status.prepend(t('无法复制，请选中提示词手动复制。'));
   }
  };
  const observer=new ResizeObserver(refresh);
  observer.observe(previewPanel);
  color.oninput=header.oninput=x.oninput=y.oninput=()=>{error.textContent='';refresh();};
  picker.oninput=()=>{color.value=picker.value;color.oninput();};
  position.onchange=previewOrientation.onchange=refresh;
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
   const profile=current();
   profile.header=profile.header.trim();
   profile.logoX=coordinate(profile.logoX,17);
   profile.logoY=coordinate(profile.logoY,6.35);
   if(color.value.trim()&&!profile.color){error.textContent=t('请输入有效的十六进制主题色。');color.focus();return;}
   try{
    if(profile.color||profile.header||profile.logo||profile.toc)localStorage.setItem(PREFIX+key,JSON.stringify(profile));
    else localStorage.removeItem(PREFIX+key);
   }catch{error.textContent=t('无法保存 PDF 版式设置，请检查浏览器存储空间。');return;}
   dialog.close();
  };
  dialog.addEventListener('close',()=>{
   cancelAnimationFrame(previewFrame);observer.disconnect();
   if(activeDialog===dialog)activeDialog=null;
   dialog.remove();
   if(button.isConnected)button.focus();
  },{once:true});
  activeDialog=dialog;
  dialog.showModal();
  refresh();
 }

 window.AideMarkdownPdfAppearance={close,open,read,buildHtml,layout,pageGeometry,MARGIN,PT_PER_PX};
})();
