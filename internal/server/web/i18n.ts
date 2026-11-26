interface Window {
  aideEnglish?: any;
  aideUI?: any;
  aideI18n?: {
    t: (key: any, ...args: any[]) => string;
    language: () => string;
    schema: (value: any) => any;
    applyStatic: () => void;
    sourceName: (source: any) => any;
    ready: () => Promise<void>;
  };
}
'use strict';
/* Explicit localization of product UI only. No DOM-wide text replacement. */
(function () {
  let catalog = window.aideEnglish || {};
  let englishLoad: Promise<void> | null = null;
  const supported = ['zh-CN', 'en'];
  function language(): string {
    const pref = window.aideUI?.get('language');
    if (supported.includes(pref)) return pref;
    return /^en(?:-|$)/i.test(navigator.language || '') ? 'en' : 'zh-CN';
  }
  function ensureEnglish(): Promise<void> {
    if (language() !== 'en' || window.aideEnglish) { catalog = window.aideEnglish || catalog; return Promise.resolve(); }
    if (englishLoad) return englishLoad;
    englishLoad = new Promise(resolve => {
      const script = document.createElement('script');
      script.src = '/locales/en.js?v=222'; script.async = true;
      script.onload = () => { catalog = window.aideEnglish || {}; resolve(); };
      script.onerror = () => { englishLoad = null; resolve(); };
      document.head.append(script);
    });
    return englishLoad;
  }
  function t(key: any, ...args: any[]): string {
    if (key == null) return '';
    const source = String(key);
    const text = language() === 'en' && Object.prototype.hasOwnProperty.call(catalog, source) ? catalog[source] : source;
    return text.replace(/\{(\d+)\}/g, (match: string, index: number) => index < args.length ? String(args[index]) : match);
  }
  function schema(value: any): any {
    if (Array.isArray(value)) return value.map(schema);
    if (!value || typeof value !== 'object') return value;
    return Object.fromEntries(Object.entries(value).map(([key, item]) =>
      [key, ['title', 'label', 'description'].includes(key) && typeof item === 'string' ? t(item) : schema(item)]));
  }
  function applyStatic(): void {
    document.documentElement.lang = language();
    document.querySelectorAll<HTMLElement>('[data-i18n]').forEach(node => { node.textContent = t(node.dataset.i18n); });
    for (const attr of ['title', 'placeholder', 'aria-label', 'data-prompt']) {
      document.querySelectorAll<HTMLElement>('[data-i18n-' + attr + ']').forEach(node => {
        node.setAttribute(attr, t(node.getAttribute('data-i18n-' + attr)));
      });
    }
    document.querySelectorAll<HTMLSelectElement>('[data-language-selector]').forEach(select => {
      select.value = window.aideUI?.get('language') || 'system';
      if (!select.dataset.languageBound) {
        select.dataset.languageBound = 'true';
        select.addEventListener('change', () => window.aideUI.set('language', select.value));
      }
    });
  }
  function sourceName(source: any) {
    const defaults: Record<string, string> = {sessions:'会话', workspace:'工作区', context:'引用目录', 'system-docs':'自动系统文档'};
    const key = defaults[source?.id || source?.root || source?.region];
    return key && source?.name === key ? t(key) : source?.name;
  }
  window.aideI18n = { t, language, schema, applyStatic, sourceName, ready: ensureEnglish };
  let previous = language();
  applyStatic();
  if (previous === 'en') void ensureEnglish().then(() => { applyStatic(); window.dispatchEvent(new CustomEvent('aide:language')); });
  function changed(): void {
    const next = language();
    void ensureEnglish().then(() => {
      if (next !== language()) return;
      applyStatic();
      if (next !== previous) {
        previous = next;
        window.dispatchEvent(new CustomEvent('aide:language'));
      }
    });
  }
  window.aideUI?.subscribe(changed);
  window.addEventListener('languagechange', changed);
})();
