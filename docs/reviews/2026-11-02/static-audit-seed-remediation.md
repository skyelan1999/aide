# 静态审阅基准注入点适配

修复对象：[715](https://github.com/skyelan1999/aide/issues/715)。在 main `418a6ce0` 的完整 race 套件第 5 分片，`TestStaticAuditFixture` 报告 `seed partial-merge no longer matches current source uniquely`。PR #714 新增逐项成功记录后，`Merged` 的正确计算同时出现在逐项和批次完成处；原夹具要求旧字符串恰好出现一次，因此拒绝生成受控故障快照。

本次将该种子的定位限定为紧接 `Collected` 之前的批次结束赋值，继续只替换一个位置、保持行数和缺陷含义：只登记一个选定文件，却错误地把整个结果标记为已合并。唯一匹配检查、全部六项种子和 Go 解析检查均保留；没有修改产品实现、放宽断言或跳过基准。`TestStaticAuditFixture` 纳入基础 CI，后续生产实现变动若使种子失配，会在基本门禁发现。

验收使用隔离、断网的 ARM64 SDK。先编译原始源码，执行 `TestStaticAuditFixture` 和 `TestStaticAuditSeedInvariants`，两项均通过；再由夹具生成临时 overlay，编译故意注入六项缺陷的独立二进制，六个对应不变量均明确失败，退出码为预期的 1。全包 basic 编译、关键回归及 vet 也通过。故障源码和二进制只存在于临时容器，没有应用到工作区；容器已删除，日志和可复用编译缓存保留。结果回填在[任务账本](../../tasks/static-audit-seed-20261102.json)。

原完整运行保留为失败且未完成，不作为正式 Release 收据。修复后的完整源码必须重新通过 full 和 tagged Delivery；单独证明夹具正确不等于全部产品测试完成。没有进行付费模型调用、真实浏览器验收或生产部署。
