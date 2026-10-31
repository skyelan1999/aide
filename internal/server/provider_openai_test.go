package server

import "testing"

func TestOpenAIManualReasoningParameters(t *testing.T) {
	temp := 0.5
	for _, stream := range []bool{false, true} {
		for _, effort := range []string{"auto", "off", "low", "medium", "high", "xhigh", "max"} {
			cfg := Settings{BaseURL: "https://api.openai.com/v1", Model: "gpt-6.1-sol", ReasoningEffort: effort}
			body := buildChatBody(cfg, []Message{{Role: "user", Content: "hello"}}, ProfileParams{MaxTokens: 2048, Temperature: &temp}, nil, stream)
			for _, key := range []string{"thinking", "max_tokens", "temperature"} {
				if _, exists := body[key]; exists {
					t.Fatalf("stream=%v effort=%s leaked %s: %#v", stream, effort, key, body)
				}
			}
			want := effort
			if effort == "off" {
				want = "low"
			}
			if effort == "auto" {
				if _, exists := body["reasoning_effort"]; exists {
					t.Fatal("auto must use the model default", body)
				}
			} else if body["reasoning_effort"] != want {
				t.Fatalf("effort=%s got %#v", effort, body)
			}
			if body["max_completion_tokens"] != 2048 || body["stream"] != stream {
				t.Fatal("request controls lost", body)
			}
		}
	}
}

func TestOpenAIWireOverridesRouterButHonorsCapabilities(t *testing.T) {
	cfg := Settings{BaseURL: "https://API.OPENAI.COM:443/v1/", Model: "gpt-6.1-sol", ReasoningEffort: "high", routedProfile: &routerProfile{WireFormat: "deepseek"}}
	if body := buildChatBody(cfg, nil, ProfileParams{}, nil, false); body["thinking"] != nil || body["reasoning_effort"] != "high" {
		t.Fatal("router overrode official endpoint", body)
	}
	cfg.ReasoningEffort = "off"
	cfg.Models = []ModelRef{{ID: cfg.Model, Capabilities: map[string]string{"reasoning": "unsupported"}}}
	if body := buildChatBody(cfg, nil, ProfileParams{}, nil, false); body["reasoning_effort"] != nil || body["thinking"] != nil {
		t.Fatal("explicit unsupported reasoning ignored", body)
	}
	cfg.Model = "gpt-4o-mini"
	if body := buildChatBody(cfg, nil, ProfileParams{MaxTokens: 512}, nil, false); body["reasoning_effort"] != nil || body["thinking"] != nil || body["max_tokens"] != 512 {
		t.Fatal("non-reasoning OpenAI request changed", body)
	}
}

func TestOpenAIEndpointIsolation(t *testing.T) {
	for _, base := range []string{"https://api.deepseek.com", "https://api.openai.com.example.org/v1", "https://example.org/api.openai.com", "https://api.openai.com@other.example/v1", "http://localhost:8080/v1"} {
		cfg := Settings{BaseURL: base, Model: "gpt-6.1-sol", ReasoningEffort: "high"}
		body := buildChatBody(cfg, nil, ProfileParams{}, nil, false)
		if body["thinking"] == nil {
			t.Fatalf("non-OpenAI endpoint %s changed", base)
		}
	}
}

func TestCustomProviderReasoningEffortOptIn(t *testing.T) {
	cfg := Settings{
		BaseURL: "http://127.0.0.1:8787/v1", Model: "gpt-6-luna", ReasoningEffort: "high",
		Models:         []ModelRef{{ID: "gpt-6-luna", ProviderID: "codex"}},
		ModelProviders: []ModelProvider{{ID: "codex", BaseURL: "http://127.0.0.1:8787/v1", Protocol: "openai-compatible", ReasoningEffortFormat: "codex-bridge"}},
	}
	if got := modelReasoningEfforts(cfg); len(got) != 9 || got[0] != "auto" || got[2] != "minimal" || got[5] != "high" || got[6] != "xhigh" || got[7] != "max" || got[8] != "ultra" {
		t.Fatal("opted-in provider did not expose aligned Aide choices", got)
	}
	for _, stream := range []bool{false, true} {
		for _, tc := range []struct{ effort, want string }{{"auto", ""}, {"off", "off"}, {"minimal", "minimal"}, {"low", "low"}, {"medium", "medium"}, {"high", "high"}, {"xhigh", "xhigh"}, {"max", "max"}, {"ultra", "ultra"}} {
			cfg.ReasoningEffort = tc.effort
			body := buildChatBody(cfg, []Message{{Role: "user", Content: "hello"}}, ProfileParams{MaxTokens: 2048}, nil, stream)
			if _, ok := body["thinking"]; ok {
				t.Fatalf("stream=%v effort=%s leaked DeepSeek thinking extension: %#v", stream, tc.effort, body)
			}
			if tc.want == "" {
				if _, ok := body["reasoning_effort"]; ok {
					t.Fatalf("auto must omit reasoning_effort: %#v", body)
				}
			} else if body["reasoning_effort"] != tc.want {
				t.Fatalf("stream=%v effort=%s got %#v", stream, tc.effort, body["reasoning_effort"])
			}
			if body["max_completion_tokens"] != 2048 || body["max_tokens"] != nil {
				t.Fatalf("OpenAI reasoning request tokens not normalized: %#v", body)
			}
		}
	}
	cfg.ModelProviders[0].ReasoningEffortFormat = ""
	cfg.ReasoningEffort = "high"
	body := buildChatBody(cfg, nil, ProfileParams{}, nil, false)
	if body["reasoning_effort"] != "high" || body["thinking"] == nil {
		t.Fatalf("default custom-provider wire behavior changed: %#v", body)
	}
	cfg.ModelProviders[0].ReasoningEffortFormat = "openai"
	cfg.ReasoningEffort = "off"
	body = buildChatBody(cfg, nil, ProfileParams{}, nil, false)
	if body["reasoning_effort"] != "none" {
		t.Fatalf("OpenAI format should encode off as none: %#v", body)
	}
}

func TestNormalizeModelProviderReasoningEffortFormat(t *testing.T) {
	base := ModelProvider{ID: "codex", Name: "Codex Bridge", BaseURL: "http://127.0.0.1:8787/v1", Protocol: "openai-compatible"}
	base.ReasoningEffortFormat = "openai"
	if _, err := normalizeModelProviders([]ModelProvider{base}); err != nil {
		t.Fatal(err)
	}
	base.ReasoningEffortFormat = "codex-bridge"
	if _, err := normalizeModelProviders([]ModelProvider{base}); err != nil {
		t.Fatal(err)
	}
	base.ReasoningEffortFormat = "unsupported"
	if _, err := normalizeModelProviders([]ModelProvider{base}); err == nil {
		t.Fatal("unsupported reasoning format was accepted")
	}
}

func TestOpenAIReasoningOffModelSupport(t *testing.T) {
	for _, tc := range []struct{ model, want string }{
		{"gpt-6.1-sol", "low"},
		{"gpt-6.1-sol-2026-10-30", "low"},
		{"gpt-6-astra", "low"},
		{"gpt-5.5", "none"},
	} {
		cfg := Settings{BaseURL: "https://api.openai.com/v1", Model: tc.model, ReasoningEffort: "off", routedProfile: &routerProfile{WireFormat: "openai-compatible"}}
		body := buildChatBody(cfg, nil, ProfileParams{}, nil, false)
		if body["reasoning_effort"] != tc.want || body["thinking"] != nil {
			t.Fatalf("%s: %#v", tc.model, body)
		}
	}
}

func TestOpenAISessionReasoningSelection(t *testing.T) {
	a := &App{settings: Settings{
		Model: "deepseek-chat", BaseURL: "https://api.deepseek.com", ReasoningEffort: "off",
		Models:         []ModelRef{{ID: "deepseek-chat", ProviderID: "deepseek"}, {ID: "gpt-6.1-sol", ProviderID: "openai"}},
		ModelProviders: []ModelProvider{{ID: "deepseek", BaseURL: "https://api.deepseek.com"}, {ID: "openai", BaseURL: "https://api.openai.com/v1"}},
	}}
	for _, effort := range []string{"low", "medium", "high", "xhigh", "max"} {
		cfg, err := a.selectionSettingsLocked(SessionModelSelection{Strategy: "auto", Model: "gpt-6.1-sol", ReasoningEffort: effort})
		if err != nil {
			t.Fatal(err)
		}
		body := buildChatBody(cfg, nil, ProfileParams{}, nil, false)
		if body["reasoning_effort"] != effort || body["thinking"] != nil {
			t.Fatalf("session selection %s was not applied: %#v", effort, body)
		}
	}
	if a.settings.ReasoningEffort != "off" || a.settings.Model != "deepseek-chat" {
		t.Fatal("session selection changed global configuration")
	}
}

func TestOpenAIReasoningOptionsAndModelSwitch(t *testing.T) {
	cfg := Settings{BaseURL: "https://api.openai.com/v1", Model: "gpt-6.1-sol"}
	options := modelReasoningEfforts(cfg)
	if len(options) != 6 || options[4] != "xhigh" || options[5] != "max" {
		t.Fatal("Sol options incomplete", options)
	}
	if adaptedReasoningEffort(cfg, "off") != "auto" {
		t.Fatal("unsupported selection must fall back to default")
	}
	cfg.Model = "gpt-6-luna"
	if adaptedReasoningEffort(cfg, "off") != "off" || adaptedReasoningEffort(cfg, "max") != "max" {
		t.Fatal("Luna supported choices lost")
	}
	cfg.BaseURL = "https://api.deepseek.com"
	if adaptedReasoningEffort(cfg, "max") != "auto" {
		t.Fatal("OpenAI intensity leaked to another provider")
	}
	cfg.Models = []ModelRef{{ID: cfg.Model, Capabilities: map[string]string{"reasoning": "unsupported"}}}
	if len(modelReasoningEfforts(cfg)) != 1 {
		t.Fatal("unsupported model exposes reasoning choices")
	}
}
