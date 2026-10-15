# 星图引用快速预览

任务：[starmap-file-preview-20261015](../../tasks/starmap-file-preview-20261015.json)。用户明确要求知识星图中的引用支持快速打开文件预览。

原先普通文件详情缺少通用打开入口；“打开源文件”藏在代码面板，“查看原文文件”只在文档检索后出现。AI 文件编号只选择节点。因此这次补回统一预览入口，AI 文件引用点击直接预览，保留原代码／原文／证据路径入口，复用已有只读文件查看器和来源校验。

每个星图最多持有一个预览窗口；不同文件强制完整导航，解决查看器 hash-only 切换不重载的问题。模块检查同源根路径、只读定位、来源字段和工作区，来源指纹本身由后端重查；关闭星图、锁屏及工作区变化清理仍属于自身的窗口，用户已转去的外部／可编辑页面不被关闭或覆盖。未修改 CSP、来源权限、文件写入逻辑或模型协议。

浏览器验收还发现行号定位的原生选区底色遮住透明 textarea 下的语法文字。仅将代码编辑层选区改为 20% 强调色，明暗主题截图确认所选代码可读；行号及只读行为保持。

## 实际检查

- Node 行为回归：真实定位构造器保留来源、格式、行号和代码指纹；窗口复用、首次加载未完成时的连续点击、弹窗阻止、错误定位、锁态／工作区、用户外部／可编辑路由保护。已有路径、实时更新及 AI 测试随新依赖更新，未删减原断言。
- 实际浏览器使用仓库真实星图及完整 `index.html/app.js` 文件查看器，API／SSH 来源为本机夹具。Markdown 标题与粗体真实渲染；代码定位 L3–L5，编辑器只读、保存禁用；跨文件和 AI 编号始终复用同一个预览页。实际请求记录确认来源、工作区、来源指纹传入文件 API。此为协议及前端链路验收，不是真实 SSH 连接证明。
- 专业浅／深色及 390×844 窄屏检查通过，窄屏页面无横向溢出；代码所选行在明暗主题可读。截图 `.agent-state/starmap-file-preview/{entry,markdown-preview,code-preview,code-preview-dark,code-preview-narrow}.png`。先后两组页面均最多同时两个（星图＋预览），关闭星图会自动关闭预览；最终浏览器零自建测试页，恢复视口，夹具服务停止。
- 本机实际 9999 API 使用独立自建 Markdown 文件：来源身份存在，正文 SHA 匹配，错误来源返回 409，没有模型调用；测试文件经摘要核对移回私有证据目录。真实工作台浏览器此前仍受自签名证书阻挡，不能用夹具页面替代该验收；真实 SSH、其他二进制格式仍未在本批重测。

初次 quick 的锁屏写入检查因源码断言要求原有取消语句形状而失败；保持原取消语句及顺序，把预览回收作为独立后续操作，原断言未修改。最后 quick81 通过。后续完整门禁、镜像和主线交付收据以任务账本为准。

源码 `743d0ec` 已通过指定启动器更新 9999 为 `997eda4026b5` 开发候选；健康及七项实际资源逐字匹配，鉴权、配置、挂载保持。完整离线包内外摘要、平台、启动器、实际 docker load、两本地目录副本通过，接收端未验收。见 [Docker 构建记录](../../../docker-images/README.md)；镜像包含继承的五项 vendor 删除，不混称干净 main 镜像。

## 回滚

回退本任务提交可撤销预览模块、入口绑定及选区样式；没有数据迁移。原有文件查看器和引用插入逻辑保留，五个继承的 vendor 删除不属于本任务，不暂存或恢复。镜像与完整离线包保留上一版用于回滚；本批不升版、tag 或创建正式 Release。

## Clean main verification

Snapshot `405df62` passed all 83 checks, including server race (517.89 seconds) and vet. Fingerprint: `d1b82c1c933f3456a601c41e8692a1d202c259de070a40c57ff97dbe6bde6603`. Main gate/push receipts are recorded in the task ledger; this does not expand the browser, SSH or receiving-device acceptance scope.

Main release-check passed; pushed main `05ba4d20db9b93f6836618c4a85163ca24ce6b95` matched the remote. Final ordinary documentation receipt follows without product/build-input changes. No version bump, tag or formal Release.
