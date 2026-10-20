package server

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type terminalContextKey struct{}
type terminalWorkbenchContextKey struct{}

// Only the Terminal daemon receives this per-call context, after normal tool
// admission. Neither workspace credentials nor key bytes enter browser replies.
func (a *App) terminalToolContext(ctx context.Context, tool string, args map[string]any) (map[string]any, error) {
	a.mu.Lock()
	config, revision, local := a.wsConfig.Workspace, a.wsRevision, a.pluginWorkspaceLocked()
	var work, ref string
	if a.workspace != nil {
		work = a.workspace.Name()
	}
	if a.reference != nil {
		ref = a.reference.Name()
	}
	areas := a.knowledgeAreasLocked(a.wsID(), work, ref)
	sources := append([]Source(nil), a.sourceRegistry.Sources...)
	scope := a.wsID()
	dataPath := a.dataPath
	disabledTools := append([]string(nil), a.settings.DisabledTools...)
	a.mu.Unlock()
	result := map[string]any{"version": 1, "storageRoot": local, "mode": config.Mode, "disabledTools": disabledTools, "communicationsVersion": 1, "relayVersion": 1, "workbench": ctx.Value(terminalWorkbenchContextKey{}) == true}
	// State and relay secrets must not be reachable through workspace file tools.
	stateIdentity, _ := json.Marshal([]any{scope, config.Mode, config.Port, config.Username, local})
	stateHash := sha256.Sum256(stateIdentity)
	stateBase := filepath.Join(dataPath, "terminal-private")
	stateRoot := filepath.Join(stateBase, hex.EncodeToString(stateHash[:]))
	for _, directory := range []string{stateBase, stateRoot} {
		if err := os.Mkdir(directory, 0700); err != nil && !os.IsExist(err) {
			return nil, err
		}
		info, err := os.Lstat(directory)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return nil, errors.New("终端私有状态目录无效")
		}
		if err := os.Chmod(directory, 0700); err != nil {
			return nil, err
		}
	}
	result["stateRoot"] = stateRoot
	if config.Mode == "ssh" {
		// Include port/user as well as workspace identity: different remote users
		// must never share terminal records, even when wsID's legacy label matches.
		identity, _ := json.Marshal([]any{scope, config.Port, config.Username})
		sum := sha256.Sum256(identity)
		base := filepath.Join(dataPath, "terminal-workspaces")
		dir := filepath.Join(base, hex.EncodeToString(sum[:]))
		for _, p := range []string{base, dir} {
			if err := os.Mkdir(p, 0700); err != nil && !os.IsExist(err) {
				return nil, err
			}
			st, err := os.Lstat(p)
			if err != nil || !st.IsDir() || st.Mode()&os.ModeSymlink != 0 {
				return nil, errors.New("终端状态目录无效")
			}
			if err := os.Chmod(p, 0700); err != nil {
				return nil, err
			}
		}
		result["storageRoot"] = dir
		username := config.Username
		if username == "" {
			username = "root"
		}
		result["remote"] = map[string]any{"host": config.Host, "port": config.Port, "username": username, "path": config.Path, "socket": sshControlSocket}
		kind, _ := args["kind"].(string)
		if tool == "terminal_session_create" && (kind == "" || kind == "local") {
			if err := a.ensureSSHSession(ctx); err != nil {
				return nil, errors.New("工作区 SSH 连接不可用，请检查工作区认证设置")
			}
			pinned, err := pinTerminalSSHMaster(dataPath, sshControlSocket)
			if err != nil {
				return nil, err
			}
			result["remote"].(map[string]any)["socket"] = pinned
		}
	}
	effective := args
	if tool == "terminal_session_relay_connect" {
		id, _ := args["id"].(string)
		decoded, err := hex.DecodeString(id)
		if err != nil || len(decoded) != 12 {
			return nil, errors.New("此工作区中不存在该会话")
		}
		private, err := os.OpenRoot(stateRoot)
		if err != nil {
			return nil, err
		}
		file, err := private.Open(filepath.Join(".aide", "terminal-workbench", id+".json"))
		private.Close()
		if err != nil {
			return nil, errors.New("此工作区中不存在该会话")
		}
		var record struct {
			Auth           string `json:"auth"`
			IdentityFile   string `json:"identityFile"`
			IdentitySource string `json:"identitySource"`
			Visible        *bool  `json:"visibleToAide"`
		}
		info, statErr := file.Stat()
		if statErr != nil || !info.Mode().IsRegular() || info.Size() > 32768 {
			file.Close()
			return nil, errors.New("会话记录无效")
		}
		err = json.NewDecoder(file).Decode(&record)
		file.Close()
		if err != nil || (record.Visible != nil && !*record.Visible && result["workbench"] != true) {
			return nil, errors.New("此工作区中不存在该会话")
		}
		// The configured identity, not model/browser-supplied overrides, controls
		// reference key reads for an existing relay session.
		effective = map[string]any{"auth": record.Auth, "identityFile": record.IdentityFile, "identitySource": record.IdentitySource}
	}
	if tool == "terminal_session_comm" {
		if action, ok := args["action"].(string); ok && toolDeniedIn(disabledTools, "terminal_"+action) {
			return nil, errors.New("通信工具已被管理员禁用")
		}
	}
	keyTool := tool == "terminal_session_create" || tool == "terminal_ssh_connect" || tool == "terminal_session_relay_connect"
	if tool == "terminal_session_comm" && args["action"] == "ssh_connect" {
		if nested, ok := args["arguments"].(map[string]any); ok {
			effective = nested
			keyTool = true
		}
	}
	sourceID, _ := effective["identitySource"].(string)
	keyPath, _ := effective["identityFile"].(string)
	auth, _ := effective["auth"].(string)
	if keyTool && auth != "password" && keyPath != "" {
		if sourceID != "" {
			allowed := false
			for _, source := range sources {
				if source.ID == sourceID && sourceAIVisible(source, scope) && (source.Type == "local" || source.Type == "skill" || source.Type == "sftp" || source.Type == "workspace-sftp") {
					allowed = true
				}
			}
			if !allowed {
				return nil, errors.New("引用来源已停用或不可访问")
			}
		}
		if err := safePath(keyPath); err != nil {
			return nil, errors.New("私钥路径必须位于所选来源内")
		}
		var material []byte
		for _, ar := range areas {
			if !ar.enabled || (sourceID == "" && ar.root != "workspace") || (sourceID != "" && (ar.root != "source" || ar.source != sourceID)) {
				continue
			}
			var err error
			material, err = a.knowledgeAreaRead(ctx, ar, keyPath, 64<<10)
			if err != nil {
				return nil, errors.New("私钥文件读取失败，请检查所选来源与路径")
			}
			break
		}
		if len(material) == 0 || len(material) > 64<<10 || !strings.Contains(string(material), "PRIVATE KEY-----") {
			return nil, errors.New("所选来源不可用或文件不是有效的私钥")
		}
		result["identityMaterial"] = base64.StdEncoding.EncodeToString(material)
	}
	if tool == "terminal_session_files" {
		requested, _ := args["path"].(string)
		if requested == "" {
			requested = "."
		}
		if err := safePath(requested); err != nil {
			return nil, errors.New("请选择来源内的相对目录")
		}
		available := []map[string]any{{"id": "", "name": "当前工作区"}}
		var selected *Source
		for _, source := range sources {
			if !sourceAIVisible(source, scope) || (source.Type != "local" && source.Type != "skill" && source.Type != "sftp" && source.Type != "workspace-sftp") {
				continue
			}
			available = append(available, map[string]any{"id": source.ID, "name": source.Name})
			if source.ID == sourceID {
				copy := source
				selected = &copy
			}
		}
		var entries []map[string]any
		var err error
		if sourceID == "" {
			entries, err = a.listWorkspaceDir(requested)
		} else if selected != nil {
			entries, err = a.listSourceDir(*selected, requested)
		} else {
			return nil, errors.New("引用来源已停用或不可访问")
		}
		if err != nil {
			return nil, errors.New("无法列出所选目录，请检查来源连接")
		}
		if len(entries) > 2000 {
			entries = entries[:2000]
		}
		result["fileListing"] = map[string]any{"sources": available, "source": sourceID, "path": requested, "entries": entries}
	}
	a.mu.Lock()
	stale := a.wsRevision != revision || a.wsConfig.Workspace != config
	a.mu.Unlock()
	if stale {
		return nil, errors.New("工作区已变化，请重新打开终端工作台")
	}
	return result, nil
}

// A workspace switch can replace the shared control socket between admission
// and OpenSSH attaching. A hard link pins this exact Unix socket inode: a late
// attachment either reaches the admitted master or fails, never the next host.
func pinTerminalSSHMaster(dataPath, socket string) (string, error) {
	info, err := os.Lstat(socket)
	if err != nil || info.Mode()&os.ModeSocket == 0 {
		return "", errors.New("工作区 SSH 主连接不可用")
	}
	owner := sha256.Sum256([]byte(dataPath))
	dir := filepath.Join(os.TempDir(), "aide-terminal-masters-"+hex.EncodeToString(owner[:8]))
	if err := os.Mkdir(dir, 0700); err != nil && !os.IsExist(err) {
		return "", err
	}
	st, err := os.Lstat(dir)
	if err != nil || !st.IsDir() || st.Mode()&os.ModeSymlink != 0 || st.Mode().Perm() != 0700 {
		return "", errors.New("SSH 连接固定目录无效")
	}
	identity := sha256.Sum256([]byte(fmt.Sprintf("%s:%v", socket, info.Sys())))
	pinned := filepath.Join(dir, hex.EncodeToString(identity[:16]))
	if err := os.Link(socket, pinned); err != nil && !os.IsExist(err) {
		return "", errors.New("无法固定工作区 SSH 连接")
	}
	linked, err := os.Lstat(pinned)
	if err != nil || !os.SameFile(info, linked) {
		return "", errors.New("工作区 SSH 连接已变化")
	}
	return pinned, nil
}
