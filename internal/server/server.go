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
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}
type Session struct {
	ID       string    `json:"id"`
	Title    string    `json:"title"`
	Created  string    `json:"created"`
	Messages []Message `json:"messages"`
	Runs     []*Task   `json:"runs"`
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
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
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
	a := &App{workspace: w, reference: r, workPath: work, dataPath: data, sessions: map[string]*Session{}, cancels: map[string]context.CancelFunc{}, commands: make(chan struct{}, 4)}
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
	a.version = readVersionFile(filepath.Join(work, "version.md"))
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
	if err := a.loadWorkspaceConfig(); err != nil {
		a.Close()
		return nil, err
	}
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
			a.Close()
			return nil, fmt.Errorf("读取会话 %s: %w", filepath.Base(path), err)
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
func (a *App) Close() { a.workspace.Close(); a.reference.Close() }
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
	mux.HandleFunc("GET /api/profiles", a.listProfiles)
	mux.HandleFunc("GET /api/plugins", a.listPlugins)
	mux.HandleFunc("POST /api/plugins", a.uploadPlugin)
	mux.HandleFunc("PUT /api/plugins/{id}", a.togglePlugin)
	mux.HandleFunc("DELETE /api/plugins/{id}", a.deletePlugin)
	mux.HandleFunc("GET /api/plugin-surface", a.pluginSurfaceHandler)
	mux.HandleFunc("GET /api/workspace-config", a.getWorkspaceConfig)
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
	jsonOut(w, 200, map[string]any{"name": "aide", "version": a.version, "baseURL": a.settings.BaseURL, "model": a.settings.Model, "configured": a.settings.Model != "" && a.settings.BaseURL != "", "hasKey": a.settings.APIKey != "", "models": a.settings.Models, "activeModel": a.settings.ActiveModel, "workspace": "/workspace", "context": "/context", "runtime": "Go · Python · Node.js · Git", "workflow": []string{"plan", "propose", "review"}})
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
