package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func readToolCall(id, name, args string) ToolCall {
	call := ToolCall{ID: id}
	call.Function.Name = name
	call.Function.Arguments = args
	return call
}

func TestParallelReadOnlyToolBatchEligibility(t *testing.T) {
	task := &Task{WorkspaceMode: "local"}
	readBatch := []ToolCall{
		readToolCall("r1", "read_file", `{"path":"internal/server/a.go"}`),
		readToolCall("r2", "read_file", `{"path":"internal/server/b.go"}`),
	}
	if !parallelReadOnlyToolBatchEligible(task, readBatch) {
		t.Fatal("independent local workspace reads should be eligible")
	}
	docxBatch := []ToolCall{
		readToolCall("docx-a", "docx_structure", `{"path":"docs/a.docx"}`),
		readToolCall("docx-b", "docx_structure", `{"path":"docs/b.docx"}`),
	}
	if !parallelReadOnlyToolBatchEligible(task, docxBatch) {
		t.Fatal("independent local DOCX structure reads should be eligible")
	}
	semanticBatch := []ToolCall{
		readToolCall("semantic-a", "semantic_search", `{"query":"approval concurrency"}`),
		readToolCall("semantic-b", "semantic_search", `{"query":"workspace recovery"}`),
	}
	if !parallelReadOnlyToolBatchEligible(task, semanticBatch) {
		t.Fatal("independent local semantic queries should be eligible")
	}
	for name, calls := range map[string][]ToolCall{
		"single":               {readBatch[0]},
		"mutation":             {readBatch[0], readToolCall("w", "write_file", `{}`)},
		"remote source":        {readBatch[0], readToolCall("s", "read_file", `{"path":"x","source":"reference"}`)},
		"malformed arguments":  {readBatch[0], readToolCall("bad", "read_file", `{`)},
		"invalid DOCX path":    {docxBatch[0], readToolCall("bad-docx", "docx_structure", `{"path":"../outside.docx"}`)},
		"non-DOCX structure":   {docxBatch[0], readToolCall("txt", "docx_structure", `{"path":"notes.txt"}`)},
		"comment operation":    {docxBatch[0], readToolCall("comments", "docx_list_comments", `{"path":"docs/a.docx"}`)},
		"empty semantic query": {semanticBatch[0], readToolCall("empty", "semantic_search", `{"query":" "}`)},
	} {
		if parallelReadOnlyToolBatchEligible(task, calls) {
			t.Fatalf("%s batch must stay serial", name)
		}
	}
	tooMany := make([]ToolCall, maxParallelReadOnlyToolCalls+1)
	for i := range tooMany {
		tooMany[i] = readToolCall("read-"+string(rune('a'+i)), "read_file", `{"path":"file.txt"}`)
	}
	if parallelReadOnlyToolBatchEligible(task, tooMany) {
		t.Fatal("read batches above the concurrency cap must stay serial")
	}
	remote := *task
	remote.WorkspaceMode = "ssh"
	if parallelReadOnlyToolBatchEligible(&remote, readBatch) {
		t.Fatal("SSH reads must stay serial")
	}
	withHook := *task
	withHook.HarnessConfig = &HarnessConfig{Hooks: []HarnessHook{{Enabled: true, Event: "before_tool", Tool: "read_file"}}}
	if parallelReadOnlyToolBatchEligible(&withHook, readBatch) {
		t.Fatal("read hooks require serial execution")
	}
	docxHook := *task
	docxHook.HarnessConfig = &HarnessConfig{Hooks: []HarnessHook{{Enabled: true, Event: "after_tool", Tool: "docx_structure"}}}
	if parallelReadOnlyToolBatchEligible(&docxHook, docxBatch) {
		t.Fatal("DOCX structure hooks require serial execution")
	}
}

func TestSemanticSearchBatchCacheLoadsCorpusOnceConcurrently(t *testing.T) {
	var loads atomic.Int32
	cache := &semanticSearchBatchCache{loadCorpus: func() ([]tfidfDoc, string, string) {
		loads.Add(1)
		return []tfidfDoc{{path: "notes.txt", start: 1, end: 1, text: "approval concurrency workspace recovery", tf: map[string]float64{"approval": 1, "concurrency": 1, "workspace": 1, "recovery": 1}}}, "", ""
	}}
	queries := []string{"approval", "concurrency", "workspace", "recovery", "approval workspace"}
	var wg sync.WaitGroup
	results := make([]string, len(queries))
	for i, query := range queries {
		wg.Add(1)
		go func(i int, query string) {
			defer wg.Done()
			results[i] = cache.search(nil, query, (*os.Root)(nil))
		}(i, query)
	}
	wg.Wait()
	if loads.Load() != 1 {
		t.Fatalf("shared semantic corpus loaded %d times; want once", loads.Load())
	}
	for i, result := range results {
		if !strings.Contains(result, "notes.txt") || !strings.Contains(result, "本地 TF-IDF") {
			t.Fatalf("query %q did not rank against the shared corpus: %q", queries[i], result)
		}
	}
}

func TestExecuteIndexedToolBatchRunsConcurrentlyAndKeepsCallOrder(t *testing.T) {
	calls := []ToolCall{
		readToolCall("slow", "read_file", `{"path":"slow"}`),
		readToolCall("fast", "read_file", `{"path":"fast"}`),
	}
	started := make(chan struct{}, len(calls))
	release := make(chan struct{})
	var active, maximum atomic.Int32
	done := make(chan []string, 1)
	go func() {
		done <- executeIndexedToolBatch(context.Background(), calls, func(_ context.Context, call ToolCall) string {
			current := active.Add(1)
			for old := maximum.Load(); current > old && !maximum.CompareAndSwap(old, current); old = maximum.Load() {
			}
			started <- struct{}{}
			<-release
			active.Add(-1)
			return call.ID
		})
	}()
	for range calls {
		select {
		case <-started:
		case <-time.After(time.Second):
			close(release)
			t.Fatal("read calls were not started concurrently")
		}
	}
	close(release)
	select {
	case results := <-done:
		if maximum.Load() != 2 || len(results) != 2 || results[0] != "slow" || results[1] != "fast" {
			t.Fatalf("unexpected concurrency/order: max=%d results=%v", maximum.Load(), results)
		}
	case <-time.After(time.Second):
		t.Fatal("read batch did not complete")
	}
}

func TestToolLoopParallelReadBatchKeepsResultAndAuditOrder(t *testing.T) {
	a, _, task := parallelFixture(t, 1)
	if err := a.workspace.WriteFile("a.txt", []byte("alpha"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := a.workspace.WriteFile("b.txt", []byte("bravo"), 0600); err != nil {
		t.Fatal(err)
	}
	task.WorkspaceMode = "local"
	task.WorkspaceID = a.wsID()
	task.Prompt = "Read both files and compare their contents."
	var requests atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requests.Add(1) == 1 {
			jsonOut(w, 200, map[string]any{"choices": []any{map[string]any{"finish_reason": "tool_calls", "message": map[string]any{"role": "assistant", "tool_calls": []any{
				map[string]any{"id": "read-a", "type": "function", "function": map[string]any{"name": "read_file", "arguments": `{"path":"a.txt"}`}},
				map[string]any{"id": "read-b", "type": "function", "function": map[string]any{"name": "read_file", "arguments": `{"path":"b.txt"}`}},
			}}}}, "usage": map[string]int{"prompt_tokens": 10, "completion_tokens": 10, "total_tokens": 20}})
			return
		}
		parallelReply(w, "a.txt contains alpha; b.txt contains bravo.")
	}))
	defer provider.Close()
	out, chain, err := a.toolLoop(context.Background(), Settings{BaseURL: provider.URL, Model: "fixture"}, []Message{{Role: "user", Content: task.Prompt}}, ProfileParams{MaxTokens: 512}, builtinTools, task, map[string]Change{}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "alpha") || !strings.Contains(out, "bravo") || len(task.ToolUses) != 2 || task.ToolUses[0].Tool != "read_file" || !strings.Contains(task.ToolUses[0].Result, "alpha") || !strings.Contains(task.ToolUses[1].Result, "bravo") {
		t.Fatalf("read batch results or audit order lost: out=%q tools=%#v", out, task.ToolUses)
	}
	if len(chain) < 3 || chain[1].Role != "assistant" || len(chain[1].ToolCalls) != 2 || chain[2].Role != "tool" || chain[2].ToolCallID != "read-a" {
		t.Fatalf("model tool result order changed: %#v", chain)
	}
}

func TestToolLoopBatchesLocalSemanticSearches(t *testing.T) {
	a, _, task := parallelFixture(t, 1)
	for name, content := range map[string]string{
		"search-auth.md":     "Approval policy controls authentication boundaries and reviewer decisions.",
		"search-recovery.md": "Workspace recovery restores checkpoints after interrupted execution.",
	} {
		if err := a.workspace.WriteFile(name, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	task.WorkspaceMode = "local"
	task.WorkspaceID = a.wsID()
	task.Prompt = "Search the workspace for approval policy and recovery checkpoints."
	var requests atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requests.Add(1) == 1 {
			jsonOut(w, 200, map[string]any{"choices": []any{map[string]any{"finish_reason": "tool_calls", "message": map[string]any{"role": "assistant", "tool_calls": []any{
				map[string]any{"id": "search-auth", "type": "function", "function": map[string]any{"name": "semantic_search", "arguments": `{"query":"approval policy authentication"}`}},
				map[string]any{"id": "search-recovery", "type": "function", "function": map[string]any{"name": "semantic_search", "arguments": `{"query":"workspace recovery checkpoint"}`}},
			}}}}, "usage": map[string]int{"prompt_tokens": 10, "completion_tokens": 10, "total_tokens": 20}})
			return
		}
		parallelReply(w, "Both independent local searches completed.")
	}))
	defer provider.Close()
	out, chain, err := a.toolLoop(context.Background(), Settings{BaseURL: provider.URL, Model: "fixture"}, []Message{{Role: "user", Content: task.Prompt}}, ProfileParams{MaxTokens: 512}, builtinTools, task, map[string]Change{}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Both independent local searches completed") || len(task.ToolUses) != 2 || !strings.Contains(task.ToolUses[0].Result, "search-auth.md") || !strings.Contains(task.ToolUses[1].Result, "search-recovery.md") {
		t.Fatalf("semantic batch results were missing or reordered: out=%q uses=%#v", out, task.ToolUses)
	}
	if len(chain) < 3 || chain[2].Role != "tool" || chain[2].ToolCallID != "search-auth" {
		t.Fatalf("semantic batch changed tool-result order: %#v", chain)
	}
}
