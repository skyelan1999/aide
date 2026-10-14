# 2026-10-14 默认推理强度与连续目标检查点

主目标仍为 `aide-continuity-knowledge-20261009`，12条工作流保持in_progress。Windows由用户验收，真实向量测试继续暂缓；本轮没有真实模型、向量或付费调用。

## 已复现及修复

1. 已有Safari `https://localhost:8097`可信标签打开模型设置，旧页面没有默认推理强度控件。此前127.0.0.1证书警告未绕过；不能因此把所有本机Safari验收都列为不可执行。
2. 运行仓已配置模型时只PUT reasoningEffort返回400“请至少配置一个模型”；规范仓已有省略模型字段保留逻辑，但运行旧版缺失。运行仓补齐ModelInput/ActiveModelInput保留语义，与规范仓兼容，保留旧版单提供商及认证逻辑。
3. `643d73c`增加五档默认推理强度与独立保存按钮；仅PUT reasoningEffort，不提交未保存的模型/提供商/密钥草稿，不要求模型已配置。保存后读回校验，重复点击只写一次，失败可重试；完整模型表单继续校验模型目录。后端拒绝非法档位。已有会话的独立选择不被新默认改写。
4. 打包门禁暴露plugins/surface.json生成时间使旧source_sha变化。`c73e18b`将该运行时快照从指纹和Docker上下文排除，新增只读source-sha命令；插件实际代码仍参与指纹，启动后CLI与镜像label保持一致。没有删除运行时快照或绕过导出门禁。

## 实测

- 规范仓定向Go race13.231s；运行仓17.379s，通过。五档未配置模型保存/GET读回/新App重新载入，非法值不改状态，配置模型时部分更新保留模型及会话high均覆盖。首次隔离断言读了createSession返回的旧副本，改为GET真实会话后规范仓通过；运行仓400复现是真缺口，补丁修复后通过。
- Node回归通过：仅reasoningEffort请求体、五档、双击、请求中继续编辑、失败/重试、刷新读回与预配置控件。源码指纹测试通过：生成快照新增/改时间不变，插件代码改变会变。
- **真实正式8097 Safari**：更新后选择高→点击独立保存→出现“默认推理强度已保存”；API确认high且仍未配置模型；仅重启AIDE后刷新并重新打开设置仍显示高；选回自动并保存，API确认恢复原始auto。未配置/更改真实模型或API密钥。
- Safari原先弹出保存密码提示，选择“以后”，未保存凭据。验收完成后页面自动锁屏，后续知识星图等真实交互等待用户手动解锁；未获取密码或绕过锁。
- 隔离真实SSH当前源码race17.639s，通过TestSSHLiveGitHistory与TestSSHLiveWorkspaceAndIndependentSource，包括历史/恢复/记忆/搜索/时间线/渐进索引，以及受限模式403且目标文件不存在、测试夹具明确完全访问时退出码1/7、取消清理远端进程组。不是用户SSH主机或物理断网验收。
- 首轮SSH旧夹具错误地期望受限远端Shell成功，整体失败19.555s，不计通过。`d9abea6`增加明确拒绝断言，并仅在一次性夹具子测试中临时选择完全访问；生产设置保持。
- 首次full在SSH夹具对齐时主动终止，未计PASS。最终full-profile正在运行，结果独立记录，不以定向或SSH通过替代完整门禁。

## 本机镜像、备份及离线包

只更新aideMacMini/aide的AIDE服务，其他服务保持。当前镜像 `sha256:f34edc6d8afdac9135210d31de78d28c99132fcdf445032b1792d39590256036`，版本 `0.1.17.0 RC2`、构建 `c73e18b-compat`，真实运行仓兼容树；不是规范仓纯净提交逐字相同。源码label `72d302f5db22b2346db0a2f54714d33b0097cab165b5c5f0f9990f60e8a1ce6b`，启动后source-sha实测匹配。

healthz、鉴权config/sessions/approval-policy均200；reasoning-settings.js/app.js/index.html/locales/en.js服务字节匹配运行仓源码；原auto、workspace-write、辅助审批关闭及revision0保留。

旧镜像 `aide:reasoning-previous-20261014` 为sha256:f642732bec958ba41e22e295f20551f4801e22afb3481cf2d9b9401c3a1c8037。初次更新备份为运行仓`.agent-state/reasoning-default-settings/data-before-update.tar.gz`；最终一致备份`data-before-final-image.tar.gz`：14308字节、39项，SHA-256 `e243da5f1a73ab33423fb6ac7ddc9f988c834a7a016191eca2d9e6888c8fbd8c`。仅检查备份可读与旧镜像可用，没有执行生产数据恢复演练。回切命令：`AIDE_IMAGE=aide:reasoning-previous-20261014 docker compose up -d --no-deps aide`；不自动覆盖数据卷。

离线启动包 `docker-images/aide-0.1.17.0-RC2-linux-arm64-f34edc6d8afd-offline.tar.gz`，561599516字节，Linux ARM64，SHA-256 `89fba30348272da024000c5ef46633f7937a50fcb6ba88fe430786566588b02d`。规范仓和运行仓均保留，外层及内部镜像校验和实际docker load身份验证通过；历史SHA256SUMS条目保留。另一端解压运行包内start.command，仅导入镜像，不重新拉取基础镜像/依赖或编译。未在另一设备、AMD64或全新Docker引擎验收，不宣称iCloud已同步完成；归档不进入Git，不执行registry push。

## 主目标边界与下一步

主账本新增followup103和12工作流逐项剩余矩阵，链接近期会话模型、沙箱、分段输出、SSH Git批次。记录已有证据及适用范围，未将12条任意一条整体改为pass。当前回滚位置/镜像/备份已落实，正式Release仍需完整范围验收与发布基线。全量测试、普通浏览器锁定、用户真实SSH、长时/大容量/成果舱及其他剩余场景分别跟踪；无“源码push=镜像发布”或“单项PASS=全目标完成”的推断。

证据：两仓`.agent-state/reasoning-default-settings/`的go-final/go-runtime-final/browser-result/restart-check/smoke/build-stable/export记录；规范仓ssh-final和full-final日志。临时日志、卷备份和私有会话数据不推送。
