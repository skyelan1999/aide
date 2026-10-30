'use strict';
const fs=require('node:fs'),vm=require('node:vm'),assert=require('node:assert/strict');
const source=fs.readFileSync('internal/server/web/app.js','utf8');
function fn(name){const at=source.indexOf('function '+name+'('),line=source.slice(at,source.indexOf('\n',at));return line.endsWith('}')?line:source.slice(at,source.indexOf('\n}',at)+2);}
function el(tag,cls='',text=''){return {tag,className:cls,textContent:text,children:[],dataset:{},scrollTop:0,classList:{toggle(){}},append(...x){this.children.push(...x);},replaceChildren(...x){this.children=x;},querySelector(){return null;},setAttribute(k,v){this[k]=v;}};}
const elements=Object.fromEntries(['strategy-menu','strategy-label','strategy-button','model-name'].map(id=>[id,el('div')]));
let assistant=false,choice={model:'code',strategy:'auto',profile:'default',reasoningEffort:'auto'};const writes=[];
const state={session:{id:'a'},mode:'chat',config:{configured:true,modelRouter:{available:false},models:[{id:'code',name:'Code'},{id:'write',name:'Writer'}]},profiles:{strategy:'auto',activeProfile:'default',profiles:[{id:'default',system:true}]}};
const c={state,window:{AideParallelChat:{choices:[[1,'standard']],value:()=>1,sync(){}},AideSessionModelSelection:{async change(p){writes.push(p);Object.assign(choice,p);}}},document:{body:{classList:{contains:()=>assistant}}},$:id=>elements[id],el,t:(s,...args)=>s.replace(/\{(\d+)\}/g,(_,n)=>args[n]),sessionModelPreferences:()=>choice,modelProviderName:()=>'',profileName:x=>x,composerAutoReview:()=>false,action:fn=>fn,recoveryScope:()=> 'fixture',closeStrategyMenu(){},toast(){},profilesManager:{flush(){throw Error('global write');}}};
vm.createContext(c);
const change=c.window.AideSessionModelSelection.change;
vm.runInContext(fs.readFileSync('internal/server/web/session-model-selection.js','utf8'),c);
c.window.AideSessionModelSelection.install({state});c.window.AideSessionModelSelection.change=change;
vm.runInContext(fn('refreshStrategyUI')+'\n'+fn('strategyMenuOption'),c);
function modelButtons(){return elements['strategy-menu'].children[1].children.filter(x=>x.tag==='button');}
(async()=>{
 c.refreshStrategyUI();assert.equal(modelButtons()[0].children[1].textContent,'Code','no plugin preserves ordinary menu');
 state.config.modelRouter.available=true;c.refreshStrategyUI();let buttons=modelButtons();assert.equal(buttons[0].children[1].textContent,'Auto');await buttons[0].onclick();assert.equal(writes.at(-1).modelRouter,'auto');assert.equal(choice.model,'code','manual fallback retained');
 c.refreshStrategyUI();assert.match(elements['strategy-label'].textContent,/Auto/);buttons=modelButtons();assert.equal(buttons[0]['aria-checked'],'true');assert.equal(buttons[1]['aria-checked'],'false');
 await buttons[2].onclick();assert.equal(writes.at(-1).model,'write');assert.equal(writes.at(-1).modelRouter,'','manual model exits Auto');
 choice.modelRouter='auto';state.config.modelRouter.available=false;c.refreshStrategyUI();assert(!modelButtons().some(b=>b.children[1]?.textContent==='Auto'));assert.equal(modelButtons()[1]['aria-checked'],'true','disabled router shows manual fallback');
 state.config.modelRouter.available=true;assistant=true;state.session.kind='assistant';c.refreshStrategyUI();assert(!modelButtons().some(b=>b.children[1]?.textContent==='Auto'),'assistant never gets plugin Auto choice');
 const routes=source.match(/AideParallelChat\?\.renderRoutes\(run/);assert(routes);assert(source.includes('run.modelRoutes || []'),'routing changes invalidate render signature');
 console.log('Auto router composer PASS: conditional top entry, session persistence path, manual exit, disabled fallback, assistant exclusion and route rendering');
})().catch(e=>{console.error(e);process.exitCode=1});
