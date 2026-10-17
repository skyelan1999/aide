# 技能库 / Skill Library 1.1.0

为 Aide 的当前任务寻找公开 GitHub Agent Skills，将完整技能目录校验并缓存在 Aide 自身数据目录，确认后登记为只读 Skill 引用。升级原来的 `skill` 占位插件，保留原 surface 名称；真正可调用的工具是 `skill_library`。

## 安装和使用

1. **先使用包含 Skill Library 原生执行器的 Aide 构建**，再在插件面板上传本插件 ZIP，或升级已有的官方 `skill` 插件。已有同 ID 插件应走既有升级流程（也可用插件工坊 prepare 升级该包），不要重复导入覆盖；升级会保留原设置和启停状态。仅上传此 ZIP 不能给旧版本 Aide 增加原生接口；旧宿主会明确报错，不能用 shell 修改 sources.json 绕过。
2. 主设置 → AI 管理权限，开启需要使用者的「Aide 对引用的管理」或「助理对引用的管理」。搜索公开目录不需要此管理权限，准备/登记引用需要。安装或升级插件本身另受插件管理规则约束。
3. 对 Aide 说「寻找适合生成 PDF 报告的 skill，准备放入引用」。插件工具说明引导模型按任务先搜索，再选择候选准备；不是在后台持续扫描全网，也不是保证每个模型都会自动调用。
4. 在输入框上方审阅管理提案的 SKILL.md、来源、提交、文件清单和摘要并确认。确认前只有公开内容缓存，**引用尚未登记**；新引用默认只读。确认后可用 `read_file(source=引用ID, path="SKILL.md")` 读取，其他资源按相对路径读取。

```json
{"action":"search","query":"制作 PDF 文档 report"}
```

```json
{"action":"prepare","repository":"openai/skills","path":"skills/.curated/pdf","ref":"main","reason":"为当前报告任务准备 PDF 工作流程"}
```

也可用 `{"action":"prepare","url":"https://github.com/owner/repo/tree/commit/skill-directory"}` 或指向 SKILL.md 的 blob/raw 链接；repository/path/ref 与 url 二选一。带斜杠的分支名请用分开的 `ref` 参数，不用有歧义的 tree URL。`installed` 仅列出当前工作空间可见且启用的 Skill 引用。

## 来源、缓存与限制

- 默认检索 `openai/skills` 和 `anthropics/skills`。插件设置支持最多四个 `owner/repository`，以及 5–1440 分钟的目录缓存（默认 60 分钟）。仓库和模型参数不能包含 API Key；只支持无凭据的公开 GitHub。
- 名称、路径、说明按关键词排序，含常用中英关键词映射；中文专业描述可同时提供英文关键词。不是向量语义搜索，也不会伪造不存在的结果。每仓库最多索引128个有效技能，完整性和网络问题会返回警告；其他技能可直接提供链接。
- 下载先固定一次提交，再校验每个 Git blob SHA-1 和缓存文件 SHA-256。只下载所选技能目录及根许可证/NOTICE，保留目录内的 scripts、references 和二进制 assets。SKILL.md 至少需要 name、description frontmatter；读取常见普通、引号与块标量，不是完整 YAML 验证器。为兼容现有目录，说明超过标准1024字符但在4096以内时保留原文，检索摘要截断并提示。
- 缓存位于 `AIDE_DATA/cache/skills/installed/<内容摘要>`（Docker 常为 `/data/cache/skills/...`）；引用显示虚拟路径 `/skill-cache/<内容摘要>`。**不依赖宿主用户名、网卡、工作空间目录或 Codex 配置**。Docker 对应持久目录通常是 `.aide-data/storage/data/cache/skills/`，清理 Docker 构建缓存不会删除它。
- 缓存不可变版本复用，重复确认同一已登记版本返回 `already_installed`；更新创建新版本，不覆盖旧版。外部编辑/新增文件会导致完整性检查失败，不能用重新安装悄悄覆盖。被隐藏或停用的同名技能引用不会被本工具重新打开；已有读写/可见锁在更新时保留。
- 单技能最多128个文件、单文件4MiB、总计8MiB；SKILL.md 256KiB，至多64个缓存版本。缓存满后明确报错，不自动删仍可能被引用的文件。取消、拒绝提案后的缓存可留作复用，清理前应先检查是否仍被引用。
- 拒绝符号链接、子模块、目录穿越、保留名和不完整的大型目录。网络只访问 GitHub API 和原始文件 HTTPS 主机，复用 Aide 的连接/DNS检查，不发送本机认证信息。目录缓存可在网络失败时返回旧结果并标注，准备安装仍需重新核对上游。
- 下载不代表安全审计。第三方内容属于不可信资料，不能替代用户指令、扩大工具权限或自动解锁引用。**不执行脚本、不安装依赖、不启动 daemon**；使用技能中的代码仍走既有工具与审批。
- Aide 和小秘共用这个原生工具，各按自己的引用管理权限判断。管理提案必须由人确认，助理自动审批不代替它。工作空间切换、权限撤销、插件升级/停用、工具禁用、锁屏或缓存变化后，旧提案不可继续应用。

## 打包

在仓库根运行 `python3 plugins/skill/package.py`。输出到忽略的 `.agent-state/packages/aide-pkg-skill-v1.1.0.zip`，包含 manifest.json、index.js、README.md 和 Aide 的 LICENSE；不携带用户设置、令牌、缓存或注册表。脚本先执行 Node 语法检查和 Aide 宿主形态验证。

格式依据：[Agent Skills specification](https://agentskills.io/specification)、[OpenAI catalog](https://github.com/openai/skills)、[Anthropic catalog](https://github.com/anthropics/skills)。公开仓库中的不同技能可能采用不同许可；以下载保存的上游许可为准。
