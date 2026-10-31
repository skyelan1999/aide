package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func quotaFixture(t *testing.T, a *App, base string) {
	t.Helper()
	u, _ := url.Parse(base)
	c := supportCatalog{Protocol: 1, ID: "quota-fixture", Name: "Fixture quota", Revision: "test1", Providers: []supportProvider{{Host: u.Hostname(), Scheme: u.Scheme, Port: u.Port(), Paths: []string{"/v1"}}}, Balance: &supportBalance{Path: "/v1/usage", Auth: "model-bearer", Format: "quota-windows"}}
	raw, _ := json.Marshal(c)
	if _, e := validateSupportCatalog(raw); e != nil {
		t.Fatal(e)
	}
	dir := filepath.Join(a.pluginsPath, c.ID)
	if e := os.MkdirAll(dir, 0700); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(filepath.Join(dir, "provider-support.json"), raw, 0600); e != nil {
		t.Fatal(e)
	}
	a.pluginRegistry.Plugins = append(a.pluginRegistry.Plugins, PluginManifest{ID: c.ID, Enabled: true, ProviderSupport: &PluginProviderSupport{Protocol: 1, Catalog: "provider-support.json"}})
	a.settings = Settings{ModelProviders: []ModelProvider{{ID: "local", BaseURL: base, ActiveKeyID: "first", Keys: []ModelProviderKey{{ID: "first"}, {ID: "second"}}}}}
	supporterVault(t, a, modelProviderVaultID("local", "second"), "synthetic-selected-key")
}

func TestPluginQuotaSelectedOriginKeyAndAllowlist(t *testing.T) {
	a := testApp(t)
	calls := 0
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != "GET" || r.URL.Path != "/v1/usage" || r.Header.Get("Authorization") != "Bearer synthetic-selected-key" {
			t.Error("wrong quota request")
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"available":true,"buckets":[{"id":"shared","label":"Shared","primary":{"usedPercent":25,"windowDurationMins":300,"resetsAt":1793450000},"secondary":{"usedPercent":10,"windowDurationMins":10080}}],"email":"PRIVATE","token":"PRIVATE"}`))
	}))
	defer s.Close()
	quotaFixture(t, a, s.URL+"/v1")
	w := request(a, "GET", "/api/balance?providerId=local&keyId=second", nil)
	requireStatus(t, w, 200)
	for _, want := range []string{`"kind":"quota"`, `"remainingPercent":75`, `"remainingPercent":90`, `"keyId":"second"`, `"supported":true`} {
		if !strings.Contains(w.Body.String(), want) {
			t.Fatal(w.Body.String())
		}
	}
	if calls != 1 || strings.Contains(w.Body.String(), "PRIVATE") || strings.Contains(w.Body.String(), "synthetic-") {
		t.Fatal("quota leaked or unexpected calls")
	}
	if a.providerQuote(s.URL+"/v1", "any-model", 100, time.Now()) != nil {
		t.Fatal("quota-only plugin supplied pricing")
	}
	a.pluginRegistry.Plugins[len(a.pluginRegistry.Plugins)-1].Enabled = false
	requireStatus(t, request(a, "GET", "/api/balance?providerId=local&keyId=second", nil), 200)
	if calls != 1 {
		t.Fatal("disabled quota plugin queried network")
	}
}

func TestPluginQuotaLocalOriginAndPathBounds(t *testing.T) {
	valid := supportCatalog{Protocol: 1, ID: "fixture", Name: "Quota", Revision: "1", Providers: []supportProvider{{Host: "localhost", Scheme: "http", Port: "8787", Paths: []string{"/v1"}}}, Balance: &supportBalance{Path: "/v1/usage", Auth: "model-bearer", Format: "quota-windows"}}
	raw, _ := json.Marshal(valid)
	if _, e := validateSupportCatalog(raw); e != nil {
		t.Fatal(e)
	}
	for _, base := range []string{"http://localhost:8788/v1", "https://localhost:8787/v1", "http://remote.invalid:8787/v1", "http://localhost:8787/v1?x=y", "http://user@localhost:8787/v1"} {
		if supportMatches(&valid, base) {
			t.Fatal("matched unexpected origin", base)
		}
	}
	for _, path := range []string{"//evil.invalid/usage", "/../usage", "/v1/%2e%2e/usage", "/v1/usage?token=x", "https://evil.invalid/usage"} {
		b := *valid.Balance
		b.Path = path
		if validateSupportQuotaPath(&b) == nil {
			t.Fatal("unsafe quota path accepted")
		}
	}
	for _, p := range []supportProvider{{Host: "remote.invalid", Scheme: "http", Port: "8787"}, {Host: "localhost", Scheme: "http", Port: "0"}, {Host: "localhost", Scheme: "http", Port: "08787"}} {
		if validateLocalSupportProvider(p) == nil {
			t.Fatal("invalid local origin accepted")
		}
	}
}

func TestPluginQuotaInflightDisableRejectsResult(t *testing.T) {
	a := testApp(t)
	started, release := make(chan struct{}), make(chan struct{})
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-release
		w.Write([]byte(`{"available":true,"buckets":[{"id":"q","primary":{"usedPercent":1}}]}`))
	}))
	defer s.Close()
	quotaFixture(t, a, s.URL+"/v1")
	done := make(chan int, 1)
	go func() { done <- request(a, "GET", "/api/balance?providerId=local&keyId=second", nil).Code }()
	<-started
	a.mu.Lock()
	a.pluginRegistry.Plugins[len(a.pluginRegistry.Plugins)-1].Enabled = false
	a.mu.Unlock()
	close(release)
	if code := <-done; code != 409 {
		t.Fatalf("inflight result status %d", code)
	}
}

func TestPluginQuotaUnavailableAndPricingOnly(t *testing.T) {
	out, e := normalizeQuotaWindows([]byte(`{"available":false,"buckets":[{"id":"q","primary":{"usedPercent":0}}],"account":"PRIVATE"}`))
	if e != nil || out["available"] != false || len(out["quota_infos"].([]map[string]any)) != 0 {
		t.Fatal(out, e)
	}
	a := testApp(t)
	manifest := supporterInstall(t, a, "deepseek")
	a.settings = Settings{ModelProviders: []ModelProvider{{ID: "p", BaseURL: "https://api.deepseek.com", Keys: []ModelProviderKey{{ID: "k"}}}}}
	// Remove only the optional balance declaration from a genuine pricing fixture.
	p := filepath.Join(a.pluginsPath, manifest.ID, "provider-support.json")
	b, e := os.ReadFile(p)
	if e != nil {
		t.Fatal(e)
	}
	var c map[string]any
	json.Unmarshal(b, &c)
	delete(c, "balance")
	b, _ = json.Marshal(c)
	if e := os.WriteFile(p, b, 0600); e != nil {
		t.Fatal(e)
	}
	w := request(a, "GET", "/api/balance?providerId=p&keyId=k", nil)
	requireStatus(t, w, 200)
	if !strings.Contains(w.Body.String(), `"pricingSupported":true`) {
		t.Fatal(w.Body.String())
	}
}

func TestPluginQuotaLegacyMalformedAndRedirect(t *testing.T) {
	for _, mode := range []string{"legacy", "malformed", "redirect"} {
		t.Run(mode, func(t *testing.T) {
			a := testApp(t)
			forwarded := false
			target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { forwarded = true }))
			defer target.Close()
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch mode {
				case "legacy":
					w.WriteHeader(404)
				case "malformed":
					w.Write([]byte(`{"available":true,"buckets":[{"id":"x","primary":{"usedPercent":"wrong"}}]}`))
				case "redirect":
					http.Redirect(w, r, target.URL, 302)
				}
			}))
			defer s.Close()
			quotaFixture(t, a, s.URL+"/v1")
			w := request(a, "GET", "/api/balance?providerId=local&keyId=second", nil)
			if mode == "legacy" {
				requireStatus(t, w, 200)
				if !strings.Contains(w.Body.String(), `"upgradeRequired":true`) {
					t.Fatal(w.Body.String())
				}
			} else {
				requireStatus(t, w, 502)
			}
			if forwarded {
				t.Fatal("quota redirect forwarded credentials")
			}
		})
	}
}
