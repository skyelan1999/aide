# aide v0.1.14.0 RC3 跨平台 Release 资产记录

状态：实现与打包脚本已准备；构建、上传与远端资产核验待执行。

## 目标与平台

发布同一版本的本地 Docker 镜像资产和平台专属启动 ZIP：

- macOS Apple Silicon：`start.command`
- Windows ARM64：`start.bat` / `start.ps1`
- Ubuntu ARM64：`start.sh`
- 三个平台共享 `linux/arm64` Docker 镜像归档与 SHA256SUMS。

启动器导入前校验镜像架构、镜像 ID 和 SHA256；Compose 使用 `--no-build --pull never`。Windows x64 与 Ubuntu x64 需要 linux/amd64 镜像。本机构建缓存只有 arm64 Docker 基础镜像，遵从不联网拉取的既有约束，本次不发布不兼容的 x64 离线包。

## 制作与上传

```bash
bash scripts/package-release-assets.sh v0.1.14.0-RC3 aide:0.1.14.0-RC3
bash scripts/publish-release-assets.sh v0.1.14.0-RC3 .agent-state/release-assets/v0.1.14.0-RC3
```

发布脚本只上传已生成文件；tag 必须已推送。校验和清单覆盖镜像与三个平台 ZIP。

## 验收记录

| 项目 | 状态 | 证据 |
| --- | --- | --- |
| 平台启动器与共享镜像包 | 待执行 |  |
| SHA256 与压缩包校验 | 待执行 |  |
| RC3 镜像健康 / API 身份 | 待执行 |  |
| GitHub prerelease 与附件 | 待执行 |  |
| Windows 实机启动 | NOT_RUN | 当前执行环境为 macOS，无 Windows 主机 |
| Ubuntu ARM64 实机启动 | NOT_RUN | 当前执行环境未提供 Ubuntu ARM64 主机 |

## 回滚

不移动或复用源码 tag。发现资产问题时删除或替换该 GitHub Release 的附件，并保留当前源码历史；修复后按版本脚本递增 RC，再重新构建和发布。无需替换用户正在运行的本地服务。
