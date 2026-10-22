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
- 独立审查首先登记 GitHub #13，覆盖 prepare 部分写入、mailbox 重放、Compose 回退、helper 停止与重建后的 LAN 转发。修复后两包 focused/vet、六平台交叉编译、真实 macOS helper 单实例/去重/同步停止，以及镜像提取校验 → prepare/start/stop 均通过。首次 Python 父进程未及时回收子进程的测试超时，修正测试回收顺序后通过，没有以超时冒充成功。
- 市场 GitHub #11：刷新策略、失败原因、同步时间、截图边界；Go race 及 1440/390 明暗 Chromium 通过。Shop #1 独立完成兼容修复和 Pages 发布。

- 常规符号链接回归首先复现并登记 GitHub #14，再修复：现有路径前缀由 os.Root 检查，工作区/资料通过 Root.OpenRoot 打开；目录被换成外向链接后，已有句柄仍访问原目录，重新打开拒绝越界。新增挂载工作区默认缓存位于 Aide 数据区，显式把额外目录设为缓存会被拒绝。
- 并发设置保存回归先复现并登记 GitHub #15，再修复：准备阶段只读取一次期望配置并校验已确认版本；之后的新保存只保持待应用，不能替换本次快照。
- 前两轮全量回归分别在补 #14/#15 时主动中断，不计为通过；最终检查以重新运行的记录为准。

## 限制

Windows 原生选择与真实多盘 Docker Desktop、macOS 选择器实际点选、真实外置硬盘拔插未验收。手动编译的 UI fixture 未附发行完整性元数据，不作为 Release 镜像验收。测试不调用付费模型，不包含对抗攻击。

## 回滚

源码提交可还原；期望目录配置保留于数据目录，生效快照和 Compose override 存于宿主 `.aide-data/local-mounts`。应用故障优先使用原启动器与原镜像，避免手动删除用户数据。未向市场或公开 Release 上传商业插件。

## 最终全量结果

2026-10-22，对冻结源码 `90404082060782ca5c30186b1bc8addd939868e4` 运行：

```sh
AIDE_TEST_IMAGE=aide:local-pp-planning-20261021 python3 scripts/agent-route.py verify full
```

结果 PASS：147 项流程检查全部 exit 0；服务端 1,127 个顶层 Test/Fuzz/Example 在 12 个互斥分片完整执行，其他 Go 包 race、全项目 vet 和 CAD 几何回归通过。输入指纹 `91b232012028f16509ce06187decfcae16c62b71b75fdfcc0eccd15919175d59`。本地原始记录 `.agent-state/verify-20261022T083332881106Z.log` 和 `.agent-state/full.json`。

这不是 Windows 实机、真实模型 API、远程 SSH 或商业插件完整发行验收。公开宿主源码、商业插件私有源码、本地私有包、公开 Release 与生产安装分别记录；本轮没有重启生产。
