/* Markdown media remain ordinary relative links and portable SVG diagrams. */
(() => {
  const mounted = new WeakMap();
  const label = text => text.replace(/[\\\[\]]/g, '\\$&').replace(/[\r\n]/g, ' ');
  const url = path => path.split('/').map(encodeURIComponent).join('/');
  const key = spec => `${spec.root || 'workspace'}:${spec.source || ''}:${spec.path}:${spec.wsId || ''}`;
  function insert(editor, text, range) {
    editor.focus(); editor.setRangeText(text, range.start, range.end, 'end'); editor.dispatchEvent(new Event('input', {bubbles:true}));
  }
  function mount(editor, options) {
    mounted.get(editor)?.();
    const spec = {...options.spec};
    if (editor.readOnly || !/\.(md|markdown)$/i.test(spec.path)) return;
    const bar = document.createElement('div'); bar.className = 'md-media-toolbar';
    const t = options.t || (s => s);
    const status = document.createElement('span'); status.className = 'md-media-status'; status.setAttribute('role', 'status');
    let busy = false;
    const buttons = [];
    const valid = () => !editor.readOnly && key(options.currentSpec()) === key(spec);
    const capture = () => ({start:editor.selectionStart, end:editor.selectionEnd, value:editor.value});
    const commit = (text, range) => {
      if (!valid() || editor.value !== range.value) throw new Error(t('附件已保存，文档已变化，请重新插入引用'));
      options.edit(); insert(editor, text, range); status.textContent = t('已插入，保存文档后生效');
    };
    const run = async fn => {
      if (busy || !valid()) return;
      busy = true; buttons.forEach(b => b.disabled = true); status.textContent = t('正在处理附件…');
      try { await fn(); } catch (e) { status.textContent = e.message; options.error(e.message); }
      finally { busy=false; buttons.forEach(b => b.disabled=false); }
    };
    const saveAsset = async (blob, name) => {
      if (!valid()) throw new Error(t('文档位置已变化，请重新打开'));
      const leaf = name.replace(/[\\/\x00-\x1f]/g, '_').slice(-160) || 'attachment';
      const relative = spec.path.split('/').pop().replace(/\.(md|markdown)$/i, '') + '.assets/' + crypto.randomUUID().slice(0,12) + '-' + leaf;
      const dir = spec.path.includes('/') ? spec.path.slice(0,spec.path.lastIndexOf('/')+1) : '';
      await options.upload(dir + relative, blob, spec); return relative;
    };
    const addFiles = async (files, range) => {
      const refs = [];
      for (const file of files) {
        if (/\.drawio$/i.test(file.name)) {
          const xml = await file.text();
          const svg = await diagram(xml, t);
          const ref = await saveAsset(svg, file.name + '.svg'); refs.push(`![${label(file.name)}](${url(ref)})`);
        } else {
          const name=file.name || ('pasted-image-'+Date.now()+'.png');
          const ref = await saveAsset(file, name);
          refs.push(`${/\.(png|jpe?g|gif|webp|svg|bmp|ico)$/i.test(name) ? '!' : ''}[${label(name)}](${url(ref)})`);
        }
      }
      commit('\n' + refs.join('\n\n') + '\n', range);
    };
    const icons = {
      image:'<rect x="3" y="3" width="18" height="18" rx="3"/><circle cx="8.5" cy="8.5" r="1.5"/><path d="m21 15-5-5L5 21"/>',
      file:'<path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"/><path d="M14 2v6h6M8 13h8M8 17h8"/>',
      link:'<path d="M10 13a5 5 0 0 0 7.1 0l3-3A5 5 0 0 0 13 2.9l-1.7 1.7"/><path d="M14 11a5 5 0 0 0-7.1 0l-3 3A5 5 0 0 0 11 21.1l1.7-1.7"/>',
      diagram:'<rect x="3" y="3" width="7" height="7" rx="1.5"/><rect x="14" y="14" width="7" height="7" rx="1.5"/><path d="M10 6.5h4a2 2 0 0 1 2 2v5.5M7 10v4a2 2 0 0 0 2 2h5"/>'
    };
    function button(title, icon, action) {
      const b = document.createElement('button'); b.type='button'; b.className='md-tool-icon'; b.title=t(title); b.setAttribute('aria-label',t(title));
      const svg=document.createElementNS('http://www.w3.org/2000/svg','svg');svg.setAttribute('class','ui-icon');svg.setAttribute('viewBox','0 0 24 24');svg.setAttribute('fill','none');svg.setAttribute('stroke','currentColor');svg.setAttribute('stroke-width','1.7');svg.setAttribute('stroke-linecap','round');svg.setAttribute('stroke-linejoin','round');svg.setAttribute('aria-hidden','true');svg.innerHTML=icons[icon];
      b.append(svg);b.onclick=action;buttons.push(b);bar.append(b);return b;
    }
    function pick(images) {
      const range = capture();
      const input = document.createElement('input'); input.type='file'; input.multiple=true;
      if (images) input.accept='.png,.jpg,.jpeg,.gif,.webp,.svg,.bmp,.ico';
      input.onchange=() => { const files=[...input.files]; if(files.length) run(()=>addFiles(files, range)); }; input.click();
    }
    button('插入图片','image',()=>pick(true)); button('插入文件','file',()=>pick(false));
    button('插入链接','link', () => {
      const range=capture(); const dlg=document.createElement('dialog'); dlg.className='md-link-dialog';
      const heading=document.createElement('h3');heading.textContent=t('插入文件或图片链接');
      const path=document.createElement('input');path.placeholder=t('相对路径或 https:// 链接');path.setAttribute('aria-label',path.placeholder);
      const caption=document.createElement('input');caption.placeholder=t('显示名称');caption.setAttribute('aria-label',caption.placeholder);
      const image=document.createElement('input');image.type='checkbox';const imageLabel=document.createElement('label');imageLabel.append(image,document.createTextNode(t('作为图片插入')));
      const ok=document.createElement('button');ok.type='button';ok.className='primary';ok.textContent=t('插入');
      const cancel=document.createElement('button');cancel.type='button';cancel.className='quiet';cancel.textContent=t('取消');cancel.onclick=()=>dlg.close();
      ok.onclick=()=>{const value=path.value.trim();if(!value || /^(?:javascript|data|file|vbscript):/i.test(value) || value.startsWith('//')) { path.focus(); return; }
        const encoded=/^https?:/i.test(value)?encodeURI(value).replace(/[()]/g,c=>'%'+c.charCodeAt(0).toString(16)):url(value);
        try {commit(`${image.checked?'\n!':''}[${label(caption.value || value.split('/').pop())}](${encoded})${image.checked?'\n':''}`,range);dlg.close();} catch(e){options.error(e.message);} };
      dlg.append(heading,path,caption,imageLabel,ok,cancel);dlg.onclose=()=>dlg.remove();document.body.append(dlg);dlg.showModal();path.focus();
    });
    button('插入 draw.io 图','diagram',()=>{ const range=capture();run(async()=>{const svg=await diagram('',t);const ref=await saveAsset(svg,'diagram.drawio.svg');commit(`\n![draw.io](${url(ref)})\n`,range);}); });
    // CommonMark/GFM structures stay one click away; each icon carries a
    // visible-on-focus tooltip and a complete screen-reader label.
    const templateIcons={
      paragraph:'<path d="M5 5h14M5 9h14M5 13h9M5 17h11"/>',
      h1:'<path d="M4 5v14m0-7h7m0-7v14M16 8v8m-2-6 2-2 2 2m-3 6h3"/>',
      h2:'<path d="M4 5v14m0-7h7m0-7v14M14 9a2.5 2.5 0 1 1 5 0c0 2-5 3-5 6h5"/>',
      h3:'<path d="M4 5v14m0-7h7m0-7v14M14 9a2.5 2.5 0 1 1 5 0c0 1-.6 1.8-1.5 2.2A2.5 2.5 0 1 1 14 14"/>',
      h4:'<path d="M4 5v14m0-7h7m0-7v14M18 5v7h4m0-7v14"/>',
      h5:'<path d="M4 5v14m0-7h7m0-7v14M21 5h-6v5h3a3 3 0 1 1-3 4"/>',
      h6:'<path d="M4 5v14m0-7h7m0-7v14M20 5h-2a3 3 0 0 0-3 3v7a3 3 0 1 0 6 0v-1a3 3 0 0 0-6 0"/>',
      bold:'<path d="M6 4h7a4 4 0 0 1 0 8H6zM6 12h8a4 4 0 0 1 0 8H6z"/>',
      italic:'<path d="M19 4h-9M14 20H5m9-16-5 16"/>',
      strike:'<path d="M4 12h16M7 7c.7-2 2.5-3 5-3 3 0 5 1.5 5 3.5M7 17c.8 2 2.5 3 5 3 2.4 0 4.2-1 5-3"/>',
      inline:'<path d="m8 8-4 4 4 4m8-8 4 4-4 4M14 5l-4 14"/>',
      code:'<path d="m8 8-4 4 4 4m8-8 4 4-4 4M14 5l-4 14"/><path d="M3 21h18"/>',
      mermaid:'<rect x="2.5" y="3" width="6" height="5" rx="1"/><rect x="15.5" y="16" width="6" height="5" rx="1"/><rect x="15.5" y="3" width="6" height="5" rx="1"/><path d="M8.5 5.5h4a3 3 0 0 1 3 3V16M8.5 6v11.5a2.5 2.5 0 0 0 2.5 2.5h4.5"/>',
      table:'<rect x="3" y="4" width="18" height="16" rx="1.5"/><path d="M3 10h18M9 4v16m6-16v16"/>',
      link:'<path d="M10 13.5a4 4 0 0 0 5.7 0l3-3a4 4 0 0 0-5.7-5.7l-1.7 1.7M14 10.5a4 4 0 0 0-5.7 0l-3 3a4 4 0 0 0 5.7 5.7l1.7-1.7"/>',
      image:'<rect x="3" y="4" width="18" height="16" rx="2"/><circle cx="8.5" cy="9" r="1.5"/><path d="m4 17 5-5 3 3 3-4 5 6"/>',
      footnote:'<path d="M7 5v14m0-7h8M15 5v14M18 7h3v4h-3zM18 14h3v5h-3z"/>',
      alert:'<path d="M12 3 2.8 19h18.4L12 3Z"/><path d="M12 9v4m0 3h.01"/>',
      details:'<path d="m8 10 4 4 4-4"/><path d="M4 4h16v16H4zM7 17h10"/>',
      math:'<path d="M4 6h16M4 18h16M8 8l8 8m0-8-8 8"/>',
      frontmatter:'<path d="M4 4h16M4 20h16M7 9h10m-10 3h7m-7 3h10"/><path d="M4 2v4m16-4v4M4 18v4m16-4v4"/>',
      quote:'<path d="M4 10h6v7H3v-5l3-5m8 3h7v7h-7v-5l3-5"/>',
      unordered:'<circle cx="5" cy="6" r="1"/><circle cx="5" cy="12" r="1"/><circle cx="5" cy="18" r="1"/><path d="M10 6h10M10 12h10M10 18h10"/>',
      ordered:'<path d="M9 6h11M9 12h11M9 18h11M4 5v3m-1 0h3m-3 6c0-1.5 3-1.5 3 0 0 .8-3 2.5-3 3h3"/>',
      task:'<rect x="3" y="4" width="6" height="6" rx="1"/><path d="m4.5 7 1.3 1.3L8 6m4 1h9M3 15h6v6H3zm9 3h9"/>',
      rule:'<path d="M3 12h18"/>'
    };
    const templates=[
      ['插入段落','段落用途','paragraph','段落内容\n\n','段落内容'],
      ['插入一级标题','一级标题用途','h1','# 标题\n','标题'],
      ['插入二级标题','二级标题用途','h2','## 标题\n','标题'],
      ['插入三级标题','三级标题用途','h3','### 标题\n','标题'],
      ['插入四级标题','四级标题用途','h4','#### 标题\n','标题'],
      ['插入五级标题','五级标题用途','h5','##### 标题\n','标题'],
      ['插入六级标题','六级标题用途','h6','###### 标题\n','标题'],
      ['插入粗体','粗体用途','bold','**粗体文本**','粗体文本'],
      ['插入斜体','斜体用途','italic','*斜体文本*','斜体文本'],
      ['插入删除线','删除线用途','strike','~~删除线文本~~','删除线文本'],
      ['插入行内代码','行内代码用途','inline','`代码`','代码'],
      ['插入代码块','代码块用途','code','```语言\n代码\n```\n','代码'],
      ['插入 Mermaid 流程图','Mermaid 流程图用途','mermaid','```mermaid\nflowchart TD\n    A[开始] --> B[步骤]\n    B --> C[结束]\n```\n','步骤'],
      ['插入表格','表格用途','table','| 标题 1 | 标题 2 |\n| --- | --- |\n| 内容 1 | 内容 2 |\n| 内容 1 | 内容 2 |\n','标题 1'],
      ['插入链接语法','链接语法用途','link','[链接文本](https://example.com)','链接文本'],
      ['插入图片语法','图片语法用途','image','![替代文字](images/image.png)','替代文字'],
      ['插入引用','引用用途','quote','> 引用内容\n','引用内容'],
      ['插入无序列表','无序列表用途','unordered','- 列表项\n- 列表项\n','列表项'],
      ['插入有序列表','有序列表用途','ordered','1. 第一步\n2. 第二步\n','第一步'],
      ['插入任务列表','任务列表用途','task','- [ ] 待办事项\n- [x] 已完成事项\n','待办事项'],
      ['插入分隔线','分隔线用途','rule','\n---\n',''],
      ['插入脚注','脚注用途','footnote','需要补充说明的内容[^1]\n\n[^1]: 脚注内容\n','脚注内容'],
      ['插入提示块','提示块用途','alert','> [!NOTE]\n> 重要提示\n','重要提示'],
      ['插入折叠块','折叠块用途','details','<details>\n<summary>点击展开</summary>\n\n折叠内容\n</details>\n','点击展开'],
      ['插入数学公式','数学公式用途','math','$$\nE = mc^2\n$$\n','E = mc^2'],
      ['插入文档元数据','文档元数据用途','frontmatter','---\ntitle: 文档标题\ndescription: 文档简介\ntags:\n  - 标签\n---\n\n','文档标题']
    ];
    for(const [name,description,icon,snippet,placeholder] of templates){
      const item=document.createElement('button');item.type='button';item.className='md-tool-icon md-template-tool';
      if(!bar.querySelector('.md-template-tool'))item.classList.add('md-template-first');
      const label=t(name),help=t(description),accessible=`${label}。${help}`;
      item.title=accessible;item.setAttribute('aria-label',accessible);
      item.addEventListener('mouseenter',()=>{status.textContent=accessible;});
      item.addEventListener('focus',()=>{status.textContent=accessible;});
      item.addEventListener('mouseleave',()=>{status.textContent='';});
      item.addEventListener('blur',()=>{status.textContent='';});
      item.addEventListener('mousedown',e=>{if(e.button===0)e.preventDefault();});
      const svg=document.createElementNS('http://www.w3.org/2000/svg','svg');svg.setAttribute('class','ui-icon');svg.setAttribute('viewBox','0 0 24 24');svg.setAttribute('fill','none');svg.setAttribute('stroke','currentColor');svg.setAttribute('stroke-width','1.7');svg.setAttribute('stroke-linecap','round');svg.setAttribute('stroke-linejoin','round');svg.setAttribute('aria-hidden','true');svg.innerHTML=templateIcons[icon];
      item.append(svg);
      item.onclick=()=>{
        const range=capture();
        try{
          if(!valid()||editor.value!==range.value)throw new Error(t('文档内容已变化，请重新插入'));
          options.edit();editor.focus();editor.setRangeText(snippet,range.start,range.end,'end');editor.dispatchEvent(new Event('input',{bubbles:true}));
          const start=placeholder?range.start+snippet.indexOf(placeholder):-1;
          if(start>=range.start)editor.setSelectionRange(start,start+placeholder.length);
          status.textContent=t('已插入 Markdown 模板');
        }catch(e){status.textContent=e.message;options.error(e.message);}
      };
      buttons.push(item);bar.append(item);
    }
    bar.append(status); const anchor=editor.parentNode.classList.contains('code-wrapper')?editor.parentNode:editor; anchor.parentNode.insertBefore(bar,anchor);
    const paste = e => {const files=[...(e.clipboardData?.files || [])]; if(files.length && valid()){e.preventDefault();e.stopPropagation();const range=capture();run(()=>addFiles(files,range));} };
    const drop = e => {const files=[...(e.dataTransfer?.files || [])];if(files.length && valid()){e.preventDefault();e.stopPropagation();const range=capture();run(()=>addFiles(files,range));} };
    const over = e => {if(e.dataTransfer?.types.includes('Files')) {e.preventDefault();e.stopPropagation();}};
    editor.addEventListener('paste',paste);editor.addEventListener('drop',drop);editor.addEventListener('dragover',over);
    mounted.set(editor,()=>{bar.remove();editor.removeEventListener('paste',paste);editor.removeEventListener('drop',drop);editor.removeEventListener('dragover',over);});
  }
  function dispose(editor) { mounted.get(editor)?.();mounted.delete(editor); }
  function diagram(xml, t = s => s) {
    return new Promise((resolve,reject)=>{
      const dlg=document.createElement('dialog');dlg.className='md-diagram-dialog';
      const head=document.createElement('header');const title=document.createElement('strong');title.textContent=t('draw.io 图表');
      const close=document.createElement('button');close.type='button';close.className='quiet';close.textContent=t('取消');close.onclick=()=>dlg.close();head.append(title,close);
      const status=document.createElement('span');status.setAttribute('role','status');status.textContent=t('正在加载…');head.append(status);
      const frame=document.createElement('iframe');frame.title=t('draw.io 图表');frame.src='/vendor/drawio/?embed=1&proto=json&spin=1';dlg.append(head,frame);document.body.append(dlg);
      let settled=false, exporting=false, timer;
      const finish=(err,blob)=>{if(settled)return;settled=true;clearTimeout(timer);window.removeEventListener('message',receive);dlg.close();dlg.remove();err?reject(err):resolve(blob);};
      const send=data=>frame.contentWindow.postMessage(JSON.stringify(data),location.origin);
      const receive=async e=>{
        if(e.source!==frame.contentWindow || e.origin!==location.origin)return;
        let msg;try{msg=JSON.parse(e.data);}catch(_){return;}
        if(msg.event==='init') {clearTimeout(timer);status.textContent='';send({action:'load',xml:xml||'<mxfile><diagram name="Page-1"><mxGraphModel><root><mxCell id="0"/><mxCell id="1" parent="0"/></root></mxGraphModel></diagram></mxfile>',title:t('draw.io 图表')});}
        else if(msg.event==='save' && !exporting){exporting=true;status.textContent=t('正在生成图表预览…');send({action:'export',format:'xmlsvg',xml:msg.xml,embedImages:true});timer=setTimeout(()=>finish(new Error(t('图表导出超时，请重试'))),30000);}
        else if(msg.event==='export' && exporting){try{
          const data=String(msg.data||'');if(!data.startsWith('data:image/svg+xml'))throw new Error(t('图表未返回 SVG 预览'));
          const comma=data.indexOf(',');const svg=data.slice(0,comma).includes(';base64')?new TextDecoder().decode(Uint8Array.from(atob(data.slice(comma+1)),c=>c.charCodeAt(0))):decodeURIComponent(data.slice(comma+1));
          if(!svg.includes('<svg'))throw new Error(t('图表未返回 SVG 预览'));finish(null,new Blob([svg],{type:'image/svg+xml'}));
        }catch(err){finish(err);}}
        else if(msg.event==='exit')dlg.close();
      };
      dlg.onclose=()=>{if(!settled)finish(new Error(t('已取消插入')));};window.addEventListener('message',receive);dlg.showModal();
      timer=setTimeout(()=>finish(new Error(t('draw.io 加载超时'))),20000);
    });
  }
  window.AideMarkdownMedia={mount,dispose,diagram};
})();
