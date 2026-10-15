# HTTP单资源与MCP能力说明的实际索引验收

任务：[http-mcp-index-live-20261015](../../tasks/http-mcp-index-live-20261015.json)。继续原12工作流中的渐进索引、混合检索和来源能力验收。开发、脚本及证据均位于移动硬盘 `/Volumes/MY PASSPORT/aide_ws/aide`。

## 真实执行范围

使用现有运行候选 `b11bb97fed59` 的独立实际二进制容器，默认用户，network none，cap-drop ALL，512MiB及1CPU，无用户目录或数据卷挂载。通过容器内部HTTPS API及实际Python loopback HTTP服务、stdio MCP夹具执行；没有模型、浏览器或外部服务调用。不是对用户真实HTTP／MCP服务器的验收。

HTTP来源URL包含目录和编码空格，作为单资源索引为 `resource.txt`，没有把服务器页面当成目录扫描。通过真实MCP initialize／tools/list发现两个工具，星图中生成两份Markdown能力说明；本轮既不读取远端业务文档，也不调用tools/call。

## 最终断言与结果

最终探针PASS，[脱敏收据](http-mcp-index-live-proof.json)记录镜像、样本和私有探针SHA。

- HTTP一份原文、MCP两份能力说明节点出现；两种来源的唯一marker检索各命中一条，hit.file的source／path／origin与星图节点一致。
- HTTP正文与准备字节精确相同；两类检索结果hash均与直接读取原文的SHA-256一致。带knowledgeOrigin／knowledgeWorkspace的预览读取返回相同hash，伪造origin被409拒绝。
- 两种只读来源的文件PUT均403。MCP工具调用marker始终不存在；能力发现不是工具调用，更不代表模型已实际使用该工具。
- 实际终止应用子进程，由同一容器内监督循环启动新PID；新进程索引回读HTTP1／MCP2，原文搜索hash保持，新的来源origin可读取，旧origin预览409。没有声称容器重建、物理断电或复杂目录渐进扫描。

初次夹具脚本取名http.py，遮蔽Python标准库http包，服务未启动，导致原文读取失败；改名并在注册来源前增加HTTP就绪／正文断言。第二次探针错误读取hit.path，实际接口为hit.file.path，已按源码与实际响应纠正。两次失败日志均保留，均为探针问题，不计作产品缺陷。较早基础版探针已PASS，最终新增预览身份和重启后原文断言亦PASS；没有修改产品源码。

## 清理、门禁与剩余范围

探针finally已移除所属容器，当前仅生产aide容器运行；没有新建网络、主机服务、标签页或付费调用。私有脚本／原始失败及最终日志保存在 `.agent-state/http-mcp-index-live/`。本批仅普通文档，完整检查须经门禁确认输入指纹与已有干净full84一致方可复用，不声称重跑full。

SMB真实服务本批NOT_RUN（本机原源协议夹具镜像未保留），HTTP重定向／认证／服务变体与真实MCP插件工具调用／模型利用、完整已鉴权工作台仍待各自验收。本轮不建立MCP远端文档目录索引已实现的结论。完整Safari按用户安排暂缓，Windows用户验收，真实向量暂缓；原12工作流仍在进行。

最终main门禁通过，输入指纹与已有干净full84精确一致，复用收据；推送后远端main核对 `0b69dccfad7c458adee65c4a8dd1ca1d63b1e356`。本条普通文档补最终回执，不改变产品输入；无产品输入变化，因此不重建当前运行镜像或重复导出旧包，不升版、tag或发布正式Release。
