package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestIssue101EvolutionCannotOverwriteInterveningEdits(t *testing.T) {
	for _, mode := range []string{"manual", "automatic"} {
		for _, edit := range []string{"save", "reset", "rollback", "reset-restore"} {
			t.Run(mode+"/"+edit, func(t *testing.T) {
				a := testApp(t)
				old := "你是 aide。" + strings.Repeat("旧", 80)
				a.setAidePersonality(t, old)
				started, release := make(chan struct{}), make(chan struct{})
				var once sync.Once
				unblock := func() { once.Do(func() { close(release) }) }
				backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					close(started)
					<-release
					jsonOut(w, 200, map[string]any{"choices": []any{map[string]any{"message": Message{Role: "assistant", Content: "你是 aide。" + strings.Repeat("新", 25)}}}})
				}))
				defer backend.Close()
				defer unblock()
				a.settings.BaseURL, a.settings.Model = backend.URL, "test"
				messages := []Message{}
				for _, line := range strings.Split(strings.TrimSpace(okSample), "\n") {
					messages = append(messages, Message{Role: "user", Content: line})
				}
				a.sessions["owned-sample"] = &Session{ID: "owned-sample", Updated: time.Now().UTC().Format(time.RFC3339Nano), Messages: messages}
				done := make(chan int, 1)
				go func() {
					if mode == "manual" {
						done <- request(a, "POST", "/api/personality/evolve", map[string]any{"id": "aide"}).Code
					} else {
						a.runAutoEvolve(personaAide, modeRefine, "owned-trigger", okSample)
						done <- 0
					}
				}()
				select {
				case <-started:
				case <-time.After(3 * time.Second):
					t.Fatal("mock evolution did not start")
				}
				switch edit {
				case "save":
					requireStatus(t, request(a, "PUT", "/api/personality", map[string]any{"id": "aide", "enabled": true, "prompt": "你是 aide。用户手动保留的新内容"}), 200)
				case "reset":
					requireStatus(t, request(a, "POST", "/api/personality/reset", map[string]any{"id": "aide"}), 200)
				case "reset-restore":
					requireStatus(t, request(a, "POST", "/api/personality/reset", map[string]any{"id": "aide"}), 200)
					a.setAidePersonality(t, old) // restore the exact original value: reset intent still invalidates the draft
				case "rollback":
					a.mu.Lock()
					st := a.personalityStateLocked(personaAide)
					st.History = []string{"你是 aide。用户选择的上一版"}
					a.personalityState.Entries[personaAide] = st
					a.mu.Unlock()
					requireStatus(t, request(a, "POST", "/api/personality/rollback", map[string]any{"id": "aide"}), 200)
				}
				a.mu.Lock()
				expected := a.personalityLocked(personaAide)
				historyLen := len(a.personalityStateLocked(personaAide).History)
				a.mu.Unlock()
				unblock()
				status := <-done
				if mode == "manual" && status != 409 {
					t.Errorf("stale manual result returned%d, want409", status)
				}
				a.mu.Lock()
				actual := a.personalityLocked(personaAide)
				gotHistory := len(a.personalityStateLocked(personaAide).History)
				a.mu.Unlock()
				if actual != expected {
					t.Error("stale model result overwrote intervening personality state")
				}
				if gotHistory != historyLen {
					t.Errorf("stale result changed rollback history: %d -> %d", historyLen, gotHistory)
				}
			})
		}
	}
}

func TestIssue101OverlappingEvolutionOnlyAdoptsCurrentDraft(t *testing.T) {
	for _, mode := range []string{"manual", "automatic"} {
		t.Run(mode, func(t *testing.T) {
			a := testApp(t)
			a.setAidePersonality(t, "你是 aide。"+strings.Repeat("旧", 80))
			started := [2]chan struct{}{make(chan struct{}), make(chan struct{})}
			releases := [2]chan struct{}{make(chan struct{}), make(chan struct{})}
			var unblocked [2]sync.Once
			release := func(i int) { unblocked[i].Do(func() { close(releases[i]) }) }
			var next atomic.Int32
			newest := "你是 aide。" + strings.Repeat("后", 25)
			backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				i := int(next.Add(1)) - 1
				if i >= 2 {
					t.Error("unexpected extra model request")
					http.Error(w, "fixture", 500)
					return
				}
				close(started[i])
				<-releases[i]
				content := newest
				if i == 0 {
					content = "你是 aide。" + strings.Repeat("先", 25)
				}
				jsonOut(w, 200, map[string]any{"choices": []any{map[string]any{"message": Message{Role: "assistant", Content: content}}}})
			}))
			defer func() { release(0); release(1); backend.Close() }()
			a.settings.BaseURL, a.settings.Model = backend.URL, "test"
			for _, line := range strings.Split(strings.TrimSpace(okSample), "\n") {
				if a.sessions["owned-sample"] == nil {
					a.sessions["owned-sample"] = &Session{ID: "owned-sample", Updated: time.Now().UTC().Format(time.RFC3339Nano)}
				}
				a.sessions["owned-sample"].Messages = append(a.sessions["owned-sample"].Messages, Message{Role: "user", Content: line})
			}
			launch := func() <-chan int {
				done := make(chan int, 1)
				go func() {
					if mode == "manual" {
						done <- request(a, "POST", "/api/personality/evolve", map[string]any{"id": "aide"}).Code
					} else {
						a.runAutoEvolve(personaAide, modeRefine, "owned-overlap", okSample)
						done <- 0
					}
				}()
				return done
			}
			first := launch()
			select {
			case <-started[0]:
			case <-time.After(3 * time.Second):
				t.Fatal("first model request not started")
			}
			second := launch()
			select {
			case <-started[1]:
			case <-time.After(3 * time.Second):
				t.Fatal("second model request not started")
			}
			release(1)
			secondStatus := <-second
			release(0)
			firstStatus := <-first
			if mode == "manual" && (secondStatus != 200 || firstStatus != 409) {
				t.Errorf("adoption statuses newer%d stale%d", secondStatus, firstStatus)
			}
			a.mu.Lock()
			current := a.personalityLocked(personaAide)
			historyLen := len(a.personalityStateLocked(personaAide).History)
			a.mu.Unlock()
			if current.Prompt != newest || current.Evolutions != 1 || historyLen != 1 {
				t.Errorf("overlap overwrote new draft or duplicated history: evolutions%d history%d", current.Evolutions, historyLen)
			}
		})
	}
}

func TestIssue101OtherPersonalityEditDoesNotInvalidateDraft(t *testing.T) {
	a := testApp(t)
	a.setAidePersonality(t, "你是 aide。"+strings.Repeat("旧", 80))
	started, unblock := make(chan struct{}), make(chan struct{})
	var once sync.Once
	release := func() { once.Do(func() { close(unblock) }) }
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-unblock
		jsonOut(w, 200, map[string]any{"choices": []any{map[string]any{"message": Message{Role: "assistant", Content: "你是 aide。" + strings.Repeat("新", 25)}}}})
	}))
	defer backend.Close()
	defer release()
	a.settings.BaseURL, a.settings.Model = backend.URL, "test"
	done := make(chan struct{})
	go func() { a.runAutoEvolve(personaAide, modeRefine, "owned-independent", okSample); close(done) }()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("model request not started")
	}
	requireStatus(t, request(a, "PUT", "/api/personality", map[string]any{"id": "xiaomi", "enabled": true, "prompt": "小秘独立维护的内容"}), 200)
	release()
	<-done
	if a.aideEvolutions(t) != 1 {
		t.Fatal("unrelated personality edit invalidated the active draft")
	}
}
