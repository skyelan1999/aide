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

/* 协议 v1 的受限 ctx（协议 §3）：logger 走 stderr；effect/on 只登记；provide/tool/slot 记入 surface */
function makeCtx(surface) {
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
      surface.tools.push({ name: String(d.name || '匿名工具').slice(0, 128), description: String(d.description || '').slice(0, 512) });
    },
    slot: def => {
      const d = def && typeof def === 'object' ? def : {};
      surface.slots.push({ id: String(d.id || 'slot').slice(0, 128), name: String(d.name || d.id || '槽位').slice(0, 128) });
    },
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
    const file = path.join(pluginsDir, entry.id, entry.main || 'index.js');
    const result = loadPlugin(file);
    if (result.error) {
      item.error = result.error;
      out.plugins.push(item);
      continue;
    }
    if (result.plugin.name && typeof result.plugin.name === 'string') item.name = String(result.plugin.name).slice(0, 64);
    try {
      const disposer = result.plugin.apply(makeCtx(item));
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

if (command === 'validate') validateCommand(args[0]);
else if (command === 'run') runCommand(args[0], args[1], args[2]);
else {
  console.error('未知命令: ' + command);
  process.exit(2);
}
