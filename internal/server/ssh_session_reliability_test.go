package server

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// These regressions exercise the lifecycle with disposable client substitutes;
// real OpenSSH/SFTP behavior is covered separately by the isolated SSH fixture.
func reliabilitySSHStub(t *testing.T, a *App, establish bool) string {
	t.Helper()
	dir := t.TempDir()
	marker, log := filepath.Join(dir, "up"), filepath.Join(dir, "connections")
	stub := filepath.Join(dir, "ssh")
	create := "touch " + shellQuote(marker)
	if !establish {
		create = ":"
	}
	writeStub(t, stub, `#!/bin/sh
op=; prev=; connect=
for arg in "$@"; do
  [ "$prev" = "-O" ] && op="$arg"
  [ "$arg" = "-fNM" ] && connect=yes
  prev="$arg"
done
case "$op" in
  check) test -f `+shellQuote(marker)+`; exit $? ;;
  exit) printf '%s\n' exit >> `+shellQuote(log)+`; rm -f `+shellQuote(marker)+`; exit 0 ;;
esac
if [ "$connect" = yes ]; then
  printf '%s\n' connect >> `+shellQuote(log)+`
  sleep 0.08
  `+create+`
  exit 0
fi
exit 0
`)
	a.sshBin = stub
	a.wsConfig.Workspace.Mode = "ssh"
	a.wsConfig.Workspace.Host = "fixture.example"
	a.wsConfig.Workspace.Username = "fixture"
	a.wsConfig.Workspace.Port = 22
	a.wsConfig.Workspace.Auth = "none"
	return log
}

func registerReliabilitySource(a *App, src Source) Source {
	src.Type, src.Enabled = "sftp", true
	a.mu.Lock()
	a.sourceRegistry.Sources = append(a.sourceRegistry.Sources, src)
	a.mu.Unlock()
	return src
}

func TestSSHSessionLifecycleConcurrentConnect(t *testing.T) {
	for _, source := range []bool{false, true} {
		name := "workspace"
		if source {
			name = "source"
		}
		t.Run(name, func(t *testing.T) {
			a := testApp(t)
			log := reliabilitySSHStub(t, a, true)
			src := registerReliabilitySource(a, Source{ID: "reliability-source", Config: SourceConfig{Host: "fixture.example", Username: "fixture", Auth: "none"}})
			var wg sync.WaitGroup
			errs := make(chan error, 16)
			start := make(chan struct{})
			for range 16 {
				wg.Add(1)
				go func() {
					defer wg.Done()
					<-start
					if source {
						errs <- a.ensureSourceSession(context.Background(), src)
					} else {
						errs <- a.ensureSSHSession(context.Background())
					}
				}()
			}
			close(start)
			wg.Wait()
			close(errs)
			for err := range errs {
				if err != nil {
					t.Fatal(err)
				}
			}
			b, err := os.ReadFile(log)
			if err != nil || strings.Count(string(b), "connect\n") != 1 {
				t.Fatalf("concurrent callers must establish one master: %q, %v", b, err)
			}
		})
	}
}

func TestSSHSessionLifecycleCanceledWaiter(t *testing.T) {
	a := &App{}
	unlock, err := a.lockSSHSession(context.Background(), "cancel-fixture")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := a.lockSSHSession(ctx, "cancel-fixture"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("blocked waiter must honor its context: %v", err)
	}
	unlock()
	if len(a.sshLifecycles) != 0 {
		t.Fatalf("idle gates must be reclaimed: %v", a.sshLifecycles)
	}
}

func TestSSHSessionTemporaryAskpassRemoved(t *testing.T) {
	a := testApp(t)
	reliabilitySSHStub(t, a, true)
	a.wsConfig.Workspace.Auth = "password"
	a.wsSecrets.Password = "disposable-fixture-password"
	if err := a.ensureSSHSession(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(sshControlSocket + ".askpass"); !os.IsNotExist(err) {
		t.Fatalf("askpass must be removed after authentication: %v", err)
	}
}

func TestSSHSourceSessionMustVerifyMaster(t *testing.T) {
	a := testApp(t)
	reliabilitySSHStub(t, a, false)
	src := registerReliabilitySource(a, Source{ID: "unestablished-source", Config: SourceConfig{Host: "fixture.example", Auth: "none"}})
	if err := a.ensureSourceSession(context.Background(), src); err == nil || !strings.Contains(err.Error(), "会话未建立") {
		t.Fatalf("client exit 0 without a master is not a successful connection: %v", err)
	}
}

func TestSSHSessionConnectHonorsCallerDeadline(t *testing.T) {
	a := testApp(t)
	a.wsConfig.Workspace.Host = "fixture.example"
	a.wsConfig.Workspace.Auth = "none"
	a.sshBin = filepath.Join(t.TempDir(), "ssh")
	writeStub(t, a.sshBin, "#!/bin/sh\nfor arg in \"$@\"; do [ \"$arg\" = \"-O\" ] && exit 1; done\nexec sleep 60\n")
	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()
	started := time.Now()
	err := a.ensureSSHSession(ctx)
	if !errors.Is(err, context.DeadlineExceeded) || time.Since(started) > 2*time.Second {
		t.Fatalf("connection must respect caller deadline without blank error: elapsed=%s err=%v", time.Since(started), err)
	}
}

func TestSFTPDirectoryReconnectIsReadOnly(t *testing.T) {
	for _, source := range []bool{false, true} {
		for _, tc := range []struct {
			name, batch, message string
			wantRetries          int
		}{
			{"directory transport", "cd \"/srv/project\"\nls -l\n", "Connection closed", 1},
			{"upload transport", "put \"local\" \"remote\"\nrename \"remote\" \"final\"\n", "Connection closed", 0},
			{"missing directory", "cd \"/missing\"\nls -l\n", "Couldn't canonicalize: No such file or directory", 0},
			{"denied directory", "cd \"/private\"\nls -l\n", "Permission denied", 0},
			{"denied before disconnect", "cd \"/private\"\nls -l\n", "Permission denied\nConnection closed", 0},
			{"changed host identity", "cd \"/srv/project\"\nls -l\n", "REMOTE HOST IDENTIFICATION HAS CHANGED!\nConnection closed", 0},
		} {
			prefix := "workspace/"
			if source {
				prefix = "source/"
			}
			t.Run(prefix+tc.name, func(t *testing.T) {
				a := testApp(t)
				reliabilitySSHStub(t, a, true)
				dir := t.TempDir()
				attempts := filepath.Join(dir, "attempts")
				a.sftpBin = filepath.Join(dir, "sftp")
				writeStub(t, a.sftpBin, `#!/bin/sh
cat >/dev/null
printf '%s\n' attempt >> `+shellQuote(attempts)+`
if [ "$(wc -l < `+shellQuote(attempts)+`)" -eq 1 ]; then
  printf '%s\n' `+shellQuote(tc.message)+` >&2
  exit 1
fi
printf '%s\n' '-rw-r--r-- 1 user group 8 Oct 8 10:00 fixture.md'
`)
				var err error
				if source {
					src := registerReliabilitySource(a, Source{ID: "retry-source", Config: SourceConfig{Host: "fixture.example", Auth: "none"}})
					_, err = a.sftpBatchSource(src, tc.batch)
				} else {
					_, err = a.sftpBatch(tc.batch)
				}
				if (err == nil) != (tc.wantRetries == 1) {
					t.Fatalf("unexpected result: retries=%d err=%v", tc.wantRetries, err)
				}
				b, readErr := os.ReadFile(attempts)
				if readErr != nil || strings.Count(string(b), "attempt\n") != 1+tc.wantRetries {
					t.Fatalf("unexpected attempts: %q err=%v", b, readErr)
				}
			})
		}
	}
}

func TestSSHReconnectDoesNotCloseNewerSocket(t *testing.T) {
	a := &App{sshBin: "must-not-run"}
	sock := filepath.Join(t.TempDir(), "socket-placeholder")
	if err := os.WriteFile(sock, []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}
	previous, err := os.Stat(sock)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(sock, sock+".old"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sock, []byte("new"), 0600); err != nil {
		t.Fatal(err)
	}
	a.resetSSHMasterIfUnchanged(context.Background(), sock, "fixture.example", previous)
	if b, err := os.ReadFile(sock); err != nil || string(b) != "new" {
		t.Fatalf("newer socket must be preserved: %q %v", b, err)
	}
}

func TestSSHRemoteCommandStatusAndCleanup(t *testing.T) {
	for _, command := range []string{"false", "exit 7", "printf result; exit 9", "true"} {
		t.Run(command, func(t *testing.T) {
			marker := filepath.Join(t.TempDir(), "process.pgid")
			cmd := exec.Command("sh", "-c", remoteCommandInner(marker, command))
			err := cmd.Run()
			want := 0
			if command == "false" {
				want = 1
			} else if strings.Contains(command, "exit 7") {
				want = 7
			} else if strings.Contains(command, "exit 9") {
				want = 9
			}
			code := 0
			if err != nil {
				var ee *exec.ExitError
				if !errors.As(err, &ee) {
					t.Fatal(err)
				}
				code = ee.ExitCode()
			}
			if code != want {
				t.Fatalf("command status changed: got %d want %d", code, want)
			}
			if _, err := os.Stat(marker); !os.IsNotExist(err) {
				t.Fatalf("EXIT cleanup did not remove marker: %v", err)
			}
		})
	}
}

func TestSSHRemoteExecKeepsExitCode(t *testing.T) {
	for _, tc := range []struct {
		name      string
		exit      string
		code      int
		wantError bool
	}{{"command failure", "7", 7, false}, {"transport or remote 255", "255", 255, true}} {
		t.Run(tc.name, func(t *testing.T) {
			a := &App{sshBin: filepath.Join(t.TempDir(), "ssh")}
			writeStub(t, a.sshBin, "#!/bin/sh\nexit "+tc.exit+"\n")
			code, err := a.execRemote(context.Background(), "exit "+tc.exit, nil, nil)
			if code != tc.code || (err != nil) != tc.wantError {
				t.Fatalf("remote failure must retain exit code and transport ambiguity: code=%d err=%v", code, err)
			}
		})
	}
}

func TestSSHRemoteExecWaitsForForkedSession(t *testing.T) {
	if _, err := exec.LookPath("setsid"); err != nil {
		t.Skip("requires Linux setsid, exercised in the isolated Go container")
	}
	for _, command := range []string{"false", "exit 7", "true"} {
		t.Run(command, func(t *testing.T) {
			a := &App{sshBin: filepath.Join(t.TempDir(), "ssh")}
			// The outer session makes the real wrapper a group leader, forcing
			// its setsid to fork just as it can under an SSH-launched shell.
			writeStub(t, a.sshBin, "#!/bin/sh\nfor arg in \"$@\"; do remote=$arg; done\nexec setsid -w bash -c \"exec $remote\"\n")
			code, err := a.execRemote(context.Background(), command, nil, nil)
			want := 0
			if command == "false" {
				want = 1
			} else if command == "exit 7" {
				want = 7
			}
			if err != nil || code != want {
				t.Fatalf("forked session must return command status: code=%d want=%d err=%v", code, want, err)
			}
		})
	}
}

func TestSSHRemoteCleanupExecutesAndStopsOwnedGroup(t *testing.T) {
	if _, err := exec.LookPath("setsid"); err != nil {
		t.Skip("requires Linux setsid, exercised in the isolated Go container")
	}
	dir := t.TempDir()
	marker, childFile := filepath.Join(dir, "group.pgid"), filepath.Join(dir, "child.pid")
	if out, err := exec.Command("bash", "-n", "-c", remoteGroupCleanupInner(marker)).CombinedOutput(); err != nil {
		t.Fatalf("remote cleanup must be valid bash, not an argv-only stub success: %v: %s", err, out)
	}
	cmd := exec.Command("setsid", "-w", "bash", "-c", "sleep 30 & echo $! > "+shellQuote(childFile)+"; echo $$ > "+shellQuote(marker)+"; wait")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	defer cmd.Process.Kill()
	waitSSHFixture(t, func() bool {
		b, _ := os.ReadFile(marker)
		child, _ := os.ReadFile(childFile)
		return len(b) > 0 && len(child) > 0
	})
	groupBytes, _ := os.ReadFile(marker)
	group, err := strconv.Atoi(strings.TrimSpace(string(groupBytes)))
	if err != nil || group <= 1 {
		t.Fatalf("invalid controlled fixture group: %q", groupBytes)
	}
	// Cleanup fallback only targets the process group created by this test.
	defer syscall.Kill(-group, syscall.SIGKILL)
	childBytes, _ := os.ReadFile(childFile)
	child := strings.TrimSpace(string(childBytes))
	a := &App{sshBin: filepath.Join(dir, "ssh")}
	writeStub(t, a.sshBin, "#!/bin/sh\nfor arg in \"$@\"; do remote=$arg; done\nexec bash -c \"$remote\"\n")
	receipt := a.cleanupRemoteGroup(marker)
	if strings.Contains(receipt, "failed") || strings.Contains(receipt, "unconfirmed") || !strings.HasPrefix(receipt, "AIDE-CLEANUP:") {
		t.Fatalf("actual remote cleanup returned no confirmed execution: %s", receipt)
	}
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("owned session shell remains running after cleanup")
	}
	for _, pid := range []string{strconv.Itoa(group), child} {
		stat, err := os.ReadFile("/proc/" + pid + "/stat")
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			t.Fatalf("cannot verify controlled fixture process %s: %v", pid, err)
		}
		// The command name in parentheses may contain spaces. Its closing
		// parenthesis precedes the kernel state field; only a zombie is stopped.
		end := strings.LastIndex(string(stat), ") ")
		if end < 0 || !strings.HasPrefix(string(stat[end+2:]), "Z ") {
			t.Fatalf("owned process %s remains runnable: %q, receipt=%s", pid, stat, receipt)
		}
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("cleanup left its process-group marker: %v", err)
	}
}

func TestSSHRemoteCleanupFailureIsVisible(t *testing.T) {
	a := &App{sshBin: filepath.Join(t.TempDir(), "ssh")}
	writeStub(t, a.sshBin, "#!/bin/sh\nexit 2\n")
	if receipt := a.cleanupRemoteGroup("/tmp/owned-fixture.pgid"); !strings.HasPrefix(receipt, "AIDE-CLEANUP:failed:") {
		t.Fatalf("cleanup subprocess failure must remain observable: %s", receipt)
	}
}

func TestSFTPFailureWithZeroExit(t *testing.T) {
	for _, source := range []bool{false, true} {
		for _, tc := range []struct {
			name, batch, output string
			wantError           bool
		}{
			{"denied", "cd \"/private\"\nls -l\n", "remote readdir(\"/private\"): Permission denied\r\n", true},
			{"missing", "cd \"/private\"\nls -l\n", "Couldn't canonicalize: No such file or directory\n", true},
			{"missing get", "get \"/missing\" \"local\"\n", "File \"/missing\" not found.\r\n", true},
			{"denied put", "put \"local\" \"/private\"\n", "remote open(\"/private\"): Permission denied\r\n", true},
			{"failed rename", "rename \"/tmp/source\" \"/private\"\n", "Couldn't rename file \"/tmp/source\" to \"/private\": Failure\r\n", true},
			{"filename words", "cd \"/private\"\nls -l\n", "-rw-r--r-- 1 fixture fixture 8 Oct 8 10:00 failure Permission denied notes.md\n", false},
		} {
			name := "workspace/"
			if source {
				name = "source/"
			}
			t.Run(name+tc.name, func(t *testing.T) {
				a := testApp(t)
				reliabilitySSHStub(t, a, true)
				a.sftpBin = filepath.Join(t.TempDir(), "sftp")
				writeStub(t, a.sftpBin, "#!/bin/sh\ncat >/dev/null\nprintf '%s' "+shellQuote(tc.output)+"\nexit 0\n")
				var err error
				if source {
					src := registerReliabilitySource(a, Source{ID: "zero-exit-source", Config: SourceConfig{Host: "fixture.example", Auth: "none"}})
					_, err = a.sftpBatchSource(src, tc.batch)
				} else {
					_, err = a.sftpBatch(tc.batch)
				}
				if (err != nil) != tc.wantError {
					t.Fatalf("SFTP diagnostics must not become empty success or filename false positive: %v", err)
				}
			})
		}
	}
}

func TestSFTPDefaultPortMatchesSSH(t *testing.T) {
	a := &App{}
	args := strings.Join(a.sftpArgs(), " ")
	if !strings.Contains(args, "-P 22") {
		t.Fatalf("empty persisted port must use SSH's default: %s", args)
	}
}

func waitSSHFixture(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("SSH fixture did not reach the requested checkpoint")
}

func TestSSHGenerationRejectsQueuedOldConfig(t *testing.T) {
	for _, source := range []bool{false, true} {
		name := "workspace"
		if source {
			name = "source"
		}
		t.Run(name, func(t *testing.T) {
			a := testApp(t)
			log := reliabilitySSHStub(t, a, true)
			src := registerReliabilitySource(a, Source{ID: "queued-source", Config: SourceConfig{Host: "fixture.example", Auth: "none"}})
			sock := sshControlSocket
			if source {
				sock = sourceSocket(src.ID)
			}
			unlock, err := a.lockSSHSession(context.Background(), sock)
			if err != nil {
				t.Fatal(err)
			}
			var once sync.Once
			release := func() { once.Do(unlock) }
			defer release()
			done := make(chan error, 1)
			go func() {
				if source {
					done <- a.ensureSourceSession(context.Background(), src)
				} else {
					done <- a.ensureSSHSession(context.Background())
				}
			}()
			waitSSHFixture(t, func() bool {
				a.sshLifecycleMu.Lock()
				defer a.sshLifecycleMu.Unlock()
				return a.sshLifecycles[sock].refs >= 2
			})
			generation := a.sshSessionGeneration(sock)
			killed := make(chan struct{})
			go func() {
				a.mu.Lock()
				if source {
					// Deletion must invalidate an already queued source connection.
					a.sourceRegistry.Sources = nil
					a.killSourceSession(src.ID, a.sftpTargetOf(src))
				} else {
					a.wsConfig.Workspace.Host = "replacement.example"
					a.killSSHSession()
				}
				a.mu.Unlock()
				close(killed)
			}()
			waitSSHFixture(t, func() bool { return a.sshSessionGeneration(sock) != generation })
			release()
			select {
			case err := <-done:
				if err == nil || !strings.Contains(err.Error(), "配置已变化") {
					t.Fatalf("stale queued connection must be rejected: %v", err)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("config update and ensure deadlocked")
			}
			select {
			case <-killed:
			case <-time.After(3 * time.Second):
				t.Fatal("master teardown did not finish")
			}
			if b, _ := os.ReadFile(log); strings.Contains(string(b), "connect\n") {
				t.Fatalf("old queued config must not start a new master: %s", b)
			}
		})
	}
}

func TestSSHSourceRejectsDeletedOrChangedConfig(t *testing.T) {
	for _, mode := range []string{"missing", "disabled", "changed"} {
		t.Run(mode, func(t *testing.T) {
			a := testApp(t)
			log := reliabilitySSHStub(t, a, true)
			src := Source{ID: "obsolete-source", Type: "sftp", Enabled: true, Config: SourceConfig{Host: "fixture.example", Auth: "none"}}
			if mode != "missing" {
				current := src
				if mode == "disabled" {
					current.Enabled = false
				} else {
					current.Config.Host = "replacement.example"
				}
				a.sourceRegistry.Sources = append(a.sourceRegistry.Sources, current)
			}
			if err := a.ensureSourceSession(context.Background(), src); err == nil || !strings.Contains(err.Error(), "来源已变化或停用") {
				t.Fatalf("obsolete source must not reconnect: %v", err)
			}
			if b, _ := os.ReadFile(log); strings.Contains(string(b), "connect\n") {
				t.Fatalf("obsolete source started a connection: %s", b)
			}
		})
	}
}

func TestSFTPGenerationBoundReadWriteRejectNewConfig(t *testing.T) {
	a := testApp(t)
	log := reliabilitySSHStub(t, a, true)
	generation := a.sshSessionGeneration(sshControlSocket)
	a.invalidateSSHSession(sshControlSocket)
	if _, err := a.sftpReadAtGeneration("/old/project/file.md", generation); err == nil || !strings.Contains(err.Error(), "配置已变化") {
		t.Fatalf("stale read must not contact replacement host: %v", err)
	}
	if err := a.sftpWriteAtGeneration("/old/project/nested/file.md", []byte("fixture"), generation); err == nil || !strings.Contains(err.Error(), "配置已变化") {
		t.Fatalf("stale write/parent mkdir must not contact replacement host: %v", err)
	}
	if b, _ := os.ReadFile(log); strings.Contains(string(b), "connect\n") {
		t.Fatalf("generation mismatch must be rejected before authentication: %s", b)
	}
}

func TestSFTPBoundTransferKeepsMasterUntilFinished(t *testing.T) {
	a := testApp(t)
	log := reliabilitySSHStub(t, a, true)
	dir := t.TempDir()
	started, finish := filepath.Join(dir, "started"), filepath.Join(dir, "finish")
	a.sftpBin = filepath.Join(dir, "sftp")
	writeStub(t, a.sftpBin, "#!/bin/sh\ncat >/dev/null\ntouch "+shellQuote(started)+"\nwhile [ ! -f "+shellQuote(finish)+" ]; do sleep 0.01; done\nexit 0\n")
	generation := a.sshSessionGeneration(sshControlSocket)
	done := make(chan error, 1)
	go func() {
		_, err := a.sftpBatchExpected("get \"/old/file\" \"local-fixture\"\n", &generation)
		done <- err
	}()
	defer os.WriteFile(finish, nil, 0600)
	waitSSHFixture(t, func() bool { _, err := os.Stat(started); return err == nil })
	killed := make(chan struct{})
	go func() { a.killSSHSession(); close(killed) }()
	waitSSHFixture(t, func() bool { return a.sshSessionGeneration(sshControlSocket) != generation })
	if b, _ := os.ReadFile(log); strings.Contains(string(b), "exit\n") {
		t.Fatalf("master was closed while a pinned transfer was still using it: %s", b)
	}
	if err := os.WriteFile(finish, nil, 0600); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("pinned transfer did not return")
	}
	select {
	case <-killed:
	case <-time.After(3 * time.Second):
		t.Fatal("master teardown did not resume after transfer")
	}
}

func TestSSHSessionInvalidatedMasterAfterKillTimeout(t *testing.T) {
	for _, source := range []bool{false, true} {
		name := "workspace"
		if source {
			name = "source"
		}
		t.Run(name, func(t *testing.T) {
			a := testApp(t)
			log := reliabilitySSHStub(t, a, true)
			src := registerReliabilitySource(a, Source{ID: "timeout-source", Config: SourceConfig{Host: "fixture.example", Auth: "none"}})
			sock := sshControlSocket
			ensure := func() error { return a.ensureSSHSession(context.Background()) }
			if source {
				sock = sourceSocket(src.ID)
				ensure = func() error { return a.ensureSourceSession(context.Background(), src) }
			}
			if err := ensure(); err != nil {
				t.Fatal(err)
			}
			unlock, err := a.lockSSHSession(context.Background(), sock)
			if err != nil {
				t.Fatal(err)
			}
			var once sync.Once
			release := func() { once.Do(unlock) }
			defer release()
			// Simulate a transfer holding the gate beyond the production teardown
			// timeout. The old master remains healthy to -O check at this point.
			if source {
				a.killSourceSession(src.ID, a.sftpTargetOf(src))
			} else {
				a.killSSHSession()
			}
			if b, _ := os.ReadFile(log); strings.Contains(string(b), "exit\n") {
				t.Fatalf("teardown must have timed out behind the held transfer: %s", b)
			}
			a.mu.Lock()
			if source {
				src.Config.Host = "replacement.example"
				a.sourceRegistry.Sources[0] = src
			} else {
				a.wsConfig.Workspace.Host = "replacement.example"
			}
			a.mu.Unlock()
			release()
			if err := ensure(); err != nil {
				t.Fatal(err)
			}
			b, err := os.ReadFile(log)
			if err != nil || string(b) != "connect\nexit\nconnect\n" {
				t.Fatalf("new config must replace the invalidated live master: %q, %v", b, err)
			}
		})
	}
}
