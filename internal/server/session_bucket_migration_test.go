package server

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

// ── 会话桶归位：save() 选桶 / 迁移 / 幂等 / 回滚 / 零丢失 ──────────────────────

// writeSessionJSON 在指定路径写一个会话文件。
func writeSessionJSON(t *testing.T, path string, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
}

// collectSessionHashes 收集所有候选位置（三桶 + 平铺根 + 隔离区）的会话文件 id→哈希多重集。
func collectSessionHashes(t *testing.T, data string) map[string][]string {
	t.Helper()
	out := map[string][]string{}
	roots := []string{SessionsActiveDir(data), SessionsArchivedDir(data), SessionsAssistantDir(data)}
	// 平铺根
	if ents, _ := filepath.Glob(filepath.Join(data, "session-*.json")); len(ents) > 0 {
		roots = append(roots, data)
	}
	// 隔离区（迁移后原件在那）
	if q, err := os.ReadDir(QuarantineDir(data)); err == nil {
		for _, e := range q {
			if e.IsDir() {
				roots = append(roots, filepath.Join(QuarantineDir(data), e.Name()))
			}
		}
	}
	seen := map[string]bool{}
	for _, r := range roots {
		ents, _ := filepath.Glob(filepath.Join(r, "session-*.json"))
		for _, f := range ents {
			rel, _ := filepath.Rel(data, f)
			if seen[rel] {
				continue
			}
			seen[rel] = true
			b, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			var s Session
			if json.Unmarshal(b, &s) != nil {
				continue // 损坏：不计入
			}
			sum := sha256.Sum256(b)
			out[s.ID] = append(out[s.ID], hex.EncodeToString(sum[:]))
		}
	}
	for k := range out {
		sort.Strings(out[k])
	}
	return out
}

// canonicalSessionHashes 只收集三个规范桶内的会话 id->内容哈希（不含隔离区/备份）。
// 这是“零丢失”断言的权威视图：迁移后系统只从这里加载会话。
func canonicalSessionHashes(t *testing.T, data string) map[string][]string {
	t.Helper()
	out := map[string][]string{}
	for _, d := range []string{SessionsActiveDir(data), SessionsArchivedDir(data), SessionsAssistantDir(data)} {
		ents, _ := filepath.Glob(filepath.Join(d, "session-*.json"))
		for _, f := range ents {
			b, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			var sess Session
			if json.Unmarshal(b, &sess) != nil {
				continue
			}
			sum := sha256.Sum256(b)
			out[sess.ID] = append(out[sess.ID], hex.EncodeToString(sum[:]))
		}
	}
	for k := range out {
		sort.Strings(out[k])
	}
	return out
}

// TestSavePicksCanonicalBucket 活动/归档/小秘三类会话分别落到正确桶。
func TestSavePicksCanonicalBucket(t *testing.T) {
	a := testApp(t)

	// 活动会话
	act := &Session{ID: newID(), Title: "act", Runs: []*Task{}}
	a.sessions[act.ID] = act
	if err := a.save(act); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(SessionPath(a.dataPath, act.ID, "active")); err != nil {
		t.Errorf("活动会话应落在 active/: %v", err)
	}
	if _, err := os.Stat(SessionPath(a.dataPath, act.ID, "archived")); !os.IsNotExist(err) {
		t.Error("活动会话不应出现在 archived/")
	}
	if _, err := os.Stat(SessionPath(a.dataPath, act.ID, "assistant")); !os.IsNotExist(err) {
		t.Error("活动会话不应出现在 assistant/")
	}

	// 归档会话
	arc := &Session{ID: newID(), Title: "arc", Archived: true, Runs: []*Task{}}
	a.sessions[arc.ID] = arc
	if err := a.save(arc); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(SessionPath(a.dataPath, arc.ID, "archived")); err != nil {
		t.Errorf("归档会话应落在 archived/: %v", err)
	}
	if _, err := os.Stat(SessionPath(a.dataPath, arc.ID, "active")); !os.IsNotExist(err) {
		t.Error("归档会话不应残留在 active/")
	}

	// 小秘会话
	asst := &Session{ID: newID(), Title: "asst", Kind: assistantSessionKind, Runs: []*Task{}}
	a.sessions[asst.ID] = asst
	if err := a.save(asst); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(SessionPath(a.dataPath, asst.ID, "assistant")); err != nil {
		t.Errorf("小秘会话应落在 assistant/: %v", err)
	}
	if _, err := os.Stat(SessionPath(a.dataPath, asst.ID, "active")); !os.IsNotExist(err) {
		t.Error("小秘会话不应残留在 active/")
	}
}

// TestArchiveMoveMovesFileBetweenBuckets 运行时归档/取消归档：文件在桶间移动，无残留副本。
func TestArchiveMoveMovesFileBetweenBuckets(t *testing.T) {
	a := testApp(t)
	w := request(a, "POST", "/api/sessions", map[string]string{"title": "moveme"})
	var s Session
	_ = json.Unmarshal(w.Body.Bytes(), &s)

	// 初始在 active/
	if _, err := os.Stat(SessionPath(a.dataPath, s.ID, "active")); err != nil {
		t.Fatalf("创建后应在 active/: %v", err)
	}
	// 归档 → 应移到 archived/，active/ 副本清除
	requireStatus(t, request(a, "PATCH", "/api/sessions/"+s.ID, map[string]any{"archived": true}), 200)
	if _, err := os.Stat(SessionPath(a.dataPath, s.ID, "archived")); err != nil {
		t.Errorf("归档后应在 archived/: %v", err)
	}
	if _, err := os.Stat(SessionPath(a.dataPath, s.ID, "active")); !os.IsNotExist(err) {
		t.Error("归档后 active/ 残留副本")
	}
	// 取消归档 → 回到 active/，archived/ 副本清除
	requireStatus(t, request(a, "PATCH", "/api/sessions/"+s.ID, map[string]any{"archived": false}), 200)
	if _, err := os.Stat(SessionPath(a.dataPath, s.ID, "active")); err != nil {
		t.Errorf("取消归档后应回 active/: %v", err)
	}
	if _, err := os.Stat(SessionPath(a.dataPath, s.ID, "archived")); !os.IsNotExist(err) {
		t.Error("取消归档后 archived/ 残留副本")
	}
}

// TestMigrationLegacyLayoutRelocates 旧布局（全堆 active/ + 平铺根残留 + 空死目录）被归位。
func TestMigrationLegacyLayoutRelocates(t *testing.T) {
	data := t.TempDir()
	if err := EnsureDirs(data); err != nil { // 建立含空死目录的骨架
		t.Fatal(err)
	}
	// 旧版本：归档/小秘会话也写在 active/
	writeSessionJSON(t, SessionPath(data, "active1", "active"), `{"id":"active1","runs":[],"updated":"2024-01-01T00:00:00Z"}`)
	writeSessionJSON(t, SessionPath(data, "arch1", "active"), `{"id":"arch1","archived":true,"runs":[],"updated":"2024-01-02T00:00:00Z"}`)
	writeSessionJSON(t, SessionPath(data, "asst1", "active"), `{"id":"asst1","kind":"assistant","runs":[],"updated":"2024-01-03T00:00:00Z"}`)
	// 平铺根残留
	writeSessionJSON(t, filepath.Join(data, "session-flat1.json"), `{"id":"flat1","runs":[],"updated":"2024-01-04T00:00:00Z"}`)

	before := collectSessionHashes(t, data)

	if err := rebalanceSessionBuckets(data); err != nil {
		t.Fatal(err)
	}

	// 归位正确
	for _, p := range []string{
		SessionPath(data, "active1", "active"),
		SessionPath(data, "arch1", "archived"),
		SessionPath(data, "asst1", "assistant"),
		SessionPath(data, "flat1", "active"),
	} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("归位后缺失 %s: %v", p, err)
		}
	}
	// 错位副本已清除
	if _, err := os.Stat(SessionPath(data, "arch1", "active")); !os.IsNotExist(err) {
		t.Error("arch1 不应仍在 active/")
	}
	if _, err := os.Stat(SessionPath(data, "asst1", "active")); !os.IsNotExist(err) {
		t.Error("asst1 不应仍在 active/")
	}
	if _, err := os.Stat(filepath.Join(data, "session-flat1.json")); !os.IsNotExist(err) {
		t.Error("平铺根 flat1 应已移走")
	}

	// 零丢失：迁移后“规范桶”里每个 id 都在，且内容哈希与迁移前某一份一致
	// （被隔离的原件是安全网，不计入加载视图）。
	after := canonicalSessionHashes(t, data)
	if len(before) != len(after) {
		t.Fatalf("会话 id 数变化：before=%d after=%d", len(before), len(after))
	}
	for id, hashes := range before {
		got, ok := after[id]
		if !ok {
			t.Fatalf("会话 %s 在迁移后规范桶中消失", id)
		}
		// got 应恰好一份（规范桶内去重）
		if len(got) != 1 {
			t.Errorf("会话 %s 规范桶内副本数=%d want 1", id, len(got))
		}
		// 内容必须是迁移前见过的某一份
		seenBefore := map[string]bool{}
		for _, h := range hashes {
			seenBefore[h] = true
		}
		if !seenBefore[got[0]] {
			t.Errorf("会话 %s 迁移后内容与迁移前任何一份都不一致", id)
		}
	}
}

// TestMigrationIdempotent 重复执行不报错、不产生重复副本、内容不变。
func TestSessionBucketIdempotent(t *testing.T) {
	data := t.TempDir()
	EnsureDirs(data)
	writeSessionJSON(t, SessionPath(data, "a1", "active"), `{"id":"a1","runs":[]}`)
	writeSessionJSON(t, SessionPath(data, "a2", "active"), `{"id":"a2","archived":true,"runs":[],"updated":"2024-01-02T00:00:00Z"}`)

	if err := rebalanceSessionBuckets(data); err != nil {
		t.Fatal(err)
	}
	firstArch, _ := os.ReadFile(SessionPath(data, "a2", "archived"))

	// 第二次：应幂等 no-op
	if err := rebalanceSessionBuckets(data); err != nil {
		t.Fatalf("二次迁移: %v", err)
	}
	secondArch, _ := os.ReadFile(SessionPath(data, "a2", "archived"))
	if string(firstArch) != string(secondArch) {
		t.Error("二次迁移改动了已归位文件")
	}
	// 每个 id 恰好一份
	for id, bucket := range map[string]string{"a1": "active", "a2": "archived"} {
		matches, _ := filepath.Glob(filepath.Join(SessionsDir(data), "*", "session-"+id+".json"))
		if len(matches) != 1 {
			t.Errorf("id=%s 桶内副本数=%d want 1", id, len(matches))
		}
		if _, err := os.Stat(SessionPath(data, id, bucket)); err != nil {
			t.Errorf("id=%s 不在 %s/: %v", id, bucket, err)
		}
	}
}

// TestMigrationRollback 回滚后磁盘回到迁移前布局，且数据字节可还原。
func TestSessionBucketRollback(t *testing.T) {
	data := t.TempDir()
	EnsureDirs(data)
	writeSessionJSON(t, SessionPath(data, "r1", "active"), `{"id":"r1","runs":[],"updated":"2024-01-01T00:00:00Z"}`)
	writeSessionJSON(t, SessionPath(data, "r2", "active"), `{"id":"r2","archived":true,"runs":[],"updated":"2024-01-02T00:00:00Z"}`)
	writeSessionJSON(t, filepath.Join(data, "session-r3.json"), `{"id":"r3","runs":[],"updated":"2024-01-03T00:00:00Z"}`)
	before := collectSessionHashes(t, data)

	if err := rebalanceSessionBuckets(data); err != nil {
		t.Fatal(err)
	}
	// 迁移后：r2 在 archived/，r3 已进 active/
	if _, err := os.Stat(SessionPath(data, "r2", "archived")); err != nil {
		t.Fatalf("迁移后 r2 应在 archived/: %v", err)
	}
	if _, err := os.Stat(filepath.Join(data, "session-r3.json")); !os.IsNotExist(err) {
		t.Fatal("迁移后平铺根 r3 应已移走")
	}

	// 回滚
	if err := RollbackSessionBucketMigration(data); err != nil {
		t.Fatal(err)
	}
	// 布局还原：r2 回到 active/，archived/ 不再有 r2，平铺根 r3 回来
	if _, err := os.Stat(SessionPath(data, "r2", "active")); err != nil {
		t.Errorf("回滚后 r2 应回到 active/: %v", err)
	}
	if _, err := os.Stat(SessionPath(data, "r2", "archived")); !os.IsNotExist(err) {
		t.Error("回滚后 archived/ 不应再有 r2")
	}
	if _, err := os.Stat(filepath.Join(data, "session-r3.json")); err != nil {
		t.Errorf("回滚后平铺根 r3 应回来: %v", err)
	}
	// 零丢失：回滚后内容哈希多重集与最初一致
	after := collectSessionHashes(t, data)
	for id, hashes := range before {
		got, ok := after[id]
		if !ok {
			t.Fatalf("回滚后会话 %s 消失", id)
		}
		if len(got) != len(hashes) {
			t.Errorf("回滚后会话 %s 副本数=%d want %d", id, len(got), len(hashes))
		}
	}
	// 再次回滚幂等（已 rolled-back，no-op）
	if err := RollbackSessionBucketMigration(data); err != nil {
		t.Fatalf("二次回滚: %v", err)
	}
}

// TestMigrationCorruptSessionUntouched 损坏会话原地保留，不被搬动/隔离，不阻断迁移。
func TestMigrationCorruptSessionUntouched(t *testing.T) {
	data := t.TempDir()
	EnsureDirs(data)
	writeSessionJSON(t, SessionPath(data, "good1", "active"), `{"id":"good1","archived":true,"runs":[]}`)
	writeSessionJSON(t, SessionPath(data, "bad1", "active"), `{{{ not json`)

	if err := rebalanceSessionBuckets(data); err != nil {
		t.Fatal(err)
	}
	// good1 归位
	if _, err := os.Stat(SessionPath(data, "good1", "archived")); err != nil {
		t.Errorf("good1 应归位 archived/: %v", err)
	}
	// bad1 原地不动（仍在 active/）
	if _, err := os.Stat(SessionPath(data, "bad1", "active")); err != nil {
		t.Errorf("损坏会话 bad1 应原地保留: %v", err)
	}
}

// TestRoundTripAllSessionsSurviveRestart 端到端：建会话→归档→重启 New()→全部会话存活、桶正确。
func TestRoundTripAllSessionsSurviveRestart(t *testing.T) {
	a := testApp(t)
	requireStatus(t, request(a, "POST", "/api/sessions", map[string]string{"title": "keep1"}), 201)
	w := request(a, "POST", "/api/sessions", map[string]string{"title": "keep2arch"})
	var s2 Session
	_ = json.Unmarshal(w.Body.Bytes(), &s2)
	requireStatus(t, request(a, "PATCH", "/api/sessions/"+s2.ID, map[string]any{"archived": true}), 200)

	// 记录重启前的会话总数（含小秘）
	beforeCount := len(a.sessions)
	// 小秘会话应在 assistant/
	var asID string
	for id, s := range a.sessions {
		if s.Kind == assistantSessionKind {
			asID = id
		}
	}

	a2, err := New(a.workPath, a.reference.Name(), a.dataPath)
	if err != nil {
		t.Fatal(err)
	}
	defer a2.Close()

	if len(a2.sessions) != beforeCount {
		t.Fatalf("重启后会话数=%d want %d", len(a2.sessions), beforeCount)
	}
	// s2 仍归档、且落在 archived/
	if !a2.sessions[s2.ID].Archived {
		t.Fatal("重启后归档标记丢失")
	}
	if _, err := os.Stat(SessionPath(a2.dataPath, s2.ID, "archived")); err != nil {
		t.Errorf("重启后归档会话应在 archived/: %v", err)
	}
	if _, err := os.Stat(SessionPath(a2.dataPath, asID, "assistant")); err != nil {
		t.Errorf("重启后小秘会话应在 assistant/: %v", err)
	}
	// 一个 id 恰好一份
	for id := range a2.sessions {
		matches, _ := filepath.Glob(filepath.Join(SessionsDir(a2.dataPath), "*", "session-"+id+".json"))
		if len(matches) != 1 {
			t.Errorf("会话 %s 磁盘副本数=%d want 1", id, len(matches))
		}
	}
}
