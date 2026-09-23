# 0.1.6.0 RC4 发布验收

## 范围

本次合并此前由用户逐项确认的 macOS 界面、专业/经典主题、关于、Agent 路由、文档目录合并与正式文档；补充 AI 场景定制定位、安装脚本与版本化 Docker 镜像交付。用户明确授权 commit、tag、Release 和推送。

## 检查

- full：JS 语法、版本、文档、路由 3 项回归、安装 4 项回归、shell 语法、Go race/vet 均通过。
- 安装回归使用隔离临时目录与 Docker stub，覆盖配置保留、正确版本、校验错误拒绝加载；不是跨平台实机结果。
- UI 此前真实浏览器验收见 design/macos-ui.md；最终 embed 镜像将在打 tag 后进行独立冒烟。
- 不调用真实付费模型；未在 x86、Windows 实机验收。发行镜像仅 linux/arm64。
- 历史 PRD/证据保留历史含义，不伪造为本轮验收。

## 发布和回滚边界

生产 8097 不替换。此次发布目标为 GitHub main、版本 tag、Release 和镜像附件；归档保留 docker-images，不提交大型镜像到 Git。源码可回到此前 tag；运行升级前另行备份业务目录与数据卷，镜像不含用户数据。

发布后的版本/SHA/镜像校验和和最终冒烟结果记录在 GitHub Release 附件 release-manifest.json；不在源码中预先声称上传已完成。

构建发现并修复 Dockerfile 的 Go ldflags 版本含空格引用缺陷。RC1 仅为本地失败候选，不推送、不发布；正式交付 RC2。
