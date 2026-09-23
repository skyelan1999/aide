package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

const (
	wsConfigFile  = "workspace-config.json"
	wsSecretsFile = "workspace-secrets.json"
	wsRecentMax   = 3
)

type WorkspaceConfig struct {
	Version   int `json:"version"`
	Workspace struct {
		Mode     string `json:"mode"` // local | ssh
		Path     string `json:"path"` // local: /workspace 下相对路径；remote: 远程目录（默认 home）
		Host     string `json:"host"`
		Port     int    `json:"port"`
		Username string `json:"username"`
		Auth     string `json:"auth"` // password | key | none
	} `json:"workspace"`
	Docs struct {
		Path string `json:"path"` // /context 下相对路径
	} `json:"docs"`
	Cache struct {
		Path string `json:"path"` // /workspace 下相对路径（自动创建）
	} `json:"cache"`
	Recent struct {
		Workspace []string `json:"workspace"`
		Docs      []string `json:"docs"`
		Cache     []string `json:"cache"`
	} `json:"recent"`
}

type workspaceSecrets struct {
	Password string `json:"password,omitempty"`
	Key      string `json:"key,omitempty"`
}

func defaultWorkspaceConfig() WorkspaceConfig {
	c := WorkspaceConfig{Version: 1}
	c.Workspace.Mode = "local"
	c.Workspace.Port = 22
	c.Workspace.Auth = "password"
	c.Recent.Workspace = []string{}
	c.Recent.Docs = []string{}
	c.Recent.Cache = []string{}
	return c
}

func pushRecent(list []string, value string) []string {
	out := []string{}
	for _, v := range list {
		if v != value {
			out = append(out, v)
		}
	}
	out = append([]string{value}, out...)
	if len(out) > wsRecentMax {
		out = out[:wsRecentMax]
	}
	return out
}

func (a *App) loadWorkspaceConfig() error {
	a.wsConfigPath = filepath.Join(a.workPath, wsConfigFile)
	a.wsSecretsPath = filepath.Join(a.dataPath, wsSecretsFile)
	a.wsConfig = defaultWorkspaceConfig()
	if b, err := os.ReadFile(a.wsConfigPath); err == nil {
		if err := json.Unmarshal(b, &a.wsConfig); err != nil {
			return fmt.Errorf("解析 %s: %w", wsConfigFile, err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	a.wsSecrets = workspaceSecrets{}
	if b, err := os.ReadFile(a.wsSecretsPath); err == nil {
		if err := json.Unmarshal(b, &a.wsSecrets); err != nil {
			return fmt.Errorf("解析 %s: %w", wsSecretsFile, err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return a.applyWorkspaceConfig()
}

func (a *App) saveWorkspaceConfig() error {
	return atomicJSON(a.wsConfigPath, a.wsConfig)
}
func (a *App) saveWorkspaceSecrets() error {
	return atomicJSON(a.wsSecretsPath, a.wsSecrets)
}

// resolveHostPath 把用户填写的路径翻译为容器路径（FR-79 核心修复）：
// 宿主机绝对路径 → /local 挂载下；容器绝对路径（/workspace|/context|/local）与相对路径（相对 /workspace）向后兼容。
// 返回容器路径与「展示路径」（宿主机视角，用于 AI 提示与界面）。
func (a *App) resolveHostPath(p string) (string, string, error) {
	p = strings.TrimSpace(p)
	if p == "" {
		return "", "", nil
	}
	if strings.HasPrefix(p, "~/") {
		p = strings.TrimRight(a.hostLocal, "/") + strings.TrimPrefix(p, "~")
	}
	// R03：容器虚拟路径必须带边界匹配（/workspaceXYZ 不算 /workspace），且 Join 后必须仍在根内
	joinWithin := func(base, prefix string) (string, bool) {
		if p != prefix && !strings.HasPrefix(p, prefix+"/") {
			return "", false
		}
		joined := filepath.Join(base, filepath.FromSlash(strings.TrimPrefix(p, prefix)))
		rel, err := filepath.Rel(base, joined)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return "", false
		}
		return joined, true
	}
	if strings.HasPrefix(p, "/workspace") {
		if joined, ok := joinWithin(a.workPath, "/workspace"); ok {
			return joined, p, nil
		}
		return "", "", fmt.Errorf("路径 %s 越出工作区根", p)
	}
	if strings.HasPrefix(p, "/context") {
		if joined, ok := joinWithin(a.reference.Name(), "/context"); ok {
			return joined, p, nil
		}
		return "", "", fmt.Errorf("路径 %s 越出参考根", p)
	}
	if strings.HasPrefix(p, "/local") {
		if joined, ok := joinWithin(a.localRoot.Name(), "/local"); ok {
			return joined, p, nil
		}
		return "", "", fmt.Errorf("路径 %s 越出本地根", p)
	}
	if !strings.HasPrefix(p, "/") {
		joined := filepath.Join(a.workPath, filepath.FromSlash(p))
		rel, err := filepath.Rel(a.workPath, joined)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return "", "", fmt.Errorf("相对路径 %s 越出工作区", p)
		}
		return joined, p, nil
	}
	rel, err := filepath.Rel(a.hostLocal, p)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", "", fmt.Errorf("路径 %s 不在可访问范围内（本机目录根：%s）", p, a.hostLocal)
	}
	return filepath.Join("/local", rel), p, nil
}

// defaultWorkspaceID 是「从未定制工作区」时的身份（local、空路径、空主机）。
const defaultWorkspaceID = "local||"

// wsID 返回当前工作区身份（模式+宿主机路径/主机+远程路径），供提案绑定（R02）。
func (a *App) wsID() string {
	w := a.wsConfig.Workspace
	return w.Mode + "|" + w.Path + "|" + w.Host
}

// applyWorkspaceConfig 按配置切换工作空间/文档/缓存根，并清理旧 SSH 会话（FR-79 / FR-80）。
// 每次成功切换递增 wsRevision：旧提案/命令的身份校验依赖该版本（R02）。
func (a *App) applyWorkspaceConfig() error {
	a.wsRevision++
	display := "/workspace"
	if a.wsConfig.Workspace.Mode == "ssh" {
		target := a.wsConfig.Workspace.Host
		if a.wsConfig.Workspace.Username != "" {
			target = a.wsConfig.Workspace.Username + "@" + target
		}
		display = target + ":" + a.wsConfig.Workspace.Path
	} else {
		cp, disp, err := a.resolveHostPath(a.wsConfig.Workspace.Path)
		if err != nil {
			return err
		}
		if disp != "" {
			display = disp
		}
		if cp == "" {
			cp = a.workPath // 空路径恢复默认根（R02）
		}
		w, err := os.OpenRoot(cp)
		if err != nil {
			return fmt.Errorf("工作空间路径不可用: %w", err)
		}
		old := a.workspace
		a.workspace = w
		if old != nil {
			// 运行中的任务可能仍持有旧句柄快照：延后到 Close 统一释放（R02 运行中切换）
			a.retiredRoots = append(a.retiredRoots, old)
		}
	}
	a.workspaceDisplay = display
	// 系统文档参考根
	if dp, disp, err := a.resolveHostPath(a.wsConfig.Docs.Path); err == nil && dp != "" {
		if r, err := os.OpenRoot(dp); err == nil {
			oldRef := a.reference
			a.reference = r
			if oldRef != nil {
				oldRef.Close()
			}
			_ = disp
		}
	}
	// 缓存目录（自动创建；来源注册表存于此）
	cacheContainer := filepath.Join(a.workPath, ".cache")
	if cp, _, err := a.resolveHostPath(a.wsConfig.Cache.Path); err == nil && cp != "" {
		cacheContainer = cp
	}
	_ = os.MkdirAll(cacheContainer, 0755)
	a.cacheContainer = cacheContainer
	a.killSSHSession()
	return nil
}

func (a *App) workspaceConfigOut() map[string]any {
	return map[string]any{
		"workspace": map[string]any{
			"mode": a.wsConfig.Workspace.Mode, "path": a.wsConfig.Workspace.Path,
			"host": a.wsConfig.Workspace.Host, "port": a.wsConfig.Workspace.Port,
			"username": a.wsConfig.Workspace.Username, "auth": a.wsConfig.Workspace.Auth,
		},
		"docs":       map[string]any{"path": a.wsConfig.Docs.Path},
		"cache":      map[string]any{"path": a.wsConfig.Cache.Path},
		"recent":     a.wsConfig.Recent,
		"hasPassword": a.wsSecrets.Password != "",
		"hasKey":     a.wsSecrets.Key != "",
	}
}

func (a *App) getWorkspaceConfig(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	defer a.mu.Unlock()
	jsonOut(w, 200, a.workspaceConfigOut())
}

func (a *App) updateWorkspaceConfig(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Workspace struct {
			Mode     string `json:"mode"`
			Path     string `json:"path"`
			Host     string `json:"host"`
			Port     int    `json:"port"`
			Username string `json:"username"`
			Auth     string `json:"auth"`
		} `json:"workspace"`
		Docs  struct{ Path string } `json:"docs"`
		Cache struct{ Path string } `json:"cache"`
		Password    string `json:"password"`
		Key         string `json:"key"`
		ClearPassword bool `json:"clearPassword"`
		ClearKey     bool  `json:"clearKey"`
	}
	if err := decode(w, r, &in); err != nil {
		fail(w, 400, err)
		return
	}
	if in.Workspace.Mode == "" {
		in.Workspace.Mode = a.wsConfig.Workspace.Mode
	}
	if in.Workspace.Mode != "local" && in.Workspace.Mode != "ssh" {
		fail(w, 400, errors.New("连接方式只支持 local 或 ssh"))
		return
	}
	if in.Workspace.Port == 0 {
		in.Workspace.Port = 22
	}
	if in.Workspace.Port < 1 || in.Workspace.Port > 65535 {
		fail(w, 400, errors.New("端口须在 1–65535"))
		return
	}
	if in.Workspace.Auth == "" {
		in.Workspace.Auth = a.wsConfig.Workspace.Auth
	}
	if in.Workspace.Auth != "password" && in.Workspace.Auth != "key" && in.Workspace.Auth != "none" {
		fail(w, 400, errors.New("认证方式只支持 password / key / none"))
		return
	}
	// 路径安全由 resolveHostPath 统一约束：
	// 宿主机绝对路径仅允许落在 AIDE_LOCAL_ROOT（默认 $HOME）之下；
	// 容器路径仅允许 /workspace | /context | /local 前缀；相对路径落在 /workspace 下。
	if in.Workspace.Mode != "ssh" && strings.TrimSpace(in.Workspace.Path) != "" {
		if _, _, err := a.resolveHostPath(in.Workspace.Path); err != nil {
			fail(w, 400, err)
			return
		}
	}
	for _, p := range []string{in.Docs.Path, in.Cache.Path} {
		if strings.TrimSpace(p) != "" {
			if _, _, err := a.resolveHostPath(p); err != nil {
				fail(w, 400, err)
				return
			}
		}
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	cfg := a.wsConfig
	cfg.Workspace.Mode = in.Workspace.Mode
	cfg.Workspace.Path = strings.TrimSpace(in.Workspace.Path)
	cfg.Workspace.Host = strings.TrimSpace(in.Workspace.Host)
	cfg.Workspace.Port = in.Workspace.Port
	cfg.Workspace.Username = strings.TrimSpace(in.Workspace.Username)
	cfg.Workspace.Auth = in.Workspace.Auth
	cfg.Docs.Path = strings.TrimSpace(in.Docs.Path)
	cfg.Cache.Path = strings.TrimSpace(in.Cache.Path)
	// 秘钥更新（存 /data 卷，0600）
	if in.ClearPassword {
		a.wsSecrets.Password = ""
	}
	if in.Password != "" {
		a.wsSecrets.Password = in.Password
	}
	if in.ClearKey {
		a.wsSecrets.Key = ""
	}
	if in.Key != "" {
		a.wsSecrets.Key = in.Key
	}
	if err := a.saveWorkspaceSecrets(); err != nil {
		fail(w, 500, err)
		return
	}
	a.wsConfig = cfg
	if err := a.applyWorkspaceConfig(); err != nil {
		fail(w, 400, err)
		return
	}
	// 最近路径：只记录「生效路径」非空的
	if cfg.Workspace.Path != "" {
		a.wsConfig.Recent.Workspace = pushRecent(a.wsConfig.Recent.Workspace, cfg.Workspace.Path)
	}
	if cfg.Docs.Path != "" {
		a.wsConfig.Recent.Docs = pushRecent(a.wsConfig.Recent.Docs, cfg.Docs.Path)
	}
	if cfg.Cache.Path != "" {
		a.wsConfig.Recent.Cache = pushRecent(a.wsConfig.Recent.Cache, cfg.Cache.Path)
	}
	if err := a.saveWorkspaceConfig(); err != nil {
		fail(w, 500, err)
		return
	}
	_ = a.upsertSystemDocs() // 自动系统文档挂载为读写来源（FR-82）
	jsonOut(w, 200, a.workspaceConfigOut())
}

// ── 本地/远程分发的文件操作（FR-78 / FR-79） ──

func (a *App) workspaceMode() string { return a.wsConfig.Workspace.Mode }
func (a *App) workspaceRemotePath(p string) string {
	if p == "." {
		return a.wsConfig.Workspace.Path
	}
	return pathJoinRemote(a.wsConfig.Workspace.Path, p)
}
func pathJoinRemote(base, p string) string {
	if strings.TrimSpace(base) == "" {
		base = "."
	}
	return strings.TrimRight(base, "/") + "/" + strings.TrimLeft(p, "/")
}

func (a *App) listWorkspaceDir(p string) ([]map[string]any, error) {
	if a.workspaceMode() == "ssh" {
		return a.sftpList(a.workspaceRemotePath(p))
	}
	return a.listLocalDir(a.workspace, p)
}
func (a *App) readWorkspaceText(p string) ([]byte, error) {
	if a.workspaceMode() == "ssh" {
		return a.sftpRead(a.workspaceRemotePath(p))
	}
	return readText(a.workspace, p)
}
func (a *App) writeWorkspaceText(p string, b []byte) error {
	if a.workspaceMode() == "ssh" {
		return a.sftpWrite(a.workspaceRemotePath(p), b)
	}
	return putText(a.workspace, p, b)
}
func (a *App) workspaceStatExists(p string) bool {
	a.mu.Lock()
	mode := a.workspaceMode()
	root := a.workspace
	remoteBase := a.wsConfig.Workspace.Path
	a.mu.Unlock()
	if mode == "ssh" {
		return a.sftpExists(pathJoinRemote(remoteBase, p))
	}
	_, err := root.Stat(p)
	return err == nil
}
