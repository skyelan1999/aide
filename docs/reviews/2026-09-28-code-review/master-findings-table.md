| ID | 级 | 分类 | 位置 | 置信 | 状态 | 标题 |
|----|----|------|------|------|------|------|
| backend-http-001 | P1 | 安全 | `internal/server/server.go:2000-2017` | high | ✅已修 | getSession 未校验小秘会话密码门，直接返回完整消息 |
| backend-http-002 | P1 | 逻辑错误 | `internal/server/server.go:1989` | high | ✅已修 | deleteAllArchived 仍按旧平铺路径删文件，归档会话删不净、重启复活 |
| backend-integrations-001 | P1 | 资源泄漏 | `internal/server/ssh_session.go:394-405` | high | ✅已修 | 每来源 SFTP 的私钥/口令临时文件写入 /tmp 后从不清理 |
| backend-integrations-002 | P1 | 安全 | `internal/server/mcp_source.go:105` | high | ✅已修 | MCP stdio 子进程继承父进程完整环境变量，API Key 泄漏给任意 MCP 可执行文件 |
| backend-persona-001 | P1 | 资源泄漏 | `internal/server/office.go:71-91` | high | ✅已修 | runOfficeScript 执行 python/soffice 子进程无超时，可永久挂起 |
| frontend-viewers-001 | P1 | 逻辑错误 | `internal/server/web/app.js:5450, 5993` | high | ✅已修 | Web Speech 兜底朗读无法被中断：onerror 把 cancel 当作普通错误继续朗读下一句 |
| frontend-viewers-002 | P1 | 安全 | `internal/server/web/app.js:7279, 7300, 7307` | high | ✅已修 | SQLite 查看器表名/列名/单元格值未转义直接拼 innerHTML |
| frontend-viewers-003 | P1 | 资源泄漏 | `internal/server/web/app.js:3680-3708` | high | ✅已修 | STL 3D 查看器关闭/切换时不销毁 renderer、rAF 循环与 ResizeObserver |
| frontend-viewers-004 | P1 | 安全 | `internal/server/web/app.js:5112, 5117-5118` | medium | ✅已修 | mermaid 以 securityLevel=loose 渲染且 svg 经 innerHTML 直插，AI 产出可触发脚本 |
| render-validation-001 | P1 | 代码异常 | `internal/server/web/app.js:3767, 3776` | high | ✅已修 | STL 3D 查看器全部失败：ReferenceError 'geometry is not defined' |
| backend-core-001 | P2 | 逻辑错误 | `internal/server/workflow.go:2924-2932` | medium | 📝记录 | ask_user 应答通道 buffered(1) 存在陈旧应答串到下一个澄清问题的竞态 |
| backend-http-003 | P2 | 代码异常 | `internal/server/assistant_agent.go:88-92` | medium | ✅已修 | runAssistantAgenticLoop 在 nil 检查前解引用 va.memory |
| backend-http-004 | P2 | 逻辑错误 | `internal/server/files.go:1181-1197` | medium | ✅已修 | SSH 工作区写文件：哈希冲突校验对已存在的二进制文件失效 |
| backend-http-005 | P2 | 安全 | `internal/server/files.go:202-226` | medium | ➖非缺陷 | readRawBytes/readText 跟随符号链接，目录列表却隐藏符号链接 |
| backend-integrations-003 | P2 | 安全 | `internal/server/sources.go:74-79, 190` | high | 📝记录 | 每来源 SFTP 凭据（密码/私钥）以明文 JSON 落盘，未进加密 vault |
| backend-integrations-004 | P2 | 安全 | `internal/server/ssh_keyutil.go:57-60` | high | 📝记录 | ssh-keygen -P 把私钥口令放在命令行参数中，进程列表可见 |
| backend-persona-002 | P2 | 安全 | `internal/server/voice_samples.go:56-61, 138-196, 209-264, 371-387` | high | ✅已修 | voiceSampleUpload/Delete/Audio/Wipe 的 profile 与 id 未做路径校验，可越目录读写 .enc |
| backend-persona-003 | P2 | 逻辑错误 | `internal/server/persona.go:335-347` | high | ✅已修 | personaReset 在目标人格无密文时跳过密码校验并覆盖内存 personaKey |
| backend-persona-004 | P2 | 并发与数据竞争 | `internal/server/plugin_daemon.go:166-216, 585-625` | high | 📝记录 | 插件 daemon Start/Stop 与 runPluginHost 在持有全局 a.mu 时同步阻塞 10-15s |
| backend-persona-005 | P2 | 资源泄漏 | `internal/server/plugins.go:312-318` | medium | ✅已修 | callPluginTool 用 cmd.CombinedOutput 无界缓存插件 stdout/stderr，可 OOM |
| backend-persona-006 | P2 | 并发与数据竞争 | `internal/server/assistant_history.go:180` | medium | ✅已修 | assistantMessageHandler 在未持 a.mu 时读 a.sessions map，与 dispatch 写存在并发 map 读写风险 |
| frontend-core-001 | P2 | 资源泄漏 | `internal/server/web/app.js:1772` | high | ✅已修 | 设置面板每次打开都重复挂载 window resize 监听与 aideUI 订阅，造成监听器/订阅累积泄漏 |
| frontend-core-002 | P2 | 安全 | `internal/server/web/app.js:5112-5118` | medium | ✅已修 (重复→dup-mermaid) | mermaid 以 securityLevel:'loose' 渲染模型生成的图表文本，存在 HTML/脚本注入面 |
| frontend-core-003 | P2 | 安全 | `internal/server/web/app.js:3497` | low | ✅已修 | sanitizeHtml 对 javascript: 链接的过滤未覆盖 scheme 内嵌控制字符（tab/换行/注释）的绕过 |
| frontend-css-001 | P2 | 代码异常 | `internal/server/web/style.css:21,27,36,52,53,58` | high | ✅已修 | 未定义令牌 --faint 被当作颜色使用（应为 --faintest） |
| frontend-css-002 | P2 | 代码异常 | `internal/server/web/style.css:238,284,302,312,315,316,332,335,336,343,713,716,721,723,726,728,729,731,980,990; macos.css:333` | high | ✅已修 | 多个未定义令牌无 fallback 被引用，导致背景/边框/文字声明失效 |
| frontend-css-003 | P2 | UX与交互逻辑 | `internal/server/web/style.css:289` | high | ✅已修 | .call-table .call-tool 颜色被高优先级硬编码 #a5b4fc 锁死，浅色主题下几乎不可读 |
| frontend-css-004 | P2 | UX与交互逻辑 | `internal/server/web/style.css:390-393` | medium | 📝记录 | 工作流四阶段选中态颜色硬编码，不随 green/light 主题切换 |
| frontend-css-005 | P2 | UX与交互逻辑 | `internal/server/web/style.css:306` | medium | ✅已修 | .win-preset.active 用白字压在 --accent 上，green dark 主题下对比不足 |
| frontend-viewers-005 | P2 | 资源泄漏 | `internal/server/web/app.js:3797-3799` | high | ✅已修 | PDF 查看器仅在 dialog close 时 teardown，file-view 页签与切换文件时不销毁文档/Observer |
| frontend-viewers-006 | P2 | 资源泄漏 | `internal/server/web/app.js:4343-4352, 3591-3592` | high | ✅已修 | DXF/图片查看器每次打开都新增永久 window mousemove/mouseup 监听，从不移除 |
| frontend-viewers-007 | P2 | 逻辑错误 | `internal/server/web/app.js:4526-4532` | high | 📝记录 | docx 添加批注时 anchorIndex 恒为 0（死代码，计数结果未赋值） |
| frontend-viewers-008 | P2 | 并发与数据竞争 | `internal/server/web/app.js:4986-4999` | medium | ✅已修 | 全局搜索无请求时序控制，快速输入时旧响应可能覆盖新结果 |
| frontend-viewers-009 | P2 | 资源泄漏 | `internal/server/web/app.js:3871-3887` | medium | 📝记录 | PDF 缩放/翻页对进行中的 render 无取消，快速缩放产生重叠渲染与画布闪烁 |
| render-validation-002 | P2 | UX与交互逻辑 | `internal/server/web/app.js:4676-4679` | high | ✅已修 | Markdown 预览的 fenced code block 仅 JS/TS 高亮，其余语言为单色纯文本 |
| runtime-functional-001 | P2 | 错误处理 | `internal/server/web/app.js (apply 调用处) + internal server apply 接口:POST /api/sessions/{sid}/runs/{rid}/apply` | high | ✅已修 | 文件提案 apply 冲突返回 409，前端无任何错误反馈且按钮原样残留 |
| security-001 | P2 | 安全 | `internal/server/web/app.js:5112-5118` | medium | ✅已修 (重复→dup-mermaid) | Mermaid 以 securityLevel=loose 渲染并把原始 SVG 直接 innerHTML 注入，绕过 Markdown 消毒 |
| security-002 | P2 | 安全 | `internal/server/web/app.js:7279,7300,7307` | high | ✅已修 (重复→dup-sqlite-xss) | SQLite 查看器把表名/列名/单元格值不经转义直接拼接 innerHTML |
| backend-core-002 | P3 | 资源泄漏 | `internal/server/workflow.go:1421-1422` | medium | 📝记录 | spawnSubagent 启动子 run 时未检查 a.cancels 并发上限，绕过了 4 路并发闸 |
| backend-core-003 | P3 | 资源泄漏 | `internal/server/command.go:157-161` | low | 📝记录 | 命令在 cmd.Wait() 返回后再杀一次进程组，可能命中已被 OS 复用的 PGID |
| backend-core-004 | P3 | 资源泄漏 | `internal/server/workflow.go:2443-2464` | low | 📝记录 | run_shell 工具路径不占用 a.commands 信号量，与 /api/command 的 4 路并发约束不一致 |
| backend-core-005 | P3 | UX与交互逻辑 | `internal/server/workflow.go:1035-1041` | low | 📝记录 | (c) 类上游错误盲重试会把已流式输出的 delta 重复推给前端 |
| backend-http-006 | P3 | 资源泄漏 | `internal/server/files.go:1349-1357` | medium | ✅已修 | SQLite 临时文件在 Write 失败时泄漏 |
| backend-http-007 | P3 | 安全 | `internal/server/files.go:1420-1426` | low | 📝记录 | SQLite 表名拼入 f-string SQL，存在标识符注入面 |
| backend-http-008 | P3 | 安全 | `internal/server/memory_access.go:93-95` | low | 📝记录 | canAccessMemory 对‘既非 aide 也非 assistant’路径默认放行（fail-open） |
| backend-http-009 | P3 | 边界条件 | `internal/server/attachments.go:174-178` | low | ✅已修 | readLocalImage 对非白名单扩展名产出空 MIME 的 data URL |
| backend-http-010 | P3 | 资源泄漏 | `internal/server/assistant_agent.go:200-209` | medium | 📝记录 | remember 工具写 voice-memory.json 在锁外、非原子、吞错 |
| backend-http-011 | P3 | 逻辑错误 | `internal/server/server.go:809-814` | medium | 📝记录 ⚠️决策 | save() 永远写 active 桶，archived/assistant 目录成死目录 |
| backend-integrations-005 | P3 | 安全 | `internal/server/sources.go:527-547, 338-360` | low | 📝记录 | link/ftp/ftps/smb 来源 URL 不做内网/元数据地址校验，存在 SSRF 面 |
| backend-integrations-006 | P3 | 安全 | `internal/server/sources.go:352-357` | high | 📝记录 | curl 来源用 -u user:password 命令行传凭据，进程列表可见 |
| backend-integrations-007 | P3 | 安全 | `internal/server/debug.go:113-116` | high | 📝记录 | debug SSE 端点允许 ?access_token= 查询参数传调试令牌 |
| backend-integrations-008 | P3 | 安全 | `internal/server/debug.go:37` | high | 📝记录 | 调试令牌 TTL 365 天且无强制轮换 |
| backend-integrations-009 | P3 | 资源泄漏 | `internal/server/factory_reset.go:108-116` | high | 📝记录 | factory reset 自动备份文件无限期堆积 |
| backend-integrations-010 | P3 | 错误处理 | `internal/server/ssh_session.go:416-434` | medium | ✅已修 | sftpBatchSource 缺少超时后 ctx.Err() 判定，错误信息不如主路径准确 |
| backend-integrations-011 | P3 | 边界条件 | `internal/server/secret_vault.go:245-262` | high | 📝记录 | ImportEnvelope 不校验导入条目是否可被当前主密钥解密，跨机备份导入后静默产生不可解密条目 |
| backend-integrations-012 | P3 | 安全 | `internal/server/ssh_session.go:54, 384` | medium | 📝记录 | SSH 主机密钥校验为 accept-new（TOFU），首次连接存在 MITM 窗口 |
| backend-integrations-013 | P3 | 错误处理 | `internal/server/workspace_config.go:671-700` | medium | 📝记录 | updateWorkspaceConfig 中 vault.Save() 先于 applyWorkspaceConfig()，路径切换失败会导致 vault 已写但配置未落盘 |
| backend-integrations-014 | P3 | 数据丢失 | `internal/server/migration.go:238-245` | low | 📝记录 | migration copyVerified 在目标文件已存在但哈希不同时静默删除源文件 |
| backend-persona-007 | P3 | 边界条件 | `internal/server/tts/edge.go:503-526` | medium | ✅已修 | edge-tts WebSocket readMessage 对帧长度无上限，恶意/异常服务端可 OOM |
| backend-persona-008 | P3 | 边界条件 | `internal/server/tts/sherpa.go:216-252` | low | ✅已修 | sherpa Synth 把用户文本作为最后一个 argv 直传，若以 -- 开头会被 CLI 当 flag |
| backend-persona-009 | P3 | 资源泄漏 | `internal/server/webauthn.go:127-134, 174-214, 372-402` | low | ✅已修 | WebAuthn sessions map 仅在 start 时惰性清理，突发 start 可致内存增长 |
| backend-persona-010 | P3 | 边界条件 | `internal/server/voice_agent.go:539-542` | high | ✅已修 | voice_agent.narrate 对 st.Text 按字节截断 1400，可能切断多字节 UTF-8 字符 |
| backend-persona-011 | P3 | 边界条件 | `internal/server/persona.go:281-318, 470-497` | medium | 📝记录 | personalitySave/personaSave 对用户输入的性格 prompt 无长度上限，可无限膨胀 settings.json |
| backend-persona-012 | P3 | 并发与数据竞争 | `internal/server/comments.go:274-322` | low | ✅已修 | comments.update 读-改-写无锁，并发 PUT 同一批注可丢更新 |
| frontend-core-004 | P3 | 资源泄漏 | `internal/server/web/app.js:415-416` | high | ✅已修 | state.runPhase[runId] 在 run 结束后只置 done=true，从不 delete，长时间会话逐 run 累积残留对象 |
| frontend-core-005 | P3 | UX与交互逻辑 | `internal/server/web/app.js:756-778` | medium | ✅已修 | 运行中轮询兜底在后端不可用时每 1.5s 弹一次错误 toast，无法自停 |
| frontend-core-006 | P3 | 错误处理 | `internal/server/web/app.js:986` | medium | ✅已修 | 「复制」按钮 clipboard.writeText 未接 .catch，非安全上下文/剪贴板被拒时产生未处理 Promise 拒绝 |
| frontend-core-007 | P3 | 逻辑错误 | `internal/server/web/app.js:2562-2570` | low | 📝记录 | Token 热力图“今天”格用 toISOString(UTC) 取日期，与本地时区相差一天导致高亮/键盘焦点格错位 |
| frontend-css-006 | P3 | 代码异常 | `internal/server/web/style.css:66-67,283-291,330-343,394-401,5,109-110,278-280,67` | high | ✅已修 | 重复/死规则：call-table、file-view-content、@keyframes phaseIn、.send-button、.traj-tabs 等 |
| frontend-css-007 | P3 | 代码异常 | `internal/server/web/style.css:224,228` | high | ✅已修 | .session-dot.dot-running 两处定义颜色冲突（荧光绿 vs 系统绿） |
| frontend-css-008 | P3 | 代码异常 | `internal/server/web/style.css:1` | medium | 📝记录 | .workspace-label 使用不必要的 margin:0 !important |
| frontend-css-009 | P3 | UX与交互逻辑 | `internal/server/web/style.css:914-917` | medium | ✅已修 | 文件预览顶栏首个 <select> 未与 36px 按钮对齐 |
| frontend-css-010 | P3 | UX与交互逻辑 | `internal/server/web/style.css:900-907` | medium | ✅已修 | editor-dialog 头部按钮高度未统一（与 file-view-head 不一致） |
| frontend-css-011 | P3 | UX与交互逻辑 | `internal/server/web/style.css:510,877,925-926,951` | low | 📝记录 | 硬编码色不随主题：assistant-entry-dot.live、ak-badge、stl-canvas、pdf-page |
| frontend-css-012 | P3 | UX与交互逻辑 | `internal/server/web/style.css:243,349,827` | low | 📝记录 | z-index 层级扁平：多个浮层同挤 1000，缺少分层约定 |
| frontend-css-013 | P3 | 代码异常 | `internal/server/web/style.css:906` | high | ✅已修 | .ro-badge.hidden 与全局 .hidden 重复 |
| frontend-css-014 | P3 | 代码异常 | `internal/server/web/app.js:7267,7272,7279,7293,7296,7300,7307` | high | ✅已修 | app.js 内联样式引用未定义令牌 --text-secondary / --border |
| frontend-viewers-010 | P3 | 边界条件 | `internal/server/web/app.js:4424-4430` | medium | 📝记录 | docx 批注定位用逐字符 += 拼接全文，大文档卡顿 |
| frontend-viewers-011 | P3 | 资源泄漏 | `internal/server/web/app.js:4034, 4039-4048` | high | ✅已修 | 代码高亮 overlay 的 window resize 监听在 teardown 后仍残留 |
| frontend-viewers-012 | P3 | 资源泄漏 | `internal/server/web/app.js:3515-3540` | medium | 📝记录 | drawio 加载超时后 iframe 与 message 监听仍在后台运行 |
| frontend-viewers-013 | P3 | 资源泄漏 | `internal/server/web/app.js:6908` | low | 📝记录 | 锁屏状态刷新 setInterval(1s) 常驻，未锁屏也持续遍历 runPhase |
| frontend-viewers-014 | P3 | 安全 | `internal/server/web/app.js:6868, 7008` | medium | 📝记录 | WebAuthn assertion/finish 把 challenge 放在 URL query 中 |
| frontend-viewers-015 | P3 | 边界条件 | `internal/server/web/app.js:4937-4940, 4945-4948` | low | 📝记录 | 导出会话 Blob 的 ObjectURL 在 click() 后同步立即 revoke，个别浏览器可能不触发下载 |
| frontend-viewers-016 | P3 | 边界条件 | `internal/server/web/app.js:3723-3725` | low | 📝记录 | ZIP 查看器无大小/条目数上限，恶意超大 zip 全量读入内存 |
| render-validation-003 | P3 | 安全 | `internal/server/web/app.js:3780, 4571` | medium | 📝记录 (重复→dup-csp-inline) | CSP style-src 'self' 拦截内联样式，控制台持续报错（docx-preview / app.js） |
| render-validation-004 | P3 | 代码异常 | `internal/server/web/vendor/highlight.js/languages/clojurescript.min.js:1` | high | ✅已修 | clojurescript 语言包是损坏的 HTML 占位文件，未注册并导致 'Unexpected token <' |
| runtime-functional-002 | P3 | 安全 | `internal/server/web/app.js?v=64:7266 / 7284 / 7292 / 7311` | medium | ✅已修 | CSP style-src 'self' 拦截 app.js 动态写入的内联样式，打开 .db 等页面单次刷出数十条 console error |
| runtime-functional-003 | P3 | 资源泄漏 | `internal/server/web/vendor/highlight.js/languages/:ruby.min.js / php.min.js / matlab.min.js` | high | ✅已修 | highlight.js 语言包 ruby/php/matlab 返回 404 且 MIME text/plain 被严格 MIME 检查拒绝 |
| runtime-functional-004 | P3 | 安全 | `前端 EventSource 调用:GET /api/sessions/{sid}/runs/{rid}/events?access_token=...` | high | 📝记录 | SSE 事件流通过 URL 查询参数 access_token 传递鉴权令牌 |
| runtime-functional-005 | P3 | UX与交互逻辑 | `前端文件提案卡片:提案卡片「文件修改 · N个文件」` | low | ✅已修 | 写文件提案卡片未见显式「拒绝/驳回」按钮，用户只能批准或忽略 |
| security-003 | P3 | 安全 | `internal/server/server.go:912-914` | high | 📝记录 | GET /healthz 完全免鉴权，泄漏版本与完整性状态 |
| security-004 | P3 | 安全 | `internal/server/server.go:1089-1091` | medium | 📝记录 | access_token 经 URL 查询串传递（/api/file/raw、/download、/events），可进入日志/历史/代理 |
| security-005 | P3 | 安全 | `internal/server/files.go:1420,1426` | low | 📝记录 | SQLite 接口把 table 查询参数以 f-string 拼进 Python SQL（仅方括号转义） |
| security-006 | P3 | 安全 | `internal/server/auth_verify.go:50-55` | medium | 📝记录 | 主密码/解锁端点无失败限速与锁定 |
| security-007 | P3 | 安全 | `internal/server/debug.go:37,120-129` | medium | 📝记录 | 外部调试令牌 TTL 长达 365 天，且仅存哈希、无异常频次告警 |
| security-008 | P3 | 安全 | `internal/server/web/app.js:4,6,1722` | high | 📝记录 | 访问令牌存于 localStorage，前端任意脚本执行即等于完整 API 接管（含 /api/command） |
| test-failures-001 | P3 | 错误处理 | `internal/server/migration.go:237-245（测试在 internal/server/migration_test.go:138）` | high | ✅已修 | TestMigrationRollback 期望冲突报错，但 d4fda74 已把冲突策略改为『保留目标、删除源、不报错』 |
| test-failures-002 | P3 | 安全 | `internal/server/workspace_config.go:570-593（测试在 internal/server/secret_vault_test.go:154）` | high | ✅已修 | TestNoPasswordReject 期望无账户密码 400，但 c536500 已删除该前置分支，vault 改由 access-token 自动解锁 |
| test-failures-003 | P3 | 安全 | `internal/server/server.go:1716-1722（测试在 internal/server/session_number_test.go:145）` | high | ✅已修 | TestAssistantSessionPasswordGate 首次调用期望 401，但 ac2ec2a 已把未设密码分支改为 200 noPassword 放行 |
| ui-deep-001 | P3 | UX与交互逻辑 | `internal/server/web/themes/light/tokens.css:--brand 与 --on-brand 定义（约 88 行附近）；应用于 style.css 中 button.primary` | high | ✅已修 | 主按钮白字 on #007aff 对比度 4.02:1，低于 WCAG AA 4.5:1 |
| ui-deep-002 | P3 | UX与交互逻辑 | `internal/server/web/themes/light/tokens.css:88` | high | ✅已修 | 弱化文字 --faint(#8e8e96) on 白底仅 3.25:1，用于 9-10px 元信息与图标按钮 |
| ui-deep-003 | P3 | UX与交互逻辑 | `internal/server/web/themes/light/tokens.css:--text-note 与 --selected 定义处` | medium | ✅已修 | 选中行蓝底(#e1eaf8)上注释文字 #707078 对比度 4.05:1 |
| ui-deep-004 | P3 | UX与交互逻辑 | `internal/server/web/style.css:927` | medium | ✅已修 | 工作空间白卡(上圆角7px)与小秘行(下圆角8px)半径不一致 |
| ui-deep-005 | P3 | UX与交互逻辑 | `internal/server/web/style.css:2（button:focus-visible 规则）；聚焦逻辑在 app.js` | medium | ✅已修 | 鼠标点击品牌按钮后焦点环残留（ESC 关闭设置后 logo 仍显示 3px 蓝框） |
| ui-deep-006 | P3 | UX与交互逻辑 | `internal/server/web/index.html:顶栏搜索 input（placeholder=搜索会话，快捷键 ⌘K）` | high | ✅已修 | 顶栏「搜索会话」输入不触发列表过滤，也无空态反馈 |
| ui-deep-007 | P3 | UX与交互逻辑 | `internal/server/web/app.js:外观/主题切换 segmented 渲染处` | high | ✅已修 | 外观设置选中主题卡右上角漂浮「跟随系统」与卡下居中标签文字重复 |