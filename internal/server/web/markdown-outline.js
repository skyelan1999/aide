/* Shared outline for file previews. Index the rendered document, never fenced code. */
(() => {
  const mounted = new WeakMap();
  function dispose(host) {
    window.AideMarkdownReader?.dispose(host);
    mounted.get(host)?.(); mounted.delete(host);
    host.classList.remove("md-preview-outline");
  }
  function mount(host, labels = {}) {
    dispose(host);
    const headings = [...host.querySelectorAll('h1,h2,h3,h4,h5,h6')].filter(h => h.textContent.trim());
    if (!headings.length) return;
    host.classList.add('md-preview-outline');
    const documentBody = document.createElement('article');
    documentBody.className = 'md-reader-content';
    while (host.firstChild) documentBody.append(host.firstChild);
    const panel = host.closest('.md-split');
    const docked = !!panel;
    const reader = document.createElement('div'); reader.className = docked ? 'md-reader md-reader-docked' : 'md-reader';
    const outline = document.createElement('details'); outline.className = 'md-outline';
    const summary = document.createElement('summary'); summary.textContent = labels.title || '标题目录'; summary.title = summary.textContent;
    const nav = document.createElement('nav'); nav.setAttribute('aria-label', summary.textContent);
    const list = document.createElement('ol');
    const baseLevel = Math.min(...headings.map(h => Number(h.tagName.slice(1))));
    const entries = headings.map((heading, index) => {
      const li = document.createElement('li');
      const link = document.createElement('a');
      // Instance-specific IDs avoid collisions with headings in the conversation.
      if (!heading.id) heading.id = `${host.id || 'md-preview'}-heading-${index + 1}`;
      link.href = `#${encodeURIComponent(heading.id)}`;
      link.textContent = heading.textContent.trim(); link.title = link.textContent;
      li.dataset.level = heading.tagName.slice(1);
      li.style.setProperty('--heading-depth', Number(heading.tagName.slice(1)) - baseLevel);
      link.onclick = event => {
        event.preventDefault();
        const top = heading.getBoundingClientRect().top - host.getBoundingClientRect().top + host.scrollTop;
        const inset = 20;
        host.scrollTo({ top: Math.max(0, top - inset), behavior: matchMedia('(prefers-reduced-motion: reduce)').matches ? 'auto' : 'smooth' });
        heading.tabIndex = -1; heading.focus({ preventScroll: true });
        if (docked || reader.getBoundingClientRect().width < 420) outline.open = false;
        select(index);
      };
      li.append(link); list.append(li); return {heading, link};
    });
    function select(index) {
      entries.forEach((entry, i) => {
        entry.link.classList.toggle('active', i === index);
        if (i === index) entry.link.setAttribute('aria-current', 'location');
        else entry.link.removeAttribute('aria-current');
      });
      // Scroll only the outline list; scrollIntoView would also move the document.
      if (!outline.open) return;
      const linkRect = entries[index].link.getBoundingClientRect();
      const navRect = nav.getBoundingClientRect();
      if (linkRect.top < navRect.top + 8) nav.scrollTop -= navRect.top + 8 - linkRect.top;
      else if (linkRect.bottom > navRect.bottom - 8) nav.scrollTop += linkRect.bottom - navRect.bottom + 8;
    }
    nav.append(list); outline.append(summary, nav);
    if (docked) {
      outline.classList.add('md-outline-dock');
      reader.append(documentBody); host.append(reader); panel.append(outline);
    } else { reader.append(outline, documentBody); host.append(reader); }
    const layout = () => {
      const height = Math.max(140, host.clientHeight - 64);
      outline.style.setProperty('--md-preview-height', `${height}px`);
      const width = reader.getBoundingClientRect().width;
      if (docked) return false;
      reader.classList.toggle("md-reader-compact", width < 420);
      reader.classList.toggle("md-reader-medium", width >= 420 && width < 640);
      return width < 420;
    };
    outline.open = docked ? false : !layout();
    let frame = 0;
    const update = () => {
      frame = 0;
      if (!reader.isConnected) return;
      const threshold = host.getBoundingClientRect().top + (reader.getBoundingClientRect().width < 420 ? summary.offsetHeight + 28 : 44);
      let lo = 0, hi = headings.length - 1, active = 0;
      while (lo <= hi) {
        const mid = (lo + hi) >> 1;
        if (headings[mid].getBoundingClientRect().top <= threshold) { active = mid; lo = mid + 1; }
        else hi = mid - 1;
      }
      if (host.scrollHeight > host.clientHeight && host.scrollTop + host.clientHeight >= host.scrollHeight - 3) active = headings.length - 1;
      select(active);
    };
    const schedule = () => { if (!frame) frame = requestAnimationFrame(update); };
    host.addEventListener('scroll', schedule, { passive: true });
    let compact = layout();
    const resize = () => {
      const next = layout();
      if (next !== compact) { compact = next; if (!docked) outline.open = !compact; }
      schedule();
    };
    const observer = typeof ResizeObserver === 'function' ? new ResizeObserver(resize) : null;
    observer?.observe(host);
    outline.addEventListener('toggle', schedule);
    const outside = event => { if (docked && !outline.contains(event.target)) outline.open = false; };
    const escape = event => { if (docked && outline.open && event.key === 'Escape') { outline.open = false; summary.focus(); } };
    document.addEventListener('pointerdown', outside);
    document.addEventListener('keydown', escape);
    mounted.set(host, () => {
      host.removeEventListener('scroll', schedule); observer?.disconnect(); cancelAnimationFrame(frame);
      document.removeEventListener('pointerdown', outside); document.removeEventListener('keydown', escape);
      if (docked) outline.remove();
    });
    schedule();
  }
  window.AideMarkdownOutline = { mount, dispose };
})();

/* Preview controls stay outside the scaled article, so zoom never shrinks the controls. */
(() => {
  const mounted = new WeakMap();
  const zoomByHost = new WeakMap();
  const levels = [0.7, 0.8, 0.9, 1, 1.1, 1.25, 1.5, 1.75, 2];
  function dispose(host) {
    if (!host) return;
    mounted.get(host)?.(); mounted.delete(host);
    host.style.removeProperty('--md-reader-toolbar-height');
  }
  function mount(host, labels = {}) {
    dispose(host);
    let reader = host.querySelector(':scope > .md-reader');
    let article = reader?.querySelector(':scope > .md-reader-content');
    if (!reader) {
      reader = document.createElement('div'); reader.className = 'md-reader md-reader-no-outline';
      article = document.createElement('article'); article.className = 'md-reader-content';
      while (host.firstChild) article.append(host.firstChild);
      reader.append(article); host.append(reader);
    }
    article.querySelectorAll('table').forEach(table => {
      const scroll = document.createElement('div'); scroll.className = 'md-table-scroll';
      scroll.setAttribute('role', 'region'); scroll.setAttribute('aria-label', labels.table || '表格');
      scroll.tabIndex = 0;
      table.replaceWith(scroll); scroll.append(table);
    });
    const toolbar = document.createElement('div'); toolbar.className = 'md-reader-controls';
    toolbar.setAttribute('role', 'group'); toolbar.setAttribute('aria-label', labels.zoom || '缩放');
    const control = (value, label) => {
      const button = document.createElement('button');
      button.type = 'button'; button.className = 'quiet quiet-sm';
      button.textContent = value; button.title = label; button.setAttribute('aria-label', label);
      toolbar.append(button); return button;
    };
    const smaller = control('−', labels.zoomOut || '缩小');
    const reset = control('', labels.actualSize || '原始大小'); reset.classList.add('md-reader-zoom-value');
    const larger = control('+', labels.zoomIn || '放大');
    const header = host.id === 'file-view-preview'
      ? host.closest('.file-view')?.querySelector('.file-view-head-actions')
      : host.id === 'editor-preview'
        ? host.closest('.editor-dialog')?.querySelector('.dialog-actions')
        : null;
    if (header) {
      toolbar.classList.add('md-reader-controls-header');
      header.insertBefore(toolbar, header.querySelector('.editor-mode-switch'));
    } else reader.prepend(toolbar);
    let level = levels.indexOf(zoomByHost.get(host) || 1);
    if (level < 0) level = 3;
    const paint = () => {
      article.style.zoom = String(levels[level]);
      reset.textContent = `${Math.round(levels[level] * 100)}%`;
      smaller.disabled = level === 0; larger.disabled = level === levels.length - 1;
      zoomByHost.set(host, levels[level]);
      host.style.setProperty('--md-reader-toolbar-height', header ? '0px' : `${toolbar.offsetHeight}px`);
      host.dispatchEvent(new Event('scroll'));
    };
    smaller.onclick = () => { level = Math.max(0, level - 1); paint(); };
    larger.onclick = () => { level = Math.min(levels.length - 1, level + 1); paint(); };
    reset.onclick = () => { level = 3; paint(); };
    paint();

    const eligible = image => image?.tagName === 'IMG' && !image.closest('.md-diagram, a[href]');
    article.querySelectorAll('img').forEach(image => {
      if (!eligible(image)) return;
      image.classList.add('md-image-zoomable'); image.tabIndex = 0;
      image.setAttribute('role', 'button');
      image.setAttribute('aria-label', `${labels.zoomImage || '放大'}${image.alt ? ': ' + image.alt : ''}`);
    });
    let lightbox = null;
    const openImage = source => {
      if (!eligible(source) || !source.src) return;
      if (lightbox?.open) lightbox.close();
      lightbox?.remove();
      const dialog = document.createElement('dialog'); dialog.className = 'md-image-lightbox';
      dialog.setAttribute('aria-label', labels.imagePreview || '图片预览');
      const close = document.createElement('button'); close.type = 'button';
      close.className = 'md-image-lightbox-close'; close.textContent = '×';
      close.setAttribute('aria-label', labels.closePreview || '关闭预览');
      close.onclick = () => dialog.close();
      const image = document.createElement('img'); image.src = source.currentSrc || source.src; image.alt = source.alt || '';
      dialog.append(close, image);
      if (source.alt) { const caption = document.createElement('p'); caption.textContent = source.alt; dialog.append(caption); }
      dialog.addEventListener('click', event => { if (event.target === dialog) dialog.close(); });
      dialog.addEventListener('close', () => { dialog.remove(); if (source.isConnected) source.focus(); if (lightbox === dialog) lightbox = null; }, {once:true});
      document.body.append(dialog); lightbox = dialog; dialog.showModal(); close.focus();
    };
    const click = event => { if (eligible(event.target)) { event.preventDefault(); openImage(event.target); } };
    const keydown = event => {
      if (eligible(event.target) && (event.key === 'Enter' || event.key === ' ')) {
        event.preventDefault(); openImage(event.target);
      }
    };
    article.addEventListener('click', click); article.addEventListener('keydown', keydown);
    mounted.set(host, () => {
      article.removeEventListener('click', click); article.removeEventListener('keydown', keydown);
      if (lightbox?.open) lightbox.close(); lightbox?.remove();
      toolbar.remove();
    });
  }
  window.AideMarkdownReader = {mount, dispose};
})();
