package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
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
func TestPluginMarketInstallUpdateAndDownload(t *testing.T) {
	a, s, raw := marketFixture(t, "2.0.0")
	id := s.Catalog.Plugins[0].ID
	requireStatus(t, request(a, "GET", "/api/plugin-market", nil), 200)
	result := request(a, "GET", "/api/plugin-market/"+id+"/download?revision="+s.Revision, nil)
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

func TestPluginMarketRefreshPinsCatalogAndBoundsCache(t *testing.T) {
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
	if _, _, e = a.marketSelected(context.Background(), s.Catalog.Plugins[0].ID, s.Revision); e == nil {
		t.Fatal("old review accepted")
	}
	if _, e = a.marketPackage(context.Background(), got, got.Catalog.Plugins[0]); e != nil {
		t.Fatal(e)
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
	for i := 0; i < 64; i++ {
		if e = skillCacheWrite(root, fmt.Sprintf("full-%02d.zip", i), []byte("x")); e != nil {
			t.Fatal(e)
		}
	}
	if _, e = a.marketPackage(context.Background(), got, got.Catalog.Plugins[0]); e == nil || !strings.Contains(e.Error(), "缓存已满") {
		t.Fatal("cache bound", e)
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
