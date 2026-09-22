'use strict';
/* aide 插件协议 v1 宿主（doc/plugin-protocol.md）
 *
 * 用法（由 Go 以 -e 方式调用，无 shell）：
 *   node -e <本脚本> validate <pluginFile>
 *   node -e <本脚本> run <pluginsDir> <enabledListJson> <outFile>
 * -e 模式下 process.argv[1] 起为参数。
 *
 * 输出约定：stdout 仅输出一行 JSON 结果；插件日志一律走 stderr，避免污染结果。
 */
const fs = require('fs');
const path = require('path');

const command = process.argv[1];
const args = process.argv.slice(2);

const log = (...a) => console.error('[plugin]', ...a);

/* 加载插件模块并做 DSH/Cordis 形态检查（协议 §2） */
function loadPlugin(file) {
  let mod;
  try {
    delete require.cache[require.resolve(file)];
    mod = require(file);
  } catch (err) {
    return { error: '模块加载失败: ' + (err && err.message ? String(err.message).slice(0, 300) : String(err)) };
  }
  let candidate = mod;
  if (mod && mod.default !== undefined) candidate = mod.default;
  if (typeof candidate === 'function') {
    if (candidate.length > 1) return { error: '工厂函数参数超过 1 个，依赖注入超出协议 v1 子集' };
    let produced;
    try {
      produced = candidate({});
    } catch (err) {
      return { error: '工厂函数执行失败: ' + String(err && err.message).slice(0, 300) };
    }
    if (!produced || typeof produced.apply !== 'function') return { error: '工厂函数未返回含 apply(ctx) 的插件对象' };
    return { plugin: produced };
  }
  if (candidate && typeof candidate.apply === 'function') return { plugin: candidate };
  return { error: '插件必须默认导出含 apply(ctx) 的对象（DSH/Cordis 形态）' };
}

/* 协议 v1.1 的受限 ctx（协议 §3）：logger 走 stderr；effect/on 只登记；provide/slot 记入 surface；
   tool 注册 name/description/parameters 与 handler（handler 不序列化，仅供 call 命令调用）。 */
function makeCtx(surface, registry) {
  const noop = () => () => {};
  return {
    logger: { info: log, warn: log, error: log },
    effect: noop,
    on: noop,
    provide: name => {
      if (typeof name === 'string') surface.provided.push(String(name).slice(0, 128));
      return () => {};
    },
    tool: def => {
      const d = def && typeof def === 'object' ? def : {};
      const name = String(d.name || '匿名工具').slice(0, 128);
      surface.tools.push({ name, description: String(d.description || '').slice(0, 512), executable: typeof d.handler === 'function' });
      if (typeof d.handler === 'function' && registry) {
        registry.set(name, { handler: d.handler, plugin: surface.__pluginId || '' });
      }
    },
    slot: def => {
      const d = def && typeof def === 'object' ? def : {};
      surface.slots.push({ id: String(d.id || 'slot').slice(0, 128), name: String(d.name || d.id || '槽位').slice(0, 128) });
    },
  };
}

/* 协议 v1.1 工具 api：读操作直接执行；写/命令返回提案对象（由 Go 侧进入用户批准流程，P2 原则）。 */
function makeToolAPI() {
  const fs2 = require('fs');
  const path2 = require('path');
  const safeJoin = p => {
    const rel = String(p || '.').replace(/\\/g, '/').replace(/^\.\//, '');
    if (rel.startsWith('/') || rel.split('/').includes('..')) throw new Error('路径越界');
    return path2.join('/workspace', rel);
  };
  return {
    readFile: rel => fs2.readFileSync(safeJoin(rel), 'utf8'),
    listFiles: rel => fs2.readdirSync(safeJoin(rel), { withFileTypes: true }).map(e => (e.isDirectory() ? e.name + '/' : e.name)),
    proposeWrite: (rel, content) => ({ proposal: { type: 'file', path: rel, content: String(content) } }),
    proposeCommand: cmd => ({ proposal: { type: 'command', command: String(cmd) } }),
    log: log,
  };
}

function validateCommand(file) {
  const result = loadPlugin(file);
  if (result.error) {
    console.log(JSON.stringify({ ok: false, error: result.error }));
    return;
  }
  const name = result.plugin.name && typeof result.plugin.name === 'string' ? String(result.plugin.name).slice(0, 64) : '';
  console.log(JSON.stringify({ ok: true, name }));
}

function runCommand(pluginsDir, enabledJSON, outFile) {
  let enabled;
  try {
    enabled = JSON.parse(enabledJSON);
  } catch (err) {
    console.log(JSON.stringify({ ok: false, error: 'enabledListJson 解析失败' }));
    process.exit(3);
  }
  const out = { generatedAt: new Date().toISOString(), plugins: [] };
  for (const entry of enabled) {
    const item = { id: entry.id, name: entry.name || entry.id, error: '', tools: [], slots: [], provided: [] };
    item.__pluginId = entry.id;
    const file = path.join(pluginsDir, entry.id, entry.main || 'index.js');
    const result = loadPlugin(file);
    if (result.error) {
      item.error = result.error;
      out.plugins.push(item);
      continue;
    }
    if (result.plugin.name && typeof result.plugin.name === 'string') item.name = String(result.plugin.name).slice(0, 64);
    try {
      const disposer = result.plugin.apply(makeCtx(item, new Map()));
      if (typeof disposer === 'function') {
        try {
          disposer();
        } catch (err) {
          /* 清理回调异常不影响其他插件 */
        }
      }
    } catch (err) {
      item.error = 'apply(ctx) 执行失败: ' + String(err && err.message).slice(0, 300);
    }
    out.plugins.push(item);
  }
  try {
    fs.writeFileSync(outFile, JSON.stringify(out));
  } catch (err) {
    console.log(JSON.stringify({ ok: false, error: 'surface 写入失败: ' + String(err && err.message) }));
    process.exit(4);
  }
  console.log(JSON.stringify({ ok: true, count: out.plugins.length }));
}

/* 协议 v1.1：call <pluginsDir> <requestJson> <outFile>
   requestJson = {plugin, tool, args}；加载插件 → 重建 registry → 调 handler(args, api)（60s 超时）→ 结果写 outFile。 */
function callCommand(pluginsDir, requestJSON, outFile) {
  let req;
  try {
    req = JSON.parse(requestJSON);
  } catch (err) {
    console.log(JSON.stringify({ ok: false, error: 'requestJson 解析失败' }));
    process.exit(3);
  }
  const write = obj => fs.writeFileSync(outFile, JSON.stringify(obj));
  const file = path.join(pluginsDir, req.plugin, 'index.js');
  const result = loadPlugin(file);
  if (result.error) {
    write({ ok: false, error: result.error });
    console.log(JSON.stringify({ ok: false }));
    return;
  }
  const surface = { id: req.plugin, name: req.plugin, error: '', tools: [], slots: [], provided: [] };
  const registry = new Map();
  try {
    result.plugin.apply(makeCtx(surface, registry));
  } catch (err) {
    write({ ok: false, error: 'apply 执行失败: ' + String(err && err.message).slice(0, 300) });
    console.log(JSON.stringify({ ok: false }));
    return;
  }
  const entry = registry.get(req.tool);
  if (!entry) {
    write({ ok: false, error: '工具未注册: ' + req.tool });
    console.log(JSON.stringify({ ok: false }));
    return;
  }
  let finished = false;
  const timer = setTimeout(() => {
    if (!finished) {
      finished = true;
      write({ ok: false, error: '工具执行超时（60s）' });
      process.exit(0);
    }
  }, 60000);
  Promise.resolve()
    .then(() => entry.handler(req.args || {}, makeToolAPI()))
    .then(value => {
      if (finished) return;
      finished = true;
      clearTimeout(timer);
      write({ ok: true, result: value === undefined ? null : value });
      console.log(JSON.stringify({ ok: true }));
    })
    .catch(err => {
      if (finished) return;
      finished = true;
      clearTimeout(timer);
      write({ ok: false, error: String(err && err.message).slice(0, 500) });
      console.log(JSON.stringify({ ok: false }));
    });
}

if (command === 'validate') validateCommand(args[0]);
else if (command === 'run') runCommand(args[0], args[1], args[2]);
else if (command === 'call') callCommand(args[0], args[1], args[2]);
else {
  console.error('未知命令: ' + command);
  process.exit(2);
}
