# Skill 引用插件验证 · 2026-10-18

任务：`skill-reference-plugin-20261018`，源码基线 `19e7c94d`。用户确认公开 GitHub 范围，默认 OpenAI/Anthropic，可配其他仓库或精确链接。

## 交付和边界

- 原 `skill` 占位插件升级为 1.1.0，保留旧工具/面板/服务声明，增加 `skill_library`。自包含 Node 入口、manifest、使用文档、可重现 ZIP 打包脚本。
- 原生执行器处理公开检索、固定提交/摘要、Aide 自有缓存、引用管理提案。插件不会读取 API Key，也不写注册表来绕过权限。只安装 ZIP 的旧宿主会明确报错，需要包含本次支持的 Aide 构建。
- `search` 是按目录名称/路径/说明的关键词检索，提供常见中英映射；不是全网搜索或向量匹配。插件工具说明供模型在任务需要时选择调用，不冒充已经完成付费模型自动调用验收。
- `prepare` 下载完整所选目录和上游许可到 `AIDE_DATA/cache/skills/installed/<摘要>`，生成现有管理提案。确认前不登记引用；确认后为 `/skill-cache/<摘要>` 的只读 Skill 引用。禁止脚本执行/依赖安装。更新保留锁，缓存不覆盖已改文件。
- 原生支持被限制在真实启用的 skill 工具归属，按 Aide/助理各自的引用管理权限判断；执行前重查工作空间、权限、锁屏、插件源码/设置/状态、缓存完整性。没有自动同意管理提案的后门。

## 实际执行证据

| 检查 | 结果 |
| --- | --- |
| `agent-route.py verify quick` | 最终125项 PASS；输入指纹 `b0770b0bc680786952877f72f814c612067839f6e1309dc7b5c7a49f0f091af1`，日志 `.agent-state/verify-20261017T174223779654Z.log`。包含当前其他未提交注册表状态；不把该文件纳入本任务提交。 |
| `AIDE_BUILTIN_PLUGINS= go test -race -mod=vendor -count=1 -timeout=3m -run '^Test(Skill\|Management\|Authoring)' ./internal/server` | 专用 Linux 容器、无外网、合成 GitHub 响应；PASS（22.215s）。Skill 7个本地顶层测试及子场景、既有管理回归；公开网络测试默认 SKIP。 |
| `go vet -mod=vendor ./internal/server`；`CGO_ENABLED=0 go build -mod=vendor -trimpath ./cmd/aide` | 隔离容器 PASS。没有替换正式二进制或镜像。 |
| macOS arm64 原生 `-test.run '^TestSkill'`，显式 `AIDE_TEST_LIVE_SKILLS=1` | 8个顶层测试 PASS。包括完整引用确认/读回、二进制资源、只读写入拒绝、更新与隐藏、角色/沙箱/停用/取消、提案撤权/锁屏/篡改/额外文件、摘要/链接/目录穿越、ZIP导入/工具归属、请求中工作空间变化。日志 `.agent-state/skill-library/native-final.log`。 |
| 最终 ZIP 的真实宿主校验 | Node 语法、loadPlugin、apply 注册与 executable 检查 PASS。实际服务端 ZIP导入及 skill_library 原生归属回归 PASS，保留旧 slot 后又验证一次。 |
| 公网 OpenAI 目录 | 实际取得44个有效 Skill，无警告；PDF固定提交 `49f948faa9258a0c61caceaf225e179651397431`，4文件下载、缓存摘要复核通过。 |
| 公网 Anthropic 目录 | 实际取得19个有效 Skill；模板跳过，claude-api 的超长说明兼容并仅截断检索摘要。PDF固定提交 `dbd4588f9e1033efb41dad4bef2f7947c8993d44`，12文件下载、缓存摘要复核通过。 |

公开检查只使用临时测试 App，不执行下载脚本、不创建真实引用，测试缓存由测试框架清除。首次检查发现一个真实技能说明超过规范建议长度，已在有界范围内兼容，未关闭 name/路径/摘要校验；模板仍不会作为可用技能安装。测试准备阶段也修复了无 Task 的助理入口空指针；隔离检查显式关闭镜像预装插件，避免测试夹带宿主配置。

ZIP：`.agent-state/packages/aide-pkg-skill-v1.1.0.zip`，6036字节，SHA-256 `ab7e49c3e8a90ec8b11453813ce6c0536f27dd4d4028b0acfd69a2afc509e9f7`。内含 manifest、index、README、Aide LICENSE，不含用户注册表、缓存或凭据。

## 未验证 / 未发布

- 没有重启生产服务、导入真实插件或修改真实引用；浏览器中安装/设置/管理提案的本轮可见验收 **NOT_RUN**，不以 httptest/API 或 CLI 冒充。
- 没有调用付费模型验证自动选技能，也没有做 Windows/Linux 浏览器实机、全量 full、正式镜像发行、版本升级或远端推送。
- 未修改 Clash/系统代理。出站下载复用现有受限 HTTPS/DNS 检查，匿名 GitHub 限流会返回错误或带警告的旧目录，不索取密钥。
- 旧 host 不会因 ZIP 获得原生能力；已存在 skill ID 必须用既有升级流程，不支持重复导入覆盖。当前源码支持、打包校验与正式部署分开。

格式与目录依据：[Agent Skills](https://agentskills.io/specification)、[OpenAI](https://github.com/openai/skills)、[Anthropic](https://github.com/anthropics/skills)。不把第三方说明或 allowed-tools 当作用户授权。
