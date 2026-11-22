package server

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

func proposalPatchFixture(t *testing.T) (*App, *Session, *Task, *Task) {
	t.Helper()
	a := testApp(t)
	if err := os.WriteFile(filepath.Join(a.workPath, "draft.py"), []byte("disk before\n"), 0600); err != nil {
		t.Fatal(err)
	}
	old := &Task{ID: "patch-old", Status: "failed", WorkspaceID: a.wsID(), WorkspaceRev: a.wsRevision,
		Files: []Change{{Path: "draft.py", Content: "before one\n未改内容\n", BaseHash: hash([]byte("disk before\n")), Before: "disk before\n", NoSymlinks: true}}}
	current := &Task{ID: "patch-current", Status: "running", WorkspaceID: a.wsID(), WorkspaceRev: a.wsRevision}
	session := &Session{ID: "patch-chat", Runs: []*Task{old, current}}
	a.sessions[session.ID] = session
	return a, session, old, current
}

func proposalPatchArgs(run *Task, old, next string) map[string]any {
	return map[string]any{"path": "draft.py", "proposalPatch": map[string]any{
		"runId": run.ID, "sha256": hash([]byte(run.Files[0].Content)),
		"replacements": []any{map[string]any{"oldText": old, "newText": next}},
	}}
}

func TestProposalPatchHistoricalCandidatePreservesBaselineAndDisk(t *testing.T) {
	a, _, old, current := proposalPatchFixture(t)
	original := old.Files[0]
	purpose := &approvalPurpose{Goal: "synthetic edit", Reason: "test exact candidate patch"}
	text, err := a.patchPendingProposal(current, proposalPatchArgs(old, "one", "two"), purpose, true)
	if err != nil {
		t.Fatal(err)
	}
	var receipt map[string]any
	if json.Unmarshal([]byte(text), &receipt) != nil || receipt["applied"] != false || receipt["sha256"] != hash([]byte("before two\n未改内容\n")) {
		t.Fatal("bad patch receipt", text)
	}
	got := current.Files[0]
	if got.Content != "before two\n未改内容\n" || got.BaseHash != original.BaseHash || got.Before != original.Before || !got.NoSymlinks || got.Applied || got.HistoryPending || got.ApprovalPurpose != purpose || len(current.readSnapshots) != 0 {
		t.Fatalf("candidate changed baseline/read/application state: %+v", got)
	}
	if old.Files[0].Content != original.Content || old.Files[0].BaseHash != original.BaseHash {
		t.Fatal("historical candidate changed")
	}
	disk, _ := os.ReadFile(filepath.Join(a.workPath, "draft.py"))
	if string(disk) != "disk before\n" {
		t.Fatal("patch wrote disk")
	}
	encoded, _ := json.Marshal(current)
	var restored Task
	if json.Unmarshal(encoded, &restored) != nil || restored.Files[0].BaseHash != original.BaseHash || restored.Files[0].Content != got.Content {
		t.Fatal("patch provenance did not survive task persistence")
	}
}

func TestProposalPatchAtomicFailureAndExactUniqueness(t *testing.T) {
	for _, mode := range []string{"missing", "duplicate", "second-fails", "stale", "empty", "invalid-utf8", "oversized-edits", "oversized-source", "unknown-field"} {
		t.Run(mode, func(t *testing.T) {
			a, _, old, current := proposalPatchFixture(t)
			args := proposalPatchArgs(old, "one", "two")
			patch := args["proposalPatch"].(map[string]any)
			switch mode {
			case "missing":
				patch["replacements"] = []any{map[string]any{"oldText": "absent", "newText": "new"}}
			case "duplicate":
				old.Files[0].Content = "one one"
				patch["sha256"] = hash([]byte(old.Files[0].Content))
			case "second-fails":
				patch["replacements"] = append(patch["replacements"].([]any), map[string]any{"oldText": "absent", "newText": "new"})
			case "stale":
				patch["sha256"] = strings.Repeat("0", 64)
			case "empty":
				patch["replacements"] = []any{map[string]any{"oldText": "", "newText": "new"}}
			case "invalid-utf8":
				patch["replacements"] = []any{map[string]any{"oldText": "one", "newText": string([]byte{255})}}
			case "oversized-edits":
				patch["replacements"] = []any{map[string]any{"oldText": "one", "newText": strings.Repeat("x", proposalPatchEditBytes)}}
			case "oversized-source":
				old.Files[0].Content = strings.Repeat("x", proposalPatchMaxBytes+1)
				patch["sha256"] = hash([]byte(old.Files[0].Content))
			case "unknown-field":
				patch["unexpected"] = true
			}
			before := old.Files[0].Content
			if _, err := a.patchPendingProposal(current, args, nil, false); err == nil || len(current.Files) != 0 || old.Files[0].Content != before {
				t.Fatal("invalid patch changed candidate", err)
			}
		})
	}
}

func TestProposalPatchScopeAndSavedCandidates(t *testing.T) {
	for _, mode := range []string{"foreign", "revision", "workspace", "unregistered", "source-applied", "file-applied", "history-pending", "inactive", "ssh", "staging", "reference", "traversal", "both-content", "existing-current"} {
		t.Run(mode, func(t *testing.T) {
			a, _, old, current := proposalPatchFixture(t)
			args := proposalPatchArgs(old, "one", "two")
			switch mode {
			case "foreign":
				args["proposalPatch"].(map[string]any)["runId"] = "foreign-chat-run"
			case "revision":
				old.WorkspaceRev++
			case "workspace":
				current.WorkspaceID = "other"
			case "unregistered":
				delete(a.sessions, "patch-chat")
			case "source-applied":
				old.Applied = true
			case "file-applied":
				old.Files[0].Applied = true
			case "history-pending":
				old.Files[0].HistoryPending = true
			case "inactive":
				current.Status = "failed"
			case "ssh":
				current.WorkspaceMode = "ssh"
			case "staging":
				a.executionEnvironment = &executionEnvironment{}
			case "reference":
				args["source"] = "reference-id"
			case "traversal":
				args["path"] = "../draft.py"
			case "both-content":
				args["content"] = "full replacement"
			case "existing-current":
				current.Files = []Change{{Path: "draft.py", Content: "newer current candidate"}}
			}
			before, _ := json.Marshal(current.Files)
			if _, err := a.patchPendingProposal(current, args, nil, false); err == nil {
				t.Fatal("scope/saved-state mismatch accepted")
			}
			after, _ := json.Marshal(current.Files)
			if string(before) != string(after) {
				t.Fatal("rejected edit changed current candidates")
			}
		})
	}
}

func TestProposalPatchConcurrentCurrentCAS(t *testing.T) {
	a, _, old, current := proposalPatchFixture(t)
	current.Files = append(current.Files, old.Files[0])
	args := proposalPatchArgs(current, "one", "two")
	var successes atomic.Int32
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := a.patchPendingProposal(current, args, nil, false); err == nil {
				successes.Add(1)
			}
		}()
	}
	wg.Wait()
	if successes.Load() != 1 || current.Files[0].Content != "before two\n未改内容\n" {
		t.Fatal("same expected hash was applied twice", successes.Load())
	}
}

func TestProposalPatchUsesNormalToolPolicyAndLegacyContent(t *testing.T) {
	a, _, old, current := proposalPatchFixture(t)
	args := proposalPatchArgs(old, "one", "two")
	encoded, _ := json.Marshal(args)
	current.HarnessConfig = &HarnessConfig{DenyTools: []string{"write_file"}}
	out := a.executeToolCall(context.Background(), testToolCall("write_file", string(encoded)), current, nil)
	if len(current.Files) != 0 || !strings.Contains(out, "禁用") {
		t.Fatal("tool policy was bypassed", out)
	}
	current.HarnessConfig = nil
	out = a.executeToolCall(context.Background(), testToolCall("write_file", string(encoded)), current, nil)
	if len(current.Files) != 1 || current.Files[0].Content != "before two\n未改内容\n" || !strings.Contains(out, "previousSHA256") {
		t.Fatal("normal write_file patch dispatch failed", out)
	}
	current.Files = nil
	// Missing content must not silently propose an empty file.
	out = a.executeToolCall(context.Background(), testToolCall("write_file", `{"path":"empty.py"}`), current, nil)
	if len(current.Files) != 0 || !strings.Contains(out, "requires") {
		t.Fatal("missing content accepted", out)
	}
	out = a.executeToolCall(context.Background(), testToolCall("write_file", `{"path":"empty.py","content":""}`), current, nil)
	if len(current.Files) != 1 || current.Files[0].Path != "empty.py" || current.Files[0].Content != "" {
		t.Fatal("legacy explicit empty content broke", out)
	}
}

func TestProposalPatchNormalApplyRetainsStaleDiskProtection(t *testing.T) {
	for _, stale := range []bool{false, true} {
		name := "unchanged"
		if stale {
			name = "stale"
		}
		t.Run(name, func(t *testing.T) {
			a, session, old, current := proposalPatchFixture(t)
			if stale {
				// A later disk read must not silently rebase an older draft edit.
				a.rememberWorkspaceRead(current, "draft.py", []byte("user edit\n"))
			}
			if _, err := a.patchPendingProposal(current, proposalPatchArgs(old, "one", "two"), nil, false); err != nil {
				t.Fatal(err)
			}
			current.Status = "awaiting_approval"
			if stale {
				if err := os.WriteFile(filepath.Join(a.workPath, "draft.py"), []byte("user edit\n"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			response := request(a, "POST", "/api/sessions/"+session.ID+"/runs/"+current.ID+"/apply", map[string]any{})
			disk, _ := os.ReadFile(filepath.Join(a.workPath, "draft.py"))
			if stale {
				if response.Code == 200 || string(disk) != "user edit\n" || current.Applied {
					t.Fatal("stale disk was overwritten", response.Code, string(disk))
				}
			} else if response.Code != 200 || string(disk) != "before two\n未改内容\n" || !current.Applied {
				t.Fatal("normal approval failed", response.Code, response.Body.String())
			}
		})
	}
}
