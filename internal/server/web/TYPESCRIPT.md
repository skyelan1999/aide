# Web 前端 JS → TS 渐进改造规范

目标：将 `internal/server/web/` 自有脚本与 `scripts/` 桥接脚本渐进改造为 TypeScript，
获得静态类型检查与接口文档能力，同时**运行时零变化**。

## 技术路线（批 1 已确定并验证）

- `.ts` 为权威源码，`tsc` 仅做类型擦除，编译出**同名 `.js` 产物继续入库**。
  Go embed、`index.html` 的 `<script src>`、启动脚本完全不改，不引入 bundler/framework。
- 根 `package.json` 只含 `typescript` devDependency（`node_modules/` 已在 .gitignore）。
- 命令（仓库根）：
  - `npm run build:web`：编译 `internal/server/web/tsconfig.json` 覆盖的 `.ts`。
  - `npm run typecheck`：仅类型检查不产出。
- `tsconfig` 要点：`target: ES2020`（避免 downlevel helper 改变行为）、
  `lib: ES2022+DOM`（Object.hasOwn 等仅类型层面）、`module: none`（经典全局脚本，
  禁止 import/export，防止产物变成模块）、`strict: true`。
- tsconfig 默认输出到源文件旁；**不要设置 outDir=out 源目录**（会把输入排除，TS18003）。

## 每文件改造约定

1. 复制 `.js` 为 `.ts`，**逻辑零改动**，只加类型：接口、参数/返回值标注、
   `catch(e)` 中的 `(e as Error)`、必要的 `??''` 等等价守卫。
2. `window.AideXxx` 全局 API 在文件顶层用 `interface Window { ... }` 合并声明；
   调用方（app.js 等未改造文件）不受影响。
   **同一全局 API 的 Window 声明只保留在权威实现文件**（如 `LockCluster` 仅在
   `lock-cluster.ts`），其他文件引用即可——重复声明不同类型会报合并冲突。
3. DOM 创建辅助函数用泛型 `node<K extends keyof HTMLElementTagNameMap>`，
   保留元素专有属性（href/type/disabled）的类型检查。
4. API 响应数据可先显式 `any` 并在后续批次收紧为接口；函数边界（参数、返回值、
   暴露到 window 的 API）必须类型化。
5. **禁止顺手改行为**：包括看似无害的 `return x` → `void x`（返回值可能被
   测试或其他调用方 await）、`x.foo()` → `x?.foo()`、`clearTimeout(t)` → 加判空、
   `n.textContent=text` → `String(text)`、文案、时序、轮询条件。
   类型不通过时用非空断言（`event!.preventDefault()`、`clearTimeout(timer!)`、
   `readyResolve!(...)`）保留原始表达式，而不是改写成"更安全"的版本。
6. 编译后递增 `index.html` 中该脚本的 `?v=` 版本号。
7. **文件头注释必须放在 interface 声明之前**（或紧贴首个运行时语句）：
   注释与首个输出语句之间夹 interface 时，tsc 会把注释随声明一起擦除。
8. **源码切片测试需适配产物格式**：`scripts/*.test.cjs` 中按字符串锚点
   `indexOf` 切片 vm 执行的测试（如 markdown-history-writable），tsc printer
   会重排空格/缩进/换行，锚点要改为产物实际格式；只改锚点，不改测试语义。
9. 含运算的断言写法注意括号：`x - y as unknown as string`（as 绑定于 y）
   可避免产物多出 `(x - y)` 括号；`setProperty` 传 number 时用此手法保留原表达式。

## 每批验收标准

- `tsc` 零错误；产物 `node --check` 通过。
- **严格等价验证**（批 2 起）：改造前 `cp` 原 `.js` 到 /tmp 备份，编译后用
  `ts.transpileModule`（removeComments）同时转译原文件与新产物，归一化空白、
  引号、顶层严格模式 prologue、单参箭头括号后比较，必须完全一致。
- 相关 `scripts/*.test.cjs` / `*.test.js` 与 Go 侧相关测试全过。
- 不新增运行时依赖、不改加载方式。

## 批次计划

- **批 1（已完成）**：基础设施 + `task-outcome.ts`（279 行，3 个 outcome 回归测试全过）。
- **批 2（已完成）**：`async-action.ts`、`account-balance-pool.ts`（UMD）、
  `lock-cluster.ts`（选举/权威状态，436 行）；双侧同转译严格等价验证通过，
  6 个相关 node 测试全过；task-outcome 的 LockCluster 声明移交 lock-cluster.ts。
- **批 3a（已完成）**：markdown 小模块六个——`markdown-history-manager`、
  `markdown-history`（LCS diff + 恢复）、`markdown-code`（高亮/复制）、
  `markdown-diagrams`（Mermaid/CSSOM）、`markdown-media`（附件/模板/drawio）、
  `markdown-outline`（目录 + 阅读器缩放双 IIFE）；严格等价验证全过，
  8 个 markdown 相关测试全过（markdown-history-writable 切片锚点已适配产物格式）。
- **批 3b（已完成）**：markdown 大模块——`markdown-pdf-style`(271)、
  `markdown-review`(342)、`markdown-pdf-appearance`(754)、`markdown-export`(869)；
  严格等价验证全过，14 个 markdown 相关 node 测试全过。
  `AideMarkdownReview` 的 Window 声明由 markdown-outline.ts 移交 markdown-review.ts。
  download-export / inline-image / template-icons 三个测试的源码锚点适配产物格式
  （等号/逗号空格、跨行语句改 `\s*` 正则、vm 注入锚点）。
  新经验：
  - 等价归一化需再归一 `=>` 周围空白并 trim 首尾（tsc 对 `: T=>` 保留原空格）。
  - 不要用 `as` 断言包裹三元/数组表达式（产物保留括号）；改为变量类型注解。
  - TS2774（方法恒存在）与"看似 bug"的原始属性访问（如 fence.char）用
    `as any` 逐字保留，禁止顺手修复。
- **批 4a（已完成）**：八个小模块——`lan-login`、`file-types`（UMD）、
  `runtime-lifecycle`、`plugin-workbench-tabs`、`mobile-capture`（BarcodeDetector/
  ZXing 用 `declare var ...: any`）、`assistant-memory`、`plugin-file-preview`、
  `settings-navigation`；严格等价全过，8 个相关 node 测试全过。
  `AidePluginWorkbench` 暂以 `any` 声明于引用方，待权威实现改造时移交；
  `globalThis.AideFileTypes` 引用用 `(globalThis as any)` 保持逐字。
  mobile-capture.html 版本号 v4；lan-login.html 本无版本号，保持原样。
- **批 4b（已完成）**：八个小模块——`starmap-optics`、`drawio-linear-regex`
  （CommonJS 导出用 `declare var module: any`）、`plugin-workbench-client`、
  `staged-output`、`delimited-text`、`desktop-capture`、`file-route`、
  `capabilities`；严格等价全过，8 个相关 node 测试全过且无需锚点适配。
  drawio-linear-regex 由服务端注入、plugin-workbench-client 打包进插件 HTML，
  两者无 html cache buster；starmap-optics 在 starmap.html 递增，其余在 index.html。
  新经验：
  - 正则内的控制字符必须保留 `\u0000` 转义写法，禁止写成字面控制字符。
  - 对象字面量直接索引需保留原括号时，对**索引键**做联合字面量 `as` 断言，
    不要对字面量本身 `as Record`（tsc 会丢掉外层括号）。
  - 返回 setTimeout id 的箭头函数保持表达式体，不要改块体 void。
- **批 4c（已完成）**：八个 starmap 子模块——`starmap-preview`、`starmap-cache`、
  `starmap-infrared`、`starmap-gravity`、`starmap-structure`、`starmap-assist`、
  `starmap-topics`、`starmap-timeline`；严格等价全过，22 个 starmap 测试全过。
  starmap.html 八个 cache buster 递增。
  新经验：
  - 内联 `({...}[key]||fallback)` 的原文括号往往包住**整个索引+回退表达式**，
    类型化时只给 key 加联合字面量 `as` 断言，括号位置原样保留。
  - `new Map(any.map(...))` 需显式 `new Map<any, any>`，否则 value 推断为 {}。
  - 三元表达式分支后的 `.filter()/.slice()` 调用链不得擅自加括号改变作用域。
- **批 4d（已完成）**：八个小模块——`remote-access`、`session-timeline`（UMD）、
  `local-mounts`、`i18n`、`plugin-upgrade`、`management`、`git-history`、
  `model-token-chart`（UMD）；严格等价全过，9 个相关测试全过（含 i18n 两个套件）。
  `aideI18n` 的 Window 声明由 lock-cluster.ts 移交权威实现 i18n.ts（完整含
  language/schema/applyStatic/sourceName）；`AideGitHistory` 声明由
  markdown-history.ts 移交 git-history.ts；`aideUI`/`aideEnglish` 暂在 i18n.ts
  以 any 声明，待 app.js/locales 改造时移交。i18n.js 在 index/mobile-capture/
  starmap 三个 html 各自递增。
  新经验：对 any 调用 `.then(data=>...)` 时回调参数无上下文类型，必须显式
  `(data: any)`，否则 TS7006。
- **批 4e（已完成）**：八个小模块——`plugin-guide`、`docx-style-bridge`（CSSOM
  rule 用 any）、`drawio-layout-fixes`（UMD factory，原型方法 `function(this: any)`）、
  `assistant-call`、`recovery-manager`、`file-diff`（UMD）、`starmap-logbook`、
  `session-model-selection`；严格等价全过，8 个相关测试全过。
  drawio-layout-fixes 由服务端注入，无 html cache buster。
- **批 4f（已完成）**：八个小模块——`text-editor`、`parallel-chat`、`plugin-market`、
  `file-transfers`、`keybindings`、`preview-safety`（UMD）、`recovery-transfer`、
  `message-notifications`；严格等价全过，9 个相关测试全过（keybindings、
  message-notifications、parallel-chat、plugin-market、recovery-transfer 与
  docx/dxf/drawio/zip 四个 preview bounds 套件）。
  基础设施随本批调整：`tsconfig` 由 `module: none`/`moduleResolution: classic`
  改为 `module: ES2020`/`moduleResolution: node`，以支持 file-transfers 的
  动态 `import('./zip.js')`；新增 `zip.d.ts` 仅声明运行时懒加载模块形状。
  非模块全局脚本的产物不受 module 选项影响（本批仅 8 个目标 js 变化）。
  `aideExperience` 暂在 plugin-market.ts 以 any 声明，待 experience 改造时移交。
  新经验：
  - 只含 `resolve()` 无参调用的 Promise 必须显式 `new Promise<void>`，否则 TS2794。
  - `new Promise` 内 resolve 恒带参时用 `new Promise<any>` 保持 result 类型灵活。
- **批 4g（已完成）**：八个中小模块——`plantuml-resource-helpers`（全局
  var/function 脚本，`AidePlantUmlRE2` 以 declare var any 声明，无 html cache
  buster）、`assistant-live`、`assistant-realtime`、`cad-measure`（UMD）、
  `starmap-lensing`、`starmap-cosmos`、`crash-monitor`、`project-records`；
  严格等价全过，28 个相关测试全过（assistant-live/assistant-realtime/
  crash-monitor/cad-measure/plantuml/starmap-lensing 单测 + 22 个 starmap 套件）。
  crash-monitor 与 starmap-lensing/starmap-cosmos 的 cache buster 在 starmap.html，
  其余在 index.html（crash-monitor 两处各自递增）。
  新经验：
  - 原函数单参定义但存在双参调用时，用**重载声明**（`function f(a,b?): void;`
    + 实现 `function f(a): void`）保留运行时签名；重载声明在产物中被擦除，
    严格等价不受影响，优于 `@ts-expect-error`（该指令只对下一行生效，长单行
    内联调用无法精准标注）。
  - DOM 标准类型不含的动态属性（如 `PerformanceEntry.type` 仅存在于
    navigation entry），在该 const 语句上一行加 `@ts-expect-error` 保留原动态
    访问；验证时 removeComments 归一化，注释不影响等价。
- **批 4h（已完成）**：八个中小模块——`voip`、`file-autosave`、`project-workflows`、
  `plugin-plans`、`drawio-preview-safety`（UMD）、`starmap-physics`、
  `highlight-grammar-fixes`（UMD，`hljs` 以 declare var any 声明）、
  `session-review`（UMD）；严格等价全过，相关测试全过（file-autosave-retry、
  plugin-plans、session-review、drawio preview bounds、highlight grammar review、
  drawio layout fixes + 22 个 starmap 套件）。drawio-preview-safety 与
  highlight-grammar-fixes 由服务端注入，无 html cache buster。
  附带修复：`scripts/session-review.test.cjs` 中对 html 引用硬编码
  `app.js?v=257` 的断言在改造前已失败（HEAD 已 v294），改为版本号无关的
  `v=\d+` 断言，消除此类既有脆弱断言。
  新经验：
  - `Object.assign(document.createElement('input'), {value: 5060})` 的交叉类型
    会使 `.value` 变成 `string & number = never`，整个 const 标注 any 即可。
  - **Window 声明唯一权威不可违反**：file-autosave 曾重复声明
    `LockCluster?: any`，与 lock-cluster.ts 的 `LockClusterApi` 冲突并导致
    多个文件回调参数失去上下文类型（TS7006）。引用方一律不重复声明，
    直接用权威文件的类型。
  - `new Map(arr.map(x => cond ? [k,v] : null).filter(Boolean))` 用
    `new Map<any, any>(... as any)` 断言构造参数，不影响产物。
- **批 4i（已完成）**：六个中大型模块——`continuity`、`settings-init`、
  `workbench-background`、`plugin-workbench`、`message-center`、`experience`；
  严格等价全过，11 个相关测试套件全过（continuity-isolation/lock-write/
  retention、experience-runtime、message-center、message-notifications、
  plugin-workbench（含 client/tabs）、workbench-background、settings-navigation）。
  新经验：
  - `Object.entries(any)` / `Object.values(any)` 的元素被推断为 unknown，
    用显式泛型 `Object.entries<any>(...)` / `Object.values<any>(...)` 保留
    原表达式并恢复 any 元素类型。
  - `new Date(a)-new Date(b)` 这类日期算术用 `(new Date(a) as any)-(new Date(b) as any)`
    保留原表达式，禁止顺手改成 `.getTime()`（会改变源码表达式）。
  - 可选链比较后的第二次访问不 narrow（如 `previousView?.key===k&&!previousView.bottom`），
    后续访问用 `previousView!.bottom` 非空断言，运行时不变。
  - `document.createElementNS(ns,'path')` 返回 Element 无 style/animate，
    结果常量标 any；`document.activeElement` 需要表单属性时同样标 any。
  - `iframe.contentWindow` 可能为 null，整 frame 常量标 any 比逐处断言更简洁。
- **批 4j（已完成）**：三个模块——`urdf-preview`（THREE/URDFLoader 动态对象）、
  `session-plan`（会话级计划面板）、`avatar-player`（精灵动画播放器）；
  严格等价全过，相关测试全过（urdf-preview、urdf-interaction-budget、
  session-review、settings-navigation）。session-plan cache buster v5→v6、
  avatar-player v81→v82；urdf-preview 的 js 无 html cache buster（仅 css 有）。
  新经验：
  - 原文件故意保留同名函数的两份实现时，TS 报 TS2393 且错误落在第一份
    声明上；在两份定义处各加 `@ts-ignore` 保留原样，产物等价。
  - `for (const x of [].concat(...))` 会被推断为 never[]，在 concat 表达式
    末尾加 `as any[]` 即可，不改变迭代结构。
  - 对象字面量动态索引不能对整个字面量 `as any`（tsc 会丢掉原括号），
    改用键的联合字面量断言（`kind as 'box'|'cylinder'|'sphere'`），并按
    原文逐处保留括号。
- **批 4k（已完成）**：`starmap`（1217 行星图主脚本）TS 化，严格等价通过，
  全部 22 个 `starmap-*.test.cjs` 套件通过。采用只插入类型标注的 codemod
  （384 个参数/catch、57 个空值变量、53 个动态对象/DOM 句柄），不改写运行
  时代码；`$` 返回 any 保留动态 DOM 访问，`graph` 等动态记录标 any，
  `new Map/Set` 显式泛型。cache buster starmap v75→v76。
  编译目标由 ES2020 提升为 **ES2021**，以保留原文件的 `||=` 逻辑赋值；
  message-center/voip 的 `||=`、workbench-background 的数字分隔符随之恢复
  原语法，已用各自改造前备份重新验证严格等价，相关测试通过。
  跨页面全局 `api` 同名（starmap/mobile-capture 为函数、management/
  remote-access 为 declare var）以 `@ts-ignore` 保留各自页面独立加载的
  原结构；`LockCluster` 全局声明使用权威 `LockClusterApi`，不得标 any。
  测试债修复：多个 starmap 套件以无空格源码标记切片/正则，tsc 格式化后
  失效。新增 `scripts/fixtures/starmap-source.cjs` 提供空白不敏感的
  `sourceIndex/sourceIncludes/sourceMatch/sourceRegex`，15 个套件改用该
  helper；empty-state 的两个单行正则切片改为明确代码边界，lensing 的函数
  提取改为起止函数边界。断言语义不变，仅消除格式耦合。
  新经验：
  - tsc 在语法错误时默认仍会 emit 并覆盖同名 js；大文件改造先 `--noEmit`
    检查，且必须保留改造前备份用于恢复与等价验证。
  - 单参箭头批量加标注时，是否已有括号应以 `=>` 前一个 token 判断，
    不能以参数前字符判断（调用表达式的 `(` 会误判）。
  - `({...} as any)[key]` 会丢掉原对象字面量括号；需要严格等价时改用
    键的联合字面量断言。
  - `drawCosmicPlanet.surfaceCache` 这类函数动态属性用
    `(drawCosmicPlanet as any).surfaceCache`，不引入 namespace 合并。
- **批 4l（待做）**：两个 AudioWorklet（独立 lib 配置）、`locales/en.js`
  与 scripts 桥接（Node/CommonJS 脚本使用独立 `module: commonjs` 配置）。
- **批 5**：`app.js`（约 14k 行）先按职责拆分再改造；`locales/en.js` 等收尾。

vendor 三方库、`.agent-state` 快照、插件备份、testdata fixture 不在改造范围。
