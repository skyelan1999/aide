package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"unicode/utf8"
)

var proposalReadTool = map[string]any{"type": "function", "function": map[string]any{
	"name":        "read_proposal",
	"description": "Inspect file candidates in this same chat and workspace without applying them. Omit path to list a bounded page, or supply runId and path to read the exact candidate in UTF-8 byte chunks. read_file reads disk, not pending candidates. Returned hashes identify candidate bytes only; normal approval and disk version checks still apply.",
	"parameters": map[string]any{"type": "object", "properties": map[string]any{
		"runId":  map[string]any{"type": "string", "description": "Optional run in this chat; omitted lists all chat runs, or reads current run."},
		"path":   map[string]any{"type": "string"},
		"source": map[string]any{"type": "string", "description": "Optional source ID; omitted means workspace candidates."},
		"sha256": map[string]any{"type": "string", "description": "Optional expected candidate hash; mismatches fail."},
		"offset": map[string]any{"type": "integer", "minimum": 0, "description": "List entry offset, or UTF-8 byte offset when reading."},
		"limit":  map[string]any{"type": "integer", "minimum": 1, "description": "List page size (max50), or read bytes (max24576)."},
	}, "additionalProperties": false},
}}

func proposalReadNumber(args map[string]any, key string, fallback, maximum int) (int, error) {
	v, exists := args[key]
	if !exists {
		return fallback, nil
	}
	n, ok := v.(float64)
	if !ok || math.IsNaN(n) || math.IsInf(n, 0) || n < 0 || n != math.Trunc(n) || n > float64(maximum) {
		return 0, fmt.Errorf("invalid %s", key)
	}
	return int(n), nil
}

func (a *App) readProposal(task *Task, args map[string]any) (string, error) {
	if task == nil {
		return "", errors.New("proposal reads require a bound task")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if task.WorkspaceID == "" || task.WorkspaceID != a.wsID() || task.WorkspaceRev != a.wsRevision {
		return "", errors.New("proposal workspace changed; return to the original workspace")
	}
	var session *Session
	for _, s := range a.sessions {
		for _, r := range s.Runs {
			if r == task {
				session = s
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
	text := func(k string) string { s, _ := args[k].(string); return s }
	runID, path, source := text("runId"), text("path"), text("source")
	if source != "" {
		src, ok := a.findSource(source)
		if !ok || !sourceAIVisible(src, task.WorkspaceID) {
			return "", errors.New("candidate reference source is not currently visible")
		}
	}
	offset, err := proposalReadNumber(args, "offset", 0, math.MaxInt32)
	if err != nil {
		return "", err
	}
	encode := func(value any) (string, error) { b, e := json.Marshal(value); return string(b), e }
	if path == "" {
		limit, err := proposalReadNumber(args, "limit", 20, 50)
		if err != nil || limit == 0 {
			return "", errors.New("list limit must be 1..50")
		}
		rows := []map[string]any{}
		seen, foundRun := 0, runID == ""
		for i := len(session.Runs) - 1; i >= 0; i-- {
			r := session.Runs[i]
			if runID != "" && r.ID != runID {
				continue
			}
			foundRun = true
			if r.WorkspaceID != task.WorkspaceID || r.WorkspaceRev != task.WorkspaceRev {
				continue
			}
			for _, f := range r.Files {
				if f.Source != source {
					continue
				}
				if seen >= offset && len(rows) < limit {
					rows = append(rows, map[string]any{"runId": r.ID, "path": f.Path, "source": f.Source, "sha256": hash([]byte(f.Content)), "bytes": len(f.Content), "applied": f.Applied || r.Applied, "historyPending": f.HistoryPending, "runStatus": r.Status})
				}
				seen++
			}
		}
		if !foundRun {
			return "", errors.New("run not found in this chat")
		}
		if offset > seen {
			return "", errors.New("list offset exceeds candidate count")
		}
		return encode(map[string]any{"candidates": rows, "offset": offset, "nextOffset": offset + len(rows), "done": offset+len(rows) >= seen, "total": seen, "boundary": "candidate records only; not current disk state or verified artifacts"})
	}
	if runID == "" {
		runID = task.ID
	}
	limit, err := proposalReadNumber(args, "limit", 12288, 24576)
	if err != nil || limit == 0 {
		return "", errors.New("read limit must be 1..24576")
	}
	for _, r := range session.Runs {
		if r.ID != runID || r.WorkspaceID != task.WorkspaceID || r.WorkspaceRev != task.WorkspaceRev {
			continue
		}
		for _, f := range r.Files {
			if f.Path != path || f.Source != source {
				continue
			}
			digest := hash([]byte(f.Content))
			if expected := text("sha256"); expected != "" && expected != digest {
				return "", errors.New("candidate hash changed; list candidates again")
			}
			if !utf8.ValidString(f.Content) || offset > len(f.Content) || offset < len(f.Content) && !utf8.RuneStart(f.Content[offset]) {
				return "", errors.New("invalid UTF-8 candidate or byte offset")
			}
			end := min(offset+limit, len(f.Content))
			for end > offset && end < len(f.Content) && !utf8.RuneStart(f.Content[end]) {
				end--
			}
			if end == offset && end != len(f.Content) {
				return "", errors.New("limit cannot hold the next UTF-8 character")
			}
			return encode(map[string]any{"runId": r.ID, "path": path, "source": source, "sha256": digest, "content": f.Content[offset:end], "offset": offset, "nextOffset": end, "done": end == len(f.Content), "bytes": len(f.Content), "applied": f.Applied || r.Applied, "historyPending": f.HistoryPending, "boundary": "candidate text; does not read disk or satisfy a disk base-version check"})
		}
	}
	return "", errors.New("candidate not found in this chat and workspace")
}
