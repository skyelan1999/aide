// aide-local-mounts is a host-only launcher companion, never an AI tool.
package main

import (
	"aide/internal/localmounts"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type request struct {
	ID       string `json:"id"`
	Action   string `json:"action"`
	Revision string `json:"revision"`
	Created  int64  `json:"created"`
}
type response struct {
	ID    string `json:"id"`
	Path  string `json:"path,omitempty"`
	Error string `json:"error,omitempty"`
	State string `json:"state"`
}

func command(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).Output()
}

// Only the launcher-selected Docker Compose implementation is accepted. No shell
// evaluation or arbitrary command arguments are taken from the environment.
func compose(ctx context.Context, docker string, args ...string) ([]byte, error) {
	selected := os.Getenv("AIDE_LOCAL_MOUNTS_COMPOSE")
	if selected == "" || selected == "docker" {
		return command(ctx, docker, append([]string{"compose"}, args...)...)
	}
	base := strings.ToLower(filepath.Base(selected))
	if base != "docker-compose" && base != "docker-compose.exe" {
		return nil, errors.New("unsupported Compose executable")
	}
	return command(ctx, selected, args...)
}

func refreshForwarders(ctx context.Context, root, docker string) error {
	if runtime.GOOS == "windows" {
		launcher := filepath.Join(root, "aide-launcher.exe")
		if info, err := os.Stat(launcher); err != nil || !info.Mode().IsRegular() {
			return errors.New("native Windows launcher missing; restore aide-launcher.exe")
		}
		cmd := exec.CommandContext(ctx, launcher, "refresh-helpers", "--root", root, "--no-browser")
		if _, err := cmd.Output(); err != nil {
			return fmt.Errorf("Aide restarted, but native forwarding refresh failed: %w", err)
		}
		return nil
	}
	script := filepath.Join(root, "scripts", "message-lan-control.sh")
	if info, err := os.Stat(script); os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return err
	} else if !info.Mode().IsRegular() {
		return errors.New("message forwarding controller is not a regular file")
	}
	cmd := exec.CommandContext(ctx, "bash", script, "start")
	cmd.Env = append(os.Environ(), "DOCKER_BIN="+docker, "AIDE_REFRESH_DOCKER="+docker, "AIDE_REFRESH_SCRIPT="+script)
	if _, err := cmd.Output(); err != nil {
		return fmt.Errorf("Aide restarted, but message/mobile forwarding refresh failed; restart the launcher: %w", err)
	}
	return nil
}

func completedRequest(dir, id string) bool {
	raw, err := os.ReadFile(filepath.Join(dir, "response.json"))
	if err != nil || len(raw) > 4096 {
		return false
	}
	var prior response
	return json.Unmarshal(raw, &prior) == nil && prior.ID == id && (prior.State == "done" || prior.State == "error")
}

// The stop marker is observed even during a native dialog or Compose operation.
// The lock remains until the operation and any rollback have returned.
func watchStop(ctx context.Context, cancel context.CancelFunc, dir string) {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if _, e := os.Stat(filepath.Join(dir, "stop")); e == nil {
				cancel()
				return
			}
		}
	}
}
func processAlive(pid int) (bool, error) {
	if pid <= 0 {
		return false, errors.New("invalid directory helper PID")
	}
	if runtime.GOOS == "windows" {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		raw, err := command(ctx, "tasklist.exe", "/FO", "CSV", "/NH", "/FI", "PID eq "+strconv.Itoa(pid))
		if err != nil {
			return false, err
		}
		records, err := csv.NewReader(strings.NewReader(string(raw))).ReadAll()
		if err != nil {
			return false, err
		}
		for _, row := range records {
			if len(row) > 1 && row[1] == strconv.Itoa(pid) {
				return true, nil
			}
		}
		return false, nil
	}
	p, err := os.FindProcess(pid)
	if err != nil {
		return false, err
	}
	err = p.Signal(syscall.Signal(0))
	if err == nil || errors.Is(err, os.ErrPermission) {
		return true, nil
	}
	if errors.Is(err, syscall.ESRCH) || errors.Is(err, os.ErrProcessDone) {
		return false, nil
	}
	return false, err
}
func clearDeadLock(path string) (bool, error) {
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	pid, err := strconv.Atoi(string(raw))
	if err != nil {
		return false, errors.New("invalid helper lock; manual recovery required")
	}
	alive, err := processAlive(pid)
	if err != nil || alive {
		return false, err
	}
	// Do not remove a replacement owner's lock.
	again, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return true, nil // The exiting owner already removed its lock.
	}
	if err != nil {
		return false, err
	}
	if string(again) != string(raw) {
		return false, nil
	}
	err = os.Remove(path)
	if os.IsNotExist(err) {
		err = nil // Removal by the owner is also a successful cleanup.
	}
	return true, err
}
func stopHelper(dir string, timeout time.Duration) error {
	if err := os.WriteFile(filepath.Join(dir, "stop"), []byte("stop"), 0600); err != nil {
		return err
	}
	deadline := time.Now().Add(timeout)
	owner := 0
	if raw, e := os.ReadFile(filepath.Join(dir, "serve.pid")); e == nil {
		owner, _ = strconv.Atoi(string(raw))
	}
	for {
		cleared, err := clearDeadLock(filepath.Join(dir, "serve.pid"))
		if err != nil {
			return err
		}
		if cleared {
			if owner == 0 {
				return nil
			}
			alive, e := processAlive(owner)
			if e != nil {
				return e
			}
			if !alive {
				return nil
			}
		}
		if time.Now().After(deadline) {
			return errors.New("directory helper is still stopping; executable must not be replaced")
		}
		time.Sleep(100 * time.Millisecond)
	}
}
func acquireHelper(dir string) (func(), error) {
	p := filepath.Join(dir, "serve.pid")
	for attempt := 0; attempt < 2; attempt++ {
		f, err := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if os.IsExist(err) && attempt == 0 {
			cleared, e := clearDeadLock(p)
			if e != nil {
				return nil, e
			}
			if cleared {
				continue
			}
		}
		if err != nil {
			return nil, fmt.Errorf("directory helper already running: %w", err)
		}
		if _, err = f.WriteString(strconv.Itoa(os.Getpid())); err != nil {
			f.Close()
			os.Remove(p)
			return nil, err
		}
		if err = f.Close(); err != nil {
			os.Remove(p)
			return nil, err
		}
		return func() {
			raw, e := os.ReadFile(p)
			if e == nil && string(raw) == strconv.Itoa(os.Getpid()) {
				os.Remove(p)
			}
		}, nil
	}
	return nil, errors.New("cannot acquire directory helper lock")
}

func nativeWindowsLauncher() (string, error) {
	executable, err := os.Executable()
	if err != nil {
		return "", err
	}
	launcher := filepath.Join(filepath.Dir(executable), "..", "..", "aide-launcher.exe")
	if info, err := os.Stat(launcher); err != nil || !info.Mode().IsRegular() {
		return "", errors.New("native Windows launcher missing; restore aide-launcher.exe")
	}
	return launcher, nil
}

func choose(ctx context.Context) (string, error) {
	switch runtime.GOOS {
	case "darwin":
		out, err := command(ctx, "/usr/bin/osascript", "-e", `POSIX path of (choose folder with prompt "选择允许 Aide 访问的目录")`)
		return strings.TrimRight(strings.TrimSpace(string(out)), "/"), err
	case "windows":
		launcher, err := nativeWindowsLauncher()
		if err != nil {
			return "", err
		}
		out, err := command(ctx, launcher, "choose-directory")
		return strings.TrimSpace(string(out)), err
	default:
		return "", errors.New("native directory selection requires Windows or macOS; enter an absolute directory instead")
	}
}
func confirmApply(ctx context.Context) error {
	switch runtime.GOOS {
	case "darwin":
		_, err := command(ctx, "/usr/bin/osascript", "-e", `display dialog "Aide 将按已保存的目录权限重新创建应用容器。请先保存工作，运行中的会话和手机连接将中断。" buttons {"取消", "应用并重启"} default button "取消" cancel button "取消"`)
		return err
	case "windows":
		launcher, err := nativeWindowsLauncher()
		if err != nil {
			return err
		}
		_, err = command(ctx, launcher, "confirm-directory-restart")
		return err
	default:
		return errors.New("use the launcher to apply directory changes on this operating system")
	}
}
func protected(ctx context.Context, docker string) ([]string, error) {
	out, err := compose(ctx, docker, "config", "--format", "json")
	if err != nil {
		return nil, errors.New("cannot resolve current Compose directory boundaries")
	}
	var cfg struct {
		Services map[string]struct {
			Volumes []struct{ Type, Source, Target string }
		}
	}
	if err = json.Unmarshal(out, &cfg); err != nil {
		return nil, err
	}
	dirs := []string{}
	for _, v := range cfg.Services["aide"].Volumes {
		if v.Type == "bind" && !strings.HasPrefix(v.Target, "/mounts/") && v.Target != "/run/aide-local-mounts.json" {
			dirs = append(dirs, v.Source)
		}
	}
	return dirs, nil
}
func prepare(ctx context.Context, root, docker string) (string, error) {
	return prepareRevision(ctx, root, docker, nil)
}
func prepareRevision(ctx context.Context, root, docker string, expected *string) (string, error) {
	p, err := protected(ctx, docker)
	if err != nil {
		return "", err
	}
	if expected != nil {
		_, out, err := localmounts.PrepareRevision(root, p, *expected)
		return out, err
	}
	_, out, err := localmounts.Prepare(root, p)
	return out, err
}

var confirmApplication = confirmApply

func apply(ctx context.Context, root, docker, revision string) error {
	desired := filepath.Join(root, ".aide-data/storage/data/local-mounts/desired.json")
	c, err := localmounts.Read(desired)
	if err != nil {
		return err
	}
	if localmounts.Revision(c) != revision {
		return errors.New("directory configuration changed; review it again")
	}
	if err = confirmApplication(ctx); err != nil {
		return errors.New("application cancelled on host")
	}
	// Recheck after the native confirmation; never apply a substituted plan.
	c, err = localmounts.Read(desired)
	if err != nil || localmounts.Revision(c) != revision {
		return errors.New("directory configuration changed during confirmation")
	}
	native := filepath.Join(root, ".aide-data/local-mounts")
	backup := map[string]json.RawMessage{}
	for _, n := range []string{"active.json", "compose.json"} {
		b, e := os.ReadFile(filepath.Join(native, n))
		if e != nil {
			return e
		}
		backup[n] = b
	}
	if _, err = prepareRevision(ctx, root, docker, &revision); err != nil {
		return err
	}
	_, err = compose(ctx, docker, "up", "-d", "--no-deps", "--no-build", "--pull", "never", "--wait", "--wait-timeout", "90", "aide")
	if err != nil {
		for n, b := range backup {
			if e := localmounts.Write(filepath.Join(native, n), b); e != nil {
				return fmt.Errorf("apply failed and rollback metadata failed: %w", e)
			}
		}
		rollbackCtx, rollbackCancel := context.WithTimeout(context.Background(), 100*time.Second)
		defer rollbackCancel()
		_, rollback := compose(rollbackCtx, docker, "up", "-d", "--no-deps", "--no-build", "--pull", "never", "--wait", "--wait-timeout", "90", "aide")
		if rollback == nil {
			rollback = refreshForwarders(rollbackCtx, root, docker)
		}
		if rollback != nil {
			return errors.New("application failed; original configuration restored but launcher restart is required")
		}
		return errors.New("application failed; original directory configuration restored")
	}
	return refreshForwarders(ctx, root, docker)
}
func main() {
	root := flag.String("root", "", "launcher root")
	docker := flag.String("docker", "docker", "Docker executable")
	mode := flag.String("mode", "serve", "prepare, serve or stop")
	flag.Parse()
	abs, err := filepath.Abs(*root)
	if err != nil || *root == "" {
		log.Fatal("root required")
	}
	if err = localmounts.ValidateNativeRoot(abs); err != nil {
		log.Fatal(err)
	}
	if err = os.Chdir(abs); err != nil {
		log.Fatal(err)
	}
	dir := filepath.Join(abs, ".aide-data/storage/data/local-mounts")
	if err = os.MkdirAll(dir, 0700); err != nil {
		log.Fatal(err)
	}
	if *mode == "stop" {
		if err = stopHelper(dir, 115*time.Second); err != nil {
			log.Fatal(err)
		}
		return
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if *mode == "prepare" {
		out, err := prepare(ctx, abs, *docker)
		if err != nil {
			log.Fatal(err)
		}
		fmt.Println(out)
		return
	}
	if *mode != "serve" {
		log.Fatal("invalid mode")
	}
	release, err := acquireHelper(dir)
	if err != nil {
		log.Fatal(err)
	}
	defer release()
	// Launcher stops the prior helper before replacing the executable.
	os.Remove(filepath.Join(dir, "stop"))
	go watchStop(ctx, cancel, dir)
	last := ""
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		if _, e := os.Stat(filepath.Join(dir, "stop")); e == nil {
			return
		}
		localmounts.Write(filepath.Join(dir, "host.json"), map[string]any{"updated": time.Now().Unix(), "os": runtime.GOOS})
		raw, e := os.ReadFile(filepath.Join(dir, "request.json"))
		var req request
		if e == nil && len(raw) < 4096 && json.Unmarshal(raw, &req) == nil && req.ID != "" && req.ID != last && !completedRequest(dir, req.ID) && time.Now().Unix()-req.Created >= 0 && time.Now().Unix()-req.Created < 180 {
			last = req.ID
			result := response{ID: req.ID, State: "done"}
			localmounts.Write(filepath.Join(dir, "response.json"), response{ID: req.ID, State: "running"})
			task, done := context.WithTimeout(ctx, 3*time.Minute)
			// Heartbeat remains live while the OS dialog is open or Docker restarts.
			finished := make(chan struct{})
			go func() {
				ticker := time.NewTicker(2 * time.Second)
				defer ticker.Stop()
				for {
					select {
					case <-finished:
						return
					case <-task.Done():
						return
					case <-ticker.C:
						localmounts.Write(filepath.Join(dir, "host.json"), map[string]any{"updated": time.Now().Unix(), "os": runtime.GOOS})
					}
				}
			}()
			switch req.Action {
			case "choose":
				result.Path, e = choose(task)
				if e == nil {
					result.Path, e = localmounts.Normalize(result.Path)
				}
			case "apply":
				e = apply(task, abs, *docker, req.Revision)
			default:
				e = errors.New("unknown directory action")
			}
			done()
			close(finished)
			if e != nil {
				result.Error = e.Error()
				result.State = "error"
			}
			localmounts.Write(filepath.Join(dir, "response.json"), result)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(500 * time.Millisecond):
		}
	}
}
