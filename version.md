# aide 版本记录

**当前版本：0.1.11.0 RC1**

## 0.1.11.0 RC1（2026-09-25）

- 界面英文化：Web 前端全站中英双语；i18n 片段拼接收口为整句 + 占位符，en.js 词条随各新模块同步补全
- 离线 Docker release：compose 固化 pull_policy:never、新增 scripts/docker-release.sh，start.command 离线导入不再联网拉取；web_search 端点改为 env 配置（AIDE_WEBSEARCH_URL），离线/保密环境优雅降级
- 锁屏主从层级联动：BroadcastChannel 选举，主界面锁定/解锁驱动所有从界面，从界面单独解锁不影响主界面
- Touch ID / WebAuthn 解锁：注册/断言端点、凭证管理与锁屏指纹按钮（须经 localhost、RP ID 不可用 IP，不满足时给出提示）
- 会话工具调用紧凑化：历史工具调用由大而空的虚线框重做为对齐流式风格的可折叠工具组（外层按 run 聚合计数 + 内层单条命令/结果），参数完整不截断、长结果内部滚动；list_files「当前目录」、搜索 query 等折叠摘要正确识别；辅助资料来源条精致化（统一类型图标、RW/锁标记、紧凑胶囊，触摸/桌面兼顾）
- iPad 移动控制台方案：proposals/ipad 单文件方案（iPad 经 Tailscale 零公网组网连常驻 aide 主机、Safari PWA；仅方案不实施）
- 在途（本版本未完成）：TTS 自然度（edge-tts 神经音 + Web Speech 降级前端已入库，后端在途，待盲听样本定默认音色）；外部 AI 调试接口（无障碍开关，排队中）

## 0.1.10.2 RC1（2026-09-24）

- 持久记忆系统：read_memory/write_memory 工具，记忆文件存缓存目录，每次会话自动注入系统提示
- 模型设置窗口加预设按钮：32K/64K/128K/200K/256K/1M 一键选，也可自定义输入
- 工具调用轮次从硬编码 10 改为默认 60，设置→权限管理可配（5-200）
- md 渲染修复：vendor mermaid@10 流程图自动渲染 SVG；md 相对路径链接拦截为 aide 内部打开，不再 404
- 轨迹面板加导出按钮（一键导出 Markdown）；加调用分析视图，主/子 Agent 调用表格支持按工具和谁过滤
- 上下文预览加彩色堆叠条形图，按 token 占比着色，hover 高亮显示详情
- 子 Agent：spawn_subagent 工具创建关联主会话的子会话，完成后自动归档
- 失败反馈循环：API 抖动自动重试一次；同一工具连续失败 3 次注入止损提示
- Codex 风格三级沙箱：read-only / workspace-write（默认）/ danger-full-access，权限面板可切
- 提前停止或轮次超限时保留已有流式输出，不再显示"未返回任何内容"
- AI 工作流模式：输入框上方弹出需求/设计/实施/验证四个彩色阶段按钮（蓝/橙/紫/绿，依次弹入动画，对话模式隐藏）；各阶段强流程自动建档，REQ/DESIGN/IMPL/TEST 唯一编号并维护索引与差异/变更记录
- 自动模式：不选阶段时由 lead 智能体调度专业子 agent 并行完成四阶段，最终由前台接需求 agent 逐条讲解核对形成闭环；自动路由按各配置实际参数（而非名称）匹配并传给子 agent，配置不足时暂停启动并给出参数建议
- 推理强度选择：策略浮层新增自动/关闭/低/中/高五档，按 DeepSeek 实际参数映射 thinking / reasoning_effort
- 语音小秘：麦克风按钮调用浏览器 Web Speech API 实时转写，后端 AI 甄别"传达给 AI / 背景噪声 / 与他人闲聊"（send/ignore/standby），识别闲聊自动退下，名字可在设置自定义
- 权限管理：设置中 per-tool 开关可单独禁用各内置工具并持久化，后端在工具列表构建与执行两处双重拦截
- 搜索能力：web_search 走 DuckDuckGo HTML 抓取并以 SearXNG 兜底（不依赖百度类商业 API）；semantic_search 用本地 TF-IDF 余弦相似度做离线语义检索；全局会话搜索同时覆盖归档内容
- draw.io 插件：create_diagram 生成 .drawio，文件视图以 iframe 渲染，可在视窗内编辑并保存回写，支持多类型文件关联
- 模型设置卡片重排为两行结构（单选+名称+删除 / 上下文窗口预设+自定义），选中蓝框高亮；所有模态弹窗支持点击遮罩空白关闭
- 消息操作：每条消息下提供复制/重试/继续/好的回答/有问题的回答，好/坏反馈写入记忆用于后续优化
- 默认排队模式：会话中继续输入默认排队而非插入；会话结束后左侧标题自动分析刷新
- 修复：contextTools 自死锁、spawnSubagent 数据竞争、调用分析表头 [object] 渲染、retry 路由 404、search_text 必填字段、策略浮层"自动路由"换行

## 0.1.10.1 RC1（2026-09-24）

- run_shell 工具从"只生成提案"改为在容器沙箱内实际执行并返回 stdout/退出码；write_file 仍保持提案审批；systemPrompt 与工具描述同步更新
- 归档当前正在查看的会话后自动回到新会话输入页（与删除当前会话一致）；取消归档不打断当前视图
- 运行中插话/排队消息加上下文包装：明确告知模型这是回答过程中的补充而非新话题，避免接不住上文；空输出轮次不再写入空 assistant 消息
- 运行中输入新消息默认走排队模式（按钮默认高亮），需要插话时再点一下切到即时插入
- 设置面板新增「权限管理」栏，展示各工具当前权限；run_shell 入口加危险命令黑名单（递归删除/提权/推送远端/pipe-to-shell 等），命中即拒绝执行

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
