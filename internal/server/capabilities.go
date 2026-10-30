package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"time"
)

// Check receipts are bounded, process-local observations, never permission grants.
type CapabilityCheck struct {
	State      string `json:"state"`
	At         string `json:"at"`
	DurationMS int64  `json:"durationMs"`
	Message    string `json:"message"`
	Binding    string `json:"-"`
}
type CapabilityItem struct {
	Key                          string          `json:"key"`
	Kind                         string          `json:"kind"`
	Name                         string          `json:"name"`
	Detail                       string          `json:"detail"`
	Enabled                      bool            `json:"enabled"`
	WritableConfigured           bool            `json:"writableConfigured,omitempty"`
	Fingerprint                  string          `json:"fingerprint"`
	ToolCount                    int             `json:"toolCount,omitempty"`
	DaemonState                  string          `json:"daemonState,omitempty"`
	HostState                    string          `json:"hostState,omitempty"`
	Check                        CapabilityCheck `json:"check"`
	WriteCheck                   CapabilityCheck `json:"writeCheck"`
	binding                      string
	source                       Source
	plugin                       PluginManifest
	root                         *os.Root
	baseURL, apiKey, pluginsPath string
}

func capabilityDigest(value any) string { b, _ := json.Marshal(value); return hash(b) }

// Caller holds a.mu. Private binding includes encrypted credential envelopes;
// neither it nor any credentials or upstream output are returned to the client.
func (a *App) capabilityItemsLocked() []CapabilityItem {
	credentials := capabilityDigest(a.wsSecrets) + capabilityDigest(a.sourceSecrets)
	if a.vault != nil {
		credentials += capabilityDigest(a.vault.entries) + fmt.Sprint(a.vault.Unlocked())
	}
	key, _ := a.modelAPIKeyLocked()
	credentials += hash([]byte(key))
	workspace := a.wsID()
	items := []CapabilityItem{{Key: "workspace", Kind: "workspace", Name: "工作空间", Detail: a.wsConfig.Workspace.Mode + " · 根目录读取", Enabled: true, WritableConfigured: a.settings.SandboxMode != "read-only", root: a.workspace}}
	items[0].Fingerprint = capabilityDigest([]any{workspace, a.wsRevision, a.wsConfig.Workspace})
	items[0].binding = items[0].Fingerprint + credentials
	for _, source := range a.sourceRegistry.Sources {
		item := CapabilityItem{Key: "source:" + source.ID, Kind: "source", Name: source.Name, Detail: source.Type + " · 根目录／工具发现", Enabled: source.Enabled, WritableConfigured: source.RW, ToolCount: len(source.Config.MCPTools), source: source}
		item.Fingerprint = capabilityDigest([]any{workspace, a.wsRevision, source})
		item.binding = item.Fingerprint + credentials
		items = append(items, item)
	}
	daemonStates := map[string]string{}
	if a.daemons != nil {
		for _, daemon := range a.daemons.List() {
			daemonStates[daemon.ID] = daemon.Status
		}
	}
	var surface struct {
		Plugins []struct {
			ID    string            `json:"id"`
			Tools []json.RawMessage `json:"tools"`
			Error string            `json:"error"`
		} `json:"plugins"`
	}
	_ = json.Unmarshal(a.pluginSurface, &surface)
	for _, plugin := range a.pluginRegistry.Plugins {
		count := 0
		hostState := "not_registered"
		for _, entry := range surface.Plugins {
			if entry.ID == plugin.ID {
				count = len(entry.Tools)
				hostState = "registered"
				if entry.Error != "" {
					hostState = "error"
				}
			}
		}
		item := CapabilityItem{Key: "plugin:" + plugin.ID, Kind: "plugin", Name: plugin.Name, Detail: "入口文件读取；宿主工具数为最近登记结果", Enabled: plugin.Enabled, ToolCount: count, DaemonState: daemonStates[plugin.ID], HostState: hostState, plugin: plugin, pluginsPath: a.pluginsPath}
		item.Fingerprint = capabilityDigest([]any{workspace, a.wsRevision, plugin, a.pluginsPath})
		item.binding = item.Fingerprint
		items = append(items, item)
	}
	for _, module := range []string{"pypdf", "docx", "openpyxl", "pptx"} {
		item := CapabilityItem{Key: "parser:" + module, Kind: "parser", Name: module, Detail: "Python 隔离导入；不代表文档处理通过", Enabled: true}
		item.Fingerprint = capabilityDigest([]string{workspace, module})
		item.binding = item.Fingerprint
		items = append(items, item)
	}
	model := CapabilityItem{Key: "model", Kind: "model", Name: "模型连接", Detail: a.settings.Model + " · 模型列表接口，不调用生成", Enabled: a.settings.BaseURL != "", baseURL: a.settings.BaseURL, apiKey: key}
	model.Fingerprint = capabilityDigest([]any{workspace, a.wsRevision, a.settings.BaseURL, a.settings.Model})
	model.binding = model.Fingerprint + credentials
	items = append(items, model)
	for i := range items {
		items[i].Check = CapabilityCheck{State: "not_run", Message: "尚未检查"}
		items[i].WriteCheck = CapabilityCheck{State: "not_run", Message: "尚未实测写入"}
		items[i].Check = a.currentCapabilityCheckLocked(items[i].Key, items[i].binding, "read")
		items[i].WriteCheck = a.currentCapabilityCheckLocked(items[i].Key, items[i].binding, "write")
	}
	return items
}

func capabilityReceiptKey(key, mode string) string {
	if mode == "write" {
		return key + "#write"
	}
	return key
}

func (a *App) currentCapabilityCheckLocked(key, binding, mode string) CapabilityCheck {
	message := "尚未检查"
	if mode == "write" {
		message = "尚未实测写入"
	}
	saved, ok := a.capabilityChecks[capabilityReceiptKey(key, mode)]
	if !ok {
		return CapabilityCheck{State: "not_run", Message: message}
	}
	if saved.Binding != binding {
		saved.State, saved.Message = "stale", "配置或凭据已变化，请重新检查"
	} else if at, err := time.Parse(time.RFC3339Nano, saved.At); err != nil || time.Since(at) > 5*time.Minute {
		saved.State, saved.Message = "stale", "检查已超过5分钟，请重新检查"
	}
	return saved
}
func (a *App) listCapabilities(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	items := a.capabilityItemsLocked()
	workspace := a.wsID()
	busy := a.capabilityChecking
	a.mu.Unlock()
	jsonOut(w, 200, map[string]any{"workspaceId": workspace, "items": items, "checking": busy, "note": "检查只证明该时间点的具体步骤；只读检查不证明写权限。结果保留5分钟，重启清空。"})
}
func (a *App) checkCapability(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Key         string `json:"key"`
		Fingerprint string `json:"fingerprint"`
		WorkspaceID string `json:"workspaceId"`
		Mode        string `json:"mode"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	if err := decode(w, r, &in); err != nil {
		fail(w, 400, err)
		return
	}
	a.mu.Lock()
	var selected CapabilityItem
	found := false
	for _, item := range a.capabilityItemsLocked() {
		if item.Key == in.Key {
			selected = item
			found = true
			break
		}
	}
	if !found {
		a.mu.Unlock()
		fail(w, 404, errors.New("能力项目不存在"))
		return
	}
	if in.WorkspaceID != a.wsID() || in.Fingerprint != selected.Fingerprint {
		a.mu.Unlock()
		fail(w, 409, errors.New("工作区或配置已变化，请重新读取"))
		return
	}
	if !selected.Enabled {
		a.mu.Unlock()
		fail(w, 400, errors.New("该能力未启用"))
		return
	}
	if in.Mode == "" {
		in.Mode = "read"
	}
	if in.Mode != "read" && in.Mode != "write" {
		a.mu.Unlock()
		fail(w, 400, errors.New("检查模式无效"))
		return
	}
	if in.Mode == "write" && (!selected.WritableConfigured || !capabilityWriteSupported(selected)) {
		a.mu.Unlock()
		fail(w, 400, errors.New("该能力未配置可用的写入探测"))
		return
	}
	if a.capabilityChecking {
		a.mu.Unlock()
		fail(w, 409, errors.New("已有能力检查正在运行"))
		return
	}
	a.capabilityChecking = true
	a.mu.Unlock()
	defer func() { a.mu.Lock(); a.capabilityChecking = false; a.mu.Unlock() }()
	start := time.Now()
	ctx, cancel := context.WithTimeout(r.Context(), 35*time.Second)
	defer cancel()
	var message string
	var err error
	if in.Mode == "write" {
		message, err = a.probeCapabilityWrite(ctx, selected)
	} else {
		message, err = a.probeCapability(ctx, selected)
	}
	if ctx.Err() != nil {
		err = errors.New("检查已取消或超时")
	}
	check := CapabilityCheck{State: "pass", At: time.Now().UTC().Format(time.RFC3339Nano), DurationMS: time.Since(start).Milliseconds(), Message: message, Binding: selected.binding}
	if err != nil {
		check.State = "failed"
		check.Message = err.Error()
	}
	a.mu.Lock()
	same := false
	for _, current := range a.capabilityItemsLocked() {
		if current.Key == selected.Key && current.binding == selected.binding {
			same = true
			break
		}
	}
	if !same || a.wsID() != in.WorkspaceID {
		a.mu.Unlock()
		fail(w, 409, errors.New("检查期间配置已变化，结果未采纳"))
		return
	}
	if a.capabilityChecks == nil {
		a.capabilityChecks = map[string]CapabilityCheck{}
	}
	// Registry removals cannot grow an unbounded cache.
	if len(a.capabilityChecks) >= 256 {
		a.capabilityChecks = map[string]CapabilityCheck{}
	}
	a.capabilityChecks[capabilityReceiptKey(selected.Key, in.Mode)] = check
	a.mu.Unlock()
	jsonOut(w, 200, check)
}

func capabilityWriteSupported(item CapabilityItem) bool {
	switch item.Kind {
	case "workspace":
		return true
	case "source":
		return item.source.RW && (item.source.Type == "local" || item.source.Type == "skill" || item.source.Type == "sftp" || item.source.Type == "workspace-sftp" || item.source.Type == "relay")
	default:
		return false
	}
}

func capabilityProbeName() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return ".aide-capability-probe-" + hex.EncodeToString(b[:]) + ".tmp", nil
}

func writeProbe(root *os.Root, name string) error {
	f, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return errors.New("无法在目标目录创建独占临时探针")
	}
	marker := []byte("aide write permission probe\n")
	n, writeErr := f.Write(marker)
	closeErr := f.Close()
	removeErr := root.Remove(name)
	if writeErr != nil || n != len(marker) || closeErr != nil || removeErr != nil {
		return errors.New("写入探针失败或清理未能确认；请检查目标目录")
	}
	return nil
}

func (a *App) probeCapabilityWrite(ctx context.Context, item CapabilityItem) (string, error) {
	name, err := capabilityProbeName()
	if err != nil {
		return "", errors.New("无法生成安全探针名称")
	}
	switch item.Kind {
	case "workspace":
		if strings.HasPrefix(item.Detail, "ssh") {
			if err := a.ensureSSHSession(ctx); err != nil {
				return "", errors.New("SSH 认证或连接未通过")
			}
			return "", a.probeRemoteWrite(a.workspaceRemotePath(name), nil)
		}
		if item.root == nil {
			return "", errors.New("工作区根目录不可用")
		}
		if err := writeProbe(item.root, name); err != nil {
			return "", err
		}
	case "source":
		if item.source.Type == "local" || item.source.Type == "skill" {
			root, err := a.localSourceRoot(item.source)
			if err != nil {
				return "", errors.New("来源根目录不可用")
			}
			err = writeProbe(root, name)
			_ = root.Close()
			if err != nil {
				return "", err
			}
		} else {
			remote := pathJoinRemote(item.source.Config.Path, name)
			if item.source.Type == "workspace-sftp" {
				remote = a.workspaceRemoteSourcePath(item.source.Config.Path, name)
				if err := a.probeRemoteWrite(remote, nil); err != nil {
					return "", err
				}
			} else if item.source.Type == "relay" {
				if err := a.relayWriteSource(item.source, name, nil, true); err != nil {
					return "", err
				}
				if err := a.relayMutateSource(item.source, "remove", name, ""); err != nil {
					return "", err
				}
			} else if item.source.Type == "sftp" {
				if err := a.probeRemoteSourceWrite(item.source, remote); err != nil {
					return "", err
				}
			} else {
				return "", errors.New("该来源类型不支持写入实测")
			}
		}
	default:
		return "", errors.New("该能力不支持写入实测")
	}
	return "已在目标根目录创建并删除独占临时探针；写入与清理均通过", nil
}

func (a *App) probeRemoteWrite(remote string, expected *uint64) error {
	dir := remote + ".d"
	command := "mkdir " + shellQuoteRemote(dir) + "\n"
	var out string
	var err error
	if expected != nil {
		out, err = a.sftpBatchExpected(command, expected)
	} else {
		out, err = a.sftpBatch(command)
	}
	if err != nil || sftpCommandFailed(out) {
		return errors.New("无法创建 SSH 临时探针目录")
	}
	file := pathJoinRemote(dir, "probe.txt")
	tmp, err := os.CreateTemp("", "aide-capability-probe-*")
	if err != nil {
		if cleanupErr := a.sftpRemove(dir, true); cleanupErr != nil {
			return errors.New("本地探针暂存失败，且远端临时目录清理未能确认")
		}
		return errors.New("本地探针暂存失败")
	}
	local := tmp.Name()
	marker := []byte("aide write permission probe\n")
	n, writeErr := tmp.Write(marker)
	closeErr := tmp.Close()
	defer os.Remove(local)
	if writeErr == nil && n == len(marker) && closeErr == nil {
		var out string
		out, err = a.sftpBatch("put " + shellQuoteRemote(local) + " " + shellQuoteRemote(file) + "\n")
		if err == nil && sftpCommandFailed(out) {
			err = errors.New("SFTP 写入命令失败")
		}
	} else {
		writeErr = errors.New("本地探针写入失败或不完整")
	}
	removeFileErr := a.sftpRemove(file, false)
	removeDirErr := a.sftpRemove(dir, true)
	if writeErr != nil || closeErr != nil || err != nil || removeFileErr != nil || removeDirErr != nil {
		return errors.New("SSH 探针写入失败或远端清理未能确认")
	}
	return nil
}

func (a *App) probeRemoteSourceWrite(src Source, remote string) error {
	dir := remote + ".d"
	out, err := a.sftpBatchSource(src, "mkdir "+shellQuoteRemote(dir)+"\n")
	if err != nil || sftpCommandFailed(out) {
		return errors.New("无法创建 SSH 来源临时探针目录")
	}
	file := pathJoinRemote(dir, "probe.txt")
	tmp, err := os.CreateTemp("", "aide-capability-probe-*")
	if err != nil {
		out, cleanupErr := a.sftpBatchSource(src, "rmdir "+shellQuoteRemote(dir)+"\n")
		if cleanupErr != nil || sftpCommandFailed(out) {
			return errors.New("本地探针暂存失败，且 SSH 来源临时目录清理未能确认")
		}
		return errors.New("本地探针暂存失败")
	}
	local := tmp.Name()
	marker := []byte("aide write permission probe\n")
	n, writeErr := tmp.Write(marker)
	closeErr := tmp.Close()
	defer os.Remove(local)
	if writeErr == nil && n == len(marker) && closeErr == nil {
		var out string
		out, err = a.sftpBatchSource(src, "put "+shellQuoteRemote(local)+" "+shellQuoteRemote(file)+"\n")
		if err == nil && sftpCommandFailed(out) {
			err = errors.New("SFTP 写入命令失败")
		}
	} else {
		writeErr = errors.New("本地探针写入失败或不完整")
	}
	removeFileOut, removeFileErr := a.sftpBatchSource(src, "rm "+shellQuoteRemote(file)+"\n")
	removeDirOut, removeDirErr := a.sftpBatchSource(src, "rmdir "+shellQuoteRemote(dir)+"\n")
	if writeErr != nil || closeErr != nil || err != nil || removeFileErr != nil || sftpCommandFailed(removeFileOut) || removeDirErr != nil || sftpCommandFailed(removeDirOut) {
		return errors.New("SSH 来源探针写入失败或远端清理未能确认")
	}
	return nil
}
func (a *App) probeCapability(ctx context.Context, item CapabilityItem) (string, error) {
	switch item.Kind {
	case "workspace":
		if strings.HasPrefix(item.Detail, "ssh") {
			if err := a.ensureSSHSession(ctx); err != nil {
				return "", errors.New("SSH 认证或连接未通过；请检查已保存配置")
			}
			if _, err := a.sftpList("."); err != nil {
				return "", errors.New("SSH 已连接，但工作区根目录读取失败")
			}
		} else {
			if item.root == nil {
				return "", errors.New("工作区根目录不可用")
			}
			f, err := item.root.Open(".")
			if err != nil {
				return "", errors.New("工作区根目录读取失败")
			}
			defer f.Close()
			if _, err = f.ReadDir(1); err != nil && err != io.EOF {
				return "", errors.New("工作区根目录读取失败")
			}
		}
		return "根目录读取通过；未检查写入或文件操作", nil
	case "source":
		if item.source.Type == "mcp" {
			tools, err := a.probeMCPSource(ctx, item.source)
			if err != nil {
				return "", errors.New("MCP 初始化或工具发现失败；未调用工具")
			}
			return fmt.Sprintf("MCP 初始化通过，发现 %d 个工具；未验证工具执行", len(tools)), nil
		}
		if _, err := a.listSourceDir(item.source, "."); err != nil {
			return "", errors.New("来源根目录读取失败；请核对路径、凭据和连接")
		}
		return "来源根目录读取通过；未验证写权限", nil
	case "plugin":
		if !pluginIDPattern.MatchString(item.plugin.ID) {
			return "", errors.New("插件编号无效")
		}
		if err := safePath(item.plugin.Main); err != nil {
			return "", errors.New("插件入口路径无效")
		}
		root, err := os.OpenRoot(item.pluginsPath)
		if err != nil {
			return "", errors.New("插件目录不可读")
		}
		defer root.Close()
		f, err := root.Open(item.plugin.ID + "/" + item.plugin.Main)
		if err != nil {
			return "", errors.New("插件入口文件不可读")
		}
		defer f.Close()
		st, err := f.Stat()
		if err != nil || !st.Mode().IsRegular() || st.Size() > maxPluginCode {
			return "", errors.New("插件入口不是有效的受限普通文件")
		}
		b, err := io.ReadAll(io.LimitReader(f, maxPluginCode+1))
		if err != nil || len(b) > maxPluginCode {
			return "", errors.New("插件入口读取失败")
		}
		return "入口文件可读，SHA-256 " + hash(b) + "；未执行插件或工具", nil
	case "parser":
		module := strings.TrimPrefix(item.Key, "parser:")
		allowed := map[string]bool{"pypdf": true, "docx": true, "openpyxl": true, "pptx": true}
		if !allowed[module] {
			return "", errors.New("解析器不在检查白名单")
		}
		cmd := exec.CommandContext(ctx, "python3", "-I", "-B", "-c", "import importlib,sys; importlib.import_module(sys.argv[1])", module)
		cmd.Dir = os.TempDir()
		cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "LANG=C.UTF-8"}
		cmd.Stdout = io.Discard
		cmd.Stderr = io.Discard
		if err := cmd.Run(); err != nil {
			return "", errors.New("Python 依赖导入失败或超时；不代表具体文件损坏")
		}
		return "依赖隔离导入通过；具体文件提取与渲染尚未检查", nil
	case "model":
		u, err := url.Parse(item.baseURL)
		if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			return "", errors.New("模型 API 地址无效")
		}
		req, err := http.NewRequestWithContext(ctx, "GET", strings.TrimRight(item.baseURL, "/")+"/models", nil)
		if err != nil {
			return "", errors.New("模型列表请求无法建立")
		}
		if item.apiKey != "" {
			req.Header.Set("Authorization", "Bearer "+item.apiKey)
		}
		client := &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
		resp, err := client.Do(req)
		if err != nil {
			return "", errors.New("模型列表连接失败；未调用生成")
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			return "", fmt.Errorf("模型列表返回 HTTP %d；该提供商可能不支持列表接口", resp.StatusCode)
		}
		b, err := io.ReadAll(io.LimitReader(resp.Body, (2<<20)+1))
		if err != nil || len(b) > 2<<20 {
			return "", errors.New("模型列表响应不可读或超过限额")
		}
		var data struct {
			Data []struct {
				ID string `json:"id"`
			} `json:"data"`
		}
		if json.Unmarshal(b, &data) != nil || len(data.Data) == 0 {
			return "", errors.New("模型列表响应格式无效或为空")
		}
		count := 0
		for _, entry := range data.Data {
			if entry.ID != "" {
				count++
			}
		}
		if count == 0 {
			return "", errors.New("模型列表未包含有效编号")
		}
		return fmt.Sprintf("模型列表读取通过，%d 个编号；未验证生成、视觉或工具调用", count), nil
	}
	return "", errors.New("未知能力检查")
}
