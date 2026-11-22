package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMixSoulApprovalToolsAcrossInputs(t *testing.T) {
	for _, source := range []string{"text", "call"} {
		for _, disabled := range []bool{false, true} {
			name := source
			if disabled {
				name += "-disabled"
			}
			t.Run(name, func(t *testing.T) {
				t.Setenv("AIDE_ASSISTANT_ENGINE", "mixsoul")
				a := testApp(t)
				unlockAssistantSessionForTest(t, a)
				run := &Task{ID: "fixture-approval-run", Status: "awaiting_approval", WorkspaceID: a.wsID(), ApprovalState: "manual", Files: []Change{{Path: "fixture.txt", Content: "PRIVATE-CONTENT"}}}
				a.sessions["fixture-approval-session"] = &Session{ID: "fixture-approval-session", Number: 188, Runs: []*Task{run}}
				if disabled {
					a.settings.DisabledTools = []string{"aide_component_status", "control_session_run"}
				}
				queries := 0
				model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					var input struct {
						Messages []Message `json:"messages"`
						Tools    []struct {
							Function struct {
								Name string `json:"name"`
							} `json:"function"`
						} `json:"tools"`
					}
					if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
						t.Error(err)
						return
					}
					reply := "fixture-query-complete"
					dialogue, learning := false, false
					for _, m := range input.Messages {
						if m.Role == "system" && strings.Contains(m.Content, "从用户原话提取") {
							reply = `{"items":[]}`
							learning = true
						}
						if m.Role == "user" && strings.Contains(m.Content, "只读查看待审批") {
							dialogue = true
						}
					}
					if dialogue && !learning {
						names := map[string]bool{}
						for _, tool := range input.Tools {
							names[tool.Function.Name] = true
						}
						if names["aide_component_status"] == disabled || names["control_session_run"] || names["run_shell"] || names["write_file"] {
							t.Error("incorrect permission-filtered tools", names)
						}
						gotResult := false
						for _, m := range input.Messages {
							if m.Role == "tool" {
								gotResult = true
								if !strings.Contains(m.Content, `"sessionNumber":188`) || strings.Contains(m.Content, "PRIVATE-CONTENT") {
									t.Error("incorrect approval query result")
								}
							}
						}
						if !disabled && !gotResult {
							queries++
							jsonOut(w, 200, map[string]any{"choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "tool_calls": []any{map[string]any{"id": "approval-query", "type": "function", "function": map[string]any{"name": "aide_component_status", "arguments": `{"component":"approvals"}`}}}}}}})
							return
						}
					}
					jsonOut(w, 200, map[string]any{"choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": reply}}}})
				}))
				defer model.Close()
				before := len(a.sessions)
				decision, err := a.runAssistantAgenticLoop(context.Background(), Settings{BaseURL: model.URL, Model: "fixture-model"}, "只读查看待审批数量，不批准、不交办。", "", source)
				expectedReply := "当前有 1 项待人工审批。"
				if disabled {
					expectedReply = "fixture-query-complete"
				}
				if err != nil || decision.Reply != expectedReply {
					t.Fatalf("reply=%q err=%v", decision.Reply, err)
				}
				expected := 1
				if disabled {
					expected = 0
				}
				if queries != expected || len(decision.ToolsUsed) != expected {
					t.Fatalf("queries=%d tools=%v", queries, decision.ToolsUsed)
				}
				if len(a.sessions) != before || run.Status != "awaiting_approval" || run.ApprovalState != "manual" || len(run.ApprovalReviews) != 0 {
					t.Fatal("read-only query mutated work or approvals")
				}
			})
		}
	}
}

func TestNativeApprovalSpokenSessionReferences(t *testing.T) {
	for _, tc := range []struct {
		heard  string
		recent bool
		want   string
	}{
		{"查看一百八十八号会话的审批", false, "#188"},
		{"查看会话一百八十八的审批", false, "#188"},
		{"再试一次，应该有一个一八八的绘画，查看审批", false, "#188"},
		{"再试一次，应该有一个一八八的绘画", true, "#188"},
		{"不是幺幺八八，是一百八十八。", true, "#188"},
		{"一百八十八", true, "#188"},
		{"一百八十八", false, ""},
		{"不是幺幺八八，是一百八十八。", false, ""},
		{"查看 #12 和 #13 的审批", true, ""},
		{"不用审批会话188", true, ""},
		{"暂停会话 #188", true, ""},
		{"停止会话 #188", true, ""},
		{"批准本次审批", true, ""},
		{"会话一百八十八，批准所有审批", false, "#188"},
	} {
		if got := nativeApprovalTargetWithContext(tc.heard, tc.recent); got != tc.want {
			t.Errorf("%q recent=%v got=%q want=%q", tc.heard, tc.recent, got, tc.want)
		}
	}
	for _, heard := range []string{"一百八十八", "不是幺幺八八，是一百八十八。", "会话一百八十八，批准所有审批"} {
		if nativeApprovalDecision(heard) != "" {
			t.Fatal("lookup granted approval authority")
		}
	}
}

func TestAssistantModelMetadataMatchesExecution(t *testing.T) {
	a := testApp(t)
	selectionModels(a, "https://a.invalid/v1", "https://b.invalid/v1")
	if err := a.vault.Put(modelProviderVaultID("b", "fixture"), VaultTypeModelAPIKey, "fixture", []byte("private-fixture-key"), ""); err != nil {
		t.Fatal(err)
	}
	a.settings.XiaomiModelSource = "configured"
	a.settings.XiaomiModel = "model-b"
	w := request(a, "GET", "/api/xiaomi/model", nil)
	requireStatus(t, w, 200)
	var out map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	if out["effectiveModel"] != "model-b" || out["configured"] != true || strings.Contains(w.Body.String(), "private-fixture-key") {
		t.Fatal("incorrect or sensitive model metadata")
	}
	a.settings.XiaomiModel = "removed-model"
	w = request(a, "GET", "/api/xiaomi/model", nil)
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	if out["effectiveModel"] != "removed-model" || out["configured"] != false {
		t.Fatal("missing assistant model silently displayed global default")
	}
}

func TestAssistantApprovalQueryRequiresFreshReceiptAndExactCount(t *testing.T) {
	for _, engine := range []string{"builtin", "mixsoul"} {
		for _, respondWithTool := range []bool{false, true} {
			name := engine
			if respondWithTool {
				name += "/repair-and-count"
			} else {
				name += "/no-receipt"
			}
			t.Run(name, func(t *testing.T) {
				t.Setenv("AIDE_ASSISTANT_ENGINE", engine)
				a := testApp(t)
				unlockAssistantSessionForTest(t, a)
				pending := &Task{ID: "owned-query-run", Status: "awaiting_approval", WorkspaceID: a.wsID(), Files: []Change{{Path: "owned.txt", Content: "pending"}}}
				a.sessions["owned-query-session"] = &Session{ID: "owned-query-session", Number: 188, Runs: []*Task{pending}}
				attempts := 0
				model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					var in struct {
						Messages []Message `json:"messages"`
					}
					_ = json.NewDecoder(r.Body).Decode(&in)
					attempts++
					for _, m := range in.Messages {
						if m.Role == "tool" {
							if !strings.Contains(m.Content, `"total":1`) {
								t.Error("missing trusted count")
							}
							jsonOut(w, 200, map[string]any{"choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": "有40项待审批。"}}}})
							return
						}
					}
					if respondWithTool {
						jsonOut(w, 200, map[string]any{"choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "tool_calls": []ToolCall{asstToolCall("aide_component_status", `{"component":"approvals"}`)}}}}})
						return
					}
					jsonOut(w, 200, map[string]any{"choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": "我没有审批查询工具，已经转交了任务。"}}}})
				}))
				defer model.Close()
				a.settings.BaseURL = model.URL
				a.settings.Model = "fixture"
				before := len(a.sessions)
				id := assistantSessionID(t, a)
				w := request(a, "POST", "/api/sessions/"+id+"/assistant-message", map[string]string{"text": "不要保存本轮到长期记忆。只读查询当前待审批总数，不批准或派发任务。"})
				requireStatus(t, w, 200)
				var got struct {
					Reply     string
					ToolsUsed []string
				}
				_ = json.Unmarshal(w.Body.Bytes(), &got)
				if got.Reply != "当前有 1 项待人工审批。" || len(got.ToolsUsed) != 1 {
					t.Fatal("ungrounded approval reply", got)
				}
				if len(a.sessions) != before || pending.Status != "awaiting_approval" || len(pending.ApprovalReviews) != 0 {
					t.Fatal("query changed work or approvals")
				}
			})
		}
	}
}

func TestAssistantApprovalQueryRejectsUnexpectedDispatch(t *testing.T) {
	a := testApp(t)
	before := len(a.sessions)
	decision := assistantDecision{Action: "chat"}
	result := a.execAssistantTool(asstToolCall("dispatch_to_aide", `{"summary":"run work"}`), &decision, "只读查询当前审批状态，不批准或派发。")
	if decision.Action != "chat" || len(a.sessions) != before || !strings.Contains(result, "只读审批查询") {
		t.Fatal("query accepted an unexpected dispatch")
	}
}

func TestAssistantApprovalReadOnlyRecoveryPreservesGuards(t *testing.T) {
	for _, kind := range []string{"disabled", "cancelled", "not-query"} {
		t.Run(kind, func(t *testing.T) {
			a := testApp(t)
			ctx := context.Background()
			heard := "查询待审批数量"
			dec := assistantDecision{Action: "chat"}
			before := len(a.sessions)
			if kind == "disabled" {
				a.settings.DisabledTools = []string{"aide_component_status"}
			}
			if kind == "cancelled" {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			if kind == "not-query" {
				heard = "不用查询审批"
			}
			_, _, err := a.recoverAssistantApprovalQuery(ctx, heard, &dec)
			if err == nil || dec.approvalTotal != nil || dec.approvalQueryRecovery || len(a.sessions) != before {
				t.Fatal("recovery bypassed a guard")
			}
		})
	}
}
