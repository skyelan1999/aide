'use strict';

const classes = require('./classes.json');
const templates = {
  '1202': [
    '# Project Plan',
    '',
    '## Purpose and scope',
    '',
    '## Organization and responsibilities',
    '',
    '## Delivery plan',
    '',
    '## Milestones',
    '',
    '## Dependencies and risks',
    '',
    '## Change and configuration management',
    '',
    '## Deliverables and acceptance',
    '',
  ].join('\n'),
  '12021': [
    '# Progress Report',
    '',
    '## Reporting period',
    '',
    '## Summary',
    '',
    '## Completed work',
    '',
    '## Planned work',
    '',
    '## Milestone status',
    '',
    '## Risks and blockers',
    '',
  ].join('\n'),
  '12022': [
    '# Verification Plan',
    '',
    '## Objectives and scope',
    '',
    '## Requirements and traceability',
    '',
    '## Verification methods',
    '',
    '## Environment and resources',
    '',
    '## Schedule and responsibilities',
    '',
    '## Entry and exit criteria',
    '',
  ].join('\n'),
  '3643': [
    '# Milestone Record',
    '',
    '## Milestone',
    '',
    '## Planned date and actual date',
    '',
    '## Evidence and outcome',
    '',
    '## Open actions',
    '',
  ].join('\n'),
};

function classFor(code) {
  const key = String(code || '').trim().replace(/\s+/g, '');
  return classes.find(item => item.code.replace(/\s+/g, '') === key);
}

function displayCode(code) {
  return /^\d{5}$/.test(code) ? `${code.slice(0, 3)} ${code.slice(3)}` : code;
}

function settingsOf(ctx) {
  return ctx.settings && typeof ctx.settings === 'object' ? ctx.settings : {};
}

function buildName(args, settings) {
  const entry = classFor(args.code);
  if (!entry) throw new Error('分类代码不在已登记的文档目录中');
  const project = String(settings.projectIdentifier || '').trim();
  if (!/^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$/.test(project)) {
    throw new Error('请先在 PjPlan 插件设置中填写有效的项目标识');
  }
  const track = String(settings.versionTrack || 'P').toUpperCase();
  const major = String(settings.majorVersion || 'A').toUpperCase();
  const minor = String(settings.minorVersion || '1').trim();
  if (!['P', 'R'].includes(track) || !['A', 'B', 'C'].includes(major) || !/^\d{1,3}$/.test(minor)) {
    throw new Error('版本设置无效：阶段用 P/R，大版本用 A/B/C，小版本用 1–3 位数字');
  }
  const ext = String(args.extension || 'md').trim().replace(/^\./, '').toLowerCase();
  if (!/^[a-z0-9]{1,12}$/.test(ext)) throw new Error('扩展名只能包含英文字母和数字');
  return `${displayCode(entry.code)} ${entry.name} - ${project} - ${track}${major}${minor}.${ext}`;
}

module.exports = {
  name: 'PjPlan 项目文档',
  apply(ctx) {
    ctx.tool({
      name: 'pjplan_class_lookup',
      description: '在项目十进制分类目录中按代码、文档类型或中文说明查找分类。生成项目文档前先确认分类，不得猜测代码。',
      parameters: {
        type: 'object', required: ['query'],
        properties: { query: { type: 'string' }, limit: { type: 'integer', minimum: 1, maximum: 20 } },
      },
      handler(args) {
        const query = String(args.query || '').trim().toLocaleLowerCase();
        if (!query) throw new Error('请输入分类代码或文档用途');
        const limit = Math.max(1, Math.min(20, Number(args.limit) || 8));
        const hits = classes.filter(item => `${item.code} ${item.name} ${item.nameZh}`.toLocaleLowerCase().includes(query)).slice(0, limit);
        return JSON.stringify({ query, totalMatches: classes.filter(item => `${item.code} ${item.name} ${item.nameZh}`.toLocaleLowerCase().includes(query)).length, classes: hits });
      },
    });

    ctx.tool({
      name: 'pjplan_build_filename',
      description: '依据十进制分类目录和 PjPlan 项目设置生成规范文件名。格式为“分类码 文档类型 - 项目标识 - P/R大版本小版本.扩展名”。P 是评审版，R 是发布版；A/B/C 是大版本，数字是小版本。',
      parameters: {
        type: 'object', required: ['code'],
        properties: { code: { type: 'string', description: '分类目录中的 Decimal Class 代码' }, extension: { type: 'string', description: '不含点的文件扩展名，缺省 md' } },
      },
      handler(args) {
        const entry = classFor(args.code);
        if (!entry) throw new Error('分类代码不在已登记的文档目录中');
        const filename = buildName(args, settingsOf(ctx));
        return JSON.stringify({ code: displayCode(entry.code), documentType: entry.name, filename, source: 'system-docs' });
      },
    });

    ctx.tool({
      name: 'pjplan_document_template',
      description: '按分类目录返回常用项目管理 Markdown 模板，并按当前 PjPlan 设置生成文件名。当前包含项目计划、进展报告、验证计划和里程碑记录；其他类型只生成规范文件名。',
      parameters: {
        type: 'object', required: ['code'],
        properties: { code: { type: 'string' }, extension: { type: 'string', description: '缺省 md' } },
      },
      handler(args) {
        const entry = classFor(args.code);
        if (!entry) throw new Error('分类代码不在已登记的文档目录中');
        const filename = buildName(args, settingsOf(ctx));
        const content = templates[entry.code] || `# ${entry.name}\n\n`;
        return JSON.stringify({ filename, source: 'system-docs', content, templateAvailable: Boolean(templates[entry.code]) });
      },
    });
  },
};
