# 插件当前工作区绑定修复

## 根因与最终设计

终端使用a.workspace对应的当前授权项目，但插件readFile/listFiles及runPython写死/workspace；资源调度又以a.workPath（安装根）解释workspace资源。用户在Downloads创建工程后，插件会去安装目录读同名路径。插件安装根a.pluginsPath必须保留，不能用简单全局替换安装路径解决。

新增锁内pluginWorkspaceLocked：本地返回当前OS Root名称，默认仍兼容workPath，SSH返回空本地根。调用捕获root/revision/package scope，用于资源声明、Node CWD、Python CWD、工具API workspaceRoot、计划策略及工作台调用。根放在宿主请求/RPC envelope，不从模型arguments读取。常驻进程每次构造独立API，不全局chdir。daemon若自行直接使用启动时process.cwd，仍须适配api.workspaceRoot，宿主不能在并发调用期间安全修改全局cwd。

排队后核对根/代次/包身份，短命与daemon返回后再次拒绝旧工作区结果；工作台继承原有撤销监视/中止。已发生的可信Node/Python副作用不能由结果拒绝撤回。helper真实路径检查拒绝符号链接越界；它不是OS沙箱，可信插件直接fs/子进程能力仍受原信任边界约束。引用可见/可写锁与插件管理权限未修改。

## 实际验证

- 第一轮：隔离dependencies容器、2CPU/3GiB，GOMAXPROCS=2 go test -mod=vendor -race -count=1 -timeout=5m ./internal/server -run 'TestPlugin(CurrentWorkspace|WorkspaceRoot|Workbench|Plan|Resource)'，PASS 116.756s。
- Node：node --test scripts/plugin-workspace.test.cjs，2项PASS，当前项目读取、路径穿越/符号链接/空SSH根拒绝，常驻插件两个交叠调用的根隔离。
- 最后一轮GOMAXPROCS=2 go test -mod=vendor -race -v -count=1 -timeout=3m ./internal/server -run 'TestPlugin(CurrentWorkspace|WorkspaceRoot)'：3项PASS，13.313s；覆盖两个项目目录、Python/Node CWD、SSH拒绝和调用中切换后的晚返回拒绝。
- 未执行真实BrdPlan STM32工程、KiCad安装/制造、真实浏览器或TestBench节点连接；无RF发射和硬件操作。

## 部署与回退

当前仅源码，未构建/替换运行宿主、未推送。保留全部插件注册表、已安装包、升级备份、会话及配置。需与BrdPlan最终插件适配和必要TestBench最终包一起串行安排一次获准本地更新，部署后另验实际当前工程可读及无串区。没有插件依赖/环境变量/网络放宽，不猜测节点令牌。

当前没有占用浏览器或改动验收中服务；源码准备完成，部署需统筹串行安排。RfToolBox当前包0.1.0/源码7ddb9b9/ZIP SHA256 da8b6f29aba2dbca116af0d028967a2bd0802b18255f7e51e791c790ad80dfd1，未因本次宿主修复替换。
