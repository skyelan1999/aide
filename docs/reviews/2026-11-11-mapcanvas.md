# 栅图 MapCanvas 公用版集成与发布

任务 mapcanvas-public-plugin-20261111；集中跟踪 [#841](https://github.com/skyelan1999/aide/issues/841)。宿主基线 93ac3ce0，工作树开始时干净。独立市场检出使用远端 main e9fc145，保留原市场工作树未跟踪的 compare 2.json。

原始 0.4.7 源码 ZIP SHA-256：aeb957e364128314f81ac397cb2f7b47cc29ef5c72aaaca4fa927bf135bcdc47。仅把 plugin 有效源码改名整理，宿主增量经当前源代码人工集成；没有直接执行用户文档中的安装命令。原安装项 ros-map-editor 保留。

新名称：栅图 MapCanvas；ID mapcanvas；首次公开版本 0.1.0。独立工具 mapcanvas_sources / mapcanvas_browse / mapcanvas_read，MIT。插件 ZIP 只包含明确白名单代码、使用说明、许可证及小型合成地图，不含实例设置、用户地图、宿主补丁或凭据。

已发现并修复 P2 像素整数进入 Uint16Array 前未检查溢出的问题。7 项 Node 核心/工具回归通过；宿主 Map/Document/Workbench/Market 聚焦 Go 回归 21.002s 通过。实际 ZIP 安装、页面、市场与远端产物状态将在完成后记录；本段不表示已发布。

## 当前本地实例验收

沿用 localhost:9999 的现有 Aide / aide-aide-1；没有另起 Aide 服务，保留现有 viewer 容器。安装 ZIP 15,326 字节、SHA-256 7fe1dc086c1168f7cadb6260f99f747e05b1233b5ce3f475d1acfcbb02fdd779，与两次确定性打包一致。

原生 Aide 插件列表实际显示并启用 mapcanvas 0.1.0，ros-map-editor 0.4.7 仍保留。新工作台成功加载来源，导入 32×24 合成 PGM 和 PNG，画墙、撤销、重做正常。插件软盘只准备草稿，磁盘 PGM 摘要不变；宿主确认后摘要改变，YAML 与 posegraph 摘要不变。PNG 保存后读回保持 PNG 和32×24尺寸，配套 YAML 不变。宿主原生下载的 ZIP CRC 全部通过，内含 YAML/PGM，栅格摘要与保存后原图一致。

临时只读来源导入、画墙与生成导出成功；保存原图明确报只读并要求另存，未写入。测试后删除本次临时来源，保留原有来源配置，关闭测试工作台。只编辑独立缓存中的合成地图，未修改用户原地图。

## 回归与构建

- 聚焦 Go：Test(MapWorkbench|MapDocument|PluginDocument|PluginFileHandler|PluginWorkbench|MarketSource)，21.002s，PASS；使用当前既有容器及官方校验过的 Go SDK。
- Node 插件核心7项、宿主 workbench-tabs8项、client5项 PASS。
- quick 初次发现主线已有34个英文翻译缺失，以及旧测试没有载入 session-timeline/新 compaction helper。补译并更新测试夹具，保留安全文本、队列、会话隔离及发送延迟断言；按当前产品时间顺序检查插话。
- 最终 quick PASS，内容指纹 c21718f876763f0323ec391ade7a26a86d641c0efbd3f0e09b30c0a4e148c4a1；build.command 对当前源码执行 vet/build 并更新同一实例，健康接口 status=ok、integrity=degraded（dirty 源码构建）。完整 Aide Delivery/全插件双架构矩阵、Windows/Linux 原生主机、外部机器人及硬件验收 NOT_RUN，本次不是新的 Aide 安装器发行。

## 回滚

禁用 mapcanvas 可撤回插件入口，原 ros-map-editor 不受影响。宿主源码可回退本次集成提交并依既有 build.command 流程重建；用户地图无迁移。市场使用不可变版本 ZIP 和 SHA；旧宿主不支持 map-image-document-v1 时拒绝安装本插件。合成验收文件保留于独立缓存供复核。
