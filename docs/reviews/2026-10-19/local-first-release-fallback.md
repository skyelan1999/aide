# 本地优先启动与撤下 LFS

任务：[local-first-release-fallback-20261019](../../tasks/local-first-release-fallback-20261019.json)。基线 main `87833951`。用户明确要求撤下巨大 LFS，总体流程改回本地可用镜像复用、本地编译优先、失败后官方 Release 回退，并发布新版本。

当前代码已移除总包、摘要、LFS 属性和总包制作入口；本地 Git LFS 过滤器与原先安装的钩子已撤下，未删除自定义业务钩子。没有强制改写远端 Git 历史，历史 LFS 服务端对象未伪称已经清除。

普通启动只复用平台匹配、image ID 和源码标签有效、CAD 完整的本地镜像；是否可运行仍由后续真实健康检查判定。存在可用旧镜像时不会自动替换为新源码；更新源码需主动 force/source。没有可用镜像先尝试编译，包括本地依赖恢复；失败后用受控清单对应的缓存或官方 Release 下载包，验证完整归档 SHA、平台和真实镜像身份后才激活。已损坏包不能绕过校验。严格构建可设 `AIDE_RELEASE_FALLBACK=0`。

POSIX 的构建异常和 Windows 的依赖准备/构建异常统一进入已有安全导入器。构建成功不访问 Release；回退成功明确打印可能使用较旧发行镜像。Windows 文件保留 UTF-8 BOM；Windows 原生运行仍需设备方验收。

已执行启动存储回归 38 项通过，包含成功构建不联网、失败编译缓存回退、缺缓存下载回退、严格失败、损坏摘要拒绝和普通复用/force 绕过。Mac 自带旧 Bash 在变量后直接紧接中文标点时错误解析变量边界；改为 `${build_status}` 后输出有效 UTF-8，回归通过。

完整门禁的 130 项现已通过；实际新版本候选镜像和新 Release 发布继续执行。生产未部署，本次不修改会话或工作区数据。

原始 `start.command` 已复制到独立 QA 目录，使用独立 Compose 项目、端口、数据和镜像别名，实际输出本地镜像复用且 Docker health 为 healthy；没有编译或下载。外接盘新建深层目录的 bind mount 被 Docker Desktop 误报为非目录，因此移至内部临时目录后通过；不把此结果称为外接盘新目录验收通过。宿主 urllib 的精确证书信任探针失败于 TLS 握手，未继续认证 API，不计 API 验收通过。候选健康由实际 Docker health 证实。原始 stop.command 退出 0，自建两个容器、网络和镜像别名已清理，用户 app/CAD 容器保持原样。

本机旧总包的 Git LFS 缓存对象也已精确删除（1725320689 字节）；原先两个单架构本地离线包继续作为缓存保留。没有全局 prune、删除 Docker.raw、重置用户密码或覆盖会话。

完整检查：`python3 scripts/agent-route.py verify full` 原始命令退出 1，仅 CAD 几何检查因 Docker Desktop 外接盘 bind transport 失败；其余 129 项通过，包括 1022 个已注册服务器测试项按 12 个互斥分片全部运行通过，以及其他包 race 和 vet。将相同 CAD 素材复制到内部临时目录并逐文件核对 SHA256，仅映射两个素材挂载路径，未改测试或断言，原始 CAD 检查退出 0。收据保留原失败及明确的 retries 来源，不能将原始 full 命令描述为退出 0。最终 130 项内容指纹为 `35ffee08edb3b63c40204b71b4ce2e7c660c6df60d60386523c41ad469516377`。原始失败、补测日志与最终收据保留于任务私有证据目录。

实际 Docker 构建失败恢复探针已使用既有官方 RC2 镜像，完整标签、真实 image ID 和平台通过导入器校验后恢复。公开旧默认 ARM64/AMD64 附件配对门禁通过；新默认清单只在新附件上传与线上摘要校验后更新。

## 新版本候选与证据

新版本 `v0.1.17.1-RC1` 已通过仓库既有 daily 升版脚本在 main 创建，标签提交 `3cdbe1a6d6264284346cfb6ef4c8dc6146a4a4f7` 已推送。两个候选都由该提交构建，公共源码标识 `21c7f4e47f9c468dd71e42a3ee2596276c1dc3f1b7a6543c0d55b062e5df3955`，内嵌版本 `0.1.17.1 RC1`，运行模式 `release-image`。

| Docker 架构 | 完整镜像 ID | 运行环境 |
| --- | --- | --- |
| linux/arm64 | `sha256:c7188a065f4b8dea26ff2d2e968cca4ee279908cda9fff872347a3abd6ea890a` | 本机原生 |
| linux/amd64 | `sha256:c9ed3977bcc7fe8f85f756a9e1cecaf1232375946705933fbee044e5c092afbb` | 本机仿真 |

使用既有 `Dockerfile.local`、各架构本地工具链及 CAD 基础镜像，网络关闭，串行构建；构建执行 JavaScript 语法、Go vet、Go build 和 LAN 桥接编译。`AIDE_RUN_TESTS=0` 表示未在构建层重复已完成的 full/race 检查，不能称为构建层跑了完整测试。

两个候选均通过非 root 文件读取、五个内置插件、六平台 LAN 桥接校验和与 DOCX 往返。独立无网络临时容器健康通过，未认证 `/api/sessions` 返回 401，认证 `/api/config` 返回 200，内嵌版本和完整提交匹配；每次请求连接后先核对完整对端 DER 证书再发送令牌，未关闭产品 TLS 或信任浏览器证书告警。真实 CAD 转换生成 12 个三角面的长方体。测试容器均已删除。这里的 AMD64 结果不替代原生 Windows 验收。

使用既有 `scripts/package-release-assets.sh` 为两个架构导出包；两个镜像归档实际 `docker image load` 成功、平台和 ID 保持一致。该导入发生在已有镜像的本机 Docker 引擎，未称为空引擎或另一设备接收验收。离线 tar 中的 BUILD 和 `.aide-image` 身份一致；完整升级 ZIP 中的 manifest、平台、版本、真实镜像 ID 和完整内嵌归档 SHA256 均检查通过。精确 tag 的源码 ZIP 不包含当前 LFS 大包、运行 `.env` 或访问令牌。

结构化证据：[local-first-release-proof.json](local-first-release-proof.json)。当前运行中的用户 app/CAD 容器和 `aide:local` 镜像 `48e99a7add36` 保持原样。

## 升级与回滚

开发目录升级先保留 `.aide-data`、工作区、配置及当前可用镜像，再更新源码；普通启动仍会复用有效本地镜像。需要应用新源码时使用 `AIDE_FORCE_REBUILD=1 ./start.command`，本地构建失败可能回退已验证发行镜像，需核对日志中的实际运行版本。严格要求构建成功可同时设置 `AIDE_RELEASE_FALLBACK=0`。

离线首次安装选择 Docker 引擎对应架构的完整包；已安装 Release 版可使用对应架构的 full ZIP 经软件更新暂存、确认激活，保留共享数据。当前生产没有切换，因此本次无生产回滚动作。后续更新若失败，应沿原软件更新页面切回旧槽；开发目录则恢复上个已知可用镜像及相应源码，禁止删除用户数据代替回滚。未强制重写 Git 历史，也未永久删除托管侧历史 LFS 对象。

## 发布完成与默认清单提升

[v0.1.17.1-RC1](https://github.com/skyelan1999/aide/releases/tag/v0.1.17.1-RC1) 已于 2026-10-19 03:09:21 UTC 公开发布为 prerelease。7 个附件包括两个完整离线 tar、两个首次安装兼应用内升级 full ZIP、精确标签源码 ZIP、SHA256SUMS 和附件说明；全部 GitHub uploaded 状态、精确大小、公开 SHA256 与本地一致。5 个二进制归档的匿名 HTTP Range 下载返回 206，范围总长度及 gzip/ZIP 文件头匹配。公开双架构门禁通过后，才将 `docker/prebuilt-images.tsv` 提升为本轮镜像配对，7 项发行校验回归及提升后的在线门禁通过。

标签产品源码经 full 130 项资格检查；发布后的默认清单、缓存摘要、说明和证据是单独的元数据跟进提交，未重新跑无变化的 Go/race。不能声称原 full 收据指纹匹配这些最终元数据；该跟进的证据是 7 项定向回归、公开附件逐个摘要校验和提升后的公开配对门禁。精确标签源码 ZIP 不会被事后重写，因此仍携带先前已验证回退清单；最新 main 才携带本轮已上传清单。

全部公开校验成功后，删除本机两个旧 RC2 应用缓存包（`07172e63ff04`、`9bf8eb9f240e`）及本轮自建重复打包目录，保留 `docker-images/` 下新版 ARM64/AMD64 完整离线缓存和本地依赖种子。日志和受控证据保留，没有全局 prune、历史 Release 删除、运行镜像替换或数据清理。
