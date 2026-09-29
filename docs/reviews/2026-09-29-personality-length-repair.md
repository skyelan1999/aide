# 性格演化超长反馈修复

任务：personality-length-repair；基线：0b3f8be；版本基线：0.1.12.0 RC1（未升版）。

截图中的 `rejected: 变长 1.49× 超过 1.10× 上限` 表示模型已返回文本，但验收拒绝。代码将 UTF-8 字节数描述为字符数，中文与英文混合时预算错误；另外超长后直接失败，没有精简反馈。

本次提示与验证统一使用 Unicode code point 数（包括空格/标点/换行）。常规预算为 min(4000, floor(旧字符数×1.1))；压缩预算为 min(4000, 旧字符数−1)。向模型明示这个整数预算。

首稿超长时追加一次精简请求，说明实际字符数及上限；两次调用共享原有 120 秒超时和非推理配置。不会循环重试、截断人格文字或放大上限。第二稿仍超长、身份丢失或调用失败均保留原性格；只有最终合法稿才增加一次演化计数。每次超长修复可能多消耗一次模型调用。

验证用本地 mock provider，不调用真实付费模型。覆盖超长后精简成功、连续超长仅两次、精简后身份丢失、中文与英文相同字符数，以及既有采纳/拒绝/压缩/持久化回归。

实际运行 `go test -mod=vendor -race ./internal/server -run 'Test(Evol|CompressMode|EndToEndEvolution|ManualEvolve|AuditOnAttempt|InsufficientSample|Rollback|Reset)' -count=1`：PASS，20.023s，Docker 退出 0。`git diff --check` PASS。日志保留 `.agent-state/personality-length-race.log`；本轮未重跑全量测试。

本次仅后端，无 UI 布局修改；完整页面及真实模型端到端未运行。运行中的服务不重启，源码修复需要后续构建部署才能生效。整个整理分支仍保留此前全量测试失败，不声明正式发布。
