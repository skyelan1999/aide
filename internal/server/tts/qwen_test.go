package tts

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

type qwenTransport func(*http.Request) (*http.Response, error)

func (f qwenTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func qwenResponse(code int, body string) *http.Response {
	return &http.Response{StatusCode: code, Body: io.NopCloser(strings.NewReader(body)), ContentLength: int64(len(body)), Header: make(http.Header)}
}

func TestQwenNativeAPI(t *testing.T) {
	for _, region := range []string{"beijing", "singapore"} {
		t.Run(region, func(t *testing.T) {
			calls := 0
			q := newQwen(Config{APIKey: "test-only-key", Region: region, Model: "qwen3-tts-flash"})
			q.client.Transport = qwenTransport(func(r *http.Request) (*http.Response, error) {
				calls++
				if calls == 1 {
					host := "dashscope.aliyuncs.com"
					if region == "singapore" {
						host = "dashscope-intl.aliyuncs.com"
					}
					if r.URL.Host != host || r.URL.Path != "/api/v1/services/aigc/multimodal-generation/generation" || r.Method != "POST" {
						t.Fatalf("wrong endpoint %v", r.URL)
					}
					if r.Header.Get("Authorization") != "Bearer test-only-key" {
						t.Fatal("missing auth")
					}
					var body struct {
						Model string
						Input struct {
							Text, Voice string
							Language    string `json:"language_type"`
						}
					}
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Fatal(err)
					}
					if body.Model != "qwen3-tts-flash" || body.Input.Text != "你好" || body.Input.Voice != "Cherry" || body.Input.Language != "Auto" {
						t.Fatalf("wrong body %#v", body)
					}
					return qwenResponse(200, `{"output":{"audio":{"url":"http://dashscope-result-bj.oss-cn-beijing.aliyuncs.com/test.wav?Signature=fake"}}}`), nil
				}
				if r.URL.Scheme != "https" || r.Header.Get("Authorization") != "" {
					t.Fatal("audio must use TLS without API key")
				}
				return qwenResponse(200, "RIFF-test-WAVE"), nil
			})
			rc, err := q.Synth(context.Background(), "你好", SynthOpts{})
			if err != nil {
				t.Fatal(err)
			}
			b, _ := io.ReadAll(rc)
			rc.Close()
			if string(b) != "RIFF-test-WAVE" || calls != 2 || q.Format() != "wav" {
				t.Fatal("audio not returned")
			}
		})
	}
}
func TestQwenRejectsUnsafeAudio(t *testing.T) {
	for _, s := range []string{"", "http://127.0.0.1/private", "https://dashscope-result-bj.oss-cn-beijing.aliyuncs.com.evil.test/x", "file:///etc/passwd", "https://user:secret@dashscope-result-bj.oss-cn-beijing.aliyuncs.com/x", "https://dashscope-result-bj.oss-cn-beijing.aliyuncs.com:8080/x"} {
		if _, err := qwenAudioURL(s); err == nil {
			t.Fatalf("accepted %s", s)
		}
	}
}
func TestQwenErrorsAndCancel(t *testing.T) {
	for _, cfg := range []Config{{}, {APIKey: "test", Region: "unknown"}, {APIKey: "test", Model: "qwen3-tts-flash-realtime"}} {
		q := newQwen(cfg)
		q.client.Transport = qwenTransport(func(*http.Request) (*http.Response, error) { t.Fatal("invalid settings made request"); return nil, nil })
		if _, err := q.Synth(context.Background(), "你好", SynthOpts{}); err == nil {
			t.Fatal("expected configuration error")
		}
	}
	q := newQwen(Config{APIKey: "test"})
	q.client.Transport = qwenTransport(func(*http.Request) (*http.Response, error) {
		return qwenResponse(401, "do not expose raw provider secrets"), nil
	})
	if _, err := q.Synth(context.Background(), "你好", SynthOpts{}); err == nil || strings.Contains(err.Error(), "secrets") {
		t.Fatal("unsafe or missing error")
	}
	if _, err := q.Synth(context.Background(), strings.Repeat("你", 601), SynthOpts{}); err == nil {
		t.Fatal("accepted oversized text")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	q.client.Transport = http.DefaultTransport
	if _, err := q.Synth(ctx, "你好", SynthOpts{}); err == nil {
		t.Fatal("ignored cancellation")
	}
	chain := buildChain(Config{Provider: "qwen"})
	if len(chain) != 1 || chain[0].Name() != "qwen" {
		t.Fatal("explicit provider used unrelated engines")
	}
}
