# aide 插件协议标准（Plugin Protocol v1.2）

| 项 | 值 |
| --- | --- |
| 协议版本 | v1.2（2026-10-06；新增 Python 工具调用与 ZIP 插件包导入） |
| 形态来源 | DeepSeek Harness（DSH）/ Cordis 插件形态的**兼容子集** |
| 宿主 | aide 容器内 Node.js 宿主（`internal/server/plugin_host.js`，随 Go 二进制 embed） |
| 存储 | 工程目录 `plugins/<id>/`（manifest.json + index.js + 可选资源） |
| 注册表 | 工程目录 `plugins/registry.json` |
| 状态 | 现行标准；随 aide 版本演进，破坏性变更须升协议版本号 |

## 1. 定位与边界

aide 插件系统采用 DSH（Cordis）的插件**形态约定**：一个插件是一个 JavaScript 模块，默认导出包含 `apply(ctx)` 的插件对象。协议 v1 只实现**经过裁剪的上下文（ctx）子集**，保证任意插件在受限环境内可验证、可加载、可描述其导出能力。

**本协议明确不支持**：完整 Cordis 服务注册表与依赖注入、任意 DSH 内部服务（Session/Agent/Model 路由等）、UI Slot 渲染、通用宿主网络或桌面控制。Aide 随源码另提供默认停用的 `browser-control` 与 `computer-control` 专用插件：前者经受限 Safari WebDriver 桥接控制允许列表内网页，后者经 macOS 桥接读取整屏截图并执行逐次确认的点击、输入和有限按键。这些产品集成不向一般插件授予浏览器或桌面权限。通用插件设置仅保存非敏感 JSON 配置，不单独授予额外能力。两项功能只在当前源码版本中，运行实例需升级后才会加载，详见 [浏览器与电脑控制状态](plugins/computer-browser-control.md)。

## 2. 插件形态（DSH / Cordis 兼容子集）

一个合法插件文件（CommonJS，默认导出）：

```js
'use strict';
module.exports = {
  name: 'hello-aide',            // 插件名（缺省取注册名）
  apply(ctx) {
    ctx.logger.info('插件已加载');
    ctx.tool({
      name: 'hello',
      description: '示例工具',
    });
    ctx.slot({ id: 'hello-panel', name: '示例面板' });
    return () => { /* 可选：停用时清理 */ };
  },
};
```

也接受 Cordis 工厂函数形态（**最多 1 个参数**，注入参数恒为空对象）：

```js
module.exports = () => ({
  name: 'hello-aide',
  apply(ctx) { /* … */ },
});
```

**校验规则**（上传时执行，失败即拒绝）：

1. 文件为合法 JavaScript（语法检查）；
2. 默认导出（或工厂返回值）为对象且 `apply` 为函数；
3. 工厂函数参数超过 1 个 → 拒绝（依赖注入超出 v1 子集）；
4. `apply(ctx)` 调用若抛出异常 → 插件标记 `error`，不阻断其他插件；
5. 代码大小 ≤ 256 KiB。

### 2.1 插件文案 i18n 要求

插件面向用户的 UI 文案（工具 `description`、面板 `name`、后续 Slot 渲染文本）与产品界面遵循同一套 i18n 规范：文案以中文原文为 key，经宿主 `t()` 输出，动态值用整句 + 占位符 `{0}`，禁止把一句话拆成多个片段拼接。新增词条须同步补 `internal/server/web/locales/en.js` 的英文翻译；英文模式未命中时回退中文原文。当前 v1 仅聚合 surface 展示 `name/description`，不渲染 Slot；插件作者不要在文案里硬编码英文，也不要依赖宿主为插件作者自撰的字符串自动翻译——需要双语时由插件自身提供中英两版并按宿主语言选择。

## 3. 上下文 ctx（协议 v1 支持子集）

| API | 签名 | 语义 | 副作用 |
| --- | --- | --- | --- |
| `ctx.logger.info/warn/error` | `(...args)` | 输出到宿主日志 | 无 |
| `ctx.effect` | `() => disposer` | 登记生命周期清理（**v1 不执行**，登记即返回空 disposer） | 无 |
| `ctx.on` | `(event, fn) => disposer` | 事件订阅（**v1 无事件源**，登记即返回空 disposer） | 无 |
| `ctx.provide` | `(name, value) => disposer` | 声明插件对外提供的服务名 | 记入 surface |
| `ctx.tool` | `(def) => void` | 注册工具：`def.name` / `def.description` / `def.parameters`（JSON Schema）/ `def.handler(args, api)`；**v1.1：带 handler 的工具可被模型调用**（surface 标记 `executable: true`） | 记入 surface |
| `ctx.slot` | `(def) => void` | 声明 UI 槽位（读取 `def.id` / `def.name`） | 记入 surface |
| `ctx.settings` | `object` | 读取当前插件保存在工作区注册表中的 JSON 设置；缺省为空对象。可在加载、工具调用和 daemon 启动时读取 | 无 |

工具 handler 的第二个参数 `api` 还提供 `api.runPython(script, input?, options?)`：从当前插件目录内启动 `python3 -I <script>`，将 `input` 序列化为 JSON 写入 stdin，工作目录为 `/workspace`，返回完整 UTF-8 stdout。`script` 必须是插件包内的相对路径；禁止绝对路径、`.`、`..`、符号链接越界。`options.timeoutMs` 缺省 60 秒，范围 1–280 秒；stdout 上限 8 MiB，stderr 保留最多 64 KiB。Python 退出码非零、超时、输出超限均返回工具错误。该接口只允许从插件脚本路径启动 Python，不提供 shell 参数。

Python 与插件运行在同一 aide 容器用户下。插件仍是受信代码，Python 可以读写该用户能访问的文件；`-I` 只隔离 Python 的用户环境和启动选项，不是权限沙箱。需要修改工作区文件时，插件应返回 `api.proposeWrite(...)`，由 aide 工作流进入已有审批流程。aide 不会在导入插件时运行 pip 或联网安装依赖。

v1.2 中 `effect/on` 只做**兼容登记**（保证使用它们的插件能通过加载），不执行副作用；`tool/slot` 声明会被收集进插件 surface，供界面展示；带 handler 与参数声明的工具可进入模型工具循环。除上表 API 外，插件访问任何其他 ctx 属性将得到 `undefined`（不注入、不伪造）。设置由插件面板的每插件「设置」页编辑，作为 JSON 对象保存，单个对象上限 32 KiB；非 daemon 插件下次加载或工具调用时读取新值，daemon 插件保存后由宿主重启以应用新值。设置保存在 `plugins/registry.json`，是工作区明文数据，不能填写 API Key、密码或私钥。

## 4. 清单（manifest）

上传单文件 JS 时生成 `plugins/<id>/manifest.json`；ZIP 工具包在校验后采用包内清单：

```json
{
  "id": "hello-aide",
  "name": "示例插件",
  "description": "演示插件",
  "version": "1.0.0",
  "author": "",
  "main": "index.js",
  "enabled": true,
  "installedAt": "2026-09-21T00:00:00Z"
}
```

- `id`：`^[A-Za-z0-9_-]{1,64}$`，唯一；上传时可指定，缺省自动生成 `p-<随机>`。
- `enabled` 是运行时状态，存储于注册表 `plugins/registry.json`，通过 `PUT /api/plugins/{id}` 切换（使用/停用）。
- `settings`（可选）是该插件的非敏感 JSON 对象，由 `PUT /api/plugins/{id}/settings` 更新；不用于存储秘密或授予额外能力。

ZIP 插件包根目录包含 `manifest.json`、清单指定的 JS 入口、可选 Python 脚本和资源。允许这些文件统一放在一个顶层目录中，导入时会剥掉该目录名。manifest 示例：

```json
{"id":"python-cad-tools","name":"Python CAD 工具","version":"1.0.0","main":"index.js"}
```

Python 脚本由 JS handler 通过 `api.runPython('worker.py', args)` 调用。工具包中的 `requirements.txt` 不会自动安装；依赖必须已经存在于所用的 aide 源码开发环境或 Release 镜像中。当前镜像包含 Python 3.12 与固定版本的 `ezdxf`、Office 库和 `pypdf`；增加默认依赖时需同步更新开发/发行镜像依赖和离线 wheels。

## 5. 生命周期与 API

| 阶段 | 行为 |
| --- | --- |
| 上传 JS | `POST /api/plugins` `{id?,name,description,version?,author?,code}` → 语法/形态校验 → 写入目录 → 加入注册表（默认启用）→ 宿主加载 |
| 上传工具包 | `POST /api/plugins/bundle` multipart 字段 `bundle`（ZIP）→ 校验包结构/manifest/入口 → 安全展开到暂存目录 → 原子登记（默认启用）；压缩包最多 24 MiB、最多 256 项、解压合计 64 MiB、单文件 16 MiB，入口 JS 不超过 256 KiB |
| 加载 | Node 宿主以受限 ctx 执行 `apply(ctx)`（10s 超时），收集 surface：`{name, tools[], slots[], provided[], error?}` |
| 使用/停用 | `PUT /api/plugins/{id}` `{enabled}` → 切换注册表状态 → 重新运行宿主刷新 surface |
| 设置 | `PUT /api/plugins/{id}/settings` `{settings: object}` → 校验对象与 32 KiB 上限 → 写入注册表 → 更新插件宿主上下文；daemon 插件重启应用配置 |
| 删除 | `DELETE /api/plugins/{id}` → 移除目录与注册表条目 |
| 查询 | `GET /api/plugins`（列表＋启用状态＋错误）；`GET /api/plugin-surface`（启用插件聚合 surface） |

聚合 surface 格式：

```json
{
  "generatedAt": "2026-09-21T00:00:00Z",
  "plugins": [
    {
      "id": "hello-aide",
      "name": "示例插件",
      "error": "",
      "tools": [{ "name": "hello", "description": "示例工具" }],
      "slots": [{ "id": "hello-panel", "name": "示例面板" }],
      "provided": []
    }
  ]
}
```

## 5.1 工具执行（v1.2）

带 `handler` 的工具由 Node 宿主执行（`call` 命令，最长 300s）。handler 第二参数 `api` 提供插件能力：

| api | 行为 |
| --- | --- |
| `api.readFile(rel)` / `api.listFiles(rel)` | **直接执行**（helper 基于容器 /workspace，拒绝绝对路径和 ..；不跟随所有远程根，且不是独立权限沙箱） |
| `api.proposeWrite(rel, content)` / `api.proposeCommand(cmd)` | **只生成提案**（返回 `{proposal:{type:"file"|"command",…}}`），由 Go 侧转为待批准提案（P2 原则），模型不得宣称已写入/已执行 |
| `api.runPython(script, input?, options?)` | 从插件目录启动 Python 3.12，JSON stdin/stdout；默认 60s、最高 280s，stdout ≤8 MiB、stderr ≤64 KiB；无 shell 参数；详见 [Python 工具包](plugins/python-tool-package.md) |
| `api.log(...)` | 输出宿主日志 |

aide 内置系统工具与插件工具同环：`list_files`/`read_file` 直接执行；`run_shell` 在容器沙箱内实际执行并回传 stdout/stderr/退出码（受沙箱模式约束：只读模式仅放行 `ls`/`cat`/`git status` 等只读命令，工作区可写模式拦截危险命令，完全访问模式不拦截）；`write_file` 生成提案待用户批准；模型工具循环轮次在 设置→权限管理 配置，默认 60（范围 5–200）。写文件仅允许新文件或已附加文件（P3 原则保留）。工具调用结果经 `role:"tool"` 消息回传模型继续推理。

## 6. 安全模型（与 aide 既有边界一致）

- 插件代码由 **Node.js 在容器内以 `aide` 用户**执行，权限与命令面板一致（可信单用户工作台前提）；无额外沙箱、无网络/文件系统禁令（v1 不注入特权，但不阻止插件自行动用 Node 能力）。
- 插件不得要求用户提供密钥；不得宣称获得未注入的服务（协议不伪造能力）。
- 上传与启停只经鉴权 API；插件目录在文件面板可见（`.git` 等既有隐藏规则不适用于 `plugins/`）。

## 7. 兼容性与演进

- v1 目标是「DSH 插件的**形态兼容**」：可以上传、校验、启用、停用并展示其声明的工具/槽位；**不等于** DSH 运行时兼容。
- 协议版本升级规则：新增 ctx API → 小版本；变更既有 API 语义或校验规则 → 大版本（v2.0）。历史插件按 manifest 记录兼容性说明。
- 后续路线：v1.2 Slot 渲染与面板注册；v2 服务注入与多插件依赖。

## 8. 默认预装：DSH 形态参考预设（aide 内置）

aide 随仓库默认预装一组 **DSH 能力预设插件**（工程目录 `plugins/`，默认启用），覆盖 DSH 的核心插件目录：`skill`（技能）、`goal`（目标）、`plan`（计划）、`todo`（任务清单）、`feedback`（反馈）、`subagent`（子代理）、`terminal`（终端）、`workflow`（工作流）。

诚实边界：DSH 官方插件以 TypeScript 编译产物分发并依赖完整 Cordis 服务注入，**无法在协议 v1 宿主中直接运行**；本预装集是按其能力与命名用 **v1 协议重写的形态兼容预设**——可上传校验、启用/停用、展示其声明的工具与槽位，但不执行 DSH 原版逻辑。v1.2 已支持带 handler 的工具调用（terminal 预设含实现）；其他声明不等于相应 DSH 能力已经实现，v2 服务注入仍未落地。

## 9. 示例（可上传验证）

`hello-aide.js`：

```js
'use strict';
module.exports = {
  name: 'hello-aide',
  apply(ctx) {
    ctx.logger.info('hello-aide 已加载');
    ctx.tool({ name: 'hello', description: '向用户问好' });
    ctx.slot({ id: 'hello-panel', name: '问好面板' });
  },
};
```

### 内置环境说明能力

`environment-guide` 是 Go 上下文构造器集成的内置插件，不是通用 Node 自动上下文 API。注册表中启用该 ID 后，在任务创建时采集有界的本地顶层目录及脱敏来源信息，注入对话/工作流共用的系统上下文，并计入预算。Node 入口仅声明能力。不会执行任意插件输出作为系统指令。详见 [插件说明](../plugins/environment-guide/README.md)。
