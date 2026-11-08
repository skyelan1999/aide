# Aide 非插件、非 Windows Issue 核查（2026-11-08）

基线：`origin/main` `b82120e599bfaefa56bf01b132822d8510c180ae`。审计在独立工作树执行；原 `main` 工作树的已修改和未跟踪文件保留原状。逐项清单与 GitHub 链接见同目录 [JSON 清单](2026-11-08-nonplugin-issue-audit.json)。

## 范围与判定

首次读取 GitHub open 列表为 111 项，其中 5 项实际为 PR。其余 106 项中，11 项属于插件/插件工作台专属路径，4 项属于 Windows 专属路径；本轮审计范围为 91 项。`/issues` API 会混入 PR，不能把 [#671](https://github.com/skyelan1999/aide/pull/671) 和 [#672](https://github.com/skyelan1999/aide/pull/672) 当作可直接关闭的 Issue；两条 PR 目前均未合并，GitHub 报告不能自动合并。

关闭条件：修复已经进入当前 `main`，并有与问题对应的验证。仅看到代码路径、历史测试记录、旧版本运行结果或新的上游文件，均不代表当前问题已解决。未完成浏览器、远程主机和正式发行物验收的地方单独注明。

## 本轮确认可关闭

| Issue | 当前源码与验证 | 范围 |
| --- | --- | --- |
| [#815 英文 Markdown PDF 分段进度](https://github.com/skyelan1999/aide/issues/815) | `24787b524` 已加入翻译；`node scripts/test_i18n_coverage.cjs` 通过，实际 `en.js` + `i18n.js` 英文设置输出 `Converting Markdown chunk 2…`。 | 源码/组件；未做桌面实际导出。 |
| [#82 旧 SSH 任务跨工作区访问](https://github.com/skyelan1999/aide/issues/82) | `502cad907` 已绑定任务工作区与 SSH 会话代际；离线 Go 1.26 容器运行 `go test ./internal/server -run '^TestIssue82' -count=1` 通过，覆盖旧任务 skill、shell、审批应答以及当前任务正常路径。 | 隔离测试；未接真实远程主机。 |

两项均在 Issue 留下证据后以 `completed` 关闭。

## 本轮修复，待合并后关闭

[#795 版本计算脚本未进入构建指纹](https://github.com/skyelan1999/aide/issues/795)：当前 `scripts/aide.sh` 的 `source_sha` 确实漏掉 `scripts/version.sh`。隔离分支已加入该输入，并扩展现有源码指纹夹具。修改 `version.sh` 的注释可使指纹改变，恢复文件后指纹复原；`python3 scripts/test_source_fingerprint.py`、`bash -n scripts/aide.sh`、`git diff --check` 通过。关闭须等合并推送 `main` 后再次核对。

## 保持开放的主要范围

- **PDF/JPEG 2000 解码**：68 项集中在当前内嵌 PDF.js/OpenJPEG 的边界、颜色、HTJ2K 与内存路径，另有 [#674](https://github.com/skyelan1999/aide/issues/674) 同属该类。当前 README 记录私有 OpenJPEG 模块已经更换为上游固定提交生成的 wasm2js 文件，但明确不宣称覆盖所有原生解码报告。`node scripts/pdf-jpx-components-review.cjs` 通过 18 种分量与 24 种参数组合，不能代替这些 Issue 各自的恶意或边界文件回归。逐项保留，需按真实当前 bundle 构造和运行各自的复现材料。
- **PDF 导出**：[#813](https://github.com/skyelan1999/aide/issues/813) 的矢量实现、表格比例与分页组件检查通过，当前本地 9999 已服务 `markdown-export.js?v=18`；历史隔离 Chromium 跑出了可提取文字的 238 页 PDF。Issue 还要求实际工作台/PDF 阅读器及 100 MiB 混合文档验收，这些本轮尚未完成，保持开放。
- **知识可见性**：[#796](https://github.com/skyelan1999/aide/issues/796) 的当前 `/api/knowledge-map/updates` 仍直接返回 `knowledgeDelta(view, cursor)`，没有 AI 可见性过滤；不能因普通星图允许人工查看而关闭。修复需引入独立 AI 范围和缓存/游标隔离，并使调用方使用它。
- **文件与主机边界**：[#691](https://github.com/skyelan1999/aide/issues/691)、[#694](https://github.com/skyelan1999/aide/issues/694)、[#695](https://github.com/skyelan1999/aide/issues/695)、[#697](https://github.com/skyelan1999/aide/issues/697) 的 SSH/SFTP 根目录限制；[#701](https://github.com/skyelan1999/aide/issues/701)、[#704](https://github.com/skyelan1999/aide/issues/704)、[#763](https://github.com/skyelan1999/aide/issues/763)、[#773](https://github.com/skyelan1999/aide/issues/773) 的 helper 锁与挂载竞态；[#762](https://github.com/skyelan1999/aide/issues/762) 的 LAN bridge 应用失败一致性，均没有本轮可支持关单的当前主干回归结果。优先处理根目录边界与竞态，再做隔离 SSH/Compose 故障注入。
- **发行链**：[#657](https://github.com/skyelan1999/aide/issues/657)、[#658](https://github.com/skyelan1999/aide/issues/658)、[#660](https://github.com/skyelan1999/aide/issues/660) 仍需逐一核对 Actions、基础镜像和 Sherpa 归档的不可变来源及双架构构建。不能只改引用而跳过实际构建。
- **其余核心项**：[#797–#800](https://github.com/skyelan1999/aide/issues/797) 的 Go 目标约束/类型绑定、[#776](https://github.com/skyelan1999/aide/issues/776) 的大文件浏览器续传崩溃，均缺当前问题专属通过证据。详细编号见 JSON；保持开放。

上述保留项是“未取得足够关单证据”，并非逐项断言当前版本一定仍有相同缺陷。审计没有修改运行中的 Aide、插件安装或用户数据，也未发布发行物。
