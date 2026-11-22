package server

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"path"
	"strings"
	"time"
	"unicode/utf8"
)

// Keep the complete original candidate, including its disk baseline. Withdrawal
// is an auditable change to an unexecuted proposal, never a file deletion.
type proposalWithdrawal struct {
	Change     Change `json:"change"`
	Reason     string `json:"reason"`
	At         string `json:"at"`
	ActorRunID string `json:"actorRunId"`
}

func (a *App) withdrawPendingProposal(task *Task, args map[string]any) (string, error) {
	if err := a.nativeEditorWriteError(); err != nil {
		return "", err
	}
	for _, key := range []string{"content", "proposalPatch"} {
		if _, exists := args[key]; exists {
			return "", errors.New("proposalWithdraw cannot be combined with content or proposalPatch")
		}
	}
	if value, exists := args["source"]; exists && value != "" {
		return "", errors.New("proposalWithdraw supports workspace candidates only")
	}
	p, ok := args["path"].(string)
	if !ok || !utf8.ValidString(p) || len(p) > 4096 || safePath(p) != nil || path.Clean(p) == "." {
		return "", errors.New("invalid candidate path")
	}
	p = path.Clean(p)
	withdraw, ok := args["proposalWithdraw"].(map[string]any)
	if !ok {
		return "", errors.New("proposalWithdraw must be an object")
	}
	for key := range withdraw {
		if key != "runId" && key != "sha256" && key != "reason" {
			return "", errors.New("unknown proposalWithdraw field")
		}
	}
	runID, _ := withdraw["runId"].(string)
	if value, exists := withdraw["runId"]; exists && (value != runID || !utf8.ValidString(runID) || len(runID) > 128) {
		return "", errors.New("invalid source runId")
	}
	expected, ok := withdraw["sha256"].(string)
	decoded, hashErr := hex.DecodeString(expected)
	if !ok || hashErr != nil || len(decoded) != 32 {
		return "", errors.New("proposalWithdraw requires a complete candidate SHA256")
	}
	expected = strings.ToLower(expected)
	reason, ok := withdraw["reason"].(string)
	if !ok || !utf8.ValidString(reason) || len(reason) > 1024 || strings.TrimSpace(reason) == "" {
		return "", errors.New("proposalWithdraw requires a nonempty UTF-8 reason up to1024bytes")
	}

	// Same lock order as normal application. A competing approval either finishes
	// first (and withdrawal fails) or observes the changed file fingerprint.
	a.filesMu.Lock()
	defer a.filesMu.Unlock()
	a.mu.Lock()
	defer a.mu.Unlock()
	if task == nil || task.Status != "running" || task.Applied || task.WorkspaceID == "" || task.WorkspaceID != a.wsID() || task.WorkspaceRev != a.wsRevision {
		return "", errors.New("withdrawal requires the active task in its original workspace")
	}
	if task.WorkspaceMode == "ssh" || a.workspaceMode() == "ssh" || a.executionEnvironment != nil {
		return "", errors.New("proposalWithdraw supports local workspace proposals without staging only")
	}
	var session *Session
	for _, candidate := range a.sessions {
		for _, run := range candidate.Runs {
			if run == task {
				session = candidate
				break
			}
		}
		if session != nil {
			break
		}
	}
	if session == nil {
		return "", errors.New("task is not registered in this chat")
	}
	if runID == "" {
		runID = task.ID
	}
	var origin *Task
	for _, run := range session.Runs {
		if run.ID == runID && run.WorkspaceID == task.WorkspaceID && run.WorkspaceRev == task.WorkspaceRev {
			origin = run
			break
		}
	}
	if origin == nil || origin.Applied || origin.WorkspaceMode == "ssh" {
		return "", errors.New("unsaved source run not found in this chat and workspace")
	}
	if origin != task && origin.Status != "awaiting_approval" && origin.Status != "failed" && origin.Status != "cancelled" {
		return "", errors.New("cannot withdraw another active or completed run's proposal")
	}
	index := -1
	for i, candidate := range origin.Files {
		if candidate.Applied || candidate.HistoryPending {
			return "", errors.New("cannot withdraw from a batch that has saved files or pending history recovery")
		}
		if candidate.Source == "" && candidate.Path == p {
			if index != -1 {
				return "", errors.New("ambiguous duplicate candidate path")
			}
			index = i
		}
	}
	if index < 0 {
		return "", errors.New("pending candidate not found")
	}
	original := origin.Files[index]
	if !utf8.ValidString(original.Content) || len(original.Content) > proposalPatchMaxBytes || hash([]byte(original.Content)) != expected {
		return "", errors.New("candidate SHA256 changed, invalid UTF-8 or candidate over1MiB; nothing withdrawn")
	}
	archivedBytes := len(original.Content) + len(original.Before)
	for _, previous := range origin.WithdrawnProposals {
		archivedBytes += len(previous.Change.Content) + len(previous.Change.Before)
	}
	if len(origin.WithdrawnProposals) >= 64 || archivedBytes > 8<<20 {
		return "", errors.New("withdrawal archive budget exceeded; candidate retained")
	}
	n := len(origin.WithdrawnProposals)
	origin.WithdrawnProposals = append(origin.WithdrawnProposals[:n:n], proposalWithdrawal{
		Change: original, Reason: reason, At: time.Now().UTC().Format(time.RFC3339Nano), ActorRunID: task.ID,
	})
	origin.Files = append(origin.Files[:index:index], origin.Files[index+1:]...)
	if origin != task && origin.Status == "awaiting_approval" {
		origin.approvalGeneration++
		if origin.approvalCancel != nil {
			origin.approvalCancel()
		}
		origin.ApprovalState, origin.ApprovalDeadline = "manual", ""
		if len(origin.Files) == 0 {
			// Close only the abandoned historical batch, never the active
			// modeling turn or overall project. Nothing has been applied.
			origin.Status = "cancelled"
		}
	}
	data, _ := json.Marshal(map[string]any{"path": p, "sourceRunId": runID, "sha256": expected,
		"withdrawn": true, "reason": reason, "remainingCandidates": len(origin.Files), "diskWritten": false,
		"sourceRunStatus":           origin.Status,
		"originalCandidateArchived": true,
		"boundary":                  "only the exact unsaved candidate was withdrawn; complete candidate and original tool records retained; no disk file deleted or written; remaining files still require normal approval and version checks"})
	return string(data), nil
}
