(function () {
  'use strict';
  const key = 'aide.chat.parallelism';
  const choices = Object.freeze([[1, '标准速度'], [2, '轻度'], [3, '中度'], [4, '高'], [5, '极高'], [10, '极致']].map(Object.freeze));
  const normalize = value => /^(?:[1-5]|10)$/.test(String(value)) ? Number(value) : 1;
  let selected = 1;
  try { selected = normalize(localStorage.getItem(key)); } catch (_) {}
  let config;
  function sync() {
    if (!config?.select) return;
    const {select, mode, assistant, t} = config;
    select.disabled = mode() !== 'chat' || assistant();
    select.value = String(selected);
    select.title = select.disabled ? t('并行分析仅用于普通对话') : t('并行分析上限；规划后汇总为一个回答，更多请求会增加 Token 用量，不保证等比例提速');
  }
  function set(value) {
    selected = normalize(value);
    try { localStorage.setItem(key, String(selected)); } catch (_) {}
    sync();
  }
  function install(options) {
    config = options;
    if (!config.select) return;
    config.select.addEventListener('change', () => {
      set(config.select.value);
    });
    sync();
  }
  function value(mode, assistant) { return mode === 'chat' && !assistant ? selected : 1; }
  function render(run, {el, t, open, initial = false}) {
    const progress = run.parallel;
    if (!progress || !(run.parallelism > 1)) return null;
    const phases = {planning:'规划拆分', running:'并行分析', merging:'汇总回答', completed:'已汇总', fallback:run.status === 'running' ? '继续执行，等待并行机会' : '主任务处理', interrupted:'已中断'};
    const statuses = {running:'分析中', completed:'已完成', failed:'未完成', cancelled:'已取消'};
    const parts = progress.parts || [];
    const finished = parts.filter(part => part.status !== 'running').length;
    const panel = el('details', 'parallel-progress');
    panel.dataset.key = run.id + ':parallel'; panel.open = !!open?.has(panel.dataset.key) || (initial && ['planning', 'running', 'merging'].includes(progress.phase));
    const label = t('并行 {0}x · {1}', run.parallelism, t(phases[progress.phase] || '并行分析'));
    panel.append(el('summary', '', label + (parts.length ? ' · ' + finished + '/' + parts.length : '')));
    if (progress.round > 1) panel.append(el('p', 'parallel-reason', t('已评估 {0} 轮；下方累计保留各轮分析', progress.round)));
    if (progress.reason) panel.append(el('p', 'parallel-reason', progress.reason));
    for (const part of parts) {
      const row = el('div', 'parallel-part');
      row.append(el('span', '', progress.round > 1 ? t('第 {0} 轮 · {1}', part.round || 1, part.title) : part.title), el('small', '', t(statuses[part.status] || '等待')));
      if (part.model) row.append(el('small', '', t('模型：{0}', part.model)));
      panel.append(row);
    }
    return panel;
  }
  function renderRoutes(run, {el, t, open}) {
    if (!run.modelRoutes?.length) return null;
    const panel = el('details', 'parallel-progress model-route-progress');
    panel.dataset.key = run.id + ':model-routes'; panel.open = !!open?.has(panel.dataset.key);
    panel.append(el('summary', '', t('Auto 模型路由 · {0} 次选择', run.modelRoutes.length)));
    const stages = {plan:'规划', propose:'生成提案', review:'审核', chat:'执行', agent:'执行', 'parallel/plan':'拆分规划', 'parallel/part':'子任务分析'};
    for (const route of run.modelRoutes) {
      const row = el('div', 'parallel-part');
      row.append(el('span', '', t(stages[route.stage] || '执行')), el('small', '', route.model + (route.provider ? ' · ' + route.provider : '')));
      row.title = route.reason || '';
      if (route.reason) row.append(el('small', '', route.reason));
      for (const candidate of route.candidates || []) row.append(el('small', '', candidate.model + (candidate.score ? ' · ' + candidate.score : '') + ' · ' + candidate.reason));
      panel.append(row);
    }
    return panel;
  }
  window.AideParallelChat = {install, sync, set, value, render, renderRoutes, normalize, choices};
})();
