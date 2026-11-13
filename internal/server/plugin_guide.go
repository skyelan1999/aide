package server

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// This is installed documentation, never arbitrary plugin assets or settings.
// Keep the workspace lock through the bounded read so a project switch cannot
// return documentation from a different installed registry.
func (a *App) pluginGuide(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.loadLockStateLocked().Locked {
		fail(w, 423, errors.New("请先解锁工作台"))
		return
	}
	id := r.PathValue("id")
	if !pluginIDPattern.MatchString(id) {
		fail(w, 400, errors.New("插件标识无效"))
		return
	}
	var plugin *PluginManifest
	for i := range a.pluginRegistry.Plugins {
		if a.pluginRegistry.Plugins[i].ID == id {
			plugin = &a.pluginRegistry.Plugins[i]
			break
		}
	}
	if plugin == nil {
		fail(w, 404, errors.New("插件未安装"))
		return
	}
	tools := []map[string]any{}
	var surface struct {
		Plugins []struct {
			ID    string           `json:"id"`
			Tools []map[string]any `json:"tools"`
		} `json:"plugins"`
	}
	_ = json.Unmarshal(a.pluginSurface, &surface)
	for _, p := range surface.Plugins {
		if p.ID != id {
			continue
		}
		for _, tool := range p.Tools {
			tools = append(tools, map[string]any{"name": tool["name"], "description": tool["description"], "executable": tool["executable"] == true})
		}
	}
	readme, truncated := "", false
	if root, err := os.OpenRoot(filepath.Join(a.pluginsPath, id)); err == nil {
		defer root.Close()
		for _, name := range []string{"README.md", "README.zh-CN.md", "readme.md", "README.en.md"} {
			f, err := root.Open(name)
			if err != nil {
				continue
			}
			info, statErr := f.Stat()
			if statErr != nil || !info.Mode().IsRegular() {
				f.Close()
				continue
			}
			data, readErr := io.ReadAll(io.LimitReader(f, (128<<10)+1))
			f.Close()
			if readErr != nil {
				continue
			}
			if len(data) > 128<<10 {
				data = data[:128<<10]
				truncated = true
			}
			readme = strings.ToValidUTF8(string(data), "�")
			break
		}
	}
	jsonOut(w, 200, map[string]any{"id": id, "name": plugin.Name, "description": plugin.Description, "version": plugin.Version, "enabled": plugin.Enabled, "error": a.pluginError(id), "workbench": plugin.Workbench, "providerSupport": plugin.ProviderSupport != nil, "modelRouter": plugin.ModelRouter != nil, "tools": tools, "readme": readme, "readmeTruncated": truncated})
}
