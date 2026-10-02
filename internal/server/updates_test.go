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
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckUpdatesReturnsVerifiedLauncherLinks(t *testing.T) {
	github := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/releases" || r.URL.Query().Get("per_page") != "20" {
			t.Fatalf("unexpected update request: %s", r.URL.String())
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"tag_name":"v0.1.15.0-RC1","name":"aide v0.1.15.0 RC1","html_url":"https://github.com/skyelan1999/aide/releases/tag/v0.1.15.0-RC1","body":"New version","published_at":"2026-10-01T00:00:00Z","assets":[{"name":"aide-v0.1.15.0-RC1-macos-arm64.zip","browser_download_url":"https://github.com/skyelan1999/aide/releases/download/v0.1.15.0-RC1/aide-v0.1.15.0-RC1-macos-arm64.zip"},{"name":"aide-v0.1.15.0-RC1-linux-arm64-image.tar.gz","browser_download_url":"https://github.com/skyelan1999/aide/releases/download/v0.1.15.0-RC1/image.tar.gz"},{"name":"aide-v0.1.15.0-RC1-windows-arm64.zip","browser_download_url":"https://evil.example/launcher.zip"}]}]`))
	}))
	defer github.Close()
	a := testApp(t)
	a.version = "0.1.14.0 RC9"
	a.updateAPIBase = github.URL + "/releases?per_page=20"
	a.updateHTTPClient = github.Client()

	w := request(a, "GET", "/api/updates", nil)
	requireStatus(t, w, http.StatusOK)
	var got struct {
		CurrentVersion  string        `json:"currentVersion"`
		LatestVersion   string        `json:"latestVersion"`
		UpdateAvailable bool          `json:"updateAvailable"`
		ReleaseNotes    string        `json:"releaseNotes"`
		Assets          []updateAsset `json:"assets"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.CurrentVersion != "0.1.14.0 RC9" || got.LatestVersion != "0.1.15.0-RC1" || !got.UpdateAvailable || got.ReleaseNotes != "New version" {
		t.Fatalf("unexpected update info: %+v", got)
	}
	if len(got.Assets) != 1 || !strings.HasSuffix(got.Assets[0].Name, "macos-arm64.zip") {
		t.Fatalf("only known launcher assets on github.com should be returned: %+v", got.Assets)
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
		"manifest.json":         manifest,
		"SHA256SUMS":            []byte(fmt.Sprintf("%s  %s\n", hex.EncodeToString(imageSum[:]), imageName)),
		imageName:               imageBytes,
		".DS_Store":             []byte("Finder metadata"),
		"._manifest.json":       []byte("AppleDouble metadata"),
		"__MACOSX/._SHA256SUMS": []byte("AppleDouble metadata in resource fork folder"),
	}
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	paths := make([]string, 0, len(files))
	for name, contents := range files {
		paths = append(paths, "aide-update-"+tag+"/"+name)
		part, err := mw.CreateFormFile("files", name)
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
