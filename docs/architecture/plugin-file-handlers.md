# 插件优先的文件预览与修改

用户启用插件后，工作区及受支持文件引用优先使用关联插件预览/编辑。宿主只增加一次通用接口，后续扩展名由独立插件声明，不需要逐格式修改原生分支，没有新增默认捆绑插件。

BrdPlan 原生编辑会话另持有宿主票据。沙箱设置成功保存为只读时，宿主撤销全部旧原生编辑票据，取消其现有 HTTP/WebSocket 代理并请求后台关闭对应 lease；切回可写不会恢复旧票据，需要重新打开。设置保存失败不会发布只读状态或撤销现有可写票据。撤销与断连不等于工程已保存；后台关闭不可达时仍保留保存状态未知及持久编辑标记的保护。宿主关闭也会撤销票据并等待有界的后台关闭请求。此约束不代替原生编辑服务自身的文件权限和保存确认。

## 注册与选择

```json
{"id":"mesh-preview","name":"模型预览","main":"index.js","enabled":true,
 "workbench":{"protocol":1,"entry":"viewer.html","title":"模型预览与编辑",
   "fileHandler":{"extensions":[".obj",".stl"],"priority":10,"editable":true}}}
```

扩展名须以点开始、小写、不含通配符或路径，最多32项；优先级为 -100 至100 的整数。同格式按优先级降序、插件ID升序选取。匹配文件名不区分大小写；`.bak` 按原格式预览但禁止编辑。`GET /api/plugins/file-handlers?path=...&root=workspace&source=...` 仅返回启用、声明/入口有效、来源可访问的候选。

文件列表和 `#file=` 引用页面均在原生读取/渲染之前选择插件。标题栏允许换其他候选或「工作台原生预览」。启用插件后下一次打开文件优先使用插件，不打断已有原生编辑草稿。活动插件停用/删除/升级或声明变化在约2.5秒检测周期内回落原生；锁屏或工作区变化只撤销旧预览，不把旧文件带到新范围。临时网络观察失败保留当前草稿，实际读写仍由服务器逐次检查权限。

入口无法建立桥时15秒回落原生。已建立桥的插件应在解析失败时调用 `failDocumentPreview()`；不能自动判断任意第三方渲染器是否逻辑卡住，用户仍可手动切原生。没有原生渲染器的自定义格式回落到既有文本/下载或不支持提示，不伪造原生建模能力。

## 文件能力与客户端

普通工作台不会自动获得文件能力。宿主用 `POST /api/plugins/{id}/workbench/open` 的 `document:{path,root,source,readOnly}` 绑定所选文件；其路径、根、来源配置、插件包、工作区/锁屏代际均由服务器维护。iframe 保持无同源权限、无网络连接权限，不接收 bearer、实例键、绝对路径或主API。现有工具能力不因文件桥而提升。文件处理实例额外允许编译内嵌WebAssembly，供本地三维/建模引擎使用；不开放JS eval、外部脚本/网络或同源权限。普通无文件工作台的CSP保持原有策略。

在自包含 HTML 中内嵌新版 `internal/server/web/plugin-workbench-client.js`：

```js
const client = AideWorkbenchClient.create('mesh-preview');
const info = (await client.documentInfo()).result;
const range = (await client.readDocument({offset:0,length:524288})).result;
// range.data 为Base64，另有 size、offset、length、eof。
```

- `documentInfo(asset?)`：大小、完整SHA-256版本、可编辑状态、块大小。摘要流式计算，大文件会产生对应I/O成本。纯预览可用 `documentInfo('', {metadataOnly:true})` 只取元数据，不预读整文件；编辑器再按需取得完整版本。
- `readDocument({offset?,length?,asset?})`：每次最多512KiB，支持大文件偏移；asset相对于主文件目录，仅同目录/子目录资源，禁止URL、绝对路径、`..`、隐藏路径和符号链接（含中间目录）。不能切换根或任意路径。
- `pluginAssetInfo('assets/kernel.wasm')` / `readPluginAsset({asset:'assets/kernel.wasm',offset,length})`：同一只读分块协议，但固定到本插件包的assets/目录。可加载较大的WASM/脚本/纹理，不要求塞进2MiB HTML；仍受原安装包体积限制。禁止读取其他插件、隐藏文件、入口代码、`..`及符号链接。assets应只放公开静态资源，不放凭据。WASM可用Uint8Array编译，脚本/图像可转Blob使用，不能因此绕过网络限制。
- `setDocumentDirty(true)`：通知有未保存修改，普通关闭或切文件时可取消离开。
- `failDocumentPreview()`：请求原生回退。

复用本地 `os.Root`、SSH工作区/SFTP随机访问后端；只读引用、后端引用根、`.bak` 不可保存。知识库固定快照身份、MCP、链接及其他非随机访问驱动继续原生，不扩大其权限。

## 编辑和人工保存

```js
await client.saveDocument(new Uint8Array(changedBytes), info.version);
// 只是暂存，原文件未改变。
client.onEvent(e => {
  if(e.event==='document.saved') info.version=e.result.version;
});
```

大文件可用 `beginDocumentSave(totalSize,version)`、`writeDocumentChunk(offset,bytes)`、`requestDocumentSave()` 流式输出，无需组装整份Uint8Array；`abortDocumentSave()` 放弃暂存。所有调用单飞，插件UI须排队，不能同时发多个请求。

宿主收到完整暂存后显示「保存到原文件」。iframe不能调用提交方法，只有宿主真实点击才发送受鉴权的 `document.save.commit`。这是手动文件编辑，不代答模型审批或为模型增加自动写入工具；直接持有宿主bearer的API客户端沿用既有手动写权限。

每块最多512KiB，顺序偏移，必须声明总大小，不设置64MiB总文件上限。所有活动暂存尚未写入的预留共同计算，保留64MiB磁盘余量。每实例一份0600私有暂存、最多8实例；关闭、过期入口清理及正常App.Close移除未提交暂存。桥每100请求或20分钟附近由宿主续期，保留原文件/暂存绑定、旧键撤销。

提交重新检查插件/来源RW/工作区、原文件完整SHA，复制到目标目录临时文件，校验回读摘要，再核对原版本并原子替换。内部写操作由filesMu串行，发布时固定范围。外部进程仍是普通编辑器的乐观并发语义，不声称跨进程文件系统事务。保留POSIX模式，不承诺所有权/ACL/xattr迁移。

SFTP缺少posix-rename扩展时拒绝覆盖、保留原文件。fsync扩展可用时同步；否则按既有传输语义验证回读，不能承诺远端断电持久性。Markdown沿用原生历史/备份和原生文本大小限制；写后归档失败明确报告已保存及警告，不静默重试。保存提交后范围立即变化时提示核对原文件。

不承诺停用、锁屏、关闭或崩溃后恢复第三方界面的内存草稿。强制撤权提示未保存修改不会写回；通信中断可能遗留未发布的目标目录临时文件，不能自动当作已保存。服务崩溃遗留私有暂存不重放，管理员可在确认没有活动实例后清理。插件本身仍是受信Node代码，不是OS沙箱；网页文件桥不能阻止原生插件代码使用其已有权限。

## 示例与验收

[OBJ示例](../../examples/file-preview-plugin/README.md) 演示线框旋转、读取、编辑、分块暂存。它是协议示例，不是完整CAD，也不默认安装。证据见[验收记录](../reviews/2026-10-21-plugin-file-preview/verification.md)。本次源码支持不代表运行镜像已经包含接口。

兼容原有 `workbench.fileExtensions` 的 `.pp` 项目入口：通用 `fileHandler` 优先匹配，无匹配时继续既有项目工作台流程；用户明确选择原生时跳过两种插件入口。两种声明各自保留原有权限边界。

## 工作台内选择来源与另存为（fileAccess v1）

普通工作台可显式声明 `fileAccess:{protocol:1,extensions:[".demo"]}`。这不登记自动预览器；`fileHandler` 仍控制文件面板的自动关联。只有宿主确认该声明后，握手才包含 `files.select`。旧宿主没有此能力时，插件须保留旧入口或明确提示更新。

内嵌客户端 `host.files(args)` 使用原受控 MessagePort，方法固定为 `files.select`：

- `action:"sources"`：列出工作区和已启用的 local/skill/sftp/workspace-sftp 引用，仅含 id/name/type/writable。引用ID为 `source:<id>`；不包含路径端点、凭据、宿主令牌或实例键。这是手动文件能力，不是模型工具；AI可见性不会因用户手动打开文件而改变。
- `action:"browse",source,path,offset?,limit?,query?`：来源内目录，默认200项、最多500项分页，每目录最多扫描5000条。只列目录及声明格式，拒绝链接，不列隐藏项；不支持的来源继续使用原生入口。
- `action:"target",source,path,mode:"save"`：只检查目标是否存在、读写状态、大小和SHA版本，不改变当前文件绑定。
- `action:"bind",source,path,mode:"open"|"save",version?`：绑定已有文件或新保存目标。保存模式必须给出刚核对的 version，空串表示目标必须不存在；恢复旧绑定时也应传原版本，不能静默刷新覆盖基线。扩展名限声明列表，引用变更、只读、非法路径和未完成暂存均会拒绝。
- `action:"clear"`：仅在没有待保存暂存时解除绑定。

绑定后沿用 document.info/read/save.begin/save.chunk/save.request；手动选择只授权该文件，不开放同目录任意 asset 读取。新目标允许空版本作为“尚不存在”的基准。iframe仍不能发送 document.save.commit；真实用户点击父页面“保存到所选文件”才提交。回执增加 source/path，便于区分数据库保存与其他导出。

现有文件保留SHA冲突检查和原子替换。新文件使用既有transfer排他发布，不覆盖后来出现的目标；默认0600权限。保存前后重新核对来源、工作区、锁屏代际、包摘要和写权限。旧文件处理器、不声明fileAccess的插件、同目录资源及现有手动项目保存接口保持原语义。该能力不扩大可信Node插件的OS权限，也不证明新源码已经进入正式镜像。
