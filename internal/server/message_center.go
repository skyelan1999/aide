package server

import (
	"aide/internal/messaging"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const VaultTypeMessageIdentity = "message-device-key"
const VaultTypeMessageStorage = "message-storage-key"

type messageCenterConfig struct {
	AssistantAccess bool                    `json:"assistantAccess"`
	Enabled         bool                    `json:"enabled"`
	Name            string                  `json:"name"`
	Network         messaging.NetworkConfig `json:"network"`
	ICEURLs         []string                `json:"iceURLs,omitempty"`
	ICEUsername     string                  `json:"iceUsername,omitempty"`
}
type messageCenter struct {
	lease        sync.RWMutex
	voiceMu      sync.Mutex
	voiceOwner   string
	voiceCall    string
	voiceExpires time.Time
	service      *messaging.Service
	network      *messaging.Network
	store        *messaging.Store
	config       messageCenterConfig
}

func messageConfigPath(data string) string { return filepath.Join(data, "config", "messages.json") }
func (a *App) messageConfig() (messageCenterConfig, error) {
	config := messageCenterConfig{Network: messaging.NetworkConfig{Listen: ":45840", LAN: true}}
	a.mu.Lock()
	name := strings.TrimSpace(a.settings.UserName)
	a.mu.Unlock()
	config.Name = name
	raw, err := os.ReadFile(messageConfigPath(a.dataPath))
	if errors.Is(err, os.ErrNotExist) {
		return config, nil
	}
	if err != nil {
		return config, err
	}
	err = json.Unmarshal(raw, &config)
	config.Name = name
	return config, err
}
func (a *App) messageVaultSecret(id, kind string) ([]byte, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.vault == nil || !a.vault.Unlocked() {
		return nil, errors.New("请先解锁保险库")
	}
	raw, err := a.vault.Get(id)
	if err == nil {
		return raw, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	seed := make([]byte, 32)
	if _, err = rand.Read(seed); err != nil {
		return nil, err
	}
	if err = a.vault.Put(id, kind, "Messages", seed, ""); err != nil {
		zeroBytes(seed)
		return nil, err
	}
	if err = a.vault.Save(); err != nil {
		a.vault.Delete(id)
		return nil, err
	}
	return a.vault.Get(id)
}
func (a *App) openMessageCenter(config messageCenterConfig) (*messageCenter, error) {
	if !config.Enabled {
		return nil, nil
	}
	if strings.TrimSpace(config.Name) == "" {
		return nil, errors.New("请先在设置 → 账户填写账户名称，才能启用设备通信。")
	}
	seed, err := a.messageVaultSecret("message:device", VaultTypeMessageIdentity)
	if err != nil {
		return nil, err
	}
	defer zeroBytes(seed)
	identity, err := messaging.FromSeed(seed)
	if err != nil {
		return nil, err
	}
	key, err := a.messageVaultSecret("message:storage", VaultTypeMessageStorage)
	if err != nil {
		return nil, err
	}
	defer zeroBytes(key)
	store, err := messaging.OpenStore(filepath.Join(a.dataPath, "messages"), key)
	if err != nil {
		return nil, err
	}
	service, err := messaging.NewService(identity, store, config.Name, nil, nil)
	if err != nil {
		store.Close()
		return nil, err
	}
	network, err := messaging.StartNetwork(a.bgCtx, service, config.Network)
	if err != nil {
		service.Close()
		store.Close()
		return nil, err
	}
	return &messageCenter{service: service, network: network, store: store, config: config}, nil
}
func (m *messageCenter) close() {
	m.lease.Lock()
	defer m.lease.Unlock()
	m.network.Close()
	m.service.Close()
	m.store.Close()
}
func (a *App) startMessagesIfEnabled() {
	a.messageMu.Lock()
	defer a.messageMu.Unlock()
	config, err := a.messageConfig()
	if err != nil {
		a.messageError = err.Error()
		return
	}
	if !config.Enabled {
		return
	}
	a.messages, err = a.openMessageCenter(config)
	if err != nil {
		a.messageError = err.Error()
	}
}
func validateMessageConfig(config messageCenterConfig) error {
	if (config.Enabled && config.Name == "") || len(config.Name) > 100 {
		return errors.New("请先在设置 → 账户填写账户名称，才能启用设备通信。")
	}
	if err := messaging.ValidateListen(config.Network.Listen); err != nil {
		return err
	}
	if len(config.Network.Relays) > 4 || len(config.Network.Advertise) > 12 || len(config.ICEURLs) > 4 || len(config.ICEUsername) > 200 {
		return errors.New("通信配置超出上限")
	}
	for _, endpoint := range append(append([]string{}, config.Network.Relays...), config.Network.Advertise...) {
		if _, err := messaging.ParseAddress(endpoint); err != nil {
			return err
		}
	}
	for _, endpoint := range config.ICEURLs {
		u, err := url.Parse(endpoint)
		if err != nil || u.Opaque == "" || u.User != nil || u.Fragment != "" || (u.Scheme != "stun" && u.Scheme != "turn" && u.Scheme != "turns") || len(endpoint) > 500 {
			return errors.New("ICE服务必须是stun:、turn:或turns:地址")
		}
	}
	return nil
}
func (a *App) configureMessages(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Config          messageCenterConfig `json:"config"`
		Credential      string              `json:"credential"`
		ClearCredential bool                `json:"clearCredential"`
	}
	if err := decode(w, r, &input); err != nil {
		fail(w, 400, err)
		return
	}
	a.mu.Lock()
	input.Config.Name = strings.TrimSpace(a.settings.UserName)
	a.mu.Unlock()
	if err := validateMessageConfig(input.Config); err != nil {
		fail(w, 400, err)
		return
	}
	if len(input.Credential) > 2000 {
		fail(w, 400, errors.New("TURN凭据过长"))
		return
	}
	a.messageMu.Lock()
	defer a.messageMu.Unlock()
	oldConfig, err := a.messageConfig()
	if err != nil {
		fail(w, 500, err)
		return
	}
	var restoreCredential func() error
	if input.Credential != "" || input.ClearCredential {
		a.mu.Lock()
		if a.vault == nil || !a.vault.Unlocked() {
			a.mu.Unlock()
			fail(w, 423, errors.New("保险库未解锁"))
			return
		}
		previous, existed := a.vault.entries["message:turn"]
		restoreCredential = func() error {
			a.mu.Lock()
			defer a.mu.Unlock()
			if existed {
				a.vault.entries["message:turn"] = previous
			} else {
				a.vault.Delete("message:turn")
			}
			return a.vault.Save()
		}
		if input.ClearCredential {
			a.vault.Delete("message:turn")
		} else {
			err = a.vault.Put("message:turn", "message-turn-credential", "Messages TURN", []byte(input.Credential), "")
		}
		if err == nil {
			err = a.vault.Save()
		}
		a.mu.Unlock()
		if err != nil {
			err = errors.Join(err, restoreCredential())
			fail(w, 500, err)
			return
		}
	}
	if a.messages != nil {
		a.messages.close()
		a.messages = nil
	}
	next, err := a.openMessageCenter(input.Config)
	if err == nil {
		err = atomicJSON(messageConfigPath(a.dataPath), input.Config)
	}
	if err != nil {
		if next != nil {
			next.close()
		}
		if restoreCredential != nil {
			err = errors.Join(err, restoreCredential())
		}
		a.messages, _ = a.openMessageCenter(oldConfig)
		a.messageError = err.Error()
		fail(w, 400, errors.New("消息配置未应用："+err.Error()))
		return
	}
	a.messages = next
	a.messageError = ""
	jsonOut(w, 200, map[string]any{"ok": true, "config": input.Config})
}
func (a *App) messageAPI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	path := strings.TrimPrefix(r.URL.Path, "/api/messages/")
	lock := a.readLockState()
	if lock.Locked && path != "cancel" && path != "voice-release" {
		fail(w, 423, errors.New("请先解锁工作台"))
		return
	}
	if path == "config" && r.Method == http.MethodPut {
		a.configureMessages(w, r)
		return
	}
	a.messageMu.Lock()
	config, err := a.messageConfig()
	if err != nil {
		a.messageMu.Unlock()
		fail(w, 500, err)
		return
	}
	m := a.messages
	if m != nil {
		if config.Name == "" {
			m.close()
			a.messages = nil
			m = nil
		} else {
			m.service.SetName(config.Name)
		}
	}
	messageErr := a.messageError
	if m != nil {
		m.lease.RLock()
	}
	a.messageMu.Unlock()
	if m != nil {
		defer m.lease.RUnlock()
	}
	if path == "state" && r.Method == http.MethodGet {
		out := map[string]any{"enabled": m != nil, "config": config, "networkError": messageErr}
		if m != nil {
			out["data"] = m.service.Snapshot()
			out["relays"] = m.network.Status()
		}
		jsonOut(w, 200, out)
		return
	}
	if m == nil {
		fail(w, 409, errors.New("请先启用设备消息通信"))
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	s := m.service
	switch {
	case path == "discover" && r.Method == http.MethodPost:
		var input struct {
			Code string `json:"code"`
		}
		if err = decode(w, r, &input); err == nil {
			var cards []messaging.Card
			cards, err = m.network.Discover(ctx, strings.ToUpper(strings.TrimSpace(input.Code)))
			if err == nil {
				jsonOut(w, 200, cards)
				return
			}
		}
	case path == "invite" && r.Method == http.MethodPost:
		var input struct {
			Peer     string              `json:"peer"`
			Card     *messaging.Card     `json:"card"`
			Kind     messaging.Kind      `json:"kind"`
			Resource string              `json:"resource"`
			Manifest *messaging.Manifest `json:"manifest"`
		}
		if err = decode(w, r, &input); err == nil {
			if input.Card != nil {
				if input.Card.Device.Fingerprint != input.Peer {
					err = errors.New("联系人指纹不匹配")
				} else {
					err = s.Remember(*input.Card)
				}
			}
			if err == nil {
				var invitation messaging.Invitation
				invitation, err = s.Request(ctx, input.Peer, input.Kind, input.Resource, input.Manifest)
				if invitation.ID != "" {
					jsonOut(w, 200, map[string]any{"invitation": invitation, "deliveryPending": err != nil})
					return
				}
			}
		}
	case path == "decide" && r.Method == http.MethodPost:
		var input struct {
			ID     string `json:"id"`
			Accept bool   `json:"accept"`
		}
		if err = decode(w, r, &input); err == nil {
			err = s.Decide(input.ID, input.Accept)
		}
	case path == "block" && r.Method == http.MethodPost:
		var input struct {
			Peer    string `json:"peer"`
			Blocked bool   `json:"blocked"`
		}
		if err = decode(w, r, &input); err == nil {
			err = s.Block(input.Peer, input.Blocked)
		}
	case path == "read" && r.Method == http.MethodPost:
		var input struct {
			Peer  string `json:"peer"`
			Group string `json:"group"`
		}
		if err = decode(w, r, &input); err == nil {
			err = s.MarkRead(input.Peer, input.Group)
		}
	case path == "text" && r.Method == http.MethodPost:
		var input struct {
			Peer  string `json:"peer"`
			Group string `json:"group"`
			Text  string `json:"text"`
		}
		if err = decode(w, r, &input); err == nil {
			err = s.SendText(ctx, input.Peer, input.Group, input.Text)
		}
	case path == "group" && r.Method == http.MethodPost:
		var input struct {
			Name  string   `json:"name"`
			Peers []string `json:"peers"`
		}
		if err = decode(w, r, &input); err == nil {
			var group messaging.GroupState
			group, err = s.CreateGroup(ctx, input.Name, input.Peers)
			if err == nil {
				jsonOut(w, 200, group)
				return
			}
		}
	case path == "group-call" && r.Method == http.MethodPost:
		var input struct {
			Group string `json:"group"`
		}
		if err = decode(w, r, &input); err == nil {
			var call messaging.CallState
			call, err = s.StartGroupCall(ctx, input.Group)
			if err == nil {
				jsonOut(w, 200, call)
				return
			}
		}
	case path == "signal" && r.Method == http.MethodPost:
		var input struct {
			Peer       string          `json:"peer"`
			Invitation string          `json:"invitation"`
			Call       string          `json:"call"`
			Group      string          `json:"group"`
			Data       json.RawMessage `json:"data"`
		}
		if err = decode(w, r, &input); err == nil {
			err = s.SendSignal(ctx, input.Peer, input.Invitation, input.Call, input.Group, input.Data)
		}
	case (path == "voice-lease" || path == "voice-release") && r.Method == http.MethodPost:
		var input struct {
			Owner      string `json:"owner"`
			Call       string `json:"call"`
			Peer       string `json:"peer"`
			Invitation string `json:"invitation"`
			Group      string `json:"group"`
		}
		if err = decode(w, r, &input); err != nil {
			break
		}
		if len(input.Owner) != 32 {
			err = errors.New("invalid voice owner")
			break
		}
		m.voiceMu.Lock()
		if path == "voice-release" {
			if m.voiceOwner == input.Owner {
				m.voiceOwner = ""
				m.voiceCall = ""
				m.voiceExpires = time.Time{}
			}
		} else if m.voiceOwner != "" && m.voiceOwner != input.Owner && time.Now().Before(m.voiceExpires) {
			err = errors.New("语音正在其他窗口使用")
		} else if !s.VoiceSessionAllowed(input.Peer, input.Invitation, input.Call, input.Group) {
			err = errors.New("通话尚未同意或已结束")
		} else {
			m.voiceOwner = input.Owner
			m.voiceCall = input.Call
			m.voiceExpires = time.Now().Add(15 * time.Second)
		}
		m.voiceMu.Unlock()
	case path == "ice" && r.Method == http.MethodGet:
		servers := []map[string]any{}
		if len(config.ICEURLs) > 0 {
			server := map[string]any{"urls": config.ICEURLs, "username": config.ICEUsername}
			a.mu.Lock()
			if a.vault != nil {
				raw, e := a.vault.Get("message:turn")
				if e == nil {
					server["credential"] = string(raw)
					zeroBytes(raw)
				}
			}
			a.mu.Unlock()
			servers = append(servers, server)
		}
		jsonOut(w, 200, servers)
		return
	case path == "cancel" && r.Method == http.MethodPost:
		var input struct {
			ID string `json:"id"`
		}
		if err = decode(w, r, &input); err == nil {
			err = s.Cancel(ctx, input.ID)
		}
	case path == "refresh" && r.Method == http.MethodPost:
		var input struct {
			ID string `json:"id"`
		}
		if err = decode(w, r, &input); err == nil {
			_, err = s.RefreshInvitation(ctx, input.ID)
		}
	case strings.HasPrefix(path, "chunks/") && r.Method == http.MethodPut:
		parts := strings.Split(path, "/")
		if len(parts) != 3 {
			err = errors.New("分块路径无效")
			break
		}
		var index int
		index, err = strconv.Atoi(parts[2])
		if err != nil {
			break
		}
		var data []byte
		r.Body = http.MaxBytesReader(w, r.Body, messaging.ChunkSize)
		data, err = io.ReadAll(r.Body)
		if err == nil {
			err = s.UploadChunk(ctx, parts[1], index, data)
		}
	case path == "save" && r.Method == http.MethodPost:
		var input struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		}
		if err = decode(w, r, &input); err == nil {
			err = a.saveMessageFile(ctx, m, input.ID, input.Name, lock.Gen)
		}
	case strings.HasPrefix(path, "download/") && r.Method == http.MethodGet:
		id := strings.TrimPrefix(path, "download/")
		snapshot := s.Snapshot()
		transfers, _ := snapshot["transfers"].(map[string]messaging.Transfer)
		file, ok := transfers[id]
		if !ok || !file.Incoming || file.Cancelled || len(file.Received) != len(file.Manifest.Hashes) {
			err = errors.New("文件尚未接收完成")
			break
		}
		w.Header().Set("Content-Length", strconv.FormatInt(file.Manifest.Size, 10))
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": filepath.Base(strings.ReplaceAll(file.Manifest.Name, "\\", "/"))}))
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if err = s.Download(ctx, id, &messageDownloadWriter{writer: w, a: a, gen: lock.Gen}); err != nil {
			return
		}
		return
	default:
		fail(w, 404, errors.New("消息接口不存在"))
		return
	}
	if err != nil {
		fail(w, 400, err)
		return
	}
	jsonOut(w, 200, map[string]bool{"ok": true})
}

type messageDownloadWriter struct {
	writer io.Writer
	a      *App
	gen    int64
	root   *os.Root
}

func (w *messageDownloadWriter) Write(data []byte) (int, error) {
	if w.root != nil {
		w.a.mu.Lock()
		current := w.a.workspace
		w.a.mu.Unlock()
		if current != w.root {
			return 0, errors.New("工作目录已切换")
		}
	}
	lock := w.a.readLockState()
	if lock.Locked || lock.Gen != w.gen {
		return 0, errors.New("工作台安全状态已变化")
	}
	return w.writer.Write(data)
}
func (a *App) saveMessageFile(ctx context.Context, m *messageCenter, id, name string, gen int64) error {
	if name == "" || name == "." || name == ".." || len(name) > 255 || strings.ContainsAny(name, "/\\\x00\r\n") {
		return errors.New("请填写不含目录的文件名")
	}
	a.filesMu.Lock()
	defer a.filesMu.Unlock()
	a.mu.Lock()
	root := a.workspace
	a.mu.Unlock()
	file, err := root.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	good := false
	defer func() {
		file.Close()
		if !good {
			_ = root.Remove(name)
		}
	}()
	if err = m.service.Download(ctx, id, &messageDownloadWriter{writer: file, a: a, gen: gen, root: root}); err != nil {
		return err
	}
	if err = file.Sync(); err != nil {
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	good = true
	return nil
}
