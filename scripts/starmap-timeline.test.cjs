const fs=require('node:fs'),vm=require('node:vm'),assert=require('node:assert/strict');
class Element{
 constructor(){this.children=[];this.handlers={};this.value='';this.open=false;this.disabled=false;this.hidden=false;this.textContent='';this.selectedIndex=0;}
 append(...nodes){this.children.push(...nodes);if(!this.value&&nodes[0]?.value)this.value=nodes[0].value;}
 replaceChildren(...nodes){this.children=[...nodes];this.value='';}
 addEventListener(name,fn){this.handlers[name]=fn;}
}
const ids=['knowledge-timeline','limits','timeline-compare','timeline-before','timeline-after','timeline-results','timeline-evidence','timeline-status','timeline-pages','timeline-page','timeline-prev','timeline-next','timeline-form'];
const elements=Object.fromEntries(ids.map(id=>[id,new Element()]));
const sandbox={window:{},document:{getElementById:id=>elements[id],createElement:()=>new Element()},AbortController,setTimeout,clearTimeout,addEventListener(){},Date};vm.createContext(sandbox);vm.runInContext(fs.readFileSync('internal/server/web/starmap-timeline.js','utf8'),sandbox);
(async()=>{
 let requests=[],resolve;const module=sandbox.window.AideKnowledgeTimeline.create({request:(path,options)=>{requests.push({path,options});return new Promise(r=>resolve=r);}});
 elements.limits.open=true;elements['knowledge-timeline'].open=true;
 module.sync({workspace:'w',code:false,revision:'1'});assert.equal(requests.length,1);
 resolve({entries:[{id:'h1',observed:'2026-01-01',nodes:1,edges:0},{id:'h2',observed:'2026-01-02',nodes:2,edges:1}],limit:32,bytesLimit:8388608,dropped:2});await new Promise(r=>setImmediate(r));assert.match(elements['timeline-status'].textContent,/已淘汰 2/);
 elements['timeline-before'].value='h1';elements['timeline-after'].value='h2';const pending=elements['timeline-form'].onsubmit({preventDefault(){}});assert.match(requests.at(-1).path,/timeline\/compare/);
 const snap=id=>({id,observed:'2026-01-01',graph:{sources:[{name:'工作区',state:'partial',coverage:{files:1,directories:1,textBytesPerFile:8192,pending:2}}],warnings:['部分覆盖']}});
 resolve({before:snap('h1'),after:snap('h2'),added:[],removed:[],changed:[{before:{id:'a',name:'note',text:'<script>old</script>'},after:{id:'a',name:'note',text:'new'}}],addedEdges:[],removedEdges:[]});await pending;assert.equal(elements['timeline-results'].children.length,1);assert.match(elements['timeline-status'].textContent,/变化 1/);assert.equal(elements['timeline-results'].children[0].children[1].children[2].textContent,'<script>old</script>');
 module.sync({workspace:'other',code:true,revision:'2'});assert.equal(elements['timeline-results'].children.length,0);const stale=resolve;module.lock(true);stale({entries:[{id:'leak'}],limit:32,bytesLimit:8388608});await new Promise(r=>setImmediate(r));assert.equal(elements['timeline-before'].children.length,0);assert.equal(elements['knowledge-timeline'].open,false);
 console.log('PASS timeline metadata, comparison excerpts, scope isolation, locked stale response');
})().catch(e=>{console.error(e);process.exitCode=1;});
