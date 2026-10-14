const assert=require('node:assert/strict'),fs=require('node:fs'),vm=require('node:vm');
const ctx={window:{}};vm.createContext(ctx);vm.runInContext(fs.readFileSync('internal/server/web/staged-output.js','utf8'),ctx);const api=ctx.window.AideStagedOutput;
const part=(id,content)=>({id,content,round:0});const step={name:'chat',content:''};
assert.equal(api.append(step,part('one','第一段')),true);assert.equal(api.append(step,part('one','第一段')),false);
assert.equal(api.pending(step,'第二段'),'第二段');assert.equal(api.text(step,'第二段'),'第一段\n\n第二段');
api.append(step,part('two','第二段'));step.content='第二段';assert.equal(api.pending(step,step.content),'');assert.equal(api.text(step),'第一段\n\n第二段');
const prev={id:'s',runs:[{id:'r',steps:[step]}]},stale={id:'s',runs:[{id:'r',steps:[{name:'chat',outputSegments:[part('one','第一段')]}]}]};api.merge(prev,stale);assert.equal(api.text(stale.runs[0].steps[0]),'第一段\n\n第二段');
for(const next of [{id:'other',runs:[{id:'r',steps:[{name:'chat'}]}]},{id:'s',runs:[{id:'new',steps:[{name:'chat'}]}]}]){api.merge(prev,next);assert.equal(api.text(next.runs[0].steps[0]),'');}
assert.equal(api.text({content:'旧回答'}),'旧回答');assert.equal(api.text({outputSegments:[part('one','早期')],content:'暂停的半段'}),'早期\n\n暂停的半段');
function el(tag,className,textContent=''){return {tag,className,textContent,dataset:{},children:[],append(...xs){this.children.push(...xs)}}}
const panel=api.render(step,{el,t:(s,n)=>s.replace('{0}',n),renderMarkdown:x=>'safe:'+x});assert.equal(panel.children.length,2);assert.equal(panel.children[0].children[1].innerHTML,'safe:第一段');assert.ok(!panel.children[0].children[1].className.includes('chat-answer'));
const app=fs.readFileSync('internal/server/web/app.js','utf8');const c={state:{historyLimit:30},window:{AideStagedOutput:api}};vm.createContext(c);vm.runInContext(app.slice(app.indexOf('function sessionRevision('),app.indexOf('function getSessionWindow(')),c);const before=c.sessionRevision(prev);api.append(step,part('three','第三段'));assert.notEqual(c.sessionRevision(prev),before);
assert.match(app,/state.stream !== es[\s\S]*addEventListener\('segment'/);assert.match(app,/copyText\(text, event.currentTarget\)/);
console.log('staged output PASS: append, final dedup, stale snapshots, session/run isolation, legacy data, partial pause, renderer and revision');

let paints=0,refreshes=0;class ES{constructor(){this.listeners={};}addEventListener(name,fn){this.listeners[name]=fn;}emit(name,data){this.listeners[name]({data:JSON.stringify(data)})}}
const liveStep={name:'chat'},run={id:'live',mode:'chat',steps:[liveStep]};const state={session:{id:'s',runs:[run]},live:{live:'first'},liveRound:{live:0},liveStable:{live:{}},runPhase:{}};
const streamCtx={state,window:{AideStagedOutput:api},EventSource:ES,closeStream(){state.stream=null},ensureRunPhase(){},touchRunActivity(){},renderSession(){paints++},refreshSessionSoon(){refreshes++},scheduleLiveRender(){},Date,encodeURIComponent};vm.createContext(streamCtx);const start=app.indexOf('function openStream('),end=app.indexOf('\n// 流式渲染节流',start);vm.runInContext(app.slice(start,end),streamCtx);streamCtx.openStream(run);const old=state.stream;
old.emit('segment',{step:'chat',round:0,segment:part('saved','first')});assert.equal(api.text(liveStep),'first');assert.equal(state.live.live,undefined);assert.equal(paints,1);
old.emit('delta',{round:1,text:'second'});assert.equal(state.live.live,'second');assert.equal(api.text(liveStep,state.live.live),'first\n\nsecond');
streamCtx.openStream(run);old.emit('segment',{step:'chat',round:1,segment:part('stale','wrong')});assert.equal(api.segments(liveStep).length,1,'closed stream cannot mutate a reopened conversation');
state.stream.emit('segment',{step:'chat',round:1,segment:part('final','second')});assert.equal(api.text(liveStep,'second'),'first\n\nsecond');assert.equal(state.live.live,undefined);
console.log('staged SSE PASS: real listeners preserve completed rounds, clear only matching live tail and reject stale streams');
