# 真实独立 SFTP 能力仪表补验

本轮在 [FTPS 延迟任务](../../tasks/ftps-index-latency-20261015.json)的完整回归等待期间，为连续目标第7项补充实际能力仪表 API 证据，无额外产品代码改动。

使用当前实际二进制候选 `236ad9bc3e3c`、交付镜像默认 aide 用户，以及一次性真实 OpenSSH／SFTP 服务。两者仅连 internal Docker 网络，不发布端口、不挂载用户目录，保留 cap-drop ALL 和 no-new-privileges。通过实例自动生成证书校验 HTTPS，创建只指向该服务的可写 SFTP 来源；凭据为仓库公开的测试凭据。

实际通过 `/api/capabilities` 和 `/api/capabilities/check` 完成：

- SFTP 根目录只读检查通过，写入模式在根目录创建并删除独占临时探针。通过独立服务端文件哈希和查找确认原文件不变、探针目录未残留；读写收据分开呈现。
- 修改测试密码后，之前的读／写收据都变为 stale；实际只读检查为 failed。恢复正确测试密码后重新检查为 pass，未沿用旧连接绕过错误凭据。
- 当前镜像内 pypdf、docx、openpyxl、pptx 四个解析器隔离导入均通过。响应未包含测试密码。

首次探针错误地覆盖为 root 用户，同时移除了所有 capabilities，无法跨越 aide home 的权限边界，API 报 mkdir `/home/aide/.ssh` permission denied。改为交付镜像默认用户后通过，未放宽目录权限、增加 capabilities 或修改产品代码；失败日志保留，不记为产品缺陷修复。

原始脚本／日志／完整收据位于 `.agent-state/ftps-index-latency/capabilities/`，摘要与哈希见任务账本。自建服务、数据与网络已清理。此项仅证明实际 HTTPS 能力检查 API、独立 SFTP 及模块导入；不证明完整浏览器仪表、任意文档处理、插件工具执行、模型生成／视觉、用户 SSH 主机或真实硬件网络。总目标第7项继续进行。
