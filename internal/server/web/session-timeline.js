(function () {
  'use strict';
  // 会话时间线：把一次 run 内的用户输入（初始 prompt、插话）与模型正文分段
  // 按真实时间合并成一条有序条目流，供渲染层按时间顺序展示。
  function timeOf(value) {
    if (typeof value === 'number') return Number.isFinite(value) ? value : null;
    if (!value) return null;
    const raw = String(value).trim();
    let parsed = Date.parse(raw);
    if (Number.isFinite(parsed)) return parsed;
    // 后端用 RFC3339 纳秒（9 位小数），JS Date 只认毫秒，截断后再解析。
    parsed = Date.parse(raw.replace(/(\.\d{3})\d+/, '$1'));
    return Number.isFinite(parsed) ? parsed : null;
  }
  function entries(run) {
    if (!run) return [];
    const created = timeOf(run.created);
    const items = [];
    if (typeof run.prompt === 'string' && run.prompt.trim()) {
      items.push({kind: 'prompt', at: created, order: 0, content: run.prompt, assistantManaged: !!run.assistantManaged});
    }
    (run.steers || []).forEach((steer, index) => {
      if (!steer || steer.queued) return;
      if (typeof steer.content !== 'string' || !steer.content.trim()) return;
      items.push({kind: 'steer', at: timeOf(steer.at), order: 1, content: steer.content, assistantManaged: !!steer.assistantManaged, index});
    });
    for (const step of run.steps || []) {
      for (const segment of step.outputSegments || []) {
        if (typeof segment?.content !== 'string' || !segment.content.trim()) continue;
        items.push({kind: 'answer', at: timeOf(segment.at), order: 2, content: segment.content, round: segment.round, id: segment.id});
      }
    }
    // 旧记录缺时间时退回 run.created，保持“输入在前、回答在后”的稳定顺序。
    for (const item of items) if (item.at === null) item.at = created;
    return items.sort((left, right) => {
      const lt = left.at === null ? -Infinity : left.at;
      const rt = right.at === null ? -Infinity : right.at;
      if (lt !== rt) return lt - rt;
      if (left.order !== right.order) return left.order - right.order;
      return (left.index || 0) - (right.index || 0);
    });
  }
  // 本地时钟标签；无法解析时返回空串，不显示占位。
  function clock(value) {
    const time = timeOf(value);
    if (time === null) return '';
    const date = new Date(time);
    return String(date.getHours()).padStart(2, '0') + ':' + String(date.getMinutes()).padStart(2, '0');
  }
  const api = Object.freeze({entries, clock, timeOf});
  if (typeof module !== 'undefined' && module.exports) module.exports = api;
  if (typeof window !== 'undefined') window.AideSessionTimeline = api;
})();
