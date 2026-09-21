'use strict';
/* DSH 能力预设插件：工作流（协议 v1 形态兼容，见 doc/plugin-protocol.md §8） */
module.exports = {
  name: 'workflow',
  apply(ctx) {
    ctx.logger.info('工作流 预设已加载');
    ctx.tool({ name: 'workflow-start', description: '启动多阶段工作流' });,
    ctx.tool({ name: 'workflow-status', description: '查询工作流进度' });;
    ctx.slot({ id: 'workflow-panel', name: '工作流面板' });;
    ctx.provide('workflow');;
  },
};
