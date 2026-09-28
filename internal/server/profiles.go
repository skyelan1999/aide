package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"
)

const (
	profilesFileName    = "profiles.json"
	routingPolicyJSON   = "routing-policy.json"
	routingPolicyMD     = "routing-policy.md"
	maxUserProfiles     = 20
	profileIDPatternSrc = `^[A-Za-z0-9_-]{1,64}$`
)

var profileIDPattern = regexp.MustCompile(profileIDPatternSrc)

// maxTokensDefault 系统 profile 的默认输出预算。旧值 4096 会把长脚本/长工具调用参数
// 在中途截断（finish_reason=length 且 arguments JSON 不闭合→工具不执行）。DeepSeek
// deepseek-flash / deepseek-v4-pro 最大输出 384K、legacy deepseek-chat 最大 8K，
// 8192 对两者都是安全的较大值，既够一次写出中等脚本，又不至于空耗预算。
const maxTokensDefault = 8192

// maxTokensCeiling 设置里允许的最大值。flash/pro 实测支持到 384K；这里取 65536
// 作为“超长脚本”的可调上限，再长就靠 length 自动续写补全（workflow.go）兜底。
const maxTokensCeiling = 65536

// ProfileParams 是 DeepSeek Chat Completions 的可选采样参数。
// 数值字段用指针区分「未设置」与 0；omitempty 保证未设置时不进入请求体。
type ProfileParams struct {
	Temperature      *float64 `json:"temperature,omitempty"`       // 0 – 2
	TopP             *float64 `json:"top_p,omitempty"`             // 0 – 1
	MaxTokens        int      `json:"max_tokens,omitempty"`        // 1 – maxTokensCeiling；0 = 未设置
	FrequencyPenalty *float64 `json:"frequency_penalty,omitempty"` // −2 – 2
	PresencePenalty  *float64 `json:"presence_penalty,omitempty"`  // −2 – 2
	ResponseFormat   string   `json:"response_format,omitempty"`   // "" | text | json_object
	Stop             []string `json:"stop,omitempty"`              // ≤16 项
}

type Profile struct {
	ID     string        `json:"id"`
	Name   string        `json:"name"`
	System bool          `json:"system,omitempty"`
	Params ProfileParams `json:"params"`
}

// ProfilesState 持久化于工程目录 profiles.json：只含用户配置与策略；
// 系统配置硬编码在 systemProfiles 中，文件无法篡改（FR-62 / D1）。
type ProfilesState struct {
	Version       int       `json:"version"`
	Strategy      string    `json:"strategy"`      // manual | auto
	ActiveProfile string    `json:"activeProfile"` // manual 模式生效 profile id
	Profiles      []Profile `json:"profiles"`      // 仅用户配置
}

func fp(v float64) *float64 { return &v }

// systemProfiles 内置三个系统配置：不可修改、不可删除（LIM-23）。
// default 与改造前行为完全一致（FR-63 默认值 / D6）。
var systemProfiles = []Profile{
	{ID: "default", Name: "默认", System: true, Params: ProfileParams{Temperature: fp(1), TopP: fp(1), MaxTokens: maxTokensDefault, FrequencyPenalty: fp(0), PresencePenalty: fp(0), ResponseFormat: "text"}},
	{ID: "precise", Name: "精确", System: true, Params: ProfileParams{Temperature: fp(0.2), TopP: fp(0.9), MaxTokens: maxTokensDefault, FrequencyPenalty: fp(0), PresencePenalty: fp(0), ResponseFormat: "text"}},
	{ID: "creative", Name: "创意", System: true, Params: ProfileParams{Temperature: fp(1.5), TopP: fp(0.95), MaxTokens: maxTokensDefault, FrequencyPenalty: fp(0), PresencePenalty: fp(0), ResponseFormat: "text"}},
}

func isSystemProfileID(id string) bool {
	for _, p := range systemProfiles {
		if p.ID == id {
			return true
		}
	}
	return false
}

func validateProfileParams(p *ProfileParams) error {
	if p.Temperature != nil && (*p.Temperature < 0 || *p.Temperature > 2) {
		return errors.New("temperature 必须在 0–2 之间")
	}
	if p.TopP != nil && (*p.TopP < 0 || *p.TopP > 1) {
		return errors.New("top_p 必须在 0–1 之间")
	}
	if p.MaxTokens != 0 && (p.MaxTokens < 1 || p.MaxTokens > maxTokensCeiling) {
		return fmt.Errorf("max_tokens 必须在 1–%d 之间", maxTokensCeiling)
	}
	if p.FrequencyPenalty != nil && (*p.FrequencyPenalty < -2 || *p.FrequencyPenalty > 2) {
		return errors.New("frequency_penalty 必须在 -2–2 之间")
	}
	if p.PresencePenalty != nil && (*p.PresencePenalty < -2 || *p.PresencePenalty > 2) {
		return errors.New("presence_penalty 必须在 -2–2 之间")
	}
	if p.ResponseFormat != "" && p.ResponseFormat != "text" && p.ResponseFormat != "json_object" {
		return errors.New("response_format 只支持 text 或 json_object")
	}
	if len(p.Stop) > 16 {
		return errors.New("stop 最多 16 项")
	}
	for _, s := range p.Stop {
		if strings.TrimSpace(s) == "" || utf8.RuneCountInString(s) > 64 {
			return errors.New("stop 每项须为 1–64 个字符")
		}
	}
	return nil
}

// findProfile 在系统配置与用户配置中查找；返回副本避免调用方意外改写共享状态。
func (a *App) findProfile(id string) (Profile, bool) {
	for _, p := range systemProfiles {
		if p.ID == id {
			return p, true
		}
	}
	for _, p := range a.profileState.Profiles {
		if p.ID == id {
			return p, true
		}
	}
	return Profile{}, false
}
func (a *App) allProfiles() []Profile {
	out := make([]Profile, 0, len(systemProfiles)+len(a.profileState.Profiles))
	out = append(out, systemProfiles...)
	out = append(out, a.profileState.Profiles...)
	return out
}
func (a *App) saveProfiles() error {
	return atomicJSON(a.profilesPath, a.profileState)
}

// loadProfiles 在 New() 中调用：文件缺失时使用缺省状态；内容非法时返回错误拒绝启动。
func (a *App) loadProfiles() error {
	a.profileState = ProfilesState{Version: 1, Strategy: "manual", ActiveProfile: "default", Profiles: []Profile{}}
	b, err := os.ReadFile(a.profilesPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := json.Unmarshal(b, &a.profileState); err != nil {
		return fmt.Errorf("解析 %s: %w", profilesFileName, err)
	}
	if a.profileState.Strategy != "auto" {
		a.profileState.Strategy = "manual"
	}
	if _, ok := a.findProfile(a.profileState.ActiveProfile); !ok {
		a.profileState.ActiveProfile = "default"
	}
	if len(a.profileState.Profiles) > maxUserProfiles {
		return fmt.Errorf("%s: 用户配置超过 %d 个", profilesFileName, maxUserProfiles)
	}
	seen := map[string]bool{}
	for i := range a.profileState.Profiles {
		p := &a.profileState.Profiles[i]
		p.System = false
		if isSystemProfileID(p.ID) {
			return fmt.Errorf("%s: 用户配置不得使用系统配置 id %q", profilesFileName, p.ID)
		}
		if !profileIDPattern.MatchString(p.ID) {
			return fmt.Errorf("%s: 配置 id %q 非法", profilesFileName, p.ID)
		}
		if seen[p.ID] {
			return fmt.Errorf("%s: 配置 id %q 重复", profilesFileName, p.ID)
		}
		seen[p.ID] = true
		if err := validateProfileParams(&p.Params); err != nil {
			return fmt.Errorf("%s: %w", profilesFileName, err)
		}
	}
	return nil
}

func (a *App) listProfiles(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	defer a.mu.Unlock()
	jsonOut(w, 200, map[string]any{"strategy": a.profileState.Strategy, "activeProfile": a.profileState.ActiveProfile, "profiles": a.allProfiles()})
}

func (a *App) updateProfiles(w http.ResponseWriter, r *http.Request) {
	var in ProfilesState
	if err := decode(w, r, &in); err != nil {
		fail(w, 400, err)
		return
	}
	if in.Strategy == "" {
		in.Strategy = "manual"
	}
	if in.Strategy != "manual" && in.Strategy != "auto" {
		fail(w, 400, errors.New("策略只支持 manual 或 auto"))
		return
	}
	if in.ActiveProfile == "" {
		in.ActiveProfile = "default"
	}
	if len(in.Profiles) > maxUserProfiles {
		fail(w, 400, fmt.Errorf("用户配置最多 %d 个", maxUserProfiles))
		return
	}
	seen := map[string]bool{}
	for i := range in.Profiles {
		p := &in.Profiles[i]
		p.System = false
		if isSystemProfileID(p.ID) {
			fail(w, 400, fmt.Errorf("系统配置 %s 不可修改", p.ID))
			return
		}
		if !profileIDPattern.MatchString(p.ID) {
			fail(w, 400, errors.New("配置 id 只能含字母、数字、-、_，长度 1–64"))
			return
		}
		if seen[p.ID] {
			fail(w, 400, fmt.Errorf("配置 id %s 重复", p.ID))
			return
		}
		seen[p.ID] = true
		p.Name = strings.TrimSpace(p.Name)
		if p.Name == "" {
			p.Name = p.ID
		}
		if utf8.RuneCountInString(p.Name) > 32 {
			fail(w, 400, errors.New("配置名称最长 32 个字符"))
			return
		}
		if err := validateProfileParams(&p.Params); err != nil {
			fail(w, 400, err)
			return
		}
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	activeKnown := isSystemProfileID(in.ActiveProfile) || seen[in.ActiveProfile]
	if !activeKnown {
		fail(w, 400, errors.New("activeProfile 不存在"))
		return
	}
	in.Version = 1
	a.profileState = in
	if err := a.saveProfiles(); err != nil {
		fail(w, 500, err)
		return
	}
	jsonOut(w, 200, map[string]any{"ok": true, "strategy": a.profileState.Strategy, "activeProfile": a.profileState.ActiveProfile, "profiles": a.allProfiles()})
}

// ── auto 路由策略（FR-64） ──

type routingPolicy struct {
	Version int           `json:"version"`
	Rules   []routingRule `json:"rules"`
	Default string        `json:"default"`
}
type routingRule struct {
	When struct {
		Mode           string   `json:"mode"`
		PromptContains []string `json:"promptContains"`
	} `json:"when"`
	Use string `json:"use"`
}

// extractJSONFence 从 markdown 文本中提取第一个 ```json（或 ```）代码块内容。
func extractJSONFence(b []byte) []byte {
	s := string(b)
	for _, fence := range []string{"```json", "```"} {
		start := strings.Index(s, fence)
		if start < 0 {
			continue
		}
		rest := s[start+len(fence):]
		if i := strings.Index(rest, "\n"); i >= 0 {
			rest = rest[i+1:]
		}
		end := strings.Index(rest, "```")
		if end < 0 {
			continue
		}
		return []byte(rest[:end])
	}
	return nil
}

// readRoutingPolicy 读取工程目录策略：routing-policy.json 优先，
// routing-policy.md 内 JSON 代码块兜底；两者均缺失或解析失败返回 nil。
func (a *App) readRoutingPolicy() *routingPolicy {
	b, err := os.ReadFile(filepath.Join(a.workPath, routingPolicyJSON))
	if err != nil {
		b, err = os.ReadFile(filepath.Join(a.workPath, routingPolicyMD))
		if err != nil {
			return nil
		}
		b = extractJSONFence(b)
		if len(b) == 0 {
			return nil
		}
	}
	var p routingPolicy
	if err := json.Unmarshal(b, &p); err != nil {
		return nil
	}
	return &p
}

// resolveProfile 决定本次任务的生效 profile：manual 直接用指定 profile；
// auto 按策略文件规则路由，无命中或文件缺失回落 default（FR-64 / D4）。
func (a *App) resolveProfile(strategy, profile, prompt, mode string) (string, ProfileParams, error) {
	if strategy == "auto" {
		if policy := a.readRoutingPolicy(); policy != nil {
			for _, rule := range policy.Rules {
				if rule.When.Mode != "" && rule.When.Mode != mode {
					continue
				}
				matched := len(rule.When.PromptContains) == 0
				if !matched {
					lower := strings.ToLower(prompt)
					for _, kw := range rule.When.PromptContains {
						if kw != "" && strings.Contains(lower, strings.ToLower(kw)) {
							matched = true
							break
						}
					}
				}
				if matched && rule.Use != "" {
					if p, ok := a.findProfile(rule.Use); ok {
						return p.ID, p.Params, nil
					}
				}
			}
			if policy.Default != "" {
				if p, ok := a.findProfile(policy.Default); ok {
					return p.ID, p.Params, nil
				}
			}
		}
		if p, ok := a.findProfile("default"); ok {
			return p.ID, p.Params, nil
		}
		return "", ProfileParams{}, errors.New("路由策略不可用")
	}
	if profile == "" {
		profile = "default"
	}
	p, ok := a.findProfile(profile)
	if !ok {
		return "", ProfileParams{}, fmt.Errorf("未知参数配置: %s", profile)
	}
	return p.ID, p.Params, nil
}
