(() => {
  'use strict';

  const milliseconds = value => {
    const time = Date.parse(value || '');
    return Number.isFinite(time) ? time : null;
  };
  const formatTime = value => value == null ? '' : new Date(value).toLocaleString();
  const formatDuration = value => {
    if (!Number.isFinite(value) || value < 0) return '';
    if (value < 1000) return `${Math.round(value)} ms`;
    if (value < 60000) return `${(value / 1000).toFixed(1)} s`;
    return `${Math.floor(value / 60000)} min ${Math.round(value % 60000 / 1000)} s`;
  };
  function eventOf(record, index, source) {
    const start = milliseconds(source === 'snapshot' ? record.at : record.startedAt);
    const end = milliseconds(record.completedAt);
    const elapsed = Number(record.elapsedMillis);
    return {
      id: `${source}-${index}`, source,
      kind: source === 'snapshot' ? 'model_request' : String(record.kind || 'other'),
      stage: source === 'snapshot' ? String(record.purpose || 'other') : String(record.stage || 'other'),
      outcome: String(record.outcome || 'unknown'),
      start, end,
      elapsed: Number.isFinite(elapsed) && elapsed >= 0 ? elapsed : (start != null && end != null ? Math.max(0, end - start) : null),
    };
  }
  function build(session) {
    const runs = Array.isArray(session?.runs) ? session.runs.filter(Boolean) : [];
    const total = Number.isInteger(session?.runsTotal) ? session.runsTotal : runs.length;
    const turns = runs.map((run, index) => {
      const ledger = Array.isArray(run.orchestrationEvents) ? run.orchestrationEvents : [];
      // Request snapshots are a timing fallback for older runs. Never expose
      // their bodies, prompts, messages or tool arguments in this review.
      const records = ledger.length ? ledger.map((event, i) => eventOf(event, i, 'ledger'))
        : (Array.isArray(run.requestSnapshots) ? run.requestSnapshots : []).map((snapshot, i) => eventOf(snapshot, i, 'snapshot'));
      records.sort((a, b) => (a.start ?? Infinity) - (b.start ?? Infinity));
      const started = milliseconds(run.created);
      const times = [started, ...records.map(event => event.start)].filter(time => time != null);
      const ends = records.flatMap(event => [event.start, event.end]).filter(time => time != null);
      return {
        id: String(run.id || `turn-${index}`), number: total - runs.length + index + 1,
        status: String(run.status || 'unknown'), model: String(run.model || ''),
        workspaceMode: String(run.workspaceMode || ''),
        started: times.length ? Math.min(...times) : null,
        lastEvent: ends.length ? Math.max(...ends) : null,
        events: records, dropped: Number(run.orchestrationDropped) || 0,
        toolCount: Array.isArray(run.toolUses) ? run.toolUses.length : 0,
        usage: run.usage || {},
      };
    });
    const starts = turns.map(turn => turn.started).filter(time => time != null);
    const ends = turns.flatMap(turn => [turn.started, turn.lastEvent]).filter(time => time != null);
    const first = starts.length ? Math.min(...starts) : null;
    const last = ends.length ? Math.max(...ends) : (starts.length ? Math.max(...starts) : null);
    const latest = turns.at(-1);
    return {
      id: String(session?.id || ''), title: String(session?.title || ''),
      status: turns.some(turn => turn.status === 'running') ? 'running' : latest?.status || (session?.kind === 'assistant' ? 'chat' : 'empty'),
      model: latest?.model || '', workspaceMode: latest?.workspaceMode || '',
      created: milliseconds(session?.created), updated: milliseconds(session?.updated),
      total, loaded: turns.length, turns, first, last,
      toolsLoaded: turns.reduce((sum, turn) => sum + turn.toolCount, 0),
      usage: session?.sessionUsage || turns.reduce((sum, turn) => ({
        prompt: sum.prompt + (Number(turn.usage.prompt) || 0),
        completion: sum.completion + (Number(turn.usage.completion) || 0),
        total: sum.total + (Number(turn.usage.total) || 0),
      }), {prompt:0, completion:0, total:0}),
      hasOlder: total > turns.length,
    };
  }

  function mount(host, session, {t = value => value, loadOlder} = {}) {
    const review = build(session);
    let selected = review.turns.at(-1) || null;
    let selectedEvent = null;
    let zoom = 1;
    const element = (tag, className, label) => {
      const node = document.createElement(tag);
      if (className) node.className = className;
      if (label != null) node.textContent = label;
      return node;
    };
    const statusLabel = status => ({running:t('运行中'), completed:t('已完成'), failed:t('失败'), paused:t('已暂停'), cancelled:t('已取消'), resumed:t('已续跑'), interrupted:t('已中断'), awaiting_approval:t('等待审批'), awaiting_clarification:t('等待澄清'), chat:t('仅对话'), empty:t('无任务'), unknown:t('未记录')})[status] || status || t('未记录');
    const eventLabel = kind => ({model_request:t('模型请求'), tool_call:t('工具调用'), step:t('工作阶段'), queue_wait:t('队列等待'), worker_run:t('并行工作'), snapshot_copy:t('快照复制'), collect:t('结果收集'), context_fit:t('上下文整理'), clarification_wait:t('等待澄清')})[kind] || kind;
    const field = (label, value) => {
      const box = element('div', 'sr-field');
      box.append(element('span', 'sr-field-label', label), element('strong', '', value == null || value === '' ? t('未记录') : String(value)));
      return box;
    };
    const heading = element('div', 'sr-heading');
    const title = element('div', 'sr-heading-title');
    title.append(element('span', 'sr-eyebrow', t('会话状态评审')), element('h3', '', review.title || t('未命名会话')));
    heading.append(title, element('span', `sr-status sr-status-${review.status}`, statusLabel(review.status)));
    const overview = element('div', 'sr-overview');
    overview.append(
      field(t('会话 ID'), review.id), field(t('模型'), review.model),
      field(t('执行环境'), review.workspaceMode === 'ssh' ? 'SSH' : review.workspaceMode === 'local' ? t('本地') : t('未记录')),
      field(t('环境状态'), t('未记录')),
      field(t('开始时间'), formatTime(review.created)), field(t('最近活动'), formatTime(review.updated)),
      field(t('任务轮次'), review.total), field(t('已载入工具调用'), review.toolsLoaded),
      field(t('Token 用量'), Number(review.usage.total || 0).toLocaleString()),
    );
    const note = element('p', 'sr-note', t('时间轴仅使用 Aide 已保存的编排耗时记录；记录跨度不等于任务总耗时。缺失的事件和环境状态不会推测。'));
    const section = element('div', 'sr-section');
    const bar = element('div', 'sr-bar');
    bar.append(element('h3', '', t('任务时间轴')));
    const controls = element('div', 'sr-controls');
    const fit = element('button', '', t('适合宽度'));
    const minus = element('button', '', '−');
    const plus = element('button', '', '+');
    for (const button of [fit, minus, plus]) button.type = 'button';
    minus.setAttribute('aria-label', t('缩小时间轴'));
    plus.setAttribute('aria-label', t('放大时间轴'));
    controls.append(fit, minus, plus); bar.append(controls);
    const viewport = element('div', 'sr-viewport');
    const track = element('div', 'sr-track'); viewport.append(track);
    const body = element('div', 'sr-body');
    const turnsList = element('div', 'sr-turns');
    const details = element('div', 'sr-details');
    body.append(turnsList, details);
    section.append(bar, viewport, body);
    host.replaceChildren(heading, overview, note, section);

    const span = Math.max(1, (review.last ?? 0) - (review.first ?? 0));
    const startPercent = time => review.first == null || time == null ? 0 : Math.max(0, Math.min(100, (time - review.first) / span * 100));
    function drawTrack() {
      track.replaceChildren();
      track.style.width = `${zoom * 100}%`;
      if (review.first == null) {
        track.append(element('p', 'sr-empty', t('没有已记录的时间点')));
        return;
      }
      for (const turn of review.turns) {
        if (turn.started == null) continue;
        const lane = element('button', `sr-lane${selected?.id === turn.id ? ' selected' : ''}`);
        lane.type = 'button';
        lane.title = `${t('第 {0} 轮', turn.number)} · ${statusLabel(turn.status)}`;
        lane.setAttribute('aria-label', lane.title);
        const segment = element('span', `sr-segment sr-segment-${turn.status}`);
        segment.style.left = `${startPercent(turn.started)}%`;
        segment.style.width = `${Math.max(0.8, (Math.max(turn.started, turn.lastEvent ?? turn.started) - turn.started) / span * 100)}%`;
        lane.append(segment);
        for (const event of turn.events) {
          if (event.start == null) continue;
          const marker = element('span', `sr-event-segment sr-event-segment-${event.kind}`);
          marker.style.left = `${startPercent(event.start)}%`;
          marker.style.width = `${Math.max(0.45, (Math.max(0, (event.end ?? event.start) - event.start)) / span * 100)}%`;
          marker.title = `${eventLabel(event.kind)} · ${event.stage} · ${formatDuration(event.elapsed) || t('未记录')}`;
          marker.onclick = click => { click.stopPropagation(); selected = turn; selectedEvent = event; redraw(); };
          lane.append(marker);
        }
        lane.onclick = () => { selected = turn; selectedEvent = null; redraw(); };
        track.append(lane);
      }
      const scale = element('div', 'sr-scale');
      scale.append(element('span', '', formatTime(review.first)), element('span', '', formatTime(review.last)));
      track.append(scale);
    }
    function drawTurns() {
      turnsList.replaceChildren();
      for (const turn of review.turns) {
        const button = element('button', `sr-turn${selected?.id === turn.id ? ' selected' : ''}`);
        button.type = 'button';
        button.setAttribute('aria-pressed', String(selected?.id === turn.id));
        const main = element('span', 'sr-turn-main', t('第 {0} 轮', turn.number));
        main.append(element('small', '', statusLabel(turn.status)));
        button.append(main, element('span', 'sr-turn-duration', turn.lastEvent != null && turn.started != null ? formatDuration(turn.lastEvent - turn.started) : '—'));
        button.onclick = () => { selected = turn; selectedEvent = null; redraw(); };
        turnsList.append(button);
      }
      if (review.hasOlder && loadOlder) {
        const older = element('button', 'sr-older', t('加载更早的任务'));
        older.type = 'button'; older.onclick = loadOlder; turnsList.append(older);
      }
    }
    function drawDetails() {
      details.replaceChildren();
      if (!selected) { details.append(element('p', 'sr-empty', t('当前会话没有任务轮次'))); return; }
      const top = element('div', 'sr-detail-head');
      top.append(element('h3', '', t('第 {0} 轮 · {1}', selected.number, statusLabel(selected.status))), element('span', '', selected.model || t('模型未记录')));
      details.append(top);
      const timing = element('div', 'sr-timing');
      timing.append(field(t('开始'), formatTime(selected.started)), field(t('最后记录'), formatTime(selected.lastEvent)), field(t('工具调用'), selected.toolCount));
      details.append(timing);
      if (selected.dropped) details.append(element('p', 'sr-note', t('早期 {0} 条计时记录已被有界日志淘汰。', selected.dropped)));
      if (!selected.events.length) { details.append(element('p', 'sr-empty', t('本轮没有可用的计时事件；旧会话可能只保存了任务状态。'))); return; }
      const events = element('div', 'sr-events');
      for (const event of selected.events) {
        const button = element('button', `sr-event${selectedEvent?.id === event.id ? ' selected' : ''}`);
        button.type = 'button'; button.setAttribute('aria-pressed', String(selectedEvent?.id === event.id));
        button.append(element('span', 'sr-event-name', `${eventLabel(event.kind)} · ${event.stage}`), element('span', 'sr-event-duration', formatDuration(event.elapsed) || '—'));
        button.onclick = () => { selectedEvent = event; drawDetails(); };
        events.append(button);
      }
      details.append(events);
      if (selectedEvent) {
        const info = element('div', 'sr-event-info');
        info.append(
          field(t('事件类型'), eventLabel(selectedEvent.kind)), field(t('阶段'), selectedEvent.stage),
          field(t('结果'), selectedEvent.outcome), field(t('开始'), formatTime(selectedEvent.start)),
          field(t('结束'), formatTime(selectedEvent.end)), field(t('耗时'), formatDuration(selectedEvent.elapsed)),
          field(t('记录来源'), selectedEvent.source === 'ledger' ? t('编排计时') : t('请求快照计时')),
        );
        details.append(info);
      }
    }
    function redraw() { drawTrack(); drawTurns(); drawDetails(); }
    fit.onclick = () => { zoom = 1; drawTrack(); viewport.scrollLeft = 0; };
    minus.onclick = () => { zoom = Math.max(1, zoom / 1.5); drawTrack(); };
    plus.onclick = () => { zoom = Math.min(8, zoom * 1.5); drawTrack(); };
    redraw();
    return review;
  }
  const api = Object.freeze({build, mount, formatDuration});
  if (typeof module !== 'undefined' && module.exports) module.exports = api;
  if (typeof window !== 'undefined') window.AideSessionReview = api;
})();
