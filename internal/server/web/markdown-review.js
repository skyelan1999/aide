/* Document review shares the Markdown reader and Aide's sidecar comment store. */
(() => {
  'use strict';

  const mounted = new WeakMap();
  const reviews = new WeakMap();
  const normalize = value => value.replace(/\s+/g, ' ').trim();
  const element = (tag, className, text = '') => {
    const node = document.createElement(tag);
    node.className = className;
    node.textContent = text;
    return node;
  };

  function selectedRange(article, selection) {
    if (!selection || selection.isCollapsed || selection.rangeCount !== 1) return null;
    const range = selection.getRangeAt(0);
    if (!article.contains(range.startContainer) || !article.contains(range.endContainer)) return null;
    return range.cloneRange();
  }

  function selectionAnchor(article, range) {
    const rawQuote = range.toString();
    const anchorQuote = normalize(rawQuote);
    if (!anchorQuote) return null;
    const before = range.cloneRange();
    before.setStart(article, 0);
    before.setEnd(range.startContainer, range.startOffset);
    // Match in document order even for a backwards selection and repeated text.
    const prefix = (before.toString() + rawQuote.match(/^\s*/)[0]).replace(/\s+/g, ' ');
    const text = article.textContent.replace(/\s+/g, ' ');
    let anchorIndex = 0;
    let offset = text.indexOf(anchorQuote);
    while (offset >= 0 && offset < prefix.length) {
      anchorIndex++;
      offset = text.indexOf(anchorQuote, offset + 1);
    }
    return offset === prefix.length ? { anchorQuote, anchorIndex } : null;
  }

  function anchorRange(article, anchor) {
    const quote = normalize(anchor.anchorQuote || '');
    if (!quote) return null;
    const raw = article.textContent;
    const text = raw.replace(/\s+/g, ' ');
    let start = text.indexOf(quote);
    for (let index = 0; start >= 0 && index < anchor.anchorIndex; index++) {
      start = text.indexOf(quote, start + 1);
    }
    if (start < 0) return null;
    const end = start + quote.length;
    let rawStart = start;
    let rawEnd = end;
    for (const match of raw.matchAll(/\s{2,}/g)) {
      const collapsed = match[0].length - 1;
      if (match.index < rawStart) rawStart += collapsed;
      if (match.index < rawEnd) rawEnd += collapsed;
      else break;
    }
    const range = document.createRange();
    const walker = document.createTreeWalker(article, NodeFilter.SHOW_TEXT);
    let offset = 0;
    let started = false;
    for (let node = walker.nextNode(); node; node = walker.nextNode()) {
      const next = offset + node.length;
      if (!started && rawStart < next) {
        range.setStart(node, rawStart - offset);
        started = true;
      }
      if (started && rawEnd <= next) {
        range.setEnd(node, rawEnd - offset);
        return range;
      }
      offset = next;
    }
    return null;
  }

  function dispose(host) {
    mounted.get(host)?.dispose();
    mounted.delete(host);
  }

  function mount(host, article, toolbar, { spec, workspaceId, api, t, version, current }) {
    dispose(host);
    if (!reviews.has(spec)) reviews.set(spec, { open: false, draft: '', anchor: null });
    const review = reviews.get(spec);
    const controller = new AbortController();
    let network = new AbortController();
    const reader = article.parentElement;
    const dialog = host.closest('dialog');
    const slot = host.id === 'editor-preview' ? 'editor' : 'file';
    const highlights = globalThis.CSS?.highlights;
    const highlightName = 'aide-md-review-' + slot;
    const activeName = highlightName + '-active';
    const params = new URLSearchParams({ root: spec.root || 'workspace', path: spec.path });
    if (spec.source) params.set('source', spec.source);
    if (workspaceId) params.set('workspaceId', workspaceId);
    const route = (id = '') => '/file/markdown-comments' + (id ? '/' + encodeURIComponent(id) : '') + '?' + params;
    const call = (id, method = 'GET', payload) => api(route(id), {
      method,
      ...(payload ? { body: JSON.stringify(payload) } : {}),
      signal: network.signal
    });
    const alive = () => !controller.signal.aborted && current();
    const button = (label, onClick) => {
      const control = element('button', 'quiet quiet-sm', t(label));
      control.type = 'button';
      control.onclick = onClick;
      return control;
    };

    let comments = [];
    let candidate = null;
    let busy = false;
    let loadId = 0;
    let activeId = '';
    const toggle = button('批注', () => setOpen(!review.open));
    toggle.classList.add('md-review-toggle');
    toggle.title = t('批注');
    toggle.insertAdjacentHTML('afterbegin', '<svg class="ui-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M20 15a2 2 0 0 1-2 2H9l-5 4V6a2 2 0 0 1 2-2h12a2 2 0 0 1 2 2zM8 9h8m-8 4h5"/></svg>');
    const panel = element('aside', 'md-review-panel');
    panel.id = 'markdown-comments-' + slot;
    toggle.setAttribute('aria-controls', panel.id);
    panel.setAttribute('aria-label', t('Markdown 批注'));
    const heading = element('header', 'md-review-heading');
    heading.append(element('strong', '', t('Markdown 批注')), button('关闭批注', () => {
      setOpen(false);
      toggle.focus();
    }));
    const add = button('添加批注', startComment);
    add.addEventListener('pointerdown', event => event.preventDefault());
    const status = element('p', 'md-review-status');
    status.setAttribute('role', 'status');
    const form = element('form', 'md-review-form');
    const quote = element('blockquote', 'md-review-quote');
    const input = element('textarea', 'md-review-input');
    input.rows = 3;
    input.maxLength = 4000;
    input.setAttribute('aria-label', t('批注内容'));
    input.value = review.draft;
    input.oninput = () => { review.draft = input.value; };
    const actions = element('div', 'md-review-actions');
    const save = element('button', 'primary primary-sm', t('保存批注'));
    save.type = 'submit';
    actions.append(save, button('取消', () => {
      review.anchor = null;
      review.draft = input.value = '';
      paintForm();
      add.focus();
    }));
    form.append(quote, input, actions);
    const list = element('div', 'md-review-list');
    panel.append(heading, add, status, form, list);
    reader.prepend(panel);
    toolbar.append(toggle);

    function paintForm() {
      form.hidden = !review.anchor;
      input.disabled = busy;
      quote.textContent = review.anchor?.anchorQuote || '';
      save.disabled = busy || !review.anchor || version() !== review.anchor.hash;
    }

    function showStatus(message) {
      status.textContent = message ? t(message) : '';
    }

    function updateSelection() {
      if (!review.open || !alive()) return;
      candidate = selectedRange(article, window.getSelection());
      add.disabled = !candidate || busy;
    }

    function startComment() {
      if (!candidate || busy || !alive()) return;
      const hash = version();
      if (!hash) {
        showStatus('请先保存文档，再添加批注');
        return;
      }
      const anchor = selectionAnchor(article, candidate);
      if (!anchor) return;
      if (Array.from(anchor.anchorQuote).length > 500) {
        showStatus('选中的批注原文不能超过 500 字');
        return;
      }
      review.anchor = { ...anchor, hash };
      showStatus('');
      paintForm();
      input.focus();
    }

    function paintComments() {
      list.replaceChildren();
      const ranges = [];
      const hash = version();
      if (!comments.length) list.append(element('p', 'muted', t('暂无批注')));
      for (const comment of comments) {
        const sameVersion = !comment.stale && hash === comment.docHash;
        const range = sameVersion ? anchorRange(article, comment) : null;
        if (range && comment.status === 'open') ranges.push(range);
        const card = element('section', 'md-review-comment');
        card.classList.toggle('md-review-resolved', comment.status === 'resolved');
        const position = button('定位批注原文', () => {
          if (highlights) highlights.set(activeName, new Highlight(range));
          else {
            const selection = window.getSelection();
            selection?.removeAllRanges();
            selection?.addRange(range);
          }
          const top = range.getBoundingClientRect().top - host.getBoundingClientRect().top;
          host.scrollTop += top - host.clientHeight / 2;
          activeId = comment.id;
          for (const item of list.children) item.classList.toggle('md-review-selected', item.dataset.id === activeId);
        });
        position.disabled = !range || busy;
        card.dataset.id = comment.id;
        card.classList.toggle('md-review-selected', activeId === comment.id);
        const note = element('p', 'md-review-text', comment.text);
        const meta = element('div', 'md-review-meta');
        if (!range) meta.append(element('span', 'muted', t(sameVersion ? '找不到批注原文' : '原文版本已变化')));
        if (comment.status === 'resolved') meta.append(element('span', 'muted', t('批注已解决')));
        const buttons = element('div', 'md-review-actions');
        const resolve = button(comment.status === 'resolved' ? '重新打开批注' : '解决批注',
          () => mutate(comment.id, 'PUT', { status: comment.status === 'resolved' ? 'open' : 'resolved' }));
        const remove = button('删除批注', () => {
          if (window.confirm(t('删除这条批注？'))) void mutate(comment.id, 'DELETE');
        });
        resolve.disabled = remove.disabled = busy;
        buttons.append(position, resolve, remove);
        card.append(element('blockquote', 'md-review-quote', comment.anchorQuote), note, meta, buttons);
        list.append(card);
      }
      if (highlights) {
        highlights.delete(activeName);
        highlights.set(highlightName, new Highlight(...ranges));
      }
    }

    async function load() {
      const request = ++loadId;
      try {
        const result = await call('');
        if (!alive() || !review.open || request !== loadId) return;
        comments = result.comments || [];
        paintComments();
      } catch (_) {
        if (alive() && review.open && request === loadId) showStatus('批注加载失败，请重新打开批注');
      }
    }

    async function mutate(id, method, payload) {
      if (busy || !alive()) return;
      const request = network;
      busy = true;
      showStatus('');
      updateSelection();
      paintForm();
      paintComments();
      try {
        await call(id, method, payload);
        if (!alive() || request !== network) return;
        if (method === 'POST') {
          review.anchor = null;
          review.draft = input.value = '';
        }
        await load();
      } catch (error) {
        if (alive() && request === network) status.textContent = error.message || t('批注保存失败');
      } finally {
        if (request === network) busy = false;
        if (alive() && request === network) {
          paintForm();
          if (review.open) paintComments();
          updateSelection();
        }
      }
    }

    form.onsubmit = event => {
      event.preventDefault();
      const anchor = review.anchor;
      const text = input.value.trim();
      if (!anchor || version() !== anchor.hash) {
        showStatus('文档已变化，请重新选中批注原文');
        return;
      }
      if (text) void mutate('', 'POST', { ...anchor, text });
    };

    function setOpen(open) {
      review.open = open;
      reader.classList.toggle('md-review-open', open);
      host.classList.toggle('md-review-host', open);
      toggle.setAttribute('aria-expanded', String(open));
      panel.hidden = !open;
      if (open) {
        showStatus('选中正文，再点击添加批注');
        updateSelection();
        paintForm();
        void load();
      } else {
        highlights?.delete(highlightName);
        highlights?.delete(activeName);
      }
    }

    document.addEventListener('selectionchange', updateSelection, { signal: controller.signal });
    const close = () => dispose(host);
    dialog?.addEventListener('close', close, { signal: controller.signal });
    window.addEventListener('pagehide', close, { signal: controller.signal });
    mounted.set(host, {
      suspend() {
        loadId++;
        network.abort();
        network = new AbortController();
        comments = [];
        candidate = null;
        busy = false;
        review.anchor = null;
        review.draft = input.value = '';
        list.replaceChildren();
        paintForm();
        setOpen(false);
      },
      dispose() {
        network.abort();
        controller.abort();
        highlights?.delete(highlightName);
        highlights?.delete(activeName);
        host.classList.remove('md-review-host');
        reader.classList.remove('md-review-open');
        toggle.remove();
        panel.remove();
      }
    });
    setOpen(review.open);
  }

  window.AideMarkdownReview = { mount, dispose, suspend: host => mounted.get(host)?.suspend() };
})();
