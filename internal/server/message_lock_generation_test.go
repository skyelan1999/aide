package server

import (
	"aide/internal/messaging"
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestIssue85DelayedMessageMutationRejectsLockTransition(t *testing.T) {
	for _, reunlock := range []bool{false, true} {
		for _, route := range []string{"block", "invite", "decide", "text", "group", "group-call", "signal", "voice-lease", "refresh", "chunks/owned/0", "config"} {
			name := route + "/locked"
			if reunlock {
				name = route + "/reunlocked"
			}
			t.Run(name, func(t *testing.T) {
				a := testApp(t)
				a.settings.UserName = "owned-account"
				config := messageCenterConfig{Enabled: true, Name: "owned", Network: structNetworkConfigForTest()}
				requireStatus(t, request(a, "PUT", "/api/messages/config", map[string]any{"config": config}), 200)
				peer := strings.Repeat("a", 64)
				body, _ := json.Marshal(map[string]any{"peer": peer, "blocked": true})
				if route == "config" {
					body, _ = json.Marshal(map[string]any{"config": messageCenterConfig{Enabled: false}})
				}
				gate := &notificationReadGate{source: bytes.NewReader(body), started: make(chan struct{}), release: make(chan struct{})}
				defer func() {
					select {
					case <-gate.release:
					default:
						close(gate.release)
					}
				}()
				method := "POST"
				if strings.HasPrefix(route, "chunks/") || route == "config" {
					method = "PUT"
				}
				r := httptest.NewRequest(method, "/api/messages/"+route, gate)
				r.Header.Set("Authorization", "Bearer "+a.token)
				out := httptest.NewRecorder()
				done := make(chan struct{})
				go func() { a.Handler().ServeHTTP(out, r); close(done) }()
				select {
				case <-gate.started:
				case <-time.After(3 * time.Second):
					t.Fatal("request did not reach body")
				}
				if _, err := a.writeLockState(true); err != nil {
					t.Fatal(err)
				}
				if reunlock {
					if _, err := a.writeLockState(false); err != nil {
						t.Fatal(err)
					}
				}
				close(gate.release)
				select {
				case <-done:
				case <-time.After(3 * time.Second):
					t.Fatal("request did not finish")
				}
				if out.Code != 423 {
					t.Errorf("%s mutated/admitted across lock generation: %d", route, out.Code)
				}
				if route == "block" {
					state := a.messages.service.Snapshot()["consent"].(messaging.State)
					if state.Relationships[peer].Blocked {
						t.Error("delayed block changed persisted consent after lock")
					}
				}
				if route == "config" && a.messages == nil {
					t.Error("delayed config disabled messaging after lock")
				}
			})
		}
	}
}

func TestIssue85LockCancelsAllLeasesBeforePublishingGeneration(t *testing.T) {
	a := testApp(t)
	ctx1, finish1, err := a.beginMessageMutation(context.Background(), 0)
	if err != nil {
		t.Fatal(err)
	}
	defer finish1()
	ctx2, finish2, err := a.beginMessageMutation(context.Background(), 0)
	if err != nil {
		t.Fatal(err)
	}
	defer finish2()
	done := make(chan error, 1)
	go func() { _, err := a.writeLockState(true); done <- err }()
	for _, ctx := range []context.Context{ctx1, ctx2} {
		select {
		case <-ctx.Done():
		case <-time.After(time.Second):
			t.Fatal("lock did not cancel an admitted message request")
		}
	}
	if st := a.readLockState(); st.Locked || st.Gen != 0 {
		t.Fatal("lock published before admitted request finished")
	}
	if _, _, err := a.beginMessageMutation(context.Background(), 0); err == nil {
		t.Fatal("new mutation admitted during transition")
	}
	finish1()
	finish1()
	finish2()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("lock failed to finish after leases released")
	}
	if st := a.readLockState(); !st.Locked || st.Gen != 1 {
		t.Fatal("lock generation not published")
	}
}
func TestIssue85CleanupRemainsAvailableWhileLocked(t *testing.T) {
	a := testApp(t)
	a.settings.UserName = "owned"
	requireStatus(t, request(a, "PUT", "/api/messages/config", map[string]any{"config": messageCenterConfig{Enabled: true, Name: "owned", Network: structNetworkConfigForTest()}}), 200)
	if _, err := a.writeLockState(true); err != nil {
		t.Fatal(err)
	}
	requireStatus(t, request(a, "POST", "/api/messages/voice-release", map[string]any{"owner": strings.Repeat("a", 32)}), 200)
	if w := request(a, "POST", "/api/messages/cancel", map[string]any{"id": "missing"}); w.Code == 423 {
		t.Fatal("lock blocked cancellation cleanup")
	}
}
