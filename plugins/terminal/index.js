'use strict';
/* DSH 能力预设插件：终端（协议 v1 形态兼容，见 doc/plugin-protocol.md §8） */
module.exports = {
  name: 'terminal',
  apply(ctx) {
    ctx.logger.info('终端 预设已加载（协议 v1.1，可执行）');
    ctx.tool({
      name: 'terminal-run',
      description: '记录一条 shell 建议命令（不会自动执行；生成提案等待用户手动运行）',
      handler: (args, api) => api.proposeCommand(String(args.command || '')),
    });
    ctx.tool({
      name: 'terminal-history',
      description: '列出当前工作目录内容（只读）',
      handler: (args, api) => ({ text: (api.listFiles(args.path || '.')).join('\n') }),
    });
    ctx.slot({ id: 'terminal-panel', name: '终端面板' });
    ctx.provide('terminal');
  },
};
