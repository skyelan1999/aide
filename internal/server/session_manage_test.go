package server

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSessionPinArchiveListAndPersistence 置顶/归档、列表过滤与排序、持久化。
func TestSessionPinArchiveListAndPersistence(t *testing.T) {
	a := testApp(t)
	w := request(a, "POST", "/api/sessions", map[string]string{"title": "first"})
	var s1 Session
	_ = json.Unmarshal(w.Body.Bytes(), &s1)
	w = request(a, "POST", "/api/sessions", map[string]string{"title": "second"})
	var s2 Session
	_ = json.Unmarshal(w.Body.Bytes(), &s2)

	requireStatus(t, request(a, "PATCH", "/api/sessions/"+s2.ID, map[string]any{"pinned": true}), 200)
	requireStatus(t, request(a, "PATCH", "/api/sessions/"+s1.ID, map[string]any{"archived": true}), 200)

	// 默认列表：归档会话不出现，置顶会话排最前
	w = request(a, "GET", "/api/sessions", nil)
	var list []map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &list)
	if len(list) != 1 || list[0]["id"] != s2.ID || list[0]["pinned"] != true {
		t.Fatalf("default list = %v", list)
	}
	// archived=1：仅归档会话
	w = request(a, "GET", "/api/sessions?archived=1", nil)
	_ = json.Unmarshal(w.Body.Bytes(), &list)
	if len(list) != 1 || list[0]["id"] != s1.ID || list[0]["archived"] != true {
		t.Fatalf("archived list = %v", list)
	}
	// 持久化
	a2, err := New(a.workPath, a.reference.Name(), a.dataPath)
	if err != nil {
		t.Fatal(err)
	}
	defer a2.Close()
	if s := a2.sessions[s1.ID]; s == nil || !s.Archived {
		t.Fatal("archive not persisted")
	}
	if !a2.sessions[s2.ID].Pinned {
		t.Fatal("pin not persisted")
	}
	// 404
	requireStatus(t, request(a, "PATCH", "/api/sessions/nope", map[string]any{"pinned": true}), 404)
}

// TestSessionDeleteRemovesFileAndCancelsRun 删除会话：移除磁盘文件、取消运行中任务、
// 收尾保存不复活会话文件；重复删除 404。
func TestSessionDeleteRemovesFileAndCancelsRun(t *testing.T) {
	a := testApp(t)
	s := &Session{ID: newID(), Title: "s", Runs: []*Task{{ID: newID(), Status: "running", Steer: make(chan string, 4)}}}
	a.sessions[s.ID] = s
	ctx, cancel := context.WithCancel(context.Background())
	a.cancels[s.Runs[0].ID] = cancel
	if err := a.save(s); err != nil {
		t.Fatal(err)
	}
	dataFile := filepath.Join(a.dataPath, "session-"+s.ID+".json")
	if _, err := os.Stat(dataFile); err != nil {
		t.Fatal("session file not created")
	}
	requireStatus(t, request(a, "DELETE", "/api/sessions/"+s.ID, nil), 200)
	if _, ok := a.sessions[s.ID]; ok {
		t.Fatal("session still in memory")
	}
	if _, err := os.Stat(dataFile); !os.IsNotExist(err) {
		t.Fatal("session file not removed")
	}
	select {
	case <-ctx.Done():
	default:
		t.Fatal("running task not cancelled")
	}
	if _, ok := a.cancels[s.Runs[0].ID]; ok {
		t.Fatal("cancel entry leaked")
	}
	// 模拟 execute 收尾保存：不得把会话文件复活
	if err := a.save(s); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dataFile); !os.IsNotExist(err) {
		t.Fatal("session file resurrected by final save")
	}
	requireStatus(t, request(a, "DELETE", "/api/sessions/"+s.ID, nil), 404)
}

// TestExportSessions 全部导出：含归档会话、attachment 头、不泄漏访问令牌。
func TestExportSessions(t *testing.T) {
	a := testApp(t)
	requireStatus(t, request(a, "POST", "/api/sessions", map[string]string{"title": "正常会话"}), 201)
	w := request(a, "POST", "/api/sessions", map[string]string{"title": "归档会话"})
	var s2 Session
	_ = json.Unmarshal(w.Body.Bytes(), &s2)
	requireStatus(t, request(a, "PATCH", "/api/sessions/"+s2.ID, map[string]any{"archived": true}), 200)

	w = request(a, "GET", "/api/export", nil)
	requireStatus(t, w, 200)
	if cd := w.Header().Get("Content-Disposition"); !strings.Contains(cd, "attachment") || !strings.Contains(cd, "aide-sessions-") {
		t.Fatalf("content-disposition = %q", cd)
	}
	var out struct {
		Count    int       `json:"count"`
		Sessions []Session `json:"sessions"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("export json: %v", err)
	}
	if out.Count != 2 || len(out.Sessions) != 2 {
		t.Fatalf("export = %+v", out)
	}
	var titles []string
	for _, s := range out.Sessions {
		titles = append(titles, s.Title)
		if s.Deleted {
			t.Fatal("deleted flag leaked")
		}
	}
	if !strings.Contains(strings.Join(titles, ","), "归档会话") {
		t.Fatalf("archived session missing: %v", titles)
	}
	if strings.Contains(w.Body.String(), a.token) {
		t.Fatal("access token leaked in export")
	}
}

// TestSessionOrderByActivity 非置顶按活动时间倒序；置顶永远在最前（活动排序不把置顶顶下去）。
func TestSessionOrderByActivity(t *testing.T) {
	a := testApp(t)
	var sA, sB Session
	_ = json.Unmarshal(request(a, "POST", "/api/sessions", map[string]string{"title": "A"}).Body.Bytes(), &sA)
	_ = json.Unmarshal(request(a, "POST", "/api/sessions", map[string]string{"title": "B"}).Body.Bytes(), &sB)
	// touch A → A 最新活动，排最前
	requireStatus(t, request(a, "PATCH", "/api/sessions/"+sA.ID, map[string]any{"touch": true}), 200)
	w := request(a, "GET", "/api/sessions", nil)
	var list []map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &list)
	if list[0]["id"] != sA.ID {
		t.Fatalf("activity order = %v", list)
	}
	// 置顶 B → B 永远最前，即使 A 再活动
	requireStatus(t, request(a, "PATCH", "/api/sessions/"+sB.ID, map[string]any{"pinned": true}), 200)
	requireStatus(t, request(a, "PATCH", "/api/sessions/"+sA.ID, map[string]any{"touch": true}), 200)
	w = request(a, "GET", "/api/sessions", nil)
	_ = json.Unmarshal(w.Body.Bytes(), &list)
	if list[0]["id"] != sB.ID || list[1]["id"] != sA.ID {
		t.Fatalf("pinned should stay on top: %v", list)
	}
}

// TestSessionCheckedFlag 完成高亮查看标记：check 持久清除并可持久化。
func TestSessionCheckedFlag(t *testing.T) {
	a := testApp(t)
	s := &Session{ID: newID(), Title: "s", Runs: []*Task{{ID: newID(), Status: "completed"}}}
	a.sessions[s.ID] = s
	if err := a.save(s); err != nil {
		t.Fatal(err)
	}
	// 默认未查看
	w := request(a, "GET", "/api/sessions", nil)
	var list []map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &list)
	if len(list) != 1 || list[0]["checked"] != false {
		t.Fatalf("default checked = %v", list)
	}
	// check → 清除
	requireStatus(t, request(a, "PATCH", "/api/sessions/"+s.ID, map[string]any{"check": true}), 200)
	w = request(a, "GET", "/api/sessions", nil)
	_ = json.Unmarshal(w.Body.Bytes(), &list)
	if list[0]["checked"] != true {
		t.Fatalf("checked = %v", list)
	}
	// 持久化
	a2, err := New(a.workPath, a.reference.Name(), a.dataPath)
	if err != nil {
		t.Fatal(err)
	}
	defer a2.Close()
	if !a2.sessions[s.ID].Checked {
		t.Fatal("checked not persisted")
	}
}
