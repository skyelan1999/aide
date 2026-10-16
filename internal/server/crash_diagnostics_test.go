package server

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"
)

func TestCrashPanicResponseAndIsolation(t *testing.T) {
	for _, value := range []any{"password=private-secret", map[string]string{"api_key": "private-secret"}} {
		h := recoverHTTP(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/bad" {
				panic(value)
			}
			fmt.Fprint(w, "healthy")
		}))
		bad := httptest.NewRecorder()
		h.ServeHTTP(bad, httptest.NewRequest("GET", "/bad?access_token=private-secret", nil))
		if bad.Code != 500 {
			t.Fatalf("status=%d", bad.Code)
		}
		var data map[string]string
		if err := json.Unmarshal(bad.Body.Bytes(), &data); err != nil {
			t.Fatal(err)
		}
		if data["code"] != serverPanicCode || !regexp.MustCompile(`^[0-9a-f]{32}$`).MatchString(data["incidentId"]) {
			t.Fatalf("missing diagnostic identity: %v", data)
		}
		if strings.Contains(bad.Body.String(), "private-secret") || strings.Contains(bad.Body.String(), "goroutine") {
			t.Fatal("panic content/stack leaked to client")
		}
		if bad.Header().Get("X-AIDE-Incident-ID") != data["incidentId"] || bad.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("missing response diagnostic headers")
		}
		good := httptest.NewRecorder()
		h.ServeHTTP(good, httptest.NewRequest("GET", "/good", nil))
		if good.Code != 200 || good.Body.String() != "healthy" {
			t.Fatal("unrelated request affected")
		}
	}
}

type crashBasicWriter struct {
	header http.Header
	body   bytes.Buffer
	code   int
}

func (w *crashBasicWriter) Header() http.Header  { return w.header }
func (w *crashBasicWriter) WriteHeader(code int) { w.code = code }
func (w *crashBasicWriter) Write(data []byte) (int, error) {
	if w.code == 0 {
		w.code = 200
	}
	return w.body.Write(data)
}

func TestCrashWriterCapabilitiesAndCommittedStream(t *testing.T) {
	basic := &crashBasicWriter{header: make(http.Header)}
	recoverHTTP(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := w.(http.Flusher); ok {
			t.Fatal("unsupported Flusher advertised")
		}
		if _, ok := w.(http.Hijacker); ok {
			t.Fatal("unsupported Hijacker advertised")
		}
		fmt.Fprint(w, "ok")
	})).ServeHTTP(basic, httptest.NewRequest("GET", "/", nil))
	stream := httptest.NewRecorder()
	func() {
		defer func() {
			if recover() != http.ErrAbortHandler {
				t.Error("committed stream must abort, not append JSON")
			}
		}()
		recoverHTTP(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprint(w, "data: existing\n\n")
			w.(http.Flusher).Flush()
			panic("private stream content")
		})).ServeHTTP(stream, httptest.NewRequest("GET", "/", nil))
		t.Error("expected stream abort")
	}()
	if stream.Body.String() != "data: existing\n\n" || !stream.Flushed {
		t.Fatal("stream corrupted")
	}
}

type crashHijackFixture struct {
	*crashBasicWriter
	conn    net.Conn
	flushed bool
}

func (w *crashHijackFixture) Flush() { w.flushed = true }
func (w *crashHijackFixture) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return w.conn, bufio.NewReadWriter(bufio.NewReader(w.conn), bufio.NewWriter(w.conn)), nil
}
func TestCrashHijackedConnectionCleanup(t *testing.T) {
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	w := &crashHijackFixture{crashBasicWriter: &crashBasicWriter{header: make(http.Header)}, conn: a}
	func() {
		defer func() {
			if recover() != http.ErrAbortHandler {
				t.Error("hijacked request must abort")
			}
		}()
		recoverHTTP(http.HandlerFunc(func(output http.ResponseWriter, r *http.Request) {
			if _, ok := output.(http.Flusher); !ok {
				t.Fatal("Flusher lost")
			}
			if _, _, err := output.(http.Hijacker).Hijack(); err != nil {
				t.Fatal(err)
			}
			panic("owned connection failure")
		})).ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	}()
	if _, err := b.Write([]byte("closed")); err == nil {
		t.Fatal("hijacked connection leaked")
	}
}

func TestCrashConcurrentRequests(t *testing.T) {
	h := recoverHTTP(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/bad" {
			panic("owned fixture")
		}
		fmt.Fprint(w, "ok")
	}))
	var group sync.WaitGroup
	for i := 0; i < 20; i++ {
		group.Add(1)
		go func(i int) {
			defer group.Done()
			path := "/good"
			want := 200
			if i%2 == 0 {
				path = "/bad"
				want = 500
			}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
			if w.Code != want {
				t.Errorf("status=%d want=%d", w.Code, want)
			}
		}(i)
	}
	group.Wait()
}

func TestCrashHealthInstanceIdentity(t *testing.T) {
	a := testApp(t)
	read := func() string {
		w := httptest.NewRecorder()
		a.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/healthz", nil))
		var data map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &data); err != nil {
			t.Fatal(err)
		}
		id, _ := data["instanceId"].(string)
		if id == "" || data["status"] != "ok" || data["service"] != "aide" {
			t.Fatalf("invalid health identity: %v", data)
		}
		return id
	}
	if read() != read() {
		t.Fatal("same process identity unstable")
	}
	unauth := httptest.NewRecorder()
	a.Handler().ServeHTTP(unauth, httptest.NewRequest("GET", "/api/config", nil))
	if unauth.Code != 401 {
		t.Fatal("recovery changed authentication")
	}
}

func TestCrashHTTPWireRecovery(t *testing.T) {
	server := httptest.NewServer(recoverHTTP(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/panic":
			w.Header().Set("Content-Length", "100000")
			w.Header().Set("Content-Encoding", "gzip")
			w.Header().Set("Content-Disposition", "attachment; filename=owned.bin")
			panic("owned wire fixture")
		case "/stream":
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprint(w, "data: preserved\n\n")
			w.(http.Flusher).Flush()
			panic("owned stream fixture")
		default:
			fmt.Fprint(w, "healthy")
		}
	})))
	defer server.Close()
	for _, path := range []string{"/panic", "/stream", "/healthy"} {
		response, err := server.Client().Get(server.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		body, readErr := io.ReadAll(response.Body)
		response.Body.Close()
		switch path {
		case "/panic":
			if response.Header.Get("Content-Encoding") != "" || response.Header.Get("Content-Disposition") != "" {
				t.Fatal("stale download headers survived recovery")
			}
			if response.StatusCode != 500 || !strings.Contains(string(body), serverPanicCode) || readErr != nil {
				t.Fatalf("invalid wire panic response: %d %s %v", response.StatusCode, body, readErr)
			}
		case "/stream":
			if string(body) != "data: preserved\n\n" || readErr == nil {
				t.Fatalf("committed stream was not aborted intact: %q %v", body, readErr)
			}
		default:
			if string(body) != "healthy" || readErr != nil {
				t.Fatal("following healthy request failed")
			}
		}
	}
}
