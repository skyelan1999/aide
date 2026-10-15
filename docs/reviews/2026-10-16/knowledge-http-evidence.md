# 按钮到服务证据链：开发检查点

任务：`knowledge-http-evidence-20261016`；基线 main `584aa9b9182dd091a3265b0f62f5a22e510d6a9a`。本批在移动硬盘的任务分支开发，未更新生产实例。

缺口确认：现有调用分析没有 HTML 按钮、JS 事件注册、HTTP 请求和 Go 路由之间的桥接。单个函数调用路径不能证明原要求的完整链。新增 Node 样例在旧解析器下以 `DOM event evidence is missing` 失败，私有复现日志位于 `.agent-state/knowledge-http-evidence/baseline-js.log`。

当前实现 JS 解析证据：直接 `document.getElementById`／简单 ID `querySelector` 的事件注册与 `onclick` 等赋值，绑定到已解析函数声明或匿名函数；字面 `fetch` URL、方法、调用者和位置。默认方法为 GET，显式方法规范化为大写；动态 URL、选项、方法和无法绑定的回调保留诊断。词法遮蔽、回调重赋值、全局覆盖、动态全局属性写入及 DOM 查询器重写不会生成伪记录。代码注释、字符串中的假调用不作为 AST 调用。

每文件每类记录最多256条，待绑定事件队列也受限，超出时标记截断。Go `codeUnit` 已接收这些记录。解析仍使用产品拥有的 Acorn 源文本提取器，不导入或执行工作区代码。回归样例含顶层 `throw`，生产解析传输成功，不执行该语句。

HTML 使用隔离的 Python 标准库源解析，保留按钮、外部脚本与位置；模板、文本域、注释中的伪按钮排除，重复 ID／属性、内联脚本及 `<base>` 有有界诊断。HTML 材料沿用代码视图80文件／4 MiB／每文件256 KiB上限。按钮作为真实源声明加入代码图，不执行脚本。

Go AST 识别标准 `net/http` 的默认注册、直接 `NewServeMux` 和显式 `*http.ServeMux` 参数；被遮蔽、重赋值的 mux 和动态注册保留诊断。解析函数、方法和闭包处理器，处理器不能绑定时仍保留字面路由占位，以免漏掉重复模式而产生伪唯一匹配。图中匹配使用产品代码创建的标准库 ServeMux，只检查方法／路径，不调用处理器。请求源站仍不是运行时证明。

初版图按工作区根、来源和区域隔离：真实按钮→已声明相对脚本中的事件绑定→回调→字面请求→唯一标准 HTTP 路由→处理函数→已有服务调用。每段有源位置、内容指纹和静态证据等级。重复路由、未知处理器的重复注册、动态注册、错误方法、跨来源、外部URL、未加载脚本及 `<base>` 页面不补造关系；规范化重定向不当作直接处理函数调用。新节点与关系沿用1200符号／6500边预算，源码行切分按文件复用。

当前已补纯 DOM 查询辅助函数及固定前缀请求封装的词法识别。只接受可证明的参数／方法透传，不执行函数；参数改写、别名突变、eval／arguments、方法覆盖及被遮蔽封装不展开。请求实例从调用位置连接，不能因共享一个 api 函数而串到另一个按钮的请求。实际 `starmap.js` 中的 `$` 和 `/api/knowledge-map` 封装已作为源样例回归，未执行页面代码。根相对脚本只在页面目录或来源根下的唯一源候选中匹配，歧义拒绝，不声称知道真实服务器挂载。

前端调用筛选、结构层级及宇宙分组已包含新增关系，关系有方向并保留候选虚线。路径和关系详情显示 DOM选择器候选／词法绑定／静态URL模板／方法路径候选等等级；源链接保持只读、内容指纹和行号。资产查询版本已更新。

实际 IAB 组件页使用当前生产路径标记、CSS和渲染/清除/查询函数，配合合成七节点六步路径：清除后0条，重新查询后7节点／6步。1440×960暗色、390×844明色无横向溢出，能看见新增等级、行列和源链接。截图：[暗色](knowledge-http-evidence/path-dark.png)、[窄屏明色](knowledge-http-evidence/path-light-390.png)。组件页的 api 使用合成数据，不是完整认证工作台或真实源码打开验收。全部自建页已关闭、视口复位、预览服务停止；未调用模型或修改生产会话。

**保留边界**：完整认证Safari仍暂缓；复杂封装、动态路由／URL和缺少材料仍可能无法连线。`app.js`目前约722 KiB，超过既有每文件256 KiB预算，不能把小范围或组件验收说成完整aide目录覆盖。该预算和4 MiB／80文件总量不在本批扩大。实际已解析的星图脚本及完整临时项目分别提供证据，不能替代整个项目或运行时追踪。

检查命令：`node scripts/knowledge-web-parser.test.cjs`、JS语法检查、Knowledge相关race、Web专项race与全包vet、quick85。解析和构图回归验证六段静态链，实际临时文件索引→路径HTTP接口另测版本／工作区／视图隔离；辅助函数和页面变更后的最终Knowledge race为69.527秒，全包vet和quick85通过。首次新增层级测试误将describe参数顺序写反而失败，修正后单项及quick85通过。完整full应为87项，尚未执行，不复用前一批次full86收据。运行镜像与main门禁随后补实际收据，不预写PASS。

后续从同一任务接续图构建与呈现；完整认证Safari沿用人类暂缓决定，Windows由用户验收，真实向量测试暂缓。总体12项不因本检查点改为完成。

收尾反例：初版37de候选对对象原型提供的HTTP方法会误按GET处理，新增Node反例确实失败；现统一直接/封装请求的方法解析，拒绝原型选项和方法覆盖。Web专项race2.992秒和vet通过；随后JS方法规范化又按原生Request观测修正，custom/patch保留大小写、post规范化为POST，最终Node与当前quick85通过。37de仅中间镜像/包，需以重建候选及最终full87为准。

本机与离线候选：指定start.command运行 sha256:1601033df8ea2434e4bc6c7759c7e9940728757f3aab4701a0e5ca85925b8dba，source SHA d5cb1d9c65e8c15abbd00a41dd3d06261cd6d5318c09e29f14150e017a985eb2。完整包 aide-0.1.17.0-RC2-linux-arm64-1601033df8ea-offline.tar.gz，705779869字节，SHA-256 74d150f1d784e2419fe064aa017b0ae041ed18652ea5617a05db54e3d8c897ef；内外摘要、应用/依赖标签、平台、启动器及实际docker load通过。会话、配置、认证及挂载保持一致；保留47cd回滚包，仅活动移动硬盘保存，接收设备导入待验。不升版/tag/正式Release。 最终full87、独立main门禁和远端推送待实际收据。

Final clean-main full87 PASS: {"status": "pass", "count": 87, "fingerprint": "9acdc376f78653bedf6e77f5d5a2e6688cfef22d67e28fdc6a2f12875cf49e70", "sourceCommit": "78f0c736b9cfc39bce94a0fd2a5aa0278362ba3a", "serverRaceSeconds": 540.604}

Independent main gate PASS; no-force main push d24bbdb48751dd4f71f09f461de171a29f1b7c14 exactly verified remotely. Final receipt commit follows. No version/tag/GitHub Release.
