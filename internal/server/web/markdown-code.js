/* Dynamic Markdown code blocks: serialized markup, delegated copy, local syntax detection. */
(() => {
  'use strict';
  const MAX_HIGHLIGHT = 65536;
  const AUTO_LANGUAGES = ['javascript','typescript','python','go','json','bash','yaml','sql','cpp','csharp','java','rust','ruby','php','xml','css','dockerfile','ini','lua','kotlin','swift'];
  const icon = copied => copied
    ? '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.9" aria-hidden="true"><path d="m5 12 4 4L19 6"/></svg>'
    : '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" aria-hidden="true"><rect x="8" y="8" width="13" height="13" rx="2"/><path d="M16 8V5a2 2 0 0 0-2-2H5a2 2 0 0 0-2 2v9a2 2 0 0 0 2 2h3"/></svg>';
  let installed = false, translate = value => value, onError = () => {};
  const resets = new WeakMap();

  function highlight(text, declared = '', engine = window.hljs) {
    if (!engine || !text.trim() || text.length > MAX_HIGHLIGHT) return null;
    const lang = declared.toLowerCase();
    if (/^(text|txt|plain|plaintext|none|nohighlight)$/.test(lang)) return null;
    const alias = {clojurescript:'clojure'};
    const known = engine.getLanguage(lang) ? lang : alias[lang];
    try {
      if (known && engine.getLanguage(known)) return {...engine.highlight(text, {language:known,ignoreIllegals:true}), detected:false};
      if (/^[{[]/.test(text.trim()) && engine.getLanguage('json')) {
        try { JSON.parse(text); return {...engine.highlight(text, {language:'json',ignoreIllegals:true}), detected:true}; } catch (_) {}
      }
      const languages = AUTO_LANGUAGES.filter(language => engine.getLanguage(language));
      if (!languages.length) return null;
      const result = engine.highlightAuto(text, languages);
      // A few words or punctuation should not give ordinary text arbitrary colors.
      if (!result.language || result.relevance < 3) return null;
      return {...result, detected:true};
    } catch (_) { return null; }
  }
  function decorate(body, {live = false, t = translate} = {}) {
    if (!live) body.querySelectorAll('pre > code').forEach(code => {
      const declared = (code.className.match(/(?:^|\s)language-([\w+-]+)/) || [])[1] || '';
      const result = highlight(code.textContent || '', declared);
      if (!result) return;
      code.innerHTML = result.value;
      code.classList.add('hljs');
      code.dataset.highlightLanguage = result.language;
      code.dataset.highlightMode = result.detected ? 'auto' : 'explicit';
    });
    body.querySelectorAll('pre').forEach(pre => {
      if (pre.parentElement?.classList.contains('md-code-copy-wrap')) return;
      const wrap = document.createElement('div'); wrap.className = 'md-code-copy-wrap';
      pre.replaceWith(wrap); wrap.append(pre);
      const button = document.createElement('button'); button.type = 'button'; button.className = 'md-code-copy';
      button.title = t('复制代码'); button.setAttribute('aria-label', button.title); button.innerHTML = icon(false);
      wrap.append(button);
    });
  }
  function fallbackCopy(text, anchor) {
    const active = document.activeElement, selection = window.getSelection?.();
    const ranges = selection ? Array.from({length:selection.rangeCount}, (_, i) => selection.getRangeAt(i).cloneRange()) : [];
    const inputSelection = active && typeof active.selectionStart === 'number'
      ? [active.selectionStart, active.selectionEnd, active.selectionDirection] : null;
    const field = document.createElement('textarea'); field.value = text; field.readOnly = true;
    field.style.cssText = 'position:fixed;left:0;top:0;width:1px;height:1px;opacity:0;font-size:16px';
    // Keep the selection inside the active modal's focus scope.
    (anchor?.closest('dialog') || document.body).append(field);
    try {
      field.focus({preventScroll:true}); field.select(); field.setSelectionRange(0, text.length);
      if (!document.execCommand('copy')) throw new Error('copy failed');
    } finally {
      field.remove();
      if (active?.isConnected) {
        active.focus({preventScroll:true});
        if (inputSelection) active.setSelectionRange(...inputSelection);
      }
      if (selection && ranges.length) { selection.removeAllRanges(); ranges.forEach(range => selection.addRange(range)); }
    }
  }
  async function copyText(text, anchor) {
    if (window.navigator.clipboard?.writeText) {
      try { await window.navigator.clipboard.writeText(text); return; } catch (_) { /* Try selection-based copying too. */ }
    }
    fallbackCopy(text, anchor);
  }
  function install(options = {}) {
    translate = options.t || translate; onError = options.onError || onError;
    if (installed) return;
    installed = true;
    document.addEventListener('click', async event => {
      const button = event.target.closest?.('button.md-code-copy');
      const pre = button?.parentElement?.querySelector('pre');
      if (!pre) return;
      event.preventDefault(); event.stopPropagation();
      const text = pre.querySelector('code')?.textContent ?? pre.textContent ?? '';
      try {
        await copyText(text, button);
        if (!button.isConnected) return;
        clearTimeout(resets.get(button));
        button.classList.add('copied'); button.title = translate('已复制代码'); button.setAttribute('aria-label', button.title); button.innerHTML = icon(true);
        resets.set(button, setTimeout(() => {
          resets.delete(button);
          if (!button.isConnected) return;
          button.classList.remove('copied'); button.title = translate('复制代码'); button.setAttribute('aria-label', button.title); button.innerHTML = icon(false);
        }, 1400));
      } catch (_) {
        clearTimeout(resets.get(button)); resets.delete(button);
        button.classList.remove('copied'); button.title = translate('复制代码'); button.setAttribute('aria-label', button.title); button.innerHTML = icon(false);
        onError();
      }
    }, true);
  }
  window.AideMarkdownCode = {decorate, highlight, copyText, install};
})();
