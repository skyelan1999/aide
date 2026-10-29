package server

import (
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
)

func issue113History(s *Session) {
	s.Messages = nil
	for i := 0; i < 40; i++ {
		s.Messages = append(s.Messages, Message{Role: "user", Content: "owned-private-assistant-message " + strings.Repeat("owned evidence ", 600)})
	}
}

func TestIssue113LockedAssistantCompactionCannotReadOrSendHistory(t *testing.T) {
	for _, configured := range []bool{false, true} {
		t.Run(map[bool]string{false: "unconfigured", true: "configured"}[configured], func(t *testing.T) {
			a := testApp(t)
			var requests atomic.Int32
			var privateSent atomic.Bool
			provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				body, _ := io.ReadAll(r.Body)
				if strings.Contains(string(body), "owned-private-assistant-message") {
					privateSent.Store(true)
				}
				jsonOut(w, 200, map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": `{"goal":"owned summary","constraints":[],"decisions":[],"files":[],"facts":[],"evidence":[],"pending":[]}`}}}})
			}))
			defer provider.Close()
			a.mu.Lock()
			s := a.findAssistantSessionLocked()
			issue113History(s)
			a.settings.BaseURL = provider.URL
			a.settings.Model = ""
			if configured {
				a.settings.Model = "test"
			}
			if err := a.save(s); err != nil {
				a.mu.Unlock()
				t.Fatal(err)
			}
			before := append([]Message(nil), s.Messages...)
			a.mu.Unlock()
			w := request(a, "POST", "/api/sessions/"+s.ID+"/compact", map[string]any{})
			if w.Code != 403 {
				t.Errorf("locked private compaction returned%d, want403", w.Code)
			}
			if requests.Load() != 0 || privateSent.Load() {
				t.Error("locked private history reached model provider")
			}
			a.mu.Lock()
			unchanged := reflect.DeepEqual(s.Messages, before) && s.CompactedMessages == 0 && s.Compact == ""
			a.mu.Unlock()
			if !unchanged {
				t.Error("locked compaction mutated private conversation")
			}
		})
	}
}

func TestIssue113AuthorizedAndOrdinarySessionCompactionRemainAvailable(t *testing.T) {
	for _, kind := range []string{"assistant", "ordinary"} {
		t.Run(kind, func(t *testing.T) {
			a := testApp(t)
			var requests atomic.Int32
			provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				jsonOut(w, 200, map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": `{"goal":"owned summary","constraints":[],"decisions":[],"files":[],"facts":[],"evidence":[],"pending":[]}`}}}})
			}))
			defer provider.Close()
			a.settings.BaseURL, a.settings.Model = provider.URL, "test"
			var s *Session
			if kind == "assistant" {
				a.mu.Lock()
				s = a.findAssistantSessionLocked()
				a.mu.Unlock()
				a.markAssistantUnlocked(s.ID)
			} else {
				created := createSession(t, a)
				a.mu.Lock()
				s = a.sessions[created.ID]
				a.mu.Unlock()
			}
			a.mu.Lock()
			issue113History(s)
			if err := a.save(s); err != nil {
				a.mu.Unlock()
				t.Fatal(err)
			}
			a.mu.Unlock()
			requireStatus(t, request(a, "POST", "/api/sessions/"+s.ID+"/compact", map[string]any{}), 200)
			if requests.Load() == 0 || s.CompactedMessages == 0 {
				t.Fatal("authorized compaction did not fold history")
			}
			if kind == "assistant" {
				before := requests.Load()
				a.clearAssistantUnlock()
				requireStatus(t, request(a, "POST", "/api/sessions/"+s.ID+"/compact", map[string]any{}), 403)
				if requests.Load() != before {
					t.Fatal("relocked private compaction reached provider")
				}
			}
		})
	}
}

func TestIssue113RelockBeforeCompactionCommitPreservesHistory(t *testing.T) {
	a := testApp(t)
	var requests atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		// This authorized request has already left the app; relocking must prevent
		// its result from being persisted, not claim to undo upstream disclosure.
		a.clearAssistantUnlock()
		jsonOut(w, 200, map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": `{"goal":"owned summary","constraints":[],"decisions":[],"files":[],"facts":[],"evidence":[],"pending":[]}`}}}})
	}))
	defer provider.Close()
	a.mu.Lock()
	s := a.findAssistantSessionLocked()
	a.settings.BaseURL, a.settings.Model = provider.URL, "test"
	issue113History(s)
	if err := a.save(s); err != nil {
		a.mu.Unlock()
		t.Fatal(err)
	}
	before := append([]Message(nil), s.Messages...)
	a.mu.Unlock()
	a.markAssistantUnlocked(s.ID)
	requireStatus(t, request(a, "POST", "/api/sessions/"+s.ID+"/compact", map[string]any{}), 403)
	if requests.Load() == 0 {
		t.Fatal("fixture did not reach authorized provider request")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if !reflect.DeepEqual(s.Messages, before) || s.CompactedMessages != 0 || s.Compact != "" {
		t.Fatal("relocked session accepted compaction result")
	}
}
