# aide 镜像归档

运行 `bash scripts/aide.sh export` 会在这里生成：

- `aide-local.tar.gz`：可通过 Docker 导入的镜像归档。
- `aide-local.tar.gz.sha256`：归档校验和。

归档被 Git 和 Docker 构建上下文忽略，避免将大型二进制文件提交到源码库。

恢复：`bash scripts/aide.sh load`，然后 `docker compose up -d --no-build`。

镜像仅包含运行环境与应用，不包含挂载的项目文件、辅助目录、API 密钥和会话数据。会话与模型设置在 `aide_aide-data` 数据卷中，需要单独备份。Apple Silicon 构建的镜像为 linux/arm64；x86 主机建议从源码重新构建。
