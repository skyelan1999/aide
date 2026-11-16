# Collabora Online Office 插件集成验收

任务：office-collabora-20261116。用户要求集成 Collabora Online，使 Office 文件可预览、编辑，并向 Aide AI 开放；补充要求是人工仅在预览或不活跃时不能持续挡住 AI。宿主基线 da8fdc95，原有本地改动保留；本次没有提交、推送、发布或替换日常运行实例。

## 完成内容

Office 1.2.0 候选提供 Collabora 启用/地址配置、六项原生工具和独立安装包。Aide 提供 discovery、文件能力会话、WOPI CheckFileInfo/GetFile/PutFile/锁操作、活动心跳及结构化文档读写。现有文件弹窗和独立查看页增加预览、编辑、保存、文档/选区交给 AI 和离线预览，不改变主布局。直接 AI 读写覆盖 DOCX/XLSX/PPTX，其他格式的网页操作取决于所连接服务 discovery。

只读预览不占 AI 写租约。编辑模式查询 Collabora Get_User_State（Elapsed 单位为秒），30 秒没有活动后先确认保存再释放；未完成输入时显式保存会提交输入并等待回执。活动心跳有效 45 秒，能力最多 8 小时。AI 写入后空闲页面打开新 broker，旧内存版本保存不能覆盖新文件。新 AI 工具、既有原生批注/XLSX 保存和普通文件写工具均检查相关人工活动；任意外部命令/程序不作为受此租约约束的承诺，版本比较仍保护 WOPI 写回。

文件能力令牌不包含 Aide 全局令牌，浏览器以 POST 表单提交，接收消息核对 origin 和 iframe 窗口。回调重查来源、工作区、权限、锁屏代次及文件 hash；文件写入有界、原子且带最终提交前重查。结构化 AI 批量修改要求最新 hash 与逐项 expectedText，保留原媒体包成员；复杂字段/对象不猜测修改。

## 实际验证

- 真实 Collabora CODE 26.04.6.1，镜像 digest `sha256:95f29f1c0a1778999125f53f058d968fe0afb37c95e32ebfb9f3444140faebd2`。使用隔离合成项目和真实 App HTTP 测试宿主，没有用户真实文档或付费模型调用。
- Chromium 有头浏览器实际打开 DOCX/XLSX/PPTX，使用画布键盘输入，WOPI 保存后独立结构化读取核对。XLSX B2 保存为 84；PPTX 标题包含 FINAL。保存反馈缺失暴露输入状态尚未提交的问题，最终实现强制提交并等待保存确认；最终 Excel/PPT 页面已复验。
- Word 选区回填任务输入框；文档加入工作区附件。实际 plugin-surface 六项工具均 executable；原生 AI 读/写工具调度集成测试通过。
- 只读预览时 AI 修改成功，页面随后换用新的 broker；活跃人工编辑返回 HTTP 409；只读 context 来源的编辑/保存按钮禁用，写接口拒绝。
- `server-final2.test -test.run '^Test(Collabora|OfficeDocument)'` 通过。覆盖能力/来源/任务撤销、过期、锁冲突、只读、活跃/空闲、三格式 AI 修改与旧 hash 拒绝、真实原生工具调度和 read-only 沙箱；显式浏览器宿主测试在普通回归中按设计跳过。
- `go test -race ./internal/server -run '^TestCollabora(Capabilities|ReadOnly|Locks|Configuration)' -count=1` 通过。相邻原生编辑器、Office 生成/XLSX 编辑/批注回归另次通过。
- Python `python3 -m unittest test_office_document test_xlsx_patch test_docx_comments`：9 项通过；包括整批失败不写入、保留媒体、分页推进、超长块截断、复杂字段拒绝及 Collabora 嵌套格式可编辑。
- JS 语法、i18n 与占位符、文件打开归属/路由、文档链接和 diff 检查通过。1440×1000 桌面和 390×844 窄屏宿主控件及 iframe 边界检查通过；窄屏文档本身仍由 Collabora 缩放和滚动查看，不声称像素排版一致。

## 镜像与安装包

源码内容指纹：`dda878843c13be9d147a0ff684780d1f8ee8b037e94e635765677d10f7cb7729`。候选镜像 `aide-collabora:candidate-20261116`，身份 `sha256:0fd1da9cc15840ab7e8c24631a0253582931786c6e7385cb40e67124ea6014ee`。镜像以保留的 Aide 运行依赖为基础，替换从当前源码编译的生产入口及 Office 脚本；这是本地候选构建，不是正式 Delivery/双架构发行。新入口携带基线和 dirty 指纹，没有创建新版本号、tag 或 Release。

在候选镜像的独立 TLS 实例上检查 health 与 integrity 均为 ok，通过真实 `/api/plugins/bundle` 接口安装最终 Office 1.2.0 ZIP（HTTP 201），配置服务地址并确认六项 AI 工具登记，文档读回正常。

ZIP SHA-256：`35bd20c0be79688ebec6dbbd6be23c74de2036ca67c1f82d5d5538e1403d98a8`。用户交付目录为 Downloads/Aide-Office-Collabora-20261116；其中包含 ZIP、SHA256SUMS、使用说明。仅安装插件不能升级旧宿主；需要按现有 Aide 升级流程同步更新主程序。市场仍为原公开版本。

## 限制与回滚

没有进行正式全量 Delivery、商业订阅、生产 TLS/容量/故障恢复、全部宿主平台或旧 DOC/XLS/ODF 往返验收。输入仍受 64 MiB、Office ZIP 128 MiB 解压及既有 Python 解析限制；AI 每页最多 200 块/60000 字符、每批最多 100 项。段落替换继承首个文字运行的格式，不声称复杂 Office 排版无损编辑。

当前日常 `aide-aide-1` 未替换，生产插件未升级，原服务和数据保留；无需生产回滚。测试浏览器已关闭，具名隔离测试容器停止并移除；测试镜像和脱敏结论保留。插件开关关闭即可恢复既有离线入口；以后实际部署时仍应使用现有版本、备份与恢复流程。

私有执行证据保存在 `.agent-state/office-collabora-20261116/verification.json`、`delivery.json`、`image-smoke.json`、`final2-go-tests.log` 及截图中；不公开能力令牌和浏览器凭据。
