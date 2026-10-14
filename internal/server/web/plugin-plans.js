/* Plugin graph records are observations, never proof of applied side effects. */
(() => {
  'use strict';
  const states = {waiting:'等待依赖或名额', dispatched:'已派发', succeeded:'返回成功', proposed:'提案尚未应用', skipped:'未执行', uncertain:'结果待核实'};
  const merges = {pending:'等待归并', queued:'提案待审批', withheld:'提案未归并', rejected:'提案被拒绝', partial:'部分提案被拒绝'};
  const phases = {running:'调度中', completed:'调度结束', interrupted:'已中断'};
  const text = value => typeof value === 'string' ? value : '';
  const checks = {effect_observed:'观察到效果', no_effect_observed:'未观察到效果', inconclusive:'仍无法判断'};
  const excerptLimit = 4096;
  function render(run, {el, t, open = new Set(), seen = new Set(), onEvidence, onCheck}) {
    const plans = Array.isArray(run?.pluginPlans) ? run.pluginPlans.filter(p => p && typeof p === 'object') : [];
    if (!plans.length) return null;
    const host = el('section', 'plugin-plans');
    for (const [index, plan] of plans.entries()) {
      const nodes = Array.isArray(plan.nodes) ? plan.nodes.filter(n => n && typeof n === 'object') : [];
      const identityKey = [run.id, 'plugin-plan', plan.id || index];
      const key = JSON.stringify(identityKey);
      const uncertain = nodes.filter(n => n.state === 'uncertain').length;
      const ended = nodes.filter(n => ['succeeded','proposed','skipped','uncertain'].includes(n.state)).length;
      const panel = el('details', 'plugin-plan' + (uncertain ? ' plugin-plan-uncertain' : ''));
      panel.dataset.key = key;
      panel.open = open.has(key) || (!seen.has(key) && (plan.state === 'running' || uncertain > 0));
      const summary = el('summary', 'plugin-plan-summary');
      summary.dataset.pluginPlanFocus = key;
      summary.append(el('strong', '', t('插件任务图 {0}', index + 1)),
        el('span', '', ' · ' + t(Object.hasOwn(phases, plan.state) ? phases[plan.state] : '未知状态')),
        el('span', '', ' · ' + t('已结束 {0}/{1}', ended, nodes.length)));
      if (uncertain) summary.append(el('span', 'plugin-plan-alert', ' · ' + t('待核实 {0}', uncertain)));
      panel.append(summary);
      const multiplier = value => Number.isInteger(Number(value)) && Number(value) >= 1 && Number(value) <= 5 ? Number(value) : null;
      const requested = multiplier(plan.requested) ?? multiplier(plan.limit) ?? '—';
      const limit = multiplier(plan.limit) ?? '—';
      panel.append(el('p', 'plugin-plan-policy', t('请求 {0}x · 执行上限 {1}x', requested, limit)),
        el('p', 'plugin-plan-note', t('派发包含资源等待；写入提案仍需审批应用。')));
      if (plan.reason) panel.append(el('p', 'plugin-plan-reason', text(plan.reason).slice(0,1200)));
      if (uncertain) panel.append(el('p', 'plugin-plan-warning', t('存在待核实结果，不能自动重放。')));
      const list = el('ol', 'plugin-plan-nodes');
      for (const node of nodes) {
        const row = el('li', 'plugin-plan-node');
        const state = Object.hasOwn(states, node.state) ? node.state : 'unknown';
        row.dataset.state = state;
        const top = el('div', 'plugin-node-top');
        const identity = el('div', 'plugin-node-identity');
        identity.append(el('strong', '', text(node.title || node.id).slice(0,512)), el('code', '', text(node.tool).slice(0,256)));
        const badge = el('span', 'plugin-node-state', t(states[state] || '未知状态'));
        if (state === 'dispatched') badge.title = t('可能仍在等待资源，未证明处理器已进入');
        top.append(identity, badge); row.append(top);
        const deps = (Array.isArray(node.dependsOn) ? node.dependsOn : []).filter(id => typeof id === 'string');
        const dependency = el('div', 'plugin-node-deps');
        dependency.append(el('span', '', deps.length ? t('依赖节点') : t('无前置依赖')));
        deps.forEach(id => dependency.append(el('code', '', id.slice(0,256))));
        row.append(dependency);
        if (node.mergeState) row.append(el('p', 'plugin-node-merge', t(Object.hasOwn(merges, node.mergeState) ? merges[node.mergeState] : '未知提案状态')));
        if (node.error) row.append(el('p', 'plugin-node-error', text(node.error).slice(0,1200)));
        if (state === 'uncertain') row.append(el('p', 'plugin-node-error', t('此节点可能已产生外部副作用，请先核实。')));
        const history = Array.isArray(node.effectChecks) ? node.effectChecks : [];
        if (history.length) {
          const detail = el('details', 'plugin-node-checks');
          detail.dataset.key = JSON.stringify([...identityKey, 'node', node.id, 'checks']);
          detail.open = open.has(detail.dataset.key);
          detail.append(el('summary', '', t('人工核查记录 {0}', history.length)), el('p', 'plugin-plan-note', t('操作者观察不替代执行回执；不会重放调用或恢复依赖。')));
          for (const check of history.slice(-20)) {
            detail.append(el('p', '', t(Object.hasOwn(checks,check.conclusion) ? checks[check.conclusion] : '未知状态') + ' · ' + text(check.recordedAt)), el('pre', 'plugin-node-result', text(check.evidence).slice(0,4096)));
          }
          row.append(detail);
        }
        if (state === 'uncertain' && plan.state !== 'running' && ['paused','interrupted','completed','failed','cancelled'].includes(run.status) && typeof onCheck === 'function') {
          const button = el('button', 'quiet', t('记录人工核查')); button.type = 'button';
          button.dataset.pluginPlanFocus = JSON.stringify([...identityKey, 'node', node.id, 'check']);
          button.onclick = () => onCheck(plan, node); row.append(button);
        }
        const result = text(node.result);
        if (result) {
          const resultKey = JSON.stringify([...identityKey, 'node', node.id, 'result']);
          const detail = el('details', 'plugin-node-result-detail');
          detail.dataset.key = resultKey; detail.open = open.has(resultKey);
          const heading = el('summary', '', t('查看节点返回摘要'));
          heading.dataset.pluginPlanFocus = resultKey;
          const body = el('pre', 'plugin-node-result');
          const draw = () => { body.textContent = detail.open ? result.slice(0,excerptLimit) : ''; };
          detail.addEventListener('toggle', draw); draw();
          detail.append(heading, body);
          if (result.length > excerptLimit) detail.append(el('p', 'plugin-plan-note', t('仅展示前 {0} 个字符，完整记录见执行证据。', excerptLimit)));
          row.append(detail);
        }
        list.append(row);
      }
      panel.append(list);
      if (typeof onEvidence === 'function') {
        const evidence = el('button', 'quiet plugin-plan-evidence', t('查看执行证据'));
        evidence.type = 'button'; evidence.dataset.pluginPlanFocus = JSON.stringify([...identityKey, 'evidence']);
        evidence.onclick = onEvidence; panel.append(evidence);
      }
      host.append(panel);
    }
    return host;
  }
  function restoreFocus(root, key) {
    if (!key) return false;
    const target = [...root.querySelectorAll('[data-plugin-plan-focus]')].find(n => n.dataset.pluginPlanFocus === key);
    if (!target) return false;
    target.focus({preventScroll:true}); return true;
  }
  let checkDialog = null;
  function closeCheck() { checkDialog?.(); }
  function openCheck({el, t, plan, node, active, submit, saved}) {
    closeCheck();
    if (!active()) return;
    const dialog = el('dialog', 'plugin-effect-dialog');
    const heading = el('h2', '', t('记录人工核查'));
    heading.id = 'plugin-effect-heading'; dialog.setAttribute('aria-labelledby', heading.id);
    const verdict = el('select');
    for (const [value, label] of Object.entries(checks)) { const option = el('option', '', t(label)); option.value = value; verdict.append(option); }
    verdict.value = 'inconclusive';
    const choice = el('label', '', t('核查结论')); choice.append(verdict);
    const evidence = el('textarea'); evidence.rows = 6; evidence.maxLength = 4096;
    const field = el('label', '', t('核查依据')); field.append(evidence);
    const error = el('p', 'plugin-node-error'); error.setAttribute('role', 'status');
    const cancel = el('button', 'quiet', t('取消')); cancel.type = 'button';
    const save = el('button', 'primary', t('保存核查记录')); save.type = 'button';
    const footer = el('div', 'editor-footer'); footer.append(cancel, save);
    dialog.append(heading, el('code', '', text(node.tool)), el('p', 'plugin-plan-note', t('操作者观察不替代执行回执；不会重放调用或恢复依赖。')), choice, field, error, footer);
    let closed = false, timer;
    const close = () => { if (closed) return; closed = true; clearInterval(timer); evidence.value = ''; dialog.close(); dialog.remove(); if (checkDialog === close) checkDialog = null; };
    checkDialog = close;
    cancel.onclick = close; dialog.addEventListener('cancel', close);
    const expected = Array.isArray(node.effectChecks) ? node.effectChecks.length : 0;
    save.onclick = async () => {
      if (closed || save.disabled) return;
      if (!active()) { close(); return; }
      if (!evidence.value.trim()) { error.textContent = t('请填写核查依据'); evidence.focus(); return; }
      save.disabled = true; error.textContent = '';
      try {
        await submit({planDigest:plan.digest, expected, conclusion:verdict.value, evidence:evidence.value});
        if (!closed && active()) { close(); saved?.(); }
      } catch (e) { if (!closed && active()) { error.textContent = e.message; save.disabled = false; } }
      if (!active()) close();
    };
    document.body.append(dialog); dialog.showModal(); evidence.focus();
    timer = setInterval(() => { if (!active()) close(); }, 250);
    return dialog;
  }
  function openRecovery({el,t,review,active,submit,saved}) {
    closeCheck(); if (!active()) return;
    const dialog=el('dialog','plugin-effect-dialog');
    const heading=el('h2','',t('核查后继续任务'));heading.id='plugin-recovery-heading';dialog.setAttribute('aria-labelledby',heading.id);
    dialog.append(heading,el('p','plugin-plan-note',t('不会自动重放旧调用或补跑旧依赖。后续新操作仍须遵循当前审批、沙箱与任务 Hook。')));
    for(const plan of review.plans||[]) {
      dialog.append(el('h3','',text(plan.id).slice(0,256)));
      if(!plan.nodes?.length) dialog.append(el('p','',t('此未完成任务图将保留原记录，后续另建新图。')));
      for(const n of plan.nodes||[]) dialog.append(el('p','',text(n.id).slice(0,256)+' · '+text(n.tool).slice(0,256)+' · '+(n.check?t(Object.hasOwn(checks,n.conclusion)?checks[n.conclusion]:'未知状态'):t('尚无有效人工核查记录'))));
    }
    for(const call of review.calls||[]) dialog.append(el('p','',t('未确认调用')+' · '+text(call.id).slice(0,256)+' · '+text(call.tool).slice(0,256)));
    if(review.hookWarning) dialog.append(el('p','plugin-node-error',t('后台调用或 Hook 曾缺少回执，外部效果仍未确认。')));
    if(!review.ready) dialog.append(el('p','plugin-node-error',t('请先为每个不确定节点记录人工核查，再重新打开恢复预览。')));
    const note=el('textarea');note.rows=5;note.maxLength=4096;const field=el('label','',t('继续依据'));field.append(note);
    const acknowledge=el('input');acknowledge.type='checkbox';const ack=el('label','plugin-recovery-ack');ack.append(acknowledge,el('span','',t('我已查看上述缺口，同意仅继续后续新工作。')));
    const error=el('p','plugin-node-error');error.setAttribute('role','status');
    const cancel=el('button','quiet',t('取消'));cancel.type='button';const save=el('button','primary',t('确认继续后续任务'));save.type='button';save.disabled=!review.ready;
    const footer=el('div','editor-footer');footer.append(cancel,save);dialog.append(field,ack,error,footer);
    let closed=false,timer;const close=()=>{if(closed)return;closed=true;clearInterval(timer);note.value='';dialog.close();dialog.remove();if(checkDialog===close)checkDialog=null;};checkDialog=close;
    cancel.onclick=close;dialog.addEventListener('cancel',close);
    save.onclick=async()=>{
      if(closed||save.disabled)return;if(!active()){close();return;}
      if(!acknowledge.checked||!note.value.trim()){error.textContent=t('请填写继续依据并确认已查看缺口');return;}
      save.disabled=true;error.textContent='';
      try{const run=await submit({snapshot:review.snapshot,decision:'continue_without_replay',note:note.value});if(!closed&&active()){close();await saved?.(run);}}
      catch(e){if(!closed&&active()){error.textContent=e.message;save.disabled=false;}}
      if(!active())close();
    };
    document.body.append(dialog);dialog.showModal();note.focus();timer=setInterval(()=>{if(!active())close();},250);return dialog;
  }
  window.AidePluginPlans = {render, restoreFocus, openCheck, closeCheck, openRecovery};
})();
