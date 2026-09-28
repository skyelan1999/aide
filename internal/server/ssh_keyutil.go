package server

// ── SSH 私钥指纹与受限文件读取 ──────────────────────────────────────────────
// 指纹：调用本机 ssh-keygen -lf 解析私钥并输出公钥 SHA256 指纹（OpenSSH 标准格式
// "SHA256:base64"）。构建/运行镜像均含 openssh-client（ssh/sftp/ssh-keygen 同处）。
// 私钥正文绝不回显到前端、绝不写入日志；这里只返回指纹元数据。

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// sshKeygenBin 可注入测试替身；默认 ssh-keygen。
var sshKeygenBin = "ssh-keygen"

var sha256FpRe = regexp.MustCompile(`SHA256:[A-Za-z0-9+/=]+`)

// sshKeyFingerprint 对一段私钥材料计算公钥 SHA256 指纹。
// keyMaterial 为 PEM/OpenSSH 私钥正文；passphrase 为空表示无口令。
// 实现：写入 0600 临时文件 → ssh-keygen -lf（-P 传口令，非交互）→ 解析 → 删除临时文件。
func sshKeyFingerprint(keyMaterial []byte, passphrase string) (string, error) {
	if len(keyMaterial) == 0 {
		return "", errors.New("私钥内容为空")
	}
	// 受控临时目录：与临时密钥文件同处 /data/secrets/.tmp（由调用方保证存在或退回系统临时目录）。
	tmp, err := os.CreateTemp("", "aide-keyfp-*")
	if err != nil {
		return "", err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := tmp.Write(keyMaterial); err != nil {
		tmp.Close()
		return "", err
	}
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return "", err
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}
	return sshKeygenFingerprintFile(tmpPath, passphrase)
}

// sshKeygenFingerprintFile 对一个私钥文件路径取指纹（ref 模式下直接引用，无需写临时文件）。
// passphrase 非空时，经 SSH_ASKPASS 助手从继承的管道 fd 读取口令：口令既不出现在子进程
// argv（不再用 -P），也不写成明文临时文件。
func sshKeygenFingerprintFile(path, passphrase string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	args := []string{"-lf", path}
	cmd := exec.CommandContext(ctx, sshKeygenBin, args...)
	if passphrase != "" {
		cleanup, err := attachPassphraseAskpass(cmd, passphrase)
		if err != nil {
			return "", err
		}
		defer cleanup()
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("解析私钥失败（非 OpenSSH 格式或口令错误）: %s", strings.TrimSpace(string(out)))
	}
	return parseSHA256Fingerprint(string(out))
}

// attachPassphraseAskpass 让 cmd 通过 SSH_ASKPASS 从继承的管道 fd 读取口令：
//   - 口令写入一把管道并通过 cmd.ExtraFiles 把读端继承为子进程 fd 3；
//   - 落地一个 0700 的 askpass 助手脚本（内容仅 "cat <&3"，不含任何口令材料），
//     由 SSH_ASKPASS 指向它；SSH_ASKPASS_REQUIRE=force 强制非交互调用。
//
// 返回的 cleanup 关闭管道读端并删除助手临时目录。口令因此不进 cmd.Args、不进环境变量、
// 也不写成明文文件。
func attachPassphraseAskpass(cmd *exec.Cmd, passphrase string) (func(), error) {
	pr, pw, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	if _, err := pw.WriteString(passphrase); err != nil {
		pr.Close()
		pw.Close()
		return nil, err
	}
	_ = pw.Close() // 写完即关：子进程 cat <&3 读到口令后遇 EOF 退出

	tmpDir, err := os.MkdirTemp("", "aide-askpass-*")
	if err != nil {
		pr.Close()
		return nil, err
	}
	askPath := filepath.Join(tmpDir, "askpass.sh")
	// 助手脚本：从继承的 fd 3 读口令并打印到 stdout。脚本本身无敏感内容。
	if err := os.WriteFile(askPath, []byte("#!/bin/sh\ncat <&3\n"), 0o700); err != nil {
		pr.Close()
		os.RemoveAll(tmpDir)
		return nil, err
	}

	cmd.Env = append(os.Environ(),
		"SSH_ASKPASS="+askPath,
		"SSH_ASKPASS_REQUIRE=force",
		"DISPLAY=aide:0",
	)
	// ExtraFiles 从 fd 3 开始继承（0/1/2 为 stdin/stdout/stderr）。
	cmd.ExtraFiles = []*os.File{pr}

	cleanup := func() {
		_ = pr.Close()
		_ = os.RemoveAll(tmpDir)
	}
	return cleanup, nil
}

// parseSHA256Fingerprint 从 ssh-keygen -lf 输出中提取 SHA256:... 令牌。
// 输出形如："256 SHA256:xxxx comment (ED25519)" 或 "3072 SHA256:yyyy comment (RSA)"。
func parseSHA256Fingerprint(out string) (string, error) {
	m := sha256FpRe.FindString(out)
	if m == "" {
		return "", fmt.Errorf("未在 ssh-keygen 输出中找到指纹: %q", strings.TrimSpace(out))
	}
	return m, nil
}

// readKeyFileInRoots 在校验过的挂载根（/workspace|/context|/local）内读取私钥文件内容。
// 复用 resolveHostPath 做边界与路径遍历防护；越权/逃逸返回错误。
func (a *App) readKeyFileInRoots(containerPath string) ([]byte, error) {
	realPath, _, err := a.resolveHostPath(containerPath)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(realPath)
	if err != nil {
		return nil, fmt.Errorf("私钥文件不可访问: %w", err)
	}
	if info.IsDir() {
		return nil, errors.New("所选路径是目录，不是私钥文件")
	}
	// 防护：禁止读取目录内显式拒绝的非常规文件（设备/管道等），仅允许常规文件。
	if info.Mode().IsRegular() == false {
		return nil, errors.New("所选文件不是常规文件")
	}
	b, err := os.ReadFile(realPath)
	if err != nil {
		return nil, fmt.Errorf("读取私钥失败: %w", err)
	}
	if len(b) == 0 {
		return nil, errors.New("私钥文件为空")
	}
	return b, nil
}

// ensureSecretsTmpDir 确保受控临时目录存在（/data/secrets/.tmp, 0700）。失败时返回系统临时目录回退。
func (a *App) ensureSecretsTmpDir() string {
	dir := filepath.Join(VaultDir(a.dataPath), ".tmp")
	if err := os.MkdirAll(dir, 0o700); err == nil {
		return dir
	}
	return os.TempDir()
}
