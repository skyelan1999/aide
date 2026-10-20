# Terminal 源码合并 main

用户明确要求“直接合并到main”，并表示将在其他环境测试。本次只执行源码整合与合并，不创建宿主新版本/tag/正式镜像、不替换运行实例。用户本次源码合并授权不被描述为完整发行门禁通过。

整合 origin/main 0e94e2b3，保留0.1.17.3 RC1版本记录、Compare工作台与其公开发行审阅记录。解决app.js、plugin-workbench.js/css与工作台测试共4文件冲突：

- 同时识别pluginWorkbench和plugin-workbench查询参数；重复或混用参数拒绝，生成链接仅包含合法插件ID。
- 保留主分支openTab/route及原Terminal openInTab/requestedPlugin API，避免已有入口失效。
- 新标签页沿用主分支独立页样式及隐藏重复开页按钮，保留Terminal后台驻留与bridge续期。
- 保留pagehide释放，DELETE使用keepalive；正在执行的detach调用仍先完成再释放，授权撤销仍立即取消。
- 开页仍等待初始化/解锁完成，补齐两个URL格式及退出释放的回归，浏览器脚本同步统一按钮名称。

冲突相关JS桥接/客户端16项及锁屏恢复、启动文件恢复回归已通过。Go工作台/市场/Terminal关键race及原生构建结果另附。本轮未重新启动原生应用；start.command此前受容器CA与磁盘限制未激活，真实浏览器和Docker full未在本轮重跑，保留原先验收及阻塞记录供其他环境继续验证。

PR #7已合并到main，合并提交33c9cdbdbe0d5e61738d6b6f06ae9ab63d93b564；整合头b24d50d3b613ac17997820b9ffcd3327daef51db。GitHub返回merged=true，远端main已核对。此后交接仅补充文档。未新建宿主tag/镜像，既有0.1.17.3 RC1发行镜像不因此自动包含此合并。
