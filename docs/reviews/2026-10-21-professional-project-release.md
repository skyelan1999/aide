# Aide 与 ProfessionalProject 发布准备 — 2026-10-21

用户授权 push、release 和新版插件包上传，私有插件独立分发。

Aide 范围为设置导航合并为七组、按需展开高级设置及 .pp 工作台入口。沿用现有 API 权限、来源快照和写入审批。插件文件扩展名仅提供入口，不授予访问权限。保留远端 main 的提醒、STEP 与 URDF 改动。

先前本地验收见对应任务账本；本次在干净 main 发布副本执行完整回归并重新构建。宿主源码和 Docker 上下文不含本机安装插件、升级备份、工作区、数据卷或本地 compose 变更。ProfessionalProject 使用私有仓库和私有 Release，公共包只包含通用 .pp 关联协议。

回滚：保留上一发行包及用户现用容器和数据卷；本次发布不切换现用实例。候选只在独立测试实例运行。镜像和远端附件验收完成后补发行收据。

## 已完成发行

[Aide 0.1.17.4 RC1](https://github.com/skyelan1999/aide/releases/tag/v0.1.17.4-RC1) 已公开发布，发布标签指向 `c8748b2442130c7f60551b4915d03b4e6c84fb36`。两个 Docker 架构的完整 ZIP、同标签源码 ZIP、元数据和 SHA256SUMS 共五份附件远端大小与 SHA-256 全部匹配。完整运行检查通过；文档迁移链接失败后已单独修复并复查，构建输入指纹保持不变，原始失败收据与复查记录均保留。

[ProfessionalProject 0.1.0](https://github.com/skyelan1999/aide-pkg-ProfessionalProject/releases/tag/v0.1.0) 仅在私有仓库发布；下载包和十二份包内文件与私有标签逐项匹配。Aide 源码根插件目录只有测试夹具；两个镜像当前插件文件为零，全部 54 / 63 个引用历史层均未发现私有插件或升级备份。

两种架构都验证健康、鉴权、实际资源指纹，以及临时容器内调用 ProfessionalProject 计算示例计划；AMD64 为本机仿真。未进行付费模型聊天、语音回放或 Microsoft Project 桌面验证。用户现用容器未切换、未重启，数据与私有插件保留。完整元数据见[发布收据](2026-10-21/professional-project-release.json)。
