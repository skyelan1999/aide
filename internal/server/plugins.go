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
	Daemon      bool   `json:"daemon,omitempty"` // 协议 v1.2：常驻守护插件
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
// 慢进程（exec node，最长 10s）在 a.mu 之外执行：仅在 a.mu 内快照启用插件列表/路径，
// node 结束后再用短锁把 surface 写回。调用方不得持有 a.mu（本函数会自行取放）。
func (a *App) runPluginHost(ctx context.Context) {
	a.mu.Lock()
	enabled := []map[string]string{}
	for _, p := range a.pluginRegistry.Plugins {
		if p.Enabled {
			enabled = append(enabled, map[string]string{"id": p.ID, "name": p.Name})
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
	if a.daemons != nil && a.isDaemonPlugin(pluginID) {
		return a.daemons.Call(pluginID, toolName, args)
	}
	req, err := json.Marshal(map[string]any{"plugin": pluginID, "tool": toolName, "args": args})
	if err != nil {
		return nil, err
	}
	outFile := filepath.Join(os.TempDir(), "aide-plugin-call-"+newID()+".json")
	defer os.Remove(outFile)
	ctx, cancel := context.WithTimeout(context.Background(), 70*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "node", "-e", pluginHostJS, "call", a.pluginsPath, string(req), outFile)
	cmd.Env = []string{"PATH=/usr/local/bin:/usr/bin:/bin", "HOME=/home/aide"}
	// 有界读取 stdout/stderr，防止坏插件狂写致 OOM（实际结果写入 outFile，此处仅用于排障）。
	pluginStdout := &limitedBytesWriter{limit: 64 << 10}
	pluginStderr := &limitedBytesWriter{limit: 64 << 10}
	cmd.Stdout = pluginStdout
	cmd.Stderr = pluginStderr
	runErr := cmd.Run()
	if ctx.Err() != nil && runErr != nil {
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
