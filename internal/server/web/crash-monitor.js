/* Independent diagnostics: no task replay, automatic reload, or report upload. */
(() => {
  'use strict';
  if (window.AideCrash) return;
  const STORE = 'aide.crash.metadata.v1', TAB = 'aide.crash.tab.v1', LIMIT = 12;
  const labels = {
    'AIDE-WEB-RUNTIME': '页面脚本异常',
    'AIDE-WEB-PROMISE': '异步操作未处理异常',
    'AIDE-WEB-ASSET': '工作台资源加载失败',
    'AIDE-WEB-UNCLEAN': '上次页面可能未正常结束',
    'AIDE-SERVICE-UNREACHABLE': '服务暂时无法连接，原因待确认',
    'AIDE-SERVER-PANIC': '服务请求发生内部异常'
  };
  const t = value => window.aideI18n?.t(value) || value;
  const clean = value => String(value ?? '').slice(0, 12000)
    .replace(/https?:\/\/[^\s<>"')]+/gi, value => {
      try { const position=value.match(/:(\d+):(\d+)$/)?.[0] || ''; const url = new URL(position ? value.slice(0,-position.length) : value); url.username = ''; url.password = ''; url.search = ''; url.hash = ''; return url.href + position; }
      catch (_) { return '[url]'; }
    })
    .replace(/\bBearer\s+[^\s"']+/gi, 'Bearer [redacted]')
    .replace(/\bsk-[a-z0-9_.-]{6,}/gi, '[redacted]')
    .replace(/((?:["']?)(?:api[_-]?key|access[_-]?token|password|secret)(?:["']?)\s*[:=]\s*["']?)[^\s,"';&]+/gi, '$1[redacted]')
    .slice(0, 6000);
  const filename = value => {
    if (!value || value === 'undefined' || value === 'null') return '';
    try { return clean(decodeURIComponent(new URL(String(value), location.href).pathname.split('/').pop())).slice(0, 100); }
    catch (_) { return ''; }
  };
  const read = () => {
    try { const data = JSON.parse(localStorage.getItem(STORE) || '[]'); return Array.isArray(data) ? data.filter(r => r && Object.prototype.hasOwnProperty.call(labels,r.code) && typeof r.id === 'string').slice(-LIMIT).map(r => ({id:r.id.slice(0,80),code:r.code,at:String(r.at).slice(0,40),file:filename(r.file),line:Number(r.line)||0,column:Number(r.column)||0,count:Math.min(9999,Math.max(1,Number(r.count)||1))})) : []; }
    catch (_) { return []; }
  };
  let reports = read(), selected = null, notice, dialog, summary, detail, copy, list, timer, controller;
  let updateNotice, updateMessage, updateRefresh, updateDismiss, updatePending = false;
  const ui = {};
  let dismissed = true, failures = 0, instance = '', connectionReport = null, busy = false, disposed = false;
  const locked = () => !!document.querySelector('#login-dialog[open],#lock-screen:not([hidden])') || document.body?.classList.contains('map-locked');
  function persist() {
    try {
      const merged = new Map([...read(), ...reports].map(r => [r.id, r]));
      // Persist metadata only. Raw exception messages and stacks stay in tab memory.
      localStorage.setItem(STORE, JSON.stringify([...merged.values()].slice(-LIMIT).map(({id,code,at,file,line,column,count}) => ({id,code,at,file,line,column,count}))));
    } catch (_) { /* Storage denial must not become another exception. */ }
  }
  function text(report) {
    return [t('错误代码') + ': ' + report.code, t('事件编号') + ': ' + report.id,
      t('发生时间') + ': ' + report.at, t(labels[report.code]),
      report.recovered ? t('服务连接已恢复') : '',
      report.file ? report.file + (report.line > 0 ? ':' + report.line + (report.column > 0 ? ':' + report.column : '') : '') : '',
      report.message || '', report.stack || t(report.current ? '本次异常未提供浏览器堆栈。' : '此历史记录仅保留诊断元数据，不保存异常正文和堆栈。')].filter(Boolean).join('\n');
  }
  function element(tag, value) { const node = document.createElement(tag); if (value) node.textContent = value; return node; }
  function paintUpdateNotice() {
    if (!document.body) return;
    if (!updateNotice) {
      updateNotice = element('aside'); updateNotice.id = 'aide-service-update-notice';
      updateNotice.setAttribute('role','status'); updateNotice.setAttribute('aria-live','polite');
      updateMessage = element('span');
      updateRefresh = element('button'); updateRefresh.type = 'button';
      updateRefresh.onclick = () => location.reload();
      updateDismiss = element('button','×'); updateDismiss.type = 'button';
      updateDismiss.onclick = () => { updatePending = false; paintUpdateNotice(); };
      updateNotice.append(updateMessage,updateRefresh,updateDismiss); document.body.append(updateNotice);
    }
    updateMessage.textContent = t('服务已更新，请刷新页面');
    updateRefresh.textContent = t('刷新页面');
    updateDismiss.setAttribute('aria-label',t('收起更新提示'));
    updateNotice.hidden = !updatePending || locked() || (!dismissed && reports.length > 0);
  }
  function paint() {
    if (!document.body) return;
    if (!notice) {
      notice = element('aside'); notice.id = 'aide-crash-notice'; notice.setAttribute('role','status'); notice.setAttribute('aria-live','polite');
      summary = element('button'); summary.type = 'button'; summary.onclick = open;
      const dismiss = element('button','×'); dismiss.type = 'button'; dismiss.setAttribute('aria-label',t('收起异常提示')); dismiss.onclick = () => { dismissed = true; paint(); };
      ui.dismiss = dismiss;notice.append(summary,dismiss); document.body.append(notice);
      dialog = element('dialog'); dialog.id = 'aide-crash-dialog';
      const heading = element('h2',t('崩溃与异常诊断')); list = element('select'); list.setAttribute('aria-label',t('选择异常记录'));
      detail = element('pre'); detail.tabIndex = 0;
      copy = element('button',t('复制诊断')); copy.type = 'button'; copy.onclick = async () => {
        if (!selected) return;
        try { await navigator.clipboard.writeText(text(selected)); copy.textContent = t('已复制诊断'); }
        catch (_) { copy.textContent = t('无法自动复制，请选择诊断文本复制'); detail.focus(); }
      };
      const close = element('button',t('关闭')); close.type = 'button'; close.onclick = () => dialog.close();
      const note = element('p',t('诊断仅保存在本机。不会自动刷新、重发任务或中断其他会话。'));
      list.onchange = () => { selected = reports.find(r => r.id === list.value) || reports.at(-1); detail.textContent = selected ? text(selected) : t('尚无异常记录'); copy.textContent = t('复制诊断'); };
      Object.assign(ui,{heading,note,close});dialog.append(heading,list,detail,note,copy,close); document.body.append(dialog);
    }
    ui.heading.textContent=t('崩溃与异常诊断');ui.note.textContent=t('诊断仅保存在本机。不会自动刷新、重发任务或中断其他会话。');ui.close.textContent=t('关闭');ui.dismiss.setAttribute('aria-label',t('收起异常提示'));list.setAttribute('aria-label',t('选择异常记录'));
    paintUpdateNotice();
    if (locked()) { notice.hidden = true; if (dialog.open) dialog.close(); return; }
    notice.hidden = dismissed || !reports.length;
    if (reports.length) summary.textContent = t(labels[reports.at(-1).code]) + ' · ' + reports.at(-1).code + ' · ' + t('查看诊断');
    if (dialog.open && selected) detail.textContent = text(selected);
  }
  function open() {
    if (locked()) return;
    paint(); selected = reports.at(-1); list.replaceChildren();
    for (const report of [...reports].reverse()) { const option = element('option', report.at + ' · ' + report.code + (report.count > 1 ? ' ×' + report.count : '')); option.value = report.id; list.append(option); }
    list.value = selected?.id || ''; detail.textContent = selected ? text(selected) : t('尚无异常记录'); copy.disabled = !selected; copy.textContent = t('复制诊断');
    if (!dialog.open) dialog.showModal();
  }
  function record(code, data = {}) {
    try {
      const message = clean(data.message), stack = clean(data.stack), file = filename(data.file);
      const last = reports.at(-1);
      if (last && last.code === code && last.message === message && last.file === file && last.line === (Number(data.line)||0) && Date.now()-Date.parse(last.at) < 10000) { last.count = Math.min(9999,last.count+1); persist(); return last; }
      const report = {code,id:String((typeof data.id === 'string' && /^[a-f0-9]{32}$/.test(data.id) ? data.id : '') || window.crypto?.randomUUID?.() || Date.now().toString(36)+'-'+Math.random().toString(36).slice(2,10)).slice(0,80),at:new Date().toISOString(),file,line:Number(data.line)||0,column:Number(data.column)||0,count:1,message,stack,current:true};
      reports.push(report); reports = reports.slice(-LIMIT); dismissed = false; persist(); paint(); return report;
    } catch (_) { /* A broken diagnostic must never recurse through global hooks. */ }
  }
  window.addEventListener('error', event => {
    if (event.target && event.target !== window) {
      const node = event.target;
      const source = node.tagName === 'SCRIPT' ? node.src : node.tagName === 'LINK' && String(node.rel).toLowerCase() === 'stylesheet' ? node.href : '';
      if (!source) return;
      try { if (new URL(source,location.href).origin === location.origin) record('AIDE-WEB-ASSET',{file:source}); } catch (_) {}
      return;
    }
    record('AIDE-WEB-RUNTIME',{message:event.message,stack:event.error?.stack,file:event.filename,line:event.lineno,column:event.colno});
  },true);
  window.addEventListener('unhandledrejection', event => {
    if (event.reason?.name === 'AbortError') return;
    if (event.reason?.data?.code === 'AIDE-SERVER-PANIC') { window.AideCrash?.reportServer(event.reason.data); return; }
    record('AIDE-WEB-PROMISE',{message:event.reason?.message ?? event.reason,stack:event.reason?.stack});
  });
  function mark(active) { try { sessionStorage.setItem(TAB,JSON.stringify({active,at:new Date().toISOString()})); } catch (_) {} }
  // A newly opened tab can inherit sessionStorage from its opener. Only a
  // reload/history return provides evidence of this same tab returning.
  const navigationType=window.performance?.getEntriesByType?.('navigation')?.[0]?.type;
  try { const previous = JSON.parse(sessionStorage.getItem(TAB) || 'null'); if (previous?.active && ['reload','back_forward'].includes(navigationType)) record('AIDE-WEB-UNCLEAN',{message:t('最后记录时间')+': '+String(previous.at).slice(0,40)}); } catch (_) {}
  mark(true);
  async function probe() {
    if (busy || disposed || document.hidden || locked()) return;
    busy = true; controller = new AbortController(); const timeout = setTimeout(() => controller?.abort(),5000);
    try {
      const response = await fetch('/healthz',{cache:'no-store',credentials:'omit',signal:controller.signal});
      if (!response.ok) throw new Error('health');
      const data = await response.json(); if (data.status !== 'ok' || data.service !== 'aide') throw new Error('identity');
      if (disposed || document.hidden || locked()) return;
      failures = 0;
      if (connectionReport) { connectionReport.recovered = true; paint(); connectionReport = null; }
      if (typeof data.instanceId === 'string') { if (instance && instance !== data.instanceId) { updatePending = true; paintUpdateNotice(); } instance = data.instanceId; }
    } catch (_) {
      if (!disposed && !document.hidden && !locked() && navigator.onLine !== false && ++failures >= 3 && !connectionReport) connectionReport = record('AIDE-SERVICE-UNREACHABLE');
    } finally { clearTimeout(timeout); controller = null; busy = false; }
  }
  function tick() { clearTimeout(timer); if (disposed) return; if (!document.hidden) mark(true); probe(); timer = setTimeout(tick,30000); }
  function lifecycle() { paint(); if (document.hidden || locked()) { controller?.abort(); failures = 0; } else if (!disposed) tick(); }
  function ready() { paint(); let wasLocked=locked();const observer = new MutationObserver(() => { const next=locked();if(next!==wasLocked){wasLocked=next;lifecycle();} }); for (const node of [...document.querySelectorAll('#login-dialog,#lock-screen'),document.body]) observer.observe(node,{attributes:true,attributeFilter:['open','hidden','class']}); tick(); }
  document.addEventListener('visibilitychange',lifecycle);
  window.addEventListener('online',lifecycle);
  window.addEventListener('pagehide',() => { disposed = true; clearTimeout(timer); controller?.abort(); mark(false); });
  window.addEventListener('pageshow',event => { if (event.persisted) { disposed = false; mark(true); tick(); } });
  window.addEventListener('aide:language',() => { paint(); if(copy)copy.textContent=t('复制诊断'); });
  window.AideCrash = {open,reportServer(data) { if (data?.code === 'AIDE-SERVER-PANIC') record(data.code,{id:data.incidentId,message:t('服务端堆栈请按事件编号查阅本机服务日志。')}); }};
  if (document.readyState === 'loading') document.addEventListener('DOMContentLoaded',ready,{once:true}); else ready();
})();
