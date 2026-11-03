# Windows 原生选项卡外观

任务：[windows-native-tabs-20261103](../../../tasks/windows-native-tabs-20261103.json)；问题：[Aide #751](https://github.com/skyelan1999/aide/issues/751)。

## 行为

Windows 独立窗口的选项卡、新建和关闭按钮使用 Aide 的背景、面板、选中、正文、次要文字和分隔线颜色；选项卡采用圆角、Segoe UI 字体和长标题省略。主页面切换明暗或配色时同步更新原生控件。

保留原生 TabControl 的标签操作与辅助接口。系统标题栏只请求操作系统明暗模式，不强制设置标题文字和底色，避免旧系统或远程桌面部分支持 DWM 属性时文字不可读。

外观消息仅接受当前可见主页面，沿用既有来源限制。消息只包含明暗布尔值与六个严格校验的六位十六进制颜色；插件页面没有原生外观或认证接口。Windows Hello 的认证路径未改动。

## 验证范围

源码为 `edbe56878a79415af75da4a5a38c92ed264c9337`。双架构编译、Windows 原生 UI、仓库 quick 和关键 Go 回归均通过。实际检查 Windows 截图：深色双标签和浅色单标签均使用相应配色；文字、圆角、选中状态及增减按钮可见。

- [Windows Desktop Check](https://github.com/skyelan1999/aide/actions/runs/41587483795)：AMD64、ARM64 编译，以及真实 Windows/WebView2 的隔离页面检查。
- [Aide CI](https://github.com/skyelan1999/aide/actions/runs/41587483787)：仓库 quick、Go 编译和关键回归。

上述原生 UI 检查使用隔离页面与临时配置目录，不代表用户 Windows Docker 安装、完整工作台启动或 Windows Hello 硬件验收。

- [原生 UI 结果](native-ui.json)：`ok: true`；明暗调色板、应用内插件标签、共享沙箱端口、来源隔离、关闭取消、下载取消和工作区尺寸通过。
- [深色双标签截图](native-desktop.png)；[浅色截图](native-light.png)。系统标题栏在 Windows CI 环境保留操作系统绘制。

## 交接

仅交付 UI；用户优先自行尝试 Docker 清理，启动排查保留在独立分支 `fix/windows-desktop-startup-20261103`。本变更未修改启动保护标记及恢复逻辑；不删除 `.aide-data`，不重启现有 Mac 服务，不创建 Release、标签或版本号。

源码回滚可反向应用这次 UI 提交；运行配置和数据不需要迁移。
