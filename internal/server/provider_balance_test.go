package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type supportRoundTrip func(*http.Request) (*http.Response, error)

func (f supportRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func supporterVault(t *testing.T, a *App, id, value string) {
	t.Helper()
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.vault.Unlocked() {
		a.vault.Unlock(bytes.Repeat([]byte{4}, 32))
	}
	if e := a.vault.Put(id, "test-fixture", "synthetic", []byte(value), ""); e != nil {
		t.Fatal(e)
	}
}
func TestProviderBalanceNoPluginNeverRequestsNetwork(t *testing.T) {
	a := testApp(t)
	a.settings = Settings{Model: "m", Models: []ModelRef{{ID: "m", ProviderID: "p"}}, ModelProviders: []ModelProvider{{ID: "p", BaseURL: "https://api.deepseek.com", ActiveKeyID: "k", Keys: []ModelProviderKey{{ID: "k"}}}}}
	old := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = old })
	http.DefaultTransport = supportRoundTrip(func(*http.Request) (*http.Response, error) {
		t.Fatal("no plugin must never query balances")
		return nil, errors.New("unexpected")
	})
	w := request(a, "GET", "/api/balance", nil)
	requireStatus(t, w, 200)
	if !strings.Contains(w.Body.String(), `"supported":false`) {
		t.Fatal(w.Body.String())
	}
}
func TestProviderBalanceSelectedKeyAndSanitizedAccount(t *testing.T) {
	a := testApp(t)
	supporterInstall(t, a, "deepseek")
	a.settings = Settings{Model: "deepseek-flash", Models: []ModelRef{{ID: "deepseek-flash", ProviderID: "deep"}}, ModelProviders: []ModelProvider{{ID: "deep", BaseURL: "https://api.deepseek.com", ActiveKeyID: "first", Keys: []ModelProviderKey{{ID: "first"}, {ID: "second"}}}}}
	supporterVault(t, a, modelProviderVaultID("deep", "second"), "synthetic-selected-key")
	old := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = old })
	http.DefaultTransport = supportRoundTrip(func(r *http.Request) (*http.Response, error) {
		if r.URL.String() != "https://api.deepseek.com/user/balance" || r.Header.Get("Authorization") != "Bearer synthetic-selected-key" {
			t.Fatal("wrong balance target or credential")
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"is_available":true,"balance_infos":[{"currency":"CNY","total_balance":"12","granted_balance":"2","topped_up_balance":"10"}],"unexpected_secret":"do-not-reflect"}`)), Header: make(http.Header)}, nil
	})
	w := request(a, "GET", "/api/balance?providerId=deep&keyId=second", nil)
	requireStatus(t, w, 200)
	if strings.Contains(w.Body.String(), "do-not-reflect") || strings.Contains(w.Body.String(), "synthetic-selected-key") || !strings.Contains(w.Body.String(), `"keyId":"second"`) {
		t.Fatal("balance leaked or misattributed", w.Body.String())
	}
}
func TestProviderBillingCredentialsSeparateVaultAndClear(t *testing.T) {
	a := testApp(t)
	supporterInstall(t, a, "ali")
	a.settings = Settings{Model: "qwen-plus", Models: []ModelRef{{ID: "qwen-plus", ProviderID: "ali"}}, ModelProviders: []ModelProvider{{ID: "ali", BaseURL: "https://dashscope.aliyuncs.com/compatible-mode/v1", ActiveKeyID: "inference", Keys: []ModelProviderKey{{ID: "inference"}}}}}
	supporterVault(t, a, modelProviderVaultID("ali", "inference"), "synthetic-model-secret")
	body := map[string]any{"providerId": "ali", "accessKeyId": "synthetic-ram-id", "accessKeySecret": "synthetic-ram-secret"}
	w := request(a, "PUT", "/api/provider-billing-credentials", body)
	requireStatus(t, w, 200)
	if strings.Contains(w.Body.String(), "synthetic-") {
		t.Fatal("credential response reflected secret")
	}
	raw, e := a.vault.Get(billingVaultID("ali"))
	if e != nil || !strings.Contains(string(raw), "synthetic-ram-secret") {
		t.Fatal(e)
	}
	model, e := a.vault.Get(modelProviderVaultID("ali", "inference"))
	if e != nil || string(model) != "synthetic-model-secret" {
		t.Fatal("billing overwrote model key")
	}
	old := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = old })
	http.DefaultTransport = supportRoundTrip(func(r *http.Request) (*http.Response, error) {
		q := r.URL.Query()
		if r.URL.Host != "business.aliyuncs.com" || q.Get("Action") != "QueryAccountBalance" || q.Get("AccessKeyId") != "synthetic-ram-id" || q.Get("Signature") == "" || r.Header.Get("Authorization") != "" {
			t.Fatal("wrong billing request")
		}
		if strings.Contains(r.URL.String(), "synthetic-ram-secret") || strings.Contains(r.URL.String(), "synthetic-model-secret") {
			t.Fatal("secret in URL")
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"Success":true,"Data":{"Currency":"CNY","AvailableAmount":"8","AvailableCashAmount":"7"}}`)), Header: make(http.Header)}, nil
	})
	w = request(a, "GET", "/api/balance?providerId=ali&keyId=inference", nil)
	requireStatus(t, w, 200)
	var out map[string]any
	if e = json.Unmarshal(w.Body.Bytes(), &out); e != nil || out["balanceScope"] != "account" {
		t.Fatal(e, out)
	}
	requireStatus(t, request(a, "PUT", "/api/provider-billing-credentials", map[string]any{"providerId": "ali", "clear": true}), 200)
	w = request(a, "GET", "/api/balance?providerId=ali", nil)
	requireStatus(t, w, 200)
	if !strings.Contains(w.Body.String(), `"billingCredentialsRequired":true`) {
		t.Fatal(w.Body.String())
	}
}

func TestProviderBillingKeySnapshotSurvivesGlobalSelectionChange(t *testing.T) {
	a := testApp(t)
	cfg := Settings{Model: "model-a", BaseURL: "https://first.example", Models: []ModelRef{{ID: "model-a", ProviderID: "first"}, {ID: "model-b", ProviderID: "second"}}, ModelProviders: []ModelProvider{{ID: "first", BaseURL: "https://first.example", ActiveKeyID: "k1", Keys: []ModelProviderKey{{ID: "k1", Label: "first-key"}}}, {ID: "second", BaseURL: "https://second.example", ActiveKeyID: "k2", Keys: []ModelProviderKey{{ID: "k2", Label: "second-key"}}}}}
	a.settings = cfg
	supporterVault(t, a, modelProviderVaultID("first", "k1"), "synthetic-first")
	supporterVault(t, a, modelProviderVaultID("second", "k2"), "synthetic-second")
	a.settings.Model = "model-b"
	a.settings.BaseURL = "https://second.example"
	selected, e := a.modelSettingsWithAPIKey(cfg)
	if e != nil || selected.APIKey != "synthetic-first" {
		t.Fatal("request used current global key instead of snapshot", e)
	}
	u := TokenUsage{}
	applyBillingIdentity(&u, selected)
	if u.ProviderID != "first" || u.KeyID != "k1" {
		t.Fatal("wrong attribution", u)
	}
}

func TestProviderBillingIsolatedAssistantDoesNotInheritGlobalKey(t *testing.T) {
	a := testApp(t)
	a.settings = Settings{Model: "deepseek-flash", BaseURL: "https://api.deepseek.com", XiaomiModelSource: "custom", XiaomiModel: "deepseek-flash", XiaomiBaseURL: "https://api.deepseek.com", Models: []ModelRef{{ID: "deepseek-flash", ProviderID: "default"}}, ModelProviders: []ModelProvider{{ID: "default", BaseURL: "https://api.deepseek.com", ActiveKeyID: "global", Keys: []ModelProviderKey{{ID: "global", Label: "Global"}}}}}
	supporterVault(t, a, VaultIDXiaomiModelAPIKey, "synthetic-isolated")
	a.mu.Lock()
	cfg, e := a.xiaomiModelConfigLocked()
	a.mu.Unlock()
	if e != nil || cfg.APIKey != "synthetic-isolated" {
		t.Fatal(e)
	}
	u := TokenUsage{Model: cfg.Model, Provider: cfg.BaseURL}
	applyBillingIdentity(&u, cfg)
	if u.ProviderID != "" || u.KeyID != "" {
		t.Fatal("isolated credential misattributed to global key", u)
	}
}

func TestProviderBillingManualPriceRequiresExplicitPair(t *testing.T) {
	a := testApp(t)
	for _, body := range []map[string]any{{"model": "m", "priceIn": 1}, {"model": "m", "priceOut": 1}, {"model": "m", "priceIn": nil, "priceOut": 0}, {"model": "m", "priceIn": 0, "priceOut": 0, "scope": "unknown"}, {"model": "m", "priceIn": 0, "priceOut": 0, "scope": "default", "providerId": "p"}} {
		requireStatus(t, request(a, "PUT", "/api/token-pricing", body), 400)
	}
	requireStatus(t, request(a, "PUT", "/api/token-pricing", map[string]any{"model": "m", "priceIn": 0, "priceOut": 0}), 200)
	requireStatus(t, request(a, "PUT", "/api/token-pricing", map[string]any{"model": "m", "clear": true}), 200)
}
func TestProviderBillingCredentialsRejectLockedAndUnauthorized(t *testing.T) {
	a := testApp(t)
	supporterInstall(t, a, "ali")
	a.settings = Settings{Model: "qwen-plus", ModelProviders: []ModelProvider{{ID: "ali", BaseURL: "https://dashscope.aliyuncs.com/compatible-mode/v1"}}, Models: []ModelRef{{ID: "qwen-plus", ProviderID: "ali"}}}
	a.vault.Lock()
	requireStatus(t, request(a, "PUT", "/api/provider-billing-credentials", map[string]any{"providerId": "ali", "accessKeyId": "fake", "accessKeySecret": "fake"}), 423)
	r := httptest.NewRequest("GET", "/api/provider-billing-credentials?providerId=ali", nil)
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, r)
	requireStatus(t, w, 401)
}
