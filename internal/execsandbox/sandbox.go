// Package execsandbox starts a fresh restricted process before executing a shell.
package execsandbox

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

const childFlag = "--aide-sandbox-child-v1"

type policy struct {
	Mode      string
	Workspace string
	Temporary string
}

// The same entrypoint works in the application and Go test executables. Only
// this fresh helper process restricts itself; the server never applies Landlock.
func init() {
	if len(os.Args) < 2 || os.Args[1] != childFlag {
		return
	}
	if len(os.Args) != 4 {
		fmt.Fprintln(os.Stderr, "invalid sandbox child invocation")
		os.Exit(125)
	}
	var p policy
	if json.Unmarshal([]byte(os.Args[2]), &p) != nil {
		os.Exit(125)
	}
	if err := runChild(p, os.Args[3]); err != nil {
		fmt.Fprintln(os.Stderr, "沙箱启动失败，命令未执行:", err)
		os.Exit(125)
	}
	os.Exit(125)
}

// Command's temporary directory must live until cmd.Wait returns.
func Command(ctx context.Context, mode, workspace, command string) (*exec.Cmd, func(), error) {
	if mode == "danger-full-access" {
		return exec.CommandContext(ctx, "bash", "--noprofile", "--norc", "-c", command), func() {}, nil
	}
	if mode != "read-only" && mode != "workspace-write" {
		return nil, nil, fmt.Errorf("未知沙箱模式 %q", mode)
	}
	if err := Available(); err != nil {
		return nil, nil, err
	}
	root, err := filepath.EvalSymlinks(workspace)
	if err != nil {
		return nil, nil, err
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return nil, nil, err
	}
	if root == string(filepath.Separator) {
		return nil, nil, fmt.Errorf("受限沙箱不能以文件系统根目录作为工作区")
	}
	tmp, err := os.MkdirTemp("", "aide-command-")
	if err != nil {
		return nil, nil, err
	}
	cleanup := func() { _ = os.RemoveAll(tmp) }
	for _, dir := range []string{"home", "cache", "go"} {
		if err = os.MkdirAll(filepath.Join(tmp, dir), 0700); err != nil {
			cleanup()
			return nil, nil, err
		}
	}
	executable, err := os.Executable()
	if err != nil {
		cleanup()
		return nil, nil, err
	}
	encoded, _ := json.Marshal(policy{Mode: mode, Workspace: root, Temporary: tmp})
	cmd := exec.CommandContext(ctx, executable, childFlag, string(encoded), command)
	return cmd, cleanup, nil
}
