# 按钮到服务证据链：开发检查点

任务：`knowledge-http-evidence-20261016`；基线 main `584aa9b9182dd091a3265b0f62f5a22e510d6a9a`。本批在移动硬盘的任务分支开发，未更新生产实例。

缺口确认：现有调用分析没有 HTML 按钮、JS 事件注册、HTTP 请求和 Go 路由之间的桥接。单个函数调用路径不能证明原要求的完整链。新增 Node 样例在旧解析器下以 `DOM event evidence is missing` 失败，私有复现日志位于 `.agent-state/knowledge-http-evidence/baseline-js.log`。

当前实现 JS 解析证据：直接 `document.getElementById`／简单 ID `querySelector` 的事件注册与 `onclick` 等赋值，绑定到已解析函数声明或匿名函数；字面 `fetch` URL、方法、调用者和位置。默认方法为 GET，显式方法规范化为大写；动态 URL、选项、方法和无法绑定的回调保留诊断。词法遮蔽、回调重赋值、全局覆盖、动态全局属性写入及 DOM 查询器重写不会生成伪记录。代码注释、字符串中的假调用不作为 AST 调用。

每文件每类记录最多256条，待绑定事件队列也受限，超出时标记截断。Go `codeUnit` 已接收这些记录。解析仍使用产品拥有的 Acorn 源文本提取器，不导入或执行工作区代码。回归样例含顶层 `throw`，生产解析传输成功，不执行该语句。

**未完成**：HTML 按钮和脚本加载归属、Go 路由声明及唯一匹配、同来源图连线、页面证据等级与源位置呈现，以及真实完整多段索引/HTTP/页面验收。新增记录目前还未用于构图，不能把解析器结果当作按钮到服务功能已完成。

检查命令：`node scripts/knowledge-web-parser.test.cjs`、JS语法检查、`TestKnowledgeWebParserTransport` race与全包vet、quick（新增解析回归后85项）。最终结果由本检查点收据记录，不使用前一批次full86收据。本批未执行完整full、main门禁、部署或离线镜像更新；运行实例仍为47cd，原会话/配置未改。

后续从同一任务接续图构建与呈现；完整认证Safari沿用人类暂缓决定，Windows由用户验收，真实向量测试暂缓。总体12项不因本检查点改为完成。
