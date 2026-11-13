/* Reuse session plans, goal reviews and indexed project relations. */
(() => {
  const drafts = new Map();
  const el = (tag, cls, text) => { const n = document.createElement(tag); if (cls) n.className = cls; if (text != null) n.textContent = text; return n; };
  const classify = { must_change: '必须修改', review: '需要复核', unaffected: '暂不受影响' };
  const states = { pending: '待处理', done: '已处理', blocked: '受阻' };
  function toolPicker(parent, choices, initial, t, remember = () => {}) {
    const selected = new Set(initial || []), picker = el('div', 'project-tool-picker'), search = el('input'), list = el('div', 'project-choice-list');
    search.type = 'search'; search.placeholder = t('搜索插件或功能'); search.setAttribute('aria-label', t('搜索插件或功能')); const notice = el('p', 'project-error'); notice.setAttribute('role', 'status'); picker.append(search, list, notice); parent.append(picker);
    function draw() {
      list.replaceChildren(); const query = search.value.trim().toLowerCase(), available = new Set(choices.map(c => c.name));
      for (const name of selected) if (!available.has(name)) { const label = el('label', 'project-replace'), check = el('input'); check.type = 'checkbox'; check.value = name; check.checked = true; check.onchange = () => { selected.delete(name); remember([...selected]); draw(); }; label.append(check, el('span', 'project-error', t('已选工具暂不可用：{0}。可启用对应插件，或取消选择后保存。', name))); list.append(label); }
      const visible = choices.filter(c => !query || (c.label + ' ' + c.description).toLowerCase().includes(query));
      for (const choice of visible.slice(0, 25)) { const label = el('label', 'project-replace'), check = el('input'); check.type = 'checkbox'; check.value = choice.name; check.checked = selected.has(choice.name); check.onchange = () => { if (check.checked && selected.size >= 8) { check.checked = false; notice.textContent = t('每阶段最多选择 8 项工具，请先取消不需要的工具。'); return; } notice.textContent = ''; if (check.checked) selected.add(choice.name); else selected.delete(choice.name); remember([...selected]); }; const text = el('span', '', choice.label); text.title = choice.description; label.append(check, text); list.append(label); }
      if (!visible.length) list.append(el('p', 'muted', t('没有匹配的插件工具。可以换个关键词，或先安装并启用需要的插件。')));
      if (visible.length > 25) list.append(el('p', 'muted', t('结果较多，请输入插件名称或功能缩小范围。')));
    }
    search.oninput = draw; draw(); return () => [...selected];
  }
  function focusStage(id) {
    const row = document.querySelector(`[data-plan-id="${CSS.escape(id)}"]`); if (!row) return;
    const goal = row.querySelector('.project-goal'); if (goal) goal.open = true;
    row.scrollIntoView({ block: 'start', behavior: 'auto' }); const summary = goal?.querySelector('summary'); summary?.focus();
  }

  function panel(host, { api, sessionId, workspaceId, t, valid, changed }) {
    const scope = JSON.stringify([workspaceId, sessionId]);
    if (!drafts.has(scope)) drafts.set(scope, {});
    const draft = drafts.get(scope);
    function details(label, cls, loader) {
      const box = el('details', cls); const body = el('div', 'project-record-body'); box.append(el('summary', '', t(label)), body); host.append(box);
      let busy = false;
      const load = async () => { if (busy || !valid()) return; busy = true; try { const v = await api(loader.path); if (valid() && box.isConnected) loader.render(body, v, load); } catch (e) { body.replaceChildren(el('p', 'project-error', e.message)); } finally { busy = false; } };
      box.addEventListener('toggle', () => { if (box.open) void load(); });
    }
    function field(parent, label, name, key, fallback = '', rows = 2, max = 2000) {
      const l = el('label', 'project-field', t(label)), n = el('textarea'); n.name = name; n.rows = rows; n.maxLength = max; n.value = draft[key] ?? fallback; n.addEventListener('input', () => { draft[key] = n.value; }); l.append(n); parent.append(l); return n;
    }
    function button(label, action) { const n = el('button', 'quiet', t(label)); n.type = 'button'; n.onclick = action; return n; }
    function select(parent, label, name, values, key, fallback) {
      const l = el('label', 'project-field', t(label)), n = el('select'); n.name = name;
      for (const [value, label] of Object.entries(values)) { const o = el('option', '', t(label)); o.value = value; n.append(o); }
      n.value = draft[key] ?? fallback; n.onchange = () => { draft[key] = n.value; }; l.append(n); parent.append(l); return n;
    }
    function download(data) { const url = URL.createObjectURL(new Blob([JSON.stringify(data, null, 2)], { type: 'application/json' })), a = el('a'); a.href = url; a.download = 'aide-change-impact.json'; document.body.append(a); a.click(); setTimeout(() => { a.remove(); URL.revokeObjectURL(url); }, 1000); }
    details('需求变更影响分析', 'project-impact', {
      path: `/sessions/${sessionId}/impacts`,
      render(body, view, load) {
        body.replaceChildren();
        if (draft.impactNotice) { const status = el('p', '', t('已找到 {0} 个相关文件。接下来逐项检查分类，填写处理依据。', draft.impactNotice)); status.setAttribute('role', 'status'); body.append(status); }
        body.append(el('p', 'muted', t('说清楚这次改动，再勾选直接改动的文件。系统会列出相关文件，供你逐项检查。')));
        const bar = el('div', 'plan-io'); bar.append(button('刷新', load), button('导出影响清单', () => download({ schema: 'aide.change-impact/v1', ...view }))); body.append(bar);
        const help = el('details', 'project-help'); help.append(el('summary', '', t('第一次使用：怎样判断影响？')), el('p', '', t('先说明改了什么，再勾选直接改动的文件。系统会找相关文件供你检查。确认要改的选“必须修改”；还拿不准的选“需要复核”；检查后确认无关的才选“暂不受影响”。处理完成后写下实际依据。'))); body.append(help);
        const form = el('form', 'project-impact-add'); const change = field(form, '这次准备改什么？', 'change', 'impactChange'); change.placeholder = t('例如：设计压力从 10 调整为 12 MPa，需要同步核对计算和报告。');
        const picker = el('div', 'project-seed-picker'); picker.append(el('strong', '', t('选择直接改动的文件'))); const search = el('input'); search.type = 'search'; search.placeholder = t('搜索项目文件'); search.setAttribute('aria-label', t('搜索项目文件')); const choices = el('div', 'project-choice-list'); picker.append(search, choices); form.append(picker);
        const advanced = el('details', 'project-path-advanced'); advanced.append(el('summary', '', t('找不到文件？刷新列表，或手动填写路径'))); const paths = field(advanced, '起点文件（每行一个工作区相对路径）', 'paths', 'impactPaths', '', 2, 8200); form.append(advanced);
        function drawFiles() {
          const selected = new Set(paths.value.split(/\r?\n/u).map(v => v.trim()).filter(Boolean)), query = search.value.trim().toLowerCase(); choices.replaceChildren();
          const files = (view.seedFiles || []).filter(p => !query || p.toLowerCase().includes(query));
          for (const path of files.slice(0, 40)) { const label = el('label', 'project-replace'), check = el('input'); check.type = 'checkbox'; check.value = path; check.checked = selected.has(path); check.onchange = () => { if (check.checked) selected.add(path); else selected.delete(path); paths.value = [...selected].join('\n'); draft.impactPaths = paths.value; }; label.append(check, el('span', '', path)); choices.append(label); }
          if (!files.length) choices.append(el('p', 'muted', t('还没找到文件。先刷新列表；索引未覆盖的文件可手动填写，但需要先完成索引。')));
          if (files.length > 40 || view.seedFilesLimited) choices.append(el('p', 'muted', t('列表只显示部分文件，请输入名称筛选，或手动填写路径。')));
        }
        search.oninput = drawFiles; paths.addEventListener('input', drawFiles); drawFiles();
        const save = el('button', 'primary', t('分析并保存影响清单')); save.type = 'submit'; const error = el('p', 'project-error'); error.setAttribute('role', 'alert'); form.append(save, error); body.append(form);
        async function mutate(data, control, error) {
          control.disabled = true; error.textContent = '';
          try { const next = await api(`/sessions/${sessionId}/impacts`, { method: 'POST', body: JSON.stringify({ ...data, workspaceId: view.workspaceId, snapshot: view.snapshot }) }); if (valid() && body.isConnected) { if (data.action === 'analyze') { draft.openImpactId = next.analyses.at(-1).analysis.id; draft.impactNotice = next.analyses.at(-1).analysis.items.length; } this.render(body, next, load); } }
          catch (e) { error.textContent = e.message; }
          finally { control.disabled = false; }
        }
        const send = mutate.bind(this);
        form.onsubmit = event => { event.preventDefault(); void send({ action: 'analyze', change: change.value, paths: paths.value.split(/\r?\n/u).map(v => v.trim()).filter(Boolean) }, save, error); };
        for (const row of [...view.analyses].reverse()) {
          const v = row.analysis, box = el('details', 'project-impact-analysis'); box.dataset.impactId = v.id;
          box.open = draft.openImpactId === v.id; box.addEventListener('toggle', () => { if (box.open) draft.openImpactId = v.id; else if (draft.openImpactId === v.id) draft.openImpactId = ''; });
          box.append(el('summary', '', v.change + ' · ' + t(row.stale ? '索引已变化，须重新分析' : row.complete ? '已处理' : '待处理')));
          if (row.stale || v.limited) box.append(el('p', 'project-error', t(row.stale ? '索引已变化，须重新分析' : '索引或关系遍历未覆盖全部项目，请补充人工复核。')));
          const limits = el('details', 'project-help'); limits.append(el('summary', '', t('这份清单覆盖到哪里？'))); for (const warning of v.warnings || []) limits.append(el('p', 'project-source', t(warning))); box.append(limits);
          if (row.stale) { const retryError = el('p', 'project-error'); retryError.setAttribute('role', 'alert'); const retry = button('按这次变更重新分析', () => send({ action: 'analyze', change: v.change, paths: v.seeds }, retry, retryError)); box.append(retry, retryError); }
          for (const item of v.items) {
            const record = el('section', 'project-criterion'); record.dataset.impactItem = item.id;
            record.append(el('strong', '', (item.root !== 'workspace' && item.sourceName ? item.sourceName + ': ' : '') + item.path));
            if (item.trail?.length) record.append(el('p', 'project-source', item.trail.join(' — ')));
            if (item.history?.length) { const history = el('details'); history.append(el('summary', '', t('影响复核历史'))); for (const old of item.history) history.append(el('p', 'project-source', old.updated + ' · ' + t(classify[old.classification]) + ' · ' + t(states[old.state]) + ' · ' + old.note)); record.append(history); }
            const relationBox = el('details'); relationBox.append(el('summary', '', t('为什么关联这个文件？')));
            for (const relation of item.relations || []) relationBox.append(el('p', 'project-source', relation.fromPath + ' → ' + relation.toPath + (relation.line ? ' · ' + t('第 {0} 行', relation.line) : '' ) + (relation.evidence ? ' · ' + relation.evidence.replace(/^显式引用：/u, t('显式引用：')) : '')));
            const raw = el('details'); raw.append(el('summary', '', t('查看索引标识（核对用）'))); for (const edge of item.evidence || []) raw.append(el('p', 'project-source', `${edge.from} → ${edge.to} · ${edge.kind}`)); relationBox.append(raw); if (item.evidence?.length) record.append(relationBox);
            const f = el('form', 'project-impact-review');
            const classification = select(f, '影响分类', 'classification', classify, item.id + '-classification', item.classification);
            const state = select(f, '处理状态', 'state', states, item.id + '-state', item.state);
            const note = field(f, '实际观察与依据', 'note', item.id + '-note', item.note);
            const submit = el('button', 'quiet', t('保存影响复核')); submit.type = 'submit'; submit.disabled = row.stale;
            const err = el('p', 'project-error'); err.setAttribute('role', 'alert'); f.append(submit, err);
            f.onsubmit = event => { event.preventDefault(); void send({ action: 'review', id: v.id, itemId: item.id, classification: classification.value, state: state.value, note: note.value }, submit, err); };
            record.append(f); box.append(record);
          }
          body.append(box);
        }
      }
    });
    details('可复用业务流程', 'project-business-flow', {
      path: `/sessions/${sessionId}/plan/flows?language=${window.aideI18n?.language?.() || 'zh-CN'}`,
      render(body, view, load) {
        body.replaceChildren(); body.append(el('p', 'muted', t('选一个场景，写清目标，就能建立五个阶段。先开始第一步，核对成果并逐项验收，再继续下一步。卡住时保留已完成成果，只处理当前缺口。')));
        body.append(button('刷新', load));
        const form = el('form', 'project-flow-add');
        const recipe = select(form, '业务场景', 'recipe', Object.fromEntries(view.recipes.map(v => [v.id, v.name])), 'recipe', view.recipes[0].id);
        const brief = field(form, '业务目标与范围', 'brief', 'flowBrief', '', 3, 1400);
        const stages = el('div', 'project-flow-stages'); form.append(stages);
        let selections = {};
        function drawStages() {
          stages.replaceChildren(); selections = {}; const selected = view.recipes.find(r => r.id === recipe.value);
          for (const step of selected.steps) {
            const row = el('details', 'project-criterion'); row.append(el('summary', '', t(step.title) + ' · ' + t(step.deliverable)));
            for (const check of step.checks) row.append(el('p', 'muted', t(check)));
            const advanced = el('details', 'project-stage-plugins'); advanced.append(el('summary', '', t('需要专业插件时再配置（可选）')), el('p', 'muted', t('不选也能开始。选用的专业工具必须实际执行并留下记录，才能完成本阶段验收。'))); row.append(advanced);
            const key = 'tools-' + selected.id + '-' + step.id;
            selections[step.id] = toolPicker(advanced, view.toolChoices || view.tools.map(name => ({ name, label: name, description: '' })), draft[key], t, values => { draft[key] = values; }); stages.append(row);
          }
        }
        recipe.addEventListener('change', drawStages); drawStages();
        const create = el('button', 'primary', t('建立分阶段目标')); create.type = 'submit'; const error = el('p', 'project-error'); error.setAttribute('role', 'alert'); form.append(create, error); body.append(form);
        form.onsubmit = async event => {
          event.preventDefault(); create.disabled = true; error.textContent = '';
          try { const stageTools = Object.fromEntries(Object.entries(selections).map(([id, read]) => [id, read()])); const created = await api(`/sessions/${sessionId}/plan/flows`, { method: 'POST', body: JSON.stringify({ workspaceId: view.workspaceId, snapshot: view.snapshot, language: view.language, recipe: recipe.value, brief: brief.value, stageTools }) }); if (valid()) { await changed(); focusStage(created.items[0].id); } }
          catch (e) { error.textContent = e.message; }
          finally { create.disabled = false; }
        };
      }
    });
  }
  function stageTools(host, { api, sessionId, item, t, valid, changed }) {
    const box = el('details', 'project-flow-tools'); box.append(el('summary', '', t('配置阶段插件工具'))); const body = el('div', 'project-record-body'); box.append(body); host.append(box);
    let loading = false;
    box.addEventListener('toggle', async () => {
      if (!box.open || loading || !valid()) return; loading = true;
      try {
        const view = await api(`/sessions/${sessionId}/plan/flows`); if (!valid() || !box.isConnected) return;
        body.replaceChildren(el('p', 'muted', t('修改工具后须重新执行当前阶段并验收，后续旧签收也会失效。')));
        const readTools = toolPicker(body, view.toolChoices || view.tools.map(name => ({ name, label: name, description: '' })), view.stages?.[item.id]?.tools || Object.keys(item.flow.tools || {}), t);
        const save = el('button', 'quiet', t('保存阶段工具')), error = el('p', 'project-error'); error.setAttribute('role', 'alert'); body.append(save, error);
        save.onclick = async () => { save.disabled = true; try { await api(`/sessions/${sessionId}/plan/flows`, { method: 'POST', body: JSON.stringify({ action: 'configure', workspaceId: view.workspaceId, snapshot: view.snapshot, itemId: item.id, tools: readTools() }) }); if (valid()) await changed(); } catch (e) { error.textContent = e.message; } finally { save.disabled = false; } };
      } catch (e) { body.replaceChildren(el('p', 'project-error', e.message)); }
      finally { loading = false; }
    });
  }
  function navigator(host, { plan, runs, t, api, sessionId, valid, changed, launchTask, selectRun }) {
    const groups = new Map(); for (const item of plan) if (item.flow) { if (!groups.has(item.flow.id)) groups.set(item.flow.id, []); groups.get(item.flow.id).push(item); }
    if (!groups.size) return;
    const accepted = item => item.status === 'completed' || item.status === 'closed' && item.contract?.criteria?.every(c => c.review?.state === 'passed' && c.review.runId === item.runId) && !runs.find(r => r.id === item.runId)?.goalInputsStale;
    const labels = { pending: '待执行', in_progress: '进行中', awaiting_acceptance: '待验收', blocked: '受阻', failed: '失败', closed: '已关闭', completed: '已完成' };
    for (const items of groups.values()) {
      const box = el('section', 'project-flow-guide'), done = items.filter(accepted).length, current = items.find(i => !accepted(i));
      box.append(el('h4', '', items[0].title.split(' · ')[0]), el('p', 'project-source', t('已验收 {0}/{1} 个阶段', done, items.length)));
      const steps = el('ol'); for (const item of items) { const li = el('li'), link = el('button', 'plan-link', item.title + ' · ' + t(labels[item.status] || '待执行')); link.type = 'button'; link.onclick = () => focusStage(item.id); if (item === current) li.setAttribute('aria-current', 'step'); li.append(link); steps.append(li); } box.append(steps);
      if (current) {
        const run = runs.find(r => r.id === current.runId), dependencies = (current.dependsOn || []).map(id => plan.find(p => p.id === id)).filter(Boolean), ready = dependencies.every(accepted);
        const hint = current.status === 'awaiting_acceptance' ? '下一步：核对实际成果，再逐项记录验收。' : run?.status === 'awaiting_approval' ? '下一步：先处理本阶段待审批的文件。' : ['paused', 'interrupted'].includes(run?.status) ? '下一步：查看原执行记录，核查后从暂停处继续。' : current.status === 'in_progress' ? '当前阶段正在执行，可查看进度或插话。' : !ready ? '请先完成并验收前置阶段。' : '下一步：开始当前阶段。完成后核对成果，再逐项验收。';
        box.append(el('p', '', t(hint)));
        const action = el('button', 'primary', t(current.status === 'awaiting_acceptance' ? '查看并验收本阶段' : current.status === 'closed' ? '重新打开本阶段' : run && ['running', 'paused', 'interrupted', 'awaiting_approval'].includes(run.status) ? '查看本阶段执行' : ready ? '开始本阶段' : '查看前置阶段'));
        const error = el('p', 'project-error'); error.setAttribute('role', 'alert');
        action.type = 'button'; action.onclick = async () => { if (!valid()) return; action.disabled = true; error.textContent = ''; try { if (!ready) { focusStage(dependencies.find(d => !accepted(d))?.id || current.id); return; } if (current.status === 'closed') { await api(`/sessions/${sessionId}/plan/${current.id}`, { method: 'PATCH', body: JSON.stringify({ status: 'pending' }) }); await changed(); focusStage(current.id); } else if (current.status === 'awaiting_acceptance') focusStage(current.id); else if (run && ['running', 'paused', 'interrupted', 'awaiting_approval'].includes(run.status) && selectRun) selectRun(run.id); else if (launchTask) await launchTask(current); } catch (e) { error.textContent = e.message; } finally { action.disabled = false; } }; box.append(action, error);
      } else box.append(el('p', '', t('所有阶段已验收。交付或发布是否成功，请以实际文件和外部回执为准。')));
      host.append(box);
    }
  }
  window.AideProjectWorkflows = { panel, stageTools, navigator };

})();
