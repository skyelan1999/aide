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
	SandboxMode string     `json:"sandboxMode,omitempty"` // read-only | workspace-write | danger-full-access
	ToolMaxRounds int      `json:"toolMaxRounds,omitempty"` // 工具循环最大轮次，默认 60
	ShellTimeout  int      `json:"shellTimeout,omitempty"` // run_shell 超时秒数，默认 60，最大 300
	PersonaEnabled bool     `json:"personaEnabled,omitempty"`
	PersonaCipher          string `json:"personaCipher,omitempty"` // 兼容旧字段：单人格时代的性格密文
	ActivePersona          string            `json:"activePersona,omitempty"` // 当前活动人格 id（aide | xiaomi），默认 aide
	PersonaCiphers         map[string]string `json:"personaCiphers,omitempty"` // 每人格自定义性格密文（personaID -> AES-256-GCM base64）
	Personalities         map[string]Personality `json:"personalities,omitempty"` // 可演化性格（aide/xiaomi），明文
	DisabledTools          []string `json:"disabledTools,omitempty"` // 被禁用的工具名列表
	ReasoningEffort        string   `json:"reasoningEffort,omitempty"`   // 推理强度：auto/off/low/medium/high
	VoiceAssistantName     string   `json:"voiceAssistantName,omitempty"` // 语音小秘名字，默认"小秘"
	VoiceReplyEnabled      bool     `json:"voiceReplyEnabled,omitempty"` // 双向语音：语音回复模式
	VoiceReplyGender       string   `json:"voiceReplyGender,omitempty"`  // 回复音色 male | female
	VoiceInputDevice       string   `json:"voiceInputDevice,omitempty"`  // 小秘语音输入设备 deviceId，空=系统默认
	UserName               string   `json:"userName,omitempty"`          // 账户用户名（锁屏欢迎语用，可空）
	UserPasswordHash       string   `json:"userPasswordHash,omitempty"`  // 账户密码 SHA-256 哈希（不存明文；即小秘历史加密密钥）
	LockTimeoutSec         int      `json:"lockTimeoutSec,omitempty"`    // 空闲锁屏秒数，0 = 不锁屏
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
		if m.ContextWindow < 1024 || m.ContextWindow > 2097152 {
			return nil, fmt.Errorf("模型 %s 的上下文窗口须在 1024–2097152 之间", m.ID)
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
	Pinned            bool      `json:"pinned,omitempty"`   // 置顶：列表排序优先（活动排序不会把置顶顶下去）
	Archived          bool      `json:"archived,omitempty"` // 归档：默认列表隐藏
	Deleted           bool      `json:"deleted,omitempty"`  // 删除墓碑：save/加载跳过，防写盘复活
	Updated           string    `json:"updated,omitempty"`  // 最近活动时间：完成/跟进按时间置顶
	Checked           bool      `json:"checked,omitempty"`  // 已完成高亮（蓝点+加粗）是否已被用户查看；新完成时复位
	ParentID          string    `json:"parentId,omitempty"` // 子会话：指向主会话 ID
	AutoArchived      bool      `json:"autoArchived,omitempty"` // 子会话完成后自动归档
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
	eventMu                   sync.Mutex
	eventSubs                 map[string]map[chan streamEvent]struct{} // SSE 订阅：taskID → subscriber set
	personaKey               string // 内存中的性格解密密码（= 账户密码），不持久化
	personaCustom            map[string]string // 解锁后：personaID -> 解密出的自定义性格明文
	voiceAgent               *VoiceAgent // 语音小秘 agent（记忆+历史）
	voiceSendSinceEvolve    int        // 小秘自上次性格演化以来的 send 计数（内存）
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
	a := &App{workspace: w, reference: r, workPath: work, dataPath: data, sessions: map[string]*Session{}, cancels: map[string]context.CancelFunc{}, commands: make(chan struct{}, 4), compactingSessions: map[string]bool{}, wsRoots: map[string]*os.Root{defaultWorkspaceID: w}, eventSubs: map[string]map[chan streamEvent]struct{}{}}
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
	if a.settings.VoiceAssistantName == "" {
		a.settings.VoiceAssistantName = "小秘"
	}
	// 兼容旧版单人格：把旧 PersonaCipher 迁移为 aide 人格的自定义性格密文
	if a.settings.PersonaCipher != "" && a.settings.PersonaCiphers == nil {
		a.settings.PersonaCiphers = map[string]string{personaAide: a.settings.PersonaCipher}
	}
	if a.settings.VoiceReplyGender == "" {
		a.settings.VoiceReplyGender = "female"
	}
	a.voiceAgent = newVoiceAgent(data)
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
		if s.Deleted {
			continue // 删除墓碑残留：不载入
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
	if s.Deleted {
		return nil // 已删除会话不再落盘（运行中任务取消后的收尾保存同样跳过）
	}
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
	mux.HandleFunc("POST /api/models", a.listModels)
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
	mux.HandleFunc("POST /api/feedback", a.feedbackHandler)
	mux.HandleFunc("POST /api/persona/unlock", a.personaUnlock)
	mux.HandleFunc("POST /api/persona/save", a.personaSave)
	mux.HandleFunc("POST /api/persona/reset", a.personaReset)
	mux.HandleFunc("GET /api/persona", a.personaGet)
	mux.HandleFunc("GET /api/personas", a.personasList)
	mux.HandleFunc("POST /api/personas/active", a.personasActive)
	mux.HandleFunc("GET /api/personality", a.personalityGet)
	mux.HandleFunc("PUT /api/personality", a.personalitySave)
	mux.HandleFunc("POST /api/personality/reset", a.personalityReset)
	mux.HandleFunc("POST /api/personality/evolve", a.personalityEvolve)
	mux.HandleFunc("GET /api/token-pricing", a.tokenPricingHandler)
	mux.HandleFunc("PUT /api/token-pricing", a.tokenPricingHandler)
	mux.HandleFunc("GET /api/search", a.searchSessions)
	mux.HandleFunc("POST /api/sessions/{id}/compact", a.compactSession)
	mux.HandleFunc("PUT /api/sources", a.updateSources)
	mux.HandleFunc("PUT /api/workspace-config", a.updateWorkspaceConfig)
	mux.HandleFunc("PUT /api/profiles", a.updateProfiles)
	mux.HandleFunc("GET /api/files", a.listFiles)
	mux.HandleFunc("GET /api/file/raw", a.readFileRaw)
	mux.HandleFunc("GET /api/file", a.readFile)
	mux.HandleFunc("PUT /api/file", a.writeFile)
	mux.HandleFunc("POST /api/file/rename", a.renameFile)
	mux.HandleFunc("GET /api/sessions", a.listSessions)
	mux.HandleFunc("POST /api/sessions", a.createSession)
	mux.HandleFunc("GET /api/sessions/{id}", a.getSession)
	mux.HandleFunc("GET /api/export", a.exportSessions)
	mux.HandleFunc("DELETE /api/sessions/{id}", a.deleteSession)
	mux.HandleFunc("DELETE /api/sessions/archived/all", a.deleteAllArchived)
	mux.HandleFunc("PATCH /api/sessions/{id}", a.patchSession)
	mux.HandleFunc("POST /api/sessions/{id}/runs", a.startTask)
	mux.HandleFunc("POST /api/sessions/{id}/runs/{run}/retry", a.retryTask)
	mux.HandleFunc("POST /api/sessions/{id}/runs/{run}/cancel", a.cancelTask)
	mux.HandleFunc("POST /api/sessions/{id}/runs/{run}/apply", a.applyTask)
	mux.HandleFunc("POST /api/sessions/{id}/runs/{run}/answer", a.answerTask)
	mux.HandleFunc("GET /api/sessions/{id}/runs/{run}/requests", a.runRequestsHandler)
	mux.HandleFunc("GET /api/sessions/{id}/runs/{run}/events", a.runEvents)
	mux.HandleFunc("POST /api/sessions/{id}/runs/{run}/queue/{index}", a.queueUpdate)
	mux.HandleFunc("POST /api/context-preview", a.contextPreviewHandler)
	mux.HandleFunc("POST /api/command", a.command)
	mux.HandleFunc("POST /api/voice-filter", a.voiceFilter)
	mux.HandleFunc("GET /api/voice-history", a.voiceHistory)
	mux.HandleFunc("DELETE /api/voice-history", a.voiceHistoryClear)
	mux.HandleFunc("POST /api/voice-history/enable", a.voiceHistoryEnable)
	mux.HandleFunc("POST /api/voice-history/unlock", a.voiceHistoryUnlock)
	mux.HandleFunc("POST /api/voice-history/lock", a.voiceHistoryLock)
	mux.HandleFunc("POST /api/voice-history/change-password", a.voiceHistoryChangePassword)
	mux.HandleFunc("POST /api/voice-history/disable", a.voiceHistoryDisable)
	mux.HandleFunc("POST /api/account/verify-password", a.accountVerifyPassword)
	web, _ := fs.Sub(assets, "web")
	mux.Handle("/", http.FileServer(http.FS(web)))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		if strings.HasPrefix(r.URL.Path, "/vendor/drawio/") {
			w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self' 'unsafe-eval'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'; frame-ancestors 'self'; base-uri 'none'")
		} else {
			w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; connect-src 'self'; img-src 'self' data:; frame-ancestors 'none'; base-uri 'none'")
		}
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
			// EventSource / img 标签无法设置 Authorization 头，events 和 file/raw 路由允许 ?access_token=；
			// 其余 API 不收 URL 中的凭据，避免令牌进入日志/代理记录。
			if token == "" && r.Method == http.MethodGet && (strings.HasSuffix(r.URL.Path, "/events") || strings.HasSuffix(r.URL.Path, "/api/file/raw")) {
				token = r.URL.Query().Get("access_token")
			}
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
	jsonOut(w, 200, map[string]any{"name": "aide", "version": a.version, "buildVersion": a.buildVersion, "buildCommit": a.buildCommit, "revision": a.buildCommit, "baseURL": a.settings.BaseURL, "model": a.settings.Model, "configured": a.settings.Model != "" && a.settings.BaseURL != "", "hasKey": a.settings.APIKey != "", "models": a.settings.Models, "activeModel": a.settings.ActiveModel, "workspace": "/workspace", "context": "/context", "hostLocal": a.hostLocal, "workspaceDisplay": a.workspaceDisplay, "runtime": "Go · Python · Node.js · Git", "disabledTools": a.settings.DisabledTools, "reasoningEffort": a.settings.ReasoningEffort, "voiceAssistantName": a.settings.VoiceAssistantName, "voiceReplyEnabled": a.settings.VoiceReplyEnabled, "voiceReplyGender": voiceReplyGender(a.settings.VoiceReplyGender), "voiceInputDevice": a.settings.VoiceInputDevice, "userName": a.settings.UserName, "lockTimeoutSec": a.settings.LockTimeoutSec, "hasPassword": a.settings.UserPasswordHash != "", "activePersona": a.activePersonaID(), "personas": a.personaListOut(), "workflow": []string{"plan", "propose", "review"}})
}
func (a *App) updateSettings(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Settings
		ClearKey          bool   `json:"clearKey"`
		VoiceReplyEnabled *bool  `json:"voiceReplyEnabled,omitempty"`
		VoiceReplyGender  string `json:"voiceReplyGender,omitempty"`
		// 账户：外层同名字段覆盖内嵌 Settings（与 VoiceReplyEnabled 同模式），以便区分"未传"与"传空/0"
		UserName       string `json:"userName,omitempty"`
		LockTimeoutSec *int   `json:"lockTimeoutSec,omitempty"`
		OldPassword    string `json:"oldPassword,omitempty"`
		NewPassword    string `json:"newPassword,omitempty"`
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
	if in.DisabledTools == nil {
		in.DisabledTools = a.settings.DisabledTools
	}
	if in.ReasoningEffort == "" {
		in.ReasoningEffort = a.settings.ReasoningEffort
	}
	if in.VoiceAssistantName == "" {
		in.VoiceAssistantName = a.settings.VoiceAssistantName
	}
	// 人格状态由专用接口管理，settings PUT 不覆盖：保留当前活动人格与每人格自定义密文
	in.Settings.ActivePersona = a.settings.ActivePersona
	in.Settings.PersonaCiphers = a.settings.PersonaCiphers
	in.Settings.PersonaEnabled = a.settings.PersonaEnabled
	if in.VoiceReplyEnabled != nil {
		in.Settings.VoiceReplyEnabled = *in.VoiceReplyEnabled
	} else {
		in.Settings.VoiceReplyEnabled = a.settings.VoiceReplyEnabled
	}
	if in.VoiceReplyGender == "" {
		in.Settings.VoiceReplyGender = a.settings.VoiceReplyGender
	} else {
		in.Settings.VoiceReplyGender = in.VoiceReplyGender
	}
	// 账户字段：未传则保留已存值；密码哈希永远不接受前端直传，只经 OldPassword/NewPassword 流程变更
	if in.UserName == "" {
		in.Settings.UserName = a.settings.UserName
	} else {
		in.Settings.UserName = strings.TrimSpace(in.UserName)
	}
	if in.LockTimeoutSec != nil {
		sec := *in.LockTimeoutSec
		if sec < 0 {
			sec = 0
		}
		if sec > 86400 {
			sec = 86400
		}
		in.Settings.LockTimeoutSec = sec
	} else {
		in.Settings.LockTimeoutSec = a.settings.LockTimeoutSec
	}
	in.Settings.UserPasswordHash = a.settings.UserPasswordHash
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
	// 密码变更：账户密码即小秘历史加密密钥。旧密码已设置时必须验证；新密码同步重加密小秘历史。
	if in.NewPassword != "" {
		hasPw := a.settings.UserPasswordHash != ""
		if hasPw && sha256Hex(in.OldPassword) != a.settings.UserPasswordHash {
			fail(w, 400, errors.New("原密码错误"))
			return
		}
		if va := a.voiceAgent; va != nil {
			if st := va.encStatus(); st["encrypted"] == true {
				if st["unlocked"] != true {
					_ = va.unlock(in.OldPassword) // 锁定态先用旧密码解开（同密钥，应成功）
				}
				_ = va.changePassword(in.OldPassword, in.NewPassword) // 用新密钥重加密历史
			} else {
				_ = va.enable(in.NewPassword) // 首次设置密码：自动启用小秘历史加密
			}
		}
		in.Settings.UserPasswordHash = sha256Hex(in.NewPassword)
		// 账户密码即人格自定义性格密钥：用新密钥重加密已解锁的人格自定义内容
		if len(a.personaCustom) > 0 {
			if in.Settings.PersonaCiphers == nil {
				in.Settings.PersonaCiphers = map[string]string{}
			}
			for id, plain := range a.personaCustom {
				if ct, err := encryptPersona(plain, in.NewPassword); err == nil {
					in.Settings.PersonaCiphers[id] = ct
				}
			}
			a.personaKey = in.NewPassword
		}
	}
	if err := atomicJSON(filepath.Join(a.dataPath, "settings.json"), in.Settings); err != nil {
		fail(w, 500, err)
		return
	}
	a.settings = in.Settings
	jsonOut(w, 200, map[string]bool{"ok": true})
}

// voiceReplyGender 归一化回复音色，非法值回落 female。
func voiceReplyGender(g string) string {
	if g == "male" || g == "female" {
		return g
	}
	return "female"
}

// voiceFilter 语音小秘：把浏览器 Web Speech API 的转写文本交给小秘 agent 分析决策。
// 小秘结合记忆与上下文判断 send（对 aide 的指令，直接发到当前会话）/ ignore（背景声）/ standby（与人闲聊退下）。
// 任何失败都降级为 send 原文，保证语音录入在模型不可用时仍可用。
func (a *App) voiceFilter(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Text string `json:"text"`
	}
	if err := decode(w, r, &in); err != nil {
		fail(w, 400, err)
		return
	}
	text := strings.TrimSpace(in.Text)
	if text == "" {
		jsonOut(w, 200, map[string]any{"action": "ignore", "text": "", "reason": "空文本"})
		return
	}
	a.mu.Lock()
	cfg := a.settings
	va := a.voiceAgent
	a.mu.Unlock()
	if cfg.BaseURL == "" || cfg.Model == "" || va == nil {
		jsonOut(w, 200, map[string]any{"action": "send", "text": text, "reason": "未配置模型，直接发送"})
		return
	}
	entry, err := va.analyze(r.Context(), cfg, text)
	if err != nil {
		jsonOut(w, 200, map[string]any{"action": "send", "text": text, "reason": "小秘分析失败，直接发送: " + err.Error()})
		return
	}
	if entry.Action == "send" && strings.TrimSpace(entry.Text) == "" {
		entry.Text = text
	}
	if entry.Action == "send" {
		a.mu.Lock()
		a.voiceSendSinceEvolve++
		fire := a.voiceSendSinceEvolve >= 15
		if fire {
			a.voiceSendSinceEvolve = 0
		}
		a.mu.Unlock()
		if fire {
			go a.autoEvolvePersonality(personaXiaomi, "")
		}
	}
	jsonOut(w, 200, entry)
}

// voiceHistory 返回小秘决策时间线。加密且未解锁时只回状态、不返回任何内容。
func (a *App) voiceHistory(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	va := a.voiceAgent
	a.mu.Unlock()
	if va == nil {
		jsonOut(w, 200, map[string]any{"encrypted": false, "unlocked": true, "history": []VoiceHistoryEntry{}})
		return
	}
	st := va.encStatus()
	if enc, _ := st["encrypted"].(bool); enc {
		if unlocked, _ := st["unlocked"].(bool); !unlocked {
			jsonOut(w, 200, map[string]any{"encrypted": true, "unlocked": false})
			return
		}
	}
	st["history"] = va.historyDesc()
	jsonOut(w, 200, st)
}

// voiceHistoryClear 清空小秘历史（保留长期记忆文件）。加密态下同样生效。
func (a *App) voiceHistoryClear(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	va := a.voiceAgent
	a.mu.Unlock()
	if va != nil {
		va.clearHistory()
	}
	jsonOut(w, 200, map[string]bool{"ok": true})
}

func (a *App) voiceHistoryEnable(w http.ResponseWriter, r *http.Request) {
	var in struct{ Password string `json:"password"` }
	if decode(w, r, &in) != nil { return }
	a.mu.Lock(); va := a.voiceAgent; a.mu.Unlock()
	if va == nil { fail(w, 500, errors.New("小秘未初始化")); return }
	if err := va.enable(in.Password); err != nil { fail(w, 400, err); return }
	jsonOut(w, 200, map[string]any{"ok": true, "encrypted": true, "unlocked": true})
}

func (a *App) voiceHistoryUnlock(w http.ResponseWriter, r *http.Request) {
	var in struct{ Password string `json:"password"` }
	if decode(w, r, &in) != nil { return }
	a.mu.Lock(); va := a.voiceAgent; a.mu.Unlock()
	if va == nil { fail(w, 500, errors.New("小秘未初始化")); return }
	if err := va.unlock(in.Password); err != nil { fail(w, 401, err); return }
	jsonOut(w, 200, map[string]any{"ok": true, "encrypted": true, "unlocked": true})
}

func (a *App) voiceHistoryLock(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock(); va := a.voiceAgent; a.mu.Unlock()
	if va != nil { va.lock() }
	jsonOut(w, 200, map[string]any{"ok": true, "encrypted": true, "unlocked": false})
}

func (a *App) voiceHistoryChangePassword(w http.ResponseWriter, r *http.Request) {
	var in struct{ OldPassword string `json:"oldPassword"`; NewPassword string `json:"newPassword"` }
	if decode(w, r, &in) != nil { return }
	a.mu.Lock(); va := a.voiceAgent; a.mu.Unlock()
	if va == nil { fail(w, 500, errors.New("小秘未初始化")); return }
	if err := va.changePassword(in.OldPassword, in.NewPassword); err != nil { fail(w, 400, err); return }
	jsonOut(w, 200, map[string]any{"ok": true})
}

func (a *App) voiceHistoryDisable(w http.ResponseWriter, r *http.Request) {
	var in struct{ Password string `json:"password"` }
	if decode(w, r, &in) != nil { return }
	a.mu.Lock(); va := a.voiceAgent; a.mu.Unlock()
	if va == nil { fail(w, 500, errors.New("小秘未初始化")); return }
	if err := va.disable(in.Password); err != nil { fail(w, 400, err); return }
	jsonOut(w, 200, map[string]any{"ok": true, "encrypted": false, "unlocked": true})
}

// accountVerifyPassword 校验账户密码（解锁锁屏 / 小秘历史二次确认共用）。
// 只比对 SHA-256 哈希，不返回任何敏感信息；未设置密码时一律拒绝。
func (a *App) accountVerifyPassword(w http.ResponseWriter, r *http.Request) {
	var in struct{ Password string `json:"password"` }
	if decode(w, r, &in) != nil {
		return
	}
	a.mu.Lock()
	hash := a.settings.UserPasswordHash
	a.mu.Unlock()
	if hash == "" || sha256Hex(in.Password) != hash {
		fail(w, 401, errors.New("密码错误"))
		return
	}
	jsonOut(w, 200, map[string]bool{"ok": true})
}

// exportSessions 全部导出：所有会话（含归档）打包为 JSON 下载，不包含访问令牌与 API Key。
func (a *App) exportSessions(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	sessions := make([]*Session, 0, len(a.sessions))
	for _, s := range a.sessions {
		sessions = append(sessions, s)
	}
	a.mu.Unlock()
	sort.Slice(sessions, func(i, j int) bool { return sessions[i].Created > sessions[j].Created })
	payload := map[string]any{
		"version":    1,
		"app":        "aide",
		"exportedAt": time.Now().UTC().Format(time.RFC3339Nano),
		"count":      len(sessions),
		"sessions":   sessions,
	}
	b, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		fail(w, 500, err)
		return
	}
	name := "aide-sessions-" + time.Now().Format("20060102-150405") + ".json"
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(200)
	_, _ = w.Write(b)
}

func (a *App) listSessions(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	defer a.mu.Unlock()
	showArchived := r.URL.Query().Get("archived") == "1"
	type item struct {
		ID, Title, Created, Status, Updated, ParentID string
		Pinned, Archived, Checked, AutoArchived       bool
	}
	items := []item{}
	for _, sess := range a.sessions {
		if sess.Archived != showArchived {
			continue
		}
		status := ""
		for i := len(sess.Runs) - 1; i >= 0; i-- {
			if sess.Runs[i].Status != "" {
				status = sess.Runs[i].Status
				break
			}
		}
		items = append(items, item{sess.ID, sess.Title, sess.Created, status, sess.Updated, sess.ParentID, sess.Pinned, sess.Archived, sess.Checked, sess.AutoArchived})
	}
	// 置顶永远最前（活动排序不会把置顶顶下去）；非置顶按最近活动时间倒序
	sort.Slice(items, func(i, j int) bool {
		if items[i].Pinned != items[j].Pinned {
			return items[i].Pinned
		}
		ui, uj := items[i].Updated, items[j].Updated
		if ui == "" {
			ui = items[i].Created
		}
		if uj == "" {
			uj = items[j].Created
		}
		return ui > uj
	})
	out := make([]map[string]any, len(items))
	for i, it := range items {
		out[i] = map[string]any{"id": it.ID, "title": it.Title, "created": it.Created, "status": it.Status, "updated": it.Updated, "pinned": it.Pinned, "archived": it.Archived, "checked": it.Checked, "parentId": it.ParentID, "autoArchived": it.AutoArchived}
	}
	jsonOut(w, 200, out)
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
	now := time.Now().UTC().Format(time.RFC3339Nano)
	s := &Session{ID: newID(), Title: in.Title, Created: now, Updated: now, Messages: []Message{}, Runs: []*Task{}}
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := a.save(s); err != nil {
		fail(w, 500, err)
		return
	}
	a.sessions[s.ID] = s
	jsonOut(w, 201, s)
}

// patchSession 会话属性更新：pinned / archived（指针字段，缺省不动）。
func (a *App) patchSession(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Pinned   *bool `json:"pinned"`
		Archived *bool `json:"archived"`
		Touch    bool  `json:"touch"`
		Check    bool  `json:"check"`
	}
	if err := decode(w, r, &in); err != nil {
		fail(w, 400, err)
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	s := a.sessions[r.PathValue("id")]
	if s == nil {
		fail(w, 404, errors.New("会话不存在"))
		return
	}
	if in.Pinned != nil {
		s.Pinned = *in.Pinned
	}
	if in.Archived != nil {
		s.Archived = *in.Archived
	}
	if in.Touch {
		s.Updated = time.Now().UTC().Format(time.RFC3339Nano)
	}
	if in.Check {
		s.Checked = true
	}
	if err := a.save(s); err != nil {
		fail(w, 500, err)
		return
	}
	jsonOut(w, 200, s)
}

// deleteSession 删除会话：置墓碑、取消运行中任务、移出内存、删磁盘文件。
// 墓碑使 execute 的收尾保存直接跳过，避免任务取消后的写盘把会话文件复活。
func (a *App) deleteSession(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	id := r.PathValue("id")
	s := a.sessions[id]
	if s == nil {
		a.mu.Unlock()
		fail(w, 404, errors.New("会话不存在"))
		return
	}
	s.Deleted = true
	for _, t := range s.Runs {
		if t.Status == "running" {
			if cancel := a.cancels[t.ID]; cancel != nil {
				cancel()
				delete(a.cancels, t.ID)
			}
		}
	}
	delete(a.sessions, id)
	a.mu.Unlock()
	// save() 落盘路径为 dataPath/session-<id>.json（直接位于数据目录）
	if err := os.Remove(filepath.Join(a.dataPath, "session-"+id+".json")); err != nil && !os.IsNotExist(err) {
		log.Printf("删除会话文件失败: %v", err)
	}
	jsonOut(w, 200, map[string]any{"ok": true})
}

// deleteAllArchived 批量删除所有归档会话，只删 Archived=true 的，不影响活跃会话。
func (a *App) deleteAllArchived(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	deleted := 0
	failed := 0
	var failedIDs []string
	for id, s := range a.sessions {
		if !s.Archived {
			continue
		}
		s.Deleted = true
		for _, t := range s.Runs {
			if t.Status == "running" {
				if cancel := a.cancels[t.ID]; cancel != nil {
					cancel()
					delete(a.cancels, t.ID)
				}
			}
		}
		delete(a.sessions, id)
		if err := os.Remove(filepath.Join(a.dataPath, "session-"+id+".json")); err != nil && !os.IsNotExist(err) {
			failed++
			failedIDs = append(failedIDs, id)
			log.Printf("删除归档会话文件失败 %s: %v", id, err)
		} else {
			deleted++
		}
	}
	a.mu.Unlock()
	jsonOut(w, 200, map[string]any{"ok": true, "deleted": deleted, "failed": failed, "failedIds": failedIDs})
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
	perModel := map[string]TokenDay{}
	for _, c := range a.tokenCalls {
		m := perModel[c.Model]
		m.Prompt += c.Prompt
		m.Completion += c.Completion
		m.Total += c.Total
		m.Calls++
		perModel[c.Model] = m
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
		"perModel":      perModel,
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
