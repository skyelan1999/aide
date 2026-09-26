package server

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// 外部 AI 机器可读诊断接口（/api/debug 路由组）。
//
// 设计红线（硬编码白名单制，绝不放行敏感字段）：
//   - 总开关默认关；关闭时 /api/debug/* 整体 404，不暴露存在性。
//   - 调试令牌与普通 access-token 物理分离：只存 SHA-256 哈希，明文仅创建时返回一次。
//   - 调试令牌仅能访问 /api/debug/*；管理面（/admin、/audit）只认普通 access-token。
//   - 全程审计落 data/debug-audit.jsonl。
//   - 响应白名单脱敏：绝不返回 APIKey/密码哈希/人格密文/请求快照正文/access-token。

const (
	debugErrorRingCap = 200
	debugAuditFile    = "debug-audit.jsonl"
	debugTokenBytes   = 32 // hex 后 64 字符
	debugTokenTTL     = 365 * 24 * time.Hour
)

// recentError 最近一条错误（errorRing 条目）。只保留脱敏后的元信息，不含消息正文。
type recentError struct {
	Time    string `json:"time"`
	Kind    string `json:"kind"` // task-failed | provider | runtime
	Session string `json:"sessionId,omitempty"`
	Run     string `json:"runId,omitempty"`
	Message string `json:"message"`
}

// providerHealth 最近一次 Provider 连通性探测缓存。
type providerHealth struct {
	reachable   bool
	lastProbeMs int
	checkedAt   time.Time
}

// debugAuditEntry 一条接入审计记录（落盘 JSONL）。
type debugAuditEntry struct {
	Time    string `json:"time"`
	IP      string `json:"ip,omitempty"`
	UA      string `json:"ua,omitempty"`
	Method  string `json:"method"`
	Path    string `json:"path"`
	Owner   bool   `json:"owner,omitempty"`   // true=普通 access-token；false=调试令牌
	TokenFP string `json:"tokenFp,omitempty"` // 调试令牌短指纹（前 8 位）；owner 请求为空，绝不存完整令牌
	Result  string `json:"result"`
}

// pushError 把一条错误放入环形缓冲（线程安全）。message 调用方需已脱敏。
func (a *App) pushError(kind, sessionID, runID, message string) {
	if message == "" {
		return
	}
	if len(message) > 500 {
		message = message[:500]
	}
	a.debugMu.Lock()
	a.errorRing = append(a.errorRing, recentError{
		Time:    time.Now().UTC().Format(time.RFC3339Nano),
		Kind:    kind,
		Session: sessionID,
		Run:     runID,
		Message: message,
	})
	if len(a.errorRing) > debugErrorRingCap {
		a.errorRing = a.errorRing[len(a.errorRing)-debugErrorRingCap:]
	}
	a.debugMu.Unlock()
}

// serveDebug 是 /api/debug/* 的统一入口，替代外层默认的 access-token 鉴权。
// 四段中间件：开关判定 → 独立鉴权 → 审计 → 路由分发（handler 内再做白名单脱敏）。
func (a *App) serveDebug(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path

	a.mu.Lock()
	enabled := a.settings.DebugAccessEnabled
	ownerToken := a.token
	debugHash := a.settings.DebugTokenHash
	debugExp := a.settings.DebugTokenExpiresAt
	allowOrigins := append([]string{}, a.settings.DebugAllowOrigins...)
	a.mu.Unlock()

	// 段1：总开关。关闭即整体 404，不区分是否携带令牌，避免暴露接口存在性；静默不写审计（外部轮询时不制造噪声）。
	if !enabled {
		http.NotFound(w, r)
		return
	}

	// 管理面：令牌管理 + 审计查看/导出，只认普通 access-token。
	isAdmin := strings.HasPrefix(path, "/api/debug/admin/") || strings.HasPrefix(path, "/api/debug/audit")

	// 段2：独立鉴权。调试令牌走 Authorization: Bearer；SSE（EventSource 无法带头）允许 ?access_token=。
	token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if token == "" && r.Method == http.MethodGet && strings.HasSuffix(path, "/events") {
		token = r.URL.Query().Get("access_token")
	}

	ownerOK := subtle.ConstantTimeCompare([]byte(token), []byte(ownerToken)) == 1
	debugOK := false
	if !ownerOK && token != "" && debugHash != "" {
		h := sha256Hex(token)
		if subtle.ConstantTimeCompare([]byte(h), []byte(debugHash)) == 1 {
			if exp, err := time.Parse(time.RFC3339Nano, debugExp); err == nil {
				debugOK = time.Now().Before(exp)
			} else {
				debugOK = debugExp == "" // 无过期时间视为长期有效
			}
		}
	}
	// 短指纹：仅取调试令牌前 8 位，用于区分不同外部程序；绝不回显/落盘完整令牌。
	tokenFP := ""
	if debugOK && len(token) >= 8 {
		tokenFP = token[:8]
	}

	// 来源白名单：仅当携带浏览器 Origin 头且配置了白名单时校验；curl 无 Origin 不受影响。
	if origin := r.Header.Get("Origin"); origin != "" && len(allowOrigins) > 0 && !originAllowed(origin, allowOrigins) {
		a.debugAuditWrite(r, ownerOK, tokenFP, "403_origin")
		fail(w, 403, errors.New("来源不在调试白名单"))
		return
	}

	// 管理面只认普通 access-token；调试令牌不得管理或读取审计。
	if isAdmin && !ownerOK {
		a.debugAuditWrite(r, false, tokenFP, "401_admin_denied")
		fail(w, 401, errors.New("管理面需要普通访问令牌"))
		return
	}
	// 只读/动作端点：普通 access-token（owner）或有效调试令牌均可。
	if !isAdmin && !ownerOK && !debugOK {
		a.debugAuditWrite(r, false, tokenFP, "401_unauthorized")
		fail(w, 401, errors.New("调试令牌无效或已过期"))
		return
	}

	// 段3：审计（按降噪规则决定是否记录）+ 段4：分发到 mux。
	a.debugAuditWrite(r, ownerOK, tokenFP, "200_ok")
	a.routes.ServeHTTP(w, r)
}

// originAllowed 判断 Origin 是否命中白名单（scheme+host 精确匹配，或白名单条目为带通配的后缀）。
func originAllowed(origin string, allow []string) bool {
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	host := u.Scheme + "://" + u.Host
	for _, item := range allow {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if item == host {
			return true
		}
		// 支持 ".example.com" 形式的后缀匹配（任意子域）。
		if strings.HasPrefix(item, ".") && strings.HasSuffix(u.Host, item) {
			return true
		}
	}
	return false
}

// debugAuditShouldWrite 审计降噪：决定本次请求是否落盘。
//
//	绝不记录：查看/导出审计自身（/api/debug/audit、/api/debug/audit/*）——否则每点一次"查看审计"就自刷一条。
//	必记：所有失败/拒绝（401/403）；调试令牌(owner=false)对数据端点的每次实际访问；owner 的写操作（令牌生成/吊销/开关等非 GET）。
//	默认不记：owner（本机普通 access-token）的只读 GET 轮询（/overview、/stats、/sessions、/errors 等）。
func debugAuditShouldWrite(r *http.Request, owner bool, result string) bool {
	path := r.URL.Path
	// 1) 审计自身的查看/导出：绝不记录。
	if path == "/api/debug/audit" || strings.HasPrefix(path, "/api/debug/audit/") {
		return false
	}
	// 2) 所有失败/拒绝必记。
	if result != "200_ok" {
		return true
	}
	// 3) 成功：外部调试令牌每次实际访问都记（"谁调了什么"）。
	if !owner {
		return true
	}
	// 4) 成功：owner 的写操作记。
	if r.Method != http.MethodGet {
		return true
	}
	// 5) owner 本机只读 GET 轮询：不记。
	return false
}

// debugAuditWrite 按降噪规则追加一条审计记录到 audit/debug-audit.jsonl（失败仅记日志，不阻断请求）。
func (a *App) debugAuditWrite(r *http.Request, owner bool, tokenFP, result string) {
	if !debugAuditShouldWrite(r, owner, result) {
		return
	}
	entry := debugAuditEntry{
		Time:    time.Now().UTC().Format(time.RFC3339Nano),
		IP:      r.RemoteAddr,
		UA:      r.UserAgent(),
		Method:  r.Method,
		Path:    r.URL.Path,
		Owner:   owner,
		TokenFP: tokenFP,
		Result:  result,
	}
	b, err := json.Marshal(entry)
	if err != nil {
		return
	}
	a.debugMu.Lock()
	f, err := os.OpenFile(DebugAuditPath(a.dataPath), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err == nil {
		_, _ = f.Write(append(b, '\n'))
		_ = f.Close()
	}
	a.debugMu.Unlock()
	if err != nil {
		log.Printf("debug-audit 写入失败: %v", err)
	}
}

// readDebugAudit 读取并解析全部审计记录（文件不存在返回空切片）。
func (a *App) readDebugAudit() ([]debugAuditEntry, error) {
	b, err := os.ReadFile(DebugAuditPath(a.dataPath))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []debugAuditEntry{}, nil
		}
		return nil, err
	}
	out := []debugAuditEntry{}
	for _, line := range strings.Split(strings.TrimRight(string(b), "\n"), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var e debugAuditEntry
		if json.Unmarshal([]byte(line), &e) == nil {
			out = append(out, e)
		}
	}
	return out, nil
}

// ──────────────────────────── 只读端点 ────────────────────────────

func fsFreeBytes(path string) uint64 {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0
	}
	return st.Bavail * uint64(st.Bsize)
}

// mountInfo 单个挂载点的脱敏摘要。
type mountInfo struct {
	Path      string `json:"path"`
	Writable  bool   `json:"writable"`
	FreeBytes uint64 `json:"freeBytes"`
}

func (a *App) debugOverview(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	baseURL := a.settings.BaseURL
	activeModel := a.settings.ActiveModel
	hasKey := a.settings.APIKey != ""
	hasPassword := a.settings.UserPasswordHash != ""
	a.mu.Unlock()

	a.debugMu.Lock()
	ph := a.providerHealth
	a.debugMu.Unlock()

	edgeAvail, edgeErr, edgeChecked := a.edgeHealthSnapshot()

	// #34：性格演化可观测——状态（evolutions/上次尝试/自上次计数）在 /api/debug 可见。
	a.mu.Lock()
	pers := map[string]any{}
	for _, id := range []string{personaAide, personaXiaomi} {
		p := a.personalityLocked(id)
		st := a.personalityStateLocked(id)
		pers[id] = map[string]any{
			"enabled":           p.Enabled,
			"evolutions":        p.Evolutions,
			"updatedAt":         p.UpdatedAt,
			"countSinceEvolve":  st.CountSinceEvolve,
			"lastEvolvedAt":     st.LastEvolvedAt,
			"lastAttemptResult": st.LastAttemptResult,
			"lastAttemptAt":     st.LastAttemptAt,
			"lastAttemptNote":   st.LastAttemptNote,
		}
	}
	a.mu.Unlock()

	host := baseURL
	if u, err := url.Parse(baseURL); err == nil && u.Host != "" {
		host = u.Host // 只回主机名，不回完整 baseURL 路径
	}

	istatus := a.integrityStatus()
	jsonOut(w, 200, map[string]any{
		"status":       "ok",
		"integrity":    istatus,
		"version":      a.version,
		"buildVersion": a.buildVersion,
		"revision":     a.buildCommit,
		"uptimeSec":    int64(time.Since(a.startedAt).Seconds()),
		"startedAt":    a.startedAt.UTC().Format(time.RFC3339Nano),
		"config": map[string]any{
			"providerHost": host,
			"model":        activeModel,
			"hasKey":       hasKey,
			"hasPassword":  hasPassword,
		},
		"mounts": map[string]mountInfo{
			"workspace": {Path: "/workspace", Writable: true, FreeBytes: fsFreeBytes(a.workPath)},
			"context":   {Path: "/context", Writable: false, FreeBytes: fsFreeBytes(a.refPath)},
			"data":      {Path: "/data", Writable: true, FreeBytes: fsFreeBytes(a.dataPath)},
			"local":     {Path: a.hostLocal, Writable: true, FreeBytes: fsFreeBytes(a.hostLocal)},
		},
		"provider": map[string]any{
			"reachable":   ph.reachable,
			"lastProbeMs": ph.lastProbeMs,
			"checkedAt":   ph.checkedAt.UTC().Format(time.RFC3339Nano),
		},
		"tts": map[string]any{
			"edgeAvailable": edgeAvail,
			"edgeLastError": edgeErr,
			"checkedAt":     edgeChecked.UTC().Format(time.RFC3339Nano),
		},
		"personality": pers,
	})
}

// debugSessionView 会话+最近 run 的脱敏摘要。
type debugSessionView struct {
	ID      string        `json:"id"`
	Title   string        `json:"title"`
	Status  string        `json:"status"`
	Updated string        `json:"updated,omitempty"`
	Runs    int           `json:"runs"`
	LastRun *debugRunView `json:"lastRun,omitempty"`
}

type debugRunView struct {
	ID       string     `json:"id"`
	Created  string     `json:"created"`
	Status   string     `json:"status"`
	Mode     string     `json:"mode,omitempty"`
	Model    string     `json:"model,omitempty"`
	Failures int        `json:"failures,omitempty"`
	Error    string     `json:"error,omitempty"`
	Usage    TokenUsage `json:"usage,omitempty"`
}

func (a *App) debugSessions(w http.ResponseWriter, r *http.Request) {
	filter := r.URL.Query().Get("status")
	a.mu.Lock()
	out := []debugSessionView{}
	for _, s := range a.sessions {
		if s.Deleted {
			continue
		}
		status := ""
		var last *Task
		for i := len(s.Runs) - 1; i >= 0; i-- {
			if s.Runs[i].Status != "" {
				status = s.Runs[i].Status
				last = s.Runs[i]
				break
			}
		}
		if filter != "" && status != filter {
			continue
		}
		v := debugSessionView{ID: s.ID, Title: s.Title, Status: status, Updated: s.Updated, Runs: len(s.Runs)}
		if last != nil {
			lr := debugRunView{ID: last.ID, Created: last.Created, Status: last.Status, Mode: last.Mode, Model: last.Model, Failures: last.Failures, Error: last.Error, Usage: last.Usage}
			v.LastRun = &lr
		}
		out = append(out, v)
	}
	a.mu.Unlock()
	sort.Slice(out, func(i, j int) bool { return out[i].Updated > out[j].Updated })
	jsonOut(w, 200, out)
}

// debugRunDetailView 单 run 的 headless 回放：步骤/工具名/错误/失败计数，不吐 messages 原文与 prompt。
type debugRunDetailView struct {
	SessionID string         `json:"sessionId"`
	RunID     string         `json:"runId"`
	Status    string         `json:"status"`
	Created   string         `json:"created"`
	Mode      string         `json:"mode,omitempty"`
	Model     string         `json:"model,omitempty"`
	Failures  int            `json:"failures,omitempty"`
	Error     string         `json:"error,omitempty"`
	Usage     TokenUsage     `json:"usage,omitempty"`
	Steps     []debugStep    `json:"steps"`
	ToolUses  []debugToolUse `json:"toolUses"`
}

type debugStep struct {
	Name   string `json:"name"`
	Status string `json:"status"`
}
type debugToolUse struct {
	Tool    string `json:"tool"`
	Preview string `json:"preview,omitempty"`
}

func (a *App) debugRunDetail(w http.ResponseWriter, r *http.Request) {
	sid := r.PathValue("id")
	rid := r.PathValue("run")
	a.mu.Lock()
	defer a.mu.Unlock()
	s := a.sessions[sid]
	if s == nil {
		fail(w, 404, errors.New("会话不存在"))
		return
	}
	var task *Task
	for _, t := range s.Runs {
		if t.ID == rid {
			task = t
			break
		}
	}
	if task == nil {
		fail(w, 404, errors.New("run 不存在"))
		return
	}
	steps := make([]debugStep, 0, len(task.Steps))
	for _, st := range task.Steps {
		steps = append(steps, debugStep{Name: st.Name, Status: st.Status})
	}
	tools := make([]debugToolUse, 0, len(task.ToolUses))
	for _, tu := range task.ToolUses {
		prev := tu.Preview
		if len(prev) > 200 {
			prev = prev[:200]
		}
		tools = append(tools, debugToolUse{Tool: tu.Tool, Preview: prev})
	}
	jsonOut(w, 200, debugRunDetailView{
		SessionID: sid, RunID: rid, Status: task.Status, Created: task.Created,
		Mode: task.Mode, Model: task.Model, Failures: task.Failures, Error: task.Error,
		Usage: task.Usage, Steps: steps, ToolUses: tools,
	})
}

func (a *App) debugErrors(w http.ResponseWriter, r *http.Request) {
	a.debugMu.Lock()
	recent := make([]recentError, len(a.errorRing))
	copy(recent, a.errorRing)
	a.debugMu.Unlock()
	// 最近 50 条
	if len(recent) > 50 {
		recent = recent[len(recent)-50:]
	}
	// 跨 run 失败循环 TopN：统计最近 run 中 failures 最高的
	type failRef struct {
		Session  string `json:"sessionId"`
		Run      string `json:"runId"`
		Failures int    `json:"failures"`
	}
	top := []failRef{}
	a.mu.Lock()
	for _, s := range a.sessions {
		for _, t := range s.Runs {
			if t.Failures > 0 {
				top = append(top, failRef{s.ID, t.ID, t.Failures})
			}
		}
	}
	a.mu.Unlock()
	sort.Slice(top, func(i, j int) bool { return top[i].Failures > top[j].Failures })
	if len(top) > 10 {
		top = top[:10]
	}
	jsonOut(w, 200, map[string]any{"recent": recent, "failingRuns": top})
}

// ──────────────────────────── 受控动作（只读探测） ────────────────────────────

// debugPingProvider 只读连通性探测：测 HTTP 可达与延迟，不回传模型列表、不发起推理。
func (a *App) debugPingProvider(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	baseURL, key := a.settings.BaseURL, a.settings.APIKey
	a.mu.Unlock()
	if baseURL == "" {
		fail(w, 400, errors.New("未配置 Provider Base URL"))
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	start := time.Now()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(baseURL, "/")+"/models", nil)
	if err != nil {
		fail(w, 400, err)
		return
	}
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	client := &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	elapsed := time.Since(start)
	reachable := err == nil
	status := 0
	if reachable {
		status = resp.StatusCode
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<10))
		_ = resp.Body.Close()
	}
	a.debugMu.Lock()
	a.providerHealth = providerHealth{reachable: reachable, lastProbeMs: int(elapsed.Milliseconds()), checkedAt: time.Now().UTC()}
	a.debugMu.Unlock()
	if err != nil {
		a.pushError("provider", "", "", "ping: "+err.Error())
	}
	host := baseURL
	if u, perr := url.Parse(baseURL); perr == nil && u.Host != "" {
		host = u.Host
	}
	jsonOut(w, 200, map[string]any{
		"reachable":   reachable,
		"host":        host,
		"httpStatus":  status,
		"lastProbeMs": int(elapsed.Milliseconds()),
		"checkedAt":   time.Now().UTC().Format(time.RFC3339Nano),
	})
}

// debugDiagnosticBundle 导出脱敏诊断包：overview + errors 摘要 + 最近 5 个 run 头部。
func (a *App) debugDiagnosticBundle(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	baseURL := a.settings.BaseURL
	a.mu.Unlock()
	host := baseURL
	if u, err := url.Parse(baseURL); err == nil && u.Host != "" {
		host = u.Host
	}

	type bundleRun struct {
		SessionID string `json:"sessionId"`
		RunID     string `json:"runId"`
		Status    string `json:"status"`
		Model     string `json:"model,omitempty"`
		Failures  int    `json:"failures,omitempty"`
		Error     string `json:"error,omitempty"`
	}
	type bundle struct {
		GeneratedAt  string        `json:"generatedAt"`
		Version      string        `json:"version"`
		Revision     string        `json:"revision"`
		UptimeSec    int64         `json:"uptimeSec"`
		ProviderHost string        `json:"providerHost"`
		Errors       []recentError `json:"recentErrors"`
		Runs         []bundleRun   `json:"recentRuns"`
	}
	b := bundle{
		GeneratedAt:  time.Now().UTC().Format(time.RFC3339Nano),
		Version:      a.version,
		Revision:     a.buildCommit,
		UptimeSec:    int64(time.Since(a.startedAt).Seconds()),
		ProviderHost: host,
	}
	a.debugMu.Lock()
	errs := make([]recentError, len(a.errorRing))
	copy(errs, a.errorRing)
	a.debugMu.Unlock()
	if len(errs) > 50 {
		errs = errs[len(errs)-50:]
	}
	b.Errors = errs

	type st struct {
		Updated string
		Run     bundleRun
	}
	var recent []st
	a.mu.Lock()
	for _, s := range a.sessions {
		if s.Deleted {
			continue
		}
		for _, t := range s.Runs {
			recent = append(recent, st{
				Updated: s.Updated,
				Run:     bundleRun{SessionID: s.ID, RunID: t.ID, Status: t.Status, Model: t.Model, Failures: t.Failures, Error: t.Error},
			})
		}
	}
	a.mu.Unlock()
	sort.Slice(recent, func(i, j int) bool { return recent[i].Updated > recent[j].Updated })
	if len(recent) > 5 {
		recent = recent[:5]
	}
	for _, x := range recent {
		b.Runs = append(b.Runs, x.Run)
	}
	jsonOut(w, 200, b)
}

// ──────────────────────────── 管理面（普通 access-token） ────────────────────────────

// debugAdminToken 生成/轮换调试令牌；明文仅本次返回一次，落盘仅哈希。
func (a *App) debugAdminToken(w http.ResponseWriter, r *http.Request) {
	raw := make([]byte, debugTokenBytes)
	if _, err := rand.Read(raw); err != nil {
		fail(w, 500, err)
		return
	}
	plain := hex.EncodeToString(raw)
	now := time.Now().UTC()
	exp := now.Add(debugTokenTTL)
	a.mu.Lock()
	a.settings.DebugTokenHash = sha256Hex(plain)
	a.settings.DebugTokenCreatedAt = now.Format(time.RFC3339Nano)
	a.settings.DebugTokenExpiresAt = exp.Format(time.RFC3339Nano)
	if err := atomicJSON(SettingsPath(a.dataPath), a.settings); err != nil {
		a.mu.Unlock()
		fail(w, 500, err)
		return
	}
	a.mu.Unlock()
	jsonOut(w, 200, map[string]any{
		"token":     plain, // 仅本次返回
		"createdAt": a.settings.DebugTokenCreatedAt,
		"expiresAt": a.settings.DebugTokenExpiresAt,
		"note":      "明文令牌仅此一次显示，关闭对话框后无法再次查看。",
	})
}

// debugAdminRevoke 立即吊销调试令牌（清空哈希）。
func (a *App) debugAdminRevoke(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	a.settings.DebugTokenHash = ""
	a.settings.DebugTokenCreatedAt = ""
	a.settings.DebugTokenExpiresAt = ""
	if err := atomicJSON(SettingsPath(a.dataPath), a.settings); err != nil {
		a.mu.Unlock()
		fail(w, 500, err)
		return
	}
	a.mu.Unlock()
	jsonOut(w, 200, map[string]bool{"revoked": true})
}

// debugAdminToggle 开关切换（规范入口仍是 PUT /api/settings）。关闭即清空令牌哈希。
func (a *App) debugAdminToggle(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Enabled *bool `json:"enabled"`
	}
	if err := decode(w, r, &in); err != nil {
		fail(w, 400, err)
		return
	}
	a.mu.Lock()
	a.settings.DebugAccessEnabled = in.Enabled != nil && *in.Enabled
	if !a.settings.DebugAccessEnabled {
		a.settings.DebugTokenHash = ""
		a.settings.DebugTokenCreatedAt = ""
		a.settings.DebugTokenExpiresAt = ""
	}
	if err := atomicJSON(SettingsPath(a.dataPath), a.settings); err != nil {
		a.mu.Unlock()
		fail(w, 500, err)
		return
	}
	a.mu.Unlock()
	jsonOut(w, 200, map[string]bool{"enabled": a.settings.DebugAccessEnabled})
}

// debugAudit 读取最近审计记录（管理面，普通 access-token）。仅返回最近 20 条用于面板预览；
// 完整日志走 /api/debug/audit/export 下载。
func (a *App) debugAudit(w http.ResponseWriter, r *http.Request) {
	out, err := a.readDebugAudit()
	if err != nil {
		fail(w, 500, err)
		return
	}
	if len(out) > 20 {
		out = out[len(out)-20:]
	}
	jsonOut(w, 200, out)
}

// debugAuditExport 导出完整审计日志（管理面，普通 access-token，附件下载）。
// format=jsonl（默认，逐行 JSON）| json（JSON 数组）| csv（带 BOM，便于 Excel 打开）。
// 导出本身不写审计（见 debugAuditShouldWrite：审计自身的查看/导出不记录）。
func (a *App) debugAuditExport(w http.ResponseWriter, r *http.Request) {
	entries, err := a.readDebugAudit()
	if err != nil {
		fail(w, 500, err)
		return
	}
	format := r.URL.Query().Get("format")
	if format == "" {
		format = "jsonl"
	}
	switch format {
	case "jsonl":
		w.Header().Set("Content-Type", "application/x-ndjson; charset=utf-8")
		w.Header().Set("Content-Disposition", `attachment; filename="debug-audit.jsonl"`)
		for _, e := range entries {
			if b, merr := json.Marshal(e); merr == nil {
				_, _ = w.Write(append(b, '\n'))
			}
		}
	case "json":
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Content-Disposition", `attachment; filename="debug-audit.json"`)
		jsonOut(w, 200, entries)
	case "csv":
		var buf bytes.Buffer
		buf.WriteString("\xEF\xBB\xBF") // UTF-8 BOM，Excel 正确识别中文
		cw := csv.NewWriter(&buf)
		_ = cw.Write([]string{"time", "ip", "ua", "method", "path", "owner", "tokenFp", "result"})
		for _, e := range entries {
			src := "owner"
			if !e.Owner {
				src = "token"
			}
			_ = cw.Write([]string{e.Time, e.IP, e.UA, e.Method, e.Path, src, e.TokenFP, e.Result})
		}
		cw.Flush()
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		w.Header().Set("Content-Disposition", `attachment; filename="debug-audit.csv"`)
		w.Header().Set("Content-Length", strconv.Itoa(buf.Len()))
		_, _ = w.Write(buf.Bytes())
	default:
		fail(w, 400, errors.New("format 仅支持 jsonl|json|csv"))
	}
}
