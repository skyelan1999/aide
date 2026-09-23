'use strict';
/* DSH 能力预设插件：子代理（协议 v1 形态兼容，见 docs/plugin-protocol.md §8） */
module.exports = {
  name: 'subagent',
  apply(ctx) {
    ctx.logger.info('子代理 预设已加载');
    ctx.tool({ name: 'subagent-spawn', description: '派生子代理执行独立任务' });
    ctx.tool({ name: 'subagent-report', description: '回收子代理结果' });
    ctx.slot({ id: 'subagent-panel', name: '子代理面板' });
    ctx.provide('subagent');
  },
};
