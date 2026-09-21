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

// applyWorkspaceConfig 按配置切换本地根目录与参考根，并清理旧 SSH 会话（FR-79 / FR-80）。
func (a *App) applyWorkspaceConfig() error {
	w, err := os.OpenRoot(a.workPath)
	if err != nil {
		return err
	}
	if a.wsConfig.Workspace.Mode == "local" && strings.TrimSpace(a.wsConfig.Workspace.Path) != "" {
		if err := safePath(a.wsConfig.Workspace.Path); err != nil {
			w.Close()
			return fmt.Errorf("工作空间路径无效: %w", err)
		}
		if _, err := w.Stat(a.wsConfig.Workspace.Path); err != nil {
			w.Close()
			return fmt.Errorf("工作空间路径不存在: %s", a.wsConfig.Workspace.Path)
		}
		sub, err := os.OpenRoot(filepath.Join(a.workPath, filepath.FromSlash(a.wsConfig.Workspace.Path)))
		if err != nil {
			w.Close()
			return err
		}
		w.Close()
		w = sub
	}
	old := a.workspace
	a.workspace = w
	if old != nil {
		old.Close()
	}
	// 系统文档参考根
	r, err := os.OpenRoot(a.workPath)
	if err != nil {
		return err
	}
	if strings.TrimSpace(a.wsConfig.Docs.Path) != "" {
		docsBase := filepath.Join(a.workPath, "..", "context")
		root, err := os.OpenRoot(docsBase)
		if err == nil {
			if _, statErr := root.Stat(a.wsConfig.Docs.Path); statErr == nil {
				sub, subErr := os.OpenRoot(filepath.Join(docsBase, filepath.FromSlash(a.wsConfig.Docs.Path)))
				if subErr == nil {
					r.Close()
					r = sub
				} else {
					root.Close()
				}
			} else {
				root.Close()
			}
		}
	}
	oldRef := a.reference
	a.reference = r
	if oldRef != nil {
		oldRef.Close()
	}
	// 缓存目录
	if strings.TrimSpace(a.wsConfig.Cache.Path) != "" && safePath(a.wsConfig.Cache.Path) == nil {
		_ = os.MkdirAll(filepath.Join(a.workPath, filepath.FromSlash(a.wsConfig.Cache.Path)), 0755)
	}
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
	// 本地挂载路径用 safePath 校验；远程工作空间路径允许绝对路径（远端语义）
	if in.Workspace.Mode == "local" && strings.TrimSpace(in.Workspace.Path) != "" {
		if err := safePath(in.Workspace.Path); err != nil {
			fail(w, 400, err)
			return
		}
	}
	for _, p := range []string{in.Docs.Path, in.Cache.Path} {
		if strings.TrimSpace(p) != "" {
			if err := safePath(p); err != nil {
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
	if a.workspaceMode() == "ssh" {
		return a.sftpExists(a.workspaceRemotePath(p))
	}
	_, err := a.workspace.Stat(p)
	return err == nil
}
