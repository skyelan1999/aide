/* Host-owned guidance; installed documentation remains read-only plain text. */
(() => {
  const fileFirst = new Set(['video-preview', 'step-viewer']);
  const tips = {
    'almanac': '选择日期即可查看；宜忌是民俗资料，节日不等于放假安排。',
    'current-time': '选择显示时区，再点“切换”。时间来自 Aide 主机，当前 SSH 设备可能不同。',
    'lunar-calendar': '点选日期查看公历与农历；方向键也可选日。支持 1901–2099 年。',
    'file-preview': '先在插件设置中配置独立渲染引擎，再选授权来源、文件和页码。原文件保持只读。',
    'mapcanvas': '先导入 YAML 和 PGM/PNG 地图，再编辑。只读来源也可导入；覆盖原图需要写权限。',
    'professional-resume': '可先体验虚构示例，再新建加密资料库。先保存资料库，再选择需要交给 AI 的资料。',
    'rviz': '没有机器人也可先看演示场景。连接真实 ROS 前，按工作台使用指南配置 bridge。',
    'terminal': '点击“创建会话”，选择本地、SSH 或通信连接。关闭页面后连接保留；结束连接需点“结束”。',
    'codex-bridge': '按工作台提示检查宿主组件，再安装并启动桥接；需要已登录的 Codex CLI。连接成功后在模型设置中选择提供商。',
    'video-preview': '先在项目文件中选择视频或剪辑工程，再用“打开方式”选择此插件。首次打开空工作台没有可读取的文件。',
    'step-viewer': '先在项目文件中选择 .step 或 .stp 文件，再用“打开方式”选择此插件。首次转换需要独立 CAD 引擎。',
    'markdown-history': '在项目文件中打开 Markdown，保存修改后从历史版本入口查看和恢复。恢复前先核对差异。',
    'environment-guide': '任务开始时向模型提供工作区和来源说明，无需打开独立面板；不会因此自动读取所有文件。',
    'starlink': '在对话中说明要找的资料、节点或关系路径，模型可用只读星图工具查询。先确认资料已经进入索引。',
    'tts-tools': '在对话中给出明确文案、音色和输出格式，要求生成语音文件。先配置可用的语音提供商。',
    'office': '在对话中附上输入资料，说明要生成或修改的文档、保存位置和验收要求，再核对实际文件。',
    'sqlite': '在对话中指定授权来源内的数据库和查询目标。修改前核对写权限；SSH 快照需要先停止数据库写入。',
    'archive-tools': '在对话中指定授权来源、归档文件和目标目录，说明压缩或解压要求，再核对实际文件。',
    'skill': '在对话中说明任务并请求查找技能。安装为引用前先核对来源和权限，引用不会自动获得写权限。',
    'plugin-authoring': '在对话中描述要制作的插件和验收要求；安装、启停及引用变更仍需核对提案。',
    'browser-control': '在对话中说明网页和操作目标；先配置并授权浏览器访问，再核对真实页面结果。'
  };
  let active;
  function close() { if (active) { active.close(); active.remove(); active = null; } }
  async function open(plugin, services) {
    close(); if (services.blocked()) return;
    const {t} = services, scope = services.scope(), focus = document.activeElement;
    const n = (tag, cls, text) => { const e = document.createElement(tag); e.className = cls || ''; if (text != null) e.textContent = text; return e; };
    const dialog = n('dialog', 'plugin-guide'); active = dialog;
    const heading = n('h2', '', plugin.name + ' · ' + t('使用说明')), status = n('p', 'muted', t('正在读取使用说明…')), body = n('div'), actions = n('div', 'dialog-actions'), dismiss = n('button', 'quiet', t('关闭说明'));
    dismiss.type = 'button'; dismiss.onclick = close; actions.append(dismiss); dialog.append(heading, status, body, actions); dialog.setAttribute('aria-label', heading.textContent);
    // Native dialog handles Escape. Do not let the workbench's Escape handler
    // close the underlying document (or its tab) while dismissing help.
    dialog.addEventListener('keydown', e => e.stopPropagation());
    const timer = setInterval(() => { if (services.blocked() || services.scope() !== scope) close(); }, 250);
    dialog.addEventListener('close', () => { clearInterval(timer); dialog.remove(); if (active === dialog) active = null; focus?.focus(); }, {once:true});
    document.body.append(dialog); dialog.showModal(); dismiss.focus();
    const addAction = (label, fn) => { if (!fn) return; const b = n('button', 'quiet', t(label)); b.type = 'button'; b.onclick = () => { close(); fn(); }; actions.prepend(b); };
    try {
      const guide = await services.api('/plugins/' + encodeURIComponent(plugin.id) + '/guide');
      if (active !== dialog || services.blocked() || services.scope() !== scope) return;
      status.textContent = t(guide.enabled ? '已启用不代表已经配置或执行成功；请按下面的步骤开始。' : '插件已停用。仍可查看说明；启用后再使用其能力。');
      if (guide.error) body.append(n('p', 'task-error', t('插件未能载入。先检查设置、依赖和安装包，再刷新重试。')), n('details', '', ''));
      if (guide.error) { const d = body.lastChild; d.append(n('summary', '', t('查看错误详情')), n('pre', '', guide.error)); }
      if (tips[guide.id]) body.append(n('p', '', t(tips[guide.id])));
      const steps = n('ol');
      const executable = (guide.tools || []).filter(tool => tool.executable);
      if (guide.providerSupport) steps.append(n('li', '', t('在模型设置中配置对应提供商与模型。此插件补充价格或账户能力，没有独立工作台；密钥由 Aide 管理。')));
      if (guide.modelRouter) steps.append(n('li', '', t('先在模型设置中添加可用模型，再选择自动策略。插件帮助选择已有模型，不会提供模型账户。')));
      if (guide.workbench) steps.append(n('li', '', t(fileFirst.has(guide.id) ? '从项目文件中选择受支持的文件，再选择插件打开方式。' : '打开工作台，按页面提示选择文件、连接或示例。需要设置时可从本说明直接进入插件设置。')));
      if (executable.length) steps.append(n('li', '', t('也可在对话中说明任务和输入，让模型调用此插件的工具。工具开关、文件权限和审批仍然生效。')));
      if (!guide.workbench && !guide.providerSupport && !guide.modelRouter && !executable.length) steps.append(n('li', '', t('此包没有独立工作台或当前可执行工具。能力声明不会新增执行器；请查看包内说明，确认它如何与 Aide 配合。')));
      steps.append(n('li', '', t('核对实际结果：文件是否保存、连接是否成功、工具是否返回有效结果。启用状态不能代替这些检查。'))); body.append(steps);
      if (guide.workbench?.fileHandler?.extensions?.length) body.append(n('p', 'muted', t('支持的文件类型：') + guide.workbench.fileHandler.extensions.join(', ')));
      if (guide.tools?.length) { const d = n('details'); d.append(n('summary', '', t('工具与能力详情'))); for (const tool of guide.tools) d.append(n('p', '', String(tool.name || '') + ' · ' + t(tool.executable ? '可执行工具' : '能力声明') + (tool.description ? ' — ' + tool.description : ''))); body.append(d); }
      const readme = n('details'); readme.append(n('summary', '', t('查看安装包内说明'))); readme.append(n('pre', '', guide.readme || t('安装包未附带 README。请根据上述入口开始，或向插件维护者索取说明。'))); if (guide.readmeTruncated) readme.append(n('p', 'muted', t('说明较长，此处仅显示前 128 KiB。'))); body.append(readme);
      addAction('插件设置', () => services.settings?.(plugin));
      if (fileFirst.has(guide.id)) addAction('前往项目文件', services.inWorkbench ? undefined : services.files);
      else if (guide.workbench && guide.enabled && !guide.error && !services.inWorkbench) addAction('打开工作台', () => services.openWorkbench?.(plugin));
    } catch (e) { status.textContent = t('使用说明读取失败，请关闭后重试。') + ' ' + e.message; }
  }
  window.AidePluginGuide = {open, close, fileFirst: id => fileFirst.has(id)};
})();
