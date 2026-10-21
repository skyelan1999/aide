package server

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

func captureFixture(t *testing.T, mode string) (*App, *pluginWorkbenchBridge, *mobileCapture) {
	t.Helper()
	a, key := bridgeFixture(t)
	for i := range a.pluginRegistry.Plugins {
		a.pluginRegistry.Plugins[i].Workbench.MobileCapture = true
	}
	if _, _, e := ensureTLSCert(CertsDir(a.dataPath)); e != nil {
		t.Fatal(e)
	}
	_ = key
	w := workbenchRequest(a, "POST", "/api/plugins/workbench-fixture/workbench/open", true)
	requireStatus(t, w, 200)
	var opened struct {
		URL    string `json:"url"`
		Bridge struct {
			Instance string `json:"instanceId"`
		} `json:"bridge"`
	}
	if e := json.Unmarshal(w.Body.Bytes(), &opened); e != nil {
		t.Fatal(e)
	}
	requireStatus(t, workbenchRequest(a, "GET", opened.URL, false), 200)
	b := a.pluginWorkbenchBridges[opened.Bridge.Instance]
	v, e := a.captureControlLocked(b, map[string]any{"action": "create", "title": "Stock capture", "mode": mode, "baseline": "baseline-one", "baseURL": "https://localhost", "items": []captureItem{{ID: "1", Name: "Part", Unit: "件", Codes: []string{"SKU-1"}}}, "zones": []string{"A", "B"}})
	if e != nil {
		t.Fatal(e)
	}
	return a, b, b.Captures[v.(map[string]any)["id"].(string)]
}
func capturePhone(a *App, c *mobileCapture, token string, in map[string]any) *httptest.ResponseRecorder {
	in["session"] = c.ID
	raw, _ := json.Marshal(in)
	r := httptest.NewRequest("POST", "https://localhost/mobile-capture/api", bytes.NewReader(raw))
	r.TLS = &tls.ConnectionState{}
	r.Header.Set("Origin", "https://localhost")
	r.Header.Set("Content-Type", "application/json")
	if token != "" {
		r.Header.Set("Authorization", "Capture "+token)
	}
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, r)
	return w
}
func captureJoin(t *testing.T, a *App, b *pluginWorkbenchBridge, c *mobileCapture, name string) (string, string) {
	t.Helper()
	v, e := a.captureControlLocked(b, map[string]any{"action": "invite", "id": c.ID})
	if e != nil {
		t.Fatal(e)
	}
	url := v.(map[string]any)["url"].(string)
	parts := bytes.Split([]byte(url), []byte("#"))
	var invite, secret string
	for _, x := range bytes.Split(parts[1], []byte("&")) {
		p := bytes.SplitN(x, []byte("="), 2)
		if string(p[0]) == "invite" {
			invite = string(p[1])
		}
		if string(p[0]) == "secret" {
			secret = string(p[1])
		}
	}
	w := capturePhone(a, c, "", map[string]any{"action": "join", "invite": invite, "secret": secret, "name": name})
	requireStatus(t, w, 200)
	var out struct {
		Token string `json:"token"`
		State struct {
			Device struct {
				ID string `json:"id"`
			} `json:"device"`
		} `json:"state"`
	}
	json.Unmarshal(w.Body.Bytes(), &out)
	requireStatus(t, capturePhone(a, c, "", map[string]any{"action": "join", "invite": invite, "secret": secret, "name": "replay"}), 403)
	return out.Token, out.State.Device.ID
}
func captureApprove(t *testing.T, a *App, b *pluginWorkbenchBridge, c *mobileCapture, token, id, zone string) {
	t.Helper()
	_, e := a.captureControlLocked(b, map[string]any{"action": "approve", "id": c.ID, "device": id, "zone": zone})
	if e != nil {
		t.Fatal(e)
	}
	requireStatus(t, capturePhone(a, c, token, map[string]any{"action": "confirm"}), 200)
}
func TestMobileCaptureMutualApprovalRetriesAndRevoke(t *testing.T) {
	a, b, c := captureFixture(t, "receipt")
	token, id := captureJoin(t, a, b, c, "phone 1")
	event := map[string]any{"action": "record", "event": "one", "code": "SKU-1", "quantity": 2}
	requireStatus(t, capturePhone(a, c, token, event), 403)
	_, e := a.captureControlLocked(b, map[string]any{"action": "approve", "id": c.ID, "device": id, "zone": "A"})
	if e != nil {
		t.Fatal(e)
	}
	requireStatus(t, capturePhone(a, c, token, event), 403)
	requireStatus(t, capturePhone(a, c, token, map[string]any{"action": "confirm"}), 200)
	accepted := capturePhone(a, c, token, event)
	requireStatus(t, accepted, 200)
	if bytes.Contains(accepted.Body.Bytes(), []byte("baseline")) || bytes.Contains(accepted.Body.Bytes(), []byte(`"zones":["A","B"]`)) {
		t.Fatal("phone received desktop-only scope")
	}
	requireStatus(t, capturePhone(a, c, token, event), 200)
	if len(c.Records) != 1 {
		t.Fatal("retry duplicated journal")
	}
	event["quantity"] = 3
	requireStatus(t, capturePhone(a, c, token, event), 409)
	_, _ = a.captureControlLocked(b, map[string]any{"action": "revoke", "id": c.ID, "device": id})
	requireStatus(t, capturePhone(a, c, token, map[string]any{"action": "status"}), 403)
}
func TestMobileCaptureConcurrentDeviceZonesAndCountVersions(t *testing.T) {
	a, b, c := captureFixture(t, "count")
	t1, id1 := captureJoin(t, a, b, c, "phone 1")
	t2, id2 := captureJoin(t, a, b, c, "phone 2")
	captureApprove(t, a, b, c, t1, id1, "A")
	if _, e := a.captureControlLocked(b, map[string]any{"action": "approve", "id": c.ID, "device": id2, "zone": "A"}); e == nil {
		t.Fatal("shared exclusive zone")
	}
	captureApprove(t, a, b, c, t2, id2, "B")
	var wg sync.WaitGroup
	for _, token := range []string{t1, t2} {
		wg.Add(1)
		go func(token string) {
			defer wg.Done()
			w := capturePhone(a, c, token, map[string]any{"action": "record", "event": "count", "code": "SKU-1", "quantity": 3, "version": 0})
			if w.Code != 200 {
				t.Error(w.Code, w.Body.String())
			}
		}(token)
	}
	wg.Wait()
	if len(c.Records) != 2 {
		t.Fatal(c.Records)
	}
	requireStatus(t, capturePhone(a, c, t1, map[string]any{"action": "record", "event": "stale", "code": "SKU-1", "quantity": 4, "version": 0}), 409)
	requireStatus(t, capturePhone(a, c, t1, map[string]any{"action": "record", "event": "fresh", "code": "SKU-1", "quantity": 4, "version": 1}), 200)
	if len(c.Records) != 2 {
		t.Fatal("count accumulated instead of replacing")
	}
	_, _ = a.captureControlLocked(b, map[string]any{"action": "seal", "id": c.ID})
	requireStatus(t, capturePhone(a, c, t2, map[string]any{"action": "record", "event": "late", "code": "SKU-1", "quantity": 2, "version": 1}), 403)
}
func TestMobileCaptureTLSOriginScopeAndExpiry(t *testing.T) {
	a, b, c := captureFixture(t, "receipt")
	token, id := captureJoin(t, a, b, c, "phone")
	captureApprove(t, a, b, c, token, id, "A")
	for _, url := range []string{"http://localhost/mobile-capture/api", "https://other/mobile-capture/api"} {
		raw, _ := json.Marshal(map[string]any{"session": c.ID, "action": "status"})
		r := httptest.NewRequest("POST", url, bytes.NewReader(raw))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Origin", "https://localhost")
		r.Header.Set("Authorization", "Capture "+token)
		w := httptest.NewRecorder()
		a.Handler().ServeHTTP(w, r)
		requireStatus(t, w, 403)
	}
	requireStatus(t, capturePhone(a, c, token, map[string]any{"action": "record", "event": "unknown", "code": "not-stock", "quantity": 1}), 404)
	c.Devices[id].Seen = time.Now().Add(-11 * time.Minute)
	requireStatus(t, capturePhone(a, c, token, map[string]any{"action": "status"}), 403)
	c.Devices[id].Seen = time.Now()
	a.mu.Lock()
	a.wsRevision++
	a.mu.Unlock()
	requireStatus(t, capturePhone(a, c, token, map[string]any{"action": "status"}), 403)
}
func TestMobileCaptureNotAdminAndNoPrivateDataBeforeApproval(t *testing.T) {
	a, b, c := captureFixture(t, "receipt")
	token, _ := captureJoin(t, a, b, c, "phone")
	w := capturePhone(a, c, token, map[string]any{"action": "status"})
	if bytes.Contains(w.Body.Bytes(), []byte("SKU-1")) {
		t.Fatal("unapproved device received catalog")
	}
	r := httptest.NewRequest("GET", "https://localhost/api/config", nil)
	r.Header.Set("Authorization", "Capture "+token)
	out := httptest.NewRecorder()
	a.Handler().ServeHTTP(out, r)
	requireStatus(t, out, 401)
	for key := range a.pluginWorkbenchBridges {
		delete(a.pluginWorkbenchBridges, key)
	}
	requireStatus(t, capturePhone(a, c, token, map[string]any{"action": "status"}), 403)
	_ = b
}

func TestMobileCaptureControlOptInReplayAndRenewal(t *testing.T) {
	a, b, c := captureFixture(t, "receipt")
	var key string
	for k, v := range a.pluginWorkbenchBridges {
		if v == b {
			key = k
		}
	}
	control := func(id string) *httptest.ResponseRecorder {
		return request(a, "POST", "/api/plugins/workbench-fixture/workbench/"+key+"/call", map[string]any{"protocol": 1, "requestId": id, "method": "capture.control", "arguments": map[string]any{"action": "status", "id": c.ID}})
	}
	requireStatus(t, control("capture_1"), 200)
	requireStatus(t, control("capture_1"), 409)
	token, _ := captureJoin(t, a, b, c, "renewed phone")
	w := request(a, "POST", "/api/plugins/workbench-fixture/workbench/"+key+"/renew", map[string]any{})
	requireStatus(t, w, 200)
	requireStatus(t, capturePhone(a, c, token, map[string]any{"action": "status"}), 200)
	a.mu.Lock()
	for i := range a.pluginRegistry.Plugins {
		a.pluginRegistry.Plugins[i].Workbench.MobileCapture = false
	}
	a.mu.Unlock()
	requireStatus(t, capturePhone(a, c, token, map[string]any{"action": "status"}), 403)
}
func TestMobileCaptureLimitsAndRegionImmutability(t *testing.T) {
	a, b, c := captureFixture(t, "receipt")
	token, id := captureJoin(t, a, b, c, "phone")
	captureApprove(t, a, b, c, token, id, "A")
	if _, e := a.captureControlLocked(b, map[string]any{"action": "approve", "id": c.ID, "device": id, "zone": "B"}); e == nil {
		t.Fatal("changed assigned region")
	}
	for i := 1; i < 16; i++ {
		captureJoin(t, a, b, c, "another phone")
	}
	if _, e := a.captureControlLocked(b, map[string]any{"action": "invite", "id": c.ID}); e == nil {
		t.Fatal("more than 16 devices")
	}
	c.RateStart = time.Now()
	c.RateRequests = 600
	requireStatus(t, capturePhone(a, c, token, map[string]any{"action": "status"}), 429)
}
func TestMobileCaptureStrictJSONAndPageHeaders(t *testing.T) {
	a, _, c := captureFixture(t, "receipt")
	for _, raw := range []string{`{"session":"` + c.ID + `","action":"join","extra":true}`, `{} {}`, `{"quantity":"NaN"}`} {
		r := httptest.NewRequest("POST", "https://localhost/mobile-capture/api", bytes.NewBufferString(raw))
		r.TLS = &tls.ConnectionState{}
		r.Header.Set("Origin", "https://localhost")
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		a.Handler().ServeHTTP(w, r)
		requireStatus(t, w, 400)
	}
	w := workbenchRequest(a, "GET", "/mobile-capture.html", false)
	requireStatus(t, w, 200)
	if w.Header().Get("Permissions-Policy") != "camera=(self), microphone=(), geolocation=()" || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal(w.Header())
	}
}

func TestMobileCaptureExplicitZeroCrossScopeAndLock(t *testing.T) {
	a, b, c := captureFixture(t, "count")
	token, id := captureJoin(t, a, b, c, "phone")
	captureApprove(t, a, b, c, token, id, "A")
	for _, event := range []map[string]any{{"action": "record", "event": "missing", "code": "SKU-1"}, {"action": "record", "event": "null", "code": "SKU-1", "quantity": nil}} {
		requireStatus(t, capturePhone(a, c, token, event), 400)
	}
	requireStatus(t, capturePhone(a, c, token, map[string]any{"action": "record", "event": "zero", "code": "SKU-1", "quantity": 0}), 200)
	other := *c
	other.ID = "other-session"
	b.Captures[other.ID] = &other
	other.Devices = map[string]*captureDevice{}
	requireStatus(t, capturePhone(a, &other, token, map[string]any{"action": "status"}), 403)
	if e := a.captureCertificate("https://warehouse.invalid"); e == nil {
		t.Fatal("accepted mismatching certificate")
	}
	if e := os.WriteFile(lockStatePath(a.dataPath), []byte(`{"locked":true,"gen":4}`), 0600); e != nil {
		t.Fatal(e)
	}
	requireStatus(t, capturePhone(a, c, token, map[string]any{"action": "status"}), 403)
}

func TestMobileCaptureUnknownAndLocations(t *testing.T) {
	a, b, _ := captureFixture(t, "receipt")
	v, e := a.captureControlLocked(b, map[string]any{"action": "create", "title": "Receiving", "mode": "receipt", "baseline": "empty", "baseURL": "https://localhost", "allowUnknown": true, "items": []captureItem{}, "zones": []string{"A", "B"}, "locations": []captureLocation{{ID: "A1", Zone: "A", Name: "Shelf A", ZoneName: "Area A"}, {ID: "B1", Zone: "B", Name: "Shelf B", ZoneName: "Area B"}}})
	if e != nil {
		t.Fatal(e)
	}
	c := b.Captures[v.(map[string]any)["id"].(string)]
	token, id := captureJoin(t, a, b, c, "one")
	captureApprove(t, a, b, c, token, id, "A")
	event := map[string]any{"action": "record", "event": "unknown", "code": "NEW-001", "quantity": 3, "zone": "B", "location": "B1"}
	requireStatus(t, capturePhone(a, c, token, event), 403)
	event["zone"] = "A"
	requireStatus(t, capturePhone(a, c, token, event), 403)
	event["location"] = "A1"
	requireStatus(t, capturePhone(a, c, token, event), 200)
	requireStatus(t, capturePhone(a, c, token, event), 200)
	if len(c.Records) != 1 || c.Records[0].Code != "NEW-001" || c.Records[0].Item != "" || c.Records[0].Location != "A1" {
		t.Fatal("unknown receipt not retained")
	}
	snapshot := captureSnapshot(c, c.Devices[id])
	raw, _ := json.Marshal(snapshot)
	if strings.Contains(string(raw), "B1") || strings.Contains(string(raw), "Area B") {
		t.Fatal("unauthorized location disclosed")
	}
	other, oid := captureJoin(t, a, b, c, "two")
	captureApprove(t, a, b, c, other, oid, "A")
	if _, e = a.captureControlLocked(b, map[string]any{"action": "create", "title": "Invalid", "mode": "count", "baseline": "empty", "baseURL": "https://localhost", "allowUnknown": true, "items": []captureItem{}, "zones": []string{"A"}}); e == nil {
		t.Fatal("unknown count accepted")
	}
}

func TestMobileCaptureOrganizationPersonBinding(t *testing.T) {
	a, b, c := captureFixture(t, "receipt")
	c.RequirePerson = true
	c.People = []captureActor{{ID: "1", Name: "Operator one"}, {ID: "2", Name: "Operator two"}}
	token, id := captureJoin(t, a, b, c, "Phone")
	if _, e := a.captureControlLocked(b, map[string]any{"action": "approve", "id": c.ID, "device": id, "zone": "A"}); e == nil {
		t.Fatal("missing employee accepted")
	}
	if _, e := a.captureControlLocked(b, map[string]any{"action": "approve", "id": c.ID, "device": id, "zone": "A", "personId": "unknown"}); e == nil {
		t.Fatal("unknown employee accepted")
	}
	if _, e := a.captureControlLocked(b, map[string]any{"action": "approve", "id": c.ID, "device": id, "zone": "A", "personId": "1"}); e != nil {
		t.Fatal(e)
	}
	requireStatus(t, capturePhone(a, c, token, map[string]any{"action": "record", "event": "before-confirm", "code": "SKU-1", "quantity": 1}), 403)
	requireStatus(t, capturePhone(a, c, token, map[string]any{"action": "confirm"}), 200)
	requireStatus(t, capturePhone(a, c, token, map[string]any{"action": "record", "event": "bound", "code": "SKU-1", "quantity": 2}), 200)
	if c.Records[0].Actor == nil || c.Records[0].Actor.ID != "1" {
		t.Fatal("server did not stamp approved person")
	}
	requireStatus(t, capturePhone(a, c, token, map[string]any{"action": "record", "event": "spoof", "code": "SKU-1", "quantity": 2, "personId": "2"}), 400)
	if _, e := a.captureControlLocked(b, map[string]any{"action": "approve", "id": c.ID, "device": id, "zone": "A", "personId": "2"}); e == nil {
		t.Fatal("approved employee changed without revocation")
	}
	snapshot, _ := json.Marshal(captureSnapshot(c, c.Devices[id]))
	if bytes.Contains(snapshot, []byte("Operator two")) {
		t.Fatal("phone received other employees")
	}
	if _, e := a.captureControlLocked(b, map[string]any{"action": "revoke", "id": c.ID, "device": id}); e != nil {
		t.Fatal(e)
	}
	requireStatus(t, capturePhone(a, c, token, map[string]any{"action": "record", "event": "revoked", "code": "SKU-1", "quantity": 2}), 403)
}
