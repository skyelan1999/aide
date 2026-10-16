# 启动目录持久化验收 · 2026-10-16

本批将 Docker 命名卷中的核心配置、会话、凭证、索引及工具用户目录迁至启动目录 `.aide-data/storage/`。项目 `.cache` 保持原归属。基线 `e47f2525`，源码提交 `9077870a`，分支 `feat/launcher-data-persistence-20261016`；未升版、打 tag 或创建正式 Release。

## 实现与边界

- 只读旧挂载，逐文件内容 SHA-256/权限/软链接比对，源重复校验，带互斥锁的单次原子目录提交；socket/FIFO/设备不迁移。有效凭据和两份目录缺失时拒绝静默初始化。
- 活动/暂停会话、来源不明、其他容器正在使用旧卷时拒绝迁移。复制失败重新启动原容器；旧卷与失败暂存保留。
- `.env`、镜像标记及 Compose 身份私有备份随启动目录保留，恢复时不执行配置文本。Linux UID/GID 映射及 Windows 私有 ACL 已实现；Windows 原生执行和 NTFS 链接权限未验。
- 核心目录被 Git/镜像构建/普通工作区路径接口/索引及 Agent 清理排除；不将此等同于任意 shell 或宿主管理员的访问隔离。
- 修正 `start.command` 日志进程替换管道被后台进程继承、导致调用端等不到 EOF 的问题；合并日志管道保留实际启动脚本退出码，真实演练确认正常返回。

## 验证证据

1. `python3 scripts/test_launcher_data.py`：10 测试通过，覆盖复制一致性、并发、损坏保留、私有配置文字和镜像身份恢复。
2. `python3 scripts/test_offline_bundle.py`：6 测试通过；`test_source_fingerprint.py` 2 测试通过。
3. [Linux 权限烟测](launcher-data-linux-permissions.json)：无网络/无用户挂载的原生Linux临时文件系统，迁移后UID1456/GID1457可读取私有文件、通过保留的软链接读取及写入数据。此项不代替Windows或完整Ubuntu宿主启动。容器本地源码 Go race：`TestTextRejectBinaryAndUnsafePath` 通过（2.456s）。新增核心路径和嵌套核心路径拒绝。
4. [真实 Docker 演练](launcher-data-docker.json)：最终镜像598，合成数据，活动任务拒绝、失败恢复原容器、校验后删除自身旧卷/容器/.env/.aide-image/.cache，启动器恢复后保护文件内容/权限一致；测试资源已删除，不使用用户凭据。
5. [本机迁移](launcher-data-runtime.json)：启动前14会话、运行/非终态/队列均0；实际 `start.command` 迁移 `/data`156项、用户目录16项。认证/配置/来源/原会话文件摘要不变，其他挂载不变，旧两个卷保留。重复启动命中源码摘要，复用同一实例。
6. [真实 API](launcher-data-api.json)：config、lock-state正常，核心数据路径及嵌套路径400拒绝。一次错误使用GET settings端点得到404，已根据实际路由改用GET config完成验证；无写入。
7. Safari原页刷新，#39及会话列表、千问/DeepSeek提供商和密钥数量、工作区及引用仍可用；关闭设置返回原会话，草稿空，未调用模型或修改配置。保留3个用户标签及固定页，IAB无测试标签。私有截图 `.agent-state/launcher-data-persistence/native-settings.png`。
8. [完整离线包](launcher-data-bundle.json)：内外摘要、双镜像、平台、许可证、新增全部启动辅助文件字节一致及实际docker load通过；未包含用户数据。接收设备导入未验。

## 安全复核与清理

按安全 skill 由当前协调者顺序复核：Go服务端路径拒绝有正/负向测试；新目录不进构建/Git；迁移过程无网络、原源只读，未输出凭证。宿主备份须保护整个目录，不能只拷保险库密文。原卷、958镜像/离线包及旧checkout均保留；未全局prune。仅UUID测试资源及验证中间导入tar被删除。

## 最终发布收据

完整full91通过：`20261016T052610020429Z`，内容指纹 `917eff9090cdbf07468481f95c4acc7cce4927c813dac79cdac7d74fad03ef36`，Go server race667.410s、TTS1.514s。最终main提交/推送以任务账本及追加收据为准；原12工作流继续活动。

Main delivery `50984b99532d579de77bb802103cbed20b241cbc` pushed and read back exactly from origin; clean-main release-check passed. This final receipt changes documentation only; product/scripts/image inputs remain unchanged. Independent gate and push follow this receipt commit. Windows native and receiving-device import remain not_run; legacy volumes and958 package retained.
