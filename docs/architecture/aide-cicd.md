# Aide 本体 CI/CD

构建产物只包含 Aide 主仓库。公开插件以既有发布包单独下载安装验收，不进入运行包/SDK；不收集用户工作区、用户安装的插件、商业源码或私有安装包。仓库内 `plugins/testdata` 仅用于主程序协议回归。流水线不访问生产 Docker、真实会话、密钥或模型 API。

## 自动检查与候选包

- `Aide CI`：所有分支 push、main PR 和手动运行，只做 quick/CI 合约、所有包编译、关键 Go 回归和 vet。`gate` 汇总所有基本检查，失败/取消不通过；日常不触发完整镜像、race 或发布。
- `Aide Delivery`：显式 workflow_dispatch，完整矩阵见[Release 验收策略](../testing/release-validation.md)。main 可请求准确 tag 公开发行；包含最新 main 的功能分支只允许空 tag 的只读候选，不能发布。
- 原生 Ubuntu ARM64/AMD64 各自构建运行包和独立 SDK。`AIDE_RUN_TESTS=1`、24 个完整 race 分片、其他包和 vet、候选资源、可选引擎边界、镜像层隐私扫描、ZIP CRC、导入/鉴权/健康全部必需。云端两 worker 8 GiB/4 CPU 上限，本地默认单 worker 4 GiB/2 CPU。Go 总时限 75 分钟，原生候选作业预算 240 分钟。
- 公开市场固定提交的所有 public ZIP 不重新编译，逐一校验、实际安装到临时宿主并运行功能断言；两架构结果都必需。市场变化需重新冻结与验收。原生宿主、48 格浏览器和新装生命周期是独立必需 job，具体收据缺项仍拒绝发布。
- 候选产物和执行证据保留 30 天。候选不会创建 tag 或修改更新默认源；插件包仅用于独立验收，不打入运行包/SDK。原始日志与截图保留为 Actions artifacts，发布附件只带脱敏摘要。

## 手动发布

1. 在干净 main 按现有发布流程完成任务验收与 `release-check`，使用 `scripts/version.sh` 的既有版本机制更新版本并推送提交、标签。现有已发布标签不可覆盖。
2. 在 Actions 的 `Aide Delivery` 选择 **Run workflow**，分支 main；`release_tag` 填该已有标签。留空仅生成候选。标签必须等于 version.sh 当前输出，并指向本次 main 提交。
3. 流水线重新验证源码、两种原生架构、所有 public 包安装与功能、Windows/macOS 宿主、三引擎浏览器和干净 Linux 生命周期，然后进入 `aide-release` 环境。仓库管理员可为该环境配置审核人；环境未配置审核规则时，手动触发即执行发布，代码没有虚构人工审批。
4. 发布任务只下载本次运行的候选，检查两份 proof 的共同源码身份、完整验证计划、无插件标志、文件大小和 SHA256。创建全新草稿后上传双架构运行包与 SDK 共四个 ZIP、SHA256SUMS、RELEASE-ASSETS.txt 和 RELEASE-VALIDATION.json；再次核对 GitHub 独立报告的摘要及标签目标，最后公开为预发布。
5. 任一步失败都不会把草稿公开。上传失败可保留草稿供检查；重跑不会覆盖既有草稿或 Release，排查后由维护者处理草稿再重试。不要对已有公开版本重跑发布。

发布不自动部署本地服务，不自动更新 `docker/prebuilt-images.tsv` 的默认下载版本，不包含 macOS / Windows Docker Desktop 真机启动验收，公开插件的兼容范围以逐包功能报告为准，不代表私有插件、物理硬件或真实付费服务全部验收。后续正式版本若调整预发布规则，应单独评审。

运行时异常定位和故障注入说明见[模块诊断](diagnostics.md)，它与 Release 验收证据相互补充。

## 权限与维护

PR 流水线仅有 contents:read，不使用 pull_request_target、不读取仓库 secrets，checkout 不保留推送凭证。写权限只给手动发布任务；候选构建不持有发布 token。所有文件名、标签和源码身份先校验后用于发布。完整构建按分支串行，公开发布全局串行，同一 PR 的过时 CI 会取消。

现有 RC3 专用工作流已改为明确拒绝发布的历史入口，其固定提交脚本只作历史资料，新版本使用上述通用流程。`pages.yml` 独立维护主页。

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
