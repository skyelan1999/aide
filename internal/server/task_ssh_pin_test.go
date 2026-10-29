package server

import (
	"context"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestIssue82SocketPinSurvivesReplacementAndCleansAlias(t *testing.T) {
	socket := filepath.Join(t.TempDir(), "master")
	listen := func() *net.UnixListener {
		l, err := net.ListenUnix("unix", &net.UnixAddr{Name: socket, Net: "unix"})
		if err != nil {
			t.Fatal(err)
		}
		l.SetUnlinkOnClose(false)
		t.Cleanup(func() { _ = l.Close() })
		return l
	}
	old := listen()
	alias, release, err := pinTaskSSHSocket(socket)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if err := os.Remove(socket); err != nil {
		t.Fatal(err)
	}
	replacement := listen()
	serve := func(l *net.UnixListener, text string) chan error {
		done := make(chan error, 1)
		go func() {
			c, err := l.Accept()
			if err == nil {
				_, err = io.WriteString(c, text)
				_ = c.Close()
			}
			done <- err
		}()
		return done
	}
	oldDone, newDone := serve(old, "old master"), serve(replacement, "new master")
	for p, want := range map[string]string{alias: "old master", socket: "new master"} {
		c, err := net.DialTimeout("unix", p, time.Second)
		if err != nil {
			t.Fatal(err)
		}
		_ = c.SetReadDeadline(time.Now().Add(time.Second))
		got, err := io.ReadAll(c)
		_ = c.Close()
		if err != nil || string(got) != want {
			t.Fatalf("socket identity switched: %q %v", got, err)
		}
	}
	if err := <-oldDone; err != nil {
		t.Fatal(err)
	}
	if err := <-newDone; err != nil {
		t.Fatal(err)
	}
	release()
	if _, err := os.Lstat(alias); !os.IsNotExist(err) {
		t.Fatal("owned task alias leaked", err)
	}
	if info, err := os.Lstat(socket); err != nil || info.Mode()&os.ModeSocket == 0 {
		t.Fatal("alias cleanup affected replacement socket", err)
	}
}

func TestIssue82CancellationUsesPinnedOriginalSSHSession(t *testing.T) {
	a := testApp(t)
	log := filepath.Join(t.TempDir(), "calls")
	bin := filepath.Join(t.TempDir(), "ssh")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" >> " + shellQuote(log) + "\nfor arg do last=$arg; done\ncase \"$last\" in *AIDE-CLEANUP*) printf 'AIDE-CLEANUP:already-gone:owned\\n';; *) exec sleep 30;; esac\n"
	if err := os.WriteFile(bin, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	a.sshBin = bin
	t.Cleanup(func() { a.sshBin = "" })
	a.wsConfig.Workspace.Host = "new-host.invalid"
	a.wsConfig.Workspace.Username = "new-user"
	session := &taskSSHSession{bin: bin, socket: "/tmp/owned-task-alias", target: "old-user@old-host.invalid", port: 2201, pinned: true}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() {
		_, err := a.execRemoteSession(ctx, "echo owned", io.Discard, io.Discard, session)
		result <- err
	}()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if info, err := os.Stat(log); err == nil && info.Size() > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("owned command did not start")
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	var err error
	select {
	case err = <-result:
	case <-time.After(8 * time.Second):
		t.Fatal("owned command cleanup did not finish")
	}
	if err == nil || !strings.Contains(err.Error(), "AIDE-CLEANUP:already-gone") {
		t.Fatal("cancel cleanup not confirmed on captured session", err)
	}
	data, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if strings.Count(text, "old-user@old-host.invalid") != 2 || strings.Count(text, "ProxyCommand=false") != 2 || strings.Contains(text, "new-host.invalid") || strings.Contains(text, sshControlSocket) {
		t.Fatalf("dispatch or cleanup escaped original session: %s", text)
	}
}
