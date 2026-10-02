# aide v0.1.14.0-RC13 资产与验收记录

## 目标

发布包含 macOS、Windows、Ubuntu 启动脚本和 Linux ARM64 镜像的完整统一包。该 ZIP 同时支持全新安装与升级页暂存；macOS 自动解压时，可直接选择解压后的完整文件夹。升级先写入非活动槽，由用户手动激活。

## 构建与测试

| 项目 | 结果 | 证据 |
| --- | --- | --- |
| 版本与源码 | RC13；构建源码提交 `b02edae41a6e135728928c26ee280e3dc5d76bf2` | `main` / `v0.1.14.0-RC13` |
| Full 门禁 | PASS | `python3 scripts/agent-route.py verify full`；fingerprint `8b7e113a35b99e92a4b0a169e743d280249e4cec92841dda34f8dfb8e9b098e1`；竞态测试、vet 和文档检查通过 |
| 镜像构建与 `/healthz` | PASS | `scripts/docker-release.sh`；镜像 `sha256:962fc2fa7e18c0db494559ecf5632fe2ae72c993329dc0f20b1a4196170b3861`；容器健康检查通过 |
| 完整 ZIP 上传与非活动槽暂存 | PASS | 隔离 RC13 Release 实例接受真实完整 ZIP，HTTP 201、目标槽 B；槽 A 仍为 RC12，槽 B 暂存 RC13，镜像 ID 与包内 manifest 一致 |
| 浏览器上传后刷新 | 源码修复完成；Safari 实际 UI 复验受阻 | 成功提示现读取 `response.data.targetSlot` 后调用 `loadSlots()`。Safari 对隔离 `localhost:18097` 显示“此连接非私人连接”；按安全策略关闭临时标签，没有绕过警告。修复前真实包 UI 测试复现了 `result is not defined` |
| SHA256 / ZIP 完整性 | PASS | 所有发布附件通过 `shasum -a 256 -c SHA256SUMS`；全部 ZIP 通过 `unzip -tq` |
| GitHub prerelease 与远端附件 | 发布后补记 | 发布完成后更新 Release URL、远端附件清单和校验结果 |

## 发布资产

预期附件：

- `aide-v0.1.14.0-RC13-macos-arm64.zip`
- `aide-v0.1.14.0-RC13-windows-arm64.zip`
- `aide-v0.1.14.0-RC13-ubuntu-arm64.zip`
- `aide-v0.1.14.0-RC13-linux-arm64-image.tar.gz`
- `aide-v0.1.14.0-RC13-full-linux-arm64.zip`
- `SHA256SUMS`、`RELEASE-ASSETS.txt`

统一 ZIP 内应有全部平台启动器、一份容器镜像归档及供升级验证使用的 `manifest.json` 和 `SHA256SUMS`。它们与首次安装共享同一份运行材料，升级时仅写入非活动槽，不接触共享会话和工作区卷。

关键资产 SHA256（全部附件见 `SHA256SUMS`）：

| 文件 | SHA256 |
| --- | --- |
| `aide-v0.1.14.0-RC13-linux-arm64-image.tar.gz` | `ad0abbc19e31217bbbff158ae49895d1d46808e3726f3d13a2834d9a392c8925` |
| `aide-v0.1.14.0-RC13-full-linux-arm64.zip` | `ff1dce1417576460c3fe70da405ef07a608e60a2d0de2c982e35eb71e30c9fdc` |

## 结果

Full 门禁、镜像冒烟、真实完整包校验和隔离实例暂存已通过。Safari 因证书安全拦截未完成修复后 UI 的最终观察；远端发布后将补记 Release URL 和附件复核结果。
