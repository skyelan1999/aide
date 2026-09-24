# aide 版本记录

**当前版本：0.1.10.1 RC1**

## 0.1.10.1 RC1（2026-09-24）

- 运行中输入新消息默认走排队模式（按钮默认高亮），需要插话时再点一下切到即时插入

## 0.1.10.0 RC1（2026-09-24）

- 会话管理与状态灯 — 置顶/归档/删除/导出、运行绿闪/审批黄/失败红/完成蓝点高亮、完成后标题自动概括、点击轻量化与竞态修复


## 0.1.9.0 RC1（2026-09-24）

- 运行中排队与插话、Codex 风格队列条、发送/停止同键与一键回底


## 0.1.8.0 RC1（2026-09-24）

- SSE 流式输出与 DSH/Codex 风格显示、离线基础镜像与启动修复


## 0.1.7.0 RC1（2026-09-24）

- Reference source tools and environment guide; unified startup and compact workspace navigation


## 0.1.6.0 RC4（2026-09-23）

- Final product tagline, professional workbench UI, agent workflow and installer


## 0.1.6.0 RC3（2026-09-23）

- AI + IDE for focused professional work; unified agent workflow, installer and Docker delivery


## 0.1.6.0 RC2（2026-09-23）

- 修复 Docker 构建版本号含空格的 ldflags 引用；交付可定制基座与安装镜像


## 0.1.6.0 RC1（2026-09-23）

- 可定制 AI 工作台基座：macOS UI、专业与经典主题、统一 Agent 路由、正式文档、环境安装和 Docker 镜像交付


## 0.1.5.0 RC5（2026-09-23）

- 合入 remediate/r01-r07 独立整改批次：R01-R10 全部闭环（压缩并发/持久化、工作区身份、路径与远程内容统一策略、摘要链、工具预算与证据链、空白字节保留、前端竞态、统计真实性+按模型费率、上下文预算预览、构建身份与恢复演练、文档与发布回滚步骤），验收证据见 docs/reviews/2026-09-23（含真实 Chromium 浏览器 12/12）


## 0.1.5.0 RC4（2026-09-23）

- 修复消耗统计热力图悬停浮窗跑偏被裁：showTip 误以 settings-sheet 为定位基准（实际 offsetParent 是 .token-stats），改为相对 offsetParent 居中+按实际宽高钳制+上方不足翻下方；?v=17


## 0.1.5.0 RC3（2026-09-23）

- 修复设置面板余额不显示：app.js 陈旧重复块（坏合并残留）遮蔽 token 统计 v4（余额 chip/计价配置/单日明细），删除后恢复；?v=16


## 0.1.5.0 RC2（2026-09-23）

- PRD 全量核对：FR-23 真实模型端到端验收通过（deepseek-v4-pro）；propose 步骤强制 response_format=json_object 修复偶发方案 JSON 失败；FR-33 状态修正（FR-81 已落地）


- 日常更新


## 0.1.5.0 RC1（2026-09-22）

- 产品化三件套：DSH 轨迹视图（任务事件时间线 + 每任务 Token 用量）、全局聊天搜索（⌘K 检索会话缓存）、会话缓存压缩（自动+手动结构化摘要，参考 DSH compaction 与 Codex）；设置面板 JSON 导航、marked 渲染引擎、Token 统计等架构重构记录见 PRD §12


## 0.1.4.4 RC1（2026-09-22）

- Token 消耗统计：git 提交图风格热力图、计价可配置（输入/输出每百万）、单日计费明细与所在周合计、账户余额查询（/user/balance）；设置面板升级为 JSON 驱动的侧边导航+二级菜单


## 0.1.4.3 RC1（2026-09-22）

- 文件打开默认 md 渲染预览（marked v12 GFM 全特性 + JS 高亮 + DOM 消毒）；新标签页单文件视图（路径 + 编辑保存 + 预览切换）


## 0.1.4.2 RC1（2026-09-22）

- 策略弹层整合模型选择（左策略右模型双栏）；每次新任务自动总结主题并更新会话标题


## 0.1.4.1 RC1（2026-09-22）

- AI 回复 Markdown 渲染与 JavaScript 语法高亮；上下文统计卡压缩为单行细条


## 0.1.4.0 RC1（2026-09-22）

- 辅助资料多来源注册表：本地/Skill/链接/MCP/SFTP/FTP/FTPS/SMB 多协议来源，登记记录存缓存文件夹，自动系统文档自动挂载并标记读写


## 0.1.3.0 RC1（2026-09-22）

- 工作空间配置与模型工具闭环：宿主机目录浏览与挂载、本地/SSH·SFTP 连接与单会话控制台、AI 工作目录识别修复、插件协议 v1.1 可执行工具、内置四工具（写/命令走提案审批）


## 0.1.2.0 RC1（2026-09-22）

- 插件系统与 DSH 协议 v1：深度研究思考入口卡、插件面板（上传/搜索/启用停用/删除）、插件协议标准文档、默认预装 8 个 DSH 能力预设插件


## 0.1.1.0 RC1（2026-09-22）

- 多模型管理与上下文统计：模型列表管理（自动获取可用模型名）、侧栏模型选择器、DSH 风格上下文统计卡；参考 DeepSeek DSH 设计


## 0.1.0.0 RC1（2026-09-21）

- 建立版本管理机制：四位版本号（产品级·重大·大版本·日常）+ RC 补丁号，格式 `X.Y.Z.W RCn`
- `version.md` 承载当前版本与 release note；`scripts/version.sh` 托管升级流程（bump / patch / note / tag / check / install-hooks）
- 版本升级以 git tag `vX.Y.Z.W-RCn` 打在对应提交上，**仅 main 分支打 tag**
- git 钩子（prepare-commit-msg）自动为每次提交标注当前版本号
- 界面展示当前版本：侧栏运行卡片与设置面板底部（`/api/config` 返回 version）
