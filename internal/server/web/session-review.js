(() => {
  'use strict';

  const timeOf = value => {
    const time = Date.parse(value || '');
    return Number.isFinite(time) ? time : null;
  };
  const timeLabel = value => value == null ? '—' : new Date(value).toLocaleString();
  const shortTime = value => new Date(value).toLocaleTimeString([], {hour: '2-digit', minute: '2-digit'});
  const clip = (value, length = 220) => {
    const plain = String(value || '').replace(/\s+/g, ' ').trim();
    return plain.length > length ? plain.slice(0, length) + '…' : plain;
  };
  const formatDuration = value => {
    if (!Number.isFinite(value) || value < 0) return '';
    if (value < 1000) return `${Math.round(value)} ms`;
    if (value < 60000) return `${(value / 1000).toFixed(1)} s`;
    return `${Math.floor(value / 60000)} min ${Math.round(value % 60000 / 1000)} s`;
  };
  function timing(record, index, source) {
    const start = timeOf(source === 'snapshot' ? record.at : record.startedAt);
    const end = timeOf(record.completedAt);
    const elapsed = Number(record.elapsedMillis);
    return {
      id: `${source}-${index}`, type: source === 'snapshot' ? 'model' :
        record.kind === 'model_request' ? 'model' : record.kind === 'tool_call' ? 'tool' : 'system',
      kind: source === 'snapshot' ? 'model_request' : String(record.kind || 'other'),
      stage: source === 'snapshot' ? String(record.purpose || 'other') : String(record.stage || 'other'),
      outcome: String(record.outcome || 'unknown'), source, start, end, at: start,
      elapsed: Number.isFinite(elapsed) && elapsed >= 0 ? elapsed :
        start != null && end != null ? Math.max(0, end - start) : null,
    };
  }
  function build(session, journals = {}) {
    const runs = Array.isArray(session?.runs) ? session.runs.filter(Boolean) : [];
    const total = Number.isInteger(session?.runsTotal) ? session.runsTotal : runs.length;
    const turns = runs.map((run, index) => {
      const ledger = Array.isArray(run.orchestrationEvents) ? run.orchestrationEvents : [];
      // Snapshot bodies may include provider secrets or full prompts; only timing metadata is read.
      const journal = journals[run.id] || {};
      const journalEvents = Array.isArray(journal.events) ? journal.events : [];
      const dispatches = journalEvents.filter(item => item.kind === 'tool_dispatch');
      const results = journalEvents.filter(item => item.kind === 'tool_result');
      const records = ledger.length ? ledger.map((item, i) => timing(item, i, 'ledger'))
        .filter(item => !dispatches.length || item.kind !== 'tool_call') :
        (Array.isArray(run.requestSnapshots) ? run.requestSnapshots : []).map((item, i) => timing(item, i, 'snapshot'));
      const started = timeOf(run.created);
      const flow = [];
      if (run.prompt) flow.push({id:'input', type:'input', at:started, label:'用户输入', preview:clip(run.prompt), content:String(run.prompt)});
      for (const item of records) flow.push({...item, label:item.kind === 'model_request' ? '模型请求' : item.kind === 'tool_call' ? '工具调用' : '编排事件',
        preview:item.stage, content:''});
      const toolUses = Array.isArray(run.toolUses) ? run.toolUses : [];
      // The execution journal owns call order and timestamps. Match a saved
      // result only when both its ordinal and tool name agree; otherwise leave
      // the old ToolUse untimed instead of claiming a false correlation.
      const receipts = toolUses.map((tool, i) =>
        results[i]?.tool === tool.tool ? results[i] : null);
      const receiptByCall = new Map(receipts.map((receipt, i) =>
        receipt ? [receipt.callId, toolUses[i]] : null).filter(Boolean));
      for (let i = 0; i < dispatches.length; i++) {
        const dispatch = dispatches[i];
        const tool = receiptByCall.get(dispatch.callId);
        flow.push({id:`dispatch-${i}`, type:'tool', at:timeOf(dispatch.at),
          label:`工具调用 · ${dispatch.tool || '未知工具'}`,
          preview:clip(tool?.args || ''), args:String(tool?.args || ''), callId:String(dispatch.callId || '')});
      }
      for (let i = 0; i < toolUses.length; i++) {
        const tool = toolUses[i];
        const at = timeOf(receipts[i]?.at || tool.completedAt);
        flow.push({id:`tool-result-${i}`, type:'tool_result', at, label:`工具结果 · ${tool.tool || '未知工具'}`,
          preview:clip(tool.preview || tool.result), content:String(tool.preview || tool.result || ''),
          args:dispatches.length ? '' : String(tool.args || ''),
          callId:String(receipts[i]?.callId || tool.callId || ''), legacy:at == null});
      }
      let outputIndex = 0;
      for (const step of Array.isArray(run.steps) ? run.steps : []) {
        const segments = Array.isArray(step.outputSegments) ? step.outputSegments : [];
        if (segments.length) {
          for (const segment of segments) {
            const at = timeOf(segment.at);
            flow.push({id:`output-${outputIndex++}`, type:'assistant', at,
              label:step.name === 'chat' ? '助手输出' : `阶段输出 · ${step.name || '未知'}`,
              preview:clip(segment.content), content:String(segment.content || ''), legacy:at == null});
          }
        } else if (step.content && step.status === 'completed') {
          flow.push({id:`step-${outputIndex++}`, type:'assistant', at:null, label:`阶段结果 · ${step.name || '未知'}`,
            preview:clip(step.content), content:String(step.content), legacy:true});
        }
      }
      // Untimed historical entries are kept after timed entries, never assigned a fabricated time.
      flow.sort((a, b) => (a.at ?? Infinity) - (b.at ?? Infinity));
      const points = [started, ...records.flatMap(item => [item.start, item.end]),
        ...flow.map(item => item.at)].filter(value => value != null);
      return {
        id:String(run.id || `turn-${index}`), number:total-runs.length+index+1,
        status:String(run.status || 'unknown'), model:String(run.model || ''),
        workspaceMode:String(run.workspaceMode || ''), started:points.length ? Math.min(...points) : null,
        lastEvent:points.length && records.some(item => item.start != null) ?
          Math.max(...records.flatMap(item => [item.start, item.end]).filter(value => value != null)) : null,
        events:records, flow, dropped:Number(run.orchestrationDropped) || 0,
        journalWarning:String(journal.warning || ''),
        toolCount:toolUses.length, usage:run.usage || {},
      };
    });
    const points = turns.flatMap(turn => [turn.started, ...turn.flow.flatMap(item => [item.at, item.end])])
      .filter(value => value != null);
    const latest = turns.at(-1);
    return {
      id:String(session?.id || ''), title:String(session?.title || ''),
      status:turns.some(turn => turn.status === 'running') ? 'running' : latest?.status ||
        (session?.kind === 'assistant' ? 'chat' : 'empty'),
      model:latest?.model || '', workspaceMode:latest?.workspaceMode || '',
      created:timeOf(session?.created), updated:timeOf(session?.updated),
      total, loaded:turns.length, turns,
      first:points.length ? Math.min(...points) : null, last:points.length ? Math.max(...points) : null,
      toolsLoaded:turns.reduce((sum, turn) => sum + turn.toolCount, 0),
      usage:session?.sessionUsage || turns.reduce((sum, turn) => ({
        prompt:sum.prompt+(Number(turn.usage.prompt)||0),
        completion:sum.completion+(Number(turn.usage.completion)||0),
        total:sum.total+(Number(turn.usage.total)||0),
      }), {prompt:0, completion:0, total:0}),
      hasOlder:total > turns.length,
    };
  }

  function mount(host, session, {t = value => value, loadOlder, journals = {}} = {}) {
    const review = build(session, journals);
    const el = (tag, cls, value) => {
      const node = document.createElement(tag);
      if (cls) node.className = cls;
      if (value != null) node.textContent = value;
      return node;
    };
    const status = value => ({
      running:t('运行中'), completed:t('已完成'), failed:t('失败'), paused:t('已暂停'),
      cancelled:t('已取消'), awaiting_approval:t('等待审批'), awaiting_clarification:t('等待澄清'),
      chat:t('仅对话'), empty:t('无任务'),
    })[value] || value || t('未记录');
    const typeLabel = value => ({
      input:t('输入'), model:t('模型'), tool:t('工具'), tool_result:t('结果'),
      assistant:t('助手'), system:t('编排'),
    })[value] || value;
    const field = (label, value) => {
      const node = el('div', 'sr-field');
      node.append(el('span', 'sr-field-label', label), el('strong', '', value == null || value === '' ? t('未记录') : String(value)));
      return node;
    };
    const heading = el('div', 'sr-heading');
    const title = el('div', 'sr-heading-title');
    title.append(el('span', 'sr-eyebrow', t('会话状态评审')), el('h3', '', review.title || t('未命名会话')));
    heading.append(title, el('span', `sr-status sr-status-${review.status}`, status(review.status)));
    const meta = el('div', 'sr-overview');
    meta.append(
      field(t('模型'), review.model), field(t('任务轮次'), review.total),
      field(t('工具调用'), review.toolsLoaded), field(t('Token 用量'), Number(review.usage.total || 0).toLocaleString()),
      field(t('开始时间'), timeLabel(review.created)), field(t('最近活动'), timeLabel(review.updated)),
    );
    const note = el('p', 'sr-note', t('按已保存的请求计时、工具结果和阶段输出展示。旧记录缺少时间时放在对应轮次末尾；并行事件可能重叠。'));
    const toolbar = el('div', 'sr-toolbar');
    const search = el('input', 'sr-search');
    search.type = 'search'; search.placeholder = t('搜索调用、结果或内容');
    search.setAttribute('aria-label', search.placeholder);
    const collapse = el('button', '', t('收起所有轮次'));
    collapse.type = 'button';
    toolbar.append(el('span', 'sr-toolbar-title', t('会话调用流程')), search, collapse);
    const overview = el('div', 'sr-flow-overview');
    const transcript = el('div', 'sr-transcript');
    const older = review.hasOlder && loadOlder ? el('button', 'sr-older', t('加载更早的任务')) : null;
    if (older) { older.type = 'button'; older.onclick = loadOlder; }
    host.replaceChildren(heading, meta, note, toolbar, overview, transcript, ...(older ? [older] : []));

    const span = Math.max(1, (review.last ?? 0) - (review.first ?? 0));
    const position = at => review.first == null || at == null ? 0 :
      Math.max(0, Math.min(99.5, (at - review.first) / span * 100));
    const kinds = ['input', 'model', 'tool'];
    for (const kind of kinds) {
      const lane = el('div', 'sr-flow-lane');
      lane.append(el('span', 'sr-flow-label', typeLabel(kind)));
      const plot = el('div', 'sr-flow-plot');
      for (const turn of review.turns) for (const item of turn.flow) {
        if (item.at == null || (kind === 'tool' ? item.type !== 'tool' && item.type !== 'tool_result' : item.type !== kind)) continue;
        const bar = el('button', `sr-flow-mark sr-flow-mark-${kind}`);
        bar.type = 'button';
        bar.style.left = `${position(item.at)}%`;
        bar.style.width = `min(max(4px, ${Math.max(0, (item.end ?? item.at) - item.at) / span * 100}%), ${100-position(item.at)}%)`;
        bar.title = `${t('第 {0} 轮', turn.number)} · ${item.label} · ${item.preview || ''}`;
        bar.setAttribute('aria-label', bar.title);
        bar.onclick = () => {
          const target = document.getElementById?.(`sr-${turn.id}-${item.id}`);
          if (target) { target.open = true; target.scrollIntoView?.({block:'center', behavior:'smooth'}); }
        };
        plot.append(bar);
      }
      lane.append(plot); overview.append(lane);
    }
    if (review.first != null) {
      const scale = el('div', 'sr-flow-scale');
      scale.append(el('span', '', shortTime(review.first)),
        el('span', '', shortTime(review.first + span / 2)), el('span', '', shortTime(review.last)));
      overview.append(scale);
    }

    let allCollapsed = false;
    const cards = [];
    function render(query = '') {
      transcript.replaceChildren();
      cards.length = 0;
      const needle = query.trim().toLocaleLowerCase();
      for (const turn of review.turns) {
        const matches = turn.flow.filter(item => !needle ||
          `${item.label} ${item.preview} ${item.content || ''} ${item.args || ''}`.toLocaleLowerCase().includes(needle));
        if (needle && !matches.length) continue;
        const card = el('section', 'sr-turn-card');
        const header = el('button', 'sr-turn-header');
        header.type = 'button';
        header.setAttribute('aria-expanded', String(!allCollapsed));
        header.append(el('span', 'sr-turn-title', `${t('第 {0} 轮', turn.number)} · ${status(turn.status)}`),
          el('span', 'sr-turn-meta', `${turn.model || t('模型未记录')} · ${turn.toolCount} ${t('次工具调用')}`),
          el('span', 'sr-chevron', '⌄'));
        const rows = el('div', 'sr-turn-rows');
        rows.hidden = allCollapsed;
        for (const item of matches) {
          const row = el('details', `sr-row sr-row-${item.type}`);
          row.id = `sr-${turn.id}-${item.id}`;
          const summary = el('summary', 'sr-row-summary');
          summary.append(el('span', `sr-role sr-role-${item.type}`, typeLabel(item.type)),
            el('span', 'sr-row-name', item.label),
            el('span', 'sr-row-preview', item.preview || '—'),
            el('time', 'sr-row-time', item.at == null ? t('时间未记录') :
              `${shortTime(item.at)}${item.elapsed != null ? ' · ' + formatDuration(item.elapsed) : ''}`));
          row.append(summary);
          const detail = el('div', 'sr-row-detail');
          if (item.content) detail.append(el('pre', '', item.content.slice(0, 5000)));
          if (item.args) detail.append(el('pre', '', `${t('参数')}\n${item.args.slice(0, 5000)}`));
          if (item.outcome) detail.append(el('p', '', `${t('结果')}：${item.outcome} · ${timeLabel(item.start)} → ${timeLabel(item.end)}`));
          if (item.legacy) detail.append(el('p', 'sr-note', t('此条历史记录没有精确时间，显示顺序仅反映保存顺序。')));
          row.append(detail); rows.append(row);
        }
        if (turn.dropped) rows.append(el('p', 'sr-note sr-dropped', t('早期 {0} 条计时记录已被有界日志淘汰。', turn.dropped)));
        if (turn.journalWarning) rows.append(el('p', 'sr-note sr-dropped', turn.journalWarning));
        if (!matches.length) rows.append(el('p', 'sr-empty', t('本轮没有可展示的调用记录')));
        header.onclick = () => {
          rows.hidden = !rows.hidden;
          header.setAttribute('aria-expanded', String(!rows.hidden));
        };
        card.append(header, rows); transcript.append(card); cards.push({header, rows});
      }
      if (!cards.length) transcript.append(el('p', 'sr-empty', needle ? t('没有匹配的调用记录') : t('当前会话没有任务轮次')));
    }
    search.oninput = () => render(search.value);
    collapse.onclick = () => {
      allCollapsed = !allCollapsed;
      collapse.textContent = allCollapsed ? t('展开所有轮次') : t('收起所有轮次');
      for (const {header, rows} of cards) { rows.hidden = allCollapsed; header.setAttribute('aria-expanded', String(!allCollapsed)); }
    };
    render();
    return review;
  }
  const api = Object.freeze({build, mount, formatDuration});
  if (typeof module !== 'undefined' && module.exports) module.exports = api;
  if (typeof window !== 'undefined') window.AideSessionReview = api;
})();
