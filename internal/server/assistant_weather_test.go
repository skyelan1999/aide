package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func installAssistantWeatherFixture(t *testing.T, a *App) {
	t.Helper()
	code := `module.exports={name:'weather',apply(ctx){for(const name of ['weather_forecast','weather_location','weather_search_locations','weather_delete'])ctx.tool({name,description:name,parameters:{type:'object',properties:{}},scheduling:{resources:[]},handler:()=>({fixture:true,temperature:22,source:'fixture'})});}};`
	requireStatus(t, request(a, "POST", "/api/plugins", map[string]any{"id": "weather", "name": "Fixture weather", "code": code}), 201)
}
func TestAssistantWeatherCuratedAndDenied(t *testing.T) {
	a := testApp(t)
	installAssistantWeatherFixture(t, a)
	names := map[string]bool{}
	for _, tool := range a.assistantAgentTools(false) {
		s := tool.(map[string]any)
		f := s["function"].(map[string]any)
		n, _ := f["name"].(string)
		names[n] = true
	}
	if !names["weather_forecast"] || !names["weather_location"] || names["weather_delete"] {
		t.Fatal(names)
	}
	call := ToolCall{}
	call.Function.Name = "weather_forecast"
	call.Function.Arguments = `{}`
	dec := assistantDecision{Action: "chat"}
	before := len(a.sessions)
	result := a.execAssistantToolWithContext(context.Background(), call, &dec, "天津今天天气")
	if !strings.Contains(result, `"temperature":22`) || dec.Action != "chat" || len(a.sessions) != before {
		t.Fatal(result, dec)
	}
	a.settings.DisabledTools = []string{"weather_forecast"}
	if got := a.execAssistantTool(call, &dec); !strings.Contains(got, "禁用") {
		t.Fatal(got)
	}
	a.settings.DisabledTools = nil
	if got := a.execAssistantTool(call, &dec, "查188号审批状态，只查询"); !strings.Contains(got, "只读审批查询") {
		t.Fatal(got)
	}
}
func TestAssistantWeatherModelQueriesWithoutDispatch(t *testing.T) {
	for _, engine := range []string{"builtin", "mixsoul"} {
		t.Run(engine, func(t *testing.T) {
			t.Setenv("AIDE_ASSISTANT_ENGINE", engine)
			a := testApp(t)
			unlockAssistantSessionForTest(t, a)
			installAssistantWeatherFixture(t, a)
			before := len(a.sessions)
			model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var in struct {
					Messages []Message `json:"messages"`
				}
				_ = json.NewDecoder(r.Body).Decode(&in)
				for _, m := range in.Messages {
					if m.Role == "tool" {
						jsonOut(w, 200, map[string]any{"choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": "合成地点天气22度。"}}}})
						return
					}
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","tool_calls":[{"id":"direct_weather","type":"function","function":{"name":"weather_forecast","arguments":"{}"}}]}}]}`))
			}))
			defer model.Close()
			d, err := a.runAssistantAgenticLoop(context.Background(), Settings{BaseURL: model.URL, Model: "fixture"}, "不要保存，今天天气怎么样？", "", "call")
			if err != nil || d.Action != "chat" || !strings.Contains(d.Reply, "22") || len(a.sessions) != before {
				t.Fatalf("%+v %v", d, err)
			}
			found := false
			for _, n := range d.ToolsUsed {
				if n == "weather_forecast" {
					found = true
				}
			}
			if !found {
				t.Fatal("missing actual weather call", d)
			}
		})
	}
}
