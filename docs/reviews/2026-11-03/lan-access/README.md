# 局域网密码访问验收

任务：[remote-access-20261103](../../../tasks/remote-access-20261103.json)；集中问题：[Aide #753](https://github.com/skyelan1999/aide/issues/753)；[行为与边界](../../../security/lan-access.md)。

## 验收清单

- 默认关闭、无密码不可开启、开启需密码；错误密码保留设置页，不误弹主令牌登录。
- 开启自动保存；原为 0 的锁屏变为 300 秒，输入下限变为 1；服务端拒绝 0，保存值保留。
- HTTPS 地址和二维码出现；使用 ZXing 实际解码 PNG，结果只有 HTTPS 登录页地址，不含查询、片段或凭据。
- 手机页面 390×844，错误密码拒绝，正确密码跳转真实嵌入工作台；连接成功且无前端异常。
- 关闭授权后旧手机会话失效并退出工作台。
- 桌面明暗与窄屏设置没有横向溢出，二维码和指纹可见；深色链接已适配主题对比度。

页面由 `TestLANAccessBrowserFixture` 在临时数据目录提供，使用虚拟密码；端口 `18640/18641` 只发布到本机回环。截图中的 `127.0.0.1` 是该隔离测试地址，实际产品地址由宿主物理私有网络接口产生。未访问用户会话或改动现有服务。

## 后端检查

`go test -race ./internal/server ./internal/messaging -run 'TestLAN|TestRemoteAccessRelay|TestCaptureRelay|TestAuthenticationAndOrigin|TestConfigImport|TestFactoryReset|TestMasterAuth|TestAutoGenerateCert|TestTLSConfig' -count=1` 已通过。

`go vet ./internal/server ./internal/messaging ./cmd/aide-message-lan` 已通过。覆盖独立令牌、主令牌拒绝、跨站/明文/Host 伪造、锁屏/改密码/关闭/过期、流上下文取消、证书 SAN/权限与 localhost 证书不变，以及固定目的地租约转发关闭。原有手机采集桥回归通过。

`python3 scripts/test_message_lan_launcher.py`：6 项通过，包含旧版发行 helper 不支持新参数时保留原启动方式。最终仓库 quick、Windows AMD64 启动器编译和 Mac/Linux/Windows ARM64/AMD64 共六种宿主桥编译全部通过。

## 页面证据

[检查结果](results.json)；[深色设置](settings-dark-final.png)；[浅色设置](settings-light-final.png)；[窄屏二维码](settings-phone-final.png)；[手机密码页](phone-login.png)；[手机深色密码页](phone-login-dark.png)。二维码使用现有 ZXing 解码器实际读取，不以图片存在代替可扫验收。

首次无密码时开关禁用；在上方设置密码后，开关立即可用，无需关开设置页。额外拒绝路径包含畸形/多对象 JSON、共用失败冷却预算和 `Retry-After`，以及关闭入口后客户端自动退出。消息模块完整 race 回归通过；没有启用设备消息时，独立工作台服务仍保持宿主桥存活。

## 未执行

用户物理手机证书信任、真实 LAN/防火墙、Windows 实机启动与当前用户服务更新未执行。不 Release、不升版、不创建 tag。
