# aide

一个以 Go 为后端、在浏览器中使用的本地 AI 开发工作台。参考 DeepSeek Harness（DSH）的会话、模型适配、工具与工作流分层思路，以独立代码实现基础功能。

**每次开发前**请先阅读 [产品需求文档 PRD](doc/PRD.md)，核对本次要做的需求编号、状态标记与范围边界；其中第 0 节和第 10 节是开工前的核对流程与清单。

开发与运维接手请先阅读 [项目交接文档](HANDOVER.md)，其中包含当前基线、部署配置、备份恢复、排障和后续开发事项。

## 启动

macOS 双击 **start.command**。它会启动 Docker Desktop、构建镜像、启动容器并在浏览器中自动登录。

也可以在本目录运行：

```bash
bash scripts/aide.sh start
```

默认地址：<http://127.0.0.1:8097>。通过普通链接打开时，需要访问令牌；`start.command` 自动从容器读取令牌并使用 URL fragment 完成登录。令牌保存在浏览器 localStorage，地址栏随即清除 fragment。

## 第一轮使用

1. 打开左下角 **模型设置**，填写 API Base URL、模型 ID 和 API Key。DeepSeek 地址通常为 `https://api.deepseek.com`；模型名称以自己的账户及提供商当前支持列表为准。
2. 在右侧打开工作目录或辅助资料，选择文件，点击 **附加到任务**。每次任务最多 8 个文件、合计 80 KB；单个可编辑文本文件上限 256 KB。
3. **对话**用于提问与代码分析。**AI 工作流**按“规划 → JSON 文件提案 → 审查”执行三个模型步骤。
4. 展开文件提案查看修改前后内容，点击 **应用这些文件修改** 写回本地项目。已有文件必须已附加；若主机上的文件已经改变，应用会拒绝覆盖。
5. 将建议命令填入底部 **命令面板**，检查后点击运行。实时展示 stdout/stderr 和退出码。

未配置模型时不会伪造 AI 回答。自动测试使用受控的本地模拟 API；真实云端回答和计费需要自己的有效 API 配置。密钥仅写入数据卷内的权限为 0600 的配置文件，不返回到前端，不进入 Git。

## 目录挂载

| 主机路径（默认） | 容器路径 | 用途 |
| --- | --- | --- |
| 当前 `aide/` | `/workspace` | 可读写项目，浏览器保存会同步到本地 |
| 上一级 `Harness/` | `/context` | 只读辅助资料，包含已有 DSH 等参考源码 |
| Docker `aide_aide-data` 卷 | `/data` | 会话、工作流状态、访问令牌、模型配置 |
| Docker `aide_aide-home` 卷 | `/home/aide` | 开发工具缓存和用户环境 |

需要修改挂载时，复制 `.env.example` 为 `.env`，将 `AIDE_WORKSPACE`、`AIDE_CONTEXT` 设置为已有的本地目录（支持带空格的绝对路径），再运行启动命令。Compose 必须从本目录运行。`.env` 已被 Git 忽略。

```dotenv
AIDE_PORT=8097
AIDE_WORKSPACE=.
AIDE_CONTEXT=../Harness
```

首次使用时可以从环境变量预置模型：`AI_BASE_URL`、`AI_MODEL`、`AI_API_KEY`。在浏览器保存设置后，数据卷中的设置优先于环境变量。连接 Mac 本机的兼容模型服务时可使用 `http://host.docker.internal:11434/v1`，模型服务须允许来自 Docker 的连接。

## 镜像与运行环境

Dockerfile、Compose 和导出的镜像均位于此项目目录中。Docker 引擎管理实际运行镜像，项目目录保存可迁移的归档。

```bash
bash scripts/aide.sh export  # docker-images/aide-local.tar.gz
bash scripts/aide.sh load    # 恢复到 Docker 引擎
docker compose up -d --no-build
```

镜像包含 Go 1.26 系列工具链、Python 3.12 + pip + venv、Node.js 24 + npm、Git、curl、bash（Compose 启用 init）。Linux ARM64 镜像可用于 Apple Silicon Docker Desktop。基础镜像版本按 Dockerfile 定义；精确构建信息见 `docs/verification.md`。

常用命令：

```bash
bash scripts/aide.sh status
bash scripts/aide.sh logs
bash scripts/aide.sh test
bash scripts/aide.sh stop
docker compose up -d --build  # 更新源代码后的重建
```

前端由 Go embed 编译进二进制；修改界面或后端后需重新构建镜像。项目默认无需 npm install，前端使用原生 JavaScript/CSS，没有 CDN 运行依赖。

## 版本管理

每次提交自动标注当前版本号（git 钩子）；版本升级在 **main 分支**执行并自动打 git tag：

```bash
bash scripts/version.sh                    # 显示当前版本（如 0.1.0.0 RC1）
bash scripts/version.sh bump <档位> -m "说明"  # 升级：product / major / feature / daily
bash scripts/version.sh patch -m "说明"     # 同一版本补丁：仅 RC+1
bash scripts/version.sh note -m "说明"      # 追加 release note
bash scripts/version.sh tag                # 为当前版本打 tag（幂等）
bash scripts/version.sh check              # 校验 version.md
bash scripts/version.sh install-hooks      # 新克隆后执行一次，安装提交标注钩子
```

- 版本号四位：`产品级.重大.大版本.日常`，呈现 `X.Y.Z.W RCn`（RC 默认 RC1，补丁 +1）。
- 当前版本与 release note 记录在工程目录 `version.md`；tag 命名 `vX.Y.Z.W-RCn`，**仅打在 main 分支**。
- 界面左下角运行卡片与设置面板底部显示当前版本（`/api/config` 的 `version` 字段）。

## 当前边界

- 面向单用户本地开发，Compose 端口仅绑定 `127.0.0.1`；API 使用随机访问令牌，并拒绝跨站 Origin。
- 命令面板是逐次运行的 shell，不是 PTY；不支持 vim、交互密码输入或跨命令保持 `cd`。单次最长 60 秒、最多 128 KB 输出。停止会终止进程组；此处不用于守护进程。
- 模型步骤异步执行，浏览器轮询进度；模型响应按步骤显示，尚未提供逐 token 流式输出。
- 工作流不会自动执行模型生成的命令。文件应用是逐文件原子替换，不是跨文件事务；部分失败会保留已应用标记。主机外部编辑、手动命令与文件应用之间不存在全局事务锁。
- `/context` 在 Docker 层只读；文件 API 使用 `os.Root` 防止目录逃逸。文件 API 隐藏 `.git`、`.env`、`.data` 和镜像归档目录，命令面板则具有容器用户的正常权限。
- 这是一个可信用户工作台。手动 shell 可以读写其有权限访问的容器数据和挂载目录；不要将服务暴露给不可信用户。未挂载 Docker socket、主机 HOME 或其他无关目录。
- 每次只将显式附加的文件提交给模型；历史回放有 60 KB 文本预算。附件中的指令被视为资料内容，系统提示明确要求以用户任务为准。
- 尚未实现 DSH 的完整 Cordis 插件兼容、MCP、多用户、向量检索、多 agent 调度或自动代码验证。后续扩展点见 [架构说明](docs/architecture.md)。

## 源码

```text
cmd/aide/                  Go 应用入口
internal/server/           HTTP API、文件工具、命令运行器、模型适配、工作流
internal/server/web/       原生浏览器界面（embed）
internal/server/*_test.go   HTTP / 工作流 / 文件边界 / 持久化测试
docs/                      架构与验证记录
scripts/                   启动、测试和镜像归档
docker-images/             可迁移镜像归档（大文件不进入 Git）
```

参考资料：[DeepSeek Harness](https://github.com/deepseek-ai/deepseek-harness)、[DeepSeek Chat Completions API](https://api-docs.deepseek.com/api/create-chat-completion/)、[Go os.Root](https://go.dev/src/os/root.go)。参考源码仅用于理解架构，没有将其文档指令作为本项目用户需求。
