# Docker MCP 接入

当前开发分支新增 Streamable HTTP 引用来源。引用设置中选择 MCP、Streamable HTTP，填写完整 `/mcp` 端点与独立 Bearer 令牌。令牌由现有本地保险库加密保存；编辑时留空保留原凭据。原有 stdio 程序与离散参数继续可用。

HTTP 客户端不跟随重定向，不把令牌放入 URL，不继承模型密钥。远端仅支持 HTTPS 并验证证书；明文 HTTP 限定回环地址或 Docker Desktop 私有宿主机别名。本机地址绕过环境代理。每次发现或调用新建有超时、有响应大小限制的会话，并尝试 DELETE 关闭。

来源地址和凭据按同一注册状态读取。后续 POST 前复查连接与凭据，来源被改配、停用或删除后拒绝继续；工具发现完成后再次核对，避免旧结果覆盖新配置。已发送的网络请求无法撤回，关闭旧会话仍使用该会话原凭据。

## 本机 Docker 验证进度（2026-10-17）

已使用 Docker MCP Toolkit 0.43.3 创建独立测试 profile，并启动宿主机认证网关。网关仅绑定 `127.0.0.1:64002`，子服务上限为 256 MiB / 1 CPU，关闭文件监听与调用日志。AIDE 容器使用 `http://host.docker.internal:64002/mcp`，勾选“Docker 本机网关”，将 Host 保持为 localhost。该模式需要独立 Bearer，不开放 LAN，也不挂载 Docker socket。

AIDE 中已注册 `docker-mcp` 来源，令牌存现有保险库；启动目录下 `.aide-data/mcp/docker-gateway/profile.yaml` 保存 profile，`.aide-data/storage/data/mcp/docker-gateway/auth-token` 保存供宿主机网关使用的令牌文件（0600）。这些是本地配置，不进入 Git 或镜像包。

Safari 实际点击“测试 MCP 并发现工具”已显示 10 个工具，其中 0 个声明只读。Time 服务提供 `get_current_time` 与 `convert_time`；Toolkit 还暴露管理工具。当前这些工具均未返回 `readOnlyHint:true`，所以 AIDE 引用策略会拒绝执行。独立宿主机协议探针成功调用过 Time，但不能作为 AIDE 工具实际执行或模型调用通过的证据。

网关目前是本机手动启动进程；自动启动、退出管理和跨平台交付尚未完成。新一致性修复后的针对性 race / vet、quick 与 Safari 表单布局、传输切换、令牌留空保存及工具发现已通过。18 个鉴权、凭据和会话文件哈希保持一致。本机候选镜像已更新；精确输入full109已通过（server race768.615秒、vet/CAD），源码检查点已推送；规范双架构镜像和最终公开交付仍需完成。已有单文件离线镜像包不包含后来下载的可选 MCP 服务镜像。不要据此宣称 MCP 交付或离线重启已完成。


## 手动恢复网关配置

本机已验证：从启动目录导出的 profile 以独立临时ID重新导入后，固定的 Time 镜像及工具描述保留；随后移除临时ID，原 profile 列表精确恢复，运行网关不受影响。没有为此删除真实 Docker 配置。该证据证明配置可导入，不代表网关已有自动启动能力。

若 Docker Toolkit 的全局 profile 丢失，可在项目启动目录运行：

```sh
docker mcp profile import .aide-data/mcp/docker-gateway/profile.yaml
```

令牌仍在启动目录下的私有文件中，不能放进命令参数、Git、日志或共享包。Time 服务镜像也需要已导入或下载；仅导入 profile 不等于镜像已在本地。现有旧版单文件镜像包不包含该可选服务。

工具发现还会以虚拟 Markdown 文件出现在引用目录 `tools`。本机Safari已实际预览 `get_current_time` 描述，并与独立API读回比对；页面明确保留不可调用原因。预览没有执行工具，返回时原文件视图恢复，18个保护文件哈希未变。
