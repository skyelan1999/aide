'use strict';
const fs=require('node:fs'),path=require('node:path'),crypto=require('node:crypto');
const {currentEmotion,shouldAppraise,validateAppraisal,emotionPolicy,NAMES}=require('./emotion.cjs');
const {semanticVector,signature,attribution,expression,routeContext,projectTopics,recallCue,presentationRecall,neutralEvidence,SEMANTIC_SPACE,DIMENSIONS}=require('./cognition.cjs');
const SPACE='mixsoul-lexical-384-v1';
const MAX_RECORDS=20000,CHUNK_SIZE=256;
function vector(text){
 const a=Array(384).fill(0),s=String(text).toLocaleLowerCase().replace(/\s+/g,' ').slice(0,4000);
 for(let i=0;i<s.length-1;i++){const h=crypto.createHash('sha256').update(s.slice(i,i+2)).digest();a[h.readUInt16LE(0)%384]+=h[2]&1?1:-1;}
 const norm=Math.hypot(...a)||1;return a.map(x=>x/norm);
}
function cosine(a,b){return a.reduce((sum,x,i)=>sum+x*b[i],0)}
const clamp=(v,lo,hi)=>Math.max(lo,Math.min(hi,v));
const finite=(v,fallback)=>Number.isFinite(v)?v:fallback;
const recoveryCue=q=>/回想|找回|以前|很久|旧的|过去|之前|还记得/.test(q);
function id(){return crypto.randomBytes(12).toString('hex')}
function nowISO(now){const d=new Date(now);if(!Number.isFinite(d.getTime()))throw Error('Invalid time');return d.toISOString()}
function seconds(a,b){return Math.max(0,(Date.parse(a)-Date.parse(b))/1000)||0}
class Store{
 constructor(storage){
  if(!storage||!path.isAbsolute(storage.directory)||!/^\w{32}$/.test(storage.scope||''))throw Error('Invalid private storage');
  this.dir=storage.directory;this.scope=storage.scope;this.key=Buffer.from(storage.key||'','base64');if(this.key.length!==32)throw Error('Storage locked');
  fs.mkdirSync(this.dir,{recursive:true,mode:0o700});if(fs.lstatSync(this.dir).isSymbolicLink())throw Error('Storage symlink rejected');
  this.data=this.read('memory.enc')||{version:2,space:SPACE,records:[],habits:{},lastInteractionAt:null,turns:[],lastTopic:null};
  if(this.data.layout==='chunked-v1'){
   if(!Array.isArray(this.data.chunks)||this.data.chunks.length>Math.ceil(MAX_RECORDS/CHUNK_SIZE))throw Error('Invalid chunk manifest');
   this.data.records=this.data.chunks.flatMap(name=>{if(!/^records-[a-f0-9]{24}-[0-9]+\.enc$/.test(name))throw Error('Invalid chunk path');const rows=this.read(name);if(!Array.isArray(rows)||rows.length>CHUNK_SIZE)throw Error('Invalid memory chunk');return rows;});
   if(this.data.records.length>MAX_RECORDS||this.data.records.length!==this.data.recordCount||new Set(this.data.records.map(r=>r.id)).size!==this.data.records.length)throw Error('Invalid chunk records');
  }
  if(![1,2,3].includes(this.data.version)||this.data.space!==SPACE)throw Error('Unsupported memory/vector version');
  this.data.spaces ||= {[SPACE]:{dimensions:384,model:'local-character-bigram',version:'1',metric:'cosine'}};
  for(const r of this.data.records)r.vectors||={[SPACE]:r.vector};
  this.data.spaces[SEMANTIC_SPACE]||={dimensions:DIMENSIONS,model:'local-inspectable-concepts',version:'1',metric:'cosine'};
  for(const r of this.data.records)r.vectors[SEMANTIC_SPACE]||=semanticVector(r.text);
  this.index=new Map(this.data.records.map(r=>[r.id,r]));this.suppressionCache=new Map();this.topicCache=new Map();
  this.data.assistantTurns||=[];this.data.queue||=[];this.data.mentionRules||=[];this.data.maintenance||={runs:0,modelCalls:0};
  if(this.data.version===1){
   if(!fs.existsSync(path.join(this.dir,'memory-v1.enc')))this.write('memory-v1.enc',this.data);
   this.data.version=2;
   for(const r of this.data.records){r.lastConfirmedAt||=r.recordedAt;r.reinforcementSources||=[];r.adjustments||=[];}
   this.data.habitUpdatedAt||=Object.fromEntries(Object.keys(this.data.habits).map(k=>[k,this.data.lastInteractionAt]));
   this.save();
  }

 }
 read(name){const p=path.join(this.dir,name);if(!fs.existsSync(p))return null;if(fs.lstatSync(p).isSymbolicLink()||fs.statSync(p).size>32*1024*1024)throw Error('Invalid encrypted state');
  const envelope=JSON.parse(fs.readFileSync(p,'utf8'));if(envelope.version!==1)throw Error('Unsupported encryption version');
  const d=crypto.createDecipheriv('aes-256-gcm',this.key,Buffer.from(envelope.nonce,'base64'));d.setAAD(Buffer.from('mixsoul:1:'+this.scope+':'+name));d.setAuthTag(Buffer.from(envelope.tag,'base64'));
  const plain=Buffer.concat([d.update(Buffer.from(envelope.ciphertext,'base64')),d.final()]);try{return JSON.parse(plain.toString('utf8'))}finally{plain.fill(0)}
 }
 write(name,value){const p=path.join(this.dir,name);if(fs.existsSync(p)&&fs.lstatSync(p).isSymbolicLink())throw Error('State symlink rejected');
  const nonce=crypto.randomBytes(12),c=crypto.createCipheriv('aes-256-gcm',this.key,nonce);c.setAAD(Buffer.from('mixsoul:1:'+this.scope+':'+name));const plain=Buffer.from(JSON.stringify(value));
  try{if(plain.length>24*1024*1024)throw Error('Memory storage budget reached');const ciphertext=Buffer.concat([c.update(plain),c.final()]);const body=JSON.stringify({version:1,nonce:nonce.toString('base64'),tag:c.getAuthTag().toString('base64'),ciphertext:ciphertext.toString('base64')});
   const tmp=p+'.'+id()+'.tmp',fd=fs.openSync(tmp,'wx',0o600);try{fs.writeFileSync(fd,body);fs.fsyncSync(fd)}finally{fs.closeSync(fd)}fs.renameSync(tmp,p);
  }finally{plain.fill(0)}
 }
 save(){
  this.knownTopicNames=null;this.topicCache.clear();this.suppressionCache=new Map();
  if(this.data.records.length>MAX_RECORDS)throw Error('Memory record budget reached');
  const prior=this.data.chunks||[];
  if(this.data.records.length>512||this.data.layout==='chunked-v1'){
   const generation=id(),chunks=[];
   for(let i=0;i<this.data.records.length;i+=CHUNK_SIZE){const name='records-'+generation+'-'+(i/CHUNK_SIZE)+'.enc';this.write(name,this.data.records.slice(i,i+CHUNK_SIZE));chunks.push(name);}
   // Publish the authenticated manifest last. Before this point the old
   // generation remains complete and readable after interruption.
   const {records,...meta}=this.data;this.write('memory.enc',{...meta,version:3,layout:'chunked-v1',chunks,recordCount:records.length});
   this.data.version=3;this.data.layout='chunked-v1';this.data.chunks=chunks;this.data.recordCount=records.length;
   for(const name of new Set(prior))if(/^records-[a-f0-9]{24}-[0-9]+\.enc$/.test(name)&&!chunks.includes(name)){try{fs.unlinkSync(path.join(this.dir,name));}catch(e){if(e.code!=='ENOENT')this.cleanupPending=true;}}
  }else this.write('memory.enc',this.data);
 }
 storageInfo(){return {status:this.data.records.length>=MAX_RECORDS?'capacity-reached':'ready',records:this.data.records.length,limit:MAX_RECORDS,layout:this.data.layout||'single-v2',queueDepth:this.data.queue.length,modelCalls:this.data.maintenance.modelCalls};}
 reconcile(now,limit=32){
  const jobs=this.data.queue.slice(0,Math.max(0,Math.min(32,limit)));if(!jobs.length)return {processed:0,remaining:0,modelCalls:0};
  const live=jobs.some(job=>this.data.records.some(r=>r.id===job.sourceId&&!r.consolidated)&&job.type==='local-maintain');
  // A batch shares one deterministic scan. No LLM runs during idle work.
  if(live){this.maintain(now);this.mergeEquivalent(now);}
  const ids=new Set(jobs.map(j=>j.id));this.data.queue=this.data.queue.filter(j=>!ids.has(j.id));
  this.data.maintenance.runs++;this.data.maintenance.lastAt=now;return {processed:jobs.length,remaining:this.data.queue.length,modelCalls:0};
 }
 mergeEquivalent(now){
  const groups=new Map();let merged=0;
  for(const r of this.data.records){if(!['active','dormant'].includes(r.status)||r.pinned||r.mergeExempt||r.kind!=='preference'||this.suppressed(r))continue;
   const key=signature(r.kind,r.text);if(!key)continue;const first=groups.get(key);if(!first){groups.set(key,r);continue;}
   // Keep the older row intact and recoverable; merging never confirms it.
   r.mergePrevious={status:r.status,validTo:r.validTo};r.status='superseded';r.validTo=now;r.mergedInto=first.id;merged++;
  }return merged;
 }
 undoMerge(recordId){const r=this.data.records.find(x=>x.id===recordId);if(!r?.mergedInto)return false;r.status=r.mergePrevious.status;r.validTo=r.mergePrevious.validTo;delete r.mergedInto;delete r.mergePrevious;r.mergeExempt=true;return true;}
 management(input,now){
  if(input.operation==='status')return {storage:this.storageInfo()};
  if(input.operation==='list'){
   const rows=this.data.records.filter(r=>r.kind!=='episode'),offset=Math.max(0,Math.floor(Number(input.offset)||0));
   return {storage:this.storageInfo(),total:Math.max(rows.length,this.data.mentionRules.length),recordTotal:rows.length,ruleTotal:this.data.mentionRules.length,pageSize:10,offset,records:rows.slice(offset,offset+10).map(r=>({id:r.id,kind:r.kind,text:r.text.slice(0,120),truncated:r.text.length>120,status:r.status,importance:r.importance,confidence:r.confidence,stability:r.stability,retention:this.retention(r,now),source:r.source||'local',sourceCount:r.sourceIds.length,recordedAt:r.recordedAt,lastConfirmedAt:r.lastConfirmedAt,validTo:r.validTo,dueAt:r.dueAt||null,suppressed:this.suppressed(r),mergedInto:r.mergedInto||null,supersededBy:r.supersededBy||null})),mentionRules:this.data.mentionRules.slice(offset,offset+10).map(r=>r.term)};
  }
  if(input.operation==='allow-topic'){this.data.mentionRules=this.data.mentionRules.filter(r=>r.term!==input.text);this.save();return {ok:true};}
  const r=this.data.records.find(r=>r.id===input.recordId);if(!r)throw Error('Unknown memory ID');
  if(input.operation==='get')return {record:{id:r.id,text:r.text}};
  if(input.operation==='correct'&&(r.kind==='emotion'||r.kind==='episode'||typeof input.text!=='string'||!input.text.trim()||input.text.length>2000))throw Error('Invalid correction');
  if(input.operation==='correct')return {result:this.manage('更正记忆 '+r.id+'：'+String(input.text||'').slice(0,2000),now)};
  if(input.operation==='delete'){const roots=new Set([r.id]);let changed=true;while(changed){changed=false;for(const row of this.data.records.filter(x=>roots.has(x.id)))for(const sourceId of row.sourceIds){if(!roots.has(sourceId)){roots.add(sourceId);changed=true;}}}const removed=this.removeIds(roots);this.save();return {removed};}
  if(input.operation==='undo-merge'){const restored=this.undoMerge(r.id);this.save();return {restored};}
  if(input.operation==='suppress'||input.operation==='allow'){r.mention=input.operation==='suppress'?'suppressed':'allowed';this.save();return {ok:true,suppressed:this.suppressed(r)};}
  throw Error('Unsupported management operation');
 }
 removeIds(ids){let changed=true;while(changed){changed=false;for(const r of this.data.records)if(!ids.has(r.id)&&r.sourceIds.some(x=>ids.has(x))){ids.add(r.id);changed=true;}}
  if(ids.has(this.data.initiative?.pending?.sourceId))delete this.data.initiative.pending;if(ids.has(this.data.initiative?.claim?.sourceId))delete this.data.initiative.claim;this.data.assistantTurns=this.data.assistantTurns.filter(x=>!x.sourceIds.some(id=>ids.has(id)));this.data.records=this.data.records.filter(r=>!ids.has(r.id));for(const id of ids)this.index.delete(id);this.suppressionCache.clear();this.topicCache.clear();this.data.queue=this.data.queue.filter(j=>!ids.has(j.sourceId));this.data.mentionRules=this.data.mentionRules.filter(r=>!ids.has(r.sourceId));for(const r of this.data.records)if(r.emotion)r.emotion.linkedMemoryIds=(r.emotion.linkedMemoryIds||[]).filter(id=>!ids.has(id));return ids.size;
 }
 recordTopics(r,seen=new Set()){
  if(this.topicCache.has(r.id))return this.topicCache.get(r.id);
  if(seen.has(r.id)||seen.size>64)return [];seen.add(r.id);
  const direct=projectTopics(r.text),inherited=(r.sourceIds||[]).flatMap(id=>{const source=this.index.get(id);return source?this.recordTopics(source,new Set(seen)):[];});
  const topics=[...new Set([...direct,...inherited])];this.topicCache.set(r.id,topics);return topics;
 }
 selectedTopics(query){
  let requested=projectTopics(query);if(!requested.length){this.knownTopicNames||=[...new Set(this.data.records.flatMap(r=>projectTopics(r.text)))];const matches=this.knownTopicNames.filter(n=>String(query).toLowerCase().includes(n));requested=matches.filter(n=>!matches.some(other=>other!==n&&other.includes(n)));}if(requested.length)return {requested,resolved:requested,ambiguous:false};
  const known=[...new Set(this.data.assistantTurns.flatMap(x=>this.index.has(x.sourceIds[0])?this.recordTopics(this.index.get(x.sourceIds[0])):[]))];
  return {requested,resolved:recallCue(query)&&known.length===1?known:[],ambiguous:recallCue(query)&&known.length>1};
 }
 topicCompatible(r,selection){
  const topics=this.recordTopics(r);if(topics.length)return selection.resolved.length>0&&topics.every(t=>selection.resolved.includes(t));
  return !selection.resolved.length||['preference','fact'].includes(r.kind);
 }
 history(messages,query=''){
  const selection=this.selectedTopics(query);let inherited=[];
  return (Array.isArray(messages)?messages:[]).filter(m=>{const text=String(m.content||m.Content||''),role=m.role||m.Role;if(role==='user')inherited=projectTopics(text);const topics=projectTopics(text);if(!topics.length)topics.push(...inherited);
   if(selection.resolved.length?(!topics.length||!topics.every(t=>selection.resolved.includes(t))):topics.length)return false;
   return !this.data.mentionRules.some(r=>text.includes(r.term))&&!this.data.records.some(r=>this.suppressed(r)&&r.text.length>=4&&text.includes(r.text));
  }).map(m=>{if(presentationRecall(query))return m;const key=typeof m.content==='string'?'content':'Content';return {...m,[key]:neutralEvidence(m[key],{source:(m.role||m.Role)==='user'})};});
 }

 manage(query,now){
  const undo=query.match(/^撤销合并记忆\s+([a-f0-9]{24})$/);if(undo){const ok=this.undoMerge(undo[1]);this.save();return ok?'已撤销本地整理的合并。':'该条记忆没有可撤销的自动合并。';}
  if(query==='查看记忆状态')return JSON.stringify(this.storageInfo());
  if(query==='查看记忆')return this.data.records.filter(r=>!['episode','emotion'].includes(r.kind)&&r.status==='active'&&!this.suppressed(r)).slice(-20).map(r=>r.id+' · '+r.kind+' · '+r.text.slice(0,120)).join('\n')||'当前没有可列出的长期记忆。';
  const m=query.match(/^更正记忆\s+([a-f0-9]{24})[：:\s]+(.+)$/s);if(m){const old=this.data.records.find(r=>r.id===m[1]);if(!old||old.kind==='episode'||old.kind==='emotion')return '未找到可更正的长期记忆。';if(this.data.records.length>MAX_RECORDS-2)return '记忆容量不足，本次没有写入。';const source=this.add('episode',query,now,{source:'current-user-statement',confidence:1,turnId:'manage-'+id()});const next=this.add(old.kind,m[2],now,{sourceIds:[source.id],evidence:query,pinned:!!old.pinned,importance:old.importance,confidence:.6,source:'explicit-user-correction',mention:old.mention||'allowed'});if(!next)return '记忆容量不足，本次没有写入。';if(next!==old)this.supersede(old.id,next.id,now);this.save();return '已更正，原版本保留为过时记录，不再用于当前召回。';}
  return null;
 }
 suppressed(record,seen=new Set()){if(this.suppressionCache.has(record.id))return this.suppressionCache.get(record.id);if(record.mention==='suppressed'||this.data.mentionRules.some(r=>record.text.includes(r.term)))return true;if(!this.data.mentionRules.length||seen.has(record.id))return false;seen.add(record.id);const hidden=(record.sourceIds||[]).some(id=>{const source=this.index.get(id);return source?this.suppressed(source,seen):false;});this.suppressionCache.set(record.id,hidden);return hidden;}
 applyBoundary(heard,source,context,now){
  const block=heard.match(/^(?:请|以后)?(?:别再提|不要再提|不要主动提)(?:起)?[：:，,\s]*(.{2,120})[。！!]?$/s),allow=heard.match(/^(?:可以再提|允许再提|恢复提及)[：:，,\s]*(.{2,120})[。！!]?$/s);
  const term=(block?.[1]||allow?.[1]||'').replace(/[。！!]$/,'').trim();
  if(term){if(block){if(this.data.mentionRules.length>=128)return {error:'mention-rule-capacity'};this.data.mentionRules.push({term,at:now,sourceId:source?.id});}else {this.data.mentionRules=this.data.mentionRules.filter(r=>r.term!==term);for(const r of this.data.records)if(r.text.includes(term))r.mention='allowed';}}
  const feedback=/这件事很重要|务必记牢|刻骨铭心/.test(heard)?'salient':/这段记忆淡一点|降低这段记忆权重/.test(heard)?'reduce':null;
  if(feedback&&source){for(const ref of context.references.filter(r=>r.kind!=='emotion'&&r.kind!=='episode'&&heard.includes(r.text)).slice(0,2)){
   const r=this.data.records.find(x=>x.id===ref.id);if(!r||r.lastSalienceAt?.slice(0,10)===now.slice(0,10))continue;r.lastSalienceAt=now;r.salience=feedback==='salient'?1:0;r.stability=feedback==='salient'?Math.min(8,r.stability*1.25):Math.max(.5,r.stability*.8);r.adjustments.push({at:now,sourceId:source.id,type:feedback,reason:'explicit user retention feedback'});r.adjustments=r.adjustments.slice(-32);
  }}
  return term?{action:block?'suppress':'allow'}:null;
 }
 close(){this.key.fill(0);this.data=null}
 timeContext(now,zone='UTC'){
  now=nowISO(now);const clock=this.read('clock.enc');
  const anchors=[now,clock?.lastPulseAt,this.data.lastInteractionAt].filter(Boolean);
  const effectiveNow=new Date(Math.max(...anchors.map(x=>Date.parse(x)).filter(Number.isFinite))).toISOString();
  const anomaly=Date.parse(now)<Date.parse(effectiveNow)||!!clock?.clockAnomaly;
  let timezone=zone,localTime;
  try{localTime=new Intl.DateTimeFormat('sv-SE',{timeZone:zone,dateStyle:'short',timeStyle:'medium',hour12:false}).format(new Date(now));}
  catch{timezone='UTC';localTime=new Intl.DateTimeFormat('sv-SE',{timeZone:'UTC',dateStyle:'short',timeStyle:'medium',hour12:false}).format(new Date(now));}
  const idleSeconds=this.data.lastInteractionAt?seconds(effectiveNow,this.data.lastInteractionAt):null;
  const idleBand=idleSeconds===null?'first-contact':idleSeconds<3600?'minutes':idleSeconds<86400?'hours':idleSeconds<604800?'days':idleSeconds<2592000?'weeks':'months';
  return {clock:now,effectiveNow,timezone,localTime,lastInteractionAt:this.data.lastInteractionAt,idleSeconds,idleBand,clockAnomaly:anomaly,elapsedReliable:!anomaly,lastTurnGapSeconds:this.data.lastTurnContext?.idleSeconds??null};
 }
 habitsAt(now){
  const out={};for(const [key,value] of Object.entries(this.data.habits)){
   const updated=this.data.habitUpdatedAt?.[key]||this.data.lastInteractionAt||now;
   out[key]=Number((.5+(value-.5)*Math.pow(.5,seconds(now,updated)/86400/90)).toFixed(4));
  }return out;
 }
 retention(r,now){
  if(r.pinned||r.kind==='promise')return 1;
  const halfLife=r.kind==='emotion'?7:r.kind==='episode'?3:['decision','preference'].includes(r.kind)?60:45;
  const age=seconds(now,r.lastConfirmedAt||r.recordedAt)/86400;
  return Math.pow(.5,age/(halfLife*clamp(finite(r.stability,1),.5,8)));
 }
 maintain(now){
  for(const r of this.data.records){
   if(['superseded','resolved','expired'].includes(r.status))continue;
   if(r.validTo&&Date.parse(r.validTo)<=Date.parse(now)){r.status='expired';continue;}
   const status=this.retention(r,now)<.15?'dormant':'active';
   if(status!==r.status){r.status=status;r.statusChangedAt=now;}
  }
 }
 initiativeControl(heard,now){
  const quiet=/^(?:请|先|你先)?(?:安静(?:一会儿?)?|别主动(?:说话|开口|提醒)|不要主动(?:说话|开口|提醒)|暂停主动交流)[。！!\s]*$/.test(heard);
  const resume=/^(?:请)?(?:恢复主动交流|恢复主动说话|可以主动(?:说话|提醒)|继续主动交流)[。！!\s]*$/.test(heard);
  if(!quiet&&!resume)return '';
  this.data.initiative||={};this.data.initiative.pausedUntil=resume?null:/安静/.test(heard)?new Date(Date.parse(now)+30*60000).toISOString():'9999-01-01T00:00:00.000Z';delete this.data.initiative.pending;delete this.data.initiative.claim;this.save();
  return resume?'好，我会在合适的时候主动接话或提醒。':'好，我先安静，不再主动接话；你随时可以叫我。';
 }
 offerInitiative(source,row,context){
  if(context.proactive||context.noStore||!source||this.suppressed(source))return;
  const heard=source.text||'',reply=row.text||'';
  if((heard.match(/项目/g)||[]).length>1)return;
  if(!/聊|说说|想法|不知道|怎么办|纠结|下一步|主意|建议|继续讨论|规划/.test(heard)||/[？?]\s*$/.test(reply)||/先安静|别主动|不要主动|不打扰|我不问了|(?:密码|口令|密钥|token)|(?:不要|别)保存/i.test(heard+' '+reply))return;
  if(/生成|制作|编译|删除|清理|发布|部署|推送/.test(heard)&&!/只聊|不交办/.test(heard))return;
  this.data.initiative||={};this.data.initiative.pending={id:id(),sourceId:source.id,assistantId:row.id,at:row.at,callId:source.nativeCallId||null};
 }
 claimInitiative(channel,now,callId){
  const p=this.data.initiative;if(!p?.pending||p.pausedUntil&&Date.parse(p.pausedUntil)>Date.parse(now))return null;
  const c=p.pending,source=this.index.get(c.sourceId),turn=this.data.assistantTurns.find(t=>t.id===c.assistantId);
  if(!source||!turn||this.suppressed(source)||!turn.sourceIds.every(id=>this.index.has(id)&&!this.suppressed(this.index.get(id)))||!['active','dormant'].includes(source.status)||source.validTo&&Date.parse(source.validTo)<=Date.parse(now))return null;
  if(c.callId?channel!=='native'||callId!==c.callId:channel!=='idle')return null;
  const age=(Date.parse(now)-Date.parse(c.at))/1000;if(!Number.isFinite(age)||age<(channel==='native'?25:180)||age>1800||Date.parse(this.data.lastInteractionAt)>Date.parse(c.at))return null;
  const topics=this.recordTopics(source);if(topics.length>1)return null;
  const day=now.slice(0,10);if(p.day!==day){p.day=day;p.idleCount=0;p.nativeCount=0;}
  if(channel==='idle'&&(p.idleCount>=6||p.lastIdleAt&&(Date.parse(now)-Date.parse(p.lastIdleAt))/1000<600))return null;
  if(channel==='native'&&(p.nativeCount>=20||p.lastNativeAt&&(Date.parse(now)-Date.parse(p.lastNativeAt))/1000<90))return null;
  delete p.pending;p.claim={...c,channel,claimedAt:now};if(channel==='idle'){p.idleCount++;p.lastIdleAt=now;}else{p.nativeCount++;p.lastNativeAt=now;}this.save();
  return {...c,turnId:source.turnId,statement:source.text,previousReply:turn.text,topics};
 }
 completeInitiative(claimId,reply,now){
  const p=this.data.initiative,c=p?.claim;if(!c||c.id!==claimId||p.pausedUntil&&Date.parse(p.pausedUntil)>Date.parse(now))return '';
  delete p.claim;const source=this.index.get(c.sourceId);if(!source||this.suppressed(source)||Date.parse(this.data.lastInteractionAt)>Date.parse(c.at)||(Date.parse(now)-Date.parse(c.claimedAt))/1000>90){this.save();return '';}
  const text=String(reply||'').trim();if(text)this.rememberReply({sourceId:source.id,noStore:false,proactive:true,references:[]},text,now);this.save();return text;
 }
 rememberReply(context,reply,now){
  if(context?.noStore||!context?.sourceId||!String(reply||'').trim())return false;
  const source=this.index.get(context.sourceId);if(!source||source.source!=='current-user-statement')return false;
  const current=context.emotion?.current||currentEmotion(source.text,now);
  const assessment={labels:current.labels||[],basis:current.basis||'unknown',confidence:current.confidence||0,at:current.at||now,evidenceSourceId:current.sourceId||source.id,epistemicStatus:current.basis==='explicit'?'user-self-report':'tentative-hypothesis'};
  const row={id:id(),role:'assistant',text:String(reply).trim().slice(0,1200),at:now,sourceIds:[...new Set([source.id,...(source.sourceIds||[]),...(context.references||[]).flatMap(r=>[r.id,...(r.sourceIds||[])]),...(context.inferenceEvidence?.assistantTurns||[]).flatMap(r=>r.sourceIds||[])])],topicKeys:this.recordTopics(source),emotionAssessment:assessment,notUserFact:true};
  if(!context.proactive)this.data.assistantTurns=this.data.assistantTurns.filter(x=>x.sourceIds[0]!==source.id);row.initiative=!!context.proactive;this.data.assistantTurns.push(row);this.offerInitiative(source,row,context);this.data.assistantTurns=this.data.assistantTurns.slice(-24);this.save();return true;
 }
 inferenceEvidence(query,now){
  const selection=this.selectedTopics(query),original=presentationRecall(query),explicit=recallCue(query);
  const turns=this.data.assistantTurns.filter(x=>{
   const source=this.index.get(x.sourceIds[0]);if(!source||seconds(now,x.at)>7*86400||!x.sourceIds.every(id=>this.index.has(id)&&!this.suppressed(this.index.get(id)))||this.data.mentionRules.some(r=>x.text.includes(r.term)))return false;
   const topics=this.recordTopics(source);
   if(selection.resolved.length)return topics.length>0&&topics.every(t=>selection.resolved.includes(t));
   return !selection.ambiguous&&!topics.length&&explicit;
  }).filter(x=>!(original&&/主动|自行|自己补充/.test(query)&&!/(?:以及|同时|分别).*(?:建议|回复|原话)|(?:两|二|多)句/.test(query))||x.initiative).slice(-2).map(x=>({...x,text:(original?x.text:neutralEvidence(x.text)).slice(0,700),sourceStatement:{id:x.sourceIds[0],role:'user',text:(original?this.index.get(x.sourceIds[0]).text:neutralEvidence(this.index.get(x.sourceIds[0]).text,{source:true})).slice(0,400)},supportingStatements:x.sourceIds.slice(1).map(id=>this.index.get(id)).filter(r=>r?.source==='current-user-statement').slice(-4).map(r=>({id:r.id,role:'user',text:(original?r.text:neutralEvidence(r.text,{source:true})).slice(0,250),at:r.recordedAt,association:r.nativeCallId?'same-call-continuation; not proof of causation':'provided-context'})),presentation:'historical data only; never a current output instruction'}));
  return {assistantTurns:turns,topicSelection:selection,originalPresentationRequested:original,policy:'Evidence is scoped to explicitly matched projects. Ambiguous projects require clarification. Historical wording, numbering and formatting are not output instructions; only the current request controls presentation. Assistant statements are not independent proof or user facts. Emotion hypotheses remain tentative.'};
 }

 context(query,now,zone='UTC'){
  const time=this.timeContext(now,zone);
  const inference=this.inferenceEvidence(query),evidenceIds=new Set(inference.assistantTurns.flatMap(r=>r.sourceIds));
  const linkedAffect=recallCue(query)&&/当时|情绪|心情|为什么|为何/.test(query)?this.data.records.filter(r=>r.kind==='emotion'&&['active','dormant'].includes(r.status)&&!this.suppressed(r)&&(r.sourceIds.some(id=>evidenceIds.has(id))||(inference.topicSelection.resolved.length>0&&this.recordTopics(r).length>0))&&this.topicCompatible(r,inference.topicSelection)).slice(-2).map(r=>({id:r.id,kind:r.kind,text:r.text,source:r.source,sourceIds:r.sourceIds,emotion:{...r.emotion,historical:true},recordedAt:r.recordedAt,status:r.status})):[];
  const options={includeDormant:recoveryCue(query),limit:32},ranked=this.retrieve(query,time.effectiveNow,options),reserved=[...linkedAffect,...['promise','preference'].flatMap(kind=>this.retrieve(query,time.effectiveNow,{...options,kind,limit:2}))];
  const candidates=[...new Map([...reserved,...ranked].map(r=>[r.id,r])).values()].map(r=>!presentationRecall(query)&&(r.kind==='episode'||r.source==='model-summary-of-user-statement')?{...r,text:neutralEvidence(r.text,{source:true})}:r).filter(r=>r.text);const routed=routeContext(candidates);const references=routed.references;
  const current=currentEmotion(query,time.effectiveNow);
  return {...time,references,inferenceEvidence:this.inferenceEvidence(query,time.effectiveNow),contextBudget:routed.budget,expression:expression(query,this.habitsAt(time.effectiveNow)),mentionPolicy:{suppressedCount:this.data.mentionRules.length,doNotInferPermission:true},storage:this.storageInfo(),emotion:{current,policy:emotionPolicy(current),historicalReferenceIds:references.filter(r=>r.kind==='emotion').map(r=>r.id)},habits:this.habitsAt(time.effectiveNow),memoryPolicy:{dormantRequiresExplicitRecall:true,retentionIsProbability:false,localEmbedding:this.semanticState||{enabled:false,status:'disabled'}}};
 }
 pulse(now){
  const time=this.timeContext(now);const previous=this.read('clock.enc');
  const backward=Date.parse(time.clock)<Date.parse(previous?.lastPulseAt||time.clock)||Date.parse(time.clock)<Date.parse(this.data.lastInteractionAt||time.clock);
  const before=JSON.stringify(this.data);this.maintain(time.effectiveNow);const maintenance=this.reconcile(time.effectiveNow);
  if(JSON.stringify(this.data)!==before)this.save();
  const result={lastPulseAt:time.effectiveNow,observedAt:time.clock,lastInteractionAt:this.data.lastInteractionAt,idleSeconds:time.idleSeconds,clockAnomaly:backward,modelCalls:0,newEvents:0,maintenance};this.write('clock.enc',result);return result;
 }
 add(kind,text,now,extra={}){text=String(text||'').trim().slice(0,2000);if(!text)return null;
  const old=kind==='episode'?this.data.records.find(r=>extra.turnId&&r.turnId===extra.turnId):this.data.records.find(r=>r.kind===kind&&r.text===text&&['active','dormant'].includes(r.status));if(old)return old;
  if(this.data.records.length>=MAX_RECORDS)return null; // Never silently evict pinned facts or incomplete promises.
  const row={id:id(),kind,text,recordedAt:nowISO(now),eventAt:nowISO(now),validTo:null,status:'active',importance:.4,confidence:.6,stability:1,lastConfirmedAt:nowISO(now),reinforcementSources:[],adjustments:[],sourceIds:[],space:SPACE,vector:vector(text),...extra};row.vectors={[SPACE]:row.vector,[SEMANTIC_SPACE]:semanticVector(text)};this.data.records.push(row);this.index.set(row.id,row);return row;
 }
 registerSpace(name,spec){if(!/^[a-zA-Z0-9_.:-]{1,100}$/.test(name)||!Number.isInteger(spec.dimensions)||spec.dimensions<2||spec.dimensions>4096||typeof spec.model!=='string'||typeof spec.version!=='string'||spec.metric!=='cosine')throw Error('Invalid vector space');if(this.data.spaces[name]&&JSON.stringify(this.data.spaces[name])!==JSON.stringify(spec))throw Error('Incompatible vector space version');this.data.spaces[name]={...spec};}
 setVector(recordId,space,values){const spec=this.data.spaces[space],record=this.data.records.find(r=>r.id===recordId);if(!spec||!record||!Array.isArray(values)||values.length!==spec.dimensions||values.some(x=>!Number.isFinite(x)))throw Error('Vector space mismatch');const norm=Math.hypot(...values);if(!norm||!Number.isFinite(norm))throw Error('Invalid vector norm');record.vectors||={};record.vectors[space]=values.map(x=>x/norm);}
 supersede(oldId,newId,at){const old=this.data.records.find(r=>r.id===oldId),next=this.data.records.find(r=>r.id===newId);if(!old||!next||old===next)throw Error('Invalid version transition');old.status='superseded';old.validTo=nowISO(at);old.supersededBy=newId;next.supersedes=oldId;}
 retrieve(query,now,{limit=8,space=SPACE,queryVector=null,kind=null,includeDormant=false,includeSuppressed=false,subject=null}={}){
  const spec=this.data.spaces[space];if(!spec)throw Error('Unknown vector space');
  if(space!==SPACE&&space!==SEMANTIC_SPACE&&!queryVector)throw Error('Named semantic space requires its own query vector');
  let q=queryVector||(space===SEMANTIC_SPACE?semanticVector(query):vector(query));if(q.length!==spec.dimensions||q.some(x=>!Number.isFinite(x)))throw Error('Query vector space mismatch');
  const norm=Math.hypot(...q)||1;q=q.map(x=>x/norm);const time=Date.parse(now),selection=this.selectedTopics(query);
  const obsoleteSources=new Set(this.data.records.filter(r=>['superseded','expired','resolved'].includes(r.status)||r.validTo&&Date.parse(r.validTo)<=time).flatMap(r=>r.sourceIds||[]));
  return this.data.records.filter(r=>this.topicCompatible(r,selection)&&(includeDormant||r.kind!=='episode'||!obsoleteSources.has(r.id))&&['active','dormant'].includes(r.status)&&r.vectors?.[space]&&(!kind||r.kind===kind)&&(!subject||r.subject===subject)&&(!r.validFrom||Date.parse(r.validFrom)<=time)&&(includeSuppressed||!this.suppressed(r))&&(!r.validTo||Date.parse(r.validTo)>time)).map(r=>{
   const retention=this.retention(r,now),dormant=retention<.15,lexical=cosine(q,r.vectors[space]),semantic=space===SPACE?(this.semanticQuery?.query===query&&r.vectors[this.semanticQuery.space]?Math.max(0,cosine(this.semanticQuery.vector,r.vectors[this.semanticQuery.space])-.4)/.6:cosine(semanticVector(query),r.vectors[SEMANTIC_SPACE]||semanticVector(r.text))):0,similarity=space===SPACE?Math.max(lexical,.65*lexical+.35*semantic):lexical;
   if(dormant&&(!includeDormant||similarity<.18))return null;
   if(similarity<=.04&&!r.pinned&&!['preference','promise'].includes(r.kind))return null;
   const exact=!!query&&r.text.toLowerCase().includes(query.toLowerCase());
   const base=.65*Math.max(0,similarity)+.15*r.importance+.1*r.confidence+(exact?.25:0);
   return {r,status:dormant?'dormant':'active',retention,score:base*(.25+.75*retention)};
  }).filter(Boolean).sort((a,b)=>b.score-a.score).slice(0,Math.max(0,Math.min(32,limit))).map(({r,status,retention,score})=>({id:r.id,kind:r.kind,text:r.text,status,recordedAt:r.recordedAt,eventAt:r.eventAt,lastConfirmedAt:r.lastConfirmedAt,validTo:r.validTo,importance:r.importance,stability:r.stability,confidence:r.confidence,source:r.source,sourceIds:r.sourceIds,mention:r.mention||'allowed',salience:r.salience||0,subject:r.subject||'self',emotion:r.kind==='emotion'?{...r.emotion,historical:true,linkedMemoryIds:(r.emotion?.linkedMemoryIds||[]).filter(id=>this.data.records.some(x=>x.id===id&&['active','dormant'].includes(x.status)))}:undefined,reviewDue:r.kind==='promise'&&!!r.dueAt&&Date.parse(r.dueAt)<=time,retentionStrength:Number(retention.toFixed(4)),score:Number(score.toFixed(3))}));
 }
 reinforce(record,source,now,reason){
  if(!record||record.kind==='emotion'||!source||source.id===record.id||record.status==='resolved'||record.status==='superseded'||record.status==='expired')return false;
  record.reinforcementSources||=[];
  if(record.reinforcementSources.includes(source.id)||record.lastReinforcedAt?.slice(0,10)===now.slice(0,10))return false;
  record.importance=clamp(record.importance+.05,.1,.9);if(record.pinned)record.importance=Math.max(.95,record.importance);
  record.stability=clamp(finite(record.stability,1)*1.25,.5,8);
  record.lastConfirmedAt=now;record.lastReinforcedAt=now;record.status='active';
  record.reinforcementSources.push(source.id);record.reinforcementSources=record.reinforcementSources.slice(-32);
  record.sourceIds=[...new Set([...record.sourceIds,source.id])];
  record.adjustments||=[];record.adjustments.push({at:now,sourceId:source.id,reason:String(reason).slice(0,160),type:'user-evidence-reinforcement'});record.adjustments=record.adjustments.slice(-32);return true;
 }
 forget(query){if(!query||query.length<2)return {removed:0,ambiguous:true};const rows=this.data.records.filter(r=>r.text.includes(query));if(rows.length>12)return {removed:0,ambiguous:true};const ids=new Set(rows.map(r=>r.id));let changed=true;while(changed){changed=false;for(const r of this.data.records)if(!ids.has(r.id)&&r.sourceIds.some(x=>ids.has(x))){ids.add(r.id);changed=true}}
  if(ids.has(this.data.initiative?.pending?.sourceId))delete this.data.initiative.pending;if(ids.has(this.data.initiative?.claim?.sourceId))delete this.data.initiative.claim;this.data.assistantTurns=this.data.assistantTurns.filter(x=>!x.sourceIds.some(id=>ids.has(id)));this.data.records=this.data.records.filter(r=>!ids.has(r.id));for(const id of ids)this.index.delete(id);this.suppressionCache.clear();this.topicCache.clear();this.data.queue=this.data.queue.filter(j=>!ids.has(j.sourceId));this.data.mentionRules=this.data.mentionRules.filter(r=>!ids.has(r.sourceId)&&!r.term.includes(query));for(const r of this.data.records)if(r.emotion)r.emotion.linkedMemoryIds=(r.emotion.linkedMemoryIds||[]).filter(id=>!ids.has(id));return {removed:ids.size,ambiguous:false};
 }
 nativeScope(callId,heard,now){
  if(typeof callId!=='string'||!/^[-\w]{1,64}$/.test(callId))return {query:heard,sourceIds:[]};
  this.data.nativeCalls=(this.data.nativeCalls||[]).filter(c=>seconds(now,c.at)<600).slice(-7);
  let call=this.data.nativeCalls.find(c=>c.id===callId);if(!call){call={id:callId,at:now,anchors:[],topics:[]};this.data.nativeCalls.push(call);}
  if(seconds(now,call.at)>90){call.anchors=[];call.topics=[];}
  if(/不要记(?:住|录)?|别记(?:住|录)?|不要保存/.test(heard)){call.blocked=true;call.anchors=[];call.topics=[];}
  if(/^(?:现在)?恢复(?:记忆|保存)/.test(heard))call.blocked=false;
  const named=projectTopics(heard),reset=/换个话题|先不聊|不说.*项目|另外一件|另一个项目|无关的事/.test(heard);
  if(named.length){call.anchors=[];call.topics=call.blocked?[]:named.length===1?named:[];}
  else if(reset){call.anchors=[];call.topics=[];}
  const continuation=recallCue(heard)||/刚才|上面|这件|这个|那个|它|评审|建议|编号|下一步|然后|所以|只聊天|不交办|不派任务|简单回复|简短|请继续|接着|不知道怎么办/.test(heard)||/^(?:我)?(?:今天|现在|此刻|这会儿)?(?:很|有点|觉得|感到)?(?:焦虑|紧张|担心|开心|难过|平静|沮丧|委屈|生气|孤独)/.test(heard);
  const anchors=(call.anchors||[]).filter(id=>this.index.has(id)&&!this.suppressed(this.index.get(id)));
  if(!named.length&&!continuation){call.anchors=[];call.topics=[];}
  const inherited=!named.length&&continuation&&!call.blocked&&call.topics.length===1&&anchors.length>0;
  call.at=now;
  return {call,query:inherited?call.topics[0]+'项目 '+heard:heard,sourceIds:inherited?anchors:[],blocked:!!call.blocked};
 }
 nativeObserve(input,now,settings){
  const heard=String(input.text||''),scope=this.nativeScope(input.isolatedFragment?'':input.callId,heard,now);
  if(input.isolatedFragment&&this.data.nativeCalls?.some(c=>c.id===input.callId&&c.blocked))scope.blocked=true;
  let context=this.begin(String(input.turnId||''),heard,now,{...settings,nativeScope:scope,memoryEnabled:settings.memoryEnabled!==false&&!scope.blocked});
  const source=this.index.get(context.sourceId);
  if(source){source.nativeCallId=input.callId||null;source.sourceIds=[...new Set([...(source.sourceIds||[]),...scope.sourceIds])];source.contextIds=[...new Set([...(source.contextIds||[]),...scope.sourceIds])];this.topicCache.clear();
   if(scope.call&&projectTopics(heard).length===1){scope.call.anchors=[source.id];}
   // Persist explicit self-report immediately; hangup must not cancel it.
   const current=currentEmotion(heard,now);
   context=this.appraise({...current,linkedMemoryIds:scope.sourceIds},context,heard,now);
  }
  this.save();return context;
 }
 begin(turnId,heard,now,settings={}){
  now=nowISO(now);const control=this.initiativeControl(heard,now);let context=this.context(settings.nativeScope?.query||heard,now,settings.timeZone||'UTC');if(control)return {...context,management:control,noStore:true};
  if(settings.nativeScope){const current=currentEmotion(heard,now);context={...context,emotion:{...context.emotion,current,policy:emotionPolicy(current)}};}
  const effective=context.effectiveNow;this.maintain(effective);
  const management=this.manage(heard,effective);if(management)return {...context,management,noStore:true};
  const deletion=heard.match(/^(?:请|帮我)?(?:忘记|删除记忆[：:]?)\s*(.{2,})$/s);
  if(deletion){const result=this.forget(deletion[1].trim());this.save();return {...context,deletion:result,references:[],feedback:[],noStore:true};}
  if(this.data.turns.includes(turnId))return {...context,duplicate:true,feedback:[],noStore:true};
  if(this.data.initiative){delete this.data.initiative.pending;delete this.data.initiative.claim;}
  const sensitive=/不要记(?:住|录)?|别记(?:住|录)?|不要保存/.test(heard),enabled=settings.memoryEnabled!==false&&!sensitive;
  const feedback=[];
  if(enabled){
   const adjustments=[[/别总追问|不要(?:一直|总是|总)问|少问(?:我|问题)/,'questions',-.18], [/短一点|简洁一点|简洁点|少说一点/,'verbosity',-.15], [/多说一点|详细一点/,'verbosity',.12], [/太像客服|不要像客服|别像客服|别说套话/,'formulaic',-.2], [/太冷淡|温柔一点/,'warmth',.12]];
   for(const [pattern,key,delta] of adjustments)if(pattern.test(heard)){
    this.data.habits[key]=clamp((context.habits[key]??.5)+delta,.1,.9);this.data.habitUpdatedAt||={};this.data.habitUpdatedAt[key]=effective;feedback.push({key,change:delta});
   }
  }
  let source=null;
  if(enabled){
   source=this.add('episode',heard,effective,{turnId,source:'current-user-statement',confidence:1,contextIds:context.references.map(r=>r.id)});
   const explicit=heard.match(/^(?:请|帮我)?记住[：:，,]?\s*(.+)$/s);
   if(explicit){const fact=this.add('fact',explicit[1],effective,{pinned:true,importance:.95,confidence:1,sourceIds:source?[source.id]:[],source:'explicit-user-memory'});if(fact&&source&&fact.sourceIds[0]!==source.id)this.reinforce(fact,source,effective,'explicit user remember');}
  }
  const boundary=enabled?this.applyBoundary(heard,source,context,effective):null;
  if(source){if(this.data.queue.length>=256)this.reconcile(effective,32);this.data.queue.push({id:id(),sourceId:source.id,type:'local-maintain',status:'pending',createdAt:effective});}
  this.data.turns.push(turnId);this.data.turns=this.data.turns.slice(-1000);this.data.lastInteractionAt=effective;
  this.data.lastTurnContext={turnId,idleSeconds:context.idleSeconds,previousInteractionAt:context.lastInteractionAt,at:effective};this.save();
  const finalRouted=routeContext(context.references.filter(r=>!this.suppressed(r)));
  return {...context,...finalRouted,contextBudget:finalRouted.budget,storage:this.storageInfo(),mentionPolicy:{suppressedCount:this.data.mentionRules.length,doNotInferPermission:true},emotion:{...context.emotion,historicalReferenceIds:finalRouted.references.filter(r=>r.kind==='emotion').map(r=>r.id)},boundary,feedback,expression:expression(heard,this.habitsAt(effective)),habits:this.habitsAt(effective),sourceId:source?.id||null,noStore:!enabled,storageStatus:this.storageInfo().status};
 }
 appraise(candidate,context,heard,now){
  now=this.timeContext(now).effectiveNow;
  const allowedIds=(context.references||[]).map(r=>r.id).filter(id=>this.data.records.some(r=>r.id===id&&['active','dormant'].includes(r.status)&&(!r.validTo||Date.parse(r.validTo)>Date.parse(now))));
  const current=validateAppraisal(candidate,heard,now,allowedIds);
  const source=this.data.records.find(r=>r.id===context.sourceId&&r.source==='current-user-statement');
  if(!context.noStore&&current.basis==='explicit'&&current.confidence>=.5&&source&&source.text===heard.slice(0,2000)&&!source.emotionAppraised&&!source.consolidated&&seconds(now,source.recordedAt)<=1800){
   // Emotion changes expression and establishes an event link. It does not
   // reinforce linked facts, change habits or increase factual confidence.
   const row=this.add('emotion','在'+now+'的一次交流中，用户自述'+current.labels.map(x=>NAMES[x]).join('、')+'：'+current.quote,now,{sourceIds:[source.id],source:'explicit-user-affect',importance:.4,confidence:.55,emotion:{labels:current.labels,basis:'explicit',confidence:current.confidence,at:now,linkedMemoryIds:current.linkedMemoryIds,evidence:current.quote,historical:true}});
   if(row){row.vector=vector(heard);row.vectors[SPACE]=row.vector;source.emotionAppraised=true;this.save();}
  }
  return {...context,emotion:{current,policy:emotionPolicy(current),historicalReferenceIds:(context.references||[]).filter(r=>r.kind==='emotion').map(r=>r.id)}};
 }
 consolidate(candidate,sourceId,heard,now,allowedIds=[]){
  candidate=candidate&&typeof candidate==='object'&&!Array.isArray(candidate)?candidate:{};
  const source=this.data.records.find(r=>r.id===sourceId&&r.source==='current-user-statement');if(!source||source.text!==heard.slice(0,2000)||source.consolidated)return 0;
  now=this.timeContext(now).effectiveNow;
  if(seconds(now,source.recordedAt)>1800)return 0;
  this.appraise(candidate?.emotion,{sourceId,references:allowedIds.map(id=>({id})),noStore:false},heard,now);
  let count=0;
  for(const item of Array.isArray(candidate.items)?candidate.items.slice(0,3):[]){
   if(!item||typeof item!=='object'||attribution(heard)!=='self-current')continue;
   if(attribution(item.quote)!=='self-current'||(item.subject&&item.subject!=='self')||(item.kind==='preference'&&/这次|本轮|今天|明天|本次/.test(item.quote)))continue;
   if(!['preference','decision','promise','episode'].includes(item.kind)||typeof item.text!=='string'||typeof item.quote!=='string'||item.quote.length<2||!heard.includes(item.quote))continue;
   // A momentary emotional utterance cannot silently become a personality
   // preference, decision or promise. Require an explicit matching intent.
   if(shouldAppraise(heard)&&item.kind!=='episode'){
    const intent=item.kind==='preference'?/长期|一直|以后|每次|通常|一贯|偏好|习惯/:item.kind==='decision'?/决定|打算|约定/:/我会|我答应|承诺|计划/;
    if(!intent.test(item.quote))continue;
   }
   const reason=typeof item.reason==='string'?item.reason.trim().slice(0,160):'';
   const importance=reason.length>=3?clamp(finite(item.importance,item.kind==='promise'?.75:.5),.2,.8):item.kind==='promise'?.75:.5;
   const sig=signature(item.kind,item.text);
   const existing=this.data.records.find(r=>r.kind===item.kind&&['active','dormant'].includes(r.status)&&(r.text===item.text||(sig&&signature(r.kind,r.text)===sig)));
   if(existing){this.reinforce(existing,source,now,'independent user evidence for equivalent preference');existing.sourceIds=[...new Set([...existing.sourceIds,source.id])];existing.variants||=[];if(!existing.variants.some(v=>v.sourceId===source.id))existing.variants.push({text:item.text.slice(0,2000),quote:item.quote.slice(0,500),sourceId:source.id,at:now});existing.variants=existing.variants.slice(-16);count++;continue;}
   const validTo=/有效至|有效期|到期失效|过期作废/.test(item.quote)&&typeof item.validTo==='string'&&/^\d{4}-\d{2}-\d{2}T/.test(item.validTo)&&heard.includes(item.validTo.slice(0,10))&&Number.isFinite(Date.parse(item.validTo))&&Date.parse(item.validTo)>Date.parse(now)?nowISO(item.validTo):null;
   const dueAt=item.kind==='promise'&&/截止|之前完成|前完成|到期/.test(item.quote)&&typeof item.dueAt==='string'&&heard.includes(item.dueAt.slice(0,10))&&/^\d{4}-\d{2}-\d{2}T/.test(item.dueAt)&&Number.isFinite(Date.parse(item.dueAt))?nowISO(item.dueAt):null;
   const row=this.add(item.kind,item.text,now,{sourceIds:[sourceId],source:'model-summary-of-user-statement',confidence:.55,importance,evidence:item.quote,importanceReason:reason,subject:'self',validTo,dueAt});
   if(row){
    if(sig&&/^expression:(concise|detailed):positive$/.test(sig)&&/以后|现在|改为|更正/.test(heard)){for(const prior of this.data.records.filter(r=>r.id!==row.id&&r.kind==='preference'&&['active','dormant'].includes(r.status)&&signature(r.kind,r.text)&&/^expression:(concise|detailed):positive$/.test(signature(r.kind,r.text))&&signature(r.kind,r.text)!==sig)){if(prior.pinned){row.pinned=true;row.importance=.95;}this.supersede(prior.id,row.id,now);}}
    count++;if(existing)this.reinforce(row,source,now,'independent user statement supports same summary');}
  }
  for(const change of Array.isArray(candidate.updates)?candidate.updates.slice(0,3):[]){
   if(!change||typeof change!=='object'||attribution(heard)!=='self-current')continue;
   const record=this.data.records.find(r=>r.id===change.id);if(!record||record.kind==='emotion'||!allowedIds.includes(record.id)||typeof change.quote!=='string'||change.quote.length<2||!heard.includes(change.quote)||typeof change.reason!=='string'||change.reason.trim().length<3)continue;
   if(shouldAppraise(heard)&&shouldAppraise(change.quote))continue;
   if(change.action==='reinforce'&&!/不再|不是|不对|别再|更正|纠正|改为|取消|不喜欢|不要/.test(heard)&&(/^(对[，,。！!]|对的|没错|是的|确认)/.test(heard)||/(仍然|一直|还是|继续)(使用|喜欢|希望|保持|遵守)|确实如此/.test(heard))){if(this.reinforce(record,source,now,change.reason))count++;}
   if(change.action==='resolve'&&record.kind==='promise'&&/已经完成|已完成|办好了|完成了|取消了|已取消|不用再|不需要再/.test(heard)){
    record.status='resolved';record.validTo=now;record.sourceIds=[...new Set([...record.sourceIds,sourceId])];record.resolution={at:now,quote:change.quote,reason:change.reason.slice(0,160),sourceId};count++;
   }
   if(change.action==='correct'&&/改为|更正|纠正|不再|现在改|已经改/.test(heard)&&typeof change.text==='string'&&change.text.trim()){
    const replacement=this.add(record.kind,change.text,now,{mention:record.mention||'allowed',sourceIds:[sourceId],source:'user-evidence-correction',confidence:.6,pinned:!!record.pinned,importance:record.pinned?.95:clamp(record.importance,.2,.8),evidence:change.quote});
    if(replacement&&replacement.id!==record.id){this.supersede(record.id,replacement.id,now);count++;}
   }
  }
  source.consolidated=true;this.data.queue=this.data.queue.filter(j=>j.sourceId!==source.id);this.maintain(now);this.save();return count;
 }

}
module.exports={Store,vector,cosine,SPACE,seconds};
