# 启动器七接口：干净环境验收（#654）

本记录对应 [issue #654](https://github.com/skyelan1999/aide/issues/654) 和 [PR #650](https://github.com/skyelan1999/aide/pull/650)。验收跨越北京时间 2026-10-30 晚至 10-31 凌晨。范围是七个标准启动器接口；Windows 完整实机测试由用户明确安排到后续，实际发行未执行。

## 结论与源码身份

macOS `.command` 与 Linux `.sh` 的完整真实 Docker 流程均 **PASS**。最终两组都从头连续执行，无断点恢复：每组 17 次入口调用、两次冷构建，均验证 HTTPS 健康、HTML、带令牌只读 API、健康实例重开、停止、持久数据保留和专属资源清理。

- main 基线：`7a1e4b6766fe5f32cdaf5c14204738679218d9fc`。
- 验收候选：`685c5e1ee0d00d52cfd5cf39795074a7695ad64b`。
- Linux CI 使用 GitHub 合成 merge ref `4991e28c4932e50dbc2577a44410b886d7c41c85`；已实际 fetch 并确认其文件树与候选相同。
- 运行镜像源码输入指纹：`ff4ecee16eec407b4ec4c87dfa8e4065e670782767a538d724dd21c429901fd5`。收尾仅更新验收文档及配置注释，不改变可执行逻辑或配置值。

Linux 原始证据：[成功的 launcher job](https://github.com/skyelan1999/aide/actions/runs/41088223840/job/124175647603)、[机器结果与逐项日志附件](https://github.com/skyelan1999/aide/actions/runs/41088223840/artifacts/12993796297)。本机最终记录为忽略目录中的 `launcher-clean-654/mac-real-c/result.json`，汇总保留在 `.agent-state/launcher-clean-654/final-posix-summary.json`。

## 干净环境的实际边界

使用独立、已提交的全新 Git worktree，路径包含中文和空格；从项目目录之外启动真实入口文件。每组均为新 HOME、新 Docker 客户端配置、新工作区/资料/本地目录、新应用数据、新项目名/端口/镜像和新 Buildx 缓存。开始前实际确认本安装镜像和构建器不存在，不继承 `PROJECT_DIR`、模型密钥或主机代理变量。

macOS 保留仓库默认依赖源；Linux CI 显式使用已有的 `PIP_INDEX_URL=https://pypi.org/simple` 配置。Docker 引擎和已安装 CLI 是测试前提，共享引擎没有被重置或全局 prune；没有复制原安装的 `.env`、`.aide-data`、工作区或会话。最小 PATH 中的可选 Node/浏览器桥接不作为插件实机验收。

## 七接口结果

| 接口 | macOS 真实入口 | Linux 真实入口 | 实际检查 |
| --- | --- | --- | --- |
| start | PASS | PASS | 新安装缺镜像时退出 1 并提示 build；构建后重开健康实例退出 0，容器/镜像身份保持；停止后可再启动；docker_clean 后再次正确拒绝。 |
| stop | PASS | PASS | 实际停止当前安装，运行实例消失；重复 stop 正常；本机结束后无本安装容器或 helper PID 文件。 |
| build | PASS | PASS | 新 builder、新缓存下从当前源码实际编译、校验并自动启动；退出 0。 |
| clean | PASS | PASS | 删除本安装构建环境后实际冷重建并启动；退出 0；深层应用数据及工作区/资料/本地目录哨兵保留。 |
| docker_clean | PASS | PASS | 实际移除本安装容器、镜像和 builder，保留应用数据、配置及外部目录哨兵；既有无关镜像仍存在。 |
| verify | PASS | PASS | 运行时退出 0；新安装、停止后、清理后退出 1 并报告未运行，未自行构建或启动。 |
| release | 门禁 PASS，发行 NOT_RUN | 门禁 PASS，发行 NOT_RUN | 在真实验收 checkout 中执行并拒绝非 main 发布；另在完整 CI 模块的独立 Git 夹具中验证合法 main/tag/说明的本地 dry-run，以及脏工作区拒绝。未调用 GitHub 发行、未创建真实项目 tag。 |

最终 macOS 冷 build 为 **284.75 秒**，clean 冷重建为 **203.99 秒**；Linux 对应为 **301.02 秒**、**309.27 秒**。两组各取得四次真实宿主健康样本：`/healthz`、应用 HTML 和带 Bearer 的 `/api/sessions` 均成功；使用安装证书进行宿主 HTTPS 请求，没有关闭证书验证。此项是服务/API 验收，不冒充浏览器渲染或模型调用验收。

## 发现、修复与失败记录

1. 用户截图的空路径错误已在无 `PROJECT_DIR` 环境只读复现。Python 协调器原先开启子 shell，却没有传入项目路径；现通过独立引号参数传递。资源检查也支持显式路径/当前目录回退，保留低磁盘和异常报告的拒绝逻辑。
2. 旧协调器单测模拟整个 `call`，跳过了真实磁盘子 shell。新增回归只隔离 Docker 边界，实际执行生产 Bash guard；覆盖未设置、空值、错误继承值以及中文空格路径。Linux 新增必需 `launcher` CI lane，实际跑完整生命周期。
3. 第一轮真正冷构建在阿里云 wheel 数据传输中超时，未记为通过。现对在线 pip 完整安装增加最多三次重试，保留原索引、版本和参数；离线模式不转联网。重试恢复、成功不重复、失败次数/退出码均有执行回归。首次失败安装的专用 builder 通过其 docker_clean 移除。
4. 第二轮构建本身已成功，但早期验收器的 Python 3.9 CA 校验不接受该本地叶证书。改用与产品一致的显式证书 curl 检查，曾续跑并核对产品输入未变；该中间结果不替代最终记录。最终 macOS 第三轮使用修正后的验收器，从全新环境连续跑完全部 17 次调用。

补充回归：资源检查 9 项、启动器/包组装 21 项、pip 恢复 3 项、CI 合约 22 项、可移植原生代码 66 项均通过；候选 `685c5e1e` 的 quick、完整 Go race/vet、Windows、Linux launcher 和汇总 gate 全部通过。[Windows job](https://github.com/skyelan1999/aide/actions/runs/41088223840/job/124175647353) 使用真实 Windows SDK/CMD 及可控注册表/Docker 边界，不等于 Windows Docker Desktop 完整实机构建。

## 本机修复与交接

已将通过验证的 POSIX/构建修复同步到用户原源码目录，并重新执行未导出 `PROJECT_DIR` 的真实磁盘检查及原目录既有资源回归。原先的 `start.command` 修改、其他任务文档、运行配置和数据均保留；没有从原数据目录实际构建部署。覆盖文件与前后摘要记录在原目录忽略的 `.agent-state/launcher-clean-654/local-repair.json`，便于后续合并时仅处理本次修复。

本机测试容器、专用镜像、builder 和 helper PID 均已清理，证据及测试数据保留。Windows 实机按用户“暂时没有，后面测”延期，#649 保持打开。#654 在 main 合入前保留。完整 Go/gate、当前 PR 提交及必需审阅以 PR 页面为准；本记录不替代合并门禁，也没有执行 tag、发行或生产部署。


## 后续复现：终端长时间无输出

用户再次报告原源码 build.command 无输出。实时进程树确认其正在 source-sha 阶段逐文件读取源码，shasum 打开的文件持续变化；稍后原进程自行进入 Docker build。该目录位于 iCloud，而此前新 checkout 没有复现相同读取耗时，不能据此归因于 Docker 卡死。之前的完整生命周期测试遗漏了终端等待反馈。

整改：Docker/磁盘检查、源码指纹、构建配置和 Docker 编译各有即时刷新提示。源码读取阶段每 10 秒输出等待时长及云盘文件下载提示；摘要仍仅从 stdout 取得，进度写 stderr。源码读取限时 30 分钟，超时或中断会清理独立进程组中的 shell/xargs/shasum 后代，避免仅杀 shell 后留下后台读取。

追加验证：23 项启动器回归通过，其中真实慢子进程验证等待输出不污染摘要，真实 shell/sleep 后代验证超时清理；原路径 PTY 实际输出即时阶段提示、1.4 秒完成已读过的源码指纹并进入 Docker 编译。发现用户原构建已运行后，主动取消新开的验证构建，仅保留用户原构建。未操作全局 Docker 配置或删除用户数据。

终端 App 的原生窗口操作被工具安全策略拒绝，此项可视验收记为 NOT_RUN，PTY 输出验收与原生窗口验收分开记录。本节修复会改变运行输入指纹，前述两平台冷构建收据仍只对应其所列候选，不能当作本节新提交的完整冷构建凭据。
