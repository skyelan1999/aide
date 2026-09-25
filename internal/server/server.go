package server

import (
	"aide/internal/server/tts"
	"bufio"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/subtle"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"embed"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"math/big"
	"net"
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
	BaseURL               string                 `json:"baseURL"`
	Model                 string                 `json:"model"`
	APIKey                string                 `json:"apiKey,omitempty"`
	Models                []ModelRef             `json:"models,omitempty"`
	ActiveModel           string                 `json:"activeModel,omitempty"`
	SandboxMode           string                 `json:"sandboxMode,omitempty"`   // read-only | workspace-write | danger-full-access
	ToolMaxRounds         int                    `json:"toolMaxRounds,omitempty"` // 工具循环最大轮次，默认 60
	ShellTimeout          int                    `json:"shellTimeout,omitempty"`  // run_shell 超时秒数，默认 60，最大 300
	PersonaEnabled        bool                   `json:"personaEnabled,omitempty"`
	PersonaCipher         string                 `json:"personaCipher,omitempty"`         // 兼容旧字段：单人格时代的性格密文
	ActivePersona         string                 `json:"activePersona,omitempty"`         // 当前活动人格 id（aide | xiaomi），默认 aide
	PersonaCiphers        map[string]string      `json:"personaCiphers,omitempty"`        // 每人格自定义性格密文（personaID -> AES-256-GCM base64）
	Personalities         map[string]Personality `json:"personalities,omitempty"`         // 可演化性格（aide/xiaomi），明文
	DisabledTools         []string               `json:"disabledTools,omitempty"`         // 被禁用的工具名列表
	ReasoningEffort       string                 `json:"reasoningEffort,omitempty"`       // 推理强度：auto/off/low/medium/high
	VoiceAssistantName    string                 `json:"voiceAssistantName,omitempty"`    // 语音小秘名字，默认"小秘"
	VoiceReplyEnabled     bool                   `json:"voiceReplyEnabled,omitempty"`     // 双向语音：语音回复模式
	VoiceReplyGender      string                 `json:"voiceReplyGender,omitempty"`      // 回复音色 male | female
	VoiceReplyVerbosity   string                 `json:"voiceReplyVerbosity,omitempty"`   // 语音回复详细度 brief(默认) | full
	VoiceInputDevice      string                 `json:"voiceInputDevice,omitempty"`      // 小秘语音输入设备 deviceId，空=系统默认
	VoiceDefaultSendMode  string                 `json:"voiceDefaultSendMode,omitempty"`  // #41：小秘默认发送调度 queue(默认,排队) | insert(插队)；模型拿不准时回落此值
	VoiceInsertSensitivity string                `json:"voiceInsertSensitivity,omitempty"` // #41：插队敏感度 conservative(默认,5s冷却) | normal(3s) | aggressive(1s)
	UserName              string                 `json:"userName,omitempty"`              // 账户用户名（锁屏欢迎语用，可空）
	UserPasswordHash      string                 `json:"userPasswordHash,omitempty"`      // 账户密码 SHA-256 哈希（不存明文；即小秘历史加密密钥）
	LockTimeoutSec        int                    `json:"lockTimeoutSec,omitempty"`        // 空闲锁屏秒数，0 = 不锁屏
	AccessibilityAutoRead bool                   `json:"accessibilityAutoRead,omitempty"` // 无障碍：输出完成后由小秘自动朗读讲解
	// ── 外部 AI 诊断接口（/api/debug）：默认关、只读、独立令牌、审计脱敏 ──
	DebugAccessEnabled  bool     `json:"debugAccessEnabled,omitempty"`  // 总开关，默认 false；关闭时 /api/debug/* 整体 404
	DebugTokenHash      string   `json:"debugTokenHash,omitempty"`      // 调试令牌 SHA-256 哈希（绝不存明文）
	DebugTokenCreatedAt string   `json:"debugTokenCreatedAt,omitempty"` // 调试令牌创建时间 RFC3339
	DebugTokenExpiresAt string   `json:"debugTokenExpiresAt,omitempty"` // 调试令牌过期时间 RFC3339
	DebugAllowOrigins   []string `json:"debugAllowOrigins,omitempty"`   // 浏览器来源白名单（curl 无 Origin 不受限）
	// ── TTS 自然度：可插拔引擎（edge-tts 神经音 + 浏览器 Web Speech 兜底）──
	TTSProvider       string  `json:"ttsProvider,omitempty"`       // auto | edge | webspeech
	TTSVoice          string  `json:"ttsVoice,omitempty"`          // edge 音色 id，空=按性别映射
	TTSEndpoint       string  `json:"ttsEndpoint,omitempty"`       // 预留：自定义 edge/云端端点
	TTSAPIKey         string  `json:"ttsAPIKey,omitempty"`         // 预留：云端引擎 key（edge 不需要，不回显）
	TTSRate           float64 `json:"ttsRate,omitempty"`           // 语速倍率 0.8-1.3，默认 1.0
	TTSExpressiveness float64 `json:"ttsExpressiveness,omitempty"` // 表现力 0-1，映射 edge styledegree
	TTSAzureKey       string  `json:"ttsAzureKey,omitempty"`       // Azure 官方 Speech key（可选，edge 后备；不回显）
	TTSAzureRegion    string  `json:"ttsAzureRegion,omitempty"`    // Azure region，如 eastasia
	// ── 少样本音色克隆（#36）：自托管克隆服务接入，仅小秘可选；未配置不影响 edge/webspeech ──
	CloneTTSBaseURL string `json:"cloneTTSBaseURL,omitempty"` // 克隆服务 BaseURL（如 http://127.0.0.1:9880），空=未配置
	CloneTTSAPIKey  string `json:"cloneTTSAPIKey,omitempty"`  // 克隆服务 Bearer token（可选，不回显）
	CloneVoiceID    string `json:"cloneVoiceID,omitempty"`    // 克隆出的音色 ID（创建音色后由克隆服务返回）
	CloneTTSBackend string `json:"cloneTTSBackend,omitempty"` // openai | gpt-sovits | indextts2 | cosyvoice2 | openvoice
	NextSessionSeq    int     `json:"nextSessionSeq,omitempty"`    // 下一个会话编号（单调递增，删除不复用，持久化）
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

// defaultSettings 返回当前版本的完整默认 Settings。
// New() 启动加载与 importConfigBackup 跨版本导入共用同一份默认底，
// 保证"旧备份缺失字段自动补当前默认"与"首次安装默认"完全一致。
// 注意：敏感字段（APIKey/TTSAPIKey/UserPasswordHash/PersonaCiphers/DebugTokenHash）
// 一律留空，由环境变量、持久化文件或用户显式导入决定，绝不在此处预置。
func defaultSettings() Settings {
	return Settings{
		BaseURL:            "https://api.deepseek.com",
		SandboxMode:        "workspace-write",
		ToolMaxRounds:      60, // 工具循环最大轮次
		ShellTimeout:       60, // run_shell 超时秒数（上限 300）
		LockTimeoutSec:     0,  // 0 = 不锁屏
		ReasoningEffort:    "auto",
		VoiceAssistantName: "小秘",
		VoiceReplyGender:   "female",
		ActivePersona:      personaAide,
		TTSProvider:        "auto",
		TTSRate:            1.0,
		DebugAccessEnabled: false,
	}
}

// validateSettings 对从外部（备份）读入的关键数值/枚举做合法性校验，非法值回落当前版本默认。
// 缺失字段补默认已由"默认底 + Unmarshal 覆盖"完成；这里只兜底"显式给出但越界/非法"的值。
func validateSettings(s *Settings) {
	if s.ToolMaxRounds <= 0 || s.ToolMaxRounds > 200 {
		s.ToolMaxRounds = 60
	}
	if s.ShellTimeout <= 0 || s.ShellTimeout > 300 {
		s.ShellTimeout = 60
	}
	if s.LockTimeoutSec < 0 {
		s.LockTimeoutSec = 0
	}
	switch s.SandboxMode {
	case "read-only", "workspace-write", "danger-full-access":
	default:
		s.SandboxMode = "workspace-write"
	}
	// #41：小秘发送调度枚举合法性回退（非法值一律保守排队）
	switch s.VoiceDefaultSendMode {
	case "insert":
	default:
		s.VoiceDefaultSendMode = "queue"
	}
	switch s.VoiceInsertSensitivity {
	case "normal", "aggressive":
	default:
		s.VoiceInsertSensitivity = "conservative"
	}
}

// normalizeLoadedSettings 把从磁盘或备份读入的 Settings 收敛到运行态一致：
// 旧格式迁移 → 文案/人格默认 → 合法性回退 → 模型列表归一化 → ActiveModel/Model 同步。
// New()（启动加载）与 importConfigBackup（导入）共用，
// 确保"导入后无需重启即生效"且与"重启后状态"完全一致。
func normalizeLoadedSettings(s *Settings) error {
	// 旧格式迁移：单 model 字段 → 模型列表 + 当前模型（FR-67 / D1）
	if len(s.Models) == 0 && s.Model != "" {
		s.Models = []ModelRef{{ID: s.Model, Name: s.Model, ContextWindow: defaultContextWindow}}
		s.ActiveModel = s.Model
	}
	if s.ActiveModel == "" {
		s.ActiveModel = s.Model
	}
	if s.VoiceAssistantName == "" {
		s.VoiceAssistantName = "小秘"
	}
	// 兼容旧版单人格：把旧 PersonaCipher 迁移为 aide 人格的自定义性格密文
	if s.PersonaCipher != "" && s.PersonaCiphers == nil {
		s.PersonaCiphers = map[string]string{personaAide: s.PersonaCipher}
	}
	if s.VoiceReplyGender == "" {
		s.VoiceReplyGender = "female"
	}
	// 关键数值/枚举合法性回退（非法值回落当前版本默认）
	validateSettings(s)
	// 模型列表归一化（含每模型上下文窗口 1024–2097152 校验、0→默认 65536）
	if s.Models != nil {
		normalized, err := normalizeModels(s.Models)
		if err != nil {
			return err
		}
		s.Models = normalized
	}
	if s.ActiveModel == "" && len(s.Models) > 0 {
		s.ActiveModel = s.Models[0].ID
		s.Model = s.ActiveModel
	}
	return nil
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
	ID                  string    `json:"id"`
	Title               string    `json:"title"`
	Created             string    `json:"created"`
	Messages            []Message `json:"messages"`
	Runs                []*Task   `json:"runs"`
	Compact             string    `json:"compact,omitempty"`           // 压缩摘要（compaction）
	CompactedMessages   int       `json:"compactedMessages,omitempty"` // 已折叠消息数
	CompactedAt         string    `json:"compactedAt,omitempty"`
	Pinned              bool      `json:"pinned,omitempty"`              // 置顶：列表排序优先（活动排序不会把置顶顶下去）
	Archived            bool      `json:"archived,omitempty"`            // 归档：默认列表隐藏
	Deleted             bool      `json:"deleted,omitempty"`             // 删除墓碑：save/加载跳过，防写盘复活
	Updated             string    `json:"updated,omitempty"`             // 最近活动时间：完成/跟进按时间置顶
	Checked             bool      `json:"checked,omitempty"`             // 已完成高亮（蓝点+加粗）是否已被用户查看；新完成时复位
	ParentID            string    `json:"parentId,omitempty"`            // 子会话：指向主会话 ID
	AutoArchived        bool      `json:"autoArchived,omitempty"`        // 子会话完成后自动归档
	Number              int       `json:"number,omitempty"`              // 会话编号 #N：单调递增、删除不复用
	Kind                string    `json:"kind,omitempty"`                // 空=普通会话；"assistant"=小秘系统会话（永久置顶、密码进入）
	FollowedByAssistant bool      `json:"followedByAssistant,omitempty"` // 小秘已标记跟进：该会话有更新时提醒
	FollowNote          string    `json:"followNote,omitempty"`          // 小秘跟进备注
}

// assistantSessionKind 小秘系统会话的 Kind 标记。全应用恰好一个，永久置顶、不可归档/删除。
const assistantSessionKind = "assistant"

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
	daemons                   *DaemonManager // 协议 v1.2：常驻守护插件管理器
	wsConfigPath              string
	wsSecretsPath             string
	wsConfig                  WorkspaceConfig
	wsSecrets                 workspaceSecrets
	vault                     *SecretVault // 统一加密凭证保险库（#38）
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
	liveBroker                *StreamBroker                            // #35：aide 主会话实时输出 → 小秘拉取
	liveTaskMu                sync.Mutex
	liveTaskSess              map[string]string    // #35：taskID → sessionID（把 SSE 增量桥到会话维度）
	personaKey                string               // 内存中的性格解密密码（= 账户密码），不持久化
	personaCustom             map[string]string    // 解锁后：personaID -> 解密出的自定义性格明文
	voiceAgent                *VoiceAgent          // 语音小秘 agent（记忆+历史）
	personalityState          personalityStateFile // 性格演化触发计数/回滚历史（#34，持久化 config/personality-state.json）
	bgCtx                     context.Context
	bgCancel                  context.CancelFunc
	bgWg                      sync.WaitGroup   // fire-and-forget 后台 goroutine（标题总结等）追踪，Close 时等待
	webAuthn                  *webAuthnManager // Touch ID / WebAuthn 解锁管理器
	colloqCache               *colloquialCache // 口语化朗读稿 LRU 缓存
	// ── 外部 AI 诊断接口（/api/debug）──
	startedAt      time.Time      // 进程启动时间（uptime 来源）
	refPath        string         // 参考/context 目录宿主路径（挂载摘要用）
	routes         *http.ServeMux // 主路由 mux（debug 鉴权后分发复用）
	debugMu        sync.Mutex     // 保护 errorRing / providerHealth / 审计写
	errorRing      []recentError  // 最近错误环形缓冲（容量 debugErrorRingCap）
	providerHealth providerHealth // 最近一次 Provider 连通性探测缓存
	// ── edge-tts 健康探测缓存（#42）──
	edgeMu        sync.Mutex
	edgeHealth    edgeHealthState
	ttsLastEngine string // 最近一次后端合成实际命中的引擎（edge/azure/unavailable）
	// ── 小秘系统会话密码门（#30）：内存解锁态，锁屏后失效，不持久化 ──
	assistantUnlockMu sync.Mutex
	assistantUnlocked map[string]bool
	// ── 数据完整性（启动校验 + 周期巡检结果）──
	integrityMu  sync.Mutex
	integrityRep IntegrityReport
}

// setIntegrity 记录最近一次完整性校验/自愈报告（healthz 与 /api/debug 读取）。
func (a *App) setIntegrity(rep IntegrityReport) {
	a.integrityMu.Lock()
	a.integrityRep = rep
	a.integrityMu.Unlock()
}

// integrityStatus 返回最近一次完整性状态（ok/degraded/corrupted）。
func (a *App) integrityStatus() IntegrityStatus {
	a.integrityMu.Lock()
	defer a.integrityMu.Unlock()
	if a.integrityRep.Status == "" {
		return IntegrityOK
	}
	return a.integrityRep.Status
}

// edgeHealthState 最近一次 edge-tts 可用性探测/合成结果缓存。TTL 内不重探，过期后下次合成或 /api/config 触发轻量探测。
type edgeHealthState struct {
	available bool
	lastError string
	checkedAt time.Time
}

const edgeHealthTTL = 60 * time.Second

// updateEdgeHealth 记录一次 edge 合成/探测结果。
func (a *App) updateEdgeHealth(available bool, err error) {
	a.edgeMu.Lock()
	defer a.edgeMu.Unlock()
	a.edgeHealth.available = available
	if err != nil {
		a.edgeHealth.lastError = err.Error()
	} else {
		a.edgeHealth.lastError = ""
	}
	a.edgeHealth.checkedAt = time.Now().UTC()
}

// edgeHealthSnapshot 返回当前 edge 健康缓存（available / lastError / checkedAt）。
func (a *App) edgeHealthSnapshot() (bool, string, time.Time) {
	a.edgeMu.Lock()
	defer a.edgeMu.Unlock()
	return a.edgeHealth.available, a.edgeHealth.lastError, a.edgeHealth.checkedAt
}

// setTTSEngine 记录最近一次后端合成实际命中的引擎。
func (a *App) setTTSEngine(name string) {
	a.edgeMu.Lock()
	defer a.edgeMu.Unlock()
	a.ttsLastEngine = name
}

// ttsEngineSnapshot 返回最近一次合成命中的引擎名。
func (a *App) ttsEngineSnapshot() string {
	a.edgeMu.Lock()
	defer a.edgeMu.Unlock()
	return a.ttsLastEngine
}

// probeEdgeAsync 若缓存过期则后台轻量探测 edge（不阻塞调用方；用户选浏览器合成时跳过）。
func (a *App) probeEdgeAsync() {
	a.edgeMu.Lock()
	fresh := !a.edgeHealth.checkedAt.IsZero() && time.Since(a.edgeHealth.checkedAt) < edgeHealthTTL
	a.edgeMu.Unlock()
	if fresh {
		return
	}
	a.mu.Lock()
	provider := a.settings.TTSProvider
	cfg := tts.Config{Provider: provider, Voice: a.settings.TTSVoice, Endpoint: a.settings.TTSEndpoint, Gender: a.settings.VoiceReplyGender}
	a.mu.Unlock()
	if strings.ToLower(strings.TrimSpace(provider)) == "webspeech" {
		return // 用户明确选浏览器合成，不探测 edge
	}
	a.bgWg.Add(1)
	go func() {
		defer a.bgWg.Done()
		ctx, cancel := context.WithTimeout(a.bgCtx, 15*time.Second)
		defer cancel()
		err := tts.ProbeEdge(ctx, cfg)
		a.updateEdgeHealth(err == nil, err)
	}()
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
	// 数据分层：建立分层目录骨架，并把旧平铺 /data 一次性迁移到分层布局（幂等、复制→校验→隔离，可回滚）。
	if err := EnsureDirs(data); err != nil {
		return nil, err
	}
	if err := MigrateFlatToLayered(data); err != nil {
		return nil, fmt.Errorf("数据分层迁移失败: %w", err)
	}
	// TLS 证书目录对齐：旧 data/tls/ → data/certs/（#31 规范，见 migrateLegacyTLSDir）。
	if err := migrateLegacyTLSDir(data); err != nil {
		log.Printf("迁移旧 tls/ 证书到 certs/ 失败（继续，将在 certs/ 重新生成）: %v", err)
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
	a := &App{workspace: w, reference: r, workPath: work, dataPath: data, refPath: reference, sessions: map[string]*Session{}, cancels: map[string]context.CancelFunc{}, commands: make(chan struct{}, 4), compactingSessions: map[string]bool{}, wsRoots: map[string]*os.Root{defaultWorkspaceID: w}, eventSubs: map[string]map[chan streamEvent]struct{}{}, liveBroker: NewStreamBroker(), liveTaskSess: map[string]string{}}
	a.startedAt = time.Now().UTC()
	a.bgCtx, a.bgCancel = context.WithCancel(context.Background())
	// 完整性：首次生成程序基线（已存在则跳过），启动校验并自愈，结果供 healthz 上报；后台周期巡检。
	_ = BuildBaseline(data)
	a.setIntegrity(SelfHeal(data, VerifyIntegrity(data)))
	go RunPeriodicIntegrity(a.bgCtx.Done(), data, 5*time.Minute, a.setIntegrity)
	b, err := os.ReadFile(AccessTokenPath(data))
	if errors.Is(err, os.ErrNotExist) {
		b = []byte(newID() + newID())
		err = os.WriteFile(AccessTokenPath(data), b, 0600)
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
	// 以当前版本默认 Settings 打底，再叠加环境变量与磁盘持久化值（文件 > 环境 > 默认）。
	// 旧版 settings.json 缺失的新字段由此自动获得当前版本默认值，与配置导入共用同一份默认底。
	a.settings = defaultSettings()
	if v := os.Getenv("AI_BASE_URL"); v != "" {
		a.settings.BaseURL = v
	}
	if v := os.Getenv("AI_MODEL"); v != "" {
		a.settings.Model = v
	}
	if v := os.Getenv("AI_API_KEY"); v != "" {
		a.settings.APIKey = v
	}
	if b, err := os.ReadFile(SettingsPath(data)); err == nil {
		if err = json.Unmarshal(b, &a.settings); err != nil {
			a.Close()
			return nil, err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		a.Close()
		return nil, err
	}
	// 旧格式迁移 + 文案/人格默认 + 合法性回退 + 模型归一化（与配置导入共用同一函数，保证状态一致）
	if err := normalizeLoadedSettings(&a.settings); err != nil {
		a.Close()
		return nil, fmt.Errorf("settings.json: %w", err)
	}
	// #34：加载性格演化触发计数/回滚历史（config/personality-state.json），重启不清零。
	a.loadPersonalityState()
	// KDF：确保本机固定的 Argon2id salt（data/kdf-salt.bin, 0600）存在并加载，
	// 之后 deriveKey() 一律走 Argon2id；旧 SHA-256 密文在登录时经 re-wrap 平滑迁移。
	if err := ensureKdfSalt(data); err != nil {
		a.Close()
		return nil, fmt.Errorf("kdf-salt: %w", err)
	}
	// 统一加密凭证保险库（#38）：/data/secrets/vault.enc（目录 0700、文件 0600）。
	// 启动时仅载入密文信封（保持锁定）；主密钥在用户会话中解锁时由账户密码派生。
	if a.vault, err = newSecretVault(data); err != nil {
		a.Close()
		return nil, fmt.Errorf("secret-vault: %w", err)
	}
	if err := a.vault.Load(); err != nil {
		a.Close()
		return nil, fmt.Errorf("secret-vault load: %w", err)
	}
	a.voiceAgent = newVoiceAgent(data)
	a.voiceAgent.attachBroker(a.liveBroker) // #35：小秘据此拉取 aide 实时输出
	a.colloqCache = newColloquialCache()
	a.webAuthn = newWebAuthnManager(data) // Touch ID / WebAuthn 解锁
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
	// 协议 v1.2：初始化常驻守护管理器，并按注册表期望状态自动恢复 enabled 的 daemon 插件
	a.daemons = newDaemonManager(a)
	a.daemons.restore()
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
	if b, err := os.ReadFile(TokenStatsPath(data)); err == nil {
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
	if b, err := os.ReadFile(TokenPricingPath(data)); err == nil {
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
	var entries []string
	for _, d := range sessionBucketDirs(data) {
		got, gerr := filepath.Glob(filepath.Join(d, "session-*.json"))
		if gerr != nil {
			a.Close()
			return nil, gerr
		}
		entries = append(entries, got...)
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
	// #30：启动幂等确保恰好一个小秘系统会话（永久置顶、密码进入）。
	a.ensureAssistantSession()
	return a, nil
}
func (a *App) Close() {
	if a.bgCancel != nil {
		a.bgCancel() // 通知后台 goroutine 停止（中断其 HTTP 请求）
	}
	a.bgWg.Wait() // 等后台写盘结束，避免与临时目录清理竞争
	if a.daemons != nil {
		a.daemons.StopAll() // 协议 v1.2：终止常驻插件子进程并释放端口
	}
	a.workspace.Close()
	a.reference.Close()
	if a.localRoot != nil {
		a.localRoot.Close()
	}
	for _, r := range a.retiredRoots {
		r.Close()
	}
}

// background 启动可追踪的 fire-and-forget 后台 goroutine；Close 会先取消 bgCtx 再等待其结束。
func (a *App) background(fn func()) {
	a.bgWg.Add(1)
	go func() { defer a.bgWg.Done(); fn() }()
}
func (a *App) save(s *Session) error {
	if s.Deleted {
		return nil // 已删除会话不再落盘（运行中任务取消后的收尾保存同样跳过）
	}
	return atomicJSON(SessionPath(a.dataPath, s.ID, "active"), s)
}

// assignSessionNumber 给会话分配下一个编号 #N：单调递增、删除不复用，分配后立即持久化 settings。
// 调用方必须已持有 a.mu（与 createSession / 子会话创建 / 启动幂等同一把锁）。
func (a *App) assignSessionNumber(s *Session) {
	s.Number = a.settings.NextSessionSeq
	if s.Number <= 0 {
		s.Number = 1
	}
	a.settings.NextSessionSeq = s.Number + 1
	if err := atomicJSON(filepath.Join(a.dataPath, "settings.json"), a.settings); err != nil {
		log.Printf("持久化 nextSessionSeq 失败: %v", err)
	}
}

// assistantNameLocked 返回当前小秘名字（空回退"小秘"）。调用方持 a.mu。
func (a *App) assistantNameLocked() string {
	if n := strings.TrimSpace(a.settings.VoiceAssistantName); n != "" {
		return n
	}
	return "小秘"
}

// syncAssistantTitleLocked 让小秘系统会话标题跟随 VoiceAssistantName。调用方持 a.mu。
func (a *App) syncAssistantTitleLocked(s *Session) {
	name := a.assistantNameLocked()
	if s.Title != name {
		s.Title = name
		_ = a.save(s)
	}
}

// ensureAssistantSession 启动幂等：全应用恰好一个 Kind=assistant 的永久置顶会话。
// 缺失则创建（编号取下一个 NextSessionSeq）；已存在则校正标题跟随名字、强制置顶。
// 无并发（New() 单线程阶段调用）；内部自取 a.mu。
func (a *App) ensureAssistantSession() {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, s := range a.sessions {
		if s.Kind == assistantSessionKind {
			s.Pinned = true // 永久置顶
			syncName := s.Title
			if want := a.assistantNameLocked(); syncName != want {
				s.Title = want
				_ = a.save(s)
			}
			return
		}
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	s := &Session{
		ID: newID(), Title: a.assistantNameLocked(),
		Created: now, Updated: now,
		Messages: []Message{}, Runs: []*Task{},
		Kind: assistantSessionKind, Pinned: true,
	}
	a.assignSessionNumber(s)
	a.sessions[s.ID] = s
	if err := a.save(s); err != nil {
		log.Printf("创建小秘系统会话失败: %v", err)
	}
}

// assistantUnlockMark 记录小秘会话已解锁（内存，锁屏后由 lockScreen 触发 clear 失效）。
func (a *App) markAssistantUnlocked(id string) {
	a.assistantUnlockMu.Lock()
	defer a.assistantUnlockMu.Unlock()
	if a.assistantUnlocked == nil {
		a.assistantUnlocked = map[string]bool{}
	}
	a.assistantUnlocked[id] = true
}

// clearAssistantUnlock 锁屏/登出时清空小秘会话内存解锁态。
func (a *App) clearAssistantUnlock() {
	a.assistantUnlockMu.Lock()
	defer a.assistantUnlockMu.Unlock()
	a.assistantUnlocked = map[string]bool{}
}

func (a *App) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		jsonOut(w, 200, map[string]any{"status": "ok", "service": "aide", "integrity": a.integrityStatus()})
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
	mux.HandleFunc("GET /api/plugins/daemons", a.listDaemons)
	mux.HandleFunc("POST /api/plugins/daemons/{id}/start", a.daemonStartHandler)
	mux.HandleFunc("POST /api/plugins/daemons/{id}/stop", a.daemonStopHandler)
	mux.HandleFunc("POST /api/plugins/daemons/{id}/restart", a.daemonRestartHandler)
	mux.HandleFunc("GET /api/plugins/daemons/{id}/events", a.daemonEventsHandler)
	mux.HandleFunc("GET /api/workspace-config", a.getWorkspaceConfig)
	mux.HandleFunc("GET /api/workspace/secrets", a.listWorkspaceSecrets)
	mux.HandleFunc("DELETE /api/workspace/secrets/{id}", a.deleteWorkspaceSecret)
	mux.HandleFunc("POST /api/workspace/unlock-vault", a.unlockWorkspaceVault)
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
	mux.HandleFunc("POST /api/personality/rollback", a.personalityRollback)
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
	mux.HandleFunc("POST /api/config/export", a.exportConfigBackup)
	mux.HandleFunc("GET /api/factory-reset/preview", a.previewFactoryReset)
	mux.HandleFunc("POST /api/factory-reset", a.postFactoryReset)
	mux.HandleFunc("POST /api/config/import", a.importConfigBackup)
	mux.HandleFunc("DELETE /api/sessions/{id}", a.deleteSession)
	mux.HandleFunc("DELETE /api/sessions/archived/all", a.deleteAllArchived)
	mux.HandleFunc("PATCH /api/sessions/{id}", a.patchSession)
	mux.HandleFunc("POST /api/sessions/{id}/runs", a.startTask)
	mux.HandleFunc("POST /api/sessions/{id}/unlock-assistant", a.unlockAssistantSession) // #30 小秘会话密码门
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
	mux.HandleFunc("POST /api/voice-narrate", a.voiceNarrate)
	mux.HandleFunc("POST /api/tts/synthesize", a.ttsSynthesize)
	mux.HandleFunc("POST /api/tts/colloquialize", a.ttsColloquialize)
	// ── 少样本音色克隆 + 性格推断（#36）──
	mux.HandleFunc("POST /api/voice-sample/upload", a.voiceSampleUpload)
	mux.HandleFunc("GET /api/voice-samples", a.voiceSamplesList)
	mux.HandleFunc("DELETE /api/voice-sample/{id}", a.voiceSampleDelete)
	mux.HandleFunc("GET /api/voice-sample/{id}/audio", a.voiceSampleAudio)
	mux.HandleFunc("POST /api/voice-samples/wipe", a.voiceSamplesWipe)
	mux.HandleFunc("POST /api/voice-clone/create", a.voiceCloneCreate)
	mux.HandleFunc("POST /api/voice-personality/infer", a.voicePersonalityInfer)
	mux.HandleFunc("POST /api/voice-personality/adopt", a.voicePersonalityAdopt)
	mux.HandleFunc("GET /api/voice-history", a.voiceHistory)
	mux.HandleFunc("DELETE /api/voice-history", a.voiceHistoryClear)
	mux.HandleFunc("POST /api/voice-history/enable", a.voiceHistoryEnable)
	mux.HandleFunc("POST /api/voice-history/unlock", a.voiceHistoryUnlock)
	mux.HandleFunc("POST /api/voice-history/lock", a.voiceHistoryLock)
	mux.HandleFunc("POST /api/voice-history/change-password", a.voiceHistoryChangePassword)
	mux.HandleFunc("POST /api/voice-history/disable", a.voiceHistoryDisable)
	mux.HandleFunc("POST /api/account/verify-password", a.accountVerifyPassword)
	// ── 锁屏集群权威状态（后端持久化，选举/veil/看门狗以后端为准）──
	mux.HandleFunc("GET /api/lock-state", a.lockStateGet)
	mux.HandleFunc("PUT /api/lock-state", a.lockStatePut)
	mux.HandleFunc("POST /api/webauthn/register/start", a.webAuthnRegisterStart)
	mux.HandleFunc("POST /api/webauthn/register/finish", a.webAuthnRegisterFinish)
	mux.HandleFunc("GET /api/webauthn/credentials", a.webAuthnListCredentials)
	mux.HandleFunc("PUT /api/webauthn/credentials/{id}", a.webAuthnRenameCredential)
	mux.HandleFunc("DELETE /api/webauthn/credentials/{id}", a.webAuthnDeleteCredential)
	mux.HandleFunc("POST /api/webauthn/assertion/start", a.webAuthnAssertionStart)
	mux.HandleFunc("POST /api/webauthn/assertion/finish", a.webAuthnAssertionFinish)
	// ── 外部 AI 诊断接口（独立鉴权，见 serveDebug）──
	mux.HandleFunc("GET /api/debug/overview", a.debugOverview)
	mux.HandleFunc("GET /api/debug/sessions", a.debugSessions)
	mux.HandleFunc("GET /api/debug/sessions/{id}/runs/{run}", a.debugRunDetail)
	mux.HandleFunc("GET /api/debug/sessions/{id}/runs/{run}/events", a.runEvents)
	mux.HandleFunc("GET /api/debug/errors", a.debugErrors)
	mux.HandleFunc("GET /api/debug/stats", a.tokenStatsHandler)
	mux.HandleFunc("POST /api/debug/actions/ping-provider", a.debugPingProvider)
	mux.HandleFunc("POST /api/debug/actions/diagnostic-bundle", a.debugDiagnosticBundle)
	mux.HandleFunc("GET /api/debug/audit", a.debugAudit)
	mux.HandleFunc("GET /api/debug/audit/export", a.debugAuditExport)
	mux.HandleFunc("POST /api/debug/admin/token", a.debugAdminToken)
	mux.HandleFunc("POST /api/debug/admin/revoke", a.debugAdminRevoke)
	mux.HandleFunc("POST /api/debug/admin/toggle", a.debugAdminToggle)
	a.routes = mux
	web, _ := fs.Sub(assets, "web")
	fileServer := http.FileServer(http.FS(web))
	mux.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// ES 模块（pdf.js 的 .mjs）必须以 JS MIME 提供，否则动态 import() / module worker 被浏览器严格 MIME 检查拦截
		if strings.HasSuffix(r.URL.Path, ".mjs") {
			w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		}
		fileServer.ServeHTTP(w, r)
	}))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 鉴权现状：全程 Bearer Token（Authorization 头 / 受信路径上的 ?access_token=），不使用 Cookie。
		// 未来若引入 Cookie，必须同时设置 Secure、HttpOnly、SameSite=Lax，且仅在 HTTPS 连接下发。
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		if strings.HasPrefix(r.URL.Path, "/vendor/drawio/") {
			w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self' 'unsafe-eval'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'; frame-ancestors 'self'; base-uri 'none'")
		} else {
			w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; worker-src 'self'; style-src 'self'; connect-src 'self'; img-src 'self' data:; frame-ancestors 'none'; base-uri 'none'")
		}
		// HSTS：仅在非回环的真实 HTTPS 站点下发；localhost/127.0.0.1 不加，避免浏览器把本机
		// http://localhost 永久强制改写。max-age 取短值 300s，本地自签场景可快速回收。
		if hsts := strictTransportSecurity(r); hsts != "" {
			w.Header().Set("Strict-Transport-Security", hsts)
		}
		w.Header().Set("Cache-Control", "no-store")
		if strings.HasPrefix(r.URL.Path, "/api/debug/") {
			a.serveDebug(w, r) // 独立令牌/审计/脱敏；不走下方普通 access-token 校验
			return
		}
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
	// edge 健康：先快照（edgeMu），再按需后台探测，最后才拿 a.mu——避免 edgeMu/a.mu 反向加锁死锁。
	edgeAvail, edgeErr, _ := a.edgeHealthSnapshot()
	a.probeEdgeAsync()
	// 本地离线 sherpa-onnx（#44）：探测二进制+已安装模型，回显给前端（文件系统 stat/readdir，极快）。
	sherpaBinOK, sherpaVoices := tts.SherpaModelInfo("", "")
	sherpaInstalled := sherpaBinOK && len(sherpaVoices) > 0
	a.mu.Lock()
	defer a.mu.Unlock()
	jsonOut(w, 200, map[string]any{"name": "aide", "version": a.version, "buildVersion": a.buildVersion, "buildCommit": a.buildCommit, "revision": a.buildCommit, "baseURL": a.settings.BaseURL, "model": a.settings.Model, "configured": a.settings.Model != "" && a.settings.BaseURL != "", "hasKey": a.settings.APIKey != "", "models": a.settings.Models, "activeModel": a.settings.ActiveModel, "workspace": "/workspace", "context": "/context", "hostLocal": a.hostLocal, "workspaceDisplay": a.workspaceDisplay, "runtime": "Go · Python · Node.js · Git", "disabledTools": a.settings.DisabledTools, "reasoningEffort": a.settings.ReasoningEffort, "voiceAssistantName": a.settings.VoiceAssistantName, "voiceReplyEnabled": a.settings.VoiceReplyEnabled, "voiceReplyGender": voiceReplyGender(a.settings.VoiceReplyGender), "voiceReplyVerbosity": a.settings.VoiceReplyVerbosity, "voiceInputDevice": a.settings.VoiceInputDevice, "accessibilityAutoRead": a.settings.AccessibilityAutoRead, "debugAccessEnabled": a.settings.DebugAccessEnabled, "hasDebugToken": a.settings.DebugTokenHash != "", "debugAllowOrigins": a.settings.DebugAllowOrigins, "ttsProvider": ttsProviderName(a.settings.TTSProvider), "ttsVoice": a.settings.TTSVoice, "ttsRate": ttsRateVal(a.settings.TTSRate), "ttsExpressiveness": a.settings.TTSExpressiveness, "hasTTSKey": a.settings.TTSAPIKey != "", "ttsVoices": tts.ChineseVoices(), "edgeAvailable": edgeAvail, "edgeLastError": edgeErr, "azureConfigured": a.settings.TTSAzureKey != "", "cloneConfigured": a.settings.CloneTTSBaseURL != "", "cloneBaseURL": a.settings.CloneTTSBaseURL, "cloneVoiceID": a.settings.CloneVoiceID, "cloneBackend": cloneBackendName(a.settings.CloneTTSBackend), "hasCloneKey": a.settings.CloneTTSAPIKey != "", "sherpaAvailable": sherpaInstalled, "sherpaBinOK": sherpaBinOK, "sherpaVoices": sherpaVoices, "currentTTSEngine": a.ttsEngineSnapshot(), "userName": a.settings.UserName, "lockTimeoutSec": a.settings.LockTimeoutSec, "hasPassword": a.settings.UserPasswordHash != "", "webAuthnReady": a.webAuthn.enabled(), "hasPlatformCredential": a.webAuthn.hasPlatformCredential(), "activePersona": a.activePersonaID(), "personas": a.personaListOut(), "workflow": []string{"plan", "propose", "review"}})
}
func (a *App) updateSettings(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Settings
		ClearKey              bool   `json:"clearKey"`
		VoiceReplyEnabled     *bool  `json:"voiceReplyEnabled,omitempty"`
		AccessibilityAutoRead *bool  `json:"accessibilityAutoRead,omitempty"`
		VoiceReplyGender      string `json:"voiceReplyGender,omitempty"`
		// 账户：外层同名字段覆盖内嵌 Settings（与 VoiceReplyEnabled 同模式），以便区分"未传"与"传空/0"
		UserName         string  `json:"userName,omitempty"`
		LockTimeoutSec   *int    `json:"lockTimeoutSec,omitempty"`
		VoiceInputDevice *string `json:"voiceInputDevice,omitempty"`
		OldPassword      string  `json:"oldPassword,omitempty"`
		NewPassword      string  `json:"newPassword,omitempty"`
		// 外部 AI 调试接口：总开关用指针区分"未传/传 false"；白名单 nil=保留
		DebugAccessEnabled *bool    `json:"debugAccessEnabled,omitempty"`
		DebugAllowOrigins  []string `json:"debugAllowOrigins,omitempty"`
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
	if in.VoiceReplyVerbosity == "" {
		in.Settings.VoiceReplyVerbosity = a.settings.VoiceReplyVerbosity
	}
	if in.VoiceReplyGender == "" {
		in.Settings.VoiceReplyGender = a.settings.VoiceReplyGender
	} else {
		in.Settings.VoiceReplyGender = in.VoiceReplyGender
	}
	if in.AccessibilityAutoRead != nil {
		in.Settings.AccessibilityAutoRead = *in.AccessibilityAutoRead
	} else {
		in.Settings.AccessibilityAutoRead = a.settings.AccessibilityAutoRead
	}
	// 调试接口：令牌哈希只由 /api/debug/admin/* 管理，普通 PUT 永不覆盖；
	// 白名单未传则保留；总开关用指针判定。关闭时立即清空令牌哈希（无凭据残留）。
	in.Settings.DebugTokenHash = a.settings.DebugTokenHash
	in.Settings.DebugTokenCreatedAt = a.settings.DebugTokenCreatedAt
	in.Settings.DebugTokenExpiresAt = a.settings.DebugTokenExpiresAt
	if in.DebugAllowOrigins != nil {
		in.Settings.DebugAllowOrigins = in.DebugAllowOrigins
	} else {
		in.Settings.DebugAllowOrigins = a.settings.DebugAllowOrigins
	}
	if in.DebugAccessEnabled != nil {
		in.Settings.DebugAccessEnabled = *in.DebugAccessEnabled
	} else {
		in.Settings.DebugAccessEnabled = a.settings.DebugAccessEnabled
	}
	if !in.Settings.DebugAccessEnabled {
		in.Settings.DebugTokenHash = ""
		in.Settings.DebugTokenCreatedAt = ""
		in.Settings.DebugTokenExpiresAt = ""
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
	// 局部 PUT（仅改某一项）不携带以下字段，未传一律保留已存值，避免性格启用态/沙箱/轮次/设备被静默重置。
	in.Settings.Personalities = a.settings.Personalities
	in.Settings.PersonaCipher = a.settings.PersonaCipher
	if in.Settings.SandboxMode == "" {
		in.Settings.SandboxMode = a.settings.SandboxMode
	}
	if in.Settings.ToolMaxRounds == 0 {
		in.Settings.ToolMaxRounds = a.settings.ToolMaxRounds
	}
	if in.Settings.ShellTimeout == 0 {
		in.Settings.ShellTimeout = a.settings.ShellTimeout
	}
	if in.VoiceInputDevice != nil {
		in.Settings.VoiceInputDevice = *in.VoiceInputDevice // 显式传空=切回系统默认
	} else {
		in.Settings.VoiceInputDevice = a.settings.VoiceInputDevice
	}
	// TTS 引擎：未传（零值/空）保留已存值；TTSAPIKey 同 apiKey 模式，空且未 clear 时保留
	if in.Settings.TTSProvider == "" {
		in.Settings.TTSProvider = a.settings.TTSProvider
	}
	if in.Settings.TTSVoice == "" {
		in.Settings.TTSVoice = a.settings.TTSVoice
	}
	if in.Settings.TTSEndpoint == "" {
		in.Settings.TTSEndpoint = a.settings.TTSEndpoint
	}
	if in.Settings.TTSAPIKey == "" {
		in.Settings.TTSAPIKey = a.settings.TTSAPIKey
	}
	if in.Settings.TTSRate == 0 {
		in.Settings.TTSRate = a.settings.TTSRate
	}
	if in.Settings.TTSExpressiveness == 0 {
		in.Settings.TTSExpressiveness = a.settings.TTSExpressiveness
	}
	// Azure key 同 TTSAPIKey：空且未 clear 时保留已存值
	if in.Settings.TTSAzureKey == "" {
		in.Settings.TTSAzureKey = a.settings.TTSAzureKey
	}
	if in.Settings.TTSAzureRegion == "" {
		in.Settings.TTSAzureRegion = a.settings.TTSAzureRegion
	}
	// 克隆音色配置：空则保留已存值；APIKey 不回显，前端留空=不改动
	if in.Settings.CloneTTSBaseURL == "" {
		in.Settings.CloneTTSBaseURL = a.settings.CloneTTSBaseURL
	}
	if in.Settings.CloneTTSAPIKey == "" {
		in.Settings.CloneTTSAPIKey = a.settings.CloneTTSAPIKey
	}
	if in.Settings.CloneVoiceID == "" {
		in.Settings.CloneVoiceID = a.settings.CloneVoiceID
	}
	if in.Settings.CloneTTSBackend == "" {
		in.Settings.CloneTTSBackend = a.settings.CloneTTSBackend
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
	// 密码变更：账户密码即小秘历史加密密钥。旧密码已设置时必须验证；新密码同步重加密小秘历史。
	if in.NewPassword != "" {
		hasPw := a.settings.UserPasswordHash != ""
		if hasPw {
			valid, _ := VerifyPassword(in.OldPassword, a.settings.UserPasswordHash)
			if !valid {
				fail(w, 400, errors.New("原密码错误"))
				return
			}
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
		in.Settings.UserPasswordHash = mustHashPassword(in.NewPassword)
		// 凭证保险库（#38）：改密码时重加密。首次设置密码时 vault 为空（无密码不允许存凭据），
		// 直接以新密钥解锁；已有密码时用旧密钥解开、新密钥重封。ReWrap 显式传入旧密钥，不依赖当前解锁态。
		if a.vault != nil {
			newVaultKey := deriveKey(in.NewPassword)
			if len(a.vault.List()) > 0 && hasPw {
				_ = a.vault.ReWrap(deriveKey(in.OldPassword), newVaultKey)
			} else {
				a.vault.Unlock(newVaultKey)
			}
		}
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
	if err := atomicJSON(SettingsPath(a.dataPath), in.Settings); err != nil {
		fail(w, 500, err)
		return
	}
	a.settings = in.Settings
	// #30：VoiceAssistantName 变更后，小秘系统会话标题跟随
	for _, sess := range a.sessions {
		if sess.Kind == assistantSessionKind {
			a.syncAssistantTitleLocked(sess)
			break
		}
	}
	jsonOut(w, 200, map[string]bool{"ok": true})
}

// voiceReplyGender 归一化回复音色，非法值回落 female。
func voiceReplyGender(g string) string {
	if g == "male" || g == "female" {
		return g
	}
	return "female"
}

// ttsProviderName 归一化 TTS 引擎名，空=auto。
func ttsProviderName(p string) string {
	switch strings.ToLower(strings.TrimSpace(p)) {
	case "edge", "edge-tts", "edgetts":
		return "edge"
	case "sherpa", "local", "offline", "sherpa-onnx":
		return "sherpa"
	case "webspeech", "browser", "web":
		return "webspeech"
	case "clone", "voice-clone", "cloned":
		return "clone"
	default:
		return "auto"
	}
}

// ttsRateVal 归一化语速倍率，非法值回退 1.0。
func ttsRateVal(r float64) float64 {
	if r < 0.5 || r > 2.0 {
		return 1.0
	}
	return r
}

// cloneBackendName 归一化克隆后端标识，空=默认 openai。
func cloneBackendName(b string) string {
	switch strings.ToLower(strings.TrimSpace(b)) {
	case "gpt-sovits", "gpt_sovits", "gptsovits":
		return "gpt-sovits"
	case "indextts2", "index-tts2", "indextts":
		return "indextts2"
	case "cosyvoice2", "cosyvoice":
		return "cosyvoice2"
	case "openvoice":
		return "openvoice"
	case "", "openai":
		return "openai"
	default:
		return strings.ToLower(strings.TrimSpace(b))
	}
}

// voiceFilter 语音小秘：把浏览器 Web Speech API 的转写文本交给小秘 agent 分析决策。
// 小秘结合记忆与上下文判断 send（对 aide 的指令，直接发到当前会话）/ ignore（背景声）/ standby（与人闲聊退下）。
// 任何失败都降级为 send 原文，保证语音录入在模型不可用时仍可用。
func (a *App) voiceFilter(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Text    string `json:"text"`
		Context string `json:"context"`
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
		if va != nil {
			jsonOut(w, 200, va.recordFallback(text, "未配置模型，直接发送"))
		} else {
			jsonOut(w, 200, map[string]any{"action": "send", "text": text, "reason": "未配置模型，直接发送"})
		}
		return
	}
	// 历史加密且锁定（如容器重启后未解锁）：不持有明文、无法落盘记录，
	// 明确返回 locked 引导解锁，而非静默发送造成"以为记录了实则丢失"。
	if hst := va.encStatus(); func() bool { e, _ := hst["encrypted"].(bool); u, _ := hst["unlocked"].(bool); return e && !u }() {
		jsonOut(w, 200, map[string]any{"action": "locked", "text": "", "reason": "小秘对话历史已锁定，请在设置→语音小秘中点「解锁」输入密钥后再发言；解锁前内容不会被记录"})
		return
	}
	entry, err := va.analyze(r.Context(), cfg, text, in.Context)
	if err != nil {
		jsonOut(w, 200, va.recordFallback(text, "小秘分析失败，直接发送: "+err.Error()))
		return
	}
	if entry.Action == "send" && strings.TrimSpace(entry.Text) == "" {
		entry.Text = text
	}
	if entry.Action == "send" {
		// #34：小秘有效交互计数持久化（personality-state.json），达到阈值后台演化，用真实语音历史样本。
		a.mu.Lock()
		fire, trigger := a.onPersonalityInteractLocked(personaXiaomi)
		var sample string
		if fire {
			sample = a.personalitySampleLocked(personaXiaomi)
		}
		a.mu.Unlock()
		if fire {
			go a.runAutoEvolve(personaXiaomi, modeRefine, trigger, sample)
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
	var in struct {
		Password string `json:"password"`
	}
	if decode(w, r, &in) != nil {
		return
	}
	a.mu.Lock()
	va := a.voiceAgent
	a.mu.Unlock()
	if va == nil {
		fail(w, 500, errors.New("小秘未初始化"))
		return
	}
	if err := va.enable(in.Password); err != nil {
		fail(w, 400, err)
		return
	}
	jsonOut(w, 200, map[string]any{"ok": true, "encrypted": true, "unlocked": true})
}

func (a *App) voiceHistoryUnlock(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Password string `json:"password"`
	}
	if decode(w, r, &in) != nil {
		return
	}
	a.mu.Lock()
	va := a.voiceAgent
	a.mu.Unlock()
	if va == nil {
		fail(w, 500, errors.New("小秘未初始化"))
		return
	}
	if err := va.unlock(in.Password); err != nil {
		fail(w, 401, err)
		return
	}
	jsonOut(w, 200, map[string]any{"ok": true, "encrypted": true, "unlocked": true})
}

func (a *App) voiceHistoryLock(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	va := a.voiceAgent
	a.mu.Unlock()
	if va != nil {
		va.lock()
	}
	jsonOut(w, 200, map[string]any{"ok": true, "encrypted": true, "unlocked": false})
}

func (a *App) voiceHistoryChangePassword(w http.ResponseWriter, r *http.Request) {
	var in struct {
		OldPassword string `json:"oldPassword"`
		NewPassword string `json:"newPassword"`
	}
	if decode(w, r, &in) != nil {
		return
	}
	a.mu.Lock()
	va := a.voiceAgent
	a.mu.Unlock()
	if va == nil {
		fail(w, 500, errors.New("小秘未初始化"))
		return
	}
	if err := va.changePassword(in.OldPassword, in.NewPassword); err != nil {
		fail(w, 400, err)
		return
	}
	jsonOut(w, 200, map[string]any{"ok": true})
}

func (a *App) voiceHistoryDisable(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Password string `json:"password"`
	}
	if decode(w, r, &in) != nil {
		return
	}
	a.mu.Lock()
	va := a.voiceAgent
	a.mu.Unlock()
	if va == nil {
		fail(w, 500, errors.New("小秘未初始化"))
		return
	}
	if err := va.disable(in.Password); err != nil {
		fail(w, 400, err)
		return
	}
	jsonOut(w, 200, map[string]any{"ok": true, "encrypted": false, "unlocked": true})
}

// accountVerifyPassword 校验账户密码（解锁锁屏 / 小秘历史二次确认共用）。
// 支持 Argon2id(PHC) 与旧 64hex(SHA-256) 两种格式；命中旧格式且密码正确时，
// 自动升级为 Argon2id 并重加密所有已有密文（persona + 小秘历史），实现平滑迁移。
// 不返回任何敏感信息；未设置密码时一律拒绝。
func (a *App) accountVerifyPassword(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Password string `json:"password"`
	}
	if decode(w, r, &in) != nil {
		return
	}
	a.mu.Lock()
	hash := a.settings.UserPasswordHash
	a.mu.Unlock()
	if hash == "" {
		fail(w, 401, errors.New("密码错误"))
		return
	}
	valid, needsUpgrade := VerifyPassword(in.Password, hash)
	if !valid {
		fail(w, 401, errors.New("密码错误"))
		return
	}
	upgraded := false
	if needsUpgrade {
		upgraded = a.migratePasswordHash(in.Password)
	} else {
		a.unlockVault(in.Password) // 密码校验通过：解锁凭证保险库供 SSH 运行时使用
	}
	jsonOut(w, 200, map[string]any{"ok": true, "upgraded": upgraded})
}

// unlockAssistantSession 小秘系统会话密码门（#30）：进入 Kind=assistant 会话需账户密码授权。
// 复用 kdf.VerifyPassword（Argon2id，兼容旧 SHA-256 自动迁移）；校验通过记录内存解锁态（锁屏后失效）。
func (a *App) unlockAssistantSession(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Password string `json:"password"`
	}
	if decode(w, r, &in) != nil {
		return
	}
	a.mu.Lock()
	s := a.sessions[r.PathValue("id")]
	hash := a.settings.UserPasswordHash
	a.mu.Unlock()
	if s == nil {
		fail(w, 404, errors.New("会话不存在"))
		return
	}
	if s.Kind != assistantSessionKind {
		fail(w, 404, errors.New("不是小秘系统会话"))
		return
	}
	if hash == "" {
		log.Printf("小秘会话密码门拒绝：未设置账户密码 id=%s", s.ID)
		fail(w, 401, errors.New("未设置账户密码"))
		return
	}
	valid, needsUpgrade := VerifyPassword(in.Password, hash)
	if !valid {
		log.Printf("小秘会话密码门失败：密码错误 id=%s", s.ID) // 审计：失败不记录密码
		fail(w, 401, errors.New("密码错误"))
		return
	}
	if needsUpgrade {
		a.migratePasswordHash(in.Password) // 旧 SHA-256 命中：升级 Argon2id 并重加密
	}
	a.markAssistantUnlocked(s.ID)
	jsonOut(w, 200, map[string]any{"ok": true})
}

// migratePasswordHash 把旧 SHA-256 密码哈希升级为 Argon2id PHC，并用新密钥重加密所有密文。
// 调用方已确认密码正确（旧格式校验通过）。无密文时 re-wrap 自然跳过。返回是否成功落盘。
func (a *App) migratePasswordHash(password string) bool {
	newHash := mustHashPassword(password)
	if newHash == "" {
		return false
	}
	oldKey := DeriveAESKeyLegacy(password) // = 旧 SHA-256 派生密钥
	newKey := DeriveAESKey(password, kdfSalt)

	a.mu.Lock()
	defer a.mu.Unlock()
	// 1) 重加密人格自定义性格密文（旧密钥解 → 新密钥封）
	for id, cipher := range a.settings.PersonaCiphers {
		if cipher == "" {
			continue
		}
		if plain, err := decryptWithKey(oldKey, cipher); err == nil {
			if nc, err := encryptWithKey(newKey, plain); err == nil {
				a.settings.PersonaCiphers[id] = nc
			}
		}
	}
	// 兼容旧单人格字段
	if a.settings.PersonaCipher != "" {
		if plain, err := decryptWithKey(oldKey, a.settings.PersonaCipher); err == nil {
			if nc, err := encryptWithKey(newKey, plain); err == nil {
				a.settings.PersonaCipher = nc
			}
		}
	}
	// 2) 重加密小秘历史密文
	if va := a.voiceAgent; va != nil {
		_ = va.ReWrapAll(oldKey, newKey)
	}
	// 3) 更新密码哈希并持久化；内存密码同步为新密码（派生即新密钥）
	a.settings.UserPasswordHash = newHash
	a.personaKey = password
	a.unlockVault(password) // 凭证保险库随之解锁（同密码派生，无需 re-wrap）
	if err := atomicJSON(SettingsPath(a.dataPath), a.settings); err != nil {
		return false
	}
	return true
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
		Number                                        int
		Kind                                          string
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
		items = append(items, item{sess.ID, sess.Title, sess.Created, status, sess.Updated, sess.ParentID, sess.Number, sess.Kind, sess.Pinned, sess.Archived, sess.Checked, sess.AutoArchived})
	}
	// #30：小秘系统会话永远最顶（先于普通置顶）；然后普通置顶；再按最近活动时间倒序
	sort.Slice(items, func(i, j int) bool {
		ai, aj := items[i].Kind == assistantSessionKind, items[j].Kind == assistantSessionKind
		if ai != aj {
			return ai
		}
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
		out[i] = map[string]any{"id": it.ID, "title": it.Title, "created": it.Created, "status": it.Status, "updated": it.Updated, "pinned": it.Pinned, "archived": it.Archived, "checked": it.Checked, "parentId": it.ParentID, "autoArchived": it.AutoArchived, "number": it.Number, "kind": it.Kind}
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
	a.assignSessionNumber(s) // #30：普通会话分配递增编号
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
	// #30：小秘系统会话永久置顶、不可归档
	if s.Kind == assistantSessionKind {
		if in.Archived != nil && *in.Archived {
			fail(w, 400, errors.New("小秘系统会话不可归档"))
			return
		}
		s.Pinned = true
	} else if in.Pinned != nil {
		s.Pinned = *in.Pinned
	}
	if s.Kind != assistantSessionKind && in.Archived != nil {
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
	if s.Kind == assistantSessionKind {
		a.mu.Unlock()
		fail(w, 403, errors.New("小秘系统会话不可删除"))
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
	if err := os.Remove(SessionPath(a.dataPath, id, "active")); err != nil && !os.IsNotExist(err) {
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

// tlsConfig 构造严格的服务端 TLS 配置：最低 TLS 1.2，仅保留 ECDHE 前向保密 + AES-GCM AEAD 套件。
// TLS 1.3 套件由 Go 自动协商，不在此列出；显式列举的 CipherSuites 只作用于 TLS 1.2。
func tlsConfig() *tls.Config {
	return &tls.Config{
		MinVersion: tls.VersionTLS12,
		CipherSuites: []uint16{
			tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
		},
		PreferServerCipherSuites: true,
	}
}

// ensureTLSCert 确保 dir 下存在 cert.pem/key.pem；缺失时自动生成仅本机回环可用的自签证书。
// SAN 固定含 DNS:localhost 与 IP:127.0.0.1，有效期 3650 天，私钥 ECDSA P-256、权限 0600。
// 已存在则复用、不轮换（重启不打断浏览器已信任的例外）。
func ensureTLSCert(dir string) (certFile, keyFile string, err error) {
	certFile = filepath.Join(dir, "cert.pem")
	keyFile = filepath.Join(dir, "key.pem")
	if st, e := os.Stat(certFile); e == nil && !st.IsDir() {
		if _, e := os.Stat(keyFile); e == nil {
			return certFile, keyFile, nil
		}
	}
	if err = os.MkdirAll(dir, 0700); err != nil {
		return "", "", err
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return "", "", err
	}
	tmpl := x509.Certificate{
		SerialNumber:          big.NewInt(time.Now().UnixNano()),
		Subject:               pkix.Name{CommonName: "aide-local"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(3650 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:              []string{"localhost"},
		IPAddresses:           []net.IP{net.IPv4(127, 0, 0, 1), net.IPv6loopback},
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &key.PublicKey, key)
	if err != nil {
		return "", "", err
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return "", "", err
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	if err := os.WriteFile(certFile, certPEM, 0644); err != nil {
		return "", "", err
	}
	if err := os.WriteFile(keyFile, keyPEM, 0600); err != nil {
		return "", "", err
	}
	return certFile, keyFile, nil
}

// isLoopbackHost 判断 Host 头是否指向本机回环（localhost / 127.0.0.1 / ::1，可带端口）。
func isLoopbackHost(host string) bool {
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	switch strings.ToLower(strings.TrimSpace(host)) {
	case "localhost", "127.0.0.1", "::1":
		return true
	}
	return false
}

// strictTransportSecurity 返回应下发的 HSTS 值；非 HTTPS 或回环主机返回空串（不加 HSTS）。
func strictTransportSecurity(r *http.Request) string {
	if r.TLS == nil || isLoopbackHost(r.Host) {
		return ""
	}
	return "max-age=300"
}

// httpsRedirectHandler 把明文 HTTP 请求 308 跳转到同主机同 URI 的 HTTPS。
// Location 直接用 r.Host（含宿主映射端口，如 localhost:8097），不写死容器 8080；
// 308 保留方法与请求体（浏览器旧标签的 API POST 会以 POST 重放到 https）。
func httpsRedirectHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		target := "https://" + r.Host + r.URL.RequestURI()
		http.Redirect(w, r, target, http.StatusPermanentRedirect)
	})
}

// protoListener 协议分流后某一协议的 net.Listener：Accept 从 channel 取已分流连接。
type protoListener struct {
	addr  net.Addr
	conns chan net.Conn
	done  chan struct{}
}

func (l *protoListener) Accept() (net.Conn, error) {
	select {
	case c := <-l.conns:
		return c, nil
	case <-l.done:
		return nil, net.ErrClosed
	}
}

func (l *protoListener) Close() error {
	select {
	case <-l.done:
	default:
		close(l.done)
	}
	return nil
}

func (l *protoListener) Addr() net.Addr { return l.addr }

// peekedConn 把 bufio.Peek 缓冲的首字节与底层 conn 合成单一连接，
// 使上层 TLS 握手能读到被 Peek 走的 0x16；其余读写透传到底层 conn。
type peekedConn struct {
	net.Conn
	br *bufio.Reader
}

func (c *peekedConn) Read(p []byte) (int, error) { return c.br.Read(p) }

// splitProto 在 raw 上 Accept 后用首字节分流：0x16(TLS record)→tlsLn，其余明文→plainLn。
// 每连接独立 goroutine Peek(1)，不批量缓冲，SSE/HTTP2 长连接不被截断或延迟。
func splitProto(raw net.Listener) (tlsLn, plainLn *protoListener) {
	tlsLn = &protoListener{addr: raw.Addr(), conns: make(chan net.Conn), done: make(chan struct{})}
	plainLn = &protoListener{addr: raw.Addr(), conns: make(chan net.Conn), done: make(chan struct{})}
	go func() {
		for {
			c, err := raw.Accept()
			if err != nil {
				tlsLn.Close()
				plainLn.Close()
				return
			}
			go routeConn(c, tlsLn, plainLn)
		}
	}()
	return tlsLn, plainLn
}

func routeConn(c net.Conn, tlsLn, plainLn *protoListener) {
	br := bufio.NewReader(c)
	b, err := br.Peek(1)
	if err != nil {
		_ = c.Close()
		return
	}
	wrapped := &peekedConn{Conn: c, br: br}
	target := plainLn
	if b[0] == 0x16 {
		target = tlsLn
	}
	select {
	case target.conns <- wrapped:
	case <-target.done:
		_ = c.Close()
	}
}

func Run() error {
	a, err := New(env("AIDE_WORKSPACE", "."), env("AIDE_CONTEXT", "context"), env("AIDE_DATA", ".data"))
	if err != nil {
		return err
	}
	defer a.Close()
	// 证书统一落在 #31 规范的 data/certs/；旧 data/tls/ 由 migrateLegacyTLSDir 自动迁入。
	certFile, keyFile, err := ensureTLSCert(CertsDir(a.dataPath))
	if err != nil {
		return err
	}
	s := &http.Server{Addr: env("AIDE_ADDR", "127.0.0.1:8097"), Handler: a.Handler(), ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 << 10, TLSConfig: tlsConfig()}

	// 单端口协议自适应：同一端口先 Peek 首字节分流，
	// 0x16=TLS 走主 server（保持 HTTP/2 + SSE 长连接），其余明文 → 308 跳 https（r.Host 自适应宿主端口）。
	// 取代旧的 AIDE_HTTP_ADDR/8081 独立跳转端口。
	rawLn, err := net.Listen("tcp", s.Addr)
	if err != nil {
		return err
	}
	tlsLn, plainLn := splitProto(rawLn)
	redirectSrv := &http.Server{Handler: httpsRedirectHandler(), ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 15 * time.Second}

	serveErr := make(chan error, 2)
	go func() { serveErr <- s.ServeTLS(tlsLn, certFile, keyFile) }()
	go func() { serveErr <- redirectSrv.Serve(plainLn) }()

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
		_ = redirectSrv.Close()
		_ = rawLn.Close()
	}()
	log.Printf("aide listening on https://%s (单端口自适应 http→https 308); cert=%s; token saved in %s", s.Addr, certFile, AccessTokenPath(a.dataPath))
	select {
	case <-ctx.Done():
		return nil
	case err := <-serveErr:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
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
	saveErr := atomicJSON(TokenStatsPath(a.dataPath), map[string]any{"version": 2, "days": a.tokenStats, "calls": a.tokenCalls})
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
	err := atomicJSON(TokenPricingPath(a.dataPath), a.pricing)
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

// voiceNarrate 接收前端确定性讲解骨架，返回小蜜生成的逐环节口语讲解词。
func (a *App) voiceNarrate(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Steps []NarrationStepInput `json:"steps"`
	}
	if err := decode(w, r, &in); err != nil {
		fail(w, 400, err)
		return
	}
	if len(in.Steps) == 0 {
		fail(w, 400, errors.New("没有可讲解的环节"))
		return
	}
	if len(in.Steps) > 40 {
		fail(w, 400, errors.New("讲解环节过多"))
		return
	}
	fallbackSpeaks := func() []string {
		sp := make([]string, len(in.Steps))
		for i, st := range in.Steps {
			if st.Kind == "openFile" {
				sp[i] = "我们先来看：" + st.Path
			} else {
				sp[i] = clip(stripMdForNarrate(st.Text), 200)
			}
		}
		return sp
	}
	a.mu.Lock()
	cfg := a.settings
	va := a.voiceAgent
	a.mu.Unlock()
	if cfg.BaseURL == "" || cfg.Model == "" || va == nil {
		jsonOut(w, 200, map[string]any{"speaks": fallbackSpeaks()})
		return
	}
	sp, err := va.narrate(r.Context(), cfg, in.Steps)
	if err != nil {
		sp = fallbackSpeaks()
	}
	jsonOut(w, 200, map[string]any{"speaks": sp})
}

// ttsSynthesize 把文本合成为 MP3 音频流（edge-tts 神经音）。
// 响应 Content-Type: audio/mpeg，分块流式返回。首包 1.5s 未到则返回 502，前端自动降级浏览器 Web Speech。
// 选 webspeech 引擎时本端点不合成，返回 400 让前端直接走浏览器朗读。
func (a *App) ttsSynthesize(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Text     string  `json:"text"`
		Voice    string  `json:"voice"`
		Rate     float64 `json:"rate"`
		Style    string  `json:"style"`
		Gender   string  `json:"gender"`
		Provider string  `json:"provider"`
	}
	if err := decode(w, r, &in); err != nil {
		return
	}
	text := strings.TrimSpace(in.Text)
	if text == "" {
		fail(w, 400, errors.New("text 不能为空"))
		return
	}
	a.mu.Lock()
	cfg := a.settings
	a.mu.Unlock()

	providerName := strings.TrimSpace(in.Provider)
	if providerName == "" {
		providerName = cfg.TTSProvider
	}
	if strings.ToLower(providerName) == "webspeech" {
		fail(w, 400, tts.ErrBrowserOnly)
		return
	}

	rate := in.Rate
	if rate == 0 {
		rate = cfg.TTSRate
	}
	if rate == 0 {
		rate = 1.0
	}
	gender := in.Gender
	if gender == "" {
		gender = cfg.VoiceReplyGender
	}
	pcfg := tts.Config{
		Provider:       providerName,
		Voice:          in.Voice,
		Endpoint:       cfg.TTSEndpoint,
		APIKey:         cfg.TTSAPIKey,
		Rate:           rate,
		Expressiveness: cfg.TTSExpressiveness,
		Gender:         gender,
		AzureKey:       cfg.TTSAzureKey,
		AzureRegion:    cfg.TTSAzureRegion,
		CloneBaseURL:   cfg.CloneTTSBaseURL,
		CloneAPIKey:    cfg.CloneTTSAPIKey,
		CloneVoiceID:   cfg.CloneVoiceID,
		CloneBackend:   cfg.CloneTTSBackend,
		SherpaBin:      "", // 空=默认 /usr/local/bin；可用 SHERPA_BIN 环境变量覆盖
		SherpaDir:      "", // 空=默认 /data/tts；可用 SHERPA_TTS_DIR 环境变量覆盖
	}
	if pcfg.Voice == "" {
		pcfg.Voice = cfg.TTSVoice
	}
	opts := tts.SynthOpts{
		Voice:          pcfg.Voice,
		Rate:           rate,
		Style:          in.Style,
		Gender:         gender,
		Expressiveness: cfg.TTSExpressiveness,
	}
	// 多引擎编排：edge → azure(若配 key)。任一可用即返回实际引擎。
	rc, engineUsed, err := tts.ChainSynth(r.Context(), text, opts, pcfg)
	if err != nil {
		a.updateEdgeHealth(false, err)
		a.setTTSEngine("unavailable")
		fail(w, 502, err)
		return
	}
	defer rc.Close()

	// 先读首包再决定是否 200：首包超时/失败时回 502，前端据此降级 Web Speech。
	var first [8192]byte
	n, err := rc.Read(first[:])
	if n == 0 {
		a.updateEdgeHealth(false, err)
		fail(w, 502, fmt.Errorf("TTS 首包超时或失败：%w", err))
		return
	}
	a.updateEdgeHealth(true, nil) // 首包到达：edge 可用，刷新健康缓存
	a.setTTSEngine(engineUsed)
	w.Header().Set("X-TTS-Engine", engineUsed)
	// sherpa 本地离线输出 WAV；edge/azure 输出 MP3。前端经 blob 播放，按此 Content-Type 正确解码。
	ct := "audio/mpeg"
	if engineUsed == "sherpa" {
		ct = "audio/wav"
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(200)
	w.Write(first[:n])
	if err == nil {
		_, _ = io.Copy(w, rc)
	}
}

// ttsColloquialize 把书面回复改写为适合朗读的口语稿（带 LRU 缓存）。
// 失败时原样返回，不阻断朗读。
func (a *App) ttsColloquialize(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Text string `json:"text"`
		Mode string `json:"mode"` // brief(默认) | full
	}
	if err := decode(w, r, &in); err != nil {
		return
	}
	text := strings.TrimSpace(in.Text)
	if text == "" {
		jsonOut(w, 200, map[string]string{"spoken": ""})
		return
	}
	spoken, err := a.colloquialize(r.Context(), text, in.Mode)
	if err != nil {
		// 改写失败：返回原文，前端照读
		jsonOut(w, 200, map[string]string{"spoken": text, "fallback": "1"})
		return
	}
	jsonOut(w, 200, map[string]string{"spoken": spoken})
}
