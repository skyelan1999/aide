(function () {
  'use strict';
  const key = 'aide.chat.parallelism';
  const normalize = value => /^[1-5]$/.test(String(value)) ? Number(value) : 1;
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
    const phases = {planning:'规划拆分', running:'并行分析', merging:'汇总回答', completed:'已汇总', fallback:'主任务处理', interrupted:'已中断'};
    const statuses = {running:'分析中', completed:'已完成', failed:'未完成', cancelled:'已取消'};
    const parts = progress.parts || [];
    const finished = parts.filter(part => part.status !== 'running').length;
    const panel = el('details', 'parallel-progress');
    panel.dataset.key = run.id + ':parallel'; panel.open = !!open?.has(panel.dataset.key) || (initial && ['planning', 'running', 'merging'].includes(progress.phase));
    const label = t('并行 {0}x · {1}', run.parallelism, t(phases[progress.phase] || '并行分析'));
    panel.append(el('summary', '', label + (parts.length ? ' · ' + finished + '/' + parts.length : '')));
    if (progress.reason) panel.append(el('p', 'parallel-reason', progress.reason));
    for (const part of parts) {
      const row = el('div', 'parallel-part');
      row.append(el('span', '', part.title), el('small', '', t(statuses[part.status] || '等待')));
      panel.append(row);
    }
    return panel;
  }
  window.AideParallelChat = {install, sync, set, value, render, normalize};
})();
