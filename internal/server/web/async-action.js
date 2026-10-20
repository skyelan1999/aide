/* Local feedback for explicitly wrapped UI actions; no global request/UI lock. */
(() => {
  'use strict';
  const pending = new WeakMap();
  function wrap(fn, onError = error => { throw error; }, {feedback = true} = {}) {
    return function (...args) {
      const event = args[0], current = feedback ? event?.currentTarget : null;
      const form = event?.type === 'submit' && current?.matches?.('form') ? current : null;
      const button = form ? (event.submitter || form.querySelector('button[type="submit"],button:not([type]),input[type="submit"]')) : current?.matches?.('button,input[type="submit"]') ? current : null;
      const owner = form || button;
      if (owner && pending.has(owner)) {
        event.preventDefault();
        return Promise.resolve();
      }
      let result;
      // Invoke synchronously: preserve preventDefault, clipboard/file-picker activation,
      // and each handler's existing guards and initial rendering.
      try { result = fn.apply(this, args); }
      catch (error) { return Promise.resolve().then(() => onError(error)); }
      if (!result || typeof result.then !== 'function') return Promise.resolve(result);
      const previousBusy = button?.getAttribute('aria-busy');
      if (button) {
        button.setAttribute('data-action-pending', '');
        if (previousBusy == null) button.setAttribute('aria-busy', 'true');
      }
      if (owner) pending.set(owner, true);
      return Promise.resolve(result).catch(onError).finally(() => {
        if (owner) pending.delete(owner);
        if (button) {
          button.removeAttribute('data-action-pending');
          if (previousBusy == null && button.getAttribute('aria-busy') === 'true') button.removeAttribute('aria-busy');
        }
      });
    };
  }
  window.AideAsyncAction = {wrap};
})();
