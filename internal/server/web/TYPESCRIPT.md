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
- **批 3**：markdown 系列与中小模块（session-plan、review、pdf-appearance、media、code、
  diagrams、message-center、plugin-workbench 等）。
- **批 4**：中型模块与 scripts 桥接（starmap、avatar-player、urdf-preview、
  computer-bridge 等；Node/CommonJS 脚本使用独立 `module: commonjs` 配置）。
- **批 5**：`app.js`（约 14k 行）先按职责拆分再改造；`locales/en.js` 等收尾。

vendor 三方库、`.agent-state` 快照、插件备份、testdata fixture 不在改造范围。
