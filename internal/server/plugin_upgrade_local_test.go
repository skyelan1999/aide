package server

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"mime/multipart"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func postLocalUpgrade(t *testing.T, a *App, id string, raw []byte, sum string) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	f, e := form.CreateFormFile("bundle", "compiled.zip")
	if e != nil {
		t.Fatal(e)
	}
	f.Write(raw)
	form.WriteField("sha256", sum)
	form.Close()
	r := httptest.NewRequest("POST", "/api/plugins/"+id+"/upgrade", &body)
	r.Header.Set("Authorization", "Bearer "+a.token)
	r.Header.Set("Content-Type", form.FormDataContentType())
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, r)
	return w
}
func TestPluginLocalBinaryUpgradePreservesSettingsAndRecoverableBytes(t *testing.T) {
	a, id := upgradeFixture(t)
	binary := []byte{0x7f, 'E', 'L', 'F', 0, 0xff, 0xfe, 0x80, 0x00, 0x0a}
	raw := upgradePackage(t, id, upgradeOldCode, map[string]string{"core/engine.so": string(binary)})
	sum := sha256.Sum256(raw)
	w := postLocalUpgrade(t, a, id, raw, hex.EncodeToString(sum[:]))
	requireStatus(t, w, 200)
	actual, e := os.ReadFile(filepath.Join(a.pluginsPath, id, "core/engine.so"))
	if e != nil || !bytes.Equal(actual, binary) {
		t.Fatal("binary bytes changed", e)
	}
	var result struct{ Backup string }
	json.Unmarshal(w.Body.Bytes(), &result)
	old, e := os.ReadFile(filepath.Join(a.pluginsPath, result.Backup, "index.js"))
	if e != nil || string(old) != upgradeOldCode {
		t.Fatal("old package not recoverable", e)
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, p := range a.pluginRegistry.Plugins {
		if p.ID == id && p.Settings["marker"] != "keep" {
			t.Fatal("settings lost")
		}
	}
}
func TestPluginLocalBinaryUpgradeFailuresKeepCurrentVersion(t *testing.T) {
	for _, scenario := range []string{"digest", "id", "probe"} {
		t.Run(scenario, func(t *testing.T) {
			a, id := upgradeFixture(t)
			packageID, code := id, upgradeOldCode
			if scenario == "id" {
				packageID = "other"
			}
			if scenario == "probe" {
				code = "module.exports={apply(){throw Error('load failure')}};"
			}
			raw := upgradePackage(t, packageID, code, map[string]string{"core/engine.so": string([]byte{0, 0xff, 0xfe})})
			sum := sha256.Sum256(raw)
			expected := hex.EncodeToString(sum[:])
			if scenario == "digest" {
				expected = string(bytes.Repeat([]byte{'0'}, 64))
			}
			w := postLocalUpgrade(t, a, id, raw, expected)
			if w.Code < 400 {
				t.Fatal("bad package accepted", w.Code)
			}
			old, e := os.ReadFile(filepath.Join(a.pluginsPath, id, "index.js"))
			if e != nil || string(old) != upgradeOldCode {
				t.Fatal("failed upgrade replaced current plugin", e)
			}
		})
	}
}
