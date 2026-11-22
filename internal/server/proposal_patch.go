package server

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"strings"
	"unicode/utf8"
)

const proposalPatchMaxBytes = 1 << 20
const proposalPatchEditBytes = 64 << 10

var writeFileTool = map[string]any{"type": "function", "function": map[string]any{
	"name":        "write_file",
	"description": "Create, revise or withdraw a file proposal, never apply it. Supply exactly one of content, proposalPatch or proposalWithdraw. To revise a pending local workspace candidate, inspect read_proposal then use proposalPatch with its exact SHA256 and ordered literal replacements; oldText must match exactly once. A historical edit creates a current-run candidate retaining its original disk baseline. To retract a rejected unsaved local workspace candidate, use proposalWithdraw with its exact SHA256 and reason; original candidate/tool records are archived, no disk file is deleted or written. runId selects the same-chat/workspace source run; withdrawal from another active/completed run or a batch with any saved/history-pending file is rejected. Normal approval, visibility, editor, binding and disk-version checks remain required for remaining proposals. Existing disk files must first be fully read or attached for content proposals. New documents use source=system-docs or a user-authorized visible writable source; patch/withdraw support workspace only. Do not read pending candidates as disk files. 使用 PjPlan 时，先查询分类并按插件设置生成文件名。",
	"parameters": map[string]any{"type": "object", "properties": map[string]any{
		approvalPurposeField: approvalPurposeSchema(),
		"source":             map[string]any{"type": "string", "description": "system-docs or a visible writable reference ID; omitted means workspace. proposalPatch is workspace-only."},
		"path":               map[string]any{"type": "string"},
		"content":            map[string]any{"type": "string", "description": "Complete content; mutually exclusive with proposalPatch. Empty string explicitly proposes an empty file."},
		"proposalWithdraw": map[string]any{"type": "object", "properties": map[string]any{
			"runId":  map[string]any{"type": "string", "description": "Optional same-chat/workspace unsaved source run; default current run."},
			"sha256": map[string]any{"type": "string", "description": "Exact SHA256 of the pending candidate from read_proposal."},
			"reason": map[string]any{"type": "string", "description": "Reason for retracting this candidate, up to1024UTF8bytes."},
		}, "required": []string{"sha256", "reason"}, "additionalProperties": false},
		"proposalPatch": map[string]any{"type": "object", "properties": map[string]any{
			"runId":  map[string]any{"type": "string", "description": "Optional pending source run in this chat and workspace; default current run."},
			"sha256": map[string]any{"type": "string", "description": "Exact SHA256 from read_proposal."},
			"replacements": map[string]any{"type": "array", "minItems": 1, "maxItems": 16, "items": map[string]any{"type": "object", "properties": map[string]any{
				"oldText": map[string]any{"type": "string"}, "newText": map[string]any{"type": "string"},
			}, "required": []string{"oldText", "newText"}, "additionalProperties": false}},
		}, "required": []string{"sha256", "replacements"}, "additionalProperties": false},
	}, "required": []string{"path"}},
}}

// patchPendingProposal edits candidate bytes only. Historical candidates retain
// their original disk baseline; this never manufactures a new disk read receipt.
// All normal write_file policy, binding and editor checks run before dispatch.
func (a *App) patchPendingProposal(task *Task, args map[string]any, purpose *approvalPurpose, bound bool) (string, error) {
	if err := a.nativeEditorWriteError(); err != nil {
		return "", err
	}
	if _, exists := args["content"]; exists {
		return "", errors.New("use either content or proposalPatch, not both")
	}
	if source, _ := args["source"].(string); source != "" {
		return "", errors.New("proposalPatch currently supports workspace candidates only")
	}
	p, ok := args["path"].(string)
	if !ok || !utf8.ValidString(p) || len(p) > 4096 || safePath(p) != nil || path.Clean(p) == "." {
		return "", errors.New("invalid candidate path")
	}
	p = path.Clean(p)
	patch, ok := args["proposalPatch"].(map[string]any)
	if !ok {
		return "", errors.New("proposalPatch must be an object")
	}
	for key := range patch {
		if key != "runId" && key != "sha256" && key != "replacements" {
			return "", errors.New("unknown proposalPatch field")
		}
	}
	runID, _ := patch["runId"].(string)
	expected, ok := patch["sha256"].(string)
	decoded, hashErr := hex.DecodeString(expected)
	if !ok || hashErr != nil || len(decoded) != 32 {
		return "", errors.New("proposalPatch requires a complete candidate SHA256")
	}
	expected = strings.ToLower(expected)
	edits, ok := patch["replacements"].([]any)
	if !ok || len(edits) == 0 || len(edits) > 16 {
		return "", errors.New("proposalPatch requires 1..16 exact replacements")
	}
	type replacement struct{ old, new string }
	replacements := make([]replacement, 0, len(edits))
	total := 0
	for _, raw := range edits {
		row, ok := raw.(map[string]any)
		if !ok || len(row) != 2 {
			return "", errors.New("replacement requires only oldText and newText")
		}
		old, oldOK := row["oldText"].(string)
		newText, newOK := row["newText"].(string)
		if !oldOK || !newOK || old == "" || old == newText || !utf8.ValidString(old) || !utf8.ValidString(newText) {
			return "", errors.New("replacement must contain nonempty distinct UTF-8 oldText and UTF-8 newText")
		}
		total += len(old) + len(newText)
		if total > proposalPatchEditBytes {
			return "", errors.New("replacement text exceeds 64 KiB budget")
		}
		replacements = append(replacements, replacement{old, newText})
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	if task == nil || task.Status != "running" || task.Applied || task.WorkspaceID == "" || task.WorkspaceID != a.wsID() || task.WorkspaceRev != a.wsRevision {
		return "", errors.New("candidate edit requires the active task in its original workspace")
	}
	if task.WorkspaceMode == "ssh" || a.workspaceMode() == "ssh" || a.executionEnvironment != nil {
		return "", errors.New("proposalPatch currently supports local workspace proposals without a staging environment")
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
	if origin == nil || origin.Applied {
		return "", errors.New("pending source run not found in this chat and workspace")
	}
	var source *Change
	for i := range origin.Files {
		if origin.Files[i].Path == p && origin.Files[i].Source == "" {
			source = &origin.Files[i]
			break
		}
	}
	if source == nil || source.Applied || source.HistoryPending || !utf8.ValidString(source.Content) || len(source.Content) > proposalPatchMaxBytes {
		return "", errors.New("source candidate is missing, saved, invalid UTF-8, or over 1 MiB")
	}
	if hash([]byte(source.Content)) != expected {
		return "", errors.New("candidate SHA256 changed; inspect the current proposal before editing")
	}
	index := -1
	for i := range task.Files {
		if task.Files[i].Path == p && task.Files[i].Source == "" {
			index = i
			if origin != task || task.Files[i].Applied || task.Files[i].HistoryPending {
				return "", errors.New("current candidate already exists or was saved; edit its current hash instead")
			}
			break
		}
	}
	content := source.Content
	for i, edit := range replacements {
		if strings.Count(content, edit.old) != 1 {
			return "", fmt.Errorf("replacement %d oldText must match exactly once; no candidate changed", i+1)
		}
		content = strings.Replace(content, edit.old, edit.new, 1)
		if len(content) > proposalPatchMaxBytes {
			return "", errors.New("patched candidate exceeds 1 MiB; no candidate changed")
		}
	}
	change := Change{Path: p, Content: content, BaseHash: source.BaseHash, Before: source.Before,
		NoSymlinks: source.NoSymlinks || bound, ApprovalPurpose: purpose}
	if index < 0 {
		task.Files = append(task.Files, change)
	} else {
		task.Files[index] = change
	}
	data, _ := json.Marshal(map[string]any{"path": p, "sourceRunId": runID, "previousSHA256": expected,
		"sha256": hash([]byte(content)), "bytes": len(content), "replacements": len(replacements), "applied": false,
		"boundary": "candidate bytes only; original disk baseline retained; normal file approval, permission and version checks still required"})
	return string(data), nil
}
