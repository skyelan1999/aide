'use strict';
/* DSH 能力预设插件：技能（协议 v1 形态兼容，见 doc/plugin-protocol.md §8） */
module.exports = {
  name: 'skill',
  apply(ctx) {
    ctx.logger.info('技能 预设已加载');
    ctx.tool({ name: 'skill-run', description: '按名称执行已注册技能' });
    ctx.tool({ name: 'skill-list', description: '列出可用技能' });
    ctx.slot({ id: 'skill-panel', name: '技能面板' });
    ctx.provide('skill');
  },
};
