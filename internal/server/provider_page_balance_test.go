package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func pageFixture(t *testing.T, a *App) (ModelProvider, string) {
	t.Helper()
	t.Setenv("AIDE_COMPUTER_BRIDGE_TOKEN", strings.Repeat("t", 64))
	c := supportCatalog{Protocol: 1, ID: "page-fixture", Name: "Page fixture", Revision: "1", Currency: "USD", Providers: []supportProvider{{Host: "api.example.com", Paths: []string{"/v1"}}}, Balance: &supportBalance{Auth: "browser-page", Format: "page-balance", PageURL: "https://billing.example.com/home", Label: "Credit remaining", AccountMarker: "Organization", IdentityLinkPrefix: "https://billing.example.com/settings/account_", RefreshSeconds: 300}}
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
	p := ModelProvider{ID: "official", BaseURL: "https://api.example.com/v1", ActiveKeyID: "first", Keys: []ModelProviderKey{{ID: "first"}, {ID: "second"}}}
	a.settings = Settings{ModelProviders: []ModelProvider{p}}
	supporterVault(t, a, modelProviderVaultID(p.ID, "first"), "synthetic-model-key")
	supporterVault(t, a, modelProviderVaultID(p.ID, "second"), "synthetic-other-key")
	return p, "first"
}
func pageAction(t *testing.T, a *App, action, pending string) map[string]any {
	t.Helper()
	w := request(a, "POST", "/api/provider-page-balance", map[string]any{"providerId": "official", "keyId": "first", "action": action, "pendingId": pending})
	requireStatus(t, w, 200)
	var out map[string]any
	if json.Unmarshal(w.Body.Bytes(), &out) != nil {
		t.Fatal(w.Body.String())
	}
	return out
}
func pageResponse(r *http.Request, account string, amount float64, available bool) *http.Response {
	var in map[string]any
	_ = json.NewDecoder(r.Body).Decode(&in)
	data, _ := json.Marshal(map[string]any{"ok": true, "available": available, "amount": amount, "currency": "USD", "accountLabel": account, "accountIdentity": in["identityLinkPrefix"].(string) + pageHash(account)[:16], "sourceURL": in["url"], "checkedAt": time.Now().UTC().Format(time.RFC3339), "state": "source_closed", "cookie": "PRIVATE"})
	return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(data))), Header: make(http.Header)}
}
func TestPageBalanceConfirmationIsolationPersistenceAndExpiry(t *testing.T) {
	a := testApp(t)
	p, kid := pageFixture(t, a)
	var calls atomic.Int32
	a.pageBalanceClient = &http.Client{Transport: supportRoundTrip(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		if r.URL.Path != "/v1/browser-page-balance" || r.Header.Get("Authorization") != "Bearer "+strings.Repeat("t", 64) {
			t.Error("unexpected native request")
		}
		data, _ := io.ReadAll(r.Body)
		if strings.Contains(string(data), "synthetic-model-key") {
			t.Error("model credential crossed native API")
		}
		r.Body = io.NopCloser(strings.NewReader(string(data)))
		return pageResponse(r, "Synthetic account", 12.5, true), nil
	})}
	before := request(a, "GET", "/api/balance?providerId=official&keyId=first", nil)
	requireStatus(t, before, 200)
	if calls.Load() != 0 || !strings.Contains(before.Body.String(), `"state":"setup"`) {
		t.Fatal(before.Body.String())
	}
	prepared := pageAction(t, a, "prepare", "")
	if !strings.HasPrefix(prepared["openURL"].(string), "https://billing.example.com/home#aide-balance-") {
		t.Fatal(prepared)
	}
	preview := pageAction(t, a, "read", "")
	page := preview["pageBalance"].(map[string]any)
	if page["state"] != "confirm" || len(preview["balance_infos"].([]any)) != 0 {
		t.Fatal(preview)
	}
	pageAction(t, a, "confirm", page["pendingId"].(string))
	result := request(a, "GET", "/api/balance?providerId=official&keyId=first", nil)
	requireStatus(t, result, 200)
	if !strings.Contains(result.Body.String(), `"total_balance":12.5`) || strings.Contains(result.Body.String(), "PRIVATE") || strings.Contains(result.Body.String(), "synthetic-model-key") {
		t.Fatal(result.Body.String())
	}
	other := request(a, "GET", "/api/balance?providerId=official&keyId=second", nil)
	if strings.Contains(other.Body.String(), `"total_balance"`) {
		t.Fatal("cross-key snapshot")
	}
	a.mu.Lock()
	a.pageBalances = nil
	a.loadPageBalancesLocked()
	c, owner := a.providerSupportLocked(p.BaseURL)
	scope, _, _, _ := a.pageTargetLocked(p, kid, c, owner)
	a.pageBalances[scope].Checked = time.Now().Unix() - 1000
	a.mu.Unlock()
	stale := request(a, "GET", "/api/balance?providerId=official&keyId=first", nil)
	if strings.Contains(stale.Body.String(), `"total_balance"`) || !strings.Contains(stale.Body.String(), `"state":"stale"`) {
		t.Fatal(stale.Body.String())
	}
	raw, e := os.ReadFile(a.pageBalanceFile())
	if e != nil || strings.Contains(string(raw), "synthetic-model-key") {
		t.Fatal("persistence leaked model key", e)
	}
}
func TestPageBalanceIdentityChangeAndInflightDisable(t *testing.T) {
	a := testApp(t)
	p, kid := pageFixture(t, a)
	account := "First account"
	a.pageBalanceClient = &http.Client{Transport: supportRoundTrip(func(r *http.Request) (*http.Response, error) { return pageResponse(r, account, 9, true), nil })}
	pageAction(t, a, "prepare", "")
	preview := pageAction(t, a, "read", "")["pageBalance"].(map[string]any)
	pageAction(t, a, "confirm", preview["pendingId"].(string))
	account = "Different account"
	a.readPageBalance(context.Background(), p, kid, true)
	changed := request(a, "GET", "/api/balance?providerId=official&keyId=first", nil)
	if strings.Contains(changed.Body.String(), `"total_balance"`) || !strings.Contains(changed.Body.String(), `"state":"confirm"`) {
		t.Fatal(changed.Body.String())
	}
	started, release := make(chan struct{}), make(chan struct{})
	a.pageBalanceClient = &http.Client{Transport: supportRoundTrip(func(r *http.Request) (*http.Response, error) {
		close(started)
		<-release
		return pageResponse(r, account, 8, true), nil
	})}
	done := make(chan struct{})
	go func() { defer close(done); a.readPageBalance(context.Background(), p, kid, false) }()
	<-started
	pageAction(t, a, "disable", "")
	close(release)
	<-done
	disabled := request(a, "GET", "/api/balance?providerId=official&keyId=first", nil)
	if strings.Contains(disabled.Body.String(), `"total_balance"`) || !strings.Contains(disabled.Body.String(), `"state":"disabled"`) {
		t.Fatal(disabled.Body.String())
	}
}
func TestPageBalanceRejectsUnsafeDeclarations(t *testing.T) {
	valid := supportBalance{Auth: "browser-page", Format: "page-balance", PageURL: "https://billing.example.com/home", Label: "Credit remaining", AccountMarker: "Organization", IdentityLinkPrefix: "https://billing.example.com/settings/account_", RefreshSeconds: 300}
	if e := validatePageBalance(&valid); e != nil {
		t.Fatal(e)
	}
	for _, source := range []string{"http://billing.example.com/home", "https://127.0.0.1/home", "https://example.local/home", "https://user@billing.example.com/home", "https://billing.example.com/home?token=x", "https://billing.example.com/home#x", "https://billing.example.com:444/home"} {
		b := valid
		b.PageURL = source
		if validatePageBalance(&b) == nil {
			t.Fatal("unsafe source", source)
		}
	}
	b := valid
	b.RefreshSeconds = 1
	if validatePageBalance(&b) == nil {
		t.Fatal("fast background reload accepted")
	}
}

func TestPageBalanceBackgroundRefreshNeedsConsentAndStopsWithPlugin(t *testing.T) {
	a := testApp(t)
	p, kid := pageFixture(t, a)
	var calls atomic.Int32
	var refreshes atomic.Int32
	a.pageBalanceClient = &http.Client{Transport: supportRoundTrip(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		body, _ := io.ReadAll(r.Body)
		var in map[string]any
		_ = json.Unmarshal(body, &in)
		if in["refresh"] == true {
			refreshes.Add(1)
		}
		r.Body = io.NopCloser(strings.NewReader(string(body)))
		return pageResponse(r, "Synthetic account", 7, true), nil
	})}
	pageAction(t, a, "prepare", "")
	preview := pageAction(t, a, "read", "")["pageBalance"].(map[string]any)
	a.syncDuePageBalances(context.Background())
	if calls.Load() != 1 {
		t.Fatal("unconfirmed balance refreshed")
	}
	pageAction(t, a, "confirm", preview["pendingId"].(string))
	a.mu.Lock()
	c, owner := a.providerSupportLocked(p.BaseURL)
	scope, _, _, _ := a.pageTargetLocked(p, kid, c, owner)
	a.pageBalances[scope].Attempt = time.Now().Unix() - 301
	a.mu.Unlock()
	a.syncDuePageBalances(context.Background())
	if calls.Load() != 2 || refreshes.Load() != 1 {
		t.Fatal("background refresh did not run")
	}
	a.syncDuePageBalances(context.Background())
	if calls.Load() != 2 {
		t.Fatal("refresh ignored interval")
	}
	a.mu.Lock()
	a.pageBalances[scope].Attempt = 0
	a.pluginRegistry.Plugins[len(a.pluginRegistry.Plugins)-1].Enabled = false
	a.mu.Unlock()
	a.syncDuePageBalances(context.Background())
	if calls.Load() != 2 {
		t.Fatal("disabled plugin refreshed")
	}
}

func TestPageBalanceBrowserFixture(t *testing.T) {
	if os.Getenv("AIDE_PAGE_BROWSER") != "1" {
		t.Skip("isolated Safari fixture")
	}
	a := actualQuotaPackageFixture(t)
	a.token = "page-balance-fixture-synthetic"
	supporterVault(t, a, modelProviderVaultID("official", "k"), "synthetic-official-key")
	t.Setenv("AIDE_COMPUTER_BRIDGE_TOKEN", strings.Repeat("t", 64))
	a.pageBalanceClient = &http.Client{Transport: supportRoundTrip(func(r *http.Request) (*http.Response, error) {
		return pageResponse(r, "Synthetic organization", 12.5, true), nil
	})}
	for _, action := range []string{"prepare", "read"} {
		w := request(a, "POST", "/api/provider-page-balance", map[string]string{"providerId": "official", "keyId": "k", "action": action})
		requireStatus(t, w, 200)
	}
	server := &http.Server{Addr: ":18981", Handler: a.Handler(), ReadHeaderTimeout: 5 * time.Second}
	t.Cleanup(func() { server.Close() })
	done := make(chan error, 1)
	go func() { done <- server.ListenAndServe() }()
	t.Log("BROWSER_READY http://127.0.0.1:18981/#token=page-balance-fixture-synthetic")
	select {
	case e := <-done:
		t.Fatal(e)
	case <-time.After(20 * time.Minute):
	}
}

func TestPageBalanceSameNameDifferentProjectAndClosedSource(t *testing.T) {
	a := testApp(t)
	p, kid := pageFixture(t, a)
	identity := "projectFirst0000"
	available := true
	a.pageBalanceClient = &http.Client{Transport: supportRoundTrip(func(r *http.Request) (*http.Response, error) {
		body, _ := io.ReadAll(r.Body)
		r.Body = io.NopCloser(strings.NewReader(string(body)))
		var in map[string]any
		_ = json.Unmarshal(body, &in)
		response := pageResponse(r, "Same display name", 8, available)
		var out map[string]any
		_ = json.NewDecoder(response.Body).Decode(&out)
		out["accountIdentity"] = in["identityLinkPrefix"].(string) + identity
		raw, _ := json.Marshal(out)
		response.Body = io.NopCloser(strings.NewReader(string(raw)))
		return response, nil
	})}
	pageAction(t, a, "prepare", "")
	preview := pageAction(t, a, "read", "")["pageBalance"].(map[string]any)
	pageAction(t, a, "confirm", preview["pendingId"].(string))
	identity = "projectSecond000"
	a.readPageBalance(context.Background(), p, kid, true)
	w := request(a, "GET", "/api/balance?providerId=official&keyId=first", nil)
	if strings.Contains(w.Body.String(), `"total_balance"`) || !strings.Contains(w.Body.String(), `"state":"confirm"`) {
		t.Fatal("same-name project change retained consent", w.Body.String())
	}
	var out map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	pageAction(t, a, "confirm", out["pageBalance"].(map[string]any)["pendingId"].(string))
	available = false
	a.readPageBalance(context.Background(), p, kid, true)
	w = request(a, "GET", "/api/balance?providerId=official&keyId=first", nil)
	if strings.Contains(w.Body.String(), `"total_balance"`) || !strings.Contains(w.Body.String(), `"state":"source_closed"`) {
		t.Fatal("closed source returned cached current money", w.Body.String())
	}
}

func TestPageBalanceKeyRotationInvalidatesBackgroundConsent(t *testing.T) {
	a := testApp(t)
	p, kid := pageFixture(t, a)
	var calls atomic.Int32
	a.pageBalanceClient = &http.Client{Transport: supportRoundTrip(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		return pageResponse(r, "Synthetic account", 8, true), nil
	})}
	pageAction(t, a, "prepare", "")
	preview := pageAction(t, a, "read", "")["pageBalance"].(map[string]any)
	pageAction(t, a, "confirm", preview["pendingId"].(string))
	a.mu.Lock()
	c, owner := a.providerSupportLocked(p.BaseURL)
	scope, _, _, _ := a.pageTargetLocked(p, kid, c, owner)
	a.pageBalances[scope].Attempt = 0
	a.mu.Unlock()
	supporterVault(t, a, modelProviderVaultID(p.ID, kid), "synthetic-rotated-key")
	a.syncDuePageBalances(context.Background())
	w := request(a, "GET", "/api/balance?providerId=official&keyId=first", nil)
	if calls.Load() != 1 || strings.Contains(w.Body.String(), `"total_balance"`) || !strings.Contains(w.Body.String(), `"state":"setup"`) {
		t.Fatal("rotated key reused website consent", w.Body.String())
	}
}
