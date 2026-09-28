package server

// ssh-keygen 口令传递（finding backend-integrations-004 / P2 #4）单元测试：
//   - 带口令的私钥：用正确口令（经 SSH_ASKPASS+管道 fd 传递）能取到指纹 → 证明口令确实解锁了密钥；
//     错误口令必须失败（负对照，证明取到指纹不是侥幸）。
//   - 子进程 argv 不含口令：用替身 ssh-keygen 脚本转储自身 argv 断言。
//
// 说明：x/crypto/ssh 未进 vendor，故"私钥可用口令解锁"直接以 ssh-keygen -lf 成功/失败为证据
//（ssh-keygen 本身就是口令的消费者），不新增依赖。

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// genPassphraseKey 用 ssh-keygen 生成一把带口令的 ed25519 私钥（仅测试 setup）。
// 返回私钥路径。
func genPassphraseKey(t *testing.T, dir, passphrase string) string {
	t.Helper()
	keyPath := filepath.Join(dir, "id_ed25519_test")
	cmd := exec.Command("ssh-keygen", "-t", "ed25519", "-N", passphrase, "-f", keyPath, "-q")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("无法生成带口令测试密钥: %v %s", err, out)
	}
	return keyPath
}

// (a) 正确口令（经 askpass 管道传递）能真正解密私钥：用 ssh-keygen -y（必须用口令解锁私钥
// 才能导出公钥）作为证据；错误口令必须失败。同时生产函数 sshKeygenFingerprintFile 能取到指纹。
func TestSshKeygenPassphraseViaAskpass(t *testing.T) {
	if _, err := exec.LookPath("ssh-keygen"); err != nil {
		t.Skip("ssh-keygen 不可用，跳过")
	}
	const pass = "correct-passphrase-xyz"
	keyPath := genPassphraseKey(t, t.TempDir(), pass)

	// 生产函数：带口令取指纹（-lf 不强制解密，但走 askpass 路径不应报错）。
	fp, err := sshKeygenFingerprintFile(keyPath, pass)
	if err != nil {
		t.Fatalf("正确口令应能取到指纹: %v", err)
	}
	if !strings.HasPrefix(fp, "SHA256:") {
		t.Fatalf("指纹=%q, want SHA256: 前缀", fp)
	}

	// 真正的解密证据：ssh-keygen -y 必须用口令解开私钥才能导出公钥。
	derivePub := func(pw string) (string, error) {
		cmd := exec.Command("ssh-keygen", "-y", "-f", keyPath)
		cleanup, err := attachPassphraseAskpass(cmd, pw)
		if err != nil {
			return "", err
		}
		defer cleanup()
		out, err := cmd.CombinedOutput()
		return strings.TrimSpace(string(out)), err
	}
	pub, err := derivePub(pass)
	if err != nil {
		t.Fatalf("正确口令应能解密私钥导出公钥: %v", err)
	}
	if !strings.HasPrefix(pub, "ssh-ed25519 ") {
		t.Fatalf("导出公钥=%q", pub)
	}
	// 负对照：错误口令必须无法解密。
	if _, err := derivePub("wrong-passphrase-zzz"); err == nil {
		t.Fatal("错误口令应无法解密私钥")
	}
}

// (b) 子进程 argv 不含口令：替身 ssh-keygen 把自身 argv 转储到文件，断言其中无口令。
func TestSshKeygenArgvLeakage(t *testing.T) {
	const pass = "argv-leak-passphrase-abc"
	dir := t.TempDir()
	argvDump := filepath.Join(dir, "argv.txt")
	stub := filepath.Join(dir, "ssh-keygen")
	// 替身：把所有 argv 逐个写到 dump 文件，再输出一个合法指纹行，让调用方走通。
	script := "#!/bin/sh\n" +
		"printf '%s\\n' \"$@\" > " + shellQuote(argvDump) + "\n" +
		"echo '256 SHA256:stubfp comment (ED25519)'\n"
	if err := os.WriteFile(stub, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}

	origBin := sshKeygenBin
	sshKeygenBin = stub
	defer func() { sshKeygenBin = origBin }()

	if _, err := sshKeygenFingerprintFile(filepath.Join(dir, "anykey"), pass); err != nil {
		t.Fatalf("替身应返回指纹: %v", err)
	}
	dumped, err := os.ReadFile(argvDump)
	if err != nil {
		t.Fatalf("未捕获 argv: %v", err)
	}
	if strings.Contains(string(dumped), pass) {
		t.Fatalf("口令泄露到子进程 argv: %s", dumped)
	}
	// 同时确认没有用 -P 传口令（旧实现的特征）。
	if strings.Contains(string(dumped), "-P") {
		t.Fatalf("argv 仍含 -P: %s", dumped)
	}
}
