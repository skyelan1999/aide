# Terminal 会话复制、Telnet 与 X11 的宿主配套

Paired release: Terminal 1.6.0 requires Aide 0.1.18.0 RC2 or newer. See [plugin PR #3](https://github.com/skyelan1999/aide-pkg-terminal/pull/3), [host PR #731](https://github.com/skyelan1999/aide/pull/731) and [requirement #726](https://github.com/skyelan1999/aide/issues/726). Publication state is determined by the corresponding GitHub Release, not by source presence. The prior Terminal1.5.3 and AideRC1 artifacts remain immutable.


## 可信复制上下文

宿主每次正常工具准入后，仅向 Terminal daemon 提供 `sessionCopyVersion: 1`，浏览器/模型参数不能授予工作台身份。会话复制沿用 `terminal_session_create`，参数为 `copyFrom: <24位hex会话ID>`、`connect: true|false`，不新增免审批工具入口。

宿主按当前工作区私有 stateRoot 读取源会话的规范记录，校验 ID、常规文件、大小与模型可见性。副本连接只使用记录中的认证方式和私钥来源，忽略请求中的认证覆盖参数。仅复制设置不读取私钥正文；复制并连接时沿用既有引用权限检查与私钥读取流程，凭据只传给 daemon，不进入网页或回复。跨工作区及隐藏会话的模型复制拒绝。

插件生成新 ID、独立 PTY/连接、输出、日志与状态，不重放命令；串口/固定监听仅允许复制设置。共享中继复用既有固定目标和密钥，连接/PTY 独立，删除一个本地副本不撤销远端共享密钥。Telnet 直连和中继都保留现有 TCP connect/send 禁用策略，连接后撤销发送也在下一次输入生效。

## 工作区 SSH 短 socket

[#730](https://github.com/skyelan1999/aide/issues/730) 的真实复现表明，将已固定的短 ControlPath 再硬链接到长私有 stateRoot 会超过 OpenSSH Unix socket 路径上限。宿主在创建/复制连接工作区终端时先确保当前 SSH master，再将该规范 socket inode 固定到宿主短私有临时目录。上下文 `remote.socket` 为固定路径、`remote.masterSocket` 为规范原路径、`remote.pinned: true` 仅由宿主在成功固定后设置。

插件不会信任用户参数中的此标记；对可信上下文仍 Lstat 两个端点，拒绝软链接、非 socket 或 dev/inode 不一致，再复用短路径。旧宿主上下文没有该标记时仍走原有固定流程并保持失败关闭。此修复不替换工作区 master、不复制认证凭据、不使一个副本的关闭影响其他副本。

## X11 的运行依赖

新源码 Dockerfile runtime 增加 `xauth`。未启用 X11 的会话不要求 X Server 或 Xauthority。功能通过 OpenSSH `-X`、`ForwardX11Trusted=no` 连接独立 SSH 设备，包括 SSH-over-relay；不改变工作区 multiplex master。插件预检 DISPLAY、可执行 xauth、可读认证文件与相应 MIT cookie，缺失时明确拒绝，不切换为无认证。远端还需已有 xauth 与启用的 sshd X11Forwarding。

Dockerfile.local 的保留依赖镜像、dev/native 自定义环境不会自动获得此新增依赖；运行时预检明确指出缺失。发行前须构建配套镜像并验证真实 xauth。Docker 内 `localhost` 不是宿主；已有 X Server 的 DISPLAY 必须从运行容器可达，Xauthority 应只读挂载或由工作区受保护的相对文件提供。具体 Compose override、隐私/Git 排除与前提见插件说明，不自动重启生产、不自动安装 X Server、不修改系统授权/开放监听，禁止以 `xhost +` 代替认证。

## 验证入口及范围

- `TestTerminalCopyContextCanonicalIdentityAndVisibility`：规范认证来源、仅复制不读缺失私钥、隐藏/跨工作区/伪造身份拒绝；纳入基本 Go CI。
- `AIDE_TERMINAL_FEATURE_BUNDLE_PATH=/absolute/candidate.zip go test -mod=vendor -race -count=1 ./internal/server -run TestTerminalSessionFeaturesInstalledCandidate`：使用 multipart 直接 io.Copy 原始 ZIP（不重新封装），同时记录/可通过 AIDE_TERMINAL_FEATURE_BUNDLE_SHA256 校验其字节SHA；将它安装到真实隔离 App 与 Node daemon，验证本地 PTY 副本独立、关闭互不影响、隐藏复制、Telnet 真实 loopback 与动态禁止发送后远端零新增字节。无变量时显式 SKIP，不冒充安装通过。
- 既有 `TestTerminalWorkbenchBundleInstallsStartsAndRetainsLegacyTools` 可通过 `AIDE_TERMINAL_BUNDLE_PATH` 指向同一 ZIP，回归安装、工具集与既有行为；此既有夹具按成员重新封装ZIP，不作为原档字节验收。
- 插件源测试、解包运行时、真实 OpenSSH 与 TLS relay、Chromium UI 的详细证据单独记录。Earlier protocol checks use synthetic xauth/X11 fixtures and real SSH. Additional isolated Linux tests now use real Xvfb, xauth, sshd/OpenSSH and xmessage: direct and TLS-relay copied sessions create real windows, including after the source session closes. Cookie authentication and StrictModes remain enabled; this is not native customer X Server acceptance.
- Release gates build and reimport each native runtime and execute real `xauth -V`. Native macOS XQuartz, customer Telnet devices, physical serial hardware and native Windows Terminal functions remain NOT_RUN.

Source, plugin and marketplace publication are tracked in `docs/tasks/terminal-release-20261102.json`; no production restart is part of publication. 回滚时分别还原宿主/插件提交，保留所有私有会话与中继密钥；安装/退回插件会断开连接，不自动重放输入。

Historical development-candidate original-ZIP acceptance (not the final public artifact): SHA256 `5139e8ac8153bdceb4e65a5a2b14fc416edd9ab90793f96c3f13f7531f49669f`、1,071,285 bytes；原始multipart包安装与后续daemon断言race PASS1/1、3.060s（用例1.95s），日志明确no ZIP reconstruction。此前focused race8/8与既有安装回归继续有效，不用旧重打包夹具证明原档身份。
