# 完整工作台路径验收与源位置指纹

任务：[knowledge-path-native-20261017](../../tasks/knowledge-path-native-20261017.json)。延续原十二项目标，使用原外置目录 start.command 和已发布 d025 镜像；未调用模型、执行样例脚本或新增浏览器标签。

## 直接证据

当前实例索引/API实际返回报告→证据→源码两段显式引用，以及按钮→事件→回调→HTTP请求→路由→处理函数→服务的六段静态链。Safari完整认证页面实际查询出报告两段路径，逐边显示显式引用和L3:20、L2:25；点击源位置在当前标签只读选中报告第三行。六段链本轮只完成当前真实索引/API验证，不能替代其完整Safari展示验收。

实际链接没有文件哈希。仅在专用报告前加两行，原引用移到第五行后，刷新旧定位地址仍选中第三行标题 `# Report`，没有过期提示。该缺陷属于路径面板的源位置入口；前一批文档检索命中的哈希保护证据保留其原范围。

## 源码修复

路径结果为普通文件的已索引原文增加 `indexedTextHash` 与 `indexedTextBytes`，仅绑定最多8192字节的准确UTF-8原文，包括CRLF，不将其标为整份文件的 `contentHash`，不额外访问文件系统。已有代码节点的全文件哈希保留。

源位置链接使用路径结果中的节点证据，避免借用后来图更新的节点。只读查看器核对原文前缀或既有全文件哈希后才应用行号；缺少证据的旧星图定位要求重新获取。大文档只编码有界文本，未观察的尾部变化不被误称为前缀变化或完整版本验证。Office等独立可视查看器继续不套用文本行选择。

新增异步摘要核对前后检查路由、认证令牌、工作区、恢复代次和锁态，迟到的读取或摘要不能覆盖后续页面。普通非星图文件路由保持兼容。相关脚本缓存版本同步更新。

## 实际检查和未完成项

新增Go回归在旧实现上因缺少指纹和字节数失败；源码修复后专项race通过1.033秒，相关Path/Web/Reference回归及vet通过60.347秒。原Go格式化调用曾漏传容器标准输入、产生空输出，已从干净基线恢复并加非空输出检查后重新执行；失败日志不作通过证据。

快速检查104项全部通过。前端实际函数回归覆盖UTF-8/CRLF、前缀变化、未观察尾部、超限/损坏参数、旧地址，以及API和摘要等待期间的令牌、工作区、锁态、代次与路由变化。它们不代表新镜像或原生复验通过。

临时来源注册表逐字节恢复，原14会话、鉴权、设置和无关挂载核对保持；样例文件恢复，查询为空、全部来源、路径面板关闭后回原工作台，无新增标签。测试索引观察遵循既有有界历史，更新前备份保留，不声称知识历史字节未变。

当前仍运行已发布d025，远端main仍为3ed6164d。本分支需构建新候选、复验报告旧位置拒绝及六段链实际页面，冻结完整包后跑新的完整回归/发布门禁。原十二项继续，Windows由用户验收，真实向量测试暂缓。

## Candidate runtime follow-up

Candidate7e3 started through original start.command; protected data passed. Native Safari selected report line3; prefix insertion refused old location. A238093-byte UTF-8/CRLF report passed8192-byte proof, unchanged unindexed tail remained locatable, changed prefix refused. No new tabs or model calls.

Actual large-map code path failed: total catalogue edges exceeded6500 and consumed the code budget. New20000-directory regression failed before fix (race0.204s). Independent code budget with shared type-binding cap fixes this; related Code/Web/GoTypes/Path race7.496s and vet pass. Final compiled six-step UI/full/package verification pending. Initial path queries also returned409 during scan revision changes; fresh queries succeeded without weakening identity guards.

## Final runtime and offline acceptance

Final9d2 (source eaa5a6a116a0, code6b011170) passes actual authenticated Safari six-step button/event/callback/HTTP/route/handler/service path. Each step distinguishes candidate/binding/declaration evidence; final source action selects server.go line4 with full hash and read-only same-tab return. Updated report path carries new prefix proof and selects line5. The238093-byte CRLF prefix/tail cases above use identical frontend files from the prior candidate; the final product adds the independent code budget.

Both complete ARM64/AMD64 archives pass outer/inner hashes, exact launcher/license bytes and real local image import. Fresh ARM64 package original start.command runs without build/pull; raw CRLF document line/hash and120-triangle native CAD pass. Original auth/settings14sessions/mounts and exact source registry preserved; owned report baselines restored. Final full107 and clean-main gate/public RC2 assets remain pending.

## Current-main integration

Remote main advanced to9740a4f2 with workbench/file-path UI changes. Merged into ba9e5a3b, retained both behaviors, resolved script cache versions (app168, locale136, file-route3). File-path navigation and viewer guard source tests pass; merged quick104 passes. Rebuilt controlled source3e5b6439ba07; current e729 runtime exact assets/health/protected state pass. Actual Safari again displays six steps and selects service source line4. Both integrated complete ARM/AMD packages pass exact hashes/scripts/licenses/import. Earlier9d2/6f670 packages are superseded local candidates; they were never uploaded. Final integrated fresh start/full/gates/publication remain pending.

Integrated fresh ARM package startup also passes: included start.command, no build/pull, exact raw CRLF document hash/line3 and native120-triangle CAD. Temporary source registry/sample bytes and rendered map cleaned; knowledge/all-sources/empty query/closed path and original workbench tab restored. Final frozen-source full107/main gate and public RC2 supplement remain pending.


## 2026-10-17 发布续接

源码及验收文档已推送并独立核对 main `cc430f2ebb7ebb30fadc71eda3c5352ef71b24d7`；两项 clean-main 发布门禁通过。源码冻结的 full107 收据仍适用，未为普通文档变更重跑。

本轮完整 ARM64/AMD64 离线包仅在本地验证完成。Safari 上传两种大包均被 GitHub 拒绝；刷新表单以及把 ARM64 包复制到本机 `/tmp` 并校验相同 SHA-256 后仍失败，小型 README 上传成功。仅能证明此次大小文件表现不同，无法确定网络、浏览器或 GitHub 侧的具体原因。未发现可用的标准 HTTPS 发布凭据。

公开 API 确认原有21附件名称、ID、大小、状态、digest及原发布说明不变；RC2 tag对象和目标提交不变。未点击更新发布说明，未把镜像标成公开可下载。只新增本轮 README。公开下载和另一设备验收继续 pending，证据见 `knowledge-path-publication-proof.json`。

保留最终完整包、全部已发布回滚包及未发布中间包，待最终远端交付成功再按哈希和归属清理。此次本机临时上传副本经哈希核对后删除，不触及项目数据或原目录。
