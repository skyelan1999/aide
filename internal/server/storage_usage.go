package server

import (
	"context"
	"errors"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type StorageUsage struct {
	ID           string         `json:"id"`
	LogicalBytes int64          `json:"logicalBytes"`
	Files        int64          `json:"files"`
	Directories  int64          `json:"directories"`
	Errors       int64          `json:"errors"`
	Children     []StorageUsage `json:"children,omitempty"`
}

type StorageDiskUsage struct {
	ID             string `json:"id"`
	LogicalBytes   int64  `json:"logicalBytes"`
	Files          int64  `json:"files"`
	Errors         int64  `json:"errors"`
	AvailableBytes int64  `json:"availableBytes"`
}

// scanStorageUsage measures logical file sizes and never follows symbolic
// links. Data children are grouped by their top-level directory; root-level
// files are aggregated so the API does not expose arbitrary file names.
func scanStorageUsage(ctx context.Context, root string, includeChildren bool, excludedTopLevel ...string) (StorageUsage, error) {
	result := StorageUsage{}
	groups := map[string]*StorageUsage{}
	excluded := map[string]bool{}
	for _, name := range excludedTopLevel {
		excluded[name] = true
	}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if walkErr != nil {
			if path == root {
				return walkErr
			}
			result.Errors++
			if entry != nil && entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if path == root {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			result.Errors++
			return nil
		}
		if excluded[rel] && entry.IsDir() {
			return filepath.SkipDir
		}
		group := "_root"
		if strings.ContainsRune(rel, filepath.Separator) {
			group = strings.Split(rel, string(filepath.Separator))[0]
		}
		if includeChildren && groups[group] == nil {
			groups[group] = &StorageUsage{ID: group}
		}
		if entry.IsDir() {
			result.Directories++
			if includeChildren {
				groups[group].Directories++
			}
			return nil
		}
		if !entry.Type().IsRegular() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			result.Errors++
			return nil
		}
		result.Files++
		result.LogicalBytes += info.Size()
		if includeChildren {
			groups[group].Files++
			groups[group].LogicalBytes += info.Size()
		}
		return nil
	})
	if err != nil {
		return result, err
	}
	if includeChildren {
		for _, group := range groups {
			result.Children = append(result.Children, *group)
		}
		sort.Slice(result.Children, func(i, j int) bool {
			if result.Children[i].LogicalBytes != result.Children[j].LogicalBytes {
				return result.Children[i].LogicalBytes > result.Children[j].LogicalBytes
			}
			return result.Children[i].ID < result.Children[j].ID
		})
	}
	return result, nil
}

func storageAvailableBytes(path string) int64 {
	root, err := os.OpenRoot(path)
	if err != nil {
		return -1
	}
	defer root.Close()
	available, err := transferAvailableBytes(root)
	if err != nil {
		return -1
	}
	return available
}

func (a *App) storageUsageHandler(w http.ResponseWriter, r *http.Request) {
	data, err := scanStorageUsage(r.Context(), a.dataPath, true)
	if err != nil {
		if !errors.Is(err, r.Context().Err()) {
			log.Printf("Aide 数据存储扫描失败: %v", err)
		}
		fail(w, 500, errors.New("Aide 数据存储扫描失败"))
		return
	}
	data.ID = "aide-data"
	dataAvailable := storageAvailableBytes(a.dataPath)
	disks := []StorageDiskUsage{{
		ID: data.ID, LogicalBytes: data.LogicalBytes, Files: data.Files, Errors: data.Errors, AvailableBytes: dataAvailable,
	}}

	a.mu.Lock()
	workspaceMode := a.wsConfig.Workspace.Mode
	a.mu.Unlock()
	workspaceExcluded := strings.EqualFold(workspaceMode, "ssh")
	workspace := StorageUsage{ID: "current-workspace"}
	workspaceAvailable := int64(-1)
	workspaceAudit := map[string]any{"included": true, "reason": ""}
	if workspaceExcluded {
		workspaceAudit = map[string]any{"included": false, "reason": "ssh-source"}
	} else {
		workspace, err = scanStorageUsage(r.Context(), a.workPath, false, ".aide-data")
		if err != nil {
			if !errors.Is(err, r.Context().Err()) {
				log.Printf("当前工作区存储扫描失败: %v", err)
			}
			fail(w, 500, errors.New("当前工作区存储扫描失败"))
			return
		}
		workspace.ID = "current-workspace"
		workspaceAvailable = storageAvailableBytes(a.workPath)
		disks = append(disks, StorageDiskUsage{
			ID: workspace.ID, LogicalBytes: workspace.LogicalBytes, Files: workspace.Files, Errors: workspace.Errors, AvailableBytes: workspaceAvailable,
		})
	}
	jsonOut(w, 200, map[string]any{
		"version": 2, "generatedAt": time.Now().UTC(), "measurement": "logical-file-bytes",
		"data": data, "workspace": workspace, "workspaceExclusions": []string{".aide-data"}, "workspaceAudit": workspaceAudit,
		"disks": disks,
		"capacity": map[string]any{
			"dataAvailableBytes": dataAvailable, "workspaceAvailableBytes": workspaceAvailable,
		},
	})
}
