# 语音助理名称同步修正

日期：2026-10-01

## 结果

- 设置分区由“语音小秘”改为“语音助理”，名称字段标为“助理名称”，默认值为“小秘”。
- 性格配置项称为“助理性格系统”，与语音助理分区保持一致。
- 自定义名称沿用现有 `voiceAssistantName` 配置，不增加字段、不迁移历史数据。
- 侧栏助手入口、入口的无障碍名称与标题、助手会话标题、锁屏眉题共用同一名称读取逻辑。
- 英文界面同步更新分区、字段和保存提示翻译。

## 验证

| 检查 | 结果 | 证据 |
| --- | --- | --- |
| 性格配置项标签与翻译 | PASS | 设置 schema 使用“助理性格系统”，英文界面为 “Assistant persona system”；i18n 检查通过。 |
| 隔离候选 Docker 页面 | PASS | 健康检查 `integrity=ok`；未连接 Safari/8097。 |
| 浏览器设置与名称联动 | PASS | 无头 Chrome 打开隔离候选：默认入口为“小秘”；设置显示“语音助理”；保存“阿斯特拉”后，侧栏入口、助手会话标题、锁屏名称节点均显示“阿斯特拉”。 |
| 后端会话标题回归 | PASS | `TestAssistantSessionTitleSync` 定向 Go 测试通过。 |
| 前端、i18n、文档及快速检查 | PASS | `python3 scripts/agent-route.py verify quick` 收据 `.agent-state/verify-20261001T082550506526Z.log`；其他定向 JS/i18n/设置检查与 `scripts/check_docs.py` 通过。 |
| 锁屏遮罩进入与解锁流程 | NOT RUN | 临时测试账户没有密码，无法进入真正的锁定状态；锁屏动态名称节点已验证。 |

## 发布状态

未提交、升版、推送或发布。当前 Safari 的 `localhost:8097` 服务未更改或重启。

## 回滚

回退 `app.js`、`index.html`、`settings-schema.json` 与 `locales/en.js` 本次变更即可。设置值仍保存在原有 `voiceAssistantName` 字段。
