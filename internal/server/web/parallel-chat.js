(function () {
  'use strict';
  const key = 'aide.chat.parallelism';
  const normalize = value => /^[1-5]$/.test(String(value)) ? Number(value) : 1;
  let selected = 1;
  try { selected = normalize(localStorage.getItem(key)); } catch (_) {}
  let config, wheel;
  function sync(position = true) {
    if (!config?.select) return;
    const {select, mode, assistant, t} = config;
    select.disabled = mode() !== 'chat' || assistant();
    select.value = String(selected);
    select.title = select.disabled ? t('并行分析仅用于普通对话') : t('并行分析上限；规划后汇总为一个回答，更多请求会增加 Token 用量，不保证等比例提速');
    if (wheel) {
      wheel.root.classList.toggle('disabled', select.disabled);
      wheel.list.tabIndex = select.disabled ? -1 : 0;
      wheel.list.title = select.title;
      wheel.list.setAttribute('aria-label', t('会话并行分析上限'));
      wheel.list.setAttribute('aria-disabled', String(select.disabled));
      wheel.list.setAttribute('aria-activedescendant', wheel.items[selected - 1].id);
      wheel.items.forEach((item, index) => {
        item.classList.toggle('selected', index + 1 === selected);
        item.setAttribute('aria-selected', String(index + 1 === selected));
      });
      // A hidden menu has no scroll geometry. Position it after opening,
      // without interpreting its temporary zero offset as a user choice.
      if (position && wheel.list.clientHeight) wheel.list.scrollTop = (selected - 1) * (wheel.items[0].offsetHeight || 28);
    }
  }
  function choose(value, position = true) {
    selected = normalize(value);
    try { localStorage.setItem(key, String(selected)); } catch (_) {}
    sync(position);
  }
  function mountWheel(select) {
    const doc = select.ownerDocument;
    if (!doc?.createElement) return;
    const root = doc.createElement('span'), list = doc.createElement('span'), items = [];
    root.className = 'parallel-wheel'; list.className = 'parallel-wheel-options';
    list.setAttribute('role', 'listbox');
    list.setAttribute('aria-orientation', 'vertical');
    for (let value = 1; value <= 5; value++) {
      const item = doc.createElement('span');
      item.className = 'parallel-wheel-option'; item.id = select.id + '-option-' + value;
      item.textContent = value + 'x'; item.setAttribute('role', 'option');
      item.addEventListener('click', event => { event.preventDefault(); if (!select.disabled) { choose(value); list.focus(); } });
      items.push(item); list.append(item);
    }
    list.addEventListener('scroll', () => {
      if (select.disabled || !list.clientHeight) return;
      const value = Math.min(5, Math.max(1, Math.round(list.scrollTop / (items[0].offsetHeight || 28)) + 1));
      if (value !== selected) choose(value, false);
    });
    list.addEventListener('keydown', event => {
      if (select.disabled) return;
      const value = {ArrowUp: Math.max(1, selected - 1), ArrowDown: Math.min(5, selected + 1), Home: 1, End: 5}[event.key];
      if (value === undefined) return;
      event.preventDefault(); choose(value);
    });
    root.append(list); select.after(root); select.hidden = true; select.tabIndex = -1;
    wheel = {root, list, items};
  }
  function install(options) {
    config = options;
    if (!config.select) return;
    config.select.addEventListener('change', () => {
      choose(config.select.value);
    });
    mountWheel(config.select);
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
  window.AideParallelChat = {install, sync, value, render, normalize};
})();
