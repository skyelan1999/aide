package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"
)

// A declarative extension: no plugin code, network, credentials or instructions
// are evaluated by the router. The ordinary host still owns every API request.
type PluginModelRouter struct {
	Protocol int    `json:"protocol"`
	Catalog  string `json:"catalog"`
}
type routerSource struct {
	ID         string `json:"id"`
	URL        string `json:"url"`
	Assessment string `json:"assessment"`
}
type routerProfile struct {
	Output        string         `json:"output,omitempty"` // empty=text, image, video, speech
	WireFormat    string         `json:"wireFormat,omitempty"`
	ID            string         `json:"id"`
	Aliases       []string       `json:"aliases"`
	ContextWindow int            `json:"contextWindow"`
	Vision        bool           `json:"vision"`
	Tools         bool           `json:"tools"`
	Structured    bool           `json:"structured"`
	Weights       map[string]int `json:"weights"`
	Source        string         `json:"source"`
}
type routerRule struct {
	Role     string   `json:"role"`
	Keywords []string `json:"keywords"`
}
type routerCatalog struct {
	Protocol  int             `json:"protocol"`
	Revision  string          `json:"revision"`
	CheckedAt string          `json:"checkedAt"`
	Sources   []routerSource  `json:"sources"`
	Models    []routerProfile `json:"models"`
	Rules     []routerRule    `json:"rules"`
}
type ModelRouteDecision struct {
	Score      int                   `json:"score,omitempty"`
	Candidates []mediaRouteCandidate `json:"candidates,omitempty"`
	Stage      string                `json:"stage"`
	Role       string                `json:"role"`
	Model      string                `json:"model"`
	Provider   string                `json:"provider,omitempty"`
	Profile    string                `json:"profile,omitempty"`
	Catalog    string                `json:"catalog,omitempty"`
	Reason     string                `json:"reason"`
	At         string                `json:"at"`
}

var routerCatalogName = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}\.json$`)

func validateRouterCatalog(raw []byte) (*routerCatalog, error) {
	if len(raw) == 0 || len(raw) > 128<<10 {
		return nil, errors.New("模型路由档案为空或超过128KiB")
	}
	var c routerCatalog
	if json.Unmarshal(raw, &c) != nil || c.Protocol != 1 || c.Revision == "" || len(c.Revision) > 80 || len(c.Models) == 0 || len(c.Models) > 64 || len(c.Rules) > 16 || len(c.Sources) > 64 {
		return nil, errors.New("模型路由档案格式无效")
	}
	sources := map[string]bool{}
	for _, s := range c.Sources {
		if s.ID == "" || sources[s.ID] || !strings.HasPrefix(s.URL, "https://") || len(s.URL) > 2048 || len(s.Assessment) > 2048 {
			return nil, errors.New("模型来源格式无效")
		}
		sources[s.ID] = true
	}
	names := map[string]bool{}
	ids := map[string]bool{}
	for _, m := range c.Models {
		if m.Output != "" && m.Output != "text" && m.Output != "image" && m.Output != "video" && m.Output != "speech" {
			return nil, errors.New("模型输出类型无效")
		}
		if m.WireFormat != "" && m.WireFormat != "deepseek" && m.WireFormat != "openai-reasoning" && m.WireFormat != "openai-compatible" {
			return nil, errors.New("模型参数适配声明无效")
		}
		if m.ID == "" || ids[m.ID] || len(m.ID) > 160 || len(m.Aliases) == 0 || len(m.Aliases) > 32 || m.ContextWindow < 1024 || m.ContextWindow > 4000000 || !sources[m.Source] {
			return nil, errors.New("模型能力条目无效")
		}
		ids[m.ID] = true
		for _, n := range m.Aliases {
			n = routerModelName(n)
			if n == "" || len(n) > 160 || names[n] {
				return nil, errors.New("模型名称重复或无效")
			}
			names[n] = true
		}
		for _, role := range []string{"coding", "reasoning", "writing", "general", "fast"} {
			if m.Weights[role] < 1 || m.Weights[role] > 5 {
				return nil, errors.New("模型适配权重须为1至5")
			}
		}
		if m.Output != "" && m.Output != "text" && (m.Weights[m.Output] < 1 || m.Weights[m.Output] > 5) {
			return nil, errors.New("媒体生成适配权重须为1至5")
		}
	}
	for _, r := range c.Rules {
		if r.Role != "coding" && r.Role != "reasoning" && r.Role != "writing" {
			return nil, errors.New("模型任务角色无效")
		}
		if len(r.Keywords) > 64 {
			return nil, errors.New("模型任务规则过多")
		}
		for _, k := range r.Keywords {
			if strings.TrimSpace(k) == "" || len(k) > 80 {
				return nil, errors.New("模型任务关键词无效")
			}
		}
	}
	return &c, nil
}
func validatePluginModelRouter(decl *PluginModelRouter, files map[string][]byte) error {
	if decl == nil {
		return nil
	}
	if decl.Protocol != 1 || !routerCatalogName.MatchString(decl.Catalog) {
		return errors.New("模型路由声明无效")
	}
	_, err := validateRouterCatalog(files[decl.Catalog])
	return err
}
func routerModelName(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	for _, prefix := range []string{"openai/", "anthropic/", "google/", "deepseek/"} {
		name = strings.TrimPrefix(name, prefix)
	}
	return name
}

// Caller holds a.mu. OpenRoot confines even a concurrently replaced catalog or
// parent directory; no caller-supplied filesystem path or URL is opened.
func (a *App) modelRouterLocked() (*routerCatalog, *PluginManifest) {
	var selected *routerCatalog
	var owner *PluginManifest
	for i := range a.pluginRegistry.Plugins {
		p := &a.pluginRegistry.Plugins[i]
		if !p.Enabled || a.pluginError(p.ID) != "" || p.ModelRouter == nil || p.ModelRouter.Protocol != 1 || !pluginIDPattern.MatchString(p.ID) || !routerCatalogName.MatchString(p.ModelRouter.Catalog) {
			continue
		}
		base, err := os.OpenRoot(a.pluginsPath)
		if err != nil {
			continue
		}
		root, err := base.OpenRoot(p.ID)
		base.Close()
		if err != nil {
			continue
		}
		f, err := root.Open(p.ModelRouter.Catalog)
		root.Close()
		if err != nil {
			continue
		}
		raw, err := io.ReadAll(io.LimitReader(f, (128<<10)+1))
		f.Close()
		if err != nil {
			continue
		}
		c, err := validateRouterCatalog(raw)
		if err != nil {
			continue
		}
		if owner != nil {
			return nil, nil
		} // Ambiguous extensions fail closed.
		selected, owner = c, p
	}
	return selected, owner
}
func (a *App) modelRouterStatusLocked() map[string]any {
	c, p := a.modelRouterLocked()
	if c == nil {
		return map[string]any{"available": false}
	}
	return map[string]any{"available": true, "pluginId": p.ID, "revision": c.Revision, "checkedAt": c.CheckedAt, "profiles": len(c.Models)}
}
func routerRole(c *routerCatalog, stage, text string) string {
	switch stage {
	case "plan", "review", "parallel/plan":
		return "reasoning"
	case "propose":
		return "coding"
	}
	text = strings.ToLower(text)
	best, score := "general", 0
	for _, r := range c.Rules {
		n := 0
		for _, k := range r.Keywords {
			if routeContains(text, strings.ToLower(k)) {
				n++
			}
		}
		if n > score {
			best, score = r.Role, n
		}
	}
	return best
}
func (a *App) routeModelLocked(task *Task, fallback Settings, input []Message, params ProfileParams, tools []any, stage string) (Settings, *ModelRouteDecision) {
	if task == nil || task.ModelSelection == nil || task.ModelSelection.ModelRouter != "auto" {
		return fallback, nil
	}
	// Each request starts from the session's manual fallback, not the last
	// automatic result; disabling the extension between requests must recover it.
	if fallback.routedProfile != nil && task.ModelSelection.Model != "" {
		if manual, err := settingsForSelectedModel(fallback, task.ModelSelection.Model); err == nil {
			if key, err := a.modelAPIKeyForConfigLocked(manual); err == nil {
				manual.APIKey = key
				manual.routedProfile = nil
				fallback = manual
			}
		}
	}
	d := &ModelRouteDecision{Stage: stage, Model: fallback.Model, At: time.Now().UTC().Format(time.RFC3339Nano), Reason: "保留手动回退模型：没有可用的已知能力候选"}
	c, p := a.modelRouterLocked()
	if c == nil {
		d.Reason = "保留手动回退模型：路由插件已停用、档案无效或声明冲突"
		return fallback, d
	}
	text := task.Prompt
	if output := generationIntent(text); output != "" && (stage == "initial" || stage == "chat") {
		return a.routeMediaModelLocked(task, fallback, c, p, output, d)
	}
	if strings.HasPrefix(stage, "parallel/part") {
		text = ""
		if len(input) > 0 {
			text = input[len(input)-1].Content
			if lines := strings.Split(text, "\n"); len(lines) > 1 && lines[0] == "[parallel analysis subtask]" {
				text = lines[1]
			}
		}
	}
	d.Role = routerRole(c, stage, text)
	d.Catalog = c.Revision
	aliases := map[string]string{}
	if raw, ok := p.Settings["aliases"].(map[string]any); ok {
		for k, v := range raw {
			if s, ok := v.(string); ok {
				aliases[routerModelName(k)] = s
			}
		}
	}
	preference, _ := p.Settings["preference"].(string)
	required := estimateProviderPromptTokens(input, tools) + params.MaxTokens
	hasImages := false
	for _, m := range input {
		hasImages = hasImages || len(m.Images) > 0
	}
	type candidate struct {
		cfg      Settings
		profile  string
		score    int
		provider string
	}
	var candidates []candidate
	for _, m := range fallback.Models {
		name := routerModelName(m.ID)
		mapped := aliases[name]
		var profile *routerProfile
		for i := range c.Models {
			item := &c.Models[i]
			matched := mapped != "" && item.ID == mapped
			if mapped == "" {
				for _, alias := range item.Aliases {
					matched = matched || name == routerModelName(alias)
				}
			}
			if matched {
				profile = item
				break
			}
		}
		if profile == nil || profile.Output != "" && profile.Output != "text" {
			continue
		}
		cfg, err := settingsForSelectedModel(fallback, m.ID)
		if err != nil || cfg.BaseURL == "" {
			continue
		}
		window := m.ContextWindow
		if window <= 0 {
			window = 65536
		}
		if profile.ContextWindow < window {
			window = profile.ContextWindow
		}
		if required > window || hasImages && (!profile.Vision || !modelSupportsVision(m.ID, cfg.Models)) || len(tools) > 0 && (!profile.Tools || modelCapabilityStatus(cfg, "tool_calls") == "unsupported") || params.ResponseFormat != "" && (!profile.Structured || modelCapabilityStatus(cfg, "structured_output") == "unsupported") {
			continue
		}
		mediaOK := true
		for _, message := range input {
			if visionGateForConfig(cfg, message.Images) != nil {
				mediaOK = false
				break
			}
		}
		if !mediaOK {
			continue
		}
		key, err := a.modelAPIKeyForConfigLocked(cfg)
		if err != nil {
			continue
		}
		cfg.APIKey = key
		cfg.routedProfile = profile
		score := profile.Weights[d.Role]*100 + profile.Weights["general"]*5
		if preference == "balanced" {
			score += profile.Weights["fast"] * 10
		}
		if preference == "fast" {
			score = profile.Weights["fast"]*100 + profile.Weights[d.Role]*10
		}
		if m.ID == fallback.Model {
			score++
		}
		candidates = append(candidates, candidate{cfg, profile.ID, score, m.ProviderID})
	}
	if len(candidates) == 0 {
		return fallback, d
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].score == candidates[j].score {
			return candidates[i].cfg.Model < candidates[j].cfg.Model
		}
		return candidates[i].score > candidates[j].score
	})
	best := candidates[0]
	d.Model, d.Provider, d.Profile = best.cfg.Model, best.provider, best.profile
	d.Score = best.score
	d.Reason = fmt.Sprintf("任务角色 %s · 适配分 %d；已配置候选 %d 个，按能力约束与插件权重选择（非官方统一排名）", d.Role, best.score, len(candidates))
	return best.cfg, d
}
func (a *App) routedModel(task *Task, cfg Settings, input []Message, params ProfileParams, tools []any, stage string) Settings {
	a.mu.Lock()
	defer a.mu.Unlock()
	next, d := a.routeModelLocked(task, cfg, input, params, tools, stage)
	if d != nil {
		n := len(task.ModelRoutes)
		if n == 0 || task.ModelRoutes[n-1].Stage != d.Stage || task.ModelRoutes[n-1].Model != d.Model || task.ModelRoutes[n-1].Role != d.Role || task.ModelRoutes[n-1].Reason != d.Reason {
			if n < 64 {
				task.ModelRoutes = append(task.ModelRoutes, *d)
			}
		}
	}
	return next
}

func routerProviderID(cfg Settings) string {
	for _, m := range cfg.Models {
		if m.ID == cfg.Model {
			return m.ProviderID
		}
	}
	return ""
}
