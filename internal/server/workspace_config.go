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
		// 私钥存储模式（#38）：
		//   "" / "paste" = 粘贴私钥，密文存 vault（ws:ssh-key）
		//   "ref"        = 仅引用宿主机/容器内路径，运行时直接读取，私钥不入库
		//   "copy"       = 导入副本：把文件内容加密存入 vault
		KeyMode       string `json:"keyMode,omitempty"`
		KeyRefPath    string `json:"keyRefPath,omitempty"`    // ref/copy 时的容器路径（/workspace|/context|/local 内）
		KeyFingerprint string `json:"keyFingerprint,omitempty"` // 公钥 SHA256 指纹（元数据，界面展示，不回显私钥）
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
	a.wsSecretsPath = WorkspaceSecretsPath(a.dataPath)
	a.wsConfig = defaultWorkspaceConfig()
	if b, err := os.ReadFile(a.wsConfigPath); err == nil {
		if err := json.Unmarshal(b, &a.wsConfig); err != nil {
			return fmt.Errorf("解析 %s: %w", wsConfigFile, err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	// 旧版明文凭证（/data/workspace-secrets.json）仅作只读迁移源加载。
	// 新凭证一律进加密 vault；解锁后 migrateLegacyWorkspaceSecrets 会把旧明文迁入并归档。
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

// migrateLegacyWorkspaceSecrets 在 vault 解锁后调用：把旧明文凭证迁入加密 vault，
// 成功后把旧明文文件改名为 *.migrated（0600），避免再次被当作明文读取。
// 幂等：vault 已有对应条目则跳过；旧文件缺失也视为完成。
func (a *App) migrateLegacyWorkspaceSecrets() {
	if a.vault == nil || !a.vault.Unlocked() {
		return
	}
	migrated := false
	if pw := strings.TrimSpace(a.wsSecrets.Password); pw != "" && !a.vault.Has(VaultIDWSPassword) {
		if err := a.vault.Put(VaultIDWSPassword, VaultTypeSSHPassword, "SSH 登录密码", []byte(pw), ""); err == nil {
			migrated = true
		}
	}
	if key := strings.TrimSpace(a.wsSecrets.Key); key != "" && !a.vault.Has(VaultIDWSKey) {
		fp, _ := sshKeyFingerprint([]byte(key), "")
		if err := a.vault.Put(VaultIDWSKey, VaultTypeSSHKey, "SSH 私钥", []byte(key), fp); err == nil {
			migrated = true
		}
	}
	if migrated {
		_ = a.vault.Save()
		// 归档旧明文文件（不删除，保留可追溯；改名后不再被加载）
		if st, err := os.Stat(a.wsSecretsPath); err == nil && !st.IsDir() {
			_ = os.Rename(a.wsSecretsPath, a.wsSecretsPath+".migrated")
		}
		// 内存中清掉明文，避免长期驻留
		a.wsSecrets.Password = ""
		a.wsSecrets.Key = ""
	}
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
	// R02：登记当前工作区身份 → 根，运行中任务的工具据此解析原工作区根
	a.wsRoots[a.wsID()] = a.workspace
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
	out := map[string]any{
		"workspace": map[string]any{
			"mode": a.wsConfig.Workspace.Mode, "path": a.wsConfig.Workspace.Path,
			"host": a.wsConfig.Workspace.Host, "port": a.wsConfig.Workspace.Port,
			"username": a.wsConfig.Workspace.Username, "auth": a.wsConfig.Workspace.Auth,
			"keyMode":        a.wsConfig.Workspace.KeyMode,
			"keyRefPath":     a.wsConfig.Workspace.KeyRefPath,
			"keyFingerprint": a.wsConfig.Workspace.KeyFingerprint,
		},
		"docs":        map[string]any{"path": a.wsConfig.Docs.Path},
		"cache":       map[string]any{"path": a.wsConfig.Cache.Path},
		"recent":      a.wsConfig.Recent,
	}
	// vault 元数据（绝不返回明文/密文）
	hasPassword, hasKey := false, false
	fp := a.wsConfig.Workspace.KeyFingerprint
	vaultLocked := a.vault == nil || !a.vault.Unlocked()
	if a.vault != nil {
		hasPassword = a.vault.Has(VaultIDWSPassword)
		hasKey = a.vault.Has(VaultIDWSKey) || a.wsConfig.Workspace.KeyMode == "ref"
	}
	out["hasPassword"] = hasPassword
	out["hasKey"] = hasKey
	out["keyFingerprint"] = fp
	out["vaultLocked"] = vaultLocked
	out["hasAccountPassword"] = a.settings.UserPasswordHash != ""
	return out
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
		Docs            struct{ Path string } `json:"docs"`
		Cache           struct{ Path string } `json:"cache"`
		Password        string                `json:"password"`         // SSH 登录密码
		Key             string                `json:"key"`              // 粘贴的私钥正文
		Passphrase      string                `json:"passphrase"`       // 私钥口令
		KeyMode         string                `json:"keyMode"`          // paste | ref | copy
		KeyRefPath      string                `json:"keyRefPath"`       // ref/copy 的容器路径（/workspace|/context|/local 内）
		VaultPassword   string                `json:"vaultPassword"`    // 账户密码（vault 锁定时解锁用）
		ClearPassword   bool                  `json:"clearPassword"`
		ClearKey        bool                  `json:"clearKey"`
		ClearPassphrase bool                  `json:"clearPassphrase"`
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
	// R03 原子性预检：docs/cache 若已存在必须是目录；workspace 路径必须能实际打开。
	// 失败时配置、根、recent 均不变。
	if strings.TrimSpace(in.Docs.Path) != "" {
		dp, _, err := a.resolveHostPath(in.Docs.Path)
		if err != nil {
			fail(w, 400, err)
			return
		}
		if info, err := os.Stat(dp); err == nil && !info.IsDir() {
			fail(w, 400, errors.New("系统文档路径不是目录"))
			return
		}
	}
	if strings.TrimSpace(in.Cache.Path) != "" {
		cp, _, err := a.resolveHostPath(in.Cache.Path)
		if err != nil {
			fail(w, 400, err)
			return
		}
		if info, err := os.Stat(cp); err == nil && !info.IsDir() {
			fail(w, 400, errors.New("缓存路径不是目录"))
			return
		}
	}
	if in.Workspace.Mode != "ssh" && strings.TrimSpace(in.Workspace.Path) != "" {
		cp, _, err := a.resolveHostPath(in.Workspace.Path)
		if err != nil {
			fail(w, 400, err)
			return
		}
		probe, err := os.OpenRoot(cp)
		if err != nil {
			fail(w, 400, fmt.Errorf("工作空间路径不可用: %w", err))
			return
		}
		probe.Close()
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

	// ── 凭证处理（#38：统一加密 vault，拒绝明文落盘）──
	// needUnlock = 写入新凭证（密码/私钥/口令/选择文件）；清除操作无需解锁。
	needUnlock := in.Password != "" || in.Key != "" || in.Passphrase != "" ||
		strings.TrimSpace(in.KeyMode) == "ref" || strings.TrimSpace(in.KeyMode) == "copy"
	if needUnlock {
		if a.settings.UserPasswordHash == "" {
			fail(w, 400, errors.New("请先在「设置→账户」设置登录密码以加密存储 SSH 凭据"))
			return
		}
		if a.vault == nil {
			fail(w, 500, errors.New("凭证保险库未初始化"))
			return
		}
		if !a.vault.Unlocked() {
			switch {
			case in.VaultPassword != "":
				if ok, _ := VerifyPassword(in.VaultPassword, a.settings.UserPasswordHash); !ok {
					fail(w, 401, errors.New("账户密码错误，无法解锁凭证保险库"))
					return
				}
				a.unlockVault(in.VaultPassword)
			case a.personaKey != "":
				a.unlockVault(a.personaKey)
			default:
				fail(w, 401, errors.New("凭证保险库已锁定，请输入账户密码解锁后再保存"))
				return
			}
		}
	}
	if a.vault != nil {
		// 登录密码
		if in.ClearPassword {
			a.vault.Delete(VaultIDWSPassword)
		}
		if in.Password != "" {
			if err := a.vault.Put(VaultIDWSPassword, VaultTypeSSHPassword, "SSH 登录密码", []byte(in.Password), ""); err != nil {
				fail(w, 500, err)
				return
			}
		}
		// 私钥：粘贴 / 引用路径 / 导入副本 三选一；未提供则保留现有。
		newKeyMode := strings.TrimSpace(in.KeyMode)
		newKeyRefPath := ""
		newFingerprint := cfg.Workspace.KeyFingerprint
		switch {
		case in.Key != "":
			fp, err := sshKeyFingerprint([]byte(in.Key), in.Passphrase)
			if err != nil {
				fail(w, 400, err)
				return
			}
			if err := a.vault.Put(VaultIDWSKey, VaultTypeSSHKey, "SSH 私钥", []byte(in.Key), fp); err != nil {
				fail(w, 500, err)
				return
			}
			newKeyMode, newKeyRefPath, newFingerprint = "paste", "", fp
		case newKeyMode == "ref" && strings.TrimSpace(in.KeyRefPath) != "":
			rp := strings.TrimSpace(in.KeyRefPath)
			realPath, _, err := a.resolveHostPath(rp)
			if err != nil {
				fail(w, 400, err)
				return
			}
			if _, err := os.Stat(realPath); err != nil {
				fail(w, 400, fmt.Errorf("私钥文件不可访问: %w", err))
				return
			}
			fp, err := sshKeygenFingerprintFile(realPath, in.Passphrase)
			if err != nil {
				fail(w, 400, err)
				return
			}
			a.vault.Delete(VaultIDWSKey) // 引用模式不存副本，运行时直接读路径
			newKeyRefPath, newFingerprint = rp, fp
		case newKeyMode == "copy" && strings.TrimSpace(in.KeyRefPath) != "":
			rp := strings.TrimSpace(in.KeyRefPath)
			material, err := a.readKeyFileInRoots(rp)
			if err != nil {
				fail(w, 400, err)
				return
			}
			fp, err := sshKeyFingerprint(material, in.Passphrase)
			if err != nil {
				fail(w, 400, err)
				return
			}
			if err := a.vault.Put(VaultIDWSKey, VaultTypeSSHKey, "SSH 私钥（导入副本）", material, fp); err != nil {
				fail(w, 500, err)
				return
			}
			newKeyRefPath, newFingerprint = "", fp
		}
		if in.ClearKey {
			a.vault.Delete(VaultIDWSKey)
			newKeyMode, newKeyRefPath, newFingerprint = "", "", ""
		}
		// 私钥口令
		if in.ClearPassphrase {
			a.vault.Delete(VaultIDWSPassphrase)
		}
		if in.Passphrase != "" {
			if err := a.vault.Put(VaultIDWSPassphrase, VaultTypeSSHPassphrase, "私钥口令", []byte(in.Passphrase), ""); err != nil {
				fail(w, 500, err)
				return
			}
		}
		if err := a.vault.Save(); err != nil {
			fail(w, 500, err)
			return
		}
		a.vaultAudit("workspace-secret-saved")
		cfg.Workspace.KeyMode = newKeyMode
		cfg.Workspace.KeyRefPath = newKeyRefPath
		cfg.Workspace.KeyFingerprint = newFingerprint
	}
	oldID := a.wsID()
	oldRoot := a.workspace
	a.wsConfig = cfg
	if err := a.applyWorkspaceConfig(); err != nil {
		fail(w, 400, err)
		return
	}
	// R02：旧工作区身份的根继续可用，供运行中任务的工具调用解析
	a.wsRoots[oldID] = oldRoot
	a.wsRoots[a.wsID()] = a.workspace
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
		// R03：路径校验必须先于任何 SFTP 传输；内容策略与本地读取一致
		if err := safePath(p); err != nil {
			return nil, err
		}
		b, err := a.sftpRead(a.workspaceRemotePath(p))
		if err != nil {
			return nil, err
		}
		if err := validateTextContent(b); err != nil {
			return nil, err
		}
		return b, nil
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
