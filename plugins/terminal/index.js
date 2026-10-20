'use strict';
let sessions;
module.exports = {
  name: 'terminal',
  apply(ctx) {
    const add = (name, description, handler, properties = {}, required = []) => ctx.tool({name, description,
      parameters:{type:'object',properties,required,additionalProperties:false}, handler});
    add('terminal-run','记录 shell 命令提案，等待用户审阅与手动运行；不会直接执行',(args,api)=>api.proposeCommand(String(args.command||'')),{command:{type:'string'}},['command']);
    add('terminal-history','只读列出当前工作区目录',(args,api)=>({text:api.listFiles(args.path||'.').join('\n')}),{path:{type:'string'}});
    const run = method => (args,api) => {
      if (process.env.AIDE_DAEMON !== '1') throw Error('终端工作台需要启用 daemon 常驻插件支持');
      sessions ||= new (require('./sessions').Sessions)();
      return sessions[method](api.workspaceRoot,args);
    };
    const id={id:{type:'string'}};
    add('terminal_session_list','列出当前工作区的后台终端会话',run('list'));
    add('terminal_session_create','直接启动交互式本地或 SSH 会话；仅在用户明确要求连接或运行时调用',run('create'),{
      kind:{type:'string',enum:['local','ssh']},name:{type:'string'},cwd:{type:'string'},host:{type:'string'},port:{type:'integer'},username:{type:'string'},identityFile:{type:'string'},logging:{type:'boolean'},cols:{type:'integer'},rows:{type:'integer'}});
    add('terminal_session_input','直接向指定会话发送输入；可能执行设备命令，请确保已获得用户授权',run('send'),{...id,data:{type:'string'}},['id','data']);
    add('terminal_session_poll','获取当前工作区会话状态和增量输出（base64）',run('poll'),{...id,offset:{type:'integer'}});
    add('terminal_session_resize','调整终端 PTY 大小',run('resize'),{...id,cols:{type:'integer'},rows:{type:'integer'}},['id','cols','rows']);
    add('terminal_session_close','结束指定后台会话，保留日志',run('close'),id,['id']);
    add('terminal_session_logging','启用或暂停指定会话的输出日志',run('logging'),{...id,enabled:{type:'boolean'}},['id','enabled']);
    add('terminal_session_logs','分页读取指定会话磁盘日志（base64），返回轮转文件身份',run('logs'),{...id,index:{type:'integer'},offset:{type:'integer'},identity:{type:'string'}},['id']);
    add('terminal_session_delete','永久删除已结束会话的记录与日志，须用户明确要求',run('remove'),id,['id']);
    ctx.slot({id:'terminal-panel',name:'设备终端工作台'});ctx.provide('terminal');
  },
  async stop() { if (sessions) await sessions.stop(); },
};
