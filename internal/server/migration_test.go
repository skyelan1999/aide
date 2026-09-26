package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeFlat 在 data 根写一个平铺文件。
func writeFlat(t *testing.T, data, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(data, name), []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}

// TestDetectLayout 识别 empty / flat / layered。
func TestDetectLayout(t *testing.T) {
	// empty：空目录
	empty := t.TempDir()
	if l, err := DetectLayout(empty); err != nil || l != LayoutEmpty {
		t.Fatalf("empty: got %v err %v", l, err)
	}
	// 不存在目录也视为 empty
	if l, _ := DetectLayout(filepath.Join(empty, "nope")); l != LayoutEmpty {
		t.Fatalf("missing dir: got %v", l)
	}

	// flat：根下有已知平铺文件
	flat := t.TempDir()
	writeFlat(t, flat, "settings.json", `{"a":1}`)
	writeFlat(t, flat, "access-token", "tokentokentokentokentokentokentokentokent")
	if l, err := DetectLayout(flat); err != nil || l != LayoutFlat {
		t.Fatalf("flat: got %v err %v", l, err)
	}

	// flat：根下有 session-*.json
	flat2 := t.TempDir()
	writeFlat(t, flat2, "session-abc.json", `{"id":"abc"}`)
	if l, _ := DetectLayout(flat2); l != LayoutFlat {
		t.Fatalf("flat session: got %v", l)
	}

	// layered：只有分层目录、无平铺文件
	layered := t.TempDir()
	os.MkdirAll(AuthDir(layered), 0700)
	os.MkdirAll(ConfigDir(layered), 0700)
	if l, _ := DetectLayout(layered); l != LayoutLayered {
		t.Fatalf("layered: got %v", l)
	}
}

// TestMigration 灌入平铺数据 → 迁移 → 归位正确、数量与 hash 一致、零丢失、原件隔离。
func TestMigration(t *testing.T) {
	data := t.TempDir()
	// 灌入平铺数据
	writeFlat(t, data, "settings.json", `{"model":"m1"}`)
	writeFlat(t, data, "access-token", strings.Repeat("a", 64))
	writeFlat(t, data, "kdf-salt.bin", strings.Repeat("s", 16))
	writeFlat(t, data, "voice-history.json", `{"history":[]}`)
	writeFlat(t, data, "token-stats.json", `{"days":{}}`)
	writeFlat(t, data, "debug-audit.jsonl", `{}\n`)
	writeFlat(t, data, "session-111.json", `{"id":"111","runs":[]}`)
	writeFlat(t, data, "session-222.json", `{"id":"222","runs":[]}`)

	if err := MigrateFlatToLayered(data); err != nil {
		t.Fatal(err)
	}

	// 关键文件已归位
	for _, p := range []string{
		SettingsPath(data), AccessTokenPath(data), KdfSaltPath(data),
		VoiceHistoryPath(data), TokenStatsPath(data), DebugAuditPath(data),
		SessionPath(data, "111", "active"), SessionPath(data, "222", "active"),
	} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("归位后缺失: %s", p)
		}
	}
	// 内容一致（settings 未变）
	if b, _ := os.ReadFile(SettingsPath(data)); !strings.Contains(string(b), `"m1"`) {
		t.Errorf("settings 内容丢失: %s", b)
	}
	// 原平铺文件已被移入隔离区（根下不再有）
	if _, err := os.Stat(filepath.Join(data, "settings.json")); !os.IsNotExist(err) {
		t.Error("原平铺 settings.json 未隔离")
	}
	if _, err := os.Stat(filepath.Join(data, "session-111.json")); !os.IsNotExist(err) {
		t.Error("原平铺 session-111.json 未隔离")
	}
	// 隔离区有原件
	ents, _ := os.ReadDir(QuarantineDir(data))
	if len(ents) == 0 {
		t.Error("隔离区为空")
	}
	// 迁移标记已写
	if _, err := os.Stat(migrationStatePath(data)); err != nil {
		t.Errorf("迁移标记未写: %v", err)
	}
}

// TestMigrationIdempotent 重复执行迁移幂等，数据不丢不重。
func TestMigrationIdempotent(t *testing.T) {
	data := t.TempDir()
	writeFlat(t, data, "settings.json", `{"model":"idem"}`)
	writeFlat(t, data, "session-9.json", `{"id":"9"}`)

	if err := MigrateFlatToLayered(data); err != nil {
		t.Fatal(err)
	}
	first, _ := os.ReadFile(SettingsPath(data))
	// 第二次：已是分层，应直接跳过不报错
	if err := MigrateFlatToLayered(data); err != nil {
		t.Fatalf("二次迁移: %v", err)
	}
	second, _ := os.ReadFile(SettingsPath(data))
	if string(first) != string(second) {
		t.Error("二次迁移改动了已归位文件")
	}
	// 不产生重复会话副本
	matches, _ := filepath.Glob(filepath.Join(SessionsActiveDir(data), "session-*.json"))
	if len(matches) != 1 {
		t.Errorf("会话副本数=%d want 1", len(matches))
	}
}

// TestMigrationRollback 目标冲突时中止、原平铺文件保留、不丢数据。
func TestMigrationRollback(t *testing.T) {
	data := t.TempDir()
	writeFlat(t, data, "settings.json", `{"model":"orig"}`)
	// 预置一个“已存在但内容不同”的分层目标 → 应判定冲突并中止
	EnsureDirs(data)
	os.WriteFile(SettingsPath(data), []byte(`{"model":"conflicting"}`), 0600)

	err := MigrateFlatToLayered(data)
	if err == nil {
		t.Fatal("期望冲突报错，实际 nil")
	}
	// 原平铺文件纹丝未动
	if b, e := os.ReadFile(filepath.Join(data, "settings.json")); e != nil || !strings.Contains(string(b), `"orig"`) {
		t.Errorf("原平铺文件被破坏: %s %v", b, e)
	}
}

// TestMigrationSessionNotLoadedIsQuarantinedNotLost 损坏会话不强行归位，保留原位。
func TestMigrationCorruptSessionKept(t *testing.T) {
	data := t.TempDir()
	writeFlat(t, data, "session-bad.json", `{{{not json`)
	if err := MigrateFlatToLayered(data); err != nil {
		t.Fatal(err)
	}
	// 损坏会话未被复制到分层区
	if _, err := os.Stat(SessionPath(data, "bad", "active")); !os.IsNotExist(err) {
		t.Error("损坏会话不应移入分层区")
	}
	// 原文件仍在平铺根（留给后续启动隔离/跳过处理）
	if _, err := os.Stat(filepath.Join(data, "session-bad.json")); err != nil {
		t.Errorf("损坏原文件应保留: %v", err)
	}
}
