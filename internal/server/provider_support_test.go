package server

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"mime/multipart"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// Normal runs use the public source snapshots. The acceptance run supplies the
// exact locally built ZIPs, so the production parser/uploader sees shipping bytes.
func supporterBundle(t *testing.T, name string) []byte {
	t.Helper()
	if dir := os.Getenv("AIDE_SUPPORTER_ZIP_DIR"); dir != "" {
		b, e := os.ReadFile(filepath.Join(dir, name+".zip"))
		if e != nil {
			t.Fatal(e)
		}
		return b
	}
	var b bytes.Buffer
	z := zip.NewWriter(&b)
	for _, file := range []string{"manifest.json", "index.js", "provider-support.json"} {
		raw, e := os.ReadFile(filepath.Join("testdata", "provider-support", name, file))
		if e != nil {
			t.Fatal(e)
		}
		h := &zip.FileHeader{Name: file, Method: zip.Deflate}
		h.SetMode(0644)
		w, e := z.CreateHeader(h)
		if e != nil {
			t.Fatal(e)
		}
		w.Write(raw)
	}
	if e := z.Close(); e != nil {
		t.Fatal(e)
	}
	return b.Bytes()
}
func supporterInstall(t *testing.T, a *App, name string) PluginManifest {
	t.Helper()
	raw := supporterBundle(t, name)
	m, _, e := parsePluginBundle(raw)
	if e != nil {
		t.Fatal(e)
	}
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	w, e := form.CreateFormFile("bundle", name+".zip")
	if e != nil {
		t.Fatal(e)
	}
	w.Write(raw)
	form.Close()
	r := httptest.NewRequest("POST", "/api/plugins/bundle", &body)
	r.Header.Set("Content-Type", form.FormDataContentType())
	r.Header.Set("Authorization", "Bearer "+a.token)
	response := httptest.NewRecorder()
	a.Handler().ServeHTTP(response, r)
	requireStatus(t, response, 201)
	return m
}
func supporterAt(t *testing.T, s string) time.Time {
	t.Helper()
	v, e := time.Parse(time.RFC3339, s)
	if e != nil {
		t.Fatal(e)
	}
	return v
}
func supporterLast(a *App) TokenCallRec {
	a.tokenStatsMu.Lock()
	defer a.tokenStatsMu.Unlock()
	return a.tokenCalls[len(a.tokenCalls)-1]
}
func TestProviderSupportActualPackagesEnabledDisabledRemoved(t *testing.T) {
	a := testApp(t)
	d := supporterInstall(t, a, "deepseek")
	supporterInstall(t, a, "ali")
	at := supporterAt(t, "2026-10-22T09:00:00+08:00")
	if q := a.providerQuote("https://api.deepseek.com/v1", "deepseek-flash", 100, at); q == nil || q.Source != d.ID || q.Tier.PriceIn != 2 {
		t.Fatalf("installed catalog not used: %+v", q)
	}
	if q := a.providerQuote("https://dashscope.aliyuncs.com/compatible-mode/v1", "qwen-plus", 128001, at); q == nil || q.Tier.PriceOut != 20 {
		t.Fatalf("second installed catalog not used: %+v", q)
	}
	for i := range a.pluginRegistry.Plugins {
		if a.pluginRegistry.Plugins[i].ID == d.ID {
			a.pluginRegistry.Plugins[i].Enabled = false
		}
	}
	if a.providerQuote("https://api.deepseek.com", "deepseek-flash", 1, at) != nil {
		t.Fatal("disabled extension still applied")
	}
	a.settings.Model = "deepseek-flash"
	a.settings.BaseURL = "https://api.deepseek.com"
	requireStatus(t, request(a, "PUT", "/api/token-pricing", map[string]any{"model": "deepseek-flash", "priceIn": 7, "priceOut": 11}), 200)
	a.recordTokenUsage(TokenUsage{Provider: a.settings.BaseURL, Model: a.settings.Model, Prompt: 1000000, Total: 1000000})
	if got := supporterLast(a); got.RateSource != "custom" || got.PriceIn != 7 || got.Cost != 7 {
		t.Fatalf("manual fallback lost: %+v", got)
	}
	os.RemoveAll(filepath.Join(a.pluginsPath, d.ID))
	if a.providerQuote(a.settings.BaseURL, a.settings.Model, 1, at) != nil {
		t.Fatal("removed package still applied")
	}
	var out map[string]any
	response := request(a, "GET", "/api/token-pricing", nil)
	requireStatus(t, response, 200)
	json.Unmarshal(response.Body.Bytes(), &out)
	if out["official"] != false {
		t.Fatal("no plugin reported official", out)
	}
}
func TestProviderSupportExactHostPathAndModel(t *testing.T) {
	a := testApp(t)
	supporterInstall(t, a, "deepseek")
	at := time.Now()
	for _, base := range []string{"https://api.deepseek.com", "https://api.deepseek.com/v1", "https://api.deepseek.com/anthropic", "https://api.deepseek.com:443/v1"} {
		if a.providerQuote(base, "deepseek-flash", 1, at) == nil {
			t.Fatal("official path rejected", base)
		}
	}
	for _, base := range []string{"http://api.deepseek.com", "https://api.deepseek.com.evil.invalid/v1", "https://proxy.invalid/v1", "https://api.deepseek.com:8443/v1", "https://user@api.deepseek.com/v1", "https://api.deepseek.com/v1?key=not-secret", "https://api.deepseek.com/v1#fragment", "https://api.deepseek.com/v10", "https://api.deepseek.com/v1/extra"} {
		if a.providerQuote(base, "deepseek-flash", 1, at) != nil {
			t.Fatal("non-official endpoint accepted", base)
		}
	}
	for _, name := range []string{"deepseek-flash-custom", "not-deepseek-flash", "deepseek-chat", "deepseek-v4-pro-preview"} {
		if a.providerQuote("https://api.deepseek.com", name, 1, at) != nil {
			t.Fatal("unknown model guessed", name)
		}
	}
}
func TestProviderSupportMalformedEscapingAndAmbiguousCatalogs(t *testing.T) {
	a := testApp(t)
	m := supporterInstall(t, a, "deepseek")
	path := filepath.Join(a.pluginsPath, m.ID, "provider-support.json")
	original, e := os.ReadFile(path)
	if e != nil {
		t.Fatal(e)
	}
	for _, raw := range [][]byte{[]byte("{"), []byte(`{"protocol":99}`), bytes.Repeat([]byte(" "), (128<<10)+1)} {
		if e = os.WriteFile(path, raw, 0600); e != nil {
			t.Fatal(e)
		}
		if a.providerQuote("https://api.deepseek.com", "deepseek-flash", 1, time.Now()) != nil {
			t.Fatal("malformed catalog applied")
		}
	}
	outside := filepath.Join(t.TempDir(), "external.json")
	os.WriteFile(outside, original, 0600)
	os.Remove(path)
	if e = os.Symlink(outside, path); e != nil {
		t.Fatal(e)
	}
	if a.providerQuote("https://api.deepseek.com", "deepseek-flash", 1, time.Now()) != nil {
		t.Fatal("catalog symlink escaped plugin root")
	}
	os.Remove(path)
	os.WriteFile(path, original, 0600)
	copyManifest := m
	copyManifest.ID = "deepseek-copy"
	copyManifest.Enabled = true
	copyDir := filepath.Join(a.pluginsPath, copyManifest.ID)
	os.MkdirAll(copyDir, 0700)
	os.WriteFile(filepath.Join(copyDir, "provider-support.json"), original, 0600)
	a.pluginRegistry.Plugins = append(a.pluginRegistry.Plugins, copyManifest)
	if a.providerQuote("https://api.deepseek.com", "deepseek-flash", 1, time.Now()) != nil {
		t.Fatal("ambiguous catalogs silently selected one")
	}
}
func TestProviderSupportCatalogValidationRejectsInvalidPricesTiersAndBalanceHosts(t *testing.T) {
	_, files, e := parsePluginBundle(supporterBundle(t, "deepseek"))
	if e != nil {
		t.Fatal(e)
	}
	for _, mutate := range []func(*supportCatalog){func(c *supportCatalog) { c.Models[0].Tiers[0].PriceIn = -1 }, func(c *supportCatalog) { c.Models = append(c.Models, c.Models[0]) }, func(c *supportCatalog) { c.Models[0].Tiers = append(c.Models[0].Tiers, supportTier{MaxInput: 100}) }, func(c *supportCatalog) { c.Balance.Endpoint = "https://attacker.invalid/user/balance" }, func(c *supportCatalog) { c.Balance.Endpoint = "http://api.deepseek.com/user/balance" }, func(c *supportCatalog) { c.Peak.Hours = [][2]int{{18, 9}} }} {
		var c supportCatalog
		json.Unmarshal(files["provider-support.json"], &c)
		mutate(&c)
		raw, _ := json.Marshal(c)
		if _, e := validateSupportCatalog(raw); e == nil {
			t.Fatalf("invalid declaration accepted: %s", raw)
		}
	}
}
func TestProviderSupportDeepSeekPeakCacheAndCalendarEstimate(t *testing.T) {
	a := testApp(t)
	supporterInstall(t, a, "deepseek")
	for _, tc := range []struct {
		date            string
		peak, estimated bool
	}{{"2026-10-22T08:59:59+08:00", false, false}, {"2026-10-22T09:00:00+08:00", true, false}, {"2026-10-22T12:00:00+08:00", false, false}, {"2026-10-22T14:00:00+08:00", true, false}, {"2026-10-22T18:00:00+08:00", false, false}, {"2026-10-01T10:00:00+08:00", false, false}, {"2026-10-10T10:00:00+08:00", false, false}, {"2027-01-01T10:00:00+08:00", true, true}} {
		q := a.providerQuote("https://api.deepseek.com", "deepseek-v4-flash", 1, supporterAt(t, tc.date))
		if q == nil || q.Peak != tc.peak || q.Estimated != tc.estimated {
			t.Fatalf("calendar %s: %+v", tc.date, q)
		}
		hit := .02
		if tc.peak {
			hit = .04
		}
		if q.Tier.PriceInCacheHit == nil || *q.Tier.PriceInCacheHit != hit {
			t.Fatalf("cache multiplier wrong: %+v", q)
		}
	}
	a.recordTokenUsage(TokenUsage{Provider: "https://api.deepseek.com", Model: "deepseek-flash", Prompt: 1000, Completion: 100, Total: 1100, CacheKnown: true, CacheHit: 800, CacheMiss: 200})
	rec := supporterLast(a)
	want := (800*rec.PriceInCacheHit + 200*rec.PriceIn + 100*rec.PriceOut) / 1e6
	if math.Abs(rec.Cost-want) > 1e-12 || !strings.HasPrefix(rec.RateSource, "plugin:deepseek-supporter:") {
		t.Fatalf("cache split charge incorrect: %+v", rec)
	}
	a.recordTokenUsage(TokenUsage{Provider: "https://api.deepseek.com", Model: "deepseek-flash", Prompt: 1000, Total: 1000})
	if rec = supporterLast(a); !rec.CostEstimated || rec.CacheMiss != 1000 {
		t.Fatalf("unknown cache not estimated: %+v", rec)
	}
}
func TestProviderSupportAliWholeRequestTiersRegionAndReasoning(t *testing.T) {
	a := testApp(t)
	supporterInstall(t, a, "ali")
	base := "https://dashscope.aliyuncs.com/compatible-mode/v1"
	for _, tc := range []struct {
		input int
		out   float64
	}{{128000, 2}, {128001, 20}, {256000, 20}, {256001, 48}} {
		q := a.providerQuote(base, "qwen-plus", tc.input, time.Now())
		if q == nil || q.Tier.PriceOut != tc.out {
			t.Fatalf("tier %+v: %+v", tc, q)
		}
	}
	if a.providerQuote(base, "qwen3.8-flash", 1000001, time.Now()) != nil {
		t.Fatal("over-context tier guessed")
	}
	for _, url := range []string{"https://dashscope-intl.aliyuncs.com/compatible-mode/v1", "https://dashscope-us.aliyuncs.com/compatible-mode/v1"} {
		if a.providerQuote(url, "qwen-plus", 1, time.Now()) != nil {
			t.Fatal("regional price leak", url)
		}
	}
	for _, tc := range []struct {
		known, thinking, estimated bool
		out                        float64
	}{{true, false, false, 48}, {true, true, false, 64}, {false, false, true, 64}} {
		a.recordTokenUsage(TokenUsage{Provider: base, Model: "qwen-plus", Prompt: 256001, Completion: 100, Total: 256101, ReasoningKnown: tc.known, Reasoning: tc.thinking})
		rec := supporterLast(a)
		if rec.PriceOut != tc.out || rec.CostEstimated != tc.estimated {
			t.Fatalf("reasoning price wrong: %+v", rec)
		}
	}
}
func TestProviderSupportScopedPriceWriteAndPersistedKeyIdentity(t *testing.T) {
	a := testApp(t)
	supporterInstall(t, a, "deepseek")
	a.settings = Settings{Model: "deepseek-flash", BaseURL: "https://api.deepseek.com", Models: []ModelRef{{ID: "deepseek-flash", ProviderID: "deep"}}, ModelProviders: []ModelProvider{{ID: "deep", BaseURL: "https://api.deepseek.com", ActiveKeyID: "first", Keys: []ModelProviderKey{{ID: "first", Label: "Key A"}, {ID: "second", Label: "Key B"}}}}}
	requireStatus(t, request(a, "PUT", "/api/token-pricing", map[string]any{"providerId": "deep", "keyId": "first", "model": "deepseek-flash", "priceIn": 7, "priceOut": 11}), 200)
	requireStatus(t, request(a, "PUT", "/api/token-pricing", map[string]any{"providerId": "deep", "keyId": "missing", "model": "deepseek-flash", "priceIn": 1, "priceOut": 2}), 400)
	for _, key := range []string{"first", "second"} {
		cfg := a.settings
		cfg.ModelProviders = append([]ModelProvider(nil), a.settings.ModelProviders...)
		cfg.ModelProviders[0].ActiveKeyID = key
		u := TokenUsage{Provider: cfg.BaseURL, Model: cfg.Model, Prompt: 1000000, Total: 1000000, CacheKnown: true, CacheMiss: 1000000}
		applyBillingIdentity(&u, cfg)
		if u.ProviderID != "deep" || u.KeyID != key || u.KeyLabel == "" {
			t.Fatalf("key identity lost: %+v", u)
		}
		a.recordTokenUsage(u)
		rec := supporterLast(a)
		if !strings.HasPrefix(rec.RateSource, "plugin:") {
			t.Fatalf("plugin must take priority over saved manual rates: %+v", rec)
		}
		if key == "second" && !strings.HasPrefix(rec.RateSource, "plugin:") {
			t.Fatalf("scoped rate leaked to other key: %+v", rec)
		}
	}
	raw, e := os.ReadFile(TokenStatsPath(a.dataPath))
	if e != nil {
		t.Fatal(e)
	}
	var saved struct {
		Calls []TokenCallRec `json:"calls"`
	}
	if e = json.Unmarshal(raw, &saved); e != nil {
		t.Fatal(e)
	}
	if len(saved.Calls) != 2 || saved.Calls[0].KeyID != "first" || saved.Calls[1].KeyID != "second" {
		t.Fatalf("key attribution not persisted: %s", raw)
	}
	if billingScope("a", "b:c", "d") == billingScope("a:b", "c", "d") {
		t.Fatal("scope separator collision")
	}
}
func TestProviderSupportBalanceNormalizationAndSigning(t *testing.T) {
	out, e := normalizeBalance([]byte(`{"is_available":true,"balance_infos":[{"currency":"CNY","total_balance":"12.50","granted_balance":"2.5","topped_up_balance":"10"}]}`), "balance-infos")
	if e != nil || out["is_available"] != true {
		t.Fatal(out, e)
	}
	out, e = normalizeBalance([]byte(`{"Success":true,"Data":{"AvailableAmount":"1,234.50","AvailableCashAmount":"1,000.00","Currency":"CNY"}}`), "aliyun-account-balance")
	if e != nil || len(out["balance_infos"].([]any)) != 1 {
		t.Fatal(out, e)
	}
	for _, tc := range []struct{ raw, format string }{{`{"Success":false}`, "aliyun-account-balance"}, {`{"Success":true,"Data":{"Currency":"USD"}}`, "aliyun-account-balance"}, {`{"balance_infos":[{"currency":"EUR"}]}`, "balance-infos"}, {`{`, "balance-infos"}} {
		if _, e = normalizeBalance([]byte(tc.raw), tc.format); e == nil {
			t.Fatal("invalid balance accepted", tc)
		}
	}
	for _, value := range []string{"NaN", "+Inf", "-Inf"} {
		for _, format := range []string{"balance-infos", "aliyun-account-balance"} {
			var raw string
			if format == "balance-infos" {
				raw = `{"balance_infos":[{"currency":"CNY","total_balance":"` + value + `","granted_balance":"0","topped_up_balance":"0"}]}`
			} else {
				raw = `{"Success":true,"Data":{"Currency":"CNY","AvailableAmount":"` + value + `","AvailableCashAmount":"0"}}`
			}
			if _, err := normalizeBalance([]byte(raw), format); err == nil {
				t.Fatal("nonfinite amount accepted", format, value)
			}
		}
	}
	at := supporterAt(t, "2026-10-22T09:30:00+08:00")
	q := signBalanceRPC("fixture-id", "fixture-secret", at, "fixture-nonce")
	sig := q.Get("Signature")
	q.Del("Signature")
	// Independent Python urllib/HMAC reference vector, not a second copy of the Go signer.
	if sig != "E2CE/qMkut2Y/bHeZ/XddI0Q0b0=" {
		t.Fatal("RPC signature does not match reference vector", sig)
	}
	if q.Get("Timestamp") != "2026-10-22T01:30:00Z" || q.Get("Action") != "QueryAccountBalance" || strings.Contains(q.Encode(), "fixture-secret") {
		t.Fatal("unexpected signed request fields", q)
	}
}

func TestProviderSupportKeyOverridesInheritProviderModelAndAllKeyView(t *testing.T) {
	a := testApp(t)
	supporterInstall(t, a, "deepseek")
	// Manual fallback remains scoped and persistent when the supporter is disabled.
	for i := range a.pluginRegistry.Plugins {
		a.pluginRegistry.Plugins[i].Enabled = false
	}
	a.settings = Settings{Model: "deepseek-flash", BaseURL: "https://api.deepseek.com", Models: []ModelRef{{ID: "deepseek-flash", ProviderID: "deep"}}, ModelProviders: []ModelProvider{{ID: "deep", BaseURL: "https://api.deepseek.com", ActiveKeyID: "first", Keys: []ModelProviderKey{{ID: "first", Label: "A"}, {ID: "second", Label: "B"}}}}}
	write := func(key string, in, out float64) {
		t.Helper()
		requireStatus(t, request(a, "PUT", "/api/token-pricing", map[string]any{"providerId": "deep", "keyId": key, "model": "deepseek-flash", "priceIn": in, "priceOut": out}), 200)
	}
	write("", 3, 5)
	write("first", 7, 11)
	for _, tc := range []struct {
		query, key string
		price      float64
	}{{"&keyId=first", "first", 7}, {"&keyId=second", "second", 3}, {"", "", 3}} {
		w := request(a, "GET", "/api/token-pricing?providerId=deep&model=deepseek-flash"+tc.query, nil)
		requireStatus(t, w, 200)
		var v map[string]any
		json.Unmarshal(w.Body.Bytes(), &v)
		if v["priceIn"] != tc.price || v["official"] != false || v["keyId"] != tc.key {
			t.Fatalf("resolved selection wrong (%s): %s", tc.query, w.Body.String())
		}
	}
	for _, key := range []string{"first", "second"} {
		a.recordTokenUsage(TokenUsage{ProviderID: "deep", KeyID: key, Provider: a.settings.BaseURL, Model: a.settings.Model, Prompt: 1000000, Total: 1000000})
		rec := supporterLast(a)
		want := 3.0
		if key == "first" {
			want = 7
		}
		if rec.Cost != want || rec.RateSource != "custom" {
			t.Fatalf("provider-model inheritance failed: %+v", rec)
		}
	}
	// Free overrides remain explicit, rather than falling back to official prices.
	write("second", 0, 0)
	a.recordTokenUsage(TokenUsage{ProviderID: "deep", KeyID: "second", Provider: a.settings.BaseURL, Model: a.settings.Model, Prompt: 100, Completion: 100, Total: 200})
	if rec := supporterLast(a); rec.Cost != 0 || rec.Defaulted {
		t.Fatalf("zero scoped override lost: %+v", rec)
	}
	raw, e := os.ReadFile(TokenPricingPath(a.dataPath))
	if e != nil {
		t.Fatal(e)
	}
	var saved PricingState
	if e = json.Unmarshal(raw, &saved); e != nil {
		t.Fatal(e)
	}
	if saved.KeyRates[billingScope("deep", "first", "deepseek-flash")].PriceIn != 7 || saved.KeyRates[billingScope("deep", "", "deepseek-flash")].PriceIn != 3 {
		t.Fatal("scoped rates not persisted")
	}
}

func TestProviderSupportBearerBalanceCannotCrossDeclaredProviderHosts(t *testing.T) {
	a := testApp(t)
	m := supporterInstall(t, a, "deepseek")
	path := filepath.Join(a.pluginsPath, m.ID, "provider-support.json")
	raw, e := os.ReadFile(path)
	if e != nil {
		t.Fatal(e)
	}
	var catalog supportCatalog
	if e = json.Unmarshal(raw, &catalog); e != nil {
		t.Fatal(e)
	}
	// Multiple hosts may price separate providers, but one provider's credential
	// must never be sent to another host merely because both appear in a catalog.
	catalog.Providers = append(catalog.Providers, supportProvider{Host: "second-provider.invalid", Paths: []string{""}})
	catalog.Balance.Endpoint = "https://second-provider.invalid/user/balance"
	raw, _ = json.Marshal(catalog)
	if _, e = validateSupportCatalog(raw); e != nil {
		t.Fatal("multi-provider catalog syntax rejected before request guard", e)
	}
	if e = os.WriteFile(path, raw, 0600); e != nil {
		t.Fatal(e)
	}
	a.settings = Settings{Model: "deepseek-flash", BaseURL: "https://api.deepseek.com", Models: []ModelRef{{ID: "deepseek-flash", ProviderID: "deep"}}, ModelProviders: []ModelProvider{{ID: "deep", BaseURL: "https://api.deepseek.com", ActiveKeyID: "first", Keys: []ModelProviderKey{{ID: "first", Label: "A"}}}}}
	requireStatus(t, request(a, "GET", "/api/balance?providerId=deep&keyId=first", nil), 400)
}
func TestProviderSupportQuoteSourceSafeDuringRegistryReplacement(t *testing.T) {
	a := testApp(t)
	m := supporterInstall(t, a, "deepseek")
	index := -1
	var manifest PluginManifest
	for i, p := range a.pluginRegistry.Plugins {
		if p.ID == m.ID {
			index = i
			manifest = p
			break
		}
	}
	if index < 0 {
		t.Fatal("installed extension missing")
	}
	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		for i := 0; i < 100; i++ {
			a.mu.Lock()
			a.pluginRegistry.Plugins[index] = manifest
			a.mu.Unlock()
		}
	}()
	go func() {
		defer wg.Done()
		<-start
		for i := 0; i < 100; i++ {
			q := a.providerQuote("https://api.deepseek.com", "deepseek-flash", 1, time.Now())
			if q == nil || q.Source != m.ID {
				t.Errorf("quote lost stable source: %+v", q)
				return
			}
		}
	}()
	close(start)
	wg.Wait()
}

func TestProviderSupportKeylessLocalProviderRetainsManualPricing(t *testing.T) {
	a := testApp(t)
	a.settings = Settings{Model: "local-model", BaseURL: "http://local.invalid/v1", Models: []ModelRef{{ID: "local-model", ProviderID: "local"}}, ModelProviders: []ModelProvider{{ID: "local", BaseURL: "http://local.invalid/v1"}}}
	requireStatus(t, request(a, "PUT", "/api/token-pricing", map[string]any{"providerId": "local", "model": "local-model", "priceIn": 0, "priceOut": 0}), 200)
	w := request(a, "GET", "/api/token-pricing?providerId=local&model=local-model", nil)
	requireStatus(t, w, 200)
	var v map[string]any
	json.Unmarshal(w.Body.Bytes(), &v)
	if v["priceIn"] != float64(0) || v["priceOut"] != float64(0) || v["official"] != false {
		t.Fatal("keyless free pricing lost", w.Body.String())
	}
}
func TestProviderSupportZeroReasoningCounterDoesNotProveNonThinkingMode(t *testing.T) {
	a := testApp(t)
	supporterInstall(t, a, "ali")
	for _, tokens := range []int{0, 5} {
		var src providerUsage
		raw := []byte(`{"completion_tokens_details":{"reasoning_tokens":` + fmt.Sprint(tokens) + `}}`)
		if e := json.Unmarshal(raw, &src); e != nil {
			t.Fatal(e)
		}
		u := TokenUsage{Provider: "https://dashscope.aliyuncs.com/compatible-mode/v1", Model: "qwen-plus", Prompt: 256001, Completion: 100, Total: 256101}
		applyPromptCacheUsage(&u, src)
		if tokens == 0 && u.ReasoningKnown {
			t.Fatal("zero counter incorrectly proves disabled reasoning")
		}
		if tokens > 0 && (!u.ReasoningKnown || !u.Reasoning) {
			t.Fatal("positive reasoning evidence ignored")
		}
		a.recordTokenUsage(u)
		rec := supporterLast(a)
		if rec.PriceOut != 64 || rec.CostEstimated != (tokens == 0) {
			t.Fatalf("thinking-mode billing not conservative: %+v", rec)
		}
	}
}

func TestProviderSupportPluginPriorityAndManualFallback(t *testing.T) {
	a := testApp(t)
	m := supporterInstall(t, a, "deepseek")
	base, model := "https://api.deepseek.com", "deepseek-flash"
	a.pricing.KeyRates = map[string]Pricing{billingScope("deep", "first", model): {PriceIn: 7, PriceOut: 11}, billingScope("deep", "", model): {PriceIn: 3, PriceOut: 5}}
	usage := func(key string) TokenUsage {
		return TokenUsage{ProviderID: "deep", KeyID: key, Provider: base, Model: model, Prompt: 1000000, Total: 1000000, CacheKnown: true, CacheMiss: 1000000}
	}
	for _, key := range []string{"first", "second"} {
		q := a.providerQuote(base, model, 0, time.Now())
		got := a.resolvedPricing(base, model, "deep", key, false)
		if got["official"] != true || got["priceIn"] != q.Tier.PriceIn {
			t.Fatalf("editor ignored plugin: %+v", got)
		}
		a.recordTokenUsage(usage(key))
		if rec := supporterLast(a); !strings.HasPrefix(rec.RateSource, "plugin:") || rec.PriceIn != q.Tier.PriceIn {
			t.Fatalf("snapshot ignored plugin: %+v", rec)
		}
	}
	old := append([]TokenCallRec(nil), a.tokenCalls...)
	for i := range a.pluginRegistry.Plugins {
		if a.pluginRegistry.Plugins[i].ID == m.ID {
			a.pluginRegistry.Plugins[i].Enabled = false
		}
	}
	for _, tc := range []struct {
		key  string
		rate float64
	}{{"first", 7}, {"second", 3}} {
		got := a.resolvedPricing(base, model, "deep", tc.key, false)
		if got["official"] != false || got["priceIn"] != tc.rate {
			t.Fatalf("manual fallback wrong: %+v", got)
		}
		a.recordTokenUsage(usage(tc.key))
		if rec := supporterLast(a); rec.Cost != tc.rate || rec.RateSource != "custom" {
			t.Fatalf("manual snapshot wrong: %+v", rec)
		}
	}
	for i, rec := range old {
		if a.tokenCalls[i] != rec {
			t.Fatal("historical snapshot changed")
		}
	}
	// An enabled plugin with no matching model must not suppress manual pricing.
	for i := range a.pluginRegistry.Plugins {
		a.pluginRegistry.Plugins[i].Enabled = true
	}
	a.pricing.Rates["unlisted-model"] = Pricing{PriceIn: 9, PriceOut: 13}
	got := a.resolvedPricing(base, "unlisted-model", "deep", "first", false)
	if got["official"] != false || got["priceIn"] != float64(9) {
		t.Fatalf("unmatched model lost manual fallback: %+v", got)
	}
}
