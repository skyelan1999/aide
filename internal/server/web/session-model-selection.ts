interface Window {
  AideSessionModelSelection?: { install: (options: any) => void; current: (session?: any) => any; change: (patch: any) => Promise<any>; flush: (session?: any) => any; applyDraft: (session: any, choice: any) => Promise<void>; reasoningOptions: (model: any) => any };
}
(function () {
  'use strict';
  let config: any;
  const drafts = new Map<any, any>(), saved = new Map<any, any>(), pending = new Map<any, any>(), queues = new Map<any, any>(), epochs = new Map<any, any>();
  function scope(): string { return config?.state.config?.workspaceId || ''; }
  function key(session: any): string { return scope() + ':' + (session?.id || 'draft'); }
  function defaults(): any {
    const state = config.state;
    return {strategy:state.profiles?.strategy || 'auto', profile:state.profiles?.activeProfile || 'default', model:state.config?.activeModel || state.config?.model || '', reasoningEffort:state.config?.reasoningEffort || 'auto'};
  }
  function reasoningOptions(model: any): any {
    return config?.state.config?.models?.find((item: any) => item.id === model)?.reasoningEfforts || ['auto','off','low','medium','high'];
  }
  function adapt(choice: any): any {
    if (!choice) return choice;
    return {...choice, reasoningEffort:reasoningOptions(choice.model).includes(choice.reasoningEffort) ? choice.reasoningEffort : 'auto'};
  }
  function current(session = config?.state.session): any {
    return adapt(rawCurrent(session));
  }
  function rawCurrent(session = config?.state.session): any {
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
  function revision(session: any, id = key(session)): number {
    const cached = saved.get(id);
    return Math.max(Number(session?.modelSelectionRevision || 0), cached?.revision || 0);
  }
  function changed(id: string): void { if (id === key(config.state.session)) config.changed(); }
  async function change(patch: any): Promise<any> {
    const session = config.state.session, id = key(session), choice = adapt({...current(session), ...patch});
    if (session?.kind === 'assistant') throw new Error('Assistant model source is managed separately');
    if (!session) { drafts.set(id, choice); changed(id); return choice; }
    const token = {}, epoch = epochs.get(id) || 0;
    pending.set(id, {choice, token}); changed(id);
    const send = async (): Promise<any> => {
      try {
        if ((epochs.get(id) || 0) !== epoch) { const error: any = new Error(config.t?.('会话配置已变化，请重新选择') || '会话配置已变化，请重新选择'); error.status = 409; throw error; }
        const reply = await config.api('/sessions/' + encodeURIComponent(session.id) + '/model-selection', {method:'PUT', body:JSON.stringify({...choice, revision:revision(session, id)})});
        saved.set(id, {choice:reply.modelSelection, revision:reply.revision});
        if (id === key(config.state.session) && Number(config.state.session?.modelSelectionRevision || 0) <= reply.revision) {
          config.state.session.modelSelection = reply.modelSelection;
          config.state.session.modelSelectionRevision = reply.revision;
        }
        return reply.modelSelection;
      } catch (error: any) {
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
  function flush(session = config.state.session): any { return queues.get(key(session)); }
  async function applyDraft(session: any, choice: any): Promise<void> {
    // Called by submission with the original draft snapshot, even if the user
    // has switched chats while the server created the new session.
    const reply = await config.api('/sessions/' + encodeURIComponent(session.id) + '/model-selection', {method:'PUT', body:JSON.stringify({...choice, revision:session.modelSelectionRevision || 0})});
    session.modelSelection = reply.modelSelection; session.modelSelectionRevision = reply.revision;
    saved.set(key(session), {choice:reply.modelSelection, revision:reply.revision});
  }
  window.AideSessionModelSelection = {install(options: any){config=options;}, current, change, flush, applyDraft, reasoningOptions};
})();
