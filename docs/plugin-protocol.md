# aide 插件协议标准（v1.2 + v1.3 工作台候选扩展）

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

工具 handler 的第二个参数 `api` 还提供 `api.runPython(script, input?, options?)`：从当前插件目录内启动 `python3 -I <script>`，将 `input` 序列化为 JSON 写入 stdin，工作目录为本次调用绑定的当前本地工作区（默认配置仍为 `/workspace`），返回完整 UTF-8 stdout。`script` 必须是插件包内的相对路径；禁止绝对路径、`.`、`..`、符号链接越界。`options.timeoutMs` 缺省 60 秒，范围 1–280 秒；stdout 上限 8 MiB，stderr 保留最多 64 KiB。Python 退出码非零、超时、输出超限均返回工具错误。该接口只允许从插件脚本路径启动 Python，不提供 shell 参数。

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
| `api.readFile(rel)` / `api.listFiles(rel)` | **直接执行**（helper 基于本次调用绑定的当前本地工作区，拒绝绝对路径、..和符号链接越界；SSH不回退本地，且不是独立权限沙箱） |
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

`environment-guide` 是 Go 上下文构造器集成的内置插件，不是通用 Node 自动上下文 API。注册表中启用该 ID 后，在任务创建时采集有界的本地顶层目录及脱敏来源信息，注入对话/工作流共用的系统上下文，并计入预算。Node 入口仅声明能力。不会执行任意插件输出作为系统指令。详见 [插件说明](https://github.com/skyelan1999/aide/blob/4dc9a7b0b01fef6ac3550b0b0fcf97db42d45f94/plugins/environment-guide/README.md)。

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

## Skill Library：公开技能缓存与引用

2026-10-18 源码把原 `skill` 占位插件升级为技能库，工具为 `skill_library(action=search|prepare|installed)`。默认目录为公开 `openai/skills`、`anthropics/skills`，可设置最多四个公开仓库，也可用明确的 GitHub tree/blob/raw 链接。按当前任务关键词搜索、由模型选择候选；没有全网后台抓取或向量语义匹配承诺。

`skill_catalog.go` 负责固定提交、目录和 Git blob 摘要校验、有界元数据检索；`skill_library.go` 负责 Aide 自有缓存、角色权限与现有引用管理提案。Node 入口只声明工具，下载和登记使用宿主的受限原生执行器，不能从任意插件伪造工具归属。**旧宿主只安装 ZIP 不会获得原生执行器**。

缓存位于 `AIDE_DATA/cache/skills/installed/<SHA256>`，引用配置以 `/skill-cache/<SHA256>` 映射到该版本，不能映射整个 `/data` 或向上越界。保留 scripts/references/assets、根许可证和来源回执；下载不执行代码、不安装依赖。每份最多128文件/8MiB，单文件4MiB、SKILL.md 256KiB；目录每仓库128技能，缓存至多64版本。目录缓存时间默认60分钟，可配置5–1440分钟，离线旧结果会标记警告。超长说明仅在有界兼容范围内接受，检索摘要截断，完整原文保留；不是完整 YAML schema 验证器。

搜索是普通公开资料读取；prepare 和确认登记要求对应 Aide/助理的引用管理权限，复用 `ManagementProposal` 的人确认与基准校验。新引用只读，更新保留原启用、可见和读写锁；隐藏/停用的同 ID 引用不由该工具重新打开。工作空间、插件状态/源码/设置、权限、锁屏或缓存摘要变化会拒绝旧提案。已有引用仍走原读写工具，Skill 的 allowed-tools 或正文不会授予权限。

源代码与使用/打包说明见 [技能库 README](https://github.com/skyelan1999/aide-pkg-skill/blob/main/README.md)。已有 `skill` 插件用既有升级流程保留设置和启停状态；不要直接重复导入同 ID。该源码功能、可上传 ZIP、真实网络检查与正式实例安装/发布状态分开记录于 [任务记录](tasks/skill-reference-plugin-20261018.json)。

## 官方市场安装

用户可以从插件面板的「市场」浏览官方条目，核对依赖与权限后安装/更新；也可从[市场网站](https://skyelan1999.github.io/aide-pkg-shop/)下载经摘要核对的 ZIP 后上传。目录和包固定到同一提交，安装沿用现有 manifest、ZIP、加载和升级规则；不自动安装 npm/系统依赖。数据模型、缓存、兼容性及未实现的签名边界见[官方插件市场](architecture/plugin-market.md)。


## 2026-10-18：独立插件工作台（v1.3 候选扩展）

旧工具API保持不变；manifest新增可选声明，只有包含本扩展的宿主才渲染“打开工作台”。这不是完整Cordis UI Slot或任意宿主服务注入。

```json
{"workbench":{"protocol":1,"entry":"workbench.html","title":"工程工作台"}}
```

entry须为包内普通相对.html路径，禁止隐藏路径、越界、反斜杠、符号链接；HTML非空且不超过2MiB，标题1–64字符。ZIP导入时验证声明与文件，安装注册表保留它。自包含HTML可带内联CSS/JS及Blob Worker，不需要放宽Aide主页面CSP。普通插件缺省无此声明，仍按原流程加载。

入口：已启用插件卡片的“打开工作台”。首版是全屏独立浮层，不是多标签页或独立浏览器窗口。`POST /api/plugins/{id}/workbench/open` 沿用Bearer与同源校验，解锁后返回60秒有效的随机单次页面URL；最多8个未过期入口，绑定插件管理作用域、锁屏代际及HTML内容摘要。`GET /plugin-workbench/{ticket}` 消耗入口，重新核对锁定、插件启用、作用域、路径与内容；失效需从卡片重新打开，不能刷新复用票据。票据不含持久访问令牌，页面与日志不得主动传播该URL。

页面在sandbox iframe中运行，仅allow-scripts/allow-downloads，不允许same-origin、popup、表单或top-navigation；服务端页面CSP限制connect、外部资源、frame、base和object；不授予摄像头、麦克风、剪贴板、USB/串口能力。宿主不向页面注入Bearer、API助手、文件权限、引用或模型上下文。可信插件的Node执行权限不因UI隔离变成进程沙箱；这些CSP/iframe限制也不应宣传为对恶意脚本自导航等行为的完整网络沙箱。

关闭浮层即卸载文档并终止Worker；前端定时检查锁屏、登录状态、恢复作用域和已知启用状态，变化即卸载。首版没有跨标签页插件撤销推送；服务端会拒绝变化后的新入口，但已加载本地计算不能据此视作可远程即时吊销。界面本地数据由插件自行组织，文件导入/下载须用户显式操作；本扩展不提供自动写引用、知识检索或聊天控制通道。

首个使用者为私有RfToolBox 0.1.0：QAM/通用OFDM、参考EVM与离线eCPRI示例。数值/协议格式、宿主回归、真实浏览器分别验收。其知识库和标准NR/O-RAN高级能力未据此实现。

### 可选工作台工具通道（protocol 1 扩展候选，未部署）

`workbench.tools`可声明最多16个本插件可执行工具名称。未声明或空列表保留原有纯离线行为。此声明不授予新文件/网络权限；仍执行sandbox、DisabledTools、工具归属、pluginPlanPolicy及资源调度。内置工具、跨插件工具、browser-control/computer-control以及带顶层source参数的别名调用不开放。

父页面通过已鉴权的open请求取得仅由父页面持有的30分钟实例；iframe没有宿主token、实例ID或任意HTTP代理。iframe内联/打包`internal/server/web/plugin-workbench-client.js`源码后调用：

```js
const host = AideWorkbenchClient.create('your-plugin-id');
await host.connect(); // 独立HTML/旧宿主无通道时会失败，不能伪装已连接
const reply = await host.call('your_declared_tool', {path:'relative/path'});
if (reply.status === 'proposal_pending') {
  // 还未写入。父页面显示“查看待审批提案”，批准继续使用宿主既有UI。
}
```

插件页面先向`parent`发`{type:'aide.workbench.ready',protocol:1}`。父页面只接受当前iframe的source，转移一个MessagePort，并发送`{type:'aide.workbench.connect',protocol:1,pluginId,tools}`。sandbox使iframe为opaque origin，targetOrigin使用`*`只用于向已确认的contentWindow转移端口；不用origin字符串冒充来源校验。首个连接后不再给第二个文档发放端口；CSP的connect-src仍为none。

端口请求：`{protocol:1,requestId,method:'tool.call',tool,arguments,attachments?}`。requestId为1–64位字母/数字/下划线标识，以字母/下划线开头；每实例最多128次工具请求、同一时刻1次，无自动重试。前端序列化正文最多256KiB，HTTP仍限制单JSON请求1MiB，结果最多2MiB。attachments最多16项，使用既有Attachment格式；修改现有文件时必须在调用前提供附件快照，否则拒绝，不在运行后重新捕获“基准”。

返回`{requestId,status:'result'|'proposal_pending'|'error',result?,proposalId?,sessionId?,error?}`。结构化工具结果可以包含proposal字段，但只有宿主recordToolProposal验证并持久化到审批会话后才返回proposal_pending；普通result不等于原生工程已保存。通道不提供approve、apply、命令执行或任意文件读取方法。

可用观察请求：`{protocol:1,requestId,method:'proposal.status',proposalId}`，只能查询当前实例产生的提案，返回sessionId/proposalId/status/applied。父页面对最近一条提案进行约1.5秒间隔观察，变化时发`{protocol:1,event:'proposal.changed',proposalId,result}`；没有伪造原生selection/dirty事件。原生文件结果仍须插件自己的已验证回执/读回证据。

关闭、锁屏、停用、工作区身份/代次变化时父页面约300ms内关闭通道并中止HTTP，服务端对活动调用约200ms重查策略，资源出队时再次验证；取消是尽力而为，已发生的可信插件外部副作用不能撤回，超时/取消结果不确定时禁止自动重放。工具和Node插件仍为可信代码，通道不是进程或OS沙箱。

父页面“查看待审批提案”会关闭工作台并打开已有会话审批UI；批准后可重新打开插件，读取自己的原生桥回执。新实例不能通过旧proposalId查询旧实例提案。此候选没有桌面流、原生实时选择、任意WebSocket，也不放宽全局CSP。源代码/模型测试不等于已部署或真实浏览器/原生工程验收。


### 当前项目目录与插件安装目录

handler第二参提供只读值api.workspaceRoot，由宿主捕获当前授权工作区，并随call/daemon请求传递；不是模型arguments可覆盖的参数。readFile/listFiles和runPython使用此根。普通短命Node进程CWD也绑定当前本地项目。常驻进程不全局chdir，避免多调用串区；daemon需要使用本次api.workspaceRoot或helper，不能把启动时process.cwd当作当前项目。插件包脚本仍从安装目录解析，插件设置/缓存不因切换工作目录搬迁。

工作区代次/根/插件scope变化后，排队调用不运行、旧调用结果被拒绝；工作台另有取消和实例失效机制。已发生的可信插件直接文件或外部副作用不能回滚，不能将结果拒绝描述为OS隔离。SSH时本地helper根为空并拒绝访问；不会把SSH项目误当本地/workspace。引用读写沿用原有可见/可写锁，当前项目绑定不赋予额外引用权限。

### 隔离页面导出交接（可选，不授予工具/文件读取能力）

Safari对opaque sandbox iframe中的blob下载不应仅凭a.click就视为成功。页面可向父窗口postMessage发送`{type:"aide.workbench.export",protocol:1,pluginId,requestId:"export_1",name,bytes:ArrayBuffer}`并transfer该buffer。父页面仅接受当前iframe.contentWindow来源、正确pluginId、当前未撤销工作台的请求；不接受URL/路径/远程资源或文件读取指令。名称长度1–160，拒绝路径、控制符、隐藏名；数据1字节至16MiB。同一实例仅一个尚未请求保存的文件，不能静默覆盖。父页面将数据视为application/octet-stream而非执行HTML/JS。

父页面显示“保存文件”按钮，用户点击才在主页面创建下载链接。子页面只接受其直接父窗口的`aide.workbench.export.status`消息：ready=数据已准备待顶部保存；requested=已交给浏览器、需检查文件；error=交接失败。没有“文件已落盘”的回执，不读取用户Downloads来伪造成功。关闭、锁屏、停用或切区清除buffer URL，旧按钮失效。无工具manifest的离线页面也可用此导出交接，不会因此获得tool.call通道或访问令牌。独立非嵌入页面保留普通下载路径；旧宿主未确认时应报错，不静默退回已知受限的iframe路径。


### Optional host theme snapshot (2026-10-18 source candidate)

An opaque workbench iframe may post `{type:"aide.workbench.theme.ready",protocol:1}` to its parent. The active parent checks `event.source` against its own frame, then replies with `aide.workbench.theme`, protocol 1, resolved `theme` (light/dark), `palette` (blue/comfort/green), and `colors`. The snapshot contains only these existing semantic colors: bg, panel, surface, field, text, muted, line, field-border, brand-hi, brand-text, on-brand, chip-bg, selected, focus-ring, danger, warn, success. It grants no tool, file, storage or settings access and does not require workbench.tools.

The parent observes theme/palette changes for the active view and disconnects on close, scope change or lock. Clients must validate the parent source, protocol, complete color allowlist and CSS color values, then map to their own prefixed presentation variables. Unknown hosts retain their standalone appearance. Chart redraws must reuse existing results rather than rerun computations. Safari source-component checks passed for RF ordinary/native views; installation/restart acceptance remains separate.

### 长时间交互与面板分离（宿主新增能力）

工作台客户端可在 `aide.workbench.ready` 中声明 `requestIds:"monotonic"`，之后每次请求 ID 必须为严格递增的 `r_<正整数>`。父页面仅维护最高序号，重放或倒退序号即拒绝；更新后的握手回传 `capabilities:["bridge.renew","daemon.detach"]`。旧客户端仍使用原128次预算，不自动转换请求 ID。

在空闲且作用域仍有效时，父页面通过带宿主鉴权的 `POST /api/plugins/{id}/workbench/{instance}/renew` 原子轮换能力标识，并延长30分钟。旧标识立即失效，工作区、锁代次、启用状态、HTML 摘要及包身份不能改变，待审批提案归属保留；忙碌、过期或已撤销实例无法续期。新客户端父页面在100次调用或20分钟后，于下一次调用之前续期；不重试失败工具，新的能力标识不会传入 iframe。序号在当前页面整个生命周期保持递增，轮换不能重放旧序号。

声明 `daemon:true` 的插件可选择 `workbench.closeBehavior:"detach"`。普通关闭面板立即卸载 iframe、关闭 MessagePort，允许**已派发的一次调用**完成后释放桥接；浏览器断开请求本身不终止该次调用。此选项不会启动额外调用、延长310秒调用期限或改变审批。锁屏、工作区/权限变化及显式桥接撤销仍取消调用，超时/取消仍保留 daemon 原有的终止实例策略。未选择此行为的工作台保持关闭时中止调用的原行为。

`GET /api/plugins` 增加 `daemon` 布尔字段，供父页面应用上述生命周期；插件的实际常驻会话由自身后台管理，桥接续期不等于进程持久化。依赖这些能力的插件必须检查握手能力；旧宿主忽略未知字段时不能据此认定支持。

### 工作台入口

插件卡片仅提供“打开工作台”，在当前 Aide 页面内创建沙箱 iframe 与受控桥接。插件新标签页按钮和 URL 自动打开逻辑已移除；旧链接参数不再启动工作台。关闭面板仍按 `closeBehavior` 释放桥接，后台会话按插件生命周期继续运行，日志下载通过工作台顶部“保存文件”。

### Terminal SSH 工作区上下文（1.2.0 预发布）

宿主只在 Terminal 的 `terminal_session_*` 调用通过既有工作区、策略和资源调度准入后，为 daemon 传递一次性 `api.terminalContext`；其他插件不获得此上下文。`workspaceRoot` 对 SSH 工作区仍为空，普通文件插件不会回退到安装目录。Terminal 上下文包含按 SSH 主机/端口/用户/目录隔离的本地状态目录，以及当前远程路径。工作区 Shell 复用宿主已认证的 SSH 主连接，并用私有硬链接固定 Unix socket inode，避免固定公共 socket 在工作区切换后指向新主机；断开时失败，不另行认证或重试输入。

`terminal_session_files(identitySource,path)` 仅返回允许来源的文件元数据。`terminal_session_create` 新增 `auth: default|password|key`、`identitySource` 与来源相对 `identityFile`。宿主验证来源、相对路径、符号链接及64KiB上限后，将私钥经 daemon 私有上下文交给 Linux memfd；私钥不进入网页、模型参数、会话记录或磁盘临时副本。密码和私钥口令只在 OpenSSH 终端提示后输入。已停用或当前工作区 AI 不可见的来源不参与选择或读取。需要 `terminal-ssh-workspace-v1` 市场能力标记；此标记不跳过最低宿主版本和未知能力检查。

### Terminal 合并通信（1.3.1 预发布）

Terminal 将原 comm-ssh、comm-tcp、comm-udp、comm-serial 的35个工具注册为 `terminal_` 前缀，保留旧插件避免同名覆盖。工作台仍使用原受控桥接，通过已声明的 `terminal_session_comm(action,arguments)` 访问固定白名单；工具总数增加不扩大 iframe 的 HTTP、凭据或任意代码权限。普通 TCP/UDP/串口资源创建同时创建工作台会话，通信发送采用显式 UTF-8/HEX 报文和 TX/RX 日志。

宿主对所有 `terminal_` daemon 工具传递工作区上下文，新增 `communicationsVersion:1` 和每次调用的 `disabledTools`。通用分发先执行规范工具名禁用检查，运行适配器也对会话发送执行同一检查，避免绕过直接工具禁用。新插件拒绝缺少策略上下文的宿主；市场使用 `terminal-communications-v1` 能力标记阻止不兼容安装。该标记不跳过最低宿主版本或未知能力检查。

高级 SSH API 的引用私钥支持直调用 `terminal_ssh_connect` 及受控分发的嵌套参数，由宿主读取后只通过 daemon 上下文注入。API 连接要求用户核验的 SHA256 主机指纹；工作台交互式 SSH 保持 OpenSSH known_hosts。每个工作区独立持有通信模块实例及带作用域身份的资源句柄。SFTP 本地路径仅允许当前本地工作区；模拟SFTP使用独立临时根。安装包内置固定版本 ssh2、serialport、运行依赖和上游许可，控制在既有256文件/64MiB展开上限内，不调用安装脚本。

### 只读文件来源与临时快照

短命 `call` handler 可使用 `await api.sources(request)`。宿主为每次已准入调用建立私有 Unix socket；不向插件传递 SSH/SFTP 密码或私钥，不开放公共 HTTP 路由。当前支持本地/SSH 工作区及已启用且允许 AI 访问的 local/skill/SFTP/workspace-sftp 引用。daemon 暂不提供此接口，其他协议来源需单独适配。

- `{operation:'sources'}` 返回 `sources:[{id,name,type,origin}]`；ID 为 `workspace`、`context` 或 `source:<id>`，不含端点及凭据。 工作区来源额外返回不透明的 `workspaceKey`，供插件隔离本地恢复缓存；相同工作区在服务重启后保持，SSH 用户或端口不同则改变。此键由工作区身份摘要生成，不含凭据，不授予访问权限；`origin` 仍表示当前进程来源，不能用作长期缓存键。
- `{operation:'browse',source,path}` 返回有界目录条目；路径是所选来源内的规范相对路径，列表包含点文件，不跟随链接。
- `{operation:'stage',source,path,slot,recursive,maxBytes,maxFiles,maxDepth,exclude}` 返回 `{workspace,path,source}`：`workspace` 是本次调用独有的临时本地根，`path` 是其内快照相对路径；`source` 记录原始来源/路径、配置身份、文件/字节数、宿主保护路径排除项与原始时间戳未保留标志。`slot` 为唯一的 `left`、`right` 或 `path`，同次调用不能重复。
- `{operation:'localRoot',source}` 仅为本地来源返回其本地根及来源身份，供已有本地 Git 等可信插件读取；远程来源返回明确错误。

快照上限为单文件 8 MiB、同次调用合计 64 MiB、每侧/每次目录列表 5,000 条目、目录深度 32、每次来源请求 90 秒；更低的请求预算生效。达到预算、读取失败或遇到链接/特殊文件时拒绝整个快照，不返回可被误读为完整的部分目录。宿主保护路径 `.git`、`.data`、`.aide-data`、`.env`、`docker-images` 不进入快照，排除范围写入来源记录。SSH/SFTP 目录必须能无损解析，无法解析的条目使请求失败。快照只证明实际读取的字节；不保留原修改时间，不保证变化目录的跨文件原子快照，不证明远端历史、作者或可信时间。

读取前后和 handler 结束时重查工作区、来源配置、连接代次及 AI 可见性。来源变化拒绝旧结果。正常返回、取消或错误后关闭 socket 并清理临时副本。引用访问需要跨来源调度时，插件应省略旧的本地 `scheduling` 契约，使用保守的执行域独占；此 API 不是对可信 Node 插件的 OS 沙箱。

`api.runPython` 新增 `options.sourceSnapshot:true`：远程工作区下仅显式启用该选项且宿主提供来源 API 时，允许在本地插件安装目录启动 Python，并由插件将宿主提供的临时根传给受限比较器。`api.workspaceRoot` 仍为空，`readFile/listFiles` 仍拒绝把 SSH 回退成本地工作区；其他插件调用行为兼容。

`workbench.standalone:false` 为旧版宿主兼容元数据；当前宿主已全局取消插件新标签页入口，统一内嵌打开。

工作台连接消息可包含经过宿主白名单过滤的 `selection:{left,right?}` 提示，每侧只有 `{source,path,directory}`。它不包含鉴权、端点、用户设置或文件内容，也不改变工具权限。Compare 的工作区/引用右键菜单可设置左侧，再选择另一来源作为右侧，或直接比较两个同类条目。来源切换、禁用或不允许 AI 访问时，插件必须重新验证并拒绝无效提示。

### 原生项目文件工作台入口

workbench.fileExtensions 可声明最多 8 个不重复的小写扩展名，例如 [".pp"]；扩展名以点开头，后接字母和最多 11 个字母/数字。当前宿主在文件面板打开 .pp 时，查询启用且唯一声明该扩展名的工作台，使用原有 selection.left 传递来源与相对路径。缺少处理器时保留文本打开；多个处理器时提示用户从插件面板选择。此声明不授予文件访问、写入或工具权限；工作台仍需显式调用已声明工具并经过宿主来源检查。
## 2026-10-21：按扩展名注册文件处理器

可选 `workbench.fileHandler = {extensions:[".obj",".stl"],priority:10,editable:true}` 注册插件优先预览/编辑。仅宿主绑定所选文件的工作台入口提供document方法，普通工作台不自动获得文件访问权；修改先暂存，经宿主用户点击才保存。停用/未声明/不支持来源/回退继续原生。完整协议、边界、流式API与示例见[插件文件处理器](architecture/plugin-file-handlers.md)。

## 2026-10-21：商店能力路由

商店 listing 可提供有界 `routing:{keywords,extensions,summary}`，每次上架/升级构建自动生成索引；宿主默认后台更新，在本地匹配需求，只披露选中插件的工具 schema。`route_plugins` 可发现能力，用户单独开启自动安装后可安装缺少的官方公开兼容插件；不会覆盖已安装插件或重新启用已停用插件，也不会扩大执行权限。完整接口、授权与缓存边界见[插件路由](architecture/plugin-routing.md)。

## 模型自动选型声明 · protocol 1 候选扩展（2026-10-22）

manifest 可声明 `"modelRouter":{"protocol":1,"catalog":"capabilities.json"}`。档案为包内单个 JSON 文件（不允许路径、符号链接逃逸；最多128KiB、64个模型、16个角色规则）。宿主导入时验证结构，执行时在工作区启用插件中重新读取；仅一个有效声明可生效。该扩展当前为新源码候选，v0.1.17.5-RC2 尚未包含。

档案包含 protocol/revision/checkedAt、sources[{id,url,assessment}]、models[{id,aliases,wireFormat,contextWindow,vision,tools,structured,weights,source}] 和 rules[{role,keywords}]。weights 的 coding/reasoning/writing/general/fast 为1–5维护者权重，不冒充官方评测得分；source 引用档案内 HTTPS 来源，不进行运行期请求。插件设置可提供 aliases（实际API名到档案ID显式映射）、preference=quality/balanced/fast。

`GET /api/config` 与 `GET /api/plugins` 返回非秘密 modelRouter 状态；会话选择新增 `modelRouter:"auto"`，保留 model 作为手动回退。普通模型选择传 modelRouter 空串退出。宿主只向已配置的 OpenAI-compatible 地址发送请求，密钥从原 SecretVault 按提供商获取；插件不接收凭据。1X按请求阶段重选，多倍率为原有只读分析分路逐路选型；旧插件、普通会话和左侧参数策略不变。禁用/卸载或档案损坏保留回退模型。Task.modelRoutes 最多64项非秘密选择记录，ParallelPart 增加 model/provider；实际用量仍由每次请求归属。

模型窗口取用户配置与档案上限较小值（未配按65536）；图片需宿主确认vision，工具/结构化显式unsupported优先。候选不足不静默扩大上下文或能力；API错误不自动轮试其他厂家。未知模型不作模糊猜测。官方API别名可能在提供商侧漂移，离线快照仅代表核对日，不能验证实际权重版本或实时最优性。

wireFormat 可为 deepseek、openai-reasoning 或 openai-compatible（空值同通用兼容）。它仅选择宿主内置的有界参数转换，不声明新的传输协议：GPT 推理格式使用 max_completion_tokens 并去掉 DeepSeek thinking 和不兼容采样参数；通用网关省略厂商推理扩展。手动调用不应用该适配。请求间停用插件恢复会话手动回退，已发送请求继续服从原取消规则。

## 文件右键菜单入口

Compare 的文件/文件夹右键入口为显式选择加入：插件设置 `fileContextMenu` 必须严格为布尔 `true`，并且插件已启用、无加载错误且具备 protocol 1 工作台。缺省、关闭、未安装、停用、卸载或无有效工作台均不添加 Compare 菜单项。Compare 插件通过自己的 `settingsSchema` 提供「将 Compare 添加到右键菜单」勾选框，默认关闭。

宿主先显示原生文件菜单，异步读取最新插件列表后添加符合条件的入口；插件查询失败不影响原生下载/复制等功能。点击比较或设置左侧时再次校验，跨工作区、登录身份或锁屏恢复代次的过期响应不再接受。关闭此开关不影响从插件面板手动打开工作台。此设置控制 UI 入口，不替代服务端工具、来源与工作区权限。其他插件目前未直接注册该文件右键菜单；原生文件功能不依赖插件启用。

## Provider support declaration (protocol 1)

A no-tool plugin can declare `providerSupport: {"protocol":1,"catalog":"provider-support.json"}` in its manifest. The catalog is validated on installation and read within the installed plugin directory. It supplies exact HTTPS endpoint matches, model names, CNY token rates and optional tier/peak calendar data. Only enabled, healthy, uniquely matching plugins apply; invalid or ambiguous data falls back to manual pricing. No plugin code receives model or billing credentials.

The host keeps per-call rate and key-identity snapshots. Manual provider/key/model overrides take priority, with provider/model inheritance, then plugin pricing, legacy model pricing and the global estimate. Balances are account values, not key-specific wallets. The balance adapters support same-host model Bearer queries and the fixed read-only China-site Alibaba QueryAccountBalance RPC with separately encrypted RAM credentials. Full boundaries, migration and API details: [provider supporter design](prd/2026-10-22-provider-supporter.md).

### TTS Tools 原生桥接（2026-10-23）

独立安装的 `tts-tools` 插件可以声明 `tts_voices`、`tts_generate`，由宿主固定桥接实现。仅工具归属为该启用插件时允许调用。插件不读取或设置密钥；宿主使用所选语音生成模型的提供商保险库凭证。此桥接与助理的默认朗读提供商、音色、API Key 完全独立。`tts_generate` 接收 `text`（1–600 字）、可选 `model`、`voice`、`format`（mp3/wav），当前仅非实时 Qwen-Audio-TTS 3.0/3.1 flash/plus。省略模型时需要支持 speech 评分的 Auto 路由档案。

生成记录在付费提交前保存，同一任务仅生成一个脚本/音色/格式组合；已提交且结果不明确时禁止自动重建。下载会验证服务商存储域、真实格式与文件大小，再写入绑定的本地工作区 `generated/speech/`。read-only 沙箱禁止生成写入，语音查询仍可用。旧宿主执行插件 handler 时明确报不支持，不伪装成功。该工具不实现声音复刻或实时麦克风；默认朗读继续使用既有 TTS 设置。

## Project briefing message extraction (local candidate)

A workbench may declare `projectMessages: true` without declaring any tools. `projectMessages` or `mobileCapture` independently creates a scoped bridge; a page declaring neither capability, tools nor another bridge feature receives no bridge. Undeclared tools/capabilities remain unavailable. The parent advertises the `project.messages` capability and accepts `client.projectMessages({projectId, projectName, path, fromDate, toDate})` through its scoped MessageChannel. Dates use Asia/Shanghai, inclusive, with a maximum 31-day range. `path` is optional and must be a safe workspace-relative `.pp` path. This capability is not an AI tool and is unavailable to undeclared, disabled, expired, locked, or changed workbench instances. Request replay and concurrent-call rules remain in force.

The host reads ordinary, non-deleted Aide conversation messages whose source run belongs to the current workspace and carries a complete explicit reference: `[project-id:PROJECT_ID]` (exact ID of at least eight bytes), `[project-name:EXACT PROJECT NAME]` (case-insensitive full value), or an exact workspace `.pp` attachment matching `path`. Ordinary words, incidental substrings and ID prefixes/suffixes do not establish membership. Existing runs containing only unmarked project names are excluded; attaching the precise PP file remains supported. Reference values are single-line; names containing marker delimiters can use the attachment route. Matching only a conversation title does not include its unrelated runs. Assistant coordinator sessions and tool/reasoning messages are excluded. This is reference-based extraction, not semantic classification or an assertion that all project discussion has been found. No messaging-center/IM search, provider call, sending, session mutation, or automatic task execution occurs.

New conversation messages retain a local `createdAt` timestamp. Legacy messages use their source run's creation time and are labeled accordingly; messages without a valid time are excluded. Non-string historical timestamp extensions remain readable. Local timestamps and source-run metadata are stripped at the provider boundary. Response rows retain source session/run IDs, role, day/time and matching reason. At most 100 messages and 48,000 content bytes are returned; long messages are excerpted to 1,200 runes and the response reports omitted rows. Consumers must expose truncation and current-vs-historical reporting limits.
