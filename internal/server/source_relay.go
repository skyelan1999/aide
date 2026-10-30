package server

import (
	"bufio"
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/tls"
	_ "embed"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// The deployed agent is a product resource, not an installed Terminal plugin.
// It reuses Terminal's TLS/pinned-identity/challenge pattern with distinct domains.
//
//go:embed relay/agent.py
var sourceRelayAgent string

const sourceRelayMaxFile = 64 << 20
const sourceRelayMaxList = 1 << 20

var relayHostname = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9.-]{0,251}[A-Za-z0-9])?$`)
var relayWindowsRoot = regexp.MustCompile(`^[A-Za-z]:[\\/]`)

func validateSourceRelay(src *Source) error {
	c := &src.Config
	c.Host, c.Path = strings.TrimSpace(c.Host), strings.TrimSpace(c.Path)
	if net.ParseIP(c.Host) == nil && !relayHostname.MatchString(c.Host) {
		return errors.New("中继需要有效的主机名或 IP 地址，不包含协议或端口")
	}
	if c.Port == 0 {
		c.Port = 8767
	}
	if c.Port < 1 || c.Port > 65535 {
		return errors.New("中继端口须为 1–65535")
	}
	if len(c.Path) > 4096 || strings.ContainsAny(c.Path, "\x00\r\n") || !(strings.HasPrefix(c.Path, "/") || relayWindowsRoot.MatchString(c.Path) || strings.HasPrefix(c.Path, `\\`)) {
		return errors.New("请填写远程设备上的绝对共享目录")
	}
	*c = SourceConfig{Host: c.Host, Port: c.Port, Path: c.Path}
	return nil
}
func sourceRelayKey(value string) ([]byte, error) {
	key, err := base64.StdEncoding.Strict().DecodeString(value)
	if err != nil || len(key) != 32 || base64.StdEncoding.EncodeToString(key) != value {
		return nil, errors.New("中继密钥必须是 32 字节随机密钥的 Base64 文本")
	}
	return key, nil
}
func newSourceRelayKey() (string, error) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return "", err
	}
	defer zeroBytes(key)
	return base64.StdEncoding.EncodeToString(key), nil
}
func sourceRelayDigest(src Source) string {
	sum := sha256.Sum256([]byte("aide-file-relay-config-v1\x00" + src.ID + "\x00" + src.Config.Path))
	return hex.EncodeToString(sum[:])
}
func relayConfigEqual(left, right Source) bool {
	a, _ := json.Marshal(left.Config)
	b, _ := json.Marshal(right.Config)
	return left.ID == right.ID && left.Type == right.Type && bytes.Equal(a, b)
}
func (a *App) relayKeyLocked(id string) ([]byte, error) {
	if a.vault != nil && a.vault.Has(sourceVaultID(id)) && !a.vault.Unlocked() {
		return nil, errors.New("凭据保险库未解锁")
	}
	_, value := a.sourceCredentialLocked(id)
	return sourceRelayKey(value)
}
func (a *App) sourceRelayCredentials(src Source, write bool) ([]byte, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	current, ok := a.findSource(src.ID)
	if !ok || !current.Enabled || current.Type != "relay" || !relayConfigEqual(current, src) {
		return nil, errors.New("中继来源已变化或停用，请重新访问")
	}
	if write && (!current.RW || !src.RW) {
		return nil, errors.New("该来源为只读")
	}
	return a.relayKeyLocked(src.ID)
}
func sourceRelayPath(p string, root bool) error {
	if root && (p == "" || p == ".") {
		return nil
	}
	if len(p) > 4096 || strings.ContainsAny(p, "\\\x00\r\n:") {
		return errors.New("中继相对路径无效")
	}
	if err := safePath(p); err != nil {
		return err
	}
	for _, part := range strings.Split(p, "/") {
		if part == "" || part == "." || part == ".." || strings.HasPrefix(part, ".aide-relay-") {
			return errors.New("中继相对路径无效")
		}
	}
	return nil
}

type sourceRelayHeader struct {
	OK            bool             `json:"ok"`
	Version       int              `json:"version"`
	Challenge     string           `json:"challenge"`
	ID            string           `json:"id"`
	Kind          string           `json:"kind"`
	Configuration string           `json:"configuration"`
	Writable      bool             `json:"writable"`
	Ready         bool             `json:"ready"`
	Size          int64            `json:"size"`
	Info          *sourceRelayInfo `json:"info"`
	Handle        string           `json:"handle"`
	Available     int64            `json:"available"`
	SHA256        string           `json:"sha256"`
	Code          string           `json:"code"`
}

func relayReadHeader(reader *bufio.Reader, requireOK bool) (sourceRelayHeader, error) {
	var header sourceRelayHeader
	line, err := reader.ReadSlice('\n')
	if err != nil || len(line) > 4096 {
		return header, errors.New("中继响应无效或超过上限")
	}
	if json.Unmarshal(line, &header) != nil {
		return header, errors.New("中继响应格式无效")
	}
	if requireOK && !header.OK {
		switch header.Code {
		case "not-found":
			return header, os.ErrNotExist
		case "exists":
			return header, os.ErrExist
		case "denied":
			return header, errors.New("中继拒绝访问；请检查共享路径和远端脚本写权限")
		}
		return header, errors.New("中继操作失败；请检查部署配置、文件类型和大小限制")
	}
	return header, nil
}
func relaySendHeader(conn net.Conn, value any) error {
	b, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if len(b) > 4095 {
		return errors.New("中继请求超过上限")
	}
	_, err = io.Copy(conn, bytes.NewReader(append(b, '\n')))
	return err
}

func (a *App) relayCall(ctx context.Context, src Source, mode string, request map[string]any, data []byte, limit int64) ([]byte, sourceRelayHeader, error) {
	payload, header, err := a.relayCallInner(ctx, src, mode, request, data, limit)
	if ctx.Err() != nil {
		return nil, header, ctx.Err()
	}
	return payload, header, err
}
func (a *App) relayCallInner(ctx context.Context, src Source, mode string, request map[string]any, data []byte, limit int64) ([]byte, sourceRelayHeader, error) {
	var empty sourceRelayHeader
	write := mode == "write" || mode == "mkdir" || mode == "rename" || mode == "remove" || mode == "openwrite" || mode == "writeat" || mode == "truncate" || mode == "sync"
	key, err := a.sourceRelayCredentials(src, write)
	if err != nil {
		return nil, empty, err
	}
	defer zeroBytes(key)
	if expected, _ := ctx.Value(relayExpectedCredential{}).(string); expected != "" && hash(key) != expected {
		return nil, empty, errors.New("中继凭据已变化，旧文件绑定不可继续使用")
	}
	derive := hmac.New(sha256.New, key)
	derive.Write([]byte("aide-file-relay-certificate-v1"))
	seed := derive.Sum(nil)
	private := ed25519.NewKeyFromSeed(seed)
	expected := append([]byte(nil), private[32:]...)
	zeroBytes(seed)
	zeroBytes(private)
	dialer := tls.Dialer{NetDialer: &net.Dialer{Timeout: 10 * time.Second}, Config: &tls.Config{MinVersion: tls.VersionTLS13,
		// Self-signed identities are verified by the exact key-derived pin, before
		// challenge proofs, credentials or file operations can enter this connection.
		InsecureSkipVerify: true, VerifyConnection: func(state tls.ConnectionState) error {
			if len(state.PeerCertificates) == 0 {
				return errors.New("relay identity missing")
			}
			actual, ok := state.PeerCertificates[0].PublicKey.(ed25519.PublicKey)
			if !ok || subtle.ConstantTimeCompare(actual, expected) != 1 {
				return errors.New("relay identity mismatch")
			}
			return nil
		}}}
	conn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(src.Config.Host, strconv.Itoa(src.Config.Port)))
	if err != nil {
		if ctx.Err() != nil {
			return nil, empty, ctx.Err()
		}
		return nil, empty, errors.New("中继无法连接或身份不匹配；请检查地址、密钥与已部署脚本")
	}
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()
	deadline := time.Now().Add(30 * time.Second)
	if until, ok := ctx.Deadline(); ok && until.Before(deadline) {
		deadline = until
	}
	if err = conn.SetDeadline(deadline); err != nil {
		return nil, empty, err
	}
	reader := bufio.NewReaderSize(conn, 4096)
	hello, err := relayReadHeader(reader, false)
	if err != nil {
		return nil, empty, err
	}
	challenge, err := base64.StdEncoding.Strict().DecodeString(hello.Challenge)
	if err != nil || len(challenge) != 32 || base64.StdEncoding.EncodeToString(challenge) != hello.Challenge || hello.Version != 1 {
		return nil, empty, errors.New("中继握手无效")
	}
	digest := sourceRelayDigest(src)
	proof := hmac.New(sha256.New, key)
	proof.Write([]byte("aide-file-relay-v1:" + hello.Challenge + ":" + mode + ":" + src.ID + ":" + digest))
	if err = relaySendHeader(conn, map[string]any{"id": src.ID, "mode": mode, "configuration": digest, "proof": hex.EncodeToString(proof.Sum(nil))}); err != nil {
		return nil, empty, err
	}
	ack, err := relayReadHeader(reader, true)
	if err != nil {
		return nil, empty, err
	}
	if ack.ID != src.ID || ack.Kind != "files" || ack.Configuration != digest {
		return nil, empty, errors.New("中继部署配置已变化，请重新下载并部署脚本")
	}
	if mode == "probe" {
		return nil, ack, nil
	}
	if write && !ack.Writable {
		return nil, empty, errors.New("远端脚本为只读，请在允许读写后重新下载并部署脚本")
	}
	// Recheck registry/permissions after the potentially slow TLS handshake.
	currentKey, err := a.sourceRelayCredentials(src, write)
	if err != nil {
		return nil, empty, err
	}
	sameKey := hmac.Equal(currentKey, key)
	zeroBytes(currentKey)
	if !sameKey {
		return nil, empty, errors.New("中继凭据已变化，请重新访问")
	}
	if err = relaySendHeader(conn, request); err != nil {
		return nil, empty, err
	}
	response, err := relayReadHeader(reader, true)
	if err != nil {
		return nil, empty, err
	}
	if mode == "write" || mode == "writeat" {
		if !response.Ready {
			return nil, empty, errors.New("中继上传未就绪")
		}
		if _, err = io.Copy(conn, bytes.NewReader(data)); err != nil {
			return nil, empty, err
		}
		response, err = relayReadHeader(reader, true)
		return nil, response, err
	}
	if mode != "list" && mode != "read" && mode != "readat" {
		return nil, response, nil
	}
	if response.Size < 0 || response.Size > limit {
		return nil, empty, errors.New("中继文件或目录响应超过读取上限")
	}
	payload := make([]byte, int(response.Size))
	if _, err = io.ReadFull(reader, payload); err != nil {
		return nil, empty, errors.New("中继传输中断，未接受不完整内容")
	}
	footer, err := relayReadHeader(reader, true)
	if err != nil {
		return nil, empty, err
	}
	payloadDigest := sha256.Sum256(payload)
	if footer.SHA256 != hex.EncodeToString(payloadDigest[:]) {
		return nil, empty, errors.New("中继内容校验失败，未接受传输内容")
	}
	if ctx.Err() != nil {
		return nil, empty, ctx.Err()
	}
	return payload, response, nil
}
func (a *App) relayListSource(ctx context.Context, src Source, p string) ([]map[string]any, error) {
	if err := sourceRelayPath(p, true); err != nil {
		return nil, err
	}
	if p == "" {
		p = "."
	}
	data, _, err := a.relayCall(ctx, src, "list", map[string]any{"path": p}, nil, sourceRelayMaxList)
	if err != nil {
		return nil, err
	}
	var items []map[string]any
	if json.Unmarshal(data, &items) != nil || len(items) > 2000 {
		return nil, errors.New("中继目录响应无效或过大")
	}
	for _, entry := range items {
		name, ok := entry["name"].(string)
		entryPath, pok := entry["path"].(string)
		_, dok := entry["dir"].(bool)
		if !ok || !pok || !dok || strings.Contains(name, "/") || sourceRelayPath(name, false) != nil || entryPath != path.Join(p, name) {
			return nil, errors.New("中继返回无效的文件路径")
		}
	}
	return items, nil
}
func (a *App) relayReadSource(ctx context.Context, src Source, p string, limit int64) ([]byte, error) {
	if err := sourceRelayPath(p, false); err != nil {
		return nil, err
	}
	if limit <= 0 || limit > sourceRelayMaxFile {
		return nil, errors.New("中继文件读取上限为 64 MiB")
	}
	data, _, err := a.relayCall(ctx, src, "read", map[string]any{"path": p, "limit": limit}, nil, limit)
	return data, err
}
func (a *App) relayWriteSource(src Source, p string, data []byte, exclusive bool) error {
	return a.relayWriteSourceContext(context.Background(), src, p, data, exclusive)
}
func (a *App) relayWriteSourceContext(ctx context.Context, src Source, p string, data []byte, exclusive bool) error {
	if err := sourceRelayPath(p, false); err != nil {
		return err
	}
	if len(data) > sourceRelayMaxFile {
		return errors.New("中继单文件上传上限为 64 MiB")
	}
	digest := sha256.Sum256(data)
	_, _, err := a.relayCall(ctx, src, "write", map[string]any{"path": p, "size": len(data), "exclusive": exclusive, "sha256": hex.EncodeToString(digest[:])}, data, 0)
	return err
}
func (a *App) relayMutateSource(src Source, mode, p, destination string) error {
	if err := sourceRelayPath(p, false); err != nil {
		return err
	}
	if mode == "rename" {
		if err := sourceRelayPath(destination, false); err != nil {
			return err
		}
	}
	_, _, err := a.relayCall(context.Background(), src, mode, map[string]any{"path": p, "destination": destination}, nil, 0)
	return err
}
func (a *App) downloadSourceRelay(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	src, ok := a.findSource(r.PathValue("id"))
	if !ok || src.Type != "relay" || src.Builtin {
		a.mu.Unlock()
		fail(w, 404, errors.New("中继来源不存在"))
		return
	}
	key, err := a.relayKeyLocked(src.ID)
	a.mu.Unlock()
	if err != nil {
		fail(w, http.StatusLocked, err)
		return
	}
	defer zeroBytes(key)
	config := map[string]any{"id": src.ID, "root": src.Config.Path, "listenHost": "0.0.0.0", "listenPort": src.Config.Port, "rw": src.RW, "key": base64.StdEncoding.EncodeToString(key)}
	if ip := net.ParseIP(src.Config.Host); ip != nil && ip.To4() == nil {
		config["listenHost"] = "::"
	}
	encoded, _ := json.Marshal(config)
	literal, _ := json.Marshal(string(encoded))
	script := strings.Replace(sourceRelayAgent, "__CONFIG_JSON__", string(literal), 1)
	w.Header().Set("Content-Type", "text/x-python; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="aide-reference-relay-%s.py"`, src.ID))
	io.WriteString(w, script)
}
func (a *App) probeSourceRelay(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	src, ok := a.findSource(r.PathValue("id"))
	a.mu.Unlock()
	if !ok || src.Type != "relay" || !src.Enabled {
		fail(w, 404, errors.New("中继来源不存在或已停用"))
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
	defer cancel()
	_, result, err := a.relayCall(ctx, src, "probe", nil, nil, 0)
	if err != nil {
		jsonOut(w, 200, map[string]any{"online": false, "error": err.Error()})
		return
	}
	jsonOut(w, 200, map[string]any{"online": true, "remoteWritable": result.Writable, "rw": src.RW && result.Writable})
}
