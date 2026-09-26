package server

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// debugAuditLen 当前审计文件中的记录条数。
func debugAuditLen(t *testing.T, a *App) int {
	t.Helper()
	entries, err := a.readDebugAudit()
	if err != nil {
		t.Fatal(err)
	}
	return len(entries)
}

// enableDebugAccess 打开外部调试并生成一个调试令牌，返回明文令牌。
func enableDebugAccess(t *testing.T, a *App) string {
	t.Helper()
	requireStatus(t, request(a, "PUT", "/api/settings", map[string]any{
		"debugAccessEnabled": true,
		"baseURL":            "https://api.example.com/v1",
		"model":              "test-model",
		"apiKey":             "test-key",
	}), 200)
	w := request(a, "POST", "/api/debug/admin/token", map[string]any{})
	requireStatus(t, w, 200)
	var tok struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &tok); err != nil || tok.Token == "" {
		t.Fatalf("生成调试令牌失败: %v %s", err, w.Body.String())
	}
	return tok.Token
}

// TestDebugAuditNoiseReduction 验证审计降噪规则：
//   - 查看审计自身（GET /api/debug/audit）不记录；
//   - owner 本机只读 GET 轮询（/overview）不记录；
//   - 失败/拒绝必记（401_unauthorized）；
//   - 调试令牌(owner=false)对数据端点的实际访问必记，且只存前 8 位短指纹、不存完整令牌；
//   - owner 管理写（吊销）必记。
func TestDebugAuditNoiseReduction(t *testing.T) {
	a := testApp(t)
	plain := enableDebugAccess(t, a)
	base := debugAuditLen(t, a) // 生成令牌的写操作已记一条

	// 1) 查看审计自身：连续点两次，记录数不应增加。
	requireStatus(t, request(a, "GET", "/api/debug/audit", nil), 200)
	requireStatus(t, request(a, "GET", "/api/debug/audit", nil), 200)
	if got := debugAuditLen(t, a); got != base {
		t.Fatalf("查看审计被记录：%d want %d（审计自刷）", got, base)
	}

	// 2) owner 只读 GET 轮询：不记录。
	requireStatus(t, request(a, "GET", "/api/debug/overview", nil), 200)
	requireStatus(t, request(a, "GET", "/api/debug/stats", nil), 200)
	if got := debugAuditLen(t, a); got != base {
		t.Fatalf("owner 只读 GET 被记录：%d want %d", got, base)
	}

	// 3) 失败/拒绝必记：无效调试令牌 -> 401。
	bad := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/debug/overview", nil)
	r.Header.Set("Authorization", "Bearer totally-bogus-token")
	a.Handler().ServeHTTP(bad, r)
	requireStatus(t, bad, 401)
	if got := debugAuditLen(t, a); got != base+1 {
		t.Fatalf("失败请求未记录：%d want %d", got, base+1)
	}

	// 4) 外部调试令牌访问必记，带短指纹，且完整令牌不落盘。
	ext := httptest.NewRecorder()
	r2 := httptest.NewRequest("GET", "/api/debug/overview", nil)
	r2.Header.Set("Authorization", "Bearer "+plain)
	a.Handler().ServeHTTP(ext, r2)
	requireStatus(t, ext, 200)
	entries, err := a.readDebugAudit()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != base+2 {
		t.Fatalf("外部令牌访问未记录：%d want %d", len(entries), base+2)
	}
	last := entries[len(entries)-1]
	if last.Owner {
		t.Fatal("外部调试令牌访问被误记为 owner")
	}
	if last.TokenFP != plain[:8] {
		t.Fatalf("短指纹=%q want %q", last.TokenFP, plain[:8])
	}
	raw, err := os.ReadFile(DebugAuditPath(a.dataPath))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), plain) {
		t.Fatal("完整调试令牌泄漏到审计日志")
	}

	// 5) owner 管理写（吊销）必记。
	requireStatus(t, request(a, "POST", "/api/debug/admin/revoke", map[string]any{}), 200)
	if got := debugAuditLen(t, a); got != base+3 {
		t.Fatalf("owner 管理写未记录：%d want %d", got, base+3)
	}
}

// TestDebugAuditExport 验证三种导出格式内容正确，且导出本身不记录审计。
func TestDebugAuditExport(t *testing.T) {
	a := testApp(t)
	requireStatus(t, request(a, "PUT", "/api/settings", map[string]any{
		"debugAccessEnabled": true,
		"baseURL":            "https://api.example.com/v1",
		"model":              "test-model",
		"apiKey":             "test-key",
	}), 200)

	// 制造两条记录：一次失败（401）+ 一次 owner 管理写（toggle）。
	bad := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/debug/overview", nil)
	r.Header.Set("Authorization", "Bearer nope")
	a.Handler().ServeHTTP(bad, r)
	requireStatus(t, bad, 401)
	requireStatus(t, request(a, "POST", "/api/debug/admin/toggle", map[string]any{"enabled": true}), 200)
	wantN := debugAuditLen(t, a)
	if wantN < 2 {
		t.Fatalf("前置记录不足：%d", wantN)
	}

	// jsonl：每行一条 JSON。
	wj := request(a, "GET", "/api/debug/audit/export?format=jsonl", nil)
	requireStatus(t, wj, 200)
	jlines := 0
	for _, ln := range strings.Split(strings.TrimSpace(wj.Body.String()), "\n") {
		if strings.TrimSpace(ln) != "" {
			jlines++
		}
	}
	if jlines != wantN {
		t.Fatalf("jsonl 行数=%d want %d", jlines, wantN)
	}
	if !strings.Contains(wj.Body.String(), "401_unauthorized") {
		t.Fatal("jsonl 缺少失败记录")
	}

	// json：JSON 数组。
	wa := request(a, "GET", "/api/debug/audit/export?format=json", nil)
	requireStatus(t, wa, 200)
	var arr []debugAuditEntry
	if err := json.Unmarshal(wa.Body.Bytes(), &arr); err != nil {
		t.Fatalf("json 导出不是数组: %v", err)
	}
	if len(arr) != wantN {
		t.Fatalf("json 条数=%d want %d", len(arr), wantN)
	}

	// csv：带 BOM，含表头与数据行。
	wc := request(a, "GET", "/api/debug/audit/export?format=csv", nil)
	requireStatus(t, wc, 200)
	if !strings.HasPrefix(wc.Body.String(), "\xEF\xBB\xBF") {
		t.Fatal("csv 缺少 UTF-8 BOM")
	}
	body := strings.TrimPrefix(wc.Body.String(), "\xEF\xBB\xBF")
	lines := strings.Split(strings.TrimSpace(body), "\n")
	if len(lines) != wantN+1 { // 表头 + 数据
		t.Fatalf("csv 行数=%d want %d", len(lines), wantN+1)
	}
	if !strings.Contains(lines[0], "result") || !strings.Contains(lines[0], "tokenFp") {
		t.Fatalf("csv 表头异常: %q", lines[0])
	}
	if !strings.Contains(body, "401_unauthorized") {
		t.Fatal("csv 缺少失败记录")
	}

	// 非法格式 -> 400。
	requireStatus(t, request(a, "GET", "/api/debug/audit/export?format=xml", nil), 400)

	// 导出/查看审计自身不应增加记录。
	if got := debugAuditLen(t, a); got != wantN {
		t.Fatalf("导出被记入审计：%d want %d", got, wantN)
	}
}
