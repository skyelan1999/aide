package server

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
)

func withdrawalArgs(run *Task) map[string]any {
	return map[string]any{"path": "draft.py", "proposalWithdraw": map[string]any{
		"runId": run.ID, "sha256": hash([]byte(run.Files[0].Content)), "reason": "source ownership disproved",
	}}
}

func TestProposalWithdrawalPreservesArchiveToolHistoryBaselineAndDisk(t *testing.T) {
	for _, historical := range []bool{false, true} {
		t.Run(map[bool]string{false: "current", true: "historical"}[historical], func(t *testing.T) {
			a, session, old, current := proposalPatchFixture(t)
			origin := old
			if !historical {
				current.Files = append([]Change(nil), old.Files...)
				origin = current
			}
			original := origin.Files[0]
			keep := Change{Path: "keep.txt", Content: "reviewed document", BaseHash: ""}
			origin.Files = append(origin.Files, keep)
			origin.ToolUses = []ToolUse{{Tool: "write_file", Args: "original full candidate evidence"}}
			args := withdrawalArgs(origin)
			beforeInfo, _ := os.Stat(filepath.Join(a.workPath, "draft.py"))
			text, err := a.withdrawPendingProposal(current, args)
			if err != nil {
				t.Fatal(err)
			}
			var receipt map[string]any
			if json.Unmarshal([]byte(text), &receipt) != nil || receipt["withdrawn"] != true || receipt["diskWritten"] != false || receipt["sha256"] != hash([]byte(original.Content)) {
				t.Fatal("invalid receipt", text)
			}
			if len(origin.Files) != 1 || !reflect.DeepEqual(origin.Files[0], keep) || len(origin.WithdrawnProposals) != 1 || !reflect.DeepEqual(origin.WithdrawnProposals[0].Change, original) || origin.WithdrawnProposals[0].ActorRunID != current.ID || origin.WithdrawnProposals[0].Reason != "source ownership disproved" || len(origin.ToolUses) != 1 || len(current.readSnapshots) != 0 {
				t.Fatal("withdrawal changed unrelated candidate, audit, baseline or read state")
			}
			disk, _ := os.ReadFile(filepath.Join(a.workPath, "draft.py"))
			afterInfo, _ := os.Stat(filepath.Join(a.workPath, "draft.py"))
			if string(disk) != "disk before\n" || !beforeInfo.ModTime().Equal(afterInfo.ModTime()) {
				t.Fatal("withdrawal touched original disk file")
			}
			encoded, _ := json.Marshal(session)
			var restored Session
			if json.Unmarshal(encoded, &restored) != nil {
				t.Fatal("archive not serializable")
			}
			i := 0
			if !historical {
				i = 1
			}
			if !reflect.DeepEqual(restored.Runs[i].WithdrawnProposals[0].Change, original) || len(restored.Runs[i].Files) != 1 {
				t.Fatal("withdrawal archive/pending set lost on roundtrip")
			}
			if _, err := a.withdrawPendingProposal(current, args); err == nil || len(origin.WithdrawnProposals) != 1 {
				t.Fatal("repeat withdrawal succeeded or duplicated archive")
			}
		})
	}
}

func TestProposalWithdrawalRejectsScopeStaleSavedAndMalformedWithoutMutation(t *testing.T) {
	for _, mode := range []string{"stale", "foreign", "origin-revision", "workspace", "current-revision", "unregistered", "inactive", "other-active", "completed", "task-applied", "file-applied", "history", "other-file-saved", "reference", "mixed-content", "mixed-patch", "traversal", "invalid-hash", "unknown-field", "empty-reason", "reason-too-large", "invalid-utf8", "oversize", "duplicate", "archive-budget", "ssh", "staging"} {
		t.Run(mode, func(t *testing.T) {
			a, _, old, current := proposalPatchFixture(t)
			args := withdrawalArgs(old)
			withdraw := args["proposalWithdraw"].(map[string]any)
			switch mode {
			case "stale":
				withdraw["sha256"] = strings.Repeat("0", 64)
			case "foreign":
				withdraw["runId"] = "foreign-run"
			case "origin-revision":
				old.WorkspaceRev++
			case "workspace":
				current.WorkspaceID = "other"
			case "current-revision":
				current.WorkspaceRev++
			case "unregistered":
				delete(a.sessions, "patch-chat")
			case "inactive":
				current.Status = "failed"
			case "other-active":
				old.Status = "running"
			case "completed":
				old.Status = "completed"
			case "task-applied":
				old.Applied = true
			case "file-applied":
				old.Files[0].Applied = true
			case "history":
				old.Files[0].HistoryPending = true
			case "other-file-saved":
				old.Files = append(old.Files, Change{Path: "already.txt", Applied: true})
			case "reference":
				args["source"] = "reference"
			case "mixed-content":
				args["content"] = ""
			case "mixed-patch":
				args["proposalPatch"] = map[string]any{}
			case "traversal":
				args["path"] = "../draft.py"
			case "invalid-hash":
				withdraw["sha256"] = "not-a-hash"
			case "unknown-field":
				withdraw["apply"] = true
			case "empty-reason":
				withdraw["reason"] = " "
			case "reason-too-large":
				withdraw["reason"] = strings.Repeat("r", 1025)
			case "invalid-utf8":
				withdraw["reason"] = string([]byte{255})
			case "oversize":
				old.Files[0].Content = strings.Repeat("x", proposalPatchMaxBytes+1)
				withdraw["sha256"] = hash([]byte(old.Files[0].Content))
			case "duplicate":
				old.Files = append(old.Files, old.Files[0])
			case "archive-budget":
				old.WithdrawnProposals = make([]proposalWithdrawal, 64)
			case "ssh":
				current.WorkspaceMode = "ssh"
			case "staging":
				a.executionEnvironment = &executionEnvironment{}
			}
			before, _ := json.Marshal(old)
			if _, err := a.withdrawPendingProposal(current, args); err == nil {
				t.Fatal("invalid withdrawal accepted")
			}
			after, _ := json.Marshal(old)
			if string(before) != string(after) {
				t.Fatal("failed withdrawal mutated task")
			}
		})
	}
}

func TestProposalWithdrawalRemainingFilesStillRequireNormalApprovalAndDiskVersion(t *testing.T) {
	for _, stale := range []bool{false, true} {
		t.Run(map[bool]string{false: "normal", true: "stale"}[stale], func(t *testing.T) {
			a, session, _, current := proposalPatchFixture(t)
			current.Files = []Change{{Path: "draft.py", Content: "rejected candidate", BaseHash: hash([]byte("disk before\n"))},
				{Path: "keep.txt", Content: "reviewed new document", BaseHash: ""}}
			if _, err := a.withdrawPendingProposal(current, withdrawalArgs(current)); err != nil {
				t.Fatal(err)
			}
			url := "/api/sessions/" + session.ID + "/runs/" + current.ID + "/apply"
			if w := request(a, "POST", url, map[string]any{}); w.Code != 409 {
				t.Fatal("applied running task", w.Code)
			}
			current.Status = "awaiting_approval"
			if stale {
				if err := os.WriteFile(filepath.Join(a.workPath, "keep.txt"), []byte("later user edit"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			w := request(a, "POST", url, map[string]any{})
			if (w.Code == 200) == stale {
				t.Fatal("wrong apply outcome", w.Code, w.Body.String())
			}
			disk, _ := os.ReadFile(filepath.Join(a.workPath, "draft.py"))
			if string(disk) != "disk before\n" {
				t.Fatal("withdrawn file was applied")
			}
			keep, _ := os.ReadFile(filepath.Join(a.workPath, "keep.txt"))
			want := "reviewed new document"
			if stale {
				want = "later user edit"
			}
			if string(keep) != want {
				t.Fatal("remaining file changed unexpectedly", string(keep))
			}
		})
	}
}

func TestProposalWithdrawalConcurrentExactHashOnlyOneSucceeds(t *testing.T) {
	a, _, old, current := proposalPatchFixture(t)
	args := withdrawalArgs(old)
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, err := a.withdrawPendingProposal(current, args); results <- err }()
	}
	wg.Wait()
	close(results)
	passed := 0
	for err := range results {
		if err == nil {
			passed++
		}
	}
	if passed != 1 || len(old.WithdrawnProposals) != 1 || len(old.Files) != 0 {
		t.Fatal("withdrawal was not serialized", passed)
	}
}

func TestProposalWithdrawalToolDispatchKeepsWritePolicy(t *testing.T) {
	for _, mode := range []string{"allowed", "denied", "read-only"} {
		a, _, old, current := proposalPatchFixture(t)
		if mode == "denied" {
			current.HarnessConfig = &HarnessConfig{DenyTools: []string{"write_file"}}
		}
		if mode == "read-only" {
			a.settings.SandboxMode = "read-only"
			old.Files = append(old.Files, Change{Path: "keep.txt", Content: "still pending"})
		}
		body, _ := json.Marshal(withdrawalArgs(old))
		call := ToolCall{ID: "withdraw-fixture", Type: "function"}
		call.Function.Name = "write_file"
		call.Function.Arguments = string(body)
		text := a.executeToolCall(context.Background(), call, current, map[string]Change{})
		if mode == "denied" {
			if len(old.Files) != 1 || len(old.WithdrawnProposals) != 0 || !strings.Contains(text, "禁用") {
				t.Fatal("deny policy bypassed", text)
			}
		} else if len(old.WithdrawnProposals) != 1 || !strings.Contains(text, `"withdrawn":true`) {
			t.Fatal("dispatch did not withdraw", text)
		}
		if mode == "read-only" {
			// Like ordinary write_file proposals, candidate-only withdrawal is
			// allowed. Applying remaining proposals is still forbidden.
			old.Status = "awaiting_approval"
			w := request(a, "POST", "/api/sessions/patch-chat/runs/"+old.ID+"/apply", map[string]any{})
			if w.Code != 403 || len(old.Files) != 1 {
				t.Fatal("readonly application boundary changed", w.Code)
			}
			if _, err := os.Stat(filepath.Join(a.workPath, "keep.txt")); !os.IsNotExist(err) {
				t.Fatal("readonly application wrote remaining proposal")
			}
		}
	}
}

func TestProposalWithdrawalChangesExactApprovalFingerprint(t *testing.T) {
	a, _, old, current := proposalPatchFixture(t)
	old.Status = "awaiting_approval"
	old.Files = append(old.Files, Change{Path: "keep.txt", Content: "document"})
	before := approvalFilesFingerprint(old.Files)
	if _, err := a.withdrawPendingProposal(current, withdrawalArgs(old)); err != nil {
		t.Fatal(err)
	}
	if approvalFilesFingerprint(old.Files) == before || old.Status != "awaiting_approval" || old.Applied {
		t.Fatal("withdrawal reused exact approval or applied a file")
	}
}

func TestProposalWithdrawalClosesOnlyEmptyHistoricalApprovalBatch(t *testing.T) {
	a, _, old, current := proposalPatchFixture(t)
	old.Status, old.ApprovalState, old.ApprovalDeadline = "awaiting_approval", "recommended", "future"
	ctx, cancel := context.WithCancel(context.Background())
	old.approvalCancel = cancel
	generation := old.approvalGeneration
	if _, err := a.withdrawPendingProposal(current, withdrawalArgs(old)); err != nil {
		t.Fatal(err)
	}
	if old.Status != "cancelled" || old.Applied || old.ApprovalState != "manual" || old.ApprovalDeadline != "" || old.approvalGeneration == generation || ctx.Err() == nil || current.Status != "running" || current.Applied {
		t.Fatal("empty old batch or active turn state incorrect")
	}
	// Withdrawing the only current candidate must not stop the active turn.
	current.Files = []Change{{Path: "draft.py", Content: "new rejected candidate"}}
	if _, err := a.withdrawPendingProposal(current, withdrawalArgs(current)); err != nil {
		t.Fatal(err)
	}
	if current.Status != "running" || current.Applied || len(current.Files) != 0 {
		t.Fatal("active turn stopped or counted withdrawal as application")
	}
}
