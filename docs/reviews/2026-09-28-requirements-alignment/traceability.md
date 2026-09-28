# 需求追踪对照表（traceability）

> 基线：`feature/permission-panel` @ `f9c4e02`。方法：静态阅读（Read/Grep/Glob），未运行时验证。
> 状态取值：**已交付**＝代码符号真实存在（多有单测）；**部分**＝主干在、有明确缺口；**未开始**＝无任何实现；**不一致**＝PRD 描述/状态与代码不符。
> 「证据」列给出 文件:符号 或 PRD 章节。验证状态分 `[测试]`(有 *_test.go) / `[代码]`(存在未本次运行验证)。

## M1 会话管理（SM-01~13）

| 编号 | PRD 描述摘要 | 真实状态 | 证据（文件:符号 / 文档） |
| --- | --- | --- | --- |
| SM-01 | 新建/列表/加载会话，持久化 /data | 已交付[测试] | server.go 会话 CRUD 路由；session-manage.json；server_test.go |
| SM-02 | 置顶/归档/删除/导出（PATCH/DELETE/GET export） | 已交付[代码] | app.js:223 `PATCH {pinned}`；server.go `/api/export`；archive_test.go |
| SM-03 | 会话状态灯（绿/黄/红/蓝） | 已交付[代码] | server.go task.Status；app.js 状态渲染 |
| SM-04 | 标题自动概括（并发、独立 ctx） | 已交付[测试] | workflow.go:430 `summarizeTopic`；:310 `a.background(...)`；server_test.go:116,303 |
| SM-05 | 归档后自动回新会话 | 已交付[代码] | app.js loadSessions/归档流；PRD §6.2 已实现 |
| SM-06 | 运行中标题自动刷新 | 已交付[代码] | app.js 轮询 + scheduleTitleSync |
| SM-07 | 全局搜索 ⌘K（含归档+标签） | 已交付[代码] | server.go `GET /api/search` |
| SM-08 | 排队/插话（FIFO、容量4、429、默认排队） | 已交付[测试] | queue_steer.json；queue_steer_test.go；a.commands 缓冲 channel |
| SM-09 | 插话上下文包装 wrapSteer | 已交付[代码] | workflow.go:940 `wrapSteer` |
| SM-10 | 发送/停止同键 + 一键回底 | 已交付[代码] | app.js 输入区按钮态/sticky 回底 |
| SM-11 | 历史压缩（>48000 字节自动+手动） | 已交付[代码] | workflow.go 历史摘要压缩逻辑 |
| SM-12 | 取消任务（保留已产出流） | 已交付[测试] | shell_cancel_test.go；/runs/{run}/cancel；stream_broker.go interrupted 保留 |
| SM-13 | 配置备份导出/导入（信封+敏感取舍） | 已交付[测试] | config_backup.go；config_backup_test.go；config_import_test.go |

## M2 工作区与文件（WF-01~13）

| 编号 | PRD 描述摘要 | 真实状态 | 证据 |
| --- | --- | --- | --- |
| WF-01 | 工作区配置（本地/SSH·SFTP/最近挂载/缓存） | 已交付[测试] | workspace_config.go；workspace-path-picker-sftp.json；ssh_session.go |
| WF-02 | 双根（/workspace RW /context RO /local 范围） | 已交付[测试] | paths.go；workspace_semantics_test.go；AIDE_LOCAL_ROOT |
| WF-03 | 文件浏览/导航（列目录 2000、上级、面包屑） | 已交付[代码] | files.go `GET /api/files` |
| WF-04 | 新建/编辑/保存（哈希冲突、原子写） | 已交付[测试] | files.go `PUT /api/file`；app.js:1703 save-file 带 hash |
| WF-05 | 文件新标签页 /file-view | 已交付[代码] | app.js:4786 file-view 渲染；:4847 setupDrawioFrame |
| WF-06 | 附加到任务（≤8 个、≤80KB、先附加再改） | 已交付[测试] | attachments.go；attachments_test.go |
| WF-07 | Markdown 渲染（marked+高亮+消毒） | 已交付[代码] | web/vendor marked；app.js md 渲染 |
| WF-08 | mermaid 渲染（vendor mermaid@10） | 已交付[代码] | web/vendor mermaid；```mermaid 块替换 |
| WF-09 | md 相对链接内部打开 | 已交付[代码] | app.js 事件委托拦截 `.md` |
| WF-10 | Office 解析（docx/xlsx/pptx→文本） | 已交付[代码] | office.go；read_file 自动转文本 |
| WF-11 | 引用 sources（多类型、list_sources、mcp_call 只读） | 已交付[测试] | sources.go；mcp_source.go；sources_test.go |
| WF-12 | 文本读取限制（256KiB/60KiB 截断） | 已交付[测试] | files_limit_test.go |
| WF-13 | md 相对图片（/api/file/raw token、三处视图、失败不破图） | 已交付[测试] | app.js:3588 起 `/api/file/raw?access_token=`；附件/模态/file-view |

## M3 内置工具（TL-01~18；实际 builtinTools 22 个）

| 编号 | PRD 描述摘要 | 真实状态 | 证据 |
| --- | --- | --- | --- |
| TL-01 | list_sources | 已交付[测试] | workflow.go:117 |
| TL-02 | list_files（截断 100/4KB） | 已交付[测试] | workflow.go:118 |
| TL-03 | read_file（Office 转文本、offset/limit 分页） | 已交付[测试] | workflow.go:119；office.go |
| TL-04 | mcp_call（只读 MCP 工具） | 已交付[测试] | workflow.go:120；mcp_source.go ⚠编号与 write_file 重号 |
| TL-04 | write_file（提案不直写） | 已交付[测试] | workflow.go:125 ⚠PRD 编为 TL-04（与 mcp_call 重号） |
| TL-05 | run_shell（沙箱实执行） | 已交付[测试] | workflow.go:126；command.go；execShellCommand |
| TL-06 | spawn_subagent（子会话+自动归档） | 已交付[测试] | workflow.go:127,2785 |
| TL-07 | read_memory | 已交付[测试] | workflow.go:128；memory_access.go |
| TL-08 | write_memory（追加列表项） | 已交付[测试] | workflow.go:129；memory_access.go |
| TL-09 | search_text（关键字/正则） | 已交付[测试] | workflow.go:130（required=["query"]，OQ-02 已关闭） |
| TL-10 | semantic_search（自称向量检索） | 不一致 | workflow.go:131 描述 "Semantic vector search"，实为 `:2181 semanticSearch` **离线 TF-IDF 余弦**（OQ-03 未修） |
| TL-11 | create_diagram + draw.io 前端联动 | 部分 | 工具 workflow.go:132 已交付；前端 app.js:3547 init 握手/空白/save 回写/超时提示**已做**；缺 `.drawio.svg/.png` 关联、会话消息内渲染第三调用点 |
| TL-12 | web_search（DDG 爬取+SearXNG fallback） | 已交付[代码] | workflow.go:133,1999-2060（离线降级提示） |
| TL-13 | create_requirement | 已交付[代码] | workflow.go:134；system-docs 建档 |
| TL-14 | create_design | 已交付[代码] | workflow.go:135 |
| TL-15 | record_implementation | 已交付[代码] | workflow.go:136 |
| TL-16 | record_verification | 已交付[代码] | workflow.go:137 |
| TL-17 | 插件工具路由（结果可转提案） | 已交付[测试] | workflow.go:2967 插件分支；plugins.go；plugin_daemon.go |
| TL-18 | ask_user（一次一问、暂停等答） | 已交付[测试] | workflow.go:138,2878；本批 P2#3 `afee71` 修轮次竞态 |
| — | （PRD 未登记）docx_structure/list_comments/add_comment/resolve_comment | 已交付[测试] | workflow.go:121-124；comments.go；comments_test.go — **未计入 111 点** |

## M4 模型与策略（MD-01~08）

| 编号 | PRD 描述摘要 | 真实状态 | 证据 |
| --- | --- | --- | --- |
| MD-01 | 多模型管理（≤20、窗口约束） | 已交付[测试] | server.go Models；models_test.go |
| MD-02 | API 连接配置（密钥不回传、可清除） | 已交付[测试] | apikey_vault.go；hasKey omitempty；server.go:1109 hasKey |
| MD-03 | 自动获取模型（/models 拉取） | 已交付[代码] | server.go 模型发现 |
| MD-04 | 上下文窗口预设（6 档+自定义） | 已交付[代码] | server.go contextWindow 校验 |
| MD-05 | 参数 Profile（temp/top_p/max_tokens） | 已交付[测试] | profiles.go；profiles_test.go |
| MD-06 | 手动/自动策略（auto 按任务匹配） | 部分 | PRD 行内自标「部分实现」；manual 完备，auto 路由粗（依赖 AG-05 暂停建议）；§6.1 M4 却计 8 已实现，口径不一致 |
| MD-07 | 余额查询 /api/balance | 已交付[代码] | server.go `/api/balance` |
| MD-08 | 推理强度五档（真实转 API 参数） | 已交付[测试] | provider.go:85-91 注入 thinking/reasoning_effort；OQ-06 已关闭 |

## M5 权限/沙箱/安全（AU-01~09）

| 编号 | PRD 描述摘要 | 真实状态 | 证据 |
| --- | --- | --- | --- |
| AU-01 | 三级沙箱模式 | 已交付[测试] | server.go:1109 SandboxMode；readOnlyAllowed |
| AU-02 | per-tool 开关（DisabledTools 持久化） | 已交付[代码] | server.go DisabledTools；contextTools() 过滤 |
| AU-03 | run_shell 危险拦截（黑名单） | 已交付[代码] | workflow.go shellBlocked；:1310-1314 pipe-to-shell 检测 |
| AU-04 | 超时60s/并发4/截断128KB | 已交付[代码] | command.go:134,127,35-48 |
| AU-05 | 路径越界/`bash --norc`/SSH 不自动跑 | 已交付[测试] | command.go:104-126 safePath/EvalSymlinks；:136 --norc；workflow.go:2420-2421 SSH 拦截 |
| AU-06 | 失败反馈循环（连错≥3 止损） | 已交付[测试] | workflow.go analyzeShellFailure；:2537 建议 |
| AU-07 | 提前停止保留已产出 | 已交付[测试] | stream_broker.go interrupted/failed 保留缓冲 |
| AU-08 | 工具轮次上限（默认60、5–200） | 已交付[测试] | server.go:54,146,162 |
| AU-09 | 账户/空闲锁屏（SHA-256、verify-password） | 已交付[测试] | server.go:72 LockTimeoutSec；kdf.go；auth_verify.go；lock_state.go |

## M6 记忆/性格/反馈（MM-01~05）

| 编号 | PRD 描述摘要 | 真实状态 | 证据 |
| --- | --- | --- | --- |
| MM-01 | 持久记忆 memory.md + 自动注入 | 已交付[测试] | memory_access.go；personality_evolution_test.go |
| MM-02 | 👍/👎 反馈写记忆 | 已交付[代码] | server.go `/api/feedback` |
| MM-03 | 性格 AES-256-GCM 加密存储 | 已交付[测试] | persona.go；kdf.go；settings_persona_persist_test.go |
| MM-04 | 性格注入系统提示 | 已交付[测试] | persona.go；context.go 注入「## 你的性格」 |
| MM-05 | 插件感知训练（性格/记忆结合插件能力） | **已交付[测试]**（PRD 原标「待开始」=错误） | plugin_learning.go（头注 MM-05）；context.go:278,281 注入；workflow.go:1144 记录；plugin_learning_test.go |

## M7 多 Agent（AG-01~05）

| 编号 | PRD 描述摘要 | 真实状态 | 证据 |
| --- | --- | --- | --- |
| AG-01 | spawn_subagent 派生（继承工作区/Profile） | 已交付[测试] | workflow.go:2785；session_number_test.go |
| AG-02 | 子会话完成自动归档 | 已交付[代码] | autoArchived 标记 |
| AG-03 | 层级列表（↳ 缩进、折叠、孤儿兜底） | 已交付[代码] | app.js 会话列表渲染 |
| AG-04 | 自动模式多智能体编排（Lead 澄清→分工→核对） | 已交付[测试] | workflow.go:1496 autoModePrompt；:1501-1508 强约束多次 spawn+核对 |
| AG-05 | 自动路由按真实数值匹配（不足暂停） | 已交付[测试] | workflow.go:1528 profileInventoryPrompt 引导 |

## M8 工作流四阶段（FL-01~06）

| 编号 | PRD 描述摘要 | 真实状态 | 证据 |
| --- | --- | --- | --- |
| FL-01 | 四阶段按钮 UI（对话模式隐藏、stagger） | 已交付[代码] | app.js 输入框上方阶段按钮；workspace-assistant-colors.json |
| FL-02 | 需求阶段（强约束 create_requirement） | 已交付[代码] | workflow.go:1491 requirementPhasePrompt |
| FL-03 | 设计阶段（先读 REQ、create_design、画 drawio） | 已交付[代码] | workflow.go:1724-1725 designPhasePrompt |
| FL-04 | 实施阶段（写真代码+run_shell 验证+record） | 已交付[代码] | workflow.go:1733 implementationPhasePrompt |
| FL-05 | 验证阶段（真实执行+量化+record_verification） | 已交付[代码] | workflow.go:1743 verifyPhasePrompt |
| FL-06 | 索引自动维护（重写 *-index.md） | 已交付[代码] | system-docs 索引重写工具 |

## M9 轨迹/上下文/Token（TR-01~10）

| 编号 | PRD 描述摘要 | 真实状态 | 证据 |
| --- | --- | --- | --- |
| TR-01 | 轨迹面板（事件时间线+真实 token） | 已交付[测试] | debug.go；stream_events_test.go |
| TR-02 | 会话导出 Markdown / 全部 JSON | 已交付[代码] | server.go 导出路由 |
| TR-03 | 调用情况分析（按工具/agent/时间过滤） | 已交付[代码] | debug.go 调用表 |
| TR-04 | 上下文消耗堆叠条形图 | 已交付[测试] | context_breakdown_test.go；context-usage-breakdown.json |
| TR-05 | 上下文预算预览（共用构建器、超限拦截） | 已交付[测试] | server.go:1001 `/api/context-preview`；workflow.go:258 同构建器；context_breakdown_test.go:48 |
| TR-06 | 请求快照证据链（RequestSnapshot） | 已交付[代码] | server.go requests 快照路由 |
| TR-07 | Token 费用热力图（可配费率） | 已交付[代码] | debug.go 费用统计；app.js 热力图 |
| TR-08 | SSE 流式逐 token（断连轮询降级） | 已交付[测试] | workflow.go:3364 runEvents；stream_events_test.go |
| TR-09 | SSE 顺滑排查（flush/代理/前端渲染） | 部分/待验证 | 后端已就绪：runEvents:3388-3422 每事件 flush+X-Accel-Buffering:no+15s 心跳；长回答顺滑性未实测（OQ-08） |
| TR-10 | 思考过程默认折叠 details | 已交付[代码] | app.js 思考过程折叠条；reasoning_content |

## M10 部署/运行（DP-01~06）

| 编号 | PRD 描述摘要 | 真实状态 | 证据 |
| --- | --- | --- | --- |
| DP-01 | 一键启动（start.command→compose，:8097） | 已交付[代码] | start.command/start.sh；compose.yaml；.env AIDE_PORT |
| DP-02 | Docker 工具环境（Go/Py/Node/Git、go:embed） | 已交付[代码] | Dockerfile；go:embed web |
| DP-03 | 数据卷分离（workspace/context/local/data/home） | 已交付[代码] | compose.yaml volumes |
| DP-04 | 镜像导入导出（scripts/aide.sh export、SHA256） | 已交付[代码] | scripts/aide.sh；docker-images/ |
| DP-05 | 健康检查 /healthz | 已交付[代码] | server.go `/healthz` |
| DP-06 | 本地令牌登录（粘贴 token、401 弹框） | 已交付[测试] | auth_verify.go；login-dialog |

## M11 插件系统（PL-01~04）

| 编号 | PRD 描述摘要 | 真实状态 | 证据 |
| --- | --- | --- | --- |
| PL-01 | 插件协议 v1.1（DSH/Cordis、上传即校验） | 已交付[测试] | plugins.go；plugin_host.js；plugins_test.go |
| PL-02 | 插件面板（上传/启停/删除、surface） | 已交付[代码] | app.js:2922-2967；`/api/plugin-surface` |
| PL-03 | 插件工具进循环（可转提案） | 已交付[测试] | plugin_daemon.go；workflow.go 插件路由 |
| PL-04 | 默认预装 8 个 DSH 插件 | 已交付[代码] | plugins/ 预置插件集 |

## M12 外观/交互（UI-01~07）

| 编号 | PRD 描述摘要 | 真实状态 | 证据 |
| --- | --- | --- | --- |
| UI-01 | 主题（专业/经典×浅深跟随系统） | 已交付[代码] | web/themes/；macos.css |
| UI-02 | 中英 i18n（不翻译输入/回复） | 已交付[测试] | i18n.js；web/locales/；english-support.json |
| UI-03 | 模态遮罩点击关闭 | 已交付[代码] | app.js 事件委托遮罩 |
| UI-04 | 消息操作条（复制/重试/继续/好/坏） | 已交付[测试] | retry 路由已注册（OQ-01 关闭）；app.js 操作条 |
| UI-05 | 三栏桌面布局（窄屏/720p 适配） | 已交付[代码] | app.js/style.css 三栏 |
| UI-06 | 设置面板（JSON 驱动导航） | 已交付[代码] | settings-schema.json；settings-init.js |
| UI-07 | 策略浮层文字换行修复 | 已修复[代码] | style.css；PRD §6.2 |

## M13 语音小秘（VO-01~07）

| 编号 | PRD 描述摘要 | 真实状态 | 证据 |
| --- | --- | --- | --- |
| VO-01 | 麦克风听写（Web Speech API） | 已交付[代码] | app.js webkitSpeechRecognition |
| VO-02 | 小秘独立 Agent（voice-filter 研判） | 已交付[测试] | voice_agent.go；server.go:1003,1428 voiceFilter；assistant_agent_test.go:167 |
| VO-03 | 动态断句直接发送（降级 send 原文） | 已交付[测试] | server.go:1501 action 映射 send/ask |
| VO-04 | 环境声甄别 send/ignore/standby | 已交付[测试] | assistant_agent_test.go:172 silent→ignore |
| VO-05 | 语音历史可视化（≤200 可清空） | 已交付[代码] | voice_samples.go；`/api/voice-history` |
| VO-06 | 小秘设置/历史加密/改名/双向朗读 | 已交付[测试] | voice_personality.go；tts/；server.go:1109 语音字段 |
| VO-07 | 模式兼容（排队/插入、自动朗读） | 已交付[代码] | 复用主发送链路；accessibilityAutoRead |

---

## 汇总

| 状态 | 数量 | 编号 |
| --- | --- | --- |
| 已交付（代码存在，多有单测；运行时 E2E 由并行验证 agent 负责） | **108** | 除下列外全部 |
| 部分交付 | **3** | MD-06、TL-11（drawio .svg/.png 关联）、TR-09（顺滑待实测） |
| 未开始 | **0** | 无（PRD 原列 MM-05/TR-09 均已动工，详见下） |
| 文档与代码不一致（非功能缺失） | **5 项** | 见 README §④ |

> 相对 PRD §6.1 原口径（109 已实现 / 2 待开始）：MM-05 实为已交付、TR-09 后端已就绪改判部分；MD-06 本就是部分实现却被 §6.1 计入已实现。净效应：已交付 108、部分 3、未开始 0。
>
> 口径自洽说明：本台账「严格口径」= 已交付 108、部分 3（MD-06/TL-11/TR-09）、未开始 0。PRD §6.1 沿用其「部分并入已实现」的汇总口径，故记 110 已实现 / 1 待验证（TR-09 长回答实测）；两个口径相差的 2 = MD-06、TL-11，差异仅在统计约定，不影响结论。
