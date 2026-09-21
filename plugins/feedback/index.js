'use strict';
/* DSH 能力预设插件：反馈（协议 v1 形态兼容，见 doc/plugin-protocol.md §8） */
module.exports = {
  name: 'feedback',
  apply(ctx) {
    ctx.logger.info('反馈 预设已加载');
    ctx.tool({ name: 'feedback-capture', description: '采集反馈条目' });
    ctx.tool({ name: 'feedback-summary', description: '汇总反馈要点' });
    ctx.slot({ id: 'feedback-panel', name: '反馈面板' });
    ctx.provide('feedback');
  },
};
