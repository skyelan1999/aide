package tts

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestOpenAISpeechProtocol(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/audio/speech" || r.Header.Get("Authorization") != "Bearer fixture-key" {
			t.Error("wrong request identity")
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["model"] != "tts-1-hd" || body["voice"] != "nova" || body["input"] != "你好" || body["response_format"] != "mp3" || body["speed"] != float64(1) {
			t.Errorf("wrong body %#v", body)
		}
		w.Header().Set("Content-Type", "audio/mpeg")
		_, _ = w.Write([]byte("ID3fixture"))
	}))
	defer srv.Close()
	p := newOpenAI(Config{Mode: "speech", Endpoint: srv.URL + "/v1/", Model: "tts-1-hd", APIKey: "fixture-key"})
	r, err := p.Synth(context.Background(), "你好", SynthOpts{Voice: "nova", Rate: 1.2})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	b, _ := io.ReadAll(r)
	if string(b) != "ID3fixture" || p.Format() != "mp3" {
		t.Fatal("not MP3")
	}
}

func TestOpenAISpeechFailures(t *testing.T) {
	for _, status := range []int{301, 401, 429, 500} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			followed := false
			target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { followed = true }))
			defer target.Close()
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Location", target.URL)
				w.WriteHeader(status)
				_, _ = w.Write([]byte("fixture-secret upstream private details"))
			}))
			defer srv.Close()
			p := newOpenAI(Config{Mode: "speech", APIKey: "fixture-secret", Endpoint: srv.URL})
			_, err := p.Synth(context.Background(), "hello", SynthOpts{})
			if err == nil || strings.Contains(err.Error(), "fixture-secret") || strings.Contains(err.Error(), "private details") || followed {
				t.Fatalf("unsafe failure: %v followed=%v", err, followed)
			}
		})
	}
	for _, base := range []string{"https://user:secret@api.openai.com/v1", "https://api.openai.com/v1?key=secret", "ftp://example.com/v1", "https://api.openai.com/v1#secret"} {
		if _, e := OpenAITTSBaseURL(base); e == nil {
			t.Errorf("accepted %s", base)
		}
	}
	for _, cfg := range []Config{{}, {APIKey: "key", Mode: "speech", Model: "gpt-realtime-2.1-mini"}, {APIKey: "key", Mode: "invalid"}} {
		if _, e := newOpenAI(cfg).Synth(context.Background(), "hello", SynthOpts{}); e == nil {
			t.Fatal("invalid configuration accepted")
		}
	}
}

func TestOpenAIRealtimeNarration(t *testing.T) {
	for _, kind := range []string{"success", "incomplete", "invalid-base64", "empty", "error", "cancel"} {
		t.Run(kind, func(t *testing.T) {
			closed := make(chan struct{})
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				defer close(closed)
				if r.URL.Path != "/v1/realtime" || r.URL.Query().Get("model") != OpenAITTSDefaultModel || r.Header.Get("Authorization") != "Bearer fixture-key" {
					t.Error("wrong realtime request")
				}
				c, e := (&websocket.Upgrader{}).Upgrade(w, r, nil)
				if e != nil {
					return
				}
				defer c.Close()
				var update map[string]any
				if e = c.ReadJSON(&update); e != nil {
					return
				}
				session := update["session"].(map[string]any)
				output := session["audio"].(map[string]any)["output"].(map[string]any)
				if session["type"] != "realtime" || output["voice"] != "cedar" || output["format"].(map[string]any)["rate"] != float64(24000) {
					t.Errorf("wrong session %#v", update)
				}
				_ = c.WriteJSON(map[string]any{"type": "session.updated"})
				var create map[string]any
				if e = c.ReadJSON(&create); e != nil {
					return
				}
				response := create["response"].(map[string]any)
				if create["type"] != "response.create" || response["conversation"] != "none" || !strings.Contains(response["instructions"].(string), "verbatim") {
					t.Errorf("wrong response %#v", create)
				}
				input := response["input"].([]any)[0].(map[string]any)["content"].([]any)[0].(map[string]any)
				if input["text"] != "朗读原文" {
					t.Error("text not preserved")
				}
				if kind == "cancel" {
					_, _, _ = c.ReadMessage()
					return
				}
				if kind == "error" {
					_ = c.WriteJSON(map[string]any{"type": "error", "error": map[string]any{"message": "fixture-key private"}})
					return
				}
				if kind != "empty" {
					delta := base64.StdEncoding.EncodeToString([]byte{1, 0, 2, 0})
					if kind == "invalid-base64" {
						delta = "!"
					}
					_ = c.WriteJSON(map[string]any{"type": "response.output_audio.delta", "delta": delta})
				}
				status := "completed"
				if kind == "incomplete" {
					status = "incomplete"
				}
				_ = c.WriteJSON(map[string]any{"type": "response.done", "response": map[string]any{"status": status}})
			}))
			defer srv.Close()
			ctx := context.Background()
			if kind == "cancel" {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, 100*time.Millisecond)
				defer cancel()
			}
			p := newOpenAI(Config{Endpoint: srv.URL + "/v1", APIKey: "fixture-key", Voice: "cedar"})
			r, err := p.Synth(ctx, "朗读原文", SynthOpts{})
			if kind != "success" {
				if err == nil {
					r.Close()
					t.Fatal("accepted failed generation")
				}
				if strings.Contains(err.Error(), "fixture-key") {
					t.Fatal("secret leaked")
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				defer r.Close()
				b, _ := io.ReadAll(r)
				if len(b) != 48 || string(b[:4]) != "RIFF" || binary.LittleEndian.Uint32(b[24:]) != 24000 || b[44] != 1 || p.Format() != "wav" {
					t.Fatal("wrong PCM WAV")
				}
			}
			select {
			case <-closed:
			case <-time.After(time.Second):
				t.Fatal("cancel/finish did not close upstream")
			}
		})
	}
}

func TestOpenAIExplicitChain(t *testing.T) {
	chain := buildChain(Config{Provider: "openai", AzureKey: "not-used"})
	if len(chain) != 1 || chain[0].Name() != "openai" {
		t.Fatal("OpenAI must not silently bill/fallback to another backend")
	}
	if p, e := NewProvider("openai", Config{}); e != nil || p.Name() != "openai" {
		t.Fatal("factory missing")
	}
}

func TestOpenAIRealtimeHandshakeRetry(t *testing.T) {
	for _, kind := range []string{"recover", "persistent-eof", "http-rejection", "cancel-backoff"} {
		t.Run(kind, func(t *testing.T) {
			var attempts atomic.Int32
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				n := attempts.Add(1)
				if kind == "http-rejection" {
					w.WriteHeader(http.StatusUnauthorized)
					return
				}
				if n == 1 || kind == "persistent-eof" {
					c, _, err := w.(http.Hijacker).Hijack()
					if err == nil {
						_ = c.Close()
					}
					if kind == "cancel-backoff" {
						cancel()
					}
					return
				}
				c, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
				if err == nil {
					_ = c.Close()
				}
			}))
			defer srv.Close()
			p := newOpenAI(Config{})
			conn, res, err := p.dialRealtime(ctx, "ws"+strings.TrimPrefix(srv.URL, "http"), http.Header{})
			if conn != nil {
				defer conn.Close()
			}
			if res != nil && res.Body != nil {
				defer res.Body.Close()
			}
			want := int32(2)
			if kind == "http-rejection" || kind == "cancel-backoff" {
				want = 1
			}
			if attempts.Load() != want {
				t.Fatalf("attempts=%d want=%d", attempts.Load(), want)
			}
			if (err == nil) != (kind == "recover") {
				t.Fatalf("unexpected handshake result: %v", err)
			}
		})
	}
}

func TestOpenAIRealtimeNoRetryAfterSession(t *testing.T) {
	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		c, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer c.Close()
		var message map[string]any
		if c.ReadJSON(&message) != nil {
			return
		}
		_ = c.WriteJSON(map[string]any{"type": "session.updated"})
		if c.ReadJSON(&message) != nil {
			return
		}
		// Simulate loss after response.create: never replay possible billable work.
	}))
	defer srv.Close()
	_, err := newOpenAI(Config{APIKey: "fixture-key", Endpoint: srv.URL}).Synth(context.Background(), "hello", SynthOpts{})
	if err == nil || attempts.Load() != 1 {
		t.Fatalf("err=%v attempts=%d", err, attempts.Load())
	}
}

func TestOpenAIRealtimeDialErrorRedaction(t *testing.T) {
	p := newOpenAI(Config{APIKey: "fixture-key"})
	calls := 0
	p.dialer.NetDialContext = func(context.Context, string, string) (net.Conn, error) {
		calls++
		return nil, errors.New("TLS proxy fixture-secret")
	}
	_, err := p.Synth(context.Background(), "hello", SynthOpts{})
	if err == nil || strings.Contains(err.Error(), "fixture-secret") || calls != 1 {
		t.Fatalf("unsafe retry/error: %v calls=%d", err, calls)
	}
}
