'use strict';
(() => {
  const $ = id => document.getElementById(id), bridge = AideWorkbenchClient.create('terminal');
  const panes = new Map(), pendingInput = new Map();
  let sessions = [], selected = null, queue = Promise.resolve(), polling = false, paused = true, connected = false, stopped = false, exportId = 0;
  const labels = {starting:'启动中',running:'运行中',exited:'已结束',interrupted:'后台已重启',error:'连接异常'};
  const current = () => sessions.find(s => s.id === selected);
  const message = text => { $('notice').hidden = false; $('notice').querySelector('span').textContent = text; };
  function enqueue(tool, args={}) {
    const request = queue.then(async () => {
      if (stopped) throw Error('工作台已关闭');
      if (!connected) throw Error('当前 Aide 尚未建立所需的常驻工作台通信，请更新 Aide 后重新打开');
      const reply = await bridge.call('terminal_session_'+tool,args);
      if (reply.status !== 'result') throw Error('操作未完成，请检查 Aide 中的待审批提案');
      return reply.result;
    });
    queue = request.catch(() => {});
    return request;
  }
  function fail(error) { message(error.message || String(error)); }
  function confirmAction(text){return new Promise(resolve=>{const dialog=$('confirm-dialog');$('confirm-message').textContent=text;dialog.onclose=()=>resolve(false);$('confirm-cancel').onclick=()=>dialog.close();$('confirm-accept').onclick=()=>{resolve(true);dialog.close();};dialog.showModal();$('confirm-cancel').focus();});}
  function bytes(base64) { return Uint8Array.from(atob(base64), c => c.charCodeAt(0)); }
  function terminal(id) {
    if (panes.has(id)) return panes.get(id);
    const element = document.createElement('div');element.className='terminal-pane';element.hidden=true;$('terminal').append(element);
    const term = new Terminal({cursorBlink:true,fontSize:13,fontFamily:'"Cascadia Code","SFMono-Regular",Consolas,monospace',scrollback:10000,
      allowProposedApi:false,screenReaderMode:true,theme:{background:'#0b1018',foreground:'#d5e3ef',cursor:'#6fe0ba',selectionBackground:'#2c5062',black:'#152130',brightBlack:'#61768f',red:'#f18b9a',green:'#85d7af',yellow:'#e5cb8b',blue:'#7bb8f5',magenta:'#c6a1f0',cyan:'#79d7dc',white:'#dde5ef'}});
    const fit = new FitAddon.FitAddon(), search = new SearchAddon.SearchAddon();term.loadAddon(fit);term.loadAddon(search);term.open(element);
    // Escape sequence responses are sent only while this session is selected and running.
    term.onData(data => {
      if (paused || selected !== id || sessions.find(s=>s.id===id)?.status !== 'running') return;
      let pending = pendingInput.get(id);
      if (!pending) { pending={text:'',timer:null};pendingInput.set(id,pending); }
      pending.text += data;
      if (pending.text.length > 65536) { pending.text='';paused=true;message('输入过多，已暂停发送；请检查设备后重新连接工作台');return; }
      if (!pending.timer) pending.timer=setTimeout(()=>flushInput(id),70);
    });
    // Do not let a terminal silently request browser clipboard access (OSC 52).
    term.parser.registerOscHandler(52,()=>true);
    const state={term,fit,search,element,offset:0,cols:0,rows:0};panes.set(id,state);return state;
  }
  async function flushInput(id) {
    const pending=pendingInput.get(id);if(!pending)return;
    pending.timer=null;const input=pending.text;pending.text='';
    if(paused||!input)return;
    try {
      for(let start=0;start<input.length;start+=4096) {
        if(paused)break;
        await enqueue('input',{id,data:input.slice(start,start+4096)});
      }
    } catch(error) { paused=true;for(const p of pendingInput.values())p.text='';message('输入结果待核实，不会自动重发。'+error.message); }
  }
  function render() {
    $('session-count').textContent=sessions.length+' / 32';
    $('sessions').replaceChildren();$('tabs').replaceChildren();
    for(const rec of sessions) {
      const button=document.createElement('button');button.type='button';button.className='session'+(rec.id===selected?' active':'');button.dataset.sessionId=rec.id;
      const icon=document.createElement('span');icon.className='icon';icon.textContent=rec.kind==='ssh'?'↗':'›_';
      const body=document.createElement('span'),name=document.createElement('strong'),detail=document.createElement('small');
      name.textContent=rec.name;detail.textContent=(rec.kind==='ssh'?rec.username+'@'+rec.host:'LOCAL · '+rec.cwd)+' · '+(labels[rec.status]||rec.status);body.append(name,detail);button.append(icon,body);button.onclick=()=>select(rec.id);$('sessions').append(button);
      const tab=document.createElement('button');tab.textContent=(rec.status==='running'?'● ':'○ ')+rec.name;tab.className=rec.id===selected?'active':'';tab.setAttribute('aria-current',String(rec.id===selected));tab.onclick=()=>select(rec.id);$('tabs').append(tab);
    }
    const rec=current(), running=rec?.status==='running';
    $('empty').hidden=!!rec;
    for(const id of ['new-top','new-empty','new-session'])$(id).disabled=!connected;
    for(const id of ['find','logs','logging'])$(id).disabled=!rec;
    $('interrupt').disabled=!running;$('close-session').disabled=!running;$('delete-session').disabled=!rec||running||rec.status==='starting';
    $('connection').textContent=rec?(rec.kind==='ssh'?rec.username+'@'+rec.host+':'+rec.port:'LOCAL · '+rec.cwd):'尚未选择会话';
    $('logging').textContent='日志：'+(rec?.logging?'开':'关');
    $('status').textContent=rec?(labels[rec.status]||rec.status)+' · '+rec.name:'就绪 · 创建本地或 SSH 会话';
    $('log-status').textContent=rec?.logError || (rec?.logging?'● 输出日志已开启':'输出日志已暂停');
    $('log-status').style.color=rec?.logError?'var(--danger)':'';
    for(const [id,pane] of panes) { pane.element.hidden=id!==selected;pane.term.options.disableStdin=paused||sessions.find(s=>s.id===id)?.status!=='running'; }
  }
  function select(id) {
    selected=id;terminal(id);render();document.body.classList.remove('sidebar-open');
    requestAnimationFrame(()=>{fitSelected();panes.get(id)?.term.focus();});
    if(!paused)poll();
  }
  let resizeTimer;
  function fitSelected() {
    const rec=current(),pane=panes.get(selected);if(!pane||!rec)return;
    pane.fit.fit();$('dimensions').textContent=pane.term.cols+' × '+pane.term.rows;
    const cols=Math.max(20,Math.min(400,pane.term.cols)),rows=Math.max(5,Math.min(200,pane.term.rows));
    if(cols===pane.cols&&rows===pane.rows)return;
    pane.cols=cols;pane.rows=rows;clearTimeout(resizeTimer);
    resizeTimer=setTimeout(()=>{if(!paused&&rec.status==='running')enqueue('resize',{id:rec.id,cols,rows}).catch(fail);},180);
  }
  async function poll() {
    if(polling||paused||stopped)return;
    polling=true;
    const id=selected,pane=panes.get(id);
    try {
      const result=await enqueue('poll',id?{id,offset:pane?.offset||0}:{});
      const signature=JSON.stringify(sessions);sessions=result.sessions;
      if(result.output&&pane) {
        if(result.output.reset){pane.term.reset();pane.term.writeln('\x1b[33m[恢复保留的输出尾部；完整记录请查看磁盘日志]\x1b[0m');}
        pane.term.write(bytes(result.output.data));pane.offset=result.output.offset;
      }
      if(!selected&&sessions.length){select(sessions[0].id);}else if(signature!==JSON.stringify(sessions))render();
    } catch(error) { paused=true;render();message('后台通信已暂停，已有会话可能仍在运行。'+error.message+'；请检查后重新打开工作台。'); }
    finally {polling=false;}
  }
  setInterval(poll,650);
  $('reconnect').onclick=async()=>{paused=false;$('notice').hidden=true;await poll();render();};
  bridge.onEvent(event=>{if(event.event==='scope.revoked'){paused=true;render();message('工作台授权已失效，请重新打开。');}});
  function openCreate(){$('create-error').textContent='';$('create-dialog').showModal();$('create-form').elements.name.focus();}
  for(const id of ['new-session','new-top','new-empty'])$(id).onclick=openCreate;
  $('cancel-create').onclick=()=>$('create-dialog').close();
  $('kind').onchange=()=>{const ssh=$('kind').value==='ssh';$('ssh-fields').hidden=!ssh;for(const name of ['host','username'])$('create-form').elements[name].required=ssh;};
  $('create-submit').onclick=async event=>{
    event.preventDefault();const form=$('create-form');if(!form.reportValidity())return;const values=new FormData(form),args={kind:values.get('kind'),name:values.get('name'),cwd:values.get('cwd')||'.',logging:form.elements.logging.checked};
    if(args.kind==='ssh')Object.assign(args,{host:values.get('host'),port:Number(values.get('port')),username:values.get('username'),identityFile:values.get('identityFile')||undefined});
    $('create-submit').disabled=true;$('create-error').textContent='';
    try {const result=await enqueue('create',args);sessions.push(result.session);$('create-dialog').close();paused=false;select(result.session.id);}
    catch(error){$('create-error').textContent=error.message;}
    finally{$('create-submit').disabled=false;}
  };
  $('create-form').onkeydown=event=>{if(event.key==='Enter'&&event.target.tagName==='INPUT'&&event.target.type!=='checkbox'){event.preventDefault();$('create-submit').click();}};
  $('interrupt').onclick=()=>{if(selected)enqueue('input',{id:selected,data:'\x03'}).catch(fail);};
  $('close-session').onclick=async()=>{
    const rec=current();if(!rec||!(await confirmAction('结束“'+rec.name+'”的连接？正在运行的设备命令可能中断，日志会保留。')))return;
    try{const pending=pendingInput.get(rec.id);if(pending){clearTimeout(pending.timer);pending.timer=null;pending.text='';}await enqueue('close',{id:rec.id});await poll();}catch(error){fail(error);}
  };
  $('delete-session').onclick=async()=>{
    const rec=current();if(!rec||!(await confirmAction('永久删除“'+rec.name+'”的会话记录和全部日志？')))return;
    try{await enqueue('delete',{id:rec.id});panes.get(rec.id)?.term.dispose();panes.get(rec.id)?.element.remove();panes.delete(rec.id);sessions=sessions.filter(s=>s.id!==rec.id);selected=null;if(sessions.length)select(sessions[0].id);else render();}
    catch(error){fail(error);}
  };
  $('logging').onclick=async()=>{const rec=current();if(!rec)return;try{const result=await enqueue('logging',{id:rec.id,enabled:!rec.logging});Object.assign(rec,result.session);render();}catch(error){fail(error);}};
  $('find').onclick=()=>{$('search-bar').hidden=false;$('search-input').focus();fitSelected();};
  $('search-close').onclick=()=>{$('search-bar').hidden=true;panes.get(selected)?.search.clearDecorations();fitSelected();};
  const find=forward=>{const pane=panes.get(selected);if(pane)pane.search[forward?'findNext':'findPrevious']($('search-input').value,{incremental:false});};
  $('search-input').oninput=()=>find(true);$('search-next').onclick=()=>find(true);$('search-prev').onclick=()=>find(false);
  $('search-input').onkeydown=event=>{if(event.key==='Enter'){event.preventDefault();find(!event.shiftKey);}};
  $('logs').onclick=async()=>{
    const rec=current();if(!rec)return;const id=rec.id;
    $('logs-dialog').showModal();$('log-files').textContent='正在读取…';$('log-progress').textContent='';
    try {
      const result=await enqueue('logs',{id});$('log-files').replaceChildren();
      if(!result.files.length)$('log-files').textContent='暂无日志。只有开启日志后接收到的终端输出会保存。';
      for(const file of result.files) {
        const row=document.createElement('div');row.className='log-file';
        const label=document.createElement('span');label.textContent=(file.index?'历史分卷 '+file.index:'当前日志')+' · '+(file.bytes/1024).toFixed(1)+' KiB';
        const button=document.createElement('button');button.textContent='准备下载';
        button.onclick=async()=>{
          for(const b of $('log-files').querySelectorAll('button'))b.disabled=true;
          const chunks=[];let offset=0,identity,target=file.bytes;
          try {
            do {const page=await enqueue('logs',{id,index:file.index,offset,identity});identity=page.identity;
              const chunk=bytes(page.data).subarray(0,Math.max(0,target-offset));chunks.push(chunk);
              if(!chunk.length&&offset<target)throw Error('日志文件已变化，请重新打开日志列表');
              offset+=chunk.length;$('log-progress').textContent='准备日志 '+Math.min(100,Math.round(offset/Math.max(1,target)*100))+'%';
            }while(offset<target);
            const output=new Uint8Array(offset);let position=0;for(const chunk of chunks){output.set(chunk,position);position+=chunk.length;}
            if(!output.length)throw Error('日志文件为空');
            window.parent.postMessage({type:'aide.workbench.export',protocol:1,pluginId:'terminal',requestId:'export_'+(++exportId),name:'terminal-'+id+(file.index?'-'+file.index:'')+'.log',bytes:output.buffer},'*',[output.buffer]);
            $('log-progress').textContent='已准备。请点击 Aide 顶部“保存文件”完成下载。';
          }catch(error){$('log-progress').textContent=error.message;}
          finally{for(const b of $('log-files').querySelectorAll('button'))b.disabled=false;}
        };row.append(label,button);$('log-files').append(row);
      }
    }catch(error){$('log-files').textContent=error.message;}
  };
  $('close-logs').onclick=()=>$('logs-dialog').close();
  $('sidebar-toggle').onclick=()=>document.body.classList.toggle('sidebar-open');
  new ResizeObserver(fitSelected).observe($('terminal-area'));
  window.addEventListener('message',event=>{
    if(event.source!==window.parent)return;
    const m=event.data;
    if(m?.type==='aide.workbench.export.status'&&m.pluginId==='terminal'&&m.status==='error')$('log-progress').textContent=m.error;
    if(m?.type==='aide.workbench.theme'&&m.protocol===1){
      // Terminal stays dark for device color fidelity; surrounding chrome follows host theme.
      const colors=m.colors||{};
      for(const key of ['bg','panel','surface','text','muted','line'])if(typeof colors[key]==='string'&&CSS.supports('color',colors[key]))document.documentElement.style.setProperty('--'+key,colors[key]);
      document.documentElement.style.colorScheme=m.theme==='light'?'light':'dark';document.documentElement.dataset.theme=m.theme;
    }
  });
  window.parent.postMessage({type:'aide.workbench.theme.ready',protocol:1},'*');
  window.addEventListener('pagehide',()=>{stopped=true;bridge.close();for(const p of pendingInput.values())clearTimeout(p.timer);});
  render();bridge.connect().then(info=>{
    if(!['bridge.renew','daemon.detach'].every(x=>info.capabilities?.includes(x)))throw Error('请先更新 Aide：此工作台需要支持通信续期和常驻会话分离的宿主版本。');
    connected=true;paused=false;render();return poll();
  }).catch(error=>{paused=true;render();fail(error);});
})();
