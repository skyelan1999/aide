// 任务与规划：会话级计划面板。数据源是 session.plan：用户用自然语言添加的
// 任务，以及 aide 通过 update_plan 拆解并被后端镜像进来的步骤。面板同时承担
// 编排（新增、关闭、依赖、导入导出）与过程留痕（审批记录、所属 run）。
(function () {
  let generation = 0;
  let active = null;

  const node = (tag, cls, text) => {
    const n = document.createElement(tag);
    if (cls) n.className = cls;
    if (text !== undefined && text !== null) n.textContent = text;
    return n;
  };

  const ICON = { awaiting_acceptance: '◌', completed: '✓', failed: '✗', blocked: '✗', in_progress: '●', pending: '○', closed: '—' };
  const LABEL = { awaiting_acceptance: '待验收', completed: '已完成', failed: '失败', blocked: '受阻', in_progress: '进行中', pending: '待执行', closed: '已关闭' };
  const STATUS_CLASS = { awaiting_acceptance: 'idle', completed: 'ok', failed: 'bad', blocked: 'bad', in_progress: 'live', pending: 'idle', closed: 'closed' };
  const RUN_STATUS = { completed: '已完成', awaiting_approval: '待审批', running: '运行中', failed: '失败', cancelled: '已停止', paused: '已暂停', interrupted: '已中断', awaiting_clarification: '等待回答' };

  function stamp(value) {
    if (!value) return '';
    const d = new Date(String(value).replace(/\.(\d{3})\d+/, '.$1'));
    if (Number.isNaN(d.getTime())) return String(value).slice(0, 16);
    const pad = n => String(n).padStart(2, '0');
    return `${d.getMonth() + 1}/${d.getDate()} ${pad(d.getHours())}:${pad(d.getMinutes())}`;
  }

  function toast(message) {
    if (typeof window.toast === 'function') window.toast(message);
  }

  function sessionOverview(host, ctx) {
    close();
    const gen = ++generation;
    const { session, t = key => key, api, workspaceId, blocked, selectRun, launchTask } = ctx || {};
    let current = session;
    let hostRef = host;
    const panelHost = () => (hostRef.isConnected ? hostRef : (document.querySelector('.traj-outcome-content') || hostRef));
    let pollTimer = null;
    let polling = false;
    let pendingRefresh = false;
    let closed = false;
    let refreshFn = null;
    const valid = () => !closed && gen === generation && panelHost().isConnected && !(blocked && blocked());
    const stop = () => {
      closed = true;
      if (pollTimer !== null) clearInterval(pollTimer);
      pollTimer = null;
      if (active && active.close === stop) active = null;
    };
    active = { close: stop, sessionId: session && session.id, refresh: () => refreshFn && refreshFn() };

    async function reload() {
      if (!api) return;
      try {
        const next = await api(`/sessions/${current.id}?limit=30&_=${Date.now()}`);
        if (!valid() || !next || next.id !== current.id) return;
        current = next;
        draw();
      } catch (e) {
        toast(e && e.message ? e.message : t('刷新失败'));
      }
    }

    function planOf(value) {
      return Array.isArray(value && value.plan) ? value.plan : [];
    }

    function runsOf(value) {
      return Array.isArray(value && value.runs) ? value.runs : [];
    }

    function runFor(item) {
      if (!item || !item.runId) return null;
      return runsOf(current).find(r => r.id === item.runId) || null;
    }

    function approvalList(run) {
      return Array.isArray(run && run.approvalReviews) ? run.approvalReviews : [];
    }

    function itemRow(item, depth) {
      const row = node('div', 'plan-row plan-depth-' + Math.min(depth, 2));
      const head = node('div', 'plan-row-head');
      const status = item.status === 'in_progress' ? 'live' : (STATUS_CLASS[item.status] || 'idle');
      const icon = node('span', 'plan-status plan-status-' + status, ICON[item.status] || '○');
      icon.title = t(LABEL[item.status] || '待执行');
      const title = node('span', 'plan-title', item.title);
      const tags = node('span', 'plan-tags');
      tags.append(node('span', 'plan-tag plan-tag-' + (item.source === 'aide' ? 'aide' : 'user'), item.source === 'aide' ? 'aide' : t('自然语言')));
      if (item.status && item.status !== 'pending' && item.status !== 'in_progress') tags.append(node('span', 'plan-tag plan-tag-state plan-state-' + status, t(LABEL[item.status])));
      head.append(icon, title, tags);
      row.dataset.planId = item.id;

      const actions = node('div', 'plan-actions');
      const run = runFor(item);
      if (run && selectRun) {
        const open = node('button', 'plan-link', t('查看 #{0}', run.number || ''));
        open.type = 'button';
        open.onclick = () => selectRun(run.id);
        actions.append(open);
      }
      if (item.source === 'user' && !['completed', 'closed'].includes(item.status)) {
        const depsReady = (item.dependsOn || []).every(id => {
          const dep = planOf(current).find(x => x.id === id);
          return !dep || dep.status === 'completed' || (dep.status === 'closed' && (!dep.contract || dep.contract.criteria.every(c => c.review?.state === 'passed' && c.review.runId === dep.runId)));
        });
        if (launchTask) {
          const launch = node('button', 'primary plan-launch', t('交给 aide'));
          launch.type = 'button';
          launch.disabled = !depsReady || !!(run && ['running', 'awaiting_approval', 'awaiting_clarification'].includes(run.status));
          if (!depsReady) launch.title = t('依赖的任务尚未完成');
          launch.onclick = () => launchTask(item);
          actions.append(launch);
        }
      }
      if (item.source === 'aide') {
        const dismiss = node('button', 'plan-link', t('关闭本步'));
        dismiss.type = 'button';
        dismiss.onclick = async () => {
          dismiss.disabled = true;
          try {
            await api(`/sessions/${current.id}/plan/dismiss`, { method: 'POST', body: JSON.stringify({ runId: item.runId, step: item.step }) });
            await reload();
          } catch (e) { toast(e && e.message ? e.message : t('操作失败')); dismiss.disabled = false; }
        };
        actions.append(dismiss);
      } else {
        const toggle = node('button', 'plan-link', t(item.status === 'closed' ? '重新打开' : '关闭任务'));
        toggle.type = 'button';
        toggle.onclick = async () => {
          toggle.disabled = true;
          try {
            await api(`/sessions/${current.id}/plan/${item.id}`, { method: 'PATCH', body: JSON.stringify({ status: item.status === 'closed' ? 'pending' : 'closed' }) });
            await reload();
          } catch (e) { toast(e && e.message ? e.message : t('操作失败')); toggle.disabled = false; }
        };
        actions.append(toggle);
      }
      head.append(actions);
      row.append(head);

      if (item.source === 'user' && window.AideProjectRecords) window.AideProjectRecords.goalCard(row, { api, sessionId: current.id, item, t, changed: reload, valid });
      if (item.detail) {
        const detail = node('p', 'plan-detail', item.detail);
        row.append(detail);
      }
      const stateTag = tags.querySelector('.plan-tag-state');
      const meta = node('div', 'plan-meta');
      if (item.created) meta.append(node('span', '', stamp(item.created)));
      if (item.agent) meta.append(node('span', 'plan-agent', t('执行人格：{0}', item.agent)));
      if (run) meta.append(node('span', 'plan-run-status', t('执行：{0}', t(RUN_STATUS[run.status] || run.status || ''))));
      for (const dep of item.dependsOn || []) {
        const target = planOf(current).find(x => x.id === dep);
        meta.append(node('span', 'plan-dep', t('依赖：{0}', target ? target.title : dep)));
      }
      if (meta.childNodes.length) row.append(meta);
      const runStatusNode = meta.querySelector('.plan-run-status');
      rowRefs.set(item.id, { icon, stateTag, tags, runStatus: runStatusNode, item });

      const approvals = approvalList(run);
      if (approvals.length) {
        const box = node('details', 'plan-approvals');
        box.append(node('summary', '', t('审批记录 · {0}', approvals.length)));
        for (const rec of approvals.slice(-6).reverse()) {
          const line = node('div', 'plan-approval-line');
          line.append(node('span', 'plan-approval-status plan-approval-' + (rec.status || ''), t(rec.status || '')));
          line.append(node('span', 'plan-approval-kind', rec.kind || rec.command || ''));
          if (rec.risk) line.append(node('span', 'plan-approval-risk', t('风险：{0}', rec.risk)));
          if (rec.at) line.append(node('time', '', stamp(rec.at)));
          if (rec.reason) line.append(node('p', 'plan-approval-reason', rec.reason));
          box.append(line);
        }
        row.append(box);
      }
      return row;
    }

    const rowRefs = new Map();

    function draw() {
      hostRef = panelHost();
      const openGoals = new Set(Array.from(hostRef.querySelectorAll('details[data-goal-item][open]'), d => d.dataset.goalItem));
      const knowledgeOpen = !!hostRef.querySelector('details[data-project-knowledge][open]');
      const draftFields = new Map(Array.from(hostRef.querySelectorAll('.plan-add textarea'), n => [n.name || 'title', n.value]));
      hostRef.replaceChildren();
      hostRef.oninput = event => { const form = event.target.closest('form'); if (form) form.dataset.dirty = 'true'; };
      hostRef.onchange = event => { const form = event.target.closest('form'); if (form) form.dataset.dirty = 'true'; };
      rowRefs.clear();
      const plan = planOf(current);
      const runs = runsOf(current);
      const intro = node('div', 'outcome-intro plan-intro');
      intro.append(node('h3', '', t('会话任务与规划')));
      intro.append(node('p', 'muted', '#' + (session.number || '?') + ' · ' + (session.title || '')));
      intro.append(node('p', 'muted', t('aide 每轮都会读到这份列表：未完成项、依赖和最近审批记录都会进入上下文，避免遗漏。')));
      hostRef.append(intro);

      const open = plan.filter(i => i.status !== 'closed');
      const totals = {
        total: open.length,
        done: plan.filter(i => i.status === 'completed').length,
        live: plan.filter(i => i.status === 'in_progress').length,
        failed: plan.filter(i => i.status === 'failed' || i.status === 'blocked').length,
      };
      const metrics = node('div', 'outcome-metrics plan-metrics');
      for (const [key, label] of [['total', '待办任务'], ['done', '已完成'], ['live', '进行中'], ['failed', '失败/受阻']]) {
        const cell = node('div', 'plan-metric plan-metric-' + key);
        cell.append(node('strong', '', String(totals[key])), node('span', '', t(label)));
        metrics.append(cell);
      }
      hostRef.append(metrics);

      const toolbar = node('div', 'plan-toolbar');
      const addForm = node('form', 'plan-add');
      const input = node('textarea', 'plan-add-input');
      input.rows = 2;
      input.name = 'title';
      input.setAttribute('aria-label', t('任务目标'));
      input.placeholder = t('用自然语言描述一个任务，交给 aide 拆解执行…');
      const submit = node('button', 'primary', t('添加任务'));
      submit.type = 'submit';
      addForm.append(input, submit);
      const contractBox = node('details', 'plan-contract-fields');
      contractBox.append(node('summary', '', t('交付物与验收条件（可选）')));
      const readContract = window.AideProjectRecords ? window.AideProjectRecords.contractFields(contractBox, null, t) : () => null;
      addForm.append(contractBox);
      addForm.onsubmit = async event => {
        event.preventDefault();
        const liveInput = document.querySelector('.plan-add-input');
        const liveSubmit = document.querySelector('.plan-add > button');
        const text = (liveInput ? liveInput.value : input.value).trim();
        if (!text) return;
        const title = text.split('\n')[0].slice(0, 200);
        if (liveSubmit) liveSubmit.disabled = true;
        try {
          await api(`/sessions/${current.id}/plan`, { method: 'POST', body: JSON.stringify({ title, detail: text, contract: readContract(), workspaceId }) });
          for (const field of addForm.querySelectorAll('textarea')) field.value = '';
          await reload();
        } catch (e) { toast(e && e.message ? e.message : t('添加失败')); }
        finally { if (liveSubmit) liveSubmit.disabled = false; }
      };
      toolbar.append(addForm);

      const io = node('div', 'plan-io');
      const exportBtn = node('button', 'quiet', t('导出 JSON'));
      exportBtn.type = 'button';
      exportBtn.onclick = async () => {
        exportBtn.disabled = true;
        try {
          const doc = await api(`/sessions/${current.id}/plan/export`);
          const blob = new Blob([JSON.stringify(doc, null, 2)], { type: 'application/json' });
          const link = node('a');
          link.href = URL.createObjectURL(blob);
          link.download = `aide-plan-${current.id}.json`;
          document.body.append(link);
          link.click();
          setTimeout(() => { URL.revokeObjectURL(link.href); link.remove(); }, 1000);
        } catch (e) { toast(e && e.message ? e.message : t('导出失败')); }
        finally { exportBtn.disabled = false; }
      };
      const importInput = node('input');
      importInput.type = 'file';
      importInput.accept = 'application/json,.json';
      importInput.hidden = true;
      const importBtn = node('button', 'quiet', t('导入 JSON'));
      importBtn.type = 'button';
      importBtn.onclick = () => importInput.click();
      importInput.onchange = async () => {
        const file = importInput.files && importInput.files[0];
        if (!file) return;
        importBtn.disabled = true;
        try {
          const doc = JSON.parse(await file.text());
          const result = await api(`/sessions/${current.id}/plan/import`, { method: 'POST', body: JSON.stringify(Array.isArray(doc) ? { tasks: doc, destinationWorkspaceId: workspaceId } : { ...doc, destinationWorkspaceId: workspaceId }) });
          toast(t('已导入 {0} 个任务', result && result.imported ? result.imported : '?'));
          await reload();
        } catch (e) { toast(e && e.message ? e.message : t('导入失败：需要 aide.plan/v1 JSON')); }
        finally { importBtn.disabled = false; importInput.value = ''; }
      };
      io.append(importBtn, importInput, exportBtn);
      toolbar.append(io);
      hostRef.append(toolbar);
      if (window.AideProjectRecords) window.AideProjectRecords.knowledgePanel(hostRef, { api, t, valid });

      const board = node('div', 'plan-board');
      const userItems = plan.filter(i => i.source === 'user');
      if (!plan.length) {
        const empty = node('div', 'plan-empty');
        empty.append(node('p', '', t('还没有任务。添加一条自然语言任务，或导入一份 aide.plan/v1 JSON 开始编排。')));
        board.append(empty);
      }
      for (const item of userItems) {
        if (item.status === 'closed') continue;
        board.append(itemRow(item, 0));
        for (const child of plan.filter(i => i.parentId === item.id && i.source === 'aide' && i.status !== 'closed')) {
          board.append(itemRow(child, 1));
        }
      }
      // 没有关联到用户任务的 aide 步骤按 run 归组，保证历史执行也能被复盘。
      const orphan = plan.filter(i => i.source === 'aide' && i.status !== 'closed' && !(i.parentId && plan.some(x => x.id === i.parentId)));
      const byRun = new Map();
      for (const item of orphan) {
        const key = item.runId || 'unknown';
        if (!byRun.has(key)) byRun.set(key, []);
        byRun.get(key).push(item);
      }
      for (const [runId, items] of byRun) {
        const run = runs.find(r => r.id === runId);
        const group = node('section', 'plan-run-group');
        const head = node('header', 'plan-run-head');
        head.append(node('strong', '', t('会话执行 · {0}', run ? '#' + (run.number || '') : t('未关联任务'))));
        if (run) head.append(node('span', 'muted', (run.prompt || '').slice(0, 90)));
        const detail = node('button', 'plan-link', t('查看本轮'));
        detail.type = 'button';
        if (selectRun && runId !== 'unknown') detail.onclick = () => selectRun(runId);
        head.append(detail);
        group.append(head);
        for (const item of items) group.append(itemRow(item, 1));
        board.append(group);
      }
      hostRef.append(board);

      const closedItems = plan.filter(i => i.status === 'closed');
      if (closedItems.length) {
        const folded = node('details', 'plan-closed');
        folded.append(node('summary', '', t('已关闭的任务 · {0}', closedItems.length)));
        for (const item of closedItems) folded.append(itemRow(item, 0));
        hostRef.append(folded);
      }

      for (const d of hostRef.querySelectorAll('details[data-goal-item]')) if (openGoals.has(d.dataset.goalItem)) d.open = true;
      const knowledge = hostRef.querySelector('details[data-project-knowledge]');
      if (knowledge && knowledgeOpen) knowledge.open = true;
      for (const n of hostRef.querySelectorAll('.plan-add textarea')) if (draftFields.has(n.name || 'title')) n.value = draftFields.get(n.name || 'title');
      hostRef.append(node('p', 'outcome-boundary', t('规划只描述意图与进度，不代表文件已落盘；实际结果以工具记录和审批记录为准。导出 JSON 可用于在其它会话中复用这套工作流。')));

      const needsPolling = value => runsOf(value).some(run => run && run.status === 'running');
      function syncPolling() {
        if (api && valid() && needsPolling(current)) {
          if (pollTimer === null) pollTimer = setInterval(poll, 2500);
        } else if (pollTimer !== null) {
          clearInterval(pollTimer);
          pollTimer = null;
        }
      }
      function patchStatuses(plan) {
        for (const item of plan) {
          const ref = rowRefs.get(item.id);
          if (!ref) continue;
          const state = item.status === 'in_progress' ? 'live' : (STATUS_CLASS[item.status] || 'idle');
          ref.icon.textContent = ICON[item.status] || '○';
          ref.icon.title = t(LABEL[item.status] || '待执行');
          ref.icon.className = 'plan-status plan-status-' + state;
          if (item.status && item.status !== 'pending' && item.status !== 'in_progress') {
            if (ref.stateTag) {
              ref.stateTag.textContent = t(LABEL[item.status]);
              ref.stateTag.className = 'plan-tag plan-tag-state plan-state-' + state;
            } else {
              const tag = node('span', 'plan-tag plan-tag-state plan-state-' + state, t(LABEL[item.status]));
              ref.tags.append(tag);
            }
          } else if (ref.stateTag) {
            ref.stateTag.remove();
          }
          const run = (current.runs || []).find(r => r.id === item.runId);
          if (run && ref.runStatus) ref.runStatus.textContent = t('执行：{0}', t(RUN_STATUS[run.status] || run.status || ''));
        }
        const metrics = hostRef.querySelectorAll('.plan-metric strong');
        if (metrics.length >= 4) {
          const open = plan.filter(i => i.status !== 'closed');
          metrics[0].textContent = String(open.length);
          metrics[1].textContent = String(plan.filter(i => i.status === 'completed').length);
          metrics[2].textContent = String(plan.filter(i => i.status === 'in_progress').length);
          metrics[3].textContent = String(plan.filter(i => i.status === 'failed' || i.status === 'blocked').length);
        }
      }
      function poll() {
        if (!valid()) { stop(); return; }
        if (!polling && needsPolling(current)) void refresh();
      }
      async function refresh() {
        if (!valid() || !api) return;
        if (polling) { pendingRefresh = true; return; }
        polling = true;
        try {
          const next = await api(`/sessions/${current.id}?limit=30&_=${Date.now()}`);
          if (!valid() || !next || next.id !== session.id) return;
          const nextRevision = signature(next);
          const structural = planOf(next).map(i => i.id + ':' + i.title + ':' + (i.runId || '') + ':' + (i.parentId || '')).join('|');
          const previous = planOf(current).map(i => i.id + ':' + i.title + ':' + (i.runId || '') + ':' + (i.parentId || '')).join('|');
          current = next;
          syncPolling();
          if (nextRevision !== lastRevision || structural !== previous) {
            const focused = document.activeElement;
            if (hostRef.querySelector('form[data-dirty="true"]') || (focused && hostRef.contains(focused) && focused.matches('textarea,input,select'))) { patchStatuses(planOf(next)); return; }
            lastRevision = nextRevision;
            draw();
          }
        } catch { /* 保留上一次可读的计划，轮询或全局事件会重试 */ }
        finally {
          polling = false;
          if (pendingRefresh && valid()) { pendingRefresh = false; void refresh(); }
        }
      }
      refreshFn = refresh;
      syncPolling();
      void refresh();
    }

    const signature = value => JSON.stringify([
      (value.plan || []).map(i => [i.id, i.title, i.status, i.runId, i.parentId, i.updated]),
      (value.runs || []).map(r => [r.id, r.status, r.approvalState, r.approvalReviews]),
    ]);
    let lastRevision = signature(current);
    draw();
    return { close: stop, ready: reload() };
  }

  function close() {
    if (active) active.close();
    active = null;
    generation++;
  }

  window.AideSessionPlan = { sessionOverview, close };
})();
