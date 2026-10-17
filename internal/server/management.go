package server

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type AgentManagementPermissions struct {
	AidePlugins         bool `json:"aidePlugins"`
	AssistantPlugins    bool `json:"assistantPlugins"`
	AideReferences      bool `json:"aideReferences"`
	AssistantReferences bool `json:"assistantReferences"`
}
type ManagementProposal struct {
	ID              string             `json:"id"`
	Workspace       string             `json:"workspace"`
	Revision        uint64             `json:"revision"`
	Actor           string             `json:"actor"`
	Kind            string             `json:"kind"`
	Action          string             `json:"action"`
	Target          string             `json:"target"`
	Reason          string             `json:"reason"`
	Status          string             `json:"status"`
	Created         string             `json:"created"`
	Result          string             `json:"result,omitempty"`
	Base            string             `json:"base"`
	Manifest        map[string]any     `json:"manifest,omitempty"`
	Files           map[string]string  `json:"files,omitempty"`
	Source          *Source            `json:"source,omitempty"`
	SHA256          string             `json:"sha256,omitempty"`
	FileBases       map[string]string  `json:"fileBases,omitempty"`
	Skill           *SkillCacheReceipt `json:"skill,omitempty"`
	SkillPluginBase string             `json:"skillPluginBase,omitempty"`
}
type managementGuardKey struct{}
type managementBundleKey struct{}

func (a *App) managementActorLocked(task *Task, forced string) string {
	if forced == "assistant" || task != nil && task.AssistantManaged {
		return "assistant"
	}
	if task != nil {
		for _, s := range a.sessions {
			if s.Kind == assistantSessionKind {
				for _, run := range s.Runs {
					if run == task {
						return "assistant"
					}
				}
			}
		}
	}
	return "aide"
}
func (a *App) managementAllowedLocked(actor, kind string) bool {
	p := a.settings.AgentManagement
	if actor == "assistant" {
		if kind == "plugins" {
			return p.AssistantPlugins
		}
		return p.AssistantReferences
	}
	if kind == "plugins" {
		return p.AidePlugins
	}
	return p.AideReferences
}
func (a *App) managementPathLocked() string {
	return filepath.Join(ConfigDir(a.dataPath), "management-"+hash([]byte(a.wsID()))[:24]+".json")
}
func loadManagementQueue(p string) ([]ManagementProposal, error) {
	b, err := os.ReadFile(p)
	if errors.Is(err, os.ErrNotExist) {
		return []ManagementProposal{}, nil
	}
	if err != nil {
		return nil, err
	}
	if len(b) > 12<<20 {
		return nil, errors.New("管理提案存储超过限额")
	}
	var q []ManagementProposal
	err = json.Unmarshal(b, &q)
	return q, err
}
func (a *App) managementBaseLocked(kind, id string) string {
	if kind == "references" {
		b, _ := json.Marshal(a.sourceRegistry)
		return hash(b)
	}
	for _, p := range a.pluginRegistry.Plugins {
		if p.ID == id {
			b, _ := json.Marshal(p)
			code, _ := os.ReadFile(filepath.Join(a.pluginsPath, id, p.Main))
			manifest, _ := os.ReadFile(filepath.Join(a.pluginsPath, id, "manifest.json"))
			return hash(append(append(b, code...), manifest...))
		}
	}
	return "absent"
}
func (a *App) managementFileBaseLocked(id, name string) string {
	root, err := os.OpenRoot(filepath.Join(a.pluginsPath, id))
	if err != nil {
		return "absent"
	}
	defer root.Close()
	b, err := readRawBytes(root, name)
	if errors.Is(err, os.ErrNotExist) {
		return "absent"
	}
	if err != nil {
		return "unreadable"
	}
	return hash(b)
}
func managementGuardLocked(a *App, r *http.Request) error {
	if guard, ok := r.Context().Value(managementGuardKey{}).(func() error); ok {
		return guard()
	}
	return nil
}
func (a *App) managementProposalCurrentLocked(p ManagementProposal) error {
	if p.Workspace != a.wsID() || p.Revision != a.wsRevision {
		return errors.New("工作空间配置已变化，请重新生成管理提案")
	}
	if !a.managementAllowedLocked(p.Actor, p.Kind) {
		return errors.New("该角色的管理权限已关闭")
	}
	if sandboxMode(a.settings.SandboxMode) == "read-only" {
		return errors.New("只读沙箱不能执行管理变更")
	}
	for name, want := range p.FileBases {
		if a.managementFileBaseLocked(p.Target, name) != want {
			return errors.New("插件源文件已变化，请重新生成管理提案")
		}
	}
	if a.managementBaseLocked(p.Kind, p.Target) != p.Base {
		return errors.New("插件或引用已变化，请重新生成管理提案")
	}
	return a.skillProposalCurrentLocked(p)
}
func (a *App) appendManagementProposal(p ManagementProposal) (ManagementProposal, error) {
	a.managementMu.Lock()
	defer a.managementMu.Unlock()
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := a.managementProposalCurrentLocked(p); err != nil {
		return p, err
	}
	file := a.managementPathLocked()
	q, err := loadManagementQueue(file)
	if err != nil {
		return p, err
	}
	pending := 0
	for _, old := range q {
		if old.Status == "pending" {
			pending++
		}
	}
	if pending >= 12 {
		return p, errors.New("待确认管理提案已达 12 个，请先处理")
	}
	if len(q) >= 24 {
		kept := []ManagementProposal{}
		for _, old := range q {
			if old.Status == "pending" {
				kept = append(kept, old)
			}
		}
		q = kept
	}
	q = append(q, p)
	b, _ := json.Marshal(q)
	if len(b) > 10<<20 {
		return p, errors.New("管理提案内容总量超过限额")
	}
	err = atomicJSON(file, q)
	if err == nil {
		go a.broadcastSessionsChanged("")
	}
	return p, err
}

const pluginAuthoringGuide = `aide 插件工坊：先根据用户提供的资料和现有能力判断落点。可执行工具/专业操作做插件；规范、模板、知识与操作说明做引用文档（write_file(source=用户指定的可见且可写引用ID)；新交付说明默认 system-docs）。新引用先用 manage_references 登记，不能自动解开原有锁。
插件包必须有 manifest.json 和自包含 index.js。manifest: {"id":"my-tool","name":"工具名称","version":"1.0.0","main":"index.js","description":"用途","author":"作者"}。不复制用户 API Key、密码或账户设置。扩展已安装插件时先 inspect，保留现有工具和行为、提高版本号、提交完整候选文件，不直接修改已安装目录。
index.js 例：module.exports={name:'工具名称',apply(ctx){ctx.tool({name:'my_tool',description:'功能',parameters:{type:'object',properties:{text:{type:'string'}},required:['text']},handler(args,api){return {text:args.text};}});}};
读取工作区使用 api.readFile(relativePath)/api.listFiles(relativePath)。写入返回 api.proposeWrite(relativePath,content)，命令返回 api.proposeCommand(command)，不能声称提案已落盘。准备包只做 Node 语法和 ZIP/manifest 检查，不执行生成代码；真实工具注册、依赖与运行结果在确认安装/升级后才可验证。index.js 上限256KiB、生成文件合计1MiB/64个，不自动下载依赖；需嵌入 JSON 表等数据的入口应自包含，以兼容独立入口校验。prepare 参数 manifest 对象和 files 对象（相对路径=>完整文本），不含 manifest.json。工具返回待确认提案；安装/升级或删除只有人确认后执行。现有引用内容编辑仍使用 read_file/write_file(source=ID) 并遵守可见与可写锁。`

func authoringBundle(manifest map[string]any, files map[string]string) ([]byte, error) {
	if len(files) == 0 || len(files) > 64 {
		return nil, errors.New("插件候选需包含 1–64 个文本文件")
	}
	raw, err := json.Marshal(manifest)
	if err != nil {
		return nil, err
	}
	if len(raw) > 64<<10 {
		return nil, errors.New("manifest 过大")
	}
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	all := map[string]string{"manifest.json": string(raw)}
	total := len(raw)
	for name, content := range files {
		if name == "manifest.json" || safePath(name) != nil || strings.Contains(name, "\\") || strings.Contains(name, ":") || strings.HasPrefix(name, "/") || filepath.ToSlash(filepath.Clean(name)) != name {
			return nil, errors.New("插件文件路径无效或重复 manifest")
		}
		total += len(content)
		if total > 1<<20 {
			return nil, errors.New("生成插件超过 1MiB，请精简或分步制作")
		}
		all[name] = content
	}
	names := make([]string, 0, len(all))
	for n := range all {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		f, e := w.Create(n)
		if e != nil {
			return nil, e
		}
		if _, e = f.Write([]byte(all[n])); e != nil {
			return nil, e
		}
	}
	if err = w.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
func validateAuthoringSyntax(ctx context.Context, raw []byte) error {
	m, files, err := parsePluginBundle(raw)
	if err != nil {
		return err
	}
	if m.Main != "index.js" {
		return errors.New("插件工坊入口必须为自包含 index.js")
	}
	dir, err := os.MkdirTemp("", "aide-authoring-check-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	for name, b := range files {
		if strings.HasSuffix(name, ".js") {
			file := filepath.Join(dir, filepath.Base(name))
			if err = os.WriteFile(file, b, 0600); err != nil {
				return err
			}
			check, cancel := context.WithTimeout(ctx, 10*time.Second)
			out, e := exec.CommandContext(check, "node", "--check", file).CombinedOutput()
			cancel()
			if e != nil {
				return fmt.Errorf("%s 语法检查失败: %s", name, string(out))
			}
		}
	}
	return nil
}
func (a *App) executeManagementTool(ctx context.Context, call ToolCall, task *Task, forced string) string {
	if ctx.Err() != nil {
		return "管理请求已取消"
	}
	if a.pluginOwnerOf(call.Function.Name) != "plugin-authoring" || a.toolDenied(call.Function.Name) {
		return "插件工坊未启用或工具被禁用"
	}
	var in struct {
		Action   string            `json:"action"`
		ID       string            `json:"id"`
		Manifest map[string]any    `json:"manifest"`
		Files    map[string]string `json:"files"`
		Name     string            `json:"name"`
		Type     string            `json:"type"`
		Config   SourceConfig      `json:"config"`
		Reason   string            `json:"reason"`
	}
	if len(call.Function.Arguments) > 2<<20 || json.Unmarshal([]byte(call.Function.Arguments), &in) != nil {
		return "管理请求无效"
	}
	kind := "plugins"
	if call.Function.Name == "manage_references" {
		kind = "references"
	}
	if kind == "plugins" && in.Action == "guide" {
		return pluginAuthoringGuide
	}
	a.mu.Lock()
	if task != nil && task.WorkspaceID != "" && task.WorkspaceID != a.wsID() {
		a.mu.Unlock()
		return "任务工作空间已变化"
	}
	actor := a.managementActorLocked(task, forced)
	allowed := a.managementAllowedLocked(actor, kind)
	if in.Action == "list" {
		out := map[string]any{"actor": actor, "managementAllowed": allowed}
		if kind == "plugins" {
			items := []map[string]any{}
			for _, p := range a.pluginRegistry.Plugins {
				items = append(items, map[string]any{"id": p.ID, "name": p.Name, "version": p.Version, "enabled": p.Enabled})
			}
			out["plugins"] = items
		} else {
			items := []map[string]any{}
			for _, s := range a.sourceRegistry.Sources {
				if allowed || sourceAIVisible(s, a.wsID()) {
					items = append(items, map[string]any{"id": s.ID, "name": s.Name, "type": s.Type, "config": s.Config, "rw": s.RW, "aiVisible": sourceAIVisible(s, a.wsID()), "enabled": s.Enabled})
				}
			}
			out["references"] = items
		}
		a.mu.Unlock()
		b, _ := json.Marshal(out)
		return string(b)
	}
	if !allowed {
		a.mu.Unlock()
		return "管理权限未开启：请在设置 → AI 管理权限中开启对应角色的权限；不要绕过设置或索取密钥"
	}
	if kind == "plugins" && in.Action == "inspect" {
		for _, p := range a.pluginRegistry.Plugins {
			if p.ID == in.ID {
				root, e := os.OpenRoot(filepath.Join(a.pluginsPath, p.ID))
				a.mu.Unlock()
				if e != nil {
					return e.Error()
				}
				defer root.Close()
				entry, e := readRawBytes(root, p.Main)
				if e != nil || len(entry) > maxPluginCode {
					return "无法读取受限的插件入口"
				}
				out := map[string]any{"id": p.ID, "name": p.Name, "description": p.Description, "version": p.Version, "main": p.Main, "code": string(entry), "guide": pluginAuthoringGuide}
				b, _ := json.Marshal(out)
				return string(b)
			}
		}
		a.mu.Unlock()
		return "插件不存在"
	}
	if sandboxMode(a.settings.SandboxMode) == "read-only" {
		a.mu.Unlock()
		return "只读沙箱不能生成管理变更"
	}
	p := ManagementProposal{ID: newID(), Workspace: a.wsID(), Revision: a.wsRevision, Actor: actor, Kind: kind, Action: in.Action, Target: in.ID, Reason: in.Reason, Status: "pending", Created: time.Now().UTC().Format(time.RFC3339Nano)}
	if kind == "plugins" && in.Action == "prepare" {
		id, _ := in.Manifest["id"].(string)
		p.Target = id
	}
	p.Base = a.managementBaseLocked(kind, p.Target)
	a.mu.Unlock()
	if kind == "plugins" {
		if !pluginIDPattern.MatchString(p.Target) {
			return "插件 ID 无效"
		}
		switch in.Action {
		case "prepare":
			clean := map[string]any{}
			for k, v := range in.Manifest {
				switch k {
				case "settings", "enabled", "installedAt", "packageSHA256", "updateURL":
					continue
				}
				clean[k] = v
			}
			in.Manifest = clean
			raw, err := authoringBundle(in.Manifest, in.Files)
			if err != nil {
				return err.Error()
			}
			if err = validateAuthoringSyntax(ctx, raw); err != nil {
				return err.Error()
			}
			p.Manifest = in.Manifest
			p.Files = in.Files
			p.SHA256 = hash(raw)
			a.mu.Lock()
			p.FileBases = map[string]string{}
			for name := range in.Files {
				p.FileBases[name] = a.managementFileBaseLocked(p.Target, name)
			}
			a.mu.Unlock()
		case "enable", "disable", "remove":
			if p.Base == "absent" {
				return "插件不存在"
			}
		default:
			return "不支持的插件管理动作"
		}
	} else {
		if !sourceIDPattern.MatchString(p.Target) || p.Target == systemDocsSource || p.Target == contextSource {
			return "请指定普通引用 ID；系统内置引用由工作空间配置管理"
		}
		switch in.Action {
		case "upsert":
			if strings.TrimSpace(in.Name) == "" || len(in.Name) > 200 {
				return "引用名称需为 1–200 字符"
			}
			if in.Type != "local" && in.Type != "skill" && in.Type != "link" && in.Type != "sftp" {
				return "请选择本地、Skill、HTTPS链接或SFTP文件引用"
			}
			if (in.Type == "local" || in.Type == "skill") && strings.TrimSpace(in.Config.Path) == "" {
				return "引用目录不能为空"
			}
			if in.Type == "link" {
				u, err := url.Parse(in.Config.URL)
				if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil {
					return "引用链接必须为不含凭据的 HTTPS 地址"
				}
			}
			if in.Type == "sftp" && (in.Config.Host == "" || (in.Config.Auth != "" && in.Config.Auth != "none")) {
				return "SFTP 管理请求不能包含凭据；请在引用设置中另行配置"
			}
			if in.Config.Command != "" || len(in.Config.Args) > 0 || in.Config.Transport != "" || len(in.Config.MCPTools) > 0 {
				return "不能通过资料引用配置执行进程"
			}
			p.Source = &Source{ID: p.Target, Name: in.Name, Type: in.Type, Enabled: true, Config: in.Config}
		case "remove":
		default:
			return "不支持的引用管理动作"
		}
	}
	if len(p.Reason) > 2000 {
		p.Reason = p.Reason[:2000]
	}
	_, err := a.appendManagementProposal(p)
	if err != nil {
		return "管理提案未生成: " + err.Error()
	}
	b, _ := json.Marshal(map[string]any{"proposalId": p.ID, "status": "pending_human_confirmation", "actor": p.Actor, "kind": p.Kind, "action": p.Action, "target": p.Target, "sha256": p.SHA256, "next": "已生成可审阅管理提案，在输入框上方确认后才会生效；不要声称已经安装或修改引用"})
	return string(b)
}

func (a *App) listManagementProposals(w http.ResponseWriter, r *http.Request) {
	a.managementMu.Lock()
	defer a.managementMu.Unlock()
	a.mu.Lock()
	file := a.managementPathLocked()
	a.mu.Unlock()
	q, err := loadManagementQueue(file)
	if err != nil {
		fail(w, 500, err)
		return
	}
	items := []map[string]any{}
	for _, p := range q {
		if p.Status != "pending" && p.Status != "failed" && p.Status != "running" {
			continue
		}
		items = append(items, map[string]any{"id": p.ID, "actor": p.Actor, "kind": p.Kind, "action": p.Action, "target": p.Target, "reason": p.Reason, "status": p.Status, "result": p.Result, "created": p.Created, "fileCount": len(p.Files), "sha256": p.SHA256})
	}
	jsonOut(w, 200, map[string]any{"proposals": items})
}
func (a *App) getManagementProposal(w http.ResponseWriter, r *http.Request) {
	a.managementMu.Lock()
	defer a.managementMu.Unlock()
	a.mu.Lock()
	file := a.managementPathLocked()
	a.mu.Unlock()
	q, err := loadManagementQueue(file)
	if err != nil {
		fail(w, 500, err)
		return
	}
	for _, p := range q {
		if p.ID == r.PathValue("id") {
			jsonOut(w, 200, p)
			return
		}
	}
	fail(w, 404, errors.New("管理提案不存在"))
}
func (a *App) downloadManagementPackage(w http.ResponseWriter, r *http.Request) {
	a.managementMu.Lock()
	defer a.managementMu.Unlock()
	a.mu.Lock()
	file := a.managementPathLocked()
	a.mu.Unlock()
	q, err := loadManagementQueue(file)
	if err != nil {
		fail(w, 500, err)
		return
	}
	for _, p := range q {
		if p.ID == r.PathValue("id") && p.Kind == "plugins" && p.Action == "prepare" {
			raw, e := authoringBundle(p.Manifest, p.Files)
			if e != nil || hash(raw) != p.SHA256 {
				fail(w, 409, errors.New("插件包指纹已变化"))
				return
			}
			w.Header().Set("Content-Type", "application/zip")
			w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s.zip\"", p.Target))
			w.Write(raw)
			return
		}
	}
	fail(w, 404, errors.New("插件包不存在"))
}
func (a *App) resolveManagementProposal(w http.ResponseWriter, r *http.Request) {
	a.managementMu.Lock()
	defer a.managementMu.Unlock()
	a.mu.Lock()
	file := a.managementPathLocked()
	a.mu.Unlock()
	q, err := loadManagementQueue(file)
	if err != nil {
		fail(w, 500, err)
		return
	}
	index := -1
	for i := range q {
		if q[i].ID == r.PathValue("id") {
			index = i
			break
		}
	}
	if index < 0 {
		fail(w, 404, errors.New("管理提案不存在"))
		return
	}
	p := q[index]
	if r.PathValue("action") == "reject" {
		if p.Status != "pending" && p.Status != "failed" {
			fail(w, 409, errors.New("当前状态不可重复决策"))
			return
		}
		q[index].Status = "rejected"
		if err = atomicJSON(file, q); err != nil {
			fail(w, 500, err)
			return
		}
		jsonOut(w, 200, map[string]bool{"ok": true})
		return
	}
	if r.PathValue("action") != "apply" || p.Status != "pending" {
		fail(w, 409, errors.New("提案不可执行，请检查当前状态"))
		return
	}
	a.mu.Lock()
	err = a.managementProposalCurrentLocked(p)
	a.mu.Unlock()
	if err != nil {
		fail(w, 409, err)
		return
	}
	q[index].Status = "running"
	if err = atomicJSON(file, q); err != nil {
		fail(w, 500, err)
		return
	}
	// The closure is server-owned, not a field accepted from client/model JSON.
	ctx := context.WithValue(r.Context(), managementGuardKey{}, func() error { return a.managementProposalCurrentLocked(p) })
	result := httptest.NewRecorder()
	if p.Kind == "plugins" {
		if p.Action == "prepare" {
			raw, e := authoringBundle(p.Manifest, p.Files)
			if e != nil || hash(raw) != p.SHA256 {
				fail(result, 409, errors.New("插件包指纹已变化"))
			} else if p.Base == "absent" {
				var body bytes.Buffer
				form := multipart.NewWriter(&body)
				f, _ := form.CreateFormFile("bundle", p.Target+".zip")
				f.Write(raw)
				form.Close()
				req := httptest.NewRequest(http.MethodPost, "/api/plugins/bundle", &body).WithContext(ctx)
				req.Header.Set("Content-Type", form.FormDataContentType())
				a.uploadPluginBundle(result, req)
			} else {
				ctx = context.WithValue(ctx, managementBundleKey{}, raw)
				req := httptest.NewRequest(http.MethodPost, "/api/plugins/"+p.Target+"/upgrade", strings.NewReader(`{}`)).WithContext(ctx)
				req.SetPathValue("id", p.Target)
				a.upgradePlugin(result, req)
			}
		} else {
			req := httptest.NewRequest(http.MethodPut, "/api/plugins/"+p.Target, strings.NewReader(fmt.Sprintf(`{"enabled":%t}`, p.Action == "enable"))).WithContext(ctx)
			req.SetPathValue("id", p.Target)
			if p.Action == "remove" {
				a.deletePlugin(result, req)
			} else {
				a.togglePlugin(result, req)
			}
		}
	} else {
		a.mu.Lock()
		sources := append([]Source(nil), a.sourceRegistry.Sources...)
		a.mu.Unlock()
		next := []Source{}
		found := false
		for _, s := range sources {
			if s.Builtin {
				continue
			} // Native updateSources preserves built-ins without re-validating their virtual empty paths.
			if s.ID != p.Target {
				next = append(next, s)
				continue
			}
			found = true
			if p.Action == "upsert" {
				replacement := *p.Source
				replacement.RW = s.RW
				replacement.AIHiddenWorkspaces = append([]string(nil), s.AIHiddenWorkspaces...)
				replacement.Enabled = s.Enabled
				next = append(next, replacement)
			}
		}
		if !found && p.Action == "upsert" {
			next = append(next, *p.Source)
		}
		if !found && p.Action == "remove" {
			fail(result, 409, errors.New("引用已不存在"))
		} else {
			raw, _ := json.Marshal(map[string]any{"sources": next})
			req := httptest.NewRequest(http.MethodPut, "/api/sources", bytes.NewReader(raw)).WithContext(ctx)
			a.updateSources(result, req)
		}
	}
	q[index].Result = result.Body.String()
	if len(q[index].Result) > 4000 {
		q[index].Result = q[index].Result[:4000]
	}
	q[index].Status = "applied"
	if result.Code < 200 || result.Code >= 300 {
		q[index].Status = "failed"
	}
	if err = atomicJSON(file, q); err != nil {
		fail(w, 500, fmt.Errorf("管理操作已返回，但回执保存失败，请检查实际状态: %w", err))
		return
	}
	go a.broadcastSessionsChanged("")
	if q[index].Status == "failed" {
		fail(w, result.Code, errors.New(q[index].Result))
		return
	}
	jsonOut(w, 200, map[string]any{"ok": true, "proposal": q[index].ID, "status": q[index].Status})
}
