# aide 插件协议标准（Plugin Protocol v1.2）

> 当前说明按 2026-10-17 源码 `0fef6411` 核对；实际界面、示例数据、截图原图及验证范围见[实际使用截图](ui-evidence.md)。源码、浏览器观察与发行验收分别记录，截图中的版本文字不代表已发布本提交。

![插件工坊入口；不是新插件功能验收](images/current-20261017/11-plugin-workshop.jpg)

当前模块职责见[软件模块说明](architecture/modules.md)；插件工坊入口；不是新插件功能验收。

| 项 | 值 |
| --- | --- |
| 协议版本 | v1.2（2026-10-06；新增 Python 工具调用与 ZIP 插件包导入；2026-10-12 补充不改变运行时能力边界的设置 schema 表单视图） |
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

v1.2 中 `effect/on` 只做**兼容登记**（保证使用它们的插件能通过加载），不执行副作用；`tool/slot` 声明会被收集进插件 surface，供界面展示；带 handler 与参数声明的工具可进入模型工具循环。除上表 API 外，插件访问任何其他 ctx 属性将得到 `undefined`（不注入、不伪造）。设置由插件面板的每插件「设置」页编辑，作为 JSON 对象保存，单个对象上限 32 KiB；支持「渲染」与「文本」两种编辑方式：渲染方式读取 manifest 的 `settingsSchema` 生成参数控件，文本方式允许完整 JSON 编辑；未在 schema 中声明的字段保留在扩展参数区。非 daemon 插件下次加载或工具调用时读取新值，daemon 插件保存后由宿主重启以应用新值。设置保存在 `plugins/registry.json`，是工作区明文数据，不能填写 API Key、密码或私钥。

### 3.1 工具读写资源调度（2026-10-14，兼容 v1.2）

工具可以在注册定义中增加 `scheduling.resources`，声明模块加载、apply/清理和handler的完整作用范围。声明来自插件宿主发布的 surface；模型参数中的 `readOnly`、`scheduling` 等自报字段不能改变它。

```js
ctx.tool({
  name: 'prepare_draft',
  parameters: {type: 'object', properties: {path: {type: 'string'}}, required: ['path']},
  scheduling: {resources: [
    {root: 'workspace', argument: 'path', access: 'read'},
    {root: 'workspace', path: 'draft.md', access: 'write'},
  ]},
  handler(args, api) {
    return api.proposeWrite('draft.md', api.readFile(args.path));
  },
});
```

每项 `root` 为 `workspace` 或 `plugin`，`access` 为 `read` 或 `write`；`path` 固定相对路径与 `argument` 顶层字符串参数名二选一，最多32项。目录声明覆盖后代；不支持通配符。相对路径不能越界；出现符号链接、硬链接别名、无法确认的根或无效参数时，回退整个插件执行域独占。`resources: []` 明确声明除自身插件目录状态外没有其他读写或外部变更；自身插件目录始终隐式独占，同插件FIFO仍保留。

统一入口一次申请全部资源。不同插件的读读及不冲突资源可重叠；读写/写写互斥，等待中的冲突写者不会被后来的读者插队。缺少/无效声明、使用跨插件服务的消费者、SSH工作区以及浏览器/电脑控制插件保守独占整个插件执行域。等待可取消，不启动宿主进程；取得资源后再次检查工作区代际、声明、启用及禁用状态。有明确且无服务依赖的契约调用只加载目标插件，避免其他模块初始化代码超出预约范围。当前 helper 操作容器本地目录，不能用本声明冒充SSH路径锁。

声明只读的工具返回写提案、写提案超出声明路径、或受限文件声明返回命令提案时，调用报告契约不一致，提案不进入任务审批队列。正确的写提案仍走原审批与哈希检查；资源预约期间没有因此自动应用文件。后续实际应用和外部服务不是本次资源锁的事务范围。插件是受信代码，声明不是OS沙箱，不能限制插件绕过helper直接访问容器或外部资源；作者必须完整声明实际效果。

短命宿主取消、daemon超时或取消时，终止对应进程组后再释放资源。普通继承进程组的Python/Node子进程随之终止；主动脱离进程组的进程和已发生的外部副作用不属于撤回保证。daemon调用的登记和派发绑定同一进程代际，不会在重启空隙把旧调用发送到替代实例。中断后结果待核实，不自动重放。

资源锁仅协调经统一入口派发的调用；人工编辑、插件配置/启停初始化与daemon主动后台活动不在本次调用预约范围内，插件应自行协调这些活动的共享状态。5x工具节点依赖、并行审批预检、节点日志和恢复归并仍按[调度设计](architecture/plugin-task-scheduling.md)继续实现，不能据此宣称完整DAG已运行。

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
  "settingsSchema": {
    "type": "object",
    "title": "参数模板",
    "properties": {
      "enabled": {"type": "boolean", "title": "启用选项", "default": true},
      "timeoutSec": {"type": "integer", "title": "超时（秒）", "minimum": 1, "maximum": 600, "default": 30},
      "allowedHosts": {"type": "array", "title": "允许的主机", "items": {"type": "string"}, "default": []}
    },
    "additionalProperties": true
  },
  "installedAt": "2026-09-21T00:00:00Z"
}
```

- `id`：`^[A-Za-z0-9_-]{1,64}$`，唯一；上传时可指定，缺省自动生成 `p-<随机>`。
- `enabled` 是运行时状态，存储于注册表 `plugins/registry.json`，通过 `PUT /api/plugins/{id}` 切换（使用/停用）。
- `settings`（可选）是该插件的非敏感 JSON 对象，由 `PUT /api/plugins/{id}/settings` 更新；不用于存储秘密或授予额外能力。
- `settingsSchema`（可选）是渲染设置表单的 JSON Schema 子集：顶层 `type: object`，支持 `properties`、`required`、`title`、`description`、`default`、`enum`、`minimum`、`maximum`、`format`、`placeholder`、`items` 与 `additionalProperties`。字段类型支持 string、number、integer、boolean、array、object；数组可用换行编辑字符串项，复杂数组/对象以 JSON 编辑。未知设置保留为可编辑扩展字段。限制 32 KiB；它只定义 UI，不授权任何能力。

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

### 在线升级与恢复

每张插件卡片提供“升级”。已有来源时直接执行；自定义插件没有来源时填写一次 HTTPS ZIP 地址，成功后记住；设置页的“升级来源”可修改来源并升级。内置插件 ID 默认使用 AIDE 官方 GitHub 仓库，先固定 main 的一次提交，再下载该插件目录并逐个校验 Git blob 摘要，不下载整个仓库。网络不可用时保持原版本。

`POST /api/plugins/{id}/upgrade` 接收 `{url?, sha256?}`（空对象使用既有来源）。仅持主访问令牌的请求可用。地址禁止凭据、HTTP、本机和私网；重定向与实际连接地址再次检查，DNS结果固定后连接；不给上游发送工作台认证信息。支持本机 TUN 代理为固定 GitHub 官方域名返回的 RFC2544 Fake-IP，仍校验原始 HTTPS 主机名；不会因此放开任意域名或字面私网地址。自建私网更新服务与认证下载目前不支持。

ZIP 与上传共用结构、路径、CRC、入口和大小检查，插件 ID 必须一致；已知数字版本禁止降级。可传发布方提供的 SHA-256；默认仍有 HTTPS、ZIP CRC 与代码加载检查，不能把它称为发布方签名校验。单次下载最多24 MiB、解压最多64 MiB/256项/单文件16 MiB/入口256 KiB；下载总期限2分钟、请求总期限5分钟。没有自动 pip/npm 安装，升级包或既有环境必须满足依赖。

下载在前，替换在后：等待现有工具调用释放资源，再序列化生命周期操作。候选目录复制原插件本地文件、依赖及数据，然后覆盖包内文件；不删除未被新版包覆盖的本地文件。原树最多1 GiB、50,000项；拒绝外部/绝对软链接、特殊文件和新版文件与本地链接的父目录冲突。保留注册表的启用状态、设置、首次安装时间，新清单更新版本/入口/schema；停用的插件不会被升级顺便启用。

暂存环境加载新插件和已启用依赖，拒绝破坏此前健康消费者的版本。切换前记恢复日志；旧目录留为 `plugins/.upgrade-backup-<id>-<随机>/`，其中 `.aide-upgrade-registry.json` 保存之前的注册表。插件清单增加可选字段 `updateURL`、`packageSHA256`、`updatedAt`、`upgradeBackup`；已有清单兼容。目录和注册表提交失败时恢复旧版，守护插件启动失败也会恢复。服务重启时依据日志恢复未提交替换，已提交版本保留；恢复失败会明确报错，不当成成功。

一实例一次升级，重复请求409；升级前快照在等待后再次核对，工作区/设置/版本已变化时拒绝替换。排队工具调用记录代码摘要，替换后拒绝旧版本调用。候选校验和替换失败后旧插件仍可调用；客户端在切换开始后断开时服务端完成事务，浏览器需刷新核对结果。当前不提供应用内手动回退按钮；手动恢复须先停服务并结合备份注册表和目录恢复，不能在并行写入时覆盖整个注册表。

插件是受信代码，候选加载可执行 apply；外部设备/网络副作用不属于文件事务的撤回保证。此处恢复保证针对插件目录与登记状态，不是OS沙箱或任意外部数据迁移的完整回滚。

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

## 受信插件的同步服务装配

插件模块可声明 `provides: ["service-name"]` 和 `requires: ["service-name"]`，每组最多64个唯一名称。宿主按依赖顺序执行同步 `apply(ctx)`；消费者使用 `ctx.consume(name)` 获取提供者通过 `ctx.provide(name, value)` 注册的真实值。没有声明的消费、服务缺失、多个提供者、循环依赖、声明但未注册服务和重复工具名（含内置工具名）均返回 surface 错误，并移除失败插件工具。

```js
// Provider module
module.exports = {
  name: 'formatter', provides: ['example.formatter'],
  apply(ctx) {
    return ctx.provide('example.formatter', text => String(text).trim());
  }
};
// Consumer module (installed as a different plugin)
module.exports = {
  name: 'consumer', requires: ['example.formatter'],
  apply(ctx) {
    const format = ctx.consume('example.formatter');
    ctx.tool({name: 'example_format', description: 'Format text',
      parameters: {type: 'object', properties: {text: {type: 'string'}}},
      handler: args => format(args.text)});
  }
};
```

非daemon工具每次调用用操作者当前启用插件列表重新装配目标插件及依赖，校验工具实际归属；结束后反序调用清理回调。扫描surface也反序清理。依赖关闭后不能通过旧工具schema越过停用检查。现有每插件设置由各模块的 `ctx.settings` 读取，配置变更不需要重新编译产品。
这不是完整Cordis兼容层，不提供跨进程服务对象、异步apply或daemon间服务注入。daemon既有生命周期保持独立。`effect/on`仍为兼容登记，不代表事件订阅已实现。同步服务是受信插件代码，不构成新的权限或隔离边界；安装/启用仍由操作者进行。

## 插件工坊与 AI 管理权限

内置 `plugin-authoring`（插件工坊）提供 `manage_plugins` 与 `manage_references` 原生工具。日常工作中，AI 可先读取 `guide`，按用户资料判断：可执行工具做插件，规范、模板、知识和操作说明放到指定引用。系统文档目的地仍来自当前工作空间配置，不能固定目录。

主设置的 **AI 管理权限** 分别保存 `aidePlugins`、`assistantPlugins`、`aideReferences`、`assistantReferences`，默认全部关闭。它们仅控制管理动作；普通工具调用、引用文件的已有读写授权不变。助理交办的任务也按助理权限判断，不能借普通工作流绕过。已有引用的可见范围、可写锁及停用状态在管理更新中保留，新引用默认只读；系统内置引用仍由工作空间配置管理。

- `manage_plugins(action=guide|list)` 返回协议或清单；`inspect(id)` 读取入口源码（需要管理权限）。`prepare(manifest, files)` 生成可下载的 ZIP 及待确认安装/升级提案，`enable|disable|remove` 生成对应管理提案。
- 候选入口为自包含 `index.js`，上限 256 KiB。候选文本合计至多 1 MiB、64 个文件；路径及 manifest 经过 ZIP 解析器校验，JavaScript 只运行 `node --check`。准备阶段不会 `require` 或执行生成代码，也不安装依赖。
- 管理提案显示在输入框上方；展开代码/配置后才能确认执行。服务器保留包指纹及原始状态，确认前重查工作空间、角色权限及插件/引用版本。权限关闭或目标变化会拒绝旧提案。助理自动审批不应用于这些管理操作。
- 确认后复用现有安装、升级和引用 API。升级保留插件设置及启停状态，沿用备份、回滚、依赖检查；真实工具注册校验仅在确认后执行。生成 ZIP 或通过语法检查不等于插件运行成功。
- `manage_references(action=list|upsert|remove)` 管理本地、Skill、HTTPS 链接或不携带凭据的 SFTP 引用。凭据由用户在既有设置中管理。引用正文编辑继续使用 `write_file(source=ID)`，不通过管理工具绕过可见/可写锁与文件审批。

队列按工作空间持久化，最多保留 12 个待确认提案，内容总量 10 MiB；执行中写入运行回执，避免刷新重放。异常中断后不能自动重试已进入执行的操作，应先核对实际安装/引用状态。

这些设置约束 Aide 原生管理工具，不是 Node 或 shell 的操作系统沙箱。已安装插件仍是可信代码执行器；既有 shell、容器挂载和插件代码权限边界不因这些开关而改变。
