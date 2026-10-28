'use strict';
// These are conversational hypotheses, not clinical labels or measured affect.
const LABELS=new Set(['joy','sadness','anxiety','anger','frustration','relief','loneliness','calm']);
const WORDS={joy:'开心|高兴|快乐|兴奋',sadness:'难过|伤心|失落',anxiety:'焦虑|紧张|担心|不安',anger:'生气|愤怒|恼火',frustration:'沮丧|挫败|委屈|烦躁',relief:'松了[一口]*气|放心了|轻松了',loneliness:'孤独|寂寞',calm:'平静|安心|踏实'};
const NAMES={joy:'开心',sadness:'难过',anxiety:'担心或紧张',anger:'生气',frustration:'挫败',relief:'松了一口气',loneliness:'孤独',calm:'平静'};
const text=x=>typeof x==='string'?x:'';
const bounded=(x,lo,hi,fallback)=>Number.isFinite(x)?Math.min(hi,Math.max(lo,x)):fallback;
// Attribution is deliberately conservative. Quoted/third-party material cannot
// establish the user's current emotion, including embedded instructions.
function uncertainAttribution(heard){return /[“”「」『』"<>]|(?:朋友|同事|家人|妈妈|爸爸|老婆|老公|他说|她说|他很|她很|他今天|她今天|用户说|小说|台词|举例|假如|如果|假设|引用|转述|不是我|并非我|你觉得我|你认为我|我是不是|我是否|我会不会|骗你的|开玩笑|是假的)/.test(heard)}
function currentEmotion(heard,now){
 heard=text(heard).slice(0,2000);const avoided=/别再提|不要再提|不要提过去|别提过去|不想再提/.test(heard);
 const out={labels:[],evidence:[],basis:'unknown',confidence:0,at:now,avoidHistoricalMention:avoided};
 if(uncertainAttribution(heard))return out;
 // Past experience is a retrieval cue, never evidence of today's mood.
 if(/(?:当时|那天|以前|曾经|去年|昨天).*(?:难过|伤心|焦虑|开心|生气|孤独|紧张)/.test(heard)&&!/(?:现在|今天|此刻|这会儿)/.test(heard))return out;
 for(const [label,words] of Object.entries(WORDS)){
  const re=new RegExp('(?:我(?:现在|今天|此刻|这会儿|真的|还是|感到|觉得|有点|有些|很|特别|非常|太|好|挺|终于|已经|又|也|\\s)*)(?:'+words+')');
  const m=heard.match(re);if(!m)continue;
  if(/^(?:吗|么|是不是|是否|[？?])/.test(heard.slice(m.index+m[0].length)))continue;
  const clausePrefix=heard.slice(0,m.index).split(/[，,。.!！？?；;]/).pop();
  if(/(?:不|没|没有|并不|不是|不再|不怎么|谈不上).*$/.test(clausePrefix))continue;
  out.labels.push(label);out.evidence.push({label,quote:m[0]});out.quote ||= m[0];if(out.labels.length===2)break;
 }
 if(out.labels.length){out.basis='explicit';out.confidence=.7;}
 return out;
}
function shouldAppraise(heard){
 const s=text(heard);if(uncertainAttribution(s))return false;
 return /开心|高兴|难过|伤心|焦虑|紧张|担心|生气|委屈|孤独|沮丧|松了.*气|平静|烦躁|放心了|失落|挫败|不安|撑不住|努力.*没有结果|不知道怎么办|不想再提/.test(s);
}
function validateAppraisal(candidate,heard,now,referenceIds=[]){
 const seed=currentEmotion(heard,now),q=text(candidate?.quote).trim();
 const result={...seed,linkedMemoryIds:[]};
 if(uncertainAttribution(heard)||q.length<2||q.length>160||!heard.includes(q))return result;
 if(!['explicit','inferred','unknown'].includes(candidate?.basis))return result;
 let labels=[...new Set((Array.isArray(candidate.labels)?candidate.labels:[]).filter(x=>LABELS.has(x)))].slice(0,2);
 if(candidate.basis==='unknown'||!labels.length)return result;
 if(seed.basis==='explicit'){labels=labels.filter(x=>seed.labels.includes(x));if(!labels.length)return result;}
 // An explicit claim is only accepted if current self-report independently
// supports the same labels. Everything else stays a low-confidence hypothesis.
 const direct=seed.basis==='explicit'&&labels.every(x=>seed.evidence.some(e=>e.label===x&&q.includes(e.quote)));
 if(!direct&&/明天|将来|假如|如果|当时|那天|以前|曾经|去年|昨天/.test(q))return result;
 if(!direct&&/(?:不再|没有|并不|不是|不怎么|不|没)(?:很|太|那么|再)?(?:难过|焦虑|生气|开心|孤独|紧张|伤心)/.test(heard))return result;
 if(!direct&&/(?:当时|那天|以前|曾经|去年|昨天)/.test(heard)&&!/(?:现在|今天|此刻|这会儿)/.test(heard))return result;
 result.labels=labels;result.basis=direct?'explicit':'inferred';result.quote=q;
 result.confidence=bounded(candidate.confidence,0,direct?.75:.4,direct?.65:.3);
 // Low confidence does not authorize linking sensitive older experiences.
 if(result.confidence>=.5&&!result.avoidHistoricalMention){const allowed=new Set(referenceIds);result.linkedMemoryIds=[...new Set((Array.isArray(candidate.linkedMemoryIds)?candidate.linkedMemoryIds:[]).filter(id=>typeof id==='string'&&allowed.has(id)))].slice(0,2);}
 return result;
}
function emotionPolicy(current){return {mode:current?.basis==='explicit'?'acknowledge-gently':current?.basis==='inferred'?'tentative':'neutral',historicalEmotionIsCurrent:false,mentionPastEmotionOnlyIfInvited:true,avoidHistoricalMention:!!current?.avoidHistoricalMention,questions:'at-most-one-if-needed',diagnosisAllowed:false,factualConfidenceBoost:false,authorizationUnchanged:true};}
function calibrateEmotionReply(reply,current){
 const words='焦虑|紧张|担心|难过|伤心|失落|沮丧|泄气|挫败|委屈|烦躁|生气|愤怒|孤独|寂寞|开心|高兴|平静';
 if(current?.basis==='explicit')return reply;
 // Preserve quoted evidence, code and identifiers. Only calibrate assertions
 // outside quotations, never change what a source actually said.
 return String(reply).split(/(```[\s\S]*?```|`[^`]*`|“[^”]*”|「[^」]*」|"[^"\n]*")/g).map((part,i)=>i%2?part:part
  .replace(new RegExp('(?:换谁|任何人|谁)(?:都)?会(?:觉得|感到)?('+words+')','g'),'这可能会让人感到$1')
  .replace(new RegExp('你(?:现在|此刻|今天)?(?:肯定|一定|显然|就是|确实)(?:很|感到|觉得|有点|特别)?('+words+')','g'),'你可能有些$1')
  .replace(new RegExp('你(?:现在|此刻)?(?:很|感到|觉得)('+words+')','g'),'你可能有些$1')
  .replace(/我(?:完全|非常)懂你(?:的)?(?:感受|心情)/g,'我想听听你自己的感受')).join('');
}
module.exports={LABELS,NAMES,currentEmotion,shouldAppraise,validateAppraisal,emotionPolicy,calibrateEmotionReply};
