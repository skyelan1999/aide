package tts

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestQwenCatalogFiltersProtocolsAndCredentials(t *testing.T) {
	calls := 0
	client := &http.Client{Transport: qwenTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		if strings.Contains(r.URL.Host, "dashscope") {
			if r.Header.Get("Authorization") != "Bearer fixture" || !strings.Contains(r.URL.RawQuery, "capabilities=TTS") {
				t.Fatal("wrong model auth/filter")
			}
			return qwenResponse(200, `{"output":{"total":4,"models":[{"model":"qwen3-tts-flash","name":"Flash"},{"model":"qwen3-tts-flash-realtime"},{"model":"qwen-audio-3.1-tts-flash"},{"model":"qwen3.8-max"}]}}`), nil
		}
		if r.Header.Get("Authorization") != "" {
			t.Fatal("key sent to public documentation")
		}
		return qwenResponse(200, "unavailable article"), nil
	})}
	c := discoverQwen(context.Background(), Config{APIKey: "fixture", Region: "beijing"}, client)
	if calls != 2 || c.ModelSource != "api" || len(c.Models) != 1 || c.Models[0].ID != "qwen3-tts-flash" {
		t.Fatalf("wrong models %#v", c)
	}
	if c.VoiceSource != "builtin" || len(c.Voices) != 48 {
		t.Fatal("offline voices missing")
	}
}
func TestQwenVoiceArticleSSRAndJSON(t *testing.T) {
	article := `<h2>非实时语音合成</h2><table><tr><td><strong>名称</strong>：芊悦<p><strong>voice<span></span>参数</strong>：Cherry</p><strong>描述</strong>：示例</td></tr><tr><td>名称：苏瑶 voice参数：Serena 描述：示例</td></tr></table>`
	parsed := parseQwenVoices(article)
	if len(parsed) != 2 || parsed[0].ID != "Cherry" || parsed[0].Name != "芊悦" {
		t.Fatalf("SSR parse: %#v", parsed)
	}
	b, _ := json.Marshal(map[string]any{"docDetailData": map[string]any{"storeData": map[string]any{"data": map[string]any{"content": article}}}})
	parsed = parseQwenVoices("<script>window.__ICE_PAGE_PROPS__=" + string(b) + ";</script>")
	if len(parsed) != 2 || parsed[1].ID != "Serena" {
		t.Fatalf("JSON parse: %#v", parsed)
	}
}
func TestQwenCatalogFailureKeepsKnownChoices(t *testing.T) {
	client := &http.Client{Transport: qwenTransport(func(*http.Request) (*http.Response, error) {
		return qwenResponse(401, `{"code":"InvalidApiKey","message":"private-secret"}`), nil
	})}
	c := discoverQwen(context.Background(), Config{APIKey: "fixture"}, client)
	if c.ModelSource != "builtin" || len(c.Models) == 0 || len(c.Voices) != 48 || strings.Contains(c.Warning, "private-secret") {
		t.Fatalf("unsafe failure %#v", c)
	}
}
