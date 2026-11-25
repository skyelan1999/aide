interface Window {
  AidePluginWorkbenchLauncher?: any;
}
/* Launcher for installed plugins that explicitly declare workbench protocol v1. */
(() => {
  'use strict';
  let refreshPanel: () => void = () => {};
  let closePanel: () => void = () => {};

  function supported(items: any): any[] {
    return (Array.isArray(items) ? items : []).filter(plugin => plugin?.workbench?.protocol === 1);
  }

  function mount(services: any): void {
    const entry = document.getElementById('plugin-workbench-entry') as HTMLButtonElement | null;
    const dialog = document.getElementById('plugin-workbench-panel') as HTMLDialogElement;
    const grid = document.getElementById('plugin-workbench-grid')!;
    const close = document.getElementById('plugin-workbench-close') as HTMLButtonElement | null;
    if (!entry || !dialog || !grid || !close || entry.dataset.mounted) return;
    entry.dataset.mounted = 'true';
    const node = services.el;
    const t = services.t;

    function refresh(): void {
      const items = supported(services.plugins());
      grid.replaceChildren();
      dialog.dataset.pluginCount = String(items.length);
      if (!items.length) {
        grid.append(node('p', 'workbench-launcher-empty', t('暂无支持工作台的插件。')));
        return;
      }
      for (const plugin of items) {
        const name = services.displayName ? services.displayName(plugin.name) : plugin.name;
        const fileFirst = services.fileFirst(plugin.id);
        const actionLabel = fileFirst ? t('从项目文件打开') : t('打开工作台');
        const status = plugin.error ? t('插件载入失败') : !plugin.enabled ? t('已停用') : actionLabel;
        const item = node('button', 'workbench-plugin-item');
        item.type = 'button';
        item.dataset.pluginId = plugin.id;
        item.disabled = !plugin.enabled || !!plugin.error;
        item.setAttribute('aria-label', name + ' · ' + status);
        item.title = plugin.workbench.title || name;
        const hash = Array.from(String(plugin.id)).reduce((sum, char) => sum + char.charCodeAt(0), 0);
        item.classList.add('tone-' + hash % 6);
        const initial = Array.from(String(name || plugin.id || '?').trim())[0]?.toUpperCase() || '?';
        const glyph = node('span', 'workbench-plugin-glyph', initial);
        glyph.setAttribute('aria-hidden', 'true');
        item.append(glyph, node('strong', '', name), node('small', '', status));
        item.onclick = async () => {
          if (services.blocked()) return;
          dialog.close();
          try {
            if (fileFirst) await services.openGuide(plugin);
            else await services.openWorkbench(plugin);
          } catch (error: any) {
            services.toast(error.message || t('操作失败'));
          }
        };
        grid.append(item);
      }
    }

    entry.onclick = () => {
      if (services.blocked()) return;
      refresh();
      if (!dialog.open) dialog.showModal();
    };
    close.onclick = () => dialog.close();
    dialog.addEventListener('click', (event: MouseEvent) => {
      if (event.target === dialog) dialog.close();
    });
    refreshPanel = refresh;
    closePanel = () => { if (dialog.open) dialog.close(); };
    refresh();
  }

  window.AidePluginWorkbenchLauncher = {
    mount,
    supported,
    refresh: () => refreshPanel(),
    close: () => closePanel(),
  };
})();
