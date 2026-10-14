/* Plugin graph records are observations, never proof of applied side effects. */
(() => {
  'use strict';
  const states = {waiting:'等待依赖或名额', dispatched:'已派发', succeeded:'返回成功', proposed:'提案尚未应用', skipped:'未执行', uncertain:'结果待核实'};
  const merges = {pending:'等待归并', queued:'提案待审批', withheld:'提案未归并', rejected:'提案被拒绝', partial:'部分提案被拒绝'};
  const phases = {running:'调度中', completed:'调度结束', interrupted:'已中断'};
  const text = value => typeof value === 'string' ? value : '';
  const excerptLimit = 4096;
  function render(run, {el, t, open = new Set(), seen = new Set(), onEvidence}) {
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
        el('span', '', t(Object.hasOwn(phases, plan.state) ? phases[plan.state] : '未知状态')),
        el('span', '', t('已结束 {0}/{1}', ended, nodes.length)));
      if (uncertain) summary.append(el('span', 'plugin-plan-alert', t('待核实 {0}', uncertain)));
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
  window.AidePluginPlans = {render, restoreFocus};
})();
