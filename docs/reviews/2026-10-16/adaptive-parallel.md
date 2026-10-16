# 执行中并行机会评估验收

基线main17779287，开发分支feat/adaptive-parallel-20261016。用户指出多倍并行经常规划失败，要求随执行中的新信息不断识别并行机会。

根因：旧版prepareParallel在首次成功或回退后永久设置ParallelPrepared，后续工具循环不再规划。35秒规划请求携带会话推理设置，小输出额度也可能被隐藏推理消耗；这是代码层风险分析，不能据此确定用户每次失败的上游原因。本机当前14个会话文件没有多倍并行历史，未获得该次失败的原始请求证据。

实现：证据指纹触发主循环检查、沿用主任务执行预算、每轮最多所选倍率分析、保留各轮上下文/用量、完成标题排除、只新增当轮笔记。规划请求90秒且仅调度关闭隐藏推理；主任务及子分析设置不变。无新证据不重试，用户插话可触发新评估，自动收尾提示不重复规划。工具/写入/审批保持主执行器原链。

验证：初次编译暴露新增测试的未使用变量，已修复；第一次针对既有与新动态路径的race通过14.229s，加入12轮预算及真实read_file工具链后，冻结专项race通过19.318s。命令：隔离Docker当前源码快照，`go test -mod=vendor -race -count=1 -timeout=180s -run '^Test(ConversationParallel|ModelLed|Context.*Parallel|RunControl|Pause)' ./internal/server`，无网络和用户数据挂载。涵盖初始空/无效JSON后新证据解锁、同检查点不重放、JSON恢复、完成标题过滤、多轮累计/用量、12次边界、取消，以及主工具循环先读文件后并行。模拟提供商不能证明真实模型规划质量。

Node进度卡回归通过：运行中等待机会、终态主任务处理、多轮任务显示与历史规划上下文行。实际工作台/模型、quick/full、镜像包和main尚待验收。

真实千问qwen3.8-flash首轮验收先因测试文件误放`/workspace`而失败，当前选择工作根实际为Downloads；已按任务AgentRoot移动自建合成资料并逐字节验证，未改用户工作区配置。正向候选b16f253b实际read_file成功，第2次规划未完成，第3次因主任务update_plan后获得两路模块分析；两路返回并进入主汇总。但内部record_research_finding也触发了多次无效评估，直到12次上限；主模型还尝试重复派发子代理，因角色未配置而被拒绝。这一候选只证明动态机会已接通，不作为最终效率验收。

据真实问题修订：证据键排除研究记账，工具结果去除记录编号后按工具/参数/正文去重；规划/子分析组件system指令明确角色，汇总提示分析请求已经派发，不得再用spawn_subagent/run_plugin_plan重复派发。无执行权限转交。规划返回工具调用与空正文分别给出固定原因，不暴露上游错误秘密。修订专项race20.518s通过；加入主汇总防重复说明后的冻结专项race21.149s、quick88通过。

第一次完整检查session87179因上述真实验收问题主动中止，精确停止其owner55364和唯一测试容器aide-verify-ffb27601ad7b，未停止生产/用户任务；未生成或采用PASS收据。后续必须对修订冻结输入重新运行full。

第二候选7ff30f88实际调用已将评估减少到3次，但读取后规划仍出现格式错误，未派发子分析；主模型还一度混淆测试模块，收尾纠正后才按原文件交付。这不是最终并行通过结果。候选完整镜像包本机导入通过，仅作中间构建，不代表最终源码或接收端验收。

进一步隔离组件协议：原执行器各角色消息编码为只读参考数据，组件只保留自己的system及用户分派消息；保留原生图片、不把图片数据或本地归属编号混入文本。在未明确不支持结构化输出的模型上启用JSON对象模式。新增回归覆盖system/tool协议隔离、原文证据保留、原生图片保留、本地归属不外发与JSON模式。冻结专项race22.253s通过，quick重新运行中。

独立实际浏览器组件页验证：390宽度明/暗主题，先顺序等待→第2轮两路运行→2/2已汇总，累计行数正确，始终只有一个进度卡，草稿逐字保留、无横向溢出；使用生产parallel-chat.js、conversation.css和主题令牌。暂时viewport已复位、唯一自建IAB页已关闭。组件注入状态不等于真实模型或生产调度成功；真实入口继续复用start.command和原Safari页。

协议隔离候选d1ad0e21实际千问5x成功：第一次无子分析，read_file读取合成资料，第2轮同时观察到3个分析请求运行并完成，主执行器汇总；update_plan后第3次检查没有再派发分析。最终含Amber/Cobalt各两个测试设计，明确未实际执行测试。工具仅read_file和update_plan，没有文件写入、研究记账循环或创建子会话。原生Safari9999显示“已评估3轮／已汇总3/3”、各轮规划和子分析独立窗口及最新主请求实际usage。明暗390组件页的注入状态与这次真实调用严格分开，见模型/浏览器证明JSON。

最终审查移除原型自设的12次永久关闭条件：这与长任务过程中继续发现机会的用户要求不一致。评估现在仅由主工具循环触发，沿用主任务已有轮数/活动时间/取消预算，保持无新证据不调用。增加第13个新证据仍能派发、主任务2轮策略保持有效的回归；这两行条件变化不影响上述3轮真实调用路径。第二次full13456在此修订前精确中止owner58747及测试容器aide-verify-a0457b7c937c，未采用为PASS，最终源码仍需新full。

测试清理：三个自建会话及唯一合成文件已按精确归属/内容哈希清除，原14会话文件、鉴权、引用配置逐字节一致，挂载/工作区配置一致；设置只有编号从42推进45，保留已使用编号而不回退。原Safari会话/文件侧栏/3个用户标签恢复，临时IAB页与服务关闭，viewport复位。仅移除自建中间7ff镜像包，原3f回滚包保留；不会删除原iCloud项目。

Final candidate: source `e0f38e02`, image `7b6321af02c1`, source SHA `a27b8a833217334bdd96d9162d6ed873771d18758549dc1f69a99011193bf099`. Designated launcher actual9999 health,15 assets, all14 original session/auth/source bytes and mounts preserved. Focused final lifecycle race22.487s/quick88 PASS, including13th evidence and main2-round policy. Real model/native3-round acceptance is separately scoped to protocol-identical d1; only the >12 bypass was removed afterwards. Complete final archive/import proof supersedes intermediate records; owned d1 archive removed after hashes/import evidence retained. Fresh full90 session20685 running with frozen product/README/SHA inputs; main gate/push pending.

Final full verification PASS: receipt `20261016T035933633084Z`, fingerprint `c757a1bcc17dfb8b44192388565c125b1619af7036929d339ef97362557e9ec4`, all90 commands exit0 and fixed inputs unchanged. Go server race647.882s, execsandbox1.246s, TTS1.539s and vet pass. [Full proof](adaptive-parallel-full-proof.json). Final7b normal native reload additionally confirmed original scene/file panel/3 tabs and empty draft; test-page state reset by safe reload. Main gate/push pending at this record.
