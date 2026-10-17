# 镜像导出与公开默认清单门禁

基线 `a44a2f68`；分支 `fix/prebuilt-publication-gate-20261017`。用户要求仓库和发行包均能快速导入启动。本轮先修复造成未发布地址进入默认清单的生成路径；现有坏地址仍需最终公开镜像替换。

## 直接负例与修复

从真实 `scripts/aide.sh` 提取并执行导出记录函数，使用临时小文件作为归档；修改前默认清单被写入未上传 URL，回归失败。修改后只更新 `.agent-state/prebuilt-images.candidate.tsv`，原默认清单字节不变；不删除或导入任何真实镜像。

新增只读 `scripts/verify_prebuilt_assets.py`，要求六字段格式、ARM64/AMD64 各一项、同一源码身份/tag、名称与平台及镜像 ID 对应；公开 API 必须返回非草稿 Release、唯一 uploaded 附件、正数体积、相同 URL 和 SHA256。只读元数据，不使用凭据、不下载镜像、不激活默认清单。网络失败和超时均拒绝。

`release-check` 先执行原本地门禁；本地通过后才调用公开附件检查，任一检查失败即拒绝。这不改变既有版本、打包、上传或 main 审阅流程，也不代替包内摘要、镜像标签、实际导入、启动和原生 UI 验收。

## 已运行的验证

- 导出记录负例确实改坏临时公开清单；修复后原样保留并产生独立候选。
- `python3 scripts/test_prebuilt_publication.py`：7 项通过，覆盖缺失/重复/草稿/错误 tag、上传状态/URL/摘要/大小、混合源码/架构、非法身份/含秘密 URL 的拒绝及网络失败无写入。
- `python3 scripts/test_agent_route.py`：6 项通过，含子检查失败和超时拒绝。
- `python3 scripts/test_launcher_data.py`：21 项通过，原启动数据/预构建导入回归保留。shell 语法检查通过。
- 真实在线检查当前受控清单返回 exit1，`linux/arm64: no unique uploaded public asset`，mutations=false。这是预期发现现有失效默认项；不能计作下载问题已经修复。此前双项 API 负例详见 [MCP 兼容性审计](mcp-http-synced-main-audit.md)。

quick/full 的准确收据由任务账本追加。开发中初次正例还发现中文全角标点紧接 Bash 变量时的解析错误，已使用显式变量边界修复后重跑专项，不隐藏该中间失败。

## 未完成

当前默认地址仍需最终已验证且公开的双架构镜像替换，实际新设备下载/导入和 Windows 实机未执行；生产未重启，清单未激活，镜像未上传，本轮没有 tag/main/push/Release 操作。继续推进 MCP 实际连接、新源码完整回归和发行交付。保留原始镜像、用户数据、保护文件及回滚引用。
