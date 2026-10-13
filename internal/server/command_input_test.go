package server

import (
	"io"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCommandInputForwardsExactBytesToActivePTY(t *testing.T) {
	reader, writer := io.Pipe()
	a := &App{commandInputs: map[string]*io.PipeWriter{"run-1": writer}}
	gotCh := make(chan string, 1)
	go func() {
		b, _ := io.ReadAll(reader)
		gotCh <- string(b)
	}()
	r := httptest.NewRequest("POST", "/api/command/run-1/input", strings.NewReader(`{"input":"password\r"}`))
	r.SetPathValue("id", "run-1")
	w := httptest.NewRecorder()
	a.commandInput(w, r)
	if w.Code != 200 {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	_ = writer.Close()
	if got := <-gotCh; got != "password\r" {
		t.Fatalf("PTY input = %q, want exact input bytes", got)
	}
}

func TestCommandInputForwardsTabWithoutSubmittingLine(t *testing.T) {
	reader, writer := io.Pipe()
	a := &App{commandInputs: map[string]*io.PipeWriter{"run-1": writer}}
	gotCh := make(chan string, 1)
	go func() {
		b, _ := io.ReadAll(reader)
		gotCh <- string(b)
	}()
	r := httptest.NewRequest("POST", "/api/command/run-1/input", strings.NewReader(`{"input":"\t"}`))
	r.SetPathValue("id", "run-1")
	w := httptest.NewRecorder()
	a.commandInput(w, r)
	if w.Code != 200 {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	_ = writer.Close()
	if got := <-gotCh; got != "\t" {
		t.Fatalf("PTY input = %q, want raw Tab byte", got)
	}
}

func TestCommandInputRejectsClosedSession(t *testing.T) {
	a := &App{commandInputs: map[string]*io.PipeWriter{}}
	r := httptest.NewRequest("POST", "/api/command/missing/input", strings.NewReader(`{"input":"x"}`))
	r.SetPathValue("id", "missing")
	w := httptest.NewRecorder()
	a.commandInput(w, r)
	if w.Code != 404 {
		t.Fatalf("status = %d, want 404", w.Code)
	}
}

func TestSSHInteractiveSessionStartsLoginShellAndFeedsFirstCommand(t *testing.T) {
	if got, want := sshInteractiveShellCommand(), `"${SHELL:-/bin/bash}" -l`; got != want {
		t.Fatalf("interactive SSH command = %q, want persistent login shell %q", got, want)
	}
	got, err := io.ReadAll(sshInteractivePTYInput("sudo su", strings.NewReader("secret\r")))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "sudo su\rsecret\r" {
		t.Fatalf("interactive PTY input = %q, want first command followed by live prompt input", got)
	}
}
