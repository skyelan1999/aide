# 2026-10-14 SSH Git 比较与 Docker 更新

用户要求 SSH 工作区支持 Git 比较，并提醒每批源码推送同步更新 Docker 镜像，避免另一端重拉依赖、构建等待。源码附件和历史文档只作为背景，交付按当前请求执行。

## 功能与边界

`d8d21b5` 已推送 `feat/ssh-git-history`。复用已有文本文件历史窗口，从 SSH 主工作区远端实际仓库读取 HEAD、完整提交编号、index、worktree 和文件提交历史。编辑器未保存正文继续在本地参与比较。缺 Git／无仓库显示原因，非法引用、权限、超限、二进制和连接切换报错，不使用本机缓存代替。独立 SFTP 来源未扩展。

远端执行采用固定 POSIX shell 脚本、逐项单引号转义及清洁环境；禁用外部 Git diff、fsmonitor、hooks、lazy fetch、replace objects和网络协议。每条 SSH 命令固定连接身份并前后校验代次；远端路径逐组件拒绝符号链接，但检查与打开不原子，不等价于本地内核路径隔离。每侧2MiB、历史50条、总截止12秒。只读 Git 对象和正文在本机隔离快照生成补丁。

## 实际测试

- 规范仓 `go test -race -mod=vendor ./internal/server -run '^TestFileGitHistory' -count=1 -timeout=3m`：9.036s，PASS。
- 运行仓同命令：9.873s，PASS；本地比较兼容保持。
- 模拟传输执行真实远端脚本：本机缓存与远端树内容不同；检查 HEAD/index/worktree/历史、字面中文/引号/`$()`/通配文件名、未跟踪、删除、无 HEAD、符号链接、越界、非法 ref、超限、二进制、取消与连接代次失效。确认模拟远端 index和磁盘正文不变，配置的危险外部程序未执行。
- 真实 SSH：现有 `scripts/test-ssh-workspace.sh` 启动两个一次性 SSH 容器及内部网络，不发布端口；本轮筛选 `TestSSHLiveGitHistory`、race、count1，2.377s，PASS。经真实 OpenSSH完成 HEAD→index、index→worktree、提交列表和非Markdown入口探测。未连接用户真实SSH服务器。
- 两仓 quick 通过；最终文档、归档校验清单更新后再次校验。生产Git窗口代码未改，继续复用既有窗口及其回归；本轮未进行正式8097浏览器完整SSH操作。
- 初始模拟夹具暴露旧双引号辅助函数会展开脚本变量，改为已有单引号转义后重跑通过。首次真实SSH运行在修正代码时中止并清理，未计通过；格式化稳定后独立复跑通过。

日志：规范仓 `.agent-state/ssh-git-history/go-verified.log`、`live-verified.log`；运行仓 `go-runtime.log`、`build-labeled.log`、`smoke.json`。日志与备份不推送。

## 本机8097更新

实际运行仓为 `aideMacMini/aide`，仅应用此次 Git API 局部差异和新增文件，保留旧版来源/认证及其他任务改动；镜像来自兼容树，不等同于规范仓纯净提交。

- 停止前再次检查普通活动任务0，数据备份 14306字节／39项，SHA-256 `ec96c27162f92740196636714fe21f5e6702a91968158544f669ec646fd3518a`。位于运行仓 `.agent-state/ssh-git-history/data-before-update.tar.gz`。
- 旧镜像 `sha256:06d8457e2e1384fda4fd805ff9d66a24466e5fbda62408821b16713fcf71564e`，标签 `aide:ssh-git-previous-20261014`。
- 当前镜像 `sha256:f642732bec958ba41e22e295f20551f4801e22afb3481cf2d9b9401c3a1c8037`，source label `b036b0b734883d91a9024f8ffd2f7ec86e49ab2cfabfb9a4926adf7bb6dfcefe`，构建身份 `d8d21b5-compat`。
- 仅替换 AIDE；数据卷、绑定目录、sandboxMode workspace-write、辅助审批关闭/revision0保持；Docmost/数据库/Redis和既有隔离AIDE服务未重启。
- healthz、鉴权config/sessions/approval-policy均200，本机工作区Git探测200且available=true；Git窗口脚本字节与源码一致；健康且重启计数0。不能把本机仓库探测当作用户远端SSH验收。
- 本轮本机镜像第一次构建标签因旧脚本未提供 `source-sha` 子命令而为unknown；随后提取同一已审阅 `source_sha` 函数重新计算并以显式build-arg重建，验证label匹配后才更新/导出。未将第一次候选当作最终镜像。

回切：在运行仓执行 `AIDE_IMAGE=aide:ssh-git-previous-20261014 docker compose up -d --no-deps aide`；不自动覆盖数据卷。

## Docker交付

离线启动包：`docker-images/aide-0.1.17.0-RC2-linux-arm64-f642732bec95-offline.tar.gz`，561590374字节（约561.6MB），SHA-256 `1b89cfb627a45664788383e565c3d514f84cd06b86815beb0b2996f0643a9de8`，Linux ARM64。规范仓和运行仓各保留同一归档；`SHA256SUMS` 已追加新条目并保留原条目。

使用现有 `export-bundle`，包内包含镜像、启动器、离线compose、BUILD身份和内外校验。另一端解压后双击 `start.command` 或运行 `bash scripts/aide.sh start-bundle`；首次只加载包内镜像，后续按镜像ID复用，不拉基础镜像、安装依赖或编译源码。本轮重建复用了稳定依赖层，未重装pip依赖。ARM64包不能用于AMD64；未宣称全架构已交付。

Git push 不携带被忽略的561MB归档；归档在iCloud工程目录，云端传输完成和另一设备下载速度未验证。当前没有配置镜像registry发布工作流或用户提供的目标仓库，所以未执行Docker registry push。未合并main、未创建GitHub Release或修改发行版本；包名沿用仓库版本及镜像身份，不代表正式发版。


包验收补充：规范仓归档重新计算SHA-256与清单一致；包内镜像tar校验通过，实际 `docker image load` 成功，导入的 `aide:offline-f642732bec95` 与声明镜像ID一致。只在本机既有Docker引擎验证导入，没有以全新引擎或另一设备验收，不代表跨架构可用。
