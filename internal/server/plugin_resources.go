package server

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
)

// This is a scheduling contract for trusted plugin code, not a filesystem
// sandbox. Missing or unusable contracts conflict with every plugin call.
type pluginResource struct {
	path  string
	write bool
}
type pluginFootprint struct {
	exclusive bool
	resources []pluginResource
	readOnly  bool
}
type pluginResourceWaiter struct {
	footprint pluginFootprint
	ready     chan struct{}
	granted   bool
}
type pluginResourceQueue struct {
	mu    sync.Mutex
	items []*pluginResourceWaiter
}

func pluginResourceContains(parent, child string) bool {
	return parent == child || strings.HasPrefix(child, strings.TrimRight(parent, string(filepath.Separator))+string(filepath.Separator))
}

func pluginResourceConflict(a, b pluginFootprint) bool {
	if a.exclusive || b.exclusive {
		return true
	}
	for _, x := range a.resources {
		for _, y := range b.resources {
			if (x.write || y.write) && (pluginResourceContains(x.path, y.path) || pluginResourceContains(y.path, x.path)) {
				return true
			}
		}
	}
	return false
}

// All claims are granted together. A later reader cannot jump an earlier
// waiting writer, while calls touching unrelated resources can make progress.
func (q *pluginResourceQueue) pumpLocked() {
	for i, item := range q.items {
		if item.granted {
			continue
		}
		blocked := false
		for j, other := range q.items {
			if other != item && (other.granted || j < i) && pluginResourceConflict(item.footprint, other.footprint) {
				blocked = true
				break
			}
		}
		if !blocked {
			item.granted = true
			close(item.ready)
		}
	}
}

func (q *pluginResourceQueue) acquire(ctx context.Context, footprint pluginFootprint) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	item := &pluginResourceWaiter{footprint: footprint, ready: make(chan struct{})}
	q.mu.Lock()
	q.items = append(q.items, item)
	q.pumpLocked()
	q.mu.Unlock()
	var once sync.Once
	release := func() {
		once.Do(func() {
			q.mu.Lock()
			defer q.mu.Unlock()
			for i, other := range q.items {
				if item == other {
					q.items = append(q.items[:i], q.items[i+1:]...)
					break
				}
			}
			q.pumpLocked()
		})
	}
	select {
	case <-ctx.Done():
		release()
		return nil, ctx.Err()
	case <-item.ready:
		if err := ctx.Err(); err != nil {
			release()
			return nil, err
		}
		return release, nil
	}
}

type pluginResourceDeclaration struct {
	Root     string `json:"root"`
	Path     string `json:"path,omitempty"`
	Argument string `json:"argument,omitempty"`
	Access   string `json:"access"`
}
type pluginSchedulingContract struct {
	Resources []pluginResourceDeclaration `json:"resources"`
}

// Symlink aliases/retargeting cannot be represented by lexical locks. Fall
// back to exclusive scheduling rather than pretending such claims are exact.
func pluginResourcePath(root, relative string) (string, bool) {
	if relative == "" || strings.ContainsAny(relative, "\\\x00") || filepath.IsAbs(relative) {
		return "", false
	}
	for _, part := range strings.Split(relative, "/") {
		if part == ".." {
			return "", false
		}
	}
	canonical, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", false
	}
	canonical, err = filepath.Abs(canonical)
	if err != nil {
		return "", false
	}
	current := canonical
	for _, part := range strings.Split(filepath.Clean(relative), string(filepath.Separator)) {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if err == nil && info.Mode()&os.ModeSymlink != 0 || err != nil && !os.IsNotExist(err) {
			return "", false
		}
		if err == nil && info.Mode().IsRegular() {
			if stat, ok := info.Sys().(*syscall.Stat_t); ok && stat.Nlink > 1 {
				return "", false
			}
		}
	}
	return current, true
}

// Called under a.mu, using the host's published definition. The model cannot
// supply its own read-only annotation in arguments to bypass a writer.
func (a *App) pluginFootprintLocked(pluginID, tool string, args map[string]any) pluginFootprint {
	exclusive := pluginFootprint{exclusive: true}
	// The helper is bound to the selected container-local project. Until
	// a unified remote driver exists, remote declarations must not imply locks
	// on SSH/SFTP paths that the helper does not actually use.
	if a.workspaceMode() == "ssh" || pluginID == "browser-control" || pluginID == "computer-control" {
		return exclusive
	}
	var surface struct {
		Plugins []struct {
			ID       string   `json:"id"`
			Error    string   `json:"error"`
			Requires []string `json:"requires"`
			Tools    []struct {
				Name       string                    `json:"name"`
				Executable bool                      `json:"executable"`
				Scheduling *pluginSchedulingContract `json:"scheduling"`
			} `json:"tools"`
		} `json:"plugins"`
	}
	if json.Unmarshal(a.pluginSurface, &surface) != nil {
		return exclusive
	}
	for _, plugin := range surface.Plugins {
		if plugin.ID != pluginID || plugin.Error != "" {
			continue
		}
		// A service consumer can execute another plugin's provider. Until the
		// dependency's complete footprint is represented, it stays exclusive.
		if len(plugin.Requires) != 0 {
			return exclusive
		}
		for _, definition := range plugin.Tools {
			if definition.Name != tool || !definition.Executable || definition.Scheduling == nil || definition.Scheduling.Resources == nil || len(definition.Scheduling.Resources) > 32 {
				continue
			}
			pluginRoot := filepath.Join(a.pluginsPath, pluginID)
			state, ok := pluginResourcePath(pluginRoot, ".")
			if !ok {
				return exclusive
			}
			footprint := pluginFootprint{readOnly: true, resources: []pluginResource{{path: state, write: true}}}
			for _, resource := range definition.Scheduling.Resources {
				root := a.pluginWorkspaceLocked()
				if resource.Root == "plugin" {
					root = pluginRoot
				} else if resource.Root != "workspace" {
					return exclusive
				}
				if resource.Access != "read" && resource.Access != "write" || (resource.Path == "") == (resource.Argument == "") {
					return exclusive
				}
				relative := resource.Path
				if resource.Argument != "" {
					var valid bool
					relative, valid = args[resource.Argument].(string)
					if !valid {
						return exclusive
					}
				}
				path, ok := pluginResourcePath(root, relative)
				if !ok {
					return exclusive
				}
				write := resource.Access == "write"
				footprint.resources = append(footprint.resources, pluginResource{path: path, write: write})
				if write {
					footprint.readOnly = false
				}
			}
			return footprint
		}
	}
	return exclusive
}

func pluginResourceResult(footprint pluginFootprint, workspace string, raw any, err error) (any, error) {
	if err != nil || footprint.exclusive {
		return raw, err
	}
	_, proposals := normalizePluginResult(raw)
	for _, proposal := range proposals {
		// Commands can have effects outside every declared file, so they need
		// an exclusive call and the existing command approval path.
		if proposal["type"] != "file" {
			return nil, errors.New("插件调度契约不一致：调用返回了未声明的命令提案，提案未入库")
		}
		relative, ok := proposal["path"].(string)
		if !ok || footprint.readOnly {
			return nil, errors.New("插件调度契约不一致：只读调用返回了写入提案，提案未入库")
		}
		path, valid := pluginResourcePath(workspace, relative)
		allowed := false
		for _, claim := range footprint.resources {
			if claim.write && valid && pluginResourceContains(claim.path, path) {
				allowed = true
			}
		}
		if !allowed {
			return nil, errors.New("插件调度契约不一致：写入提案超出声明资源，提案未入库")
		}
	}
	return raw, nil
}
