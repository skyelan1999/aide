package server

import (
	"archive/zip"
	"bufio"
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

func sftpRelaySSHServer(t *testing.T, root string) (int, string, func()) {
	t.Helper()
	var current atomic.Pointer[ssh.ServerConfig]
	_, userKey, _ := ed25519.GenerateKey(rand.Reader)
	userSigner, _ := ssh.NewSignerFromKey(userKey)
	block, _ := ssh.MarshalPrivateKey(userKey, "fixture")
	rotate := func() {
		_, host, _ := ed25519.GenerateKey(rand.Reader)
		signer, _ := ssh.NewSignerFromKey(host)
		config := &ssh.ServerConfig{PasswordCallback: func(c ssh.ConnMetadata, p []byte) (*ssh.Permissions, error) {
			if c.User() == "fixture" && string(p) == "fixture-password" {
				return nil, nil
			}
			return nil, os.ErrPermission
		}, PublicKeyCallback: func(c ssh.ConnMetadata, k ssh.PublicKey) (*ssh.Permissions, error) {
			if string(k.Marshal()) == string(userSigner.PublicKey().Marshal()) {
				return nil, nil
			}
			return nil, os.ErrPermission
		}}
		config.AddHostKey(signer)
		current.Store(config)
	}
	rotate()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	go func() {
		for {
			raw, e := listener.Accept()
			if e != nil {
				return
			}
			go func() {
				conn, chans, reqs, e := ssh.NewServerConn(raw, current.Load())
				if e != nil {
					raw.Close()
					return
				}
				defer conn.Close()
				go ssh.DiscardRequests(reqs)
				for ch := range chans {
					if ch.ChannelType() != "session" {
						ch.Reject(ssh.UnknownChannelType, "session only")
						continue
					}
					channel, requests, e := ch.Accept()
					if e != nil {
						continue
					}
					go func() {
						defer channel.Close()
						for r := range requests {
							var subsystem struct{ Name string }
							ssh.Unmarshal(r.Payload, &subsystem)
							if r.Type == "subsystem" && subsystem.Name == "sftp" {
								r.Reply(true, nil)
								server, e := sftp.NewServer(channel, sftp.WithServerWorkingDirectory(root))
								if e == nil {
									server.Serve()
									server.Close()
								}
								return
							}
							r.Reply(false, nil)
						}
					}()
				}
			}()
		}
	}()
	return listener.Addr().(*net.TCPAddr).Port, string(pem.EncodeToMemory(block)), rotate
}

func startSFTPRelayTestAgent(t *testing.T, a *App, src Source) {
	t.Helper()
	response := request(a, "GET", "/api/sources/"+src.ID+"/relay-script", nil)
	requireStatus(t, response, 200)
	if response.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("script cache policy")
	}
	script := filepath.Join(t.TempDir(), "relay.py")
	os.WriteFile(script, response.Body.Bytes(), 0600)
	cmd := exec.Command("python3", "-u", script)
	output, _ := cmd.StdoutPipe()
	cmd.Stderr = stdoutWriter{t: t}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cmd.Process.Kill(); cmd.Wait() })
	ready := make(chan bool, 1)
	go func() {
		scanner := bufio.NewScanner(output)
		ready <- scanner.Scan() && strings.Contains(scanner.Text(), "ready on TCP port")
	}()
	select {
	case ok := <-ready:
		if !ok {
			t.Fatal("agent did not start")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("agent startup timeout")
	}
}

func TestSFTPRelayActualSSH(t *testing.T) {
	if _, err := exec.LookPath("ssh"); err != nil {
		t.Skip("OpenSSH required")
	}
	a := testApp(t)
	a.sshBin, _ = exec.LookPath("ssh")
	a.sftpBin, _ = exec.LookPath("sftp")
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "hello.md"), []byte("through SFTP relay"), 0600)
	port, key, rotate := sftpRelaySSHServer(t, root)
	listener, _ := net.Listen("tcp", "127.0.0.1:0")
	relayPort := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	src := Source{ID: "sftp-relay-integration", Type: "sftp", Name: "SFTP relay", Enabled: true, Config: SourceConfig{Host: "127.0.0.1", Port: port, Username: "fixture", Auth: "password", Path: root, RelayHost: "127.0.0.1", RelayPort: relayPort}}
	save := func(secret map[string]string) {
		requireStatus(t, request(a, "PUT", "/api/sources", map[string]any{"sources": []Source{src}, "secrets": map[string]any{src.ID: secret}}), 200)
	}
	save(map[string]string{"password": "fixture-password"})
	t.Cleanup(func() { a.killSourceSession(src.ID, a.sftpTargetOf(src)) })
	secret := a.sourceCredentialEntryLocked(src.ID)
	if _, e := sourceRelayKey(secret.RelayKey); e != nil {
		t.Fatal(e)
	}
	registry := request(a, "GET", "/api/sources", nil)
	for _, value := range []string{secret.RelayKey, "fixture-password"} {
		if strings.Contains(registry.Body.String(), value) {
			t.Fatal("secret in registry")
		}
	}
	script := request(a, "GET", "/api/sources/"+src.ID+"/relay-script", nil).Body.String()
	if strings.Contains(script, "fixture-password") || strings.Contains(script, key) {
		t.Fatal("SSH credential in script")
	}
	startSFTPRelayTestAgent(t, a, src)
	probe := request(a, "POST", "/api/sources/"+src.ID+"/relay-probe", nil)
	if !strings.Contains(probe.Body.String(), `"online":true`) {
		t.Fatal(probe.Body.String())
	}
	listing, e := a.listSourceDir(src, ".")
	if e != nil || !strings.Contains(mustJSON(listing), "hello.md") {
		t.Fatalf("list %v %v", listing, e)
	}
	data, e := a.readSourceRaw(src, "hello.md")
	if e != nil || string(data) != "through SFTP relay" {
		t.Fatalf("read: %q %v", data, e)
	}
	if e := a.writeSourceText(src, "hello.md", []byte("denied")); e == nil {
		t.Fatal("read-only source wrote")
	}
	src.RW = true
	save(nil)
	if e := a.writeSourceText(src, "hello.md", []byte("saved via relay")); e != nil {
		t.Fatal(e)
	}
	actual, _ := os.ReadFile(filepath.Join(root, "hello.md"))
	if string(actual) != "saved via relay" {
		t.Fatal("write not persisted")
	}
	bulk := bytes.Repeat([]byte("relay data\n"), 420000)
	if e := a.writeSourceText(src, "bulk.md", bulk); e != nil {
		t.Fatal("bulk write:", e)
	}
	back, e := a.readSourceRaw(src, "bulk.md")
	if e != nil || !bytes.Equal(back, bulk) {
		t.Fatal("bulk readback mismatch:", e)
	}
	testSFTPRelayTransfer(t, a, src, root)
	src.Config.Auth = "key"
	save(map[string]string{"key": key})
	if _, e := a.readSourceRaw(src, "hello.md"); e != nil {
		t.Fatal("private key auth:", e)
	}
	if a.sourceCredentialEntryLocked(src.ID).RelayKey != secret.RelayKey {
		t.Fatal("SSH update rotated relay key")
	}
	// A different relay identity must fail before SSH authentication.
	other, _ := newSourceRelayKey()
	save(map[string]string{"relayKey": other})
	if _, e := a.readSourceRaw(src, "hello.md"); e == nil {
		t.Fatal("wrong relay identity accepted")
	}
	save(map[string]string{"relayKey": secret.RelayKey})
	src.Config.Path += "/changed"
	save(nil)
	stale := request(a, "POST", "/api/sources/"+src.ID+"/relay-probe", nil)
	if !strings.Contains(stale.Body.String(), `"online":false`) {
		t.Fatal("old deployment accepted changed configuration")
	}
	src.Config.Path = root
	save(nil)
	// Target SSH host keys are still checked against the original SFTP host.
	a.killSourceSession(src.ID, a.sftpTargetOf(src))
	rotate()
	if _, e := a.readSourceRaw(src, "hello.md"); e == nil {
		t.Fatal("changed SSH host key accepted")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	changed := src
	changed.Config.Path += "/changed"
	if e := a.probeSFTPRelay(ctx, changed); e == nil {
		t.Fatal("stale source accepted")
	}
}

func TestSFTPRelayBrowserFixture(t *testing.T) {
	if os.Getenv("AIDE_SFTP_RELAY_BROWSER_FIXTURE") != "1" {
		t.Skip("opt-in browser fixture")
	}
	a := testApp(t)
	a.token = "sftp-relay-browser-synthetic"
	a.sshBin, _ = exec.LookPath("ssh")
	a.sftpBin, _ = exec.LookPath("sftp")
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "acceptance.md"), []byte("# SFTP relay acceptance\n\nReal SFTP through downloaded TLS relay.\n"), 0600)
	port, _, _ := sftpRelaySSHServer(t, root)
	listener, _ := net.Listen("tcp", "127.0.0.1:0")
	rp := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	src := Source{ID: "browser-sftp-relay", Name: "SFTP relay acceptance", Type: "sftp", Enabled: true, Config: SourceConfig{Host: "127.0.0.1", Port: port, Username: "fixture", Auth: "password", Path: root, RelayHost: "127.0.0.1", RelayPort: rp}}
	requireStatus(t, request(a, "PUT", "/api/sources", map[string]any{"sources": []Source{src}, "secrets": map[string]any{src.ID: map[string]string{"password": "fixture-password"}}}), 200)
	startSFTPRelayTestAgent(t, a, src)
	t.Cleanup(func() { a.killSourceSession(src.ID, a.sftpTargetOf(src)) })
	server := &http.Server{Addr: ":18978", Handler: a.Handler(), ReadHeaderTimeout: 5 * time.Second}
	t.Cleanup(func() { server.Close() })
	done := make(chan error, 1)
	go func() { done <- server.ListenAndServe() }()
	t.Log("BROWSER_READY http://127.0.0.1:18978/#token=sftp-relay-browser-synthetic")
	select {
	case err := <-done:
		t.Fatal(err)
	case <-time.After(20 * time.Minute):
	}
}

func TestSFTPRelayVaultPatch(t *testing.T) {
	a := testApp(t)
	if !a.vaultIsUnlocked() {
		t.Fatal("vault not unlocked")
	}
	src := Source{ID: "sftp-relay-vault", Type: "sftp", Enabled: true, Config: SourceConfig{Host: "target.invalid", Port: 22, Auth: "key", RelayHost: "relay.invalid", RelayPort: 8768}}
	save := func(patch map[string]any) {
		requireStatus(t, request(a, "PUT", "/api/sources", map[string]any{"sources": []Source{src}, "secrets": map[string]any{src.ID: patch}}), 200)
	}
	save(map[string]any{"password": "fixture-password", "key": "fixture-private"})
	old := a.sourceCredentialEntryLocked(src.ID)
	if old.RelayKey == "" {
		t.Fatal("missing generated relay key")
	}
	save(map[string]any{"password": "new-password"})
	fresh := a.sourceCredentialEntryLocked(src.ID)
	if fresh.RelayKey != old.RelayKey || fresh.Key != old.Key || fresh.Password != "new-password" {
		t.Fatal("partial secret patch lost fields")
	}
	if _, ok := a.sourceSecrets.Secrets[src.ID]; ok {
		t.Fatal("sealed relay secret retained plaintext")
	}
	generation := a.sshSessionGeneration(sourceSocket(src.ID))
	save(nil)
	if generation != a.sshSessionGeneration(sourceSocket(src.ID)) {
		t.Fatal("unchanged save retired live session")
	}
	a.vault.Lock()
	requireStatus(t, request(a, "PUT", "/api/sources", map[string]any{"sources": []Source{src}, "secrets": map[string]any{src.ID: map[string]string{"relayKey": old.RelayKey}}}), 423)
}

func TestSFTPRelayRejectMalformedExistingCredentials(t *testing.T) {
	a := testApp(t)
	src := Source{ID: "sftp-malformed", Type: "sftp", Enabled: true, Config: SourceConfig{Host: "target.invalid", RelayHost: "relay.invalid"}}
	if err := a.vault.Put(sourceVaultID(src.ID), VaultTypeSourceSecret, "fixture", []byte("bad-json"), ""); err != nil {
		t.Fatal(err)
	}
	requireStatus(t, request(a, "PUT", "/api/sources", map[string]any{"sources": []Source{src}}), 500)
	original, err := a.vault.Get(sourceVaultID(src.ID))
	if err != nil || string(original) != "bad-json" {
		t.Fatal("malformed credential overwritten")
	}
}

func testSFTPRelayTransfer(t *testing.T, a *App, src Source, root string) {
	t.Helper()
	m := a.transfers()
	size := 2*transferChunkSize + 37
	j, err := m.create(context.Background(), transferCreate{Operation: "upload", Destination: src.ID, Name: "upload.bin", Size: size})
	if err != nil {
		t.Fatal(err)
	}
	requireStatus(t, transferTestPut(a, j.ID, 1, transferTestChunk(1, transferChunkSize)), 200)
	testTransferRestart(a)
	m = a.transfers()
	j = m.jobs[j.ID]
	if j.Status != "paused" || len(j.UploadChunks) != 1 {
		t.Fatal("SFTP upload resume journal lost")
	}
	requireStatus(t, request(a, "POST", "/api/file/transfers/"+j.ID+"/resume", map[string]any{}), 200)
	requireStatus(t, transferTestPut(a, j.ID, 0, transferTestChunk(0, transferChunkSize)), 200)
	requireStatus(t, transferTestPut(a, j.ID, 2, transferTestChunk(2, 37)), 200)
	if _, err := os.Stat(filepath.Join(root, "upload.bin")); !os.IsNotExist(err) {
		t.Fatal("partial upload published")
	}
	requireStatus(t, request(a, "POST", "/api/file/transfers/"+j.ID+"/commit", map[string]any{}), 200)
	testTransferWait(t, m, j)
	expected := append(transferTestChunk(0, transferChunkSize), transferTestChunk(1, transferChunkSize)...)
	expected = append(expected, transferTestChunk(2, 37)...)
	if testFileDigest(t, filepath.Join(root, "upload.bin")) != hash(expected) {
		t.Fatal("SFTP upload digest")
	}
	raw := request(a, "GET", "/api/file/download?source="+src.ID+"&path=upload.bin", nil)
	requireStatus(t, raw, 200)
	if !bytes.Equal(raw.Body.Bytes(), expected) {
		t.Fatal("SFTP direct download mismatch")
	}
	download, err := m.create(context.Background(), transferCreate{Operation: "download", Source: src.ID, Paths: []string{"upload.bin"}})
	if err != nil {
		t.Fatal(err)
	}
	if err = m.start(download); err != nil {
		t.Fatal(err)
	}
	testTransferWait(t, m, download)
	archive, err := zip.OpenReader(filepath.Join(m.jobDir(download), "download.zip"))
	if err != nil {
		t.Fatal(err)
	}
	defer archive.Close()
	if len(archive.File) != 1 {
		t.Fatal("unexpected SFTP archive entries")
	}
	f, err := archive.File[0].Open()
	if err != nil {
		t.Fatal(err)
	}
	b, err := io.ReadAll(f)
	f.Close()
	if err != nil || !bytes.Equal(b, expected) {
		t.Fatal("SFTP archive download mismatch", err)
	}
	copyJob, err := m.create(context.Background(), transferCreate{Operation: "copy", Source: src.ID, Paths: []string{"upload.bin"}, Destination: "workspace"})
	if err != nil {
		t.Fatal(err)
	}
	if err = m.start(copyJob); err != nil {
		t.Fatal(err)
	}
	testTransferWait(t, m, copyJob)
	if testFileDigest(t, filepath.Join(a.workPath, "upload.bin")) != hash(expected) {
		t.Fatal("SFTP-to-workspace copy digest")
	}
	src.RW = false
	requireStatus(t, request(a, "PUT", "/api/sources", map[string]any{"sources": []Source{src}}), 200)
	if _, err = m.create(context.Background(), transferCreate{Operation: "upload", Destination: src.ID, Name: "denied.bin", Size: 1}); err == nil {
		t.Fatal("read-only SFTP upload accepted")
	}
	src.RW = true
	requireStatus(t, request(a, "PUT", "/api/sources", map[string]any{"sources": []Source{src}}), 200)
}
