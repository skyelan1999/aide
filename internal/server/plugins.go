package server

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

//go:embed plugin_host.js
var pluginHostJS string

const (
	pluginsDirName   = "plugins"
	pluginSurfaceFN  = "surface.json"
	maxPlugins       = 50
	maxPluginCode    = 256 << 10
	pluginRunTimeout = 10 * time.Second
)

var pluginIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

type PluginManifest struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Version     string `json:"version,omitempty"`
	Author      string `json:"author,omitempty"`
	Main        string `json:"main"`
	Enabled     bool   `json:"enabled"`
	InstalledAt string `json:"installedAt"`
}

type pluginRegistry struct {
	Version int              `json:"version"`
	Plugins []PluginManifest `json:"plugins"`
}

// loadPlugins 在 New() 中调用：读取工程目录 plugins/registry.json；缺失时使用空注册表。
func (a *App) loadPlugins() error {
	a.pluginsPath = filepath.Join(a.workPath, pluginsDirName)
	a.pluginRegistry = pluginRegistry{Version: 1, Plugins: []PluginManifest{}}
	b, err := os.ReadFile(filepath.Join(a.pluginsPath, "registry.json"))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := json.Unmarshal(b, &a.pluginRegistry); err != nil {
		return fmt.Errorf("解析 plugins/registry.json: %w", err)
	}
	return nil
}

func (a *App) savePluginRegistry() error {
	if err := os.MkdirAll(a.pluginsPath, 0755); err != nil {
		return err
	}
	return atomicJSON(filepath.Join(a.pluginsPath, "registry.json"), a.pluginRegistry)
}

// runPluginHost 以协议 v1 宿主加载全部启用插件，聚合 surface（LIM-26：10s 超时）。
func (a *App) runPluginHost(ctx context.Context) {
	enabled := []map[string]string{}
	for _, p := range a.pluginRegistry.Plugins {
		if p.Enabled {
			enabled = append(enabled, map[string]string{"id": p.ID, "name": p.Name})
		}
	}
	if len(enabled) == 0 {
		empty, _ := json.Marshal(map[string]any{"generatedAt": time.Now().UTC().Format(time.RFC3339Nano), "plugins": []any{}})
		a.pluginSurface = empty
		return
	}
	listJSON, _ := json.Marshal(enabled)
	surfacePath := filepath.Join(a.pluginsPath, pluginSurfaceFN)
	cctx, cancel := context.WithTimeout(ctx, pluginRunTimeout)
	defer cancel()
	cmd := exec.CommandContext(cctx, "node", "-e", pluginHostJS, "run", a.pluginsPath, string(listJSON), surfacePath)
	cmd.Env = []string{"PATH=/usr/local/bin:/usr/bin:/bin", "HOME=/home/aide"}
	_ = cmd.Run() // 失败时保留旧 surface；stdout 结果不依赖（结果写入 surface 文件）
	if b, err := os.ReadFile(surfacePath); err == nil && len(b) <= 2<<20 {
		a.pluginSurface = b
	}
}

func (a *App) validatePluginCode(code string) (string, error) {
	if len(code) > maxPluginCode {
		return "", errors.New("插件代码超过 256 KiB 限制")
	}
	dir, err := os.MkdirTemp("", "aide-validate-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(dir)
	file := filepath.Join(dir, "index.js")
	if err := os.WriteFile(file, []byte(code), 0644); err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(context.Background(), pluginRunTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, "node", "-e", pluginHostJS, "validate", file).Output()
	if err != nil && ctx.Err() != nil {
		return "", errors.New("插件校验超时")
	}
	if err != nil {
		return "", fmt.Errorf("插件校验失败: %s", strings.TrimSpace(string(out)))
	}
	var result struct {
		OK    bool   `json:"ok"`
		Name  string `json:"name"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal(out, &result); err != nil {
		return "", errors.New("插件校验结果无法解析")
	}
	if !result.OK {
		return "", errors.New(result.Error)
	}
	return result.Name, nil
}

func (a *App) listPlugins(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	defer a.mu.Unlock()
	items := make([]map[string]any, 0, len(a.pluginRegistry.Plugins))
	for _, p := range a.pluginRegistry.Plugins {
		items = append(items, map[string]any{
			"id": p.ID, "name": p.Name, "description": p.Description, "version": p.Version,
			"author": p.Author, "enabled": p.Enabled, "installedAt": p.InstalledAt,
			"error": a.pluginError(p.ID),
		})
	}
	jsonOut(w, 200, map[string]any{"plugins": items})
}

func (a *App) pluginError(id string) string {
	var surface struct {
		Plugins []struct {
			ID    string `json:"id"`
			Error string `json:"error"`
		} `json:"plugins"`
	}
	if err := json.Unmarshal(a.pluginSurface, &surface); err != nil {
		return ""
	}
	for _, p := range surface.Plugins {
		if p.ID == id {
			return p.Error
		}
	}
	return ""
}

func (a *App) uploadPlugin(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ID          string `json:"id"`
		Name        string `json:"name"`
		Description string `json:"description"`
		Version     string `json:"version"`
		Author      string `json:"author"`
		Code        string `json:"code"`
	}
	if err := decode(w, r, &in); err != nil {
		fail(w, 400, err)
		return
	}
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" {
		fail(w, 400, errors.New("请填写插件名称"))
		return
	}
	if len([]rune(in.Name)) > 32 {
		fail(w, 400, errors.New("插件名称最长 32 个字符"))
		return
	}
	if in.ID == "" {
		in.ID = "p-" + newID()[:8]
	}
	if !pluginIDPattern.MatchString(in.ID) {
		fail(w, 400, errors.New("插件 id 只能含字母、数字、-、_，长度 1–64"))
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.pluginRegistry.Plugins) >= maxPlugins {
		fail(w, 400, fmt.Errorf("插件最多 %d 个", maxPlugins))
		return
	}
	for _, p := range a.pluginRegistry.Plugins {
		if p.ID == in.ID {
			fail(w, 409, errors.New("插件 id 已存在"))
			return
		}
	}
	if a.pluginsPath == "" {
		a.pluginsPath = filepath.Join(a.workPath, pluginsDirName)
	}
	dir := filepath.Join(a.pluginsPath, in.ID)
	if err := os.MkdirAll(dir, 0755); err != nil {
		fail(w, 500, err)
		return
	}
	pluginName, err := a.validatePluginCode(in.Code)
	if err != nil {
		_ = os.RemoveAll(dir)
		fail(w, 400, err)
		return
	}
	if in.Name == "" {
		in.Name = pluginName
	}
	manifest := PluginManifest{ID: in.ID, Name: in.Name, Description: in.Description, Version: in.Version, Author: in.Author, Main: "index.js", Enabled: true, InstalledAt: time.Now().UTC().Format(time.RFC3339Nano)}
	if err := os.WriteFile(filepath.Join(dir, "index.js"), []byte(in.Code), 0644); err != nil {
		_ = os.RemoveAll(dir)
		fail(w, 500, err)
		return
	}
	if err := atomicJSON(filepath.Join(dir, "manifest.json"), manifest); err != nil {
		_ = os.RemoveAll(dir)
		fail(w, 500, err)
		return
	}
	a.pluginRegistry.Plugins = append(a.pluginRegistry.Plugins, manifest)
	if err := a.savePluginRegistry(); err != nil {
		fail(w, 500, err)
		return
	}
	a.runPluginHost(context.Background())
	jsonOut(w, 201, map[string]any{"id": in.ID, "name": in.Name, "enabled": true, "error": a.pluginError(in.ID)})
}

func (a *App) togglePlugin(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Enabled bool `json:"enabled"`
	}
	if err := decode(w, r, &in); err != nil {
		fail(w, 400, err)
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	for i := range a.pluginRegistry.Plugins {
		if a.pluginRegistry.Plugins[i].ID == r.PathValue("id") {
			a.pluginRegistry.Plugins[i].Enabled = in.Enabled
			if err := a.savePluginRegistry(); err != nil {
				fail(w, 500, err)
				return
			}
			a.runPluginHost(context.Background())
			jsonOut(w, 200, map[string]any{"id": r.PathValue("id"), "enabled": in.Enabled, "error": a.pluginError(r.PathValue("id"))})
			return
		}
	}
	fail(w, 404, errors.New("插件不存在"))
}

func (a *App) deletePlugin(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	defer a.mu.Unlock()
	id := r.PathValue("id")
	for i := range a.pluginRegistry.Plugins {
		if a.pluginRegistry.Plugins[i].ID == id {
			a.pluginRegistry.Plugins = append(a.pluginRegistry.Plugins[:i], a.pluginRegistry.Plugins[i+1:]...)
			_ = os.RemoveAll(filepath.Join(a.pluginsPath, id))
			if err := a.savePluginRegistry(); err != nil {
				fail(w, 500, err)
				return
			}
			a.runPluginHost(context.Background())
			jsonOut(w, 200, map[string]bool{"ok": true})
			return
		}
	}
	fail(w, 404, errors.New("插件不存在"))
}

func (a *App) pluginSurfaceHandler(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.pluginSurface) == 0 {
		jsonOut(w, 200, map[string]any{"generatedAt": "", "plugins": []any{}})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(200)
	_, _ = w.Write(a.pluginSurface)
}
