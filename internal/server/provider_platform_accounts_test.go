package server

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func installTestPlatformAccount(t *testing.T, a *App, adapter, host string) {
	t.Helper()
	id := "fixture-" + adapter
	catalog := supportCatalog{
		Protocol: 1, ID: id, Name: "Fixture " + adapter, Revision: "test", Currency: "CNY",
		AccountBalance: adapter,
		Providers:      []supportProvider{{Host: host, Paths: []string{"/v1"}}},
		Models:         []supportModel{{Names: []string{"fixture-model"}, Tiers: []supportTier{{PriceIn: 2, PriceOut: 6}}}},
	}
	raw, err := json.Marshal(catalog)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(a.pluginsPath, id)
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "provider-support.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	a.pluginRegistry.Plugins = append(a.pluginRegistry.Plugins, PluginManifest{
		ID: id, Name: catalog.Name, Enabled: true,
		ProviderSupport: &PluginProviderSupport{Protocol: 1, Catalog: "provider-support.json"},
	})
}

func testPlatformAccounts(t *testing.T) *App {
	t.Helper()
	a := testApp(t)
	installTestPlatformAccount(t, a, "xiaomi-mimo", "api.xiaomimimo.com")
	installTestPlatformAccount(t, a, "moonshot", "api.moonshot.cn")
	a.settings.ModelProviders = []ModelProvider{{ID: "p", ActiveKeyID: "current",
		Keys: []ModelProviderKey{{ID: "current"}, {ID: "requested"}}}}
	return a
}

func replacePlatformAccountTransport(t *testing.T, transport supportRoundTrip) {
	t.Helper()
	previous := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = previous })
	http.DefaultTransport = transport
}

func TestPlatformAccountOfficialRequestsAndResults(t *testing.T) {
	const mimoBalance = `{"object":"balance","is_available":true,"balance_infos":[{"currency":"USD","cash_balance":"12.25","gift_balance":"3.50","frozen_balance":"1.25","total_available":"15.75","internal":"do-not-reflect"}]}`
	const tokenPlan = `{"object":"token_plan","period":{"end_at":"2026-12-18 08:00:00"},"quota":{"remaining_credits":1750,"total_credits":2500},"account_token":"do-not-reflect"}`
	const kimiBalance = `{"code":0,"status":true,"data":{"available_balance":10.5,"voucher_balance":2.25,"cash_balance":8.25,"internal":"do-not-reflect"}}`
	cases := []struct {
		name, base, endpoint, body, key, currency, total, cash, grant string
	}{
		{"mimo-metered", "https://api.xiaomimimo.com/v1", "https://api.xiaomimimo.com/v1/platform/balance", mimoBalance, "sk-synthetic-metered", "USD", "14.50", "12.25", "3.50"},
		{"mimo-token-plan-cn", "https://token-plan-cn.xiaomimimo.com/v1", "https://token-plan-cn.xiaomimimo.com/v1/platform/token_plan", tokenPlan, "tp-synthetic-cn", "", "", "", ""},
		{"mimo-token-plan-sgp", "https://token-plan-sgp.xiaomimimo.com/v1", "https://token-plan-sgp.xiaomimimo.com/v1/platform/token_plan", tokenPlan, "tp-synthetic-sgp", "", "", "", ""},
		{"mimo-token-plan-ams", "https://token-plan-ams.xiaomimimo.com/v1", "https://token-plan-ams.xiaomimimo.com/v1/platform/token_plan", tokenPlan, "tp-synthetic-ams", "", "", "", ""},
		{"kimi-metered", "https://api.moonshot.cn/v1", "https://api.moonshot.cn/v1/users/me/balance", kimiBalance, "sk-synthetic-kimi", "CNY", "10.5", "8.25", "2.25"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := testPlatformAccounts(t)
			a.settings.ModelProviders[0].BaseURL = tc.base
			supporterVault(t, a, modelProviderVaultID("p", "current"), "synthetic-unselected-key")
			supporterVault(t, a, modelProviderVaultID("p", "requested"), tc.key)
			calls := 0
			replacePlatformAccountTransport(t, func(r *http.Request) (*http.Response, error) {
				calls++
				if r.Method != http.MethodGet || r.URL.String() != tc.endpoint || r.Header.Get("Authorization") != "Bearer "+tc.key {
					t.Fatal("official balance URL or selected key did not match")
				}
				return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(tc.body))}, nil
			})

			w := request(a, "GET", "/api/balance?providerId=p&keyId=requested", nil)
			requireStatus(t, w, http.StatusOK)
			if calls != 1 || strings.Contains(w.Body.String(), tc.key) ||
				strings.Contains(w.Body.String(), "synthetic-unselected-key") || strings.Contains(w.Body.String(), "do-not-reflect") {
				t.Fatal("response leaked raw data or did not query exactly once")
			}
			var out struct {
				Supported    bool                `json:"supported"`
				ProviderID   string              `json:"providerId"`
				KeyID        string              `json:"keyId"`
				Kind         string              `json:"kind"`
				BalanceScope string              `json:"balanceScope"`
				BalanceAuth  string              `json:"balanceAuth"`
				BalanceInfos []map[string]string `json:"balance_infos"`
				QuotaInfos   []struct {
					RemainingPercent float64 `json:"remainingPercent"`
					RemainingCredits int64   `json:"remainingCredits"`
					TotalCredits     int64   `json:"totalCredits"`
				} `json:"quota_infos"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
				t.Fatal(err)
			}
			if !out.Supported || out.ProviderID != "p" || out.KeyID != "requested" || out.BalanceScope != "account" || out.BalanceAuth != "model-bearer" {
				t.Fatalf("wrong account scope: %+v", out)
			}
			quote := a.providerQuote(tc.base, "fixture-model", 1, time.Now())
			if tc.currency == "" {
				if out.Kind != "quota" || len(out.BalanceInfos) != 0 || len(out.QuotaInfos) != 1 || quote != nil {
					t.Fatalf("Token Plan exposed a metered balance or price: %+v, quote=%v", out, quote)
				}
				quota := out.QuotaInfos[0]
				if quota.RemainingCredits != 1750 || quota.TotalCredits != 2500 || quota.RemainingPercent != 70 {
					t.Fatalf("wrong Token Plan credits: %+v", quota)
				}
			} else {
				if len(out.BalanceInfos) != 1 || len(out.QuotaInfos) != 0 || quote == nil {
					t.Fatalf("missing metered balance or price: %+v, quote=%v", out, quote)
				}
				balance := out.BalanceInfos[0]
				if balance["currency"] != tc.currency || balance["total_balance"] != tc.total ||
					balance["available_cash_balance"] != tc.cash || balance["granted_balance"] != tc.grant {
					t.Fatalf("wrong account balance: %+v", balance)
				}
			}
		})
	}
}

func TestPlatformAccountRejectsUnsupportedOriginsAndBrowserOverride(t *testing.T) {
	a := testPlatformAccounts(t)
	supporterVault(t, a, modelProviderVaultID("p", "current"), "tp-synthetic-private")
	replacePlatformAccountTransport(t, func(*http.Request) (*http.Response, error) {
		t.Fatal("unsupported origin or browser override made a network request")
		return nil, nil
	})

	for _, base := range []string{
		"http://api.xiaomimimo.com/v1",
		"https://api.xiaomimimo.com:8443/v1",
		"https://api.xiaomimimo.com.example.invalid/v1",
		"https://api.xiaomimimo.com/v1?next=elsewhere",
		"https://token-plan-cn.xiaomimimo.com/private/v1",
		"https://user@token-plan-cn.xiaomimimo.com/v1",
		"https://token-plan-cn.xiaomimimo.com.example.invalid/v1",
		"https://token-plan-other.xiaomimimo.com/v1",
		"https://api.moonshot.cn.example.invalid/v1",
		"https://api.moonshot.cn/v1#fragment",
	} {
		a.settings.ModelProviders[0].BaseURL = base
		w := request(a, "GET", "/api/balance?providerId=p", nil)
		requireStatus(t, w, http.StatusOK)
		if !strings.Contains(w.Body.String(), `"supported":false`) || strings.Contains(w.Body.String(), "tp-synthetic-private") {
			t.Fatalf("unsupported base was not safely ignored: %s", base)
		}
	}

	a.settings.ModelProviders[0].BaseURL = "https://token-plan-cn.xiaomimimo.com/v1"
	w := request(a, "POST", "/api/provider-page-balance", map[string]any{
		"providerId": "p", "keyId": "current", "action": "prepare",
		"pageURL": "https://elsewhere.example.invalid/",
	})
	requireStatus(t, w, http.StatusBadRequest)
	if strings.Contains(w.Body.String(), "tp-synthetic-private") {
		t.Fatal("browser route exposed model credentials")
	}

	supporterVault(t, a, modelProviderVaultID("p", "current"), "ttp-synthetic-team")
	w = request(a, "GET", "/api/balance?providerId=p", nil)
	requireStatus(t, w, http.StatusOK)
	if !strings.Contains(w.Body.String(), `"supported":false`) || strings.Contains(w.Body.String(), "ttp-synthetic-team") {
		t.Fatal("unsupported team key was treated like a personal Token Plan key")
	}
}

func TestPlatformAccountRejectsMalformedRepliesWithoutReflectingUpstream(t *testing.T) {
	a := testPlatformAccounts(t)
	const secret = "sk-synthetic-secret"
	supporterVault(t, a, modelProviderVaultID("p", "current"), secret)
	for _, tc := range []struct {
		name, base, body string
		status           int
	}{
		{"balance-amounts", "https://api.xiaomimimo.com/v1", `{"object":"balance","is_available":true,"balance_infos":[{"currency":"CNY","cash_balance":"sk-synthetic-secret","gift_balance":"0","frozen_balance":"0","total_available":"NaN"}]}`, http.StatusOK},
		{"quota-overflow", "https://token-plan-cn.xiaomimimo.com/v1", `{"object":"token_plan","quota":{"remaining_credits":15,"total_credits":10},"echo":"sk-synthetic-secret"}`, http.StatusOK},
		{"kimi-missing-voucher", "https://api.moonshot.cn/v1", `{"code":0,"status":true,"data":{"available_balance":7,"cash_balance":6,"echo":"sk-synthetic-secret"}}`, http.StatusOK},
		{"upstream-auth", "https://api.xiaomimimo.com/v1", `{"error":"sk-synthetic-secret"}`, http.StatusUnauthorized},
		{"redirect", "https://api.xiaomimimo.com/v1", `{"error":"sk-synthetic-secret"}`, http.StatusFound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a.settings.ModelProviders[0].BaseURL = tc.base
			calls := 0
			replacePlatformAccountTransport(t, func(r *http.Request) (*http.Response, error) {
				calls++
				header := make(http.Header)
				header.Set("Location", "https://elsewhere.example.invalid/balance")
				return &http.Response{StatusCode: tc.status, Header: header, Body: io.NopCloser(strings.NewReader(tc.body))}, nil
			})
			w := request(a, "GET", "/api/balance?providerId=p", nil)
			requireStatus(t, w, http.StatusBadGateway)
			if calls != 1 || strings.Contains(w.Body.String(), secret) || strings.Contains(w.Body.String(), "elsewhere.example.invalid") {
				t.Fatal("invalid response repeated the request, followed a redirect, or echoed upstream details")
			}
		})
	}
}
