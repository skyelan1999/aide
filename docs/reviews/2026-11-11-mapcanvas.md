# 栅图 MapCanvas 公用版集成与发布

任务 mapcanvas-public-plugin-20261111；集中跟踪 [#841](https://github.com/skyelan1999/aide/issues/841)。宿主基线 93ac3ce0，工作树开始时干净。独立市场检出使用远端 main e9fc145，保留原市场工作树未跟踪的 compare 2.json。

原始 0.4.7 源码 ZIP SHA-256：aeb957e364128314f81ac397cb2f7b47cc29ef5c72aaaca4fa927bf135bcdc47。仅把 plugin 有效源码改名整理，宿主增量经当前源代码人工集成；没有直接执行用户文档中的安装命令。原安装项 ros-map-editor 保留。

新名称：栅图 MapCanvas；ID mapcanvas；首次公开版本 0.1.0。独立工具 mapcanvas_sources / mapcanvas_browse / mapcanvas_read，MIT。插件 ZIP 只包含明确白名单代码、使用说明、许可证及小型合成地图，不含实例设置、用户地图、宿主补丁或凭据。

已发现并修复 P2 像素整数进入 Uint16Array 前未检查溢出的问题。7 项 Node 核心/工具回归通过；宿主 Map/Document/Workbench/Market 聚焦 Go 回归 21.002s 通过。安装、页面、市场与远端产物的后续验证记录如下。

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

## 已发布产物与发行回归接入

- [插件源码](https://github.com/skyelan1999/aide-pkg-MapCanvas/tree/fb457bd23d5787480bb1c0a622515cbaf945b923)，Node18/22 CI通过。
- [插件 0.1.0 Release](https://github.com/skyelan1999/aide-pkg-MapCanvas/releases/tag/v0.1.0)，ZIP及SHA256SUMS.txt均已上传；重新下载的ZIP摘要与本地安装包相同。
- [宿主核心集成 8214743c](https://github.com/skyelan1999/aide/commit/8214743ce2b3c19987e0b494d9b1e336af7e63ae) 已推送main，GitHub Aide CI quick/go/gate通过。按用户本次直接push授权推送，GitHub报告使用了维护者的PR/状态检查规则绕过；这不替代随后CI通过的证据。
- [市场 bda968b](https://github.com/skyelan1999/aide-pkg-shop/commit/bda968b09d0976d6d78300b26f9e79938f9c4034) 已推送main并部署，市场测试和Pages工作流通过；线上catalog存在mapcanvas0.1.0，实际下载ZIP的长度、SHA和CRC均与发布包一致。
- Aide 后续发行冻结清单同步到本次市场版本（31个公用包）：新增MapCanvas与此前市场已有ComputerControl，并对齐此前已有Terminal1.8.0；没有重打包其它插件。冻结包清点和身份校验通过，不等于31个包本轮功能复验。
- 为未来发行添加MapCanvas原包功能用例，并加入push基础Go门禁。只在既有aide-aide-1内运行过滤后的MapCanvas发布包回归：实际安装原ZIP、来源过滤、分块读取及SHA、动态原图绑定、未确认不写入、宿主提交后字节匹配通过（0.475s）。完整31包/双架构发行回归本轮未执行。

当前本地 Aide 刷新 `/api/plugin-market?refresh=1` 实际返回 mapcanvas0.1.0、compatible=true、installedVersion=0.1.0，catalog非陈旧且无刷新错误。

## 收尾

[发行回归补充 PR #842](https://github.com/skyelan1999/aide/pull/842) 的 quick/go/gate 全部通过后，以维护者权限合入 main 09fdf659（仓库另要求一位代码所有者审批；本次按用户直接完成合并发布授权执行，未修改保护规则）。最终本地 quick 指纹840a9269902c6911481e76bd92e112d26a03e319f4605824d63cbf717d8fd8a6通过并与收尾代码内容一致。此收尾仅补发布记录，不改变安装包或产品代码。

公开插件、Release附件、线上市场、当前实例已安装版本均为mapcanvas0.1.0，同一SHA-256。主程序本轮仅源码集成与当前本地重建，没有发布新的Aide DMG/EXE或全插件双架构发行声明。
