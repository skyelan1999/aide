# AIDE 完整离线总包

当前唯一总包：`docker-images/aide-runtime-all-platforms.zip`，使用 Git LFS 存储；每次交付覆盖这个固定路径，不另外累计版本文件。总包同时包含 Linux ARM64 与 AMD64 两个完整离线包，含应用、原生 CAD、平台启动脚本和许可证。不是把全部历史镜像打包，也不包含用户会话、密码、工作区数据或 Docker 虚拟磁盘。

## 拉取与启动

先安装 Git 和 [Git LFS](https://git-lfs.com/)，在项目根目录执行：

```sh
git lfs install
git pull --ff-only
git lfs pull --include="docker-images/aide-runtime-all-platforms.zip"
```

普通 Git 拉取只有约百字节的 LFS 指针时，必须完成 `git lfs pull`，再解压 ZIP。GitHub 网页的源码 ZIP 不保证包含 LFS 实体，请用以上命令。

1. 校验 ZIP：macOS 用 `shasum -a 256 -c docker-images/aide-runtime-all-platforms.zip.sha256`；Windows 用 `Get-FileHash docker-images/aide-runtime-all-platforms.zip -Algorithm SHA256` 与旁边摘要比较。
2. 解压总包，启动 Docker Desktop。执行 `docker info --format '{{.OSType}}/{{.Architecture}}'`：`linux/aarch64` 或 `linux/arm64` 选 ARM64；`linux/x86_64` 或 `linux/amd64` 选 AMD64。以 Docker 引擎为准，不只看宿主机型号。
3. 按总包内 `SHA256SUMS` 校验所选 `*-offline.tar.gz`，解压该架构包，得到 `aide-offline/`。
4. macOS 双击其中的 `start.command`；Windows 双击 `start.bat`；Linux 执行 `bash start.sh`。沿用包内原始启动器导入本地镜像并启动，不需要现场编译，也无需分别拉取 CAD 镜像。Docker 本身仍需先安装。

后续更新总包时，在相同安装位置更新程序文件，保留 `.aide-data`、`.cache`、`.env`、workspace、context 等用户配置与数据；不要删除安装目录重建来代替升级。首次接收设备和 Windows 原生运行仍需设备方验收。

## 当前镜像身份

- 包版本：0.1.17.0-RC2。
- 镜像源码快照：`f316cf8d6f4ba4243a05007f6e8ebf6f586cbd4c`，共同源码 SHA-256 `2239861e3902cced4bdb9a772b489ee3a95340b7fb1bd70646c60ce3979d282e`。
- ARM64 镜像：`sha256:07172e63ff04662f4f91757cc15e253d138e4c192a62f1d81136c8f9059bdfd1`。
- AMD64 镜像：`sha256:9bf8eb9f240ef69356b529fed4da4b1dac5f4b0a54a3b8661253a78c8e3fe3aa`。

这是当前已有预编译包中的同源最新组合，不包含随后 main `e182e063` 的插件外观更新或其他后续源码变动；总包上传不等于重新编译最新 main。详细文件名、归档摘要见 `docker/runtime-bundle.json` 和总包内同名文件。Release 默认下载清单仍按既有公开附件门禁管理，本总包不伪造 GitHub Release 的上传状态。

## 重新制作

将同源、经验证的两种架构完整包放入 `docker-images/`，更新 `docker/runtime-bundle.json`，运行 `python3 scripts/build-runtime-bundle.py`。该脚本校验两份归档摘要、平台与共同源码，流式计算哈希，将已压缩完整包按原字节存入一个 ZIP，并生成外部摘要。随后提交固定总包、摘要与清单；`git push` 会通过 Git LFS 上传实体。Git LFS 保存历史对象，覆盖工作区文件不会改写已有 Git 历史；本次不强制重写远端历史或调整计费配置。
