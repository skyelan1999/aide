'use strict';

const menuButton = document.querySelector('.menu-toggle');
const navigation = document.querySelector('#navigation');

function setMenu(open, restoreFocus = false) {
  navigation.classList.toggle('open', open);
  menuButton.setAttribute('aria-expanded', String(open));
  menuButton.setAttribute('aria-label', open ? '关闭导航' : '打开导航');
  if (restoreFocus) menuButton.focus();
}

menuButton.addEventListener('click', () => {
  setMenu(menuButton.getAttribute('aria-expanded') !== 'true');
});
navigation.addEventListener('click', (event) => {
  if (event.target.closest('a')) setMenu(false);
});
document.addEventListener('keydown', (event) => {
  if (event.key === 'Escape' && menuButton.getAttribute('aria-expanded') === 'true') {
    setMenu(false, true);
  }
});
document.addEventListener('click', (event) => {
  if (!event.target.closest('.header')) setMenu(false);
});
const desktopQuery = window.matchMedia('(min-width: 761px)');
desktopQuery.addEventListener('change', (event) => {
  if (event.matches) setMenu(false);
});

const tabs = Array.from(document.querySelectorAll('[data-scenario]'));
const panels = Array.from(document.querySelectorAll('.scenario-panel'));

function activateTab(tab, focus = false) {
  for (const item of tabs) {
    const selected = item === tab;
    item.classList.toggle('active', selected);
    item.setAttribute('aria-selected', String(selected));
    item.tabIndex = selected ? 0 : -1;
  }
  for (const panel of panels) {
    panel.hidden = panel.id !== tab.getAttribute('aria-controls');
  }
  if (focus) tab.focus();
}

tabs.forEach((tab, index) => {
  tab.addEventListener('click', () => activateTab(tab));
  tab.addEventListener('keydown', (event) => {
    let next;
    if (event.key === 'ArrowDown' || event.key === 'ArrowRight') next = (index + 1) % tabs.length;
    if (event.key === 'ArrowUp' || event.key === 'ArrowLeft') next = (index - 1 + tabs.length) % tabs.length;
    if (event.key === 'Home') next = 0;
    if (event.key === 'End') next = tabs.length - 1;
    if (next !== undefined) {
      event.preventDefault();
      activateTab(tabs[next], true);
    }
  });
});

const copyButton = document.querySelector('#copy-command');
const command = document.querySelector('#install-command');
const copyStatus = document.querySelector('#copy-status');
let copyReset;

copyButton.addEventListener('click', async () => {
  clearTimeout(copyReset);
  const text = command.textContent.trim();
  try {
    if (!navigator.clipboard || !window.isSecureContext) throw new Error('Clipboard unavailable');
    await navigator.clipboard.writeText(text);
    copyButton.querySelector('span').textContent = '已复制';
    copyStatus.textContent = '安装命令已复制';
  } catch {
    const range = document.createRange();
    range.selectNodeContents(command);
    const selection = window.getSelection();
    selection.removeAllRanges();
    selection.addRange(range);
    copyStatus.textContent = '命令已选中，请手动复制';
  }
  copyReset = setTimeout(() => {
    copyButton.querySelector('span').textContent = '复制';
    copyStatus.textContent = '';
  }, 4500);
});
