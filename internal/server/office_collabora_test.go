package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

func collaboraFixture(t *testing.T) (*App, map[string]any) {
	t.Helper()
	a := testApp(t)
	discovery := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/xml")
		w.Write([]byte(`<wopi-discovery><net-zone><app><action ext="docx" name="edit" urlsrc="http://internal/browser/test/cool.html?"/><action ext="docx" name="view" urlsrc="http://internal/browser/test/cool.html?"/></app></net-zone></wopi-discovery>`))
	}))
	t.Cleanup(discovery.Close)
	a.mu.Lock()
	a.pluginRegistry.Plugins = append(a.pluginRegistry.Plugins, PluginManifest{ID: "office", Enabled: true, Settings: map[string]any{"collaboraEnabled": true, "collaboraUrl": "http://editor.test:9980", "collaboraInternalUrl": discovery.URL, "collaboraCallbackUrl": "http://aide.test"}})
	a.mu.Unlock()
	dwgWrite(t, a, "sample.docx", officePreviewTestInput(t))
	w := request(a, "POST", "/api/office/collabora/sessions", map[string]any{"path": "sample.docx"})
	requireStatus(t, w, 200)
	var session map[string]any
	if json.Unmarshal(w.Body.Bytes(), &session) != nil {
		t.Fatal(w.Body.String())
	}
	return a, session
}
func collaboraRequest(a *App, s map[string]any, method, tail string, b []byte, headers map[string]string) *httptest.ResponseRecorder {
	u, _ := url.Parse(s["url"].(string))
	callback, _ := url.Parse(u.Query().Get("WOPISrc"))
	r := httptest.NewRequest(method, callback.Path+tail+"?access_token="+url.QueryEscape(s["accessToken"].(string)), bytes.NewReader(b))
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, r)
	return w
}
func TestCollaboraCapabilitiesAndVersionConflict(t *testing.T) {
	a, s := collaboraFixture(t)
	if strings.Contains(s["url"].(string), s["accessToken"].(string)) || !strings.HasPrefix(s["url"].(string), "http://editor.test:9980/browser/") {
		t.Fatal("editor URL leaked token or escaped configured origin")
	}
	w := collaboraRequest(a, s, "GET", "", nil, nil)
	requireStatus(t, w, 200)
	if !strings.Contains(w.Body.String(), `"UserCanWrite":true`) {
		t.Fatal(w.Body.String())
	}
	w = collaboraRequest(a, s, "GET", "/contents", nil, nil)
	requireStatus(t, w, 200)
	changed := append(officePreviewTestInput(t), ' ')
	w = collaboraRequest(a, s, "POST", "/contents", changed, map[string]string{"X-WOPI-Override": "PUT"})
	requireStatus(t, w, 200)
	if b, _ := a.workspace.ReadFile("sample.docx"); !bytes.Equal(b, changed) {
		t.Fatal("save missing")
	}
	dwgWrite(t, a, "sample.docx", officePreviewTestInput(t))
	w = collaboraRequest(a, s, "POST", "/contents", changed, nil)
	requireStatus(t, w, 409)
	if !strings.Contains(w.Body.String(), "1010") {
		t.Fatal("missing Collabora conflict code")
	}
	u, _ := url.Parse(s["url"].(string))
	callback, _ := url.Parse(u.Query().Get("WOPISrc"))
	r := httptest.NewRequest("GET", callback.Path+"?access_token="+a.token, nil)
	rec := httptest.NewRecorder()
	a.Handler().ServeHTTP(rec, r)
	requireStatus(t, rec, 401)
	a.mu.Lock()
	a.wsRevision++
	a.mu.Unlock()
	requireStatus(t, collaboraRequest(a, s, "GET", "", nil, nil), 401)
}
func TestCollaboraReadOnlyIdleAndActiveLease(t *testing.T) {
	a, s := collaboraFixture(t)
	ar, e := a.officeDocumentArea(officeDocumentRequest{Path: "sample.docx"})
	if e != nil {
		t.Fatal(e)
	}
	if a.collaboraBusy(ar, "sample.docx") {
		t.Fatal("merely open editor blocked AI")
	}
	requireStatus(t, request(a, "PUT", "/api/office/collabora/sessions/"+s["id"].(string), map[string]any{"active": true}), 200)
	if !a.collaboraBusy(ar, "sample.docx") {
		t.Fatal("active editor not protected")
	}
	requireStatus(t, request(a, "PUT", "/api/office/collabora/sessions/"+s["id"].(string), map[string]any{"active": false, "dirty": false}), 200)
	if a.collaboraBusy(ar, "sample.docx") {
		t.Fatal("idle saved editor blocked AI")
	}
	w := request(a, "POST", "/api/office/collabora/sessions", map[string]any{"path": "sample.docx", "readOnly": true})
	requireStatus(t, w, 200)
	var ro map[string]any
	json.Unmarshal(w.Body.Bytes(), &ro)
	requireStatus(t, collaboraRequest(a, ro, "POST", "/contents", officePreviewTestInput(t), nil), 403)
	requireStatus(t, request(a, "PUT", "/api/office/collabora/sessions/"+ro["id"].(string), map[string]any{"active": true, "dirty": true}), 200)
	if a.collaboraBusy(ar, "sample.docx") {
		t.Fatal("read-only preview blocked AI")
	}
	a.collabora.mu.Lock()
	a.collabora.sessions[s["id"].(string)].Expires = time.Now().Add(-time.Second)
	a.collabora.mu.Unlock()
	requireStatus(t, collaboraRequest(a, s, "GET", "", nil, nil), 401)
}
func TestCollaboraLocksAndReadOnlySourceRevocation(t *testing.T) {
	a, s := collaboraFixture(t)
	requireStatus(t, collaboraRequest(a, s, "POST", "", nil, map[string]string{"X-WOPI-Override": "LOCK", "X-WOPI-Lock": "one"}), 200)
	requireStatus(t, collaboraRequest(a, s, "POST", "", nil, map[string]string{"X-WOPI-Override": "LOCK", "X-WOPI-Lock": "two"}), 409)
	requireStatus(t, collaboraRequest(a, s, "POST", "/contents", officePreviewTestInput(t), map[string]string{"X-WOPI-Lock": "two"}), 409)
	requireStatus(t, collaboraRequest(a, s, "POST", "", nil, map[string]string{"X-WOPI-Override": "UNLOCK", "X-WOPI-Lock": "one"}), 200)
	os.WriteFile(a.reference.Name()+"/sample.docx", officePreviewTestInput(t), 0600)
	a.mu.Lock()
	a.sourceRegistry.Sources = append(a.sourceRegistry.Sources, Source{ID: "office-readonly", Name: "Readonly", Type: "local", Enabled: true, RW: false})
	a.mu.Unlock()
	w := request(a, "POST", "/api/office/collabora/sessions", map[string]any{"path": "sample.docx", "source": "office-readonly"})
	requireStatus(t, w, 200)
	var ro map[string]any
	json.Unmarshal(w.Body.Bytes(), &ro)
	if ro["readOnly"] != true {
		t.Fatal("source permissions bypassed")
	}
	a.mu.Lock()
	a.sourceRegistry.Sources[len(a.sourceRegistry.Sources)-1].Enabled = false
	a.mu.Unlock()
	requireStatus(t, collaboraRequest(a, ro, "GET", "/contents", nil, nil), 401)
}
func TestCollaboraConfigurationAndCSP(t *testing.T) {
	for _, raw := range []string{"javascript:alert(1)", "http://user:pass@host", "https://host/?token=x", "https://host/#x", "https://host\r\n"} {
		if _, e := collaboraURL(raw); e == nil {
			t.Fatal(raw)
		}
	}
	a, s := collaboraFixture(t)
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	if !strings.Contains(w.Header().Get("Content-Security-Policy"), "frame-src 'self' http://editor.test:9980") {
		t.Fatal("editor blocked by CSP")
	}
	a.mu.Lock()
	a.pluginRegistry.Plugins[len(a.pluginRegistry.Plugins)-1].Enabled = false
	a.mu.Unlock()
	requireStatus(t, collaboraRequest(a, s, "GET", "", nil, nil), 401)
}
