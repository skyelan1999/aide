package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestVoiceFallbackRecordPlaintext 未加密时：分析失败兜底 recordFallback 必须记录并落盘，
// 重启（重新 newVoiceAgent）后历史仍在，且 historyDesc 倒序。
func TestVoiceFallbackRecordPlaintext(t *testing.T) {
	dir := t.TempDir()
	va := newVoiceAgent(dir)
	va.recordFallback("帮我把按钮改成蓝色", "小秘分析失败，直接发送: 模型没有返回文本内容 (finish_reason=length)")

	if got := len(va.historyDesc()); got != 1 {
		t.Fatalf("historyDesc len = %d, want 1", got)
	}
	b, err := os.ReadFile(filepath.Join(dir, "voice-history.json"))
	if err != nil {
		t.Fatalf("persist file missing: %v", err)
	}
	if !strings.Contains(string(b), "帮我把按钮改成蓝色") {
		t.Fatalf("plaintext history not persisted: %s", b)
	}
	// 模拟重启：从文件重新加载
	va2 := newVoiceAgent(dir)
	if got := len(va2.historyDesc()); got != 1 {
		t.Fatalf("after reload len = %d, want 1", got)
	}
	if va2.historyDesc()[0].Action != "send" {
		t.Fatalf("fallback entry action = %q, want send", va2.historyDesc()[0].Action)
	}
}

// TestVoiceEncryptLifecycle 加密全周期：启用前历史被迁移加密，解锁态继续记录，
// 文件不含明文；锁定后不记录（安全）；重启后错误密钥被拒、正确密钥恢复全部历史。
func TestVoiceEncryptLifecycle(t *testing.T) {
	dir := t.TempDir()
	va := newVoiceAgent(dir)
	va.recordFallback("启用加密前的一条记录", "未加密期") // 启用前明文历史
	if err := va.enable("s3cret-pass"); err != nil {
		t.Fatalf("enable: %v", err)
	}
	// 解锁态：仍应记录（加密落盘）
	va.recordFallback("解锁态又听到一句", "正常判断")
	if got := len(va.historyDesc()); got != 2 {
		t.Fatalf("unlocked len = %d, want 2", got)
	}
	b, _ := os.ReadFile(filepath.Join(dir, "voice-history.json"))
	bs := string(b)
	if !strings.Contains(bs, `"encrypted":true`) {
		t.Fatalf("file not marked encrypted: %s", bs)
	}
	if strings.Contains(bs, "解锁态又听到一句") || strings.Contains(bs, "启用加密前的一条记录") {
		t.Fatalf("cipher file must not contain plaintext heard: %s", bs)
	}

	// 锁定：清明文，之后的兜底不记录
	va.lock()
	if got := va.encStatus()["count"]; got != 0 {
		t.Fatalf("after lock count = %v, want 0", got)
	}
	va.recordFallback("锁定期间的话不应落盘", "锁定")
	if got := va.encStatus()["count"]; got != 0 {
		t.Fatalf("locked must not record, count = %v, want 0", got)
	}

	// 模拟重启：从密文加载为锁定态
	restarted := newVoiceAgent(dir)
	if st := restarted.encStatus(); st["encrypted"] != true || st["unlocked"] != false {
		t.Fatalf("reload should be encrypted+locked, got %v", st)
	}
	if err := restarted.unlock("wrong-pass"); err == nil {
		t.Fatal("wrong password should be rejected")
	}
	if err := restarted.unlock("s3cret-pass"); err != nil {
		t.Fatalf("unlock with right password: %v", err)
	}
	if got := len(restarted.historyDesc()); got != 2 {
		t.Fatalf("after unlock len = %d, want 2", got)
	}
}

// TestVoiceAnalyzeRecordsDecision analyze 成功时决策必须落盘到历史（非流式上游）。
func TestVoiceAnalyzeRecordsDecision(t *testing.T) {
	decision := `{"action":"send","summarized":"把设置界面按钮改成蓝色","ask":"","reason":"用户在对aide说话，意图明确"}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		jsonOut(w, 200, map[string]any{
			"choices": []any{map[string]any{"message": Message{Role: "assistant", Content: decision}}},
		})
	}))
	defer srv.Close()

	dir := t.TempDir()
	va := newVoiceAgent(dir)
	cfg := Settings{BaseURL: srv.URL, Model: "test"}
	entry, err := va.analyze(context.Background(), cfg, "嗯那个帮我把按钮颜色改成蓝色", "")
	if err != nil {
		t.Fatalf("analyze: %v", err)
	}
	if entry.Action != "send" || entry.Text != "把设置界面按钮改成蓝色" {
		t.Fatalf("parsed entry wrong: %+v", entry)
	}
	if got := len(va.historyDesc()); got != 1 {
		t.Fatalf("decision not recorded, len = %d, want 1", got)
	}
}
