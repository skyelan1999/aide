// @ts-expect-error -- classic page scripts expose their own element helper.
function el(tag: any, cls?: any, text?: any) { const e = document.createElement(tag); if (cls) e.className = cls; if (text != null) e.textContent = text; return e; }
const SIDEBAR_WIDTHS_KEY = 'aide-sidebar-widths-v1';
const sidebarWidths: any = { left: null, right: null };
function defaultSidebarWidth(side: any) {
  if (side === 'left') return 238;
  if (innerWidth <= 600) return Math.min(360, innerWidth);
  return innerWidth <= 950 ? Math.min(290, Math.floor(innerWidth * .82)) : (innerWidth <= 1150 ? 235 : 286);
}
function sidebarWidthBounds(side: any) {
  const narrow = innerWidth <= 950;
  const centerMin = narrow ? 350 : (innerWidth <= 1150 ? 380 : 420);
  const otherMin = 220;
  if (side === 'left') return { min: 238, max: Math.max(238, Math.min(380, innerWidth - centerMin - otherMin)) };
  const max = narrow ? Math.floor(innerWidth * .88) : Math.max(220, Math.min(440, innerWidth - centerMin - (sidebarWidths.left || defaultSidebarWidth('left'))));
  return { min: 220, max };
}
function currentSidebarWidth(side: any) { return sidebarWidths[side] ?? defaultSidebarWidth(side); }
function updateSidebarHandle(handle: any, side: any) {
  const width = currentSidebarWidth(side), bounds = sidebarWidthBounds(side);
  handle.setAttribute('aria-valuemin', String(bounds.min));
  handle.setAttribute('aria-valuemax', String(bounds.max));
  handle.setAttribute('aria-valuenow', String(width));
  handle.setAttribute('aria-valuetext', t('{0} 像素', width));
}
function saveSidebarWidths() {
  try { localStorage.setItem(SIDEBAR_WIDTHS_KEY, JSON.stringify({ left: currentSidebarWidth('left'), right: currentSidebarWidth('right') })); } catch (_: any) {}
}
function setSidebarWidth(side: any, value: any, persist = true) {
  const bounds = sidebarWidthBounds(side);
  sidebarWidths[side] = Math.round(Math.min(bounds.max, Math.max(bounds.min, value)));
  document.documentElement.style.setProperty(side === 'left' ? '--aide-left-sidebar-width' : '--aide-right-sidebar-width', `${sidebarWidths[side]}px`);
  const handle = $(side === 'left' ? 'left-sidebar-resizer' : 'right-sidebar-resizer');
  if (handle) updateSidebarHandle(handle, side);
  if (persist) saveSidebarWidths();
}
function resetSidebarWidths() {
  try { localStorage.removeItem(SIDEBAR_WIDTHS_KEY); } catch (_: any) {}
  sidebarWidths.left = sidebarWidths.right = null;
  document.documentElement.style.removeProperty('--aide-left-sidebar-width');
  document.documentElement.style.removeProperty('--aide-right-sidebar-width');
  document.querySelectorAll('.sidebar-resizer').forEach((handle: any) => updateSidebarHandle(handle, handle.dataset.side));
}
function initSidebarResizers() {
  let stored = null;
  try { stored = JSON.parse(localStorage.getItem(SIDEBAR_WIDTHS_KEY) || 'null'); } catch (_: any) {}
  for (const side of ['left', 'right']) {
    const handle = el('div', `sidebar-resizer sidebar-resizer-${side}`);
    handle.id = `${side}-sidebar-resizer`; handle.dataset.side = side;
    handle.tabIndex = 0; handle.setAttribute('role', 'separator'); handle.setAttribute('aria-orientation', 'vertical');
    handle.dataset.i18nAriaLabel = side === 'left' ? '调整左侧工作区栏宽度' : '调整右侧侧栏宽度';
    handle.setAttribute('aria-label', t(handle.dataset.i18nAriaLabel));
    document.body.append(handle);
    if (Number.isFinite(stored?.[side])) setSidebarWidth(side, stored[side], false);
    updateSidebarHandle(handle, side);
    handle.addEventListener('pointerdown', (event: any) => {
      if (event.button !== 0) return;
      event.preventDefault(); handle.setPointerCapture(event.pointerId);
      const startX = event.clientX, startWidth = currentSidebarWidth(side);
      document.body.classList.add('sidebar-resizing');
      const move = (e: any) => setSidebarWidth(side, startWidth + (side === 'left' ? e.clientX - startX : startX - e.clientX), false);
      const finish = () => { handle.removeEventListener('pointermove', move); handle.removeEventListener('pointerup', finish); handle.removeEventListener('pointercancel', finish); document.body.classList.remove('sidebar-resizing'); saveSidebarWidths(); };
      handle.addEventListener('pointermove', move); handle.addEventListener('pointerup', finish); handle.addEventListener('pointercancel', finish);
    });
    handle.addEventListener('keydown', (event: any) => {
      if (event.key !== 'ArrowLeft' && event.key !== 'ArrowRight') return;
      event.preventDefault();
      const delta = (event.key === 'ArrowRight' ? 1 : -1) * (event.shiftKey ? 32 : 12);
      setSidebarWidth(side, currentSidebarWidth(side) + (side === 'left' ? delta : -delta));
    });
  }
  const updateVisibility = () => {
    for (const side of ['left', 'right']) {
      if (sidebarWidths[side] !== null) setSidebarWidth(side, sidebarWidths[side], false);
      else updateSidebarHandle($(side === 'left' ? 'left-sidebar-resizer' : 'right-sidebar-resizer'), side);
    }
    const compact = innerWidth <= 950;
    const rightOpen = compact
      ? document.body.classList.contains('plugins-mode') || document.body.classList.contains('reminders-mode') || $('file-panel')?.classList.contains('mobile-open')
      : !document.body.classList.contains('files-hidden');
    $('left-sidebar-resizer').hidden = innerWidth <= 600;
    $('right-sidebar-resizer').hidden = !rightOpen;
  };
  new MutationObserver(updateVisibility).observe(document.body, { attributes: true, attributeFilter: ['class'] });
  if ($('file-panel')) new MutationObserver(updateVisibility).observe($('file-panel'), { attributes: true, attributeFilter: ['class'] });
  window.addEventListener('resize', updateVisibility);
  window.addEventListener('aide:language', updateVisibility);
  updateVisibility();
}
initSidebarResizers();
// @ts-expect-error -- classic page scripts expose their own toast helper.
function toast(text: any) {
  const host = document.querySelector('dialog[open]') || document.body;
  let notice = $('toast');
  if (!notice) {
    notice = document.createElement('div');
    notice.id = 'toast';
    notice.className = 'toast hidden';
    notice.setAttribute('role', 'status');
  }
  host.append(notice);
  notice.textContent = text;
  notice.classList.remove('hidden');
  clearTimeout((toast as any).timer);
  (toast as any).timer = setTimeout(() => notice.classList.add('hidden'), 5000);
  if (host !== document.body) host.addEventListener('close', () => {
    if (notice.parentElement === host) document.body.append(notice);
  }, { once: true });
}
