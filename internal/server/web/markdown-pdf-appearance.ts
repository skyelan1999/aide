interface PdfAppearanceOpenOptions {
  key: string;
  valid: () => boolean;
  button: HTMLButtonElement;
  t: (key: string) => string;
  title: string;
  headings?: { level: number; title: string; key: string }[];
  download: (blob: Blob, filename: string) => void;
}
interface Window {
  AideMarkdownPdfAppearance?: {
    close: () => void;
    open: (options: PdfAppearanceOpenOptions) => void;
    read: (key: string) => any;
    buildHtml: (doc: Document, profile: any, region?: string, context?: any) => HTMLElement | null;
    layout: (profile: any, page: { width: number; height: number }, htmlHeightPx: number, htmlWidthPx?: number, options?: any) => any;
    coverLayout: (page: { width: number; height: number }, topMargin: number, bottomMargin: number, heightPx: number, widthPx: number) => any;
    pageGeometry: (landscape: boolean) => { width: number; height: number };
    relativeBox: (element: Element | null | undefined, root: Element | null | undefined) => { x: number; y: number; width: number; height: number } | null;
    pdfValue: (source: string, page: any, pages: any) => string;
    templateValue: (field: string, context: any) => string;
    COVER_TEMPLATE: string;
    MARGIN: number;
    PT_PER_PX: number;
    test?: any;
  };
  __AIDE_MARKDOWN_EXPORT_TEST__?: boolean;
}
/* A sanitized, document-local HTML layer and a separate PDF logo layer. */
(() => {
 'use strict';

 const PREFIX='aide.markdown-pdf.appearance.v1:';
 const CONFIG_FORMAT='aide.markdown-pdf-layout', CONFIG_VERSION=1;
 // Fits every supported profile, even when all text requires JSON escaping.
 const MAX_CONFIG_BYTES=5*1024*1024;
 const MAX_HTML=64*1024;
 const MAX_LOGO=512*1024;
 const MAX_FIELDS=128, MAX_FIELD_LENGTH=4096;
 const FIELD_NAME=/^[A-Za-z_][A-Za-z0-9_-]{0,63}$/;
 const FIELD_LABELS=new Map([
  ['title','文档名称'],['coverTitle','首页标题'],['subtitle','副标题'],['summary','摘要'],
  ['documentNumber','文档编号'],['preparedBy','编制'],['approvedBy','批准'],
  ['checkedBy','校对'],['date','日期'],['revision','修订版'],['reference','参考']
 ]);
 const HTML_NS='http://www.w3.org/1999/xhtml';
 const TAGS=new Set('DIV SPAN SECTION ARTICLE HEADER FOOTER P H1 H2 H3 H4 H5 H6 STRONG B EM I U S DEL SMALL SUB SUP BR HR UL OL LI DL DT DD BLOCKQUOTE CODE PRE TABLE CAPTION COLGROUP COL THEAD TBODY TFOOT TR TH TD'.split(' '));
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
 const CONTENTS_STYLES=[['editorial','简洁分栏'],['classic','经典引导线'],['technical','技术编号'],['banded','章节色带']];
 const PAGE_NUMBER_OPTIONS=[['footer','页脚右侧'],['header','页眉右侧'],['off','仅使用模板变量']];
 const PAGE_VALUE=/\{\{\s*(page|pages)\s*\}\}/g;
 const TEMPLATE_PART=/\{\{\s*(?:(?:page|pages)\s*\}\}(?:[ \t/().–-]*\{\{\s*(?:page|pages)\s*\}\}(?:\))?)*|[A-Za-z_][A-Za-z0-9_-]{0,63}\s*\}\})/g;
 const pdfValue=(source: string,page: any,pages: any): string=>source.replace(PAGE_VALUE,(_,field)=>field==='page'?page:pages);
 const pageNumberPosition=(value: any): string=>value==='header'||value==='off'?value:'footer';
 const contentsStyle=(value: any): string=>CONTENTS_STYLES.some(([key])=>key===value)?value:'editorial';

 function controlHeaderTemplate(t: (key: string) => string): string{
  const rule='padding:3px 5px;vertical-align:middle;border:0;border-bottom:1px solid #444';
  const cell=(label: string,value: string,span=1): string=>`    <td colspan="${span}" style="${rule}">
      <small style="display:block;color:#222;font-size:9px;line-height:1.14">${t(label)}</small>
      <span style="display:block;min-height:14px;color:#111;font-size:12px;line-height:1.19">${value}</span>
    </td>`;
  return `<div style="padding-top:24px;font-family:Arial,sans-serif">
<table style="width:86%;margin:0 auto;table-layout:fixed;border-collapse:collapse;border-top:1px solid #444">
  <colgroup>
    <col style="width:33%"><col style="width:14%"><col style="width:25%"><col style="width:13%"><col style="width:15%">
  </colgroup>
  <tbody>
  <tr>
    <td colspan="2" style="${rule}">{{logo}}</td>
${cell('文档名称','{{title}}',2)}
${cell('页码','{{page}}({{pages}})')}
  </tr>
  <tr>
${cell('编制','{{preparedBy}}',2)}
${cell('文档编号','{{documentNumber}}',3)}
  </tr>
  <tr>
${cell('批准','{{approvedBy}}')}
${cell('校对','{{checkedBy}}')}
${cell('日期','{{date}}')}
${cell('修订版','{{revision}}')}
${cell('参考','{{reference}}')}
  </tr>
  </tbody>
</table>
</div>`;
 }
 const COVER_TEMPLATE=`<section style="padding:52px 80px 0;color:#111;font-family:Arial,sans-serif">
  <div style="text-align:center;font-size:40px;font-weight:400;line-height:1.13">{{coverTitle}}</div>
  <div style="margin-top:53px;min-height:36px;text-align:center;font-size:31px;font-weight:700;line-height:1.15">{{subtitle}}</div>
  <div style="margin-top:141px;font-size:14px;line-height:1.35;text-align:left">{{summary}}</div>
</section>`;
 const htmlValue=(value: any): string=>typeof value==='string'&&value.length<=MAX_HTML?value:'';
 function fieldValues(values: any): any{
  if(!values||typeof values!=='object'||Array.isArray(values))return Object.create(null);
  return Object.assign(Object.create(null),Object.fromEntries(Object.entries(values).filter(([key,value])=>
   FIELD_NAME.test(key)&&typeof value==='string'&&value.length<=MAX_FIELD_LENGTH
  ).slice(0,MAX_FIELDS)));
 }
 function templateValue(field: string,context: any): string{
  if(Object.hasOwn(context.values||{},field))return context.values[field];
  if(field==='title')return (context.region==='cover'?context.coverTitle:context.title)||context.title||'';
  if(field==='coverTitle')return context.coverTitle||context.title||'';
  return '';
 }
 const PAGE={width:595.28,height:841.89};
 const MARGIN=48, TOP=18, GAP=14, PT_PER_PX=0.75, PT_PER_MM=72/25.4, LOGO_WIDTH=76, LOGO_HEIGHT=34;
 let activeDialog: HTMLDialogElement | null=null;

 const logoSide=(value: any): string=>['left','custom','slot'].includes(value)?value:'right';
 function coordinate(value: any,fallback: number): number{
  if(value===''||value==null)return fallback;
  const number=Number(value);
  return Number.isFinite(number)?Math.min(297,Math.max(0,number)):fallback;
 }
 function colorValue(value: any): string{
  const hex=String(value||'').trim().toLowerCase();
  if(/^#[0-9a-f]{6}$/.test(hex))return hex;
  return /^#[0-9a-f]{3}$/.test(hex)?'#'+Array.from(hex.slice(1),n=>n+n).join(''):'';
 }
 function normalizeProfile(saved: any): any{
  if(!saved||typeof saved!=='object'||Array.isArray(saved))return {};
  return {
   color:colorValue(saved.color),
   header:htmlValue(saved.header),footer:htmlValue(saved.footer),cover:htmlValue(saved.cover),
   coverEnabled:saved.coverEnabled===true,coverInToc:saved.coverInToc===true,
   tocAfter:typeof saved.tocAfter==='string'&&saved.tocAfter.length<=4096?saved.tocAfter:'',
   values:fieldValues(saved.values),typography:window.AideMarkdownPdfStyle?.normalize(saved.typography)||null,
   pageNumbers:pageNumberPosition(saved.pageNumbers),tocStyle:contentsStyle(saved.tocStyle),
   logo:typeof saved.logo==='string'&&saved.logo.length<=MAX_LOGO&&/^data:image\/png;base64,[A-Za-z0-9+/]+=*$/.test(saved.logo)?saved.logo:'',
   toc:saved.toc===true,logoSide:logoSide(saved.logoSide),logoX:coordinate(saved.logoX,17),logoY:coordinate(saved.logoY,6.35)
  };
 }
 function read(key: string): any{
  if(!key)return {};
  try{return normalizeProfile(JSON.parse(localStorage.getItem(PREFIX+key)||'{}'));}
  catch{return {};}
 }
 function encodeConfig(profile: any): string{
  return JSON.stringify({format:CONFIG_FORMAT,version:CONFIG_VERSION,profile},null,2)+'\n';
 }
 // Reject a lossy import, allowing field order to differ from the exported JSON.
 function matchesProfile(input: any,profile: any): boolean{
  if(!profile||typeof profile!=='object')return input===profile;
  if(!input||typeof input!=='object'||Array.isArray(input)!==Array.isArray(profile))return false;
  const keys=Object.keys(profile);
  return Object.keys(input).length===keys.length&&keys.every(key=>Object.hasOwn(input,key)&&matchesProfile(input[key],profile[key]));
 }
 function decodeConfig(text: string): any{
  const config=JSON.parse(text);
  if(config?.format!==CONFIG_FORMAT||config.version!==CONFIG_VERSION||!config.profile||
     typeof config.profile!=='object'||Array.isArray(config.profile))throw new Error('Invalid PDF layout configuration');
  const profile=normalizeProfile(config.profile);
  if(!matchesProfile(config.profile,profile))throw new Error('Invalid PDF layout profile');
  if(profile.logo){
   const dimensions=logoDimensions(profile.logo);
   if(!dimensions||dimensions.width>600||dimensions.height>240)throw new Error('Invalid PDF layout logo');
  }
  return profile;
 }
 function appendTemplateText(doc: Document,parent: HTMLElement,text: string,context: any): void{
  let start=0;
  for(const match of text.matchAll(TEMPLATE_PART)){
   const source=match[0];
   const field=source.slice(2,-2).trim();
   parent.append(doc.createTextNode(text.slice(start,match.index)));
   if(field==='logo'){
    const marker=doc.createElement('span');
    marker.className='markdown-pdf-logo-slot';
    marker.setAttribute('aria-hidden','true');
    parent.append(marker);
   }else if(/^\{\{\s*pages?\s*\}\}/.test(source)){
    const marker=doc.createElement('span');
    marker.className='markdown-pdf-page-value';
    marker.textContent=pdfValue(source,context.measuring?'88888':1,context.measuring?'88888':27);
    marker.dataset.template=source;
    parent.append(marker);
   }else{
    const marker=doc.createElement('span');
    marker.className='markdown-pdf-field';
    marker.dataset.field=field;
    marker.textContent=templateValue(field,context);
    if(context.fieldLabel){
     marker.classList.add('markdown-pdf-field-editable');
     marker.tabIndex=0;
     marker.setAttribute('role','button');
     marker.setAttribute('aria-label',context.fieldLabel(field));
     marker.dataset.placeholder=context.fieldLabel(field);
    }
    parent.append(marker);
   }
   start=match.index+source.length;
  }
  parent.append(doc.createTextNode(text.slice(start)));
 }
 function buildHtml(doc: Document,profile: any,region='header',context={}): HTMLElement | null{
  if(!profile[region]?.trim())return null;
  const template=doc.createElement('template');
  template.innerHTML=profile[region];
  context={...context,region,values:profile.values};
  const surface=doc.createElement('div');
  surface.className='markdown-pdf-rich-html';
  surface.dataset.region=region;
  if(profile.color)surface.style.color=profile.color;
  const paint=doc.createElement('span').style;
  const pending: {node: any; parent: HTMLElement}[]=Array.from(template.content.childNodes,node=>({node,parent:surface})).reverse();
  // Only reconstructed HTML elements and these paint properties ever enter
  // the workbench. URL attributes, network paints and page rules stay inert.
  while(pending.length){
   const {node,parent}=pending.pop()!;
   if(node.nodeType===3){appendTemplateText(doc,parent,node.data,context);continue;}
   if(node.nodeType!==1||node.namespaceURI!==HTML_NS||DROP_TAGS.has(node.tagName))continue;
   const tag=node.tagName;
   let target=parent;
   if(TAGS.has(tag)){
    target=doc.createElement(tag.toLowerCase());
    if(tag==='TD'||tag==='TH'){
     (target as HTMLTableCellElement).colSpan=Math.min(node.colSpan,36);
     (target as HTMLTableCellElement).rowSpan=Math.min(node.rowSpan||1,200);
    }
    if(tag==='COL'||tag==='COLGROUP')(target as HTMLTableColElement).span=Math.min(node.span,36);
    if(tag==='OL'&&node.hasAttribute('start'))(target as HTMLOListElement).start=node.start;
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
  window.AideMarkdownPdfStyle?.applyHtmlFonts(surface,profile.typography);
  return surface.textContent!.trim()||surface.children.length?surface:null;
 }
 function logoDimensions(logo: string): {width: number; height: number} | null{
  if(!logo)return null;
  try{
   const start=logo.indexOf(',')+1;
   const bytes=Uint8Array.from(atob(logo.slice(start,start+32)),c=>c.charCodeAt(0));
   if(bytes.length!==24||bytes[0]!==137||bytes[1]!==80||bytes[2]!==78||bytes[3]!==71)return null;
   const data=new DataView(bytes.buffer);
   const width=data.getUint32(16),height=data.getUint32(20);
   return width&&height?{width,height}:null;
  }catch{return null;}
 }
 function logoSize(logo: string): {width: number; height: number} | null{
  const dimensions=logoDimensions(logo);
  if(!dimensions)return null;
  const {width,height}=dimensions;
  const fit=Math.min(LOGO_WIDTH/width,LOGO_HEIGHT/height);
  return {width:width*fit,height:height*fit};
 }
 function pageGeometry(landscape: boolean): {width: number; height: number}{
  return landscape?{width:PAGE.height,height:PAGE.width}:PAGE;
 }
 function relativeBox(element: Element | null | undefined,root: Element | null | undefined): {x: number; y: number; width: number; height: number} | null{
  if(!element)return null;
  const box=element.getBoundingClientRect(),origin=root!.getBoundingClientRect();
  return box.width&&box.height?{x:box.x-origin.x,y:box.y-origin.y,width:box.width,height:box.height}:null;
 }
 function layout(profile: any,page: {width: number; height: number},htmlHeightPx: number,htmlWidthPx=(page.width-2*MARGIN)/PT_PER_PX,options: any={}): any{
  const {region='header',slot,automatic=false}=options;
  const footer=region==='footer';
  const edge=footer&&automatic?37:TOP;
  const lowerLimit=page.height/(footer||profile.footer?.trim()?3:2)-GAP;
  const fullHeight=htmlHeightPx*PT_PER_PX;
  const fullWidth=htmlWidthPx*PT_PER_PX;
  const fit=fullHeight?Math.min(1,(lowerLimit-edge)/fullHeight,(page.width-2*MARGIN)/fullWidth):1;
  const html=fullHeight?{x:MARGIN,y:footer?page.height-edge-fullHeight*fit:edge,width:fullWidth*fit,height:fullHeight*fit,fit}:null;
  const dimensions=footer?null:logoSize(profile.logo);
  let logo=null;
  if(dimensions){
   if(profile.logoSide==='slot'&&html&&slot){
    const unit=fit*PT_PER_PX;
    const ratio=Math.min(slot.width*unit/dimensions.width,slot.height*unit/dimensions.height);
    const width=dimensions.width*ratio,height=dimensions.height*ratio;
    logo={width,height,x:html.x+slot.x*unit,y:html.y+(slot.y+slot.height/2)*unit-height/2};
   }else{
    const custom=profile.logoSide==='custom';
    const x=custom?coordinate(profile.logoX,17)*PT_PER_MM:
     profile.logoSide==='left'||profile.logoSide==='slot'?MARGIN:page.width-MARGIN-dimensions.width;
    const y=custom?coordinate(profile.logoY,6.35)*PT_PER_MM:TOP;
    logo={...dimensions,x:Math.min(x,page.width-dimensions.width),y:Math.min(y,lowerLimit-dimensions.height)};
   }
  }
  if(footer)return {html,bottomMargin:html?Math.max(MARGIN,page.height-html.y+GAP):MARGIN};
  const bottom=Math.max(html?html.y+html.height:0,logo?logo.y+logo.height:0);
  const numberTop=automatic?Math.max(26,bottom+11):0;
  return {html,logo,numberTop,topMargin:Math.max(MARGIN,bottom+GAP,numberTop+GAP)};
 }
 function coverLayout(page: {width: number; height: number},topMargin: number,bottomMargin: number,heightPx: number,widthPx: number): any{
  if(!heightPx||!widthPx)return null;
  const y=Math.max(topMargin,118);
  const fit=Math.min(1,(page.height-y-bottomMargin)/(heightPx*PT_PER_PX),(page.width-2*MARGIN)/(widthPx*PT_PER_PX));
  return fit>0?{x:MARGIN,y,width:widthPx*PT_PER_PX*fit,height:heightPx*PT_PER_PX*fit,fit}:null;
 }
 async function loadLogo(file: File): Promise<string>{
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
   context!.drawImage(image,0,0,canvas.width,canvas.height);
   const data=canvas.toDataURL('image/png');
   canvas.width=canvas.height=0;
   if(data.length>MAX_LOGO)throw new Error('Logo 文件过大，请选用较小的图片。');
   return data;
  }finally{URL.revokeObjectURL(url);}
 }

 function close(): void{activeDialog?.close();}

 function open({key,valid,button,t,title,headings=[],download}: PdfAppearanceOpenOptions): void{
  close();
  if(!key||!valid())return;
  const saved=read(key);
  let logo=saved.logo||'',logoRequest=0,activeField='',activeRegion='';
  let values=fieldValues(saved.values);
  const coverTitle=headings.find(item=>item.level===1)?.title||title;
  const fieldLabel=(field: string): string=>FIELD_LABELS.has(field)?t(FIELD_LABELS.get(field)!):field;
  const make=<K extends keyof HTMLElementTagNameMap>(tag: K,text?: string | null,className?: string | null): HTMLElementTagNameMap[K]=>{
   const node=document.createElement(tag);
   if(text)node.textContent=t(text);
   if(className)node.className=className;
   return node;
  };
  const action=(label: string,className='quiet'): HTMLButtonElement=>{
   const item=make('button',label,className);item.type='button';return item;
  };
  const label=(name: string,control: HTMLElement): HTMLElement=>{
   const wrap=make('label',name);
   control.setAttribute('aria-label',t(name));wrap.append(control);return wrap;
  };
  const select=(options: any[][],value: string): HTMLSelectElement=>{
   const control=make('select');
   for(const [key,name] of options){const option=make('option',name);option.value=key;control.append(option);}
   control.value=value;return control;
  };
  const number=(value: any): HTMLInputElement=>{
   const input=make('input');input.type='number';input.min='0';input.max='297';input.step='any';input.value=String(value);
   return input;
  };

  const dialog=make('dialog',null,'dialog markdown-pdf-appearance');
  dialog.setAttribute('aria-label',t('PDF 版式设置'));
  const heading=make('header',null,'markdown-pdf-appearance-heading');
  const copy=action('复制 AI 提示词');
  heading.append(make('h2','PDF 版式设置'),copy);

  const form=make('form');
  const navigation=make('div',null,'markdown-pdf-layout-navigation');
  navigation.setAttribute('role','group');
  navigation.setAttribute('aria-label',t('PDF 设置分区'));
  const tabs=[['header','页眉'],['cover','首页'],['footer','页脚'],['contents','目录'],['style','正文样式']].map(([key,name])=>[key,action(name)] as [string, HTMLButtonElement]);
  for(const [,tab] of tabs)navigation.append(tab);
  let section='header';
  const columns=make('div',null,'markdown-pdf-appearance-columns');
  const editor=make('section',null,'markdown-pdf-appearance-editor');
  const previewPanel=make('section',null,'markdown-pdf-appearance-preview');
  columns.append(editor,previewPanel);

  const color=make('input');color.type='text';color.maxLength=7;color.placeholder='#24598A';color.value=saved.color||'';color.spellcheck=false;
  const picker=make('input');picker.type='color';picker.value=saved.color||'#24598a';
  picker.setAttribute('aria-label',t('选择主题色'));
  const colorField=make('div',null,'markdown-pdf-color');colorField.append(label('文档主题色',color),picker);
  const editors: Record<string, HTMLTextAreaElement>={};
  const fields: Record<string, HTMLElement>={};
  for(const [region,name] of [['header','HTML 页眉'],['cover','HTML 首页'],['footer','HTML 页脚']]){
   const input=make('textarea');input.value=saved[region]||(region==='cover'?COVER_TEMPLATE:'');input.maxLength=MAX_HTML;input.rows=9;input.spellcheck=false;
   editors[region]=input;
   fields[region]=label(name,input);
  }
  const headerTools=make('div',null,'markdown-pdf-template-tools');
  const templateButton=action('套用文控表格');
  headerTools.append(templateButton);
  const coverEnabled=make('input');coverEnabled.type='checkbox';coverEnabled.checked=saved.coverEnabled;
  const coverToggle=make('label',null,'markdown-pdf-toc-setting');
  coverToggle.append(coverEnabled,make('span','添加首页'));
  const coverPreset=action('套用参考首页');
  const coverHelp=make('p','首页默认使用第一个一级标题，可在预览中编辑标题、副标题和摘要。','muted');
  const coverTools=make('div',null,'markdown-pdf-cover-tools');
  coverTools.append(coverToggle,coverPreset,coverHelp);
  const variables=make('div',null,'markdown-pdf-template-tools');
  for(const [field,name] of [['page','插入当前页码'],['pages','插入总页数']]){
   const insert=action(name);
   insert.onclick=()=>{
    const input=editors[section];
    input.setRangeText('{{'+field+'}}',input.selectionStart,input.selectionEnd,'end');
    input.focus();
    refresh();
   };
   variables.append(insert);
  }
  const help=make('p','用 {{fieldName}} 定义文字变量。点击预览里的字段即可填写。','muted');
  const numberPlacement=select(PAGE_NUMBER_OPTIONS,pageNumberPosition(saved.pageNumbers));
  const numberSetting=label('自动页码位置',numberPlacement);
  const toc=make('input');toc.type='checkbox';toc.checked=saved.toc===true;
  const tocField=make('label',null,'markdown-pdf-toc-setting');
  tocField.append(toc,make('span','按标题生成目录'));
  const tocPreset=select(CONTENTS_STYLES,contentsStyle(saved.tocStyle));
  const contentsField=label('目录样式',tocPreset);
  const tocAfter=select([['','正文之前']], '');
  for(const [index,item] of headings.entries()){
   const option=make('option');option.value=item.key;
   option.textContent=(index+1)+'. '+'#'.repeat(item.level)+' '+item.title;
   tocAfter.append(option);
  }
  if(headings.some(item=>item.key===saved.tocAfter))tocAfter.value=saved.tocAfter;
  const tocPosition=label('目录插入位置',tocAfter);
  const includeCover=make('input');includeCover.type='checkbox';includeCover.checked=saved.coverInToc;
  const coverEntry=make('label',null,'markdown-pdf-toc-setting');
  coverEntry.append(includeCover,make('span','将首页标题列入目录'));
  const contentsHelp=make('p','目录独立成页。选择标题时，目录插在该标题之后。','muted');
  editor.append(colorField,headerTools,coverTools,fields.header,fields.cover,fields.footer,variables,help,numberSetting,tocField,contentsField,tocPosition,coverEntry,contentsHelp);

  const styleFields=make('section',null,'markdown-pdf-style-settings');
  const styleUI=window.AideMarkdownPdfStyle!.controls(styleFields,{initial:saved.typography,t,change:refresh});
  editor.append(styleFields);

  const file=make('input');file.type='file';file.accept='image/png,image/jpeg,image/webp';
  file.hidden=true;file.setAttribute('aria-label',t('页眉 Logo'));
  const choose=action('选择 Logo');
  const remove=action('移除 Logo');
  const logoField=make('div',null,'markdown-pdf-logo-setting');
  const logoActions=make('div',null,'markdown-pdf-logo-actions');
  logoActions.append(choose,remove);
  logoField.append(make('div','页眉 Logo'),logoActions,file);
  choose.onclick=()=>file.click();
  const position=select([['left','左上角'],['right','右上角'],['custom','自定义坐标'],['slot','跟随表格中的 Logo 占位符']],logoSide(saved.logoSide));
  const x=number(coordinate(saved.logoX,17)),y=number(coordinate(saved.logoY,6.35));
  const coords=make('div',null,'markdown-pdf-logo-coordinates');
  coords.append(label('X（毫米）',x),label('Y（毫米）',y));
  const tip=make('p','以纸张左上角为原点。超出页眉区域的 Logo 会贴边显示。','muted');
  const previewOrientation=select([['portrait','A4 竖版'],['landscape','A4 横版']],'portrait');
  const previewHeading=make('div',null,'markdown-pdf-preview-heading');
  previewOrientation.setAttribute('aria-label',t('预览页面'));
  const previewLabel=make('strong','页眉预览');
  previewHeading.append(previewLabel,previewOrientation);
  const preview=make('div',null,'markdown-pdf-header-preview');
  const paper=make('div',null,'markdown-pdf-preview-paper');
  preview.append(paper);
  const positionField=label('Logo 位置',position);
  const valueEditor=make('div',null,'markdown-pdf-value-editor');valueEditor.hidden=true;
  const valueLabel=make('span');valueLabel.id='markdown-pdf-field-label';
  const valueInput=make('textarea');valueInput.rows=2;valueInput.maxLength=MAX_FIELD_LENGTH;
  valueInput.setAttribute('aria-labelledby',valueLabel.id);
  const defaultValue=action('恢复默认值');
  const closeValue=action('完成编辑');
  const valueActions=make('div',null,'markdown-pdf-template-tools');
  valueActions.append(defaultValue,closeValue);
  valueEditor.append(valueLabel,valueInput,valueActions);
  previewPanel.append(logoField,positionField,coords,tip,previewHeading,valueEditor,preview);
  const error=make('p',null,'task-error');error.setAttribute('role','status');
  const status=make('p',null,'muted');status.setAttribute('role','status');
  const buttons=make('div',null,'markdown-pdf-appearance-actions');
  const transfer=make('div',null,'markdown-pdf-layout-transfer');
  const configFile=make('input');configFile.type='file';configFile.accept='.json,application/json';configFile.hidden=true;
  configFile.setAttribute('aria-label',t('导入 PDF 版式配置'));
  const importConfig=action('导入配置');
  const exportConfig=action('导出配置');
  transfer.append(importConfig,exportConfig,configFile);
  const cancel=action('取消');
  const save=action('保存','primary');save.type='submit';
  buttons.append(status,transfer,cancel,save);
  form.append(navigation,columns,error,buttons);
  dialog.append(heading,form);
  document.body.append(dialog);

  const current=(): any=>({
   header:editors.header.value,footer:editors.footer.value,cover:editors.cover.value,color:colorValue(color.value),
   coverEnabled:coverEnabled.checked,coverInToc:includeCover.checked,tocAfter:tocAfter.value,values,typography:styleUI.read(),
   logo,logoSide:position.value,logoX:x.value,logoY:y.value,
   pageNumbers:numberPlacement.value,toc:toc.checked,tocStyle:tocPreset.value
  });
  function editValue(target: any): void{
   const marker=target.closest('.markdown-pdf-field-editable');
   if(!marker)return;
   activeRegion=marker.closest('.markdown-pdf-rich-html').dataset.region;
   const field=activeField=marker.dataset.field;
   valueLabel.textContent=fieldLabel(field);
   valueInput.value=templateValue(field,{title,coverTitle,values,region:activeRegion});
   valueEditor.hidden=false;
   valueInput.focus();valueInput.select();
  }
  preview.addEventListener('click',event=>editValue(event.target));
  preview.addEventListener('keydown',event=>{
   if(event.key!=='Enter'&&event.key!==' ')return;
   if(!(event.target as HTMLElement).matches('.markdown-pdf-field-editable'))return;
   event.preventDefault();editValue(event.target);
  });
  valueInput.oninput=()=>{
   if(!Object.hasOwn(values,activeField)&&Object.keys(values).length>=MAX_FIELDS){
    error.textContent=t('模板文字字段数量已达上限。');
    return;
   }
   values[activeField]=valueInput.value.replace(/\r\n/g,'\n');
   error.textContent='';
   refresh();
  };
  defaultValue.onclick=()=>{
   delete values[activeField];
   valueInput.value=templateValue(activeField,{title,coverTitle,values,region:activeRegion});
   refresh();
  };
  closeValue.onclick=()=>{
   paper.querySelectorAll('.markdown-pdf-field-editable').forEach(element=>{if((element as HTMLElement).dataset.field===activeField&&(element.closest('.markdown-pdf-rich-html') as HTMLElement).dataset.region===activeRegion)(element as HTMLElement).focus();});
   activeField='';valueEditor.hidden=true;
  };
  let previewFrame=0;
  function refresh(): void{
   cancelAnimationFrame(previewFrame);
   previewFrame=requestAnimationFrame(paint);
  }
  function paintContents(profile: any): void{
   const sample=make('div',null,'markdown-pdf-contents-sample');
   sample.dataset.preset=profile.tocStyle;
   if(profile.color)sample.style.setProperty('--contents-accent',profile.color);
   sample.append(make('strong','目录'));
   for(const [index,name,page,chapter] of [['1','项目概述','3',true],['1.1','范围与目标','4',false],['1.2','使用条件','6',false],['2','设计与验证','9',true],['2.1','验证方法','10',false]] as [string, string, string, boolean][]){
    const row=make('div',null,chapter?'chapter':'subsection');
    const order=make('span');order.textContent=index;order.className='order';
    const leader=make('span');leader.className='leader';
    const number=make('span');number.textContent=page;
    row.append(order,make('span',name),leader,number);
    sample.append(row);
   }
   window.AideMarkdownPdfStyle!.applyHtmlFonts(sample,profile.typography);
   paper.replaceChildren(sample);
   paper.style.width='100%';
   paper.style.height='auto';
  }
  function paint(): void{
   if(!dialog.open)return;
   const profile=current();
   const isHeader=section==='header',isCover=section==='cover',isContents=section==='contents',isStyle=section==='style';
   for(const [name,tab] of tabs)tab.setAttribute('aria-pressed',String(section===name));
   copy.hidden=colorField.hidden=variables.hidden=help.hidden=isContents||isStyle;
   numberSetting.hidden=isContents||isCover||isStyle;
   styleFields.hidden=!isStyle;
   headerTools.hidden=fields.header.hidden=logoField.hidden=positionField.hidden=!isHeader;
   fields.footer.hidden=section!=='footer';
   coverTools.hidden=fields.cover.hidden=!isCover;
   tocField.hidden=contentsField.hidden=tocPosition.hidden=contentsHelp.hidden=!isContents;
   coverEntry.hidden=!isContents||!profile.coverEnabled;
   tocAfter.disabled=!profile.toc||!headings.length;
   tocPreset.disabled=includeCover.disabled=!profile.toc;
   previewOrientation.hidden=isContents||isCover||isStyle;
   preview.dataset.section=section;
   previewLabel.textContent=t(isStyle?'正文样式预览':isContents?'目录预览':isCover?'首页预览':isHeader?'页眉预览':'页脚预览');
   if(profile.color)picker.value=profile.color;
   position.disabled=!logo;remove.hidden=!logo;
   choose.textContent=t(logo?'更换 Logo':'选择 Logo');
   const custom=isHeader&&!!logo&&position.value==='custom';
   coords.hidden=tip.hidden=!custom;
   x.disabled=y.disabled=!custom;
   paper.style.zoom='1';
   if(isContents){paintContents(profile);return;}
   if(isStyle){
    paper.style.width='100%';paper.style.height='auto';
    window.AideMarkdownPdfStyle!.preview(paper,profile.typography,styleUI.section(),t);
    return;
   }

   const page=pageGeometry(!isCover&&previewOrientation.value==='landscape');
   paper.style.width=page.width/PT_PER_PX+'px';
   paper.replaceChildren();
   const context={title,coverTitle,fieldLabel};
   const headerHtml=buildHtml(document,profile,'header',context);
   const footerHtml=buildHtml(document,profile,'footer',context);
   const coverHtml=isCover?buildHtml(document,{...profile,cover:profile.cover||COVER_TEMPLATE},'cover',context):null;
   const placement=[headerHtml,footerHtml].some(html=>html?.querySelector('.markdown-pdf-page-value'))?'off':profile.pageNumbers;
   const showAutomatic=!coverHtml?.querySelector('.markdown-pdf-page-value');
   const width=(page.width-2*MARGIN)/PT_PER_PX;
   const regions: [string, HTMLElement | null][]=isCover?[['header',headerHtml],['footer',footerHtml]]:[[section,isHeader?headerHtml:footerHtml]];
   const arrangements=new Map<string, any>();
   const place=(html: HTMLElement,box: any,origin=0): void=>{
    html.style.left=box.x/PT_PER_PX+'px';
    html.style.top=(box.y-origin)/PT_PER_PX+'px';
    html.style.transform='scale('+box.fit+')';
   };
   for(const [region,html] of regions){
    if(html){html.style.width=width+'px';paper.append(html);}
    const slot=relativeBox(html?.querySelector('.markdown-pdf-logo-slot'),html);
    const arrangement=layout(profile,page,html?.scrollHeight||0,html?.scrollWidth,{region,slot,automatic:placement===region});
    arrangements.set(region,arrangement);
    const origin=isCover||region==='header'?0:page.height-arrangement.bottomMargin-12;
    if(arrangement.html)place(html!,arrangement.html,origin);
    if(arrangement.logo){
     const img=make('img',null,'markdown-pdf-preview-logo');
     img.alt=t('页眉 Logo');img.src=logo;
     for(const [property,key] of [['left','x'],['top','y'],['width','width'],['height','height']])(img.style as any)[property]=arrangement.logo[key]/PT_PER_PX+'px';
     paper.append(img);
    }
    if(showAutomatic&&placement===region){
     const number=make('span',null,'markdown-pdf-preview-number');number.textContent='1 / 27';
     number.style.top=((region==='header'?arrangement.numberTop:page.height-20)-origin-8)/PT_PER_PX+'px';
     paper.append(number);
    }
    if(!isCover&&!arrangement.html&&!arrangement.logo&&placement!==region)paper.append(make('p',isHeader?'页眉为空':'页脚为空','markdown-pdf-empty-preview'));
   }
   if(isCover&&coverHtml){
    coverHtml.style.width=width+'px';paper.append(coverHtml);
    const box=coverLayout(page,arrangements.get('header').topMargin,arrangements.get('footer').bottomMargin,
     coverHtml.scrollHeight,coverHtml.scrollWidth);
    if(box)place(coverHtml,box);
   }
   const height=isCover?page.height:(isHeader?arrangements.get('header').topMargin:arrangements.get('footer').bottomMargin)+24;
   paper.style.height=height/PT_PER_PX+'px';
   paper.style.zoom=String(Math.min(1,Math.max(0.1,(preview.clientWidth-2)/(page.width/PT_PER_PX))));
  }

  for(const [name,tab] of tabs)tab.onclick=()=>{section=name;activeField='';valueEditor.hidden=true;paint();};
  const useHeaderTemplate=(): void=>{
   editors.header.value=controlHeaderTemplate(t);
   position.value='slot';
   numberPlacement.value='off';
   editors.header.scrollTop=0;
  };
  templateButton.onclick=()=>{useHeaderTemplate();refresh();};
  coverPreset.onclick=()=>{
   coverEnabled.checked=true;
   editors.cover.value=COVER_TEMPLATE;
   if(!editors.header.value.trim())useHeaderTemplate();
   refresh();
  };

  copy.onclick=async()=>{
   const guide=t(section==='cover'
    ?'请生成可粘贴到 Aide 的 HTML PDF 首页。使用内联 CSS，设置居中标题 {{coverTitle}}、副标题 {{subtitle}} 和摘要 {{summary}}。文字可用自定义 {{fieldName}} 变量，变量名用英文字母、数字或下划线，只输出 HTML。'
    :section==='footer'
    ?'请生成可直接粘贴到 Aide 的 HTML PDF 页脚。使用 HTML 片段、表格和内联 CSS。用 {{page}} 表示当前页，{{pages}} 表示总页数，只输出 HTML。'
    :'请生成可直接粘贴到 Aide 的 HTML PDF 页眉。使用 HTML 表格和内联 CSS，包含文档名称、编号、编制与审批、日期和版本。{{title}} 是文件名，{{page}} 是当前页，{{pages}} 是总页数，{{logo}} 是上传 Logo 的占位符。只输出 HTML。');
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
  color.oninput=editors.header.oninput=editors.footer.oninput=editors.cover.oninput=x.oninput=y.oninput=()=>{error.textContent='';refresh();};
  picker.oninput=()=>{color.value=picker.value;(color.oninput as any)();};
  position.onchange=previewOrientation.onchange=numberPlacement.onchange=tocPreset.onchange=toc.onchange=coverEnabled.onchange=refresh;
  file.onchange=async()=>{
   const chosen=file.files?.[0];if(!chosen)return;
   const request=++logoRequest;
   setTransferDisabled(true);error.textContent='';
   try{
    const data=await loadLogo(chosen);
    if(dialog.open&&valid()&&request===logoRequest){logo=data;refresh();}
   }catch(cause: any){
    if(dialog.open&&request===logoRequest)error.textContent=t(cause.message);
   }finally{
    if(request===logoRequest){file.value='';setTransferDisabled(false);}
   }
  };
  remove.onclick=()=>{logoRequest++;setTransferDisabled(false);file.value='';logo='';refresh();};
  function draftProfile(): any{
   const draft=current();
   if(color.value.trim()&&!draft.color){section='header';paint();error.textContent=t('请输入有效的十六进制主题色。');color.focus();return null;}
   draft.header=draft.header.trim();
   draft.footer=draft.footer.trim();
   draft.cover=draft.cover.trim();
   if(draft.cover===COVER_TEMPLATE)draft.cover='';
   return normalizeProfile(draft);
  }
  function applyProfile(profile: any): void{
   color.value=profile.color;
   picker.value=profile.color||'#24598a';
   for(const region of ['header','footer','cover']){
    editors[region].value=profile[region]||(region==='cover'?COVER_TEMPLATE:'');
    editors[region].scrollTop=0;
   }
   coverEnabled.checked=profile.coverEnabled;
   includeCover.checked=profile.coverInToc;
   toc.checked=profile.toc;
   tocPreset.value=profile.tocStyle;
   const missingHeading=profile.tocAfter&&!headings.some(item=>item.key===profile.tocAfter);
   tocAfter.value=missingHeading?'':profile.tocAfter;
   numberPlacement.value=profile.pageNumbers;
   logo=profile.logo;
   position.value=profile.logoSide;
   x.value=profile.logoX;
   y.value=profile.logoY;
   values=profile.values;
   activeField=activeRegion='';
   valueEditor.hidden=true;
   styleUI.write(profile.typography);
   refresh();
   status.textContent=t(missingHeading?'配置已导入。未找到对应标题，目录位置已设为正文之前。点击保存以应用。':'配置已导入。点击保存以应用到当前文档。');
  }
  function setTransferDisabled(disabled: boolean): void{
   save.disabled=importConfig.disabled=exportConfig.disabled=disabled;
  }
  importConfig.onclick=()=>configFile.click();
  configFile.onchange=async()=>{
   const chosen=configFile.files?.[0];
   configFile.value='';
   if(!chosen||save.disabled)return;
   status.textContent=error.textContent='';
   setTransferDisabled(true);
   choose.disabled=remove.disabled=true;
   try{
    if(chosen.size>MAX_CONFIG_BYTES)throw new Error('PDF layout file is too large');
    const profile=decodeConfig(await chosen.text());
    if(profile.logo){
     const image=new Image();
     image.src=profile.logo;
     await image.decode();
    }
    if(dialog.open&&valid())applyProfile(profile);
   }catch{
    if(dialog.open&&valid())error.textContent=t('无法导入配置，请选择有效且版本受支持的 Aide PDF 版式 JSON 文件。');
   }finally{
    setTransferDisabled(false);
    choose.disabled=remove.disabled=false;
   }
  };
  exportConfig.onclick=()=>{
   if(!valid()){dialog.close();return;}
   status.textContent=error.textContent='';
   const profile=draftProfile();
   if(!profile)return;
   download(new Blob([encodeConfig(profile)],{type:'application/json'}),(title||'document')+'.pdf-layout.json');
   status.textContent=t('已导出 PDF 版式配置。');
  };
  cancel.onclick=()=>dialog.close();
  form.onsubmit=event=>{
   event.preventDefault();
   if(save.disabled)return;
   if(!valid()){dialog.close();return;}
   const profile=draftProfile();
   if(!profile)return;
   try{
    if(profile.typography||profile.color||profile.header||profile.footer||profile.cover||profile.coverEnabled||Object.keys(profile.values).length||profile.logo||profile.toc||profile.tocAfter||profile.coverInToc||profile.pageNumbers!=='footer'||profile.tocStyle!=='editorial')localStorage.setItem(PREFIX+key,JSON.stringify(profile));
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

 window.AideMarkdownPdfAppearance={close,open,read,buildHtml,layout,coverLayout,pageGeometry,relativeBox,pdfValue,templateValue,COVER_TEMPLATE,MARGIN,PT_PER_PX};
 if(window.__AIDE_MARKDOWN_EXPORT_TEST__)window.AideMarkdownPdfAppearance!.test={encodeConfig,decodeConfig};
})();
