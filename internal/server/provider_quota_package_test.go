package server

import (
	"bytes"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func installQuotaPackageFile(t *testing.T, a *App, file string) {
	t.Helper()
	raw, e := os.ReadFile(file)
	if e != nil {
		t.Fatal(e)
	}
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	part, e := form.CreateFormFile("bundle", "plugin.zip")
	if e != nil {
		t.Fatal(e)
	}
	part.Write(raw)
	form.Close()
	req := httptest.NewRequest("POST", "/api/plugins/bundle", &body)
	req.Header.Set("Content-Type", form.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+a.token)
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, req)
	requireStatus(t, w, 201)
}

func actualQuotaPackageFixture(t *testing.T) *App {
	file := os.Getenv("AIDE_QUOTA_PLUGIN_PACKAGE")
	if file == "" {
		t.Skip("actual quota package opt-in")
	}
	a := testApp(t)
	installQuotaPackageFile(t, a, file)
	if f := os.Getenv("AIDE_OPENAI_PACKAGE"); f != "" {
		installQuotaPackageFile(t, a, f)
	}
	a.settings = Settings{Model: "fixture", ModelProviders: []ModelProvider{
		{ID: "bridge", Name: "Bridge fixture", BaseURL: "http://host.docker.internal:8787/v1", ActiveKeyID: "k", Keys: []ModelProviderKey{{ID: "k", Label: "Fixture"}}},
		{ID: "official", Name: "Official fixture", BaseURL: "https://api.openai.com/v1", ActiveKeyID: "k", Keys: []ModelProviderKey{{ID: "k", Label: "Fixture"}}},
	}}
	supporterVault(t, a, modelProviderVaultID("bridge", "k"), "synthetic-quota-key")
	old := http.DefaultTransport
	a.pluginUpdateClient = &http.Client{Transport: supportRoundTrip(func(*http.Request) (*http.Response, error) { return upgradeResponse([]byte(`{}`)), nil })}
	t.Cleanup(func() { http.DefaultTransport = old })
	http.DefaultTransport = supportRoundTrip(func(r *http.Request) (*http.Response, error) {
		if r.URL.String() != "http://host.docker.internal:8787/v1/usage" || r.Header.Get("Authorization") != "Bearer synthetic-quota-key" {
			t.Error("unexpected quota request")
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"available":true,"buckets":[{"id":"codex","label":"Codex fixture","primary":{"usedPercent":25,"windowDurationMins":300,"resetsAt":1793500000},"secondary":{"usedPercent":40,"windowDurationMins":10080,"resetsAt":1794000000}}]}`))}, nil
	})
	return a
}

func TestPluginQuotaActualPackage(t *testing.T) {
	a := actualQuotaPackageFixture(t)
	w := request(a, "GET", "/api/balance?providerId=bridge&keyId=k", nil)
	requireStatus(t, w, 200)
	if !strings.Contains(w.Body.String(), `"remainingPercent":75`) || !strings.Contains(w.Body.String(), `"remainingPercent":60`) {
		t.Fatal(w.Body.String())
	}
}

func TestPluginQuotaBrowserFixture(t *testing.T) {
	if os.Getenv("AIDE_QUOTA_BROWSER") != "1" {
		t.Skip("isolated browser fixture")
	}
	a := actualQuotaPackageFixture(t)
	a.token = "quota-fixture-synthetic"
	server := &http.Server{Addr: ":18980", Handler: a.Handler(), ReadHeaderTimeout: 5 * time.Second}
	t.Cleanup(func() { server.Close() })
	done := make(chan error, 1)
	go func() { done <- server.ListenAndServe() }()
	t.Log("BROWSER_READY http://127.0.0.1:18980/#token=quota-fixture-synthetic")
	select {
	case e := <-done:
		t.Fatal(e)
	case <-time.After(20 * time.Minute):
	}
}
