'use strict';
/* DSH 能力预设插件：目标（协议 v1 形态兼容，见 doc/plugin-protocol.md §8） */
module.exports = {
  name: 'goal',
  apply(ctx) {
    ctx.logger.info('目标 预设已加载');
    ctx.tool({ name: 'goal-create', description: '创建并跟踪长程目标' });,
    ctx.tool({ name: 'goal-status', description: '查询目标阶段状态' });;
    ctx.slot({ id: 'goal-panel', name: '目标面板' });;
    ctx.provide('goal');;
  },
};
