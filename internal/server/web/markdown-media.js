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
    function button(title, action) {
      const b = document.createElement('button'); b.type='button'; b.className='quiet quiet-sm'; b.textContent=t(title); b.onclick=action; buttons.push(b); bar.append(b); return b;
    }
    function pick(images) {
      const range = capture();
      const input = document.createElement('input'); input.type='file'; input.multiple=true;
      if (images) input.accept='.png,.jpg,.jpeg,.gif,.webp,.svg,.bmp,.ico';
      input.onchange=() => { const files=[...input.files]; if(files.length) run(()=>addFiles(files, range)); }; input.click();
    }
    button('插入图片',()=>pick(true)); button('插入文件',()=>pick(false));
    button('插入链接', () => {
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
    button('插入 draw.io 图',()=>{ const range=capture();run(async()=>{const svg=await diagram('',t);const ref=await saveAsset(svg,'diagram.drawio.svg');commit(`\n![draw.io](${url(ref)})\n`,range);}); });
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
