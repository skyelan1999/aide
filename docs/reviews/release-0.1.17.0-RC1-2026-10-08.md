# Aide 0.1.17.0 RC1 发布记录

状态：**已发布 GitHub prerelease；本地完整验收、实际发行包启动及八项远端附件核对通过**。标签 `v0.1.17.0-RC1` 已推送；版本提交与发行镜像固定一致。任务：[release-20261008](../tasks/release-20261008.json)。

用户授权：2026-10-08“整理所有资料和文档并release”；本轮后续答复明确允许运行完整验收并发布。沿用用户指定的 `start.command` 启动入口。目标为 GitHub prerelease 与八项 ARM64 标准附件；不替换用户正在使用的生产实例，不自动部署 Pages。

## 发布范围

本次整理覆盖 `v0.1.16.0-RC1` 之后的知识星图、界面与审批入口，以及 SSH 修复、多引用来源、Office 检索配合和独立产品主页。起始工作树提交为 `912cc06f49167f6789f1007547fc0b128f7dbfa0`；此前已提交的星图／界面变更为 `e33fff77f0d743016a299cdd0fb956c5516b1c5b`。这些普通源码提交不是新的版本标签或发行镜像。

| 内容 | 本次行为 | 说明 |
| --- | --- | --- |
| 审批入口 | 聊天输入框策略菜单单独审批栏，按钮摘要显示手动／辅助审批 | 延续独立审核具体命令；默认手动、计入模型用量，不扩大沙箱和插件权限 |
| 知识星图 | 文件页眉图标打开独立页面，直接显示星图；删除入场／重播／手动刷新 | 自动按稳定编号合并新增、修改、移出节点和关系，保留有效星位、镜头与选择；保留持续动效、归位与减少动态效果 |
| 星域与星等 | 同一知识宇宙内按真实下级数量形成更大结构，星等随静态调用候选层变化 | 装饰星尘不计入知识数量；目录深度不代替调用层，单一组不冒充多个星系 |
| 代码理解 | Go AST、JavaScript Acorn、Python AST 的声明、导入和调用候选 | 查看调用者／被调用者、编号和源码行号；不执行项目代码或完整类型检查 |
| 文档检索 | 原文逐字匹配与本地 TF-IDF 片段排序；点击 AI 再生成引用回答 | 片段有编号、定位、出处与原文件指纹，回填至原会话草稿，不自动发送 |
| 多引用来源 | 本地／Skill、SSH工作区、自动系统文档、独立SFTP、HTTP／HTTPS、FTP／FTPS、SMB按来源分区 | MCP只索引已发现的工具说明；状态、来源名称／类型／编号与当前身份分别保留 |
| 读取及查看 | 图谱、原文、RAG、Office文档搜索和星图只读查看共用有界适配 | 来源改配、停用、移除或内容变化后拒绝旧引用；虚拟文件按实际格式进入现有查看器 |
| SSH目录与生命周期 | 串行建立／关闭master、连接代次、临时凭据清理、SFTP诊断 | 明确断线的只读目录查询重连一次；写入、提案与命令不自动重放 |
| 远端命令与提案 | 保留真实退出码、取消进程组及清理回执；提案网络I/O释放状态锁并固定连接代次 | 文件应用仍逐文件原子处理，多个文件不是整体事务 |
| 文档与缓存路径 | 远端绝对路径从服务器根解析，相对路径从工作区解析；独立来源凭据部分更新保留旧vault字段 | 工作区一套SSH；自动系统文档／项目缓存远端复用该服务器，辅助资料可在其他SFTP服务器 |
| 界面和加载 | 共享语义配色、短交互动画、后台暂停、按需加载Mermaid与三维库、代码编辑器合帧 | 静态资源ETag校验；API／HTML／工作区文件仍no-store；不声称全设备帧率或眼健康效果 |
| 独立产品主页 | `site/` 静态HTML/CSS/JavaScript、本地素材、安装和文档入口 | 无npm/CDN或应用API；Pages仅手动工作流，源码推送不自动部署 |
| 文档整理 | README、用户指南、文档中心、架构专题、主页说明及交接入口对齐 | 旧验收报告和原始日志保留当次范围，未将编译／截图当成当前全部通过 |

详细配置、预算及实现链路见[知识星图架构](../architecture/knowledge-map.md)、[SSH与存储配置](../architecture/ssh-storage.md)、[执行策略](../architecture/execution-policy.md)、[主页说明](../website.md)和[用户指南](../user-guide.md)。

## 验收与证据

下表仅在本轮收据实际产生并核对后更新。历史 SSH、Safari 和候选编译结果用于定位范围，不代替当前发布门禁。

| 检查 | 当前状态 | 证据与范围 |
| --- | --- | --- |
| 原聊天取消测试fixture修复 | PASS（focused） | 既有 `TestChatHistoryAndCancelEndpoint` 按user prompt确定mock响应并先取消再关闭；禁网Linux/ARM64容器 `-race -count=10`，10次PASS、9.413秒，输入前后相同；`.agent-state/release-gate-audit/cancel-focused-race.json`。此前SSH报告的基线超时由此闭环，不能因此声称全量通过 |
| 完整quick／Go race／vet发布门禁 | PASS（修复后最终） | 19项均exit0，server race317.793秒、tts3.100秒、vet通过；指纹 `d331b9d9c719d16a0950e05a70695aed98565c7868d131caa26bad874530a7c2`，`.agent-state/full.json`、`verify-20261008T063234683227Z.log`。修复前两轮收据保留为历史，不替代当前结果 |
| 当前SSH／多来源功能验收 | PASS（修复后隔离验收） | 双真实SSH28/28 race，15.848秒；活动/超时socket8/8 race；多来源50+12+35=97/97；两次stop/start.command重启30/30。19项来源配置持久化，旧origin拒绝。见[联调报告](2026-10-08-release-integration.md)，不代替用户服务器验收 |
| 当前工作台／星图浏览器验收 | PASS（Safari桌面核心范围）；窄屏NOT_RUN | 审批四栏默认手动、直接新tab星图、来源状态、F编号草稿回填未发送、原文只读查看、71→72→71差量及镜头视觉保留通过；IAB证书错误未绕过，Aide窄屏／全部主题／量化性能／付费模型未测。见[UI报告](2026-10-08-release-ui.md) |
| 独立主页浏览器验收 | PASS（主验收）；两项探索NOT_RUN | IAB HTTP18200，1280×900／390×844／320×844无横向溢出、图片加载及锚点；菜单／Escape焦点／导航、场景键盘、实际clipboard matches:true、4FAQ通过。禁用JS与OS减少动效未运行，仅静态检查；[UI报告](2026-10-08-release-ui.md) |
| 文档整理检查 | PASS（本轮普通文档整理） | `python3 scripts/check_docs.py`：166 Markdown／406本地链接、JPEG签名与FR-01..100覆盖；`git diff --check -- docs` exit 0。普通docs更新不替代补充源码修复后的完整门禁 |
| main清洁、release-check、升版／tag／推送 | PASS | 功能提交9c755e3，version.sh产生8a2ddb21a389644c2b57e11356a673b9fafd3a51及不可变标签；最终校验和纳入完整门禁后release-check通过；main及tag已原子推送 |
| 开启测试的发行镜像构建 | PASS | AIDE_RUN_TESTS=1，全量Go test（server126.558秒、tts2.256秒）及vet通过；HTTPS healthz通过；镜像及源码摘要见下 |
| `start.command`隔离发行启动及HTTPS／鉴权冒烟 | PASS | 完整ZIP保留原始解压副本，在独立工作副本用原启动器导入包内镜像；端口18201，独立项目aide-release-0170。固定信任证书、校验主机名；healthz正常、未鉴权401、鉴权200、版本与buildCommit匹配；13节点11关系；Safari版本／审批四栏／直接星图通过 |
| 八项附件本地完整性与包内隐私检查 | PASS | 八项齐全、七载荷SHA256一致、五ZIP CRC及隐私检查通过、镜像／manifest匹配；本地收据local-assets-receipt.json |
| GitHub prerelease发布与八项远端附件核对 | PASS | draft=false、prerelease=true；八项远端uploaded，逐项字节数及GitHub SHA256 digest与本地收据一致 |
| 生产替换／Pages部署 | NOT_RUN | 本轮不替换生产；Pages未自动部署 |

多来源原始收据：`.agent-state/release-20261008/integration/summary.json`（50）、`baseline-summary.json`（12）、`formats-summary.json`（35）及各对应API回执；SSH日志为同目录 `ssh/aide-ssh-test-20261008134050-15648.log`。隔离候选和夹具不使用用户服务器或付费模型。

本地原始日志、收据和测试数据位于忽略目录 `.agent-state/`。本记录保存可审阅结论；不发布访问令牌、API Key、用户服务器连接参数或真实会话。

## 已知边界

- 图谱最多800文件／200目录，按启用区域分配；空区域配额暂不转移。远端快照约30秒复用，最多3个并行扫描、单来源4秒／批次6秒。状态partial／unavailable与保留旧星点不代表本次读取成功；移出索引也不等于证实文件删除。
- HTTP／SMB仅配置单资源，不递归爬取；FTP列表格式、服务器权限与网络兼容性依实际验收范围。MCP目录只含此前发现的工具说明，不代表工具结果或远端资料库。
- 来源身份随配置／凭据变化，进程重启后旧只读查看页需重新打开。SFTP类型检查与下载不是服务器侧快照；原文件SHA-256和定位校验不提供事务一致性。
- 文档没有OCR、向量模型或持久化知识库；AI会发送所选摘录并计费。模型引用质量、审批判断和跨模型结论不能仅由模拟模型或API测试证明。无扩展名PDF按文件头识别，无扩展名Office不推断格式；未新增PPTX查看器。
- 代码调用为静态候选，缺少完整类型检查／运行轨迹；TypeScript／TSX／JSX等有解析缺口。无入边不证明未使用。
- 工作区A、辅助文档B、缓存A或本机可配置；原生“工作区A／自动系统文档B／项目缓存C”未实现。会话、应用凭据与文档提取缓存不随项目缓存迁移。
- 普通SSH浏览与编辑仍有连接切换窗口；用户服务器账号、目录权限、掉包及长空闲不由隔离双服务器验收替代。来源及知识查看链路的身份守卫不等于全部远端I/O完成代次隔离。
- 本次仅交付linux/arm64镜像。macOS Apple Silicon、Windows ARM64、Ubuntu ARM64启动包不等于在三种真实宿主设备都完成启动；未提供amd64／x64镜像。macOS ZIP尚无Developer ID签名与公证。

- 当前Aide桌面通过不等于窄屏通过；主页窄屏是另一静态服务。全部主题／编号回填、无JS／OS减少动效运行及真实模型质量未全部验收。

## 八项标准附件

已使用 `scripts/package-release-assets.sh` 生成并通过本地完整性检查，八项远端字节数与SHA256 digest已逐项一致。`SHA256SUMS`覆盖其余七项载荷，包内另含镜像校验和与升级manifest。

| 文件 | 用途 | 当前状态 |
| --- | --- | --- |
| `aide-v0.1.17.0-RC1-linux-arm64-image.tar.gz` | Linux/ARM64 Docker镜像归档 | 本地／远端PASS |
| `aide-v0.1.17.0-RC1-full-linux-arm64.zip` | 完整运行／升级包：三平台启动器与一份镜像 | 本地／远端PASS |
| `aide-v0.1.17.0-RC1-update-linux-arm64.zip` | 应用内升级manifest、镜像及校验和 | 本地／远端PASS |
| `aide-v0.1.17.0-RC1-macos-arm64.zip` | Apple Silicon启动包；入口start.command | 本地／远端PASS |
| `aide-v0.1.17.0-RC1-windows-arm64.zip` | Windows ARM64启动包；入口start.ps1／start.bat | 本地／远端PASS |
| `aide-v0.1.17.0-RC1-ubuntu-arm64.zip` | Ubuntu ARM64启动包；入口start.sh | 本地／远端PASS |
| `RELEASE-ASSETS.txt` | 产物、平台和使用说明 | 本地／远端PASS |
| `SHA256SUMS` | 七项载荷的SHA-256 | 本地／远端PASS |

## 本次发行收据

- 标签：`v0.1.17.0-RC1`。版本提交：`8a2ddb21a389644c2b57e11356a673b9fafd3a51`；main及tag已原子推送。
- 镜像ID：`sha256:2e7b3584fd3f605fe0fc4cb90a52f36f0b680cc681785926833d8907164c6bbe`；linux/arm64；源码摘要：`c2b0719d9ff7b330bcc2550d72cb84be03ccda2ec61caa27d82b371eb0fcf306`。构建日志：`.agent-state/release-20261008/build-0.1.17.0-RC1.log`。
- 版本注入前隔离候选：`start.command`、端口18201，二进制 `5f06c39b7060330fec0267e664bb149112160de68cac3c12c33cd0b7e7c5df98`；协议/API使用固定本地信任证书并校验主机名。它没有发行版本和提交注入；Safari桌面与静态主页本轮验收见UI报告；升版后的实际发行包启动／鉴权／Safari桌面核心验收已PASS；[工作台截图](2026-10-08-release-ui/final-package.png)及[星图截图](2026-10-08-release-ui/final-package-starmap.png)。
- 本地附件：`.agent-state/release-assets/v0.1.17.0-RC1/`；本地收据：`.agent-state/release-assets/local-assets-receipt.json`；八项远端字节数与SHA256 digest已逐项一致。
- GitHub prerelease：[https://github.com/skyelan1999/aide/releases/tag/v0.1.17.0-RC1](https://github.com/skyelan1999/aide/releases/tag/v0.1.17.0-RC1)；draft=false、prerelease=true；原始远端API回执与核对收据位于 `.agent-state/release-20261008/remote-release.json`、`remote-assets-receipt.json`。
- 正式生产实例：本轮不替换；独立Pages：未部署。

## 交付与回滚

验收、升版、构建、打包、推送和远端资产门禁均已通过，使用已有脚本发布。上述GitHub地址已经实际API核对，不以预计URL作为交付证明。

保留上一发行 `v0.1.16.0-RC1` 与原运行服务、挂载目录和数据卷；本轮候选仅使用自己的隔离数据。后续部署前分别备份工作区和应用数据；回退选择旧发行镜像与原挂载，不删除数据卷或以旧镜像覆盖唯一数据。原始入口仍为 `start.command`，版本升级不是仅刷新浏览器页面。

Pages可独立撤销部署或关闭站点，不影响本地工作台和会话。具体回滚动作在执行后另行记录，本说明不表示已部署或已回滚。

### SSH重启补充闭环

控制检查失败后，仅在请求未取消、Unix socket连接明确被拒绝且inode/mtime未变化时清理残留。修复后28项双服务器race、8项活动/超时保留、97项多来源API、两轮共30项实际stop/start.command重启检查均通过。候选二进制SHA-256 `b0ee07874a88ed119071d100b893c3735ff1af4a0538c9568b2a3bb488d59a1d`；最终带版本发行镜像另行验收。

## 本地资产身份

| 文件 | 字节 | SHA-256 |
| --- | ---: | --- |
| `RELEASE-ASSETS.txt` | 522 | `203b1611c32850f0d08f2e6ced34e260973e26176403d75f3007dd2ede33ecbd` |
| `SHA256SUMS` | 725 | `6fd79d06b0494b0294dce456ff725d90d7323191533148b2fde8c3cf3bcfb7ec` |
| `aide-v0.1.17.0-RC1-full-linux-arm64.zip` | 552843760 | `016d9f6cf9929b558cf7e9efc46b9565528341b5e6efc5e655777a4da9b05098` |
| `aide-v0.1.17.0-RC1-linux-arm64-image.tar.gz` | 552720535 | `91e846865f6789edb8fd58cf41eaf70452a3552daf6691aa4597969903f46dad` |
| `aide-v0.1.17.0-RC1-macos-arm64.zip` | 39783 | `8ae0f666c652ea44c2873d364f0ed22888d4e4a4b5a4a64e4314c11bed9b8b25` |
| `aide-v0.1.17.0-RC1-ubuntu-arm64.zip` | 40115 | `7ffb40e89178eb7d04177820d16f4a07bab50b76309bc43ab691dc5e4b934ca3` |
| `aide-v0.1.17.0-RC1-update-linux-arm64.zip` | 552787703 | `def1dd905998f148b6e4a4272a89fc1675888f56b9ee33d22fa6b8e7fd3d4c0f` |
| `aide-v0.1.17.0-RC1-windows-arm64.zip` | 49651 | `4748956a3e12b12994c911136e233ad021672fb7150c16c6ebef8373ff537135` |

## 打包后的最终门禁

发行构建更新了已跟踪的 docker-images/SHA256SUMS，因此又执行一次完整19项门禁；.agent-state/verify-20261008T063234683227Z.log，当前指纹 `d331b9d9c719d16a0950e05a70695aed98565c7868d131caa26bad874530a7c2`，所有命令exit0。这份收据覆盖最终校验和输入，前述三轮保留历史结果。标签仍固定版本提交8a2ddb21a389644c2b57e11356a673b9fafd3a51，后续仅追加文档及截图，未重写标签。

隔离发行包服务仍在 https://localhost:18201 供本机查看；未配置模型，专用aide-release-0170项目和数据卷。停止命令：在 `.agent-state/release-20261008/bundle-runtime/` 使用 `COMPOSE_PROJECT_NAME=aide-release-0170 docker compose down`；不加-v，不涉及原实例。静态18200预览已停止，协议夹具已清理。
