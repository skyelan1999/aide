# 会话配置与本机 8097 更新回执 · 2026-10-14

普通会话分别保存策略、参数模板、模型和推理强度，切换会话恢复各自选择。新任务保存配置快照；运行中更改菜单只影响后续发送，插话仍进入原任务，重试和恢复沿用原任务配置。模型目录、参数模板内容和自动路由规则仍共享；小秘保留原有独立模型配置。

后端第一批 `6d8a3d3` 已推送至 `feat/session-model-selection`；第二批 `36ad78a` 提交前端保存队列、版本冲突处理、发送和预览快照，以及晚配置模型、插话和辅助审批路由的补充修复。没有创建 Release 或合并 main。

## 验证

- 规范仓和本机兼容仓最终 `TestSessionModelSelection` 定向 race 分别通过，耗时 9.021s / 7.999s。覆盖持久化、会话隔离、版本冲突、模型能力、请求推理参数、运行中改选择、插话、重试和恢复；规范仓另测不同提供商的地址和测试凭据。
- 两仓扩展 race 回归通过，耗时 40.032s / 39.460s，包含 SessionModelSelection、ConversationParallel、Context、Harness、Pause、Resume、Retry、Vision、ModelProvider、Assisted 对应测试。运行环境使用带 Node 的本地镜像，清空继承的 `AIDE_BUILTIN_PLUGINS`，让测试使用各仓自己的插件源码。此前缺 Node、继承目录缺插件的失败保留在本机日志；没有为通过而删改测试断言。
- 两仓 quick 通过，包括前端语法、会话保存队列、上下文与点击延迟夹具和文档检查。
- 原生 Safari 离线夹具使用实际生产菜单及选择模块，观察 A/B 会话不同模型、策略和 low/high 推理各自保存与恢复，全局默认不变；深色、浅色均检查。合成保存不会连接真实模型。

## 本机更新

仅替换 `aideMacMini/aide` 的 Compose `aide` 服务。更新前鉴权检查没有运行或待澄清任务；先停止 AIDE，再只读挂载原数据卷制作一致备份，随后执行 `docker compose up -d --no-deps aide`。原数据、home 命名卷及工作目录挂载保持。其他容器未重启。

- 新运行镜像：`sha256:10e35d72c80bbf89d0dda8c1fdafbce656a60aa27dfface106471c3f6e740647`，容器 healthy。
- 备份：本机忽略目录 `.agent-state/session-model-selection/data-before-update.tar.gz`，14306 字节、39 条归档记录，tar 可读且非空；SHA256 `9038d511a9160ceb0075fe60b185f7f179649e5f3381775d972e00e6d648e3da`。含服务数据，不提交或公开内容。
- 上一版镜像保留为 `aide:session-selection-previous-20261014`，已核验 ID 为 `sha256:1657ad320d9648ac9b3e249d09a9f5ac7090e1dbffebb02ebe057041bc9f917e`。需要回滚时将该镜像重新标记为 `aide:local` 并仅重建 AIDE 容器；不自动覆盖数据卷。
- `/healthz`、鉴权后的 `/api/sessions` 与 `/api/config` 均 200。首页包含新选择模块，`session-model-selection.js`、`app.js`、`locales/en.js` 服务字节与本机源码一致。新路由用有效空 JSON 请求不存在会话返回 404；空请求体先返回 400，属输入校验。
- 运行中只有既有小秘会话，其独立配置不纳入普通会话断言；未向真实普通会话写测试数据。普通会话的端到端行为由隔离 API 测试和离线菜单夹具覆盖。

镜像由已有未提交工作的本机开发仓构建，不宣称来自规范仓的干净提交。本机仓是单提供商架构，保留共享端点和凭据；规范仓支持多提供商，两份兼容实现分开验证。

Safari 实际工作台仍停在本机证书提示，需用户手动处理；未自动绕过。实际浏览器交互及真实模型调用仍待验收。本轮没有发送真实模型请求，也不将模拟接口与组件验收等同于真实模型效果。

日志与脱敏运行回执位于两仓的 `.agent-state/session-model-selection/`。任务状态见[任务账本](../tasks/session-model-selection-20261014.json)。
