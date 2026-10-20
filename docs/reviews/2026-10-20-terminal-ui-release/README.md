# Terminal v1.1.1 发布交付

用户授权：“push release”。本轮发布专业三栏终端工作台、连接搜索/筛选/历史分组、活动标签、连接与日志详情；配套宿主使独立标签页铺满窗口。

## 验证

6项真实PTY/SSH测试通过。最终ZIP通过SHA256检查后使用正式 `/api/plugins/terminal/upgrade` 安装到隔离8101，版本1.1.1、11项工具与宿主运行文件一致；实际安装包的Chromium四主题/详情/三种窗口尺寸/独立标签页布局与手机交互通过。完整功能、新标签页和桥接验收见前两轮UI证据；最终HTML字节相同，仅版本、README与发行记录改变。

当前源码重新运行full，共138项，133通过、5失败：启动器UID1000所有权断言；离线包测试的启动/xdg-open假设；runtime assets、Go snapshot、CAD几何缺少aide:local/aide-cad:local测试镜像，Docker拉取本地镜像名被拒绝。没有passing full收据，不进行宿主main合并、版本/tag或正式镜像发布。原始收据见full-gate.json。

## 分发与兼容

独立Terminal v1.1.1以GitHub prerelease分发，保留v1.0.0/v1.1.0及稳定市场1.0.0。ZIP和SHA256SUMS托管在固定提交的packages/v1.1.1目录，Release说明给出下载链接；不将仓库ZIP描述为Release附件。需要配套宿主PR #7中的bridge.renew、daemon.detach及独立页布局，不能只装进旧公开宿主后宣称完整支持。

宿主全部改动推送到现有codex/terminal-workbench分支和PR #7，正式Release门禁独立记录。原生产8097未替换。升级插件会结束该插件现有连接，工作区日志和记录保留；回退前备份.aide/terminal-workbench，旧归档可恢复。
