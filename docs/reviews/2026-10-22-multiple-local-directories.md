# 多目录与市场修复验收

## 范围与运行环境

源码起点 86d7617c；独立工作树、隔离 Docker 容器 `aide-mount-acceptance-20261022`，全部目录和认证数据均为临时 fixture。生产 localhost:9999 及用户项目没有修改。

## 已验证

- Go 路径规范化、盘符/大小写、边界、重复和重叠、缺失目录不创建；接口认证、CAS、只读工作空间限制。
- 六平台宿主 helper 交叉编译（darwin/linux/windows × arm64/amd64）。
- Chromium 实际设置操作：添加、命名、权限、保存、整页刷新回读；390/1440 明暗主题无横向溢出、无页面异常。
- 实际浏览器 apply 请求经过专用测试 helper 重建隔离 Aide，三目录重新挂载并回读。只有测试二进制替换 OS 确认函数；生产没有跳过 OS 确认的开关。
- Docker 实际只读目录拒绝写入，读写目录可写；目录浏览下拉列出三项并返回正确宿主路径。
- Windows 启动 12 项模拟回归和三脚本 PowerShell 解析通过；launcher-data 38 项及 offline-bundle 13 项通过。
- 独立审查首先登记 GitHub #13，覆盖 prepare 部分写入、mailbox 重放、Compose 回退、helper 停止与重建后的 LAN 转发。修复后测试见最终记录。
- 市场 GitHub #11：刷新策略、失败原因、同步时间、截图边界；Go race 及 1440/390 明暗 Chromium 通过。Shop #1 独立完成兼容修复和 Pages 发布。

## 限制

Windows 原生选择与真实多盘 Docker Desktop、macOS 选择器实际点选、真实外置硬盘拔插未验收。手动编译的 UI fixture 未附发行完整性元数据，不作为 Release 镜像验收。测试不调用付费模型，不包含对抗攻击。

## 回滚

源码提交可还原；期望目录配置保留于数据目录，生效快照和 Compose override 存于宿主 `.aide-data/local-mounts`。应用故障优先使用原启动器与原镜像，避免手动删除用户数据。未向市场或公开 Release 上传商业插件。
