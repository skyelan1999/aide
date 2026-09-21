'use strict';
/* aide 主题初始化 · 主题的唯一事实源（FR-49~FR-56 / LIM-15~LIM-19）
 *
 * 本文件必须以同步外链脚本形式置于 <head> 首位：
 *   <script src="/theme-init.js"></script>   —— 无 defer、无 async、无 type=module
 * 原因：defer 脚本在文档解析完成后才执行，晚于首次内容绘制，会产生主题闪烁（FR-55）。
 * 同步执行可保证首次绘制前 <html> 的 data-theme 已就位。
 *
 * 硬约束：
 *   ① 只允许写 document.documentElement 的属性——执行时 <body> 尚不存在，
 *      **不得**访问任何 DOM 元素（无 getElementById / querySelector / body）。
 *   ② 首次应用必须同步完成，不得等待任何异步操作。
 *   ③ 两个属性语义不同，不得混用：
 *        documentElement.dataset.theme     = 生效主题（light | dark）—— CSS 唯一开关
 *        documentElement.dataset.themePref = 用户偏好（light | dark | system）
 *   ④ 对外仅暴露 window.aideTheme；app.js 不得复制本文件的解析/存储/监听逻辑。
 */
(function () {
  var KEY = 'aide.theme';
  var VALID = ['light', 'dark', 'system']; // 主题枚举唯一来源（FR-56：新增方案只改这里）
  var DEFAULT_PREF = 'system';             // 缺省偏好（LIM-15）
  var SYSTEM_QUERY = '(prefers-color-scheme: dark)';

  var root = document.documentElement;
  var media = window.matchMedia ? window.matchMedia(SYSTEM_QUERY) : null;
  var subscribers = [];
  var preference = DEFAULT_PREF;           // 运行时偏好，已规范化

  /** 是否为合法枚举取值 */
  function isChoice(value) {
    return VALID.indexOf(value) >= 0;
  }

  /** 任意输入 → 合法偏好；非法（含空串 / 大小写不符 / null / undefined）一律回退 system */
  function normalize(value) {
    return isChoice(value) ? value : DEFAULT_PREF;
  }

  /** 偏好 → 生效主题：显式 light/dark 优先；system 交由系统外观决定 */
  function effectiveOf(pref) {
    if (pref === 'light' || pref === 'dark') return pref;
    return media && media.matches ? 'dark' : 'light';
  }

  /** 把生效主题与用户偏好分别写到 <html> 上（仅此一处写 DOM） */
  function apply() {
    root.setAttribute('data-theme', effectiveOf(preference));
    root.setAttribute('data-theme-pref', preference);
  }

  /** 通知订阅者；单个订阅者异常不得影响主题本身 */
  function notify() {
    for (var i = 0; i < subscribers.length; i++) {
      try {
        subscribers[i](preference);
      } catch (err) {
        /* 忽略订阅者内部异常，保证主题变更流程不中断 */
      }
    }
  }

  /** 从 localStorage 读取偏好：缺省 → system（不回写）；非法值 → system 并回写（LIM-15 / A7） */
  function readStored() {
    var raw = null;
    try {
      raw = window.localStorage.getItem(KEY);
    } catch (err) {
      raw = null; // 读取失败（隐私模式等）→ 按缺省处理
    }
    if (raw === null) return DEFAULT_PREF;
    if (!isChoice(raw)) {
      try {
        window.localStorage.setItem(KEY, DEFAULT_PREF);
      } catch (err) {
        /* 存储不可写时静默降级 */
      }
      return DEFAULT_PREF;
    }
    return raw;
  }

  /** 写入偏好；存储不可用时静默降级（不抛错） */
  function persist(value) {
    try {
      window.localStorage.setItem(KEY, value);
    } catch (err) {
      /* 忽略：偏好无法持久化不应阻塞主题应用 */
    }
  }

  /* ── 首次应用：同步执行，早于首次内容绘制（FR-55 / A10） ── */
  preference = readStored();
  apply();

  /* ── 跟随系统外观：仅当偏好为 system 时响应（FR-54 / A8 / A9） ── */
  if (media) {
    var onSystemChange = function () {
      if (preference !== DEFAULT_PREF) return; // 用户已显式选择，忽略系统变化
      apply();
      notify();
    };
    if (media.addEventListener) media.addEventListener('change', onSystemChange);
    else if (media.addListener) media.addListener(onSystemChange); // 旧内核兜底
  }

  /* ── 跨标签页同步（增强项，非验收项） ── */
  window.addEventListener('storage', function (event) {
    if (event.key !== KEY) return;
    if (event.newValue !== null && !isChoice(event.newValue)) persist(DEFAULT_PREF);
    preference = normalize(event.newValue);
    apply();
    notify();
  });

  /* ── 对外 API 契约（§10.2）：app.js 只允许通过此处操作主题 ── */
  window.aideTheme = {
    key: KEY,
    valid: VALID.slice(),
    pref: function () {
      return preference;
    },
    effective: function () {
      return effectiveOf(preference);
    },
    set: function (next) {
      preference = normalize(next);
      persist(preference);
      apply();
      notify();
      return preference;
    },
    subscribe: function (fn) {
      if (typeof fn !== 'function') {
        return function () {};
      }
      subscribers.push(fn);
      return function () {
        var index = subscribers.indexOf(fn);
        if (index >= 0) subscribers.splice(index, 1);
      };
    }
  };
})();
