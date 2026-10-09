'use strict';
// Native-backed plugin: the Go workflow owns workspace identity, SSH and
// source permissions. This entrypoint supplies the standard plugin surface.
module.exports = {
  name: 'office',
  apply(ctx) {
    ctx.tool({name:"office_document_search",description:"原文搜索或RAG片段检索，复用星图文档服务。original逐字匹配提取原文，rag本地TF-IDF片段排序（非向量）。返回文件指纹、片段编号及页/段落/工作表/幻灯片定位；只读，无OCR，不代表全文已读。支持本地/SSH工作区及已启用的文件来源；MCP仅检索已发现工具说明，不自动调用工具。",parameters:{type:"object",required:["query","mode"],properties:{query:{type:"string"},mode:{type:"string",enum:["original","rag"]},source:{type:"string"},path:{type:"string"}}},handler(){throw new Error("office_document_search 必须由 aide 原生工作区执行器调用");}});
    ctx.tool({
      name: 'office_create',
      description: '直接生成 DOCX/XLSX/PPTX 并优先写入当前工作空间绑定的自动系统文档目录（本地或工作空间 SFTP）；未绑定时写入工作区，不覆盖同名文件。docx content={title,blocks:[{type:"heading"|"paragraph",text,level?}|{type:"table",rows:[[...]]}]}；xlsx content={sheets:[{name,rows:[[...]]}]}；pptx content={slides:[{title,body}]}。',
      parameters: {
        type: 'object', required: ['path', 'format', 'content'],
        properties: {
          path: { type: 'string', description: '自动系统文档目录的相对路径；未绑定时相对工作区，后缀与格式一致' },
          format: { type: 'string', enum: ['docx', 'xlsx', 'pptx'] },
          content: { type: 'object' },
        },
      },
      handler() {
        throw new Error('office_create 必须由 aide 原生工作区执行器调用');
      },
    });
    ctx.tool({
      name: 'office_comments',
      description: '读取 DOCX 内嵌批注及其精确锚定原文。处理修改要求时先调用此工具；anchorValid=false 时不可猜测位置。支持本地和 SSH 工作区。',
      parameters: {type:'object',required:['path'],properties:{path:{type:'string',description:'工作区相对 DOCX 路径'}}},
      handler() { throw new Error('office_comments 必须由 aide 原生工作区执行器调用'); },
    });
    ctx.tool({
      name: 'office_comment_edit',
      description: '按照一条 DOCX 内嵌批注修改其精确锚定的原文。先用 office_comments 看批注，理解指示并形成 newText；只有 expectedText 与当前锚点逐字一致时写入。保留批注供人工复核，不自动标记已解决。',
      parameters: {type:'object',required:['path','id','expectedText','newText'],properties:{path:{type:'string'},id:{type:'integer'},expectedText:{type:'string'},newText:{type:'string'}}},
      handler() { throw new Error('office_comment_edit 必须由 aide 原生工作区执行器调用'); },
    });
  },
};
