# Windows 独立工作台窗口

对应 [#751](https://github.com/skyelan1999/aide/issues/751)；构建关闭竞态修复跟踪于 [#812](https://github.com/skyelan1999/aide/issues/812)。用户确认 `a6728b16` 的 Windows 浏览器主工作台和插件均能正常打开；这次适配补充独立应用呈现，不重新引入 Concept03 布局。

## 源码安装与窗口

Git 源码目录运行 `build.bat` 编译当前单 EXE 宿主与后端镜像，成功后启动；后续 `start.bat` 复用已安装的版本，不隐式编译。已有后台服务、Docker 项目、端口及数据迁移流程保持。Mac 的 AppKit 宿主不参与 Windows 构建。

Windows 窗口使用 Microsoft Edge WebView2 Runtime；窗口、标签和系统面板由原生 C++ 宿主管理，不显示浏览器地址栏。需要系统已经安装兼容的 WebView2 Runtime。缺少或初始化失败会报告错误，不静默打开普通浏览器，也不自动下载/安装 Runtime。SDK 只在显式构建时从官方 NuGet 下载，固定版本与 SHA256，Loader DLL 和许可声明嵌入原有单 EXE；启动时只释放并校验本安装私有缓存中的 Loader，不在线编译或下载 SDK。

默认启动进入独立 Aide 窗口。同安装再次启动激活已有窗口，不启动第二份后台服务。`+` / `Ctrl+T` 新建工作台标签；鼠标悬停某标签显示该标签的 `×`，点击关闭该标签，中键点击也可关闭；`Ctrl+W` 关闭当前标签。关闭非活动标签会保留当前标签，关闭活动标签则选择相邻标签。`Ctrl+Tab` 切换标签，`Ctrl+R` / `F5` 刷新，`Ctrl+,` 打开既有设置。原生标签共用该安装的 WebView2 profile，保留原有工作台内容、布局与插件沙箱；本地 `window.open` 由带 deferral 的 NewWindowRequested 接入应用内新标签，外部网页仅在用户操作时交系统浏览器。并发标签最多 16 个。

系统文件选择和网页脚本对话框由 WebView2 承载。下载先显示原生保存对话框，取消不写文件；权限与下载事件先取得 COM deferral，再调度原生面板，避免事件内嵌套消息循环。麦克风/摄像头仅本机主工作台可申请，需用户确认。插件存在未保存修改或无法确认页面状态时，关闭会询问，取消保留标签；关闭最后标签只退出 GUI，保留后台任务。桌面子进程启动时先取得本安装单实例锁，并在私有状态目录登记 PID 与进程创建时间，然后才初始化服务和窗口。显式构建请求正常关闭后等待桌面进程确实退出；如果旧进程仍在初始化、窗口尚未出现，也会等窗口出现后再关闭。超时会保留旧程序并提示处理未保存确认，不强制杀 GUI。

## 原生标签和图标

对应 [#757](https://github.com/skyelan1999/aide/issues/757) 与 [#779](https://github.com/skyelan1999/aide/issues/779)。标签栏高度为 32 DIP，关闭叉号随鼠标悬停显示在每个标签内，新建按钮紧随标签组；溢出时为原生左右箭头保留独立区域。使用紧凑 Segoe UI、实体底色和选中标记；文字对比不足时使用可读色，保持键盘焦点可见。标签宽度随窗口/数量收紧，溢出仍通过原生滚动控件操作；不新增内部网页滚动条。字体采用系统字体平滑偏好，不强制 ClearType。主页面配色继续通过既有受限 appearance 消息同步；插件不能修改原生身份验证桥。

EXE 嵌入现有 Aide 蓝底白色 a 标志，含 16/20/24/32/40/48/64/96/128/256 像素图标；窗口大小图标和任务栏使用内置资源，启动不下载图标。构建链接资源同时声明 Common Controls v6 和 PerMonitorV2；收到 DPI 变化后更新字体、几何和图标，再使用系统建议的窗口矩形。资源对 AMD64/ARM64 共用，普通 Windows/Linux 构建直接消费已提交的 ICO。维护者在 macOS 可运行 `python3 scripts/build-windows-icon.py`，复用原有原生图标渲染器生成 ICO；无需安装第三方图形包，不参与 Windows 启动。

Windows runner 的窄窗口和合成 WM_DPICHANGED 截图用于验证宿主响应；不代表已在实体廉价显示器或多显示器硬件上测试。

## 会话、证书与身份验证

桌面子进程仅通过参数接收安装路径；它自行校验现有实例并经私有 Docker 管道读取 transport token 和当前服务证书。Token 不出现在子进程命令行、桌面日志或插件新页链接中。TLS 仅对精确 localhost 原点、端口及一致叶证书作例外，同时检查证书有效期和 localhost 名称；没有全局忽略证书错误或写入系统信任根。

profile 位于当前用户 LocalAppData 的 `Aide/Desktop/<安装摘要>/`，与浏览器普通 profile 分离，按安装隔离并设置当前用户 DACL。Windows Hello 使用系统 WebAuthn API；私钥由 Windows 认证器管理，系统处理指纹、面容或 PIN，应用不保存账户密码。首次用账户密码进入「设置 → 数据与账户 → 账户 → 绑定 Windows Hello」完成单独绑定；浏览器凭据保留。系统不可用或取消时仍可用账户密码。

原生桥仅公开 status/create/get，接收有界 JSON，限制当前可见主工作台顶层文档；插件页、iframe、其他原点/端口不能调用身份验证。API 返回标准 WebAuthn 注册和签名响应，继续由服务端执行挑战、原点、公钥、UV、一次性、过期和锁代际检查，原生布尔值不能解锁。切换/隐藏/关闭窗口会取消进行中的系统认证，导航代际变化后不交付旧结果。Hello 不代替重启后用密码派生保险库主密钥的既有规则。

## 独立工作台电脑控制

Windows 桌面桥只在交互桌面运行：锁屏或安全桌面不可读写。应用身份以进程可执行文件名和 PID 表示，`/v1/foreground-app` 只报告当前前台应用；`/v1/resolve-app` 只按准确进程名查找唯一可见应用，不读取其它窗口文字。交互动作必须同时匹配逐次 Aide 审批、允许应用、审批时 PID 和进程规则；坐标点击限定在已审批前台窗口内。截图返回 HWND 与物理窗口矩形，让服务端把图像像素换算到桌面坐标，并在动作前核对窗口没有移动或变化。

| macOS desktop route/capability | Windows implementation/status | Difference and acceptance |
| --- | --- | --- |
| foreground app / resolve app | Process name + PID; exact app resolution to one visible window | Windows executable identity replaces macOS bundle ID; duplicate matching processes are rejected. |
| window snapshot | Current approved foreground HWND, PNG, HWND, physical bounds and pixel size | No OCR text locations or background capture yet; PrintWindow may omit GPU-only content. |
| inspect controls | Tracked separately in [#924](https://github.com/skyelan1999/aide/issues/924) | UIA inspection/semantic actions require bounded-provider and real-app acceptance. |
| click / type / key | Approved current foreground process | Click is window-bounded; stale PID/HWND/bounds are rejected. `CMD` maps to Windows `Ctrl` for common key chords. Background semantic focus is not yet equivalent. |
| drag / scroll | Approved foreground-window drag with bounded duration and guaranteed mouse release; wheel input is constrained to the approved window and revalidates focus/geometry | Windows wheel messages approximate shared pixel deltas at 3 wheel units per pixel; no animated controller pointer yet. The isolated integration fixture checks successful input and invalid-target denial. |
| open browser / browser page balance | Approved Chrome launch with an optional complete HTTP(S) URL; Safari is unavailable | Chrome must resolve to an existing executable through Windows App Paths or a standard installation folder. Credentials, whitespace/control characters, quotes and backslashes are rejected before process launch. Browser page balance is still unsupported; the macOS flow reads only an explicitly bound Safari accessibility tree and needs a separate Windows UIA design. |
| system input / app / capture scheduling | Bridge currently serializes all requests | Safer than concurrent global input, but independent app/capture lanes are not yet equivalent. |
| virtual controller pointer | Not yet implemented | The Windows design must remain non-activating, non-intercepting, DPI aware, high contrast and auto-dismissing. |

The current implementation and evidence are tracked in [#932](https://github.com/skyelan1999/aide/issues/932) and [the task record](../tasks/windows-desktop-control-20261116.json). The table describes an incremental parity audit, not completion of all macOS workflows. Touch ID has a Windows Hello counterpart documented above. Local real-app interaction and visual overlay acceptance must be recorded separately from CI compilation.

## 验证范围

```text
python scripts/test_windows_desktop.py
node scripts/plugin-workbench-tabs.test.cjs
node scripts/native-touchid.test.cjs
python scripts/agent-route.py verify quick
```

`Windows Desktop Check` 只做验证，不调用 Delivery 或发布 Release：Linux 分别交叉编译 AMD64/ARM64；Windows runner 用单独测试 EXE 和合成 localhost TLS 服务，实际检查 WebView2 窗口、应用内新标签、共享存储、沙箱 MessagePort、插件页原生桥隔离、未保存关闭取消及下载取消。测试 EXE 不编译进产品；测试不读真实账户、启动 Docker 或发起真实 Hello 注册/签名。合成界面不等于 Compare/ProfessionalProject 真正功能或用户设备验收。

精确通过/未执行项、源码与构建证据见 [任务记录](../tasks/windows-desktop-20261103.json)。Windows Hello 硬件签名、用户环境完整 build/start 和实际插件操作仍须在对应 Windows 设备验证，不能用跨编译或隔离窗口测试替代。

