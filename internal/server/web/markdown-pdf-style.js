/* Document-wide fonts and Markdown typography for the PDF renderer. */
(() => {
 'use strict';

 const CJK_FONTS=[['NotoSansSC','Noto Sans SC'],['NotoSerifSC','Noto Serif SC']];
 const WESTERN_FONTS=[['','跟随默认字体'],['Helvetica','Helvetica'],['Times','Times'],['Courier','Courier']];
 const STANDARDS={
  Helvetica:{normal:'Helvetica',bold:'Helvetica-Bold',italics:'Helvetica-Oblique',bolditalics:'Helvetica-BoldOblique'},
  Times:{normal:'Times-Roman',bold:'Times-Bold',italics:'Times-Italic',bolditalics:'Times-BoldItalic'},
  Courier:{normal:'Courier',bold:'Courier-Bold',italics:'Courier-Oblique',bolditalics:'Courier-BoldOblique'}
 };
 const CSS_FONTS={
  NotoSansSC:'"Aide PDF Noto Sans SC",sans-serif',NotoSerifSC:'"Aide PDF Noto Serif SC",serif',
  Helvetica:'Arial,Helvetica,sans-serif',Times:'"Times New Roman",Times,serif',Courier:'"Courier New",monospace'
 };
 const HEADING_STYLES=[['regular','常规'],['bold','粗体'],['italic','斜体'],['boldItalic','粗斜体']];
 const ALIGNMENTS=[['left','左对齐'],['center','居中'],['right','右对齐']];
 const TABLE_STYLES=[['horizontal','横线'],['grid','完整网格'],['minimal','仅表头分隔线']];
 const HEADING_SIZES=[29,21,16,13,11,10];
 const HEADING_BEFORE=[6,18,13,10,8,8];
 const HEADING_AFTER=[18,9,7,5,4,4];
 const ASCII_DIGIT=/^[0-9]$/;
 const WESTERN=/^[\x20-\x7e\u00a0-\u00ffŒœŠšŸŽžƒˆ˜‘’‚“”„†‡•–—…‰‹›€™]$/u;
 const EAST_ASIAN=/[\p{Script=Han}\p{Script=Hiragana}\p{Script=Katakana}\p{Script=Hangul}\p{Script=Bopomofo}\u3000-\u303f\uff00-\uffef]/u;

 function defaults(){
  return {
   fonts:{han:'NotoSansSC',latin:'',digits:''},size:10.5,lineHeight:1.24,
   headings:HEADING_SIZES.map((size,index)=>({size,style:'bold',align:'left',color:'',before:HEADING_BEFORE[index],after:HEADING_AFTER[index]})),
   table:{mode:'horizontal',size:9,padding:5,border:0.45,borderColor:'#d8e0e8',headerFill:'#eaf0f6',stripe:false,stripeFill:'#f4f6f9',repeat:true,headerBold:true}
  };
 }
 function reference(){
  const value=defaults();
  value.fonts.latin='Helvetica';
  value.fonts.digits='Helvetica';
  value.headings=[24,20,16,14,12,10.5].map((size,index)=>({
   size,style:index===5?'bold':'regular',align:'left',color:'#0f4761',before:[24,8,8,4,4,2][index],after:[4,4,4,2,2,0][index]
  }));
  value.table={...value.table,mode:'grid',size:10.5,border:0.5,borderColor:'#303030',headerFill:'#d0cece'};
  return value;
 }
 const choice=(value,choices,fallback)=>choices.some(([id])=>id===value)?value:fallback;
 const color=(value,fallback)=>typeof value==='string'&&/^#[0-9a-f]{6}$/i.test(value)?value.toLowerCase():fallback;
 function number(value,min,max,fallback){
  return typeof value==='number'&&Number.isFinite(value)?Math.min(max,Math.max(min,value)):fallback;
 }
 function normalize(input){
  if(!input||typeof input!=='object'||Array.isArray(input))return null;
  const base=defaults();
  const table=input.table||{};
  return {
   fonts:{
    han:choice(input.fonts?.han,CJK_FONTS,base.fonts.han),
    latin:choice(input.fonts?.latin,WESTERN_FONTS,''),
    digits:choice(input.fonts?.digits,WESTERN_FONTS,'')
   },
   size:number(input.size,6,36,base.size),lineHeight:number(input.lineHeight,1,2.5,base.lineHeight),
   headings:base.headings.map((heading,index)=>{
    const value=input.headings?.[index]||{};
    return {
     size:number(value.size,6,88,heading.size),style:choice(value.style,HEADING_STYLES,heading.style),
     align:choice(value.align,ALIGNMENTS,heading.align),color:color(value.color,''),
     before:number(value.before,0,100,heading.before),after:number(value.after,0,100,heading.after)
    };
   }),
   table:{
    mode:choice(table.mode,TABLE_STYLES,base.table.mode),
    size:number(table.size,6,26,base.table.size),padding:number(table.padding,1,24,base.table.padding),
    border:number(table.border,0.25,3,base.table.border),borderColor:color(table.borderColor,base.table.borderColor),
    headerFill:color(table.headerFill,base.table.headerFill),stripe:table.stripe===true,
    stripeFill:color(table.stripeFill,base.table.stripeFill),repeat:table.repeat!==false,headerBold:table.headerBold!==false
   }
  };
 }
 function fontRuns(text,fonts){
  const base=fonts.han,latin=fonts.latin||base,digits=fonts.digits||latin;
  if(base==='NotoSansSC'&&latin===base&&digits===base)return [{text,font:base}];
  const runs=[];
  for(const char of text){
   const previous=runs.at(-1);
   const font=ASCII_DIGIT.test(char)?digits:char===' '||char==='\n'||char==='\t'?previous?.font||latin:
    EAST_ASIAN.test(char)?base:WESTERN.test(char)?latin:'NotoSansSC';
   if(previous?.font===font)previous.text+=char;
   else runs.push({text:char,font});
  }
  return runs.length?runs:[{text:'',font:base}];
 }
 function applyPdfFonts(content,settings){
  if(!settings)return;
  const {fonts}=settings;
  if(fonts.han==='NotoSansSC'&&!fonts.latin&&!fonts.digits)return;
  const pending=[content];
  while(pending.length){
   const node=pending.pop();
   if(!node||typeof node!=='object')continue;
   if(Array.isArray(node)){
    for(let i=0;i<node.length;i++){
     if(typeof node[i]==='string')node[i]={text:node[i]};
     pending.push(node[i]);
    }
    continue;
   }
   if(typeof node.text==='string'){
    const runs=fontRuns(node.text,fonts);
    if(runs.length===1)node.font=runs[0].font;
    else node.text=runs;
   }else if(Array.isArray(node.text))pending.push(node.text);
   const children=node.table?.body||node.stack||node.columns||node.ul||node.ol;
   if(children)pending.push(children);
  }
 }
 function applyHtmlFonts(root,settings){
  if(!settings)return;
  const doc=root.ownerDocument,walker=doc.createTreeWalker(root,4);
  const nodes=[];
  while(walker.nextNode())nodes.push(walker.currentNode);
  for(const node of nodes){
   if(!node.data.trim())continue;
   if(node.parentElement.closest('.markdown-pdf-page-value')){
    const font=settings.fonts.digits||settings.fonts.latin||'Helvetica';
    node.parentElement.style.fontFamily=CSS_FONTS[font];
    continue;
   }
   const fragment=doc.createDocumentFragment();
   for(const {text,font} of fontRuns(node.data,settings.fonts)){
    const span=doc.createElement('span');
    span.textContent=text;span.style.fontFamily=CSS_FONTS[font];fragment.append(span);
   }
   node.replaceWith(fragment);
  }
 }
 function headingPaint(element,heading){
  element.style.fontSize=heading.size+'pt';
  element.style.fontWeight=heading.style==='bold'||heading.style==='boldItalic'?'700':'400';
  element.style.fontStyle=heading.style==='italic'||heading.style==='boldItalic'?'italic':'normal';
  element.style.textAlign=heading.align;
  if(heading.color)element.style.color=heading.color;
 }
 function tablePaint(table,settings){
  const headers=table.tHead?.rows.length||0;
  for(const [index,row] of Array.from(table.rows).entries()){
   const header=index<headers;
   for(const cell of row.cells){
    cell.style.fontSize=settings.size+'pt';
    cell.style.padding=settings.padding+'pt';
    if(header){
     cell.style.backgroundColor=settings.headerFill;
     cell.style.fontWeight=settings.headerBold?'700':'400';
    }else if(settings.stripe&&(index-headers)%2===0)cell.style.backgroundColor=settings.stripeFill;
   }
  }
 }
 function preview(container,settings,view,t){
  const value=settings||defaults(),doc=container.ownerDocument;
  const sample=doc.createElement('div');sample.className='markdown-pdf-style-sample';
  sample.style.fontSize=value.size+'pt';sample.style.lineHeight=String(value.lineHeight);
  const append=(parent,tag,text)=>{
   const node=doc.createElement(tag);node.textContent=text;parent.append(node);return node;
  };
  if(view==='heading'){
   value.headings.forEach((heading,index)=>{
    const node=append(sample,'div','H'+(index+1)+' · '+t('标题样例'));
    headingPaint(node,heading);node.style.margin=heading.before+'pt 0 '+heading.after+'pt';
   });
  }else if(view==='table'){
   const table=append(sample,'table','');
   const header=table.createTHead().insertRow();
   for(const title of ['项目','状态','数量'])append(header,'th',t(title));
   const rows=table.createTBody();
   for(const [name,status,count] of [['Alpha','OK','128'],['Beta','OK','256'],['Gamma','OK','512']]){
    const row=rows.insertRow();
    for(const text of [name,status,count])append(row,'td',text);
   }
   tablePaint(table,value.table);
   const border=value.table.border+'pt solid '+value.table.borderColor;
   for(const cell of table.querySelectorAll('th,td')){
    if(value.table.mode==='grid')cell.style.border=border;
    else if(value.table.mode==='horizontal'||cell.tagName==='TH')cell.style.borderBottom=border;
   }
  }else{
   append(sample,'p','中文排版 · English text · 0123456789');
   append(sample,'p','项目 A2 已完成 128 次检查。');
   append(sample,'p','Quality review 2026  /  12.50 %');
  }
  applyHtmlFonts(sample,value);
  container.replaceChildren(sample);
 }
 function controls(container,{initial,t,change}){
  let settings=normalize(initial),section='font',headingLevel=0;
  const doc=container.ownerDocument;
  const create=(tag,text)=>{
   const node=doc.createElement(tag);if(text)node.textContent=t(text);return node;
  };
  const action=text=>{const node=create('button',text);node.type='button';node.className='quiet';return node;};
  const tools=create('div');tools.className='markdown-pdf-template-tools';
  const useReference=action('套用文档排版样式');
  const reset=action('恢复正文默认样式');
  tools.append(useReference,reset);
  const navigation=create('div');navigation.className='markdown-pdf-layout-navigation';
  const tabs=[['font','字体'],['heading','各级标题'],['table','表格样式']].map(([key,name])=>[key,action(name)]);
  for(const [,tab] of tabs)navigation.append(tab);
  const fields=create('div');fields.className='markdown-pdf-style-fields';
  container.append(tools,navigation,fields);
  const replace=update=>{
   settings=normalize(update);change();
  };
  function draw(){
   fields.replaceChildren();
   for(const [key,tab] of tabs)tab.setAttribute('aria-pressed',String(key===section));
   const value=settings||defaults();
   const add=(title,control)=>{
    const label=create('label',title);control.setAttribute('aria-label',t(title));label.append(control);fields.append(label);
   };
   const set=(options,selected,update)=>{
    const field=create('select');
    for(const [key,text] of options){const item=create('option',text);item.value=key;field.append(item);}
    field.value=String(selected);field.onchange=()=>update(field.value);return field;
   };
   const size=(current,min,max,step,update)=>{
    const field=create('input');field.type='number';field.min=min;field.max=max;field.step=step;field.value=current;
    field.onchange=()=>{update(number(field.valueAsNumber,Number(min),Number(max),current));field.value=number(field.valueAsNumber,Number(min),Number(max),current);};
    return field;
   };
   const paint=(current,update)=>{
    const field=create('input');field.type='color';field.value=current;field.oninput=()=>update(field.value);return field;
   };
   const checkbox=(title,current,update)=>{
    const field=create('input');field.type='checkbox';field.checked=current;field.onchange=()=>update(field.checked);
    const label=create('label');label.className='markdown-pdf-toc-setting';
    label.append(field,create('span',title));fields.append(label);
   };
   if(section==='font'){
    for(const [key,title,choices] of [['han','中文字体',CJK_FONTS],['latin','英文字体',WESTERN_FONTS],['digits','数字字体',WESTERN_FONTS]]){
     add(title,set(choices,value.fonts[key],selected=>{value.fonts[key]=selected;replace(value);}));
    }
    add('正文字号（pt）',size(value.size,6,36,0.5,input=>{value.size=input;replace(value);}));
    add('正文行距',size(value.lineHeight,1,2.5,0.01,input=>{value.lineHeight=input;replace(value);}));
   }else if(section==='heading'){
    add('标题级别',set(value.headings.map((_,i)=>[i,'H'+(i+1)]),headingLevel,selected=>{headingLevel=Number(selected);draw();}));
    const heading=value.headings[headingLevel];
    const update=(key,input)=>{heading[key]=input;replace(value);};
    add('标题字号（pt）',size(heading.size,6,88,0.5,input=>update('size',input)));
    add('标题字形',set(HEADING_STYLES,heading.style,input=>update('style',input)));
    add('标题对齐',set(ALIGNMENTS,heading.align,input=>update('align',input)));
    add('标题颜色',paint(heading.color||'#142b45',input=>update('color',input)));
    add('段前间距（pt）',size(heading.before,0,100,0.5,input=>update('before',input)));
    add('段后间距（pt）',size(heading.after,0,100,0.5,input=>update('after',input)));
   }else{
    const table=value.table,update=(key,input)=>{table[key]=input;replace(value);};
    add('表格边框',set(TABLE_STYLES,table.mode,input=>update('mode',input)));
    add('表格字号（pt）',size(table.size,6,26,0.5,input=>update('size',input)));
    add('单元格内边距（pt）',size(table.padding,1,24,0.5,input=>update('padding',input)));
    add('表格线宽（pt）',size(table.border,0.25,3,0.05,input=>update('border',input)));
    add('边框颜色',paint(table.borderColor,input=>update('borderColor',input)));
    add('表头底色',paint(table.headerFill,input=>update('headerFill',input)));
    add('隔行底色',paint(table.stripeFill,input=>update('stripeFill',input)));
    checkbox('表头加粗',table.headerBold,input=>update('headerBold',input));
    checkbox('使用隔行底色',table.stripe,input=>update('stripe',input));
    checkbox('跨页重复表头',table.repeat,input=>update('repeat',input));
   }
  }
  for(const [key,tab] of tabs)tab.onclick=()=>{section=key;draw();change();};
  useReference.onclick=()=>{replace(reference());draw();};
  reset.onclick=()=>{replace(null);draw();};
  draw();
  return {read:()=>settings,write:value=>{replace(value);draw();},section:()=>section};
 }

 window.AideMarkdownPdfStyle={normalize,STANDARDS,CSS_FONTS,applyPdfFonts,applyHtmlFonts,headingPaint,tablePaint,preview,controls};
})();
