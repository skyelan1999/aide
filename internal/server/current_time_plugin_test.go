package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestCurrentTimePluginAvailableToAideAndXiaomi(t *testing.T) {
	a := testApp(t)
	code, err := os.ReadFile(filepath.Join("..", "..", "plugins", "current-time", "index.js"))
	if err != nil {
		t.Fatal(err)
	}
	otherPlugin := `'use strict'; module.exports = { name:'other', apply(ctx) { ctx.tool({name:'other_tool', description:'unrelated', handler:()=>'no'}); } };`
	for _, p := range []struct{ id, name, code string }{
		{"current-time", "Current time", string(code)},
		{"other", "Other plugin", otherPlugin},
	} {
		w := request(a, "POST", "/api/plugins", map[string]any{"id": p.id, "name": p.name, "code": p.code})
		requireStatus(t, w, 201)
	}

	toolNames := func(tools []any) map[string]bool {
		got := map[string]bool{}
		for _, item := range tools {
			fn, _ := item.(map[string]any)["function"].(map[string]any)
			if name, _ := fn["name"].(string); name != "" {
				got[name] = true
			}
		}
		return got
	}
	aideTools := toolNames(a.contextToolsFor(&Session{ID: "aide-test"}))
	if !aideTools["get_current_datetime"] || !aideTools["get_week_number"] || !aideTools["other_tool"] {
		t.Fatalf("aide plugin tools missing: %v", aideTools)
	}
	xiaomiTools := toolNames(a.assistantAgentTools(true))
	if !xiaomiTools["get_current_datetime"] || !xiaomiTools["get_week_number"] || xiaomiTools["other_tool"] {
		t.Fatalf("XiaoMi should only receive the clock plugin, got %v", xiaomiTools)
	}

	var call ToolCall
	call.Function.Name = "get_current_datetime"
	call.Function.Arguments = `{"timeZone":"Asia/Shanghai"}`
	result := a.execAssistantTool(call, &assistantDecision{})
	var clock struct {
		Date     string `json:"date"`
		Time     string `json:"time"`
		TimeZone string `json:"timeZone"`
		ISOUTC   string `json:"isoUtc"`
		UnixMS   int64  `json:"unixMs"`
	}
	if err := json.Unmarshal([]byte(result), &clock); err != nil {
		t.Fatalf("XiaoMi clock result is not JSON: %s (%v)", result, err)
	}
	if clock.TimeZone != "Asia/Shanghai" || len(clock.Date) != 10 || len(clock.Time) != 8 || clock.UnixMS == 0 || clock.ISOUTC == "" {
		t.Fatalf("unexpected XiaoMi clock result: %+v", clock)
	}
	call.Function.Name = "get_week_number"
	call.Function.Arguments = `{"date":"2021-01-01"}`
	result = a.execAssistantTool(call, &assistantDecision{})
	var week struct {
		Date       string `json:"date"`
		WeekYear   int    `json:"weekYear"`
		WeekNumber int    `json:"weekNumber"`
		WeekStart  string `json:"weekStart"`
		WeekEnd    string `json:"weekEnd"`
	}
	if err := json.Unmarshal([]byte(result), &week); err != nil {
		t.Fatalf("XiaoMi week result is not JSON: %s (%v)", result, err)
	}
	if week.Date != "2021-01-01" || week.WeekYear != 2020 || week.WeekNumber != 53 || week.WeekStart != "2020-12-28" || week.WeekEnd != "2021-01-03" {
		t.Fatalf("unexpected XiaoMi week result: %+v", week)
	}

	owner := a.pluginOwnerOf("get_current_datetime")
	if owner != "current-time" {
		t.Fatalf("aide plugin owner = %q", owner)
	}
}
