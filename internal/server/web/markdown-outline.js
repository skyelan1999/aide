/* Shared outline for file previews. Index the rendered document, never fenced code. */
(() => {
  const mounted = new WeakMap();
  function dispose(host) { mounted.get(host)?.(); mounted.delete(host); host.classList.remove("md-preview-outline"); }
  function mount(host, labels = {}) {
    dispose(host);
    const headings = [...host.querySelectorAll('h1,h2,h3,h4,h5,h6')].filter(h => h.textContent.trim());
    if (!headings.length) return;
    host.classList.add('md-preview-outline');
    const documentBody = document.createElement('article');
    documentBody.className = 'md-reader-content';
    while (host.firstChild) documentBody.append(host.firstChild);
    const reader = document.createElement('div'); reader.className = 'md-reader';
    const outline = document.createElement('details'); outline.className = 'md-outline';
    outline.open = host.clientWidth >= 640;
    const summary = document.createElement('summary'); summary.textContent = labels.title || '标题目录';
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
      li.style.setProperty('--heading-depth', Number(heading.tagName.slice(1)) - baseLevel);
      link.onclick = event => {
        event.preventDefault();
        const top = heading.getBoundingClientRect().top - host.getBoundingClientRect().top + host.scrollTop;
        const inset = reader.getBoundingClientRect().width < 640 ? Math.min(outline.offsetHeight, host.clientHeight * .35) + 16 : 20;
        host.scrollTo({ top: Math.max(0, top - inset), behavior: matchMedia('(prefers-reduced-motion: reduce)').matches ? 'auto' : 'smooth' });
        heading.tabIndex = -1; heading.focus({ preventScroll: true });
        if (reader.getBoundingClientRect().width < 640) outline.open = false;
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
    }
    nav.append(list); outline.append(summary, nav); reader.append(outline, documentBody); host.append(reader);
    let frame = 0;
    const update = () => {
      frame = 0;
      if (!reader.isConnected) return;
      const threshold = host.getBoundingClientRect().top + (reader.getBoundingClientRect().width < 640 ? summary.offsetHeight + 28 : 44);
      let active = 0;
      for (let i = 0; i < headings.length; i++) { if (headings[i].getBoundingClientRect().top <= threshold) active = i; else break; }
      if (host.scrollHeight > host.clientHeight && host.scrollTop + host.clientHeight >= host.scrollHeight - 3) active = headings.length - 1;
      select(active);
    };
    const schedule = () => { if (!frame) frame = requestAnimationFrame(update); };
    host.addEventListener('scroll', schedule, { passive: true });
    const observer = typeof ResizeObserver === 'function' ? new ResizeObserver(schedule) : null;
    observer?.observe(host);
    mounted.set(host, () => { host.removeEventListener('scroll', schedule); observer?.disconnect(); cancelAnimationFrame(frame); });
    schedule();
  }
  window.AideMarkdownOutline = { mount, dispose };
})();
