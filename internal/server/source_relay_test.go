package server

import (
	"bufio"
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"math/big"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func relayTestSource(t *testing.T, a *App, root string, rw bool) Source {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	src := Source{ID: "relay-test", Name: "Relay fixture", Type: "relay", Enabled: true, RW: rw, Config: SourceConfig{Host: "127.0.0.1", Port: port, Path: root}}
	relayTestSave(t, a, src, "")
	return src
}
func relayTestSave(t *testing.T, a *App, src Source, key string) {
	t.Helper()
	payload := map[string]any{"sources": []Source{src}}
	if key != "" {
		payload["secrets"] = map[string]any{src.ID: map[string]string{"key": key}}
	}
	requireStatus(t, request(a, "PUT", "/api/sources", payload), 200)
}
func relayTestStart(t *testing.T, a *App, src Source) {
	t.Helper()
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Fatal("Python 3 + cryptography required for relay integration:", err)
	}
	if out, err := exec.Command(python, "-c", "import cryptography").CombinedOutput(); err != nil {
		t.Fatalf("relay test dependency: %v %s", err, out)
	}
	response := request(a, "GET", "/api/sources/"+src.ID+"/relay-script", nil)
	requireStatus(t, response, 200)
	if response.Header().Get("Cache-Control") != "no-store" || !strings.Contains(response.Header().Get("Content-Disposition"), ".py") {
		t.Fatal("script cache/download headers")
	}
	script := filepath.Join(src.Config.Path, "relay-agent.py")
	if err := os.WriteFile(script, response.Body.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(python, "-u", script, "--listen-host", "127.0.0.1")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stderr = stdoutWriter{t: t}
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	ready := make(chan bool, 1)
	go func() {
		scanner := bufio.NewScanner(stdout)
		ready <- scanner.Scan() && strings.Contains(scanner.Text(), "ready on TCP port")
	}()
	select {
	case ok := <-ready:
		if !ok {
			t.Fatal("downloaded agent failed startup")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("agent startup timeout")
	}
}

type stdoutWriter struct{ t *testing.T }

func (w stdoutWriter) Write(p []byte) (int, error) { w.t.Log(string(p)); return len(p), nil }

func TestReferenceRelayDownloadedAgent(t *testing.T) {
	a := testApp(t)
	root := t.TempDir()
	src := relayTestSource(t, a, root, true)
	requireStatus(t, request(a, "GET", "/api/sources/"+src.ID+"/relay-script", nil), 200)
	offline := request(a, "POST", "/api/sources/"+src.ID+"/relay-probe", nil)
	requireStatus(t, offline, 200)
	if !strings.Contains(offline.Body.String(), `"online":false`) {
		t.Fatal(offline.Body.String())
	}
	relayTestStart(t, a, src)
	requireStatus(t, request(a, "POST", "/api/sources/"+src.ID+"/relay-probe", nil), 200)
	ctx := context.Background()
	if err := a.relayWriteSource(src, "目录/space name.md", []byte("hello 中继"), true); err != nil {
		t.Fatal(err)
	}
	if err := a.relayWriteSource(src, "目录/space name.md", []byte("overwritten"), true); !errors.Is(err, os.ErrExist) {
		t.Fatal("exclusive", err)
	}
	w := request(a, "GET", "/api/files?source="+src.ID+"&path=.", nil)
	requireStatus(t, w, 200)
	if strings.Contains(w.Body.String(), "relay-agent.py") {
		t.Fatal("secret-bearing script must be hidden")
	}
	w = request(a, "GET", "/api/file?source="+src.ID+"&path=目录/space%20name.md", nil)
	requireStatus(t, w, 200)
	if !strings.Contains(w.Body.String(), "hello 中继") {
		t.Fatal(w.Body.String())
	}
	requireStatus(t, request(a, "PUT", "/api/file", map[string]string{"source": src.ID, "path": "目录/space name.md", "content": "edited", "hash": hash([]byte("hello 中继"))}), 200)
	data, err := a.relayReadSource(ctx, src, "目录/space name.md", 100)
	if err != nil || string(data) != "edited" {
		t.Fatal(string(data), err)
	}
	if _, err = a.relayReadSource(ctx, src, "relay-agent.py", sourceRelayMaxFile); err == nil {
		t.Fatal("download script exposed")
	}
	if err = a.relayMutateSource(src, "rename", "目录/space name.md", "目录/renamed.md"); err != nil {
		t.Fatal(err)
	}
	if err = a.relayMutateSource(src, "mkdir", "empty", ""); err != nil {
		t.Fatal(err)
	}
	if err = a.relayMutateSource(src, "remove", "empty", ""); err != nil {
		t.Fatal(err)
	}
	// The same native driver supplies retrieval/indexing and the existing AI gates.
	a.mu.Lock()
	areas := a.knowledgeAreasLocked(a.wsID(), a.workPath, a.refPath)
	a.mu.Unlock()
	var area knowledgeArea
	for _, v := range areas {
		if v.source == src.ID {
			area = v
		}
	}
	if _, err = a.knowledgeAreaRead(ctx, area, "目录/renamed.md", 100); err != nil {
		t.Fatal(err)
	}
	requireStatus(t, request(a, "PUT", "/api/sources/ai-visibility", map[string]any{"source": src.ID, "workspaceId": a.wsID(), "visible": false}), 200)
	for _, name := range []string{"read_file", "list_files", "semantic_search"} {
		got := a.executeToolCall(ctx, readCall(name, `{"source":"relay-test","path":"目录/renamed.md","query":"edited"}`), &Task{}, nil)
		if !strings.Contains(got, "disabled") {
			t.Fatal("hidden relay accessible to AI", name, got)
		}
	}
	requireStatus(t, request(a, "GET", "/api/file?source="+src.ID+"&path=目录/renamed.md", nil), 200)
	for _, p := range []string{"../escape", "/etc/passwd", `C:\Windows`, "目录/../x", "a//b", ".aide-relay-x"} {
		if _, err = a.relayReadSource(ctx, src, p, 100); err == nil {
			t.Fatalf("path allowed: %q", p)
		}
	}
	outside := t.TempDir()
	os.WriteFile(filepath.Join(outside, "secret"), []byte("outside"), 0600)
	if err = os.Symlink(outside, filepath.Join(root, "linked")); err == nil {
		if _, err = a.relayReadSource(ctx, src, "linked/secret", 100); err == nil {
			t.Fatal("symlink escaped root")
		}
	}
	// Pinning refuses a different credential before the file protocol starts.
	a.mu.Lock()
	key, _ := a.relayKeyLocked(src.ID)
	a.mu.Unlock()
	metadata := request(a, "GET", "/api/sources", nil).Body.String()
	if strings.Contains(metadata, base64.StdEncoding.EncodeToString(key)) || strings.Contains(metadata, `"key":`) {
		t.Fatal("credential exposed in metadata")
	}
	stale, err := a.newSourceRelayFS(ctx, src, false)
	if err != nil {
		t.Fatal(err)
	}
	defer stale.Close()
	cached, err := stale.Open("目录/renamed.md", os.O_RDONLY)
	if err != nil {
		t.Fatal(err)
	}
	b := make([]byte, 2)
	if _, err = cached.ReadAt(b, 0); err != nil {
		t.Fatal(err)
	}
	rotated, _ := newSourceRelayKey()
	relayTestSave(t, a, src, rotated)
	if _, err = a.knowledgeAreaRead(ctx, area, "目录/renamed.md", 100); err == nil {
		t.Fatal("stale retrieval origin survived key change")
	}
	if _, err = cached.ReadAt(b, 0); err == nil {
		t.Fatal("cached reader survived key rotation")
	}
	if _, err = a.relayReadSource(ctx, src, "目录/renamed.md", 100); err == nil {
		t.Fatal("old agent accepted new key")
	}
	// Restore the same key to exercise config and host permission revocation.
	oldKey := base64.StdEncoding.EncodeToString(key)
	zeroBytes(key)
	relayTestSave(t, a, src, oldKey)
	changed := src
	changed.Config.Path = root + "-changed"
	relayTestSave(t, a, changed, "")
	if _, err = a.relayReadSource(ctx, changed, "目录/renamed.md", 100); err == nil {
		t.Fatal("old deployment accepted changed root")
	}
	relayTestSave(t, a, src, "")
	src.RW = false
	relayTestSave(t, a, src, "")
	if err = a.relayWriteSource(src, "denied.md", []byte("no"), false); err == nil {
		t.Fatal("host read-only ignored")
	}
	src.Enabled = false
	relayTestSave(t, a, src, "")
	if _, err = a.relayReadSource(ctx, src, "目录/renamed.md", 100); err == nil {
		t.Fatal("disabled source readable")
	}
}

func TestReferenceRelayRemoteReadOnly(t *testing.T) {
	a := testApp(t)
	src := relayTestSource(t, a, t.TempDir(), false)
	relayTestStart(t, a, src)
	src.RW = true
	relayTestSave(t, a, src, "")
	if err := a.relayWriteSource(src, "no.md", []byte("no"), false); err == nil {
		t.Fatal("deployed RO script allowed write")
	}
	probe := request(a, "POST", "/api/sources/"+src.ID+"/relay-probe", nil)
	requireStatus(t, probe, 200)
	if !strings.Contains(probe.Body.String(), `"remoteWritable":false`) {
		t.Fatal(probe.Body.String())
	}
}

func TestReferenceRelayStreamingAndRevocation(t *testing.T) {
	a := testApp(t)
	src := relayTestSource(t, a, t.TempDir(), true)
	relayTestStart(t, a, src)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	fs, err := a.newSourceRelayFS(ctx, src, true)
	if err != nil {
		t.Fatal(err)
	}
	defer fs.Close()
	file, err := fs.Open("stream.bin", os.O_RDWR|os.O_CREATE|os.O_EXCL)
	if err != nil {
		t.Fatal(err)
	}
	payload := bytes.Repeat([]byte("streaming-payload"), 600000)
	if n, err := file.WriteAt(payload, 0); err != nil || n != len(payload) {
		t.Fatal(n, err)
	}
	if err = file.Sync(); err != nil {
		t.Fatal(err)
	}
	got := make([]byte, len(payload)+1)
	n, err := file.ReadAt(got, 0)
	if !errors.Is(err, io.EOF) || n != len(payload) || !bytes.Equal(got[:n], payload) {
		t.Fatalf("stream read n=%d err=%v", n, err)
	}
	if _, err = file.Seek(7, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	small := make([]byte, 20)
	if n, err = file.Read(small); err != nil || n != 20 || !bytes.Equal(small, payload[7:27]) {
		t.Fatal("seek/read", n, err)
	}
	if err = file.Truncate(100); err != nil {
		t.Fatal(err)
	}
	info, err := file.Stat()
	if err != nil || info.Size() != 100 {
		t.Fatal(info, err)
	}
	if err = file.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err = file.ReadAt(small, 0); !errors.Is(err, os.ErrClosed) {
		t.Fatal("closed cached read", err)
	}
	if err = fs.mutate("rename", "stream.bin", "published.bin"); err != nil {
		t.Fatal(err)
	}
	rd, err := fs.ReadDir(".")
	if err != nil || len(rd) != 1 || rd[0].Size() != 100 {
		t.Fatal(rd, err)
	}
	file, err = fs.Open("published.bin", os.O_RDONLY)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = file.ReadAt(small, 0); err != nil {
		t.Fatal(err)
	}
	cancel()
	if _, err = file.ReadAt(small, 0); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled cached read", err)
	}
}

func TestReferenceRelayDurableTransfer(t *testing.T) {
	a := testApp(t)
	src := relayTestSource(t, a, t.TempDir(), true)
	relayTestStart(t, a, src)
	m := a.transfers()
	size := int64(65<<20) + 37
	j, err := m.create(context.Background(), transferCreate{Operation: "upload", Destination: src.ID, Name: "large.bin", Size: size})
	if err != nil {
		t.Fatal(err)
	}
	requireStatus(t, transferTestPut(a, j.ID, 1, transferTestChunk(1, transferChunkSize)), 200)
	testTransferRestart(a)
	m = a.transfers()
	j = m.jobs[j.ID]
	if j.Status != "paused" || len(j.UploadChunks) != 1 {
		t.Fatal("resume journal", j.Status, len(j.UploadChunks))
	}
	requireStatus(t, request(a, "POST", "/api/file/transfers/"+j.ID+"/resume", map[string]any{}), 200)
	for n := int64(0); n*transferChunkSize < size; n++ {
		if n != 1 {
			requireStatus(t, transferTestPut(a, j.ID, n, transferTestChunk(n, min(transferChunkSize, size-n*transferChunkSize))), 200)
		}
	}
	if _, err = os.Stat(filepath.Join(src.Config.Path, "large.bin")); !os.IsNotExist(err) {
		t.Fatal("partial file published", err)
	}
	requireStatus(t, request(a, "POST", "/api/file/transfers/"+j.ID+"/commit", map[string]any{}), 200)
	testTransferWait(t, m, j)
	if j.Status != "complete" {
		t.Fatal(j.Status, j.Error)
	}
	f, err := os.Open(filepath.Join(src.Config.Path, "large.bin"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	expected := make([]byte, 37)
	f.ReadAt(expected, 65<<20)
	if !bytes.Equal(expected, transferTestChunk(16, 37)) {
		t.Fatal("large upload content")
	}
}

func TestReferenceRelayProtocolBounds(t *testing.T) {
	for _, line := range []string{"not-json\n", strings.Repeat("x", 4096) + "\n", "{\"ok\":false}\n"} {
		if _, err := relayReadHeader(bufio.NewReaderSize(strings.NewReader(line), 4096), true); err == nil {
			t.Fatal("bad header accepted")
		}
	}
	for _, cfg := range []SourceConfig{{Host: "https://host", Path: "/x"}, {Host: "host", Path: "relative"}, {Host: "host", Path: "/x", Port: 65536}} {
		src := Source{Config: cfg}
		if validateSourceRelay(&src) == nil {
			t.Fatal("invalid config", cfg)
		}
	}
	// Ordinary metadata must never serialize the generated script configuration.
	a := testApp(t)
	src := relayTestSource(t, a, t.TempDir(), false)
	var result map[string]any
	if err := json.Unmarshal(request(a, "GET", "/api/sources", nil).Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if src.RW {
		t.Fatal("default lock")
	}
}

func TestReferenceRelayVaultAndProposalBinding(t *testing.T) {
	a := testApp(t)
	src := relayTestSource(t, a, t.TempDir(), true)
	relayTestStart(t, a, src)
	a.mu.Lock()
	key, err := a.relayKeyLocked(src.ID)
	a.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	defer zeroBytes(key)
	if !a.vault.Has(sourceVaultID(src.ID)) {
		t.Fatal("generated key missing from vault")
	}
	onDisk, err := os.ReadFile(SourcesSecretsPath(a.dataPath))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(onDisk, []byte(base64.StdEncoding.EncodeToString(key))) {
		t.Fatal("unlocked vault leaked key to plaintext fallback")
	}
	a.mu.Lock()
	a.vault.Lock()
	a.mu.Unlock()
	relayTestSave(t, a, src, "")
	requireStatus(t, request(a, "GET", "/api/sources/"+src.ID+"/relay-script", nil), 423)
	if _, err = a.sourceRelayCredentials(src, false); err == nil {
		t.Fatal("locked vault readable")
	}
	a.mu.Lock()
	err = a.unlockVaultAtStartup()
	a.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	a.mu.Lock()
	same, err := a.relayKeyLocked(src.ID)
	a.mu.Unlock()
	if err != nil || !bytes.Equal(same, key) {
		t.Fatal("locked save rotated existing key", err)
	}
	zeroBytes(same)
	s, task := documentProposalTask(a)
	if _, err = a.recordToolProposal(task, nil, map[string]any{"type": "file", "source": src.ID, "path": "proposal.md", "content": "proposed"}); err != nil {
		t.Fatal(err)
	}
	requireStatus(t, request(a, "POST", "/api/sessions/"+s.ID+"/runs/"+task.ID+"/apply", nil), 200)
	content, err := os.ReadFile(filepath.Join(src.Config.Path, "proposal.md"))
	if err != nil || string(content) != "proposed" {
		t.Fatal(string(content), err)
	}
	_, write, closeWriter, err := a.historyRestoreWriter(src.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer closeWriter()
	if err = write("restored.md", []byte("restored")); err != nil {
		t.Fatal(err)
	}
	// An already-created history writer and proposal cannot retarget after rotation.
	s, task = documentProposalTask(a)
	if _, err = a.recordToolProposal(task, nil, map[string]any{"type": "file", "source": src.ID, "path": "stale.md", "content": "stale"}); err != nil {
		t.Fatal(err)
	}
	rotated, _ := newSourceRelayKey()
	relayTestSave(t, a, src, rotated)
	if err = write("blocked.md", []byte("blocked")); err == nil {
		t.Fatal("stale history writer survived rotation")
	}
	requireStatus(t, request(a, "POST", "/api/sessions/"+s.ID+"/runs/"+task.ID+"/apply", nil), 409)
}

func TestReferenceRelayUntrustedPayloadAndCancellation(t *testing.T) {
	for _, mode := range []string{"oversize", "checksum", "truncated", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			a := testApp(t)
			src := relayTestSource(t, a, t.TempDir(), false)
			a.mu.Lock()
			key, err := a.relayKeyLocked(src.ID)
			a.mu.Unlock()
			if err != nil {
				t.Fatal(err)
			}
			defer zeroBytes(key)
			h := hmac.New(sha256.New, key)
			h.Write([]byte("aide-file-relay-certificate-v1"))
			private := ed25519.NewKeyFromSeed(h.Sum(nil))
			template := &x509.Certificate{SerialNumber: big.NewInt(1), NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour)}
			der, err := x509.CreateCertificate(rand.Reader, template, template, private.Public(), private)
			if err != nil {
				t.Fatal(err)
			}
			listener, err := tls.Listen("tcp", net.JoinHostPort(src.Config.Host, fmtTransferIndex(int64(src.Config.Port))), &tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: private}}})
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			done := make(chan struct{})
			release := make(chan struct{})
			defer close(release)
			go func() {
				defer close(done)
				conn, err := listener.Accept()
				if err != nil {
					return
				}
				defer conn.Close()
				conn.SetDeadline(time.Now().Add(5 * time.Second))
				reader := bufio.NewReader(conn)
				relaySendHeader(conn, map[string]any{"version": 1, "challenge": base64.StdEncoding.EncodeToString(make([]byte, 32))})
				relayReadHeader(reader, false)
				relaySendHeader(conn, map[string]any{"ok": true, "id": src.ID, "kind": "files", "configuration": sourceRelayDigest(src)})
				relayReadHeader(reader, false)
				switch mode {
				case "oversize":
					relaySendHeader(conn, map[string]any{"ok": true, "size": sourceRelayMaxFile + 1})
				case "checksum":
					relaySendHeader(conn, map[string]any{"ok": true, "size": 3})
					conn.Write([]byte("bad"))
					relaySendHeader(conn, map[string]any{"ok": true, "sha256": strings.Repeat("0", 64)})
				case "truncated":
					relaySendHeader(conn, map[string]any{"ok": true, "size": 100})
					conn.Write([]byte("short"))
				case "cancel":
					<-release
				}
			}()
			ctx := context.Background()
			var cancel context.CancelFunc
			if mode == "cancel" {
				ctx, cancel = context.WithTimeout(ctx, 100*time.Millisecond)
				defer cancel()
			}
			payload, err := a.relayReadSource(ctx, src, "file", 100)
			if err == nil || len(payload) != 0 {
				t.Fatalf("untrusted %s accepted: bytes=%d err=%v", mode, len(payload), err)
			}
			if mode == "cancel" && !errors.Is(err, context.DeadlineExceeded) {
				t.Fatal(err)
			}
			if mode != "cancel" {
				select {
				case <-done:
				case <-time.After(time.Second):
					t.Fatal("fake peer did not stop")
				}
			}
		})
	}
}
