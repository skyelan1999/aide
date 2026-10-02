package server

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type rewriteUpdateTransport struct {
	base      string
	transport http.RoundTripper
}

func (rt rewriteUpdateTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	u, _ := url.Parse(rt.base)
	clone.URL.Scheme, clone.URL.Host = u.Scheme, u.Host
	clone.Host = u.Host
	return rt.transport.RoundTrip(clone)
}

func TestCheckUpdatesReturnsVerifiedLauncherLinks(t *testing.T) {
	github := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/releases" || r.URL.Query().Get("per_page") != "20" {
			t.Fatalf("unexpected update request: %s", r.URL.String())
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"tag_name":"v0.1.15.0-RC1","name":"aide v0.1.15.0 RC1","html_url":"https://github.com/skyelan1999/aide/releases/tag/v0.1.15.0-RC1","body":"New version","published_at":"2026-10-01T00:00:00Z","assets":[{"name":"aide-v0.1.15.0-RC1-macos-arm64.zip","browser_download_url":"https://github.com/skyelan1999/aide/releases/download/v0.1.15.0-RC1/aide-v0.1.15.0-RC1-macos-arm64.zip"},{"name":"aide-v0.1.15.0-RC1-full-linux-arm64.zip","browser_download_url":"https://github.com/skyelan1999/aide/releases/download/v0.1.15.0-RC1/aide-v0.1.15.0-RC1-full-linux-arm64.zip"},{"name":"aide-v0.1.15.0-RC1-linux-arm64-image.tar.gz","browser_download_url":"https://github.com/skyelan1999/aide/releases/download/v0.1.15.0-RC1/image.tar.gz"},{"name":"aide-v0.1.15.0-RC1-windows-arm64.zip","browser_download_url":"https://evil.example/launcher.zip"}]}]`))
	}))
	defer github.Close()
	a := testApp(t)
	a.version = "0.1.14.0 RC9"
	a.updateAPIBase = github.URL + "/releases?per_page=20"
	a.updateHTTPClient = github.Client()

	w := request(a, "GET", "/api/updates", nil)
	requireStatus(t, w, http.StatusOK)
	var got struct {
		CurrentVersion  string          `json:"currentVersion"`
		LatestVersion   string          `json:"latestVersion"`
		UpdateAvailable bool            `json:"updateAvailable"`
		ReleaseNotes    string          `json:"releaseNotes"`
		Assets          []updateAsset   `json:"assets"`
		OnlineReleases  []onlineRelease `json:"onlineReleases"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.CurrentVersion != "0.1.14.0 RC9" || got.LatestVersion != "0.1.15.0-RC1" || !got.UpdateAvailable || got.ReleaseNotes != "New version" {
		t.Fatalf("unexpected update info: %+v", got)
	}
	if len(got.Assets) != 2 || !isLauncherAsset(got.Assets[0].Name, "v0.1.15.0-RC1") || !isUpdateBundleAsset(got.Assets[1].Name, "v0.1.15.0-RC1") {
		t.Fatalf("known launcher and unified runtime/update assets on github.com should be returned: %+v", got.Assets)
	}
	if len(got.OnlineReleases) != 1 || got.OnlineReleases[0].Tag != "v0.1.15.0-RC1" || !strings.Contains(got.OnlineReleases[0].AssetName, "full-linux-arm64.zip") {
		t.Fatalf("expected an online-install choice matching this platform: %+v", got.OnlineReleases)
	}
}

func TestStageUpdatePackageAllowsSameVersionAndDowngrade(t *testing.T) {
	for _, tag := range []string{"v0.1.14.0-RC14", "v0.1.14.0-RC11"} {
		t.Run(tag, func(t *testing.T) {
			a := testApp(t)
			if err := os.MkdirAll(a.updatePackagesPath(), 0700); err != nil {
				t.Fatal(err)
			}
			packagePath := filepath.Join(a.updatePackagesPath(), "fixture.zip")
			if err := os.WriteFile(packagePath, []byte("validated fixture"), 0600); err != nil {
				t.Fatal(err)
			}
			a.updatesMu.Lock()
			state := updateSlotsState{Version: 1, ActiveSlot: "A", Slots: map[string]updateSlot{
				"A": {Version: "0.1.14.0 RC14", Tag: "v0.1.14.0-RC14", ImageRef: "aide:slot-a", ImageID: "sha256:" + strings.Repeat("a", 64), Platform: runtimePlatform()},
				"B": {},
			}}
			if err := a.saveUpdateStateLocked(state); err != nil {
				a.updatesMu.Unlock()
				t.Fatal(err)
			}
			a.updatesMu.Unlock()
			manifest := updatePackageManifest{Format: "aide-update-package", Version: 1, ReleaseTag: tag, Platform: runtimePlatform(), ImageID: "sha256:" + strings.Repeat("b", 64)}
			if err := a.stageUpdatePackage(packagePath, manifest, strings.Repeat("c", 64), ""); err != nil {
				t.Fatalf("version %s should be stageable: %v", tag, err)
			}
			response := request(a, http.MethodGet, "/api/updates/slots", nil)
			var got updateSlotsState
			if err := json.Unmarshal(response.Body.Bytes(), &got); err != nil {
				t.Fatal(err)
			}
			if got.ActiveSlot != "A" || got.Slots["B"].Tag != tag || got.Slots["B"].PackageID == "" || got.Pending != nil {
				t.Fatalf("staging must preserve the active slot and wait for manual activation: %+v", got)
			}
		})
	}
}

func TestOnlineInstallDownloadsVerifiesAndStagesOfficialBundle(t *testing.T) {
	tag := "v0.1.14.0-RC15"
	platform := runtimePlatform()
	imageName := "aide-" + tag + "-" + strings.ReplaceAll(platform, "/", "-") + "-image.tar.gz"
	imageBytes := []byte("compressed docker image fixture")
	imageSum := sha256.Sum256(imageBytes)
	manifest, _ := json.Marshal(updatePackageManifest{Format: "aide-update-package", Version: 1, ReleaseTag: tag, Platform: platform, ImageArchive: imageName, ImageID: "sha256:" + strings.Repeat("d", 64)})
	var bundle bytes.Buffer
	zw := zip.NewWriter(&bundle)
	for name, content := range map[string][]byte{"manifest.json": manifest, "SHA256SUMS": []byte(fmt.Sprintf("%s  %s\n", hex.EncodeToString(imageSum[:]), imageName)), imageName: imageBytes} {
		part, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := part.Write(content); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	assetName := "aide-" + tag + "-update-linux-" + strings.TrimPrefix(platform, "linux/") + ".zip"
	assetURL := "https://github.com/asset.zip"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/releases" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprintf(w, `[{"tag_name":%q,"name":%q,"html_url":%q,"assets":[{"name":%q,"browser_download_url":%q}]}]`, tag, tag, "https://github.com/skyelan1999/aide/releases/tag/"+tag, assetName, assetURL)
			return
		}
		if r.URL.Path == "/asset.zip" {
			w.Header().Set("Content-Length", fmt.Sprint(bundle.Len()))
			_, _ = w.Write(bundle.Bytes())
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()
	a := testApp(t)
	a.buildRuntimeMode = "release-image"
	a.updateAPIBase = server.URL + "/releases?per_page=20"
	a.updateHTTPClient = &http.Client{Transport: rewriteUpdateTransport{base: server.URL, transport: server.Client().Transport}}
	response := request(a, http.MethodPost, "/api/updates/online", map[string]string{"tag": tag})
	requireStatus(t, response, http.StatusAccepted)
	deadline := time.Now().Add(3 * time.Second)
	completed := false
	for time.Now().Before(deadline) {
		stateResponse := request(a, http.MethodGet, "/api/updates/slots", nil)
		var got updateSlotsState
		if err := json.Unmarshal(stateResponse.Body.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		if got.Download != nil && got.Download.Status == "complete" {
			if got.ActiveSlot != "A" || got.Slots["B"].Tag != tag || got.Slots["B"].PackageID == "" || got.Pending != nil {
				t.Fatalf("online install must stage only the inactive slot: %+v", got)
			}
			completed = true
			break
		}
		if got.Download != nil && got.Download.Status == "failed" {
			t.Fatalf("online install failed: %s", got.Download.Message)
		}
		time.Sleep(10 * time.Millisecond)
		if time.Now().Add(20 * time.Millisecond).After(deadline) {
			t.Fatal("online install did not complete")
		}
	}
	if !completed {
		t.Fatal("online install did not complete before timeout")
	}
}

func TestCheckUpdatesRequiresAuthentication(t *testing.T) {
	a := testApp(t)
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/api/updates", nil))
	requireStatus(t, w, http.StatusUnauthorized)
}

func TestSourceModeSkipsABUpgrade(t *testing.T) {
	a := testApp(t)
	a.version = "0.1.14.0 RC9"
	a.buildVersion = "dev"

	w := request(a, http.MethodGet, "/api/updates/slots", nil)
	requireStatus(t, w, http.StatusOK)
	var slots struct {
		RuntimeMode string `json:"runtimeMode"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &slots); err != nil {
		t.Fatal(err)
	}
	if slots.RuntimeMode != "source" {
		t.Fatalf("runtime mode = %q, want source", slots.RuntimeMode)
	}
	requireStatus(t, request(a, http.MethodPost, "/api/updates/packages", nil), http.StatusConflict)
	requireStatus(t, request(a, http.MethodPost, "/api/updates/online", map[string]string{"tag": "v0.1.14.0-RC10"}), http.StatusConflict)
	requireStatus(t, request(a, http.MethodPost, "/api/updates/switch", map[string]string{"target": "B"}), http.StatusConflict)
	requireStatus(t, request(a, http.MethodGet, "/api/updates/agent", nil), http.StatusConflict)
	requireStatus(t, request(a, http.MethodPost, "/api/updates/agent/sync", map[string]any{}), http.StatusConflict)
	requireStatus(t, request(a, http.MethodPost, "/api/updates/agent/result", map[string]any{}), http.StatusConflict)
}

func TestUpdateAgentSyncClearsStaleSwitchFailureWhenReportingActiveSlot(t *testing.T) {
	a := testApp(t)
	a.buildRuntimeMode = "release-image"
	a.updatesMu.Lock()
	state := updateSlotsState{
		Version: 1, ActiveSlot: "A", LastMessage: "槽切换失败并已回滚：target container failed to start",
		Slots: map[string]updateSlot{
			"A": {Version: "0.1.14.0 RC10", Tag: "v0.1.14.0-RC10", ImageRef: "aide:slot-a", ImageID: "sha256:" + strings.Repeat("a", 64), Platform: "linux/arm64"},
			"B": {Version: "0.1.14.0 RC11", Tag: "v0.1.14.0-RC11", ImageRef: "aide:slot-b", ImageID: "sha256:" + strings.Repeat("b", 64), Platform: "linux/arm64"},
		},
	}
	if err := a.saveUpdateStateLocked(state); err != nil {
		a.updatesMu.Unlock()
		t.Fatal(err)
	}
	a.updatesMu.Unlock()

	response := request(a, http.MethodPost, "/api/updates/agent/sync", map[string]any{
		"version": 1, "activeSlot": "B",
		"slots": map[string]any{
			"B": map[string]any{
				"version": "0.1.14.0 RC11", "tag": "v0.1.14.0-RC11", "imageRef": "aide:slot-b",
				"imageId": "sha256:" + strings.Repeat("b", 64), "platform": "linux/arm64",
			},
		},
	})
	requireStatus(t, response, http.StatusOK)
	var got updateSlotsState
	if err := json.Unmarshal(response.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.ActiveSlot != "B" || got.LastMessage != "宿主启动器已同步当前活动槽" || got.Pending != nil {
		t.Fatalf("launcher sync should report the actual active slot and clear stale failure state: %+v", got)
	}
}

func TestManualSwitchAgentCommandPreservesEmptyOptionalFields(t *testing.T) {
	a := testApp(t)
	a.buildRuntimeMode = "release-image"
	a.updatesMu.Lock()
	state := updateSlotsState{Version: 1, ActiveSlot: "A", Slots: map[string]updateSlot{
		"A": {Version: "0.1.14.0 RC12", Tag: "v0.1.14.0-RC12", ImageRef: "aide:slot-a", ImageID: "sha256:" + strings.Repeat("a", 64), Platform: "linux/arm64"},
		"B": {Version: "0.1.14.0 RC11", Tag: "v0.1.14.0-RC11", ImageRef: "aide:slot-b", ImageID: "sha256:" + strings.Repeat("b", 64), Platform: "linux/arm64"},
	}}
	if err := a.saveUpdateStateLocked(state); err != nil {
		a.updatesMu.Unlock()
		t.Fatal(err)
	}
	a.updatesMu.Unlock()
	requireStatus(t, request(a, http.MethodPost, "/api/updates/switch", map[string]string{"target": "B"}), http.StatusAccepted)
	response := request(a, http.MethodGet, "/api/updates/agent", nil)
	requireStatus(t, response, http.StatusOK)
	fields := strings.Split(strings.TrimSpace(response.Body.String()), "\t")
	if len(fields) != 9 || fields[1] != "B" || fields[2] != "-" || fields[3] != "v0.1.14.0-RC11" || fields[4] != "sha256:"+strings.Repeat("b", 64) || fields[5] != "linux/arm64" || fields[6] != "-" || fields[7] != "aide:slot-b" || fields[8] != "0.1.14.0 RC11" {
		t.Fatalf("manual activation command fields shifted: %#v", fields)
	}
}

func TestUpdateAgentProgressIsVisibleInSlotState(t *testing.T) {
	a := testApp(t)
	a.buildRuntimeMode = "release-image"
	a.updatesMu.Lock()
	state := updateSlotsState{Version: 1, ActiveSlot: "A", Slots: map[string]updateSlot{
		"A": {ImageRef: "aide:slot-a", ImageID: "sha256:" + strings.Repeat("a", 64), Platform: "linux/arm64"},
		"B": {ImageRef: "aide:slot-b", ImageID: "sha256:" + strings.Repeat("b", 64), Platform: "linux/arm64"},
	}, Pending: &updateOperation{ID: strings.Repeat("c", 32), Target: "B", Status: "requested"}}
	if err := a.saveUpdateStateLocked(state); err != nil {
		a.updatesMu.Unlock()
		t.Fatal(err)
	}
	a.updatesMu.Unlock()
	response := request(a, http.MethodPost, "/api/updates/agent/progress", map[string]any{"operationId": strings.Repeat("c", 32), "status": "importing", "progress": 52, "message": "正在导入 Docker 镜像"})
	requireStatus(t, response, http.StatusOK)
	stateResponse := request(a, http.MethodGet, "/api/updates/slots", nil)
	requireStatus(t, stateResponse, http.StatusOK)
	var got updateSlotsState
	if err := json.Unmarshal(stateResponse.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Pending == nil || got.Pending.Status != "importing" || got.Pending.Progress != 52 || got.Pending.Message != "正在导入 Docker 镜像" {
		t.Fatalf("progress must persist in slot status for the UI: %+v", got.Pending)
	}
}

func TestReleaseVersionOrdering(t *testing.T) {
	for _, tc := range []struct {
		current, latest string
		want            bool
	}{
		{"0.1.14.0 RC9", "0.1.14.0-RC10", true},
		{"0.1.14.0 RC9", "0.1.14.0-RC9", false},
		{"0.1.15.0 RC1", "0.1.14.0-RC99", false},
		{"dev", "0.1.14.0-RC9", false},
	} {
		if got := versionLess(tc.current, tc.latest); got != tc.want {
			t.Errorf("versionLess(%q, %q) = %v, want %v", tc.current, tc.latest, got, tc.want)
		}
	}
}

func TestUploadUpdatePackageAcceptsExtractedFolder(t *testing.T) {
	a := testApp(t)
	a.version = "0.1.14.0 RC9"
	a.buildVersion = a.version
	a.buildRuntimeMode = "release-image"
	tag := "v0.1.14.0-RC10"
	platform := runtimePlatform()
	imageName := "aide-" + tag + "-" + strings.ReplaceAll(platform, "/", "-") + "-image.tar.gz"
	imageBytes := []byte("compressed docker image fixture")
	imageSum := sha256.Sum256(imageBytes)
	manifest, _ := json.Marshal(updatePackageManifest{
		Format: "aide-update-package", Version: 1, ReleaseTag: tag, Platform: platform,
		ImageArchive: imageName, ImageID: "sha256:" + strings.Repeat("a", 64),
	})
	files := map[string][]byte{
		"docker-images/manifest.json": manifest,
		"docker-images/SHA256SUMS":    []byte(fmt.Sprintf("%s  %s\n", hex.EncodeToString(imageSum[:]), imageName)),
		"docker-images/" + imageName:  imageBytes,
		".DS_Store":                   []byte("Finder metadata"),
		"._manifest.json":             []byte("AppleDouble metadata"),
		"__MACOSX/._SHA256SUMS":       []byte("AppleDouble metadata in resource fork folder"),
	}
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	paths := make([]string, 0, len(files))
	for name, contents := range files {
		paths = append(paths, "aide-"+tag+"-full-linux-"+strings.TrimPrefix(platform, "linux/")+"/"+name)
		part, err := mw.CreateFormFile("files", filepath.Base(name))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := part.Write(contents); err != nil {
			t.Fatal(err)
		}
	}
	pathJSON, err := json.Marshal(paths)
	if err != nil {
		t.Fatal(err)
	}
	if err := mw.WriteField("paths", string(pathJSON)); err != nil {
		t.Fatal(err)
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodPost, "/api/updates/packages", &body)
	r.Header.Set("Content-Type", mw.FormDataContentType())
	r.Header.Set("Authorization", "Bearer "+a.token)
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, r)
	requireStatus(t, w, http.StatusCreated)
	var result struct {
		PackageID  string `json:"packageId"`
		TargetSlot string `json:"targetSlot"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.PackageID == "" || result.TargetSlot != "B" {
		t.Fatalf("unexpected upload result: %+v", result)
	}
	stateResponse := request(a, http.MethodGet, "/api/updates/slots", nil)
	requireStatus(t, stateResponse, http.StatusOK)
	var state updateSlotsState
	if err := json.Unmarshal(stateResponse.Body.Bytes(), &state); err != nil {
		t.Fatal(err)
	}
	if state.ActiveSlot != "A" || state.Pending != nil || state.Slots["B"].PackageID != result.PackageID {
		t.Fatalf("upload must stage the package without activating it: %+v", state)
	}
	switchResponse := request(a, http.MethodPost, "/api/updates/switch", map[string]string{"target": "B"})
	requireStatus(t, switchResponse, http.StatusAccepted)
	commandResponse := request(a, http.MethodGet, "/api/updates/agent", nil)
	requireStatus(t, commandResponse, http.StatusOK)
	if !strings.Contains(commandResponse.Body.String(), "\tB\t") {
		t.Fatalf("manual activation must queue a host-side switch to slot B: %q", commandResponse.Body.String())
	}
	archivePath := filepath.Join(a.updatePackagesPath(), result.PackageID+".zip")
	manifestOut, imageHash, err := validateUpdatePackage(archivePath)
	if err != nil {
		t.Fatalf("server-generated folder archive should pass normal validation: %v", err)
	}
	if manifestOut.ReleaseTag != tag || imageHash != hex.EncodeToString(imageSum[:]) {
		t.Fatalf("unexpected validated package: manifest=%+v sha=%s", manifestOut, imageHash)
	}
}

func TestUploadUpdatePackageAcceptsZip(t *testing.T) {
	a := testApp(t)
	a.version = "0.1.14.0 RC10"
	a.buildVersion = a.version
	a.buildRuntimeMode = "release-image"
	tag := "v0.1.14.0-RC10"
	platform := runtimePlatform()
	imageName := "aide-" + tag + "-" + strings.ReplaceAll(platform, "/", "-") + "-image.tar.gz"
	imageBytes := []byte("compressed docker image fixture")
	imageSum := sha256.Sum256(imageBytes)
	manifest, _ := json.Marshal(updatePackageManifest{
		Format: "aide-update-package", Version: 1, ReleaseTag: tag, Platform: platform,
		ImageArchive: imageName, ImageID: "sha256:" + strings.Repeat("b", 64),
	})
	entries := map[string][]byte{
		"manifest.json": manifest,
		"SHA256SUMS":    []byte(fmt.Sprintf("%s  %s\n", hex.EncodeToString(imageSum[:]), imageName)),
		imageName:       imageBytes,
	}
	var archive bytes.Buffer
	zw := zip.NewWriter(&archive)
	for name, contents := range entries {
		entry, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write(contents); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	part, err := mw.CreateFormFile("package", "aide-"+tag+"-update-linux-"+strings.TrimPrefix(platform, "linux/")+".zip")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(archive.Bytes()); err != nil {
		t.Fatal(err)
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodPost, "/api/updates/packages", &body)
	r.Header.Set("Content-Type", mw.FormDataContentType())
	r.Header.Set("Authorization", "Bearer "+a.token)
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, r)
	requireStatus(t, w, http.StatusCreated)
}

func TestUploadUpdatePackageAcceptsFullRuntimeZip(t *testing.T) {
	a := testApp(t)
	a.version = "0.1.14.0 RC9"
	a.buildVersion = a.version
	a.buildRuntimeMode = "release-image"
	tag := "v0.1.14.0-RC10"
	platform := runtimePlatform()
	imageName := "aide-" + tag + "-" + strings.ReplaceAll(platform, "/", "-") + "-image.tar.gz"
	imageBytes := []byte("compressed docker image fixture")
	imageSum := sha256.Sum256(imageBytes)
	manifest, _ := json.Marshal(updatePackageManifest{
		Format: "aide-update-package", Version: 1, ReleaseTag: tag, Platform: platform,
		ImageArchive: imageName, ImageID: "sha256:" + strings.Repeat("c", 64),
	})
	entries := map[string][]byte{
		"aide-" + tag + "-full-linux-" + strings.TrimPrefix(platform, "linux/") + "/start.command":               []byte("launcher"),
		"aide-" + tag + "-full-linux-" + strings.TrimPrefix(platform, "linux/") + "/docker-images/manifest.json": manifest,
		"aide-" + tag + "-full-linux-" + strings.TrimPrefix(platform, "linux/") + "/docker-images/SHA256SUMS":    []byte(fmt.Sprintf("%s  %s\n", hex.EncodeToString(imageSum[:]), imageName)),
		"aide-" + tag + "-full-linux-" + strings.TrimPrefix(platform, "linux/") + "/docker-images/" + imageName:  imageBytes,
	}
	var archive bytes.Buffer
	zw := zip.NewWriter(&archive)
	for name, contents := range entries {
		entry, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write(contents); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	part, err := mw.CreateFormFile("package", "aide-"+tag+"-full-linux-"+strings.TrimPrefix(platform, "linux/")+".zip")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(archive.Bytes()); err != nil {
		t.Fatal(err)
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodPost, "/api/updates/packages", &body)
	r.Header.Set("Content-Type", mw.FormDataContentType())
	r.Header.Set("Authorization", "Bearer "+a.token)
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, r)
	requireStatus(t, w, http.StatusCreated)
	var result struct {
		PackageID string `json:"packageId"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	manifestOut, imageHash, err := validateUpdatePackage(filepath.Join(a.updatePackagesPath(), result.PackageID+".zip"))
	if err != nil {
		t.Fatalf("full runtime package should be normalized to the strict updater payload: %v", err)
	}
	if manifestOut.ReleaseTag != tag || imageHash != hex.EncodeToString(imageSum[:]) {
		t.Fatalf("unexpected normalized package: manifest=%+v sha=%s", manifestOut, imageHash)
	}
}
