# 插件运行文件与源码分离

插件源码、安装包与 Release 仅在各自 `aide-pkg-*` 仓库维护，市场登记分发。Aide 主仓库不再携带插件副本，Docker 镜像不预装插件，首次启动也不自动安装。请从插件市场或经过摘要校验的 ZIP 明确安装所需插件。

`plugins/` 是用户安装、设置和升级后的运行状态，不由 Git 跟踪；仅 `plugins/testdata/` 保留宿主协议测试夹具。主程序源码指纹和镜像不包含安装状态。已有插件、注册表、设置、日志和会话均保留，不随主程序更新同步或覆盖；旧镜像遗留的 `AIDE_BUILTIN_PLUGINS` 环境变量不再触发安装。

Office 已独立分发于 https://github.com/skyelan1999/aide-pkg-office 。串口、SSH、TCP 和 UDP 能力归入 Terminal，旧独立通信插件与 Propeller Aero 已从市场下架；已有用户文件不自动删除。

## 独立插件集成测试

宿主协议单元测试使用 `plugins/testdata/` 中的合成夹具。验证真实插件时，请在主仓库外准备各独立仓库的指定提交，以插件 ID 命名目录，并显式设置 `AIDE_PLUGIN_TEST_ROOT=/path/to/plugin-checkouts`。例如根目录下的 `terminal/` 对应独立 Terminal checkout。这些来源只供测试读取，不参与镜像构建、启动安装或插件升级。未配置时，相应 Go 集成测试明确跳过；已配置但缺失文件会失败，不自动联网下载。

Terminal 完整安装包集成测试还可以显式提供 `AIDE_TERMINAL_BUNDLE_PATH=/path/to/verified-terminal.zip`。JS 插件检查脚本同样要求 `AIDE_PLUGIN_TEST_ROOT`。浏览器桥接服务使用宿主自己的 URL 准入策略，不再导入插件源码。

## 已有克隆的一次性迁移

上游首次移除已跟踪插件文件时，Git 可能阻止拉取本地修改，或删除未修改的旧文件。必须先备份，不能使用强制 reset、clean 或放弃本地升级来绕过。

从旧克隆根目录执行，备份放在仓库外：

```sh
# 使用尚不存在的备份目录；包含注册表、自定义插件及本地升级。
mkdir ../aide-plugins-backup
cp -a plugins ../aide-plugins-backup/
# 暂存仅限 plugins 的已跟踪修改；有其他主程序修改时按通常 Git 流程保留。
git stash push -m plugin-state-before-isolation -- plugins
git pull --ff-only
# 完成拉取后恢复安装状态。不要将旧 stash apply 回已删除的源码路径。
cp -a ../aide-plugins-backup/plugins/. plugins/
git status --short
```

确认插件、设置和本地升级均正常后再自行清理备份与这次 stash。不要将备份注册表或凭据提交到 Git；本迁移不重启服务，也不发布宿主镜像。后续正常插件升级只修改已忽略的运行文件。

官方插件未配置自定义升级地址时，升级入口改用官方市场，以独立仓库登记的包和摘要为准；旧官方源码地址自动迁移，自定义 URL 保留。
