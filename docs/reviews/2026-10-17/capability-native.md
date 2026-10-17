# 完整工作台能力仪表验收

任务：[capability-native-20261017](../../tasks/capability-native-20261017.json)。继续原目标第7项，使用现有 Safari `https://localhost:9999/` 标签和当前 e729 镜像；产品代码 ba9e5a3b，源码指纹 3e5b6439ba07。本批无产品变更，不调用模型、不重新构建镜像。

## 直接观察

初始31张能力卡均为“未检查”，打开设置没有自动启动探测。在“无障碍 → 接口测试”分别点击工作空间、技能插件及 pypdf/docx/openpyxl/pptx 的“检查此项”。六项实际页面均显示通过；独立鉴权API读回的状态、时间和消息一致。工作空间明确提示“根目录读取通过；未检查写入或文件操作”，写入仍显示“尚未实测”。

插件显示实际入口 SHA-256 f474be4253920c7f9f4bedb3300ba247120031b8ab45b368792184ee5dbd1d6b，与运行容器工作根目录的 plugins/skill/manifest.json 所指定 index.js 的实际哈希一致；提示明确为“未执行插件或工具”。四种文档依赖实际完成隔离Python导入，页面不把它表述成具体文件提取或渲染通过。TCP/UDP/串口/SSH通讯四个停用插件的检查按钮禁用，未改动启用状态。

原生点击后会自动滚动到操作卡片，长列表下部控件可通过设置正文滚动到达。实际截图已核看，openpyxl/pptx状态、耗时、范围说明均可读。私有原始AX/API与截图在 `.agent-state/capability-native-20261017/`；脱敏[证据](capability-native-proof.json)保留具体收据。

## 范围与保全

仅检查本地工作空间读取、一个插件入口和四个解析器依赖。未执行模型列表请求、生成请求、插件工具或SSH探测，也未实测写权限。先前插件生命周期API证明与此次页面入口读取证明分开，不能合并成任意工具完整通过。

受保护目录 config/auth/secrets/sessions 共22个文件，运行镜像、挂载和网络端点在检查后逐项一致。没有新增浏览器标签。初次独立文件哈希检查曾误以为插件位于 /app、清单名为 plugin.json；按实际 AIDE_WORKSPACE 和 manifest.json 修正后核对通过，没有据此修改产品。

五分钟自然到期后，页面自动更新为“检查已失效”，无手动重读。API确认六项原结果均过期，原检查时间保留。关闭设置后原#39会话、引用根目录及标签保留；再次核对全部22个保护文件（含14会话）及镜像/挂载/网络端点一致。总目标仍未完成：SSH工作区模式、审批/成果舱/知识时光完整浏览器联动等仍需推进；Windows由用户验收，真实向量测试暂缓。


An additional existing local reference root check passed in native Safari and API. This proves directory listing only, not individual file body readability; no source configuration was changed.
