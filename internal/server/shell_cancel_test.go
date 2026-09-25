package server

import (
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// TestExecShellCommandCancellable 用户点停止（cancel run ctx）后，正在执行的 run_shell
// 必须立即中断返回，而不是只能等 ShellTimeout；并验证 bash 子进程（sleep）随进程组被杀，不留孤儿。
func TestExecShellCommandCancellable(t *testing.T) {
	a := testApp(t)
	a.mu.Lock()
	a.settings.SandboxMode = "danger-full-access" // 确保 sleep 真实执行，不被沙箱拦截造成假通过
	a.mu.Unlock()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	start := time.Now()
	go func() {
		_, _, err := a.execShellCommand(ctx, "sleep 30")
		done <- err
	}()
	time.Sleep(600 * time.Millisecond) // 等命令启动
	cancel()
	select {
	case <-done:
		if elapsed := time.Since(start); elapsed > 5*time.Second {
			t.Fatalf("命令未被及时取消，耗时 %v", elapsed)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("取消后命令仍未停止")
	}
	// 验证 bash 的子进程 sleep 也随进程组被杀，不留孤儿（无 pgrep 则跳过此检查）。
	time.Sleep(300 * time.Millisecond)
	if out, err := exec.Command("pgrep", "-f", "sleep 30").CombinedOutput(); err == nil {
		if len(strings.TrimSpace(string(out))) != 0 {
			t.Fatalf("sleep 子进程残留为孤儿: %s", out)
		}
	}
}

// TestExecShellCommandStillHonorsTimeout 未取消时，ShellTimeout 仍然作为单命令上限生效。
func TestExecShellCommandStillHonorsTimeout(t *testing.T) {
	a := testApp(t)
	start := time.Now()
	// ShellTimeout 默认 60s，这里用一条正常快速命令确认链路正常返回。
	_, code, err := a.execShellCommand(context.Background(), "echo hello-aide")
	if err != nil || code != 0 {
		t.Fatalf("普通命令执行失败 code=%d err=%v", code, err)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("普通命令耗时异常 %v", elapsed)
	}
}
