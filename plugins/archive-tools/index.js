'use strict';
module.exports = {
  name: '压缩文件',
  apply(ctx) {
    ctx.tool({
      name: 'archive_compress',
      description: '将工作区中的文件或文件夹压缩到原路径旁；不覆盖已存在的目标。支持 zip、tar、tar.gz、tar.bz2、tar.xz。',
      parameters: {
        type: 'object', required: ['path', 'format'],
        properties: {
          path: {type: 'string', description: '相对工作区根目录的文件或文件夹路径'},
          format: {type: 'string', enum: ['zip', 'tar', 'tar.gz', 'tar.bz2', 'tar.xz']},
        },
      },
      handler() { throw new Error('archive_compress 必须由 Aide 原生归档执行器调用'); },
    });
    ctx.tool({
      name: 'archive_extract',
      description: '在工作区中安全解压 ZIP、TAR、TAR.GZ/TGZ、TAR.BZ2/TBZ2、TAR.XZ/TXZ 归档，或独立 GZ/BZ2/XZ 压缩流到同级新文件夹；自动避让同名目录，拒绝路径穿越、链接和特殊文件。',
      parameters: {
        type: 'object', required: ['path'],
        properties: {path: {type: 'string', description: '相对工作区根目录的 .zip/.tar/.tar.gz/.tgz/.tar.bz2/.tbz2/.tar.xz/.txz/.gz/.bz2/.xz 路径'}},
      },
      handler() { throw new Error('archive_extract 必须由 Aide 原生归档执行器调用'); },
    });
  },
};
