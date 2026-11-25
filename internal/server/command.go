package server

import (
	"aide/internal/execsandbox"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

type streamWriter struct {
	mu        sync.Mutex
	w         http.ResponseWriter
	total     int
	truncated bool
}

func (s *streamWriter) event(v any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	_ = json.NewEncoder(s.w).Encode(v)
	if f, ok := s.w.(http.Flusher); ok {
		f.Flush()
	}
}
func (s *streamWriter) Write(b []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := len(b)
	if s.total < 128<<10 {
		if len(b) > (128<<10)-s.total {
			b = b[:(128<<10)-s.total]
		}
		s.total += len(b)
		_ = json.NewEncoder(s.w).Encode(map[string]any{"type": "output", "text": string(b)})
		if f, ok := s.w.(http.Flusher); ok {
			f.Flush()
		}
	}
	if s.total >= 128<<10 && !s.truncated {
		s.truncated = true
		_ = json.NewEncoder(s.w).Encode(map[string]string{"type": "output", "text": "\n[输出已截断：128 KB]\n"})
	}
	return n, nil
}

// standaloneCD recognizes only a complete, literal cd builtin. Compound shell
// commands and expansions retain normal shell behavior and do not change the
// command panel's persistent working directory.
func standaloneCD(command string) (string, bool) {
	var args []string
	var word strings.Builder
	var quote rune
	escaped, started, dynamic := false, false, false
	flush := func() {
		if started {
			args = append(args, word.String())
			word.Reset()
			started = false
		}
	}
	for _, r := range strings.TrimSpace(command) {
		if escaped {
			word.WriteRune(r)
			escaped = false
			continue
		}
		if quote != 0 {
			switch {
			case r == quote:
				quote = 0
			case r == '\\' && quote == '"':
				escaped = true
			case r == '$' || r == '`':
				dynamic = true
			default:
				word.WriteRune(r)
			}
			continue
		}
		switch r {
		case '\'', '"':
			quote, started = r, true
		case '\\':
			escaped, started = true, true
		case ' ', '\t', '\r', '\n':
			flush()
		case '$', '`':
			dynamic = true
			word.WriteRune(r)
			started = true
		case ';', '&', '|', '<', '>', '(', ')':
			return "", false
		default:
			word.WriteRune(r)
			started = true
		}
	}
	flush()
	if escaped || quote != 0 || dynamic || len(args) == 0 || args[0] != "cd" {
		return "", false
	}
	if len(args) == 1 {
		return ".", true
	}
	if len(args) == 2 && args[1] != "-" && !strings.HasPrefix(args[1], "~") {
		return args[1], true
	}
	if len(args) == 3 && args[1] == "--" && args[2] != "-" && !strings.HasPrefix(args[2], "~") {
		return args[2], true
	}
	return "", false
}

func commandRelativePath(base, dir string) (string, error) {
	rel, err := filepath.Rel(base, dir)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", errors.New("工作目录越界")
	}
	if err := safePath(rel); err != nil {
		return "", err
	}
	return rel, nil
}

func localCommandDisplayTarget(target, display, base string) string {
	if !filepath.IsAbs(target) || !filepath.IsAbs(display) {
		return target
	}
	rel, err := filepath.Rel(filepath.Clean(display), filepath.Clean(target))
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return target
	}
	return filepath.Join(base, rel)
}

func resolveLocalCommandDir(base, current, target string) (string, string, error) {
	candidate := target
	if !filepath.IsAbs(candidate) {
		candidate = filepath.Join(current, candidate)
	}
	dir, err := filepath.EvalSymlinks(filepath.Clean(candidate))
	if err != nil {
		return "", "", err
	}
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return "", "", errors.New("目标不是可访问目录")
	}
	rel, err := commandRelativePath(base, dir)
	if err != nil {
		return "", "", err
	}
	return dir, rel, nil
}

func resolveRemoteCommandDir(base, current, target string) (string, string, error) {
	base = path.Clean("/" + strings.TrimPrefix(base, "/"))
	candidate := target
	if !path.IsAbs(candidate) {
		candidate = path.Join(base, current, candidate)
	}
	dir := path.Clean(candidate)
	if base != "/" && dir != base && !strings.HasPrefix(dir, base+"/") {
		return "", "", errors.New("工作目录越界")
	}
	rel := strings.TrimPrefix(strings.TrimPrefix(dir, base), "/")
	if rel == "" {
		rel = "."
	}
	if err := safePath(rel); err != nil {
		return "", "", err
	}
	return dir, rel, nil
}

func commandStreamHeaders(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/x-ndjson")
	w.Header().Set("X-Accel-Buffering", "no")
}

func (a *App) command(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Command     string `json:"command"`
		Cwd         string `json:"cwd"`
		Interactive bool   `json:"interactive"`
	}
	if err := decode(w, r, &in); err != nil {
		fail(w, 400, err)
		return
	}
	if strings.TrimSpace(in.Command) == "" || len(in.Command) > 16000 {
		fail(w, 400, errors.New("命令为空或过长"))
		return
	}
	a.mu.Lock()
	mode := sandboxMode(a.settings.SandboxMode)
	workspaceMode := a.workspaceMode()
	commandTask := &Task{WorkspaceID: a.wsID(), WorkspaceRev: a.wsRevision, WorkspaceMode: workspaceMode, WorkspaceRemotePath: a.wsConfig.Workspace.Path}
	wsRoot := a.workspace
	workspaceDisplay := a.workspaceDisplay
	cacheContainer := a.cacheContainer
	a.mu.Unlock()
	remoteMode := workspaceMode == "ssh"
	if err := shellSandboxPolicy(mode, in.Command, remoteMode, in.Interactive); err != nil {
		fail(w, 403, err)
		return
	}
	if in.Cwd == "" {
		in.Cwd = "."
	}
	if err := safePath(in.Cwd); err != nil {
		fail(w, 400, err)
		return
	}
	cdTarget, changeDir := standaloneCD(in.Command)
	changeDir = changeDir && !in.Interactive
	if remoteMode {
		// R02/R03：远程模式不做本地 cwd 校验；命令进入绑定的远程目录
		timeout := 60 * time.Second
		if in.Interactive {
			timeout = 15 * time.Minute
		}
		ctx, cancel := context.WithTimeout(r.Context(), timeout)
		defer cancel()
		session, release, err := a.taskSSHSession(ctx, commandTask)
		if err != nil {
			fail(w, 400, err)
			return
		}
		defer release()
		remote := in.Command
		commandDir, _, err := resolveRemoteCommandDir(session.directory, ".", in.Cwd)
		if err != nil {
			fail(w, 400, err)
			return
		}
		nextCwd := ""
		if changeDir {
			var nextDir string
			nextDir, nextCwd, err = resolveRemoteCommandDir(session.directory, in.Cwd, cdTarget)
			if err != nil {
				fail(w, 400, err)
				return
			}
			commandDir = nextDir
			remote = "cd " + shellQuote(nextDir)
		}
		if in.Interactive {
			// Keep one login shell attached to the PTY. The first command is
			// delivered through stdin below so subsequent input, sudo prompts,
			// shell history and Tab completion all share the same shell state.
			remote = sshInteractiveShellCommand()
		}
		if rp := strings.TrimSpace(commandDir); rp != "" && !changeDir {
			remote = "cd " + shellQuote(rp) + " && " + remote
		}
		if cache := session.cache; cache != "" {
			remote = "mkdir -p " + shellQuote(cache) + " && export AIDE_CACHE=" + shellQuote(cache) + " GOCACHE=" + shellQuote(cache) + " && " + remote
		}
		select {
		case a.commands <- struct{}{}:
			defer func() { <-a.commands }()
		default:
			fail(w, 429, errors.New("同时最多运行 4 个命令"))
			return
		}
		commandStreamHeaders(w)
		stream := &streamWriter{w: w}
		start := time.Now()
		var code int
		var runErr error
		if in.Interactive {
			inputID := newID()
			reader, writer := io.Pipe()
			a.commandInputMu.Lock()
			if a.commandInputs == nil {
				a.commandInputs = make(map[string]*io.PipeWriter)
			}
			a.commandInputs[inputID] = writer
			a.commandInputMu.Unlock()
			stream.event(map[string]string{"type": "session", "id": inputID})
			ptyInput := sshInteractivePTYInput(in.Command, reader)
			code, runErr = a.execRemotePTYSession(ctx, remote, ptyInput, stream, stream, session)
			a.commandInputMu.Lock()
			delete(a.commandInputs, inputID)
			a.commandInputMu.Unlock()
			_ = reader.Close()
			_ = writer.Close()
		} else {
			code, runErr = a.execRemoteSession(ctx, remote, stream, stream, session)
		}
		message := ""
		if runErr != nil {
			message = runErr.Error()
		}
		if ctx.Err() != nil {
			if in.Interactive {
				message = "交互命令已取消或超过 15 分钟"
			} else {
				message = "命令已取消或超过 60 秒"
			}
		}
		if changeDir && runErr == nil && code == 0 {
			stream.event(map[string]string{"type": "cwd", "cwd": nextCwd, "display": commandDir})
		}
		stream.event(map[string]any{"type": "exit", "code": code, "error": message, "elapsedMS": time.Since(start).Milliseconds()})
		return
	}
	if wsRoot == nil {
		fail(w, 400, errors.New("工作区不可用"))
		return
	}
	wsRootPath := wsRoot.Name() // The captured root cannot be redirected by a concurrent switch.
	dir, err := filepath.EvalSymlinks(filepath.Join(wsRootPath, in.Cwd))
	if err != nil {
		fail(w, 400, err)
		return
	}
	base, err := filepath.EvalSymlinks(wsRootPath)
	if err != nil {
		fail(w, 400, err)
		return
	}
	dir, _ = filepath.Abs(dir)
	base, _ = filepath.Abs(base)
	if dir != base && !strings.HasPrefix(dir, base+string(filepath.Separator)) {
		fail(w, 400, errors.New("工作目录越界"))
		return
	}
	if changeDir {
		target := localCommandDisplayTarget(cdTarget, workspaceDisplay, base)
		nextDir, nextCwd, err := resolveLocalCommandDir(base, dir, target)
		if err != nil {
			fail(w, 400, err)
			return
		}
		commandStreamHeaders(w)
		stream := &streamWriter{w: w}
		stream.event(map[string]string{"type": "cwd", "cwd": nextCwd, "display": nextDir})
		stream.event(map[string]any{"type": "exit", "code": 0, "error": "", "elapsedMS": 0})
		return
	}
	select {
	case a.commands <- struct{}{}:
		defer func() { <-a.commands }()
	default:
		fail(w, 429, errors.New("同时最多运行 4 个命令"))
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	cmd, cleanup, err := execsandbox.Command(ctx, mode, dir, in.Command)
	if err != nil {
		fail(w, 403, err)
		return
	}
	defer cleanup()
	cmd.Dir = dir
	cacheEnv := cacheContainer
	cmd.Env = []string{"PATH=/usr/local/go/bin:/usr/local/bin:/usr/bin:/bin", "HOME=/home/aide", "LANG=C.UTF-8", "TERM=dumb", "GOCACHE=" + cacheEnv, "GOPATH=/home/aide/go", "AIDE_CACHE=" + cacheEnv}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	cmd.WaitDelay = 2 * time.Second
	commandStreamHeaders(w)
	stream := &streamWriter{w: w}
	cmd.Stdout = stream
	cmd.Stderr = stream
	start := time.Now()
	err = cmd.Start()
	if err == nil {
		err = cmd.Wait()
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	code := 0
	if err != nil {
		code = -1
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			code = ee.ExitCode()
		}
	}
	message := ""
	if err != nil {
		message = err.Error()
	}
	if ctx.Err() != nil {
		message = "命令已取消或超过 60 秒"
	}
	stream.event(map[string]any{"type": "exit", "code": code, "error": message, "elapsedMS": time.Since(start).Milliseconds()})
}

func sshInteractiveShellCommand() string {
	return `"${SHELL:-/bin/bash}" -l`
}

func sshInteractivePTYInput(firstCommand string, liveInput io.Reader) io.Reader {
	if firstCommand == "" {
		return liveInput
	}
	return io.MultiReader(strings.NewReader(firstCommand+"\r"), liveInput)
}

// commandInput writes transient terminal input to an active SSH PTY. Input is
// deliberately neither logged nor added to command history.
func (a *App) commandInput(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Input string `json:"input"`
	}
	if err := decode(w, r, &in); err != nil {
		fail(w, 400, err)
		return
	}
	if len(in.Input) > 4096 {
		fail(w, 400, errors.New("终端输入过长"))
		return
	}
	a.commandInputMu.Lock()
	writer := a.commandInputs[r.PathValue("id")]
	a.commandInputMu.Unlock()
	if writer == nil {
		fail(w, 404, errors.New("交互终端已结束"))
		return
	}
	// The client sends exact terminal bytes: printable text plus CR on Enter,
	// and control sequences such as Tab or Ctrl+C without an implicit newline.
	if _, err := io.WriteString(writer, in.Input); err != nil {
		fail(w, 410, errors.New("交互终端已关闭"))
		return
	}
	jsonOut(w, 200, map[string]bool{"ok": true})
}
