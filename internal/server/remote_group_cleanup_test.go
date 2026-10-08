package server

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

// writeSSHCleanupStub 生成一个 ssh 替身：把每次调用的全部 argv 落到 logPath，
// -O check 立即成功；带 AIDE-CLEANUP 标记的回收调用立即输出假复核行；
// 真正的远程执行模拟为长睡眠，便于中途取消/超时。
func writeSSHCleanupStub(t *testing.T, logPath string) string {
	t.Helper()
	stub := filepath.Join(t.TempDir(), "ssh-stub")
	script := "#!/bin/bash\n" +
		"echo \"=== invocation $(date +%s.%N) ===\" >> " + logPath + "\n" +
		"printf '%s\\n' \"$@\" >> " + logPath + "\n" +
		"for arg in \"$@\"; do [ \"$arg\" = \"-O\" ] && exit 0; done\n" +
		"case \"$*\" in *AIDE-CLEANUP*) echo \"AIDE-CLEANUP:reaped:1234\" >> " + logPath + "; echo \"AIDE-CLEANUP:reaped:1234\"; exit 0 ;; esac\n" +
		"case \"$*\" in *\"sleep 30\"*) exec sleep 30 ;; esac\n" +
		"exit 0\n"
	if err := os.WriteFile(stub, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	return stub
}

func sshCleanupTestApp(t *testing.T) *App {
	a := testApp(t)
	requireStatus(t, request(a, "PUT", "/api/workspace-config", map[string]any{
		"workspace": map[string]any{"mode": "ssh", "path": "/srv/app", "host": "remote.example", "port": 22, "username": "deploy", "auth": "none"},
	}), 200)
	return a
}

// TestRemoteCommandWrappedInOwnPG 正常执行时：远端命令必须被包进 setsid 独立会话，
// 且把本组 PGID 写入标记文件；正常结束不应触发额外的回收调用。
func TestRemoteCommandWrappedInOwnPG(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "ssh.log")
	a := sshCleanupTestApp(t)
	a.sshBin = writeSSHCleanupStub(t, logPath)

	_, code, err := a.execShellCommand(context.Background(), &Task{WorkspaceMode: "ssh", WorkspaceRemotePath: "/srv/app"}, "echo hi")
	if err != nil || code != 0 {
		t.Fatalf("正常远程命令失败 code=%d err=%v", code, err)
	}
	log := readFileString(t, logPath)
	if !strings.Contains(log, "setsid -w bash -c") {
		t.Fatalf("远端命令未包进 setsid 独立会话:\n%s", log)
	}
	if !strings.Contains(log, "/tmp/.aide-remote-") {
		t.Fatalf("远端命令未写入 PGID 标记文件:\n%s", log)
	}
	if strings.Contains(log, "AIDE-CLEANUP") {
		t.Fatalf("正常结束不应触发进程组回收调用:\n%s", log)
	}
}

// TestRemoteCancelKillsRemoteGroup 用户取消：除了杀掉本地 ssh，必须发起一条
// 带 AIDE-CLEANUP 标记的回收调用，且其指向的 PGID 标记文件与启动时一致。
func TestRemoteCancelKillsRemoteGroup(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "ssh.log")
	a := sshCleanupTestApp(t)
	a.sshBin = writeSSHCleanupStub(t, logPath)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, _, err := a.execShellCommand(ctx, &Task{WorkspaceMode: "ssh", WorkspaceRemotePath: "/srv/app"}, "sleep 30")
		done <- err
	}()
	time.Sleep(500 * time.Millisecond) // 等远端命令启动（stub 进入 sleep 30）
	cancel()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("取消后远程命令未返回")
	}

	log := readFileString(t, logPath)
	// 启动包裹里写入的标记文件路径（形如 /tmp/.aide-remote-<n>-<n>.pgid）
	re := regexp.MustCompile(`/tmp/\.aide-remote-[0-9]+-[0-9]+\.pgid`)
	matches := re.FindAllString(log, -1)
	if len(matches) == 0 {
		t.Fatalf("日志中找不到 PGID 标记:\n%s", log)
	}
	marker := matches[0]
	// 回收调用必须出现，且指向同一个标记文件
	if !strings.Contains(log, "AIDE-CLEANUP") {
		t.Fatalf("取消后未发起远端进程组回收调用:\n%s", log)
	}
	if !strings.Contains(log, marker) {
		t.Fatalf("回收调用未复用启动时的 PGID 标记 %q:\n%s", marker, log)
	}
	if !strings.Contains(log, "reaped:1234") {
		t.Fatalf("回收调用未返回复核结果行:\n%s", log)
	}
}

// TestRemoteTimeoutKillsRemoteGroup 超时路径与取消路径走同一个回收分支。
func TestRemoteTimeoutKillsRemoteGroup(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "ssh.log")
	a := sshCleanupTestApp(t)
	a.sshBin = writeSSHCleanupStub(t, logPath)
	a.mu.Lock()
	a.settings.ShellTimeout = 2 // 2s 后超时
	a.mu.Unlock()

	start := time.Now()
	_, _, err := a.execShellCommand(context.Background(), &Task{WorkspaceMode: "ssh", WorkspaceRemotePath: "/srv/app"}, "sleep 30")
	if err == nil {
		t.Fatal("超时应返回错误")
	}
	if elapsed := time.Since(start); elapsed > 10*time.Second {
		t.Fatalf("超时后未及时返回，耗时 %v", elapsed)
	}
	log := readFileString(t, logPath)
	if !strings.Contains(log, "AIDE-CLEANUP") {
		t.Fatalf("超时后未发起远端进程组回收调用:\n%s", log)
	}
}

func readFileString(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
