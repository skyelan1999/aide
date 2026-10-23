/* Model usage shares: exact ring geometry and a separate, accessible legend. */
(function (root) {
  'use strict';
  const COLORS = ['#3b82f6', '#f59e0b', '#8b5cf6', '#10b981', '#ef4444', '#06b6d4', '#ec4899', '#84cc16'];
  function formatTokens(value) {
    const count = Number(value);
    if (!Number.isFinite(count) || count <= 0) return '0';
    const units = ['', 'K', 'M', 'B', 'T'];
    let scaled = count, unit = 0;
    while (scaled >= 1000 && unit < units.length - 1) { scaled /= 1000; unit++; }
    // Promote rounded boundary values too, so the UI never shows 1000.0K.
    if (unit && Number(scaled.toFixed(1)) >= 1000 && unit < units.length - 1) { scaled /= 1000; unit++; }
    return unit ? scaled.toFixed(1) + units[unit] : String(Math.round(scaled));
  }
  function shares(perModel) {
    const entries = Object.entries(perModel || {}).map(([model, stats]) => ({model, total:Number(stats?.total)}))
      .filter(row => Number.isFinite(row.total) && row.total > 0).sort((a, b) => b.total - a.total || a.model.localeCompare(b.model));
    const total = entries.reduce((sum, row) => sum + row.total, 0);
    if (!Number.isFinite(total) || total <= 0) return {total:0, rows:[]};
    const rows = [], other = [];
    for (const row of entries) (row.total / total < 0.01 ? other : rows).push(row);
    if (other.length) rows.push({model:null, total:other.reduce((sum, row) => sum + row.total, 0), detail:other});
    let offset = 0;
    return {total, rows:rows.map((row, index) => {
      const share = row.total / total;
      const result = {...row, share, offset, color:COLORS[index % COLORS.length]};
      offset += share;
      return result;
    })};
  }
  function render(host, perModel, {t, format}) {
    const doc = host.ownerDocument, NS = 'http://www.w3.org/2000/svg';
    const node = (tag, cls, text) => { const n = doc.createElement(tag); n.className = cls; if (text !== undefined) n.textContent = text; return n; };
    const svgNode = (tag, attrs) => { const n = doc.createElementNS(NS, tag); for (const [key, value] of Object.entries(attrs)) n.setAttribute(key, value); return n; };
    const data = shares(perModel);
    host.replaceChildren(node('div', 'model-pie-title', t('模型 Token 占比')));
    if (!data.rows.length) { host.append(node('div', 'model-pie-empty', t('暂无模型调用数据'))); return; }
    const body = node('div', 'model-pie-body'), plot = node('div', 'model-pie-plot');
    const svg = svgNode('svg', {class:'model-pie-svg', viewBox:'0 0 160 160', 'aria-hidden':'true'});
    const center = node('div', 'model-pie-center');
    center.append(node('strong', '', format(data.total)), node('small', '', 'tokens'));
    const legend = node('ul', 'model-pie-legend'), circumference = 2 * Math.PI * 54, links = [];
    for (const row of data.rows) {
      const label = row.model === null ? t('其他') : row.model;
      const percent = row.share < 0.001 ? '<0.1%' : (row.share * 100).toFixed(1) + '%';
      const slice = svgNode('circle', {class:'pie-slice', cx:80, cy:80, r:54, fill:'none', stroke:row.color,
        'stroke-width':20, 'stroke-linecap':'butt', transform:'rotate(-90 80 80)',
        'stroke-dasharray':`${row.share * circumference} ${(1 - row.share) * circumference}`, 'stroke-dashoffset':-row.offset * circumference});
      svg.append(slice);
      const item = node('li', ''), button = node('button', 'model-pie-row'); button.type = 'button';
      const dot = node('span', 'model-pie-dot'); dot.style.backgroundColor = row.color; dot.setAttribute('aria-hidden', 'true');
      const name = node('span', 'model-pie-name', label), values = node('span', 'model-pie-values');
      values.append(node('strong', '', percent), node('small', '', format(row.total) + ' tokens'));
      button.append(dot, name, values);
      button.title = row.detail ? row.detail.map(part => `${part.model}: ${format(part.total)} tokens`).join('\n') : label;
      item.append(button); legend.append(item); links.push({slice, button});
      const highlight = () => { for (const link of links) link.slice.style.opacity = link.slice === slice ? '1' : '.28'; };
      const reset = () => { for (const link of links) link.slice.style.opacity = '1'; };
      for (const target of [slice, button]) { target.addEventListener('mouseenter', highlight); target.addEventListener('mouseleave', reset); }
      button.addEventListener('focus', highlight); button.addEventListener('blur', reset);
    }
    plot.append(svg, center); body.append(plot, legend); host.append(body);
  }
  const api = {formatTokens, shares, render};
  if (typeof module === 'object' && module.exports) module.exports = api;
  else root.AideModelTokenChart = api;
})(typeof window === 'object' ? window : globalThis);
