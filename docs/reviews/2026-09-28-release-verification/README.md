# 发布前全量验证（2026-09-28）

本目录保存 `0.1.12.0` 合并 main 前的全量验证与回归证据。验证在 Docker 内进行，验证过程不改动源码、不提交代码。

## 报告
- [VALIDATION-REPORT.md](VALIDATION-REPORT.md) — 首轮全量验证：后端三门槛 + 运行时重建 + Playwright 7 组回归。
- [REVERIFY-REPORT.md](REVERIFY-REPORT.md) — 对首轮发现的两项缺陷（会话桶布局、docx 批注锚点）修复后的复核，均 PASS。

## 验证范围与结论（汇总）

**后端门槛（`golang:1.26-bookworm`，`-mod=vendor`）**
- `go vet ./...`：0 问题。
- `go build ./...`：成功。
- `go test -race -count=1 ./internal/server/`：**0 DATA RACE、0 panic**；仅有的失败全部为 golang 测试镜像缺 `node` 导致的环境性非回归（运行中的 aide 容器内 node v24 可用，真实功能不受影响）：`TestPluginUploadAndValidation`、`TestPluginLifecycleAndSurface`、`TestPluginApplyErrorIsolated`、`TestPluginToolExecution`、`TestNonDaemonUnchanged`、`TestDaemonEventReporting`、`TestDaemonStartStop`（含 `CallViaIPC`/`CrashRestart` 子测试），均已与改动前基线逐项核对。

**运行时**：`docker compose build && up -d` 成功，容器 `aide-aide-1` healthy；`/healthz` 200、带 token `/api/config` 200、无 token 401。

**Playwright 回归（7 组，最终全部通过）**
1. 每来源凭据 vault：新增带密码来源后 API/落盘无明文、`vault.enc` 密文落盘、旧 `sources-secrets.json` 清空、刷新后来源不丢。
2. 插件启停：启停毫秒级返回，并发请求不被全局锁阻塞，状态/注册表最终一致。
3. ask_user 轮次：迟到/错轮应答返回 409 不串轮、不 panic。
4. PDF：快速缩放/翻页画布数恒定、无重叠闪烁，关闭即销毁。
5. 工作流四阶段色：颜色随深/浅主题变量切换，对比度达 WCAG AA。
6. docx 批注：`anchorIndex` 正确计算；重载后高亮落在正文对应出现处；docHash 改 SHA-256 后新批注 stale=false。
7. 会话桶布局：1680/1280/600 三宽度下消息流与输入区左右边缘逐像素重合。

## evidence/
后端 race 日志与失败清单、运行时构建日志、各 API 基线/结果 JSON，以及 7 组回归与复核截图（含修复前后对比）。
