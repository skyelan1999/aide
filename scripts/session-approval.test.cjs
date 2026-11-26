'use strict';
const assert = require('node:assert/strict');
const test = require('node:test');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const { readAppSource, sourceFunction } = require('./fixtures/app-source.cjs');

const app = readAppSource();
const ts = fs.readFileSync(path.join(__dirname, '../internal/server/web/app.ts'), 'utf8');
const runtime = fs.readFileSync(path.join(__dirname, '../internal/server/web/app-runtime.ts'), 'utf8');

test('approval state is isolated to the current session', () => {
  const context = {
    state: {approvalPolicy: {enabled: false}, session: {id: 'one', approvalMode: ''}},
    t: value => value,
  };
  vm.createContext(context);
  for (const name of ['currentSessionApprovalMode', 'composerAutoReview', 'composerApprovalLabel']) {
    vm.runInContext(sourceFunction(app, name), context);
  }
  assert.equal(vm.runInContext('composerAutoReview()', context), false);
  vm.runInContext('state.session.approvalMode="assisted_auto"', context);
  assert.equal(vm.runInContext('composerAutoReview()', context), true);
  assert.equal(vm.runInContext('composerApprovalLabel()', context), '辅助通过 · 本会话');
  vm.runInContext('state.session.approvalMode="full_auto"', context);
  assert.equal(vm.runInContext('composerAutoReview()', context), false);
  assert.equal(vm.runInContext('composerApprovalLabel()', context), '全部通过 · 本会话');
  vm.runInContext('state.session={id:"two",approvalMode:""}', context);
  assert.equal(vm.runInContext('composerAutoReview()', context), false, 'a different session must use the global policy');
});

test('high-risk menu requires explicit risk confirmation and bound master authorization', () => {
  for (const mode of ['assisted_auto', 'full_auto']) {
    assert.match(ts, new RegExp("strategyMenuOption\\('sessionApproval', '" + mode + "'"));
  }
  assert.match(ts, /我熟知并已确认风险/);
  assert.match(ts, /authBody: \{approvalSessionId: sessionID, approvalMode: mode\}/);
  assert.match(ts, /approvalGrant/);
  assert.match(ts, /riskConfirmation: grant \? '我熟知并已确认风险' : ''/);
  const change = sourceFunction(app, 'setSessionApprovalMode');
  assert.doesNotMatch(change, /api\('\/approval-policy'/, 'session mode must not modify the global policy');
  assert.match(change, /'\/sessions\/' \+ encodeURIComponent\(sessionID\) \+ '\/approval-mode'/);
  assert.match(runtime, /\.\.\.\(authBody \|\| \{\}\)/);
  assert.match(runtime, /done\(authBody \? result : pw\)/);
  assert.match(runtime, /done\(authBody \? result : 'success'\)/);
  assert.match(runtime, /approvalCredentialMissing = !!authBody\?\.approvalMode && !state\.config\?\.hasPassword && !hasCred/);
  assert.match(runtime, /高风险审批需要先设置账户密码或绑定指纹/);
});

test('risk dialog explains both automatic rejection and unconditional full-auto behavior', () => {
  const dialog = sourceFunction(app, 'confirmSessionApprovalRisk');
  assert.match(dialog, /拒绝、超时或证据不足时自动拒绝/);
  assert.match(dialog, /不再经过辅助审核或人工确认/);
  assert.match(dialog, /只对当前会话生效/);
  assert.match(dialog, /密码或指纹授权/);
});
