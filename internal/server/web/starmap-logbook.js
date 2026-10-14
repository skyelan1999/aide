/* A local observing journal. Entries describe UI/index events, never invented travel. */
'use strict';
window.AideStellarLogbook = {
  create({ root = document, now = () => new Date() } = {}) {
    const $ = id => root.getElementById(id);
    let workspace = null, signature = '', entries = [], sequence = 0;
    const number = n => Math.max(0, Number(n) || 0).toLocaleString();
    const text = (id, value) => { $(id).textContent = value; };
    function record(kind, message) {
      const date = now();
      entries.unshift({ kind, message, date });
      entries = entries.slice(0, 8);
      $('logbook-entries').replaceChildren();
      for (const entry of entries) {
        const row = root.createElement('li'), time = root.createElement('time'), body = root.createElement('span');
        time.dateTime = entry.date.toISOString();
        time.textContent = entry.date.toLocaleTimeString([], { hour12: false });
        body.textContent = entry.message;
        row.dataset.kind = entry.kind;
        row.append(time, body);
        $('logbook-entries').append(row);
      }
      $('logbook-received').dateTime = date.toISOString();
      text('logbook-received', date.toLocaleString([], { hour12: false }));
      text('logbook-sequence', 'OBS / ' + String(++sequence).padStart(3, '0'));
    }
    function status(kind, message) {
      if ($('logbook-state').dataset.state === kind && $('logbook-state').textContent === message) return;
      $('logbook-state').dataset.state = kind;
      text('logbook-state', message);
    }
    function clear() {
      workspace = null; signature = ''; entries = []; sequence = 0;
      $('logbook-entries').replaceChildren();
      for (const id of ['logbook-stars', 'logbook-routes', 'logbook-sectors', 'logbook-received']) text(id, '—');
      $('logbook-received').removeAttribute('datetime');
      text('logbook-sector', '等待定位'); text('logbook-sequence', 'OBS / —');
      text('logbook-dispatch', '观测舱待命。连接星图后，这里将记下本次探索的第一束星光。');
      status('waiting', '等待星图信号');
    }
    function observe(graph, { code = false, region = '' } = {}) {
      if (!graph.workspace) return;
      if (workspace !== graph.workspace) { clear(); workspace = graph.workspace; }
      const sources = Array.isArray(graph.sources) ? graph.sources : [];
      const nodes = (graph.nodes || []).filter(n => !region || n.region === region);
      const ids = new Set(nodes.map(n => n.id));
      const edges = (graph.edges || []).filter(e => !region || (ids.has(e.from) && ids.has(e.to)));
      const sector = region ? (sources.find(s => s.region === region)?.name || '选定星域') : '全部已知星域';
      const view = code ? '代码星层' : '知识星层';
      const warnings = [...(graph.warnings || []), ...(graph.code?.diagnostics || [])];
      const limited = !!graph.truncated || sources.some(s => s.state === 'partial' || s.state === 'unavailable' || s.coverage?.reasons?.length);
      const pending = sources.some(s => s.coverage?.progressive && s.coverage.pending > 0);
      const state = warnings.length || limited ? 'caution' : pending ? 'scanning' : 'ready';
      const label = state === 'caution' ? '发现观测盲区 · 查看异常与范围' : pending ? '深空扫描中 · 仍有待探索区域' : '星图已同步 · 以已索引范围为准';
      status(state, label);
      text('logbook-sector', sector + ' / ' + view);
      text('logbook-stars', number(nodes.length)); text('logbook-routes', number(edges.length));
      text('logbook-sectors', Array.isArray(graph.sources) ? number(sources.length) : '—');
      text('logbook-dispatch', nodes.length
        ? `领航员，${sector}已在舷窗外展开。${number(nodes.length)} 颗知识星体，${number(edges.length)} 条有据可循的连接。沿星光靠近一份资料，或回放两次观测，看看这片宇宙如何改变。`
        : '领航员，这片星域还没有可见星体。接入资料来源，等待索引带回第一束星光。');
      const next = JSON.stringify([code, region, nodes.length, edges.length, sources.map(s => [s.region, s.name, s.state]), warnings, limited, pending]);
      if (next === signature) return;
      const first = !signature;
      signature = next;
      record(state, `${first ? '观测舱接入' : '航图更新'} · ${sector} / ${view} · ${number(nodes.length)} 颗星体 / ${number(edges.length)} 条连接${limited || warnings.length ? '；存在观测盲区，请查阅范围与诊断。' : pending ? '；扫描仍在继续。' : '。'}`);
    }
    function connecting() { status('scanning', '正在校准航图 · 连接索引'); }
    function interrupted() {
      status('caution', '星图信号中断 · 等待重新连接');
      if (entries[0]?.kind !== 'interrupted') record('interrupted', '星图连接暂时中断。已显示的观测可能过时，等待重新连接。');
    }
    clear();
    return { observe, connecting, interrupted, lock: clear };
  }
};
