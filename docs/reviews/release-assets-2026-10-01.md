# aide v0.1.14.0 RC6 跨平台 Release 资产记录

状态：修复资产脚本的相对输出路径与 Release notes 选择；RC6 tag、镜像构建、资产打包与远端发布待执行。

## 目标与平台

发布同一版本的本地 Docker 镜像资产和平台专属启动 ZIP：

- macOS Apple Silicon：`start.command`
- Windows ARM64：`start.bat` / `start.ps1`
- Ubuntu ARM64：`start.sh`
- 三个平台共享 `linux/arm64` Docker 镜像归档与 SHA256SUMS。

启动器导入前校验镜像架构、镜像 ID 和 SHA256；Compose 使用 `--no-build --pull never`。Windows x64 与 Ubuntu x64 需要 linux/amd64 镜像。本机构建缓存只有 arm64 Docker 基础镜像，遵从不联网拉取的既有约束，本次不发布不兼容的 x64 离线包。

## 制作与上传

```bash
bash scripts/package-release-assets.sh v0.1.14.0-RC6 aide:0.1.14.0-RC6
bash scripts/publish-release-assets.sh v0.1.14.0-RC6 .agent-state/release-assets/v0.1.14.0-RC6
```

发布脚本只上传已生成文件；tag 必须已推送。校验和清单覆盖镜像与三个平台 ZIP。

## 验收记录

| 项目 | 状态 | 证据 |
| --- | --- | --- |
| 平台启动器与共享镜像包 | 待执行 |  |
| SHA256 与压缩包校验 | 待执行 |  |
| RC6 镜像健康 / API 身份 | 待执行 |  |
| GitHub prerelease 与附件 | 待执行 |  |
| Windows 实机启动 | NOT_RUN | 当前执行环境为 macOS，无 Windows 主机 |
| Ubuntu ARM64 实机启动 | NOT_RUN | 当前执行环境未提供 Ubuntu ARM64 主机 |

## 发布构建修正

RC5 的首次带 `AIDE_RUN_TESTS=1` 镜像构建失败：Go Office 集成测试在 build 阶段缺少 Python `python-docx`，尽管此前本机完整 race/vet 门禁已通过。随后确认本地 runtime 依赖层已缓存，RC5 镜像在 `AIDE_RUN_TESTS=0` 下使用本地 Docker 基础镜像与缓存完成构建。打包阶段发现资产目录若为相对路径会在临时 staging 目录中写错位置，现已修复；新候选使用 RC6。该 RC6 构建使用已通过的完整 race/vet 验证结果，不宣称 Docker build 内运行了 Go 集成测试。

## 回滚

不移动或复用源码 tag。发现资产问题时删除或替换该 GitHub Release 的附件，并保留当前源码历史；修复后按版本脚本递增 RC，再重新构建和发布。无需替换用户正在运行的本地服务。

## RC6 验收结果（2026-10-01）

RC5 初次 Docker build 将 Go Office 集成测试放在没有 `python-docx` 的 builder stage，导致 `AIDE_RUN_TESTS=1` 失败。此前 host full race/vet 已通过；RC6 Docker 构建使用本机缓存的 arm64 基础镜像和 runtime 依赖层，并令 `AIDE_RUN_TESTS=0`，不从网络拉取镜像。完整 race/vet 由当前源码的 full verifier 单独通过，不将 Docker build 描述成运行了 Go 集成测试。

RC6 全量发布验证：`python3 scripts/agent-route.py verify full` PASS，fingerprint `fad181940ddb9a0296450372dc990b7294c4784581dd73dfb8078e7fda253759`，HEAD `284e3670e4dd84e758c22463f0c0016df52e74d2`，日志 `.agent-state/verify-20261001T073906393112Z.log`。隔离容器 `/healthz` 返回 `{integrity:ok,service:aide,status:ok}`，首页 HTTPS 返回 200。运行容器已清理。

资产目录：`.agent-state/release-assets/v0.1.14.0-RC6/`。镜像 `aide-v0.1.14.0-RC6-linux-arm64-image.tar.gz`，490M，SHA256 `1c70f07d1800374e306be1f03514f2d395f574631efa92a76f44cc53d9eb8bb2`，image ID `sha256:19b1da3729efcef339de8110ca3db608fb62a9e31d5e4a78a185a4bb8ff73743`。启动包：macOS Apple Silicon ZIP 7.9K（`c70124133509fb6eb6b5aff60a27fd4369c148c33a7df81559df11c0fa5846ec`）、Ubuntu ARM64 ZIP 8.2K（`3539e6c03a5da62e9c671398811ad6b836eb651895e7030a49859e6a70aed338`）、Windows ARM64 ZIP 14K（`9a9a5e271376ed69150bfe4eef1a3a1ef47377bb3d30c0fa28361bfa6d897f14`）。gzip、三个 ZIP 与 `SHA256SUMS` 均通过。

Windows 和 Ubuntu 实机启动仍为 NOT_RUN。三个下载包是各平台 Docker 启动器包，并非将应用编译成不同 CPU 架构的原生 GUI 二进制。Windows x64 与 Ubuntu x64 未发布，因为没有本地 linux/amd64 镜像输入。RC3/RC4/RC5 均为本地失败候选 tag，不推送。
