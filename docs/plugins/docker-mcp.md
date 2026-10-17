# Docker MCP 接入

引用来源支持本地程序（stdio）和 HTTP 网关（Streamable HTTP）。模型接口与 MCP 服务使用独立配置；配置 MCP 不会把模型密钥交给该服务。

## 配置来源

在文件面板的“引用”中新建 MCP 来源，选择传输方式。

- stdio：填写容器内可用的启动程序，参数每行一个；不会按 shell 字符串执行。
- HTTP：填写完整 MCP 端点。远端使用 HTTPS 并验证证书；明文 HTTP 仅限本机回环或 `host.docker.internal`。Bearer 令牌用独立密码字段填写，保存到本地加密保险库；编辑时留空保留已有凭据，不要把令牌写进 URL。
- Docker Desktop 宿主机网关：使用 `http://host.docker.internal:<端口>/mcp`，勾选“Docker Desktop 本机网关”，填写独立 Bearer 令牌。该模式保留网关的 localhost Host；网关本身应绑定宿主机回环，不需开放 LAN 或挂载 Docker socket。

保存后点击“测试 MCP 并发现工具”。发现只读取工具描述，不执行工具。AI 仍只可通过引用调用服务明确声明只读的工具；未声明时会拒绝。发现工具可在引用目录的 `tools` 下打开为虚拟 Markdown 说明，说明预览不等于执行成功。

HTTP 请求有超时、响应体积上限及取消处理，不跟随重定向，本机连接不经环境 HTTP 代理。后续 POST 与发现结果写回会重新核对来源/凭据；已经发出的请求不能撤回。

## 本机配置与生命周期

已有手动 Docker MCP 测试配置保存在启动目录的 `.aide-data/mcp/docker-gateway/profile.yaml`，独立网关令牌文件位于 `.aide-data/storage/data/mcp/docker-gateway/auth-token`（0600）。这些私有文件不进入 Git、镜像或共享包。实际目录以本机配置为准。

当前启动器没有自动管理 Docker MCP 网关进程，也没有把可选 MCP 服务镜像纳入 AIDE 标准离线包。网关或服务镜像未运行时，仅保存来源配置不能使连接可用。恢复本机 Toolkit profile 可使用已有工具的 `docker mcp profile import .aide-data/mcp/docker-gateway/profile.yaml`；不要把令牌写入命令参数或日志。

## 验证状态

当前源码的合成 HTTP JSON/SSE、认证拒绝、来源变化、取消、只读策略、配置保留及旧 stdio 专项通过；实际 Safari 表单组件已验证传输切换与令牌分离。当前源码随后通过实际 Docker 网关认证、10 项工具发现、说明读取及临时 App 从磁盘重建后再次连接；10 项中 0 项声明只读，未执行工具。此结果不等于模型调用或宿主机/容器重启验收。完整工作台 UI 因用户暂不方便处理证书告警而延后。

准确来源、内容指纹和未完成项见 [MCP 恢复任务](../tasks/mcp-http-synced-main-20261017.json)及[审计记录](../reviews/2026-10-17/mcp-http-synced-main-audit.md)。Windows 由用户实机验收。
