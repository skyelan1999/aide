'use strict';
/* aide 插件协议 v1 宿主（docs/plugin-protocol.md）
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
function makeCtx(surface, registry, settings, services = new Map(), required = []) {
  const noop = () => () => {};
  return {
    settings: settings && typeof settings === 'object' && !Array.isArray(settings) ? settings : {},
    logger: { info: log, warn: log, error: log },
    effect: noop,
    on: noop,
    provide: (name, value) => {
      if (typeof name !== 'string' || !/^[A-Za-z_][A-Za-z0-9_.:-]{0,127}$/.test(name)) throw new Error('服务名无效');
      if (services.has(name)) throw new Error('服务重复注册: ' + name);
      services.set(name, {owner: surface.__pluginId || surface.id, value});
      surface.provided.push(name);
      return () => { if (services.get(name)?.owner === (surface.__pluginId || surface.id)) services.delete(name); };
    },
    consume: name => {
      if (!required.includes(name)) throw new Error('服务依赖未声明: ' + name);
      if (!services.has(name)) throw new Error('服务依赖不可用: ' + name);
      return services.get(name).value;
    },
    tool: def => {
      const d = def && typeof def === 'object' ? def : {};
      const name = String(d.name || '');
      if (!/^[A-Za-z_][A-Za-z0-9_.:-]{0,127}$/.test(name)) throw new Error('工具名无效');
      if (registry && registry.has(name)) throw new Error('工具重复注册: ' + name);
      const toolEntry = { name, description: String(d.description || '').slice(0, 512), executable: typeof d.handler === 'function' };
      if (d.parameters && typeof d.parameters === 'object') toolEntry.parameters = d.parameters;
      surface.tools.push(toolEntry);
      if (typeof d.handler === 'function' && registry) {
        registry.set(name, { handler: d.handler, plugin: surface.__pluginId || surface.id || '' });
      }
    },
    slot: def => {
      const d = def && typeof def === 'object' ? def : {};
      surface.slots.push({ id: String(d.id || 'slot').slice(0, 128), name: String(d.name || d.id || '槽位').slice(0, 128) });
    },
  };
}

/* 协议 v1.1 工具 api：读操作直接执行；写/命令返回提案对象（由 Go 侧进入用户批准流程，P2 原则）。 */
function makeToolAPI(pluginDir) {
  const fs2 = require('fs');
  const path2 = require('path');
  const { spawn } = require('child_process');
  const realPluginDir = fs2.realpathSync(pluginDir);
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
    runPython: (script, input = {}, options = {}) => new Promise((resolve, reject) => {
      const rel = String(script || '').replace(/\\/g, '/');
      if (!rel || rel.startsWith('/') || rel.split('/').some(part => !part || part === '.' || part === '..')) return reject(new Error('Python 脚本路径必须是插件包内的相对路径'));
      const scriptPath = path2.resolve(realPluginDir, rel);
      let realScript;
      try { realScript = fs2.realpathSync(scriptPath); } catch (_) { return reject(new Error('Python 脚本不存在')); }
      if (!realScript.startsWith(realPluginDir + path2.sep)) return reject(new Error('Python 脚本路径越界'));
      const timeoutMs = Math.max(1000, Math.min(Number(options.timeoutMs) || 60000, 280000));
      const outputLimit = 8 << 20;
      const proc = spawn('python3', ['-I', realScript], { cwd: '/workspace', env: { PATH: '/usr/local/bin:/usr/bin:/bin', HOME: '/home/aide', PYTHONUNBUFFERED: '1' }, stdio: ['pipe', 'pipe', 'pipe'] });
      let stdout = Buffer.alloc(0), stderr = Buffer.alloc(0), settled = false;
      const finish = (err, value) => { if (settled) return; settled = true; clearTimeout(timer); err ? reject(err) : resolve(value); };
      const timer = setTimeout(() => { proc.kill('SIGKILL'); finish(new Error('Python 工具执行超时')); }, timeoutMs);
      proc.stdout.on('data', chunk => { stdout = Buffer.concat([stdout, chunk]); if (stdout.length > outputLimit) { proc.kill('SIGKILL'); finish(new Error('Python 工具标准输出超过 8 MiB')); } });
      proc.stderr.on('data', chunk => { if (stderr.length < 64 << 10) stderr = Buffer.concat([stderr, chunk]).subarray(0, 64 << 10); });
      proc.on('error', err => finish(new Error('启动 Python 失败: ' + err.message)));
      proc.on('close', code => {
        if (settled) return;
        if (code !== 0) return finish(new Error(('Python 工具失败: ' + stderr.toString('utf8')).slice(0, 1000)));
        finish(null, stdout.toString('utf8'));
      });
      try { proc.stdin.end(JSON.stringify(input)); } catch (err) { proc.kill('SIGKILL'); finish(err); }
    }),
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

// Assemble service providers before consumers. A missing, ambiguous or cyclic
// dependency disables its consumer instead of exposing partially wired tools.
// Contracts come from trusted enabled plugin code, not model/tool arguments.
function assemblePlugins(pluginsDir, enabled, target = '', reserved = []) {
  const modules = new Map(), providers = new Map(), states = new Map();
  const out = {generatedAt: new Date().toISOString(), plugins: []};
  const registry = new Map(reserved.map(name => [name, {plugin: 'builtin'}])), services = new Map(), disposers = [];
  const names = values => {
    if (values == null) return [];
    if (!Array.isArray(values) || values.length > 64 || values.some(v => typeof v !== 'string' || !/^[A-Za-z_][A-Za-z0-9_.:-]{0,127}$/.test(v)) || new Set(values).size !== values.length) throw new Error('服务契约必须是最多64项唯一服务名数组');
    return values;
  };
  for (const entry of enabled) {
    const item = {id: entry.id, name: entry.name || entry.id, error: '', tools: [], slots: [], provided: [], requires: []};
    item.__pluginId = entry.id; out.plugins.push(item);
    try {
      if (!/^[a-z0-9][a-z0-9_-]{0,63}$/.test(entry.id) || String(entry.main || 'index.js').startsWith('/') || String(entry.main || 'index.js').split(/[\\/]/).includes('..')) throw new Error('插件入口路径无效');
      if (modules.has(entry.id)) throw new Error('插件ID重复');
      const loaded = loadPlugin(path.join(pluginsDir, entry.id, entry.main || 'index.js'));
      if (loaded.error) throw new Error(loaded.error);
      const requires = names(loaded.plugin.requires), provides = names(loaded.plugin.provides);
      item.requires = requires;
      modules.set(entry.id, {entry, item, plugin: loaded.plugin, requires, provides});
      for (const name of provides) { if (!providers.has(name)) providers.set(name, []); providers.get(name).push(entry.id); }
    } catch (error) { item.error = String(error.message || error).slice(0, 300); }
  }
  const visit = id => {
    const mod = modules.get(id); if (!mod) throw new Error('依赖插件不可用: ' + id);
    if (states.get(id) === 'ready') return;
    if (states.get(id) === 'visiting') throw new Error('服务依赖存在循环: ' + id);
    if (states.get(id) === 'failed') throw new Error(mod.item.error);
    states.set(id, 'visiting');
    try {
      for (const name of mod.requires) {
        const owners = providers.get(name) || [];
        if (owners.length !== 1) throw new Error('服务缺失或存在多个提供者: ' + name);
        visit(owners[0]);
        if (!services.has(name)) throw new Error('声明的服务未提供: ' + name);
      }
      const dispose = mod.plugin.apply(makeCtx(mod.item, registry, mod.entry.settings, services, mod.requires));
      if (dispose && typeof dispose.then === 'function') throw new Error('同步服务装配不支持异步apply');
      if (typeof dispose === 'function') disposers.push(dispose);
      for (const name of mod.provides) if (services.get(name)?.owner !== id) throw new Error('声明的服务未提供: ' + name);
      states.set(id, 'ready');
    } catch (error) {
      mod.item.error = String(error.message || error).slice(0, 300);
      for (const [name, entry] of registry) if (entry.plugin === id) registry.delete(name);
      for (const [name, entry] of services) if (entry.owner === id) services.delete(name);
      mod.item.tools = []; states.set(id, 'failed'); throw error;
    }
  };
  if (target) { try { visit(target); } catch (_) {} }
  else for (const id of modules.keys()) { try { visit(id); } catch (_) {} }
  const dispose = () => { for (const fn of disposers.reverse()) { try { fn(); } catch (_) {} } };
  return {out, registry, dispose};
}
function runCommand(pluginsDir, enabledJSON, outFile) {
  let enabled;
  let reserved = [];
  try { const document = JSON.parse(enabledJSON); enabled = Array.isArray(document) ? document : document.plugins; reserved = Array.isArray(document.reservedTools) ? document.reservedTools : []; if (!Array.isArray(enabled) || enabled.length > 256) throw new Error('invalid plugin list'); }
  catch (error) { console.log(JSON.stringify({ok:false,error:String(error.message)})); process.exit(3); }
  const assembled = assemblePlugins(pluginsDir, enabled, '', reserved);
  assembled.dispose();
  try { fs.writeFileSync(outFile, JSON.stringify(assembled.out)); }
  catch (error) { console.log(JSON.stringify({ok:false,error:'surface 写入失败: ' + error.message})); process.exit(4); }
  console.log(JSON.stringify({ok:true,count:assembled.out.plugins.length}));
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
  const pluginDir = path.join(pluginsDir, req.plugin);
  let main = 'index.js';
  try {
    const manifest = JSON.parse(fs.readFileSync(path.join(pluginDir, 'manifest.json'), 'utf8'));
    if (typeof manifest.main === 'string' && manifest.main) main = manifest.main;
  } catch (_) {}
  if (main.startsWith('/') || main.split(/[\\/]/).includes('..')) {
    write({ ok: false, error: '插件入口路径无效' });
    console.log(JSON.stringify({ ok: false }));
    return;
  }
  const enabled = Array.isArray(req.enabledPlugins) ? req.enabledPlugins : [{id:req.plugin,main,settings:req.settings}];
  const assembled = assemblePlugins(pluginsDir, enabled, req.plugin, Array.isArray(req.reservedTools) ? req.reservedTools : []);
  const registry = assembled.registry;
  const entry = registry.get(req.tool);
  if (!entry || entry.plugin !== req.plugin) {
    assembled.dispose();
    write({ ok: false, error: '工具未注册、服务依赖无效或归属不匹配: ' + req.tool });
    console.log(JSON.stringify({ ok: false }));
    return;
  }
  let finished = false;
  const timer = setTimeout(() => {
    if (!finished) {
      finished = true;
      write({ ok: false, error: '工具执行超时（300s）' });
      process.exit(0);
    }
  }, 300000);
  Promise.resolve()
    .then(() => entry.handler(req.args || {}, makeToolAPI(path.join(pluginsDir, req.plugin))))
    .then(value => {
      if (finished) return;
      finished = true;
      clearTimeout(timer);
      assembled.dispose();
      write({ ok: true, result: value === undefined ? null : value });
      console.log(JSON.stringify({ ok: true }));
    })
    .catch(err => {
      if (finished) return;
      finished = true;
      clearTimeout(timer);
      assembled.dispose();
      write({ ok: false, error: String(err && err.message).slice(0, 500) });
      console.log(JSON.stringify({ ok: false }));
    });
}

/* 协议 v1.2：daemon <pluginPath>
 * 长驻宿主：stdin/stdout 行分隔 JSON-RPC。stdout 仅输出 JSON-RPC 行（结果/错误/事件），
 * 插件日志一律走 stderr。加载插件并缓存 require，工具调用经 IPC 转发到常驻 handler。
 *
 * 入站:  {"jsonrpc":"2.0","id":N,"method":"tool.call","params":{tool,args}}
 * 入站:  {"jsonrpc":"2.0","id":N,"method":"ping"}            （心跳）
 * 出站:  {"jsonrpc":"2.0","id":N,"result":<任意>}             （成功，result 即 handler 返回值）
 * 出站:  {"jsonrpc":"2.0","id":N,"error":{code,message}}      （失败）
 * 出站:  {"method":"event","params":{type,time,direction,length,payloadTruncated,...}}
 * 生命周期：apply(ctx) → 可选 start() → ready 事件 → 服务 tool.call；stdin EOF / SIGTERM → 可选 stop() → 退出。
 */
function sendLine(obj) { process.stdout.write(JSON.stringify(obj) + '\n'); }

function daemonCommand(pluginPath) {
  let loaded;
  try {
    loaded = loadPlugin(pluginPath);
  } catch (err) {
    sendLine({ method: 'event', params: { type: 'log', level: 'error', message: '加载异常: ' + String(err && err.message).slice(0, 300), time: new Date().toISOString() } });
    process.exit(1);
  }
  if (loaded.error) {
    sendLine({ method: 'event', params: { type: 'log', level: 'error', message: '插件加载失败: ' + loaded.error, time: new Date().toISOString() } });
    process.exit(1);
  }
  const plugin = loaded.plugin;
  const registry = new Map();
  const pluginId = path.basename(path.dirname(pluginPath));
  const surface = { id: pluginId, name: plugin.name || pluginId, error: '', tools: [], slots: [], provided: [] };

  /* 事件上报：type 限 traffic/event/log；其余字段透传（direction/length/payloadTruncated/subtype/message…）。 */
  const emit = (type, fields) => {
    if (!['traffic', 'event', 'log'].includes(type)) type = 'event';
    const params = Object.assign({ type, time: new Date().toISOString() }, fields || {});
    sendLine({ method: 'event', params });
  };

  let settings = {};
  try {
    const parsed = JSON.parse(process.env.AIDE_PLUGIN_SETTINGS || '{}');
    if (parsed && typeof parsed === 'object' && !Array.isArray(parsed)) settings = parsed;
  } catch (_) {}
  const ctx = makeCtx(surface, registry, settings);
  ctx.emit = emit; // daemon 专有：插件上报流量/事件/日志

  let stopped = false;
  const shutdown = () => {
    if (stopped) return;
    stopped = true;
    try { if (typeof plugin.stop === 'function') plugin.stop(); } catch (err) { log('stop() 异常:', err && err.message); }
    process.exit(0);
  };

  Promise.resolve()
    .then(() => plugin.apply(ctx))
    .then(() => (typeof plugin.start === 'function' ? plugin.start() : undefined))
    .then(() => {
      sendLine({
        method: 'event',
        params: { type: 'event', subtype: 'ready', time: new Date().toISOString(), tools: Array.from(registry.keys()) },
      });

      const readline = require('readline');
      const rl = readline.createInterface({ input: process.stdin });
      rl.on('line', (line) => {
        let msg;
        try { msg = JSON.parse(line); } catch (_) { return; } // 跳过非法行
        if (msg.method === 'ping') {
          if (msg.id !== undefined && msg.id !== null) sendLine({ jsonrpc: '2.0', id: msg.id, result: 'pong' });
          else sendLine({ method: 'pong' });
          return;
        }
        if (msg.method === 'tool.call' && msg.id !== undefined && msg.id !== null) {
          const params = msg.params || {};
          const entry = registry.get(params.tool);
          if (!entry) {
            sendLine({ jsonrpc: '2.0', id: msg.id, error: { code: -32601, message: '工具未注册: ' + params.tool } });
            return;
          }
          Promise.resolve()
            .then(() => entry.handler(params.args || {}, makeToolAPI(path.dirname(pluginPath))))
            .then((value) => sendLine({ jsonrpc: '2.0', id: msg.id, result: value === undefined ? null : value }))
            .catch((err) => sendLine({ jsonrpc: '2.0', id: msg.id, error: { code: -32000, message: String(err && err.message).slice(0, 500) } }));
        }
      });
      rl.on('close', shutdown);
      process.on('SIGTERM', shutdown);
      process.on('SIGINT', shutdown);
    })
    .catch((err) => {
      emit('log', { level: 'error', message: 'daemon 启动失败: ' + String(err && err.message).slice(0, 300) });
      process.exit(1);
    });
}

if (command === 'validate') validateCommand(args[0]);
else if (command === 'run') runCommand(args[0], args[1], args[2]);
else if (command === 'call') callCommand(args[0], args[1], args[2]);
else if (command === 'daemon') daemonCommand(args[0]);
else {
  console.error('未知命令: ' + command);
  process.exit(2);
}
