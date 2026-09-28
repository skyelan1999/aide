package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// 辅助资料多来源注册表（FR-82~FR-84 / LIM-30）：
// 登记记录存缓存目录 sources.json；密码/密钥存 /data/sources-secrets.json（0600）。

const (
	sourcesFileName  = "sources.json"
	sourcesSecretsFN = "sources-secrets.json"
	maxSources       = 20
	systemDocsSource = "system-docs"
	// contextSource 是内置只读 /context 来源（AIDE_CONTEXT 常驻挂载）：
	// 恒存在、不可移除、不被 Docs.Path/工作区切换覆盖。Config.Path 恒空，
	// localSourceRoot 据此回落 a.reference（真实 /context）。
	contextSource = "context"
	curlTimeout   = 30 * time.Second
)

var sourceIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

type Source struct {
	ID      string       `json:"id"`
	Name    string       `json:"name"`
	Type    string       `json:"type"` // local | skill | link | mcp | sftp | workspace-sftp | ftp | ftps | smb
	Enabled bool         `json:"enabled"`
	RW      bool         `json:"rw,omitempty"`
	Builtin bool         `json:"builtin,omitempty"`
	Config  SourceConfig `json:"config"`
}

// SourceConfig deliberately keeps an MCP command separate from its arguments.
// A reference source is never executed through a shell, so spaces, quoting and
// shell metacharacters in a command field cannot turn into an extra command.
type SourceConfig struct {
	Path      string    `json:"path,omitempty"`      // local/skill 宿主机路径
	URL       string    `json:"url,omitempty"`       // link/ftp/ftps/smb
	Host      string    `json:"host,omitempty"`      // sftp
	Port      int       `json:"port,omitempty"`      // sftp
	Username  string    `json:"username,omitempty"`  // sftp
	Auth      string    `json:"auth,omitempty"`      // sftp: password | key | none
	Command   string    `json:"command,omitempty"`   // mcp: executable only, never a shell string
	Args      []string  `json:"args,omitempty"`      // mcp: one process argument per item
	Transport string    `json:"transport,omitempty"` // mcp: currently stdio
	MCPTools  []MCPTool `json:"mcpTools,omitempty"`  // safe, bounded discovery summary
}

type MCPTool struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	ReadOnly    bool   `json:"readOnly"`
}

type sourcesRegistry struct {
	Version int      `json:"version"`
	Sources []Source `json:"sources"`
}

type sourcesSecrets struct {
	Secrets map[string]struct {
		Password string `json:"password,omitempty"`
		Key      string `json:"key,omitempty"`
	} `json:"secrets"`
}

func (a *App) sourcesPath() string {
	if a.cacheContainer != "" {
		return filepath.Join(a.cacheContainer, sourcesFileName)
	}
	return filepath.Join(a.workPath, ".cache", sourcesFileName)
}
func (a *App) sourcesSecretsPath() string { return SourcesSecretsPath(a.dataPath) }

func (a *App) loadSources() error {
	a.sourceRegistry = sourcesRegistry{Version: 1, Sources: []Source{}}
	a.sourceSecrets = sourcesSecrets{Secrets: map[string]struct {
		Password string `json:"password,omitempty"`
		Key      string `json:"key,omitempty"`
	}{}}
	b, err := os.ReadFile(a.sourcesPath())
	if errors.Is(err, os.ErrNotExist) {
		// 全新部署：注册表为空，先补内置来源（#53）
		a.ensureBuiltinSources()
		return nil
	}
	if err != nil {
		return err
	}
	if err := json.Unmarshal(b, &a.sourceRegistry); err != nil {
		return fmt.Errorf("解析 %s: %w", sourcesFileName, err)
	}
	a.sourceSecrets = sourcesSecrets{Secrets: map[string]struct {
		Password string `json:"password,omitempty"`
		Key      string `json:"key,omitempty"`
	}{}}
	if b, err := os.ReadFile(a.sourcesSecretsPath()); err == nil {
		_ = json.Unmarshal(b, &a.sourceSecrets)
	}
	if a.sourceSecrets.Secrets == nil {
		a.sourceSecrets.Secrets = map[string]struct {
			Password string `json:"password,omitempty"`
			Key      string `json:"key,omitempty"`
		}{}
	}
	// 内置来源常驻兜底：/context（只读参考根）+ 自动系统文档（Docs.Path 读写挂载）
	a.ensureBuiltinSources()
	return nil
}

// ensureBuiltinSources 保证两个内置来源常驻且语义正确（#53）：
//   - contextSource：只读、内置、路径恒空 → localSourceRoot 回落 a.reference（真实 /context）。
//     任何工作区切换/Docs.Path 改动都不改变它，用户不可删除。
//   - systemDocsSource：读写、内置、路径跟随 wsConfig.Docs.Path（空路径亦回落 /context）。
//
// 新增/缺失时补回；已存在时就地校正为内置标记。调用方负责并发（loadSources 启动单线程；
// updateSources 已持 a.mu）。
func (a *App) ensureBuiltinSources() {
	hasCtx, hasSys := false, false
	for i := range a.sourceRegistry.Sources {
		switch a.sourceRegistry.Sources[i].ID {
		case contextSource:
			hasCtx = true
			s := &a.sourceRegistry.Sources[i]
			s.Name = "引用"
			s.Type = "local"
			s.Enabled = true
			s.RW = false // /context 只读参考
			s.Builtin = true
			s.Config.Path = "" // 恒指向 /context，不被 Docs.Path 覆盖
		case systemDocsSource:
			hasSys = true
			s := &a.sourceRegistry.Sources[i]
			s.Builtin = true
			s.RW = true
			if a.wsConfig.Docs.Location == "workspace" && a.wsConfig.Workspace.Mode == "ssh" {
				s.Type = "workspace-sftp"
			} else {
				s.Type = "local"
			}
			s.Config.Path = a.wsConfig.Docs.Path // Docs.Path 叠加为读写来源
		}
	}
	if !hasSys {
		entry := Source{ID: systemDocsSource, Name: "自动系统文档", Type: "local", Enabled: true, RW: true, Builtin: true}
		if a.wsConfig.Docs.Location == "workspace" && a.wsConfig.Workspace.Mode == "ssh" {
			entry.Type = "workspace-sftp"
		}
		entry.Config.Path = a.wsConfig.Docs.Path
		a.sourceRegistry.Sources = append(a.sourceRegistry.Sources, entry)
	}
	if !hasCtx {
		entry := Source{ID: contextSource, Name: "引用", Type: "local", Enabled: true, RW: false, Builtin: true}
		// 路径恒空 → 回落 a.reference（/context）；置顶
		a.sourceRegistry.Sources = append([]Source{entry}, a.sourceRegistry.Sources...)
	}
}

// localSourceRoot 本地类来源根：空路径回落参考根（/context）。
func (a *App) localSourceRoot(src Source) (*os.Root, error) {
	if strings.TrimSpace(src.Config.Path) == "" {
		return os.OpenRoot(a.reference.Name())
	}
	cp, _, err := a.resolveHostPath(src.Config.Path)
	if err != nil {
		return nil, err
	}
	return os.OpenRoot(cp)
}
func (a *App) saveSources() error {
	if err := os.MkdirAll(filepath.Dir(a.sourcesPath()), 0755); err != nil {
		return err
	}
	return atomicJSON(a.sourcesPath(), a.sourceRegistry)
}
func (a *App) saveSourcesSecrets() error { return atomicJSON(a.sourcesSecretsPath(), a.sourceSecrets) }

func (a *App) findSource(id string) (Source, bool) {
	for _, s := range a.sourceRegistry.Sources {
		if s.ID == id {
			return s, true
		}
	}
	return Source{}, false
}

// upsertSystemDocs 把自动系统文档挂载为内置来源（读写标记；不可删除）——工作空间配置保存后调用。
func (a *App) upsertSystemDocs() error {
	docs := a.wsConfig.Docs.Path
	entry := Source{ID: systemDocsSource, Name: "自动系统文档", Type: "local", Enabled: true, RW: true, Builtin: true}
	entry.Config.Path = docs
	if a.wsConfig.Docs.Location == "workspace" && a.wsConfig.Workspace.Mode == "ssh" {
		entry.Type = "workspace-sftp"
	}
	for i := range a.sourceRegistry.Sources {
		if a.sourceRegistry.Sources[i].ID == systemDocsSource {
			a.sourceRegistry.Sources[i].Type = entry.Type
			a.sourceRegistry.Sources[i].Config.Path = docs
			a.sourceRegistry.Sources[i].Enabled = true
			return a.saveSources()
		}
	}
	a.sourceRegistry.Sources = append(a.sourceRegistry.Sources, entry)
	return a.saveSources()
}

// ── 来源访问驱动 ──

func (a *App) listSourceDir(src Source, p string) ([]map[string]any, error) {
	switch src.Type {
	case "local", "skill":
		root, err := a.localSourceRoot(src)
		if err != nil {
			return nil, err
		}
		defer root.Close()
		return a.listLocalDir(root, p)
	case "sftp":
		return a.sftpListSource(src, p)
	case "workspace-sftp":
		return a.sftpListWorkspaceSource(src.Config.Path, p)
	case "link", "ftp", "ftps", "smb":
		return a.curlListSource(src, p)
	case "mcp":
		return mcpVirtualFiles(src, p)
	}
	return nil, errors.New("未知来源类型")
}

func (a *App) readSourceText(src Source, p string) ([]byte, error) {
	switch src.Type {
	case "local", "skill":
		root, err := a.localSourceRoot(src)
		if err != nil {
			return nil, err
		}
		defer root.Close()
		return readText(root, p)
	case "sftp":
		// R03：路径校验先于 SFTP 传输；内容策略统一
		if err := safePath(p); err != nil {
			return nil, err
		}
		b, err := a.sftpReadSource(src, p)
		if err != nil {
			return nil, err
		}
		if err := validateTextContent(b); err != nil {
			return nil, err
		}
		return b, nil
	case "workspace-sftp":
		if err := safePath(p); err != nil {
			return nil, err
		}
		b, err := a.sftpRead(a.workspaceRemotePath(pathJoinRemote(src.Config.Path, p)))
		if err != nil {
			return nil, err
		}
		if err := validateTextContent(b); err != nil {
			return nil, err
		}
		return b, nil
	case "link", "ftp", "ftps", "smb":
		return a.curlReadSource(src, p)
	case "mcp":
		return mcpVirtualFile(src, p)
	}
	return nil, errors.New("未知来源类型")
}

// readSourceRaw 返回来源文件的原始字节（不做文本校验），供 /api/file/raw 查看器使用。
func (a *App) readSourceRaw(src Source, p string) ([]byte, error) {
	switch src.Type {
	case "local", "skill":
		root, err := a.localSourceRoot(src)
		if err != nil {
			return nil, err
		}
		defer root.Close()
		return readRawBytes(root, p)
	case "sftp":
		if err := safePath(p); err != nil {
			return nil, err
		}
		return a.sftpReadSource(src, p)
	case "workspace-sftp":
		if err := safePath(p); err != nil {
			return nil, err
		}
		return a.sftpRead(a.workspaceRemotePath(pathJoinRemote(src.Config.Path, p)))
	case "link", "ftp", "ftps", "smb":
		return a.curlReadSource(src, p)
	case "mcp":
		return nil, errors.New("MCP 引用工具没有原始文件内容")
	}
	return nil, errors.New("未知来源类型")
}

func (a *App) writeSourceText(src Source, p string, b []byte) error {
	if !src.RW {
		return errors.New("该来源为只读")
	}
	switch src.Type {
	case "local", "skill":
		root, err := a.localSourceRoot(src)
		if err != nil {
			return err
		}
		defer root.Close()
		return putText(root, p, b)
	case "sftp":
		return a.sftpWriteSource(src, p, b)
	case "workspace-sftp":
		if err := safePath(p); err != nil {
			return err
		}
		return a.sftpWrite(a.workspaceRemotePath(pathJoinRemote(src.Config.Path, p)), b)
	}
	return errors.New("该类型来源不支持写入")
}

// curl 通道（ftp/ftps/smb/link）：只读；列目录用 curl 目录页解析，读文件直接取文本。
func (a *App) curlArgs(src Source, p string) []string {
	target := src.Config.URL
	if p != "" && p != "." && src.Type != "link" && src.Type != "smb" {
		u, err := url.Parse(target)
		if err == nil {
			u.Path = strings.TrimRight(u.Path, "/") + "/" + strings.TrimPrefix(p, "/")
			u.RawPath = ""
			target = u.String()
		}
	}
	args := []string{"-sS", "--fail", "--max-time", "30", "-L", "--proto", "=http,https,ftp,ftps,smb,smbs", "--proto-redir", "=http,https,ftp,ftps,smb,smbs"}
	if src.Type == "ftps" {
		args = append(args, "--ssl-reqd")
	}
	if src.Config.Username != "" {
		a.mu.Lock()
		secret := a.sourceSecrets.Secrets[src.ID].Password
		a.mu.Unlock()
		args = append(args, "-u", src.Config.Username+":"+secret)
	}
	args = append(args, target)
	return args
}

func (a *App) curlListSource(src Source, p string) ([]map[string]any, error) {
	if err := safePath(p); err != nil {
		return nil, err
	}
	if src.Type == "link" || src.Type == "smb" {
		if p != "." {
			return nil, errors.New("该来源是单个资源，不支持目录浏览")
		}
		return []map[string]any{{"name": "resource.txt", "path": "resource.txt", "dir": false}}, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), curlTimeout)
	defer cancel()
	args := a.curlArgs(src, p)
	if len(args) > 0 {
		args[len(args)-1] = strings.TrimRight(args[len(args)-1], "/") + "/"
	}
	out, err := exec.CommandContext(ctx, a.curlBin, args...).CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("获取列表失败: %s", strings.TrimSpace(string(out)))
	}
	items := []map[string]any{}
	for _, line := range strings.Split(string(out), "\n") {
		name := strings.TrimSpace(line)
		dir := strings.HasSuffix(name, "/")
		fields := strings.Fields(name)
		if len(fields) >= 9 && len(fields[0]) == 10 && (fields[0][0] == 'd' || fields[0][0] == '-') {
			dir = fields[0][0] == 'd'
			rest := name
			for n := 0; n < 8; n++ {
				rest = strings.TrimLeft(rest, " \t")
				i := strings.IndexAny(rest, " \t")
				if i < 0 {
					rest = ""
					break
				}
				rest = rest[i:]
			}
			name = strings.TrimSpace(rest)
		} else if strings.HasPrefix(name, "total ") || strings.HasPrefix(name, "l") && len(fields) >= 9 {
			continue
		}
		name = strings.TrimSuffix(name, "/")
		if name == "" || name == "." || strings.Contains(name, "/") || safePath(name) != nil {
			continue
		}
		items = append(items, map[string]any{"name": name, "path": path.Join(p, name), "dir": dir})
		if len(items) >= 2000 {
			break
		}
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i]["dir"] != items[j]["dir"] {
			return items[i]["dir"].(bool)
		}
		return items[i]["name"].(string) < items[j]["name"].(string)
	})
	return items, nil
}

func (a *App) curlReadSource(src Source, p string) ([]byte, error) {
	if err := safePath(p); err != nil {
		return nil, err
	}
	if (src.Type == "link" || src.Type == "smb") && p != "resource.txt" {
		return nil, errors.New("未知资源路径")
	}
	ctx, cancel := context.WithTimeout(context.Background(), curlTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, a.curlBin, a.curlArgs(src, p)...).Output()
	if err != nil {
		return nil, fmt.Errorf("读取失败: %v", err)
	}
	if err := validateTextContent(out); err != nil {
		return nil, err
	}

	return out, nil
}

// ── handlers ──

func (a *App) listSources(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	defer a.mu.Unlock()
	items := make([]map[string]any, 0, len(a.sourceRegistry.Sources))
	for _, s := range a.sourceRegistry.Sources {
		items = append(items, map[string]any{
			"id": s.ID, "name": s.Name, "type": s.Type, "enabled": s.Enabled, "rw": s.RW, "builtin": s.Builtin,
			"config": s.Config, "hasSecret": a.sourceSecrets.Secrets[s.ID].Password != "" || a.sourceSecrets.Secrets[s.ID].Key != "",
		})
	}
	jsonOut(w, 200, map[string]any{"sources": items})
}

func (a *App) updateSources(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Sources []Source `json:"sources"`
		Secrets map[string]struct {
			Password string `json:"password"`
			Key      string `json:"key"`
			Clear    bool   `json:"clear"`
		} `json:"secrets"`
	}
	if err := decode(w, r, &in); err != nil {
		fail(w, 400, err)
		return
	}
	if len(in.Sources) > maxSources {
		fail(w, 400, fmt.Errorf("来源最多 %d 个", maxSources))
		return
	}
	seen := map[string]bool{}
	for i := range in.Sources {
		s := &in.Sources[i]
		if s.ID == systemDocsSource && !s.Builtin {
			fail(w, 400, errors.New("自动系统文档来源由系统管理，不可修改 id"))
			return
		}
		if s.ID == contextSource && !s.Builtin {
			fail(w, 400, errors.New("内置 /context 来源由系统管理，不可移除或降级"))
			return
		}
		if !sourceIDPattern.MatchString(s.ID) {
			fail(w, 400, errors.New("来源 id 只能含字母、数字、-、_，长度 1–64"))
			return
		}
		if seen[s.ID] {
			fail(w, 400, fmt.Errorf("来源 id %s 重复", s.ID))
			return
		}
		seen[s.ID] = true
		s.Name = strings.TrimSpace(s.Name)
		if s.Name == "" {
			s.Name = s.ID
		}
		switch s.Type {
		case "local", "skill":
			if strings.TrimSpace(s.Config.Path) == "" {
				fail(w, 400, errors.New("本地来源需要路径"))
				return
			}
			if _, _, err := a.resolveHostPath(s.Config.Path); err != nil {
				fail(w, 400, err)
				return
			}
		case "sftp":
			if strings.TrimSpace(s.Config.Host) == "" {
				fail(w, 400, errors.New("SFTP 来源需要主机"))
				return
			}
			if s.Config.Port == 0 {
				s.Config.Port = 22
			}
			if s.Config.Auth == "" {
				s.Config.Auth = "none"
			}
		case "workspace-sftp":
			if s.ID != systemDocsSource || !s.Builtin {
				fail(w, 400, errors.New("仅自动系统文档可以使用工作空间 SFTP"))
				return
			}
			if a.wsConfig.Workspace.Mode != "ssh" {
				fail(w, 400, errors.New("工作空间未连接 SSH/SFTP"))
				return
			}
		case "link", "ftp", "ftps", "smb":
			{
				u, err := url.Parse(strings.TrimSpace(s.Config.URL))
				valid := err == nil && u.Host != "" && u.User == nil
				if valid {
					switch s.Type {
					case "link":
						valid = u.Scheme == "http" || u.Scheme == "https"
					case "ftp":
						valid = u.Scheme == "ftp"
					case "ftps":
						valid = u.Scheme == "ftps" || u.Scheme == "ftp"
					case "smb":
						valid = u.Scheme == "smb" || u.Scheme == "smbs"
					}
				}
				if !valid {
					fail(w, 400, errors.New("URL 协议必须与来源类型匹配；账号密码请使用独立字段"))
					return
				}
			}
			if s.RW {
				fail(w, 400, errors.New("该来源类型只支持读取，不能标记读写"))
				return
			}
			if strings.TrimSpace(s.Config.URL) == "" {
				fail(w, 400, errors.New("该类型来源需要 URL"))
				return
			}
		case "mcp":
			if err := validateMCPConfig(&s.Config); err != nil {
				fail(w, 400, err)
				return
			}
			s.RW = false
		default:
			fail(w, 400, errors.New("未知来源类型"))
			return
		}
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	for id, sec := range in.Secrets {
		entry := a.sourceSecrets.Secrets[id]
		if sec.Clear {
			entry.Password = ""
			entry.Key = ""
		}
		if sec.Password != "" {
			entry.Password = sec.Password
		}
		if sec.Key != "" {
			entry.Key = sec.Key
		}
		if entry.Password != "" || entry.Key != "" {
			a.sourceSecrets.Secrets[id] = entry
		} else {
			delete(a.sourceSecrets.Secrets, id)
		}
	}
	if err := a.saveSourcesSecrets(); err != nil {
		fail(w, 500, err)
		return
	}
	// 来源被删除，或 SFTP 连接参数（主机/端口/用户/认证方式）变化时，关闭旧
	// ControlMaster 并清理其临时凭据文件；否则 -O check 会命中指向旧主机的残留 socket，
	// 后续 sftpBatchSource 仍走旧连接。下次使用时由 ensureSourceSession 以新参数重建。
	oldSFTP := map[string]Source{}
	for _, old := range a.sourceRegistry.Sources {
		if old.Type == "sftp" {
			oldSFTP[old.ID] = old
		}
	}
	newByID := make(map[string]Source, len(in.Sources))
	for _, fresh := range in.Sources {
		newByID[fresh.ID] = fresh
	}
	for id, old := range oldSFTP {
		fresh, exists := newByID[id]
		if !exists || fresh.Type != "sftp" || sftpConnIdentity(old) != sftpConnIdentity(fresh) {
			a.killSourceSession(id, a.sftpTargetOf(old))
		}
	}
	a.sourceRegistry.Sources = in.Sources
	// 内置来源常驻：/context（只读）+ 自动系统文档（Docs.Path 读写）；被客户端漏提交时补回
	a.ensureBuiltinSources()
	if err := a.saveSources(); err != nil {
		fail(w, 500, err)
		return
	}
	items := make([]map[string]any, 0, len(a.sourceRegistry.Sources))
	for _, src := range a.sourceRegistry.Sources {
		items = append(items, map[string]any{
			"id": src.ID, "name": src.Name, "type": src.Type, "enabled": src.Enabled, "rw": src.RW, "builtin": src.Builtin,
			"config": src.Config, "hasSecret": a.sourceSecrets.Secrets[src.ID].Password != "" || a.sourceSecrets.Secrets[src.ID].Key != "",
		})
	}
	jsonOut(w, 200, map[string]any{"sources": items})
}
