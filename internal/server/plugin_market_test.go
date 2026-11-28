package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func marketFixture(t *testing.T, version string) (*App, marketSnapshot, []byte) {
	t.Helper()
	a := testApp(t)
	a.buildVersion = "0.1.17.0 RC2"
	code := `module.exports={apply(ctx){ctx.tool({name:'market_fixture_tool',description:'fixture',parameters:{type:'object'},handler:()=>({text:'ok'})});}};`
	raw := upgradePackage(t, "market-fixture", code, nil)
	// upgradePackage always generates 2.0.0; use that version for the signed metadata fixture.
	_ = version
	sum := sha256.Sum256(raw)
	p := marketPlugin{ID: "market-fixture", Kind: "plugin", Name: "Market fixture", Version: "2.0.0", Package: "packages/market-fixture/2.0.0.zip", SHA256: hex.EncodeToString(sum[:]), Bytes: int64(len(raw)), MinAideVersion: "0.1.17.0 RC2", Source: "https://github.com/skyelan1999/aide", Platforms: []string{"macos", "windows", "linux"}}
	snapshot := marketSnapshot{Revision: strings.Repeat("a", 40), Fetched: time.Now().Unix(), Catalog: marketCatalog{SchemaVersion: 1, Plugins: []marketPlugin{p}}}
	cacheMarket(t, a, snapshot)
	a.pluginUpdateClient = &http.Client{Transport: pluginUpgradeTransport(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" {
			t.Error("market forwarded credentials")
		}
		if r.URL.String() == marketRaw+snapshot.Revision+"/"+p.Package {
			return upgradeResponse(raw), nil
		}
		return nil, errors.New("offline fixture")
	})}
	return a, snapshot, raw
}
func cacheMarket(t *testing.T, a *App, s marketSnapshot) {
	t.Helper()
	root, e := a.marketRoot()
	if e != nil {
		t.Fatal(e)
	}
	defer root.Close()
	b, _ := json.Marshal(s)
	if e = skillCacheWrite(root, "catalog.json", b); e != nil {
		t.Fatal(e)
	}
}
func marketInput(a *App, s marketSnapshot) map[string]any {
	p := s.Catalog.Plugins[0]
	return map[string]any{"revision": s.Revision, "version": p.Version, "sha256": p.SHA256, "workspace": a.wsID(), "workspaceRevision": a.wsRevision}
}
func TestOfficialMarketEnvironmentProxyScope(t *testing.T) {
	a := &App{}
	t.Setenv(marketUseEnvProxy, "")
	if tr, ok := a.marketClient().Transport.(*http.Transport); !ok || tr.Proxy != nil {
		t.Fatal("official market must default to its direct transport")
	}
	t.Setenv(marketUseEnvProxy, "1")
	if _, ok := a.marketClient().Transport.(officialMarketTransport); !ok {
		t.Fatal("official market did not opt in to environment proxy")
	}
	if tr, ok := newPluginUpdateClient().Transport.(*http.Transport); !ok || tr.Proxy != nil {
		t.Fatal("market opt-in changed the generic updater")
	}
	directCalls, proxyCalls := 0, 0
	useProxy := true
	transport := officialMarketTransport{
		direct: pluginUpgradeTransport(func(r *http.Request) (*http.Response, error) {
			directCalls++
			return upgradeResponse(nil), nil
		}),
		proxied: pluginUpgradeTransport(func(r *http.Request) (*http.Response, error) {
			proxyCalls++
			return upgradeResponse(nil), nil
		}),
		proxy: func(r *http.Request) (*url.URL, error) {
			if useProxy {
				return &url.URL{Scheme: "http", Host: "proxy.invalid:8080"}, nil
			}
			return nil, nil
		},
	}
	ref := "https://api.github.com/repos/" + marketRepository + "/git/ref/heads/main"
	catalog := marketRaw + strings.Repeat("a", 40) + "/catalog.json"
	bundle := marketRaw + strings.Repeat("a", 40) + "/packages/current-time/1.1.0.zip"
	check := func(raw string, allowed bool) {
		t.Helper()
		req, err := http.NewRequest(http.MethodGet, raw, nil)
		if err != nil {
			t.Fatal(err)
		}
		resp, err := transport.RoundTrip(req)
		if resp != nil {
			resp.Body.Close()
		}
		if (err == nil) != allowed {
			t.Fatalf("market transport decision for %q: %v", raw, err)
		}
	}
	for _, raw := range []string{ref, catalog, bundle} {
		check(raw, true)
	}
	for _, raw := range []string{
		strings.Replace(ref, "https:", "http:", 1),
		strings.Replace(ref, marketRepository, "another/repo", 1),
		strings.Replace(ref, "api.github.com", "api.github.com:443", 1),
		strings.Replace(catalog, "raw.githubusercontent.com", "example.invalid", 1),
		strings.Replace(catalog, strings.Repeat("a", 40), "main", 1),
		catalog + "?unexpected=1",
	} {
		check(raw, false)
	}
	useProxy = false
	check(ref, true)
	if directCalls != 1 || proxyCalls != 3 {
		t.Fatalf("unexpected forward counts: direct=%d proxy=%d", directCalls, proxyCalls)
	}
}

func TestPluginMarketInstallUpdateAndDownload(t *testing.T) {
	a, s, raw := marketFixture(t, "2.0.0")
	id := s.Catalog.Plugins[0].ID
	catalog := request(a, "GET", "/api/plugin-market", nil)
	requireStatus(t, catalog, 200)
	var listed struct {
		Plugins []struct {
			ID          string `json:"id"`
			DownloadURL string `json:"downloadURL"`
		} `json:"plugins"`
	}
	if err := json.Unmarshal(catalog.Body.Bytes(), &listed); err != nil || len(listed.Plugins) != 1 || listed.Plugins[0].ID != id {
		t.Fatalf("unexpected market catalog: %q (%v)", catalog.Body.String(), err)
	}
	p := s.Catalog.Plugins[0]
	query := url.Values{"revision": {s.Revision}, "version": {p.Version}, "sha256": {p.SHA256}}
	if listed.Plugins[0].DownloadURL != "/api/plugin-market/"+id+"/download?"+query.Encode() {
		t.Fatalf("download URL missing review: %s", listed.Plugins[0].DownloadURL)
	}
	result := request(a, "GET", listed.Plugins[0].DownloadURL, nil)
	requireStatus(t, result, 200)
	if string(result.Body.Bytes()) != string(raw) {
		t.Fatal("download content")
	}
	requireStatus(t, request(a, "POST", "/api/plugin-market/"+id+"/install", marketInput(a, s)), 201)
	found := a.pluginRegistry.Plugins[len(a.pluginRegistry.Plugins)-1]
	if !found.Enabled || found.PackageSHA256 != s.Catalog.Plugins[0].SHA256 || found.UpdateURL != marketUpdatePrefix+id {
		t.Fatalf("receipt %+v", found)
	}
	requireStatus(t, request(a, "PUT", "/api/plugins/"+id+"/settings", map[string]any{"settings": map[string]string{"marker": "keep"}}), 200)
	requireStatus(t, request(a, "PUT", "/api/plugins/"+id, map[string]bool{"enabled": false}), 200)
	// Simulate an existing older installation without mutating source files or private data.
	a.mu.Lock()
	for i, p := range a.pluginRegistry.Plugins {
		if p.ID == id {
			a.pluginRegistry.Plugins[i].Version = "1.0.0"
			a.pluginRegistry.Plugins[i].PackageSHA256 = ""
		}
	}
	a.mu.Unlock()
	requireStatus(t, request(a, "POST", "/api/plugin-market/"+id+"/install", marketInput(a, s)), 200)
	for _, p := range a.pluginRegistry.Plugins {
		if p.ID == id {
			if p.Enabled || p.Settings["marker"] != "keep" || p.UpgradeBackup == "" {
				t.Fatalf("state lost %+v", p)
			}
		}
	}
}
func TestPluginMarketRejectsUnreviewedOrRevokedInstall(t *testing.T) {
	for _, scenario := range []string{"hash", "version", "catalog", "workspace", "read-only", "locked", "changed-during-download", "cancelled", "manifest-mismatch", "digest", "load-failure", "busy"} {
		t.Run(scenario, func(t *testing.T) {
			a, s, raw := marketFixture(t, "2.0.0")
			in := marketInput(a, s)
			id := s.Catalog.Plugins[0].ID
			switch scenario {
			case "hash":
				in["sha256"] = strings.Repeat("b", 64)
			case "version":
				in["version"] = "3.0.0"
			case "catalog":
				in["revision"] = strings.Repeat("b", 40)
			case "workspace":
				in["workspace"] = "other"
			case "read-only":
				a.settings.SandboxMode = "read-only"
			case "locked":
				if e := atomicJSON(lockStatePath(a.dataPath), LockStateFile{Locked: true, Gen: 2}); e != nil {
					t.Fatal(e)
				}
			case "busy":
				a.marketInstallMu.Lock()
				defer a.marketInstallMu.Unlock()
			}
			if scenario == "changed-during-download" || scenario == "cancelled" || scenario == "digest" {
				a.pluginUpdateClient = &http.Client{Transport: pluginUpgradeTransport(func(r *http.Request) (*http.Response, error) {
					if scenario == "changed-during-download" {
						a.mu.Lock()
						a.wsRevision++
						a.mu.Unlock()
					}
					if scenario == "cancelled" {
						return nil, context.Canceled
					}
					if scenario == "digest" {
						return upgradeResponse([]byte("tampered")), nil
					}
					return upgradeResponse(raw), nil
				})}
			}
			if scenario == "manifest-mismatch" || scenario == "load-failure" {
				code := `module.exports={apply(){throw Error('broken')}};`
				if scenario == "manifest-mismatch" {
					raw = upgradePackage(t, "foreign", upgradeOldCode, nil)
				} else {
					raw = upgradePackage(t, id, code, nil)
				}
				sum := sha256.Sum256(raw)
				s.Catalog.Plugins[0].SHA256 = hex.EncodeToString(sum[:])
				s.Catalog.Plugins[0].Bytes = int64(len(raw))
				cacheMarket(t, a, s)
				in = marketInput(a, s)
				a.pluginUpdateClient = &http.Client{Transport: pluginUpgradeTransport(func(*http.Request) (*http.Response, error) { return upgradeResponse(raw), nil })}
			}
			w := request(a, "POST", "/api/plugin-market/"+id+"/install", in)
			if w.Code < 400 {
				t.Fatalf("accepted %s: %d %s", scenario, w.Code, w.Body.String())
			}
			for _, p := range a.pluginRegistry.Plugins {
				if p.ID == id {
					t.Fatal("rejected plugin was registered")
				}
			}
			if _, e := os.Stat(filepath.Join(a.pluginsPath, id)); !os.IsNotExist(e) {
				t.Fatal("rejected plugin left installed directory")
			}
		})
	}
}
func TestPluginMarketCatalogIntegrityAndOfflineFallback(t *testing.T) {
	a, s, _ := marketFixture(t, "2.0.0")
	s.Fetched = 1
	cacheMarket(t, a, s)
	got, stale, e := a.loadMarket(context.Background(), true)
	if e != nil || !stale || got.Revision != s.Revision {
		t.Fatalf("offline %v %v", stale, e)
	}
	for _, bad := range []string{"path", "duplicate", "source", "hash", "size", "feature", "min-version"} {
		t.Run(bad, func(t *testing.T) {
			b, _ := json.Marshal(s)
			var invalid marketSnapshot
			_ = json.Unmarshal(b, &invalid)
			p := &invalid.Catalog.Plugins[0]
			switch bad {
			case "path":
				p.Package = "packages/market-fixture/../../secret.zip"
			case "duplicate":
				invalid.Catalog.Plugins = append(invalid.Catalog.Plugins, *p)
			case "source":
				p.Source = "https://github.com.evil.test/skyelan1999/aide"
			case "hash":
				p.SHA256 = "bad"
			case "size":
				p.Bytes = maxPluginBundle + 1
			case "feature":
				p.Features = []string{"future-native"}
				ok, _ := marketCompatible(*p, a.buildVersion)
				if ok {
					t.Fatal("unsupported feature")
				}
				return
			case "min-version":
				p.MinAideVersion = "9.0.0"
				ok, _ := marketCompatible(*p, a.buildVersion)
				if ok {
					t.Fatal("new host required")
				}
				return
			}
			if validateMarket(invalid) == nil {
				t.Fatal("accepted malformed catalog")
			}
		})
	}
	root, e := a.marketRoot()
	if e != nil {
		t.Fatal(e)
	}
	defer root.Close()
	_ = root.Remove("catalog.json")
	got, stale, e = a.loadMarket(context.Background(), true)
	if e != nil || !stale || len(got.Catalog.Plugins) == 0 {
		t.Fatal("bundled offline catalog unavailable", e)
	}
}
func TestPluginMarketNumericVersionCompatibility(t *testing.T) {
	p := marketPlugin{ID: "terminal", Version: "1.6.0", MinAideVersion: "0.1.18.0 RC2", Platforms: []string{"linux"}}
	for _, x := range []struct {
		version string
		want    bool
	}{{"0.1.18.0 RC1", false}, {"0.1.18.0 RC2", true}, {"0.1.18.0 RC10", true}} {
		ok, reason := marketCompatible(p, x.version)
		if ok != x.want {
			t.Fatalf("Terminal paired host %s: got %v, want %v (%s)", x.version, ok, x.want, reason)
		}
	}
	for _, x := range []struct {
		a, b string
		want int
	}{{"0.1.17.0 RC2", "0.1.17.0 RC10", -1}, {"0.1.17.0", "0.1.17.0 RC2", 1}, {"1.10.0", "1.2.0", 1}, {"1.0.0", "1.0.0", 0}} {
		n, ok := marketCompare(x.a, x.b)
		if !ok || n != x.want {
			t.Fatalf("%+v -> %d %v", x, n, ok)
		}
	}
}

func TestPluginMarketRefreshKeepsReviewedDownloadWhenPackageUnchanged(t *testing.T) {
	a, s, raw := marketFixture(t, "2.0.0")
	newRevision := strings.Repeat("b", 40)
	catalog, _ := json.Marshal(s.Catalog)
	seen := []string{}
	a.pluginUpdateClient = &http.Client{Transport: pluginUpgradeTransport(func(r *http.Request) (*http.Response, error) {
		seen = append(seen, r.URL.String())
		switch r.URL.String() {
		case "https://api.github.com/repos/" + marketRepository + "/git/ref/heads/main":
			return upgradeResponse([]byte(`{"object":{"sha":"` + newRevision + `"}}`)), nil
		case marketRaw + newRevision + "/catalog.json":
			return upgradeResponse(catalog), nil
		case marketRaw + newRevision + "/" + s.Catalog.Plugins[0].Package:
			return upgradeResponse(raw), nil
		}
		t.Fatal("unexpected URL", r.URL)
		return nil, errors.New("unexpected")
	})}
	got, stale, e := a.loadMarket(context.Background(), true)
	if e != nil || stale || got.Revision != newRevision {
		t.Fatalf("refresh %v %v", stale, e)
	}
	p := got.Catalog.Plugins[0]
	download := "/api/plugin-market/" + p.ID + "/download?"
	for _, query := range []url.Values{
		{"revision": {s.Revision}}, // Old client keeps the strict revision contract.
		{"revision": {s.Revision}, "version": {p.Version}},
		{"revision": {s.Revision}, "version": {p.Version}, "sha256": {strings.Repeat("c", 64)}},
		{"revision": {s.Revision}, "version": {"3.0.0"}, "sha256": {p.SHA256}},
		{"revision": {"invalid"}, "version": {p.Version}, "sha256": {p.SHA256}},
	} {
		requireStatus(t, request(a, "GET", download+query.Encode(), nil), 409)
	}
	requireStatus(t, request(a, "POST", "/api/plugin-market/"+p.ID+"/install", marketInput(a, s)), 409)
	review := url.Values{"revision": {s.Revision}, "version": {p.Version}, "sha256": {p.SHA256}}
	result := request(a, "GET", download+review.Encode(), nil)
	requireStatus(t, result, 200)
	if string(result.Body.Bytes()) != string(raw) || result.Header().Get("X-Content-SHA256") != p.SHA256 {
		t.Fatal("download no longer matches the reviewed package")
	}
	if len(seen) != 3 {
		t.Fatalf("network calls %v", seen)
	}
	root, e := a.marketRoot()
	if e != nil {
		t.Fatal(e)
	}
	defer root.Close()
	// All content must be checked again when reusing a file, not just when downloading.
	if e = skillCacheWrite(root, got.Catalog.Plugins[0].SHA256+".zip", []byte("tampered")); e != nil {
		t.Fatal(e)
	}
	a.pluginUpdateClient = &http.Client{Transport: pluginUpgradeTransport(func(*http.Request) (*http.Response, error) { return nil, errors.New("offline") })}
	if _, e = a.marketPackage(context.Background(), got, got.Catalog.Plugins[0]); e == nil {
		t.Fatal("tampered cached package reused")
	}
}

func TestPluginMarketFullCacheStillDownloadsWithoutRetainingPackage(t *testing.T) {
	for _, scenario := range []struct {
		name  string
		count int
		size  int64
	}{
		{name: "file limit", count: 64, size: 1},
		{name: "byte limit", count: 1, size: 256 << 20},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			a, s, raw := marketFixture(t, "2.0.0")
			p := s.Catalog.Plugins[0]
			root, err := a.marketRoot()
			if err != nil {
				t.Fatal(err)
			}
			defer root.Close()
			for i := 0; i < scenario.count; i++ {
				f, err := root.Create(fmt.Sprintf("full-%02d.zip", i))
				if err != nil {
					t.Fatal(err)
				}
				err = f.Truncate(scenario.size)
				f.Close()
				if err != nil {
					t.Fatal(err)
				}
			}
			download := "/api/plugin-market/" + p.ID + "/download?revision=" + s.Revision
			result := request(a, "GET", download, nil)
			requireStatus(t, result, 200)
			if string(result.Body.Bytes()) != string(raw) {
				t.Fatal("full cache changed downloaded bytes")
			}
			if _, err := root.Stat(p.SHA256 + ".zip"); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("cache boundary violated: %v", err)
			}
			a.pluginUpdateClient.Transport = pluginUpgradeTransport(func(*http.Request) (*http.Response, error) {
				return upgradeResponse([]byte("tampered")), nil
			})
			requireStatus(t, request(a, "GET", download, nil), 502)
		})
	}
}

// Explicit local-package integration gate. Does not download or touch user plugins.
func TestPluginMarketPublishedPackages(t *testing.T) {
	fixture := os.Getenv("AIDE_TEST_MARKET_PACKAGES")
	if fixture == "" {
		t.Skip("set AIDE_TEST_MARKET_PACKAGES to a verified official package tree")
	}
	var snapshot marketSnapshot
	if err := json.Unmarshal(shippedMarket, &snapshot); err != nil {
		t.Fatal(err)
	}
	snapshot.Fetched = time.Now().Unix()
	for _, p := range snapshot.Catalog.Plugins {
		t.Run(p.ID, func(t *testing.T) {
			a := testApp(t)
			a.buildVersion = "0.1.17.0 RC2"
			cacheMarket(t, a, snapshot)
			raw, err := os.ReadFile(filepath.Join(fixture, filepath.FromSlash(p.Package)))
			if err != nil {
				t.Fatal(err)
			}
			a.pluginUpdateClient = &http.Client{Transport: pluginUpgradeTransport(func(r *http.Request) (*http.Response, error) {
				if r.URL.String() != marketRaw+snapshot.Revision+"/"+p.Package {
					t.Fatal("unexpected URL", r.URL)
				}
				return upgradeResponse(raw), nil
			})}
			input := map[string]any{"revision": snapshot.Revision, "version": p.Version, "sha256": p.SHA256, "workspace": a.wsID(), "workspaceRevision": a.wsRevision}
			requireStatus(t, request(a, "POST", "/api/plugin-market/"+p.ID+"/install", input), 201)
			for _, entry := range a.pluginRegistry.Plugins {
				if entry.ID == p.ID {
					if entry.PackageSHA256 != p.SHA256 || !entry.Enabled {
						t.Fatal("package receipt")
					}
					return
				}
			}
			t.Fatal("not installed")
		})
	}
}

func TestPluginMarketInstalledUpgradeHonorsRevocation(t *testing.T) {
	for _, kind := range []string{"locked", "read-only"} {
		t.Run(kind, func(t *testing.T) {
			a, s, _ := marketFixture(t, "2.0.0")
			id := s.Catalog.Plugins[0].ID
			requireStatus(t, request(a, "POST", "/api/plugin-market/"+id+"/install", marketInput(a, s)), 201)
			if kind == "locked" {
				if e := atomicJSON(lockStatePath(a.dataPath), LockStateFile{Locked: true, Gen: 3}); e != nil {
					t.Fatal(e)
				}
			} else {
				a.settings.SandboxMode = "read-only"
			}
			requireStatus(t, request(a, "POST", "/api/plugins/"+id+"/upgrade", map[string]any{}), 409)
			requireStatus(t, request(a, "POST", "/api/plugins/"+id+"/upgrade", map[string]any{"url": officialPluginPrefix + id}), 409)
		})
	}
}

func TestPluginMarketTerminalWorkbenchCapability(t *testing.T) {
	a, snapshot, _ := marketFixture(t, "2.0.0")
	snapshot.Catalog.Plugins[0].Features = []string{"terminal-workbench-v1", "terminal-ssh-workspace-v1", "terminal-communications-v1"}
	cacheMarket(t, a, snapshot)
	requireStatus(t, request(a, "POST", "/api/plugin-market/market-fixture/install", marketInput(a, snapshot)), 201)
	// Capability support does not waive the minimum version or unknown-feature checks.
	p := snapshot.Catalog.Plugins[0]
	if ok, _ := marketCompatible(p, "0.1.16.0"); ok {
		t.Fatal("minimum version was bypassed")
	}
	p.Features = append(p.Features, "future-terminal-v2")
	if ok, _ := marketCompatible(p, "dev"); ok {
		t.Fatal("unknown capability was accepted")
	}
}
