package server

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"
)

// findAssistantSession 返回内存中的小秘系统会话；调用方需自行处理锁。
func findAssistantSession(a *App) *Session {
	for _, s := range a.sessions {
		if s.Kind == assistantSessionKind {
			return s
		}
	}
	return nil
}

// mustPostSession 通过 HTTP 创建一个普通会话并反序列化，便于读取分配到的 Number。
func mustPostSession(t *testing.T, a *App, title string) Session {
	t.Helper()
	w := request(a, "POST", "/api/sessions", map[string]string{"title": title})
	requireStatus(t, w, 201)
	var s Session
	if err := json.Unmarshal(w.Body.Bytes(), &s); err != nil {
		t.Fatalf("decode session: %v", err)
	}
	return s
}

// TestSessionNumberUnique 创建多个普通会话：编号唯一且严格递增。
func TestSessionNumberUnique(t *testing.T) {
	a := testApp(t)
	s1 := mustPostSession(t, a, "一")
	s2 := mustPostSession(t, a, "二")
	s3 := mustPostSession(t, a, "三")
	if s1.Number <= 0 || s2.Number <= 0 || s3.Number <= 0 {
		t.Fatalf("编号应 >0: %d %d %d", s1.Number, s2.Number, s3.Number)
	}
	if !(s1.Number < s2.Number && s2.Number < s3.Number) {
		t.Fatalf("编号应严格递增: %d < %d < %d 不成立", s1.Number, s2.Number, s3.Number)
	}
	if s1.Number == s2.Number || s2.Number == s3.Number {
		t.Fatalf("编号重复: %d %d %d", s1.Number, s2.Number, s3.Number)
	}
}

// TestSessionNumberNotReused 删除会话后新会话编号不回退。
func TestSessionNumberNotReused(t *testing.T) {
	a := testApp(t)
	s1 := mustPostSession(t, a, "待删除")
	n1 := s1.Number
	requireStatus(t, request(a, "DELETE", "/api/sessions/"+s1.ID, nil), 200)
	s2 := mustPostSession(t, a, "新的")
	if s2.Number <= n1 {
		t.Fatalf("删除后编号被复用/回退: 旧=%d 新=%d", n1, s2.Number)
	}
}

// TestAssistantSessionIdempotent 启动两次后恰好一个小秘系统会话。
func TestAssistantSessionIdempotent(t *testing.T) {
	a := testApp(t)
	count := 0
	for _, s := range a.sessions {
		if s.Kind == assistantSessionKind {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("首次启动后 assistant 会话数=%d, 期望 1", count)
	}
	// 二次启动：不得再创建第二个
	a2, err := New(a.workPath, a.reference.Name(), a.dataPath)
	if err != nil {
		t.Fatal(err)
	}
	defer a2.Close()
	count2 := 0
	for _, s := range a2.sessions {
		if s.Kind == assistantSessionKind {
			count2++
		}
	}
	if count2 != 1 {
		t.Fatalf("二次启动后 assistant 会话数=%d, 期望 1", count2)
	}
}

// TestAssistantSessionPinned 小秘会话 pinned=true 且不可归档/删除。
func TestAssistantSessionPinned(t *testing.T) {
	a := testApp(t)
	a.mu.Lock()
	as := findAssistantSession(a)
	if as == nil {
		a.mu.Unlock()
		t.Fatal("未找到小秘系统会话")
	}
	id := as.ID
	if !as.Pinned {
		a.mu.Unlock()
		t.Fatal("小秘会话应永久 pinned")
	}
	a.mu.Unlock()
	// 不可归档
	requireStatus(t, request(a, "PATCH", "/api/sessions/"+id, map[string]any{"archived": true}), 400)
	// 不可删除
	requireStatus(t, request(a, "DELETE", "/api/sessions/"+id, nil), 403)
}

// TestAssistantSessionTitleSync 修改 VoiceAssistantName 后小秘会话标题跟随。
func TestAssistantSessionTitleSync(t *testing.T) {
	a := testApp(t)
	requireStatus(t, request(a, "PUT", "/api/settings", map[string]any{
		"baseURL": "https://api.example.com", "model": "m1",
	}), 200)
	requireStatus(t, request(a, "PUT", "/api/settings", map[string]any{
		"voiceAssistantName": "秘书小A", "activeModel": "m1",
	}), 200)
	a.mu.Lock()
	as := findAssistantSession(a)
	title := ""
	if as != nil {
		title = as.Title
	}
	a.mu.Unlock()
	if title != "秘书小A" {
		t.Fatalf("小秘会话标题未跟随 VoiceAssistantName: %q", title)
	}
}

// TestAssistantSessionPasswordGate 无密码→401；正确密码→200；错误密码→401。
func TestAssistantSessionPasswordGate(t *testing.T) {
	a := testApp(t)
	a.mu.Lock()
	as := findAssistantSession(a)
	if as == nil {
		a.mu.Unlock()
		t.Fatal("未找到小秘系统会话")
	}
	id := as.ID
	a.mu.Unlock()

	// 未设置账户密码 → 401
	requireStatus(t, request(a, "POST", "/api/sessions/"+id+"/unlock-assistant",
		map[string]string{"password": "whatever"}), 401)

	// 设置账户密码
	a.mu.Lock()
	a.settings.UserPasswordHash = mustHashPassword("secret")
	if err := atomicJSON(SettingsPath(a.dataPath), a.settings); err != nil {
		a.mu.Unlock()
		t.Fatal(err)
	}
	a.mu.Unlock()

	// 错误密码 → 401
	requireStatus(t, request(a, "POST", "/api/sessions/"+id+"/unlock-assistant",
		map[string]string{"password": "wrong"}), 401)
	// 正确密码 → 200
	requireStatus(t, request(a, "POST", "/api/sessions/"+id+"/unlock-assistant",
		map[string]string{"password": "secret"}), 200)
}

// TestSearchSessions 关键词搜索返回匹配会话（含归档）。
func TestSearchSessions(t *testing.T) {
	a := testApp(t)
	s1 := mustPostSession(t, a, "苹果季度报告")
	s2 := mustPostSession(t, a, "香蕉采购清单")
	requireStatus(t, request(a, "PATCH", "/api/sessions/"+s2.ID, map[string]any{"archived": true}), 200)

	a.mu.Lock()
	fresh := a.searchSessionsTool("苹果", true)
	arch := a.searchSessionsTool("香蕉", true)
	noArch := a.searchSessionsTool("香蕉", false)
	a.mu.Unlock()

	found := false
	for _, r := range fresh {
		if r.ID == s1.ID {
			found = true
		}
	}
	if !found {
		t.Fatal("关键词「苹果」未命中会话")
	}
	found = false
	for _, r := range arch {
		if r.ID == s2.ID {
			found = true
		}
	}
	if !found {
		t.Fatal("includeArchived=true 时未命中已归档会话「香蕉」")
	}
	for _, r := range noArch {
		if r.ID == s2.ID {
			t.Fatal("includeArchived=false 时不应返回已归档会话")
		}
	}
}

// TestGetSessionByNumber 按 #N 编号读取会话。
func TestGetSessionByNumber(t *testing.T) {
	a := testApp(t)
	s := mustPostSession(t, a, "按编号找我")
	a.mu.Lock()
	got, err := a.getSessionTool("#" + strconv.Itoa(s.Number))
	a.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != s.ID {
		t.Fatalf("#%d 解析到 %s, 期望 %s", s.Number, got.ID, s.ID)
	}
}

// TestGetSessionByID 按 hash ID 读取会话。
func TestGetSessionByID(t *testing.T) {
	a := testApp(t)
	s := mustPostSession(t, a, "按ID找我")
	a.mu.Lock()
	got, err := a.getSessionTool(s.ID)
	a.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != s.ID {
		t.Fatalf("ID 解析到 %s, 期望 %s", got.ID, s.ID)
	}
}

// TestFollowSession 标记跟进后元数据更新。
func TestFollowSession(t *testing.T) {
	a := testApp(t)
	s := mustPostSession(t, a, "待跟进")
	a.mu.Lock()
	updated, err := a.followSessionTool(s.ID, "周五前看结论")
	a.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if !updated.FollowedByAssistant || updated.FollowNote != "周五前看结论" {
		t.Fatalf("跟进标记未写入: %+v", updated)
	}
}

// TestPushToSession 推送消息后会话包含该消息。
func TestPushToSession(t *testing.T) {
	a := testApp(t)
	s := mustPostSession(t, a, "接收推送")
	a.mu.Lock()
	pushed, err := a.pushToSessionTool(s.ID, "这是小秘推送的备注")
	a.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	n := len(pushed.Messages)
	if n == 0 || !strings.Contains(pushed.Messages[n-1].Content, "小秘推送") ||
		!strings.Contains(pushed.Messages[n-1].Content, "这是小秘推送的备注") {
		t.Fatalf("推送消息未出现在会话: %+v", pushed.Messages)
	}
}

// TestCrossSessionToolsOnlyAssistant 普通会话不暴露跨会话工具，仅小秘会话暴露 4 个。
func TestCrossSessionToolsOnlyAssistant(t *testing.T) {
	a := testApp(t)
	normal := mustPostSession(t, a, "普通会话")
	a.mu.Lock()
	var as *Session
	for _, s := range a.sessions {
		if s.Kind == assistantSessionKind {
			as = s
		}
	}
	normalTools := a.crossSessionToolsFor(&normal)
	assistantTools := a.crossSessionToolsFor(as)
	a.mu.Unlock()

	if len(normalTools) != 0 {
		t.Fatalf("普通会话不应暴露跨会话工具, 实际 %v", normalTools)
	}
	if len(assistantTools) != 4 {
		t.Fatalf("小秘会话应暴露 4 个跨会话工具, 实际 %v", assistantTools)
	}
	want := map[string]bool{"search_sessions": false, "get_session": false, "follow_session": false, "push_to_session": false}
	for _, name := range assistantTools {
		if _, ok := want[name]; !ok {
			t.Fatalf("未知工具 %q", name)
		}
		want[name] = true
	}
	for name, seen := range want {
		if !seen {
			t.Fatalf("缺少工具 %q", name)
		}
	}
}
