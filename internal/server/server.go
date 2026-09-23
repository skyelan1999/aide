package server

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"
)

//go:embed web/*
var assets embed.FS

type ModelRef struct {
	ID            string `json:"id"`
	Name          string `json:"name,omitempty"`
	ContextWindow int    `json:"contextWindow,omitempty"`
}
type Settings struct {
	BaseURL     string     `json:"baseURL"`
	Model       string     `json:"model"`
	APIKey      string     `json:"apiKey,omitempty"`
	Models      []ModelRef `json:"models,omitempty"`
	ActiveModel string     `json:"activeModel,omitempty"`
}

const (
	defaultContextWindow = 65536
	maxModels            = 20
)

// normalizeModels 校验并补全模型列表（LIM-25）；返回错误时列表不被采纳。
func normalizeModels(models []ModelRef) ([]ModelRef, error) {
	if len(models) > maxModels {
		return nil, fmt.Errorf("模型最多 %d 个", maxModels)
	}
	seen := map[string]bool{}
	out := make([]ModelRef, 0, len(models))
	for _, m := range models {
		m.ID = strings.TrimSpace(m.ID)
		if m.ID == "" || len(m.ID) > 64 {
			return nil, errors.New("模型 id 不能为空且最长 64 字符")
		}
		if seen[m.ID] {
			return nil, fmt.Errorf("模型 id %s 重复", m.ID)
		}
		seen[m.ID] = true
		m.Name = strings.TrimSpace(m.Name)
		if m.Name == "" {
			m.Name = m.ID
		}
		if len([]rune(m.Name)) > 32 {
			return nil, fmt.Errorf("模型名称 %q 最长 32 个字符", m.Name)
		}
		if m.ContextWindow == 0 {
			m.ContextWindow = defaultContextWindow
		}
		if m.ContextWindow < 1024 || m.ContextWindow > 1048576 {
			return nil, fmt.Errorf("模型 %s 的上下文窗口须在 1024–1048576 之间", m.ID)
		}
		out = append(out, m)
	}
	return out, nil
}

type ToolCall struct {
	ID       string `json:"id,omitempty"`
	Type     string `json:"type,omitempty"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}
type Message struct {
	Role       string     `json:"role"`
	Content    string     `json:"content"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}
type Session struct {
	ID                string    `json:"id"`
	Title             string    `json:"title"`
	Created           string    `json:"created"`
	Messages          []Message `json:"messages"`
	Runs              []*Task   `json:"runs"`
	Compact           string    `json:"compact,omitempty"`           // 压缩摘要（compaction）
	CompactedMessages int       `json:"compactedMessages,omitempty"` // 已折叠消息数
	CompactedAt       string    `json:"compactedAt,omitempty"`
}
type App struct {
	mu                        sync.Mutex
	filesMu                   sync.Mutex
	workspace, reference      *os.Root
	workPath, dataPath, token string
	settings                  Settings
	sessions                  map[string]*Session
	cancels                   map[string]context.CancelFunc
	commands                  chan struct{}
	profilesPath              string
	profileState              ProfilesState
	version                   string
	pluginsPath               string
	pluginRegistry            pluginRegistry
	pluginSurface             []byte
	wsConfigPath              string
	wsSecretsPath             string
	wsConfig                  WorkspaceConfig
	wsSecrets                 workspaceSecrets
	sshBin, sftpBin           string
	localRoot                 *os.Root
	hostLocal                 string
	workspaceDisplay          string
	cacheContainer            string
	curlBin                   string
	sourceRegistry            sourcesRegistry
	sourceSecrets             sourcesSecrets
	tokenStats                map[string]TokenDay
	tokenStatsMu              sync.Mutex
	wsRevision                uint64
	compactingSessions        map[string]bool
	retiredRoots              []*os.Root
	wsRoots                   map[string]*os.Root // 工作区身份 → 打开中的根（R02 运行中任务的工具绑定）
	pricing                   PricingState
	tokenCalls                []TokenCallRec
	buildVersion, buildCommit string
}

// Pricing 单模型费率（R08）：0 为合法值；历史费用按调用时刻快照，改价只影响后续调用。
type Pricing struct {
	PriceIn  float64 `json:"priceIn"`
	PriceOut float64 `json:"priceOut"`
}

// PricingState 服务端计价状态：按模型费率 + 未配置模型的刊例默认价。
// 显式配置（含 0/0 免费）优先于默认；默认价仅用于从未配置过的模型，且记录必须标记 defaulted。
type PricingState struct {
	Rates   map[string]Pricing `json:"rates"`
	Default Pricing            `json:"default"`
}

// TokenCallRec 逐调用记录（R08）：来源、模型、计价快照、费用。
type TokenCallRec struct {
	Time       string  `json:"time"`
	Model      string  `json:"model"`
	Provider   string  `json:"provider,omitempty"`
	Prompt     int     `json:"prompt"`
	Completion int     `json:"completion"`
	Total      int     `json:"total"`
	Estimated  bool    `json:"estimated,omitempty"`
	Defaulted  bool    `json:"defaulted,omitempty"` // 该模型未配置费率，费用按刊例默认价（估算性质）
	PriceIn    float64 `json:"priceIn"`
	PriceOut   float64 `json:"priceOut"`
	Cost       float64 `json:"cost"`
}

// TokenDay 单日 Token 消耗（FR-90）。Priced 表示该日累计时存在计价快照；
// 旧版汇总（v1）没有逐调用与计价证据，Priced=false，UI 必须显示为未计价。
type TokenDay struct {
	Prompt     int  `json:"prompt"`
	Completion int  `json:"completion"`
	Total      int  `json:"total"`
	Calls      int  `json:"calls"`
	Estimated  bool `json:"estimated,omitempty"`
	Priced     bool `json:"priced,omitempty"`
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

var buildVersion, buildCommit string

var versionRE = regexp.MustCompile(`[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+ RC[0-9]+`)

// readVersionFile 从工程目录 version.md 取当前版本（FR-66 / LIM-24）；缺失或非法返回空串。
func readVersionFile(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return versionRE.FindString(string(b))
}

func newID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}
func jsonOut(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
func fail(w http.ResponseWriter, code int, err error) {
	jsonOut(w, code, map[string]string{"error": err.Error()})
}
func decode(w http.ResponseWriter, r *http.Request, v any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	d := json.NewDecoder(r.Body)
	if err := d.Decode(v); err != nil {
		return err
	}
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		return errors.New("请求必须是单个 JSON 对象")
	}
	return nil
}
func atomicJSON(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".aide-*")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if _, err = f.Write(b); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(name, path)
}
func New(work, reference, data string) (*App, error) {
	for _, dir := range []string{work, reference, data} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			return nil, err
		}
	}
	w, err := os.OpenRoot(work)
	if err != nil {
		return nil, err
	}
	r, err := os.OpenRoot(reference)
	if err != nil {
		w.Close()
		return nil, err
	}
	a := &App{workspace: w, reference: r, workPath: work, dataPath: data, sessions: map[string]*Session{}, cancels: map[string]context.CancelFunc{}, commands: make(chan struct{}, 4), compactingSessions: map[string]bool{}, wsRoots: map[string]*os.Root{defaultWorkspaceID: w}}
	b, err := os.ReadFile(filepath.Join(data, "access-token"))
	if errors.Is(err, os.ErrNotExist) {
		b = []byte(newID() + newID())
		err = os.WriteFile(filepath.Join(data, "access-token"), b, 0600)
	}
	if err != nil {
		a.Close()
		return nil, err
	}
	a.token = strings.TrimSpace(string(b))
	if len(a.token) < 32 {
		a.Close()
		return nil, errors.New("access-token 无效")
	}
	a.settings = Settings{BaseURL: env("AI_BASE_URL", "https://api.deepseek.com"), Model: os.Getenv("AI_MODEL"), APIKey: os.Getenv("AI_API_KEY")}
	if b, err := os.ReadFile(filepath.Join(data, "settings.json")); err == nil {
		if err = json.Unmarshal(b, &a.settings); err != nil {
			a.Close()
			return nil, err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		a.Close()
		return nil, err
	}
	if len(a.settings.Models) == 0 && a.settings.Model != "" {
		// 旧格式迁移：单模型 → 模型列表 + 当前模型（FR-67 / D1）
		a.settings.Models = []ModelRef{{ID: a.settings.Model, Name: a.settings.Model, ContextWindow: defaultContextWindow}}
		a.settings.ActiveModel = a.settings.Model
	}
	if a.settings.ActiveModel == "" {
		a.settings.ActiveModel = a.settings.Model
	}
	if a.settings.Models != nil {
		normalized, err := normalizeModels(a.settings.Models)
		if err != nil {
			a.Close()
			return nil, fmt.Errorf("settings.json: %w", err)
		}
		a.settings.Models = normalized
	}
	if a.settings.ActiveModel == "" && len(a.settings.Models) > 0 {
		a.settings.ActiveModel = a.settings.Models[0].ID
		a.settings.Model = a.settings.ActiveModel
	}
	a.profilesPath = filepath.Join(work, profilesFileName)
	if err := a.loadProfiles(); err != nil {
		a.Close()
		return nil, err
	}
	// R09：版本为构建期身份（ldflags 注入），不得被工作区内的 version.md 覆盖；
	// 仅在开发构建（未注入）时回退读取工程 version.md（FR-66 / LIM-24）。
	if buildVersion != "" {
		a.version = buildVersion
	} else {
		a.version = readVersionFile(filepath.Join(work, "version.md"))
	}
	a.buildVersion = buildVersion
	a.buildCommit = buildCommit
	if err := a.loadPlugins(); err != nil {
		a.Close()
		return nil, err
	}
	if a.pluginsPath == "" {
		a.pluginsPath = filepath.Join(work, pluginsDirName)
	}
	a.runPluginHost(context.Background())
	a.sshBin = "ssh"
	a.sftpBin = "sftp"
	a.curlBin = "curl"
	a.hostLocal = env("AIDE_HOST_LOCAL", os.Getenv("HOME"))
	localRoot, localErr := os.OpenRoot("/local")
	if localErr != nil {
		localRoot, localErr = os.OpenRoot(work)
		if localErr != nil {
			a.Close()
			return nil, localErr
		}
	}
	a.localRoot = localRoot
	if err := a.loadWorkspaceConfig(); err != nil {
		a.Close()
		return nil, err
	}
	if err := a.loadSources(); err != nil {
		a.Close()
		return nil, err
	}
	a.tokenStats = map[string]TokenDay{}
	if b, err := os.ReadFile(filepath.Join(data, "token-stats.json")); err == nil {
		var raw struct {
			Version int `json:"version"`
			Legacy  *struct {
				Version int                 `json:"version"`
				Days    map[string]TokenDay `json:"days"`
			} `json:"legacyStatsV1"`
			Days  map[string]TokenDay `json:"days"`
			Calls []TokenCallRec      `json:"calls"`
		}
		if err := json.Unmarshal(b, &raw); err != nil {
			// R08：损坏或未知版本保留原文件并可诊断，绝不静默清零
			log.Printf("token-stats.json 无法解析（保留原文件，未迁移）: %v", err)
		} else if raw.Legacy != nil && raw.Legacy.Days != nil {
			// 旧版汇总：保留用量与 estimated 标记；无逐调用与计价证据，Priced=false（未计价）
			a.tokenStats = raw.Legacy.Days
			log.Printf("token-stats.json 为旧版汇总（legacyStatsV1）：已迁移用量，历史费用标记为未计价")
		} else if raw.Days != nil {
			a.tokenStats = raw.Days
			a.tokenCalls = raw.Calls
		} else {
			log.Printf("token-stats.json 缺少有效 days（保留原文件，未清零）")
		}
	}
	a.pricing = PricingState{Rates: map[string]Pricing{}, Default: Pricing{PriceIn: 2, PriceOut: 8}}
	if b, err := os.ReadFile(filepath.Join(data, "token-pricing.json")); err == nil {
		var raw map[string]json.RawMessage
		if json.Unmarshal(b, &raw) != nil {
			log.Printf("token-pricing.json 无法解析（保留原文件，使用默认刊例价）")
		} else if _, ok := raw["rates"]; !ok {
			// 旧单费率格式：整体迁移为默认刊例价，不冒充任何模型的精确费率
			var legacy Pricing
			if json.Unmarshal(b, &legacy) == nil && legacy.PriceIn >= 0 && legacy.PriceOut >= 0 {
				a.pricing.Default = legacy
				log.Printf("token-pricing.json 旧单费率已迁移为默认刊例价")
			}
		} else {
			var pr PricingState
			if json.Unmarshal(b, &pr) == nil {
				if pr.Rates == nil {
					pr.Rates = map[string]Pricing{}
				}
				for m, v := range pr.Rates {
					if v.PriceIn < 0 || v.PriceOut < 0 {
						delete(pr.Rates, m) // 非法费率不采纳
					}
				}
				if pr.Default.PriceIn < 0 || pr.Default.PriceOut < 0 {
					pr.Default = Pricing{PriceIn: 2, PriceOut: 8}
				}
				a.pricing = pr
			}
		}
	}
	tokenUsageRecorder.Store(func(u TokenUsage) { a.recordTokenUsage(u) })
	entries, err := filepath.Glob(filepath.Join(data, "session-*.json"))
	if err != nil {
		a.Close()
		return nil, err
	}
	for _, path := range entries {
		b, err := os.ReadFile(path)
		if err != nil {
			a.Close()
			return nil, err
		}
		var s Session
		if err := json.Unmarshal(b, &s); err != nil {
			log.Printf("跳过损坏会话文件 %s: %v", filepath.Base(path), err) // R09：坏文件不阻断启动
			continue
		}
		a.sessions[s.ID] = &s
		for _, task := range s.Runs {
			if task.Status == "running" {
				task.Status = "interrupted"
				task.Error = "服务重启，任务已中断。可重新提交。"
			}
		}
		if err := a.save(&s); err != nil {
			a.Close()
			return nil, err
		}
	}
	return a, nil
}
func (a *App) Close() {
	a.workspace.Close()
	a.reference.Close()
	if a.localRoot != nil {
		a.localRoot.Close()
	}
	for _, r := range a.retiredRoots {
		r.Close()
	}
}
func (a *App) save(s *Session) error {
	return atomicJSON(filepath.Join(a.dataPath, "session-"+s.ID+".json"), s)
}

func (a *App) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		jsonOut(w, 200, map[string]string{"status": "ok", "service": "aide"})
	})
	mux.HandleFunc("GET /api/config", a.config)
	mux.HandleFunc("PUT /api/settings", a.updateSettings)
	mux.HandleFunc("GET /api/models", a.listModels)
	mux.HandleFunc("GET /api/balance", a.listBalance)
	mux.HandleFunc("GET /api/profiles", a.listProfiles)
	mux.HandleFunc("GET /api/plugins", a.listPlugins)
	mux.HandleFunc("POST /api/plugins", a.uploadPlugin)
	mux.HandleFunc("PUT /api/plugins/{id}", a.togglePlugin)
	mux.HandleFunc("DELETE /api/plugins/{id}", a.deletePlugin)
	mux.HandleFunc("GET /api/plugin-surface", a.pluginSurfaceHandler)
	mux.HandleFunc("GET /api/workspace-config", a.getWorkspaceConfig)
	mux.HandleFunc("GET /api/sources", a.listSources)
	mux.HandleFunc("GET /api/token-stats", a.tokenStatsHandler)
	mux.HandleFunc("GET /api/token-pricing", a.tokenPricingHandler)
	mux.HandleFunc("PUT /api/token-pricing", a.tokenPricingHandler)
	mux.HandleFunc("GET /api/search", a.searchSessions)
	mux.HandleFunc("POST /api/sessions/{id}/compact", a.compactSession)
	mux.HandleFunc("PUT /api/sources", a.updateSources)
	mux.HandleFunc("PUT /api/workspace-config", a.updateWorkspaceConfig)
	mux.HandleFunc("PUT /api/profiles", a.updateProfiles)
	mux.HandleFunc("GET /api/files", a.listFiles)
	mux.HandleFunc("GET /api/file", a.readFile)
	mux.HandleFunc("PUT /api/file", a.writeFile)
	mux.HandleFunc("GET /api/sessions", a.listSessions)
	mux.HandleFunc("POST /api/sessions", a.createSession)
	mux.HandleFunc("GET /api/sessions/{id}", a.getSession)
	mux.HandleFunc("POST /api/sessions/{id}/runs", a.startTask)
	mux.HandleFunc("POST /api/sessions/{id}/runs/{run}/cancel", a.cancelTask)
	mux.HandleFunc("POST /api/sessions/{id}/runs/{run}/apply", a.applyTask)
	mux.HandleFunc("GET /api/sessions/{id}/runs/{run}/requests", a.runRequestsHandler)
	mux.HandleFunc("POST /api/context-preview", a.contextPreviewHandler)
	mux.HandleFunc("POST /api/command", a.command)
	web, _ := fs.Sub(assets, "web")
	mux.Handle("/", http.FileServer(http.FS(web)))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; connect-src 'self'; img-src 'self' data:; frame-ancestors 'none'; base-uri 'none'")
		w.Header().Set("Cache-Control", "no-store")
		if strings.HasPrefix(r.URL.Path, "/api/") {
			if origin := r.Header.Get("Origin"); origin != "" {
				u, err := url.Parse(origin)
				if err != nil || u.Host != r.Host {
					fail(w, 403, errors.New("跨站请求被拒绝"))
					return
				}
			}
			token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
			if subtle.ConstantTimeCompare([]byte(token), []byte(a.token)) != 1 {
				fail(w, 401, errors.New("请输入访问令牌，或用 start.command 打开"))
				return
			}
		}
		mux.ServeHTTP(w, r)
	})
}
func (a *App) config(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	defer a.mu.Unlock()
	jsonOut(w, 200, map[string]any{"name": "aide", "version": a.version, "buildVersion": a.buildVersion, "buildCommit": a.buildCommit, "revision": a.buildCommit, "baseURL": a.settings.BaseURL, "model": a.settings.Model, "configured": a.settings.Model != "" && a.settings.BaseURL != "", "hasKey": a.settings.APIKey != "", "models": a.settings.Models, "activeModel": a.settings.ActiveModel, "workspace": "/workspace", "context": "/context", "hostLocal": a.hostLocal, "workspaceDisplay": a.workspaceDisplay, "runtime": "Go · Python · Node.js · Git", "workflow": []string{"plan", "propose", "review"}})
}
func (a *App) updateSettings(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Settings
		ClearKey bool `json:"clearKey"`
	}
	if err := decode(w, r, &in); err != nil {
		fail(w, 400, err)
		return
	}
	// 前端部分更新（如仅切换 activeModel）时，用已存值补全后再校验
	a.mu.Lock()
	defer a.mu.Unlock()
	if in.BaseURL == "" {
		in.BaseURL = a.settings.BaseURL
	}
	if in.APIKey == "" && !in.ClearKey {
		in.APIKey = a.settings.APIKey
	}
	if in.Models == nil {
		in.Models = a.settings.Models
	}
	u, err := url.Parse(in.BaseURL)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		fail(w, 400, errors.New("请输入有效的 HTTP(S) API Base URL"))
		return
	}
	if in.Model == "" && in.ActiveModel == "" {
		fail(w, 400, errors.New("请至少配置一个模型"))
		return
	}
	// 旧式单模型请求兼容：model 字段 → 模型列表 + 当前模型（FR-67 / D1）
	if len(in.Models) == 0 && in.Model != "" {
		in.Models = []ModelRef{{ID: in.Model, Name: in.Model, ContextWindow: defaultContextWindow}}
		if in.ActiveModel == "" {
			in.ActiveModel = in.Model
		}
	}
	models, err := normalizeModels(in.Models)
	if err != nil {
		fail(w, 400, err)
		return
	}
	in.Models = models
	if in.ActiveModel == "" && len(in.Models) > 0 {
		in.ActiveModel = in.Models[0].ID
	}
	activeFound := false
	for _, m := range in.Models {
		if m.ID == in.ActiveModel {
			activeFound = true
			break
		}
	}
	if !activeFound {
		fail(w, 400, errors.New("当前模型不在模型列表中"))
		return
	}
	in.Model = in.ActiveModel // Model 保留为当前生效模型 id（Provider 与旧逻辑零改动）
	in.BaseURL = strings.TrimRight(in.BaseURL, "/")
	if err := atomicJSON(filepath.Join(a.dataPath, "settings.json"), in.Settings); err != nil {
		fail(w, 500, err)
		return
	}
	a.settings = in.Settings
	jsonOut(w, 200, map[string]bool{"ok": true})
}
func (a *App) listSessions(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	defer a.mu.Unlock()
	items := []map[string]string{}
	for _, s := range a.sessions {
		items = append(items, map[string]string{"id": s.ID, "title": s.Title, "created": s.Created})
	}
	sort.Slice(items, func(i, j int) bool { return items[i]["created"] > items[j]["created"] })
	jsonOut(w, 200, items)
}
func (a *App) createSession(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Title string `json:"title"`
	}
	if err := decode(w, r, &in); err != nil {
		fail(w, 400, err)
		return
	}
	if strings.TrimSpace(in.Title) == "" {
		in.Title = "新会话"
	}
	s := &Session{ID: newID(), Title: in.Title, Created: time.Now().UTC().Format(time.RFC3339Nano), Messages: []Message{}, Runs: []*Task{}}
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := a.save(s); err != nil {
		fail(w, 500, err)
		return
	}
	a.sessions[s.ID] = s
	jsonOut(w, 201, s)
}
func (a *App) getSession(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	defer a.mu.Unlock()
	s := a.sessions[r.PathValue("id")]
	if s == nil {
		fail(w, 404, errors.New("会话不存在"))
		return
	}
	jsonOut(w, 200, s)
}

func Run() error {
	a, err := New(env("AIDE_WORKSPACE", "."), env("AIDE_CONTEXT", "context"), env("AIDE_DATA", ".data"))
	if err != nil {
		return err
	}
	defer a.Close()
	s := &http.Server{Addr: env("AIDE_ADDR", "127.0.0.1:8097"), Handler: a.Handler(), ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 << 10}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		a.mu.Lock()
		for _, cancel := range a.cancels {
			cancel()
		}
		a.mu.Unlock()
		c, done := context.WithTimeout(context.Background(), 8*time.Second)
		defer done()
		_ = s.Shutdown(c)
	}()
	log.Printf("aide listening on %s; token saved in %s/access-token", s.Addr, a.dataPath)
	err = s.ListenAndServe()
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

// recordTokenUsage 累计当日 Token 消耗并持久化到 /data/token-stats.json（FR-90）。
func (a *App) recordTokenUsage(u TokenUsage) {
	a.tokenStatsMu.Lock()
	day := time.Now().UTC().Format("2006-01-02")
	d := a.tokenStats[day]
	d.Prompt += u.Prompt
	d.Completion += u.Completion
	d.Total += u.Total
	d.Calls++
	d.Priced = true // 本次累计存在计价快照与逐调用证据
	if u.Estimated {
		d.Estimated = true
	}
	a.tokenStats[day] = d
	entry, configured := a.pricing.Rates[u.Model]
	if !configured {
		entry = a.pricing.Default
	}
	rec := TokenCallRec{Time: time.Now().UTC().Format(time.RFC3339Nano), Model: u.Model, Provider: u.Provider, Prompt: u.Prompt, Completion: u.Completion, Total: u.Total, Estimated: u.Estimated, Defaulted: !configured, PriceIn: entry.PriceIn, PriceOut: entry.PriceOut}
	rec.Cost = float64(rec.Prompt)*rec.PriceIn/1e6 + float64(rec.Completion)*rec.PriceOut/1e6
	a.tokenCalls = append(a.tokenCalls, rec)
	if len(a.tokenCalls) > 5000 {
		a.tokenCalls = a.tokenCalls[len(a.tokenCalls)-5000:]
	}
	saveErr := atomicJSON(filepath.Join(a.dataPath, "token-stats.json"), map[string]any{"version": 2, "days": a.tokenStats, "calls": a.tokenCalls})
	a.tokenStatsMu.Unlock()
	_ = saveErr
}

func (a *App) tokenPricingHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		a.tokenStatsMu.Lock()
		p := a.pricing
		active := a.settings.Model
		a.tokenStatsMu.Unlock()
		entry, configured := p.Rates[active]
		if !configured {
			entry = p.Default
		}
		jsonOut(w, 200, map[string]any{
			"priceIn":   entry.PriceIn,
			"priceOut":  entry.PriceOut,
			"model":     active,
			"defaulted": !configured,
			"rates":     p.Rates,
			"default":   p.Default,
		})
		return
	}
	var in struct {
		Model    string  `json:"model"`
		PriceIn  float64 `json:"priceIn"`
		PriceOut float64 `json:"priceOut"`
	}
	if err := decode(w, r, &in); err != nil {
		fail(w, 400, err)
		return
	}
	in.Model = strings.TrimSpace(in.Model)
	if in.Model == "" {
		a.mu.Lock()
		in.Model = a.settings.Model
		a.mu.Unlock()
	}
	if in.Model == "" {
		fail(w, 400, errors.New("请先配置当前模型，或显式指定 model"))
		return
	}
	if in.PriceIn < 0 || in.PriceOut < 0 {
		fail(w, 400, errors.New("费率必须 ≥ 0（0 为合法免费，不等于留空）"))
		return
	}
	a.tokenStatsMu.Lock()
	a.pricing.Rates[in.Model] = Pricing{PriceIn: in.PriceIn, PriceOut: in.PriceOut}
	err := atomicJSON(filepath.Join(a.dataPath, "token-pricing.json"), a.pricing)
	a.tokenStatsMu.Unlock()
	if err != nil {
		fail(w, 500, err)
		return
	}
	jsonOut(w, 200, map[string]any{"model": in.Model, "priceIn": in.PriceIn, "priceOut": in.PriceOut})
}

func (a *App) tokenStatsHandler(w http.ResponseWriter, r *http.Request) {
	a.tokenStatsMu.Lock()
	defer a.tokenStatsMu.Unlock()
	totals := TokenDay{}
	priced := TokenDay{}
	unpriced := TokenDay{}
	for _, d := range a.tokenStats {
		totals.Prompt += d.Prompt
		totals.Completion += d.Completion
		totals.Total += d.Total
		totals.Calls += d.Calls
		if d.Estimated {
			totals.Estimated = true
		}
		tgt := &unpriced
		if d.Priced {
			tgt = &priced
		}
		tgt.Prompt += d.Prompt
		tgt.Completion += d.Completion
		tgt.Total += d.Total
		tgt.Calls += d.Calls
		if d.Estimated {
			tgt.Estimated = true
		}
	}
	totalCost := 0.0
	estimatedCost := 0.0
	modelCost := map[string]float64{}
	for _, c := range a.tokenCalls {
		if c.Defaulted {
			estimatedCost += c.Cost // 未配置费率的模型：刊例默认价估算，不算精确费用
			continue
		}
		totalCost += c.Cost
		modelCost[c.Model] += c.Cost
	}
	active := a.settings.Model
	entry, configured := a.pricing.Rates[active]
	if !configured {
		entry = a.pricing.Default
	}
	jsonOut(w, 200, map[string]any{
		"days":          a.tokenStats,
		"totals":        totals,
		"today":         a.tokenStats[time.Now().UTC().Format("2006-01-02")],
		"cost":          totalCost,
		"estimatedCost": estimatedCost,
		"modelCost":     modelCost,
		"pricing": map[string]any{
			"priceIn":   entry.PriceIn,
			"priceOut":  entry.PriceOut,
			"defaulted": !configured,
			"rates":     a.pricing.Rates,
			"default":   a.pricing.Default,
		},
		"calls": len(a.tokenCalls),
		// R08：已计价/未计价拆分；旧版汇总没有逐调用与计价证据，费用必须显示为未知
		"pricedTotals":   priced,
		"unpricedTotals": unpriced,
		"callRecords":    a.tokenCalls,
	})
}
