package server

import (
	"strings"
	"testing"
)

// #61：run_shell 路径白名单（防呆）。B=/local/Users/skyelan/debug 为工程目录，A=/workspace 为产品目录。
func TestShellPathGuard(t *testing.T) {
	const B = "/local/Users/skyelan/debug"
	cases := []struct {
		name    string
		command string
		wantBad bool
	}{
		// 允许：工程目录内相对/绝对路径
		{"relative ls", "ls -la", false},
		{"relative grep", "grep -rn foo .", false},
		{"inside B abs", "cat /local/Users/skyelan/debug/README.md", false},
		{"touch inside B", "echo x > out.txt", false},
		// 拒绝：敏感 dotfile
		{"read .ssh", "cat /local/Users/skyelan/.ssh/config", true},
		{"read .kube", "cat /local/Users/skyelan/.kube/config", true},
		{"read .zsh_history", "tail /local/Users/skyelan/.zsh_history", true},
		// 拒绝：写产品目录 A（B 不是 A 时）
		{"write /workspace", "echo x > /workspace/evil.txt", true},
		{"touch /workspace", "touch /workspace/foo", true},
		// 拒绝：配置目录
		{"read /data", "cat /data/settings.json", true},
		// 拒绝：B 之外的 /local
		{"write outside B", "echo x > /local/Users/skyelan/other.txt", true},
		{"ls outside B home", "ls /local/Users/skyelan/", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, bad := shellPathGuard(c.command, B)
			if bad != c.wantBad {
				t.Fatalf("shellPathGuard(%q) bad=%v want %v", c.command, bad, c.wantBad)
			}
		})
	}
}

// #61：默认工作区 B==/workspace 时，不应误拦对 /workspace 的常规操作（向后兼容）。
func TestShellPathGuardDefaultWorkspaceTolerant(t *testing.T) {
	const B = "/workspace"
	for _, cmd := range []string{"ls", "go build ./...", "echo x > out.txt", "cat README.md"} {
		if _, bad := shellPathGuard(cmd, B); bad {
			t.Fatalf("默认工作区模式误拦 %q", cmd)
		}
	}
	// 即使默认模式，敏感 dotfile 仍拦
	if _, bad := shellPathGuard("cat /local/Users/skyelan/.ssh/id_rsa", B); !bad {
		t.Fatalf("默认模式仍应拦敏感 dotfile")
	}
}

// #61：ssh 模式 containerAbs 为空，不粗筛。
func TestShellPathGuardSSHNoop(t *testing.T) {
	if _, bad := shellPathGuard("cat /etc/passwd", ""); bad {
		t.Fatal("空 containerAbs 应跳过粗筛")
	}
}

// #61：系统提示行新行为报容器内绝对路径并禁止写 /workspace；legacy 回退宿主路径。
func TestCwdPromptLine(t *testing.T) {
	a := testApp(t)
	a.mu.Lock()
	a.workspaceDisplay = "/Users/skyelan/debug"
	a.containerAbs = "/local/Users/skyelan/debug"
	line := a.cwdPromptLineLocked()
	a.mu.Unlock()
	if !strings.Contains(line, "容器内绝对路径)=/local/Users/skyelan/debug") {
		t.Fatalf("新提示应报容器内绝对路径，got: %s", line)
	}
	if !strings.Contains(line, "禁止使用 /workspace") {
		t.Fatalf("新提示应禁止写 /workspace，got: %s", line)
	}

	a.mu.Lock()
	a.settings.AgentCWDMode = "legacy"
	lineLegacy := a.cwdPromptLineLocked()
	a.mu.Unlock()
	if !strings.Contains(lineLegacy, "当前工作目录: /Users/skyelan/debug") {
		t.Fatalf("legacy 应回退宿主路径，got: %s", lineLegacy)
	}
	if strings.Contains(lineLegacy, "容器内绝对路径") {
		t.Fatalf("legacy 不应出现新提示，got: %s", lineLegacy)
	}
}

// #61：任务快照 AgentRoot 在子 agent 继承时整体复制。
func TestTaskAgentRootSnapshot(t *testing.T) {
	parent := &Task{AgentRoot: AgentRoot{
		DisplayHost:  "/Users/skyelan/debug",
		ContainerAbs: "/local/Users/skyelan/debug",
		ID:           "local|/Users/skyelan/debug|",
	}}
	sub := &Task{AgentRoot: parent.AgentRoot}
	if sub.AgentRoot.ContainerAbs != parent.AgentRoot.ContainerAbs {
		t.Fatalf("子 agent 应继承父任务 ContainerAbs")
	}
	// taskContainerAbs 优先取任务快照
	if got := (&App{}).taskContainerAbs(parent); got != parent.AgentRoot.ContainerAbs {
		t.Fatalf("taskContainerAbs 应返回快照，got %q", got)
	}
}
