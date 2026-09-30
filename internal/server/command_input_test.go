package server

import (
	"io"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCommandInputIsForwardedToActivePTY(t *testing.T) {
	reader, writer := io.Pipe()
	a := &App{commandInputs: map[string]*io.PipeWriter{"run-1": writer}}
	gotCh := make(chan string, 1)
	go func() {
		b, _ := io.ReadAll(reader)
		gotCh <- string(b)
	}()
	r := httptest.NewRequest("POST", "/api/command/run-1/input", strings.NewReader(`{"input":"password"}`))
	r.SetPathValue("id", "run-1")
	w := httptest.NewRecorder()
	a.commandInput(w, r)
	if w.Code != 200 {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	_ = writer.Close()
	if got := <-gotCh; got != "password\n" {
		t.Fatalf("PTY input = %q, want one newline-terminated input", got)
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
