# Office 插件：Collabora Online 与 Aide AI 集成

用户明确要求将 Collabora Online 接入 Office 插件，提供 Office 文件的编辑、预览及 Aide AI 能力；补充要求是人工仅预览或不活跃时，不能持续阻止 AI 修改。本次直接实施，不涉及购买商业订阅或公开发布。

## 产品行为

在原有文件弹窗和独立文件页使用 Collabora，保留 Aide 既有布局。默认以预览打开，用户可切换编辑、保存、将已保存文档或选区交给 AI，以及切回离线预览。DOCX/XLSX/PPTX 是主验收格式；DOC/XLS/PPT 和 ODT/ODS/ODP 仅在所连接服务 discovery 声明对应操作时启用。原有 DOCX 批注、XLSX 网格与静态演示预览作为离线备用。

Office 插件设置提供启用开关、浏览器服务地址、后端内部服务地址和服务回调 Aide 的地址。Collabora 是独立进程/服务；插件 ZIP 不携带其二进制或商业订阅。浏览器地址必须使用浏览器可访问的有效 HTTPS；容器内部地址可以使用受控内部 HTTP。地址变更后刷新 Aide，使 CSP 更新。

## AI 读写

新增 office_document_read、office_document_edit，保留生成、批注、原文/RAG 检索工具。读工具提供最多 200 块/60000 字符的分页内容、精确块 ID、可编辑标记和文件 hash。写工具要求当前 hash 以及每块 expectedText，最多 100 项，全部验证后原子替换。直接结构化 AI 修改覆盖 DOCX/XLSX/PPTX；旧二进制与 ODF 请先在编辑器另存为 OOXML。段落替换使用原首个文字运行格式，复杂字段、对象、修订等不猜测写入。图片、图表等未改包成员保持原始字节。

来源必须对 AI 可见；只读引用保持只读。任务工作区、来源配置、锁屏代次、文件版本都在提交前重查；read-only 沙箱不得执行写工具。手工 WOPI 回调使用随机文件能力令牌，不能使用 Aide 全局令牌。文档文本仍是待处理数据，不作为系统指令。

## 人工与 AI 协作

预览模式不占写租约。编辑模式使用 Collabora Get_User_State 活动秒数与修改状态；30 秒没有活动后先保存，确认保存后释放活动状态。活跃或未完成空闲保存时，AI 精确修改返回可重试提示。心跳断开后活动状态最多保留 45 秒；8 小时能力过期后需重新打开。

AI 提交后标记旧编辑器需刷新；空闲且已保存的页面加载新的 broker 身份，旧内存版本再次保存会收到 409，不能覆盖新文件。该保护不承诺阻止文件系统外部程序写入，外部变更同样触发版本冲突。WOPI 锁用于编辑器协议，与人工是否活跃的 AI 协作租约分别处理。

## 验收和交付

使用隔离合成文件、真实 Collabora CODE 服务、真实浏览器验证打开/编辑/保存与读回内容；同时验证只读、活动、空闲、失效令牌、来源撤销、过期任务和冲突。CODE 用于本次集成验证，不能据此声称获得企业订阅、生产 SLA、生产容量或跨平台验收。大型生产使用应独立进行容量、TLS、身份和故障恢复验收。

参考：[Collabora SDK](https://sdk.collaboraonline.com/)、[WOPI PutFile](https://learn.microsoft.com/en-us/openspecs/office_protocols/ms-wopi/42d0e5ae-074f-4d11-9d9b-706d843fad9c)、[MPL 2.0](https://www.mozilla.org/en-US/MPL/2.0/)。
