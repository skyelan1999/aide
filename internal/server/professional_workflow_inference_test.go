package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestProfessionalWorkflowInferenceValidation(t *testing.T) {
	for _, n := range []pwNode{{Type: "llm", Parallelism: 7}, {Type: "agent", ReasoningEffort: "extreme"}, {Type: "llm", MaxTokens: 65537}, {Type: "llm", Tools: []string{"read_file"}}, {Type: "agent", ModelRouter: "url"}, {Type: "template", Parallelism: 2}, {Type: "agent", Tools: []string{"run_shell"}, Bindings: []pwBinding{{Path: "output", Kind: "directory", Access: "write"}}}} {
		if validatePWInference(n) == nil {
			t.Fatalf("accepted %+v", n)
		}
	}
	if err := validatePWInference(pwNode{Type: "agent", ReasoningEffort: "high", Parallelism: 10, MaxTokens: 4096, Tools: []string{"read_file"}}); err != nil {
		t.Fatal(err)
	}
}
func TestProfessionalWorkflowInferenceParallelAndOptions(t *testing.T) {
	a, s, d := pwFixture(t)
	var workers, maximum, active atomic.Int32
	arrived := make(chan struct{}, 2)
	release := make(chan struct{})
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var b struct {
			Messages []Message `json:"messages"`
			Tools    []any     `json:"tools"`
			Max      int       `json:"max_tokens"`
		}
		json.NewDecoder(r.Body).Decode(&b)
		if len(b.Tools) > 0 {
			t.Error("text/analysis received tools")
		}
		last := b.Messages[len(b.Messages)-1].Content
		if strings.HasPrefix(last, "[parallel planner]") {
			parallelReply(w, `{"tasks":["section-one","section-two"]}`)
			return
		}
		if strings.HasPrefix(last, "[parallel analysis subtask]") {
			n := active.Add(1)
			workers.Add(1)
			defer active.Add(-1)
			for {
				old := maximum.Load()
				if n <= old || maximum.CompareAndSwap(old, n) {
					break
				}
			}
			arrived <- struct{}{}
			select {
			case <-release:
			case <-r.Context().Done():
				return
			}
			parallelReply(w, "worker evidence")
			return
		}
		if b.Max != 3072 {
			t.Errorf("max budget %d", b.Max)
		}
		parallelReply(w, "merged")
	}))
	defer provider.Close()
	cfg := routerSettings(t, a, provider.URL, provider.URL)
	n := pwNode{ID: "text", Type: "llm", Parallelism: 2, MaxTokens: 3072, Profile: "precise", ReasoningEffort: "low"}
	task := &Task{ID: "pw-parallel-options", Mode: "pw", Status: "running", Model: cfg.Model, ModelSelection: &SessionModelSelection{Model: cfg.Model, Strategy: "auto", Profile: "default", ReasoningEffort: "auto"}, WorkspaceID: a.wsID(), PW: &pwExecution{Definition: d, Current: "text", Nodes: map[string]*pwNodeResult{"text": {Status: "running"}}}}
	s.Runs = append(s.Runs, task)
	done := make(chan error, 1)
	go func() {
		out, err := a.executePWInference(context.Background(), s, task, cfg, n, "Analyze fixture input", map[string]Change{})
		if out != "merged" && err == nil {
			err = context.Canceled
		}
		done <- err
	}()
	for i := 0; i < 2; i++ {
		select {
		case <-arrived:
		case <-time.After(5 * time.Second):
			close(release)
			t.Fatal("PW workers did not overlap")
		}
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	nr := task.PW.Nodes["text"]
	if maximum.Load() != 2 || workers.Load() != 2 || nr.Parallel == nil || len(nr.Parallel.Parts) != 2 || nr.Usage.Total < 80 {
		t.Fatalf("missing parallel evidence %+v, workers=%d max=%d", nr, workers.Load(), maximum.Load())
	}
	if nr.Request["profile"] != "precise" || nr.Request["reasoningEffort"] != "low" || task.Parallel != nil || task.ModelSelection.ReasoningEffort != "auto" || task.AgentToolsSet {
		t.Fatal("node settings leaked or missing")
	}
}
func TestProfessionalWorkflowAgentToolLoopAndScope(t *testing.T) {
	for _, attempt := range []string{"read_file", "write_file"} {
		t.Run(attempt, func(t *testing.T) {
			a, s, d := pwFixture(t)
			os.WriteFile(filepath.Join(a.workspace.Name(), "proof.txt"), []byte("node evidence"), 0600)
			var rounds atomic.Int32
			provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var b struct {
					Messages []Message        `json:"messages"`
					Tools    []map[string]any `json:"tools"`
				}
				json.NewDecoder(r.Body).Decode(&b)
				if len(b.Tools) != 1 || b.Tools[0]["function"].(map[string]any)["name"] != "read_file" {
					t.Error("node tools not bounded")
				}
				if rounds.Add(1) == 1 {
					args := `{"path":"proof.txt"}`
					if attempt == "write_file" {
						args = `{"path":"should-not-exist.txt","content":"bad"}`
					}
					jsonOut(w, 200, map[string]any{"choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "tool_calls": []any{map[string]any{"id": "node-call", "type": "function", "function": map[string]any{"name": attempt, "arguments": args}}}}, "finish_reason": "tool_calls"}}})
					return
				}
				found := false
				for _, m := range b.Messages {
					if m.Role == "tool" {
						found = true
						if attempt == "read_file" && !strings.Contains(m.Content, "node evidence") {
							t.Error("file evidence missing")
						}
						if attempt == "write_file" && !strings.Contains(m.Content, "禁用") {
							t.Errorf("unadvertised call was not denied: %s", m.Content)
						}
					}
				}
				if !found {
					t.Error("tool result missing")
				}
				parallelReply(w, "agent done")
			}))
			defer provider.Close()
			cfg := routerSettings(t, a, provider.URL, provider.URL)
			task := &Task{ID: "pw-agent-" + attempt, Mode: "pw", Status: "running", Model: cfg.Model, ModelSelection: &SessionModelSelection{Model: cfg.Model, Strategy: "manual", Profile: "precise", ReasoningEffort: "off"}, WorkspaceID: a.wsID(), WorkspaceRev: a.wsRevision, PW: &pwExecution{Definition: d, Current: "text", Nodes: map[string]*pwNodeResult{"text": {Status: "running"}}}}
			s.Runs = append(s.Runs, task)
			out, err := a.executePWInference(context.Background(), s, task, cfg, pwNode{ID: "text", Type: "agent", Tools: []string{"read_file"}}, "Use available evidence", map[string]Change{})
			if err != nil || out != "agent done" || rounds.Load() != 2 {
				t.Fatalf("agent failed %s %v", out, err)
			}
			if len(task.Files) != 0 {
				t.Fatal("unadvertised write generated proposal")
			}
			if _, err := os.Stat(filepath.Join(a.workspace.Name(), "should-not-exist.txt")); !os.IsNotExist(err) {
				t.Fatal("unadvertised write touched disk")
			}
		})
	}
}
