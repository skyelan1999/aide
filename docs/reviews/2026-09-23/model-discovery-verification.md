# 模型自动获取修复验收

> 历史快照，当前以 docs/verification.md 为准。

日期：2026-09-23。修复提交：`1fb7cfd`。

## 问题与修复

自动获取原来仅使用已保存的连接设置，忽略弹窗里尚未保存的地址和密钥。新增 `POST /api/models` 接收当前表单草稿，获取过程不保存配置；保留原有 GET 接口。仅当地址与已保存地址相同（忽略末尾斜杠）时复用已保存密钥。更换地址不携带旧服务密钥；清除密钥选项优先。

## 已执行验证

| 检查 | 实际结果 |
| --- | --- |
| `python3 scripts/agent-route.py verify full` | PASS，含 Go race/vet、前端语法及既有回归 |
| `TestModelDiscoveryDraft` | PASS：草稿地址、新密钥、清除密钥、同地址复用、跨地址不复用、不保存草稿、拒绝无效 URL |
| 18121 浏览器模拟获取 | 点击自动获取后，候选列表包含 `preview-model` |
| 经 aide `POST /api/models` 请求真实 DeepSeek | HTTP 200；返回 `deepseek-flash`、`deepseek-v4-pro` |
| 直接调用真实 DeepSeek 对话接口 | `deepseek-flash` 返回 `aide connection OK`；finish_reason 为 stop |

真实对话 usage：输入 37、输出 29、总计 66 tokens；输出中包含 25 reasoning tokens，不能另加到总数。该次调用经用户明确授权。模型列表是当次观察，不是硬编码的长期可用模型清单。

## 验证与部署边界

真实模型列表测试经过 aide 后端；真实对话测试直接调用提供商接口，不能据此声称 aide 的完整真实模型工作流已端到端验证。浏览器工作流使用本地模拟服务。密钥未记录在仓库或应用配置中。

18121 隔离预览已更新，正式服务未重启。此次交付为修复与文档合并推送，不打 tag、不发布新 Release、不导出镜像。
