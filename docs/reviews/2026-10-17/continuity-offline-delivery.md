# 当前完整离线包公开交付 · 2026-10-17

任务 `continuity-offline-delivery-20261017`，基于本轮[运行候选验收](continuity-runtime-candidate.md)。两个平台使用相同源码哈希，ARM 已通过原始启动脚本及进程重启、实际 MCP 与离线解压启动，AMD64 构建及实际导入通过；AMD64 原生运行 / Windows 由用户验收。

## 发布前盘点

实际公开 GitHub API 确认现有 `v0.1.17.0-RC2` 是公开预发布，Release ID `406597437`，原有 23 个附件；两个新归档名均不存在。归档已校验无宿主数据路径，摘要和身份见[机器可读交付证据](continuity-offline-delivery-proof.json)。

本机没有 `gh` CLI；使用已有登录的 Safari Release 编辑页，不新建标签、不修改或提交已有未保存说明。通过原生文件选择框选中当前 ARM 包后，页面显示 “Uploading your file now...”。这只证明上传已经开始；公开 API 确认 uploaded、大小和 SHA256 之前，不记为上传完成。

## 当前状态

- ARM64 / AMD64 完整包及独立说明 / 校验文件全部公开校验通过。
- 公开配对门禁通过后，本地已更新默认清单、仓库校验文件及说明；最终源码目录启动和 full/main 门禁待执行。
- 已在本地 main 快进集成并完成最终 full；远端 main 未推送。源码已推至任务分支并创建草稿 PR #4；未升版、打标签或提交 Release 编辑表单。
- 生产实例保持；完整原生工作台验收暂缓，Windows 用户验收，真实向量暂缓。

## 后续门禁

两个公开附件精确校验通过后才更新默认清单及摘要；交付输入变化后重新执行 full，随后按干净 main 和 release-check 门禁推进。保留已有公开附件、镜像与数据作为回滚依据。

## 上传期间的门禁与配套资料

对本轮真实候选清单运行 `scripts/verify_prebuilt_assets.py`，返回 exit1：`linux/arm64: no unique uploaded public asset`，mutations=false。公开附件尚未就绪，门禁按预期拒绝，未激活默认地址。

已准备独立 `AIDE-runtime-07cdef2d4719-README.txt` 和 `AIDE-runtime-07cdef2d4719-SHA256SUMS.txt`，写明两架构归档身份、启动步骤、资料保留、原生 AMD64 / Windows 和完整工作台验收边界；未修改现有 Release 说明，也未覆盖旧摘要附件。配套资料待主归档上传后追加。

## ARM 公开附件已完成

公开 API 返回 ARM 附件 ID `649882716`、state `uploaded`、771016230 字节，SHA256 与本地 `d1392c59b789f8d8d7b8150d00a604ac557a7a5d2e6b353e8f7959485ef16283` 一致。原 23 个附件全部保留，公开 Release 正文哈希不变。

实际访问[ARM 完整离线包](https://github.com/skyelan1999/aide/releases/download/v0.1.17.0-RC2/aide-0.1.17.0-RC2-linux-arm64-4b784ce0724f-offline.tar.gz)，Range 0–1023 返回 HTTP206，Content-Range 总大小正确，读取 1024 字节且 gzip 头正确。这是公开下载入口冒烟，不是网络全包下载或另一设备导入验收。

本轮候选公开门禁已推进到 `linux/amd64: no unique uploaded public asset`，exit1/mutations=false；ARM 对应项已通过，仍不激活双架构默认清单。随后在同一现有 Safari 页面选择精确 AMD64 `382c90e72fca` 包开始上传，未重复上传 ARM，未提交说明表单。

## 双架构配对及默认清单

AMD64 附件 ID `649902301`、state uploaded、916933833 字节、SHA256 `df892d449ffb70c6edf5265aeb13c9c3870f194bed1b890f73f667e411b2793c` 与本地一致。公开 URL Range 冒烟 HTTP206 / 1024 字节 / gzip 头及总大小正确，同样不是另一设备全包导入验收。

两个完整包的公开配对门禁 PASS，mutations=false，同一源码 `07cdef2d47190f9cbaf13786511186d4e309ccb3a50b6c4cefd45fdcd1c28def`。随后才更新本地 `docker/prebuilt-images.tsv`、`docker-images/SHA256SUMS` 及说明；实际受控默认清单再次通过在线门禁。旧清单可从 Git 及私有备份回溯，未替换生产。

独立 README / SUMS 附件 ID `649909981` / `649910020` 已公开 uploaded；大小、摘要及实际完整下载字节与本地一致。公开 Release 当前 27 个附件，原 23 个保留、正文哈希不变，未提交现有说明编辑表单。

继续验证普通源码目录默认预构建启动路径，以及交付输入变化后的 full/main 门禁；截至本段记录尚未推送 main。

## 普通源码目录默认启动

从干净提交 `4c22dc5a` 的 Git archive 创建独立普通源码目录，无 `.aide-image`、`.env`、AIDE_IMAGE 覆盖或 AIDE_BUILD_FROM_SOURCE 开关；通过原始 `start.command` 直接选择本机已存在、与公开默认项相同的 ARM4b784 镜像。健康检查、未鉴权401、运行构建身份 db02c58f 和实际 CAD OCP 导入通过，没有编译或镜像下载。

随后原始 `stop.command` 停止实例，核对归属后移除两个已停止容器及两个无容器的自有网络；测试数据和镜像保留。生产身份与22个受保护文件再次精确一致。该结果证明已有正确本地镜像时的普通仓库启动，不证明全新另一设备下载导入。

浏览器未新增标签，恢复原右侧 AIDE #39 空输入；保留 GitHub 未提交表单。交付文件变化后旧 full 收据不作为当前发布门禁，需要干净 main 最终 full；main 尚未推送。

## 最终 full、失败记录及审阅分支

校正后的正式 `verify full`：114 个命令全部退出0，946 个服务端注册入口在12个互斥串行分组执行，其他 Go 包 race、vet、CAD 均通过。收据 HEAD `8aedbca382fdd30a8a18affe4270542a1e39268f`，内容指纹 `8fe143570be6a886d8d10bce3ba61d1da2d3ff4368edc575b4a78a5584f68f24`。实际隔离容器为4GiB/2CPU/512PIDs、network none、无用户数据挂载，结束已移除。

前一整轮 exit1：宿主 PATH 缺少 node，88项 Node 命令未执行，其余26项含 Go/race/vet/CAD 退出0。该失败日志保留在 `.agent-state/verify-20261017T075944366264Z.log`。设置现有捆绑 Node 的进程 PATH 后，quick111 先通过，再重跑正式 full114，通过日志 `.agent-state/verify-20261017T081611200593Z.log`；未手工拼接或伪造 full 收据。产品源码未因此改变，运行源码 SHA 仍为07cdef2d4719开头的同一身份。

源码任务分支已实际推送，独立远端核对 head8aedbca3；创建并验证[草稿 PR #4](https://github.com/skyelan1999/aide/pull/4)，基线 main 仍为ceb82481。PR说明已更新正式full114通过和实际6 PASS/2 BLOCKED门禁，保持草稿。自有审阅标签已关闭，原4个非固定标签及固定页保留，恢复#39空输入。

审批有效期任务 acceptance3 明确要求 “Run exact-source quick/full and native candidate before release”；其当前原生候选验收仍由用户暂缓。成果来源链接任务的完整工作台验收也仍在进行。两项保留 verification=in_progress，不能用镜像交付任务自身通过来替代它们。下一步实际检查各子任务门禁，远端 main 合入与生产替换继续保持待办。

## 原始验收证据校正

[17日成果舱原生证据](outcome-native-proof.json)已证明历史e729实例的101条原生分页、原始摘要一致及完整摘要下载；旧16日待办中的“分页未测”不再适用。发布记录提交卡片、隐藏运行活动及运行中快照变化仍是独立待验收项，不把历史证据提升为当前整页通过。

[17日能力仪表证据](capability-native-proof.json)已有7项检查及自然过期显示，超过旧16日仅3项检查的摘要；它们仍只证明根目录、插件入口和依赖导入，不证明写入、文档处理或用户主机权限。

## 实际发布门禁矩阵

在干净的本地 main `c75764b8`，逐个执行 `python3 scripts/agent-route.py release-check <任务号>`，结果如下。源码指纹仍与最终 full 一致；没有手改收据。

| 子任务 | 结果 |
| --- | --- |
| timeline-retention-synced-main-20261017 | PASS |
| approval-expiry-synced-main-20261017 | BLOCKED：verification 未完成 |
| outcome-release-source-link-20261017 | BLOCKED：verification 未完成 |
| docker-resource-guard-synced-main-20261017 | PASS |
| startup-inputs-notice-synced-main-20261017 | PASS |
| mcp-http-synced-main-20261017 | PASS |
| prebuilt-publication-gate-20261017 | PASS |
| continuity-offline-delivery-20261017 | PASS |

通过项同时验证了当前公开双架构默认附件。[审批有效期子任务](../../tasks/approval-expiry-synced-main-20261017.json)和[成果来源链接子任务](../../tasks/outcome-release-source-link-20261017.json)的当前原生验收仍由用户暂缓，两次实际门禁均返回 `BLOCKED: Incomplete stage: verification`。整批源码继续在任务分支与草稿PR中审阅，远端main未推送、生产未替换；无需重复运行已通过且输入未变的全量测试。

最终 PR 说明经公开 API 复核，草稿状态、分支及全文均正确；GitHub 原生编辑表单把行尾转为CRLF，第一次原始字节等值检查因此失败，逐行核对仅CRLF差异后以明确的LF规范化比较确认内容完全一致，原始及规范化SHA分别保留。未标记Ready、未合并、未提交Release说明。最后一个自有PR页已关闭并恢复原#39空输入，原4个非固定标签和固定页保留。
