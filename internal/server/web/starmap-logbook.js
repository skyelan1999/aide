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
      text('logbook-sector', uiTextStarmapLogbook('等待定位')); text('logbook-sequence', 'OBS / —');
      text('logbook-dispatch', uiTextStarmapLogbook('观测舱待命。连接星图后，这里将记下本次探索的第一束星光。'));
      status('waiting', uiTextStarmapLogbook('等待星图信号'));
    }
    function observe(graph, { code = false, region = '' } = {}) {
      if (!graph.workspace) return;
      if (workspace !== graph.workspace) { clear(); workspace = graph.workspace; }
      const sources = Array.isArray(graph.sources) ? graph.sources : [];
      const nodes = (graph.nodes || []).filter(n => !region || n.region === region);
      const ids = new Set(nodes.map(n => n.id));
      const edges = (graph.edges || []).filter(e => !region || (ids.has(e.from) && ids.has(e.to)));
      const sector = region ? (sources.find(s => s.region === region)?.name || uiTextStarmapLogbook('选定星域')) : uiTextStarmapLogbook('全部已知星域');
      const view = code ? uiTextStarmapLogbook('代码星层') : uiTextStarmapLogbook('知识星层');
      const warnings = [...(graph.warnings || []), ...(graph.code?.diagnostics || [])];
      const limited = !!graph.truncated || sources.some(s => s.state === 'partial' || s.state === 'unavailable' || s.coverage?.reasons?.length);
      const pending = sources.some(s => s.coverage?.progressive && s.coverage.pending > 0);
      const state = warnings.length || limited ? 'caution' : pending ? 'scanning' : 'ready';
      const label = state === 'caution' ? uiTextStarmapLogbook('发现观测盲区 · 查看异常与范围') : pending ? uiTextStarmapLogbook('深空扫描中 · 仍有待探索区域') : uiTextStarmapLogbook('星图已同步 · 以已索引范围为准');
      status(state, label);
      text('logbook-sector', sector + ' / ' + view);
      text('logbook-stars', number(nodes.length)); text('logbook-routes', number(edges.length));
      text('logbook-sectors', Array.isArray(graph.sources) ? number(sources.length) : '—');
      text('logbook-dispatch', nodes.length
        ? uiTextStarmapLogbook("领航员，{0}已在舷窗外展开。{1} 颗知识星体，{2} 条有据可循的连接。沿星光靠近一份资料，或回放两次观测，看看这片宇宙如何改变。", sector, number(nodes.length), number(edges.length))
        : uiTextStarmapLogbook('领航员，这片星域还没有可见星体。接入资料来源，等待索引带回第一束星光。'));
      const next = JSON.stringify([code, region, nodes.length, edges.length, sources.map(s => [s.region, s.name, s.state]), warnings, limited, pending]);
      if (next === signature) return;
      const first = !signature;
      signature = next;
      record(state, uiTextStarmapLogbook("{0} · {1} / {2} · {3} 颗星体 / {4} 条连接{5}", first ? uiTextStarmapLogbook('观测舱接入') : uiTextStarmapLogbook('航图更新'), sector, view, number(nodes.length), number(edges.length), limited || warnings.length ? uiTextStarmapLogbook('；存在观测盲区，请查阅范围与诊断。') : pending ? uiTextStarmapLogbook('；扫描仍在继续。') : '。'));
    }
    function connecting() { status('scanning', uiTextStarmapLogbook('正在校准航图 · 连接索引')); }
    function interrupted() {
      status('caution', uiTextStarmapLogbook('星图信号中断 · 等待重新连接'));
      if (entries[0]?.kind !== 'interrupted') record('interrupted', uiTextStarmapLogbook('星图连接暂时中断。已显示的观测可能过时，等待重新连接。'));
    }
    clear();
    return { observe, connecting, interrupted, lock: clear };
  }
};

function uiTextStarmapLogbook(key, ...values) { return globalThis.window?.aideI18n?.t(key, ...values) ?? String(key).replace(/\{(\d+)\}/g, (match, index) => index < values.length ? String(values[index]) : match); }
