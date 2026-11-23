interface Window {
  AideWorkbenchClient?: { create: (pluginId: string) => any };
}
'use strict';
// Bundle this source into a plugin HTML. Never load parent credentials or arbitrary host URLs.
(function(){
 function create(pluginId: string){
  if(typeof pluginId!=='string'||!/^[A-Za-z0-9_-]{1,64}$/.test(pluginId))throw Error('Invalid plugin id');
  let port: any=null,tools: string[]=[],capabilities: string[]=[],next=0,connecting: Promise<any> | null=null,settle: any=null,closed=false;const pending=new Map<any, any>(),listeners=new Set<any>();
  const revoke=(reason: string): void=>{closed=true;port?.close();port=null;for(const p of pending.values()){clearTimeout(p.timer);p.reject(Error(reason));}pending.clear();if(settle){settle.reject(Error(reason));clearTimeout(settle.timer);settle=null;}window.removeEventListener('message',receive);};
  function receive(e: MessageEvent): void{const m=e.data;if(closed||port||e.source!==window.parent||m?.type!=='aide.workbench.connect'||m.protocol!==1||m.pluginId!==pluginId||!Array.isArray(m.tools)||m.tools.some((x: any)=>typeof x!=='string')||e.ports?.length!==1)return;
   port=e.ports[0];tools=m.tools.slice();capabilities=Array.isArray(m.capabilities)?m.capabilities.filter((x: any)=>typeof x==='string').slice(0,16):[];port.onmessage=(event: any)=>{const value=event.data;if(value?.event==='scope.revoked'){revoke('Host scope revoked; verify effects before retry');for(const listener of listeners){try{listener(value);}catch(_){}}return;}if(value?.event){for(const listener of listeners)listener(value);return;}const p=pending.get(value?.requestId);if(!p)return;pending.delete(value.requestId);clearTimeout(p.timer);if(value.status==='error')p.reject(Error(value.error||'Host call failed'));else p.resolve(value);};port.start();clearTimeout(settle?.timer);settle?.resolve({protocol:1,tools:tools.slice(),capabilities:capabilities.slice()});settle=null;
  }
  window.addEventListener('message',receive);
  function connect(): Promise<any>{if(closed)return Promise.reject(Error('Client closed'));if(port)return Promise.resolve({protocol:1,tools:tools.slice(),capabilities:capabilities.slice()});if(!connecting){connecting=new Promise((resolve,reject)=>{settle={resolve,reject,timer:setTimeout(()=>revoke('Host bridge unavailable'),10000)};});window.parent.postMessage({type:'aide.workbench.ready',protocol:1,requestIds:'monotonic'},'*');}return connecting;}
  async function request(method: string,body: any): Promise<any>{await connect();if(closed||!port)throw Error('Bridge closed');if(pending.size)throw Error('A call is already pending');const requestId='r_'+(++next);return new Promise((resolve,reject)=>{const timer=setTimeout(()=>{pending.delete(requestId);reject(Error('Reply timeout; outcome uncertain, do not automatically retry'));},320000);pending.set(requestId,{resolve,reject,timer});try{port.postMessage({protocol:1,requestId,method,...body});}catch(e){pending.delete(requestId);clearTimeout(timer);reject(e);}});}
  return {connect,
   openWindow:(args: any)=>request('window.open',{arguments:args}),
   speech:(args: any)=>request('speech.local',{arguments:args}),
   workflow:(args: any)=>request('workflow.control',{arguments:args}),
   capture:(args: any)=>request('capture.control',{arguments:args}),
   captureNetwork:(args: any)=>request('capture.network',{arguments:args}),
   projectMessages:(args: any)=>request('project.messages',{arguments:args}),
   files:(args: any)=>request('files.select',{arguments:args}),
   fileTarget:(path: string)=>request('file.target',{arguments:{path}}),
   saveFile:(path: string,content: string,hash: string)=>request('file.save',{arguments:{path,content,hash}}),
   documentInfo:(asset='',options: any={})=>request('document.info',{arguments:{asset,metadataOnly:options.metadataOnly===true}}),
   readDocument:(args={})=>request('document.read',{arguments:args}),
   pluginAssetInfo:(asset: string,options: any={})=>request('document.package.info',{arguments:{asset,metadataOnly:options.metadataOnly===true}}),
   readPluginAsset:(args={})=>request('document.package.read',{arguments:args}),
   failDocumentPreview:()=>request('document.error',{arguments:{}}),
   setDocumentDirty:(dirty: boolean)=>request('document.dirty',{arguments:{dirty:dirty===true}}),
   abortDocumentSave:()=>request('document.save.abort',{arguments:{}}),
   beginDocumentSave:(size: number,version: any)=>request('document.save.begin',{arguments:{size,version}}),
   writeDocumentChunk:(offset: number,bytes: Uint8Array)=>{if(!(bytes instanceof Uint8Array)||bytes.byteLength>524288)throw Error('Expected Uint8Array of at most 512KiB');let binary='';for(let i=0;i<bytes.length;i+=8192)binary+=String.fromCharCode(...bytes.subarray(i,i+8192));return request('document.save.chunk',{arguments:{offset,data:btoa(binary)}});},
   requestDocumentSave:()=>request('document.save.request',{arguments:{}}),

   async saveDocument(bytes: Uint8Array,version: any){if(!(bytes instanceof Uint8Array))throw Error('Expected Uint8Array');await request('document.save.begin',{arguments:{size:bytes.byteLength,version}});for(let offset=0;offset<bytes.byteLength;offset+=524288){const chunk=bytes.subarray(offset,offset+524288);let binary='';for(let i=0;i<chunk.length;i+=8192)binary+=String.fromCharCode(...chunk.subarray(i,i+8192));await request('document.save.chunk',{arguments:{offset,data:btoa(binary)}});}return request('document.save.request',{arguments:{}});},
   async call(tool: string,args={},attachments: any[]=[]){await connect();if(!tools.includes(tool))throw Error('Tool not declared by this workbench');return request('tool.call',{tool,arguments:args,attachments});},proposalStatus:(proposalId: string)=>request('proposal.status',{proposalId}),onEvent(fn: any){listeners.add(fn);return()=>listeners.delete(fn);},close(){revoke('Client closed; verify effects before retry');}};
 }
 window.AideWorkbenchClient={create};
})();
