'use strict';
// Bundle this source into a plugin HTML. Never load parent credentials or arbitrary host URLs.
(function(){
 function create(pluginId){
  if(typeof pluginId!=='string'||!/^[A-Za-z0-9_-]{1,64}$/.test(pluginId))throw Error('Invalid plugin id');
  let port=null,tools=[],capabilities=[],next=0,connecting=null,settle=null,closed=false;const pending=new Map(),listeners=new Set();
  const revoke=reason=>{closed=true;port?.close();port=null;for(const p of pending.values()){clearTimeout(p.timer);p.reject(Error(reason));}pending.clear();if(settle){settle.reject(Error(reason));clearTimeout(settle.timer);settle=null;}window.removeEventListener('message',receive);};
  function receive(e){const m=e.data;if(closed||port||e.source!==window.parent||m?.type!=='aide.workbench.connect'||m.protocol!==1||m.pluginId!==pluginId||!Array.isArray(m.tools)||m.tools.some(x=>typeof x!=='string')||e.ports?.length!==1)return;
   port=e.ports[0];tools=m.tools.slice();capabilities=Array.isArray(m.capabilities)?m.capabilities.filter(x=>typeof x==='string').slice(0,16):[];port.onmessage=event=>{const value=event.data;if(value?.event==='scope.revoked'){revoke('Host scope revoked; verify effects before retry');return;}if(value?.event){for(const listener of listeners)listener(value);return;}const p=pending.get(value?.requestId);if(!p)return;pending.delete(value.requestId);clearTimeout(p.timer);if(value.status==='error')p.reject(Error(value.error||'Host call failed'));else p.resolve(value);};port.start();clearTimeout(settle?.timer);settle?.resolve({protocol:1,tools:tools.slice(),capabilities:capabilities.slice()});settle=null;
  }
  window.addEventListener('message',receive);
  function connect(){if(closed)return Promise.reject(Error('Client closed'));if(port)return Promise.resolve({protocol:1,tools:tools.slice(),capabilities:capabilities.slice()});if(!connecting){connecting=new Promise((resolve,reject)=>{settle={resolve,reject,timer:setTimeout(()=>revoke('Host bridge unavailable'),10000)};});window.parent.postMessage({type:'aide.workbench.ready',protocol:1,requestIds:'monotonic'},'*');}return connecting;}
  async function request(method,body){await connect();if(closed||!port)throw Error('Bridge closed');if(pending.size)throw Error('A call is already pending');const requestId='r_'+(++next);return new Promise((resolve,reject)=>{const timer=setTimeout(()=>{pending.delete(requestId);reject(Error('Reply timeout; outcome uncertain, do not automatically retry'));},320000);pending.set(requestId,{resolve,reject,timer});try{port.postMessage({protocol:1,requestId,method,...body});}catch(e){pending.delete(requestId);clearTimeout(timer);reject(e);}});}
  return {connect,async call(tool,args={},attachments=[]){await connect();if(!tools.includes(tool))throw Error('Tool not declared by this workbench');return request('tool.call',{tool,arguments:args,attachments});},proposalStatus:proposalId=>request('proposal.status',{proposalId}),onEvent(fn){listeners.add(fn);return()=>listeners.delete(fn);},close(){revoke('Client closed; verify effects before retry');}};
 }
 window.AideWorkbenchClient={create};
})();
