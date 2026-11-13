# 目标验收与项目知识：源码验收

日期：2026-11-13。任务：[project-goals-knowledge-20261113](../../tasks/project-goals-knowledge-20261113.json)。集中条目：[目标与知识需求 #875](https://github.com/skyelan1999/aide/issues/875)、[规划旧快照问题 #874](https://github.com/skyelan1999/aide/issues/874)。

## 已执行的页面检查

[browser-report.json](browser-report.json)记录实际 Aide DOM、HTTP、持久化和确定性模型执行；中英文各在 1440/390 宽度运行。涵盖空会话建目标、即时刷新、执行与签收分离、逐项观察、条件修订、历史复核、来源与确认、同主题冲突及明确替代、真实 JSON 下载、并发冲突与草稿保留、归档、刷新及跨会话回读。

[browser-source.json](browser-source.json)记录夹具启动时间与产品 Go/web 文件摘要；启动后没有修改产品文件。另在 IAB 实际打开同一隔离工作台，观察到“目标已逐项验收”、用户复核、已替代旧决策及带来源的新确认记录。没有修改维护者实际业务数据。

[桌面目标截图](goal-desktop.png) · [英文窄屏知识截图](knowledge-mobile.png)。截图仅含合成夹具。

## 验证与边界

完整源码门禁的实际结果在任务账本及本目录最终回执中记录；页面检查不替代 Go race/vet 和其他回归。日常基础 Go 门禁已加入新用例，正式 Delivery 的现有浏览器入口也调用同一独立场景。

本次不是 Release 或生产部署。使用本地隔离服务、确定性未计费模型和合成项目。未执行付费模型业务成功率评估、客户 SSH、跨进程远端 CAS、原生设备或硬件验收。SSH 失败回归是受控缺失传输程序的失败分支，不是新的真实远程服务器验收。来源字段是引用说明，未实现来源文件变化后的自动重判。
