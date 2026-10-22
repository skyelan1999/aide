# aide 0.1.17.5 RC3

修复 Windows Docker Desktop 使用 containerd 镜像存储时，离线镜像导入后的本机 ID 与包内 ID 不同，导致启动报“无法初始化 A/B 活动镜像槽”的问题。

启动脚本保留归档 SHA256 和架构检查，仅从本次成功导入的已校验归档确认镜像，验证槽位后保存本机 ID。首次启动和重复启动均有回归覆盖。

已有 RC2 用户可下载 `aide-v0.1.17.5-RC2-windows-launcher-fix.zip`，备份原 `start.ps1` 后仅替换此文件，再运行 `start.bat`。请保留原安装目录中的 `.env`、`.aide-data`、workspace、context 和 Docker 数据卷。此修复包不含镜像，也不需要删除或重装 Docker。

新用户请根据 Docker 引擎架构选择 ARM64 或 AMD64 完整包。各架构的构建、运行、归档摘要与测试边界见 RELEASE-ASSETS.txt。Windows 修复已在 PowerShell 7 的隔离 Docker 测试中复现和验证；Windows 真机和 Windows PowerShell 5.1 仍待用户现场验收。本次只修复 Windows 启动流程，不宣称修改了应用内升级导入器或其他平台启动器。

私有商业插件、用户项目、会话、令牌和已安装插件不进入公开发行包。本次发布不重启现用服务或迁移用户数据。
