'use strict';
/* DSH 能力预设插件：终端（协议 v1 形态兼容，见 doc/plugin-protocol.md §8） */
module.exports = {
  name: 'terminal',
  apply(ctx) {
    ctx.logger.info('终端 预设已加载');
    ctx.tool({ name: 'terminal-run', description: '在容器终端执行命令' });,
    ctx.tool({ name: 'terminal-history', description: '查看命令历史' });;
    ctx.slot({ id: 'terminal-panel', name: '终端面板' });;
    ctx.provide('terminal');;
  },
};
