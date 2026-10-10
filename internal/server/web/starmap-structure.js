'use strict';
// Visual layers describe indexed static relationships, never runtime stack depth.
window.AideStarStructure = Object.freeze({build(nodes, edges) {
 const byID=new Map(nodes.map(n=>[n.id,n])),graphs=new Map(),globalViews=new Map(),localViews=new Map();
 const kinds={calls:new Set(['call_candidate','call_typed']),imports:new Set(['imports']),hierarchy:new Set(['contains','defines'])};
 const resolve=mode=>mode!=='all'&&kinds[mode]?mode:edges.some(e=>['call_candidate','call_typed'].includes(e.kind))?'calls':edges.some(e=>e.kind==='imports')?'imports':'hierarchy';
 function adjacency(mode){
  if(graphs.has(mode))return graphs.get(mode);
  const out=new Map(),back=new Map();
  for(const e of edges){if(!kinds[mode].has(e.kind)||!byID.has(e.from)||!byID.has(e.to))continue;
   if(!out.has(e.from)){out.set(e.from,new Set());back.set(e.from,new Set());}
   if(!out.has(e.to)){out.set(e.to,new Set());back.set(e.to,new Set());}
   out.get(e.from).add(e.to);back.get(e.to).add(e.from);
  }
  const data={out,back};graphs.set(mode,data);return data;
 }
 function entry(mode='all'){
  mode=resolve(mode);if(globalViews.has(mode))return globalViews.get(mode);
  const {out,back}=adjacency(mode),seen=new Set(),order=[];
  // Iterative Kosaraju keeps deeply nested projects off the JavaScript call stack.
  for(const root of out.keys()){if(seen.has(root))continue;seen.add(root);const stack=[[root,out.get(root).values()]];
   while(stack.length){const top=stack[stack.length-1],next=top[1].next();if(next.done){order.push(top[0]);stack.pop();}
    else if(!seen.has(next.value)){seen.add(next.value);stack.push([next.value,out.get(next.value).values()]);}}
  }
  const component=new Map(),groups=[];
  for(let i=order.length-1;i>=0;i--){const root=order[i];if(component.has(root))continue;const index=groups.length,group=[],stack=[root];component.set(root,index);
   while(stack.length){const id=stack.pop();group.push(id);for(const previous of back.get(id)){if(!component.has(previous)){component.set(previous,index);stack.push(previous);}}}groups.push(group);
  }
  const next=groups.map(()=>new Set()),indegree=groups.map(()=>0),depth=groups.map(()=>0),cycles=new Set();
  groups.forEach((group,index)=>{if(group.length>1||out.get(group[0]).has(group[0]))for(const id of group)cycles.add(id);
   for(const id of group)for(const to of out.get(id)){const other=component.get(to);if(other!==index&&!next[index].has(other)){next[index].add(other);indegree[other]++;}}});
  const roots=indegree.map((count,i)=>count===0?i:-1).filter(i=>i>=0),pending=[...roots],rootIDs=new Set();
  for(const index of roots)for(const id of groups[index])rootIDs.add(id);
  for(let head=0;head<pending.length;head++){const index=pending[head];for(const other of next[index]){depth[other]=Math.max(depth[other],depth[index]+1);if(--indegree[other]===0)pending.push(other);}}
  const levels=new Map();groups.forEach((group,index)=>{for(const id of group)levels.set(id,depth[index]);});
  const view={levels,rootIDs,cycles,incoming:new Set(),maxDepth:depth.length?Math.max(...depth):0,mode,selectedID:null};globalViews.set(mode,view);return view;
 }
 function describe(selectedID,mode='all'){
  mode=resolve(mode);if(!selectedID)return entry(mode);
  const key=mode+'|'+selectedID;if(localViews.has(key))return localViews.get(key);
  const {out,back}=adjacency(mode),global=entry(mode),levels=new Map([[selectedID,0]]),pending=[selectedID];let maxDepth=0;
  for(let head=0;head<pending.length;head++){const id=pending[head],distance=levels.get(id);for(const to of out.get(id)||[]){if(!levels.has(to)){levels.set(to,distance+1);maxDepth=Math.max(maxDepth,distance+1);pending.push(to);}}}
  const view={levels,maxDepth,rootIDs:new Set([selectedID]),incoming:new Set(back.get(selectedID)||[]),cycles:global.cycles,mode,selectedID};
  // Only one selection per mode is needed by the renderer; avoid retained O(V²) maps.
  if(localViews.size>=4)localViews.clear();localViews.set(key,view);return view;
 }
 return Object.freeze({entry,describe});
}});
