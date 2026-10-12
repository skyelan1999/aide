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
    // Common Markdown structures are inserted as editable source at the caret.
    const templateWrap=document.createElement('div');templateWrap.className='md-template-wrap';
    const templateButton=document.createElement('button');templateButton.type='button';templateButton.className='md-tool-icon md-template-trigger';templateButton.title=t('插入 Markdown 示例');templateButton.setAttribute('aria-label',t('插入 Markdown 示例'));templateButton.setAttribute('aria-haspopup','menu');templateButton.setAttribute('aria-expanded','false');
    const templateSvg=document.createElementNS('http://www.w3.org/2000/svg','svg');templateSvg.setAttribute('class','ui-icon');templateSvg.setAttribute('viewBox','0 0 24 24');templateSvg.setAttribute('fill','none');templateSvg.setAttribute('stroke','currentColor');templateSvg.setAttribute('stroke-width','1.7');templateSvg.setAttribute('stroke-linecap','round');templateSvg.setAttribute('stroke-linejoin','round');templateSvg.setAttribute('aria-hidden','true');templateSvg.innerHTML='<path d="M4 5h16M4 12h16M4 19h16"/><path d="M8 3v4m8-4v4M8 10v4m8-4v4M8 17v4m8-4v4"/>';templateButton.append(templateSvg,document.createTextNode(t('MD 示例')));
    const menu=document.createElement('div');menu.className='md-template-menu';menu.setAttribute('role','menu');menu.setAttribute('aria-label',t('Markdown 常用结构'));menu.hidden=true;
    const templates=[
      ['插入表格','table','| 标题 1 | 标题 2 |\n| --- | --- |\n| 内容 | 内容 |\n| 内容 | 内容 |\n'],
      ['插入代码块','code','```\n代码\n```\n'],
      ['插入标题','heading','## 标题\n'],
      ['插入引用','quote','> 引用内容\n'],
      ['插入无序列表','list','- 列表项\n- 列表项\n'],
      ['插入任务列表','task','- [ ] 待办事项\n- [x] 已完成事项\n'],
      ['插入分隔线','rule','\n---\n']
    ];
    const templateButtons=[];
    function closeMenu(){menu.hidden=true;templateButton.setAttribute('aria-expanded','false');}
    for(const [title,icon,snippet] of templates){
      const item=document.createElement('button');item.type='button';item.className='md-template-item';item.setAttribute('role','menuitem');item.textContent=t(title);item.onclick=()=>{
        const range=templateRange||capture();
        try{if(!valid()||editor.value!==range.value)throw new Error(t('文档内容已变化，请重新插入'));options.edit();editor.focus();editor.setRangeText(snippet,range.start,range.end,'end');editor.dispatchEvent(new Event('input',{bubbles:true}));const start=range.start+snippet.indexOf(icon==='table'?'内容':icon==='code'?'代码':icon==='heading'?'标题':icon==='quote'?'引用内容':icon==='task'?'待办事项':icon==='list'?'列表项':'');if(icon!=='rule'&&start>=range.start){const end=start+(icon==='table'?2:icon==='code'?2:icon==='heading'?2:icon==='quote'?4:icon==='task'?4:3);editor.setSelectionRange(start,end);}status.textContent=t('已插入 Markdown 示例');}catch(e){status.textContent=e.message;options.error(e.message);}closeMenu();templateButton.focus();
      };menu.append(item);templateButtons.push(item);
    }
    let templateRange=null;
    templateButton.onclick=()=>{if(!menu.hidden){closeMenu();return;}templateRange=capture();menu.hidden=false;templateButton.setAttribute('aria-expanded','true');templateButtons[0]?.focus();};
    templateButton.onkeydown=e=>{if(e.key==='ArrowDown'){e.preventDefault();templateRange=capture();menu.hidden=false;templateButton.setAttribute('aria-expanded','true');templateButtons[0]?.focus();}};
    menu.addEventListener('keydown',e=>{const i=templateButtons.indexOf(document.activeElement);if(e.key==='Escape'){e.preventDefault();closeMenu();templateButton.focus();}else if(e.key==='ArrowDown'||e.key==='ArrowUp'){e.preventDefault();templateButtons[(i+(e.key==='ArrowDown'?1:-1)+templateButtons.length)%templateButtons.length].focus();}});
    const outsideTemplateClick=e=>{if(!templateWrap.contains(e.target))closeMenu();};
    document.addEventListener('pointerdown',outsideTemplateClick,true);
    templateWrap.append(templateButton,menu);bar.append(templateWrap);buttons.push(templateButton);
    bar.append(status); const anchor=editor.parentNode.classList.contains('code-wrapper')?editor.parentNode:editor; anchor.parentNode.insertBefore(bar,anchor);
    const paste = e => {const files=[...(e.clipboardData?.files || [])]; if(files.length && valid()){e.preventDefault();e.stopPropagation();const range=capture();run(()=>addFiles(files,range));} };
    const drop = e => {const files=[...(e.dataTransfer?.files || [])];if(files.length && valid()){e.preventDefault();e.stopPropagation();const range=capture();run(()=>addFiles(files,range));} };
    const over = e => {if(e.dataTransfer?.types.includes('Files')) {e.preventDefault();e.stopPropagation();}};
    editor.addEventListener('paste',paste);editor.addEventListener('drop',drop);editor.addEventListener('dragover',over);
    mounted.set(editor,()=>{bar.remove();document.removeEventListener('pointerdown',outsideTemplateClick,true);editor.removeEventListener('paste',paste);editor.removeEventListener('drop',drop);editor.removeEventListener('dragover',over);});
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
