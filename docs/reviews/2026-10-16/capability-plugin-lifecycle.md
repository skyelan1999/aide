# 插件执行与能力仪表生命周期验收

任务：[capability-plugin-lifecycle-20261016](../../tasks/capability-plugin-lifecycle-20261016.json)，基线main `b31be2ffc7beece21035db1aab92f96b0d8fdab5`，移动硬盘开发目录。本批仅新增验收代码及文档，不修改产品实现、配置、数据格式或UI。

## 验收方式

新回归 `TestCapabilityPluginLiveLifecycle` 创建实际临时App，经产品鉴权Handler上传仓库内置current-time源码。工具发现及调用使用真实Node宿主进程，调用通过Go的资源调度、插件串行队列、启用与禁用工具守卫。能力状态及读取检查经真实GET/POST路由，不手工注入pluginSurface，也不模拟插件结果。

专项命令基于现有 `scripts/test_go_snapshot.py`，将隔离快照命令替换为 `go test -v -mod=vendor -race -count=1 -timeout=90s -run ^TestCapabilityPluginLiveLifecycle$ ./internal/server`，随后保留 `go vet -mod=vendor ./...`。使用本地aide:local，容器无网络、无生产数据挂载。实际终态退出0，测试正文1.60秒，server race结果2.696秒，全包vet通过。

## 实际断言

| 阶段 | 直接观察与断言 |
| --- | --- |
| 上传与登记 | hostState=registered，工具数2；读取检查仍为not_run。登记不会虚构检查或执行成功。 |
| 入口读取 | 收据pass，SHA256与所上传源码一致，明确“未执行插件或工具”。 |
| 正常工具调用 | Go派发Asia/Shanghai时间请求，结果时区正确，unixMs位于本次实际调用前后时间之间。 |
| 非法参数 | Invalid/Acceptance时区返回错误；宿主仍登记2个工具，原入口读取收据不被改写。 |
| 停用 | PUT启停路由后enabled=false、hostState=not_registered、工具数0、旧读取检查stale；检查400，Go调用明确拒绝未执行。 |
| 重新启用 | 真实宿主重新登记2个工具，入口读取检查通过。 |
| 外部损坏入口 | 写入合成异常代码并重新运行真实宿主，hostState=error、工具数0；调用失败。能力API不泄漏合成私有异常正文。 |
| 修复恢复 | 恢复原内置源码并重读宿主，登记2个工具；UTC工具调用实际成功。 |

入口的历史读取收据与最近宿主加载结果是不同观察。外部编辑不受持续监测，因此宿主加载失败时，仍可能显示五分钟内的入口读取通过；页面已有说明，不应将其解释为工具可执行保证。此测试验证现有分离语义，不把单次参数错误当作整个插件失效，也不新增自动执行任意插件的探针。

## 当前范围与交付边界

没有真实模型请求，没有连接用户SSH主机，没有修改或重启本机9999。生产仍运行上一轮已验证的8d636fd镜像，既有离线包不因本批验收代码重复构建。新回归进入后续full；最终收据及独立main门禁在实际完成后补记。

本项补齐“插件实际工具结果与能力仪表状态一致性”的协议/API级证据，不代表完整认证浏览器、所有插件或长期运行通过。原12项仍保留；Windows由用户验收，真实向量暂缓。

## 最终回归收据

新增回归纳入当前输入full88，全部退出0；server race 556.471秒、全包vet、非root19项资源和DOCX往返通过。内容指纹 `061e94b5c256889fd797ab492b453c91392cbdfd5914f643e6327811e682310e`，记录HEAD `b31be2ffc7beece21035db1aab92f96b0d8fdab5`。详见[机器收据](capability-plugin-lifecycle-proof.json)。继续独立main门禁和推送；本批产品实现未变，不重建镜像。
