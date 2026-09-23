'use strict';
// The Go context builder supplies current, bounded metadata at task creation.
module.exports = {
  name: 'environment-guide',
  apply(ctx) {
    ctx.provide('environment-guide');
    ctx.slot({ id: 'environment-guide', name: '环境说明 / Environment guide' });
    ctx.logger.info('Environment guide enabled: workspace and reference inventory is added to task context.');
  },
};
