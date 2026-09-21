'use strict';
/* DSH 能力预设插件：任务清单（协议 v1 形态兼容，见 doc/plugin-protocol.md §8） */
module.exports = {
  name: 'todo',
  apply(ctx) {
    ctx.logger.info('任务清单 预设已加载');
    ctx.tool({ name: 'todo-list', description: '列出当前任务清单' });
    ctx.tool({ name: 'todo-update', description: '更新任务状态' });
    ctx.slot({ id: 'todo-panel', name: '任务清单面板' });
    ctx.provide('todo');
  },
};
