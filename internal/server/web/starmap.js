'use strict';
const $=id=>document.getElementById(id), canvas=$('sky'), ctx=canvas.getContext('2d');
const logbook=$('limits'),logbookToggle=$('logbook-toggle');
const knowledgeTimeline=window.AideKnowledgeTimeline.create({request:(path,options)=>api(path,options)});
addEventListener('pointerdown',e=>{if(logbook.open&&!logbook.contains(e.target))logbook.open=false;});
addEventListener('keydown',e=>{if(e.key==='Escape'&&logbook.open){e.preventDefault();e.stopImmediatePropagation();logbook.open=false;logbookToggle.focus();}},true);
const params=new URLSearchParams(location.search), channelName=params.get('channel');
const channel=channelName&&/^[a-zA-Z0-9-]{10,100}$/.test(channelName)?new BroadcastChannel('aide-stars-'+channelName):null;
let token=localStorage.getItem('aide-token')||'',graph={nodes:[],edges:[],sources:[],warnings:[]},selected=null,region='',results=[],yaw=0,pitch=.1,zoom=1,drag=null,projection=[],width=0,height=0,pixelRatio=0,loading=false;
const motionReduced=()=>document.documentElement.dataset.motion==='reduced'||matchMedia('(prefers-reduced-motion: reduce)').matches;
const infrared=window.AideInfrared.create({button:$('infrared'),note:$('infrared-note'),redraw:()=>redraw(),motionEnabled:()=>motionEnabled(),onChange:enabled=>{scheduleSkyRecovery();say(enabled?'红外伪彩视觉模拟已开启 · 非真实观测数据':'已恢复可见光星图配色');}});
let codeView=params.get('code')==='1',edgeMode='all',focusCode=false,focusCache=null,focusKey='',graphGeneration=0,loadAbort=null,aiAbort=null;
let documentAbort=null,documentGeneration=0,documentBatch=null;
function resetDocumentBatch(){documentBatch=null;$('document-progress').hidden=true;$('document-next').hidden=true;}
// Data revisions and rendering lifetimes are separate: polling never reloads the page.
let liveCursor='',liveTimer=0,liveAbort=null,liveEpoch=0,liveFailures=0,liveAnimationUntil=0;
let liveReconcile=false,liveWireNodes=new Map(),liveStableScenes=new Map(),liveGhosts=[];
let pathAbort=null,pathGeneration=0,pathResult=null;
function clearEvidencePath(message='路径基于当前索引，不等于运行轨迹。',clearInputs=false){
 pathGeneration++;pathAbort?.abort();pathAbort=null;pathResult=null;$('path-run').disabled=false;$('path-steps').replaceChildren();$('path-status').textContent=message;
 if(clearInputs){$('path-from').value='';$('path-to').value='';}redraw();
}
function pathSourceLink(n,line){
 if(!n?.path||n.kind==='directory')return null;
 const spec={root:n.root,path:n.path,origin:n.origin||'',readOnly:true,knowledgeWorkspace:graph.workspace};
 if(n.source)spec.source=n.source;if(n.format)spec.format=n.format;
 if(line||n.line){spec.line=line||n.line;spec.endLine=line||n.endLine;spec.codeHash=n.contentHash;}
 return '/#file='+encodeURIComponent(JSON.stringify(spec));
}
function renderEvidencePath(result){
 $('path-steps').replaceChildren();
 $('path-status').textContent=result.found?`${result.steps.length} 步 · 当前索引证据路径${result.truncated?' · 索引仍有缺口':''}`:`当前索引及规则下未找到路径${result.limited?' · 搜索达到32步或16000节点限制':''}${result.truncated?' · 索引不完整':''}。不代表没有真实联系。`;
 const labels={call_typed:'快照类型绑定',call_candidate:'静态名称候选',defines:'源码声明',imports:'导入路径',mention:'文本提及',attachment:'会话附件',contains:'目录归属'};
 for(const [i,n] of result.nodes.entries()){
  const row=document.createElement('li'),button=document.createElement('button');button.type='button';button.className='code-relation';button.textContent=n.name+' · '+n.id;button.onclick=()=>{const current=nodeByID.get(n.id);if(current)select(current);};row.append(button);
  const link=pathSourceLink(n);if(link){const a=document.createElement('a');a.href=link;a.target='_blank';a.rel='noopener';a.textContent='打开原文件 ↗';row.append(a);}
  const step=result.steps[i];if(step){const e=step.edge,p=document.createElement('p');p.textContent=`${step.reversed?'逆向探索 ←':'沿关系 →'} ${labels[e.kind]||e.kind}${e.line?' · L'+e.line+(e.column?':'+e.column:''):''}`+(e.evidence?' · '+e.evidence:' · 此关系未提供源位置证据');row.append(p);
   const origin=nodeByID.get(e.from),href=e.line&&pathSourceLink(origin,e.line);if(href){const a=document.createElement('a');a.href=href;a.target='_blank';a.rel='noopener';a.textContent='查看关系源位置 ↗';row.append(a);}}
  $('path-steps').append(row);
 }
}
async function queryEvidencePath(){
 if(locked||loading||!liveCursor)return;
 clearEvidencePath('正在寻找当前索引中的证据路径…');
 const gen=pathGeneration,revision=liveCursor,workspace=graph.workspace,mode=codeView,controller=new AbortController();pathAbort=controller;$('path-run').disabled=true;
 const timeout=setTimeout(()=>controller.abort(),6000);
 try{const result=await api('/paths',{method:'POST',signal:controller.signal,body:JSON.stringify({workspace,revision,code:mode,from:$('path-from').value.trim(),to:$('path-to').value.trim(),undirected:$('path-undirected').checked,includeStructure:$('path-structure').checked})});
  if(locked||gen!==pathGeneration||revision!==liveCursor||workspace!==graph.workspace||mode!==codeView)return;
  if(result.revision!==revision||result.workspace!==workspace)throw Error('路径版本不一致，请重新查询。');
  pathResult=result;renderEvidencePath(result);redraw();
 }catch(e){if(gen===pathGeneration&&!locked)$('path-status').textContent=e.name==='AbortError'?'查询已取消或超时，请重试。':e.message;}
 finally{clearTimeout(timeout);if(gen===pathGeneration){pathAbort=null;$('path-run').disabled=false;}}
}
$('path-form').onsubmit=e=>{e.preventDefault();queryEvidencePath();};
for(const side of ['from','to'])$('path-set-'+side).onclick=()=>{if(!selected){$('path-status').textContent='先搜索或点击一个节点。';return;}clearEvidencePath();$('path-'+side).value=selected.id;$('path-explorer').open=true;};
$('path-clear').onclick=()=>clearEvidencePath(undefined,true);
for(const id of ['path-from','path-to','path-undirected','path-structure'])$(id).addEventListener('input',()=>clearEvidencePath());
function drawEvidencePath(){
 if(!pathResult?.found||locked)return;
 const visible=cosmicEnabled?new Map(cosmicStars.map(s=>[s.n.id,s.p])):new Map(renderNodes.map(n=>[n.id,n.projected]));
 ctx.save();ctx.strokeStyle=mapPalette.star;ctx.lineWidth=1.5;ctx.globalAlpha=.8;
 for(const step of pathResult.steps){const a=visible.get(step.edge.from),b=visible.get(step.edge.to);if(!a?.onScreen||!b?.onScreen)continue;ctx.setLineDash(step.edge.kind==='call_candidate'||step.edge.kind==='mention'?[3,6]:[]);ctx.beginPath();ctx.moveTo(a.x,a.y);ctx.lineTo(b.x,b.y);ctx.stroke();}
 ctx.setLineDash([]);for(const n of pathResult.nodes){const p=visible.get(n.id);if(!p?.onScreen)continue;ctx.beginPath();ctx.arc(p.x,p.y,10,0,Math.PI*2);ctx.stroke();}ctx.restore();
}


// Store navigation metadata only: no document excerpts, model answers or tokens.
let recoveryTimer=0,recoveryLoading=false,recoveryScopeLoaded='',pendingRecoveredView=null,recoveryInput=0,recoveryWarning=false;
function persistSkyView(){
 clearTimeout(recoveryTimer);
 if(locked||loading||recoveryLoading||!graph.workspace||!window.AideContinuity?.enabled())return;
 const destination=cameraTween?.to||{yaw,pitch,zoom};
 const value={version:1,codeView,region,query:$('search').value,retrievalMode:$('retrieval-mode').value,edgeMode,callDepth,focusCode,
  camera:{yaw:destination.yaw,pitch:destination.pitch,zoom:wheelZoom?.target??destination.zoom},motionPhase:{skyTime,driftTime},
  scroll:{search:document.querySelector('.search-panel').scrollTop,detail:$('detail').scrollTop,detailText:$('detail-text').scrollTop},
  path:cosmicScene?cosmicPath(cosmicScene.group).map(g=>g.key):[],selected:selected?{id:selected.id,origin:selected.origin||''}:null,animate,infrared:infrared.enabled};
 AideContinuity.writeTab('knowledge:'+graph.workspace,'scene','starmap',value).catch(e=>{if(!recoveryWarning){recoveryWarning=true;say('星图恢复记录保存失败 · '+e.message);}});
}
function scheduleSkyRecovery(){if(locked)return;clearTimeout(recoveryTimer);recoveryTimer=setTimeout(persistSkyView,850);}
function defaultSkyView(){
 return {version:1,codeView:params.get('code')==='1',region:'',query:'',retrievalMode:'nodes',edgeMode:'all',callDepth:'all',focusCode:false,camera:{yaw:0,pitch:.1,zoom:1},motionPhase:{skyTime:0,driftTime:0},scroll:{search:0,detail:0},path:[],selected:null,animate:!motionReduced(),infrared:false};
}
function restoreSkyView(value){
 if(!value||value.version!==1||locked)return;
 region=typeof value.region==='string'&&currentSources().some(s=>s.region===value.region)?value.region:'';
 const selectOption=(id,value)=>{const input=$(id);if([...input.options].some(o=>o.value===value))input.value=value;return input.value;};
 edgeMode=selectOption('edge-mode',value.edgeMode);callDepth=selectOption('call-depth',value.callDepth);
 selectOption('retrieval-mode',value.retrievalMode);
 $('search').value=typeof value.query==='string'?value.query.slice(0,2000):'';
 focusCode=false;search();
 const node=value.selected&&nodeByID.get(value.selected.id);
 if(node&&(node.origin||'')===(value.selected.origin||'')&&(!region||node.region===region)){select(node,false);focusCode=!!value.focusCode;}
 $('focus-code').setAttribute('aria-pressed',String(focusCode));
 invalidateView();refreshView();ensureCosmicView();
 const key=Array.isArray(value.path)?[...value.path].reverse().find(k=>typeof k==='string'&&cosmos?.find(k)):null;
 if(key)enterCosmic(key,false,true);
 interruptCamera();
 const finite=(v,fallback,min,max)=>Number.isFinite(v)?clamp(v,min,max):fallback;
 yaw=finite(value.camera?.yaw,0,-2.5,2.5);pitch=finite(value.camera?.pitch,.1,-1,1);zoom=finite(value.camera?.zoom,1,.5,2.8);
 skyTime=finite(value.motionPhase?.skyTime,0,0,604800000);driftTime=finite(value.motionPhase?.driftTime,0,0,604800000);skyFloat.x=Math.sin(driftTime/22000)*.038;skyFloat.y=Math.sin(driftTime/29000)*.024;
 if(typeof value.animate==='boolean')animate=value.animate&&!motionReduced();
 if(typeof value.infrared==='boolean')infrared.set(value.infrared,false);
 document.querySelector('.search-panel').scrollTop=finite(value.scroll?.search,0,0,1000000);$('detail').scrollTop=finite(value.scroll?.detail,0,0,1000000);$('detail-text').scrollTop=finite(value.scroll?.detailText,0,0,1000000);
 syncMotion();syncCodeView();syncGraphChrome();syncCosmicNav();redraw();say('已恢复上次星图视角与筛选；未自动执行检索或 AI。');
}
addEventListener('scroll',()=>{if(!locked&&!loading)scheduleSkyRecovery();},{capture:true,passive:true});
for(const event of ['pointerup','pointercancel','wheel','click','input','change','keydown'])addEventListener(event,()=>{if(!locked){recoveryInput++;if(loading)pendingRecoveredView=null;scheduleSkyRecovery();}},{passive:true});

const nodeFields=['id','name','kind','region','path','root','source','sourceName','sourceType','origin','format','session','number','text','size','modified','language','symbolKind','line','endLine','parentFile','signature','contentHash'];
let sourceCatalog=[],sourceByRegion=new Map();
const sourceTypes={session:'会话',local:'本地目录',ssh:'SSH 工作区',skill:'Skill',link:'HTTP 链接',mcp:'MCP 工具目录',sftp:'SSH / SFTP','workspace-sftp':'工作区 SSH / SFTP',ftp:'FTP',ftps:'FTPS',smb:'SMB'};
const sourceStates={ready:'已索引',partial:'部分索引',unavailable:'暂不可用',disabled:'已停用',catalog:'仅工具目录'};
const sourceTypeLabel=type=>sourceTypes[type]||type||'未报告类型';
const sourceStateLabel=state=>sourceStates[state]||'未报告状态';
function fallbackSourceName(r){return r==='sessions'?'会话':r==='workspace'?'工作区':r==='context'?'引用目录':graph.nodes.find(n=>n.region===r&&n.path==='.')?.name||'引用';}
function currentSources(){
 // Older servers do not publish a catalog. Derive its visible regions only in
 // that case; a reported empty or unavailable source remains an independent chip.
 if(Array.isArray(graph.sources))return graph.sources.filter(s=>s&&typeof s.region==='string'&&s.region).map(s=>({...s,name:s.name||fallbackSourceName(s.region),nodeCount:Math.max(0,Number(s.nodeCount)||0)}));
 const byRegion=new Map();for(const n of graph.nodes){if(!n.region)continue;let s=byRegion.get(n.region);if(!s){s={id:n.source||n.region,name:n.sourceName||fallbackSourceName(n.region),type:n.sourceType||(n.region==='sessions'?'session':''),region:n.region,state:'ready',nodeCount:0};byRegion.set(n.region,s);}s.nodeCount++;}return [...byRegion.values()];
}
function sourceCoverageState(source){
 const c=source.coverage;
 if(source.state!=='ready'&&source.state!=='partial')return sourceStateLabel(source.state);
 if(!c)return sourceStateLabel(source.state);
 if(c.reasons?.length)return '范围受限';
 if(c.progressive&&c.pending>0)return '分批扫描中';
 return source.state==='partial'?'部分索引':'限定范围已扫描';
}
function sourceCoverageLabel(source){
 const c=source.coverage;if(!c)return '';
 const reasons={files:'文件预算',directories:'目录预算',depth:'目录深度',time:'扫描时限',directory_entries:'单目录条目预算',read_error:'读取失败',catalog_limit:'索引目录容量上限',scope_missing:'配置范围不存在'};
 const progress=c.progressive?`累计索引 ${c.files} 个文件 / ${c.directories} 个目录；单批预算 ${c.fileBudget} 文件 / ${c.directoryBudget} 目录 / ${c.depthBudget} 层；第 ${c.cycle||1} 轮；待扫描 ${c.pending||0} 个目录任务；容量上限 ${c.catalogLimit||0} 条`:`已扫描 ${c.files} 个文件 / ${c.directories} 个目录；预算 ${c.fileBudget} 文件 / ${c.directoryBudget} 目录 / ${c.depthBudget} 层`;
 const limits=c.reasons?.length?'；受限：'+c.reasons.map(r=>reasons[r]||r).join('、'):'';
 const status=c.progressive?(c.pending?'；分批扫描中，保留上一轮已知节点':c.reasons?.length?'；本轮队列已结束，仍有未覆盖范围':'；本轮限定范围扫描完成'):c.reasons?.length?'':'；限定目录范围内扫描完成';
 return progress+(c.scopePaths?.length?'；索引范围：'+c.scopePaths.join('、'):'')+`；文本仅前 ${c.textBytesPerFile/1024} KiB；全库总数未统计`+limits+status;
}

function nodeSource(n){const s=sourceByRegion.get(n.region);return {id:s?.id||n.source||n.region,name:s?.name||n.sourceName||fallbackSourceName(n.region),type:s?.type||n.sourceType||(n.region==='sessions'?'session':''),state:s?.state||'',message:s?.message||''};}
function nodeSourceLabel(n){const s=nodeSource(n);return [s.name,s.type?sourceTypeLabel(s.type):'',n.format].filter(Boolean).join(' · ');}
function syncNodeSourceChrome(){
 if(selected){const s=nodeSource(selected);$('detail-source').textContent=nodeSourceLabel(selected)+(s.id?' · 来源 #'+s.id:'')+(s.type==='mcp'?' · 仅工具名称与说明，不含工具执行结果或远端文档正文；不会调用工具。':s.state&&s.state!=='ready'?' · '+sourceStateLabel(s.state):'');}
 for(const box of [$('results'),$('ai-found')])for(const b of box.querySelectorAll('[data-source-node]')){const current=nodeByID.get(b.dataset.sourceNode),original=b.sourceNode;const n=current?{...current,format:current.origin===original?.origin?original?.format||current.format:current.format}:original;if(n){b.textContent=nodeSourceLabel(n);b.title=b.textContent;}}
}
function appendSourceLabel(parent,n){const meta=document.createElement('small');meta.className='source-meta';meta.dataset.sourceNode=n.id;meta.sourceNode=n;meta.textContent=nodeSourceLabel(n);meta.title=meta.textContent;parent.append(meta);return meta;}
const edgeKey=e=>JSON.stringify([e.from,e.to,e.kind,e.line||0,e.column||0,e.evidence||'',e.confidence||'']);
function birthAlpha(n,now){if(!n.bornAt)return 1;return easing(clamp((now-n.bornAt)/680,0,1));}
function liveReady(){return !locked&&!document.hidden&&!pageLeaving&&!loading&&!drag&&!cosmicFlight;}
function stopLive(clearCursor=false){clearTimeout(liveTimer);liveTimer=0;liveEpoch++;liveAbort?.abort();liveAbort=null;if(clearCursor)liveCursor='';}
function scheduleLive(delay=4000){clearTimeout(liveTimer);liveTimer=0;if(locked||document.hidden||pageLeaving)return;liveTimer=setTimeout(pollLive,delay);}
async function pollLive(){
 liveTimer=0;if(!liveReady()){scheduleLive(650);return;}if(liveAbort)return;
 // Initial/transient load failures must retry the restoration path, not seed
 // a graph through delta polling and silently replace the saved camera.
 if(!graph.workspace||recoveryScopeLoaded!==graph.workspace){load();return;}
 const epoch=liveEpoch,mode=codeView,cursor=liveCursor,controller=new AbortController();liveAbort=controller;
 const timeout=setTimeout(()=>controller.abort(),14000);
 try{const update=await api('/updates?code='+(mode?'1':'0')+'&cursor='+encodeURIComponent(cursor),{signal:controller.signal});
  if(epoch!==liveEpoch||mode!==codeView||!liveReady())return;
  if(graph.workspace&&update.workspace!==graph.workspace){persistSkyView();load();return;}
  applyLiveUpdate(update);liveCursor=update.revision;knowledgeTimeline.sync({workspace:graph.workspace,code:codeView,revision:liveCursor});liveFailures=0;
 }catch(e){if(epoch===liveEpoch&&!locked&&!document.hidden){liveFailures++;if(liveFailures===1)say('自动同步暂不可用，保留当前星图并稍后重试。');}}
 finally{clearTimeout(timeout);if(liveAbort===controller)liveAbort=null;if(epoch===liveEpoch)scheduleLive(Math.min(30000,4000*Math.pow(2,liveFailures)));}
}

const retrievalMode=()=>codeView?'nodes':$('retrieval-mode').value;
let animate=!motionReduced(), frame=0, pageLeaving=false;
let driftTime=0;const skyFloat={x:0,y:0};
let skyTime=0,lastPaint=null,pausedAt=document.hidden?performance.now():null,cameraTween=null,graphFade=null;
let wheelZoom=null,interactionUntil=0,paintRequested=true,renderRevision=0,renderKey='';
let structure=null,layerState=null,nodeByID=new Map(),edgeAdjacency=new Map(),renderNodes=[],renderEdges=[],edgeBatches=[];
let searchQuery='',resultIDs=new Set(),callDepth='all',skyCache=null;
const glowCache=new Map(),cloudCache=new Map(),tau=Math.PI*2;
let cosmos=null,cosmicEnabled=true,cosmicScene=null,cosmicKey='',cosmicRecords=[],cosmicLinks=[],cosmicSelected='',cosmicHover='',cosmicFlight=null;
let cosmicWheel=0,cosmicWheelLast=0,cosmicGate=0,cosmicScopeKey='',cosmicStars=[],cosmicStarEdges=[],cosmicCallState=null;
const cosmicTextureCache=new Map(),cosmicSceneCache=new Map();
const panelAnimations=new Map(),motionMedia=matchMedia('(prefers-reduced-motion: reduce)');
// Read the same appearance contract as the workbench, once per change. Canvas
// keeps its own prepared textures; it never reads CSS in the animation loop.
function readMapPalette(){
 const css=getComputedStyle(document.documentElement),pixel=document.createElement('canvas');pixel.width=pixel.height=1;const sample=pixel.getContext('2d',{willReadFrequently:true});
 // Normalize CSS colors, including system high-contrast colors, to cached hex.
 // Alpha suffixes below never depend on a user's choice of CSS color notation.
 const read=(name,fallback)=>{sample.clearRect(0,0,1,1);sample.fillStyle=fallback;sample.fillStyle=css.getPropertyValue(name).trim()||fallback;sample.fillRect(0,0,1,1);return '#'+Array.from(sample.getImageData(0,0,1,1).data).slice(0,3).map(v=>v.toString(16).padStart(2,'0')).join('');},light=document.documentElement.dataset.theme==='light';
 return {light,highContrast:matchMedia('(forced-colors: active)').matches,file:read('--map-source-file',light?'#55779e':'#9bb5d1'),reference:read('--map-source-reference',light?'#71849b':'#91a5bd'),session:read('--map-source-session',light?'#596574':'#b0b7c1'),bg:read('--map-canvas-bg',light?'#f2f3f5':'#202328'),text:read('--map-canvas-text',light?'#293039':'#dfe3e9'),star:read('--map-canvas-star',light?'#55779e':'#b6c7d9'),nebula:read('--map-canvas-nebula',light?'#71849b':'#91a5bd'),muted:read('--map-muted',light?'#596574':'#b0b7c1')};
}
let mapPalette=readMapPalette();
// Celestial colors are a visual metaphor; degree is unique non-structural neighbors.
const stellarBands=[{min:0,name:'孤立灰星',dark:'#8e969f',light:'#8b9198'},{min:1,name:'暖红星',dark:'#d8a294',light:'#ad7668'},{min:3,name:'金色星',dark:'#e1c293',light:'#9c8259'},{min:8,name:'白色星',dark:'#e4e2d6',light:'#71808d'},{min:16,name:'蓝白星',dark:'#b4d2eb',light:'#6086a7'},{min:32,name:'蓝色强光星',dark:'#7db9e8',light:'#3b76a6'}];
function prepareStellarConnections(){
 const neighbors=new Map(graph.nodes.map(n=>[n.id,new Set()]));
 const link=(a,b)=>{if(a===b||!neighbors.has(a)||!neighbors.has(b))return;neighbors.get(a).add(b);neighbors.get(b).add(a);};
 for(const e of graph.edges){if(['contains','defines'].includes(e.kind))continue;link(e.from,e.to);
  const a=nodeByID.get(e.from),b=nodeByID.get(e.to);if(a&&b)link(a.parentFile||a.id,b.parentFile||b.id);
 }
 for(const n of graph.nodes){n.connectionCount=neighbors.get(n.id).size;applyStellarColor(n);}
}
function applyStellarColor(n){
 const count=n.connectionCount||0,band=stellarBands.findLast(b=>count>=b.min);n.stellarBand=band.name;
 n.stellarStrength=count?Math.min(1,Math.log2(count+1)/6):0;
 n.starColor=n.spectralColor=mapPalette.highContrast?mapPalette.star:band[mapPalette.light?'light':'dark'];n.glow=glowSprite(n.starColor);
}
function stellarVisual(n,depth,chosen=false,incoming=false){
 const strength=n.stellarStrength||0,attenuation=codeView&&Number.isFinite(depth)?Math.max(.22,1/(1+Math.min(depth,16)*.38)):1;
 return {radius:chosen?2.8:incoming?2.1:(.65+strength*1.85)*attenuation,halo:chosen?32:incoming?24:(n.connectionCount?10+strength*30:5)*attenuation,alpha:chosen||mapPalette.highContrast?1:incoming?.86:(.38+strength*.54)*attenuation};
}
function syncMapPalette(){
 const next=readMapPalette();if(JSON.stringify(next)===JSON.stringify(mapPalette))return;mapPalette=next;
 glowCache.clear();cloudCache.clear();cosmicTextureCache.clear();cosmicSceneCache.clear();
 for(const c of clusters){c.color=colors(c);c.texture=cloudTexture(c.key,c.color);}
 for(const n of graph.nodes)applyStellarColor(n);invalidateView();
 if(cosmicScene){const data=cosmicData(cosmicKey);if(data){cosmicRecords=data.records;cosmicStars=data.stars;cosmicStarEdges=data.edges;}}
 graphFade=null;cosmicFlight=null;if(width&&height)makeSky();redraw();
}
const easing=u=>u*u*u*(u*(u*6-15)+10);
const clamp=(x,a,b)=>Math.max(a,Math.min(b,x));
function motionEnabled(){return animate&&!motionReduced();}
function nearestYaw(value){return yaw+Math.atan2(Math.sin(value-yaw),Math.cos(value-yaw));}
function updateCamera(now){
 if(wheelZoom){
  const elapsed=clamp(now-wheelZoom.last,0,80),k=1-Math.exp(-elapsed/70);
  zoom=Math.exp(Math.log(zoom)+(Math.log(wheelZoom.target)-Math.log(zoom))*k);wheelZoom.last=now;
  if(Math.abs(Math.log(wheelZoom.target/zoom))<.0003){zoom=wheelZoom.target;wheelZoom=null;}
 }
 if(!cameraTween)return;
 const u=clamp((now-cameraTween.start)/cameraTween.duration,0,1),k=easing(u),{from,to}=cameraTween;
 yaw=from.yaw+(to.yaw-from.yaw)*k;pitch=from.pitch+(to.pitch-from.pitch)*k;
 zoom=Math.exp(Math.log(from.zoom)+(Math.log(to.zoom)-Math.log(from.zoom))*k);
 if(u===1)cameraTween=null;
}
function moveCamera(to,duration=700){
 const now=performance.now();updateCamera(now);wheelZoom=null;
 const target={yaw:nearestYaw(to.yaw??yaw),pitch:clamp(to.pitch??pitch,-1,1),zoom:clamp(to.zoom??zoom,.5,2.8)};
 if(!motionEnabled()){cameraTween=null;({yaw,pitch,zoom}=target);redraw();return;}
 cameraTween={from:{yaw,pitch,zoom},to:target,start:now,duration};redraw();
}
function interruptCamera(){updateCamera(performance.now());cameraTween=null;wheelZoom=null;}
function beginGraphTransition(){
 if(!motionEnabled()||locked||!canvas.width)return;
 // A single local snapshot blends filters/index refreshes; no second render loop.
 const c=document.createElement('canvas');c.width=canvas.width;c.height=canvas.height;c.getContext('2d').drawImage(canvas,0,0);
 graphFade={canvas:c,start:performance.now(),duration:260};redraw();
}
function revealPanel(el,shown){
 const old=panelAnimations.get(el),style=old?getComputedStyle(el):null;
 const current=style?{opacity:style.opacity,transform:style.transform}:null;
 old?.cancel();panelAnimations.delete(el);el.inert=!shown;
 if(!motionEnabled()||typeof el.animate!=='function'){el.hidden=!shown;return;}
 const entering=el.hidden;
 if(!shown&&entering)return;
 el.hidden=false;
 const first=current||(shown?{opacity:entering?0:.65,transform:entering?'translateY(10px)':'none'}:{opacity:1,transform:'none'});
 const last=shown?{opacity:1,transform:'none'}:{opacity:0,transform:'translateY(6px)'};
 const a=el.animate([first,last],{duration:shown?(entering?240:170):150,easing:'cubic-bezier(.22,1,.36,1)'});
 panelAnimations.set(el,a);
 a.finished.then(()=>{if(panelAnimations.get(el)!==a)return;if(!shown)el.hidden=true;panelAnimations.delete(el)},()=>{});
}
function settlePanels(){for(const [el,a]of panelAnimations){a.cancel();if(el.inert)el.hidden=true;}panelAnimations.clear();}
function openAI(){revealPanel($('ai-body'),true);$('ai-toggle').setAttribute('aria-expanded','true');}
function say(s){$('status').textContent=s;}
async function api(p,options={}){const r=await fetch('/api/knowledge-map'+p,{...options,headers:{Authorization:'Bearer '+token,'Content-Type':'application/json'}});const d=await r.json();if(!r.ok)throw Error(d.error||'读取失败，请回到工作台登录');return d;}
function seeded(s){let h=2166136261;for(const c of s){h^=c.charCodeAt(0);h=Math.imul(h,16777619)}h ^= h >>> 16; h = Math.imul(h, 0x7feb352d); h ^= h >>> 15; h = Math.imul(h, 0x846ca68b); h ^= h >>> 16; return (h>>>0)/4294967296;}
const colors=n=>mapPalette.highContrast?mapPalette.star:n.kind==='symbol'||n.region==='sessions'?mapPalette.session:n.region==='workspace'?mapPalette.file:mapPalette.reference;
// Static sky, nebula textures and stellar glows are rasterized once. Interaction
// frames only project cached graph records; they never rebuild gradients or hashes.
const stars=Array.from({length:64},(_,i)=>({x:seeded('x'+i),y:seeded('y'+i),a:seeded('a'+i),r:.35+seeded('r'+i)*1.05}));
let clusters=[];
function glowSprite(color){
 if(glowCache.has(color))return glowCache.get(color);
 const c=document.createElement('canvas');c.width=c.height=96;const g=c.getContext('2d'),fog=g.createRadialGradient(48,48,0,48,48,46);
 fog.addColorStop(0,color+'f0');fog.addColorStop(.055,color+'c0');fog.addColorStop(.15,color+'48');fog.addColorStop(.42,color+'13');fog.addColorStop(1,color+'00');
 g.fillStyle=fog;g.fillRect(0,0,96,96);glowCache.set(color,c);return c;
}
function makeSky(){
 const c=document.createElement('canvas'),scale=Math.min(1,1900/width);c.width=Math.ceil(width*scale);c.height=Math.ceil(height*scale);
 const g=c.getContext('2d');g.scale(scale,scale);g.fillStyle=mapPalette.bg;g.fillRect(0,0,width,height);
 const haze=g.createRadialGradient(width*.61,height*.45,0,width*.61,height*.45,width*.72);
 haze.addColorStop(0,mapPalette.nebula+'16');haze.addColorStop(.36,mapPalette.nebula+'08');haze.addColorStop(1,mapPalette.nebula+'00');g.fillStyle=haze;g.fillRect(0,0,width,height);
 g.save();g.translate(width*.49,height*.43);g.rotate(-.37);g.scale(1,.28);
 const mist=g.createRadialGradient(0,0,0,0,0,width*.64);mist.addColorStop(0,mapPalette.nebula+'0b');mist.addColorStop(.45,mapPalette.nebula+'08');mist.addColorStop(1,mapPalette.nebula+'00');g.fillStyle=mist;g.fillRect(-width,-height*2,width*2,height*4);g.restore();
 // Filamentary dust is stable while the camera moves; sparse stars remain crisp.
 for(let i=0;i<900;i++){
  const u=seeded('sky-u'+i),v=seeded('sky-v'+i),band=i<500;
  const x=u*width,y=band?height*.54-(u-.5)*height*.62+(v-.5)*height*.30:v*height;
  if(y<0||y>height)continue;
  const light=seeded('sky-light'+i);g.globalAlpha=.05+light*(mapPalette.light?.16:.27);g.fillStyle=mapPalette.star;g.beginPath();g.arc(x,y,.2+light*.55,0,tau);g.fill();
 }
 g.globalAlpha=1;skyCache=c;
}
function cloudTexture(key,color){
 // Six seeded dust variants per source color bound texture memory as clusters grow.
 const textureKey=color+'|'+Math.floor(seeded(key)*6);if(cloudCache.has(textureKey))return cloudCache.get(textureKey);key=textureKey;
 const c=document.createElement('canvas');c.width=c.height=384;const g=c.getContext('2d');
 for(let i=0;i<48;i++){const angle=seeded(key+i+'a')*tau,r=Math.sqrt(seeded(key+i+'r'))*112,x=192+Math.cos(angle)*r,y=192+Math.sin(angle)*r*.57,size=22+seeded(key+i+'s')*63;
  const fog=g.createRadialGradient(x,y,0,x,y,size);fog.addColorStop(0,color+'1a');fog.addColorStop(.35,color+'0a');fog.addColorStop(1,color+'00');g.fillStyle=fog;g.fillRect(x-size,y-size,size*2,size*2);}
 g.globalCompositeOperation='destination-out';for(let i=0;i<13;i++){const x=64+i*21,y=186+Math.sin(i*.69)*28,f=g.createRadialGradient(x,y,0,x,y,27);f.addColorStop(0,'#00000075');f.addColorStop(1,'#00000000');g.fillStyle=f;g.fillRect(x-27,y-27,54,54);}g.globalCompositeOperation='source-over';cloudCache.set(textureKey,c);return c;
}
// Spatial scale comes from qualified, real child units. Source/path zoning is
// only a layout cue: an unlabeled dust region never adds a galaxy to the model.
function cosmicTexture(depth,variant=0){
 const key=depth+'|'+variant;if(cosmicTextureCache.has(key))return cosmicTextureCache.get(key);
 const c=document.createElement('canvas');c.width=c.height=384;const g=c.getContext('2d');
 // Atmospheric gas only. Every visible foreground star is drawn from a real ID.
 const flattened=depth===4||depth===5,stretch=flattened?.31:.65;
 g.save();g.translate(192,192);g.rotate(-.32+variant*.19);g.scale(1,stretch);
 for(let i=0;i<38;i++){const u=seeded(key+'u'+i),v=seeded(key+'v'+i),x=(u-.5)*235,y=(v-.5)*110+Math.sin(u*5)*24,r=24+seeded(key+'r'+i)*52,f=g.createRadialGradient(x,y,0,x,y,r);f.addColorStop(0,mapPalette.nebula+(flattened?'14':'10'));f.addColorStop(.4,mapPalette.nebula+'07');f.addColorStop(1,mapPalette.nebula+'00');g.fillStyle=f;g.fillRect(x-r,y-r,r*2,r*2);}
 g.restore();
 // Irregular dark interstellar lanes, never a fake repeated celestial icon.
 g.globalCompositeOperation='destination-out';for(let i=0;i<12;i++){const x=54+i*23,y=185+Math.sin(i*.57+variant)*24,r=14+seeded(key+'dark'+i)*15,f=g.createRadialGradient(x,y,0,x,y,r);f.addColorStop(0,'#00000099');f.addColorStop(1,'#00000000');g.fillStyle=f;g.fillRect(x-r,y-r,r*2,r*2);}g.globalCompositeOperation='source-over';
 cosmicTextureCache.set(key,c);while(cosmicTextureCache.size>12)cosmicTextureCache.delete(cosmicTextureCache.keys().next().value);return c;
}
function cosmicLabel(depth){return cosmos?.levels?.[depth]||['知识宇宙','超星系团','星系团','星系群','银河','旋臂','恒星系统','节点'][depth];}
function cosmicData(key){
 if(cosmicSceneCache.has(key))return cosmicSceneCache.get(key);const scene=cosmos?.scene(key);if(!scene)return null;
 const stable=liveStableScenes.get(key),stableRecords=new Map((stable?.records||[]).map(r=>[r.group.key,r])),stableStars=new Map((stable?.stars||[]).map(r=>[r.n.id,r]));
 const children=scene.children.length?scene.children:scene.group.nodeID?[scene.group]:[],ordered=[...children].sort((a,b)=>a.key.localeCompare(b.key)),records=[],owner=new Map();
 for(let i=0;i<ordered.length;i++){
  const group=ordered[i],leaf=!!group.nodeID,n=leaf?nodeByID.get(group.nodeID):null;
  // Retain familiar source/path constellations when no higher unit qualifies.
  const members=group.members.map(id=>nodeByID.get(id)).filter(Boolean),centroid=members.reduce((a,n)=>({x:a.x+(n.x||0),y:a.y+(n.y||0)}),{x:0,y:0});
  const x=members.length?centroid.x/members.length*.72:0,y=members.length?centroid.y/members.length*.66:0;
  const spread=leaf?0:Math.min(.24,.055+Math.sqrt(members.length)*.012),record={group,x:ordered.length===1?0:x,y:ordered.length===1?0:y,z:.96+seeded(group.key+'z')*.09,spread,phase:seeded(group.key+'phase'),p:{x:0,y:0,onScreen:false},radius:12,texture:leaf?null:cosmicTexture(group.depth,Math.floor(seeded(group.key)*2))};
  const previous=stableRecords.get(group.key);if(previous){for(const prop of ['x','y','z','spread','radius'])record[prop]=previous[prop];record.p=previous.p;}
  record.label=group.label.length>30?group.label.slice(0,29)+'…':group.label;records.push(record);for(const id of group.members)owner.set(id,record);
 }
 const stars=[];
 for(const id of scene.members){const n=nodeByID.get(id);if(!n)continue;const r=owner.get(id);if(!r)continue;let x,y;
  if(r.group.nodeID){x=ordered.length===1?0:(n.x||0)*.72;y=ordered.length===1?0:(n.y||0)*.66;r.x=x;r.y=y;}
  else{const a=seeded(id+'cosmic-a')*tau,rr=Math.sqrt(seeded(id+'cosmic-r'))*r.spread,disc=r.group.depth===4||r.group.depth===5;x=r.x+Math.cos(a)*rr;y=r.y+Math.sin(a)*rr*(disc?.30:.68)+(disc?Math.sin(a*2)*rr*.08:0);}
  const star={n,x,y,z:(r.z+(seeded(id+'cosmic-z')-.5)*.07)*(1+Math.min(n.globalVisualDepth??5,12)*.035),p:{n,x:0,y:0,onScreen:false},owner:r};const previous=stableStars.get(id);if(previous){star.x=previous.x;star.y=previous.y;star.z=previous.z;star.p=previous.p;star.p.n=n;}stars.push(star);
 }
 const starByID=new Map(stars.map(r=>[r.n.id,r]));
 const edges=renderEdges.map(e=>({...e,a:starByID.get(e.from)?.p,b:starByID.get(e.to)?.p})).filter(e=>e.a&&e.b),batches=new Map();for(const e of edges){if(!batches.has(e.kind))batches.set(e.kind,{kind:e.kind,dashed:e.dashed,hierarchy:e.hierarchy,edges:[]});batches.get(e.kind).edges.push(e);}
 // Faint source gas preserves region recognition; it is not counted as a unit.
 const gas=new Map();for(const star of stars){const key=star.n.cluster?.key||star.n.region||'source';if(!gas.has(key))gas.set(key,{x:0,y:0,count:0,key,texture:cloudTexture(key,mapPalette.nebula)});const zone=gas.get(key);zone.x+=star.x;zone.y+=star.y;zone.count++;}
 for(const zone of gas.values()){zone.x/=zone.count;zone.y/=zone.count;zone.spread=Math.min(.28,.05+Math.sqrt(zone.count)*.012);}
 const data={scene,records,stars,edges,batches:[...batches.values()],gas:[...gas.values()],projection:stars.map(star=>star.p)};cosmicSceneCache.set(key,data);while(cosmicSceneCache.size>4)cosmicSceneCache.delete(cosmicSceneCache.keys().next().value);return data;
}
function cosmicPath(group){const path=[];for(let g=group;g;g=g.parent)path.unshift(g);return path;}
function cosmicPreferred(){return cosmicRecords.find(r=>r.group.key===cosmicSelected)?.group||cosmicRecords.find(r=>r.group.key===cosmicHover)?.group||cosmicScene?.children.reduce((best,g)=>!best||g.members.length>best.members.length?g:best,null);}
function syncCosmicNav(){
 const hud=$('cosmic-nav');if(!hud)return;hud.hidden=!cosmicScene;document.body.classList.toggle('cosmic-mode',cosmicEnabled);document.body.classList.remove('cosmic-legacy');
 if(!cosmicScene){$('cosmic-title').textContent='知识星域';$('cosmic-meta').textContent='等待索引';return;}
 const counts=cosmicScene.scaleCounts||[],units=[];for(let depth=1;depth<7;depth++)if(counts[depth])units.push(counts[depth]+' '+cosmicLabel(depth));
 const calls=structure?.entry('calls'),hasCalls=cosmicScene.members.some(id=>calls?.levels.has(id));$('cosmic-title').textContent=cosmicScene.group.nodeID?cosmicScene.group.label:cosmicScene.group.parent?cosmicScene.group.label+' · '+cosmicLabel(cosmicScene.depth):'知识宇宙';
 $('cosmic-meta').textContent=(units.length?units.join(' · ')+' · ':'')+cosmicScene.members.length+' 个真实节点 · '+(hasCalls?'星色按关联数 · 星等按调用层':'星色与光晕按有效关联数');
 canvas.setAttribute('aria-label','真实节点知识星域，拖动环顾，滚轮靠近，双击进入，滚轮远离或 Alt 加左方向键返回。');
}
function enterCosmic(key,animateFlight=true,keepSelection=false){
 if(!cosmos||locked)return;const data=cosmicData(key);if(!data)return;if(key===cosmicKey){syncCosmicNav();return;}
 const oldScene=cosmicScene,anchor=cosmicRecords.find(r=>r.group.key===key)?.p,now=performance.now(),inward=oldScene&&cosmicPath(data.scene.group).some(g=>g.key===oldScene.group.key);
 if(animateFlight&&motionEnabled()&&oldScene&&canvas.width){const snapshot=document.createElement('canvas');snapshot.width=canvas.width;snapshot.height=canvas.height;snapshot.getContext('2d').drawImage(canvas,0,0);cosmicFlight={canvas:snapshot,start:now,duration:760,direction:inward?1:-1,anchor:{x:anchor?.x??width*.57,y:anchor?.y??height*.5}};}else cosmicFlight=null;
 cosmicKey=key;cosmicScene=data.scene;cosmicRecords=data.records;cosmicStars=data.stars;cosmicStarEdges=data.edges;cosmicSelected='';cosmicHover='';projection=[];drag=null;interruptCamera();yaw=0;pitch=.1;zoom=1;cosmicWheel=0;cosmicGate=now+800;graphFade=null;
 if(!keepSelection){selected=null;revealPanel($('detail'),false);}syncCosmicNav();redraw();
}
function ensureCosmicView(){
 if(!cosmicEnabled||locked||!window.AideStarCosmos)return;
 const key=[graphGeneration,region,codeView,edgeMode,focusCode,focusCode?selected?.id||'':'',focusCode?callDepth:''].join('|');if(key===cosmicScopeKey)return;
 const keep=liveReconcile;liveReconcile=false;
 const oldPath=cosmicScene?cosmicPath(cosmicScene.group).map(g=>g.key):[],transition=graphFade;
 if(keep){liveStableScenes=new Map(cosmicSceneCache);}
 const visibleIDs=new Set(renderNodes.map(n=>n.id)),modelEdges=graph.edges.filter(e=>visibleIDs.has(e.from)&&visibleIDs.has(e.to));
 cosmicScopeKey=key;cosmos=AideStarCosmos.build(renderNodes,modelEdges);cosmicSceneCache.clear();
 if(keep){const target=[...oldPath].reverse().find(k=>cosmos.find(k))||cosmos.root.key,data=cosmicData(target);
  cosmicKey=target;cosmicScene=data.scene;cosmicRecords=data.records;cosmicStars=data.stars;cosmicStarEdges=data.edges;projection=data.projection;
  cosmicHover=cosmos.find(cosmicHover)?cosmicHover:'';cosmicSelected=cosmos.find(cosmicSelected)?cosmicSelected:'';syncCosmicNav();
 }else{liveStableScenes.clear();cosmicScene=null;cosmicKey='';enterCosmic(cosmos.root.key,false,true);graphFade=transition;}
}

function cosmicLocate(n){const path=cosmos?.pathFor(n.id);if(!path?.length)return;const target=path.length>2?path.at(-2):path[0];enterCosmic(target.key,true,true);}
function cosmicPick(x,y){let nearest=null,score=Infinity;for(const r of cosmicRecords){if(!r.p.onScreen)continue;const radius=r.group.nodeID?12:Math.min(90,r.radius+15),d=Math.hypot(r.p.x-x,r.p.y-y);if(d<radius&&d/radius<score){nearest=r;score=d/radius;}}return nearest;}
function cosmicStarPick(x,y){let nearest=null,distance=11;for(const star of cosmicStars){if(!star.p.onScreen)continue;const d=Math.hypot(star.p.x-x,star.p.y-y);if(d<distance){distance=d;nearest=star;}}return nearest;}
function drawCosmos(now,t){
 if(!cosmicScene)return;const data=cosmicSceneCache.get(cosmicKey),flight=cosmicFlight,u=flight?clamp((now-flight.start)/flight.duration,0,1):1,k=easing(u),fade=flight?k:1,scale=flight?(flight.direction>0?.83+.17*k:1.17-.17*k):1;
 const cx=width*(.57+skyFloat.x),cy=height*(.5+skyFloat.y),drift=0;
 ctx.save();ctx.globalAlpha=fade;ctx.translate(cx,cy);ctx.scale(scale,scale);ctx.translate(-cx,-cy);
 // No synthetic cosmic web, disc badge or planet is painted behind the real stars.
 for(const zone of data?.gas||[]){const x=cx+(zone.x+yaw*.22+drift)*width*zoom,y=cy+(zone.y+(pitch-.1)*.18)*height*zoom,size=Math.max(90,zone.spread*width*3.1*zoom);if(x+size/2<0||x-size/2>width||y+size*.4<0||y-size*.4>height)continue;ctx.save();ctx.globalAlpha=fade*.12;ctx.translate(x,y);ctx.rotate(-.32);ctx.drawImage(zone.texture,-size/2,-size*.36,size,size*.72);ctx.restore();}
 for(const r of cosmicRecords){const p=r.p;p.x=cx+(r.x+yaw*.22+drift)*width*zoom/r.z;p.y=cy+(r.y+(pitch-.1)*.18)*height*zoom/r.z;p.onScreen=p.x>-100&&p.x<width+100&&p.y>-100&&p.y<height+100;r.radius=r.group.nodeID?10:Math.max(25,r.spread*width*zoom/r.z*.65);if(r.texture&&p.onScreen){const size=Math.max(110,r.spread*width*3.6*zoom/r.z);ctx.save();ctx.globalAlpha=fade*.13;ctx.translate(p.x,p.y);ctx.rotate(-.32+r.phase*.13);ctx.drawImage(r.texture,-size/2,-size/2,size,size);ctx.restore();}}
 for(const star of cosmicStars){const p=star.p;p.x=cx+(star.x+yaw*.22+drift)*width*zoom/star.z;p.y=cy+(star.y+(pitch-.1)*.18)*height*zoom/star.z;p.onScreen=p.x>-30&&p.x<width+30&&p.y>-30&&p.y<height+30;if(star.owner.group.nodeID){star.owner.p.x=p.x;star.owner.p.y=p.y;star.owner.p.onScreen=p.onScreen;}}
 infrared.draw(ctx,cosmicStars.map(star=>({x:star.p.x,y:star.p.y,degree:star.n.connectionCount||0,distance:star.z,scale:zoom/star.z,birth:birthAlpha(star.n,now)})),width,height,now);
 const calls=structure?.entry('calls');cosmicCallState=selected&&calls?.levels.has(selected.id)?structure.describe(selected.id,'calls'):calls;
 for(const batch of data?.batches||[]){if(cosmicStars.length>140&&batch.hierarchy)continue;ctx.strokeStyle=mapPalette.star+(batch.hierarchy?'18':'30');ctx.lineWidth=.6;ctx.setLineDash(batch.dashed?[3,7]:[]);ctx.beginPath();for(const e of batch.edges){if(!edgeInView(e)||selected&&(e.from===selected.id||e.to===selected.id))continue;if(birthAlpha(nodeByID.get(e.from)||{},now)<.97||birthAlpha(nodeByID.get(e.to)||{},now)<.97)continue;ctx.moveTo(e.a.x,e.a.y);ctx.lineTo(e.b.x,e.b.y);}ctx.stroke();}ctx.setLineDash([]);
 let particles=0;for(const e of cosmicStarEdges){if(!edgeInView(e))continue;const birth=Math.min(birthAlpha(nodeByID.get(e.from)||{},now),birthAlpha(nodeByID.get(e.to)||{},now));if(birth<.97)continue;const active=selected&&(e.from===selected.id||e.to===selected.id);if(active){ctx.strokeStyle=mapPalette.star+'90';ctx.lineWidth=1;ctx.setLineDash(e.dashed?[3,6]:[]);ctx.beginPath();ctx.moveTo(e.a.x,e.a.y);ctx.lineTo(e.b.x,e.b.y);ctx.stroke();ctx.setLineDash([]);}if(active&&animate&&particles++<24){const q=(t/6500+e.phase)%1;ctx.fillStyle=mapPalette.star;ctx.beginPath();ctx.arc(e.a.x+(e.b.x-e.a.x)*q,e.a.y+(e.b.y-e.a.y)*q,1,0,tau);ctx.fill();}}
 for(const star of cosmicStars){const {n,p}=star;if(!p.onScreen)continue;const participating=!!calls?.levels.has(n.id),depth=participating?cosmicCallState?.levels.get(n.id):null,known=Number.isFinite(depth),incoming=!!cosmicCallState?.incoming.has(n.id),chosen=selected?.id===n.id;
  const visual=stellarVisual(n,known?depth:null,chosen,incoming),color=n.starColor,radius=visual.radius,halo=visual.halo,match=n.match,alpha=searchQuery&&!match&&!chosen?.18:visual.alpha;
  const birth=birthAlpha(n,now);ctx.globalAlpha=fade*alpha*birth*(chosen?.65:.28);ctx.drawImage(glowSprite(color),p.x-halo/2,p.y-halo/2,halo,halo);ctx.globalAlpha=fade*alpha*birth;ctx.fillStyle=color;ctx.beginPath();ctx.arc(p.x,p.y,radius*(.4+.6*birth),0,tau);ctx.fill();ctx.globalAlpha=fade;
  if((chosen||match)&&birth>.5){ctx.font='11px -apple-system,sans-serif';ctx.fillStyle=chosen?mapPalette.text:mapPalette.muted;ctx.fillText(n.drawLabel||n.name,p.x+9,p.y+4);}
  if(chosen){ctx.strokeStyle=mapPalette.star+'70';ctx.lineWidth=.7;ctx.beginPath();ctx.arc(p.x,p.y,12+(motionEnabled()?Math.sin(t/1000)*.8:0),0,tau);ctx.stroke();}
 }
 for(const r of cosmicRecords){if(!r.p.onScreen||r.group.nodeID||r.group.key!==cosmicHover&&r.group.key!==cosmicSelected)continue;ctx.font='11px -apple-system,sans-serif';ctx.fillStyle=mapPalette.text;ctx.fillText(r.label,r.p.x+15,r.p.y-15);ctx.font='9px -apple-system,sans-serif';ctx.fillStyle=mapPalette.muted;ctx.fillText(r.group.members.length+' 个真实节点',r.p.x+15,r.p.y);}
 if(!cosmicScene.members.length){ctx.font='13px -apple-system,sans-serif';ctx.fillStyle=mapPalette.text;ctx.fillText('当前范围尚无已索引节点',cx-90,cy);}
 ctx.restore();projection=data?.projection||[];drawLiveGhosts(now,drift);
 if(flight){ctx.save();ctx.globalAlpha=1-k;const s=flight.direction>0?1+.60*k:1-.22*k;ctx.translate(flight.anchor.x,flight.anchor.y);ctx.scale(s,s);ctx.translate(-flight.anchor.x,-flight.anchor.y);ctx.drawImage(flight.canvas,0,0,width,height);ctx.restore();if(u===1)cosmicFlight=null;}
}
function relationshipOverview(){
 cosmicEnabled=true;cosmicScopeKey='';cosmicSceneCache.clear();cosmicScene=null;cosmicKey='';projection=[];renderKey='';
 moveCamera({yaw:0,pitch:.1,zoom:1},650);redraw();
}
addEventListener('keydown',e=>{if(!cosmicEnabled||locked||e.target.closest?.('input,textarea,select,[contenteditable=true]'))return;if((e.altKey&&e.key==='ArrowLeft')||(e.key==='Escape'&&!selected)){if(cosmicScene?.group.parent){e.preventDefault();enterCosmic(cosmicScene.group.parent.key);}}});
function invalidateView(){renderRevision++;renderKey='';focusCache=null;focusKey='';}
function layout(preserve=false){
 const oldClusters=new Map(clusters.map(c=>[c.key,c])),oldPositions=new Map(preserve?graph.nodes.filter(n=>Number.isFinite(n.x)).map(n=>[n.id,{x:n.x,y:n.y,depth:n.depth}]):[]);
 const regions=[...new Set(graph.nodes.map(n=>n.region))],buckets=new Map();
 nodeByID=new Map(graph.nodes.map(n=>[n.id,n]));edgeAdjacency=new Map();
 structure=window.AideStarStructure?.build(graph.nodes,graph.edges)||null;prepareStellarConnections();
 for(const n of graph.nodes){const path=(n.path||'').split('/').filter(x=>x&&x!=='.');const branch=n.kind==='session'?'conversations':path.length>2?path.slice(0,2).join('/'):path.length>1?path[0]:'root';const key=n.region+'/'+branch;
  if(!buckets.has(key))buckets.set(key,{key,name:branch==='root'?(n.region==='workspace'?'Workspace':n.region==='sessions'?'Conversations':n.name):branch,region:n.region,nodes:[]});buckets.get(key).nodes.push(n);}
 clusters=[...buckets.values()];
 for(const r of regions){const group=clusters.filter(c=>c.region===r),ri=regions.indexOf(r),rx=r==='workspace'?.05:r==='sessions'?-.9:.65+(ri%3)*.2,ry=r==='workspace'?.04:r==='sessions'?.15:(ri%3-1)*.42;
  group.forEach((c,i)=>{const angle=i*2.399963,ring=group.length===1?0:.12+.17*Math.sqrt(i/group.length);const old=preserve&&oldClusters.get(c.key);c.x=old?.x??rx+Math.cos(angle)*ring;c.y=old?.y??ry+Math.sin(angle)*ring*1.4;c.depth=old?.depth??.95+seeded(c.key)*.15;c.phase=seeded(c.key);c.color=colors(c);c.projected={x:0,y:0,onScreen:false};c.texture=cloudTexture(c.key,c.color);
   const spread=old?.radius??Math.min(.3,.055+Math.sqrt(c.nodes.length)*.014);c.radius=spread;
   c.nodes.forEach(n=>{const angle=seeded(n.id+'a')*tau,r=Math.sqrt(seeded(n.id+'r'))*spread;n.x=c.x+Math.cos(angle)*r;n.y=c.y+Math.sin(angle)*r*.68;n.depth=c.depth+(seeded(n.id+'z')-.5)*.08;if(oldPositions.has(n.id))Object.assign(n,oldPositions.get(n.id));n.cluster=c;
    applyStellarColor(n);n.drawLabel=n.name.length>27?n.name.slice(0,27)+'…':n.name;n.projected=preserve&&n.projected?n.projected:{n,x:0,y:0,onScreen:false};});});}
 for(const n of graph.nodes){if(n.kind!=='symbol'||preserve&&oldPositions.has(n.id))continue;const p=nodeByID.get(n.parentFile);if(!p)continue;const a=seeded(n.id+'orbit')*tau,r=.016+seeded(n.id+'span')*.038;n.x=p.x+Math.cos(a)*r;n.y=p.y+Math.sin(a)*r;n.depth=p.depth-.01;}
 for(const e of graph.edges){e.a=nodeByID.get(e.from)?.projected;e.b=nodeByID.get(e.to)?.projected;e.phase=seeded(e.from+e.to);e.hierarchy=['contains','defines'].includes(e.kind);e.dashed=['mention','call_candidate'].includes(e.kind);e.directed=['call_candidate','call_typed','imports'].includes(e.kind);
  for(const id of [e.from,e.to]){if(!edgeAdjacency.has(id))edgeAdjacency.set(id,[]);edgeAdjacency.get(id).push(e);}}
 if(!preserve){cosmos=window.AideStarCosmos?.build(graph.nodes,graph.edges)||null;cosmicSceneCache.clear();cosmicTextureCache.clear();cosmicScene=null;cosmicKey='';cosmicRecords=[];cosmicLinks=[];cosmicStars=[];cosmicStarEdges=[];cosmicFlight=null;cosmicScopeKey='';liveStableScenes.clear();
  if(cosmos)enterCosmic(cosmos.root.key,false);else{cosmicEnabled=false;syncCosmicNav();}
 }else liveReconcile=true;
 invalidateView();
}
function eligibleEdge(e){return !codeView||edgeMode==='all'||edgeMode==='calls'&&['call_candidate','call_typed'].includes(e.kind)||edgeMode==='imports'&&e.kind==='imports'||edgeMode==='hierarchy'&&e.hierarchy;}
function focusIDs(){
 if(!codeView||!focusCode||!selected)return null;
 const key=selected.id+'|'+edgeMode+'|'+callDepth+'|'+renderRevision;if(focusKey===key&&focusCache)return focusCache;
 const max=callDepth==='all'?Infinity:Number(callDepth)||2,ids=new Set([selected.id]);
 if(structure){const layers=structure.describe(selected.id,edgeMode);for(const [id,depth]of layers.levels){if(depth<=max)ids.add(id);}for(const id of layers.incoming||[])ids.add(id);}
 else{let frontier=[selected.id];for(let depth=0;depth<max&&frontier.length;depth++){const next=[];for(const id of frontier){for(const e of edgeAdjacency.get(id)||[]){if(!eligibleEdge(e))continue;const to=e.from===id?e.to:e.from;if(!ids.has(to)){ids.add(to);next.push(to);}}}frontier=next;}}
 focusKey=key;focusCache=ids;return ids;
}
function refreshView(){
 const key=[renderRevision,region,codeView,focusCode,selected?.id||'',edgeMode,callDepth,searchQuery].join('|');if(key===renderKey)return;
 renderKey=key;const focus=focusIDs(),globalLayers=structure?.entry('calls'),callLayers=selected&&globalLayers?.levels.has(selected.id)?structure.describe(selected.id,'calls'):globalLayers;layerState=structure?(selected?structure.describe(selected.id,edgeMode):globalLayers):null;
 renderNodes=[];const visibleIDs=new Set();
 for(const n of graph.nodes){if(region&&n.region!==region||focus&&!focus.has(n.id)||codeView&&n.kind==='session')continue;
  const depth=callLayers?.levels.get(n.id),finite=!!globalLayers?.levels.has(n.id)&&Number.isFinite(depth);n.visualDepth=finite?depth:null;
  // Call distance remains a separate static structure cue; connection degree
  // controls the color/halo baseline. Neither is a physical measurement.
  const d=finite?Math.min(depth,16):9;
  n.magnitude=finite?1.1+1.65*Math.log2(d+1):4.3;n.luminosity=finite?Math.max(.10,Math.pow(10,-.4*(n.magnitude-1.1))):.24;
  n.apparentRadius=Math.max(1,.9+2.3/(1+d*.48));n.globalVisualDepth=globalLayers?.levels.get(n.id);const globalDepth=Number.isFinite(n.globalVisualDepth)?n.globalVisualDepth:9;n.spatialDepth=n.depth*(1+Math.min(globalDepth,12)*.07);
  n.match=!!searchQuery&&resultIDs.has(n.id);n.chosen=selected?.id===n.id;n.incoming=!!selected&&!!callLayers?.incoming.has(n.id);
  const visual=stellarVisual(n,finite?depth:null,n.chosen,n.incoming);n.apparentRadius=visual.radius;
  n.drawAlpha=searchQuery&&!n.match&&!n.chosen?.20:visual.alpha;
  n.glowSize=visual.halo;renderNodes.push(n);visibleIDs.add(n.id);
 }
 renderEdges=graph.edges.filter(e=>eligibleEdge(e)&&visibleIDs.has(e.from)&&visibleIDs.has(e.to));if(selected)renderEdges.sort((a,b)=>Number(b.from===selected.id||b.to===selected.id)-Number(a.from===selected.id||a.to===selected.id));const batches=new Map();for(const e of renderEdges){if(!batches.has(e.kind))batches.set(e.kind,{edges:[],kind:e.kind,dashed:e.dashed,hierarchy:e.hierarchy});batches.get(e.kind).edges.push(e);}edgeBatches=[...batches.values()];
 projection=renderNodes.map(n=>n.projected);
 // The optional UI consumes the same traversal that drives brightness/focus.
 updateLayerLabels();
}
function updateLayerLabels(){
 const title=layerState?.mode==='calls'?'静态调用候选':layerState?.mode==='imports'?'模块导入':layerState?.mode==='hierarchy'?'声明层级':'关系';
 const maxDepth=layerState?.maxDepth??0,hasLayers=!!layerState?.levels.size,hopName=layerState?.mode==='calls'?'调用':layerState?.mode==='imports'?'导入':'结构';
 const localRange=selected?'已解析至 '+maxDepth+' 跳'+(focusCode?' · 聚焦 '+(callDepth==='all'?'全部已解析跳数':'向外 '+callDepth+' 跳'):''):'';
 if($('layer-summary'))$('layer-summary').textContent=hasLayers?`${title} · ${selected?'从当前节点向外':(layerState.rootIDs?.size||0)+' 个入口候选'} · ${selected?localRange:'全局 '+(maxDepth+1)+' 层'}`:'当前索引没有可分层的连接';
 const callLayers=structure?.entry('calls'),selectedHasCalls=selected&&callLayers?.levels.has(selected.id);
 if($('magnitude-key'))$('magnitude-key').textContent=callLayers?.levels.size?(selectedHasCalls?'当前节点最亮 · 调用跳数 1、2、3…逐步变暗':'同等关联数下，静态调用层 1、2、3…逐层变暗')+' · 星等仅表示调用结构距离':'无调用层证据 · 星色与光晕按有效关联数';
 if($('detail-level')){
  $('detail-level').hidden=!selected;
  const globalDepth=selected?.globalVisualDepth,cycle=selected&&callLayers?.cycles.has(selected.id);
  $('detail-level').textContent=(selected?`${selected.connectionCount||0} 个有效关联 · ${selected.stellarBand} · `:'')+(Number.isFinite(globalDepth)?`调用星等 · 全局静态调用层 ${globalDepth+1}`:'调用关系未知 · 基准星等')+(selected?' · 当前节点（选中增亮）':'')+(cycle?' · 候选闭环':'');

 }
}
function projectInto(n,out,driftX,driftY,depth=n.depth){out.x=width*(.57+skyFloat.x)+Math.sin(n.x+yaw+driftX)*width*.43*zoom/depth;out.y=height*(.5+skyFloat.y)+Math.sin(n.y+pitch+driftY)*height*.47*zoom/depth;out.onScreen=out.x>-70&&out.x<width+70&&out.y>-70&&out.y<height+70;return out;}
function edgeInView(e){const a=e.a,b=e.b;return a&&b&&Math.max(a.x,b.x)>=-70&&Math.min(a.x,b.x)<=width+70&&Math.max(a.y,b.y)>=-70&&Math.min(a.y,b.y)<=height+70;}
function draw(now=performance.now()){
 frame=0;if(document.hidden)return;
 const active=!!(drag||cameraTween||wheelZoom||graphFade||cosmicFlight||now<interactionUntil||now<liveAnimationUntil),budget=active?1000/60:1000/30;
 if(!paintRequested&&lastPaint!==null&&now-lastPaint<budget-1){redraw(false);return;}
 paintRequested=false;const delta=lastPaint===null?0:clamp(now-lastPaint,0,64);lastPaint=now;if(animate)skyTime+=delta;
 // A bounded, rigid sky drift preserves relations and hit testing. Its clock
 // pauses during manipulation and while motion is disabled, without snapping.
 if(motionEnabled()&&!drag&&!cameraTween&&!wheelZoom&&!cosmicFlight&&now>=interactionUntil){
  driftTime+=delta;skyFloat.x=Math.sin(driftTime/22000)*.038;skyFloat.y=Math.sin(driftTime/29000)*.024;
 }
 const t=skyTime;updateCamera(now);
 const dpr=Math.min(devicePixelRatio,1.75);if(width!==innerWidth||height!==innerHeight||pixelRatio!==dpr){width=innerWidth;height=innerHeight;pixelRatio=dpr;canvas.width=Math.round(width*dpr);canvas.height=Math.round(height*dpr);ctx.setTransform(dpr,0,0,dpr,0,0);graphFade=null;makeSky();}
 ctx.drawImage(skyCache,0,0,width,height);
 for(const s of stars){const x=((s.x*width-yaw*33-t/2800)%width+width)%width,y=((s.y*height+pitch*20)%height+height)%height;ctx.globalAlpha=.06+s.a*.16+Math.sin(t/2400+s.a*17)*.04;ctx.fillStyle=mapPalette.star;ctx.beginPath();ctx.arc(x,y,s.r,0,tau);ctx.fill();}ctx.globalAlpha=1;
 refreshView();ensureCosmicView();
 if(cosmicEnabled&&cosmicScene){
  drawCosmos(now,t);drawEvidencePath();
  if(graphFade){const u=clamp((now-graphFade.start)/graphFade.duration,0,1);ctx.save();ctx.globalAlpha=1-easing(u);ctx.drawImage(graphFade.canvas,0,0,width,height);ctx.restore();if(u===1)graphFade=null;}
  if(infrared.transitioning||animate||cameraTween||wheelZoom||graphFade||cosmicFlight||now<liveAnimationUntil)redraw(false);return;
 }
 refreshView();const driftX=0,driftY=0;
 for(const c of clusters){if(region&&c.region!==region)continue;const p=projectInto(c,c.projected,driftX,driftY),size=Math.max(110,c.radius*width*3.3*zoom),density=Math.min(.85,.16+c.nodes.length/85);
  if(p.x+size/2<0||p.x-size/2>width||p.y+size*.4<0||p.y-size*.4>height)continue;
  ctx.save();ctx.globalAlpha=density;ctx.translate(p.x,p.y);ctx.rotate(-.3+Math.sin(t/32000+c.phase)*.018);ctx.drawImage(c.texture,-size/2,-size*.38,size,size*.76);ctx.restore();
  if(c.nodes.length>8){ctx.font='10px -apple-system,sans-serif';ctx.fillStyle=c.color+'99';ctx.fillText(c.name+' / '+c.nodes.length,p.x+18,p.y-size*.2);}}
 for(const n of renderNodes)projectInto(n,n.projected,driftX,driftY,n.spatialDepth);
 infrared.draw(ctx,renderNodes.map(n=>({x:n.projected.x,y:n.projected.y,degree:n.connectionCount||0,distance:n.spatialDepth,scale:zoom,birth:birthAlpha(n,now)})),width,height,now);
 // Each non-selected relation kind is stroked as one path. A dense graph no
 // longer dispatches a separate Canvas stroke/state change for every edge.
 for(const batch of edgeBatches){if(renderNodes.length>120&&batch.hierarchy)continue;
  ctx.strokeStyle=mapPalette.star+(batch.hierarchy?'18':'30');ctx.lineWidth=.55;ctx.setLineDash(batch.dashed?[3,5]:[]);ctx.beginPath();
  for(const e of batch.edges){if(selected&&(e.from===selected.id||e.to===selected.id)||!edgeInView(e)||birthAlpha(nodeByID.get(e.from)||{},now)<.97||birthAlpha(nodeByID.get(e.to)||{},now)<.97)continue;ctx.moveTo(e.a.x,e.a.y);ctx.lineTo(e.b.x,e.b.y);}ctx.stroke();
 }
 let particles=0,lastDash=null;
 for(const e of renderEdges){if(!edgeInView(e)||birthAlpha(nodeByID.get(e.from)||{},now)<.97||birthAlpha(nodeByID.get(e.to)||{},now)<.97)continue;const a=e.a,b=e.b,activeEdge=!!selected&&(e.from===selected.id||e.to===selected.id);
  if(renderNodes.length>120&&!activeEdge&&e.hierarchy)continue;
  if(activeEdge||e.directed&&renderNodes.length<120){ctx.strokeStyle=mapPalette.star+(activeEdge?'90':'30');ctx.lineWidth=activeEdge?1.2:.55;
   if(lastDash!==e.dashed){ctx.setLineDash(e.dashed?[3,5]:[]);lastDash=e.dashed;}
   if(activeEdge){ctx.beginPath();ctx.moveTo(a.x,a.y);ctx.lineTo(b.x,b.y);ctx.stroke();}
   if(e.directed){const angle=Math.atan2(b.y-a.y,b.x-a.x),x=b.x-Math.cos(angle)*7,y=b.y-Math.sin(angle)*7;ctx.beginPath();ctx.moveTo(x,y);ctx.lineTo(x-Math.cos(angle-.45)*5,y-Math.sin(angle-.45)*5);ctx.moveTo(x,y);ctx.lineTo(x-Math.cos(angle+.45)*5,y-Math.sin(angle+.45)*5);ctx.stroke();}}
  if(animate&&!e.hierarchy&&particles<48&&(activeEdge||e.phase<.20)){const progress=(t/6500+e.phase)%1,px=a.x+(b.x-a.x)*progress,py=a.y+(b.y-a.y)*progress;ctx.fillStyle=mapPalette.star+(activeEdge?'ff':'88');ctx.beginPath();ctx.arc(px,py,activeEdge?1.65:.9,0,tau);ctx.fill();particles++;}
 }ctx.setLineDash([]);
 for(const n of renderNodes){const {x,y,onScreen}=n.projected;if(!onScreen)continue;const chosen=n.chosen,match=n.match,r=n.apparentRadius+(chosen?1.5:0),g=n.glowSize;
  ctx.globalAlpha=n.drawAlpha*birthAlpha(n,now);ctx.drawImage(n.glow,x-g/2,y-g/2,g,g);ctx.fillStyle=n.starColor;ctx.beginPath();ctx.arc(x,y,r*(.4+.6*birthAlpha(n,now)),0,tau);ctx.fill();ctx.globalAlpha=1;
  if(birthAlpha(n,now)>.5&&(chosen||match||renderNodes.length<120&&n.kind==='directory'&&n.path==='.')){ctx.font='11px -apple-system,sans-serif';ctx.fillStyle=chosen?mapPalette.text:mapPalette.muted;ctx.fillText(n.drawLabel,x+10,y+4);}
  if(chosen){ctx.strokeStyle=mapPalette.star+'80';ctx.beginPath();ctx.arc(x,y,15+Math.sin(t/650)*1.4,0,tau);ctx.stroke();
   ctx.strokeStyle=mapPalette.star+'45';ctx.lineWidth=.8;ctx.beginPath();ctx.moveTo(x-10,y);ctx.lineTo(x+10,y);ctx.moveTo(x,y-10);ctx.lineTo(x,y+10);ctx.stroke();}}
 drawEvidencePath();
 ctx.font='9px -apple-system,sans-serif';ctx.fillStyle=mapPalette.muted+'66';for(let i=0;i<9;i++){const x=width*(i+1)/10;ctx.fillRect(x,height*.82,1,5);ctx.fillText(String(i*30).padStart(3,'0')+'°',x-8,height*.82+19);}
 if(graphFade){const u=clamp((now-graphFade.start)/graphFade.duration,0,1);ctx.save();ctx.globalAlpha=1-easing(u);ctx.drawImage(graphFade.canvas,0,0,width,height);ctx.restore();if(u===1)graphFade=null;}
 drawLiveGhosts(now,0);
 if(infrared.transitioning||animate||cameraTween||wheelZoom||graphFade||now<liveAnimationUntil)redraw(false);
}
function redraw(dirty=true){if(dirty)paintRequested=true;if(!frame&&!document.hidden)frame=requestAnimationFrame(draw);}
function nodeResults(announce=true){
 const q=$('search').value.trim().toLowerCase(),terms=q.split(/\s+/).filter(Boolean);searchQuery=q;
 results=graph.nodes.filter(n=>n.kind!=='directory'&&(!region||region===n.region)).map(n=>({n,score:terms.reduce((score,k)=>score+(n.id.toLowerCase()===k?100:n.name.toLowerCase().includes(k)?15:n.signature?.toLowerCase().includes(k)?12:n.path?.toLowerCase().includes(k)?10:n.text?.toLowerCase().includes(k)?2:0),0)})).filter(v=>!q||v.score>0).sort((a,b)=>b.score-a.score||a.n.id.localeCompare(b.n.id)).map(v=>v.n);
 resultIDs=new Set(results.map(n=>n.id));invalidateView();
 const box=$('results'),previous=new Map([...box.children].map(el=>[el.dataset.nodeID,el])),keep=new Set();
 for(const [i,n]of results.slice(0,40).entries()){
  let button=previous.get(n.id);if(!button){button=document.createElement('button');button.className='result';button.type='button';button.dataset.nodeID=n.id;button.append(document.createElement('code'),document.createElement('span'));const path=document.createElement('small');path.className='result-path';button.lastChild.append(path);appendSourceLabel(button.lastChild,n);button.onclick=()=>{const node=nodeByID.get(button.dataset.nodeID);if(node)select(node);};}
  const mark=button.firstChild,label=button.lastChild,path=label.querySelector('.result-path'),meta=label.querySelector('.source-meta');if(mark.textContent!==n.id)mark.textContent=n.id;
  if(!label.firstChild||label.firstChild===path)label.insertBefore(document.createTextNode(n.name),path);else if(label.firstChild.textContent!==n.name)label.firstChild.textContent=n.name;
  const locator=n.path||'会话 #'+n.number;if(path.textContent!==locator)path.textContent=locator;
  meta.sourceNode=n;meta.textContent=nodeSourceLabel(n);meta.title=meta.textContent;
  keep.add(button);if(box.children[i]!==button)box.insertBefore(button,box.children[i]||null);
 }
 for(const child of [...box.children])if(!keep.has(child))child.remove();
 if(announce){const s=sourceByRegion.get(region);say(!results.length&&s?`${s.name} · ${sourceStateLabel(s.state)} · 当前范围暂无可检索节点`:q?`找到 ${results.length} 个节点`:'点击星点，开始探索');}redraw();
}
function search(){resetDocumentBatch();searchQuery=$('search').value.trim().toLowerCase();if(retrievalMode()!=='nodes'){documentGeneration++;selected=null;revealPanel($('detail'),false);documentAbort?.abort();$('results').replaceChildren();results=[];resultIDs.clear();invalidateView();say('输入问题后点击探索，检索文档正文。');redraw();return;}nodeResults();}

function select(n,focus=true){if(selected?.id!==n.id)beginGraphTransition();$('document-insight').hidden=true;selected=nodeByID.get(n.id)||n;revealPanel($('detail'),true);if(cosmicEnabled&&focus)cosmicLocate(selected);else if(!cosmicEnabled&&focus&&Number.isFinite(selected.x))moveCamera({yaw:-selected.x,pitch:-selected.y,zoom:Math.max(zoom,1.08)},650);$('detail-name').textContent=n.name;$('detail-region').textContent=n.kind==='symbol'?'CODE / '+n.language:n.region==='sessions'?'CONVERSATION / 会话':n.region==='workspace'?'WORKSPACE / 工作区':'REFERENCE / 引用';$('detail-id').textContent=n.id;$('detail-path').textContent=(n.path||'会话 #'+n.number)+(n.line?' : '+n.line+'–'+n.endLine:'');$('detail-text').textContent=n.text||'此节点仅索引名称，尚未读取正文。';const edges=edgeAdjacency.get(n.id)||[];$('neighbors').textContent=`${edges.length} 条连接 · ${n.kind==='symbol'?'声明附近片段':(n.size||0)+' 字节'}`;$('insert').disabled=n.kind==='directory';$('understand').disabled=n.kind==='directory';syncNodeSourceChrome();renderCodeInsight(n);redraw();}
function renderCodeInsight(n){
 const on=codeView||n.kind==='symbol';$('code-insight').hidden=!on;if(!on)return;
 $('code-signature').textContent=n.signature||'';$('code-location').textContent=n.kind==='symbol'?`${n.language} · ${n.symbolKind} · L${n.line}–L${n.endLine} · ${n.id}`+(n.contentHash?' · SHA '+n.contentHash.slice(0,10):''):'文件 / 模块视角';
 $('code-open').hidden=!n.path||n.kind==='directory';const spec={root:n.root,path:n.path,readOnly:true,origin:n.origin||'',knowledgeWorkspace:graph.workspace};if(n.line){spec.line=n.line;spec.endLine=n.endLine;spec.codeHash=n.contentHash;}if(n.source)spec.source=n.source;if(n.format)spec.format=n.format;$('code-open').href='/#file='+encodeURIComponent(JSON.stringify(spec));
 const byID=nodeByID,edges=graph.edges;
 function list(id,title,items){const box=$(id);box.replaceChildren();const h=document.createElement('h3');h.textContent=title+' · '+items.length;box.append(h);for(const {node,edge,label}of items.slice(0,24)){const b=document.createElement('button');b.type='button';b.className='code-relation';b.textContent=(label||'')+node.name+(edge?.line?' · L'+edge.line:'');const confidence={source_declaration:'声明证据',source_path:'路径匹配',name_candidate:'名称候选',type_binding:'类型绑定'}[edge?.confidence]||'';if(confidence){const tag=document.createElement('small');tag.textContent=confidence;b.append(tag);}b.title=(node.path||'')+' / '+node.id+(edge?.evidence?' / '+edge.evidence:'');b.onclick=()=>select(node);box.append(b);}if(items.length>24){const p=document.createElement('p');p.textContent='仅显示前24项，可用函数名继续搜索。';box.append(p);}if(!items.length){const p=document.createElement('p');p.textContent='当前索引内未发现；不代表项目中不存在。';box.append(p);}}
 const mapped=(es,dir)=>es.map(e=>({node:byID.get(e[dir]),edge:e})).filter(x=>x.node);
 const hierarchy=[...mapped(edges.filter(e=>['defines','contains'].includes(e.kind)&&e.to===n.id),'from').map(x=>({...x,label:'上级 / '})),...mapped(edges.filter(e=>['defines','contains'].includes(e.kind)&&e.from===n.id),'to').map(x=>({...x,label:'声明 / '}))];
 list('code-hierarchy','代码层级',hierarchy);list('code-callers','调用关系 ←',mapped(edges.filter(e=>['call_candidate','call_typed'].includes(e.kind)&&e.to===n.id),'from'));list('code-callees','调用关系 →',mapped(edges.filter(e=>['call_candidate','call_typed'].includes(e.kind)&&e.from===n.id),'to'));
 list('code-imports','导入路径证据',mapped(edges.filter(e=>e.kind==='imports'&&(e.from===n.id||e.from===n.parentFile)),'to'));
 const unresolved=(graph.code?.unresolvedRelations||[]).filter(r=>r.from===n.id||r.file===n.id),unresolvedBox=$('code-unresolved');unresolvedBox.replaceChildren();
 const unresolvedTitle=document.createElement('h3');unresolvedTitle.textContent='未解析关系 · '+unresolved.length+(graph.code?.relationsTruncated?'（记录达到上限）':'');unresolvedBox.append(unresolvedTitle);
 const reasons={dynamic_binding:'动态绑定',ambiguous_name:'名称歧义',outside_index_or_unknown_binding:'索引外或绑定未知',outside_index_or_unresolved_path:'索引外或路径未解析'};
 for(const r of unresolved.slice(0,24)){const file=byID.get(r.file),row=document.createElement(file?.path?'a':'p');row.className='code-relation';row.textContent=(r.kind==='import'?'导入 / ':'调用 / ')+r.name+(r.line?' · L'+r.line:'');const reason=document.createElement('small');reason.textContent=reasons[r.reason]||'未解析';row.append(reason);if(file?.path){const link={root:file.root,path:file.path,origin:file.origin||'',readOnly:true,knowledgeWorkspace:graph.workspace,codeHash:file.contentHash};if(file.source)link.source=file.source;if(r.line)link.line=r.line;row.href='/#file='+encodeURIComponent(JSON.stringify(link));row.target='_blank';row.rel='noopener';row.title='查看源位置 · '+file.path;}unresolvedBox.append(row);}
 if(unresolved.length>24||!unresolved.length){const p=document.createElement('p');p.className='code-caveat';p.textContent=unresolved.length>24?'仅显示前24项；更多记录可在索引证据中检查。':'当前记录未发现；不代表没有未解析关系。';unresolvedBox.append(p);}
 const adj=new Map();for(const e of edges.filter(e=>['call_candidate','call_typed'].includes(e.kind))){if(!adj.has(e.from))adj.set(e.from,[]);adj.get(e.from).push(e.to)}
 let cycle=null,visited=0;const pending=[[n.id]];while(pending.length&&visited++<600){const route=pending.shift();if(route.length>12)continue;for(const to of adj.get(route.at(-1))||[]){if(to===n.id){cycle=[...route,to];break;}if(!route.includes(to)&&pending.length<600)pending.push([...route,to]);}if(cycle)break;}
 $('code-recursion').textContent=cycle?'静态候选闭环：'+cycle.map(id=>byID.get(id)?.name||id).join(' → '):'未发现12层 / 600步范围内的候选闭环；不构成无递归证明。';
}
function syncCodeView(){for(const [id,on]of [['view-code',codeView],['view-knowledge',!codeView]]){$(id).setAttribute('aria-pressed',String(on));$(id).disabled=loading;}$('code-tools').hidden=!codeView;$('retrieval-mode').disabled=codeView;document.body.classList.toggle('code-view',codeView);}
$('view-code').onclick=()=>{if(loading||codeView)return;codeView=true;syncCodeView();load()};$('view-knowledge').onclick=()=>{if(loading||!codeView)return;codeView=false;syncCodeView();load()};
if($('call-depth')){callDepth=$('call-depth').value;$('call-depth').onchange=()=>{beginGraphTransition();relationshipOverview();callDepth=$('call-depth').value;if(selected){focusCode=true;$('focus-code').setAttribute('aria-pressed','true');}else say('先选中节点，深度将用于向外聚焦。');invalidateView();redraw();};}
$('edge-mode').onchange=()=>{beginGraphTransition();relationshipOverview();edgeMode=$('edge-mode').value;redraw()};$('focus-code').onclick=()=>{beginGraphTransition();relationshipOverview();focusCode=!focusCode;$('focus-code').setAttribute('aria-pressed',String(focusCode));if(focusCode&&!selected)say('先选中一个文件或函数，再查看指定深度的关系。');redraw()};syncCodeView();

function syncGraphChrome(){
 $('counts').textContent=`${graph.nodes.length} STARS / ${graph.edges.length} CONNECTIONS`;
 $('code-summary').textContent=graph.code?`${graph.code.files} 份源码 · ${graph.code.symbols} 个声明 · ${graph.code.calls} 条调用候选 · ${graph.code.typedCalls||0} 条类型绑定 · ${graph.code.unresolved} 个未解析调用 · ${graph.code.unresolvedImports||0} 个未解析导入`:'';
 $('warnings').textContent=[...(graph.warnings||[]),...(graph.code?.diagnostics||[]),...(graph.truncated?['索引达到数量、深度或时间上限；未覆盖全部文件。']:[])].join('\n');
 const box=$('regions'),previous=new Map([...box.children].map(el=>[el.dataset.region,el])),keep=new Set();
 sourceCatalog=currentSources();
 // A removed source does not silently broaden an existing search to all sources.
 // Retain its selected chip until the user explicitly chooses another region.
 if(region&&!sourceCatalog.some(s=>s.region===region)){const old=previous.get(region)?.sourceDescriptor;sourceCatalog.push({id:old?.id||region,name:old?.name||fallbackSourceName(region),type:old?.type||'',region,state:'unavailable',nodeCount:graph.nodes.filter(n=>n.region===region).length,detached:true,message:'当前索引不再报告此来源，已保留筛选。请选择其他星域以继续。'});}
 sourceByRegion=new Map(sourceCatalog.map(s=>[s.region,s]));
 for(const [i,s]of [{region:'',name:'全部星域'},...sourceCatalog].entries()){
  const r=s.region;let button=previous.get(r);if(!button){button=document.createElement('button');button.type='button';button.dataset.region=r;button.append(document.createElement('span'),document.createElement('small'));button.firstChild.className='source-chip-name';button.lastChild.className='source-chip-meta';button.onclick=()=>{beginGraphTransition();documentGeneration++;documentAbort?.abort();aiAbort?.abort();$('ai-found').replaceChildren();$('answer').textContent='';selected=null;revealPanel($('detail'),false);region=button.dataset.region;cosmicSceneCache.clear();cosmicKey='';if(cosmos)enterCosmic(cosmos.root.key,true);syncGraphChrome();search();};}
  button.sourceDescriptor=s;button.classList.toggle('source-chip',!!r);button.dataset.state=s.state||'';button.firstChild.textContent=s.name;
  const state=s.detached?'已移除':sourceCoverageState(s),type=sourceTypeLabel(s.type),meta=r?`${type} · ${state} · ${s.nodeCount} 颗星`:'';
  button.dataset.count=r?String(s.nodeCount):String(graph.nodes.length);button.lastChild.textContent=r?state:'已索引节点，非文件总数';button.lastChild.hidden=false;button.title=r?[s.name,meta,'来源 #'+s.id,sourceCoverageLabel(s),s.message].filter(Boolean).join(' · '):'查看全部已索引来源';button.setAttribute('aria-label',r?s.name+'，'+meta+'，来源 '+s.id:s.name);button.setAttribute('aria-pressed',String(region===r));keep.add(button);if(box.children[i]!==button)box.insertBefore(button,box.children[i]||null);
 }
 for(const child of [...box.children])if(!keep.has(child))child.remove();
 const catalog=$('source-catalog'),prior=new Map([...catalog.children].map(el=>[el.dataset.region,el])),retained=new Set();
 for(const [i,s]of sourceCatalog.entries()){let item=prior.get(s.region);if(!item){item=document.createElement('li');item.dataset.region=s.region;item.append(document.createElement('strong'),document.createElement('span'),document.createElement('small'));}item.dataset.state=s.state;item.classList.toggle('source-current',s.region===region);item.children[0].textContent=s.name;item.children[1].textContent=`${sourceTypeLabel(s.type)} · ${s.detached?'已移除':sourceStateLabel(s.state)} · ${s.nodeCount} 颗星 · 来源 #${s.id}`+(s.region===region?' · 当前筛选':'');item.children[2].textContent=[sourceCoverageLabel(s),s.message].filter(Boolean).join('。')||(s.type==='mcp'?'仅已保存的工具名称与说明；不调用工具，也不读取工具执行结果。':s.state==='ready'&&!s.nodeCount?'当前来源尚无可索引节点。':'');item.children[2].hidden=!item.children[2].textContent;retained.add(item);if(catalog.children[i]!==item)catalog.insertBefore(item,catalog.children[i]||null);}
 for(const item of [...catalog.children])if(!retained.has(item))item.remove();$('source-catalog-section').hidden=!sourceCatalog.length;
 syncNodeSourceChrome();
}
function drawLiveGhosts(now,drift){
 if(!liveGhosts.length)return;const remaining=[];ctx.save();
 for(const ghost of liveGhosts){const age=(now-ghost.start)/380;if(age>=1)continue;remaining.push(ghost);
  const x=ghost.cosmic?width*(.57+skyFloat.x)+(ghost.x+yaw*.22+drift)*width*zoom/ghost.z:ghost.x,y=ghost.cosmic?height*(.5+skyFloat.y)+(ghost.y+(pitch-.1)*.18)*height*zoom/ghost.z:ghost.y;
  ctx.globalAlpha=(1-easing(clamp(age,0,1)))*.55;ctx.fillStyle=ghost.color;ctx.beginPath();ctx.arc(x,y,ghost.radius*(1-age*.4),0,tau);ctx.fill();
 }ctx.restore();liveGhosts=remaining;
}
function applyLiveUpdate(update,initial=false){
 if(!update||typeof update.revision!=='string'||!Array.isArray(update.nodes)||!Array.isArray(update.edges))throw Error('自动同步响应不完整');
 if(initial||update.workspace!==graph.workspace)clearEvidencePath('星图已同步，请选择路径节点。',true);
 else if(update.revision!==liveCursor){
  if(pathAbort)clearEvidencePath('查询期间索引已更新，请重新查询。');
  else if(pathResult){
   const ids=new Set(pathResult.nodes.map(n=>n.id)),keys=new Set(pathResult.steps.map(s=>edgeKey(s.edge)));
   const changed=!pathResult.found||(update.reset&&pathResult.nodes.some(n=>!update.nodes.some(current=>current.id===n.id)))||(update.removedNodes||[]).some(id=>ids.has(id))||update.nodes.some(n=>ids.has(n.id)&&liveWireNodes.get(n.id)!==JSON.stringify(n))||(update.removedEdges||[]).some(e=>keys.has(edgeKey(e)))||(update.reset&&pathResult.steps.some(s=>!update.edges.some(e=>edgeKey(e)===edgeKey(s.edge))));
   if(changed)clearEvidencePath('路径涉及的节点或关系已变化，请重新查询。');
   else pathResult.revision=update.revision; // Every displayed node and edge is still present, unchanged.
  }
 }
 const differentWorkspace=!!graph.workspace&&graph.workspace!==update.workspace;
 if(initial||differentWorkspace||!graph.workspace){
  if(!initial)graphGeneration++;
  if(aiAbort&&!aiAbort.signal.aborted&&$('ask').disabled)$('answer').textContent='工作区资料已更新，当前理解已中止；请重新探索。';aiAbort?.abort();
  liveGhosts=[];liveAnimationUntil=0;liveStableScenes.clear();liveReconcile=false;liveWireNodes=new Map(update.nodes.map(n=>[n.id,JSON.stringify(n)]));
  graph={workspace:update.workspace,nodes:update.nodes,edges:update.edges,sources:Array.isArray(update.sources)?update.sources:null,warnings:update.warnings||[],truncated:!!update.truncated,code:update.code};
  documentGeneration++;documentAbort?.abort();$('document-insight').hidden=true;selected=null;revealPanel($('detail'),false);region='';layout();syncGraphChrome();search();return;
 }
 const incoming=new Map(update.nodes.map(n=>[n.id,n])),removed=new Set(update.removedNodes||[]),oldNodes=new Map(graph.nodes.map(n=>[n.id,n]));
 if(update.reset)for(const id of oldNodes.keys())if(!incoming.has(id))removed.add(id);
 const changed=[],added=[];
 for(const raw of update.nodes){if(liveWireNodes.get(raw.id)===JSON.stringify(raw))continue;changed.push(raw);if(!oldNodes.has(raw.id))added.push(raw);}
 const oldEdges=new Map(graph.edges.map(e=>[edgeKey(e),e])),nextEdges=update.reset?new Map(update.edges.map(e=>[edgeKey(e),oldEdges.get(edgeKey(e))||e])):new Map(oldEdges);
 if(!update.reset){for(const e of update.removedEdges||[])nextEdges.delete(edgeKey(e));for(const e of update.edges)nextEdges.set(edgeKey(e),oldEdges.get(edgeKey(e))||e);}
 for(const [key,e]of nextEdges)if(removed.has(e.from)||removed.has(e.to))nextEdges.delete(key);
 const edgesChanged=oldEdges.size!==nextEdges.size||[...nextEdges.keys()].some(k=>!oldEdges.has(k));
 if(Array.isArray(update.sources))graph.sources=update.sources;
 graph.warnings=update.warnings||[];graph.truncated=!!update.truncated;graph.code=update.code;syncGraphChrome();
 if(!changed.length&&!removed.size&&!edgesChanged)return;
 const now=performance.now(),motion=motionEnabled(),selectionID=selected?.id,selectionChanged=changed.some(n=>n.id===selectionID),citation=selected?.documentCitation;
 if(motion)for(const star of cosmicStars){if(removed.has(star.n.id)&&star.p.onScreen)liveGhosts.push({cosmic:true,x:star.x,y:star.y,z:star.z,color:mapPalette.star,radius:1.3,start:now});}
 if(liveGhosts.length>512)liveGhosts=liveGhosts.slice(-512);
 for(const id of removed){oldNodes.delete(id);liveWireNodes.delete(id);}
 for(const raw of changed){let node=oldNodes.get(raw.id);if(!node){node={};oldNodes.set(raw.id,node);}for(const field of nodeFields)if(!(field in raw))delete node[field];Object.assign(node,raw);liveWireNodes.set(raw.id,JSON.stringify(raw));}
 if(motion)for(const [i,raw]of added.entries())oldNodes.get(raw.id).bornAt=now+Math.min(i,20)*32;
 graph.nodes=[...oldNodes.values()];graph.edges=[...nextEdges.values()];graphGeneration++;documentGeneration++;documentAbort?.abort();
 if(aiAbort&&!aiAbort.signal.aborted&&$('ask').disabled)$('answer').textContent='资料有更新，当前理解已中止；请重新理解最新资料。';aiAbort?.abort();
 if(selectionID&&!oldNodes.has(selectionID)){selected=null;$('insert').disabled=true;$('understand').disabled=true;revealPanel($('detail'),false);}
 else if(selectionID)selected=!selectionChanged&&citation?{...oldNodes.get(selectionID),documentCitation:citation}:oldNodes.get(selectionID);
 layout(true);if(selected&&citation&&!selectionChanged)selected={...nodeByID.get(selectionID),documentCitation:citation};if(retrievalMode()==='nodes')nodeResults(false);else{invalidateView();if(selectionChanged)$('document-note').textContent='当前原文已更新；保留检索结果，重新探索可核对最新摘录。';}
 refreshView();ensureCosmicView();syncGraphChrome();
 if(selected){$('detail-name').textContent=selected.name;$('detail-path').textContent=(selected.path||'会话 #'+selected.number)+(selected.line?' : '+selected.line+'–'+selected.endLine:'');$('detail-region').textContent=selected.kind==='symbol'?'CODE / '+selected.language:selected.region==='sessions'?'CONVERSATION / 会话':selected.region==='workspace'?'WORKSPACE / 工作区':'REFERENCE / 引用';$('insert').disabled=selected.kind==='directory';$('understand').disabled=selected.kind==='directory';$('neighbors').textContent=`${(edgeAdjacency.get(selected.id)||[]).length} 条连接`;if(selectionChanged){$('detail-text').textContent=selected.text||'此节点仅索引名称。';$('document-insight').hidden=true;delete selected.documentCitation;}renderCodeInsight(selected);}
 liveAnimationUntil=motion?now+(added.length?Math.min(added.length-1,20)*32+680:380):0;
 say('已同步'+(added.length?' · '+added.length+' 颗新星':'')+(removed.size?' · '+removed.size+' 个节点移出':'')+(changed.length>added.length?' · '+(changed.length-added.length)+' 个节点更新':''));redraw();
}
async function load(){
 clearEvidencePath('正在同步星图，请同步完成后查询。',true);
 if(loading||locked)return;stopLive(true);loading=true;const generation=++graphGeneration,mode=codeView;loadAbort=new AbortController();syncCodeView();say('正在连接本地星图…');
 try{const update=await api('/updates?code='+(mode?'1':'0'),{signal:loadAbort.signal});if(locked||generation!==graphGeneration||mode!==codeView)return;
  const input=recoveryInput;
  if(recoveryScopeLoaded!==update.workspace){
   recoveryLoading=true;
   const saved=await window.AideContinuity?.readTab('knowledge:'+update.workspace,'scene','starmap').catch(()=>null);
   recoveryLoading=false;
   if(locked||generation!==graphGeneration||mode!==codeView)return;
   const changedWorkspace=!!graph.workspace&&graph.workspace!==update.workspace;
   recoveryScopeLoaded=update.workspace;pendingRecoveredView=input===recoveryInput?(saved?.version===1?saved:changedWorkspace?defaultSkyView():null):null;
   if(changedWorkspace){aiAbort?.abort();aiAbort=null;$('answer').textContent='';$('question').value='';$('usage').textContent='';$('ai-found').replaceChildren();resetDocumentBatch();}
   if(pendingRecoveredView?.version===1&&!params.has('code')&&typeof pendingRecoveredView.codeView==='boolean'&&pendingRecoveredView.codeView!==codeView){codeView=pendingRecoveredView.codeView;graphGeneration++;return;}
  }
  beginGraphTransition();applyLiveUpdate(update,true);if(pendingRecoveredView){restoreSkyView(pendingRecoveredView);pendingRecoveredView=null;}liveCursor=update.revision;knowledgeTimeline.sync({workspace:graph.workspace,code:codeView,revision:liveCursor});liveFailures=0;
 }catch(e){if(!locked&&e.name!=='AbortError'){liveFailures++;say(e.message);}}
 finally{loading=false;syncCodeView();redraw();if(!locked&&generation!==graphGeneration)load();else scheduleLive();}
}
function syncMotion(){ $('motion').textContent=animate?'动效 · 开':'动效 · 关';$('motion').setAttribute('aria-pressed',String(animate)); }
$('motion').onclick=()=>{if(motionReduced()){say('当前已减少动态效果，请在工作台外观或系统设置中调整。');return;}animate=!animate;if(!animate){for(const n of graph.nodes)delete n.bornAt;liveGhosts=[];liveAnimationUntil=0;if(cameraTween){({yaw,pitch,zoom}=cameraTween.to);cameraTween=null;}if(wheelZoom){zoom=wheelZoom.target;wheelZoom=null;}graphFade=null;cosmicFlight=null;settlePanels();}syncMotion();redraw();};syncMotion();
addEventListener('visibilitychange',()=>{
 const now=performance.now();cancelAnimationFrame(frame);frame=0;
 if(document.hidden){persistSkyView();stopLive();pausedAt=now;drag=null;settlePanels();}
 else{scheduleLive(0);if(pausedAt!==null){if(cameraTween)cameraTween.start+=now-Math.max(pausedAt,cameraTween.start);if(graphFade)graphFade.start+=now-Math.max(pausedAt,graphFade.start);if(cosmicFlight)cosmicFlight.start+=now-Math.max(pausedAt,cosmicFlight.start);const pause=now-pausedAt;for(const n of graph.nodes)if(n.bornAt)n.bornAt+=pause;for(const ghost of liveGhosts)ghost.start+=pause;if(liveAnimationUntil)liveAnimationUntil+=pause;cosmicGate+=pause;}pausedAt=null;if(wheelZoom)wheelZoom.last=now;lastPaint=now;redraw();}
});
$('search-form').onsubmit=e=>{e.preventDefault();if(retrievalMode()==='nodes')search();else searchDocuments()};$('search').oninput=search;$('close-detail').onclick=()=>{beginGraphTransition();selected=null;revealPanel($('detail'),false);redraw()};$('home').onclick=()=>{if(cosmicEnabled&&cosmos&&cosmicKey!==cosmos.root.key)enterCosmic(cosmos.root.key);else moveCamera({yaw:0,pitch:.1,zoom:1},850);};
canvas.onpointerdown=e=>{if(locked||cosmicEnabled&&cosmicFlight)return;interruptCamera();interactionUntil=performance.now()+220;drag={x:e.clientX,y:e.clientY,startX:e.clientX,startY:e.clientY};canvas.setPointerCapture(e.pointerId);};
canvas.onpointermove=e=>{if(!drag){if(cosmicEnabled&&!cosmicFlight){const r=cosmicPick(e.clientX,e.clientY),next=r?.group.key||'';if(next!==cosmicHover){cosmicHover=next;canvas.style.cursor=next?'pointer':'grab';redraw();}}return;}yaw+= (e.clientX-drag.x)/width*2;if(cosmicEnabled)yaw=clamp(yaw,-2.5,2.5);pitch=clamp(pitch+(e.clientY-drag.y)/height*1.5,-1,1);drag.x=e.clientX;drag.y=e.clientY;interactionUntil=performance.now()+220;redraw();};
canvas.onpointerup=e=>{if(drag&&Math.hypot(e.clientX-drag.startX,e.clientY-drag.startY)<5){if(cosmicEnabled){const star=cosmicStarPick(e.clientX,e.clientY);if(star)select(star.n,false);else{const r=cosmicPick(e.clientX,e.clientY);if(r&&!r.group.nodeID){cosmicSelected=r.group.key;syncCosmicNav();say('已选中 '+r.group.label+'，双击或继续滚轮靠近，进入这个星域。');redraw();}}}else{let nearest=null,distance=20;for(const p of projection){if(!p.onScreen)continue;const d=Math.hypot(p.x-e.clientX,p.y-e.clientY);if(d<distance){nearest=p;distance=d;}}if(nearest)select(nearest.n,false);}}drag=null;interactionUntil=performance.now()+180;};
canvas.ondblclick=e=>{if(!cosmicEnabled||cosmicFlight||locked)return;const star=cosmicStarPick(e.clientX,e.clientY);if(star){const path=cosmos.pathFor(star.n.id),index=path.findIndex(g=>g.key===cosmicKey),child=path[index+1];if(child)enterCosmic(child.key,true,true);select(star.n,false);return;}const r=cosmicPick(e.clientX,e.clientY);if(r&&!r.group.nodeID)enterCosmic(r.group.key);else if(cosmicScene?.group.parent)enterCosmic(cosmicScene.group.parent.key);};
canvas.onpointerleave=()=>{cosmicHover='';canvas.style.cursor='grab';};
canvas.onpointercancel=()=>{drag=null;};
canvas.addEventListener('wheel',e=>{
 e.preventDefault();if(locked)return;
 const now=performance.now();updateCamera(now);if(cameraTween){cameraTween=null;}
 const units=e.deltaMode===1?16:e.deltaMode===2?height:1,delta=clamp(e.deltaY*units,-240,240),base=wheelZoom?.target??zoom,target=clamp(base*Math.exp(-delta*.001),.5,2.8);
 interactionUntil=now+250;
 if(cosmicEnabled&&cosmicScene){
  if(now-cosmicWheelLast>260||Math.sign(cosmicWheel)!==Math.sign(delta))cosmicWheel=0;cosmicWheel+=delta;cosmicWheelLast=now;
  if(now>cosmicGate&&delta<0&&target>=2.15&&cosmicWheel<-65&&cosmicScene.children.length){const child=cosmicStarPick(e.clientX,e.clientY)?.owner.group||cosmicPick(e.clientX,e.clientY)?.group||cosmicPreferred();if(child&&child.depth>cosmicScene.depth){enterCosmic(child.key);if(child.nodeID){const n=nodeByID.get(child.nodeID);if(n)select(n,false);}return;}}
  if(now>cosmicGate&&delta>0&&target<=.64&&cosmicWheel>65&&cosmicScene.group.parent){enterCosmic(cosmicScene.group.parent.key);return;}
 }
 if(!motionEnabled()){zoom=target;wheelZoom=null;}else{wheelZoom={target,last:now};}
 redraw();
},{passive:false});
$('ai-toggle').onclick=()=>{const shown=$('ai-toggle').getAttribute('aria-expanded')!=='true';revealPanel($('ai-body'),shown);$('ai-toggle').setAttribute('aria-expanded',String(shown))};$('understand').onclick=()=>{openAI();$('question').value=selected?.documentCitation?'请依据原文关键词 '+$('search').value+' 解释这份文档的相关内容，引用片段编号及定位，并列出缺口。':codeView?'请依据源码和行号解释此节点的代码层级、输入输出、分支、副作用、调用链与未解析调用。区分声明证据和调用候选，不假定已经运行。':'请解释这个节点的内容、相关文件与证据缺口。';$('question').focus()};
async function askAI(find=false){
 if(locked)return;
 const generation=graphGeneration,docGeneration=documentGeneration;aiAbort?.abort();aiAbort=new AbortController();
 const docs=retrievalMode()!=='nodes';const ids=find?[]:(selected?[selected]:(docs?[]:results)).filter(n=>n.kind!=='directory').slice(0,8).map(n=>n.id);
 $('ask').disabled=true;$('find-ai').disabled=true;$('answer').textContent=docs?'正在检索文档片段，再生成带引用回答…':find?'正在搜索知识目录，再依据资料理解…':'正在依据选中资料理解…';$('ai-found').replaceChildren();
 try {
  const d=await api('/assist',{method:'POST',signal:aiAbort.signal,body:JSON.stringify({query:$('question').value||$('search').value,ids,workspace:graph.workspace,code:codeView,retrievalMode:docs?'rag':'nodes',region})});
  if(locked||generation!==graphGeneration||docGeneration!==documentGeneration)return;
  $('answer').textContent=d.answer;if(d.retrieval){renderDocumentResults(d.retrieval,'ai-found');$('warnings').textContent=d.retrieval.warnings.join('\n');}$('usage').textContent=d.model+' · '+JSON.stringify(d.usage);
  for(const n of d.retrieval?[]:(d.references||[])){const b=document.createElement('button');b.type='button';b.className='result';const code=document.createElement('code'),label=document.createElement('span');code.textContent=n.id;label.textContent=n.name;appendSourceLabel(label,n);b.append(code,label);b.onclick=()=>select(n);$('ai-found').append(b);}
 }catch(e){if(!locked&&generation===graphGeneration&&docGeneration===documentGeneration&&e.name!=='AbortError')$('answer').textContent=e.message}finally{$('ask').disabled=false;$('find-ai').disabled=false}
}
$('ask').onclick=()=>askAI(false);$('find-ai').onclick=()=>askAI(true);
let insertionSessions=[];
function requestInsertionSessions(){if(channel&&!locked)channel.postMessage({type:'sessions'});}
$('insert-session').addEventListener('focus',requestInsertionSessions);
$('insert').onclick=()=>{
 if(locked||!selected)return;
 if(!channel){say('请从工作台的星图按钮打开，以连接会话。');return;}
 const value=$('insert-session').value.trim();
 const target=value?insertionSessions.find(s=>s.id===value||String(s.number)===value.replace(/^#/,'')||('#'+s.number+' · '+s.title)===value):null;
 if(value&&!target){say('未找到目标会话，请选择列表中的会话或输入完整 ID／#编号。');requestInsertionSessions();return;}
 channel.postMessage({type:'insert',session:target?.id||'',id:selected.id,workspace:graph.workspace,origin:selected.origin||'',document:selected.documentCitation||null});say('正在插入目标会话草稿…');
};
if(channel){channel.onmessage=e=>{
 if(e.data?.type==='ack')say(e.data.message);
 if(e.data?.type==='sessions'){
  if(locked)return;
  if(e.data.error){say(e.data.error);return;}
  insertionSessions=Array.isArray(e.data.sessions)?e.data.sessions:[];
  $('insert-sessions').replaceChildren(...insertionSessions.map(s=>{const option=document.createElement('option');option.value=s.id;option.label='#'+s.number+' · '+s.title;return option;}));
  const original=insertionSessions.find(s=>s.id===e.data.originalSession);
  $('insert-target-note').textContent=(original?'默认原会话 #'+original.number+'；':'')+'支持完整 ID 或 #编号，仅插入草稿。';
 }
};}

addEventListener('resize',redraw);addEventListener('pagehide',()=>{persistSkyView();pageLeaving=true;stopLive();cancelAnimationFrame(frame);frame=0;settlePanels();channel?.close();});let locked=true,lockStateKnown=false;
addEventListener('pageshow',()=>{pageLeaving=false;scheduleLive(0);redraw();});
function setLocked(value){
 // Presence heartbeats may repeat the same effective state. Only a transition
 // should clear or reload the graph; the first notification still initializes it.
 if(lockStateKnown&&locked===value)return;
 if(value&&!locked)persistSkyView();
 lockStateKnown=true;locked=value;knowledgeTimeline.lock(value);if(value)clearEvidencePath('星图已锁定。',true);if(value){clearTimeout(recoveryTimer);recoveryScopeLoaded='';pendingRecoveredView=null;recoveryInput++;}if(value){insertionSessions=[];$('insert-sessions').replaceChildren();}else requestInsertionSessions();if(value){logbook.open=false;stopLive(true);liveWireNodes.clear();liveStableScenes.clear();liveGhosts=[];liveAnimationUntil=0;drag=null;settlePanels();cameraTween=null;wheelZoom=null;graphFade=null;}document.body.classList.toggle('map-locked',value);if(value){resetDocumentBatch();documentGeneration++;documentAbort?.abort();$('document-insight').hidden=true;graphGeneration++;loadAbort?.abort();aiAbort?.abort();graph={nodes:[],edges:[],sources:[],warnings:[]};sourceCatalog=[];sourceByRegion.clear();$('regions').replaceChildren();$('source-catalog').replaceChildren();$('source-catalog-section').hidden=true;$('detail-source').textContent='';$('warnings').textContent='';$('code-summary').textContent='';$('code-insight').hidden=true;projection=[];clusters=[];selected=null;renderNodes=[];renderEdges=[];edgeBatches=[];structure=null;layerState=null;cosmos=null;cosmicScene=null;cosmicKey='';cosmicRecords=[];cosmicLinks=[];cosmicStars=[];cosmicStarEdges=[];cosmicCallState=null;cosmicScopeKey='';cosmicSelected='';cosmicHover='';cosmicFlight=null;cosmicSceneCache.clear();cosmicTextureCache.clear();syncCosmicNav();nodeByID.clear();edgeAdjacency.clear();resultIDs.clear();searchQuery='';invalidateView();$('ai-found').replaceChildren();$('detail').hidden=true;$('results').replaceChildren();$('answer').textContent='';}else load();redraw();}
if(window.LockCluster){LockCluster.on('effective',setLocked);LockCluster.onReady().then(setLocked);}else setLocked(false);
for(const event of ['pointerdown','pointermove','keydown','wheel'])addEventListener(event,()=>{if(!locked)window.LockCluster?.noteActivity();},{passive:true});
redraw();

function respectMotion(){if(motionReduced()){animate=false;for(const n of graph.nodes)delete n.bornAt;liveGhosts=[];liveAnimationUntil=0;if(cameraTween){({yaw,pitch,zoom}=cameraTween.to);cameraTween=null;}if(wheelZoom){zoom=wheelZoom.target;wheelZoom=null;}graphFade=null;cosmicFlight=null;settlePanels();syncMotion();redraw();}}
addEventListener('aide:motion',respectMotion);motionMedia.addEventListener('change',respectMotion);
window.aideUI?.subscribe(syncMapPalette);
matchMedia('(forced-colors: active)').addEventListener('change',syncMapPalette);

$('retrieval-mode').onchange=()=>{beginGraphTransition();documentGeneration++;documentAbort?.abort();aiAbort?.abort();$('ai-found').replaceChildren();$('answer').textContent='';selected=null;revealPanel($('detail'),false);search();};
function selectDocument(hit){select(hit.file);const origin=hit.file.origin||selected.origin||'';selected={...selected,format:hit.file.format||selected.format,origin,documentCitation:{locator:hit.locator,offset:hit.offset,hash:hit.hash,origin}};syncNodeSourceChrome();$('detail-text').textContent=hit.text;$('document-insight').hidden=false;$('document-locator').textContent=hit.id+' · '+hit.locator+' · 段内字符偏移 '+hit.offset;$('document-hash').textContent='SHA-256 '+hit.hash;const spec={root:hit.file.root,path:hit.file.path,readOnly:true,origin,knowledgeWorkspace:graph.workspace};if(hit.file.source)spec.source=hit.file.source;if(hit.file.format)spec.format=hit.file.format;$('document-open').href='/#file='+encodeURIComponent(JSON.stringify(spec));}
function renderDocumentResults(data,id='results'){const box=$(id);box.replaceChildren();for(const hit of data.hits||[]){const b=document.createElement('button');b.type='button';b.className='result document-result';const title=document.createElement('span');title.textContent=hit.file.name+' · '+hit.locator;appendSourceLabel(title,hit.file);const snippet=document.createElement('small');snippet.textContent=hit.text.slice(0,220);title.append(snippet);const code=document.createElement('code');code.textContent=hit.file.id;b.append(code,title);b.onclick=()=>selectDocument(hit);box.append(b);}if(!(data.hits||[]).length){const p=document.createElement('p');p.textContent='未检索到匹配原文；请检查关键词、来源范围与提取诊断。';box.append(p);}}
async function searchDocuments(continuing=false){
 if(locked||loading)return;
 const mode=retrievalMode(),query=$('search').value,workspace=graph.workspace;
 const key=JSON.stringify({query,mode,workspace,region});
 const previous=continuing&&documentBatch?.key===key?documentBatch:null;
 if(continuing&&!previous?.cursor)return;
 const gen=++documentGeneration,graphGen=graphGeneration;
 documentAbort?.abort();documentAbort=new AbortController();$('document-next').disabled=true;
 if(!previous)resetDocumentBatch();
 say('正在提取与检索文档正文…');
 try{
  const d=await api('/documents/search',{method:'POST',signal:documentAbort.signal,body:JSON.stringify({query,mode,workspace,region,cursor:previous?.cursor||''})});
  if(locked||gen!==documentGeneration||graphGen!==graphGeneration)return;
  const hits=[...(previous?.hits||[]),...d.hits],seen=new Set();
  documentBatch={key,cursor:d.nextCursor||'',hits:hits.filter(h=>!seen.has(h.id)&&seen.add(h.id))};
  renderDocumentResults({hits:documentBatch.hits});
  $('document-progress').hidden=false;
  $('document-coverage').textContent=`已检查 ${d.scanned} / ${d.total} 份已索引文档${d.nextCursor?' · 可继续检索':' · 当前范围检查完毕'}`;
  $('document-next').hidden=!d.nextCursor;
  $('warnings').textContent=[...d.warnings,...(d.truncated?['索引／提取／结果达到限制，不能据此声称全文无匹配。']:[])].join('\n');
  say(`${d.files} 份文档 · ${d.chunks} 个片段 · 累计 ${documentBatch.hits.length} 条结果 · ${mode==='original'?'逐字原文':d.engine?.startsWith('weighted-rrf')?'关键词 + 向量 RAG':'本地关键词 RAG'}`);
 }catch(e){if(!locked&&gen===documentGeneration&&e.name!=='AbortError'){resetDocumentBatch();say(e.message);}}
 finally{if(gen===documentGeneration)$('document-next').disabled=false;}
}
$('document-next').onclick=()=>searchDocuments(true);
