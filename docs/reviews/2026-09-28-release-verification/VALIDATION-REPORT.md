# aide 合并前全量验证与回归报告

- 分支：feature/permission-panel，HEAD=f9c4e02，工作树干净
- 容器：aide-aide-1（HTTPS https://127.0.0.1:8097）
- 角色：只验证，未 commit/push/merge，未改源码
- 证据根目录：`/Users/skyelan/Library/Mobile Documents/com~apple~CloudDocs/WorkStation/AI WorkStation/aide/artifacts/`

---

## 一、后端静态/单测门槛（Docker golang:1.26-bookworm，-mod=vendor）

| # | 命令 | 退出码 | 结论 | 证据 |
|---|------|--------|------|------|
| 1 | `docker run --rm -v "$PWD":/src -w /src golang:1.26-bookworm go vet -mod=vendor ./...` | 0 | **PASS**（0 问题，日志 0 行） | artifacts/go-vet.log |
| 2 | `... go build -mod=vendor ./...` | 0 | **PASS**（日志 0 行） | artifacts/go-build.log |
| 3 | `... go test -mod=vendor -race -count=1 ./internal/server/` | 1（预期） | **PASS**（见下） | artifacts/go-test-race.log |

门槛 3 细节：
- 耗时 287s。**DATA RACE = 0**（race-datarace-count.txt），**panic = 0**（race-panic-count.txt）。
- 共 9 条 `--- FAIL`，逐一比对 hint 列出的 7 类「缺 node」已知环境性非回归，**全部命中，无其它失败**：

| FAIL | 耗时 | 归属 hint 已知非回归 |
|------|------|------|
| TestDaemonStartStop | 15.40s | #7（TestDaemonStartStop 主体） |
| TestDaemonCallViaIPC | 15.44s | #7（子测试） |
| TestDaemonCrashRestart | 15.49s | #7（子测试） |
| TestDaemonEventReporting | 15.46s | #6 |
| TestNonDaemonUnchanged | 0.42s | #5 |
| TestPluginUploadAndValidation | 0.38s | #1 |
| TestPluginLifecycleAndSurface | 0.38s | #2 |
| TestPluginApplyErrorIsolated | 0.37s | #3 |
| TestPluginToolExecution | 0.38s | #4 |

失败原因均为 `exec: "node": executable file not found` 或 `插件校验失败:`（测试镜像缺 node；运行容器内 node v24.21.0 正常，真实插件功能不受影响）。另有一条 `httptest.Server blocked in Close after 5 seconds` 为测试框架 shutdown 警告，非 DATA RACE/panic。

**结论：三门槛全部通过。**

## 二、重建并确认运行时

- `docker compose build` → BUILD_RC=0（全缓存层）；`docker compose up -d` → UP_RC=0。容器 aide-aide-1 重建后 ~55s 内 healthy。
- `/healthz` → 200；带 `Authorization: Bearer <token>` 的 `/api/config` → 200（deepseek-chat 已配、hasKey=true、hasPassword=false）；无 token → 401。
- 证据：artifacts/compose-build.log、compose-up.log。
- **结论：PASS。**

## 三、Playwright 受影响功能/UI 回归（7 组）

| # | 项 | 结论 | 关键证据（绝对路径） |
|---|----|------|------|
| 1 | 凭据设置/每来源 vault | **PASS** | artifacts/t1-source-dialog-filled.png、t1-source-persisted-after-reload.png、api-sources-after-add.json |
| 2 | 插件启停不阻塞全局锁 | **PASS** | artifacts/t2-plugin-panel.png、t2-plugin-commtcp-enabled.png、api-plugins-baseline.json、api-daemons-baseline.json |
| 3 | ask_user 轮次 | **PASS** | （API 响应证据，无截图） |
| 4 | PDF 快速缩放/翻页 | **PASS** | artifacts/t4-pdf-after-rapid-zoom.png |
| 5 | 工作流四阶段色（深/浅主题） | **PASS** | artifacts/t5-phases-light.png、t5-phases-dark.png、t5-composer-dark.png |
| 6 | docx 批注 anchorIndex | **部分 PASS / 重载高亮 FAIL** | artifacts/t6-docx-reload-no-highlight.png、api-comments-after-create.json |
| 7 | 会话桶布局对齐 | **FAIL（1600+ 宽）** | artifacts/t7-layout-1680-misaligned.png |

### 逐项说明

**1. 凭据 vault — PASS**
新增 SFTP 来源 QA-SFTP-vault（id=s-cvl5pb），密码 QA-SECRET-plaintext-DO-NOT-LEAK-2026。GET /api/sources 返回 hasSecret=true，config 中无密码明文；vault.enc 由 382→761 字节（密文落盘）；/data/secrets/sources-secrets.json 仅 19 字节 `{"secrets": {}}`（旧明文已清空=迁移终态），整个 /data/secrets grep 不到明文串。刷新页面后来源仍在。

**2. 插件启停不阻塞全局锁 — PASS**
容器内 node v24.21.0 可用。comm-tcp enable→200/73ms、start→200/1ms、stop→200/11ms、disable→200/40ms；并发轮询 /api/sources 最大 11ms，无 10–15s 全局锁阻塞。6 次快速 start/stop 全 200（2–42ms），最终 stopped/enabled=false 一致。UI 开关可切到「✓ 使用中」。

**3. ask_user 轮次 — PASS**
发模糊 workflow 任务后模型立即 ask_user（awaiting_clarification）。应答当前问题→200 {ok:true}，run 继续；随后迟到应答→409「当前无可应答的澄清问题」，且未串入下一轮；错 run→409、错 session→404。无 panic、无挂起。

**4. PDF — PASS**
打开 sample.pdf（2 页），12 次快速缩放+翻页期间 .pdf-canvas 数量恒为 2，0 个多画布页（无重叠渲染）；关闭对话框触发 teardown（取消在途 renderTask + destroy pdfDoc）。无 PDF 渲染错误。

**5. 工作流四阶段色 — PASS**
四阶段按钮颜色来自 CSS 变量 --phase-req/design/impl/verify-bg/fg。light 主题 fg 为深色饱和色，dark 主题 fg 变浅粉彩。dark 主题有效对比度（合成半透明底色到真实页面底 [36,36,38]）：requirement 6.49、design 6.47、implementation 7.45、verify 7.34（均 ≥ WCAG AA 4.5）。

**6. docx 批注 — 创建侧 PASS；重载高亮落点 FAIL**
- 现场生成 qa-comment-anchor.docx（短语「ANCHORPHRASE-重复句子-用于批注定位。」出现 3 次）。
- 选中**第 2 处**加批注：捕获 POST /api/comments 负载 `{path:qa-comment-anchor.docx, hash:MzY3Njg=, anchorQuote:..., anchorIndex:1, text:...}` —— **anchorIndex=1 正确**（P2#6 创建侧修复生效）。服务端已保存（id=8dfd8cbb…，anchorIndex=1）。
- **但重载后高亮未落在第 2 处**：全文档唯一的 `<mark class="comment-anchor-hl">` 被包在 docx-preview 注入的 `<style>` 元素内部、包裹的是 CSS 文本（"id-accent4 span {\n  color"），三处正文均无高亮，批注卡片显示「锚点可能失效」。
- **根因**：`findTextRange()`（app.js:4536）在空白规范化后的全文里搜出现位置，却用该索引去取「逐原始字符偏移」的 map；docx-preview 注入的内嵌样式块含 ~7569 个空白字符被折叠，导致第 2 处的规范化索引（199260）对应到原始坐标时整体偏移 ~7569 字符，落进 CSS 而非正文。
- 复现步骤：打开 qa-comment-anchor.docx → 切到「批注」面板 → 重载文档 → DOM 中 `.comment-anchor-hl` 在 `<style>` 内，正文三处无高亮。
- 影响面：所有 docx 批注重载后高亮均错位（锚点定位失效）；但 anchorIndex 的**提交值**是对的。另：前端 docHash=btoa(文件字节数) 与服务端内容哈希算法不一致，导致 stale 恒 true（独立问题）。

**7. 会话桶布局 — FAIL（1600+ 宽）**
- 实测（getBoundingClientRect）：
  - 1280 宽：.run 260–976（716），composer 256–980（724），每边差 4px。
  - **1680 宽（≥1600）：.run 428–1208（780px），composer 398–1238（840px）——每边外探 30px，未对齐。**
  - 600 窄屏：.run 20–580（560），composer 14–586（572），无崩溃、无回退（侧栏正常隐藏）。
- **根因**：P2#8（f9c4e02）只改了 `style.css`，把 .composer 设为 max-width:840px、.welcome 设为 840px；但 `macos.css:120` 仍有 `.run{max-width:780px}`、`macos.css:78` 仍有 `.welcome{max-width:640px}`，后者在 macOS 浏览器下覆盖生效。结果 composer(840) 比消息列 .run(780) 宽 60px，左右各悬出 30px；welcome 也未与 composer 协调。
- 复现步骤：浏览器宽度调到 ≥1600（如 1680），发一条消息，观察底部输入框比上方消息列明显更宽。
- 影响面：宽屏下消息流与输入区左右边缘不对齐，与 P2#8 目标「共享同一内容列宽」不符。窄屏（≤1150）因 max-width 不触发，差异仅 padding 级。

---

## 四、汇总

- 后端三门槛：**全部 PASS**（vet/build exit 0；race 0、panic 0；9 条 FAIL 全部命中 7 类已知缺 node 非回归，无其它失败）。
- 重建运行时：**PASS**（healthy，带 token 200、无 token 401）。
- Playwright 7 项：
  - PASS：#1 vault、#2 插件启停、#3 ask_user、#4 PDF、#5 四阶段色（5 项）。
  - 部分：#6 docx 批注（anchorIndex=1 提交正确，但重载高亮错位——落进内嵌 CSS，未落在第 2 处）。
  - FAIL：#7 会话桶布局（≥1600 宽 .run=780 vs composer=840，每边悬出 30px；welcome=640 未协调。根因 macos.css 覆盖 style.css 的 P2#8 值）。
- 两个新发现问题均**未改源码**，按要求记录复现步骤与影响面，待人工评估后修复。
