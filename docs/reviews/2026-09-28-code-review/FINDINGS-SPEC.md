# 全局代码审查 · 缺陷记录规范（2026-09-28）

本文件是所有审查 / 复核 / 修复 agent 必须遵守的统一输出契约，确保多源结果可机械合并。

## 输出位置
- 每个审查 agent 只写一个文件：`docs/reviews/2026-09-28-code-review/findings/<slug>.json`
- 审查阶段为**只读**：不得改动源码、不得 git commit、不得 docker build/up、不得启动服务。

## JSON 结构
```json
{
  "agent": "<slug>",
  "lens": "审查视角/范围一句话",
  "summary": "总体结论",
  "findings": [
    {
      "id": "<slug>-001",
      "title": "简短标题",
      "category": "逻辑错误 | 代码异常 | 并发与数据竞争 | 边界条件 | 错误处理 | 安全 | 资源泄漏 | UX与交互逻辑",
      "severity": "P0 | P1 | P2 | P3",
      "file": "internal/server/xxx.go",
      "line": "123 或 123-130",
      "evidence": "为何错：触发路径/数据流/后果，引用关键代码片段",
      "fix": "具体修复建议",
      "confidence": "high | medium | low",
      "needs_user_decision": false
    }
  ]
}
```

## 严重级口径
- **P0**：崩溃 / 数据丢失或损坏 / 可利用安全漏洞 / 核心流程阻断。
- **P1**：正常使用下的重要错误行为、数据竞争、资源泄漏、授权缺失、明显错误结果。
- **P2**：边界条件下的一般缺陷、错误处理不当、可恢复的异常、明显 UX 问题。
- **P3**：建议 / 加固 / 可维护性 / 轻微体验问题。

## 类别口径
- 逻辑错误：分支/状态机/排序/计算等业务逻辑不正确。
- 代码异常：nil 解引用、panic 风险、类型/编码问题、死代码、明显笔误。
- 并发与数据竞争：goroutine 竞态、map 并发读写、锁粒度/死锁、channel 误用、可见性。
- 边界条件：空/nil、超长/超大、越界、零值、特殊字符、时间与数值边界。
- 错误处理：吞错、错误被忽略、错误状态码/包装不当、降级逻辑错误。
- 安全：鉴权/授权、路径穿越、命令注入、SSRF、XSS、密钥泄漏、ZIP Slip、反序列化、TLS/主机校验。
- 资源泄漏：goroutine、文件句柄、HTTP body、定时器/Ticker、锁、ObjectURL、EventSource/Observer、子进程。
- UX与交互逻辑：交互状态、反馈、对齐/滚动/遮挡、可访问性、i18n、加载与禁用态。

## 防误报（已知非 bug，禁止上报）
1. golang 容器内无 node，4 个插件用例 `TestPluginUploadAndValidation` / `TestPluginLifecycleAndSurface` / `TestPluginApplyErrorIsolated` / `TestPluginToolExecution` 报「插件校验失败」是测试环境缺 node，非代码缺陷。
2. marked v12 遵循 CommonMark/GFM 的标准渲染行为，不是 bug。
3. 无法证明错误的「有意设计」不要报；拿不准则 `confidence=low`，不得夸大严重级。
4. 第三方代码（Go `vendor/`、前端 `internal/server/web/vendor/`）不报内部缺陷，仅在「我方集成误用」时报我方代码。

## 证据要求
- 必须用 Read/Grep 实际打开文件，核对真实行号，禁止凭印象。
- 每条缺陷须有可复现的触发路径与具体影响；P0/P1 必须 `confidence=high`（复核前）。
