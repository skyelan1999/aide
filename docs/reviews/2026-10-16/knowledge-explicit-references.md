# 报告到来源的显式引用修复

任务：`knowledge-explicit-references-20261016`。基线 main `8442dcabd94b783186d669084b9a809721fe591c`；开发目录为移动硬盘 `aide_ws/aide`。

旧实现使用不接受空白的目标表达式，无法建立带空格的证据文件引用；提取引用后再次查找字符串，可能定位到更早的同名前缀；跨来源命名空间使用子串匹配，可能把 `.bak` 后缀或外部 URL 当作引用。三个新增回归在旧实现下均失败，私有基线日志为 `.agent-state/knowledge-explicit-references/baseline.log`。

修复保留目标在正文中的实际字节偏移，再计算 Unicode 行列。支持 Markdown 尖括号目标、可选标题、百分号编码、片段和被引号包围的本地路径；本地匹配要求区域、根和来源身份一致。跨来源仍要求命名空间或稳定节点 ID。命名空间检查边界，拒绝文件名后缀和外部 URL 内的伪目标；保持每段200次提取、单目标1024字节和4000条总关系上限。

新增回归包括报告→中文空格证据文件→源码的两段路径、位置准确性、编码与命名空间隔离、非法目标与提取上限。联动测试在临时工作区写入真实文件，执行生产索引构建，再通过注册的 HTTP 路径接口检查两段证据、节点身份、行列、版本和工作区隔离。测试使用临时 App，不读取生产会话，也不调用模型。

这仍是静态显式引用证据，不证明运行时调用。完整按钮→服务多段证明、浏览器原文跳转及完整认证 Safari 验收继续保留；Windows由用户验收，真实向量测试暂缓。

发布状态：开发分支验证中，尚未合入 main、更新本机镜像或生成本批离线包。现有运行实例及4cc镜像回滚包保持可用。全量检查和独立发布门禁尚未执行，不复用旧界面批次收据。

开发检查点：`^TestKnowledgeReference` race（3.832秒）和全包 vet 通过；最终代码的 `^TestKnowledge` race 与全包 vet 通过；quick84通过。准确日志与内容指纹见同目录 proof.json。完整full与发行门禁仍待执行。

候选本机/离线验收：指定start.command运行 sha256:47cd10e0cf683302d20822b98006cf780f05c3d44fd48bbbddd2a18530250679，source SHA 67fde75b95a712505f05dc06a48170cce23c32f38dff773448d52f41001fd242。完整包 aide-0.1.17.0-RC2-linux-arm64-47cd10e0cf68-offline.tar.gz，705767139字节，SHA-256 de684665fc8175c3021edd90c33a84c0e82d1c9bb675dc806ce419b3c2aaec5d；内外摘要、应用/依赖标签、平台/启动器及实际docker load通过。会话、配置、认证和挂载保持一致；保留4cc回滚包，接收设备导入待验。不升版/tag/正式Release。 全量检查、独立main门禁和远端推送仍待执行；以随后实际收据为准。

Final clean-main full86 PASS: {"status": "pass", "count": 86, "fingerprint": "2acd078e975ffa86e0186f3e1c6cad4a5d2e1f97ebd00ffacb476bbdfb4c0554", "sourceCommit": "d3d89d1a92fd30c01e292c7241bc51e2e4a19455", "serverRaceSeconds": 550.825}
