package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// 本文件集中验证小秘的身份核心（voiceIdentityPrompt）：它必须第一人称、动态跟随名字、
// 明确与 aide 的分工，并被统一拼接到 analyze / narrate / 小秘对话三类 system prompt 最前面。

func TestVoiceIdentityContainsName(t *testing.T) {
	cfg := Settings{VoiceAssistantName: "小秘"}
	if got := voiceIdentityPrompt(cfg); !strings.Contains(got, "「小秘」") {
		t.Fatalf("身份提示未包含配置名字: %q", got)
	}
}

func TestVoiceIdentityFirstPerson(t *testing.T) {
	cfg := Settings{VoiceAssistantName: "小秘"}
	got := voiceIdentityPrompt(cfg)
	if !strings.Contains(got, "我是") {
		t.Fatalf("身份提示缺少第一人称自称\"我是\": %q", got)
	}
	if !strings.Contains(got, "说\"我\"") {
		t.Fatalf("身份提示未要求用第一人称\"我\"自称: %q", got)
	}
}

func TestVoiceIdentityAideDivision(t *testing.T) {
	got := voiceIdentityPrompt(Settings{VoiceAssistantName: "小秘"})
	if !strings.Contains(got, "aide") {
		t.Fatalf("身份提示未提及 aide: %q", got)
	}
	// 小秘是桥梁，专业产出交给 aide
	if !strings.Contains(got, "桥梁") {
		t.Fatalf("身份提示未说明小秘是用户与 aide 之间的桥梁: %q", got)
	}
	if !strings.Contains(got, "产出交给 aide") {
		t.Fatalf("身份提示未说明专业产出交给 aide: %q", got)
	}
}

func TestVoiceIdentityCapabilities(t *testing.T) {
	got := voiceIdentityPrompt(Settings{VoiceAssistantName: "小秘"})
	for _, kw := range []string{"send", "ignore", "standby", "ask",
		"search_sessions", "get_session", "follow_session", "push_to_session",
		"总结", "讲解"} {
		if !strings.Contains(got, kw) {
			t.Fatalf("身份提示缺少能力关键词 %q: %q", kw, got)
		}
	}
}

func TestVoiceIdentityContinuity(t *testing.T) {
	got := voiceIdentityPrompt(Settings{VoiceAssistantName: "小秘"})
	for _, kw := range []string{"长期记忆", "对话历史", "加密"} {
		if !strings.Contains(got, kw) {
			t.Fatalf("身份提示缺少连续性关键词 %q: %q", kw, got)
		}
	}
}

func TestVoiceIdentityBoundaries(t *testing.T) {
	got := voiceIdentityPrompt(Settings{VoiceAssistantName: "小秘"})
	for _, kw := range []string{"不越权", "不编造", "隐私", "确认"} {
		if !strings.Contains(got, kw) {
			t.Fatalf("身份提示缺少边界准则关键词 %q: %q", kw, got)
		}
	}
}

func TestVoiceIdentityNameChange(t *testing.T) {
	// 默认名字
	if got := voiceIdentityPrompt(Settings{}); !strings.Contains(got, "「小秘」") {
		t.Fatalf("空名字应回退\"小秘\": %q", got)
	}
	// 改名后立即跟随
	cfg := Settings{VoiceAssistantName: "小蓝"}
	got := voiceIdentityPrompt(cfg)
	if !strings.Contains(got, "「小蓝」") || !strings.Contains(got, "我是小蓝") {
		t.Fatalf("改名后身份提示未跟随: %q", got)
	}
	if strings.Contains(got, "「小秘」") {
		t.Fatalf("改名后仍残留旧名字: %q", got)
	}
}

func TestIdentityNotAideOrGeneric(t *testing.T) {
	got := voiceIdentityPrompt(Settings{VoiceAssistantName: "小秘"})
	// 不得把自己肯定地描述成 aide
	if strings.Contains(got, "你是 aide") {
		t.Fatalf("身份提示把自己描述成了 aide: %q", got)
	}
	// 必须明确否认自己是 aide / 泛用助手
	if !strings.Contains(got, "你不是 aide") {
		t.Fatalf("身份提示未否认自己是 aide: %q", got)
	}
	if !strings.Contains(got, "不是一个泛用问答助手") {
		t.Fatalf("身份提示未否认自己是泛用助手: %q", got)
	}
}

// captureSystemViaMock 起一个 mock 上游，把请求里的 system 消息正文回吐，便于断言拼接。
func captureSystemViaMock(t *testing.T, reply string) (sys *string, base string) {
	t.Helper()
	var captured string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		var req struct {
			Messages []Message `json:"messages"`
		}
		_ = json.Unmarshal(b, &req)
		for _, m := range req.Messages {
			if m.Role == "system" {
				captured = m.Content
				break
			}
		}
		jsonOut(w, 200, map[string]any{
			"choices": []any{map[string]any{"message": Message{Role: "assistant", Content: reply}}},
		})
	}))
	t.Cleanup(srv.Close)
	return &captured, srv.URL
}

func TestAnalyzePromptIncludesIdentity(t *testing.T) {
	decision := `{"action":"send","summarized":"把按钮改成蓝色","ask":"","reason":"用户在对 aide 说话"}`
	sysPtr, base := captureSystemViaMock(t, decision)

	dir := t.TempDir()
	va := newVoiceAgent(dir)
	cfg := Settings{BaseURL: base, Model: "test", VoiceAssistantName: "小秘"}
	if _, err := va.analyze(context.Background(), cfg, "帮我把按钮改蓝", ""); err != nil {
		t.Fatalf("analyze: %v", err)
	}
	want := voiceIdentityPrompt(cfg)
	if !strings.HasPrefix(*sysPtr, want) {
		t.Fatalf("analyze 的 system prompt 未以身份核心开头\n--- got ---\n%s", *sysPtr)
	}
	if !strings.Contains(*sysPtr, "听懂这句口语") {
		t.Fatalf("analyze 的 system prompt 在身份核心后丢失了本环节任务指令: %q", *sysPtr)
	}
}

func TestNarratePromptIncludesIdentity(t *testing.T) {
	sysPtr, base := captureSystemViaMock(t, `["讲解一"]`)

	dir := t.TempDir()
	va := newVoiceAgent(dir)
	cfg := Settings{BaseURL: base, Model: "test", VoiceAssistantName: "小秘"}
	if _, err := va.narrate(context.Background(), cfg, []NarrationStepInput{
		{Kind: "speak", Text: "这是第一段讲解内容"},
	}); err != nil {
		t.Fatalf("narrate: %v", err)
	}
	want := voiceIdentityPrompt(cfg)
	if !strings.HasPrefix(*sysPtr, want) {
		t.Fatalf("narrate 的 system prompt 未以身份核心开头\n--- got ---\n%s", *sysPtr)
	}
	if !strings.Contains(*sysPtr, "边演示边口头讲解") {
		t.Fatalf("narrate 的 system prompt 在身份核心后丢失了本环节任务指令: %q", *sysPtr)
	}
}
