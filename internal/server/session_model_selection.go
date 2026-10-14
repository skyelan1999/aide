package server

import (
	"errors"
	"fmt"
	"net/http"
	"time"
)

// Non-secret choices are session-owned; catalogs and credentials remain shared.
type SessionModelSelection struct {
	Strategy        string `json:"strategy"`
	Profile         string `json:"profile"`
	Model           string `json:"model"`
	ReasoningEffort string `json:"reasoningEffort"`
}

func cloneModelSelection(in *SessionModelSelection) *SessionModelSelection {
	if in == nil {
		return nil
	}
	out := *in
	return &out
}
func (a *App) defaultModelSelectionLocked() SessionModelSelection {
	strategy := a.profileState.Strategy
	if strategy == "" {
		strategy = "auto"
	}
	profile := a.profileState.ActiveProfile
	if profile == "" {
		profile = "default"
	}
	effort := a.settings.ReasoningEffort
	if effort == "" {
		effort = "auto"
	}
	return SessionModelSelection{Strategy: strategy, Profile: profile, Model: a.settings.Model, ReasoningEffort: effort}
}
func (a *App) sessionModelSelectionLocked(s *Session) SessionModelSelection {
	if s != nil && s.ModelSelection != nil {
		return *s.ModelSelection
	}
	choice := a.defaultModelSelectionLocked()
	if s != nil {
		for i := len(s.Runs) - 1; i >= 0; i-- {
			run := s.Runs[i]
			if run.ModelSelection != nil {
				return *run.ModelSelection
			}
			if run.Model != "" {
				choice.Model = run.Model
				if run.Strategy != "" {
					choice.Strategy = run.Strategy
				}
				if run.Profile != "" {
					choice.Profile = run.Profile
				}
				break
			}
		}
	}
	return choice
}
func (a *App) taskModelSelectionLocked(task *Task) SessionModelSelection {
	if task.ModelSelection != nil {
		return *task.ModelSelection
	}
	choice := a.defaultModelSelectionLocked()
	if task.Model != "" {
		choice.Model = task.Model
	}
	if task.Strategy != "" {
		choice.Strategy = task.Strategy
	}
	if task.Profile != "" {
		choice.Profile = task.Profile
	}
	return choice
}
func settingsForSelectedModel(cfg Settings, modelID string) (Settings, error) {
	if modelID == "" {
		modelID = cfg.Model
	}
	cfg.Model = modelID
	cfg.ActiveModel = modelID
	var selected *ModelRef
	for i := range cfg.Models {
		if cfg.Models[i].ID == modelID {
			selected = &cfg.Models[i]
			break
		}
	}
	if selected == nil && len(cfg.Models) > 0 && modelID != "" {
		return cfg, fmt.Errorf("会话选择的模型 %q 已不可用，请重新选择", modelID)
	}
	if selected != nil && selected.ProviderID != "" {
		found := false
		for _, provider := range cfg.ModelProviders {
			if provider.ID == selected.ProviderID {
				cfg.BaseURL = provider.BaseURL
				found = true
				break
			}
		}
		if !found {
			return cfg, errors.New("会话模型的提供商已不可用，请重新选择")
		}
	}
	return cfg, nil
}
func (a *App) selectionSettingsLocked(choice SessionModelSelection) (Settings, error) {
	if choice.Strategy != "auto" && choice.Strategy != "manual" {
		return Settings{}, errors.New("策略只支持 manual 或 auto")
	}
	if choice.Strategy == "manual" {
		if _, ok := a.findProfile(choice.Profile); !ok {
			return Settings{}, errors.New("会话参数策略已不可用，请重新选择")
		}
	}
	switch choice.ReasoningEffort {
	case "auto", "off", "low", "medium", "high":
	default:
		return Settings{}, errors.New("无效会话推理强度")
	}
	cfg, err := settingsForSelectedModel(a.settings, choice.Model)
	cfg.ReasoningEffort = choice.ReasoningEffort
	return cfg, err
}
func (a *App) modelAPIKeyForConfigLocked(cfg Settings) (string, error) {
	providerID := "default"
	for _, model := range cfg.Models {
		if model.ID == cfg.Model && model.ProviderID != "" {
			providerID = model.ProviderID
			break
		}
	}
	vaultID := VaultIDModelAPIKey
	if len(cfg.ModelProviders) > 0 {
		found := false
		for _, provider := range cfg.ModelProviders {
			if provider.ID == providerID {
				vaultID = modelProviderVaultID(provider.ID, provider.ActiveKeyID)
				found = true
				break
			}
		}
		if !found {
			return "", errors.New("会话模型的提供商已不可用")
		}
	}
	return a.vaultKeyByIDLocked(vaultID)
}
func visionGateForConfig(cfg Settings, images []MessageImage) error {
	if len(images) > 0 && !modelSupportsVision(cfg.Model, cfg.Models) {
		return fmt.Errorf("会话模型 %q 不支持图片输入", cfg.Model)
	}
	return nil
}
func (a *App) sessionModelSelectionHandler(w http.ResponseWriter, r *http.Request) {
	var in struct {
		SessionModelSelection
		Revision *uint64 `json:"revision,omitempty"`
	}
	if err := decode(w, r, &in); err != nil {
		fail(w, 400, err)
		return
	}
	choice := in.SessionModelSelection
	a.mu.Lock()
	defer a.mu.Unlock()
	s := a.sessions[r.PathValue("id")]
	if s == nil || s.Deleted {
		fail(w, 404, errors.New("会话不存在"))
		return
	}
	if s.Kind == assistantSessionKind {
		fail(w, 400, errors.New("小秘模型来源由独立设置管理"))
		return
	}
	if in.Revision != nil && *in.Revision != s.ModelSelectionRevision {
		fail(w, 409, errors.New("会话配置已变化，请重新选择"))
		return
	}
	if _, err := a.selectionSettingsLocked(choice); err != nil {
		fail(w, 400, err)
		return
	}
	old, updated, revision := s.ModelSelection, s.Updated, s.ModelSelectionRevision
	s.ModelSelectionRevision++
	s.ModelSelection = cloneModelSelection(&choice)
	s.Updated = time.Now().UTC().Format(time.RFC3339Nano)
	if err := a.save(s); err != nil {
		s.ModelSelection = old
		s.ModelSelectionRevision = revision
		s.Updated = updated
		fail(w, 500, err)
		return
	}
	a.broadcastSessionsChanged(s.ID)
	jsonOut(w, 200, map[string]any{"modelSelection": choice, "revision": s.ModelSelectionRevision})
}
