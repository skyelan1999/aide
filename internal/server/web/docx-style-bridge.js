/* Install document-generated DOCX styles through CSSOM under style-src 'self'. */
(function (root) {
  'use strict';
  let nextScope = 0;
  function apply(body, styleContainer) {
    const styles = Array.from(styleContainer.querySelectorAll('style'));
    if (!styles.length) return () => {};
    const id = 'aide-docx-' + (++nextScope);
    body.setAttribute('data-docx-style-scope', id);
    const parsed = new CSSStyleSheet(), sheet = new CSSStyleSheet();
    parsed.replaceSync(styles.map(style => style.textContent).join('\n'));
    const fonts = new Map();
    for (const rule of parsed.cssRules) if (rule.type === CSSRule.FONT_FACE_RULE) {
      const name = rule.style.fontFamily.replace(/^(['"])(.*)\1$/, '$2');
      if (!fonts.has(name)) fonts.set(name, id + '-font-' + fonts.size);
    }
    function fontFamily(value) {
      return value.replace(/"(?:[^"\\]|\\.)*"|'(?:[^'\\]|\\.)*'|[^,]+/g, token => {
        const name = token.trim().replace(/^(['"])(.*)\1$/, '$2');
        return fonts.has(name) ? '"' + fonts.get(name) + '"' : token;
      });
    }
    function declarations(style) {
      const copy = document.createElement('span').style;
      copy.cssText = style.cssText;
      if (copy.fontFamily) copy.fontFamily = fontFamily(copy.fontFamily);
      return copy.cssText;
    }
    function selectors(value) {
      const parts = []; let start = 0, depth = 0, quote = '';
      for (let i = 0; i < value.length; i++) {
        const c = value[i];
        if (c === '\\') { i++; continue; }
        if (quote) { if (c === quote) quote = ''; continue; }
        if (c === '"' || c === "'") quote = c;
        else if (c === '(' || c === '[') depth++;
        else if (c === ')' || c === ']') depth--;
        else if (c === ',' && depth === 0) { parts.push(value.slice(start, i)); start = i + 1; }
      }
      parts.push(value.slice(start));
      return parts.map(selector => ':where([data-docx-style-scope="' + id + '"]) ' + selector.trim()).join(',');
    }
    function rules(list) {
      let css = '';
      for (const rule of list) {
        if (rule.type === CSSRule.STYLE_RULE) {
          css += selectors(rule.selectorText) + '{' + declarations(rule.style) + '}\n';
        } else if (rule.type === CSSRule.FONT_FACE_RULE) {
          css += '@font-face{' + declarations(rule.style) + '}\n';
        } else if (rule.type === CSSRule.MEDIA_RULE || rule.type === CSSRule.SUPPORTS_RULE) {
          css += (rule.type === CSSRule.MEDIA_RULE ? '@media ' : '@supports ') + rule.conditionText + '{' + rules(rule.cssRules) + '}\n';
        }
        // DOCX does not need imports, keyframes or rules that can affect the application.
      }
      return css;
    }
    sheet.replaceSync(rules(parsed.cssRules));
    for (const node of body.querySelectorAll('[style]')) if (node.style.fontFamily) node.style.fontFamily = fontFamily(node.style.fontFamily);
    document.adoptedStyleSheets = [...document.adoptedStyleSheets, sheet];
    let released = false;
    return () => {
      if (released) return;
      released = true;
      document.adoptedStyleSheets = document.adoptedStyleSheets.filter(item => item !== sheet);
      body.removeAttribute('data-docx-style-scope');
    };
  }
  root.AideDocxStyles = Object.freeze({apply});
})(window);
