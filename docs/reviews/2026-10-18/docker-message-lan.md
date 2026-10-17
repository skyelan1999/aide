# Docker 消息局域网桥验证

任务：`docker-message-lan-20261018`。源码基线 `97c2554a`，最终受测输入指纹 `eccc44d0dd13ece293a2f1214eef6b633fc56833bc459782e0de88418ebcb7c9`。指纹包含当时工作区已有的其他插件改动；该任务不提交这些文件。没有修改用户 Clash、路由、防火墙、证书信任或正在使用的工作台。

## 问题与最终行为

原部署只发布工作台回环端口；容器组播与容器地址无法直接作为宿主局域网发现路径。新 Compose 为消息 TLS 增加随机回环映射，原生宿主桥负责物理 IPv4 接口上的组播和 TCP 转发。镜像带六个平台/架构目标，宿主无需额外安装语言运行时。启动器从不可变镜像 ID 的临时容器提取并校验程序，维护停止同时停止自己的桥。

宿主与容器通过现有数据挂载内的有界私有目录交换租约和公开签名卡；随机代号防止旧回复/停止请求混入。桥不持有设备私钥，不终止 TLS，不代替联系人同意或拉黑判断。没有有效容器公告时不对外发现或转发。公告心跳独立于可能阻塞的审批重试。

## 已执行

| 检查 | 环境与结果 |
| --- | --- |
| `python3 scripts/agent-route.py verify quick` | macOS，125 项通过；含 4 项新增启动器隔离回归与 9 项维护脚本回归。收据日志 `.agent-state/verify-20261017T165956635872Z.log`（UTC 时间名）。 |
| `go test -race -mod=vendor -count=1 -timeout=2m ./internal/messaging` | 无外网专用 Linux 容器；最终通过，7.965s。真实 UDP 回环查询、文件邮箱、TCP 转发、mTLS、联系人审批、聊天、拉黑；密文捕获中没有测试聊天明文。 |
| `go test -race -mod=vendor -count=1 -timeout=3m -run '^TestMessageCenter' ./internal/server` | 同一隔离容器，通过，7.414s。包括鉴权/账户门槛、固定容器端口、浏览器不能覆盖邮箱路径、缺桥诊断和配置不暴露私有路径。 |
| `go vet -mod=vendor ./internal/messaging ./internal/server ./cmd/aide-message-lan` | 隔离容器通过。 |
| `sh scripts/build-message-lan.sh /tmp/lan-helpers` | CGO 关闭，六目标交叉编译通过：darwin/linux/windows × arm64/amd64，Windows 文件为 `.exe`。 |
| macOS arm64 原生测试程序 `-test.run '^TestLANBridge'` | 本机 5 项通过：真实回环发现及加密/审批、过期租约/根目录约束、慢审批心跳、错误代号回复/不安全目标、停止代号隔离。 |
| `test_runtime_assets.py aide:lan-fix-candidate --require-lan-bridge` | 隔离候选镜像，UID 1000；六程序清单与 SHA-256、非 root 读取、原有 DOCX 往返通过。候选镜像用最终编译程序/资源叠加保留的本地运行时，不冒充完整正式发行构建。 |
| 实际 macOS Docker Desktop 启动链 | 独立 Compose，随机回环消息映射；从镜像提取宿主程序并校验，宿主原生桥启动/停止成功；宿主 0700 目录内邮箱可由容器 UID 1000 读写 0600 文件。没有发布身份公告、没有联系真实设备；临时容器和网络已销毁。 |
| Shell 语法、`git diff --check`、Compose 解析 | 通过。PowerShell 保留 UTF-8 BOM，Windows 执行未验收。 |

回归中先发现测试账户未填写、慢审批测试仍存在直达路径，分别补齐合成账户和移除合成直达路径后重新运行通过；未放宽产品的账户门槛或审批限制。

## 未执行与交接

- **用户两台电脑的实机互通 NOT_RUN**。隔离发现测试使用实际 UDP 回环替代物理组播，并不证明 Wi-Fi/防火墙/TUN 放行。macOS 宿主桥实际启动验证了物理接口建监听，但没有接触用户联系人。
- **Clash/TUN 兼容验收 NOT_RUN**，也未修改代理配置。没有 Clash 时仍走相同 Docker 桥路径。
- Windows/Linux 宿主运行、系统防火墙提示、多物理电脑、跨 NAT、真实语音媒体不在本次通过范围；交叉编译不替代这些结果。
- 宿主接口/IP 变化后需重新运行启动器以刷新地址；身份密钥和六位码不变。Linux 自定义容器 UID 仍须保留既有持久目录权限要求。
- 未运行仓库全量 full，未升级版本、推送、发布、替换生产镜像或重启当前工作台。两端需要包含宿主程序的新镜像及匹配 Compose/启动脚本，旧镜像只更新脚本不足以启用。
- 回滚时先停止自己的宿主桥，再一起恢复旧源码/镜像/Compose；不删除 `.aide-data`，不重置联系人密钥。其他并发插件改动保留原样。
