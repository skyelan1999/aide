package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestPWRuntimeMissingAndInvalidSignatureRemainUnavailable(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux PW runtime")
	}
	a := testApp(t)
	a.pluginRegistry.Plugins = append(a.pluginRegistry.Plugins, PluginManifest{ID: "professional-workflow", Version: "0.4.0", Main: "index.js", Enabled: true, Workbench: &PluginWorkbench{ProfessionalWorkflow: true, Entry: "workbench.html"}})
	if a.pwAvailableLocked() {
		t.Fatal("missing signed runtime enabled PW")
	}
	dir := filepath.Join(a.pluginsPath, "professional-workflow")
	os.MkdirAll(dir, 0700)
	os.WriteFile(filepath.Join(dir, "runtime.json"), []byte(`{"schemaVersion":1,"protocol":1,"pluginId":"professional-workflow","version":"0.4.0"}`), 0600)
	os.WriteFile(filepath.Join(dir, "runtime.sig"), []byte("invalid"), 0600)
	if a.pwAvailableLocked() {
		t.Fatal("invalid signature enabled PW")
	}
	if _, err := a.pwControlLocked(map[string]any{"action": "status"}); err == nil || !strings.Contains(err.Error(), "signature") {
		t.Fatal("missing clear signature error", err)
	}
}
func TestPWSealedRuntimeCleanEnvironmentAndOutputBound(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux sealed memfd runner")
	}
	image, err := os.ReadFile("/usr/bin/env")
	if err != nil {
		t.Skip("env executable unavailable")
	}
	t.Setenv("AIDE_TEST_SECRET", "fixture-should-not-reach-runtime")
	output, err := executePWImage(context.Background(), image, nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(output), "AIDE_TEST_SECRET") || strings.Contains(string(output), "fixture-should-not-reach-runtime") {
		t.Fatal("inherited host environment")
	}
	if !strings.Contains(string(output), "HOME=/nonexistent") {
		t.Fatal("clean environment missing")
	}
	writer := &pwBoundedOutput{limit: 2}
	if _, err = writer.Write([]byte("abc")); err == nil {
		t.Fatal("output bound missing")
	}
}

func TestPWSignedRuntimeIntegrityAndVerifiedImage(t *testing.T) {
	a, _, d := pwFixture(t)
	directory := filepath.Join(a.pluginsPath, "professional-workflow")
	image, err := loadPWRuntime(directory)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = a.pwEngine(context.Background(), pwEngineRequest{Op: "validate", Definition: d}); err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(directory, "bin/linux-"+runtime.GOARCH+"/pw-engine")
	changed := append([]byte{}, image...)
	changed[len(changed)-1] ^= 1
	if err = os.Chmod(binary, 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(binary, changed, 0500); err != nil {
		t.Fatal(err)
	}
	if _, err = loadPWRuntime(directory); err == nil || !strings.Contains(err.Error(), "checksum") {
		t.Fatal("modified binary accepted", err)
	}
	// Already verified bytes execute independently of any later path replacement.
	output, err := executePWImage(context.Background(), image, []byte(`{"protocol":1,"op":"status"}`))
	if err != nil || !strings.Contains(string(output), `"protocol":1`) {
		t.Fatal("verified image execution failed", string(output), err)
	}
}

func TestPWSealedRuntimeCancellation(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux sealed runtime")
	}
	image, err := os.ReadFile("/bin/sh")
	if err != nil {
		t.Skip("shell fixture unavailable")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	started := time.Now()
	if _, err = executePWImage(ctx, image, []byte("sleep 10\n")); err == nil {
		t.Fatal("cancelled runtime completed successfully")
	}
	if time.Since(started) > 2*time.Second {
		t.Fatal("runtime cancellation did not bound pipe wait")
	}
}

func TestPWSignedAssetsAndExactWorkbenchBytes(t *testing.T) {
	for _, asset := range []string{"manifest.json", "index.js", "workbench.html"} {
		t.Run(asset, func(t *testing.T) {
			a, _, _ := pwFixture(t)
			directory := filepath.Join(a.pluginsPath, "professional-workflow")
			page, err := os.ReadFile(filepath.Join(directory, "workbench.html"))
			if err != nil {
				t.Fatal(err)
			}
			if err = verifyPWWorkbenchPage(directory, page); err != nil {
				t.Fatal(err)
			}
			modified := append([]byte{}, page...)
			modified[len(modified)-1] ^= 1
			if err = verifyPWWorkbenchPage(directory, modified); err == nil {
				t.Fatal("unsigned exact page bytes accepted")
			}
			path := filepath.Join(directory, asset)
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			raw[len(raw)-1] ^= 1
			os.Chmod(path, 0700)
			if err = os.WriteFile(path, raw, 0600); err != nil {
				t.Fatal(err)
			}
			if a.pwAvailableLocked() {
				t.Fatal("changed signed asset enabled reserved PW capability")
			}
			if _, err = a.pwEngine(context.Background(), pwEngineRequest{Op: "status"}); err == nil {
				t.Fatal("changed signed asset executed engine")
			}
		})
	}
	a, _, _ := pwFixture(t)
	a.pluginRegistry.Plugins[len(a.pluginRegistry.Plugins)-1].Workbench.Entry = "alternative.html"
	if a.pwAvailableLocked() {
		t.Fatal("registry override enabled alternate reserved workbench")
	}
}

func TestPWSignedBundleInstallAndUpgradePreserveManifest(t *testing.T) {
	source := os.Getenv("PW_TEST_RUNTIME_DIR")
	if source == "" {
		t.Skip("explicit signed runtime package fixture")
	}
	entries := map[string]string{}
	err := filepath.WalkDir(source, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, e := filepath.Rel(source, path)
		if e != nil {
			return e
		}
		raw, e := os.ReadFile(path)
		if e != nil {
			return e
		}
		entries[filepath.ToSlash(rel)] = string(raw)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	a := testApp(t)
	out := pluginBundleRequest(t, a, entries)
	requireStatus(t, out, 201)
	installed := filepath.Join(a.pluginsPath, "professional-workflow", "manifest.json")
	raw, err := os.ReadFile(installed)
	if err != nil || string(raw) != entries["manifest.json"] {
		t.Fatal("signed manifest normalized on installation", err)
	}
	if !a.pwAvailableLocked() {
		t.Fatal("installed signed package unavailable")
	}
	for i := range a.pluginRegistry.Plugins {
		if a.pluginRegistry.Plugins[i].ID == "professional-workflow" {
			a.pluginRegistry.Plugins[i].Version = "0.3.9"
			a.pluginRegistry.Plugins[i].Settings = map[string]any{"preserved": true}
		}
	}
	bundle := upgradePackage(t, "professional-workflow", entries["index.js"], entries)
	sum := sha256.Sum256(bundle)
	out = postLocalUpgrade(t, a, "professional-workflow", bundle, hex.EncodeToString(sum[:]))
	requireStatus(t, out, 200)
	raw, err = os.ReadFile(installed)
	if err != nil || string(raw) != entries["manifest.json"] {
		t.Fatal("signed manifest normalized on upgrade", err)
	}
	if !a.pwAvailableLocked() {
		t.Fatal("upgraded signed package unavailable")
	}
}
