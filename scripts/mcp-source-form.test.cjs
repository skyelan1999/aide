'use strict';
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
const source = fs.readFileSync('internal/server/web/app.js', 'utf8');
const registry = new Map();
function element(tag) {
  const node = {tag,children:[],value:'',checked:false,hidden:false,dataset:{}, append(...children) { for (const child of children) {this.children.push(child);if (child && typeof child === 'object') child.parentNode=this;} if (tag==='select' && !this.value) this.value=children[0]?.value||'';},replaceChildren(){this.children=[];},close(){},reset(){}};
  Object.defineProperty(node,'id',{set(value){this._id=value;registry.set(value,this);},get(){return this._id;}});
  return node;
}
for(const id of ['src-type','src-rw','src-fields','src-name','source-form','source-dialog']){const e=element('input');e.id=id;}
registry.get('src-type').value='mcp';registry.get('src-type').options=[{value:'mcp',dataset:{}}];
const submitted=[];
const context=vm.createContext({console,URL,Math,$:id=>registry.get(id),el:(tag)=>element(tag),t:s=>s,document:{createTextNode:s=>s},editingSourceID:'',state:{sources:[],config:{}},action:fn=>fn,api:async(path,options)=>{submitted.push(JSON.parse(options.body));},sourceUpdatePayload:change=>[change],loadSourcesList:async()=>{},loadFiles:async()=>{},toast:()=>{}});
const start=source.indexOf('function renderSourceFields()');const end=source.indexOf("$('src-type').addEventListener",start);
assert(start>=0&&end>start);vm.runInContext(source.slice(start,end),context);
const submitStart=source.indexOf("$('source-form').onsubmit = action(async event => {");const submitEnd=source.indexOf('/* ── Markdown 渲染',submitStart);
assert(submitStart>=0&&submitEnd>submitStart);vm.runInContext(source.slice(submitStart,submitEnd),context);
(async()=>{
  context.renderSourceFields();
  assert.equal(registry.get('src-command').parentNode.hidden,false);
  assert.equal(registry.get('src-url').parentNode.hidden,true);
  assert.equal(registry.get('src-password').type,'password');
  registry.get('src-mcp-transport').value='streamable-http';registry.get('src-mcp-transport').onchange();
  assert.equal(registry.get('src-command').parentNode.hidden,true);assert.equal(registry.get('src-url').parentNode.hidden,false);
  registry.get('src-name').value='Docker MCP';registry.get('src-url').value='http://host.docker.internal:8811/mcp';registry.get('src-password').value='private-test-token';registry.get('src-mcp-docker').checked=true;
  await registry.get('source-form').onsubmit({preventDefault(){}});
  let body=submitted.at(-1),config=body.sources[0].config;
  assert.equal(config.transport,'streamable-http');assert.equal(config.dockerGateway,true);assert.equal(config.command,undefined);assert.equal(JSON.stringify(config).includes('private-test-token'),false);
  assert.equal(body.secrets[body.sources[0].id].password,'private-test-token');
  context.editingSourceID='existing';context.state.sources=[{id:'existing',type:'mcp',enabled:true,hasSecret:true}];registry.get('src-password').value='';
  await registry.get('source-form').onsubmit({preventDefault(){}});assert.equal(submitted.at(-1).secrets,undefined,'blank secret must preserve the vault entry');
  registry.get('src-mcp-transport').value='stdio';registry.get('src-mcp-transport').onchange();registry.get('src-command').value='node';registry.get('src-mcp-args').value='server.js\n--profile\nwith spaces';registry.get('src-password').value='hidden-token';
  await registry.get('source-form').onsubmit({preventDefault(){}});body=submitted.at(-1);assert.deepEqual(body.sources.at(-1).config,{transport:'stdio',command:'node',args:['server.js','--profile','with spaces']});assert.equal(body.secrets,undefined,'hidden HTTP token must not enter a stdio source');
  console.log('PASS MCP form: transport switching, encrypted-token payload, blank-token retention and legacy stdio arguments');
})().catch(error=>{console.error(error);process.exitCode=1;});
