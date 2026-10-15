# Docker 镜像交付

## 2026-10-15 旧任务重试与成果发布记录修复候选（构建时记录）

- 源码8196baf／09e0d89：旧任务重试限定原输入之前的历史，保留原模型选择并重建当前权限；压缩快照不重复中断正文。成果发布导入采用读接口同一份裁剪快照，避免正常fresh token被误判409。最终定向race19.335秒通过；真实Qwen三任务完成／隐藏活动元数据和无工具调用在183候选验证，最终a082二进制导入／幂等／旧token／跨会话／来源／磁盘读回通过。两阶段范围见[任务](../docs/tasks/outcome-hidden-active-live-20261015.json)。
- 指定start.command更新9999为 `sha256:a082bd336de11ec5537bf991637637399a2f6dadc653e70b5a2807a35ea18d54`，source SHA `91ccfe6d399d5e707d6397f279404ccc6a162dce111f33acd2fddf247f090083`；当前受控源码干净，原13会话／配置／鉴权／挂载及资源／健康保持。所有开发与镜像包操作均在移动硬盘，不向旧目录重建重复归档。
- 完整离线包 `aide-0.1.17.0-RC2-linux-arm64-a082bd336de1-offline.tar.gz`，705745837字节，SHA-256 `430df6013c38dd2d82f113c5648ded4f812fa101cd177f25fb168c2775fbafa0`。应用／依赖镜像、内外摘要、平台、启动器及实际docker load通过；接收设备导入待验收。已删除本轮自己生成且被最终版取代的183临时包，原9bc等既有回滚包保留。
- 本记录写入时，最终干净full／main门禁待收据，完整Safari按用户安排暂缓，Windows用户验收，真实向量暂缓；不升版、tag或正式Release。

## 2026-10-15 历史对象完整性修复候选（构建时记录）

- 源码 `f6ca3e1`：历史对象复用前验证普通文件、大小和SHA-256，拒绝损坏对象／软链接，保留原文及故障档案。定向Go race11.264秒、quick82、新版实际HTTPS故障／恢复重试／进程重启4.322秒通过；完整full及main门禁后续见[任务账本](../docs/tasks/history-object-integrity-20261015.json)。
- 指定start.command更新9999，13份会话及配置／来源／鉴权／挂载保持，六项网页资源和健康匹配。应用 `sha256:9bc517eba16fa5c713a05ef552ec24d53b4080e4f26e32971e3cf3edb12134cd`，source SHA `6766b920c58364fcce05f1297aac432ee135126f6ee59f3fc4aa23b22c1289ca`；运行候选继承五处vendor删除，不冒充干净main镜像。
- 完整离线包 `aide-0.1.17.0-RC2-linux-arm64-9bc517eba16f-offline.tar.gz`，705683435字节，SHA-256 `d9cc4b6642b44ca3409a30e180bb170ae92735116d7d32712bcf38124020ee62`；应用／依赖镜像、内外摘要、平台、启动器及实际docker load通过，并校验复制到aideMacMini对应本地交付目录。移动硬盘已校验副本保持原样；接收设备导入仍待验收。上一 `b8769857af35` 包保留回滚。
- 完整已鉴权Safari按用户当前安排暂缓，Windows用户验收，真实向量暂缓；不升版、tag或创建正式Release。

## 2026-10-15 正文保存／历史归档部分成功修复候选（构建时记录）

- 源码 `1c34755`：文件PUT明确区分正文已写入后的归档失败，编辑器接纳新哈希并保留历史警告／重试，保护更新的草稿和上下文；两入口不弹普通成功提示。定向Go race10.132秒、quick82、共享JS／锁屏回归与独立浏览器组件交互通过；组件API为夹具，不替代完整Safari，用户表示现在不方便，完整工作台暂缓。
- 指定start.command更新9999为 `b8769857af35`，13份会话及配置／来源／鉴权／挂载保持，六项资源和健康通过。应用 `sha256:b8769857af357d1aa03342e9d1da3e440b180d113f6018edb345d8585d068201`，source SHA `9197c7ad58c3d8ce952fdb03eca167205023cad0a71f4f82b4e094c8534988ec`。继承五处vendor删除未提交，运行镜像为开发候选，不混称干净main镜像。
- 实际新版HTTPS／8MiB tmpfs ENOSPC／权限／恢复／timeline／进程重启组合探针PASS11.173秒，确认仅后写入归档错误携带saved／hash元数据。完整包 `aide-0.1.17.0-RC2-linux-arm64-b8769857af35-offline.tar.gz`，705686931字节，SHA-256 `66e4a78f6587aebc781f858bbb7c6bdd21efd90b37bd26e89468e36009eb9056`；内外摘要、平台、启动器、应用／依赖镜像、实际load及两本地副本通过，接收端待验收；上一 `0e29f3cafa29` 保留回滚。
- 此记录冻结时新full84／main门禁待补，以[任务账本](../docs/tasks/history-partial-save-20261015.json)最终收据为准。无升版／tag／正式Release。

## 2026-10-15 代码关系词法绑定与接口分派修复候选（构建时记录）

- 源码 `3ed74f8`、`9b6bda6`：JS／Python 调用先检查作用域与遮蔽；Go 类型服务明确识别的接口分派不再回退连接同名具体方法。最终定向 race3.275秒、quick82及真实 SFTP／HTTPS 代码与路径／原文哈希探针通过。
- 指定 `AIDE_OPEN_BROWSER=0 bash ./start.command` 更新9999，13份会话及配置／鉴权／来源／挂载保持。应用 `sha256:0e29f3cafa29f303f9163742acb573b6d174a380514272f0023e7214b91aca2f`，source SHA `82e8b68fe0c7970fd79b9b29de89e9a6bf69adc87112ab2ea3d6647a291cb293`。继承五处 vendor 删除仍未提交，属于开发候选，不混称干净 main 镜像。
- 完整包 `aide-0.1.17.0-RC2-linux-arm64-0e29f3cafa29-offline.tar.gz`，705682258字节，SHA-256 `ae747c663dd56940c98e7ff8ccc80f1b0eb0f4be8ab59feb1c967e72800ef033`。内外摘要、平台、应用／依赖镜像、启动器、实际 docker load 与两个本地目录副本通过；接收设备导入待验收，上一 `236ad9bc3e3c` 包保留回滚。
- 构建时最终干净 full84／main 门禁待补，以[任务账本](../docs/tasks/code-relation-live-20261015.json)后续收据为准。用户 SSH、浏览器、其他语言及完整工程类型／运行证明仍有缺口；不升版、tag 或正式 Release。

## 2026-10-15 FTPS 索引连接延迟修复候选（构建时记录）

- 源码 `91df81f`：FTPS 星图读取采用 50ms curl 连接尝试间隔，保留 TLS、证书及分批／来源身份约束；真实协议 race13.885 秒、定向隐式 FTPS／渐进 race8.042 秒通过。实际二进制重启／HTTPS 原文检索探针各完成80文件，样本数由上一候选80次降至18次（本次18.396秒）；仅为隔离夹具观察，不作为普遍性能保证。
- 指定 `AIDE_OPEN_BROWSER=0 bash ./start.command` 更新9999为 `236ad9bc3e3c`，13份既有会话文件及鉴权／配置／来源／挂载保持、三项实际网页资源及健康通过。应用 `sha256:236ad9bc3e3c69ee9c81ad307b97b3c657d9449759e596e8bbb225d409ab8b6a`，source SHA `67ec306e04c438f6a4360ccdf823ff268f100992514b97a81b439e93fd6428df`。原五处 vendor 删除保留未提交，仍为开发候选，不混称干净main镜像。
- 完整包 `aide-0.1.17.0-RC2-linux-arm64-236ad9bc3e3c-offline.tar.gz`，705679801字节，SHA-256 `d81ee2547917257909dda79b187ce775b8c41f8a9c23d025afd3b79982369410`。内外摘要、平台、应用／依赖镜像、启动器、实际docker load及两个本地副本通过；接收设备导入待验收，上一 `5c8f109dd119` 包保留回滚。
- 构建时新的干净full84、main门禁／推送待补，以[任务账本](../docs/tasks/ftps-index-latency-20261015.json)后续收据为准。浏览器、IPv6／用户网络／大库仍有缺口，不升版／tag／正式Release。

## 2026-10-15 引用渐进索引联调候选（构建时记录）

- 源码 `e17e867` 新增真实 vsftpd FTP／显式 FTPS 渐进索引、磁盘 App 重载、取消保留和原文检索集成验收，没有修改产品行为。按指定 `AIDE_OPEN_BROWSER=0 bash ./start.command` 更新本机 9999 为 `5c8f109dd119`；健康、三项实际网页资源及鉴权／配置／挂载保持通过。五项继承 vendor 删除未提交，属于开发候选，不混称干净 main 镜像。
- 应用 `sha256:5c8f109dd119762763e4c3ec4fbb1dfcaa503f9c5510d3c43e471792df8cbb2c`，source SHA `2a0f627ce28c0b6c47fe102e74a79348167d697d676d0c2398788d8fb151f0da`。
- 完整离线包 `aide-0.1.17.0-RC2-linux-arm64-5c8f109dd119-offline.tar.gz`，705679404 字节，SHA-256 `3ca0f09da5e8c533229193931efb28f48828328ee5af8bfdfce53628a7230b6b`。应用与依赖镜像、内外摘要、平台、启动器、实际 docker load 及两本地副本通过，接收设备导入未验收。上一 `b5a8b5cba520` 包保留回滚。
- 构建时整套真实协议 race17.935 秒通过；新的干净 full84、main 门禁及推送以[任务账本](../docs/tasks/source-index-live-20261015.json)后续收据为准。不升版／tag／正式 Release，不把目录范围完成等同于全库或浏览器验收。

## 2026-10-15 SSH 授权联调候选（构建时记录）

- 源码 `1541514` 仅增加真实双 OpenSSH 授权生命周期集成验收，没有修改产品行为。测试文件进入现有构建指纹，因此通过指定 `AIDE_OPEN_BROWSER=0 bash ./start.command` 更新本机 9999 为 `b5a8b5cba520`；健康及三项实际网页资源匹配，鉴权／配置／挂载保持。五项继承 vendor 删除未提交，候选不混称干净 main 镜像。
- 应用 `sha256:b5a8b5cba520097f24e62293dea51cdb4d266ade275ef419b7085be473b567e3`，source SHA `5def7d746bfafe0519a8ed3696692cbdee1ad98dbd12acf459ca0e85b72ffa40`。
- 完整离线包 `aide-0.1.17.0-RC2-linux-arm64-b5a8b5cba520-offline.tar.gz`，705679530 字节，SHA-256 `e896596e7b71e0c2f0ec9377c03a013b6322dd2041dfbf29aa787f5732f6d669`。应用／依赖镜像、内外摘要、平台、启动器、实际 docker load 及两本地目录副本通过，接收设备实际导入未验收。保留上一 `bf701a670f6a` 包回滚。
- 此记录写入时 quick82 和真实双 SSH 整套 race19.349 秒通过；新的干净 full84／main 门禁及推送状态以[任务账本](../docs/tasks/ssh-approval-lifecycle-20261015.json)后续收据为准，旧 full 不覆盖新测试输入。UI／用户主机仍有缺口，不升版、tag 或创建正式 Release。验收专用 SSH 镜像单独本地恢复，不声称该测试镜像已随应用包在另一端可用。

## 2026-10-15 Markdown 混排 PDF 最终候选（构建时记录）

- 源码 `2b0ea2c` 加超高表格行修复 `a0ee2ab`，导出资源 v5。相同指定启动器已更新 9999 为 `bf701a670f6a`；三项实际资源、健康及鉴权／配置／挂载保持通过。完整 19 页混排夹具含左右各 90 行的跨页单行表格，经浏览器导出和 Poppler 检查，左右列均到 89。原用户文档及峰值内存未测，五项继承 vendor 删除保留，仍为开发候选。
- 应用 `sha256:bf701a670f6a1e10ea69f445aa582a2a4fca1e9a89b821b1e83f2d52ce1a2e89`，source SHA `fddfe85c0d980cb29dd742e08ed9b851ac6a8a3a3f8054a82b98528b30d0a67e`。
- 完整包 `aide-0.1.17.0-RC2-linux-arm64-bf701a670f6a-offline.tar.gz`，705678942 字节，SHA-256 `fd7caad98e73a1d74cd2dae5b23a508b0aacf551487fbcf5421a184fbe43224f`。应用／依赖镜像、内外摘要、平台、启动器、实际 docker load 和两本地副本通过，接收端导入待验收。保留 `997eda4026b5` 及本批中间包回滚。
- 初始 `cd588b3` full84 PASS（server race 538.971 秒及 vet）只覆盖中间版；最终 quick82 已通过，最终 full84／main 门禁以[任务账本](../docs/tasks/markdown-pdf-mixed-layout-20261015.json)后续收据为准，不复用旧输入收据。不升版、tag 或创建正式 Release。

## 2026-10-15 Markdown 混排 PDF 候选（构建时记录）

- 源码 `2b0ea2c`：修复长列表后标题／正文叠加、跨页行内片段重排及表格列宽变化；统一原坐标定位并避开文字行、短列表项／表格行及图像分页。指定 `AIDE_OPEN_BROWSER=0 bash ./start.command` 更新本机 9999，健康及导出／图表／首页三项资源匹配，鉴权／配置／挂载保持。五项继承 vendor 删除保留，镜像为开发候选，不混称干净 main 镜像。
- 应用 `sha256:297695c9a0468ecc87b8a33b7db2cdce864a789da7c213948848afb1bcb76589`，source SHA `5d643c2cab640e5ed2fb95642832e9b0cf94928e4cdc498cf2ddee39e18bd5f0`。
- 完整离线包 `aide-0.1.17.0-RC2-linux-arm64-297695c9a046-offline.tar.gz`，705678359 字节，SHA-256 `d17ad91615bd7064e8dab9d38cdc4a70c0107f61162e6b56051761f31740ec9f`。应用／依赖镜像、内外摘要、平台、启动器、实际 docker load 和两本地目录副本通过；接收端导入未验收。保留上一 `997eda4026b5` 包回滚。
- 构建时 quick82、真实导出器的 17 页混排夹具和 Poppler 页面检查通过；full84、main 门禁／推送后续状态见[任务账本](../docs/tasks/markdown-pdf-mixed-layout-20261015.json)。未使用用户原始问题文档，未测峰值内存，不替代完整已鉴权工作台浏览器验收；不升版、tag 或创建正式 Release。

## 2026-10-15 星图文件预览候选（构建时记录）

- 源码 `743d0ec`：恢复文件节点通用预览入口，AI 文件引用直接打开，预览窗口复用及回收；代码定位选区可读。指定 `AIDE_OPEN_BROWSER=0 bash ./start.command` 更新本机 9999，健康及七项实际网页资源匹配，鉴权／配置／挂载未变。保留五个继承的 vendor 删除，属于开发候选，不冒充干净 main 构建。
- 应用镜像 `sha256:997eda4026b5a0552547ca9a72e231a0119274f1d21a56423f0c59fe2ce67f43`，source SHA `8cb68327e894f89da832d84a5896438398bbfaffe8df97b195293de53c8beedb`。
- 完整包 `aide-0.1.17.0-RC2-linux-arm64-997eda4026b5-offline.tar.gz`，705672961 字节，SHA-256 `8d0da15b4b6768ce2da263cf417ef24daca86088d746cf19a61dfbae16495f8e`。含应用／依赖镜像；内外摘要、平台／启动器、实际 docker load 和两本地目录副本通过，接收设备实际导入仍待验收。上一版 `f86461692f67` 保留回滚。
- 此构建记录写入时 quick81、真实网页代码的 Markdown／代码预览链路和本机文件来源 API 检查通过；完整 full83、main 门禁及推送待补，以[任务账本](../docs/tasks/starmap-file-preview-20261015.json)的后续收据为准。真实 SSH 和完整已鉴权工作台浏览器未在本批验收，不升级版本或发布正式 Release。

## 2026-10-15 AI 辅助探索候选（构建时记录）

- 源码提交 `2457aa8` 加空文档／页面退出修复 `0a2e928` + test-only `0e1eaeb`，本机仍有五个继承的 drawio vendor 删除；镜像为开发候选，不混称干净 main 构建。通过指定 `AIDE_OPEN_BROWSER=0 bash ./start.command` 更新 9999，健康、实际提供的星图四项资源与当前源码一致，鉴权、配置、挂载未变。
- 应用镜像 `sha256:f86461692f6776a36288b5dc2336cb8147c950a008dca02eeee47c2c932dd941`；source SHA `2ac887793e3a1d77ee891b5482785744bcf7e96bc1ca221daa76238e39e2047f`。
- 完整离线包 `aide-0.1.17.0-RC2-linux-arm64-f86461692f67-offline.tar.gz`，705674313 字节；SHA-256 `3d90c439ad8c655d9ad37904f69baab2a90a61c1759f71a2b381e8547b1ce30b`。含应用与依赖镜像，内外摘要、平台及启动器匹配、实际 docker load 通过；已校验复制到 aideMacMini 对应目录，接收端导入仍待对端验收。此前 `96caed8f11cd` 候选包保留回滚。
- 本记录写入时：行为回归、明暗／窄屏隔离页面及真实千问节点理解通过，完整 full 与 main 门禁尚未完成；后续状态以[任务账本](../docs/tasks/starmap-ai-exploration-20261015.json)为准。保留旧包回滚，不升级版本或发正式 Release。

发行镜像提供 aide（AI + IDE 专业工作台）的完整运行环境：内置 Go、Python、Node.js、Git 与已编译应用。场景定制使用源码和 [Agent 工作流](../docs/customization.md)，完成后重建自己的镜像。

## 统一校验清单 SHA256SUMS

本目录**只使用一份 `SHA256SUMS` 清单**（与镜像归档同目录、相对路径）：

```bash
shasum -a 256 -c SHA256SUMS
```

历史零散的 `aide-*.sha256` / `aide-*.tar.gz.sha256` 单文件校验和**已废弃**，仅供回溯旧版；新发布一律以 `SHA256SUMS` 为准。`install.sh --image` 也只读取归档旁的 `SHA256SUMS`。

## 离线 / 空气 Gap 使用要点

- **运行离线包使用包内 `start.command` 或 `start-bundle`**，不执行构建。源码 `start` 仅在完整本地依赖镜像就绪时支持无网络重建；缺少本地镜像的首次构建仍需联网。
- 离线路径固定为 `bash scripts/aide.sh start-image`（= `--no-build --pull never`），只启动已 `docker load` 的镜像。
- **双保险防 pull**：Compose 中 aide 服务已设 `pull_policy: never`，叠加 `start-image` 的 `--pull never`。即使裸跑 `docker compose up` 也不会联网拉取；缺镜像会立即报错。
- 镜像自包含运行工具链，无 CDN/外部字体依赖；离线时模型指向局域网地址，`web_search` 在未配置端点时优雅降级为本地 `search_text`。完整离线流程见 [docs/installation.md §3C](../docs/installation.md)。

## 联网构建机：产出离线镜像

在有外网、可拉取基础镜像的机器上，于 Git 仓库根目录执行（复用 `scripts/version.sh` 版本号，不另造）：

```bash
bash scripts/docker-release.sh
```

脚本流程：读取版本/commit → 固定 tag `aide:<VER>` → `docker build`（注入版本/commit）→ 容器内 `healthz` 冒烟 → `docker save | gzip` → 生成统一 `SHA256SUMS`。产物命名：

```
docker-images/aide-<VER>-linux-<arch>.tar.gz   # arch = aarch64 | amd64
docker-images/SHA256SUMS                        # 单文件清单（相对路径）
```

交付三件套：同 tag 源码 + 上述 tar.gz + SHA256SUMS，随介质拷贝到目标机。

## 下载与启动

从 [GitHub Releases](https://github.com/skyelan1999/aide/releases) 下载同版源码、`aide-0.1.10.2-RC1-linux-aarch64.tar.gz` 与 `SHA256SUMS`，放在本目录。此次提供 **linux/aarch64（Apple Silicon）** 镜像；x86/amd64 请从源码本机构建，不把 ARM 镜像当作原生 x86 版本。

```bash
# 在归档所在目录验证，再导入
shasum -a 256 -c SHA256SUMS
docker load -i aide-0.1.10.2-RC1-linux-aarch64.tar.gz
```

进入源码根目录，首次复制 `.env.example` 为 `.env`，创建 context 目录，设置已存在的目录：

```dotenv
AIDE_IMAGE=aide:0.1.10.2-RC1
AIDE_PORT=8097
AIDE_WORKSPACE=.
AIDE_CONTEXT=./context
AIDE_LOCAL_ROOT=/absolute/path/to/your/projects
```

```bash
mkdir -p context
bash scripts/aide.sh start-image
```

`start-image` 只启动已导入的镜像，不重建、不拉取，并沿用浏览器令牌登录流程。`start` 是源码重建入口。不要用新版本标签构建未验证的自定义代码；定制时将 AIDE_IMAGE 改为自己的名称（如 my-aide:local）。

## 本地导出与数据边界

`bash scripts/aide.sh export` 将本地开发镜像 aide:local 导出为 aide-local.tar.gz 与 SHA256SUMS；`load` 导入该开发归档。正式 Release 使用 `scripts/docker-release.sh` 产出的版本化归档和 `SHA256SUMS`，避免混淆。

需要换电脑直接启动时，先通过 `start.command` 构建当前工作树，再执行 `bash scripts/aide.sh export-bundle`。这会导出当前 Compose 配置实际使用的镜像，并生成 `aide-<版本>-linux-<架构>-<镜像ID前12位>-offline.tar.gz` 与 SHA256SUMS。构建输入与镜像不一致时拒绝导出，防止误交付旧镜像。

离线包包含运行镜像、独立 Compose、启动脚本、空工作目录模板和使用说明。目标电脑解压后双击包内 `start.command`（Linux/WSL：`bash start.command`），自动校验、导入并启动；无需源码、Go、Node 或 Python 宿主环境，不执行 build/pull。Docker/Compose、Bash、shasum 仍需由目标电脑提供。CPU 架构不符时明确停止。包使用独立的 `aide-offline` 项目；默认端口仍为 8097，和源码版同时运行时应在包内 .env 修改端口。

这是当前构建快照的离线交付，不会自动打 Git 标签、推送或发布 GitHub Release。详见 [包内说明模板](../docker/OFFLINE.md)。

大型归档保留在 aide/docker-images，上传到 Release 附件，不进入 Git 或 Docker 构建上下文（`docker-images/*.tar*`、`*.sha256` 已 gitignore）。镜像不包含宿主工作文件、辅助目录、API 密钥、访问令牌或会话数据。数据卷另行备份；加载镜像不会恢复数据卷。升级/回滚前阅读 [交接手册](../HANDOVER.md)。


## 开发分批交付与镜像更新

Git push 只同步源码，不包含本目录被忽略的大型镜像归档。开发分支每批功能通过验证后应同步构建镜像，再使用现有 `bash scripts/aide.sh export-bundle` 生成带镜像身份的离线启动包。另一端解压后运行包内 `start.command`（或 `bash scripts/aide.sh start-bundle`），首次只导入自带镜像，后续按镜像 ID 复用；不拉基础镜像、不装依赖、不编译源码。不同 CPU 架构需要对应镜像包，不可把 ARM64 包当作 AMD64 交付。

保留原有 `SHA256SUMS` 条目，新增归档校验行；不要让一次开发导出丢掉历史包的校验记录。源码分支 push、镜像本机构建、离线包生成、外部镜像仓库 push 是独立状态；没有配置仓库和目标标签时不能宣称已经推送镜像。当前仓库未配置镜像 registry 发布工作流。稳定依赖层位于应用二进制层之前，普通代码变更应复用现有构建缓存；离线更新路径始终避免在接收端重新构建。


`plugins/surface.json` 是插件宿主启动时重写的能力快照（含生成时间），已从源码指纹及Docker构建上下文排除；插件真实源码仍计入。这样重启不会仅因快照时间变化就误判镜像过期。可用只读命令 `bash scripts/aide.sh source-sha` 检查当前构建指纹，该命令不启动Docker；离线包导出继续严格核对镜像label与当前指纹，不绕过一致性门禁。


2026-10-14 星图分批更新包：`aide-0.1.17.0-RC2-linux-arm64-ab75ecc66e3d-offline.tar.gz`。镜像 `sha256:ab75ecc66e3dc9eff663fd757d3b72e869111ef6087efe6a6ee1af8b6bfb207d`，SHA-256 `68c110e614b2cccca0047c689858b52bd77e904a53c39b5b848dda84fad7a3be`。本机8097已部署，包内镜像校验与 `docker image load` 已验证；另一端仍需导入验收。详见[批次验收](../docs/tasks/starmap-progressive-latency-20261014.json)。


2026-10-14 汇总包（星图分批 + 5x 文档规划 + 插件写入队列）：`aide-0.1.17.0-RC2-linux-arm64-ff1553153a55-offline.tar.gz`。镜像 `sha256:ff1553153a558f1002ffc0df84a8aa03e25921a2025c312b650f7db59c6eea39`，SHA-256 `9af6d3c0115241669dfe1a8def3558732b0e8da75bce896082ea09f600765ca5`。本机8097健康，包内外校验与实际 `docker image load` 通过；另一端请使用此包导入。含前述星图修复。详见[ROS2与插件队列复测](../docs/tasks/parallel-document-plugin-scheduling-20261014.json)。


上一批包（2026-10-14 22:50，含常驻插件超时实例停止修复及之前的星图/5x改动）：`aide-0.1.17.0-RC2-linux-arm64-7f0281ea4ceb-offline.tar.gz`。镜像 `sha256:7f0281ea4ceb3076170f46edfc3a13c3b0f00d9c52cc7095db9b7441c1c376f0`，SHA-256 `8e47db108507525c8964a745ce1e13963dc73f5604b9b6494096322f859af611`；本机8097健康，包内外校验及实际导入通过。保留此前包作为回滚材料；本段保留作回滚。

上一批包（2026-10-14 23:13，增加星图首屏缓存/扫描失败回退修复）：`aide-0.1.17.0-RC2-linux-arm64-05ba9e1b9c84-offline.tar.gz`。镜像 `sha256:05ba9e1b9c84cf7cac389e9dd65fb34c8c7c1a4b8ddceef4056f5a670387ca04`，SHA-256 `59b191e5e8e301854e3bbd5e561534099ea14b31fa9270f73dd1ecf56e1e9a06`；内外哈希、实际docker load和两工作树副本校验通过。本机8097原生Safari两次刷新在约1.16秒/0.91秒观察点均可见1563节点。本段保留作回滚；另一设备导入和冷启动尚未验证。

当前最新包（2026-10-14 23:36，增加插件声明资源预约与取消进程组收尾，包含此前星图首屏修复）：`aide-0.1.17.0-RC2-linux-arm64-9fa963d2209b-offline.tar.gz`。镜像 `sha256:9fa963d2209b59bd72c5a69c4108c7a99f209ca3be6305e3ba1600259ed811dc`，SHA-256 `bf5094ebd911301b5021dad9d71b382ea101a95c4c3943d465923a27df871f29`；内外哈希、实际docker load及两工作树副本校验通过。8097健康，设置/挂载/原加密历史保留。该包尚未实现5x插件工具完整依赖图；另一端优先导入本包，另一设备导入待用户实际验证。


2026-10-15 00:03后当前候选包（包含最终并行倍率列表与此前星图/插件修复）：`aide-0.1.17.0-RC2-linux-arm64-6ac93ca10a8d-offline.tar.gz`。镜像 `sha256:6ac93ca10a8d3b90b5d813e0ad89894afc0a6f16307e27da15c73c483c585289`，SHA-256 `1e1171d0590fb1a8dd5b1b8cb3d3d49bccd1bab2f76446bac051f2b9297ef4fd`，561613174 字节。8097原生Safari已验证推理强度与五条倍率同列滚动、选择持久化；包内外校验及实际 `docker image load` 通过，归档复制到两工作树。另一端导入待验；用户新报“引用aide目录无节点”尚未在该实例复现，本包不宣称解决此新问题。详见[布局验收](../docs/tasks/composer-parallel-placement-20261014.json)和[引用排查](../docs/tasks/starmap-reference-empty-20261015.json)。


2026-10-15 上一批启动恢复包（已由下述资源权限修正版替代）：`aide-0.1.17.0-RC2-linux-arm64-f154cce63fed-offline.tar.gz`（linux/arm64，705564170 字节），SHA-256 `7c202d09a8148d95c2511df597004933c405111a7e609f137455db172dcd6037`。应用镜像 `sha256:f154cce63fed211234c668eeb550718100ced70ff3df4eccddde20ebe2f6e579`；同一包内 image tar 同时包含 `aide:dependencies-v1`，已验证两个标签及运行平台 manifest、镜像包内外哈希和实际 docker load，并复制至两个工作树。原包保留作回滚。按用户要求从 canonical `start.command` 实测启动及清构建缓存后本地重建；实际端口9999，Safari已自动打开但证书提示仍待用户手动处理，尚未把工作台UI验收记作通过。

仅需补充源码构建依赖时，可使用 `aide-dependencies-v1-linux-arm64.tar.gz`，SHA-256 `520baf83d7bc5fc1f20e7b0b8a9efd55976d22fa5bd4b6d2f713566eda905b76`，直接 `docker load -i` 导入。运行离线包不需要再下载此文件或重新构建。首次联网从零构建、Docker缓存与保留镜像的边界见[本地镜像说明](../docs/local-dependency-image.md)及[本批任务](../docs/tasks/launcher-local-image-20261015.json)。


2026-10-15 当前资源权限修正版：`aide-0.1.17.0-RC2-linux-arm64-05c7a9ee2663-offline.tar.gz`（linux/arm64，705587320 字节），SHA-256 `ecb62181f5efc32029e509d00ebbd59de335d0eeb2ede7117272e444374379e5`。应用镜像 `sha256:05c7a9ee2663aa94e49752a67d221babe69deca51c7d97c4da39305ee983ac14`，源码指纹 `5be86528b20af58d36940751e2b56b614ea5de25057c8ad4043cb96c54ab5ca0`。保留同一完整依赖镜像，修复私有源码模式复制后普通运行用户无法读取 Office 和内置插件的问题；工作台实际端口9999，Safari工作台与原配置保留已观测。镜像包内外校验、实际导入和副本状态以[本批任务](../docs/tasks/launcher-local-image-20261015.json)为准。旧包保留追溯，另一设备请优先使用此修正版；接收端导入仍待实际验收。


## 2026-10-15 incremental local snapshot

The canonical `aide/start.command` rebuilt the current local candidate using `aide:dependencies-v1` with build networking disabled and opened Safari on configured port9999. Latest offline bundle: `aide-0.1.17.0-RC2-linux-arm64-29c59cea57b5-offline.tar.gz`; use the adjacent SHA256SUMS. It includes both application and complete dependency image tags for one local import. Runtime/source startup and cache-only cleanup were verified in the launcher task. The current map additionally retains verified observations on source failures, but some host directory opens still wait; complete map coverage is not accepted. This is a development snapshot, not a formal release. Other-device import and Windows native acceptance remain external.


Latest plugin admission-order development snapshot: `aide-0.1.17.0-RC2-linux-arm64-ab31e79ca6c3-offline.tar.gz` (linux/arm64, 705581306 bytes), SHA256 `b49180ef4ce23a6d0ca73d51d248e71d62907f90b9ac38d17be864abbe877683`. Source648fa40 fixes cross-plugin readers overtaking writers hidden behind plugin FIFO. Resource/call race twice and vet PASS; quick68 PASS. Canonical start.command opened healthy Safari9999. Both application/dependency tags, actual docker load and both local copies verified. The prior full70 is historical evidence; no new full/formal release/real-model or receiving-device acceptance claimed.


Plugin dependency-plan development candidate: `aide-0.1.17.0-RC2-linux-arm64-2b4c7e19c77a-offline.tar.gz` (linux/arm64, 705608875 bytes), SHA256 `2aeebb09e34b6d0115512eff7ef9db6a97bc1e7efc61cef5ea57d805ec67ee93`. Source d8f7059 adds explicit plugin DAG dispatch, selected concurrency, serialized confirmations, durable node receipts and conservative recovery. Scoped race/vet and native model-protocol fixture pass; final full gate pending. Canonical start.command opens Safari9999. App/dependency tags, archive hashes and actual local import/copies verified. Dedicated graph UI, uncertain-effect reconciliation and real model/receiving-device acceptance remain open.


Latest readonly plugin graph UI development snapshot (2026-10-15): `aide-0.1.17.0-RC2-linux-arm64-045bdfce262f-offline.tar.gz` (linux/arm64, 705616235 bytes), SHA256 `b046c9a2772d13d0e097460e2e48bc8ffe44626d702a9278d0d01f04bbaa4f69`. Source e54f390; canonical `start.command` opened healthy Safari9999 using local complete dependencies with build networking disabled. App and dependency tags, inner/outer hashes, launcher bytes, actual local docker load and both local copies verified. The included dependency image supports local source rebuilds after cache-only cleanup; offline image startup imports directly. Synthetic display-only Safari desktop/current-dark, actual581x971 and keyboard/evidence navigation checks pass; no live model/plugin execution or receiving-device import claimed. See [current UI task](../docs/tasks/plugin-plan-ui-20261015.json). The previous backend full70 is historical; this UI batch has its own quick69 receipt. No formal release.


Latest chat draft navigation development snapshot (2026-10-15): `aide-0.1.17.0-RC2-linux-arm64-659d44949a0b-offline.tar.gz` (linux/arm64, 705613537 bytes), SHA256 `c193bf748ce6b40d93f42c0eefd31d29f06d256c8493f6d637255b73957111fe`. Source0cb5e42 fixes outgoing draft cancellation and destination input cached under the previous conversation. Canonical `start.command` rebuilt with complete local dependencies and opens healthy Safari9999. Actual typed A/B switching/reload and empty new-session reload pass; lock/workspace guards have focused regression evidence. App/dependency tags, launcher bytes, inner/outer hashes, actual local import and both local copies verified. See [draft navigation task](../docs/tasks/chat-draft-navigation-20261015.json). This replaces the prior local candidate; no new full71/formal release, browser crash/quota or receiving-device acceptance claimed.


Latest per-file star-map body isolation development snapshot (2026-10-15): `aide-0.1.17.0-RC2-linux-arm64-a187ed7d3c6a-offline.tar.gz` (linux/arm64, 705619992 bytes), SHA256 `72aa6fa24cf4e2dd27e0b362ae036e8de1f5dc8ff0a009caa93fded2306e60bb`. Sourcec4f9295 continues indexing peers when a local body is unreadable, shows metadata-only nodes and body-gap counts, and removes obsolete body/code evidence. Canonical `start.command` opened healthy Safari9999 using complete local dependencies. All Knowledge/local-root scoped race73.040s+vet and actual mounted four-batch probe pass; native Safari reference grew2 to3875 nodes with truthful unavailable/readable details. Two sampled cloud placeholders required normal host reads before their bodies became available; full body availability and all latency/host syscall issues remain open. App/dependency tags, launcher bytes, inner/outer hashes, actual local import and both local copies verified. See [body isolation task](../docs/tasks/starmap-file-read-isolation-20261015.json). No new full71/formal release, narrow/other-theme or receiving-device acceptance claimed.


Latest per-batch star-map material reuse development snapshot (2026-10-15): `aide-0.1.17.0-RC2-linux-arm64-1e34815bcb87-offline.tar.gz` (linux/arm64, 705617146 bytes), SHA256 `dec8cb657ed51b5309cfc34d860cb9f8990cc95107dd11c3265a585ffd17552a`. Sourcea3824e5 reuses stamp-validated body success/error within one candidate across aliases; errors are retried on the next candidate, and source IDs/origins remain distinct. Code upgrade reuses text; code-only error does not deny a text-only alias. Scoped all Knowledge/local-root race79.549s+vet and mounted counted probe pass; instrumented old algorithm reproduces four reads per fixture body versus one after repair. Canonical start.command opened healthy Safari9999 and actual reference counters progressed beyond5800. This is reader-call reduction, not a fixed wall-clock speedup or directory-discovery deduplication. Cloud body gaps, first-load selection reset and full latency acceptance remain open. App/dependency tags, inner/outer hashes, exact launchers, actual local import and both local copies verified. See [material reuse task](../docs/tasks/starmap-batch-material-reuse-20261015.json). No new full71/formal release or receiving-device acceptance claimed.


Latest star-map startup selection development snapshot (2026-10-15): `aide-0.1.17.0-RC2-linux-arm64-b687ad3e321d-offline.tar.gz` (linux/arm64, 705620605 bytes), SHA256 `9035e2265ac5e2b3691a3fd9c80761298d5de65167908fd3c71ecca3c7ed129d`. Final sourceee39d65 retains current filter/query/camera edits during cached startup synchronization and identity/network failure retries; same-workspace graph replacement preserves view, workspace/lock/mode boundaries remain. Deterministic old/intermediate negative proofs and final source VM cases pass. Canonical start.command opened Safari9999; native old reset reproduced, final context/query persisted through actual updates6787 to7542 and normal reload. Timing-specific retries/lock/mode/camera are VM evidence, not forced native error/crash acceptance. Complete app/dependency tags, exact launchers, inner/outer hashes, running platform manifest, actual local import and both local copies verified. See [startup selection task](../docs/tasks/starmap-startup-selection-20261015.json). Large-source latency/cloud body gaps remain; no new full71/formal release or receiving-device acceptance claimed.


Latest bounded star-map material development snapshot (2026-10-15): `aide-0.1.17.0-RC2-linux-arm64-abca7611229b-offline.tar.gz` (linux/arm64, 705638919 bytes), SHA256 `131ce82df4f86e6f8244cc56759a0bf6050f6728a5b47bea0ab83aef5b4b3e9b`. Source5ffaaa3 applies fair quota/time-sliced file and directory material validation, explicit pending/unavailable metadata and durable queue cursors. Encrypted payload v2 reads v1; older images safely rescan v2 instead of restoring it. All Knowledge/local-root scoped race99.729s+vet PASS; actual readonly945-node frozen prefix proves bounded reader calls, not full scan latency. Canonical start.command opened healthy Safari9999; native reference grew10744 to13156 nodes, pending detail remained explicit and current selection survived updates. Complete app/dependency tags, exact launchers, inner/outer hashes, running platform manifest, actual local import and both local copies verified. See [material batching task](../docs/tasks/starmap-material-batches-20261015.json). Cloud body gaps and single OS-call blocking remain; no new full71/formal release or receiving-device acceptance claimed.


Latest operator plugin-effect check development snapshot (2026-10-15): `aide-0.1.17.0-RC2-linux-arm64-e5998ca4b4ef-offline.tar.gz` (linux/arm64, 705643413 bytes), SHA256 `19f23a2b501c749ac19b3f3779b1610b1ffa1cd20f419f40a5ed51d6b738053a`. Source060088e adds append-only operator evidence to stopped uncertain nodes with digest/revision guards and save rollback. Operator observations do not replace execution receipts or authorize replay/recovery. Outcome exports omit private check evidence. Scoped graph/effect/outcome race38.884s+vet and quick69 PASS; canonical start.command Safari fixture save, concurrent revision rejection with draft preservation, reload and real stopped/started service restore2 checks PASS. Fixture only: no provider or external plugin action executed. Complete app/dependency tags, launchers, inner/outer hashes, running platform manifest, actual local import and both local copies verified. See [operator check task](../docs/tasks/plugin-effect-checks-20261015.json). Whole-repository Go race (server743.173s) and vet PASS on the frozen Go snapshot. The full71 wrapper is FAIL: it ran an exact-body legacy lock assertion before its behavioral-test repair, and delivery metadata also changed during the run; corrected final quick69 PASS. This is not a passing current full release gate. No formal release or receiving-device acceptance claimed.


Latest reviewed-recovery continuation development snapshot (2026-10-15): `aide-0.1.17.0-RC2-linux-arm64-b5c31b79e1a7-offline.tar.gz` (linux/arm64, 705655682 bytes), SHA256 `2a216d336b1e929bd66d385c88881159f9f641e748d97a811bd976f4fdec8d28`. Final source3248686 (with e446ea9/4994835) adds session/workspace/epoch-bound explicit continue-without-replay decisions, current-policy new work admission, immutable old graph history and exact unresolved-operation retry refusal. Cached graph results and outcome summaries exclude private check/decision notes. Scoped graph/harness/effect/outcome/resume race/vet and final guards16.065s+vet PASS. Native canonical Safari e446 simulated-provider fixture verified missing checks, required note/ack, stale409 retaining note and successful successor; one Docker-internal model request without private markers/credentials, no plugin action. Backend-only owner/ordinary-checkpoint hardening has current API race proof; final source rebuilt by exact start.command with original settings/config/mounts preserved. Complete app+dependency tags, launchers, inner/outer hashes, running platform manifest, actual local import and both local copies verified. Gate result is recorded in the [continuation task](../docs/tasks/plugin-recovery-continuation-20261015.json). No real provider/plugin-effect, receiving-device or formal-release acceptance claimed.

## 2026-10-15 启动错误恢复与并发关闭修复候选

最新本机 ARM64 包：`aide-0.1.17.0-RC2-linux-arm64-0d67b0b6bbfc-offline.tar.gz`，SHA-256 `e18e8f3b54e603eb531547ebd219fc59ef3f074ddb72f0fafd67e4676c3eb0fe`。镜像 `sha256:0d67b0b6bbfc1907d92b419508182da098ab8d19c60995b24454e72b15d894aa`，构建源码提交 `334237a`，源内容指纹 `0759c1e5cc41c0a9c91a7b5cc0b96a031e72f449407239af08e28c16f03105a7`。包含完整应用及 `aide:dependencies-v1` 依赖镜像；内外哈希、镜像标签、运行平台、实际docker load、启动脚本及两工作树本地副本均校验通过。包内入口沿用 `start.command`，仅导入本地镜像再启动；源码入口可使用依赖镜像在无网络模式重建。

本机仍使用保存的9999配置；原不可用工作区配置保留并明确提示临时工作区。修复不可用引用目录误回登录框及HTTP/TLS监听器并发关闭panic。定向race/vet与系统Safari恢复验收通过；完整72项门禁待最终收据。开发分支推送和本机镜像交付不代表正式main/tag发布或接收设备导入成功。


## 2026-10-15 会话编号及独立语音偏好修复候选

当前本机 ARM64 包：`aide-0.1.17.0-RC2-linux-arm64-00c6011ad9f4-offline.tar.gz`，705659485 字节，SHA-256 `fb39935fe3d0be3e09fb8181629a5d9c6db3d69a737ca10f1bad948da47d7ab2`。源码提交 `57e1579`（含 `d6dbf55`），源内容指纹 `eb8d7f89e3ca583c3aa2bc52d4729cceb21bd823967653f36a62f75611066cf6`。应用及完整本地依赖镜像均在包内；内外哈希、启动脚本、运行平台、实际流式 docker load 和两个本地工作树副本校验通过。沿用包内 `start.command` 直接导入；接收设备尚未验收。

规范路径持久化编号、旧配置仅提升编号、独立设置保存保留语音派发偏好已修复。定向 race/vet、quick70、系统 Safari 编号重启与语音读回通过；完整72项门禁单独记录于[任务账本](../docs/tasks/session-counter-canonical-20261015.json)，当前不声明全量通过。六主题桌面及574像素窗口显示检查通过，390像素与真实模型执行仍未验收。开发分支交付，不代表main/tag正式发布。


## 2026-10-15 星图持续扫描快照候选

`aide-0.1.17.0-RC2-linux-arm64-320315d7e14b-offline.tar.gz`，705662934字节，SHA-256 `aafddf23a1d2212b60313af6bd625f7d488c3dd1b2875dd72b9dc42ce0db3c1b`。源码 `6bdcb41`，源指纹 `0220874f51107c9361102fecfbf9112859f12b6b2617b5c15dc562cc59958cd1`。修复持续扫描时缓存写入被不断推迟；quick70、定向计时器回归及系统Safari大引用显示通过。完整应用和本地依赖镜像均包含，流式内外哈希、运行平台、启动器、实际docker load和两个本地副本校验通过。原`start.command`已在本机启动9999；接收设备导入未验收。

新full72结果见[当前任务](../docs/tasks/starmap-current-latency-20261015.json)；前一批全量通过不能代替本批门禁。当前不声明大星域交互或冷启动整体提速，不做main/tag正式发布。


## 2026-10-15 星图元数据布局复用候选

`aide-0.1.17.0-RC2-linux-arm64-fbbbcc0aa4d7-offline.tar.gz`，705661053字节，SHA-256 `dba765290d4f297cf6cb31bcf102bc297d35e60811d9b0ee16ce888307c8d367`。源码`98d6599`，源指纹`e77bddba1abef7056c993ff4aac1c3acfccde32853c9ca9c6df93ec0d6d8bf1c`。仅正文及读取状态等更新时保留空间布局，保持搜索、详情与引用校验更新，结构变化仍重建。quick71和系统Safari自有文件等长改写／删除验收通过，包含完整应用与本地依赖镜像，流式内外哈希／平台／启动器／实际docker load与两个本地副本校验通过。

现有清理脚本实际释放38.37GB默认构建缓存，镜像、卷及本机工作台保留；清理后原`start.command`直接复用本地镜像启动9999。当前full73状态以[任务账本](../docs/tasks/starmap-metadata-layout-20261015.json)为准；不借用前批full72结果。大星域总体交互／接收设备导入和正式main/tag发布未完成。


## 2026-10-15 会话推理入口候选

`aide-0.1.17.0-RC2-linux-arm64-d16b809f97e0-offline.tar.gz`，705660059字节，SHA-256 `4781775d42fbc5c4aefb0b7bbcd77fbe5b76562b3561360e1022f74187354363`。源码`62e3c1d`，源指纹`d1c547d16feb085f5dcdf026406e93fcee20bc9b8d193448a0845bd0601cd086`。模型设置去掉重复默认推理入口，保存模型时不写推理强度；会话策略面板保留五档。含完整应用与本地依赖镜像，内外哈希、平台、启动器、实际docker load、两个本地副本校验通过。通过原start.command启动9999，Safari桌面与会话菜单验收通过；窄屏未完成。接收设备导入及main/tag正式发布未做。详见[任务账本](../docs/tasks/session-only-reasoning-20261015.json)。前批星图full73已PASS，不代替本批验证。


## 2026-10-15 星图逐帧计算候选

`aide-0.1.17.0-RC2-linux-arm64-7d3baf350986-offline.tar.gz`，705663018字节，SHA-256 `eb96d3d0142e6abb1cc1f758653223fa13cc80dcfca6b9e8e59aebd13c835cb1`。源码`39ff208`，源指纹`bdb56cb32f93ce76d8e1dcbcc27b8cf399d9f01f78af7b10b96712f1f79d11b7`。复用稳定视角的尘埃采样，跳过关闭红外及非活动关系的无效工作，保留全部节点和光学计算语义。含完整应用与本地依赖镜像，流式内外哈希、平台、启动器、实际docker load及两个本地副本校验通过。原start.command在9999启动，配置和挂载保持当前基线；原生Safari渐进显示、红外、选中和镜头交互已观察。本批full74待完成，接收端导入、整体大星域延迟与正式main/tag发布尚未完成。详见[任务账本](../docs/tasks/starmap-frame-work-20261015.json)。


## 2026-10-15 成果总览生命周期候选

`aide-0.1.17.0-RC2-linux-arm64-5d04cc677fd8-offline.tar.gz`，705658916字节，SHA-256 `095bef09de111fd50f20a0a5b0a63dff34c671ca17189c76a1af5dbb8fd94c89`。源码`ab89e85`，源指纹`becbb6325e7b7d8ae0dc2ee685e8f2fc492c4c82f85145919bd06b706ab68038`。总览完成后停止轮询，响应以全会话runningRuns覆盖分页外运行任务；关闭后丢弃旧回执。73项快速检查、Go定向race/vet及原生Safari真实模块本地数据验收通过。包含完整应用与本地依赖镜像，流式内外哈希、平台、启动器、实际docker load及两份本地副本校验通过。原start.command已启动9999；未把确定性数据当真实模型验收，未复用前批main full74作为此候选门禁，未合入main/tag/正式Release。详见[任务](../docs/tasks/outcome-overview-polling-20261015.json)。


## 2026-10-15 成果快照请求归属开发候选

`aide-0.1.17.0-RC2-linux-arm64-ad3d1e2c81ba-offline.tar.gz`，705661234字节，SHA-256 `383f77a8178a1c1263904255e8411444d3e8925caffbdd9b44ea388bbdbd6117`。源码`8221669`，源指纹`19c6421d8c594ae9611f2d10ff5be63eba798ee1e51aba6038bb55bf6570cd98`。修复成果重载后的旧响应覆盖/误标、分页互斥、证据归属和关闭句柄；后续修正原生验收发现的重载分页禁用。74项快速检查通过，含十项并发回归；最终Safari/IAB复验仍受UI通道超时阻断，不作为正式可发布验收。完整应用与本地依赖镜像均在包内，流式内外哈希/平台/启动器/实际docker load/两份本地副本校验通过。原start.command更新9999，配置/挂载及提供资源核对通过。此前62915881初次候选存在分页控件问题，已由本候选替代。新full76、main、tag/Release及接收设备导入未完成。详见[任务](../docs/tasks/outcome-request-ownership-20261015.json)。

## 2026-10-15 千问实测与状态卡片候选

当前开发候选：`aide-0.1.17.0-RC2-linux-arm64-0667141784e8-offline.tar.gz`，705662934 字节，SHA-256 `ce62032b42b530294a87210edece40d6c57fbe56a5dfb6b3d90edc18dbd724d6`。源码 `4034555`，镜像 `sha256:0667141784e8966306c9da940b49d990d109356835e173f15ac87fd8907a1d74`。包含文件读取归属修复、运行中思考单一显示、实时工具仅保留未结束调用，以及会话隔离的统一停止入口。指定 `start.command` 已更新本机 9999，保护配置/认证文件和挂载前后校验相同；随后验证加密千问配置重启后仍可用。完整应用及本地依赖镜像均在包内，内外哈希、标签/平台、启动器、实际 docker load 与两个本地树副本校验通过。包不包含本机模型密钥、用户会话或数据卷。

77 项快速检查、真实提供商/AIDE 图片附件以及真实流式 API 停止通过；最终源码停止函数到实际服务器的连接测试也通过。实际浏览器被自签名证书拦截，完整页面和点击验收尚未完成；不是正式 Release。新 full79、main/tag、接收设备导入仍未执行。自动收尾重复读取和长文提前结束保留待查，详见[实测报告](../docs/reviews/2026-10-15/provider-output-interruption.md)。

## 2026-10-15 星图文案候选

当前文案候选 `aide-0.1.17.0-RC2-linux-arm64-b558566a411c-offline.tar.gz`，705662235 字节，SHA-256 `6306fa43a3ab951c0e50fbd0bdfbfc68306535bb5ea7637181eb444fcca98a37`，源码 `d946fce`。两个说明标题改为“观测站手册”“星域档案”，移除外部游戏名称与设定链接；包含此前卡片/停止修复。指定start.command已更新9999，配置/挂载/实际资源一致，浏览器文案摘录通过，验收后0标签。完整应用及依赖镜像在包内，内外校验、元数据、实际docker load、启动器及两本地树副本通过。接收设备导入、完整星图交互和正式main/tag/full发布仍未验证。见[任务](../docs/tasks/starmap-plain-labels-20261015.json)。

## 2026-10-15 累计星级与尺寸候选

`aide-0.1.17.0-RC2-linux-arm64-2b60f35f4633-offline.tar.gz`，705659543 字节，SHA-256 `4b96ba335062c5aca18897e18977ae0354d0f2383731f722a56c01a41a43bd3e`，源码 `f5845d5`，源指纹 `41c53604214ff062eb2d89f0b404bfd1d511080ea21b26d4ecd032e720e83e00`。每级新增关联成本1/2/4/8…，累计门槛1/3/7/15/31/63…；基础体积正比有效关联数、半径立方根增长，选中与近景保持差异。quick78、实际绘制回归及浏览器函数组件预览通过，指定start.command更新9999，配置/挂载保留，提供资源与源码一致。完整应用与依赖镜像均包含，流式内外哈希、标签/平台、启动器、实际docker load及两本地树副本校验通过。完整星图交互、full80、接收设备导入和正式main/tag发布未验收。见[任务](../docs/tasks/starmap-degree-level-scale-20261015.json)。

## 2026-10-15 流程图明暗配色候选

`aide-0.1.17.0-RC2-linux-arm64-9031bd56acb0-offline.tar.gz`，705664078 字节，SHA-256 `0952114b54e69f6c2c3be9492a9e10f6145236870c004d8c6c57f113709298f8`，源码 `846b7f7`，源指纹 `060487f69d53d5e197445523f1d21eb67df4ab6cf2c81c912a24fb7456b23f0d`。统一页面/导出渲染队列，明暗主题自适应，蓝色步骤、青绿起止和暖金判断节点；保留作者显式样式。quick79与真实vendor组件三套主题明暗组合及白纸配色通过。指定start.command更新9999，配置/挂载保留，资源一致。完整应用与依赖镜像均包含，内外哈希/标签/平台/启动器/实际docker load及两本地树副本通过。完整工作台/PDF、新full81/main和接收设备导入未验收。见[任务](../docs/tasks/flowchart-theme-contrast-20261015.json)。

## 2026-10-15 正文交付与收尾检查候选

`aide-0.1.17.0-RC2-linux-arm64-d18d953b69da-offline.tar.gz`，705668737 字节，SHA-256 `c71b7a54c903aa7a6ffa6576ba0a2657a0ffa83832d569485138d8b5838b12e7`，源码 `f5baec6`，源指纹 `29295790b0da2f8eacb91ef023a347d89d06f80a3691e1303787b90754c9593e`。未完成计划及明确产出请求的短执行承诺触发有界检查，收尾复用已有读取并回到原用户目标，保留旧规划阶段。quick79、定向Go race/vet及真实Marker只读一次通过。ROS2长文持续输出30节后被验收脚本240秒取消，同会话补齐到40节；篇幅和停止后上下文恢复未通过，不作为完整长文成功。指定start.command更新9999，配置/挂载/资源一致。完整应用及依赖镜像内外哈希/标签/平台/启动器/实际导入/两本地副本通过。前一a51faa64f79b候选保留回滚。新full81/main/tag、完整UI及接收设备导入未验收；见[实测报告](../docs/reviews/2026-10-15/completion-review-delivery.md)。

## 2026-10-15 中断正文上下文候选

`aide-0.1.17.0-RC2-linux-arm64-d9e90810dea4-offline.tar.gz`，705670006 字节，SHA-256 `68781730cc5af999ce0a936b9a92370e1c3a79b9bda763f9fbd0310fce50a2f8`，源码 `08f0027`，源指纹 `97b5cd89f03a15dfef0c40a67e4fa74634ef8f03b872bdf46e75b30c7c0c1ba7`。保留公开阶段/中断尾段、旧历史可确认归属补回、上下文统计/指纹/压缩一致性与暂停尾段续接。本地归属元数据不发送模型。quick79及定向race/vet、真实千问停止后请求和实际重启恢复通过；完整UI、干净full81、main/tag及接收端尚待验收。指定start.command更新9999，配置/挂载一致，仅两个验收会话使编号计数增加2，已逐字节哈希反证其他设置未变。完整应用与依赖镜像内外哈希/标签/平台/启动器/实际导入及两本地副本通过。见[验收记录](../docs/reviews/2026-10-15/cancelled-output-context.md)。
