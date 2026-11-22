package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestNativeCallWorkKeepsOnlyCurrentCallUserReferences(t *testing.T) {
	order := []string{"weather", "secret", "city", "current", "later"}
	heard := map[string]string{"weather": "你帮我查一下天气吧。", "secret": "密码 private-value", "city": "天津的。", "current": "明天呢？", "later": "不能继承未来输入"}
	h := nativeCallWorkHistory(order, heard, "current")
	if len(h) != 2 || h[0].Content != heard["weather"] || h[1].Content != heard["city"] {
		t.Fatal(h)
	}
	ctx := context.WithValue(context.Background(), assistantCallHistoryKey{}, h)
	b, _ := json.Marshal(assistantCurrentCallHistory(ctx))
	if strings.Contains(string(b), "private-value") || strings.Contains(string(b), "未来") {
		t.Fatal("non-reference input included")
	}
}
func TestNativeWeatherFactsRequireLiveForecastProof(t *testing.T) {
	for _, s := range []string{"天津今天多云，19到25度。", "明天20到26度，湿度76%。", "下周不会有雨。"} {
		if !nativeWeatherClaim(s) {
			t.Fatal(s)
		}
	}
	for _, s := range []string{"天气查询失败，不能确认。", "请告诉我城市。", "我没有查到结果。"} {
		if nativeWeatherClaim(s) {
			t.Fatal(s)
		}
	}
	for _, s := range []string{"Hello.", "哦。", "嗯", "好的", "什么话题？"} {
		if nativeOpeningTopic(s) {
			t.Fatal("ack displaced actual topic", s)
		}
	}
	if !nativeOpeningTopic("帮我查一下今天天气。") {
		t.Fatal("meaningful topic discarded")
	}
}
func TestAssistantSIPCatalogueSearchNeverLoadsColdHistories(t *testing.T) {
	a := testApp(t)
	a.mu.Lock()
	a.sessions["cold-target"] = &Session{ID: "cold-target", Number: 188, Title: "糖尿病资料查询", historyDeferred: true}
	a.sessions["cold-unrelated"] = &Session{ID: "cold-unrelated", Number: 10, Title: "另一个项目", historyDeferred: true}
	// No history loaders exist: a full-body scan would drop the wanted match.
	result := a.searchAssistantCallSessionsLocked("糖尿病", true)
	target := a.resolveSessionRefMetadata("#188")
	a.mu.Unlock()
	matches := result["matches"].([]crossSessionSummary)
	if len(matches) != 1 || matches[0].Number != 188 || result["historySearched"] != false || target == nil || !target.historyDeferred {
		t.Fatal(result, target)
	}
}

func TestNativeWeatherReadbackRejectsInventedFactsAndKeepsRealPluginResult(t *testing.T) {
	for _, engine := range []string{"builtin", "mixsoul"} {
		for _, withReceipt := range []bool{false, true} {
			name := engine + "-missing"
			if withReceipt {
				name = engine + "-receipt"
			}
			t.Run(name, func(t *testing.T) {
				t.Setenv("AIDE_ASSISTANT_ENGINE", engine)
				a := testApp(t)
				unlockAssistantSessionForTest(t, a)
				id := assistantSessionID(t, a)
				if withReceipt {
					installAssistantWeatherFixture(t, a)
				}
				var calls atomic.Int32
				model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					n := calls.Add(1)
					raw, _ := io.ReadAll(r.Body)
					if n == 1 && (!bytes.Contains(raw, []byte("查天气")) || !bytes.Contains(raw, []byte("天津的"))) {
						t.Error("current call reference context lost")
					}
					text := "天津今天多云，19到25度。"
					if withReceipt {
						text = "天气插件显示天津22度，来源为 fixture。"
					}
					message := map[string]any{"role": "assistant", "content": text}
					if withReceipt && n == 1 {
						message = map[string]any{"role": "assistant", "tool_calls": []any{map[string]any{"id": "forecast-fixture", "type": "function", "function": map[string]any{"name": "weather_forecast", "arguments": "{}"}}}}
					}
					jsonOut(w, 200, map[string]any{"choices": []any{map[string]any{"message": message}}})
				}))
				defer model.Close()
				a.mu.Lock()
				a.settings.Model = "fixture"
				a.settings.BaseURL = model.URL
				a.settings.Models, a.settings.ModelProviders = nil, nil
				a.mu.Unlock()
				if err := a.vault.Put(VaultIDModelAPIKey, VaultTypeModelAPIKey, "fixture", []byte("fixture"), ""); err != nil {
					t.Fatal(err)
				}
				ctx := context.WithValue(context.Background(), assistantCallHistoryKey{}, []Message{{Role: "user", Content: "查天气"}})
				result := a.realtimeCallWork(ctx, id, a.wsID(), "天津的。")
				if withReceipt {
					if !strings.Contains(result, "22度") {
						t.Fatal(result)
					}
				} else if strings.Contains(result, "19到25") || !strings.Contains(result, "没有取得天气插件") {
					t.Fatal("invented facts escaped", result)
				}
			})
		}
	}
}
