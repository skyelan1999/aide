(function () {
  'use strict';
  let config;
  const drafts = new Map(), saved = new Map(), pending = new Map(), queues = new Map(), epochs = new Map();
  function scope() { return config?.state.config?.workspaceId || ''; }
  function key(session) { return scope() + ':' + (session?.id || 'draft'); }
  function defaults() {
    const state = config.state;
    return {strategy:state.profiles?.strategy || 'auto', profile:state.profiles?.activeProfile || 'default', model:state.config?.activeModel || state.config?.model || '', reasoningEffort:state.config?.reasoningEffort || 'auto'};
  }
  function current(session = config?.state.session) {
    if (!config) return null;
    const base = defaults();
    if (session?.kind === 'assistant') return base;
    const id = key(session);
    if (pending.has(id)) return {...pending.get(id).choice};
    if (!session) return {...base, ...(drafts.get(id) || {})};
    const cached = saved.get(id), revision = Number(session.modelSelectionRevision || 0);
    const choice = cached && cached.revision > revision ? cached.choice : session.modelSelection;
    return {...base, ...(choice || {}), model:choice?.model || base.model};
  }
  function revision(session, id = key(session)) {
    const cached = saved.get(id);
    return Math.max(Number(session?.modelSelectionRevision || 0), cached?.revision || 0);
  }
  function changed(id) { if (id === key(config.state.session)) config.changed(); }
  async function change(patch) {
    const session = config.state.session, id = key(session), choice = {...current(session), ...patch};
    if (session?.kind === 'assistant') throw new Error('Assistant model source is managed separately');
    if (!session) { drafts.set(id, choice); changed(id); return choice; }
    const token = {}, epoch = epochs.get(id) || 0;
    pending.set(id, {choice, token}); changed(id);
    const send = async () => {
      try {
        if ((epochs.get(id) || 0) !== epoch) { const error = new Error(config.t?.('会话配置已变化，请重新选择') || '会话配置已变化，请重新选择'); error.status = 409; throw error; }
        const reply = await config.api('/sessions/' + encodeURIComponent(session.id) + '/model-selection', {method:'PUT', body:JSON.stringify({...choice, revision:revision(session, id)})});
        saved.set(id, {choice:reply.modelSelection, revision:reply.revision});
        if (id === key(config.state.session) && Number(config.state.session?.modelSelectionRevision || 0) <= reply.revision) {
          config.state.session.modelSelection = reply.modelSelection;
          config.state.session.modelSelectionRevision = reply.revision;
        }
        return reply.modelSelection;
      } catch (error) {
        // A conflicting window edit is authoritative. Refresh only this small
        // choice cache; never overwrite messages or switch the visible session.
        if (error.status === 409) {
          epochs.set(id, Math.max(epochs.get(id) || 0, epoch + 1));
          const latest = await config.api('/sessions/' + encodeURIComponent(session.id) + '?limit=1');
          saved.set(id, {choice:latest.modelSelection, revision:latest.modelSelectionRevision || 0});
        }
        throw error;
      } finally {
        if (pending.get(id)?.token === token) pending.delete(id);
        changed(id);
      }
    };
    const result = (queues.get(id) || Promise.resolve()).catch(() => {}).then(send);
    queues.set(id, result);
    try { return await result; } finally { if (queues.get(id) === result) queues.delete(id); }
  }
  function flush(session = config.state.session) { return queues.get(key(session)); }
  async function applyDraft(session, choice) {
    // Called by submission with the original draft snapshot, even if the user
    // has switched chats while the server created the new session.
    const reply = await config.api('/sessions/' + encodeURIComponent(session.id) + '/model-selection', {method:'PUT', body:JSON.stringify({...choice, revision:session.modelSelectionRevision || 0})});
    session.modelSelection = reply.modelSelection; session.modelSelectionRevision = reply.revision;
    saved.set(key(session), {choice:reply.modelSelection, revision:reply.revision});
  }
  window.AideSessionModelSelection = {install(options){config=options;}, current, change, flush, applyDraft};
})();
