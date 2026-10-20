'use strict';
// Native executor keeps downloads, cache paths and reference permissions server-owned.
module.exports = {
  name: '技能库 / Skill Library',
  apply(ctx) {
    ctx.tool({
      name: 'skill_library',
      description: '为当前任务自动寻找适用的 Agent Skill。search 按任务关键词查询公开 GitHub 技能目录（中文需求可补充英文关键词）；prepare 将选中的 url 或 repository/path/ref 固定到提交、校验并缓存，生成待确认的只读 Skill 引用；installed 列出当前可见的已安装技能。准备后必须等用户确认管理提案，不得声称已安装，不执行下载的脚本、安装依赖或解开引用锁。Find, cache and prepare public GitHub skills as reviewed references; never execute downloaded code.',
      parameters: {type:'object', required:['action'], properties:{
        action:{type:'string', enum:['search','prepare','installed']},
        query:{type:'string', description:'任务描述和检索关键词 / task and keywords'},
        url:{type:'string', description:'GitHub tree/blob SKILL.md or raw Skill URL'},
        repository:{type:'string', description:'owner/repository'},
        path:{type:'string', description:'Skill directory in repository'},
        ref:{type:'string', description:'Branch, tag or immutable commit; default HEAD'},
        reason:{type:'string', description:'为何当前任务需要该技能 / reason for use'}
      }},
      handler(){throw new Error('此工具需要带 Skill Library 原生执行器的 Aide。请先更新 Aide，再使用技能插件；不要用 shell 或 API 令牌绕过权限。');}
    });
    // Preserve the original preset surface for callers that recognize it.
    ctx.tool({name:'skill-run',description:'请使用 skill_library 搜索并准备引用，再通过 read_file(source=引用ID,path="SKILL.md") 读取；不自动执行第三方脚本。'});
    ctx.tool({name:'skill-list',description:'请使用 skill_library(action="installed") 查询当前可见的已安装技能。'});
    ctx.slot({id:'skill-panel',name:'技能面板'});
    ctx.provide('skill');
  }
};
