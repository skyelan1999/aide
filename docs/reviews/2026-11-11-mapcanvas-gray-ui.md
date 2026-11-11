# MapCanvas 0.2.1：原图灰度一致与界面打磨

任务mapcanvas-gray-ui-20261111，集中Issue[#845](https://github.com/skyelan1999/aide/issues/845)。宿主基线a9d00b66，插件基线9f625644。

## 修复

0.2.0灰色画笔使用free/occupied阈值中点，产生原图128而画笔140的可见差异。0.2.1导入时统计原图像素，选择最多的有效未知灰度，同票按灰度升序，绘图中固定使用。候选在原生灰度和转换8位后都必须符合trinary未知范围。无有效像素时明确回退，无法表达则禁用灰笔但不阻碍地图导入。AI工具共用选择器并返回unknownBrush元数据。

界面统一SVG线性图标、分组工具、来源卡片、灰度色样、修改标记和Aide主题跟随。放大视图发现控件挤掉画布，已增加工具区高度限制及独立滚动，保留画布。未改变Aide全局布局。

## 实际验证

- npm test：16项通过，新增128/205/16位/negate、冻结选值、回退、8位精度不可表达、原图不变及AI试画和配套文件像素断言。
- 现有aide-aide-1容器内TestMapCanvasInstalledCandidate：最终ZIP通过0.657s；原0.2.0包向后兼容通过0.629s。初次新包回归正确暴露旧测试硬编码147，已改成按版本能力验证新包继承源127、旧包维持147，而非移除断言。
- 当前localhost:9999原生Aide升级启用0.2.1。合成80×60地图中显示“原图未知灰度128/255”，画笔与背景灰色一致；撤销/重做和宿主保存成功。读回64个变化像素全部128，原YAML未改。
- 同一实例工具预览/试画返回真实PNG；派生PGM/YAML两条提案应用后6个变化像素全部128，输入未改，旧摘要拒绝。未调用付费模型。
- 原生界面实际检查深浅主题、100%和200%页面缩放、独立滚动和适应画布、路径隐藏与指南。恢复原深色偏好与100%缩放，关闭已保存测试工作台。仅合成缓存和已应用测试会话保留，未改用户原地图，没有另开Aide或容器。

包mapcanvas-0.2.1.zip：35,478字节，SHA-256 c69d77085ee5d3a3d5cf9600d8bbfe1756249b9cd8cfe751e79433bd13457618。

没有运行ROS2 map_server、机器人导航、Windows/Linux原生或完整31插件Delivery；不能把像素/阈值回归称为导航验收。主程序只同步回归与冻结清单，产品运行时无需更新、重启或新安装器。

## 发布

[插件PR#2](https://github.com/skyelan1999/aide-pkg-MapCanvas/pull/2)已合并到e79a8558828a38745d9500887f53efa70c5f9df4，Node18/22 CI通过。[v0.2.1 Release](https://github.com/skyelan1999/aide-pkg-MapCanvas/releases/tag/v0.2.1)已上传同一包和SHA256SUMS。市场安装包、listing、catalog及routing已更新，测试通过；市场main ba6b72e Pages已部署，Release和线上ZIP下载读回的35,478字节/SHA/CRC一致；当前Aide市场返回version/installedVersion0.2.1、compatible=true、stale=false。原0.2.0升级备份和ros-map-editor保留，回退沿用原插件升级恢复流程。

最终发布包的MapCanvas过滤功能验收通过0.629s；仅代表MapCanvas，不是全31插件功能重验。主仓库quick通过且内容指纹匹配。
