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
- **批 4d（待做）**：其余小模块（remote-access、session-timeline、management、
  plugin-*、file-* 等）与 scripts 桥接（Node/CommonJS 脚本使用独立
  `module: commonjs` 配置）。
- **批 5**：`app.js`（约 14k 行）先按职责拆分再改造；`locales/en.js` 等收尾。

vendor 三方库、`.agent-state` 快照、插件备份、testdata fixture 不在改造范围。
