'use strict';
/* 协议 v1.2 daemon 宿主测试插件（极简，无第三方依赖）。
 * 仅用于 Go 侧 plugin_daemon_test.go 的端到端验证，不进入生产插件注册表。
 * 工具：
 *   echo        —— 原样返回 args.msg
 *   emit-event  —— 经 ctx.emit 上报一条 event，返回 {ok:true}
 */
module.exports = {
  name: 'mock-daemon',
  daemon: true,
  apply(ctx) {
    ctx.tool({
      name: 'echo',
      description: '原样返回输入消息',
      handler: (args) => ({ text: String((args && args.msg) || '') }),
    });
    ctx.tool({
      name: 'emit-event',
      description: '上报一条事件',
      handler: (args) => {
        ctx.emit('event', { subtype: 'note', message: String((args && args.note) || '') });
        return { ok: true };
      },
    });
  },
  start() { this.__startedAt = Date.now(); },
  stop() {},
};
