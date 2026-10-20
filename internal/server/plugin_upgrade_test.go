package server

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type pluginUpgradeTransport func(*http.Request) (*http.Response, error)

func (f pluginUpgradeTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func upgradeResponse(data []byte) *http.Response {
	return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(data)), ContentLength: int64(len(data)), Header: make(http.Header)}
}
func upgradePackage(t *testing.T, id, code string, extra map[string]string) []byte {
	t.Helper()
	files := map[string]string{"manifest.json": fmt.Sprintf(`{"id":%q,"name":"Upgrade fixture","main":"index.js","version":"2.0.0"}`, id), "index.js": code}
	for name, data := range extra {
		files[name] = data
	}
	var out bytes.Buffer
	z := zip.NewWriter(&out)
	for name, data := range files {
		f, err := z.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = io.WriteString(f, data)
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

const upgradeOldCode = `module.exports={apply(ctx){ctx.tool({name:'upgrade_fixture_tool',description:'fixture',parameters:{type:'object'},handler:()=>({text:'old'})});}};`

func upgradeFixture(t *testing.T) (*App, string) {
	t.Helper()
	a := testApp(t)
	id := "upgrade-fixture"
	requireStatus(t, request(a, "POST", "/api/plugins", map[string]any{"id": id, "name": "Upgrade fixture", "version": "1.0.0", "code": upgradeOldCode}), 201)
	requireStatus(t, request(a, "PUT", "/api/plugins/"+id+"/settings", map[string]any{"settings": map[string]any{"marker": "keep"}}), 200)
	return a, id
}
func setUpgradeBytes(a *App, raw []byte) {
	a.pluginUpdateClient = &http.Client{Transport: pluginUpgradeTransport(func(r *http.Request) (*http.Response, error) { return upgradeResponse(raw), nil })}
}
func postUpgrade(a *App, id string, options map[string]any) *httptest.ResponseRecorder {
	return request(a, "POST", "/api/plugins/"+id+"/upgrade", options)
}
func TestPluginUpgradePreservesStateAndRunsNewResources(t *testing.T) {
	a, id := upgradeFixture(t)
	dir := filepath.Join(a.pluginsPath, id)
	if err := os.MkdirAll(filepath.Join(dir, ".data"), 0700); err != nil {
		t.Fatal(err)
	}
	record := []byte(`{"private":"kept"}`)
	_ = os.WriteFile(filepath.Join(dir, ".data/records.json"), record, 0600)
	raw := upgradePackage(t, id, `module.exports={apply(ctx){ctx.tool({name:'upgrade_fixture_tool',description:'fixture',parameters:{type:'object'},handler:()=>({text:require('./helper.js').value+ctx.settings.marker})});}};`, map[string]string{"helper.js": "exports.value='new-';"})
	setUpgradeBytes(a, raw)
	sum := sha256.Sum256(raw)
	w := postUpgrade(a, id, map[string]any{"url": "https://updates.example.test/latest.zip", "sha256": hex.EncodeToString(sum[:])})
	requireStatus(t, w, 200)
	var result struct {
		Backup string `json:"backup"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &result)
	if result.Backup == "" {
		t.Fatal(w.Body.String())
	}
	got, err := a.callPluginTool(id, "upgrade_fixture_tool", nil)
	if err != nil || !strings.Contains(gotText(got), "new-keep") {
		t.Fatalf("new tool: %v %v", got, err)
	}
	after, err := os.ReadFile(filepath.Join(dir, ".data/records.json"))
	if err != nil || !bytes.Equal(after, record) {
		t.Fatal("data changed", err)
	}
	old, err := os.ReadFile(filepath.Join(a.pluginsPath, result.Backup, "index.js"))
	if err != nil || string(old) != upgradeOldCode {
		t.Fatal("rollback files missing")
	}
	if _, err = os.Stat(filepath.Join(a.pluginsPath, result.Backup, ".aide-upgrade-registry.json")); err != nil {
		t.Fatal(err)
	}
	requireStatus(t, postUpgrade(a, id, map[string]any{}), 200)
	registry := a.pluginRegistry.Plugins[0]
	if registry.Version != "2.0.0" || registry.Settings["marker"] != "keep" || !registry.Enabled || registry.UpdateURL != "https://updates.example.test/latest.zip" {
		t.Fatalf("registry: %+v", registry)
	}
	requireStatus(t, postUpgrade(a, id, map[string]any{}), 200)
	if err := a.loadPlugins(); err != nil {
		t.Fatal(err)
	}
	if a.pluginRegistry.Plugins[0].PackageSHA256 == "" {
		t.Fatal("upgrade not persistent")
	}
}
func TestPluginUpgradePreservesDisabledState(t *testing.T) {
	a, id := upgradeFixture(t)
	requireStatus(t, request(a, "PUT", "/api/plugins/"+id, map[string]any{"enabled": false}), 200)
	setUpgradeBytes(a, upgradePackage(t, id, upgradeOldCode, nil))
	requireStatus(t, postUpgrade(a, id, map[string]any{"url": "https://updates.example.test/latest.zip"}), 200)
	if a.pluginRegistry.Plugins[0].Enabled {
		t.Fatal("disabled plugin enabled")
	}
	if _, err := a.callPluginTool(id, "upgrade_fixture_tool", nil); err == nil {
		t.Fatal("disabled plugin ran")
	}
}
func TestPluginUpgradeRejectsWithoutChangingOriginal(t *testing.T) {
	cases := []struct {
		name, id, code, checksum string
		extra                    map[string]string
	}{
		{name: "wrong-id", id: "other", code: upgradeOldCode},
		{name: "hash", id: "upgrade-fixture", code: upgradeOldCode, checksum: strings.Repeat("0", 64)},
		{name: "traversal", id: "upgrade-fixture", code: upgradeOldCode, extra: map[string]string{"../outside": "bad"}},
		{name: "apply", id: "upgrade-fixture", code: `module.exports={apply(){throw Error('invalid apply')}}`},
		{name: "missing-module", id: "upgrade-fixture", code: `require('./missing');module.exports={apply(){}}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a, id := upgradeFixture(t)
			path := filepath.Join(a.pluginsPath, "registry.json")
			before, _ := os.ReadFile(path)
			surface := append([]byte(nil), a.pluginSurface...)
			setUpgradeBytes(a, upgradePackage(t, tc.id, tc.code, tc.extra))
			requireStatus(t, postUpgrade(a, id, map[string]any{"url": "https://updates.example.test/latest.zip", "sha256": tc.checksum}), 400)
			after, _ := os.ReadFile(path)
			code, _ := os.ReadFile(filepath.Join(a.pluginsPath, id, "index.js"))
			if !bytes.Equal(before, after) || string(code) != upgradeOldCode || !bytes.Equal(surface, a.pluginSurface) {
				t.Fatal("failed update changed original")
			}
		})
	}
}
func waitUpgradeQueue(t *testing.T, a *App, n int) {
	t.Helper()
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		a.pluginResources.mu.Lock()
		size := len(a.pluginResources.items)
		a.pluginResources.mu.Unlock()
		if size >= n {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("resource waiters did not arrive")
}
func TestPluginUpgradeWaitsAndRejectsQueuedOldCode(t *testing.T) {
	a, id := upgradeFixture(t)
	setUpgradeBytes(a, upgradePackage(t, id, upgradeOldCode, nil))
	release, err := a.pluginResources.acquire(context.Background(), pluginFootprint{exclusive: true})
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	upgraded := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		upgraded <- postUpgrade(a, id, map[string]any{"url": "https://updates.example.test/latest.zip"})
	}()
	waitUpgradeQueue(t, a, 2)
	called := make(chan error, 1)
	go func() { _, err := a.callPluginTool(id, "upgrade_fixture_tool", nil); called <- err }()
	waitUpgradeQueue(t, a, 3)
	select {
	case <-upgraded:
		t.Fatal("upgrade did not wait")
	default:
	}
	release()
	requireStatus(t, <-upgraded, 200)
	if err := <-called; err == nil || !strings.Contains(err.Error(), "变化") {
		t.Fatalf("queued old call: %v", err)
	}
}
func TestPluginUpgradeDuplicateAndChangedWorkspace(t *testing.T) {
	a, id := upgradeFixture(t)
	ready, finish := make(chan struct{}), make(chan struct{})
	raw := upgradePackage(t, id, upgradeOldCode, nil)
	a.pluginUpdateClient = &http.Client{Transport: pluginUpgradeTransport(func(r *http.Request) (*http.Response, error) {
		close(ready)
		<-finish
		return upgradeResponse(raw), nil
	})}
	result := make(chan *httptest.ResponseRecorder, 1)
	go func() { result <- postUpgrade(a, id, map[string]any{"url": "https://updates.example.test/latest.zip"}) }()
	<-ready
	requireStatus(t, postUpgrade(a, id, map[string]any{}), 409)
	a.mu.Lock()
	a.wsRevision++
	a.mu.Unlock()
	close(finish)
	requireStatus(t, <-result, 409)
	code, _ := os.ReadFile(filepath.Join(a.pluginsPath, id, "index.js"))
	if string(code) != upgradeOldCode {
		t.Fatal("workspace change replaced code")
	}
}
func TestPluginUpgradeCancelledWaitKeepsOriginal(t *testing.T) {
	a, id := upgradeFixture(t)
	setUpgradeBytes(a, upgradePackage(t, id, upgradeOldCode, nil))
	release, _ := a.pluginResources.acquire(context.Background(), pluginFootprint{exclusive: true})
	defer release()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	body := strings.NewReader(`{"url":"https://updates.example.test/latest.zip"}`)
	r := httptest.NewRequest("POST", "/api/plugins/"+id+"/upgrade", body).WithContext(ctx)
	r.Header.Set("Authorization", "Bearer "+a.token)
	w := httptest.NewRecorder()
	done := make(chan struct{})
	go func() { a.Handler().ServeHTTP(w, r); close(done) }()
	waitUpgradeQueue(t, a, 2)
	cancel()
	<-done
	requireStatus(t, w, 409)
	if a.pluginRegistry.Plugins[0].Version != "1.0.0" {
		t.Fatal("cancelled update committed")
	}
}
func TestPluginUpgradeRecoveryRestoresInterruptedSwap(t *testing.T) {
	a, id := upgradeFixture(t)
	dir := filepath.Join(a.pluginsPath, id)
	stage, _ := os.MkdirTemp(a.pluginsPath, ".upgrade-stage-")
	backup := ".upgrade-backup-" + id + "-test"
	old := a.pluginRegistry.Plugins[0]
	j := pluginUpgradeJournal{id, backup, filepath.Base(stage), strings.Repeat("a", 64), old}
	if err := atomicJSON(filepath.Join(a.pluginsPath, ".upgrade-journal-"+id+".json"), j); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(dir, filepath.Join(a.pluginsPath, backup)); err != nil {
		t.Fatal(err)
	}
	_ = os.MkdirAll(dir, 0755)
	_ = os.WriteFile(filepath.Join(dir, "index.js"), []byte("broken replacement"), 0644)
	if err := a.loadPlugins(); err != nil {
		t.Fatal(err)
	}
	code, _ := os.ReadFile(filepath.Join(dir, "index.js"))
	if string(code) != upgradeOldCode {
		t.Fatal("original not restored")
	}
	if _, err := os.Stat(stage); !os.IsNotExist(err) {
		t.Fatal("staging not removed")
	}
	if err := a.loadPlugins(); err != nil {
		t.Fatal("recovery not idempotent", err)
	}
}
func TestPluginUpgradeRecoveryKeepsCommittedSwap(t *testing.T) {
	a, id := upgradeFixture(t)
	sum := strings.Repeat("b", 64)
	old := a.pluginRegistry.Plugins[0]
	a.pluginRegistry.Plugins[0].PackageSHA256 = sum
	if err := a.savePluginRegistry(); err != nil {
		t.Fatal(err)
	}
	stage, _ := os.MkdirTemp(a.pluginsPath, ".upgrade-stage-")
	j := pluginUpgradeJournal{id, ".upgrade-backup-" + id + "-test", filepath.Base(stage), sum, old}
	_ = atomicJSON(filepath.Join(a.pluginsPath, ".upgrade-journal-"+id+".json"), j)
	if err := a.loadPlugins(); err != nil {
		t.Fatal(err)
	}
	if a.pluginRegistry.Plugins[0].PackageSHA256 != sum {
		t.Fatal("committed update reverted")
	}
}
func TestPluginUpgradeNetworkPolicy(t *testing.T) {
	for _, raw := range []string{"http://example.com/a.zip", "file:///tmp/a.zip", "https://user:pass@example.com/a.zip", "https://127.0.0.1/a.zip", "https://[::1]/a.zip", "https://169.254.169.254/a.zip", "https://10.0.0.1/a.zip", "https://100.64.0.1/a.zip"} {
		if _, err := pluginUpdateURL(raw); err == nil {
			t.Fatal("unsafe URL accepted", raw)
		}
	}
	if _, err := pluginUpdateURL("https://example.com/a.zip"); err != nil {
		t.Fatal(err)
	}
	if pluginPublicIP(net.IP{127, 0, 0, 1}) || !pluginPublicIP(net.IP{8, 8, 8, 8}) {
		t.Fatal("IPv4 length policy")
	}
	client := newPluginUpdateClient()
	req, _ := http.NewRequest("GET", "http://example.com/redirect", nil)
	if client.CheckRedirect(req, []*http.Request{{}}) == nil {
		t.Fatal("insecure redirect accepted")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if conn, err := client.Transport.(*http.Transport).DialContext(ctx, "tcp", "127.0.0.1:9"); err == nil {
		conn.Close()
		t.Fatal("private dial accepted")
	}
	client = &http.Client{Transport: pluginUpgradeTransport(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("Authorization") != "" {
			t.Error("workbench token sent upstream")
		}
		response := upgradeResponse([]byte("123456789"))
		response.ContentLength = -1
		return response, nil
	})}
	if _, err := pluginFetch(context.Background(), client, "https://example.com/a.zip", 8); err == nil {
		t.Fatal("unbounded download")
	}
}

func TestPluginUpgradeRejectsExternalLocalSymlink(t *testing.T) {
	a, id := upgradeFixture(t)
	if err := os.Symlink(t.TempDir(), filepath.Join(a.pluginsPath, id, "external")); err != nil {
		t.Fatal(err)
	}
	setUpgradeBytes(a, upgradePackage(t, id, upgradeOldCode, nil))
	requireStatus(t, postUpgrade(a, id, map[string]any{"url": "https://example.com/a.zip"}), 400)
}

func TestPluginUpgradeDaemonFailureRollsBackSwappedFiles(t *testing.T) {
	a, id := upgradeFixture(t)
	if a.daemons == nil {
		t.Fatal("daemon manager missing")
	}
	code := `module.exports={apply(ctx){if(process.env.AIDE_DAEMON==='1')throw Error('daemon rejected');ctx.tool({name:'upgrade_fixture_tool',description:'fixture',parameters:{type:'object'},handler:()=>({text:'new'})});}}`
	raw := upgradePackage(t, id, code, map[string]string{"manifest.json": `{"id":"upgrade-fixture","name":"Fixture","version":"2.0.0","main":"new.js","daemon":true}`, "new.js": code})
	setUpgradeBytes(a, raw)
	requireStatus(t, postUpgrade(a, id, map[string]any{"url": "https://example.com/latest.zip"}), 500)
	current, err := os.ReadFile(filepath.Join(a.pluginsPath, id, "index.js"))
	if err != nil || string(current) != upgradeOldCode {
		t.Fatal("old code was not restored", err)
	}
	if a.pluginRegistry.Plugins[0].Version != "1.0.0" {
		t.Fatal("registry not restored")
	}
	got, err := a.callPluginTool(id, "upgrade_fixture_tool", nil)
	if err != nil || !strings.Contains(gotText(got), "old") {
		t.Fatal("old plugin not callable", got, err)
	}
}
func TestPluginUpgradeVersionAndDependentGuard(t *testing.T) {
	for _, pair := range [][2]string{{"2.0.0", "1.9.9"}, {"1.2.0", "1.2.0-beta"}} {
		if !pluginVersionDowngrade(pair[0], pair[1]) {
			t.Fatal("downgrade accepted", pair)
		}
	}
	if pluginVersionDowngrade("1.9.0", "1.10.0") {
		t.Fatal("numeric upgrade rejected")
	}
	before := []byte(`{"plugins":[{"id":"consumer","error":""}]}`)
	after := []byte(`{"plugins":[{"id":"consumer","error":"provider missing"}]}`)
	if validatePluginUpgradeDependents(before, after, "provider") == nil {
		t.Fatal("broken consumer accepted")
	}
}

func TestPluginUpgradeGitHubFakeIPProxy(t *testing.T) {
	fake := net.ParseIP("198.18.2.1")
	if !pluginResolvedIPAllowed("api.github.com", fake) || !pluginResolvedIPAllowed("raw.githubusercontent.com", fake) {
		t.Fatal("GitHub TUN proxy rejected")
	}
	for _, host := range []string{"evil.invalid", "api.github.com.attacker.test", "198.18.2.1"} {
		if pluginResolvedIPAllowed(host, fake) {
			t.Fatal("Fake-IP exception widened", host)
		}
	}
	if pluginResolvedIPAllowed("api.github.com", net.ParseIP("127.0.0.1")) || pluginResolvedIPAllowed("api.github.com", net.ParseIP("10.0.0.1")) {
		t.Fatal("GitHub private DNS accepted")
	}
	if _, err := pluginUpdateURL("https://198.18.2.1/secret"); err == nil {
		t.Fatal("literal Fake-IP accepted")
	}
}

func TestOfficialPluginDefaultsFollowIndependentMarket(t *testing.T) {
	for _, old := range []PluginManifest{{ID: "office"}, {ID: "terminal"}, {ID: "office", UpdateURL: officialPluginPrefix + "office"}} {
		if got := pluginUpdateSource(old); got != marketUpdatePrefix+old.ID {
			t.Fatalf("source = %q", got)
		}
	}
	if got := pluginUpdateSource(PluginManifest{ID: "office", UpdateURL: "https://example.com/custom.zip"}); got != "https://example.com/custom.zip" {
		t.Fatal(got)
	}
	if got := pluginUpdateSource(PluginManifest{ID: "propeller-aero"}); got != "" {
		t.Fatal(got)
	}
}
