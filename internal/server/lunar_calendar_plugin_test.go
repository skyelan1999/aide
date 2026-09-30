package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestLunarCalendarPluginAvailableToAideAndXiaomi(t *testing.T) {
	a := testApp(t)
	code, err := os.ReadFile(filepath.Join("..", "..", "plugins", "lunar-calendar", "index.js"))
	if err != nil {
		t.Fatal(err)
	}
	otherPlugin := `'use strict'; module.exports = { name:'other', apply(ctx) { ctx.tool({name:'other_tool', description:'unrelated', handler:()=>'no'}); } };`
	for _, p := range []struct{ id, name, code string }{
		{"lunar-calendar", "Lunar calendar", string(code)},
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
	if !aideTools["get_lunar_date"] || !aideTools["other_tool"] {
		t.Fatalf("aide plugin tools missing: %v", aideTools)
	}
	xiaomiTools := toolNames(a.assistantAgentTools(true))
	if !xiaomiTools["get_lunar_date"] || xiaomiTools["other_tool"] {
		t.Fatalf("XiaoMi should only receive explicitly allowed plugin tools, got %v", xiaomiTools)
	}

	var call ToolCall
	call.Function.Name = "get_lunar_date"
	call.Function.Arguments = `{"date":"2024-02-10"}`
	result := a.execAssistantTool(call, &assistantDecision{})
	var lunar struct {
		GregorianDate string `json:"gregorianDate"`
		LunarDate     string `json:"lunarDate"`
		Zodiac        string `json:"zodiac"`
	}
	if err := json.Unmarshal([]byte(result), &lunar); err != nil {
		t.Fatalf("XiaoMi lunar result is not JSON: %s (%v)", result, err)
	}
	if lunar.GregorianDate != "2024-02-10" || lunar.LunarDate != "甲辰年正月初一" || lunar.Zodiac != "龙" {
		t.Fatalf("unexpected XiaoMi lunar result: %+v", lunar)
	}
	if owner := a.pluginOwnerOf("get_lunar_date"); owner != "lunar-calendar" {
		t.Fatalf("aide plugin owner = %q", owner)
	}
}
