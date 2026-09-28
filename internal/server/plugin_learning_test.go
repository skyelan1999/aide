package server

import (
	"strings"
	"testing"
)

func TestPluginCapabilityHint(t *testing.T) {
	a := testApp(t)
	// 无 surface → 空
	a.pluginSurface = nil
	if got := a.pluginCapabilityHint(); got != "" {
		t.Fatalf("空 surface 应返回空，得到 %q", got)
	}
	// 一个含可执行工具的插件 + 一个纯面板插件
	a.pluginSurface = []byte(`{"plugins":[
		{"id":"sqlite","provided":["sqlite"],"error":"",
		 "tools":[{"name":"sqlite_query","executable":true,"description":"query"},
		         {"name":"sqlite_schema","executable":true,"description":"schema"}]},
		{"id":"skill","provided":["skill"],"error":"",
		 "tools":[{"name":"skill-list","executable":false,"description":"list"}]}
	]}`)
	got := a.pluginCapabilityHint()
	if !strings.Contains(got, "sqlite_query") || !strings.Contains(got, "sqlite_schema") {
		t.Fatalf("能力提示应包含可执行工具，得到 %q", got)
	}
	if strings.Contains(got, "skill-list") {
		t.Fatalf("纯面板插件的非执行工具不应列出，得到 %q", got)
	}
	if !strings.Contains(got, "扩展能力") {
		t.Fatalf("应包含扩展能力标题，得到 %q", got)
	}
	// 加载错误的插件应跳过
	a.pluginSurface = []byte(`{"plugins":[{"id":"bad","error":"boom","tools":[{"name":"x","executable":true}]}]}`)
	if got := a.pluginCapabilityHint(); got != "" {
		t.Fatalf("全部插件出错时应返回空，得到 %q", got)
	}
}

func TestPluginExperienceLifecycle(t *testing.T) {
	a := testApp(t)
	// 记录一次成功
	a.recordPluginExperience("sqlite_query", "sqlite", true, "返回 12 行数据")
	entries := a.readPluginExperience()
	if len(entries) != 1 {
		t.Fatalf("应有 1 条经验，得到 %d", len(entries))
	}
	if !entries[0].OK || entries[0].Used != 1 || entries[0].Tool != "sqlite_query" {
		t.Fatalf("经验内容错误: %+v", entries[0])
	}
	// 再次记录（失败）→ 合并，计数 +1，状态更新
	a.recordPluginExperience("sqlite_query", "sqlite", false, "no such table: foo")
	entries = a.readPluginExperience()
	if len(entries) != 1 {
		t.Fatalf("应合并为 1 条，得到 %d", len(entries))
	}
	if entries[0].OK || entries[0].Used != 2 {
		t.Fatalf("应更新为失败且计数 2，得到 %+v", entries[0])
	}
	if !strings.Contains(entries[0].Detail, "no such table") {
		t.Fatalf("应更新失败细节，得到 %q", entries[0].Detail)
	}
	// 不同工具 → 新条目
	a.recordPluginExperience("terminal-run", "terminal", true, "ls 输出")
	if entries = a.readPluginExperience(); len(entries) != 2 {
		t.Fatalf("应有 2 条经验，得到 %d", len(entries))
	}
	// 经验提示：失败在前
	hint := a.pluginExperienceHint()
	if !strings.Contains(hint, "sqlite_query") || !strings.Contains(hint, "terminal-run") {
		t.Fatalf("经验提示应含两个工具，得到 %q", hint)
	}
	if strings.Index(hint, "sqlite_query") > strings.Index(hint, "terminal-run") {
		t.Fatalf("失败经验应排在成功经验之前，得到 %q", hint)
	}
	// 重置
	if err := a.resetPluginExperience(); err != nil {
		t.Fatalf("重置失败: %v", err)
	}
	if entries = a.readPluginExperience(); len(entries) != 0 {
		t.Fatalf("重置后应无经验，得到 %d", len(entries))
	}
}

func TestParseExperienceLine(t *testing.T) {
	e := parseExperienceLine("[FAIL] sqlite_query(sqlite) ×3 最近 2026-09-28：no such table: foo")
	if e.OK || e.Tool != "sqlite_query" || e.Plugin != "sqlite" || e.Used != 3 ||
		e.LastAt != "2026-09-28" || e.Detail != "no such table: foo" {
		t.Fatalf("解析失败行错误: %+v", e)
	}
	e = parseExperienceLine("[OK] terminal-run(terminal) ×1 最近 2026-09-28：调用成功")
	if !e.OK || e.Tool != "terminal-run" || e.Used != 1 {
		t.Fatalf("解析成功行错误: %+v", e)
	}
}

func TestSanitizeExpDetail(t *testing.T) {
	got := sanitizeExpDetail("line1\nline2\r\n  multi   space")
	if strings.ContainsAny(got, "\n\r") || strings.Contains(got, "  ") {
		t.Fatalf("应去除换行与重复空格，得到 %q", got)
	}
	long := strings.Repeat("a", 200)
	if got := sanitizeExpDetail(long); !strings.HasSuffix(got, "…") || len(got) > 124 {
		t.Fatalf("超长应截断到 120+…，得到长度 %d", len(got))
	}
}

func TestTrimExperience(t *testing.T) {
	entries := []pluginExperienceEntry{}
	for i := 0; i < maxPluginExpEntries+10; i++ {
		entries = append(entries, pluginExperienceEntry{
			Tool: "ok" + string(rune('A'+i%26)) + string(rune('a'+i/26)),
			OK:   true, Used: 1,
		})
	}
	// 加入几条失败、高频
	entries = append(entries, pluginExperienceEntry{Tool: "fail1", OK: false, Used: 1})
	got := trimExperience(entries)
	if len(got) != maxPluginExpEntries {
		t.Fatalf("应裁剪到上限，得到 %d", len(got))
	}
	// 失败条目必须保留
	foundFail := false
	for _, e := range got {
		if e.Tool == "fail1" {
			foundFail = true
		}
	}
	if !foundFail {
		t.Fatal("裁剪后失败经验应优先保留")
	}
}

func TestRecordPluginExperienceIgnoresNonPlugin(t *testing.T) {
	a := testApp(t)
	a.recordPluginExperience("", "", true, "")
	a.recordPluginExperience("read_file", "", true, "")
	if entries := a.readPluginExperience(); len(entries) != 0 {
		t.Fatalf("非插件工具不应记录，得到 %d", len(entries))
	}
}
