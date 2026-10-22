package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestPluginMarketRefreshFailureVisibleWithoutLosingCache(t *testing.T) {
	a, cached, _ := marketFixture(t, "2.0.0")
	cached.Fetched = time.Now().Unix() - 1200
	cacheMarket(t, a, cached)
	a.pluginUpdateClient = &http.Client{Transport: pluginUpgradeTransport(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 403, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("rate limited"))}, nil
	})}
	r := request(a, "GET", "/api/plugin-market?refresh=1", nil)
	requireStatus(t, r, 200)
	var out struct {
		Revision                     string
		Stale                        bool
		Fetched, Checked, RetryAfter int64
		RefreshError                 string
		Plugins                      []map[string]any
	}
	if err := json.Unmarshal(r.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if !out.Stale || out.Revision != cached.Revision || out.Fetched != cached.Fetched || out.Checked < time.Now().Unix()-5 || out.RetryAfter < 55 || out.RetryAfter > 60 || !strings.Contains(out.RefreshError, "HTTP 403") || len(out.Plugins) != 1 {
		t.Fatalf("lost refresh diagnostics or cached catalog: %s", r.Body)
	}
	// The diagnostic survives a later tab opening without another network request.
	got, stale, err := a.loadMarket(context.Background(), false)
	if err != nil || !stale || got.RefreshError != out.RefreshError || got.Fetched != cached.Fetched {
		t.Fatalf("fallback status not retained: %+v %v %v", got, stale, err)
	}
	// A successful refresh clears failure status and uses the new pinned commit.
	next := strings.Repeat("d", 40)
	raw, _ := json.Marshal(cached.Catalog)
	a.pluginUpdateClient = &http.Client{Transport: pluginUpgradeTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.String() == "https://api.github.com/repos/"+marketRepository+"/git/ref/heads/main" {
			return upgradeResponse([]byte(`{"object":{"sha":"` + next + `"}}`)), nil
		}
		if r.URL.String() != marketRaw+next+"/catalog.json" {
			t.Fatalf("catalog request not revision pinned: %s", r.URL)
		}
		return upgradeResponse(raw), nil
	})}
	got, stale, err = a.loadMarket(context.Background(), true)
	if err != nil || stale || got.Revision != next || got.RefreshError != "" || got.Fetched <= cached.Fetched || got.Checked != got.Fetched {
		t.Fatalf("failed to recover: %+v %v %v", got, stale, err)
	}
}

func TestPluginMarketInvalidRefreshDisclosesValidationFailure(t *testing.T) {
	a, cached, _ := marketFixture(t, "2.0.0")
	next := strings.Repeat("e", 40)
	invalid := cached.Catalog
	invalid.Plugins = append([]marketPlugin{}, invalid.Plugins...)
	invalid.Plugins[0].Package = "../not-a-package.zip"
	raw, _ := json.Marshal(invalid)
	a.pluginUpdateClient = &http.Client{Transport: pluginUpgradeTransport(func(r *http.Request) (*http.Response, error) {
		if strings.Contains(r.URL.Path, "/git/ref/") {
			return upgradeResponse([]byte(`{"object":{"sha":"` + next + `"}}`)), nil
		}
		return upgradeResponse(raw), nil
	})}
	got, stale, err := a.loadMarket(context.Background(), true)
	if err != nil || !stale || got.Revision != cached.Revision || got.RefreshError == "" {
		t.Fatalf("invalid metadata must retain old catalog and explain failure: %+v %v %v", got, stale, err)
	}
	if err := validateMarket(got); err != nil {
		t.Fatal("failed refresh corrupted valid fallback", err)
	}
}

// Optional real public metadata acceptance; no plugins or packages are installed.
// The caller supplies a separately downloaded commit-pinned catalog and SHA.
func TestPluginMarketExternalCatalogMetadata(t *testing.T) {
	file := os.Getenv("AIDE_TEST_PUBLIC_MARKET_CATALOG")
	if file == "" {
		t.Skip("optional public metadata snapshot")
	}
	raw, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	var catalog marketCatalog
	if err = json.Unmarshal(raw, &catalog); err != nil {
		t.Fatal(err)
	}
	s := marketSnapshot{Revision: os.Getenv("AIDE_TEST_PUBLIC_MARKET_REVISION"), Catalog: catalog}
	if err = validateMarket(s); err != nil {
		t.Fatal(err)
	}
	for _, p := range catalog.PrivateListings {
		if p.Package != "" || p.SHA256 != "" || p.Bytes != 0 {
			t.Fatalf("private package exposed: %s", p.ID)
		}
	}
	t.Logf("validated %d public and %d private metadata listings at %s; no packages fetched", len(catalog.Plugins), len(catalog.PrivateListings), s.Revision)
}

func TestPluginMarketScreenshotRepositoryBoundary(t *testing.T) {
	p := marketPlugin{Source: "https://github.com/skyelan1999/aide-pkg-Fixture"}
	commit := strings.Repeat("a", 40)
	pinned := p
	pinned.Source += "/tree/" + commit
	if !marketScreenshotAllowed(pinned, "https://raw.githubusercontent.com/skyelan1999/aide-pkg-Fixture/"+commit+"/docs/shot.png") {
		t.Fatal("rejected screenshot matching source commit")
	}
	if marketScreenshotAllowed(pinned, "https://raw.githubusercontent.com/skyelan1999/aide-pkg-Fixture/"+strings.Repeat("b", 40)+"/docs/shot.png") {
		t.Fatal("accepted screenshot from another source commit")
	}

	for _, image := range []string{"https://raw.githubusercontent.com/skyelan1999/aide-pkg-shop/main/screenshots/fixture.png", "https://raw.githubusercontent.com/skyelan1999/aide-pkg-Fixture/" + commit + "/docs/evidence/light.png"} {
		if !marketScreenshotAllowed(p, image) {
			t.Fatal("rejected official screenshot", image)
		}
	}
	for _, image := range []string{
		"https://raw.githubusercontent.com/another/aide-pkg-Fixture/" + commit + "/shot.png",
		"https://raw.githubusercontent.com/skyelan1999/another-repo/" + commit + "/shot.png",
		"https://raw.githubusercontent.com/skyelan1999/aide-pkg-Fixture/main/shot.png",
		"https://raw.githubusercontent.com.evil.test/skyelan1999/aide-pkg-Fixture/" + commit + "/shot.png",
		"https://user@raw.githubusercontent.com/skyelan1999/aide-pkg-Fixture/" + commit + "/shot.png",
		"https://raw.githubusercontent.com/skyelan1999/aide-pkg-Fixture/" + commit + "/../shot.png",
		"https://raw.githubusercontent.com/skyelan1999/aide-pkg-Fixture/" + commit + "/%2e%2e/shot.png",
		"https://raw.githubusercontent.com/skyelan1999/aide-pkg-Fixture/" + commit + "/shot.png?token=secret",
		"https://raw.githubusercontent.com/skyelan1999/aide-pkg-Fixture/" + commit + "/shot.png#fragment",
		"https://raw.githubusercontent.com:443/skyelan1999/aide-pkg-Fixture/" + commit + "/shot.png",
	} {
		if marketScreenshotAllowed(p, image) {
			t.Fatal("accepted screenshot outside boundary", image)
		}
	}
}

// Optional interactive QA server, bound only to localhost and seeded entirely
// with synthetic listings. It has no production credentials, models or projects.
func TestPluginMarketRefreshBrowserHost(t *testing.T) {
	dir := os.Getenv("AIDE_MARKET_BROWSER_EVIDENCE")
	if dir == "" {
		t.Skip("opt-in interactive marketplace QA")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	a, cached, _ := marketFixture(t, "2.0.0")
	var phase atomic.Int32
	next := strings.Repeat("c", 40)
	a.pluginUpdateClient = &http.Client{Transport: pluginUpgradeTransport(func(r *http.Request) (*http.Response, error) {
		if phase.Load() == 2 {
			return &http.Response{StatusCode: 403, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("offline fixture"))}, nil
		}
		if strings.Contains(r.URL.Path, "/git/ref/") {
			return upgradeResponse([]byte(`{"object":{"sha":"` + next + `"}}`)), nil
		}
		value := cached.Catalog
		value.Plugins = append([]marketPlugin{}, value.Plugins...)
		value.Plugins[0].Version = "3.0.0"
		raw, _ := json.Marshal(value)
		return upgradeResponse(raw), nil
	})}
	mux := http.NewServeMux()
	mux.Handle("/", a.Handler())
	mux.HandleFunc("POST /__market-test/{state}", func(w http.ResponseWriter, r *http.Request) {
		switch r.PathValue("state") {
		case "next":
			phase.Store(1)
		case "offline":
			phase.Store(2)
		case "recover":
			phase.Store(1)
		default:
			http.Error(w, "unknown fixture state", 400)
			return
		}
		a.marketMu.Lock()
		cacheMarket(t, a, marketSnapshot{Revision: cached.Revision, Fetched: 1, Catalog: cached.Catalog})
		a.marketMu.Unlock()
		w.WriteHeader(204)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	stop := filepath.Join(dir, "browser.stop")
	_ = os.Remove(stop)
	if err := atomicJSON(filepath.Join(dir, "browser.json"), map[string]string{"url": srv.URL, "token": a.token}); err != nil {
		t.Fatal(err)
	}
	t.Log("isolated browser fixture ready; metadata stays in private evidence directory")
	deadline := time.Now().Add(8 * time.Minute)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(stop); err == nil {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatal("interactive browser fixture timed out")
}
