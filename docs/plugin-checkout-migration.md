# 插件运行文件与源码分离

`plugins/` 是用户安装、设置及升级后的运行状态，不再由 Git 跟踪；仅 `plugins/testdata/` 保留测试夹具。`bundled-plugins/` 是受控的初始分发和测试输入，不能将市场升级写入此目录。源码指纹和 Docker 镜像只使用初始副本，插件升级不会改变主程序构建身份。

源码工作区首次缺少 `plugins/registry.json` 时，Aide 从同目录下的 `bundled-plugins/registry.json` 初始化插件。已有注册表、插件代码、设置和删除记录保留，不在后续启动时重置。镜像仍独立携带已有五种控制插件；外部工作区沿用原有安装行为。

Office 已独立分发于 https://github.com/skyelan1999/aide-pkg-office 。旧通信能力归入 Terminal。Propeller Aero 不再随初始副本分发，也从市场下架；已有用户文件不自动删除。

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
