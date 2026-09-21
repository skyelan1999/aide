# 版本号管理与自动流程托管 · 增量 PRD

| 项 | 值 |
| --- | --- |
| 文档类型 | 增量 PRD（Incremental PRD） |
| 日期 | 2026-09-21 |
| 上游依据 | `doc/PRD.md` v1.4 |
| 对应分支 | `feat/version-management` |
| 功能基线 | `672969e`（文档同步后 main HEAD） |
| 作者 | 编码助手 |
| 状态 | 待实施 |

## 1. 需求来源（用户原话）

> 从后面开始，每个版本要添加版本号，显示当前版本，每次提交内容增加版本号，版本号一共有4位，第一位为产品级更新，第二为重大版本更新（核心方案或技术路径变更等），第三位为大版本更新（新增了比较复杂或比较有新意的创意亮点等），第四位为日常更新，RC默认为RC1，如果有针对这个版本的补丁修改就是RC2，就相当于在加一个数字，最终呈现结果为0.0.0.1 RC1，版本在目录中通过version.md的方式呈现，并写release note，将其添加到项目工作流中并做自动流程托管。

## 2. 需求分解

| 编号 | 需求 | 验收标准 | 状态 |
| --- | --- | --- | --- |
| FR-65 | 版本号与 Release Note 管理 | 四位版本号（产品级·重大·大版本·日常）+ RC 补丁号，呈现 `X.Y.Z.W RCn`；工程目录 `version.md` 承载当前版本与 release note；`scripts/version.sh` 提供 show/bump/patch/note/tag/check/install-hooks；**版本升级以 git tag（`vX.Y.Z.W-RCn`）打在对应提交上，且仅在 main 分支打 tag**（bump/patch/note/tag 在非 main 分支拒绝执行）；git 钩子（prepare-commit-msg）自动为每次提交标注当前版本号（含功能分支） | 未实现 |
| FR-66 | 版本展示 | `/api/config` 返回 version（读工程目录 `version.md`，缺失返回空 → 界面显示 dev）；侧栏运行卡片与设置面板底部显示当前版本 | 未实现 |

## 3. 关键决策

| 决策 | 结论 | 理由 |
| --- | --- | --- |
| D1 位数语义 | `产品级.重大.大版本.日常`；bump 高位 → 低位清零；RC 重置 RC1 | 用户明确定义四档语义与低位清零惯例 |
| D2 RC 语义 | 每个版本默认 RC1；对**同一版本**的补丁修改 → RC+1（patch），不产生新版本号 | 用户原话「就相当于在加一个数字」 |
| D3 载体 | `version.md` 放工程目录根：标题 + `**当前版本：…**` 行 + 每版一节 `## 版本（日期）` + release note 列表 | 用户指定「版本在目录中通过 version.md 的方式呈现」 |
| D4 提交标注 | 每次提交**自动**带版本号：`prepare-commit-msg` 钩子在提交信息末尾追加 `[版本 X.Y.Z.W RCn]`；bump/patch/note 子命令的提交消息由脚本直接写入版本号 | 满足「每次提交内容增加版本号」+「自动流程托管」；钩子重复追加有去重保护 |
| D5 版本升级时机 | 版本升级由 `version.sh bump/patch` 显式执行（release note 与版本号同一次提交）；普通提交只标注、不自动升级 | 避免每次提交产生新版本号噪声；升级点由人按四档语义判断 |
| D9 git tag 规则 | 版本 tag 命名 `vX.Y.Z.W-RCn`（空格改连字符以保证 tag 合法），annotated tag 指向该版本的提交；**仅在 main 分支打 tag**：`bump/patch/note/tag` 先校验当前分支，非 main 直接拒绝（错误信息提示先合并回 main）；功能分支提交只做消息标注 | 用户澄清：版本以 tag 形式打在 git commit 上，且仅标记主分支 |
| D6 钩子托管 | 钩子模板存 `scripts/git-hooks/prepare-commit-msg`（随仓库分发），`version.sh install-hooks` 安装到 `.git/hooks/`；新克隆执行一次即可 | `.git/hooks` 不随仓库分发，模板 + 一键安装是标准做法 |
| D7 版本读取 | 后端启动时读工程目录 `version.md`，正则取第一个 `X.Y.Z.W RCn`；前端经 `/api/config` 获取 | 与 profiles.json 同一读取模式；文件缺失降级 dev，不阻塞启动 |
| D8 初始版本 | `0.1.0.0 RC1`：产品级 0、重大 1（核心方案已成型）、大版本 0、日常 0、RC1 | 「从后面开始」——此前功能不回溯编号 |

## 4. 版本号语义（LIM-24）

| 位 | 名称 | 触发示例 |
| --- | --- | --- |
| 1 | 产品级更新 | 定位变更、商业发布、1.0 里程碑 |
| 2 | 重大版本更新 | 核心方案 / 技术路径变更（如换架构、换存储） |
| 3 | 大版本更新 | 复杂或有创意亮点的新功能（如主题系统、Profile 路由） |
| 4 | 日常更新 | 常规改动、小功能、修复、文档 |

- 呈现格式：`X.Y.Z.W RCn`（中间有空格）；正则 `^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+ RC[0-9]+$`。
- bump product/major/feature → 低位清零；bump daily → 第四位 +1；patch → 仅 RC+1。
- git tag：`vX.Y.Z.W-RCn`（annotated，消息为 release note）；**仅 main 分支允许打 tag**；`bump/patch/note/tag` 在非 main 分支报错退出。
- `version.md` 结构：标题行 `# aide 版本记录` → `**当前版本：X.Y.Z.W RCn**` → 每版一节 `## X.Y.Z.W RCn（日期）` + `- ` 列表 release note（最新在上）。

## 5. 非目标（本期不做）

- 不做 GitHub Releases / 远端推送集成（tag 仅存本地仓库，推送需用户配置 remote）。
- 不做变更日志自动聚合（release note 由人撰写，脚本只托管写入位置与版本号）。
- 不回溯为历史提交补版本号。

## 6. 验证方式

| 项 | 方法 |
| --- | --- |
| 后端门禁 | `bash scripts/aide.sh test`（新增 version 解析测试：version.md 存在/缺失/格式非法） |
| 脚本实测 | 容器/主机执行 `version.sh show/check`；临时仓库演练 bump/patch 的数字变化与 version.md 结构 |
| 钩子实测 | 安装钩子后做一次真实提交，确认消息自动带 `[版本 …]`；bump 提交不重复标注 |
| tag 实测 | main 上执行 bump/patch 后 `git tag -l` 出现 `vX.Y.Z.W-RCn` 且指向版本提交；在功能分支执行 bump 被拒绝并给出提示 |
| 浏览器实测 | 侧栏运行卡片与设置面板底部显示 `v0.1.0.0 RC1`（用户视觉确认） |
