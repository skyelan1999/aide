# 提醒定时执行验收

基线 `7b8dd80a554b73ddbf6b5d5a976912581e76a22e`，源码内容指纹和实际命令见 [verification.json](verification.json)。本任务按 Asia/Shanghai 日期记账。

- PASS：Go race，提醒新旧 API、模型来源权限、无效时间/编号/指令、并发扫描至多一次、未来任务不提前执行、忙碌与审批不抢占、取消、持久领取失败、重启中断、小秘锁态、运行结果同步。真实 startTask 与小秘 agentic 路径使用本地模拟模型通过；小秘转交保留工作区隔离，原有小秘回归通过。
- PASS：实际隔离 Go testApp 工作台 + Chromium；真实创建/编辑接口、编号与小秘切换、本地上海时间与 UTC 往返、实际调度错误卡片、待执行取消、纯通知兼容、1440/390 宽度、蓝色/舒适明暗主题均通过。无页面异常，见 [result.json](result.json)。浏览器、上下文与本测试服务器均已关闭。
- PASS：`git diff --check`；`app.js` 与新增浏览器脚本语法。
- QUICK：135/137。`test_launcher_data.py` 的 Windows auth/cad-auth ownership 断言为已有失败；`test_offline_bundle.py` 在当前磁盘约 4.1 GiB 可用、启动保护要求 10 GiB 的环境中提前退出，引发其模拟启动断言与 calls 文件检查失败。上述启动器路径没有在本任务修改。
- FULL / RELEASE：没有当前通过的 full 收据；[release-check.txt](release-check.txt) 明确阻塞 full 与干净 Git 状态。保留 10 个任务前已有未跟踪账本，不为发布清空它们。不打版本/tag，不构建或替换运行镜像；仅按既有用户授权推送源码。

浏览器使用临时工作区、临时数据与测试令牌，证据仅包含合成任务。没有真实模型付费调用、用户 SSH/设备会话、真实联系人通知或真实定时任务。这里的实际工作台验收为 httptest 启动的产品页面，不宣称 start.command 生产实例或 Docker 部署成功。没有停止已有用户服务。

截图：

- [桌面蓝色暗色](blue-dark-1440.png)
- [窄屏舒适亮色](comfort-light-390.png)
- [执行失败显示](execution-failed-390.png)

人工审阅确认：执行授权不从模型工具获取；新字段向后兼容；领取在投递之前持久化；原会话编号只在创建时解析；工作区约束传递给小秘转交；定时指令不能成为用户审批应答；结果读取深复制避免数据竞争；窄屏保存按钮固定在表单可达位置。任务采用至多一次投递，未知状态不自动重发，详见架构文档。

源码 `4a63ee0333c3f76b0b9e0e594f62707001137312` 已成功推送 `origin/main`（7b8dd80a..4a63ee03）。后续账本提交不改变产品源码和验证指纹。
