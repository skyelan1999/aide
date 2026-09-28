'use strict';
/* SQLite 数据库插件（aide 协议 v1.1，见 docs/plugins/sqlite.md）
 *
 * 零第三方依赖：仅使用 Node 内置 node:sqlite 与 node:fs/path，可离线、免原生编译。
 * 普通（非 daemon）插件：每次 call 短连接——打开数据库 → 执行 → 关闭，无跨调用连接池。
 *
 * 安全模型（见 docs/plugins/sqlite.md §安全模型）：
 *  - db 路径限定挂载根 /workspace、/context、/local；拒绝 ../ 遍历与符号链接越权。
 *  - 默认 readonly=true，写/DDL 需显式 readonly=false。
 *  - 危险语句（DROP、无 WHERE 的 UPDATE/DELETE）拦截，需 confirm=true。
 *  - ATTACH DATABASE 仅允许根内路径；扩展加载默认禁用。
 *  - 一律参数绑定，不拼接 SQL；结果集截断并返回 truncated 标记。
 *  - busy_timeout 防锁死；写操作审计（不记录数据正文）。
 */
const fs = require('fs');
const path = require('path');
const { DatabaseSync } = require('node:sqlite');

const ALLOWED_ROOTS = ['/workspace', '/context', '/local'];
const DEFAULT_BASE = '/workspace';
const MAX_ROWS = 500;
const MAX_CELL_CHARS = 10000;
const DEFAULT_TIMEOUT_MS = 5000;

const READ_VERBS = new Set(['SELECT', 'PRAGMA', 'EXPLAIN', 'WITH']);
const WRITE_VERBS = new Set([
  'INSERT', 'UPDATE', 'DELETE', 'REPLACE', 'DROP', 'ALTER', 'CREATE',
  'TRUNCATE', 'ATTACH', 'DETACH', 'VACUUM', 'REINDEX', 'ANALYZE',
]);

/* ---------------- 安全中间件 ---------------- */

function auditWrite(dbPath, op, changes) {
  // 走 stderr → 宿主日志；只记路径/操作/影响行数，绝不记录数据正文。
  try {
    console.error('[sqlite-audit]', JSON.stringify({
      ts: new Date().toISOString(), db: dbPath, op, changes: Number(changes) || 0,
    }));
  } catch (_) { /* 审计失败不阻断主流程 */ }
}

function ensureInsideRoot(p, label) {
  for (const root of ALLOWED_ROOTS) {
    if (p === root || p.startsWith(root + '/')) return;
  }
  throw new Error(`${label}路径越界：${p}（仅允许 ${ALLOWED_ROOTS.join('、')} 之内）`);
}

/* 解析并校验数据库文件路径：相对路径相对 /workspace 解析；拒绝越界与符号链接逃逸。 */
function resolveDbPath(input) {
  if (typeof input !== 'string' || !input.trim()) throw new Error('缺少必填参数 db（数据库文件路径）');
  const abs = path.resolve(DEFAULT_BASE, input.trim());
  ensureInsideRoot(abs, 'db');
  // 符号链接逃逸检测：对已存在的目录与文件取真实路径后再次校验。
  const parent = path.dirname(abs);
  if (fs.existsSync(parent)) ensureInsideRoot(fs.realpathSync(parent), 'db 目录');
  if (fs.existsSync(abs)) ensureInsideRoot(fs.realpathSync(abs), 'db 文件');
  return abs;
}

/* 导出输出路径：同 resolveDbPath，但禁止写入只读挂载 /context。 */
function resolveOutputPath(input) {
  if (typeof input !== 'string' || !input.trim()) throw new Error('缺少必填参数 outputPath');
  const abs = path.resolve(DEFAULT_BASE, input.trim());
  ensureInsideRoot(abs, 'outputPath');
  if (abs.startsWith('/context')) throw new Error('outputPath 位于只读挂载 /context，无法写出');
  return abs;
}

function firstVerb(sql) {
  const s = String(sql || '')
    .replace(/^\uFEFF/, '')
    .replace(/^\s*--[^\n]*\n?/, '')
    .trim();
  const m = s.match(/^([A-Za-z]+)/);
  return m ? m[1].toUpperCase() : '';
}

/* 危险写操作拦截：DROP / 无 WHERE 的 UPDATE|DELETE，需 confirm=true 放行。 */
function checkDangerousWrite(sql, confirm) {
  const up = String(sql).toUpperCase();
  if (/\bDROP\s+(TABLE|INDEX|VIEW|TRIGGER|SCHEMA|DATABASE)\b/.test(up) && confirm !== true) {
    throw new Error('危险操作被拦截：DROP 需要显式 confirm=true 确认后才执行');
  }
  const stripped = up.replace(/--[^\n]*/g, '');
  const w = stripped.match(/^(UPDATE|DELETE)\b/);
  if (w && !/\bWHERE\b/.test(stripped) && confirm !== true) {
    throw new Error(`危险操作被拦截：${w[1]} 缺少 WHERE 条件，需要显式 confirm=true 确认后才执行`);
  }
}

/* ATTACH DATABASE：仅允许挂载根内的数据库文件，否则拒绝。 */
function checkAttachInsideRoot(sql) {
  if (!/\bATTACH\b/i.test(sql)) return;
  let allowed = false;
  const re = /'([^']*)'/g;
  let m;
  while ((m = re.exec(sql)) !== null) {
    const abs = path.resolve(DEFAULT_BASE, m[1]);
    try { ensureInsideRoot(abs); allowed = true; } catch (_) { /* 该候选越界 */ }
  }
  if (!allowed) throw new Error('ATTACH DATABASE 被拦截：附加数据库路径不在允许的挂载根内');
}

/* ---------------- 数据规范化（防 BigInt 序列化崩溃） ---------------- */

function sanitize(v) {
  if (typeof v === 'bigint') {
    const n = Number(v);
    return Number.isSafeInteger(n) ? n : v.toString();
  }
  if (v === null || v === undefined) return v;
  if (Buffer.isBuffer(v) || v instanceof Uint8Array) return `<BLOB ${v.length} bytes>`;
  if (Array.isArray(v)) return v.map(sanitize);
  if (typeof v === 'object') {
    const o = {};
    for (const k of Object.keys(v)) o[k] = sanitize(v[k]);
    return o;
  }
  return v;
}

function cellToCell(v) {
  if (v === null || v === undefined) return { value: v, truncated: false };
  if (typeof v === 'bigint') return { value: sanitize(v), truncated: false };
  if (typeof v === 'object') {
    let s;
    try { s = JSON.stringify(sanitize(v)); } catch (_) { s = String(v); }
    if (s.length > MAX_CELL_CHARS) {
      return { value: s.slice(0, MAX_CELL_CHARS) + '…[truncated]', truncated: true };
    }
    return { value: v, truncated: false };
  }
  // 保留 number/boolean 原始类型；仅超长字符串截断。
  if (typeof v === 'string' && v.length > MAX_CELL_CHARS) {
    return { value: v.slice(0, MAX_CELL_CHARS) + '…[truncated]', truncated: true };
  }
  return { value: v, truncated: false };
}

function columnNames(stmt, rows) {
  try {
    if (Array.isArray(stmt.columns) && stmt.columns.length) {
      const names = stmt.columns.map(c => c && (c.name !== undefined ? c.name : c.columnName)).filter(x => typeof x === 'string');
      if (names.length) return names;
    }
  } catch (_) { /* 回退到行键 */ }
  if (Array.isArray(rows) && rows.length) return Object.keys(rows[0]);
  return [];
}

function normalizeParams(params) {
  if (params === undefined || params === null) return undefined;
  if (Array.isArray(params) || typeof params === 'object') return params;
  return undefined;
}
/* node:sqlite：位置参数是可变参 .run(a,b)，命名参数才传对象；无参时零参调用。 */
function runStmt(stmt, params) {
  const p = normalizeParams(params);
  if (p === undefined) return stmt.run();
  if (Array.isArray(p)) return stmt.run(...p);
  return stmt.run(p);
}
function allStmt(stmt, params) {
  const p = normalizeParams(params);
  if (p === undefined) return stmt.all();
  if (Array.isArray(p)) return stmt.all(...p);
  return stmt.all(p);
}

function quoteSqlPath(p) {
  return "'" + String(p).replace(/'/g, "''") + "'";
}

/* 打开数据库：readOnly 时只读模式；禁用扩展；busy_timeout 防锁死。 */
function openDb(abs, readOnly, timeoutMs) {
  const db = new DatabaseSync(abs, {
    readOnly: !!readOnly,
    readBigInts: true,
    allowExtension: false, // 扩展加载默认禁用（双重保险）
  });
  db.exec('PRAGMA busy_timeout = ' + Math.min(Number(timeoutMs) || DEFAULT_TIMEOUT_MS, 60000));
  db.exec('PRAGMA foreign_keys = ON');
  return db;
}

/* ---------------- 工具 handler ---------------- */

// 1) 通用查询：SELECT/PRAGMA → 行集；写/DDL → changes
function handleQuery(args) {
  const abs = resolveDbPath(args.db);
  const sql = String(args.sql || '');
  if (!sql.trim()) throw new Error('缺少必填参数 sql');
  const readonly = args.readonly !== false; // 默认只读
  const timeout = Number(args.timeout) || DEFAULT_TIMEOUT_MS;
  const verb = firstVerb(sql);
  if (readonly && WRITE_VERBS.has(verb)) {
    throw new Error(`只读模式拒绝写操作（${verb}）；如需写入请显式传 readonly=false`);
  }
  if (!readonly) {
    checkDangerousWrite(sql, args.confirm);
    checkAttachInsideRoot(sql);
  }
  const db = openDb(abs, readonly, timeout);
  try {
    if (READ_VERBS.has(verb)) {
      const stmt = db.prepare(sql);
      const all = allStmt(stmt, args.params);
      const truncatedColumns = columnNames(stmt, all);
      let truncated = false;
      let rows = all;
      if (all.length > MAX_ROWS) { rows = all.slice(0, MAX_ROWS); truncated = true; }
      const outRows = [];
      for (const row of rows) {
        const o = {};
        for (const k of Object.keys(row)) {
          const c = cellToCell(row[k]);
          if (c.truncated) truncated = true;
          o[k] = c.value;
        }
        outRows.push(o);
      }
      return { columns: truncatedColumns, rows: outRows, rowCount: outRows.length, totalRows: all.length, truncated };
    }
    const info = runStmt(db.prepare(sql), args.params);
    auditWrite(abs, verb || 'WRITE', info.changes);
    return { changes: Number(info.changes), lastInsertRowid: sanitize(info.lastInsertRowid) };
  } finally {
    db.close();
  }
}

// 2) 事务
function handleTransaction(args) {
  const abs = resolveDbPath(args.db);
  const list = Array.isArray(args.statements) ? args.statements : [];
  if (!list.length) throw new Error('statements 为空：请传入 [{sql, params?}, ...]');
  if (args.readonly === true) throw new Error('事务包含写操作；readonly=true 时拒绝执行');
  for (const s of list) {
    if (!s || typeof s.sql !== 'string' || !s.sql.trim()) throw new Error('statements 中存在空 sql');
    const v = firstVerb(s.sql);
    if (WRITE_VERBS.has(v)) checkDangerousWrite(s.sql, args.confirm);
  }
  const db = openDb(abs, false, Number(args.timeout) || DEFAULT_TIMEOUT_MS);
  try {
    db.exec('BEGIN');
    const results = [];
    let totalChanges = 0;
    let lastId = null;
    try {
      for (const s of list) {
        const v = firstVerb(s.sql);
        if (READ_VERBS.has(v)) {
          const st = db.prepare(s.sql);
          results.push({ type: 'rows', rows: sanitize(allStmt(st, s.params)) });
        } else {
          const info = runStmt(db.prepare(s.sql), s.params);
          totalChanges += Number(info.changes);
          lastId = info.lastInsertRowid;
          results.push({ type: 'changes', changes: Number(info.changes), lastInsertRowid: sanitize(info.lastInsertRowid) });
        }
      }
      db.exec('COMMIT');
    } catch (e) {
      try { db.exec('ROLLBACK'); } catch (_) { /* 忽略回滚异常 */ }
      return { committed: false, error: String((e && e.message) || e), results };
    }
    auditWrite(abs, 'TRANSACTION', totalChanges);
    return { committed: true, results, changes: totalChanges, lastInsertRowid: sanitize(lastId) };
  } finally {
    db.close();
  }
}

// 3) 多语句脚本/迁移（事务内 exec）
function handleExecute(args) {
  const abs = resolveDbPath(args.db);
  const script = String(args.script || '');
  if (!script.trim()) throw new Error('缺少必填参数 script');
  if (args.readonly === true) throw new Error('execute 为写操作；readonly=true 拒绝');
  checkDangerousWrite(script, args.confirm);
  checkAttachInsideRoot(script);
  const db = openDb(abs, false, Number(args.timeout) || DEFAULT_TIMEOUT_MS);
  try {
    db.exec('BEGIN');
    try {
      db.exec(script);
      db.exec('COMMIT');
    } catch (e) {
      try { db.exec('ROLLBACK'); } catch (_) { /* 忽略 */ }
      throw e;
    }
    const executed = script.split(';').map(s => s.trim()).filter(Boolean).length;
    auditWrite(abs, 'SCRIPT', 0);
    return { changes: 0, executedStatements: executed };
  } finally {
    db.close();
  }
}

// 4) 列出表与视图
function handleTables(args) {
  const abs = resolveDbPath(args.db);
  const db = openDb(abs, true, DEFAULT_TIMEOUT_MS);
  try {
    const rows = db.prepare(
      "SELECT type, name, sql FROM sqlite_master WHERE type IN ('table','view') ORDER BY type, name"
    ).all();
    return { tables: rows.map(r => ({ name: r.name, type: r.type, sql: r.sql })) };
  } finally {
    db.close();
  }
}

// 5) 表结构：建表 SQL、列、索引、外键
function handleSchema(args) {
  const abs = resolveDbPath(args.db);
  const table = String(args.table || '').trim();
  if (!/^[A-Za-z_][A-Za-z0-9_$]*$/.test(table)) {
    throw new Error('非法表名：仅允许字母、数字、下划线、$ 且不以数字开头');
  }
  const db = openDb(abs, true, DEFAULT_TIMEOUT_MS);
  try {
    const created = db.prepare("SELECT sql FROM sqlite_master WHERE type='table' AND name=?").get(table);
    const columns = db.prepare(`PRAGMA table_info("${table}")`).all();
    const indexes = db.prepare(`PRAGMA index_list("${table}")`).all();
    const foreignKeys = db.prepare(`PRAGMA foreign_key_list("${table}")`).all();
    return {
      table,
      sql: created ? created.sql : null,
      columns: sanitize(columns),
      indexes: sanitize(indexes),
      foreignKeys: sanitize(foreignKeys),
    };
  } finally {
    db.close();
  }
}

// 6) 导出：CSV 或 dump（VACUUM INTO）
function toCsv(columns, rows) {
  const esc = v => {
    if (v === null || v === undefined) return '';
    const s = typeof v === 'object' ? JSON.stringify(v) : String(v);
    return /[",\n\r]/.test(s) ? '"' + s.replace(/"/g, '""') + '"' : s;
  };
  const lines = [columns.map(esc).join(',')];
  for (const row of rows) lines.push(columns.map(c => esc(row[c])).join(','));
  return lines.join('\n') + '\n';
}

function handleExport(args) {
  const abs = resolveDbPath(args.db);
  const format = String(args.format || 'csv').toLowerCase();
  const out = resolveOutputPath(args.outputPath);
  const db = openDb(abs, format === 'csv', Number(args.timeout) || DEFAULT_TIMEOUT_MS);
  try {
    if (format === 'dump') {
      db.exec(`VACUUM INTO ${quoteSqlPath(out)}`);
      auditWrite(abs, 'DUMP', 0);
      return { format, outputPath: out, bytes: fs.statSync(out).size };
    }
    if (format !== 'csv') throw new Error('format 仅支持 csv 或 dump');
    const query = String(args.query || "SELECT name FROM sqlite_master WHERE type IN ('table','view')");
    const stmt = db.prepare(query);
    const all = stmt.all();
    const cols = columnNames(stmt, all);
    const rows = all.slice(0, MAX_ROWS).map(r => sanitize(r));
    fs.writeFileSync(out, toCsv(cols, rows));
    auditWrite(abs, 'EXPORT_CSV', rows.length);
    return { format: 'csv', outputPath: out, columns: cols, rows: rows.length, truncated: all.length > rows.length };
  } finally {
    db.close();
  }
}

/* ---------------- 插件导出（协议 v1.1） ---------------- */

const p = { type: 'object', additionalProperties: true, description: '参数绑定：? 用数组，:name 用对象' };

module.exports = {
  name: 'sqlite',
  apply(ctx) {
    ctx.logger && ctx.logger.info && ctx.logger.info('SQLite 数据库插件已加载（node:sqlite，零依赖）');
    ctx.tool({
      name: 'sqlite_query',
      description: '对 SQLite 数据库执行一条参数化查询。SELECT/PRAGMA/EXPLAIN 返回 {columns,rows,rowCount,truncated}；INSERT/UPDATE/DELETE 返回 {changes,lastInsertRowid}；DDL 返回 {changes}。默认只读，写操作需显式 readonly=false。',
      parameters: {
        type: 'object',
        required: ['db', 'sql'],
        properties: {
          db: { type: 'string', description: '数据库文件路径，须在 /workspace、/context、/local 内；相对路径基于 /workspace' },
          sql: { type: 'string', description: '单条 SQL，占位符用 ? 或 :name，禁止拼接用户输入' },
          params: { oneOf: [{ type: 'array' }, { type: 'object' }], description: '位置参数数组或命名参数对象' },
          readonly: { type: 'boolean', description: '默认 true（只读打开）；写/DDL 须显式 false', default: true },
          timeout: { type: 'number', description: 'busy_timeout 毫秒，默认 5000' },
          confirm: { type: 'boolean', description: '危险写操作（DROP、无 WHERE 的 UPDATE/DELETE）需 true 确认' },
        },
      },
      handler: args => handleQuery(args || {}),
    });
    ctx.tool({
      name: 'sqlite_transaction',
      description: '在单个事务内顺序执行多条语句：BEGIN → 逐条 → COMMIT；任一失败自动 ROLLBACK。返回 {committed,results,changes,lastInsertRowid}。默认可写（readonly=false）。',
      parameters: {
        type: 'object',
        required: ['db', 'statements'],
        properties: {
          db: { type: 'string', description: '数据库文件路径（限挂载根内）' },
          statements: {
            type: 'array', description: '待执行语句数组 [{sql, params?}]',
            items: {
              type: 'object', required: ['sql'],
              properties: { sql: { type: 'string' }, params: p },
            },
          },
          readonly: { type: 'boolean', description: '默认 false；传 true 会被拒绝（事务含写）' },
          timeout: { type: 'number', description: 'busy_timeout 毫秒，默认 5000' },
          confirm: { type: 'boolean', description: '危险语句需 true 确认' },
        },
      },
      handler: args => handleTransaction(args || {}),
    });
    ctx.tool({
      name: 'sqlite_execute',
      description: '在事务内执行多语句脚本/迁移（db.exec），任一失败整体回滚。返回 {changes, executedStatements}。',
      parameters: {
        type: 'object',
        required: ['db', 'script'],
        properties: {
          db: { type: 'string', description: '数据库文件路径（限挂载根内）' },
          script: { type: 'string', description: '多语句 SQL 脚本，语句以分号分隔' },
          readonly: { type: 'boolean', description: '默认 false；传 true 被拒绝' },
          confirm: { type: 'boolean', description: 'DROP / 无 WHERE 语句需 true 确认' },
        },
      },
      handler: args => handleExecute(args || {}),
    });
    ctx.tool({
      name: 'sqlite_tables',
      description: '列出数据库中的表与视图（查询 sqlite_master）。只读。',
      parameters: {
        type: 'object', required: ['db'],
        properties: {
          db: { type: 'string', description: '数据库文件路径（限挂载根内）' },
          readonly: { type: 'boolean', description: '默认 true' },
        },
      },
      handler: args => handleTables(args || {}),
    });
    ctx.tool({
      name: 'sqlite_schema',
      description: '查看单表结构：建表 SQL、列（PRAGMA table_info）、索引（PRAGMA index_list）、外键（PRAGMA foreign_key_list）。只读。',
      parameters: {
        type: 'object', required: ['db', 'table'],
        properties: {
          db: { type: 'string', description: '数据库文件路径（限挂载根内）' },
          table: { type: 'string', description: '表名（标识符，仅字母数字下划线$）' },
          readonly: { type: 'boolean', description: '默认 true' },
        },
      },
      handler: args => handleSchema(args || {}),
    });
    ctx.tool({
      name: 'sqlite_export',
      description: '导出数据库。format=csv：对 query 结果导出 CSV 到 outputPath；format=dump：VACUUM INTO 生成一致性副本。outputPath 限挂载根内且不可写在只读 /context。',
      parameters: {
        type: 'object', required: ['db', 'format', 'outputPath'],
        properties: {
          db: { type: 'string', description: '数据库文件路径（限挂载根内）' },
          format: { type: 'string', enum: ['csv', 'dump'], description: '导出格式' },
          outputPath: { type: 'string', description: '输出文件路径（限挂载根内、不可在 /context）' },
          query: { type: 'string', description: 'CSV 导出时的查询 SQL；缺省列出表/视图' },
          timeout: { type: 'number', description: 'busy_timeout 毫秒，默认 5000' },
        },
      },
      handler: args => handleExport(args || {}),
    });
    ctx.provide && ctx.provide('sqlite');
  },
};
