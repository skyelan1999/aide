'use strict';
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');

const source = fs.readFileSync('internal/server/web/app.js', 'utf8');
const signature = source.match(/function sessionRunRenderSignature\(run, view = \{\}\) \{[\s\S]*?\n\}/)?.[0];
const reuse = source.match(/function appendCachedSessionRun\(run, signature, previous, timeline\) \{[\s\S]*?\n\}/)?.[0];
const textRevision = source.match(/function sessionTextRevision\(value\) \{[\s\S]*?\n\}/)?.[0];
const sessionRevision = source.match(/function sessionRevision\(s\) \{[\s\S]*?\n\}/)?.[0];
const valueFingerprint = source.match(/function sessionValueFingerprint\(value\) \{[\s\S]*?\n\}/)?.[0];
assert.ok(signature && reuse && textRevision && sessionRevision, 'session and run-card fingerprints and reuse helpers are present');
const planView = source.match(/function sessionPluginPlanView\(run\) \{[\s\S]*?\n\}/)?.[0];
assert.ok(planView);
const context = {Math, state:{historyLimit:30}};
vm.createContext(context);
assert.ok(valueFingerprint, 'large run-card content uses a streaming value fingerprint');
vm.runInContext(`${planView}; ${textRevision}; ${sessionRevision}; ${valueFingerprint}; ${signature}; ${reuse}; this.textRevision=sessionTextRevision; this.sessionRevision=sessionRevision; this.valueFingerprint=sessionValueFingerprint; this.signature=sessionRunRenderSignature; this.reuse=appendCachedSessionRun;`, context);

const base = {
  id: 'run-1', mode: 'chat', prompt: 'summarize', status: 'completed', created: '2026-10-14T00:00:00Z',
  steps: [{name:'chat', status:'completed', content:'# Result\n\nStable answer', reasoning:'hidden reasoning'}],
  files: [], commands: [], toolUses: [], attachments: [], steers: [], approvalReviews: [], applied: false,
};
const original = context.signature(base);
assert.equal(context.signature(structuredClone(base)), original, 'equivalent persisted runs keep the same card signature');
assert.equal(context.valueFingerprint({b:['same',1],a:true}), context.valueFingerprint({a:true,b:['same',1]}), 'object insertion order does not affect fingerprints');
assert.notEqual(context.valueFingerprint(['ab','c']), context.valueFingerprint(['a','bc']), 'fingerprints preserve field boundaries');
const largeRun = structuredClone(base); largeRun.steps[0].content = 'x'.repeat(1024 * 1024);
assert.ok(context.signature(largeRun), 'large answers can be fingerprinted without materializing a serialized copy');
assert.doesNotMatch(signature, /JSON\.stringify/, 'run signature does not serialize the full rendered task into a temporary string');
const session = {id:'session-1',updated:'same',runs:[base],messages:[],runsTotal:1,messagesTotal:0};
const sessionSnapshot = context.sessionRevision(session);
const sameLengthPollEdit = structuredClone(session); sameLengthPollEdit.runs[0].steps[0].content = '# Result\n\nAnswer answer';
assert.notEqual(context.sessionRevision(sameLengthPollEdit), sessionSnapshot, 'equal-length last-run output edits must trigger a new session render');
const sameLengthReasoningEdit = structuredClone(session); sameLengthReasoningEdit.runs[0].steps[0].reasoning = 'masked reasoning';
const sameLengthReasoningBase = structuredClone(session); sameLengthReasoningBase.runs[0].steps[0].reasoning = 'hidden reasoning';
assert.equal(sameLengthReasoningEdit.runs[0].steps[0].reasoning.length, sameLengthReasoningBase.runs[0].steps[0].reasoning.length);
assert.notEqual(context.sessionRevision(sameLengthReasoningEdit), context.sessionRevision(sameLengthReasoningBase), 'equal-length reasoning edits must trigger a new session render');
const sameWaitingStateReview = structuredClone(session);
sameWaitingStateReview.runs[0].status = 'awaiting_clarification';
sameWaitingStateReview.runs[0].approvalState = 'reviewing';
const approvedWithoutStatusChange = structuredClone(sameWaitingStateReview);
approvedWithoutStatusChange.runs[0].approvalState = 'approved';
approvedWithoutStatusChange.runs[0].approvalReviews = [{decision:'approved', ruleId:'rule-1'}];
assert.equal(approvedWithoutStatusChange.runs[0].status, sameWaitingStateReview.runs[0].status);
assert.notEqual(context.sessionRevision(approvedWithoutStatusChange), context.sessionRevision(sameWaitingStateReview), 'approval decisions must trigger a render even while the run status stays the same');
const earlierPending = structuredClone(session);
earlierPending.runs.unshift({id:'run-old',mode:'agent',status:'awaiting_approval',approvalState:'reviewing',approvalReviews:[],steps:[]});
const earlierApproved = structuredClone(earlierPending);
earlierApproved.runs[0].approvalState = 'approved';
assert.notEqual(context.sessionRevision(earlierApproved), context.sessionRevision(earlierPending), 'approval changes on a non-latest visible run must trigger a render');
const modelEdit = structuredClone(base); modelEdit.model = 'model-b';
assert.notEqual(context.signature(modelEdit), original, 'model label changes invalidate the rendered card');
const sameLengthEdit = structuredClone(base); sameLengthEdit.steps[0].content = '# Result\n\nAnswer answer';
assert.notEqual(context.signature(sameLengthEdit), original, 'same-length answer edits invalidate the rendered card');
const phaseChange = structuredClone(base); phaseChange.status = 'failed';
assert.notEqual(context.signature(phaseChange), original, 'run state changes invalidate the rendered card');
assert.notEqual(context.signature(base, {language:'en'}), context.signature(base, {language:'zh-CN'}), 'language changes invalidate translated card labels');
assert.notEqual(context.signature(base, {ignoredProposal:false}), context.signature(base, {ignoredProposal:true}), 'local proposal dismissal changes invalidate the rendered card');

const node = {id:'run-1'}, timeline = {children:[], append(value){this.children.push(value);}};
assert.equal(context.reuse(base, original, new Map([['run-1',{node,signature:original}]]), timeline), true);
assert.equal(timeline.children[0], node, 'unchanged card DOM is preserved');
assert.equal(context.reuse(base, 'changed', new Map([['run-1',{node,signature:original}]]), timeline), false, 'changed cards are rebuilt');
assert.match(source, /box\.dataset\.renderSignature = renderSignature/);
console.log('session run-card reuse preserves unchanged DOM and invalidates changed content: PASS');
