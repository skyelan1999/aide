# aide v0.1.14.0-RC13 资产与验收记录

## 目标

发布包含 macOS、Windows、Ubuntu 启动脚本和 Linux ARM64 镜像的完整统一包。该 ZIP 同时支持全新安装与升级页暂存；macOS 自动解压时，可直接选择解压后的完整文件夹。升级先写入非活动槽，由用户手动激活。

## 构建与测试

| 项目 | 结果 | 证据 |
| --- | --- | --- |
| 版本与源码 | RC13；发布源码提交 `519b223fcfa700c431cd3e49311f1bf0c9b66f21` | `main` / `v0.1.14.0-RC13` |
| Full 门禁 | 待最终修复后的完整重跑 | 最后一次运行发现发布说明链接的本报告缺失；race 测试与 vet 已通过，其余细节见任务账本 |
| 镜像构建与 `/healthz` | 待以最终源码提交重建 | `scripts/docker-release.sh` |
| Release ZIP/文件夹上传 UI | 初步通过；修复后复验中 | 隔离桌面 Chrome / `127.0.0.1:18097`，真实完整包两种选择均 HTTP 201 并暂存槽 B；进度从 0% 到 100% |
| 上传后槽位刷新 | 已发现并修复；复验中 | `app.js` 成功提示改用 `response.data.targetSlot`，避免未定义变量导致 `loadSlots()` 不执行 |
| SHA256 / ZIP 完整性 / 远端 Release | 待最终资产生成后验证 | 本报告将在发布前补入实际资产哈希和远端核验结果 |

## 发布资产

预期附件：

- `aide-v0.1.14.0-RC13-macos-arm64.zip`
- `aide-v0.1.14.0-RC13-windows-arm64.zip`
- `aide-v0.1.14.0-RC13-ubuntu-arm64.zip`
- `aide-v0.1.14.0-RC13-linux-arm64-image.tar.gz`
- `aide-v0.1.14.0-RC13-full-linux-arm64.zip`
- `SHA256SUMS`、`RELEASE-ASSETS.txt`

统一 ZIP 内应有全部平台启动器、一份容器镜像归档及供升级验证使用的 `manifest.json` 和 `SHA256SUMS`。它们与首次安装共享同一份运行材料，升级时仅写入非活动槽，不接触共享会话和工作区卷。

## 结果

RC13 构建、最终浏览器回归、资产校验及 GitHub prerelease 远端资产确认完成后，在此补记构建 ID、完整门禁 fingerprint、资产 SHA256、Release URL 和附件核验结果。
