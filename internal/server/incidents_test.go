package server

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestIncidentsPluginFailureIsCorrelatedAndNeverStoresPayload(t *testing.T) {
	a := testApp(t)
	requireStatus(t, request(a, "POST", "/api/plugins", map[string]any{"id": "incident-fixture", "name": "fixture", "version": "2.3.4", "code": `module.exports={apply(ctx){ctx.tool({name:'incident_explode',parameters:{type:'object'},handler(){throw Object.assign(new TypeError('password=TOP_SECRET raw private content /home/customer/private.txt'),{code:'EIO'})}})}}`}), 201)
	ctx := context.WithValue(context.Background(), incidentContextKey{}, incidentScope{Request: "owned-request", Task: "owned-task", Workspace: "owned-workspace"})
	if _, err := a.callPluginToolContext(ctx, "incident-fixture", "incident_explode", map[string]any{"private": "MUST_NOT_STORE"}); err == nil {
		t.Fatal("fixture did not actually fail")
	}
	rows, _ := a.incidentSnapshot()
	found := false
	for _, e := range rows {
		if e.Plugin == "incident-fixture" && e.Phase == "call" {
			found = true
			if e.PluginVersion != "2.3.4" || e.Request != "owned-request" || e.Task != "owned-task" || e.Workspace != "owned-workspace" || e.Tool != "incident_explode" || len(e.ID) != 32 || len(e.Fingerprint) != 64 || len(e.CauseTypes) == 0 || e.PluginFault == nil || e.PluginFault.Type != "TypeError" || e.PluginFault.Code != "EIO" || len(e.PluginFault.Frames) == 0 || e.PluginFault.Frames[0].File != "index.js" {
				t.Fatalf("incomplete correlation: %+v", e)
			}
		}
	}
	if !found {
		t.Fatal("no plugin incident")
	}
	data, err := os.ReadFile(filepath.Join(a.dataPath, "diagnostics", "incidents.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"TOP_SECRET", "MUST_NOT_STORE", "raw private content", "/home/customer"} {
		if strings.Contains(string(data), secret) {
			t.Fatal("incident exposed payload", secret)
		}
	}
}

func TestIncidentsHTTPAuthenticationAndCrossRequestIdentity(t *testing.T) {
	a := testApp(t)
	unauth := httptest.NewRecorder()
	a.Handler().ServeHTTP(unauth, httptest.NewRequest("GET", "/api/diagnostics/incidents", nil))
	requireStatus(t, unauth, 401)
	rows, _ := a.incidentSnapshot()
	if len(rows) != 0 {
		t.Fatal("unauthenticated requests filled incident journal")
	}
	first := request(a, "GET", "/api/file?root=workspace&path=../secret", nil)
	if first.Code < 400 || first.Header().Get("X-AIDE-Incident-ID") == "" {
		t.Fatal("failure did not provide correlation header", first.Code, first.Header())
	}
	id := first.Header().Get("X-AIDE-Incident-ID")
	trace := first.Header().Get("X-AIDE-Request-ID")
	second := request(a, "GET", "/api/diagnostics/incidents?incidentId="+id, nil)
	requireStatus(t, second, 200)
	var body struct {
		Entries []incident `json:"entries"`
	}
	if err := json.Unmarshal(second.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Entries) != 1 || body.Entries[0].Request != trace || body.Entries[0].Module != "files" || second.Header().Get("X-AIDE-Request-ID") == trace {
		t.Fatal("cross-request attribution", second.Body.String())
	}
}

func TestIncidentsBoundedPersistenceAndConcurrentAttribution(t *testing.T) {
	directory := t.TempDir()
	a := &App{dataPath: directory, version: "1.2.3.4 RC1", buildCommit: strings.Repeat("a", 40)}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for n := 0; n < 4; n++ {
				a.recordIncident(context.Background(), incident{Module: "sources", Phase: "read"}, &net.OpError{Op: "dial", Net: "tcp", Err: context.DeadlineExceeded}, time.Now())
			}
		}()
	}
	wg.Wait()
	rows, health := a.incidentSnapshot()
	if len(rows) != 32 || health["persistenceErrors"] != 0 {
		t.Fatal(len(rows), health)
	}
	ids := map[string]bool{}
	for _, e := range rows {
		if ids[e.ID] || e.Code != "TIMEOUT" || !e.Retryable {
			t.Fatal("lost attribution", e)
		}
		ids[e.ID] = true
	}
	restarted := &App{dataPath: directory}
	loaded, _ := restarted.incidentSnapshot()
	if len(loaded) != 32 {
		t.Fatal("restart lost history", len(loaded))
	}
	file := filepath.Join(directory, "diagnostics", "incidents.jsonl")
	f, err := os.OpenFile(file, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.Truncate(incidentFileBytes); err != nil {
		t.Fatal(err)
	}
	f.Close()
	a.recordIncident(context.Background(), incident{Module: "sources", Phase: "read"}, errors.New("synthetic failure"), time.Time{})
	if _, err := os.Stat(file + ".1"); err != nil {
		t.Fatal("rotation missing", err)
	}
	if info, err := os.Stat(file); err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("journal privacy", err)
	}
	for n := 0; n < incidentCapacity+2; n++ {
		a.incidents.append(directory, incident{ID: crashIncidentID(), Module: "fixture"})
	}
	bounded, h := a.incidentSnapshot()
	if len(bounded) != incidentCapacity || h["droppedFromMemory"] == 0 {
		t.Fatal("unbounded retention")
	}
}

func TestIncidentsPersistenceFailureRemainsObservable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(path, []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	a := &App{dataPath: path}
	a.recordIncident(context.Background(), incident{Module: "files", Phase: "write"}, os.ErrPermission, time.Time{})
	rows, h := a.incidentSnapshot()
	if len(rows) != 1 || h["persistenceErrors"] < 1 || rows[0].Code != "ACCESS_DENIED" {
		t.Fatal(rows, h)
	}
	outside := t.TempDir()
	marker := []byte(`{"incidentId":"12345678901234567890123456789012","module":"outside-must-not-load"}` + "\n")
	if err := os.WriteFile(filepath.Join(outside, "incidents.jsonl"), marker, 0600); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "diagnostics")); err != nil {
		t.Fatal(err)
	}
	linked := &App{dataPath: root}
	rows, h = linked.incidentSnapshot()
	if len(rows) != 0 || h["persistenceErrors"] == 0 {
		t.Fatal("followed diagnostic directory link", rows, h)
	}
	linked.recordIncident(context.Background(), incident{Module: "files", Phase: "write"}, os.ErrPermission, time.Time{})
	data, err := os.ReadFile(filepath.Join(outside, "incidents.jsonl"))
	if err != nil || string(data) != string(marker) {
		t.Fatal("modified outside diagnostic store", err)
	}
}

func TestIncidentsContextFailureAndCancellationAreDifferent(t *testing.T) {
	a := testApp(t)
	if _, err := a.buildCompactionSummary(context.Background(), nil, Settings{}, ""); err == nil {
		t.Fatal("missing summary source unexpectedly accepted")
	}
	a.recordIncident(context.Background(), incident{Module: "shell", Phase: "execute"}, context.Canceled, time.Time{})
	rows, _ := a.incidentSnapshot()
	if len(rows) < 2 {
		t.Fatal(rows)
	}
	if rows[len(rows)-2].Module != "context" || rows[len(rows)-2].Phase != "summary" || rows[len(rows)-1].Severity != "info" || rows[len(rows)-1].Code != "CANCELED" {
		t.Fatal(rows)
	}
}

func TestIncidentsPanicKeepsSemanticsAndOmitsValue(t *testing.T) {
	a := testApp(t)
	func() {
		defer func() {
			if value := recover(); value != "DO_NOT_RECORD_PANIC_BODY" {
				t.Fatal("panic swallowed or changed")
			}
		}()
		defer a.observeIncidentPanic(context.Background(), incident{Module: "tools", Phase: "execute", Tool: "fixture"})
		panic("DO_NOT_RECORD_PANIC_BODY")
	}()
	rows, _ := a.incidentSnapshot()
	if len(rows) != 1 || rows[0].Code != "PANIC" || rows[0].PanicType != "string" || len(rows[0].Locations) == 0 {
		t.Fatal(rows)
	}
	encoded, _ := json.Marshal(rows)
	if strings.Contains(string(encoded), "DO_NOT_RECORD_PANIC_BODY") {
		t.Fatal("panic value leaked")
	}
}

func TestIncidentsDetectExplicitFailureResultWithoutChangingRPC(t *testing.T) {
	a := testApp(t)
	requireStatus(t, request(a, "POST", "/api/plugins", map[string]any{"id": "reported-failure", "name": "fixture", "code": `module.exports={apply(ctx){ctx.tool({name:'reported_fixture',parameters:{type:'object'},handler(){return {ok:false,error:'DO_NOT_STORE_RETURNED_PAYLOAD'}}})}}`}), 201)
	value, err := a.callPluginToolContext(context.Background(), "reported-failure", "reported_fixture", nil)
	if err != nil || !incidentReportedFailure(value) {
		t.Fatal("RPC behavior changed", value, err)
	}
	rows, _ := a.incidentSnapshot()
	if len(rows) == 0 || rows[len(rows)-1].Code != "REPORTED_FAILURE" || rows[len(rows)-1].Phase != "result-status" {
		t.Fatal(rows)
	}
	for _, normal := range []any{"plain error prose is task data", map[string]any{"ok": true}, map[string]any{"status": "completed"}, map[string]any{"nested": map[string]any{"error": "reference data"}}} {
		if incidentReportedFailure(normal) {
			t.Fatal("false positive", normal)
		}
	}
	encoded, _ := json.Marshal(rows)
	if strings.Contains(string(encoded), "DO_NOT_STORE_RETURNED_PAYLOAD") {
		t.Fatal("result content leaked")
	}
}

func TestIncidentsPluginLoadFaultAndMetadataBoundary(t *testing.T) {
	a := testApp(t)
	requireStatus(t, request(a, "POST", "/api/plugins", map[string]any{"id": "load-fault", "name": "load fault fixture", "version": "3.2.1", "code": `module.exports={apply(){throw Object.assign(new TypeError('PRIVATE_LOAD_BODY'),{code:'EIO'})}}`}), 201)
	rows, _ := a.incidentSnapshot()
	found := false
	for _, e := range rows {
		if e.Plugin == "load-fault" && e.Phase == "load-plugin" {
			found = true
			if e.PluginFault == nil || e.PluginFault.Type != "TypeError" || e.PluginFault.Code != "EIO" || e.PluginVersion != "3.2.1" || len(e.PluginFault.Frames) == 0 {
				t.Fatal("load failure attribution lost", e)
			}
		}
	}
	if !found {
		t.Fatal("plugin surface failure went unobserved")
	}
	clean := sanitizedPluginFault(&pluginFailureMetadata{Type: "PRIVATE_LOAD_BODY", Code: "PRIVATE_LOAD_BODY", Frames: []pluginFailureFrame{{File: "../../customer.txt", Line: 1, Column: 1}, {File: "/absolute/private", Line: 1, Column: 1}, {File: "C:/private.txt", Line: 1, Column: 1}, {File: "lib/index.js", Line: 12, Column: 3}}})
	encoded, _ := json.Marshal(clean)
	if strings.Contains(string(encoded), "PRIVATE_LOAD_BODY") || len(clean.Frames) != 1 || clean.Frames[0].File != "lib/index.js" {
		t.Fatal("unsafe diagnostic metadata", string(encoded))
	}
}
