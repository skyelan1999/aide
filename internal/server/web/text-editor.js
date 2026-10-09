/* Shared text editor: logical line numbers, optional highlighting, bounded gutter DOM. */
(() => {
  'use strict';
  const mounted = new WeakMap();
  const enabled = key => window.aideUI?.get(key) !== false;
  function dispose(textarea) { mounted.get(textarea)?.(); mounted.delete(textarea); }
  function mount(textarea, lang) {
    dispose(textarea);
    const wrapper = document.createElement('div'); wrapper.className = 'code-wrapper';
    textarea.before(wrapper); wrapper.append(textarea);
    const gutter = document.createElement('div'); gutter.className = 'code-line-gutter'; gutter.setAttribute('aria-hidden','true');
    const numbers = document.createElement('pre'); gutter.append(numbers);
    const pre = document.createElement('pre'); pre.className = 'code-highlight-overlay'; pre.setAttribute('aria-hidden','true');
    const code = document.createElement('code'); pre.append(code); wrapper.prepend(gutter,pre);
    const original = {wrap:textarea.wrap,whiteSpace:textarea.style.whiteSpace,wordBreak:textarea.style.wordBreak,overflowWrap:textarea.style.overflowWrap,fontVariantLigatures:textarea.style.fontVariantLigatures};
    // One logical source line per row; horizontal scrolling keeps numbers exact.
    textarea.wrap='off'; textarea.style.whiteSpace='pre'; textarea.style.wordBreak='normal'; textarea.style.overflowWrap='normal'; textarea.style.fontVariantLigatures='none';
    let frame=0, count=1, lineHeight=20, paddingTop=0, highlight=false, composing=false, disposed=false;
    function syncScroll() {
      // Paint inside the native client viewport, including Windows scrollbar widths.
      code.style.transform='translate('+(-textarea.scrollLeft)+'px,'+(-textarea.scrollTop)+'px)';
      if (gutter.hidden) return;
      const first=Math.max(0,Math.floor((textarea.scrollTop-paddingTop)/lineHeight));
      const last=Math.min(count,first+Math.ceil(textarea.clientHeight/lineHeight)+3);
      const visible=[]; for(let i=first;i<last;i++)visible.push(String(i+1));
      numbers.textContent=visible.join('\n');
      numbers.style.transform='translateY('+(paddingTop+first*lineHeight-textarea.scrollTop)+'px)';
    }
    function layout() {
      if(disposed)return;
      const style=getComputedStyle(textarea);
      lineHeight=parseFloat(style.lineHeight)||parseFloat(style.fontSize)*1.5; paddingTop=parseFloat(style.paddingTop)||0;
      pre.style.left=(textarea.offsetLeft+textarea.clientLeft)+'px'; pre.style.top=(textarea.offsetTop+textarea.clientTop)+'px';
      pre.style.width=textarea.clientWidth+'px'; pre.style.height=textarea.clientHeight+'px';
      for (const key of ['fontFamily','fontSize','fontWeight','fontStyle','fontStretch','fontVariant','fontVariantLigatures','fontFeatureSettings','fontVariationSettings','fontKerning','fontOpticalSizing','lineHeight','letterSpacing','wordSpacing','textIndent','textTransform','textAlign','direction','padding','tabSize'])pre.style[key]=style[key];
      pre.style.boxSizing='border-box';
      pre.style.whiteSpace='pre'; pre.style.wordBreak='normal';
      numbers.style.font=style.font; numbers.style.lineHeight=lineHeight+'px'; numbers.style.letterSpacing=style.letterSpacing;
      gutter.style.top=(textarea.offsetTop+textarea.clientTop)+'px'; gutter.style.height=textarea.clientHeight+'px';
      syncScroll();
    }
    function render() {
      frame=0; if(disposed)return; const text=textarea.value; count=text.split('\n').length;
      wrapper.style.setProperty('--code-gutter-width',enabled('fileLineNumbers')?Math.max(48,String(count).length*9+24)+'px':'0px');
      gutter.hidden=!enabled('fileLineNumbers');
      highlight=!!(!composing && enabled('fileSyntaxHighlight') && window.hljs && lang && text.length<=500000);
      textarea.classList.toggle('code-editable',highlight); pre.hidden=!highlight;
      if(highlight) { try {code.innerHTML=window.hljs.highlight(text+'\n',{language:lang,ignoreIllegals:true}).value;} catch {code.textContent=text+'\n';} }
      layout();
    }
    function schedule(){if(!disposed && !frame)frame=requestAnimationFrame(render);}
    function compositionStart(){composing=true;textarea.classList.remove('code-editable');pre.hidden=true;layout();}
    function compositionEnd(){composing=false;schedule();}
    const fonts=document.fonts; fonts?.addEventListener('loadingdone',schedule); fonts?.ready.then(schedule);
    textarea.addEventListener('compositionstart',compositionStart); textarea.addEventListener('compositionend',compositionEnd);
    const observer=typeof ResizeObserver==='function'?new ResizeObserver(layout):null; observer?.observe(textarea);
    textarea.addEventListener('input',schedule); textarea.addEventListener('scroll',syncScroll,{passive:true}); window.addEventListener('resize',layout);
    let prefs=[enabled('fileLineNumbers'),enabled('fileSyntaxHighlight')].join(':');
    const unsub=window.aideUI?.subscribe(()=>{const next=[enabled('fileLineNumbers'),enabled('fileSyntaxHighlight')].join(':');if(next!==prefs){prefs=next;render();}});
    mounted.set(textarea,()=>{
      disposed=true; cancelAnimationFrame(frame); fonts?.removeEventListener('loadingdone',schedule);
      textarea.removeEventListener('compositionstart',compositionStart); textarea.removeEventListener('compositionend',compositionEnd);
      observer?.disconnect(); unsub?.(); window.removeEventListener('resize',layout);
      textarea.removeEventListener('input',schedule); textarea.removeEventListener('scroll',syncScroll); textarea.classList.remove('code-editable');
      textarea.wrap=original.wrap; for(const key of ['whiteSpace','wordBreak','overflowWrap','fontVariantLigatures'])textarea.style[key]=original[key];
      wrapper.before(textarea); wrapper.remove();
    });
    render();
  }
  window.AideTextEditor={mount,dispose};
})();
