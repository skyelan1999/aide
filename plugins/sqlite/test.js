'use strict';
/* SQLite 插件集成测试：直接 require 插件模块、构造协议 v1.1 假 ctx、驱动各 handler。
 *
 * 运行（容器内，/workspace 挂载仓库根）：
 *   docker run --rm -v "$PWD":/workspace -w /workspace node:24-bookworm-slim node plugins/sqlite/test.js
 */
const fs = require('fs');
const path = require('path');
const { DatabaseSync } = require('node:sqlite');

// 容器内确保挂载根存在（node 镜像默认 root，可写根 fs）。
for (const d of ['/context', '/local']) { try { fs.mkdirSync(d, { recursive: true }); } catch (_) {} }

const mod = require('./index.js');
const tools = new Map();
const fakeCtx = {
  logger: { info() {}, warn() {}, error() {} },
  tool(def) { tools.set(def.name, def); },
  provide() {}, slot() {},
};
mod.apply(fakeCtx);
const call = (name, args) => tools.get(name).handler(args);

/* ---- 极简断言 ---- */
let pass = 0, fail = 0;
const failures = [];
function ok(cond, name) {
  if (cond) { pass++; console.log('  ✓', name); }
  else { fail++; failures.push(name); console.log('  ✗', name); }
}
function throws(name, fn) {
  try { fn(); ok(false, name + '（未抛错）'); }
  catch (_) { ok(true, name); }
}

const ROOT = '/workspace/.sqltest';
fs.rmSync(ROOT, { recursive: true, force: true });
fs.mkdirSync(ROOT, { recursive: true });
let seq = 0;
const fresh = (ext) => path.join(ROOT, 't' + (seq++) + '.' + (ext || 'db'));

console.log('== 1. 基础 CRUD：建表/插入(lastInsertRowid)/更新/删除/查询一致 ==');
{
  const db = fresh();
  call('sqlite_query', { db, sql: 'CREATE TABLE users(id INTEGER PRIMARY KEY, name TEXT, age INTEGER)', readonly: false });
  const ins = call('sqlite_query', { db, sql: 'INSERT INTO users(name, age) VALUES (?, ?)', params: ['alice', 30], readonly: false });
  ok(ins.lastInsertRowid === 1, 'insert lastInsertRowid=1, got ' + ins.lastInsertRowid);
  call('sqlite_query', { db, sql: 'INSERT INTO users(name, age) VALUES (?, ?)', params: ['bob', 25], readonly: false });
  const upd = call('sqlite_query', { db, sql: 'UPDATE users SET age = ? WHERE name = ?', params: [31, 'alice'], readonly: false });
  ok(upd.changes === 1, 'update changes=1');
  const rows = call('sqlite_query', { db, sql: 'SELECT id, name, age FROM users ORDER BY id' });
  ok(rows.rowCount === 2 && rows.columns.join(',') === 'id,name,age', 'rows+columns 正确: ' + JSON.stringify(rows.columns));
  ok(rows.rows[0].age === 31 && rows.rows[1].name === 'bob', '查询结果与写入一致');
  const del = call('sqlite_query', { db, sql: 'DELETE FROM users WHERE id = ?', params: [2], readonly: false, confirm: true });
  ok(del.changes === 1, 'delete changes=1');
}

console.log('== 2. 事务：成功 commit 与失败 rollback ==');
{
  const db = fresh();
  call('sqlite_query', { db, sql: 'CREATE TABLE a(id INTEGER PRIMARY KEY, v TEXT)', readonly: false });
  const committed = call('sqlite_transaction', { db, statements: [
    { sql: 'INSERT INTO a(v) VALUES (?)', params: ['x'] },
    { sql: 'INSERT INTO a(v) VALUES (?)', params: ['y'] },
  ]});
  ok(committed.committed === true && committed.changes === 2, '事务 commit changes=2');
  const afterOk = call('sqlite_query', { db, sql: 'SELECT count(*) c FROM a' });
  ok(afterOk.rows[0].c === 2, 'commit 后 2 行');
  const rolled = call('sqlite_transaction', { db, statements: [
    { sql: 'INSERT INTO a(v) VALUES (?)', params: ['z'] },
    { sql: 'INSERT INTO a(v) VALUES (?)', params: ['z'] },
    { sql: 'NOPE SYNTAX ERR' },
  ]});
  ok(rolled.committed === false, '事务失败 rollback（committed=false）');
  const afterBad = call('sqlite_query', { db, sql: 'SELECT count(*) c FROM a' });
  ok(afterBad.rows[0].c === 2, 'rollback 后仍为 2 行（未脏写）');
}

console.log('== 3. tables / schema / 索引 / 外键 ==');
{
  const db = fresh();
  call('sqlite_query', { db, sql: 'CREATE TABLE parent(id INTEGER PRIMARY KEY, p TEXT)', readonly: false });
  call('sqlite_query', { db, sql: 'CREATE TABLE child(id INTEGER PRIMARY KEY, pid INTEGER REFERENCES parent(id))', readonly: false });
  call('sqlite_query', { db, sql: 'CREATE INDEX idx_p ON parent(p)', readonly: false });
  const tables = call('sqlite_tables', { db });
  ok(tables.tables.some(t => t.name === 'parent') && tables.tables.some(t => t.name === 'child'), 'tables 列出 parent/child');
  const sch = call('sqlite_schema', { db, table: 'child' });
  ok(sch.columns.length === 2, 'child 有 2 列');
  ok(sch.foreignKeys.length === 1 && sch.foreignKeys[0].table === 'parent', '外键指向 parent');
  const ps = call('sqlite_schema', { db, table: 'parent' });
  ok(ps.indexes.some(i => i.name === 'idx_p'), '索引 idx_p 可见');
  ok(/CREATE TABLE parent/.test(ps.sql), '建表 SQL 返回');
}

console.log('== 4. 默认只读写被拒；显式 readonly=false 可写 ==');
{
  const db = fresh();
  throws('默认 readonly=true 时 INSERT 被拒', () => call('sqlite_query', { db, sql: 'CREATE TABLE x(a)' }));
  call('sqlite_query', { db, sql: 'CREATE TABLE x(a)', readonly: false });
  call('sqlite_query', { db, sql: 'INSERT INTO x VALUES (1)', readonly: false });
  const r = call('sqlite_query', { db, sql: 'SELECT count(*) c FROM x' });
  ok(r.rows[0].c === 1, 'readonly=false 写入成功并可读回');
}

console.log('== 5. 危险语句拦截：DROP / 无 WHERE ==');
{
  const db = fresh();
  call('sqlite_query', { db, sql: 'CREATE TABLE d(a)', readonly: false });
  throws('DROP TABLE 无 confirm 被拦截', () => call('sqlite_query', { db, sql: 'DROP TABLE d', readonly: false }));
  throws('UPDATE 无 WHERE 被拦截', () => call('sqlite_query', { db, sql: 'UPDATE d SET a=1', readonly: false }));
  throws('DELETE 无 WHERE 被拦截', () => call('sqlite_query', { db, sql: 'DELETE FROM d', readonly: false }));
  const okDrop = call('sqlite_query', { db, sql: 'DROP TABLE d', readonly: false, confirm: true });
  ok(typeof okDrop.changes === 'number', 'confirm=true 后 DROP 放行');
}

console.log('== 6. 根外路径与 ../ 被拒 ==');
{
  throws('绝对路径 /etc/x.db 被拒', () => call('sqlite_tables', { db: '/etc/x.db' }));
  throws('../ 越界被拒', () => call('sqlite_tables', { db: '../outside.db' }));
  throws('相对 / 根外被拒', () => call('sqlite_tables', { db: '/root/secret.db' }));
}

console.log('== 7. ATTACH 根外与 load_extension 被拒 ==');
{
  const db = fresh();
  call('sqlite_query', { db, sql: 'CREATE TABLE e(a)', readonly: false });
  throws('ATTACH 根外路径被拒', () => call('sqlite_query', { db, sql: "ATTACH '/etc/evil.db' AS evil", readonly: false }));
  throws('load_extension 被拒', () => call('sqlite_query', { db, sql: "SELECT load_extension('x')", readonly: false }));
}

console.log('== 8. 注入参数值不改变语义 ==');
{
  const db = fresh();
  call('sqlite_query', { db, sql: 'CREATE TABLE u(id INTEGER PRIMARY KEY, token TEXT)', readonly: false });
  call('sqlite_query', { db, sql: "INSERT INTO u(token) VALUES ('secret')", readonly: false });
  const r = call('sqlite_query', { db, sql: 'SELECT count(*) c FROM u WHERE token = ?', params: ["' OR '1'='1"] });
  ok(r.rows[0].c === 0, "参数绑定使 ' OR 1=1 被当作字面量（0 行命中），实际 " + r.rows[0].c);
}

console.log('== 9. 大结果截断有 truncated 标记 ==');
{
  const db = fresh();
  call('sqlite_query', { db, sql: 'CREATE TABLE big(id INTEGER PRIMARY KEY, v TEXT)', readonly: false });
  const stmts = [];
  for (let i = 0; i < 600; i++) stmts.push({ sql: 'INSERT INTO big(v) VALUES (?)', params: ['r' + i] });
  call('sqlite_transaction', { db, statements: stmts });
  const r = call('sqlite_query', { db, sql: 'SELECT id, v FROM big ORDER BY id' });
  ok(r.truncated === true && r.rowCount === 500 && r.totalRows === 600, '截断到 500 行且 truncated=true');
}

console.log('== 10. busy/超时不挂死 ==');
{
  const db = fresh();
  call('sqlite_query', { db, sql: 'CREATE TABLE lockt(a)', readonly: false });
  call('sqlite_query', { db, sql: 'INSERT INTO lockt VALUES (1)', readonly: false });
  const locker = new DatabaseSync(db);
  locker.exec('BEGIN EXCLUSIVE');
  locker.exec('INSERT INTO lockt VALUES (2)');
  let errored = false, elapsed = 0;
  const t0 = Date.now();
  try {
    call('sqlite_query', { db, sql: 'INSERT INTO lockt VALUES (3)', readonly: false, timeout: 400 });
  } catch (e) { errored = true; }
  elapsed = Date.now() - t0;
  try { locker.exec('ROLLBACK'); } catch (_) {}
  locker.close();
  ok(errored && elapsed < 5000, '锁冲突在 busy_timeout 内报错返回（未挂死），耗时 ' + elapsed + 'ms');
}

console.log('== 11. 跨 call 重新打开读到持久数据 ==');
{
  const db = fresh();
  call('sqlite_query', { db, sql: 'CREATE TABLE persist(id INTEGER PRIMARY KEY, note TEXT)', readonly: false });
  call('sqlite_query', { db, sql: 'INSERT INTO persist(note) VALUES (?)', params: ['hello-persist'], readonly: false });
  // 全新 handler 调用（模拟新进程），不共享连接
  const again = call('sqlite_query', { db, sql: 'SELECT note FROM persist WHERE id = ?', params: [1] });
  ok(again.rows[0].note === 'hello-persist', '重新打开文件读到上一次 call 写入的数据');
}

console.log('== 12. 导出 CSV / dump 正确且限根 ==');
{
  const db = fresh();
  call('sqlite_query', { db, sql: 'CREATE TABLE exp(id INTEGER PRIMARY KEY, name TEXT)', readonly: false });
  call('sqlite_query', { db, sql: "INSERT INTO exp(name) VALUES ('a'),('b')", readonly: false });
  const csvOut = path.join(ROOT, 'out.csv');
  const csv = call('sqlite_export', { db, format: 'csv', query: 'SELECT id, name FROM exp ORDER BY id', outputPath: csvOut });
  ok(csv.rows === 2 && fs.existsSync(csvOut), 'CSV 写出 2 行');
  const content = fs.readFileSync(csvOut, 'utf8');
  ok(content.startsWith('id,name') && content.includes('1,a') && content.includes('2,b'), 'CSV 内容正确');
  const dumpOut = path.join(ROOT, 'backup.db');
  const dump = call('sqlite_export', { db, format: 'dump', outputPath: dumpOut });
  ok(dump.bytes > 0 && fs.existsSync(dumpOut), 'dump(VACUUM INTO) 生成副本');
  throws('导出到 /etc 被拒', () => call('sqlite_export', { db, format: 'csv', outputPath: '/etc/evil.csv' }));
}

/* 清理 */
fs.rmSync(ROOT, { recursive: true, force: true });

console.log('\n========================================');
console.log('通过 ' + pass + ' / ' + (pass + fail));
if (fail) { console.log('失败用例: ' + failures.join('; ')); process.exit(1); }
console.log('ALL_TESTS_PASS');
