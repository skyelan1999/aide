package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// browseSFTPSource lists directories using the current editor values without
// saving a source or sharing the workspace's SSH connection.
func (a *App) browseSFTPSource(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ID       string       `json:"id"`
		Config   SourceConfig `json:"config"`
		Password string       `json:"password"`
		Key      string       `json:"key"`
		Path     string       `json:"path"`
	}
	if err := decode(w, r, &in); err != nil {
		fail(w, 400, err)
		return
	}
	if in.Path == "" {
		in.Path = "."
	}
	if err := validateRemoteBrowsePath(in.Path); err != nil {
		fail(w, 400, err)
		return
	}
	host := strings.TrimSpace(in.Config.Host)
	if host == "" || strings.HasPrefix(host, "-") || strings.ContainsAny(host, " \t\r\n\x00/@") {
		fail(w, 400, errors.New("请填写有效 SSH 主机"))
		return
	}
	if strings.HasPrefix(in.Config.Username, "-") || strings.ContainsAny(in.Config.Username, " \t\r\n\x00/@") {
		fail(w, 400, errors.New("SSH 用户名无效"))
		return
	}
	in.Config.Host = host
	if in.Config.Port == 0 {
		in.Config.Port = 22
	}
	if in.Config.Port < 1 || in.Config.Port > 65535 {
		fail(w, 400, errors.New("SSH 端口无效"))
		return
	}
	src := Source{Type: "sftp", Enabled: true, Config: in.Config}
	pw, key := in.Password, in.Key
	if in.ID != "" && pw == "" && key == "" {
		a.mu.Lock()
		saved, ok := a.findSource(in.ID)
		if ok && saved.Type == "sftp" && sftpConnIdentity(saved) == sftpConnIdentity(src) {
			pw, key = a.sourceCredentialLocked(in.ID)
		}
		a.mu.Unlock()
	}
	if src.Config.Auth != "key" && src.Config.Auth != "password" && src.Config.Auth != "none" {
		fail(w, 400, errors.New("SSH 认证方式无效"))
		return
	}
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		fail(w, 500, err)
		return
	}
	src.ID = "browse-" + hex.EncodeToString(nonce[:])
	sock := sourceSocket(src.ID)
	defer func() {
		a.killSourceSession(src.ID, a.sftpTargetOf(src))
		a.sshLifecycleMu.Lock()
		delete(a.sshGenerations, sock)
		a.sshLifecycleMu.Unlock()
	}()
	ctx, cancel := context.WithTimeout(r.Context(), sftpTimeout)
	defer cancel()
	authCtx, authCancel := context.WithTimeout(ctx, 10*time.Second)
	err := a.ensureSourceSessionCredentials(authCtx, src, pw, key, a.sshSessionGeneration(sock))
	authCancel()
	if err != nil {
		fail(w, 400, err)
		return
	}
	args := []string{"-o", "ControlPath=" + sock, "-o", "BatchMode=yes", "-o", "LogLevel=ERROR", "-P", fmt.Sprint(src.Config.Port), "-b", "-", a.sftpTargetOf(src)}
	out, err := a.runSFTPBatch(ctx, args, "cd "+shellQuoteRemote(in.Path)+"\nls -l\n")
	if err != nil {
		fail(w, 400, sshProcessError(ctx, "SSH 目录浏览失败", out, err))
		return
	}
	jsonOut(w, 200, parseSourceSFTPListing(string(out), in.Path))
}
