'use strict';

// Celestial scale describes established groups, never accumulated leaf volume.
// One permanent universe contains the real snapshot. Missing scales are skipped.
window.AideStarCosmos = Object.freeze({build(nodes, edges) {
 const levels=Object.freeze(['知识宇宙','超星系团','星系团','本星系群','银河系','旋臂','恒星系统','节点行星']);
 // A parent needs this many distinct established units of the immediately lower
 // scale, within an actual source boundary. The universe itself is a container.
 const thresholds=Object.freeze([null,2,2,2,2,3,4,null]);
 const byID=new Map(),byKey=new Map(),rawByKey=new Map(),paths=new Map(),owners=new Map(),out=new Map(),back=new Map();
 const union=new Map(),rank=new Map(),minimum=new Map(),localIncident=new Set(),scopeByID=new Map();
 for(const node of nodes||[])if(node&&typeof node.id==='string'&&!byID.has(node.id)){byID.set(node.id,node);union.set(node.id,node.id);rank.set(node.id,0);minimum.set(node.id,node.id);}
 const validEdges=[];
 for(const edge of edges||[]){if(!edge||!byID.has(edge.from)||!byID.has(edge.to))continue;validEdges.push(edge);
  if(edge.kind==='defines'){const old=owners.get(edge.to);if(!old||edge.from<old)owners.set(edge.to,edge.from);}
 }
 function declarationFor(node){const owner=byID.get(owners.get(node.id));return node.kind==='symbol'&&owner&&owner.kind==='symbol'?'owner:'+owner.id:'kind:'+(node.kind==='symbol'?node.symbolKind||'symbol':node.kind);}
 for(const node of byID.values()){const file=node.kind==='symbol'&&byID.has(node.parentFile)?byID.get(node.parentFile):node;scopeByID.set(node.id,JSON.stringify([file.id,declarationFor(node)]));}
 function representative(id){let root=id;while(union.get(root)!==root)root=union.get(root);while(id!==root){const next=union.get(id);union.set(id,root);id=next;}return root;}
 function join(a,b){a=representative(a);b=representative(b);if(a===b)return;if(rank.get(a)<rank.get(b))[a,b]=[b,a];union.set(b,a);minimum.set(a,minimum.get(a)<minimum.get(b)?minimum.get(a):minimum.get(b));if(rank.get(a)===rank.get(b))rank.set(a,rank.get(a)+1);}
 for(const edge of validEdges){if(!['call_candidate','call_typed'].includes(edge.kind))continue;
  if(!out.has(edge.from)){out.set(edge.from,new Set());back.set(edge.from,new Set());}
  if(!out.has(edge.to)){out.set(edge.to,new Set());back.set(edge.to,new Set());}
  out.get(edge.from).add(edge.to);back.get(edge.to).add(edge.from);
  // Local components are genuine induced call graphs inside file/owner scope.
  // A global component's visual slices must not invent several global systems.
  if(scopeByID.get(edge.from)===scopeByID.get(edge.to)){join(edge.from,edge.to);localIncident.add(edge.from);localIncident.add(edge.to);}
 }
 // Global SCC flags retain cross-file recursion candidates. No runtime is run.
 const seen=new Set(),order=[],components=new Map(),cyclic=new Set();
 for(const id of out.keys()){if(seen.has(id))continue;seen.add(id);const stack=[[id,out.get(id).values()]];
  while(stack.length){const top=stack[stack.length-1],next=top[1].next();if(next.done){order.push(top[0]);stack.pop();}else if(!seen.has(next.value)){seen.add(next.value);stack.push([next.value,out.get(next.value).values()]);}}
 }
 for(let i=order.length-1;i>=0;i--){const id=order[i];if(components.has(id))continue;const members=[],stack=[id];components.set(id,id);
  while(stack.length){const member=stack.pop();members.push(member);for(const previous of back.get(member))if(!components.has(previous)){components.set(previous,id);stack.push(previous);}}
  if(members.length>1||out.get(id).has(id))for(const member of members)cyclic.add(member);
 }
 const callGroups=new Map();
 for(const id of localIncident){const key=minimum.get(representative(id));if(!callGroups.has(key))callGroups.set(key,{key,members:[],edges:0});callGroups.get(key).members.push(id);}
 for(const edge of validEdges)if(['call_candidate','call_typed'].includes(edge.kind)&&scopeByID.get(edge.from)===scopeByID.get(edge.to))callGroups.get(minimum.get(representative(edge.from))).edges++;
 const normalized=value=>String(value||'').replace(/\\/g,'/').split('/').filter(part=>part&&part!=='.').join('/');
 const dirname=value=>{const parts=value.split('/');parts.pop();return parts.join('/')||'.';};
 const base=value=>value.split('/').filter(Boolean).pop()||value;
 const kindNames={function:'函数',method:'方法',type:'类型',class:'类',interface:'接口',closure:'闭包',variable:'变量',constant:'常量',property:'属性'};
 const roles=['snapshot','source','project','directory','file','declaration','local-call-component','node'];
 const regionColor=region=>region==='workspace'?'#a7caff':region==='sessions'?'#f3d19b':'#89d7c8';
 function create(parent,metadataDepth,identity,label,region,description,evidence,metadata={}){
  const key=parent?parent.key+'/'+metadataDepth+':'+encodeURIComponent(identity):'cosmos:0';let group=rawByKey.get(key);
  if(!group){group={key,unitID:key,depth:metadataDepth,metadataDepth,label,region,color:regionColor(region),description,evidence,members:[],children:[],parent:parent||null,cyclic:false,connections:[],sourceMetadata:{role:roles[metadataDepth],classificationKey:key,label,evidence,...metadata}};rawByKey.set(key,group);if(parent)parent.children.push(group);}return group;
 }
 const root=create(null,0,'universe','全部已索引知识','', '永久知识宇宙容器。其他天体只有在真实下一级分组达到门槛时成立；单个银河不能因节点多而变成星系团。','真实索引快照');
 const sourceNames=new Map();
 for(const node of byID.values()){const scope=JSON.stringify([node.region||node.root||'workspace',node.root||'',node.source||'']);if(node.kind==='directory'&&(!node.path||node.path==='.'))sourceNames.set(scope,node.name||node.region);}
 for(const node of byID.values()){
  const region=node.region||node.root||'workspace',scope=JSON.stringify([region,node.root||'',node.source||'']);
  const path=normalized(node.path),fileNode=node.kind==='symbol'&&byID.has(node.parentFile)?byID.get(node.parentFile):node;
  const filePath=normalized(fileNode.path)||path,parts=filePath.split('/').filter(Boolean);
  const directory=node.kind==='directory'?(filePath||'.'):dirname(filePath),project=node.kind==='session'?'sessions':parts.length>1||node.kind==='directory'&&parts.length?parts[0]:'.';
  const sourceLabel=sourceNames.get(scope)||(region==='workspace'?'工作区':region==='sessions'?'会话':node.source||node.root||region);
  const source=create(root,1,scope,sourceLabel,region,'实际索引来源；须有至少两个已成立星系团才显示为超星系团。','region / root / source',{region,root:node.root||'',source:node.source||''});
  const projectGroup=create(source,2,project,project==='sessions'?'会话集合':project==='.'?'根目录项目':project,region,'实际路径首段项目；须有至少两个已成立星系群才显示为星系团。','path 首段',{project});
  const directoryGroup=create(projectGroup,3,node.kind==='session'?'sessions':directory,node.kind==='session'?'会话记录区':directory==='.'?'根目录':directory,region,'实际完整包含目录；须有至少两个已成立银河才显示为本星系群。','完整 containing directory',{directory});
  const fileIdentity=fileNode.id,fileLabel=fileNode.name||(node.kind==='session'?'会话 #'+node.number:base(filePath)||node.id);
  const fileGroup=create(directoryGroup,4,fileIdentity,fileLabel,region,'实际 parentFile 或文件节点；须有至少两个已成立旋臂才显示为银河。',node.kind==='symbol'?'parentFile '+fileIdentity:'真实节点 '+fileIdentity,{fileID:fileIdentity,path:filePath});
  const owner=byID.get(owners.get(node.id)),symbolOwner=node.kind==='symbol'&&owner&&owner.kind==='symbol'?owner:null;
  const declarationIdentity=declarationFor(node),declarationLabel=symbolOwner?symbolOwner.name:node.kind==='symbol'?(kindNames[node.symbolKind]||node.symbolKind||'代码')+'声明':node.kind==='session'?'会话原文':node.kind==='directory'?'目录索引':'文件正文';
  const declarationGroup=create(fileGroup,5,declarationIdentity,declarationLabel,region,'实际 defines 所属对象或声明种类；须有至少三个不同已成立恒星系统才显示为旋臂。',symbolOwner?'defines '+symbolOwner.id:'symbolKind / kind',{ownerID:symbolOwner?.id||'',symbolKind:node.symbolKind||node.kind});
  const callKey=localIncident.has(node.id)?minimum.get(representative(node.id)):null,callGroup=callKey?callGroups.get(callKey):null;
  const orbitIdentity=callKey?'calls:'+callKey:'node:'+node.id;
  const orbit=create(declarationGroup,6,orbitIdentity,(node.name||node.id)+' · 调用单元',region,callGroup?'同一真实 file/owner 范围内的局部静态调用联通分量；至少四个不同真实节点才成立恒星系统。':'此范围没有已解析的局部调用联通分量，单节点保留行星；跨范围调用仍保留真实关系。',callGroup?'局部静态调用联通分量':'不推断未使用或不存在运行时调用',{scope:scopeByID.get(node.id),componentKey:callKey||''});
  if(callGroup){orbit.unitID='local-calls:'+scopeByID.get(node.id)+':'+callKey;orbit.componentKey=callKey;orbit.componentMemberCount=callGroup.members.length;orbit.componentEdgeCount=callGroup.edges;
   if(!orbit.localCenterID||node.id<orbit.localCenterID){orbit.localCenterID=node.id;orbit.label=(node.name||node.id)+' · 调用单元';}}
  const leaf=create(orbit,7,node.id,node.name||node.id,region,'真实知识节点；编号、原文位置和会话回调保持不变。','node '+node.id,{nodeID:node.id});leaf.nodeID=node.id;leaf.unitID='node:'+node.id;
  leaf.sourceMetadata.classifications=[source,projectGroup,directoryGroup,fileGroup,declarationGroup,orbit].map(group=>group.sourceMetadata);
  for(const group of [root,source,projectGroup,directoryGroup,fileGroup,declarationGroup,orbit,leaf]){group.members.push(node.id);if(cyclic.has(node.id))group.cyclic=true;}
 }
 // Classification boundaries are evidence, not mandatory celestial wrappers.
 // Count distinct established direct child units of the immediately lower scale.
 function collapse(group){
  if(group.nodeID){group.nextThreshold=thresholds[6];group.qualifyingChildCount=0;group.sourceMetadata.qualified=true;return [group];}
  const children=[];for(const child of group.children)for(const established of collapse(child))children.push(established);
  const distinct=new Set(children.filter(child=>child.depth===group.metadataDepth+1).map(child=>child.unitID));
  const required=thresholds[group.metadataDepth],qualified=group===root||distinct.size>=required;
  group.qualifyingChildCount=distinct.size;group.requiredChildCount=required;group.sourceMetadata.qualified=qualified;group.sourceMetadata.qualifyingChildCount=distinct.size;group.sourceMetadata.requiredChildCount=required;
  if(!qualified)return children;
  group.children=children;group.nextThreshold=group.depth>1?thresholds[group.depth-1]:null;
  return [group];
 }
 collapse(root);
 const scaleCounts=Array(8).fill(0),stack=[[root,null,[]]];
 while(stack.length){const [group,parent,ancestors]=stack.pop();group.parent=parent;group.depth=group.metadataDepth;byKey.set(group.key,group);scaleCounts[group.depth]++;
  const route=[...ancestors,group];if(group.nodeID)paths.set(group.nodeID,route);
  const counts=Array(8).fill(0);for(const child of group.children){counts[child.depth]++;stack.push([child,group,route]);}
  group.directScaleCounts=counts;group.nodeCount=group.members.length;group.establishedChildCount=group.children.length;
 }
 // Recompute connections over variable-length retained paths. Every edge goes
 // to the visible direct children of its lowest common ancestor, without twins.
 const connectionMaps=new Map();
 for(const edge of validEdges){const from=paths.get(edge.from),to=paths.get(edge.to);let common=0;
  while(common<from.length&&common<to.length&&from[common]===to[common])common++;
  if(common===0||common===from.length||common===to.length)continue;
  const parent=from[common-1],a=from[common],b=to[common];if(!connectionMaps.has(parent.key))connectionMaps.set(parent.key,new Map());
  const map=connectionMaps.get(parent.key),key=JSON.stringify([a.key,b.key,edge.kind||'relation']);let connection=map.get(key);
  if(!connection){connection={from:a.key,to:b.key,kind:edge.kind||'relation',count:0,evidence:edge.evidence||'indexed edge'};map.set(key,connection);}connection.count++;
 }
 for(const [key,map]of connectionMaps)byKey.get(key).connections=[...map.values()];
 rawByKey.clear();
 function find(key){return byKey.get(key)||null;}
 function pathFor(id){return paths.get(id)||[];}
 function scene(key){const group=find(key)||root;return {group,children:group.children,depth:group.depth,members:group.members,connections:group.connections,scaleCounts:group.directScaleCounts,nextThreshold:group.nextThreshold};}
 return Object.freeze({root,byKey,paths,scene,pathFor,find,levels,thresholds,scaleCounts:Object.freeze(scaleCounts)});
}});
