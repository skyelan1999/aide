# Aide 本体 CI/CD

仅构建 Aide 主仓库。不会收集工作区、安装的插件、商业插件源码或插件安装包；仓库内 `plugins/testdata` 仅用于主程序协议回归。流水线不访问生产 Docker、真实会话、密钥或模型 API。

## 自动检查与候选包

- `Aide CI`：main 的 PR、main 推送以及手动运行，执行现有 quick 门禁、CI 交付合同测试、完整 Go race/vet，以及 Windows PowerShell 5.1 / PowerShell 7 启动脚本隔离回归。`gate` 是汇总检查；任一依赖失败或取消均不通过。
- `Aide Delivery`：仅在同仓库 main 推送对应的 CI 成功后构建；也可在 main 手动构建。它只采用 main 的提交，不采用 PR 的产物或脚本。
- ARM64 使用原生 `ubuntu-24.04-arm`，AMD64 使用原生 `ubuntu-24.04`。沿用 Dockerfile、version.sh 和 package-release-assets.sh，构建时强制 `AIDE_RUN_TESTS=1`。
- 云端完整 Go 测试保留全部 24 个互斥分片，使用两个进程、8 GiB / 4 CPU 上限；本地默认仍为单进程、4 GiB / 2 CPU。Office 脚本明确绑定当前源码快照，不依赖测试镜像中的旧副本。完整 Go 快照总时限为 75 分钟（宿主额外保留 50 秒退出清理），CI 作业预算 90 分钟；原生候选作业预算 180 分钟，涵盖构建、完整检查及打包。
- 每个候选镜像再运行 quick、候选镜像资源检查、完整 Go race/vet、CAD 检查和隔离 HTTPS 冒烟；不把旧镜像的验证收据用于候选。检查全部镜像层、ZIP CRC、镜像身份及摘要，拒绝插件载荷。
- Actions 仅保存两份完整 ZIP 和各自 proof，保留三天，不上传插件市场。候选包沿用源码内版本名称，下载时须同时看 proof 的提交 SHA；候选构建不会创建标签、修改 Release 或更改默认更新源。

## 手动发布

1. 在干净 main 按现有发布流程完成任务验收与 `release-check`，使用 `scripts/version.sh` 的既有版本机制更新版本并推送提交、标签。现有已发布标签不可覆盖。
2. 在 Actions 的 `Aide Delivery` 选择 **Run workflow**，分支 main；`release_tag` 填该已有标签。留空仅生成候选。标签必须等于 version.sh 当前输出，并指向本次 main 提交。
3. 流水线重新验证源码、两种原生架构和 Windows 启动回归，然后进入 `aide-release` 环境。仓库管理员可为该环境配置审核人；环境未配置审核规则时，手动触发即执行发布，代码没有虚构人工审批。
4. 发布任务只下载本次运行的候选，检查两份 proof 的共同源码身份、完整验证计划、无插件标志、文件大小和 SHA256。创建全新草稿后上传两个完整包、SHA256SUMS 和 RELEASE-ASSETS.txt；再次核对 GitHub 独立报告的摘要及标签目标，最后公开为预发布。
5. 任一步失败都不会把草稿公开。上传失败可保留草稿供检查；重跑不会覆盖既有草稿或 Release，排查后由维护者处理草稿再重试。不要对已有公开版本重跑发布。

发布不自动部署本地服务，不自动更新 `docker/prebuilt-images.tsv` 的默认下载版本，不包含 macOS / Windows Docker Desktop 真机启动验收，也不代表插件兼容验收。后续正式版本若调整预发布规则，应单独评审。

## 权限与维护

PR 流水线仅有 contents:read，不使用 pull_request_target、不读取仓库 secrets，checkout 不保留推送凭证。写权限只给手动发布任务；候选构建不持有发布 token。所有文件名、标签和源码身份先校验后用于发布。并发构建按提交串行，同一 PR 的过时 CI 会取消。

现有 RC3 专用工作流与其固定提交脚本作为历史流程保留，新版本使用上述通用流程。`pages.yml` 独立维护主页。

本地检查：

```sh
python3 -m unittest discover -s scripts/ci -p 'test_*.py' -v
python3 scripts/agent-route.py verify quick
python3 scripts/check_docs.py
```

原生构建入口（只在相应架构的 Linux Docker 环境执行）：

```sh
python3 scripts/ci/build_candidate.py --arch amd64 --out .agent-state/ci-candidate/amd64
```

本地语法/合同测试通过不等于托管构建通过。以对应 Actions 运行、候选 proof 和 Release 附件实际状态分别判断。
