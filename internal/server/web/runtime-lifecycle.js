'use strict';
// A stopped server must not leave an authenticated conversation visible.
// Browsers may refuse window.close() for user-created tabs; synchronously erase
// the document and replace it with about:blank in that case as well.
(() => {
  let stopping = false;
  function shutdown(dispose) {
    if (stopping) return;
    stopping = true;
    try { window.dispatchEvent(new Event('aide:runtime-stopping')); } catch (_) {}
    try { dispose?.(); } catch (_) {}
    try { window.stop(); } catch (_) {}
    document.title = '';
    document.documentElement.replaceChildren();
    try { window.close(); } catch (_) {}
    try { window.location.replace('about:blank'); } catch (_) {}
  }
  window.addEventListener('pageshow', () => {
    if (stopping) {
      document.documentElement.replaceChildren();
      window.location.replace('about:blank');
    }
  });
  window.AideRuntimeLifecycle = Object.freeze({shutdown});
})();
