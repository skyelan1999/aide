'use strict';
/* Explicit localization of product UI only. No DOM-wide text replacement. */
(function () {
  const catalog = window.aideEnglish || {};
  const supported = ['zh-CN', 'en'];
  function language() {
    const pref = window.aideUI?.get('language');
    if (supported.includes(pref)) return pref;
    return /^en(?:-|$)/i.test(navigator.language || '') ? 'en' : 'zh-CN';
  }
  function t(key, ...args) {
    if (key == null) return '';
    const source = String(key);
    const text = language() === 'en' && Object.prototype.hasOwnProperty.call(catalog, source) ? catalog[source] : source;
    return text.replace(/\{(\d+)\}/g, (match, index) => index < args.length ? String(args[index]) : match);
  }
  function schema(value) {
    if (Array.isArray(value)) return value.map(schema);
    if (!value || typeof value !== 'object') return value;
    return Object.fromEntries(Object.entries(value).map(([key, item]) =>
      [key, ['title', 'label', 'description'].includes(key) && typeof item === 'string' ? t(item) : schema(item)]));
  }
  function applyStatic() {
    document.documentElement.lang = language();
    document.querySelectorAll('[data-i18n]').forEach(node => { node.textContent = t(node.dataset.i18n); });
    for (const attr of ['title', 'placeholder', 'aria-label', 'data-prompt']) {
      document.querySelectorAll('[data-i18n-' + attr + ']').forEach(node => {
        node.setAttribute(attr, t(node.getAttribute('data-i18n-' + attr)));
      });
    }
    document.querySelectorAll('[data-language-selector]').forEach(select => {
      select.value = window.aideUI?.get('language') || 'system';
      if (!select.dataset.languageBound) {
        select.dataset.languageBound = 'true';
        select.addEventListener('change', () => window.aideUI.set('language', select.value));
      }
    });
  }
  window.aideI18n = { t, language, schema, applyStatic };
  let previous = language();
  applyStatic();
  function changed() {
    const next = language();
    applyStatic();
    if (next !== previous) {
      previous = next;
      window.dispatchEvent(new CustomEvent('aide:language'));
    }
  }
  window.aideUI?.subscribe(changed);
  window.addEventListener('languagechange', changed);
})();
