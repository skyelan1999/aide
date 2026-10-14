package server

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// SSH necessarily transports a shell command. Both scripts are fixed; user paths
// are separately quoted positional arguments, never interpolated shell code.
// env -i strips inherited Git routing/config variables and exported functions.
const gitHistoryRemoteEnv = "exec env -i PATH=/usr/local/bin:/usr/bin:/bin LC_ALL=C GIT_CONFIG_NOSYSTEM=1 GIT_CONFIG_GLOBAL=/dev/null GIT_TERMINAL_PROMPT=0 GIT_OPTIONAL_LOCKS=0 GIT_NO_LAZY_FETCH=1 GIT_NO_REPLACE_OBJECTS=1 GIT_ALLOW_PROTOCOL= "
const gitHistoryRemotePathCheck = `
cd -P -- "$1" || exit 23
base=$PWD
cur=$base
rest=$2
while [ -n "$rest" ]; do
 part=${rest%%/*}
 cur=$cur/$part
 [ ! -L "$cur" ] || exit 22
 case "$rest" in */*) rest=${rest#*/} ;; *) rest= ;; esac
done
`
const gitHistoryRemoteDiscover = gitHistoryRemotePathCheck + `
command -v git >/dev/null 2>&1 || exit 20
file=$base/$2
dir=${file%/*}
while :; do
 if [ -e "$dir/.git" ]; then
  top=$(git --no-pager -c safe.directory="$dir" -c core.fsmonitor=false -c core.hooksPath=/dev/null -C "$dir" rev-parse --show-toplevel) || exit 23
  [ "$top" = "$dir" ] || exit 23
  rel=${file#"$dir/"}
  printf '%s\000%s\000%s\000' "$base" "$dir" "$rel"
  exit 0
 fi
 [ "$dir" != "$base" ] || exit 21
 dir=${dir%/*}
 case "$dir/" in "$base/"*) ;; *) exit 21 ;; esac
done
`
const gitHistoryRemoteRead = gitHistoryRemotePathCheck + `
file=$base/$2
[ -e "$file" ] || exit 44
[ -f "$file" ] || exit 45
exec head -c 2097153 -- "$file"
`

type gitHistorySSH struct {
	app                          *App
	bin, target, workspace, base string
	args                         []string
	generation                   uint64
}

func (s gitHistorySSH) unchanged() bool {
	s.app.mu.Lock()
	defer s.app.mu.Unlock()
	return s.app.wsID() == s.workspace && s.app.sshSessionGeneration(sshControlSocket) == s.generation
}

func (s gitHistorySSH) run(ctx context.Context, limit int64, command string) ([]byte, error) {
	if !s.unchanged() {
		return nil, errors.New("SSH 工作区已切换，请重新打开文件历史")
	}
	// ensureSSHSession acquires this same lifecycle gate, so establish first.
	if err := s.app.ensureSSHSession(ctx); err != nil {
		return nil, fmt.Errorf("SSH Git 连接失败: %w", err)
	}
	unlock, err := s.app.lockSSHSession(ctx, sshControlSocket)
	if err != nil {
		return nil, err
	}
	defer unlock()
	if !s.unchanged() {
		return nil, errors.New("SSH 工作区已切换，请重新打开文件历史")
	}
	args := append(append([]string(nil), s.args...), s.target, command)
	cmd := exec.CommandContext(ctx, s.bin, args...)
	cmd.Env = []string{"PATH=/usr/local/bin:/usr/bin:/bin", "HOME=/home/aide"}
	output := &knowledgeLimitWriter{max: limit, stop: func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
	}}
	cmd.Stdout = output
	cmd.WaitDelay = time.Second
	err = cmd.Run()
	if !s.unchanged() {
		return nil, errors.New("SSH 工作区已切换，请重新打开文件历史")
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if output.overflow {
		return nil, errors.New("SSH Git 比较超过读取上限")
	}
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) && exit.ExitCode() == 255 {
			return nil, errors.New("SSH Git 连接中断或远端拒绝执行")
		}
	}
	return output.buf.Bytes(), err
}

func remoteGitScript(script, base, p string) string {
	return gitHistoryRemoteEnv + "sh -c " + shellQuote(script) + " aide-git " + shellQuote(base) + " " + shellQuote(p)
}

func (a *App) findSSHGitHistoryRepo(ctx context.Context, p string) (*gitHistoryRepo, string, error) {
	a.mu.Lock()
	if a.wsConfig.Workspace.Host == "" {
		a.mu.Unlock()
		return nil, "未配置 SSH 远程主机。", nil
	}
	s := gitHistorySSH{app: a, bin: a.sshBin, target: a.sshTarget(), workspace: a.wsID(), base: a.wsConfig.Workspace.Path, args: append([]string{"-T", "-S", sshControlSocket}, a.sshCommonArgs()...), generation: a.sshSessionGeneration(sshControlSocket)}
	a.mu.Unlock()
	if s.base == "" {
		return nil, "", errors.New("SSH 工作区目录未配置")
	}
	// Patch generation stays local and never invokes remote diff hooks or filters.
	bin, err := exec.LookPath("git")
	if err != nil {
		return nil, "本机运行环境未安装 Git，无法生成比较补丁。", nil
	}
	raw, err := s.run(ctx, 16384, remoteGitScript(gitHistoryRemoteDiscover, s.base, p))
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			switch exit.ExitCode() {
			case 20:
				return nil, "SSH 远端未安装 Git。", nil
			case 21:
				return nil, "当前目录不属于 SSH 工作区内的 Git 仓库。", nil
			case 22:
				return nil, "", errors.New("Git 比较不支持符号链接路径")
			case 23:
				return nil, "", errors.New("无法读取 SSH 工作区目录或 Git 仓库")
			}
		}
		return nil, "", err
	}
	fields := bytes.Split(raw, []byte{0})
	if len(fields) != 4 || len(fields[0]) == 0 || len(fields[1]) == 0 || len(fields[2]) == 0 {
		return nil, "", errors.New("SSH Git 仓库信息无效")
	}
	g := &gitHistoryRepo{bin: bin, base: string(fields[0]), dir: string(fields[1]), path: string(fields[2])}
	g.remoteCommand = func(ctx context.Context, limit int64, args ...string) ([]byte, error) {
		argv := []string{"git", "--no-pager", "--literal-pathspecs", "-c", "safe.directory=" + g.dir, "-c", "core.fsmonitor=false", "-c", "core.hooksPath=/dev/null", "-C", g.dir}
		argv = append(argv, args...)
		quoted := make([]string, len(argv))
		for i, arg := range argv {
			quoted[i] = shellQuote(arg)
		}
		return s.run(ctx, limit, gitHistoryRemoteEnv+strings.Join(quoted, " "))
	}
	g.remoteRead = func(ctx context.Context) ([]byte, error) {
		raw, err := s.run(ctx, gitHistoryTextLimit+1, remoteGitScript(gitHistoryRemoteRead, s.base, p))
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			switch exit.ExitCode() {
			case 44:
				return nil, os.ErrNotExist
			case 22:
				return nil, errors.New("Git 比较不支持符号链接路径")
			case 45:
				return nil, errors.New("只支持普通文本文件")
			}
		}
		return raw, err
	}
	return g, "", nil
}
