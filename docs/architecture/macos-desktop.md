# macOS 独立工作台窗口

对应需求 [#746](https://github.com/skyelan1999/aide/issues/746)。AppKit 与 WKWebView 承载原有工作台，复用本安装的 Go/Docker 服务。Mac 宿主不打包另一份服务、模型或 API 密钥，Windows 入口保持原有行为。

## 使用与更新

macOS 14+ 的源码安装可双击根目录 `desktop.command`，或运行：

```sh
python3 scripts/build-macos-desktop.py --install --open
```

首次编译需要已有 Xcode Command Line Tools（`xcrun swiftc`）、Python 3 和 Docker Desktop。编译产物保存在安装专属的 `~/Library/Caches/local.aide.desktop.<目录摘要>/builds/`，避免 iCloud 给应用包附加属性而破坏签名校验；连接日志和当前构建收据仍在安装的 `.aide-data/desktop/`。生成本机架构的应用，安装到 `~/Applications/Aide.app`，可放入 Dock。应用绑定当前安装目录；移动源码目录后重新运行 `desktop.command`。若目标已有其他安装的同名应用，安装器保留它并报告冲突；可用 `--destination` 指定另一应用路径。

`build.command` 成功验证 Docker 镜像后编译安装 Mac 宿主，随后更新启动配置并启动本安装；Swift 编译失败时不切换服务配置。桌面代码更新通过正常退出请求替换旧应用，未保存插件内容由原生确认保护；用户取消或应用未退出时停止替换，不强制杀进程。相同桌面构建直接复用。`start.command` 只打开已经编译的宿主，尚无宿主时继续打开浏览器。常规启动不会调用 Swift 或 Docker 编译。源码构建后的回收仅删除本安装可确认的旧宿主 bundle，保留当前版本、仍运行的旧 bundle、共享 Swift 模块缓存与未知文件。

本地应用以稳定的安装专属 identifier 作 ad-hoc 签名。它不是已公证的公共发行包；本次没有发布新的 macOS Release 附件。

## 窗口和标签

- 会话仍在工作台现有侧栏内管理；`⌘T` 新建工作台标签，页面打开的本地新页也进入 AppKit 原生标签。
- 标签共用该应用的数据存储和同一个后台服务；Safari 的标签、浏览器存储与应用窗口分别管理。
- 插件列表“打开工作台”、Compare 右键入口及工程工作台每次打开新页，路由携带工作区身份和经过校验的文件选择，不带 token。新页重查插件启用与当前工作区后复用原有沙箱 iframe/MessagePort；文件预览的插件渲染器仍遵循原有文件预览生命周期。
- 用户激活的外部网页链接交给系统浏览器；不在本地宿主中加载外部顶层网站或执行自定义 URL 命令。
- `⌘,` 打开原有设置，锁屏时聚焦解锁输入；`⌘R` 刷新、`⌘W` 关闭当前页、`⌘Q` 退出应用。
- 关闭主窗口保留窗口并停止语音，点击 Dock 图标恢复；退出应用保留共享后台任务。菜单“停止本地服务并退出…”先确认中断影响，再调用原有停止流程。
- 文件选择、下载位置、打印与网页 alert/confirm/prompt 使用系统面板。

## 连接、身份与权限

应用启动调用原有 `scripts/aide.sh start`，设置 `AIDE_OPEN_BROWSER=0` 避免递归。健康实例复用；启动不会变成编译。私有子进程管道校验 Compose 容器归属、端口、内外服务实例 ID 和本机证书后返回 transport token；token 仅经既有 `#token` 启动流程交给页面，启动日志脱敏，不包含模型密钥。

WKWebView 仅对已验证 localhost 原点处理精确叶证书 pin，同时检查证书有效期和主机名。其他 HTTPS 请求遵循系统正常校验，没有全局关闭 TLS 检查或修改系统根证书库。文件面板、媒体授权和诊断计数也受本地原点约束，没有向网页暴露通用原生命令桥。

独立应用不复制 Safari 的密码、指纹凭据或已解锁状态。现有账户密码与工作台锁屏继续生效；原生设置菜单不能绕过锁屏。实测此宿主的 `PublicKeyCredential.isUserVerifyingPlatformAuthenticatorAvailable()` 为 false，使用原有密码解锁。

麦克风首次使用由系统和 WebKit 请求授权。代码包含麦克风、摄像头与语音识别的用途说明。存在 WebSocket/getUserMedia/AudioContext/SpeechRecognition API 仅代表可用性探测，不等于真实采集、实时模型通话或厂商连接验收。未授权时不自动采音或替用户点击允许。

## 验证

```sh
python3 scripts/test_macos_desktop.py
python3 scripts/test_launcher_contract.py
python3 scripts/test_source_fingerprint.py
"$HOME/Applications/Aide.app/Contents/MacOS/AideDesktop" --policy-test
python3 scripts/agent-route.py verify quick
```

开发时可通过 `open -a <本安装 Aide.app> --args --smoke-output <本地 JSON 路径>` 启用固定的原生冒烟。它统计页面异常、验证 AppKit 标签/共享 WebKit 存储、关闭恢复、系统对话框及文件选择/下载的取消路径，解锁后检查设置页，并打开 Compare 与 ProfessionalProject 的独立插件标签后关闭测试标签。测试只操作自己的临时标签，不选择、上传或保存文件，不记录正文或凭据；不会绕过锁屏、申请媒体权限或调用付费模型。默认启动没有这套冒烟操作。

精确本地验收与限制见 `docs/tasks/macos-desktop-20261103.json`。回退时退出桌面应用，在 Safari 打开现有 localhost 地址即可；删除或保留 `~/Applications/Aide.app` 均不删除后台数据。无需为回退停止 Docker 服务。
