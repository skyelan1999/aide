# 版本号管理与自动流程托管 · 系统设计（增量）

> **历史设计（2026-09-21）**：版本脚本仍复用；运行版本已改由构建 ldflags 注入，不再读取工作区 version.md。 当前状态见 [现行 PRD](../PRD.md) 与 [架构](../architecture.md)。下文“待实施/已验证”仅代表原设计时间点，不能用于今天的发布判断。

> **现状核对（2026-09-25，当前版本 `0.1.10.2 RC1`）**：`scripts/version.sh` 的 show/bump/patch/note/tag/check/install-hooks、四位号 `X.Y.Z.W RCn`、仅 main 分支打 tag、tag 名 `vX.Y.Z.W-RCn` 均与本文一致。偏差：
> - **运行时版本来源已改为构建期 ldflags 注入**（`Dockerfile` 用 `-X 'aide/internal/server.buildVersion=…' -X 'aide/internal/server.buildCommit=…'`）；生产构建以注入值为准，**不再读工作区 version.md**。仅当开发构建未注入 `buildVersion` 时，`New()` 才回退读工程目录 `version.md`（`server.go:365-370`，R09）。`/api/config` 返回 `version`/`buildVersion`/`buildCommit`/`revision`。
> - `version.md` 在仓库根（非 `docs/` 下）；历史镜像存于 `docker-images/`，命名 `aide-<version>-linux-arm64.tar.gz`（附 `.sha256`）。

| 项 | 值 |
| --- | --- |
| 文档类型 | 系统设计（System Design） |
| 日期 | 2026-09-21 |
| 上游依据 | `docs/prd/2026-09-21-version-management.md`（增量 PRD）+ `docs/PRD.md` v1.4 |
| 对应分支 | `feat/version-management` |
| 功能基线 | `672969e` |
| 作者 | 编码助手 |
| 状态 | 已落地；运行时身份改 ldflags 注入 |

## 1. 架构总览

```text
① 事实源    工程目录根 version.md
    # aide 版本记录
    **当前版本：0.1.0.0 RC1**
    ## 0.1.0.0 RC1（2026-09-21）
    - release note 条目…

② 自动化层  scripts/version.sh（bash，宿主机/容器内均可运行）
    show           显示当前版本（正则取 version.md 首个 X.Y.Z.W RCn）
    bump <档位>    产品/重大/大版本/日常 → 重算版本 → 改写当前版本行 → 插入新一节
                   release note → git add version.md → git commit（消息带版本号）
                   → git tag -a vX.Y.Z.W-RCn（annotated，消息 = release note）
    patch          同一版本补丁：RC+1，其余同上
    note           在当前版本节下追加 release note 条目并提交（仅更新文件与提交，不打 tag）
    tag            为当前版本在 HEAD 打 tag（幂等：已存在且指向同一提交则跳过）
    check          校验 version.md 结构（当前版本行 / 正则 / 至少一节）
    install-hooks  把 scripts/git-hooks/prepare-commit-msg 安装到 .git/hooks/
    —— bump/patch/note/tag 仅允许在 main 分支执行；非 main 直接报错退出

    git 钩子（prepare-commit-msg，每次提交自动执行）
    读取 version.md 当前版本 → 提交信息末尾追加一行 `[版本 X.Y.Z.W RCn]`
    （已含相同标注则不重复追加；version.md 缺失时静默跳过）

③ 展示层    Go 后端：版本身份由构建 ldflags 注入（buildVersion/buildCommit，见 Dockerfile）；
            生产构建以注入值为准，仅开发构建未注入时回退读工程目录 version.md（正则解析，失败为空→界面 dev）
    GET /api/config 返回 "version"/"buildVersion"/"buildCommit"
    前端：refreshConfig 后 → 侧栏运行卡片 #app-version、设置面板底部 #settings-sheet-version

④ 工作流   功能分支：普通提交由 hook 自动标注版本号（不打 tag）
    合并回 main 后：version.sh bump <档位> -m "note" → 新版本 + release note + 提交 + tag
    补丁修复：version.sh patch -m "note" → RC+1 + 提交 + tag（仅 main）
    校验：version.sh check；查看 tag：git tag -l
```

## 2. 版本重算规则（LIM-24）

| 命令 | 计算 | 例（当前 0.1.2.3 RC1） |
| --- | --- | --- |
| `bump product` | 第 1 位 +1，其余归零，RC1 | 1.0.0.0 RC1 |
| `bump major` | 第 2 位 +1，低 2 位归零，RC1 | 0.2.0.0 RC1 |
| `bump feature` | 第 3 位 +1，第 4 位归零，RC1 | 0.1.3.0 RC1 |
| `bump daily` | 第 4 位 +1，RC1 | 0.1.2.4 RC1 |
| `patch` | 仅 RC+1 | 0.1.2.3 RC2 |

## 3. 文件变更清单

| 文件 | 变更 |
| --- | --- |
| `version.md` | 新建：初始 `0.1.0.0 RC1` + 版本管理机制 release note |
| `scripts/version.sh` | 新建：show / bump / patch / note / tag / check / install-hooks（纯 bash，兼容 macOS bash 3.2；bump/patch 在 main 自动打 annotated tag `vX.Y.Z.W-RCn`） |
| `scripts/git-hooks/prepare-commit-msg` | 新建：提交信息自动标注当前版本（去重、缺失跳过） |
| `internal/server/server.go` | App 增 version 字段；New() 读工程目录 version.md；config 返回 version |
| `internal/server/server_test.go` | 新增版本解析/接口测试 |
| `internal/server/web/index.html` | 侧栏运行卡片增 `#app-version`；设置面板底部增 `#settings-sheet-version` |
| `internal/server/web/app.js` | refreshConfig 后写入两处版本显示 |
| `internal/server/web/style.css` | `.app-version` 等展示样式（沿用令牌） |
| `docs/PRD.md` + 增量文档 | v1.4：FR-65/66、LIM-24 |
| `README.md` | 「版本管理」小节：version.sh 用法与钩子安装 |

## 4. 约束与边界

- 版本号解析统一用正则 `[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+ RC[0-9]+`（Go / bash 各一份，语义一致）。
- git tag 命名 `vX.Y.Z.W-RCn`（annotated，消息为 release note）；**仅 main 分支打 tag**，`bump/patch/note/tag` 先校验当前分支。
- 钩子只在 `version.md` 存在时生效；缺失时静默跳过，不阻断提交。
- `version.sh` 的 git 操作全部 `-C "$ROOT"` 定位仓库根，与调用目录无关。
- bump/patch 提交只含 `version.md` 一个文件（代码提交独立进行，均带版本标注）。
- 后端版本读取失败不阻塞启动（返回空 → 界面 dev）。
- 不改 CSP、不新增颜色字面量。
