package server

// Targeted, backend-observed before states complement a bounded workspace scan.
// The model supplies paths, never existence claims, hashes, or acceptance.
import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"
)

type artifactPathBefore struct {
	Existed bool   `json:"existed"`
	Size    int64  `json:"size,omitempty"`
	SHA256  string `json:"sha256,omitempty"`
}

var artifactPrepareTool = map[string]any{"type": "function", "function": map[string]any{
	"name":        "prepare_artifacts",
	"description": "Before exporting artifact files, record the exact intended local-workspace output paths. This read-only operation captures backend before-state evidence for completion checks when a large workspace's initial scan is incomplete. Call before creating/modifying the outputs. No file is written or approved; unchanged old files never become new delivery. Up to16 paths per call/128 per task. Normal command and write approvals still apply.",
	"parameters": map[string]any{"type": "object", "properties": map[string]any{
		"paths": map[string]any{"type": "array", "minItems": 1, "maxItems": 16, "items": map[string]any{"type": "string"}},
	}, "required": []string{"paths"}, "additionalProperties": false},
}}

func captureArtifactPathBefore(root *os.Root, p string, remaining *int64) (artifactPathBefore, error) {
	parts := strings.Split(p, "/")
	for i := range parts {
		prefix := strings.Join(parts[:i+1], "/")
		st, err := root.Lstat(prefix)
		if os.IsNotExist(err) {
			return artifactPathBefore{}, nil
		}
		if err != nil {
			return artifactPathBefore{}, err
		}
		if st.Mode()&os.ModeSymlink != 0 {
			return artifactPathBefore{}, errors.New("artifact paths cannot contain symlinks")
		}
		if i < len(parts)-1 && !st.IsDir() {
			return artifactPathBefore{}, errors.New("artifact parent is not a directory")
		}
		if i == len(parts)-1 && !st.Mode().IsRegular() {
			return artifactPathBefore{}, errors.New("artifact target is not a regular file")
		}
	}
	f, info, err := openDeliveryFile(root, p)
	if err != nil {
		return artifactPathBefore{}, err
	}
	defer f.Close()
	digest := deliveryDigest(f, info, remaining)
	if digest == "" {
		return artifactPathBefore{}, errors.New("before-state hash unavailable or64MiB read budget exceeded; choose a fresh output path")
	}
	return artifactPathBefore{Existed: true, Size: info.Size(), SHA256: digest}, nil
}

func (a *App) prepareArtifacts(ctx context.Context, task *Task, args map[string]any) (string, error) {
	if task == nil {
		return "", errors.New("artifact preparation requires a task")
	}
	values, ok := args["paths"].([]any)
	if !ok || len(values) < 1 || len(values) > 16 {
		return "", errors.New("paths must contain1..16 artifact paths")
	}
	paths := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, v := range values {
		p, ok := v.(string)
		if !ok || len(p) > 4096 || safePath(p) != nil || !deliveryFileType(p) {
			return "", errors.New("invalid artifact path or unsupported artifact extension")
		}
		p = filepath.ToSlash(filepath.Clean(p))
		if seen[p] {
			return "", errors.New("duplicate artifact path")
		}
		seen[p] = true
		paths = append(paths, p)
	}
	a.filesMu.Lock()
	defer a.filesMu.Unlock()
	a.mu.Lock()
	owner := a.approvalSessionLocked(task)
	if task == nil || owner == nil || owner.Deleted || task.WorkspaceID != a.wsID() || task.WorkspaceRev != a.wsRevision || a.workspaceMode() == "ssh" || task.WorkspaceMode == "ssh" {
		a.mu.Unlock()
		return "", errors.New("artifact preparation requires a registered task in the unchanged current local workspace")
	}
	root, id, revision, dataPath := a.workspace, a.wsID(), a.wsRevision, a.dataPath
	old := task.DeliveryBaseline
	var next TaskDeliveryBaseline
	if old != nil {
		if old.WorkspaceID != id || old.Revision != revision {
			a.mu.Unlock()
			return "", errors.New("artifact baseline belongs to another workspace")
		}
		next = *old
		next.Prepared = maps.Clone(old.Prepared)
	} else {
		next = TaskDeliveryBaseline{WorkspaceID: id, Revision: revision}
	}
	if next.Prepared == nil {
		next.Prepared = map[string]artifactPathBefore{}
	}
	a.mu.Unlock()
	remaining := int64(deliveryBaselineReadBytes)
	receipts := []map[string]any{}
	for _, p := range paths {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		if allowed, _ := canAccessMemory(dataPath, callerAide, filepath.Join(root.Name(), filepath.FromSlash(p)), opRead); !allowed {
			return "", errors.New("artifact path is not readable by this task")
		}
		for _, excluded := range next.Excluded {
			if excluded == "." || p == excluded || strings.HasPrefix(p, excluded+"/") {
				return "", errors.New("artifact path is excluded from this task's workspace scope")
			}
		}
		before, known := next.Prepared[p]
		if !known {
			if original, exists := next.Files[p]; exists && original.SHA256 == "" {
				return "", errors.New("original artifact content was not hashed; choose a fresh output path")
			}
			if len(next.Prepared) >= 128 {
				return "", errors.New("prepared artifact budget128 reached; finish the current verified batch")
			}
			var err error
			before, err = captureArtifactPathBefore(root, p, &remaining)
			if err != nil {
				return "", fmt.Errorf("%s: %w", p, err)
			}
			next.Prepared[p] = before
		}
		receipts = append(receipts, map[string]any{"path": p, "before": before, "alreadyPrepared": known})
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	if a.workspace != root || a.wsID() != id || a.wsRevision != revision || a.dataPath != dataPath || task.WorkspaceID != id || task.WorkspaceRev != revision || task.WorkspaceMode == "ssh" || task.DeliveryBaseline != old || a.approvalSessionLocked(task) != owner || owner.Deleted {
		return "", errors.New("artifact task/workspace changed during observation")
	}
	task.DeliveryBaseline = &next
	if err := a.save(owner); err != nil {
		task.DeliveryBaseline = old
		return "", fmt.Errorf("artifact before-state evidence not saved: %w", err)
	}
	b, err := json.Marshal(map[string]any{"status": "before-state recorded", "taskId": task.ID, "paths": receipts, "boundary": "not a write, approval, current artifact validation, or engineering acceptance"})
	return string(b), err
}

func (scope deliveryWorkspace) preparedArtifactIsCurrent(task *Task, p string, f *os.File, info os.FileInfo, remaining *int64) bool {
	before, ok := task.DeliveryBaseline.Prepared[p]
	if !ok {
		return false
	}
	if !before.Existed {
		return true
	}
	if before.SHA256 == "" {
		return false
	}
	if info.Size() != before.Size {
		return true
	}
	digest := deliveryDigest(f, info, remaining)
	return digest != "" && digest != before.SHA256
}
