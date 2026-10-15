# 真实隔离 SSH 授权生命周期

任务：[ssh-approval-lifecycle-20261015](../../tasks/ssh-approval-lifecycle-20261015.json)。继续原 12 项中的审批／授权及真实环境验收，未用本批替代浏览器和用户真实主机要求。

## 验收设计

在既有 `TestSSHLiveWorkspaceAndIndependentSource` 中加入 `approval_lifecycle_real_ssh`。两个一次性 OpenSSH 服务位于 internal-only Docker 网络，不发布主机端口、不挂载用户文件。调用实际 `approvedShell`、鉴权 Handler 的应答和规则管理接口；人工应答由测试调用模拟，未使用模型，也不是浏览器点击。准备／读回通过独立 SSH 命令核对目录是否存在，执行路线本身不能用准备命令替代。

- 首次等待明确确认前，远端目录不存在；确认并记忆后实际创建。移除自建目录后，在另一个会话复用完整同一命令，无需再次确认并实际创建，成果接口有持久 remembered 回执和 rule ID。
- 同一授权不能越过 read-only 或远端不支持内核沙箱的 workspace-write；远端目录仍不存在。
- 两台服务器使用完全相同的绝对目录，保持命令不变，只变化主机，须重新询问；同一主机改为另一个根目录也须询问。拒绝后均无远端创建。
- 管理接口修改有效期后，用旧 revision 撤销返回 409；按真实时钟等到期限，再次执行须确认。重新授权后撤销，重载磁盘策略仍为空，再次执行须确认；拒绝后无远端创建。

所有路径都在一次性服务中，未改当前 9999 的工作区、审批规则、沙箱配置或用户远端目录。本批仅新增 Go 集成验收，没有产品行为修复。

## 实际结果

最终命令：`AIDE_SSH_RUNTIME_IMAGE=aide:local AIDE_SSH_RACE=1 AIDE_SSH_REPEAT=1 AIDE_SSH_TEST_EVIDENCE_DIR=.agent-state/ssh-approval-lifecycle/live bash scripts/test-ssh-workspace.sh`。整套真实双服务器联调 PASS，race 包耗时 19.349 秒，新增授权子项约 3.4 秒。原有文件／来源、历史恢复、知识索引／时间线、缓存、进程取消等也通过；源输入摘要前后相同，临时容器和网络由脚本回收。

此前定向和初次整套运行均通过；随后把跨主机案例的路径固定为相同值，排除主机与目录同时变化的混杂，再跑上述最终整套。没有把中间版输入当作最终收据。原始日志、输入摘要与汇总位于 `.agent-state/ssh-approval-lifecycle/`，最终日志 SHA-256 为 `09fd40d24b2ac40efa4ea1f5506e6b1a0ceccb7e374664bef54e8b4bb12ff6c8`。

本机起初没有验收镜像 `aide-source-fixtures:test`，已按仓库现有 Dockerfile 恢复。此测试依赖的初次构建读取软件包仓库，镜像之后本地复用；不因此声称其他设备已有该验收依赖或全套测试能离线运行。当前应用镜像已经包含 Go，显式用它作为验收客户端，未拉取缺失的独立 golang 镜像。客户端、两台服务和网络均隔离，缓存镜像／编译缓存保留供后续复用。

## 剩余与交付

完整回归期间还运行了现有 `TestSourcesLiveProtocols`：local、skill、HTTP、FTP、FTPS、SFTP、SMB 七项均通过，race 包耗时 10.417 秒。使用实际协议服务及鉴权 Handler，覆盖注册、列目录、读取、相应写入／拒绝路径及工具内容返回；模型端使用确定性测试服务，没有付费调用。FTPS 通过本次进程的 CA 文件校验证书；SMB 为隔离测试服务的 NT1 配置，不代表用户主机或 SMB2/3 已验收。输入摘要前后相同，服务／客户端／网络已回收。日志 SHA-256：`5de7881abe02dd653196f017fdeb74ad2088e5363d222bac0be24516aad72679`。本项不证明全库索引、策略或浏览器交互通过。

实际用户 SSH 主机、Safari 的并发撤销／过期管理交互、逆向模式即时同步以及其他原始工作流继续。已请求用户按电脑控制工具要求手动打开 Safari 并处理证书页；未代点告警、安装信任证书或绕过浏览器保护。

测试文件属于构建指纹输入，后续快检、干净 full、候选镜像及完整包／main 门禁分别记录到任务账本，不用本节定向 race 冒充完整门禁。回滚仅需撤销本批验收代码；不迁移数据、不升版／tag／正式 Release，保留上一镜像包。

## 完整回归收据

干净 main 工作树基于 `529d951` 完整 84 项 PASS，Go server race 530.379 秒，vet 通过。输入指纹 `2176a299197ee3b8bee85383d4bd5f2a1ad2f5002c487ebd1ee165efb3734edf`；原始收据私有保留。本轮未升版、打 tag 或创建正式 Release。当前 9999 和完整离线包为 `b5a8b5cba520` 开发候选，包含原目录继承的五处 vendor 删除，不能称为干净 main 构建；这些删除不纳入提交。接收设备导入、用户 SSH 主机和完整浏览器验收仍待完成。

干净 main 的 release-check 已通过，`3fa5b1a8e80c7823fab801e817572b9865792467` 已推送，独立 ls-remote 确认远端 main 一致。本条收据更新只有普通文档，使用相同输入的 full84 收据复核，不声称重新运行完整测试。原 12 项继续。
