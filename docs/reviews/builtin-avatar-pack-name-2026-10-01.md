# 内置虚拟形象包名称调整

日期：2026-10-01

## 结果

- 内置素材包显示名改为“小鲸鱼 - 光栅版 - 原画动态包”。
- 英文界面对应名称为 “Little Whale - Raster - Original Animation Pack”。
- 内置包稳定 ID 仍为 `builtin-whale`，动画清单、海报、素材路径与使用偏好保持不变。
- 提升内置包 revision，已保存的 revision 2 记录会迁移显示名，而不是要求用户手工删除或重选素材包。

## 验证

| 检查 | 结果 | 证据 |
| --- | --- | --- |
| 虚拟形象设置回归 | PASS | `node scripts/test_avatar_settings.cjs`；覆盖旧存储迁移、包身份稳定、预览与播放设置。 |
| i18n 与语法 | PASS | `node scripts/test_i18n.cjs`、`node --check` 检查通过。 |
| 候选运行页 | PASS | 隔离 Docker 候选在无头 Chrome 中打开；包卡片和单选项显示新名称，revision 2 本地记录自动迁移为 revision 3，保留 activePack、opacity、playbackRate、素材 ID 与 manifest 路径。 |
| 快速检查 | PASS | `.agent-state/verify-20261001T083152581856Z.log`。 |

Safari 的 `localhost:8097` 未改动或重启。隔离候选验证后已清理。

## 发布状态

未升版、提交、推送或发布。

## 回滚

恢复 `app.js` 的旧内置包名称与 revision 检查、英文词条及回归断言即可；内置包 ID 和动画资源不需恢复数据。
