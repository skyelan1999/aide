package server

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

func TestMarketSourceCompatibilityKeepsReleaseFloor(t *testing.T) {
	p := marketPlugin{MinAideVersion: "0.1.17.6 RC2", SourceCompatibility: &marketSourceCompatibility{MinAideVersion: "0.1.17.6 RC1", Features: []string{"provider-support-usd-v1"}}}
	for _, tt := range []struct {
		version, mode string
		want          bool
	}{
		{"0.1.17.6 RC1", "release-image", false}, {"0.1.17.6 RC1", "", false},
		{"0.1.17.6 RC1", "source", true}, {"0.1.17.5 RC12", "source", false},
		{"0.1.17.6 RC2", "release-image", true},
	} {
		t.Run(tt.version+tt.mode, func(t *testing.T) {
			if ok, _ := marketCompatibleForRuntime(p, tt.version, tt.mode); ok != tt.want {
				t.Fatalf("compatibility=%v want=%v", ok, tt.want)
			}
		})
	}
	p.SourceCompatibility.Features = []string{"unknown-source-feature"}
	if ok, _ := marketCompatibleForRuntime(p, "0.1.17.6 RC1", "source"); ok {
		t.Fatal("unknown capability accepted")
	}
	p.SourceCompatibility.Features = []string{"provider-support-usd-v1"}
	p.Features = []string{"unknown-base-feature"}
	if ok, _ := marketCompatibleForRuntime(p, "0.1.17.6 RC1", "source"); ok {
		t.Fatal("base capability gate bypassed")
	}
	p.Features = nil
	p.SourceCompatibility = nil
	if ok, _ := marketCompatibleForRuntime(p, "0.1.17.6 RC1", "source"); ok {
		t.Fatal("implicit source exception")
	}
}

func TestMarketSourceCompatibilityCatalogBrowseAndInstall(t *testing.T) {
	for _, mode := range []string{"source", "release-image"} {
		t.Run(mode, func(t *testing.T) {
			a, s, _ := marketFixture(t, "2.0.0")
			a.buildVersion = "0.1.17.6 RC1"
			a.buildRuntimeMode = mode
			s.Catalog.Plugins[0].MinAideVersion = "0.1.17.6 RC2"
			s.Catalog.Plugins[0].SourceCompatibility = &marketSourceCompatibility{MinAideVersion: "0.1.17.6 RC1", Features: []string{"provider-support-usd-v1"}}
			cacheMarket(t, a, s)
			response := request(a, "GET", "/api/plugin-market", nil)
			requireStatus(t, response, 200)
			want := `"compatible":true`
			status := 201
			if mode != "source" {
				want = `"compatible":false`
				status = 409
			}
			if !strings.Contains(response.Body.String(), want) {
				t.Fatal(response.Body.String())
			}
			if mode == "source" && !strings.Contains(response.Body.String(), `"compatibilityMode":"source-capabilities"`) {
				t.Fatal("source compatibility reason omitted")
			}
			task := routingTask(a)
			discovered := a.executeToolCall(context.Background(), readCall("browse_plugin_market", `{"action":"list"}`), task, nil)
			if !strings.Contains(discovered, want) {
				t.Fatal("AI discovery disagrees with catalog:", discovered)
			}
			requireStatus(t, request(a, "POST", "/api/plugin-market/market-fixture/install", marketInput(a, s)), status)
		})
	}
}

func TestMarketSourceCompatibilityMetadataBounds(t *testing.T) {
	a, s, _ := marketFixture(t, "2.0.0")
	_ = a
	for _, c := range []*marketSourceCompatibility{
		{MinAideVersion: "bad", Features: []string{"provider-support-usd-v1"}},
		{MinAideVersion: "0.1.17.6 RC1"},
		{MinAideVersion: "0.1.17.6 RC1", Features: []string{strings.Repeat("x", 81)}},
		{MinAideVersion: "0.1.17.6 RC1", Features: []string{"a", "a"}},
	} {
		s.Catalog.Plugins[0].SourceCompatibility = c
		if validateMarket(s) == nil {
			t.Fatal("invalid source metadata accepted")
		}
	}
}

func sourceMarketActualFixture(t *testing.T) (*App, marketSnapshot) {
	filename := os.Getenv("AIDE_OPENAI_MARKET_ZIP")
	if filename == "" {
		t.Skip("actual immutable package opt-in")
	}
	raw, err := os.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}
	if hash(raw) != "dae0cbbf00cf955df0c086575583e90ccabbc45e72c1a495d27b4dcda71672c1" {
		t.Fatal("actual package digest changed")
	}
	a, s, _ := marketFixture(t, "0.1.0")
	a.buildVersion = "0.1.17.6 RC1"
	a.buildRuntimeMode = "source"
	p := &s.Catalog.Plugins[0]
	p.ID = "openai-supporter"
	p.Name = "OpenAISupporter"
	p.Version = "0.1.0"
	p.Package = "packages/openai-supporter/0.1.0-dae0cbbf00cf.zip"
	p.SHA256 = hash(raw)
	p.Bytes = int64(len(raw))
	p.MinAideVersion = "0.1.17.6 RC2"
	p.SourceCompatibility = &marketSourceCompatibility{MinAideVersion: "0.1.17.6 RC1", Features: []string{"provider-support-usd-v1"}}
	if file := os.Getenv("AIDE_OPENAI_MARKET_LISTING"); file != "" {
		b, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		if err = json.Unmarshal(b, p); err != nil {
			t.Fatal(err)
		}
		if p.SHA256 != hash(raw) || p.Bytes != int64(len(raw)) {
			t.Fatal("listing/package identity mismatch")
		}
	}
	cacheMarket(t, a, s)
	a.pluginUpdateClient = &http.Client{Transport: pluginUpgradeTransport(func(r *http.Request) (*http.Response, error) { return upgradeResponse(raw), nil })}
	return a, s
}

func TestMarketSourceOpenAIActualPackage(t *testing.T) {
	a, s := sourceMarketActualFixture(t)
	requireStatus(t, request(a, "POST", "/api/plugin-market/openai-supporter/install", marketInput(a, s)), 201)
	quote := a.providerQuote("https://api.openai.com/v1", "gpt-6.1-sol", 1000, time.Now())
	if quote == nil || quote.Currency != "USD" {
		t.Fatal("actual USD package not active")
	}
	a.recordTokenUsage(TokenUsage{Provider: "https://api.openai.com/v1", Model: "gpt-6.1-sol", Prompt: 1000, Completion: 1000, Total: 2000})
	if got := supporterLast(a); got.Currency != "USD" || got.Cost <= 0 {
		t.Fatal("actual package USD accounting not applied")
	}
	requireStatus(t, request(a, "PUT", "/api/plugins/openai-supporter", map[string]bool{"enabled": false}), 200)
	if a.providerQuote("https://api.openai.com/v1", "gpt-6.1-sol", 1000, time.Now()) != nil {
		t.Fatal("disabled package still quotes")
	}
}

func TestMarketSourceCompatibilityBrowserFixture(t *testing.T) {
	if os.Getenv("AIDE_SOURCE_MARKET_BROWSER") != "1" {
		t.Skip("isolated browser fixture")
	}
	a, _ := sourceMarketActualFixture(t)
	a.token = "source-market-browser-synthetic"
	server := &http.Server{Addr: ":18979", Handler: a.Handler(), ReadHeaderTimeout: 5 * time.Second}
	t.Cleanup(func() { server.Close() })
	done := make(chan error, 1)
	go func() { done <- server.ListenAndServe() }()
	t.Log("BROWSER_READY http://127.0.0.1:18979/#token=source-market-browser-synthetic")
	select {
	case err := <-done:
		t.Fatal(err)
	case <-time.After(20 * time.Minute):
	}
}
