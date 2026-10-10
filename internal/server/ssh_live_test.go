package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func leaveStaleSSHSocket(t *testing.T, sock string) {
	t.Helper()
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: sock, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	listener.SetUnlinkOnClose(false)
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(sock)
	if err != nil || info.Mode()&os.ModeSocket == 0 {
		t.Fatalf("fixture must retain a real socket after its listener exits: %v", err)
	}
	t.Cleanup(func() {
		if current, err := os.Lstat(sock); err == nil && os.SameFile(info, current) {
			os.Remove(sock)
		}
	})
}

func TestSSHFailedControlCheckPreservesLiveSocket(t *testing.T) {
	for _, source := range []bool{false, true} {
		for _, slow := range []bool{false, true} {
			name := fmt.Sprintf("workspace=%t/timeout=%t", !source, slow)
			t.Run(name, func(t *testing.T) {
				a := testApp(t)
				log := reliabilitySSHStub(t, a, true)
				src := registerReliabilitySource(a, Source{ID: "live-check-" + newID(), Config: SourceConfig{Host: "fixture.example", Auth: "none"}})
				sock := sshControlSocket
				if source {
					sock = sourceSocket(src.ID)
				}
				listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: sock, Net: "unix"})
				if err != nil {
					t.Fatal(err)
				}
				defer listener.Close()
				before, err := os.Lstat(sock)
				if err != nil {
					t.Fatal(err)
				}
				ctx := context.Background()
				if slow {
					writeStub(t, a.sshBin, "#!/bin/sh\nexec sleep 60\n")
					var cancel context.CancelFunc
					ctx, cancel = context.WithTimeout(ctx, 80*time.Millisecond)
					defer cancel()
				}
				if source {
					err = a.ensureSourceSession(ctx, src)
				} else {
					err = a.ensureSSHSession(ctx)
				}
				if err == nil || (slow && !errors.Is(err, context.DeadlineExceeded)) {
					t.Fatalf("failed/expired check must return without replacing a live socket: %v", err)
				}
				after, statErr := os.Lstat(sock)
				if statErr != nil || !os.SameFile(before, after) {
					t.Fatalf("failed check removed or replaced a live socket: %v", statErr)
				}
				if b, _ := os.ReadFile(log); len(b) != 0 {
					t.Fatalf("failed check must not close or create a master: %q", b)
				}
			})
		}
	}
}

type sshLiveStartWriter struct {
	mu      sync.Mutex
	buffer  bytes.Buffer
	started chan struct{}
	once    sync.Once
}

func (w *sshLiveStartWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	n, err := w.buffer.Write(p)
	if strings.Contains(w.buffer.String(), "SSH-FIXTURE-STARTED") {
		w.once.Do(func() { close(w.started) })
	}
	return n, err
}

// Opt-in real OpenSSH/SFTP acceptance. The runner creates two disposable servers
// and a disposable client container: the product's fixed /tmp master socket and
// /home/aide/known_hosts must never be shared with a running Aide instance.
func TestSSHLiveWorkspaceAndIndependentSource(t *testing.T) {
	hostA, hostB := os.Getenv("AIDE_SSH_FIXTURE_A"), os.Getenv("AIDE_SSH_FIXTURE_B")
	if hostA == "" || hostB == "" {
		t.Skip("run scripts/test-ssh-workspace.sh to start two isolated SSH fixtures")
	}
	if hostA == hostB || os.Getenv("AIDE_SSH_TEST_ISOLATED") != "1" {
		t.Fatal("two distinct fixture hosts and an isolated client container are required")
	}
	a := testApp(t)
	run := newID()
	workspace := "/home/fixture/项目 space-" + run
	docsAbs := "/home/fixture/docs absolute-" + run
	cacheAbs := "/home/fixture/cache absolute-" + run
	config := func(t *testing.T, workPath, docsLocation, docsPath, cacheLocation, cachePath string) {
		t.Helper()
		requireStatus(t, request(a, "PUT", "/api/workspace-config", map[string]any{
			"workspace": map[string]any{"mode": "ssh", "host": hostA, "port": 22, "username": "fixture", "auth": "password", "path": workPath},
			"docs":      map[string]any{"location": docsLocation, "path": docsPath},
			"cache":     map[string]any{"location": cacheLocation, "path": cachePath},
			"password":  "fixture-pass",
		}), 200)
	}
	remote := func(t *testing.T, command string) string {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if err := a.ensureSSHSession(ctx); err != nil {
			t.Fatal(err)
		}
		var stdout, stderr bytes.Buffer
		code, err := a.execRemote(ctx, command, &stdout, &stderr)
		if err != nil || code != 0 {
			t.Fatalf("fixture preparation/read failed: status=%d err=%v stderr=%s", code, err, stderr.String())
		}
		return stdout.String()
	}
	readFile := func(t *testing.T, source, file string) string {
		t.Helper()
		query := url.Values{"path": []string{file}}
		if source != "" {
			query.Set("source", source)
		} else {
			query.Set("root", "workspace")
		}
		w := request(a, "GET", "/api/file?"+query.Encode(), nil)
		requireStatus(t, w, 200)
		return w.Body.String()
	}
	config(t, "", "local", "", "local", "")
	remote(t, "mkdir -p "+shellQuote(path.Join(workspace, "中文 子目录"))+" "+shellQuote(path.Join(workspace, "docs relative"))+" "+shellQuote(docsAbs)+" "+shellQuote(cacheAbs)+
		" && printf '%s' 'A workspace body' > "+shellQuote(path.Join(workspace, "中文 子目录", "设计 notes.md"))+
		" && printf '%s' 'A relative docs body' > "+shellQuote(path.Join(workspace, "docs relative", "relative.md"))+
		" && printf '%s' 'A absolute docs body' > "+shellQuote(path.Join(docsAbs, "absolute.md")))

	t.Run("empty_workspace_is_home", func(t *testing.T) {
		w := request(a, "POST", "/api/workspace-config/test", map[string]any{})
		requireStatus(t, w, 200)
		if !strings.Contains(w.Body.String(), `"ok":true`) {
			t.Fatal(w.Body.String())
		}
		if a.workspaceRemotePath(".") != "." {
			t.Fatalf("empty workspace root should be home '.', got %q", a.workspaceRemotePath("."))
		}
	})
	config(t, workspace, "local", "", "local", "")
	t.Run("unicode_space_list_read_write", func(t *testing.T) {
		w := request(a, "GET", "/api/files?root=workspace&path=.", nil)
		requireStatus(t, w, 200)
		if !strings.Contains(w.Body.String(), "中文 子目录") {
			t.Fatal("Unicode directory was not listed:", w.Body.String())
		}
		if body := readFile(t, "", "中文 子目录/设计 notes.md"); !strings.Contains(body, "A workspace body") {
			t.Fatal("wrong workspace body:", body)
		}
		p := "新建 空间/二层/round trip.md"
		if _, err := a.readWorkspaceText(p); err != nil {
			t.Logf("new workspace file pre-read: %v", err)
		}
		requireStatus(t, request(a, "PUT", "/api/file", map[string]string{"root": "workspace", "path": p, "content": "written over real SFTP"}), 200)
		if body := readFile(t, "", p); !strings.Contains(body, "written over real SFTP") {
			t.Fatal("write roundtrip mismatch:", body)
		}
	})
	t.Run("concurrent_first_connection", func(t *testing.T) {
		a.killSSHSession()
		start := make(chan struct{})
		errs := make(chan error, 8)
		var wg sync.WaitGroup
		for i := 0; i < 8; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				items, err := a.sftpList(".")
				if err == nil && len(items) == 0 {
					err = fmt.Errorf("empty real directory listing")
				}
				errs <- err
			}()
		}
		close(start)
		wg.Wait()
		close(errs)
		for err := range errs {
			if err != nil {
				t.Error(err)
			}
		}
	})
	t.Run("closed_master_recovers", func(t *testing.T) {
		a.killSSHSession()
		if body := readFile(t, "", "中文 子目录/设计 notes.md"); !strings.Contains(body, "A workspace body") {
			t.Fatal("master recovery returned wrong file:", body)
		}
	})
	t.Run("stale_workspace_socket_recovers", func(t *testing.T) {
		a.killSSHSession()
		leaveStaleSSHSocket(t, sshControlSocket)
		before, _ := os.Lstat(sshControlSocket)
		if body := readFile(t, "", "中文 子目录/设计 notes.md"); !strings.Contains(body, "A workspace body") {
			t.Fatal("stale workspace socket recovery returned wrong file:", body)
		}
		after, err := os.Lstat(sshControlSocket)
		if err != nil || os.SameFile(before, after) {
			t.Fatalf("stale workspace socket must be replaced by a verified real master: %v", err)
		}
	})
	t.Run("encrypted_private_key_paste_and_reference", func(t *testing.T) {
		keyPath := filepath.Join(a.workPath, "fixture-ed25519")
		if out, err := exec.Command("ssh-keygen", "-q", "-t", "ed25519", "-a", "1", "-N", "fixture-key-passphrase", "-f", keyPath).CombinedOutput(); err != nil {
			t.Fatalf("disposable fixture key generation failed: %v %s", err, out)
		}
		pub, err := os.ReadFile(keyPath + ".pub")
		if err != nil {
			t.Fatal(err)
		}
		key, err := os.ReadFile(keyPath)
		if err != nil {
			t.Fatal(err)
		}
		remote(t, "mkdir -p /home/fixture/.ssh && chmod 0700 /home/fixture/.ssh && printf '%s\\n' "+shellQuote(strings.TrimSpace(string(pub)))+" >> /home/fixture/.ssh/authorized_keys && chmod 0600 /home/fixture/.ssh/authorized_keys")
		for _, mode := range []string{"paste", "ref"} {
			payload := map[string]any{
				"workspace":  map[string]any{"mode": "ssh", "host": hostA, "port": 22, "username": "fixture", "auth": "key", "path": workspace},
				"docs":       map[string]any{"location": "local", "path": ""},
				"cache":      map[string]any{"location": "local", "path": ""},
				"passphrase": "fixture-key-passphrase", "keyMode": mode,
			}
			if mode == "paste" {
				payload["key"] = string(key)
			} else {
				payload["keyRefPath"] = "/workspace/fixture-ed25519"
			}
			requireStatus(t, request(a, "PUT", "/api/workspace-config", payload), 200)
			requireStatus(t, request(a, "POST", "/api/workspace-config/test", map[string]any{}), 200)
			if body := readFile(t, "", "中文 子目录/设计 notes.md"); !strings.Contains(body, "A workspace body") {
				t.Fatal(mode+": encrypted private key did not read the workspace", body)
			}
		}
		config(t, workspace, "local", "", "local", "")
	})
	t.Run("caller_deadline_is_respected", func(t *testing.T) {
		ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
		defer cancel()
		started := time.Now()
		err := a.ensureSSHSession(ctx)
		if !errors.Is(err, context.DeadlineExceeded) || time.Since(started) > time.Second {
			t.Fatalf("expired caller deadline must be preserved promptly: elapsed=%s err=%v", time.Since(started), err)
		}
	})
	t.Run("missing_and_permission_errors", func(t *testing.T) {
		_, err := a.sftpList("not-present-" + run)
		if err == nil || !strings.Contains(strings.ToLower(err.Error()), "no such file") {
			t.Fatalf("missing directory must remain a missing-path error, got %v", err)
		}
		err = a.sftpWrite("/srv/ssh-denied/new.md", []byte("must not succeed"))
		if err == nil || !strings.Contains(strings.ToLower(err.Error()), "permission denied") {
			t.Fatalf("write denial must remain a permission error, got %v", err)
		}
		_, err = a.sftpListRemote("/srv/ssh-list-denied", ".")
		if err == nil || !strings.Contains(strings.ToLower(err.Error()), "permission denied") {
			out, batchErr := a.sftpBatch("cd " + shellQuoteRemote("/srv/ssh-list-denied") + "\nls -l\n")
			t.Errorf("directory-list denial must remain a permission error, got %v; raw batch err=%v output=%q", err, batchErr, out)
		}
		config(t, workspace+"-not-present", "local", "", "local", "")
		defer config(t, workspace, "local", "", "local", "")
		w := request(a, "POST", "/api/workspace-config/test", map[string]any{})
		requireStatus(t, w, 400)
		if !strings.Contains(strings.ToLower(w.Body.String()), "no such file") {
			t.Fatal("saved missing workspace must report a path error:", w.Body.String())
		}
	})
	t.Run("independent_source_server_b", func(t *testing.T) {
		id := "live-server-b-" + run
		requireStatus(t, request(a, "PUT", "/api/sources", map[string]any{
			"sources": []any{sourceBody(id, "server B", "sftp", map[string]any{"host": hostB, "port": 22, "username": "fixture", "auth": "password", "path": "/srv/refs"}, true)},
			"secrets": map[string]any{id: map[string]any{"password": "fixture-pass"}},
		}), 200)
		if body := readFile(t, id, "server-id.txt"); !strings.Contains(body, "SERVER-B") || strings.Contains(body, "SERVER-A") {
			t.Fatal("source connection was not isolated to B:", body)
		}
		file := "nested/中文 空格-" + run + ".md"
		requireStatus(t, request(a, "PUT", "/api/file", map[string]string{"source": id, "path": file, "content": "B-only independent write"}), 200)
		if body := readFile(t, id, file); !strings.Contains(body, "B-only independent write") {
			t.Fatal("independent source write did not roundtrip:", body)
		}
		if _, err := a.sftpRead("/srv/refs/" + file); err == nil {
			t.Fatal("server B source write incorrectly appeared on server A")
		}
		if body := readFile(t, "", "中文 子目录/设计 notes.md"); !strings.Contains(body, "A workspace body") {
			t.Fatal("opening source B redirected the workspace:", body)
		}
		keyPath := filepath.Join(a.workPath, "fixture-source-ed25519")
		if out, err := exec.Command("ssh-keygen", "-q", "-t", "ed25519", "-N", "", "-f", keyPath).CombinedOutput(); err != nil {
			t.Fatalf("disposable source key generation failed: %v %s", err, out)
		}
		pub, err := os.ReadFile(keyPath + ".pub")
		if err != nil {
			t.Fatal(err)
		}
		key, err := os.ReadFile(keyPath)
		if err != nil {
			t.Fatal(err)
		}
		saveSource := func(auth, base string, secret map[string]any) {
			t.Helper()
			requireStatus(t, request(a, "PUT", "/api/sources", map[string]any{
				"sources": []any{sourceBody(id, "server B", "sftp", map[string]any{"host": hostB, "port": 22, "username": "fixture", "auth": auth, "path": base}, true)},
				"secrets": map[string]any{id: secret},
			}), 200)
		}
		saveSource("password", "/home/fixture", map[string]any{"password": "fixture-pass"})
		// Repeat runs share the disposable server. Existing files require their
		// observed version; never disable the product's overwrite protection.
		keyHash := ""
		q := url.Values{"source": {id}, "path": {".ssh/authorized_keys"}}
		current := request(a, "GET", "/api/file?"+q.Encode(), nil)
		if current.Code == 200 {
			var file map[string]string
			if err := json.Unmarshal(current.Body.Bytes(), &file); err != nil {
				t.Fatal(err)
			}
			keyHash = file["hash"]
		} else if current.Code != 400 || (!strings.Contains(current.Body.String(), "not found") && !strings.Contains(current.Body.String(), "No such file")) {
			t.Fatalf("cannot observe disposable key file: %s", current.Body.String())
		}
		requireStatus(t, request(a, "PUT", "/api/file", map[string]string{"source": id, "path": ".ssh/authorized_keys", "content": string(pub), "hash": keyHash}), 200)
		saveSource("key", "/srv/refs", map[string]any{"key": string(key)})
		if body := readFile(t, id, "server-id.txt"); !strings.Contains(body, "SERVER-B") {
			t.Fatal("source key authentication did not reach server B:", body)
		}
		// Updating just the password must retain the already sealed private key.
		saveSource("key", "/srv/refs", map[string]any{"password": "fixture-pass"})
		if body := readFile(t, id, "server-id.txt"); !strings.Contains(body, "SERVER-B") {
			t.Fatal("partial source credential update lost key authentication:", body)
		}
		t.Run("stale_source_socket_recovers", func(t *testing.T) {
			a.killSourceSession(id, "fixture@"+hostB)
			sock := sourceSocket(id)
			leaveStaleSSHSocket(t, sock)
			before, _ := os.Lstat(sock)
			if body := readFile(t, id, "server-id.txt"); !strings.Contains(body, "SERVER-B") {
				t.Fatal("stale source socket recovery returned wrong server:", body)
			}
			after, err := os.Lstat(sock)
			if err != nil || os.SameFile(before, after) {
				t.Fatalf("stale source socket must be replaced by a verified real master: %v", err)
			}
		})
	})
	t.Run("docs_relative_and_absolute_share_server_a", func(t *testing.T) {
		config(t, workspace, "workspace", "docs relative", "local", "")
		if body := readFile(t, systemDocsSource, "relative.md"); !strings.Contains(body, "A relative docs body") {
			t.Fatal("relative system docs mismatch:", body)
		}
		config(t, workspace, "workspace", docsAbs, "local", "")
		if body := readFile(t, systemDocsSource, "absolute.md"); !strings.Contains(body, "A absolute docs body") {
			t.Fatal("absolute system docs mismatch:", body)
		}
		dir := "创建 目录-" + run
		requireStatus(t, request(a, "POST", "/api/directory", directoryRequest{Root: "context", Source: systemDocsSource, Parent: ".", Name: dir}), 200)
		query := url.Values{"source": []string{systemDocsSource}, "path": []string{"."}}
		w := request(a, "GET", "/api/files?"+query.Encode(), nil)
		requireStatus(t, w, 200)
		if !strings.Contains(w.Body.String(), dir) {
			t.Fatal("absolute docs directory was created outside its configured root:", w.Body.String())
		}
		requireStatus(t, request(a, "POST", "/api/file/transfer", fileTransferRequest{Operation: "copy", Source: "workspace", Paths: []string{"中文 子目录/设计 notes.md"}, Destination: systemDocsSource, DestinationPath: dir}), 200)
		copied := path.Join(dir, "设计 notes.md")
		if body := readFile(t, systemDocsSource, copied); !strings.Contains(body, "A workspace body") {
			t.Fatal("copy to the absolute docs root did not preserve content:", body)
		}
		requireStatus(t, request(a, "POST", "/api/file/transfer", fileTransferRequest{Operation: "move", Source: systemDocsSource, Paths: []string{copied}, Destination: "workspace", DestinationPath: "."}), 200)
		if body := readFile(t, "", "设计 notes.md"); !strings.Contains(body, "A workspace body") {
			t.Fatal("move from the absolute docs root did not preserve content:", body)
		}
		query.Set("path", copied)
		w = request(a, "GET", "/api/file?"+query.Encode(), nil)
		requireStatus(t, w, 400)
		if body := strings.ToLower(w.Body.String()); !strings.Contains(body, "not found") && !strings.Contains(body, "no such file") {
			t.Fatal("move source must be missing, without a connection or permission failure:", w.Body.String())
		}
	})
	t.Run("absolute_cache_push_pull_and_local_mirror", func(t *testing.T) {
		// Unbound documents retain the legacy cache destination. Bound documents
		// are tested separately; they must follow the selected system-docs root.
		config(t, workspace, "workspace", "", "workspace", cacheAbs)
		if got := a.workspaceRemoteCachePath(); got != cacheAbs {
			t.Fatalf("absolute cache path changed: got %q want %q", got, cacheAbs)
		}
		if strings.HasPrefix(a.cacheContainer, a.workPath+string(os.PathSeparator)) || !strings.HasPrefix(a.cacheContainer, a.dataPath+string(os.PathSeparator)) {
			t.Fatal("remote working copy must live in disposable application data:", a.cacheContainer)
		}
		out := a.createDesign("real ssh cache "+run, "## 开发流程\nSSH fixture acceptance", "", "")
		if !strings.Contains(out, "已建档") || !strings.Contains(out, cacheAbs+"/system-docs/designs/") {
			t.Fatal("remote cache design failed:", out)
		}
		dir := filepath.Join(a.cacheContainer, "system-docs", "designs")
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		var name string
		for _, entry := range entries {
			if strings.HasPrefix(entry.Name(), "DESIGN-") {
				name = entry.Name()
				break
			}
		}
		if name == "" {
			t.Fatal("local working copy missing generated design")
		}
		local, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		b, err := a.sftpRead(path.Join(cacheAbs, "system-docs/designs", name))
		if err != nil || !bytes.Equal(b, local) {
			t.Fatalf("remote design differs from local working copy: %v", err)
		}
		if err := os.Remove(filepath.Join(dir, name)); err != nil {
			t.Fatal(err)
		}
		if err := a.pullProjectCacheDir("system-docs/designs"); err != nil {
			t.Fatal(err)
		}
		pulled, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil || !bytes.Equal(pulled, local) {
			t.Fatalf("remote cache pull did not restore the working copy: %v", err)
		}
	})
	t.Run("continuity_history_and_memory", func(t *testing.T) {
		config(t, workspace, "workspace", docsAbs, "workspace", cacheAbs)
		runSSHContinuity(t, a, workspace, docsAbs, cacheAbs, "live-server-b-"+run, remote)
	})
	t.Run("command_exit_status", func(t *testing.T) {
		for _, test := range []struct {
			command string
			want    int
		}{{"false", 1}, {"exit 7", 7}} {
			w := request(a, "POST", "/api/command", map[string]any{"command": test.command})
			requireStatus(t, w, 200)
			decoder := json.NewDecoder(strings.NewReader(w.Body.String()))
			seen := false
			for decoder.More() {
				var event struct {
					Type string `json:"type"`
					Code int    `json:"code"`
				}
				if err := decoder.Decode(&event); err != nil {
					t.Fatal(err)
				}
				if event.Type == "exit" {
					seen = true
					if event.Code != test.want {
						t.Errorf("%s: exit=%d want=%d", test.command, event.Code, test.want)
					}
				}
			}
			if !seen {
				t.Fatal("missing command exit event")
			}
		}
	})
	t.Run("cancellation_stops_remote_process_group", func(t *testing.T) {
		if err := a.ensureSSHSession(context.Background()); err != nil {
			t.Fatal(err)
		}
		parentFile := path.Join(workspace, "cancel-parent.pid")
		childFile := path.Join(workspace, "cancel-child.pid")
		command := "printf '%s' $$ > " + shellQuote(parentFile) + "; sleep 30 & child=$!; printf '%s' \"$child\" > " + shellQuote(childFile) + "; echo SSH-FIXTURE-STARTED; wait \"$child\""
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		stdout := &sshLiveStartWriter{started: make(chan struct{})}
		var stderr bytes.Buffer
		type execution struct {
			code int
			err  error
		}
		done := make(chan execution, 1)
		go func() {
			code, err := a.execRemote(ctx, command, stdout, &stderr)
			done <- execution{code, err}
		}()
		select {
		case <-stdout.started:
		case result := <-done:
			t.Fatalf("command stopped before process group was observed: status=%d err=%v", result.code, result.err)
		case <-time.After(5 * time.Second):
			cancel()
			t.Fatal("remote process did not announce startup")
		}
		cancelled := time.Now()
		cancel()
		select {
		case result := <-done:
			if result.code != -1 || result.err == nil || !strings.Contains(result.err.Error(), "取消") {
				t.Fatalf("remote cancellation result: status=%d err=%v", result.code, result.err)
			}
			t.Logf("remote cancellation receipt: %v", result.err)
		case <-time.After(8 * time.Second):
			t.Fatal("remote cancellation did not finish within cleanup deadline")
		}
		stopped := true
		for _, file := range []string{parentFile, childFile} {
			// A defunct process may await PID-1 reaping in the fixture container, but
			// no runnable shell or child may survive the cancellation.
			out := remote(t, "p=$(cat "+shellQuote(file)+"); state=$(ps -o stat= -p \"$p\" 2>/dev/null | tr -d '[:space:]'); case \"$state\" in ''|Z*) printf 'stopped pid=%s state=%s' \"$p\" \"${state:-absent}\";; *) printf 'alive pid=%s state=%s' \"$p\" \"$state\";; esac")
			t.Logf("observed cancellation process state: %s", out)
			if !strings.HasPrefix(out, "stopped pid=") {
				stopped = false
				t.Errorf("remote process remained runnable: %s", out)
			}
		}
		if stopped {
			t.Logf("observed shell and child stopped after cancellation; elapsed=%s", time.Since(cancelled))
		}
	})
}
