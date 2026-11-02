# Terminal 会话复制、Telnet、X11 发行验收

已正式发布并标记 Latest：[Aide v0.1.18.0-RC3](https://github.com/skyelan1999/aide/releases/tag/v0.1.18.0-RC3) 与 [Terminal v1.6.0](https://github.com/skyelan1999/aide-pkg-terminal/releases/tag/v1.6.0)。请先升级宿主，再安装插件。市场最低代码门槛为 RC2，本轮可下载宿主为 RC3。

| 项目 | 实际结果 |
| --- | --- |
| Aide 源码身份 | 标签提交 `86f83431ec3fceb67dc4e4722ab67cf603dffaaa`；源码 SHA256 `27cae693b5029dbebb10b2e8c51d2b1a7261d9e86ff4b2c14b02d8e9b276de2f` |
| Terminal 安装包 | 1,071,482 bytes；SHA256 `c682f56ff469212b8da7f5e4e9e25052d5d8248c3f298345611558578c2b149b` |
| 本地完整前提 | 干净、非同步目录的 main 上 full 318 项通过，结束指纹一致 |
| 双架构 Go | ARM64、AMD64 各登记 1,594 项，1,515 通过、79 条件跳过；24 个互斥分片完整完成，其他包、平台契约、vet 通过 |
| 现行公开插件 | 两架构各 30/30 包检查、实际安装、功能通过；Terminal 原始 ZIP 复制/Telnet/隔离/动态输入策略用例真实执行 |
| X11 | 真实 Xvfb/xauth/OpenSSH/X 客户端直连与中继副本窗口通过；两架构 runtime/reimport 共四份 xauth1.1.2 检查通过 |
| 浏览器与宿主 | 真实 App 三浏览器 48 格通过（blue/comfort）；Windows 10 项、macOS ARM/Intel 各 8 项通过；干净 Linux 生命周期通过 |
| 插件主题 UI | 专业 blue、舒适 comfort、经典 green × 明暗 × 桌面/窄屏 12 格通过；实际 HTML/Sessions/Telnet，桥接与工作区资料为夹具 |
| 发布资产 | 7 份 Aide 上传附件与清单、源码和标签身份独立核对；小清单实际下载，大包以服务器摘要及流水线实际 CRC/导入/运行证据共同核对 |
| 客户公开入口 | 13 条匿名 HTTP/TLS 路径通过；公开 Release、市场 Pages/raw/frozen 包与校验文件一致 |

完整 [Delivery 10/10](https://github.com/skyelan1999/aide/actions/runs/41396578224) 与[发行收据](terminal-aide-publication.json)。插件[公开入口](terminal-plugin-publication.json)、[主题逐格数据和代表截图](terminal-ui-themes.json)、[本地 full](terminal-local-full.json) 分别记录，静态源码用例数量不等于执行通过。

本轮保留了失败事实：[第一次 full 的输入漂移与重复文件/Git 引用备份](terminal-source-stability.json)；[RC2 说明章节格式失败](terminal-rc2-source-gate.json)。RC2 没有运行原生/公开包验收，也没有发布运行包；其标签未改写。修复使用原有格式校验和 version.sh 的下一补丁标签，没有放宽门禁。

发布不等于本机部署：生产容器仍健康，镜像 `sha256:e4b3040c61c64835833bfc2e28101b0da263842ea51efd1d588df20a75f3e84b` 未切换，用户会话和数据未重启或迁移。原 Aide RC1、Terminal1.5.3 安装包及其他 29 个公开、3 个私有展示条目保持不变。

真实客户设备、物理串口、macOS XQuartz、Windows X Server、付费模型与系统隐私授权没有被上述隔离测试替代。X11 仍需用户可认证访问的 X Server、DISPLAY/Xauthority 和远端配置。停止/升级插件会断开连接，不会自动重放命令；升级前保留数据备份和旧运行包。
