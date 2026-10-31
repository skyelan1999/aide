package server

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"os"
	"os/exec"
	"strings"
)

//go:embed relay/sftp_agent.py
var sftpRelayAgent string

//go:embed relay/sftp_proxy.py
var sftpRelayClient string

func isSFTPRelay(src Source) bool { return src.Type == "sftp" && src.Config.RelayHost != "" }

func validateSFTPRelay(src *Source) error {
	c := &src.Config
	c.RelayHost = strings.TrimSpace(c.RelayHost)
	if c.RelayHost == "" {
		c.RelayPort = 0
		return nil
	}
	if strings.HasPrefix(c.Username, "-") || strings.ContainsAny(c.Username, " \t\r\n\x00/@") {
		return errors.New("SSH 用户名无效")
	}
	for _, host := range []string{c.Host, c.RelayHost} {
		if net.ParseIP(host) == nil && !relayHostname.MatchString(host) {
			return errors.New("SFTP 和中继主机须为有效主机名或 IP，不含协议或端口")
		}
	}
	if c.Port == 0 {
		c.Port = 22
	}
	if c.RelayPort == 0 {
		c.RelayPort = 8768
	}
	if c.Port < 1 || c.Port > 65535 || c.RelayPort < 1 || c.RelayPort > 65535 {
		return errors.New("SFTP 和中继端口须为 1–65535")
	}
	return nil
}

func sftpRelayConfiguration(src Source) string {
	b, _ := json.Marshal([]any{src.ID, src.Config.Host, src.Config.Port, src.Config.Path})
	sum := sha256.Sum256(append([]byte("aide-sftp-relay-config-v1\x00"), b...))
	return hex.EncodeToString(sum[:])
}

func (a *App) sftpRelayConfigLocked(src Source) (map[string]any, error) {
	current, ok := a.findSource(src.ID)
	if !ok || !isSFTPRelay(current) || !current.Enabled || !relayConfigEqual(current, src) {
		return nil, errors.New("SFTP 中继来源已变化或停用")
	}
	if a.vault != nil && a.vault.Has(sourceVaultID(src.ID)) && !a.vault.Unlocked() {
		return nil, errors.New("请先解锁凭据保险库")
	}
	key, err := sourceRelayKey(a.sourceCredentialEntryLocked(src.ID).RelayKey)
	if err != nil {
		return nil, err
	}
	defer zeroBytes(key)
	return map[string]any{"id": src.ID, "host": src.Config.RelayHost, "listenPort": src.Config.RelayPort, "listenHost": "0.0.0.0", "targetHost": src.Config.Host, "targetPort": src.Config.Port, "configuration": sftpRelayConfiguration(src), "key": base64.StdEncoding.EncodeToString(key)}, nil
}

// ProxyCommand files are private, short-lived and never contain SFTP credentials.
// SSH's original target, host-key policy and authentication remain unchanged.
func (a *App) sftpRelayProxy(src Source) (string, func(), error) {
	a.mu.Lock()
	config, err := a.sftpRelayConfigLocked(src)
	a.mu.Unlock()
	if err != nil {
		return "", func() {}, err
	}
	python, err := exec.LookPath("python3")
	if err != nil {
		return "", func() {}, errors.New("SFTP 中继需要 Python 3 和 cryptography")
	}
	var paths []string
	cleanup := func() {
		for _, p := range paths {
			os.Remove(p)
		}
	}
	write := func(content []byte) (string, error) {
		f, e := os.CreateTemp("", "aide-sftp-proxy-*")
		if e != nil {
			return "", e
		}
		paths = append(paths, f.Name())
		_, e = f.Write(content)
		closeErr := f.Close()
		if e == nil {
			e = closeErr
		}
		return f.Name(), e
	}
	script, err := write([]byte(sftpRelayClient))
	if err != nil {
		cleanup()
		return "", func() {}, err
	}
	encoded, _ := json.Marshal(config)
	private, err := write(encoded)
	zeroBytes(encoded)
	if err != nil {
		cleanup()
		return "", func() {}, err
	}
	return shellQuote(python) + " " + shellQuote(script) + " " + shellQuote(private), cleanup, nil
}

func (a *App) probeSFTPRelay(ctx context.Context, src Source) error {
	// Execute the identical pinned handshake as ProxyCommand without opening SSH.
	a.mu.Lock()
	config, err := a.sftpRelayConfigLocked(src)
	a.mu.Unlock()
	if err != nil {
		return err
	}
	encoded, _ := json.Marshal(config)
	defer zeroBytes(encoded)
	cmd := exec.CommandContext(ctx, "python3", "-c", sftpRelayClient, "--probe")
	cmd.Stdin = strings.NewReader(string(encoded))
	if err := cmd.Run(); err != nil {
		return errors.New("SFTP 中继无法连接、密钥不匹配或目标服务不可达")
	}
	return nil
}

func sftpRelayScript(config map[string]any) string {
	// The downloaded bridge can forward only to this fixed target. It contains
	// the independent relay key, never the SFTP password or SSH private key.
	if ip := net.ParseIP(config["host"].(string)); ip != nil && ip.To4() == nil {
		config["listenHost"] = "::"
	}
	b, _ := json.Marshal(config)
	literal, _ := json.Marshal(string(b))
	return strings.Replace(sftpRelayAgent, "__CONFIG_JSON__", string(literal), 1)
}
