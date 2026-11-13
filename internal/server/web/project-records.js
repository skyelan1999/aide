// Extends the existing task board and project memory. User text is rendered as
// text nodes; only explicit operator actions submit acceptance/confirmation.
(function () {
  const drafts = new Map();
  const el = (tag, cls, text) => { const n = document.createElement(tag); if (cls) n.className = cls; if (text != null) n.textContent = text; return n; };
  const lines = value => value.split('\n').map(s => s.trim()).filter(Boolean);
  const states = { pending: '待验收', needs_review: '需要重新复核', passed: '验收通过', failed: '验收未通过', blocked: '验收受阻', proposed: '待确认', confirmed: '已确认', superseded: '已替代', archived: '已归档' };
  const kinds = { fact: '事实', constraint: '约束', decision: '决策', question: '未决问题' };
  function field(form, label, name, value, t, rows = 2) {
    const box = el('label', 'project-field'); box.append(el('span', '', t(label)));
    const input = el('textarea'); input.name = name; input.rows = rows; input.value = value || ''; input.setAttribute('aria-label', t(label)); box.append(input); form.append(box); return input;
  }
  function button(text, t) { const b = el('button', 'quiet', t(text)); b.type = 'button'; return b; }
  function errorBox(host) { const n = el('p', 'project-error'); n.setAttribute('role', 'alert'); host.append(n); return n; }
  function retainDraft(form, key, snapshot, save, t) {
    const fields = () => [...form.querySelectorAll('[name]')];
    const initial = Object.fromEntries(fields().map(n => [n.name, n.value]));
    let stored = drafts.get(key), ready = !stored || stored.snapshot === snapshot;
    if (stored) { for (const n of fields()) if (Object.hasOwn(stored.values, n.name)) n.value = stored.values[n.name]; form.dataset.dirty = 'true'; }
    const remember = () => { form.dataset.dirty = 'true'; drafts.set(key, {snapshot: ready ? snapshot : stored.snapshot, values: Object.fromEntries(fields().map(n => [n.name, n.value]))}); if (drafts.size > 256) drafts.delete(drafts.keys().next().value); };
    form.addEventListener('input', remember); form.addEventListener('change', remember);
    if (!ready) {
      const notice = el('div', 'project-draft-conflict'); notice.setAttribute('role', 'status'); notice.append(el('p', '', t('草稿已保留，但原记录已变化。请核对当前记录后，再使用草稿提交。')));
      const keep = button('已核对当前记录，继续使用草稿', t), discard = button('放弃草稿，使用当前记录', t);
      save.disabled = true;
      const resume = () => { ready = true; save.disabled = false; notice.remove(); };
      keep.onclick = () => { resume(); remember(); };
      discard.onclick = () => { for (const n of fields()) n.value = initial[n.name]; drafts.delete(key); delete form.dataset.dirty; resume(); };
      notice.append(keep, discard); form.append(notice);
    }
    return { ready: () => ready, clear: () => drafts.delete(key) };
  }
  function contractFields(form, contract, t) {
    const deliverables = field(form, '交付物（每行一项）', 'deliverables', (contract?.deliverables || []).join('\n'), t);
    const criteria = field(form, '验收条件（每行一项）', 'criteria', (contract?.criteria || []).map(c => c.text).join('\n'), t, 3);
    deliverables.maxLength = 2010; criteria.maxLength = 10020;
    return () => { const texts = lines(criteria.value); return texts.length || lines(deliverables.value).length ? { deliverables: lines(deliverables.value), criteria: texts.map(text => ({ text })) } : null; };
  }
  function goalCard(host, { api, workspaceId, sessionId, item, t, changed, valid = () => true }) {
    const scope = JSON.stringify([workspaceId || '', sessionId, item.id]);
    const details = el('details', 'project-goal'); details.dataset.goalItem = item.id;
    details.append(el('summary', '', t('目标与验收') + (item.contract ? ` · ${item.contract.criteria.length}` : '')));
    const body = el('div', 'project-record-body'); details.append(body); host.append(details);
    let loading = false;
    async function load() {
      if (loading) return; loading = true;
      try { const view = await api(`/sessions/${sessionId}/plan/${item.id}/acceptance`); if (!valid() || !details.isConnected) return; render(view); }
      catch (e) { body.replaceChildren(); errorBox(body).textContent = e.message; }
      finally { loading = false; }
    }
    function render(view) {
      body.replaceChildren();
      body.append(el('p', 'muted', t(view.accepted ? '目标已逐项验收' : '执行完成后仍须逐项复核；记录实际观察，模型回答不能代替签收。')));
      if (view.inputsStale) body.append(el('p', 'project-error', t('前置成果已变化，请重新执行当前阶段。')));
      if (view.flowInputs?.length) {
        const inputs = el('details'); inputs.append(el('summary', '', t('阶段输入与来源')));
        for (const input of view.flowInputs) {
          inputs.append(el('strong', '', input.title), el('p', '', input.output));
          if (input.truncated) inputs.append(el('p', 'project-error', t('输入有截断，请回读原文件或原执行记录。')));
          for (const file of input.files || []) inputs.append(el('p', 'project-source', file.path));
          const evidence = el('details'); evidence.append(el('summary', '', t('查看原始执行与证据摘要')), el('p', 'project-source', input.runId + ' · ' + input.snapshot));
          for (const file of input.files || []) evidence.append(el('p', 'project-source', (file.source || '') + ':' + file.path + ' · ' + file.digest));
          for (const receipt of input.receipts || []) { evidence.append(el('p', 'project-source', receipt.tool + ' · ' + receipt.digest), el('p', '', receipt.result)); if (receipt.truncated) evidence.append(el('p', 'project-error', t('输入有截断，请回读原文件或原执行记录。'))); }
          inputs.append(evidence);
        }
        body.append(inputs);
      }
      for (const text of view.contract?.deliverables || []) body.append(el('p', 'project-source', text));
      if (view.contract && !view.canReview) body.append(el('p', 'muted', t('先开始执行；执行结束并处理文件审批后，再记录验收。')));
      for (const c of view.criteria || []) {
        const row = el('section', 'project-criterion'); row.dataset.criterionId = c.id;
        row.append(el('strong', '', c.text), el('span', 'plan-tag', t(states[c.state] || '待验收')));
        if (c.review) row.append(el('p', 'project-source', t('用户观察记录') + ' · ' + c.review.recordedAt + ' · ' + c.review.note));
        if (c.history?.length) { const history = el('details'); history.append(el('summary', '', t('历史复核记录'))); for (const r of c.history) history.append(el('p', 'project-source', r.recordedAt + ' · ' + t(states[r.state] || '待验收') + ' · ' + r.note)); row.append(history); }
        if (!view.canReview) { body.append(row); continue; }
        const form = el('form', 'project-review');
        const choice = el('select'); choice.setAttribute('aria-label', t('验收状态'));
        choice.name = 'state';
        for (const state of ['pending', 'passed', 'failed', 'blocked']) { const option = el('option', '', t(states[state])); option.value = state; choice.append(option); }
        if (!view.executionReady) choice.querySelector('option[value="passed"]').disabled = true;
        choice.value = ['passed', 'failed', 'blocked'].includes(c.state) ? c.state : 'pending';
        const receipt = el('select'); receipt.setAttribute('aria-label', t('关联工具证据'));
        receipt.name = 'toolIndex';
        const manual = el('option', '', t('用户实际观察（非独立自动验证）')); manual.value = '0'; receipt.append(manual);
        for (const r of view.receipts || []) { const o = el('option', '', `#${r.index} · ${r.tool}`); o.value = String(r.index); receipt.append(o); }
        if (c.review?.toolIndex) receipt.value = String(c.review.toolIndex);
        form.append(choice, receipt);
        const note = field(form, '实际观察与依据', 'note', c.review?.note, t); note.maxLength = 2000;
        const save = button('保存验收记录', t); save.type = 'submit'; form.append(save); const error = errorBox(form);
        const draft = retainDraft(form, scope + ':review:' + c.id + ':' + (item.runId || ''), view.snapshot, save, t);
        form.onsubmit = async event => {
          event.preventDefault(); if (!draft.ready()) return; save.disabled = true; error.textContent = '';
          try { await api(`/sessions/${sessionId}/plan/${item.id}/acceptance`, { method: 'POST', body: JSON.stringify({ snapshot: view.snapshot, criterionId: c.id, state: choice.value, note: note.value, toolIndex: Number(receipt.value) }) }); draft.clear(); if (valid()) await changed(); }
          catch (e) { error.textContent = e.message; }
          finally { save.disabled = false; }
        };
        row.append(form); body.append(row);
      }
      const edit = el('details', 'project-contract-edit'); edit.append(el('summary', '', t('设置或修改验收条件')));
      const form = el('form'); const read = contractFields(form, view.contract, t);
      const save = button('保存条件并重新验收', t); save.type = 'submit'; form.append(save); const error = errorBox(form);
      const draft = retainDraft(form, scope + ':contract', view.snapshot, save, t);
      form.onsubmit = async event => {
        event.preventDefault(); if (!draft.ready()) return; save.disabled = true; error.textContent = '';
        try { await api(`/sessions/${sessionId}/plan/${item.id}/acceptance`, { method: 'PUT', body: JSON.stringify({ snapshot: view.snapshot, contract: read() }) }); draft.clear(); if (valid()) await changed(); }
        catch (e) { error.textContent = e.message; }
        finally { save.disabled = false; }
      };
      edit.append(form); if (!view.contract) edit.open = true; body.append(edit);
    }
    details.addEventListener('toggle', () => { if (details.open && valid()) void load(); });
  }
  function knowledgePanel(host, { api, t, valid = () => true }) {
    const details = el('details', 'project-knowledge'); details.dataset.projectKnowledge = 'true';
    details.append(el('summary', '', t('项目知识与决策'))); const body = el('div', 'project-record-body'); details.append(body); host.append(details);
    let loading = false;
    async function load() { if (loading) return; loading = true; try { const view = await api('/project-knowledge'); if (valid() && details.isConnected) render(view, true); } catch (e) { body.replaceChildren(); errorBox(body).textContent = e.message; } finally { loading = false; } }
    function render(view) {
      body.replaceChildren(); body.append(el('p', 'muted', t('保存在当前项目记忆中，可跨会话读取。确认表示用户核对过记录，不代表新授权；来源内容仍需核实。')));
      const toolbar = el('div', 'plan-io'); const refresh = button('刷新', t); refresh.onclick = load;
      const download = button('导出知识记录', t); download.onclick = () => { const url = URL.createObjectURL(new Blob([JSON.stringify({ schema: 'aide.project-knowledge/v1', workspaceId: view.workspaceId, records: view.records }, null, 2)], { type: 'application/json' })); const a = el('a'); a.href = url; a.download = 'aide-project-knowledge.json'; document.body.append(a); a.click(); setTimeout(() => { URL.revokeObjectURL(url); a.remove(); }, 1000); };
      toolbar.append(refresh, download); body.append(toolbar); if (view.warning) body.append(el('p', 'project-error', view.warning));
      const form = el('form', 'project-knowledge-add'); const kind = el('select'); kind.name = 'kind'; kind.setAttribute('aria-label', t('知识类型'));
      for (const [value, label] of Object.entries(kinds)) { const o = el('option', '', t(label)); o.value = value; kind.append(o); } form.append(kind);
      const subject = field(form, '主题', 'subject', '', t, 1); subject.maxLength = 200;
      const content = field(form, '记录内容', 'content', '', t, 3); content.maxLength = 2000;
      const source = field(form, '来源与版本', 'source', '', t); source.maxLength = 1000;
      for (const input of [subject, content, source]) input.required = true;
      form.append(el('p', 'muted', t('主题、内容、来源与版本均为必填。来源可写文件路径和版本、会话记录，或实际观察的时间与依据。')));
      source.placeholder = t('例如：spec.md，修订 3；或 2026-11-13 的现场观察记录。');
      const submit = button('保存为待确认', t); submit.type = 'submit'; form.append(submit); const error = errorBox(form);
      const draft = retainDraft(form, 'knowledge:' + view.workspaceId, view.snapshot, submit, t);
      async function mutate(data, control, error) {
        control.disabled = true; error.textContent = '';
        try { const next = await api('/project-knowledge', { method: 'POST', body: JSON.stringify({ ...data, workspaceId: view.workspaceId, snapshot: view.snapshot }) }); if (data.action === 'propose') draft.clear(); if (valid() && details.isConnected) render(next, data.action !== 'propose'); }
        catch (e) { error.textContent = e.message; }
        finally { control.disabled = false; }
      }
      form.onsubmit = event => { event.preventDefault(); if (draft.ready()) void mutate({ action: 'propose', kind: kind.value, subject: subject.value, content: content.value, source: source.value }, submit, error); }; body.append(form);
      for (const k of view.records || []) {
        const row = el('article', 'project-knowledge-record'); row.dataset.knowledgeId = k.id;
        row.append(el('strong', '', k.subject), el('span', 'plan-tag', t(kinds[k.kind]) + ' · ' + t(states[k.state])));
        row.append(el('p', '', k.content), el('p', 'project-source', t('来源与版本') + '：' + k.source), el('p', 'project-source', `${k.id} · ${t(k.origin === 'model_proposal' ? '模型提议' : '用户记录')} · ${k.updated}`));
        if (k.supersededBy) row.append(el('p', 'project-source', t('替代记录') + '：' + k.supersededBy));
        if (view.conflicts?.[k.id]) row.append(el('p', 'project-error', t('同主题存在多条已确认记录，请核对冲突或明确替代。')));
        const error = errorBox(row);
        if (k.state === 'proposed') {
          const replacements = [];
          const key = value => value.trim().replace(/\s+/gu, ' ').toLowerCase();
          for (const old of view.records.filter(old => old.state === 'confirmed' && old.kind === k.kind && key(old.subject) === key(k.subject))) {
            const label = el('label', 'project-replace'); const check = el('input'); check.type = 'checkbox'; check.value = old.id; label.append(check, el('span', '', t('替代此记录') + '：' + old.content)); row.append(label); replacements.push(check);
          }
          const confirm = button('确认此记录', t); confirm.onclick = () => mutate({ action: 'confirm', id: k.id, supersedes: replacements.filter(c => c.checked).map(c => c.value) }, confirm, error); row.append(confirm);
        }
        if (k.state === 'proposed' || k.state === 'confirmed') { const archive = button('归档此记录', t); archive.onclick = () => mutate({ action: 'archive', id: k.id }, archive, error); row.append(archive); }
        body.append(row);
      }
    }
    details.addEventListener('toggle', () => { if (details.open && valid()) void load(); });
  }
  window.AideProjectRecords = { contractFields, goalCard, knowledgePanel };
})();
