# Aide 0.1.16.0 RC1 发布记录

用户授权：2026-10-07“整理一下，release吧”。GitHub prerelease published; all eight remote asset sizes and SHA-256 digests verified.正式服务不在本次替换范围。

## 发布内容

- 输入区“帮我审批”：独立无工具审核确切命令，默认手动，不扩大沙箱；删除、权限变更和不确定操作转人工，审核记录先保存再放行。
- 全局/工作区执行策略与扩展配置：JSON保存、严格校验、导入导出、任务快照；参数修改无需改代码或重新编译。
- Hooks、Skills、自定义子代理、插件同步服务依赖装配，以及本地Git工作树创建/选择/归档。
- 执行前检查点、摘要事件日志、恢复时区分未派发与结果未知；未知副作用不自动重放。人工审批等待不计入任务执行预算。
- 研究来源账本、PDF实际阅读范围、参数来源/计算/假设登记及有界收尾纠正。桨叶CAD/BEMT插件按明确假设生成文件提案和图表。
- 文件面板页眉收紧，配置长路径在768/390像素视口换行，中文/英文界面保留。性格提示词仍进入系统提示，独立审批使用专用规则。

## 验收及边界

已有隔离候选通过原始start.command启动。实际Safari验收设置保存/重载/导入导出/重启保留、工作树操作、人工及辅助审批、英文标签和窄屏布局。辅助审批模拟命令exit0，审核approved、任务completed、文件及0600日志落盘，之后切回手动。

此前完整清单分批race覆盖473顶层测试，540测试/子测试PASS、4环境SKIP；收据属于此前指纹，不能代替当前发布全量门禁。当前源码完整门禁已通过19项：server race373.126秒、tts race17.413秒、go vet exit0；指纹7c0ba932987f7d255c03c85147ed4f7b5037e6cbb1be446fe2775885c5e00820，原始日志.agent-state/verify-20261007T155312516346Z.log。Test-enabled release image build passed; see the receipts below.

实现沿用现有容器/SSH及工具权限，不等于Codex内核沙箱；Hooks和插件不是独立安全隔离进程；子代理同Provider；工作树归档保留目录；恢复不是exactly-once。只验收本地模拟模型链路，不保证真实模型审批判断质量或跨模型结论一致。桨叶模型不是原厂接口尺寸精确复刻；BEMT不是CFD，不能当绝对噪声、耗电或续航验证。

## 交付及回滚

Published using the original version/build/package/publish scripts. [GitHub prerelease](https://github.com/skyelan1999/aide/releases/tag/v0.1.16.0-RC1): draft=false, prerelease=true; all eight assets uploaded and remote size/digest checks passed. Receipt: .agent-state/release-remote-verified-0.1.16.0-RC1.json. The immutable release tag remains7da5278; the subsequent documentation commit records publication results only.

## 本次发行收据

- 标签：v0.1.16.0-RC1；提交：7da52784a7a547e5ef590222bff02a000e31516d；main与标签已原子推送。
- 镜像开启AIDE_RUN_TESTS=1：server测试215.129秒、tts2.351秒、go vet与编译成功，脚本healthz PASS。此前“镜像构建待执行”状态由此更新。
- 镜像ID：sha256:605d6cd94cef2ef73a8bca0d4515a632c652d8b3ef42711ced569e89997cf31f；源码指纹：71ea13c81b9a5f970f9c2c32d4e705cc989474969c842e171ab116793de47beb。
- 原始start.command启动隔离发行候选18189；本地叶证书作为明确的信任锚，保留证书与主机名验证。healthz200、未认证config401、Bearer认证config/执行策略/harness配置200，buildCommit与标签提交一致。
- Safari实际显示v0.1.16.0 RC1与输入区手动审批入口；未绕过浏览器安全警告。
- 八项标准附件：ARM64镜像、full/update ZIP、macOS/Windows/Ubuntu ARM64启动包、RELEASE-ASSETS.txt、SHA256SUMS。七项载荷SHA-256全部通过，ZIP清单确认无data、workspace、user.env。未交付x64镜像。
- 原始收据：.agent-state/release-build-0.1.16.0-RC1.log、release-package-0.1.16.0-RC1.log、release-smoke-0.1.16.0-RC1.json；Safari截图：/private/tmp/aide-harness-ui-evidence/release-0.1.16.0-RC1.png。

正式实例维持原版本；上一发行v0.1.15.0-RC3保留可用。以后部署前备份数据，回退选用旧发行镜像并保留同一数据挂载；本次不自动迁移或删除用户会话/工作树。
