package server

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path"
	"strings"
	"time"
)

// 单 SSH 会话实现（FR-77 / LIM-28）：
// OpenSSH ControlMaster（ControlPersist）复用一条 master 连接；
// 密码经 SSH_ASKPASS 注入（无交互终端）；私钥经 -i 指向临时密钥文件。
// sshBin/sftpBin 可注入以支持测试替身。

const (
	sshControlSocket = "/tmp/aide-ssh.sock"
	sshTimeout       = 10 * time.Second
	remoteCmdTimeout = 60 * time.Second
	sftpTimeout      = 30 * time.Second
)

// killSSHSession 关闭既有 master 连接（配置变化时调用；仅允许一个会话）。
func (a *App) killSSHSession() {
	if a.sshBin == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = exec.CommandContext(ctx, a.sshBin, "-S", sshControlSocket, "-O", "exit", a.sshTarget()).Run()
	_ = os.Remove(sshControlSocket)
	_ = os.Remove(sshControlSocket + ".askpass")
	_ = os.Remove(sshControlSocket + ".key")
}

func (a *App) sshTarget() string {
	u := a.wsConfig.Workspace.Username
	if u == "" {
		u = "root"
	}
	return u + "@" + a.wsConfig.Workspace.Host
}

func (a *App) sshCommonArgs() []string {
	port := a.wsConfig.Workspace.Port
	if port == 0 {
		port = 22
	}
	return []string{
		"-o", "ControlPath=" + sshControlSocket,
		"-o", "StrictHostKeyChecking=accept-new",
		"-o", "UserKnownHostsFile=/home/aide/.ssh/known_hosts",
		"-o", "ConnectTimeout=10",
		"-o", "LogLevel=ERROR",
		"-p", fmt.Sprint(port),
	}
}

// ensureSSHSession 建立/复用唯一 master 会话。返回错误信息（供用户界面反馈）。
func (a *App) ensureSSHSession(ctx context.Context) error {
	host := a.wsConfig.Workspace.Host
	if host == "" {
		return fmt.Errorf("未配置远程主机")
	}
	checkCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	check := exec.CommandContext(checkCtx, a.sshBin, append([]string{"-S", sshControlSocket, "-O", "check"}, append(a.sshCommonArgs(), a.sshTarget())...)...)
	if err := check.Run(); err == nil {
		return nil // 会话已存在
	}
	args := append([]string{"-fNM", "-o", "ControlMaster=yes", "-o", "ControlPersist=600"}, append(a.sshCommonArgs(), a.sshTarget())...)
	env := []string{"PATH=/usr/local/bin:/usr/bin:/bin", "HOME=/home/aide"}
	// 凭据从加密 vault 解密（#38）；ref 模式直接引用已校验路径，不写临时文件。
	auth := a.wsConfig.Workspace.Auth
	password, keyMaterial, passphrase, directKeyPath := a.wsRuntimeCredentials()
	if auth == "key" {
		switch {
		case directKeyPath != "":
			// 引用路径模式：直接使用（保存时已校验在挂载根内），私钥不落地副本
			args = append([]string{"-i", directKeyPath}, args...)
		case keyMaterial != "":
			// 粘贴/导入副本：解密后写入受控临时文件（0600），defer 用完即删
			tmpDir := a.ensureSecretsTmpDir()
			tmp, err := os.CreateTemp(tmpDir, "aide-sshkey-*")
			if err != nil {
				return fmt.Errorf("创建临时密钥文件失败: %w", err)
			}
			tmpPath := tmp.Name()
			if _, err := tmp.WriteString(keyMaterial); err != nil {
				tmp.Close()
				os.Remove(tmpPath)
				return fmt.Errorf("写入临时密钥失败: %w", err)
			}
			tmp.Close()
			_ = os.Chmod(tmpPath, 0600)
			defer os.Remove(tmpPath) // 主连接建立后立即删除，私钥不留存
			args = append([]string{"-i", tmpPath}, args...)
		default:
			return fmt.Errorf("SSH 密钥未配置或凭证保险库已锁定（请在工作空间设置中解锁）")
		}
		// 私钥口令经 SSH_ASKPASS 注入（不写命令行参数）
		if passphrase != "" {
			ask := sshControlSocket + ".askpass"
			script := "#!/bin/sh\necho " + shellQuote(passphrase) + "\n"
			if err := os.WriteFile(ask, []byte(script), 0700); err != nil {
				return fmt.Errorf("写入 askpass 失败: %w", err)
			}
			env = append(env, "SSH_ASKPASS="+ask, "SSH_ASKPASS_REQUIRE=force", "DISPLAY=aide:0")
		}
	} else if auth == "password" && password != "" {
		ask := sshControlSocket + ".askpass"
		script := "#!/bin/sh\necho " + shellQuote(password) + "\n"
		if err := os.WriteFile(ask, []byte(script), 0700); err != nil {
			return fmt.Errorf("写入 askpass 失败: %w", err)
		}
		env = append(env, "SSH_ASKPASS="+ask, "SSH_ASKPASS_REQUIRE=force", "DISPLAY=aide:0")
		args = append([]string{"-o", "NumberOfPasswordPrompts=1"}, args...)
	}
	cmd := exec.CommandContext(ctx, a.sshBin, args...)
	cmd.Env = env
	cmd.Dir = "/home/aide"
	_ = os.MkdirAll("/home/aide/.ssh", 0700)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("SSH 连接失败: %s", strings.TrimSpace(string(out)))
	}
	// 复检
	checkCtx2, cancel2 := context.WithTimeout(ctx, 5*time.Second)
	defer cancel2()
	check2 := exec.CommandContext(checkCtx2, a.sshBin, append([]string{"-S", sshControlSocket, "-O", "check"}, append(a.sshCommonArgs(), a.sshTarget())...)...)
	if err := check2.Run(); err != nil {
		return fmt.Errorf("SSH 会话未建立")
	}
	return nil
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// wsRuntimeCredentials 为运行时取 SSH 凭据：优先从加密 vault 解密；未解锁时回退旧明文（迁移过渡）。
// 返回：登录密码、私钥正文（ref 模式为空）、私钥口令、直接使用的密钥路径（ref 模式非空）。
// 返回的明文由调用方（ensureSSHSession）尽快消费；这里用完即清切片。
func (a *App) wsRuntimeCredentials() (password, keyMaterial, passphrase, directKeyPath string) {
	if a.vault != nil && a.vault.Unlocked() {
		if b, e := a.vault.Get(VaultIDWSPassword); e == nil {
			password = string(b)
			zeroBytes(b)
		}
		if b, e := a.vault.Get(VaultIDWSKey); e == nil {
			keyMaterial = string(b)
			zeroBytes(b)
		}
		if b, e := a.vault.Get(VaultIDWSPassphrase); e == nil {
			passphrase = string(b)
			zeroBytes(b)
		}
	}
	// 旧明文回退（vault 未解锁/迁移前）
	if password == "" {
		password = a.wsSecrets.Password
	}
	if keyMaterial == "" {
		keyMaterial = a.wsSecrets.Key
	}
	// 引用路径模式：直接使用保存时校验过的容器路径（翻译到真实 fs 路径）
	if a.wsConfig.Workspace.KeyMode == "ref" && strings.TrimSpace(a.wsConfig.Workspace.KeyRefPath) != "" {
		if realPath, _, err := a.resolveHostPath(a.wsConfig.Workspace.KeyRefPath); err == nil {
			directKeyPath = realPath
		}
	}
	return
}

// execRemote 通过唯一 master 会话执行远程命令；输出经 io.Writer 流式返回。
func (a *App) execRemote(ctx context.Context, command string, stdout, stderr *streamWriter) (int, error) {
	args := append([]string{"-S", sshControlSocket}, a.sshCommonArgs()...)
	args = append(args, a.sshTarget(), command)
	cmd := exec.CommandContext(ctx, a.sshBin, args...)
	cmd.Env = []string{"PATH=/usr/local/bin:/usr/bin:/bin", "HOME=/home/aide"}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	err := cmd.Run()
	if err == nil {
		return 0, nil
	}
	if ctx.Err() != nil {
		return -1, fmt.Errorf("远程命令已取消或超时")
	}
	return -1, fmt.Errorf("远程执行失败: %w", err)
}

// ── SFTP（经同一 master 会话，无需二次认证） ──

func (a *App) sftpArgs() []string {
	return append([]string{"-o", "ControlPath=" + sshControlSocket, "-o", "BatchMode=yes", "-o", "LogLevel=ERROR"}, append([]string{"-P", fmt.Sprint(a.wsConfig.Workspace.Port)}, a.sshTarget())...)
}

func (a *App) sftpBatch(batch string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), sftpTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, a.sftpBin, append(a.sftpArgs(), "-b", "-")...)
	cmd.Stdin = strings.NewReader(batch)
	cmd.Env = []string{"PATH=/usr/local/bin:/usr/bin:/bin", "HOME=/home/aide"}
	out, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		return "", fmt.Errorf("SFTP 操作超时")
	}
	if err != nil {
		return "", fmt.Errorf("SFTP 失败: %s", strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

// sftpList 列远程目录：返回 {name, path, dir} 列表（与 listLocalDir 同构）。
func (a *App) sftpList(remoteDir string) ([]map[string]any, error) {
	out, err := a.sftpBatch("cd " + shellQuoteRemote(remoteDir) + "\nls -l\n")
	if err != nil {
		return nil, err
	}
	items := []map[string]any{}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "sftp>") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 9 || (fields[0] != "-rw" && !strings.HasPrefix(fields[0], "drw") && fields[0][0] != 'd' && fields[0][0] != '-' && fields[0][0] != 'l') {
			continue
		}
		name := strings.Join(fields[8:], " ")
		dir := fields[0][0] == 'd'
		items = append(items, map[string]any{"name": name, "path": name, "dir": dir})
		if len(items) >= 2000 {
			break
		}
	}
	return items, nil
}

func (a *App) sftpRead(remoteFile string) ([]byte, error) {
	tmp, err := os.CreateTemp("", "aide-sftp-*")
	if err != nil {
		return nil, err
	}
	tmpPath := tmp.Name()
	tmp.Close()
	defer os.Remove(tmpPath)
	if _, err := a.sftpBatch("get " + shellQuoteRemote(remoteFile) + " " + shellQuoteRemote(tmpPath) + "\n"); err != nil {
		return nil, err
	}
	return os.ReadFile(tmpPath)
}

func (a *App) sftpWrite(remoteFile string, b []byte) error {
	tmp, err := os.CreateTemp("", "aide-sftp-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return err
	}
	tmp.Close()
	defer os.Remove(tmpPath)
	tmpRemote := remoteFile + ".aide-tmp"
	if _, err := a.sftpBatch("put -P " + shellQuoteRemote(tmpPath) + " " + shellQuoteRemote(tmpRemote) + "\nrename " + shellQuoteRemote(tmpRemote) + " " + shellQuoteRemote(remoteFile) + "\n"); err != nil {
		return err
	}
	return nil
}

func (a *App) sftpExists(remoteFile string) bool {
	out, err := a.sftpBatch("ls -l " + shellQuoteRemote(remoteFile) + "\n")
	if err != nil || strings.Contains(out, "Couldn't") || strings.Contains(out, "No such file") {
		return false
	}
	return true
}

func shellQuoteRemote(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `\"`) + `"`
}

// ── 来源级 SFTP（每来源独立 ControlMaster socket；复用 ensure 语义） ──

func sourceSocket(id string) string { return "/tmp/aide-src-" + id + ".sock" }

func (a *App) sftpTargetOf(src Source) string {
	u := src.Config.Username
	if u == "" {
		u = "root"
	}
	return u + "@" + src.Config.Host
}

func (a *App) ensureSourceSession(ctx context.Context, src Source) error {
	if src.Config.Host == "" {
		return fmt.Errorf("未配置主机")
	}
	sock := sourceSocket(src.ID)
	port := src.Config.Port
	if port == 0 {
		port = 22
	}
	base := []string{"-o", "ControlPath=" + sock, "-o", "StrictHostKeyChecking=accept-new", "-o", "UserKnownHostsFile=/home/aide/.ssh/known_hosts", "-o", "ConnectTimeout=10", "-o", "LogLevel=ERROR", "-p", fmt.Sprint(port)}
	if err := exec.CommandContext(ctx, a.sshBin, append([]string{"-S", sock, "-O", "check"}, append(base, a.sftpTargetOf(src))...)...).Run(); err == nil {
		return nil
	}
	args := append([]string{"-fNM", "-o", "ControlMaster=yes", "-o", "ControlPersist=600"}, append(base, a.sftpTargetOf(src))...)
	env := []string{"PATH=/usr/local/bin:/usr/bin:/bin", "HOME=/home/aide"}
	a.mu.Lock()
	sec := a.sourceSecrets.Secrets[src.ID]
	a.mu.Unlock()
	if src.Config.Auth == "key" && sec.Key != "" {
		keyPath := sock + ".key"
		if err := os.WriteFile(keyPath, []byte(sec.Key), 0600); err != nil {
			return err
		}
		args = append([]string{"-i", keyPath}, args...)
	} else if src.Config.Auth == "password" && sec.Password != "" {
		ask := sock + ".askpass"
		if err := os.WriteFile(ask, []byte("#!/bin/sh\necho "+shellQuote(sec.Password)+"\n"), 0700); err != nil {
			return err
		}
		env = append(env, "SSH_ASKPASS="+ask, "SSH_ASKPASS_REQUIRE=force", "DISPLAY=aide:0")
		args = append([]string{"-o", "NumberOfPasswordPrompts=1"}, args...)
	}
	cmd := exec.CommandContext(ctx, a.sshBin, args...)
	cmd.Env = env
	cmd.Dir = "/home/aide"
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("SFTP 连接失败: %s", strings.TrimSpace(string(out)))
	}
	return nil
}

func (a *App) sftpBatchSource(src Source, batch string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), sftpTimeout)
	defer cancel()
	if err := a.ensureSourceSession(ctx, src); err != nil {
		return "", err
	}
	port := src.Config.Port
	if port == 0 {
		port = 22
	}
	args := []string{"-o", "ControlPath=" + sourceSocket(src.ID), "-o", "BatchMode=yes", "-o", "LogLevel=ERROR", "-P", fmt.Sprint(port), "-b", "-", a.sftpTargetOf(src)}
	cmd := exec.CommandContext(ctx, a.sftpBin, args...)
	cmd.Stdin = strings.NewReader(batch)
	cmd.Env = []string{"PATH=/usr/local/bin:/usr/bin:/bin", "HOME=/home/aide"}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("SFTP 失败: %s", strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

func (a *App) sftpListSource(src Source, p string) ([]map[string]any, error) {
	remoteDir := pathJoinRemote(src.Config.Path, p)
	out, err := a.sftpBatchSource(src, "cd "+shellQuoteRemote(remoteDir)+"\nls -l\n")
	if err != nil {
		return nil, err
	}
	items := []map[string]any{}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "sftp>") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 9 || (fields[0][0] != 'd' && fields[0][0] != '-' && fields[0][0] != 'l') {
			continue
		}
		name := strings.Join(fields[8:], " ")
		if fields[0][0] == 'l' || name == "." || name == ".." || safePath(name) != nil {
			continue
		}
		items = append(items, map[string]any{"name": name, "path": path.Join(p, name), "dir": fields[0][0] == 'd'})
		if len(items) >= 2000 {
			break
		}
	}
	return items, nil
}

func (a *App) sftpReadSource(src Source, p string) ([]byte, error) {
	remoteFile := pathJoinRemote(src.Config.Path, p)
	tmp, err := os.CreateTemp("", "aide-src-sftp-*")
	if err != nil {
		return nil, err
	}
	tmpPath := tmp.Name()
	tmp.Close()
	defer os.Remove(tmpPath)
	if _, err := a.sftpBatchSource(src, "get "+shellQuoteRemote(remoteFile)+" "+shellQuoteRemote(tmpPath)+"\n"); err != nil {
		return nil, err
	}
	return os.ReadFile(tmpPath)
}

func (a *App) sftpWriteSource(src Source, p string, b []byte) error {
	remoteFile := pathJoinRemote(src.Config.Path, p)
	tmp, err := os.CreateTemp("", "aide-src-sftp-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return err
	}
	tmp.Close()
	defer os.Remove(tmpPath)
	tmpRemote := remoteFile + ".aide-tmp"
	if _, err := a.sftpBatchSource(src, "put -P "+shellQuoteRemote(tmpPath)+" "+shellQuoteRemote(tmpRemote)+"\nrename "+shellQuoteRemote(tmpRemote)+" "+shellQuoteRemote(remoteFile)+"\n"); err != nil {
		return err
	}
	return nil
}
