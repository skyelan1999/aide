package server

import (
	"context"
	"errors"
	"os"
	"path/filepath"
)

// Local tasks retain their captured root. A remote task cannot inherit a new
// workspace's host/user/port or approval context after a config revision.
func (a *App) taskSSHBindingLocked(task *Task) error {
	if a.workspaceMode() != "ssh" && (task == nil || task.WorkspaceMode != "ssh") {
		return nil
	}
	// Internal immediate commands have no persisted task identity. Persisted
	// tasks always have an ID and must carry a verifiable workspace binding.
	if task == nil || task.ID == "" && task.WorkspaceID == "" {
		return nil
	}
	if task.WorkspaceID == "" || task.WorkspaceID != a.wsID() || task.WorkspaceRev != a.wsRevision || task.WorkspaceMode != a.workspaceMode() {
		return errors.New("工作区已变化，请在原工作区重新创建或重试任务")
	}
	return nil
}

type taskSSHSession struct {
	bin, socket, target, directory, cache string
	port                                  int
	pinned                                bool
}

func (s *taskSSHSession) args(command string) []string {
	args := append([]string{"-S", s.socket}, sshConnectionArgs(s.socket, s.port)...)
	if s.pinned {
		// Require this exact authenticated master. If it is gone, never fall
		// back to creating a fresh connection (ProxyCommand is not used by mux).
		args = append(args, "-o", "ControlMaster=no", "-o", "BatchMode=yes", "-o", "ProxyCommand=false")
	}
	return append(args, s.target, command)
}

func pinTaskSSHSocket(socket string) (string, func(), error) {
	info, err := os.Lstat(socket)
	if err != nil || info.Mode()&os.ModeSocket == 0 {
		return "", nil, errors.New("原工作区 SSH 主连接不可用")
	}
	dir, err := os.MkdirTemp("", "aide-run-ssh-")
	if err != nil {
		return "", nil, err
	}
	alias := filepath.Join(dir, "master")
	release := func() { _ = os.Remove(alias); _ = os.Remove(dir) }
	if err := os.Link(socket, alias); err != nil {
		release()
		return "", nil, err
	}
	linked, err := os.Lstat(alias)
	if err != nil || !os.SameFile(info, linked) {
		release()
		return "", nil, errors.New("工作区 SSH 主连接已变化")
	}
	return alias, release, nil
}

func (a *App) taskSSHSession(ctx context.Context, task *Task) (*taskSSHSession, func(), error) {
	a.mu.Lock()
	if err := a.taskSSHBindingLocked(task); err != nil {
		a.mu.Unlock()
		return nil, nil, err
	}
	session := &taskSSHSession{bin: a.sshBin, socket: sshControlSocket, target: a.sshTarget(), port: a.wsConfig.Workspace.Port, directory: a.wsConfig.Workspace.Path, cache: a.workspaceRemoteCachePath()}
	if task != nil && task.WorkspaceRemotePath != "" {
		session.directory = task.WorkspaceRemotePath
	}
	bound := task != nil && (task.ID != "" || task.WorkspaceID != "")
	generation := a.sshSessionGeneration(sshControlSocket)
	a.mu.Unlock()
	if err := a.ensureSSHSession(ctx); err != nil {
		return nil, nil, err
	}
	release := func() {}
	if bound {
		unlock, err := a.lockSSHSession(ctx, sshControlSocket)
		if err != nil {
			return nil, nil, err
		}
		if generation != a.sshSessionGeneration(sshControlSocket) {
			unlock()
			return nil, nil, errors.New("工作区已变化，SSH 命令未运行")
		}
		alias, cleanup, err := pinTaskSSHSocket(sshControlSocket)
		unlock()
		if err != nil {
			return nil, nil, err
		}
		session.socket, session.pinned, release = alias, true, cleanup
	}
	a.mu.Lock()
	err := a.taskSSHBindingLocked(task)
	a.mu.Unlock()
	if err != nil || generation != a.sshSessionGeneration(sshControlSocket) {
		release()
		return nil, nil, errors.New("工作区已变化，SSH 命令未运行")
	}
	return session, release, nil
}
