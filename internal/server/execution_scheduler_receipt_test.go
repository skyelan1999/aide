package server

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestExecutionSchedulerReceiptRebuildsAfterCompaction(t *testing.T) {
	a, task, p := environmentFixture(t, 5)
	p.jobs["ready"] = &executionJob{result: ExecutionEnvironmentResult{ID: "ready", Status: "completed", Assignment: "Implement invoices", Changes: []Change{{Path: "invoices.js", Content: "secret candidate content"}}, Output: "unverified worker output"}}
	input := []Message{{Role: "system", Content: "user policy"}, {Role: "user", Content: "build this project"}}
	first := p.withSchedulerReceipt(input)
	if len(first) != 3 || !strings.Contains(first[2].Content, `"collected":false`) || strings.Contains(first[2].Content, "secret candidate content") || strings.Contains(first[2].Content, "unverified worker output") {
		t.Fatal("missing scheduling evidence or leaked raw worker content", first)
	}
	// Simulate collection, an unapplied candidate and a compressed conversation
	// that has discarded every environment tool result. State must still survive.
	p.jobs["ready"].result.Collected = true
	p.jobs["ready"].result.Merged = true
	a.mu.Lock()
	task.Files = []Change{{Path: "invoices.js", Content: "candidate"}, {Path: "invoices.js", Content: "revision"}, {Path: "old.js", Applied: true}}
	task.OrchestrationEvents = []OrchestrationEvent{{Kind: "snapshot_copy", ElapsedMillis: 125}}
	a.mu.Unlock()
	compressed := []Message{input[0], {Role: "system", Content: "historical summary omits job ids"}, input[1], first[2]}
	got := p.withSchedulerReceipt(compressed)
	if len(got) != 4 || !strings.Contains(got[3].Content, `"mergedAsPending":true`) || !strings.Contains(got[3].Content, `"totalPendingPaths":1`) || !strings.Contains(got[3].Content, `"averageMillis":125`) || strings.Contains(got[3].Content, "old.js") || !strings.Contains(got[3].Content, "NOT independently verified") {
		t.Fatal("receipt duplicated, stale or misrepresented pending work", got)
	}
	if len(task.Files) != 3 || task.Files[0].Applied {
		t.Fatal("receipt applied or changed pending proposals")
	}
}

func TestExecutionSchedulerReceiptLeavesOrdinaryProjectUnchanged(t *testing.T) {
	_, _, p := environmentFixture(t, 1)
	in := []Message{{Role: "user", Content: "ordinary work"}}
	out := p.withSchedulerReceipt(in)
	if len(out) != len(in) || &out[0] != &in[0] {
		t.Fatal("1X scheduling altered ordinary input")
	}
}

func TestExecutionSchedulerReceiptBoundsPromptWithoutLimitingWork(t *testing.T) {
	_, task, p := environmentFixture(t, 10)
	for i := 0; i < 40; i++ {
		id := fmt.Sprint(i)
		p.jobs[id] = &executionJob{result: ExecutionEnvironmentResult{ID: id, Assignment: strings.Repeat("untrusted\n", 300), Status: "completed", Collected: i != 39, Changes: []Change{{Path: strings.Repeat("目录", 1000)}}}}
	}
	for i := 0; i < 300; i++ {
		task.Files = append(task.Files, Change{Path: fmt.Sprint(i) + strings.Repeat("目录", 1000)})
	}
	out := p.withSchedulerReceipt(nil)
	content := out[0].Content
	body := content[strings.LastIndex(content, "\n")+1:]
	var data struct {
		TotalJobs         int                   `json:"totalJobs"`
		TotalPendingPaths int                   `json:"totalPendingPaths"`
		Jobs              []struct{ ID string } `json:"jobs"`
	}
	if len(body) > 12000 || json.Unmarshal([]byte(body), &data) != nil || data.TotalJobs != 40 || data.TotalPendingPaths != 300 || data.Jobs[0].ID != "39" {
		t.Fatal("large work lost pending counts, unread job or prompt budget", len(body), data)
	}
	if len(task.Files) != 300 || len(p.jobs) != 40 {
		t.Fatal("prompt presentation limits changed actual work")
	}
}
