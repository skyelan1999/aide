'use strict';
/* DSH 能力预设插件：计划（协议 v1 形态兼容，见 doc/plugin-protocol.md §8） */
module.exports = {
  name: 'plan',
  apply(ctx) {
    ctx.logger.info('计划 预设已加载');
    ctx.tool({ name: 'plan-make', description: '生成分步实施计划' });
    ctx.tool({ name: 'plan-review', description: '审查计划完整性' });
    ctx.slot({ id: 'plan-panel', name: '计划面板' });
    ctx.provide('plan');
  },
};
