'use strict';
// Deliberately small, inspectable local semantic baseline; not a trained model.
const CONCEPTS=[['concise',/简洁|简短|精简|简明|短一点|少说|别啰嗦|不要啰嗦|言简意赅|长话短说|直接说重点|只说重点/],['detailed',/详细|展开讲|多说一点|讲清来龙去脉/],['questions',/追问|提问|问问题|问我/],['formal',/正式|专业|严谨/],['warm',/温柔|温暖|亲切/],['formulaic',/客服|套话|客套/],['initiative',/主动|提醒/],['blue',/蓝色|天蓝/],['green',/绿色|翠绿/],['red',/红色/],['project',/项目|工程/],['meeting',/会议|开会|评审/],['deadline',/截止|到期|期限/],['promise',/答应|承诺|约定/]];
const normalize=s=>String(s||'').normalize('NFKC').toLowerCase().replace(/[\s，,。.!！?？:：;；]/g,'');
function concepts(text){return CONCEPTS.filter(([,re])=>re.test(text)).map(([name])=>name)}
function semanticVector(text){const values=CONCEPTS.map(([,re])=>re.test(text)?1:0),norm=Math.hypot(...values)||1;return values.map(v=>v/norm)}
function polarity(text){return /不(?:太|怎么)?喜欢|不想|不希望|不愿意|不接受|不要(?:详细|简洁|蓝色|绿色|红色)|讨厌|拒绝|不再/.test(text)?'negative':'positive'}
function attribution(text){return /朋友|同事|他说|她说|小说|台词|假如|假设|如果/.test(text)?'other-or-hypothetical':/以前|过去|曾经|去年/.test(text)&&!/现在|如今|改为/.test(text)?'past':'self-current'}
function signature(kind,text){
 if(kind!=='preference'||attribution(text)!=='self-current')return null;
 const cs=concepts(text),styles=cs.filter(c=>['concise','detailed','questions','formulaic','warm','formal','initiative'].includes(c));
 // Only collapse a single, known expression preference. Projects, names and
 // arbitrary paraphrases are never guessed to be the same entity.
 if(styles.length!==1||cs.some(c=>!styles.includes(c)))return null;
 if(!/回答|回复|表达|说话|讲解|解释|语气|追问|啰嗦|套话|客服|只说重点|直接说重点|长话短说/.test(text))return null;
 const residual=normalize(text).replace(/用户|助理|希望|喜欢|偏好|习惯|请|我|你|的|要|以后|每次|通常|一直|回答|回复|表达|说话|讲解|解释|保持|尽量|一些|一点|风格|更加|更|简洁|简短|精简|简明|短一点|少说|别啰嗦|不要啰嗦|言简意赅|长话短说|直接说重点|只说重点|详细|展开讲|多说一点|讲清来龙去脉/g,'');
 if(residual.length>6)return null;
 return 'expression:'+styles[0]+':'+polarity(text);
}
function expression(query,habits={}){
 const scope=/代码|程序|报错|测试|技术/.test(query)?'technical':/难过|伤心|焦虑|开心|陪我/.test(query)?'support':'general';
 const verbosity=/详细|展开讲|多说一点/.test(query)?'detailed':/简短|简洁|短一点|只说重点/.test(query)?'concise':habits.verbosity<.4?'concise':'balanced';
 return {scope,verbosity,questions:habits.questions<.4||scope==='support'?'only-if-essential':'at-most-one',tone:scope==='technical'?'precise':scope==='support'?'gentle':'natural',avoidBoilerplate:habits.formulaic<.5,currentRequestOverridesHabits:true};
}
function routeContext(rows,{maxChars=3200,limit=8}={}){
 const budget=Math.max(256,Math.min(6000,maxChars)),out=[],seen=new Set(),coveredSources=new Set();let used=2;
 const priorities=[...rows.filter(r=>r.kind==='promise'),...rows.filter(r=>r.kind==='preference'),...rows.filter(r=>!['promise','preference'].includes(r.kind))];
 // At most two global preferences/promises each; reserve room for the query.
 const counts={};for(const row of priorities){if(out.length>=limit)break;if(['promise','preference'].includes(row.kind)&&(counts[row.kind]||0)>=2)continue;
  const key=signature(row.kind,row.text)||normalize(row.text);if(seen.has(key))continue;
  if(row.kind==='episode'&&coveredSources.has(row.id))continue;
  const value={...row,sourceIds:(row.sourceIds||[]).slice(-4),sourceCount:(row.sourceIds||[]).length,text:row.text.slice(0,row.kind==='promise'?320:240)};const cost=JSON.stringify(value).length+1;
  if(used+cost>budget)continue;out.push(value);used+=cost;seen.add(key);for(const id of row.sourceIds||[])coveredSources.add(id);counts[row.kind]=(counts[row.kind]||0)+1;
 }
 return {references:out,budget:{unit:'JSON UTF-16 code units',limit:budget,used,candidateCount:rows.length,selectedCount:out.length}};
}
function boundedHistory(history,budget=4000){const out=[];let size=0;for(const m of [...history].reverse()){const content=m.content.slice(-1600);if(size+content.length>budget)break;out.unshift({...m,content});size+=content.length;}return out;}

// Exact named-project boundaries, not fuzzy entity aliases. Shared acceptance
// markers and generic words cannot establish that two projects are identical.
function projectTopics(text){
 const s=String(text||'').normalize('NFKC').replace(/MIXPROBE-[a-f0-9]{32}/gi,' ').toLowerCase(),out=[];
 const clean=value=>value.replace(/^(?:(?:合成|验收|测试|请|帮我|回想|关于|针对|之前|以前|现在|今天|当前|承诺|完成|你刚才|你之前|你上次|我们|我的|我|对|给|为|的|在|喜欢|想起|再看|继续|讨论|谈谈|看看|一下))+/g,'');
 const generic=/^(?:这个|那个|本|新|旧|另一个|上一个|同一个|其他|其它|所有|整个|当前|合成|测试|验收)$/;
 for(const m of s.matchAll(/([\p{L}\p{N}_-]{1,40})项目/gu)){const n=clean(m[1]);if(n&&n.length<=24&&!generic.test(n))out.push(n);}
 for(const m of s.matchAll(/项目\s*[「“"《]([^」”"》]{1,24})[」”"》]/gu))out.push(m[1].trim());
 return [...new Set(out)].slice(0,8);
}
function recallCue(text){return /你(?:刚才|之前|上次|上一次)|你说过|你说的原话|(?:上次|上一次|前次|上一回)(?:通话|聊天|对话)|(?:上次|当时).*(?:建议|说|编号)|刚才.*(?:建议|说)|回想|还记得|之前.*(?:编号|代号)|(?:上次|之前|以前|刚才|前次).*(?:原话|原文|主动补充)/.test(text);}
function presentationRecall(text){return recallCue(text)&&/编号|标识|代号|原话|原文|原样|逐字|照读|一字不差|格式|json|markdown|表格/i.test(text);}
function neutralEvidence(text,{source=false}={}){
 let s=String(text||'');
 if(source)s=s.split(/(?<=[。！？!?\n])/).filter(part=>!/(?:编号|标识|代号|格式|json|markdown|表格|最后一行|末行|六位|自行生成)/i.test(part)).join('');
 return s.replace(/(?:\*\*)?(?:编号|标识|代号)(?:\*\*)?[：:\s=]*[A-Za-z0-9_-]{4,}(?:\*\*)?/g,'').replace(/^\s*\*{0,2}(?:记录|推测)[^\n]*\n/gm,'').trim();
}
module.exports={projectTopics,recallCue,presentationRecall,neutralEvidence,concepts,semanticVector,signature,normalize,polarity,attribution,expression,routeContext,boundedHistory,SEMANTIC_SPACE:'mixsoul-concepts-14-v1',DIMENSIONS:CONCEPTS.length};
