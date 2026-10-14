#!/usr/bin/env python3
# -*- coding: utf-8 -*-
from pathlib import Path
import re
root=Path(__file__).resolve().parents[2];web=root/'internal/server/web';source=(web/'app.js').read_text();functions=[]
for name in ['fileEntryName','fileIconMarkup','renderFileEntries','beginInlineRename']:
 m=re.search(r'^function '+name+r'\(',source,re.M);functions.append(source[m.start():source.index('\n}',m.start())+2])
css='\n'.join((web/p).read_text() for p in ['themes/dark/tokens.css','themes/light/tokens.css','style.css','macos.css'])
html='''<!doctype html><html data-theme="dark"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>Linux 文件身份小字验收</title><style>'''+css+'''\nbody{display:block;padding:30px;overflow:auto}h1{font-size:22px}.fixture-tools{display:flex;gap:10px;margin-bottom:24px}button{padding:8px}.fixture-panel{width:320px;max-width:calc(100vw - 60px);background:var(--panel);border:1px solid var(--line);padding:12px;border-radius:12px}#files{width:100%}.fixture-note{color:var(--muted);font-size:12px;max-width:650px;line-height:1.8}</style><h1>工作区文件 · Linux 身份与权限</h1><p class="fixture-note">使用实际文件行渲染器与重命名逻辑的合成数据验收；不连接8097，不修改真实文件。</p><div class="fixture-tools"><button id="theme">切换明暗</button><button id="narrow">240px 文件面板</button><button id="rename">重命名文档（Esc取消）</button></div><section class="fixture-panel"><div id="files"></div></section><p id="status" class="fixture-note">有元数据的条目显示第二行；没有元数据的条目保持单行。</p><script>
const $=id=>document.getElementById(id),el=(tag,cls,text)=>{const e=document.createElement(tag);e.className=cls||'';if(text!=null)e.textContent=text;return e;},t=(s,...args)=>s.replace(/\\{(\\d+)\\}/g,(_,n)=>args[n]);
const state={root:'workspace',fileEntries:[{name:'src',path:'src',dir:true,unix:{owner:'aide',group:'developers',permissions:'drwxr-xr-x'}},{name:'工作区说明.md',path:'工作区说明.md',unix:{owner:'aide',group:'aide',permissions:'-rw-r--r--'}},{name:'deploy.sh',path:'deploy.sh',unix:{owner:'1001',group:'1002',permissions:'-rwxr-x---'}},{name:'很长的文件名与很长的账户名称.txt',path:'long.txt',unix:{owner:'long-owner-name',group:'long-group-name',permissions:'-rw-r-----'}},{name:'未知身份.txt',path:'unknown.txt'}],fileSelection:new Set(),fileSelectionAnchor:-1};
const fileNavigationGesture={suppressClickUntil:0},FILE_CLICK_OPEN_MS=240,FILE_DBLCLICK_MS=450,FILE_RENAME_MS=1600;
const bindFileNavigationGesture=()=>{},fileIconKind=file=>file.dir?'folder':'code',clearFileOpenTimers=()=>{},syncFileSelection=()=>{},selectFileRow=()=>{},openFileInNewTab=()=>{},openFileContextMenu=()=>{},openFile=()=>{},currentFileTarget=()=>({source:''}),loadFiles=async()=>renderFileEntries(),api=async()=>{},action=fn=>fn,toast=message=>{$('status').textContent=message;};
'''+ '\n'.join(functions)+'''
$('theme').onclick=()=>{document.documentElement.dataset.theme=document.documentElement.dataset.theme==='dark'?'light':'dark';};
$('narrow').onclick=()=>{document.querySelector('.fixture-panel').style.width='240px';};
$('rename').onclick=()=>{const row=$('files').children[1];beginInlineRename(row,row.querySelector('.file-name'),state.fileEntries[1]);};renderFileEntries();
</script></html>'''
p=root/'.agent-state/linux-file-metadata/preview.html';p.parent.mkdir(parents=True,exist_ok=True);p.write_text(html);print(p)
