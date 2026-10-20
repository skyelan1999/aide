# STEP爆炸与零件拆装 · 2026-10-20

任务`step-assembly-explode-20261020`，基线`a2c6d5c9c0611848e8036c7ba682c2e00074aaa2`。路由因“安装”关键词匹配运行时，但用户意图是零件装回；实际使用frontend，并补读backend处理原生协议。未启动子Agent。

## 结果

新增按需装配面板、0–200%爆炸间距、径向/XYZ、模型拾取、名称/编号搜索、拆除/装回及全部复原。拆除为显示隐藏，原STEP和几何数组不变；展示期间测量暂停，复原收起后恢复。保留原生叶实例身份，跨块/混合图元都属于同一零件。默认合并渲染，进入装配模式才建立共享顶点缓冲的实例组；不随滑块重复构建相同选择列表。单零件不虚构爆炸，旧CAD服务缺少身份时明确禁用装配。

## 实际验证

`PYTHONPATH=/tmp/aide-cad-assembly-runtime python3 scripts/test-step-assembly-native.py`在临时目录内仓库SHA256固定的OCP8.0.1.1运行。真实fidelity.step原生转换，强制每5个三角面一块共24块，仍为两个独立实例，范围完整、身份和名称稳定；继承原有原生几何程序，验证嵌套变换、面色、单位、NURBS失败面恢复、混合/纯线点和退化边界。是Linux主机固定运行时验收，未冒充Docker镜像验证。

第一次原生完整回归暴露无Name属性的根标签FindAttribute导致段错误，已改为先检查空Label及属性存在；最终完整回归通过，没有跳过该根节点测试。

`AIDE_EVIDENCE_DIR=… node scripts/browser-step-assembly.cjs`在一个自建HTTP组件页和一个Chromium标签使用真实STEP viewer、Worker、WebGL、OCP转换输出及离线WASM解析。HTTP只替代工作台后端，未绕过产品Worker几何/范围校验。[结果](result.json)包括合并批次、24块归为两个零件、选择拆装、爆炸往返无漂移、原几何字节相同、测量隔离、真实canvas拾取、单零件和旧服务边界、销毁，以及1280×900/390×900、blue/comfort明暗。无用户模型、令牌、付费调用或生产服务。

相关JS与CAD服务回归、最终内容指纹和quick实际结果见verification.json。完整工作台启动器、生产镜像、SSH真实文件和硬件未验；先前start.command可用磁盘门槛未满足。相关测试页面/HTTP/Worker退出。临时固定CAD运行时仅用于测试，未改系统或应用Python环境。

## 截图、交付与回滚

[爆炸视图](exploded-x-1280.png)、[装配复原](installed-1280.png)、[窄屏浅色](blue-light-390.png)、[经典深色](comfort-dark-1280.png)。

本轮交付源码，不发布版本/tag/镜像，不替换用户服务。原生装配功能需要工作台和CAD服务同时包含新源码；只更新浏览器页面而保留旧CAD服务会显示缺少零件边界的提示。回滚git revert功能提交后按原流程构建，协议新增字段对旧前端可忽略；无数据迁移，不删工作区或数据。保留此前10份未跟踪任务。
