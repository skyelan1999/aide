package server

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
	"unicode/utf8"
)

// remoteRunSeq 为每次远程执行生成唯一 runID，避免并发命令的 PGID 标记互相覆盖。
var remoteRunSeq int64

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
//
// 取消/超时清理语义（远程 shell 取消链）：远端命令被包进一个独立会话/进程组
// （setsid），启动瞬间把该组 PGID 写入远端标记文件。命令正常结束时由包装脚本
// 自行删除标记；一旦 ctx 被取消（用户点停止）或超时，CommandContext 只会杀掉本地
// ssh 客户端——setsid 出去的远端进程组并不会随之退出——因此本函数在返回前另开
// 一条 SSH 连接显式 `kill -- -PGID` 把整个远端进程组（含子孙）收掉并复核。
// 注意：workflow 的心跳只是本地定时 ticker，绝不能当作远端存活证据；远端是否被
// 真正清理由 cleanupRemoteGroup 的复核行（AIDE-CLEANUP:reaped/...）证明。
func (a *App) execRemote(ctx context.Context, command string, stdout, stderr io.Writer) (int, error) {
	runID := fmt.Sprintf("%d-%d", time.Now().UnixNano(), atomic.AddInt64(&remoteRunSeq, 1))
	pgidFile := "/tmp/.aide-remote-" + runID + ".pgid"
	// inner：先记录本组 PGID（$$ 即 setsid 出的会话/组长 PID），再跑用户命令，收尾删标记。
	inner := "echo $$ > " + shellQuote(pgidFile) + "; " + command + "; rm -f " + shellQuote(pgidFile)
	wrapped := "setsid bash -c " + shellQuote(inner)
	args := append([]string{"-S", sshControlSocket}, a.sshCommonArgs()...)
	args = append(args, a.sshTarget(), wrapped)
	cmd := exec.CommandContext(ctx, a.sshBin, args...)
	cmd.Env = []string{"PATH=/usr/local/bin:/usr/bin:/bin", "HOME=/home/aide"}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	// CommandContext 会在取消时终止本地 ssh 客户端；WaitDelay 防止失联的
	// ControlMaster/管道让 Wait 永久停住。
	cmd.WaitDelay = 2 * time.Second
	err := cmd.Run()
	if err == nil {
		return 0, nil
	}
	if ctx.Err() != nil {
		// 用户取消或超时：显式清除远端进程组（详见 cleanupRemoteGroup）。
		a.cleanupRemoteGroup(pgidFile)
		return -1, fmt.Errorf("远程命令已取消或超时")
	}
	return -1, fmt.Errorf("远程执行失败: %w", err)
}

// cleanupRemoteGroup 显式清除远端进程组：读标记文件里的 PGID，先 TERM 后 KILL
// 整个负进程组（kill -- -PGID，覆盖子孙），再复核组内是否还有存活进程，最后删标记。
// 返回形如 "AIDE-CLEANUP:reaped:1234" 的结果行（日志/取证用）；master 不可达时
// 返回空串。它使用独立 context，绝不复用已被取消的 ctx。
func (a *App) cleanupRemoteGroup(pgidFile string) string {
	if a.sshBin == "" {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()
	pgf := shellQuote(pgidFile)
	inner := "p=$(cat " + pgf + " 2>/dev/null); res=absent;" +
		"case \"$p\" in ''|*[!0-9]*) ;; *)" +
		"if kill -0 -- -\"$p\" 2>/dev/null; then" +
		"kill -TERM -- -\"$p\" 2>/dev/null; sleep 0.3; kill -KILL -- -\"$p\" 2>/dev/null; sleep 0.2;" +
		"if kill -0 -- -\"$p\" 2>/dev/null; then res=still-alive; else res=reaped; fi;" +
		"else res=already-gone; fi;; esac;" +
		"rm -f " + pgf + "; echo \"AIDE-CLEANUP:$res:$p\""
	remote := "bash -c " + shellQuote(inner)
	args := append([]string{"-S", sshControlSocket}, a.sshCommonArgs()...)
	args = append(args, a.sshTarget(), remote)
	cmd := exec.CommandContext(ctx, a.sshBin, args...)
	cmd.Env = []string{"PATH=/usr/local/bin:/usr/bin:/bin", "HOME=/home/aide"}
	var out strings.Builder
	cmd.Stdout = &out
	cmd.Stderr = &out
	cmd.WaitDelay = 2 * time.Second
	_ = cmd.Run()
	line := strings.TrimSpace(out.String())
	if i := strings.LastIndex(line, "AIDE-CLEANUP:"); i >= 0 {
		return strings.TrimSpace(line[i:])
	}
	return ""
}

// ── SFTP（经同一 master 会话，无需二次认证） ──

func (a *App) sftpArgs() []string {
	return []string{"-o", "ControlPath=" + sshControlSocket, "-o", "BatchMode=yes", "-o", "LogLevel=ERROR", "-P", fmt.Sprint(a.wsConfig.Workspace.Port)}
}

func (a *App) sftpBatch(batch string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), sftpTimeout)
	defer cancel()
	// SFTP is deliberately non-interactive, so it must reuse the authenticated
	// SSH control connection. Saving a workspace closes that connection; ensure
	// it is available before the first subsequent refresh or directory browse.
	if err := a.ensureSSHSession(ctx); err != nil {
		return "", err
	}
	args := append(a.sftpArgs(), "-b", "-", a.sshTarget())
	cmd := exec.CommandContext(ctx, a.sftpBin, args...)
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
func (a *App) sftpList(p string) ([]map[string]any, error) {
	return a.sftpListRemote(a.workspaceRemotePath(p), p)
}

// sftpListWorkspaceSource 通过当前工作区已验证的 SSH 会话列出自动系统文档目录。
// base 和 p 都是相对于远程工作区的路径，返回的 path 仅保留来源内相对路径。
func (a *App) sftpListWorkspaceSource(base, p string) ([]map[string]any, error) {
	remoteDir := a.workspaceRemotePath(pathJoinRemote(base, p))
	return a.sftpListRemote(remoteDir, p)
}

func (a *App) sftpListRemote(remoteDir, relativePath string) ([]map[string]any, error) {
	out, err := a.sftpBatch("cd " + shellQuoteRemote(remoteDir) + "\nls -l\n")
	if err != nil {
		return nil, err
	}
	return parseSFTPList(out, relativePath), nil
}

// OpenSSH sftp 在非交互 ls -l 中将非 ASCII UTF-8 字节打印为反斜杠八进制。
// 必须按字节还原后再做路径校验，否则中文文件被 safePath 的反斜杠检查误丢弃。
func decodeSFTPName(raw string) string {
	var decoded []byte
	for i := 0; i < len(raw); i++ {
		if raw[i] == '\\' && i+3 < len(raw) && raw[i+1] >= '0' && raw[i+1] <= '7' && raw[i+2] >= '0' && raw[i+2] <= '7' && raw[i+3] >= '0' && raw[i+3] <= '7' {
			n, _ := strconv.ParseUint(raw[i+1:i+4], 8, 8)
			decoded = append(decoded, byte(n))
			i += 3
			continue
		}
		decoded = append(decoded, raw[i])
	}
	if !utf8.Valid(decoded) {
		return ""
	}
	return string(decoded)
}

func parseSFTPList(out, relativePath string) []map[string]any {
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
		name := decodeSFTPName(strings.Join(fields[8:], " "))
		dir := fields[0][0] == 'd'
		if name == "" || name == "." || name == ".." || strings.Contains(name, "/") || strings.ContainsAny(name, "\x00\r\n\t") || safePath(name) != nil {
			continue
		}
		size, _ := strconv.ParseInt(fields[4], 10, 64)
		items = append(items, map[string]any{"name": name, "path": path.Join(relativePath, name), "dir": dir, "size": size, "modified": strings.Join(fields[5:8], " ")})
		if len(items) >= 2000 {
			break
		}
	}
	return items
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
	if err := a.sftpEnsureParents(remoteFile); err != nil {
		return err
	}
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

// sftpEnsureParents creates missing ancestors for a workspace write. SFTP has
// no mkdir -p, so probe one directory at a time and create only absent paths.
// This keeps atomic uploads working for first writes such as .cache/aide/x.py.
func (a *App) sftpEnsureParents(remoteFile string) error {
	parent := path.Dir(path.Clean(remoteFile))
	if parent == "." || parent == "/" {
		return nil
	}
	absolute := strings.HasPrefix(parent, "/")
	current := "."
	if absolute {
		current = "/"
	}
	for _, part := range strings.Split(strings.Trim(parent, "/"), "/") {
		if part == "" || part == "." {
			continue
		}
		current = path.Join(current, part)
		if a.sftpExists(current) {
			continue
		}
		if err := a.sftpMakeDirectory(current); err != nil {
			// A concurrent writer may have created it between the probe and mkdir.
			if a.sftpExists(current) {
				continue
			}
			return fmt.Errorf("SFTP 创建父目录 %q 失败: %w", current, err)
		}
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

func sftpCommandFailed(out string) bool {
	low := strings.ToLower(out)
	return strings.Contains(low, "couldn't") || strings.Contains(low, "failure") ||
		strings.Contains(low, "permission denied") || strings.Contains(low, "no such file") ||
		strings.Contains(low, "not a directory") || strings.Contains(low, "already exists")
}

// sftpMakeDirectory and sftpRenameDirectory are intentionally limited to
// directory-picker operations. Their callers validate relative or remote
// browse paths before composing these quoted SFTP commands.
func (a *App) sftpMakeDirectory(remotePath string) error {
	out, err := a.sftpBatch("mkdir " + shellQuoteRemote(remotePath) + "\n")
	if err != nil {
		return err
	}
	if sftpCommandFailed(out) {
		return fmt.Errorf("SFTP 创建文件夹失败: %s", strings.TrimSpace(out))
	}
	return nil
}

func (a *App) sftpRenameDirectory(oldPath, newPath string) error {
	if a.sftpExists(newPath) {
		return fmt.Errorf("已存在同名文件或文件夹")
	}
	out, err := a.sftpBatch("rename " + shellQuoteRemote(oldPath) + " " + shellQuoteRemote(newPath) + "\n")
	if err != nil {
		return err
	}
	if sftpCommandFailed(out) {
		return fmt.Errorf("SFTP 重命名文件夹失败: %s", strings.TrimSpace(out))
	}
	return nil
}

// sftpRemove removes one regular file or one empty directory. Recursive
// deletion is deliberately not supported by the file panel.
func (a *App) sftpRemove(remotePath string, dir bool) error {
	command := "rm "
	if dir {
		command = "rmdir "
	}
	out, err := a.sftpBatch(command + shellQuoteRemote(remotePath) + "\n")
	if err != nil {
		return err
	}
	if sftpCommandFailed(out) {
		return fmt.Errorf("SFTP 删除失败: %s", strings.TrimSpace(out))
	}
	return nil
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

// sftpConnIdentity 取决定 ControlMaster 连接对象的关键字段；任一变化即应重建会话。
func sftpConnIdentity(src Source) string {
	return fmt.Sprintf("%s|%d|%s|%s", src.Config.Host, src.Config.Port, src.Config.Username, src.Config.Auth)
}

// killSourceSession 关闭某来源的 ControlMaster 并清理其 socket/凭据临时文件
// （来源删除或连接参数变更时调用；清理失败静默忽略，绝不 panic）。
func (a *App) killSourceSession(id, target string) {
	if a.sshBin == "" {
		return
	}
	sock := sourceSocket(id)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	args := []string{"-S", sock, "-O", "exit"}
	if target != "" {
		args = append(args, target)
	}
	_ = exec.CommandContext(ctx, a.sshBin, args...).Run()
	_ = os.Remove(sock)
	_ = os.Remove(sock + ".askpass")
	_ = os.Remove(sock + ".key")
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
	secPassword, secKey := a.sourceCredentialLocked(src.ID)
	a.mu.Unlock()
	if src.Config.Auth == "key" && secKey != "" {
		keyPath := sock + ".key"
		if err := os.WriteFile(keyPath, []byte(secKey), 0600); err != nil {
			return err
		}
		// master 认证完成后（本函数返回时）立即删除：后续会话复用 ControlPath，
		// 私钥正文不再需要落盘；master 若掉线，下次 ensureSourceSession 会重写。
		defer os.Remove(keyPath)
		args = append([]string{"-i", keyPath}, args...)
	} else if src.Config.Auth == "password" && secPassword != "" {
		ask := sock + ".askpass"
		if err := os.WriteFile(ask, []byte("#!/bin/sh\necho "+shellQuote(secPassword)+"\n"), 0700); err != nil {
			return err
		}
		// master 认证完成后立即删除 askpass，口令不长期驻留 /tmp。
		defer os.Remove(ask)
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
	if ctx.Err() != nil {
		return "", fmt.Errorf("SFTP 操作超时")
	}
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
		name := decodeSFTPName(strings.Join(fields[8:], " "))
		if fields[0][0] == 'l' || name == "" || name == "." || name == ".." || strings.Contains(name, "/") || strings.ContainsAny(name, "\x00\r\n\t") || safePath(name) != nil {
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
