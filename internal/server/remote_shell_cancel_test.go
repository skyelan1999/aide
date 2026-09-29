package server

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestRemoteShellCommandCancellable 覆盖 SSH 工作区的停止路径。替身把真正的远端
// 执行替换为 exec sleep，确保取消 run context 时本地 SSH 客户端不会无限等待。
func TestRemoteShellCommandCancellable(t *testing.T) {
	a := testApp(t)
	stub := filepath.Join(t.TempDir(), "ssh-stub")
	if err := os.WriteFile(stub, []byte(`#!/bin/sh
for arg in "$@"; do
  [ "$arg" = "-O" ] && exit 0
done
# 取消后的远端进程组回收是一条带 AIDE-CLEANUP 标记的附加 ssh 调用；
# 桩环境里没有真正的远端进程，立即返回即可。
case "$*" in
  *AIDE-CLEANUP*) exit 0 ;;
esac
exec sleep 30
`), 0700); err != nil {
		t.Fatal(err)
	}
	a.sshBin = stub
	requireStatus(t, request(a, "PUT", "/api/workspace-config", map[string]any{
		"workspace": map[string]any{"mode": "ssh", "path": "/srv/app", "host": "remote.example", "port": 22, "username": "deploy", "auth": "none"},
	}), 200)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	start := time.Now()
	go func() {
		_, _, err := a.execShellCommand(ctx, &Task{WorkspaceMode: "ssh", WorkspaceRemotePath: "/srv/app"}, "sleep 30")
		done <- err
	}()
	time.Sleep(300 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("取消后的远程命令不应报告成功")
		}
		if elapsed := time.Since(start); elapsed > 5*time.Second {
			t.Fatalf("远程命令未被及时取消，耗时 %v", elapsed)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("取消后远程 SSH 客户端仍未停止")
	}
}
