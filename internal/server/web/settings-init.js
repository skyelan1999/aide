'use strict';
/* aide 设置存储 · 唯一事实源（FR-58~FR-60 / LIM-21）
 *
 * 本文件必须以同步外链脚本形式置于 <head> 首位：
 *   <script src="/settings-init.js"></script>   —— 无 defer、无 async、无 type=module
 * 原因：defer 脚本在文档解析完成后才执行，晚于首次内容绘制，会产生主题闪烁。
 * 同步执行可保证首次绘制前 <html> 的 data-theme 已就位。
 *
 * 存储契约（LIM-21）：
 *   localStorage['aide.ui'] = {"version":1,"theme":"system",…}
 *   旧键 aide.theme（字符串）自动迁移；非法值回退默认并回写。
 *   仅 localStorage，不入 cookie、不入服务端。
 *
 * 对外契约：
 *   window.aideUI     —— JSON 设置门面：get / getAll / set / subscribe
 *   window.aideTheme  —— 主题门面（兼容主题增量 PRD §10.2）：pref / effective / set / subscribe
 *
 * 硬约束：
 *   ① 只允许写 document.documentElement 的属性——执行时 <body> 尚不存在，
 *      **不得**访问任何 DOM 元素（无 getElementById / querySelector / body）。
 *   ② 首次应用必须同步完成，不得等待任何异步操作。
 *   ③ 两个属性语义不同，不得混用：
 *        documentElement.dataset.theme     = 生效主题（light | dark）—— CSS 唯一开关
 *        documentElement.dataset.themePref = 用户偏好（light | dark | system）
 */
(function () {
  var KEY = 'aide.ui';
  var LEGACY_KEY = 'aide.theme';
  var VERSION = 1;
  var VALID_THEME = ['light', 'dark', 'system']; // 主题枚举唯一来源
  var DEFAULT_PREF = 'system';                   // 缺省偏好
  var DEFAULT_DOC = { version: VERSION, theme: DEFAULT_PREF, palette: 'blue' };
  var SYSTEM_QUERY = '(prefers-color-scheme: dark)';

  var root = document.documentElement;
  var media = window.matchMedia ? window.matchMedia(SYSTEM_QUERY) : null;
  var uiSubscribers = [];
  var themeSubscribers = [];

  /* ── 存储基础操作：全部静默降级（隐私模式等不抛错） ── */
  function rawGet(key) {
    try {
      return window.localStorage.getItem(key);
    } catch (err) {
      return null;
    }
  }
  function rawSet(key, value) {
    try {
      window.localStorage.setItem(key, value);
    } catch (err) {
      /* 忽略：持久化失败不应阻塞设置应用 */
    }
  }
  function rawRemove(key) {
    try {
      window.localStorage.removeItem(key);
    } catch (err) {
      /* 忽略 */
    }
  }

  /* ── 校验 ── */
  function isTheme(value) {
    return VALID_THEME.indexOf(value) >= 0;
  }
  function normalizeTheme(value) {
    return isTheme(value) ? value : DEFAULT_PREF;
  }

  /* ── 读取：旧键迁移 → JSON 解析 → 字段校验 → 回写规范化文档 ── */
  function readDoc() {
    var legacy = rawGet(LEGACY_KEY);
    if (legacy !== null) rawRemove(LEGACY_KEY); // 迁移后删除旧键
    var stored = rawGet(KEY);
    var parsed = null;
    if (stored !== null) {
      try {
        parsed = JSON.parse(stored);
      } catch (err) {
        parsed = null; // 损坏 JSON → 按缺省处理
      }
    }
    if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) parsed = {};
    var theme = typeof parsed.theme === 'string' ? normalizeTheme(parsed.theme) : DEFAULT_PREF;
    if (legacy !== null && typeof parsed.theme !== 'string') theme = normalizeTheme(legacy);
    // 保留未知字段（向前兼容），规范 version、theme、palette；兼容旧 classic 偏好
    var doc = {};
    for (var key in parsed) {
      if (Object.prototype.hasOwnProperty.call(parsed, key)) doc[key] = parsed[key];
    }
    doc.version = VERSION;
    doc.theme = parsed.theme === 'classic' ? 'light' : theme;
    doc.palette = parsed.theme === 'classic' || parsed.palette === 'green' ? 'green' : 'blue';
    var normalized = JSON.stringify(doc);
    if (stored !== normalized) rawSet(KEY, normalized);
    return doc;
  }

  var doc = readDoc();

  /* ── 主题应用：生效主题与用户偏好分别写到 <html>（仅此一处写 DOM） ── */
  function effectiveOf(pref) {
    if (pref === 'light' || pref === 'dark') return pref;
    return media && media.matches ? 'dark' : 'light';
  }
  function applyTheme() {
    root.setAttribute('data-theme', effectiveOf(doc.theme));
    root.setAttribute('data-theme-pref', doc.theme);
    root.setAttribute('data-palette', doc.palette);
  }

  /* ── 通知：单个订阅者异常不得影响设置本身 ── */
  function snapshot() {
    var copy = {};
    for (var key in doc) {
      if (Object.prototype.hasOwnProperty.call(doc, key)) copy[key] = doc[key];
    }
    return copy;
  }
  function notifyAll() {
    var uiSnapshot = null;
    for (var i = 0; i < themeSubscribers.length; i++) {
      try {
        themeSubscribers[i](doc.theme);
      } catch (err) {
        /* 忽略订阅者异常 */
      }
    }
    for (var j = 0; j < uiSubscribers.length; j++) {
      try {
        if (uiSnapshot === null) uiSnapshot = snapshot();
        uiSubscribers[j](uiSnapshot);
      } catch (err) {
        /* 忽略订阅者异常 */
      }
    }
  }
  function persist() {
    rawSet(KEY, JSON.stringify(doc));
  }

  /* ── 首次应用：同步执行，早于首次内容绘制 ── */
  applyTheme();

  /* ── 跟随系统外观：仅当偏好为 system 时响应 ── */
  if (media) {
    var onSystemChange = function () {
      if (doc.theme !== DEFAULT_PREF) return; // 用户已显式选择，忽略系统变化
      applyTheme();
      notifyAll();
    };
    if (media.addEventListener) media.addEventListener('change', onSystemChange);
    else if (media.addListener) media.addListener(onSystemChange); // 旧内核兜底
  }

  /* ── 跨标签页同步：JSON 键与旧键都吸收 ── */
  window.addEventListener('storage', function (event) {
    if (event.key === KEY) {
      var parsed = null;
      if (event.newValue !== null) {
        try {
          parsed = JSON.parse(event.newValue);
        } catch (err) {
          parsed = null;
        }
      }
      if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) parsed = {};
      var theme = typeof parsed.theme === 'string' ? normalizeTheme(parsed.theme) : DEFAULT_PREF;
      doc = {};
      for (var key in parsed) {
        if (Object.prototype.hasOwnProperty.call(parsed, key)) doc[key] = parsed[key];
      }
      doc.version = VERSION;
      doc.theme = parsed.theme === 'classic' ? 'light' : theme;
    doc.palette = parsed.theme === 'classic' || parsed.palette === 'green' ? 'green' : 'blue';
      applyTheme();
      notifyAll();
    } else if (event.key === LEGACY_KEY) {
      if (event.newValue !== null && !isTheme(event.newValue)) rawRemove(LEGACY_KEY);
      doc.theme = normalizeTheme(event.newValue);
      persist();
      applyTheme();
      notifyAll();
    }
  });

  /* ── JSON 设置门面（FR-59）：面板只允许通过此处操作设置 ── */
  window.aideUI = {
    key: KEY,
    version: VERSION,
    get: function (name) {
      return doc[name];
    },
    getAll: snapshot,
    setAppearance: function (palette, theme) {
      doc.palette = palette === 'green' ? 'green' : 'blue';
      doc.theme = normalizeTheme(theme);
      persist();
      applyTheme();
      notifyAll();
    },
    set: function (name, value) {
      doc[name] = name === 'theme' ? normalizeTheme(value) : value;
      persist();
      if (name === 'theme') applyTheme();
      notifyAll();
      return doc[name];
    },
    subscribe: function (fn) {
      if (typeof fn !== 'function') {
        return function () {};
      }
      uiSubscribers.push(fn);
      return function () {
        var index = uiSubscribers.indexOf(fn);
        if (index >= 0) uiSubscribers.splice(index, 1);
      };
    }
  };

  /* ── 主题门面（兼容旧契约）：内部已改走 JSON 文档 ── */
  window.aideTheme = {
    key: KEY,
    valid: VALID_THEME.slice(),
    pref: function () {
      return doc.theme;
    },
    effective: function () {
      return effectiveOf(doc.theme);
    },
    set: function (next) {
      return window.aideUI.set('theme', next);
    },
    subscribe: function (fn) {
      if (typeof fn !== 'function') {
        return function () {};
      }
      themeSubscribers.push(fn);
      return function () {
        var index = themeSubscribers.indexOf(fn);
        if (index >= 0) themeSubscribers.splice(index, 1);
      };
    }
  };
})();
