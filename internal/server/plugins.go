package server

import (
	"archive/zip"
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	pathpkg "path"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

//go:embed plugin_host.js
var pluginHostJS string

const (
	pluginsDirName    = "plugins"
	pluginSurfaceFN   = "surface.json"
	maxPlugins        = 50
	maxPluginCode     = 256 << 10
	maxPluginBundle   = 24 << 20
	maxPluginExpanded = 64 << 20
	maxPluginFiles    = 256
	pluginRunTimeout  = 10 * time.Second
)

var pluginIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

type PluginManifest struct {
	ID          string         `json:"id"`
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Version     string         `json:"version,omitempty"`
	Author      string         `json:"author,omitempty"`
	Main        string         `json:"main"`
	Enabled     bool           `json:"enabled"`
	Daemon      bool           `json:"daemon,omitempty"` // 协议 v1.2：常驻守护插件
	Settings    map[string]any `json:"settings,omitempty"`
	InstalledAt string         `json:"installedAt"`
}

type pluginRegistry struct {
	Version         int              `json:"version"`
	Plugins         []PluginManifest `json:"plugins"`
	RemovedBuiltins []string         `json:"removedBuiltins,omitempty"`
}

// loadPlugins reads workspace state, then adds newly shipped control plugins.
// Existing plugin code, enablement and settings belong to the workspace.
func (a *App) loadPlugins() error {
	a.pluginsPath = filepath.Join(a.workPath, pluginsDirName)
	a.pluginRegistry = pluginRegistry{Version: 1, Plugins: []PluginManifest{}}
	b, err := os.ReadFile(filepath.Join(a.pluginsPath, "registry.json"))
	if errors.Is(err, os.ErrNotExist) {
		return a.installBundledControlPlugins(os.Getenv("AIDE_BUILTIN_PLUGINS"))
	}
	if err != nil {
		return err
	}
	if err := json.Unmarshal(b, &a.pluginRegistry); err != nil {
		return fmt.Errorf("解析 plugins/registry.json: %w", err)
	}
	return a.installBundledControlPlugins(os.Getenv("AIDE_BUILTIN_PLUGINS"))
}

func (a *App) installBundledControlPlugins(bundlePath string) error {
	if bundlePath == "" {
		return nil
	}
	for _, id := range []string{"browser-control", "computer-control"} {
		found := false
		for _, removed := range a.pluginRegistry.RemovedBuiltins {
			if removed == id {
				found = true
			}
		}
		for _, existing := range a.pluginRegistry.Plugins {
			if existing.ID == id {
				found = true
				break
			}
		}
		if found {
			continue
		}
		source := filepath.Join(bundlePath, id)
		raw, err := os.ReadFile(filepath.Join(source, "manifest.json"))
		if err != nil {
			return fmt.Errorf("read bundled plugin %s: %w", id, err)
		}
		var manifest PluginManifest
		if err := json.Unmarshal(raw, &manifest); err != nil || manifest.ID != id || manifest.Main != "index.js" {
			return fmt.Errorf("invalid bundled control plugin manifest: %s", id)
		}
		code, err := os.ReadFile(filepath.Join(source, "index.js"))
		if err != nil {
			return err
		}
		if err := os.MkdirAll(a.pluginsPath, 0755); err != nil {
			return err
		}
		destination := filepath.Join(a.pluginsPath, id)
		// Source upgrades may already contain the files but retain an older
		// registry. Register a valid local copy without replacing its contents.
		if info, err := os.Lstat(destination); err == nil {
			if !info.IsDir() {
				return fmt.Errorf("bundled plugin path is not a directory: %s", id)
			}
			local, readErr := os.ReadFile(filepath.Join(destination, "manifest.json"))
			if readErr != nil || json.Unmarshal(local, &manifest) != nil || manifest.ID != id || manifest.Main != "index.js" {
				return fmt.Errorf("cannot register existing control plugin directory: %s", id)
			}
			entry, statErr := os.Lstat(filepath.Join(destination, "index.js"))
			if statErr != nil || !entry.Mode().IsRegular() {
				return fmt.Errorf("control plugin entry is not a regular file: %s", id)
			}
			manifest.Enabled = false
			a.pluginRegistry.Plugins = append(a.pluginRegistry.Plugins, manifest)
			if err := a.savePluginRegistry(); err != nil {
				a.pluginRegistry.Plugins = a.pluginRegistry.Plugins[:len(a.pluginRegistry.Plugins)-1]
				return err
			}
			continue
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		stage, err := os.MkdirTemp(a.pluginsPath, ".builtin-")
		if err != nil {
			return err
		}
		manifest.Enabled = false
		manifest.InstalledAt = time.Now().UTC().Format(time.RFC3339Nano)
		err = os.WriteFile(filepath.Join(stage, "index.js"), code, 0644)
		if err == nil {
			err = atomicJSON(filepath.Join(stage, "manifest.json"), manifest)
		}
		if err == nil {
			err = os.Rename(stage, destination)
		}
		_ = os.RemoveAll(stage)
		if err != nil {
			return err
		}
		a.pluginRegistry.Plugins = append(a.pluginRegistry.Plugins, manifest)
		// Persist each successful install so a later failure can be retried.
		if err := a.savePluginRegistry(); err != nil {
			a.pluginRegistry.Plugins = a.pluginRegistry.Plugins[:len(a.pluginRegistry.Plugins)-1]
			_ = os.RemoveAll(destination)
			return err
		}
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
// 慢进程（exec node，最长 10s）在 a.mu 之外执行：仅在 a.mu 内快照启用插件列表/路径，
// node 结束后再用短锁把 surface 写回。调用方不得持有 a.mu（本函数会自行取放）。
func (a *App) runPluginHost(ctx context.Context) {
	a.mu.Lock()
	enabled := []map[string]any{}
	for _, p := range a.pluginRegistry.Plugins {
		if p.Enabled {
			enabled = append(enabled, map[string]any{"id": p.ID, "name": p.Name, "main": p.Main, "settings": p.Settings})
		}
	}
	pluginsPath := a.pluginsPath
	a.mu.Unlock()

	if len(enabled) == 0 {
		empty, _ := json.Marshal(map[string]any{"generatedAt": time.Now().UTC().Format(time.RFC3339Nano), "plugins": []any{}})
		a.mu.Lock()
		a.pluginSurface = empty
		a.mu.Unlock()
		return
	}
	listJSON, _ := json.Marshal(enabled)
	surfacePath := filepath.Join(pluginsPath, pluginSurfaceFN)
	cctx, cancel := context.WithTimeout(ctx, pluginRunTimeout)
	defer cancel()
	cmd := exec.CommandContext(cctx, "node", "-e", pluginHostJS, "run", pluginsPath, string(listJSON), surfacePath)
	cmd.Env = []string{"PATH=/usr/local/bin:/usr/bin:/bin", "HOME=/home/aide"}
	_ = cmd.Run() // 失败时保留旧 surface；stdout 结果不依赖（结果写入 surface 文件）
	if b, err := os.ReadFile(surfacePath); err == nil && len(b) <= 2<<20 {
		a.mu.Lock()
		a.pluginSurface = b
		a.mu.Unlock()
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
			"error": a.pluginError(p.ID), "settings": pluginSettingsOrEmpty(p.Settings),
		})
	}
	jsonOut(w, 200, map[string]any{"plugins": items})
}

func pluginSettingsOrEmpty(settings map[string]any) map[string]any {
	if settings == nil {
		return map[string]any{}
	}
	return settings
}

// updatePluginSettings saves non-secret, workspace-local settings for one plugin.
func (a *App) updatePluginSettings(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Settings map[string]any `json:"settings"`
	}
	if err := decode(w, r, &in); err != nil {
		fail(w, 400, err)
		return
	}
	if in.Settings == nil {
		fail(w, 400, errors.New("插件设置必须是 JSON 对象"))
		return
	}
	encoded, err := json.Marshal(in.Settings)
	if err != nil || len(encoded) > 32<<10 {
		fail(w, 400, errors.New("插件设置不能超过 32 KiB"))
		return
	}
	id := r.PathValue("id")
	a.mu.Lock()
	index := -1
	for i := range a.pluginRegistry.Plugins {
		if a.pluginRegistry.Plugins[i].ID == id {
			index = i
			break
		}
	}
	if index < 0 {
		a.mu.Unlock()
		fail(w, 404, errors.New("插件不存在"))
		return
	}
	p := &a.pluginRegistry.Plugins[index]
	p.Settings = in.Settings
	isDaemon, mainPath := a.daemonManifestSnapshotLocked(id)
	enabled := p.Enabled
	settings := p.Settings
	if err := a.savePluginRegistry(); err != nil {
		a.mu.Unlock()
		fail(w, 500, err)
		return
	}
	a.mu.Unlock()

	var restartError string
	if isDaemon && enabled && a.daemons != nil {
		if err := a.daemons.RestartWithSettings(id, mainPath, settings); err != nil {
			restartError = err.Error()
		}
	}
	a.runPluginHost(context.Background())
	result := map[string]any{"id": id, "settings": settings}
	if restartError != "" {
		result["restartError"] = restartError
	}
	jsonOut(w, 200, result)
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
	if len(a.pluginRegistry.Plugins) >= maxPlugins {
		a.mu.Unlock()
		fail(w, 400, fmt.Errorf("插件最多 %d 个", maxPlugins))
		return
	}
	for _, p := range a.pluginRegistry.Plugins {
		if p.ID == in.ID {
			a.mu.Unlock()
			fail(w, 409, errors.New("插件 id 已存在"))
			return
		}
	}
	if a.pluginsPath == "" {
		a.pluginsPath = filepath.Join(a.workPath, pluginsDirName)
	}
	dir := filepath.Join(a.pluginsPath, in.ID)
	if err := os.MkdirAll(dir, 0755); err != nil {
		a.mu.Unlock()
		fail(w, 500, err)
		return
	}
	pluginName, err := a.validatePluginCode(in.Code)
	if err != nil {
		a.mu.Unlock()
		_ = os.RemoveAll(dir)
		fail(w, 400, err)
		return
	}
	if in.Name == "" {
		in.Name = pluginName
	}
	manifest := PluginManifest{ID: in.ID, Name: in.Name, Description: in.Description, Version: in.Version, Author: in.Author, Main: "index.js", Enabled: true, InstalledAt: time.Now().UTC().Format(time.RFC3339Nano)}
	if err := os.WriteFile(filepath.Join(dir, "index.js"), []byte(in.Code), 0644); err != nil {
		a.mu.Unlock()
		_ = os.RemoveAll(dir)
		fail(w, 500, err)
		return
	}
	if err := atomicJSON(filepath.Join(dir, "manifest.json"), manifest); err != nil {
		a.mu.Unlock()
		_ = os.RemoveAll(dir)
		fail(w, 500, err)
		return
	}
	a.pluginRegistry.Plugins = append(a.pluginRegistry.Plugins, manifest)
	if err := a.savePluginRegistry(); err != nil {
		a.mu.Unlock()
		fail(w, 500, err)
		return
	}
	// 慢进程（runPluginHost 聚合 surface，node 最长 10s）放 a.mu 之外执行。
	a.mu.Unlock()
	a.runPluginHost(context.Background())
	a.mu.Lock()
	errText := a.pluginError(in.ID)
	a.mu.Unlock()
	jsonOut(w, 201, map[string]any{"id": in.ID, "name": in.Name, "enabled": true, "error": errText})
}

// uploadPluginBundle installs a self-contained ZIP package with a manifest, JS
// entry point, and optional resources such as Python scripts. It never runs pip.
func (a *App) uploadPluginBundle(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxPluginBundle)
	if err := r.ParseMultipartForm(2 << 20); err != nil {
		fail(w, 400, fmt.Errorf("插件包上传失败或超过 24 MiB: %w", err))
		return
	}
	if r.MultipartForm != nil {
		defer r.MultipartForm.RemoveAll()
	}
	f, _, err := r.FormFile("bundle")
	if err != nil {
		fail(w, 400, errors.New("请选择插件 ZIP 包"))
		return
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, maxPluginBundle+1))
	if err != nil || len(raw) > maxPluginBundle {
		fail(w, 400, errors.New("插件 ZIP 包超过 24 MiB"))
		return
	}
	zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		fail(w, 400, fmt.Errorf("插件 ZIP 包无法读取: %w", err))
		return
	}
	if len(zr.File) == 0 || len(zr.File) > maxPluginFiles {
		fail(w, 400, fmt.Errorf("插件包文件数必须为 1–%d", maxPluginFiles))
		return
	}
	files := make(map[string][]byte, len(zr.File))
	manifestPath := ""
	var expanded int64
	for _, entry := range zr.File {
		name := strings.ReplaceAll(entry.Name, "\\", "/")
		if entry.FileInfo().IsDir() {
			continue
		}
		if entry.Mode()&os.ModeSymlink != 0 || name == "" || strings.HasPrefix(name, "/") || pathpkg.Clean(name) != name || strings.Contains(name, ":") {
			fail(w, 400, errors.New("插件包包含不安全路径"))
			return
		}
		for _, part := range strings.Split(name, "/") {
			if part == "" || part == "." || part == ".." || strings.HasPrefix(part, ".") {
				fail(w, 400, errors.New("插件包包含不允许的路径"))
				return
			}
		}
		if strings.HasPrefix(name, "__MACOSX/") || pathpkg.Base(name) == ".DS_Store" {
			continue
		}
		if _, duplicate := files[name]; duplicate {
			fail(w, 400, fmt.Errorf("插件包存在重复文件: %s", name))
			return
		}
		if entry.UncompressedSize64 > 16<<20 || expanded+int64(entry.UncompressedSize64) > maxPluginExpanded {
			fail(w, 400, errors.New("插件包解压后超过限制（单文件 16 MiB，合计 64 MiB）"))
			return
		}
		reader, openErr := entry.Open()
		if openErr != nil {
			fail(w, 400, openErr)
			return
		}
		contents, readErr := io.ReadAll(io.LimitReader(reader, int64(entry.UncompressedSize64)+1))
		closeErr := reader.Close()
		if readErr != nil || closeErr != nil || uint64(len(contents)) != entry.UncompressedSize64 {
			fail(w, 400, fmt.Errorf("插件包文件读取失败: %s", name))
			return
		}
		expanded += int64(len(contents))
		files[name] = contents
		if pathpkg.Base(name) == "manifest.json" {
			if manifestPath != "" {
				fail(w, 400, errors.New("插件包必须只有一个 manifest.json"))
				return
			}
			manifestPath = name
		}
	}
	if manifestPath == "" {
		fail(w, 400, errors.New("插件包缺少 manifest.json"))
		return
	}
	root := pathpkg.Dir(manifestPath)
	packageFiles := make(map[string][]byte, len(files))
	for name, contents := range files {
		rel := name
		if root != "." {
			if !strings.HasPrefix(name, root+"/") {
				fail(w, 400, fmt.Errorf("插件包内容必须位于同一目录: %s", name))
				return
			}
			rel = strings.TrimPrefix(name, root+"/")
		}
		packageFiles[rel] = contents
	}
	var manifest PluginManifest
	manifestBytes := packageFiles["manifest.json"]
	if len(manifestBytes) == 0 || json.Unmarshal(manifestBytes, &manifest) != nil {
		fail(w, 400, errors.New("插件 manifest.json 无效"))
		return
	}
	if !pluginIDPattern.MatchString(manifest.ID) || strings.TrimSpace(manifest.Name) == "" || len([]rune(manifest.Name)) > 32 {
		fail(w, 400, errors.New("manifest 必须包含合法 id 和 1–32 字的 name"))
		return
	}
	main := strings.ReplaceAll(manifest.Main, "\\", "/")
	if main == "" || strings.HasPrefix(main, "/") || pathpkg.Clean(main) != main || !strings.HasSuffix(main, ".js") || strings.Contains(main, ":") {
		fail(w, 400, errors.New("manifest.main 必须是插件包内的相对 .js 路径"))
		return
	}
	for _, part := range strings.Split(main, "/") {
		if part == "" || part == "." || part == ".." || strings.HasPrefix(part, ".") {
			fail(w, 400, errors.New("manifest.main 路径无效"))
			return
		}
	}
	code := packageFiles[main]
	if len(code) == 0 || len(code) > maxPluginCode {
		fail(w, 400, errors.New("插件入口缺失或超过 256 KiB"))
		return
	}
	if _, err := a.validatePluginCode(string(code)); err != nil {
		fail(w, 400, err)
		return
	}
	if a.pluginsPath == "" {
		a.pluginsPath = filepath.Join(a.workPath, pluginsDirName)
	}
	if err := os.MkdirAll(a.pluginsPath, 0755); err != nil {
		fail(w, 500, err)
		return
	}
	stage, err := os.MkdirTemp(a.pluginsPath, ".install-")
	if err != nil {
		fail(w, 500, err)
		return
	}
	defer os.RemoveAll(stage)
	for rel, contents := range packageFiles {
		if rel == "manifest.json" {
			continue
		}
		dest := filepath.Join(stage, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
			fail(w, 500, err)
			return
		}
		mode := os.FileMode(0644)
		if strings.HasSuffix(rel, ".py") {
			mode = 0644
		}
		if err := os.WriteFile(dest, contents, mode); err != nil {
			fail(w, 500, err)
			return
		}
	}
	manifest.Main = main
	manifest.Enabled = true
	manifest.InstalledAt = time.Now().UTC().Format(time.RFC3339Nano)
	if err := atomicJSON(filepath.Join(stage, "manifest.json"), manifest); err != nil {
		fail(w, 500, err)
		return
	}
	a.mu.Lock()
	if len(a.pluginRegistry.Plugins) >= maxPlugins {
		a.mu.Unlock()
		fail(w, 400, fmt.Errorf("插件最多 %d 个", maxPlugins))
		return
	}
	for _, p := range a.pluginRegistry.Plugins {
		if p.ID == manifest.ID {
			a.mu.Unlock()
			fail(w, 409, errors.New("插件 id 已存在"))
			return
		}
	}
	destination := filepath.Join(a.pluginsPath, manifest.ID)
	if err := os.Rename(stage, destination); err != nil {
		a.mu.Unlock()
		fail(w, 500, err)
		return
	}
	a.pluginRegistry.Plugins = append(a.pluginRegistry.Plugins, manifest)
	if err := a.savePluginRegistry(); err != nil {
		a.pluginRegistry.Plugins = a.pluginRegistry.Plugins[:len(a.pluginRegistry.Plugins)-1]
		_ = os.RemoveAll(destination)
		a.mu.Unlock()
		fail(w, 500, err)
		return
	}
	a.mu.Unlock()
	a.runPluginHost(context.Background())
	jsonOut(w, 201, map[string]any{"id": manifest.ID, "name": manifest.Name, "enabled": true})
}

func (a *App) togglePlugin(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Enabled bool `json:"enabled"`
	}
	if err := decode(w, r, &in); err != nil {
		fail(w, 400, err)
		return
	}
	id := r.PathValue("id")
	// a.mu 内只做注册表快速变更与持久化；daemon 启停与 surface 聚合（慢进程）放锁外。
	a.mu.Lock()
	found := false
	var wantStart bool
	var wantStop bool
	var mainPath string
	for i := range a.pluginRegistry.Plugins {
		if a.pluginRegistry.Plugins[i].ID == id {
			a.pluginRegistry.Plugins[i].Enabled = in.Enabled
			found = true
			if a.daemons != nil {
				if isD, mp := a.daemonManifestSnapshotLocked(id); isD {
					mainPath = mp
					if in.Enabled {
						wantStart = true
					} else {
						wantStop = true
					}
				}
			}
			break
		}
	}
	if !found {
		a.mu.Unlock()
		fail(w, 404, errors.New("插件不存在"))
		return
	}
	if err := a.savePluginRegistry(); err != nil {
		a.mu.Unlock()
		fail(w, 500, err)
		return
	}
	a.mu.Unlock()

	if wantStop {
		_ = a.daemons.Stop(id)
	}
	if wantStart {
		_ = a.daemons.StartWithPath(id, mainPath)
	}
	a.runPluginHost(context.Background())
	a.mu.Lock()
	errText := a.pluginError(id)
	a.mu.Unlock()
	jsonOut(w, 200, map[string]any{"id": id, "enabled": in.Enabled, "error": errText})
}

func (a *App) deletePlugin(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	// a.mu 内只做注册表快速删除与持久化；daemon 停止与 surface 聚合（慢进程）放锁外。
	a.mu.Lock()
	found := false
	needStop := false
	for i := range a.pluginRegistry.Plugins {
		if a.pluginRegistry.Plugins[i].ID == id {
			a.pluginRegistry.Plugins = append(a.pluginRegistry.Plugins[:i], a.pluginRegistry.Plugins[i+1:]...)
			found = true
			if a.daemons != nil && a.isDaemonPlugin(id) {
				needStop = true
			}
			break
		}
	}
	if !found {
		a.mu.Unlock()
		fail(w, 404, errors.New("插件不存在"))
		return
	}
	pluginsPath := a.pluginsPath
	if id == "browser-control" || id == "computer-control" {
		a.pluginRegistry.RemovedBuiltins = append(a.pluginRegistry.RemovedBuiltins, id)
	}
	if err := a.savePluginRegistry(); err != nil {
		a.mu.Unlock()
		fail(w, 500, err)
		return
	}
	a.mu.Unlock()

	if needStop {
		_ = a.daemons.Stop(id) // 终止常驻进程并释放端口
	}
	_ = os.RemoveAll(filepath.Join(pluginsPath, id))
	a.runPluginHost(context.Background())
	jsonOut(w, 200, map[string]bool{"ok": true})
}

// callPluginTool 调用启用插件的可执行工具。
// 协议 v1.2：声明 daemon:true 的插件走常驻 DaemonManager（IPC），其余保持 v1.1 短命进程（向后兼容）。
func (a *App) callPluginTool(pluginID, toolName string, args map[string]any) (any, error) {
	return a.callPluginToolContext(context.Background(), pluginID, toolName, args)
}

func (a *App) callPluginToolContext(parent context.Context, pluginID, toolName string, args map[string]any) (any, error) {
	if err := parent.Err(); err != nil {
		return nil, err
	}
	if pluginID == "browser-control" || pluginID == "computer-control" {
		a.mu.Lock()
		enabled := false
		for _, p := range a.pluginRegistry.Plugins {
			if p.ID == pluginID {
				enabled = p.Enabled
				break
			}
		}
		a.mu.Unlock()
		if !enabled {
			return nil, errors.New("控制插件已停用，操作未执行")
		}
	}
	if a.daemons != nil && a.isDaemonPlugin(pluginID) {
		return a.daemons.Call(pluginID, toolName, args)
	}
	a.mu.Lock()
	settings := map[string]any{}
	for _, p := range a.pluginRegistry.Plugins {
		if p.ID == pluginID {
			settings = pluginSettingsOrEmpty(p.Settings)
			break
		}
	}
	a.mu.Unlock()
	req, err := json.Marshal(map[string]any{"plugin": pluginID, "tool": toolName, "args": args, "settings": settings})
	if err != nil {
		return nil, err
	}
	outFile := filepath.Join(os.TempDir(), "aide-plugin-call-"+newID()+".json")
	defer os.Remove(outFile)
	ctx, cancel := context.WithTimeout(parent, 310*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "node", "-e", pluginHostJS, "call", a.pluginsPath, string(req), outFile)
	cmd.Env = []string{"PATH=/usr/local/bin:/usr/bin:/bin", "HOME=/home/aide"}
	if bridgeToken := os.Getenv("AIDE_BROWSER_BRIDGE_TOKEN"); pluginID == "browser-control" && len(bridgeToken) >= 32 {
		cmd.Env = append(cmd.Env, "AIDE_BROWSER_BRIDGE_TOKEN="+bridgeToken)
	}
	if bridgeToken := os.Getenv("AIDE_COMPUTER_BRIDGE_TOKEN"); pluginID == "computer-control" && len(bridgeToken) >= 32 {
		cmd.Env = append(cmd.Env, "AIDE_COMPUTER_BRIDGE_TOKEN="+bridgeToken)
	}
	// 有界读取 stdout/stderr，防止坏插件狂写致 OOM（实际结果写入 outFile，此处仅用于排障）。
	pluginStdout := &limitedBytesWriter{limit: 64 << 10}
	pluginStderr := &limitedBytesWriter{limit: 64 << 10}
	cmd.Stdout = pluginStdout
	cmd.Stderr = pluginStderr
	runErr := cmd.Run()
	if ctx.Err() != nil && runErr != nil {
		if parent.Err() != nil {
			return nil, parent.Err()
		}
		return nil, errors.New("插件工具执行超时")
	} else if runErr != nil {
		detail := strings.TrimSpace(pluginStderr.String())
		if detail == "" {
			detail = strings.TrimSpace(pluginStdout.String())
		}
		return nil, fmt.Errorf("插件宿主失败: %s", detail)
	}
	b, err := os.ReadFile(outFile)
	if err != nil {
		return nil, fmt.Errorf("工具结果读取失败: %w", err)
	}
	var result struct {
		OK     bool   `json:"ok"`
		Result any    `json:"result"`
		Error  string `json:"error"`
	}
	if err := json.Unmarshal(b, &result); err != nil {
		return nil, errors.New("工具结果解析失败")
	}
	if !result.OK {
		return nil, errors.New(result.Error)
	}
	return result.Result, nil
}

// normalizePluginResult 归一化插件工具结果：文本摘要 + 提案列表。
func normalizePluginResult(raw any) (string, []map[string]any) {
	text := "插件工具已执行。"
	proposals := []map[string]any{}
	collect := func(v any) {
		switch t := v.(type) {
		case string:
			if t != "" {
				text = t
			}
		case map[string]any:
			if prop, ok := t["proposal"].(map[string]any); ok {
				proposals = append(proposals, prop)
			} else if s, ok := t["text"].(string); ok {
				text = s
			} else {
				// Preserve structured status/results for the model. Do not dump
				// binary screenshot data into a text-only tool message.
				summary := make(map[string]any, len(t))
				for k, v := range t {
					if k == "imageBase64" {
						summary["imageNotice"] = "截图已返回；当前文本工具结果不包含图像，不能据此声称已看见屏幕。"
						continue
					}
					summary[k] = v
				}
				if b, err := json.Marshal(summary); err == nil {
					text = string(b)
				}
			}
		}
	}
	if list, ok := raw.([]any); ok {
		for _, item := range list {
			collect(item)
		}
	} else {
		collect(raw)
	}
	if len(proposals) > 0 {
		text += "（含 " + fmt.Sprint(len(proposals)) + " 条提案，等待用户批准）"
	}
	return text, proposals
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
