# RfToolBox 0.1.0 与插件工作台候选

## 范围与实现

用户要求先汇总并实现初版。私有独立仓库aide-pkg-RfToolBox保存计划与源码；不导入企业知识库或标准全文。宿主新增可选manifest.workbench与全屏独立面板，不实现任意UI Slot、标签页系统或宿主服务注入。

后端POST入口受现有Bearer/同源检查，60秒随机单次页面票据绑定锁屏代际、插件管理作用域和HTML摘要，最多8个。页面最大2MiB，通过sandbox iframe提供内联脚本、样式与Blob Worker；不向页面提供持久令牌、文件API或引用。CSP不放宽Aide主页面；这些限制不等于完整恶意脚本网络沙箱，Node仍按原可信代码模型执行。

插件共享JS内核生成Gray QAM与通用CP-OFDM；已知参考测量，无FEC/同步搜索/均衡。eCPRI示例仅通用Type0/I16大端与经典Ethernet PCAP，不宣称完整NR/O-RAN或天线口实测。

## 已执行

- 插件Node测试9通过：功率/Gray标签、FFT与独立DFT、解析EVM向量、8组无损调制闭环、固定种子重放、AWGN统计、CFO、eCPRI字节/量化、独立构造大小端/VLAN PCAP、畸形输入、打包入口。
- 实际plugin_host.js validate返回ok=true；ZIP上传HTTP201，安装ID rf-toolbox。
- 宿主新增TestPluginWorkbench*的离线Go race测试通过，涵盖鉴权、跨站、单次、过期、锁屏、停用、文件/作用域变更、票据预算、包路径/缺页/符号链接；首轮测试夹具未初始化Handler而失败，修正后重跑通过。
- scripts/plugin-workbench.test.cjs：3通过，涵盖无同源隔离、关闭卸载、锁/作用域/停用/Escape撤销、非法入口拒绝。模型测试不等于物理浏览器行为。
- 使用AIDE_OPEN_BROWSER=0 AIDE_BUILD_FROM_SOURCE=1 bash start.command完成本地构建，默认语法/vet/build，容器Healthy；未跑全量Aide测试。
- Safari现有Aide页：搜索插件RfToolBox→打开工作台→默认计算，图表与指标实际呈现：2016符号、3808样点，RMS EVM1.336%、Peak3.727%、BER0、离散PAPR9.28dB。
- 同一HTML独立预览：SNR35→20点击重算，RMS EVM7.515%；eCPRI示例frame520B/payload516B、128对IQ，量化EVM0.00291%。实际截图在私有插件docs/screenshots/ecpri.png。

## 指纹与运行状态

最终ZIP SHA256：317c26c5f6a4486b293df66fd4727f78d37da7e6214caed92b7a353680e861f7。
已安装入口SHA256：b7df0ab9bcff51bdbb2d04f28437626a5cab186fe9ab5eb71fba055417e5ccb0。
已安装HTML SHA256：d84ec9a1e8321003af73f5f0352fbabea83a8cf91e4ed66a06e9416ba3a79876。
入口和HTML与最终ZIP逐字节一致；安装之后仅ZIP内README补充了计划/验收链接，因此最终ZIP与最初上传包摘要不同，未伪称重新安装同版本变化代码。

临时预览8769已停止，测试容器自动清理。本地Aide和新插件保留供用户试用。既有插件manifest/registry/升级备份未清空、未提交。未推送或正式发布。

## 尚未验收与未实现

实验导出等待未得到完成确认，随后独立预览访问明确被浏览器安全策略拒绝；已停止操作，未切换浏览器表面绕过。文件导出/导入、IQ/PCAP下载、PCAP界面导入、取消/关闭及窄屏/深色的完整浏览器验收NOT_RUN；配置重放与格式自动测试不能替代。

标准NR、完整O-RAN及压缩、知识库受控检索、Touchstone/Smith、仪表/SDR、双向聊天状态同步、多标签和独立窗口仍为后续计划。没有射频发射或网络发包。工作台应视作本地工程初版，不能作为标准一致性仪表交付。

追加旧插件兼容回归在资源高占用时主动停止，未取得完成结果，不记为PASS；新工作台后端race、3项前端模型测试和9项插件测试的已完成证据不变。未再启动构建。宿主复用点：PluginManifest.Workbench、plugin_workbench.go的POST/单次页面路由、web/plugin-workbench.js与CSS、插件卡片打开按钮。
