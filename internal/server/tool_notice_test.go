package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestToolNoticeUsesOnlyUnquotedTrailingWorkbenchLabel(t *testing.T) {
	const notice = "工具调用中：list_files, read_file"
	for _, sample := range []string{notice, "进度已保存\n\n" + notice + "\r\n"} {
		prose, names := splitUnissuedToolNotice(sample)
		if len(names) != 2 || names[0] != "list_files" || names[1] != "read_file" || strings.Contains(prose, notice) {
			t.Errorf("did not split %q: %q %v", sample, prose, names)
		}
	}
	for _, sample := range []string{"文档引用：" + notice, "> " + notice, "    " + notice, "```text\n" + notice, "~~~\n" + notice + "\n~~~", notice + "\n已完成", notice + "。"} {
		prose, names := splitUnissuedToolNotice(sample)
		if len(names) != 0 || prose != sample {
			t.Errorf("modified ordinary prose %q", sample)
		}
	}
	tools := []any{updatePlanTool}
	const plan = "工具调用中：update_plan"
	for _, tc := range []struct{ prompt, text, finish string }{
		{"请原样输出工具调用的状态", plan, "stop"},
		{"继续", notice, "stop"}, // Names are not offered on this request.
		{"继续", plan, "length"}, // The existing continuation path owns truncation.
	} {
		if out, missing := toolResponseProse(tc.prompt, tc.text, tc.finish, tools, nil, nil); missing || out != tc.text {
			t.Fatalf("incorrect repair for %+v: %q", tc, out)
		}
	}
	if _, missing := toolResponseProse("继续", "模型响应", "tool_calls", tools, nil, nil); !missing {
		t.Fatal("provider declared tool_calls without a structured call")
	}
	if out, missing := toolResponseProse("继续", plan, "tool_calls", tools, []ToolCall{readCall("update_plan", "{}")}, nil); out != plan || missing {
		t.Fatal("structured calls must use the ordinary execution path")
	}
}

func TestToolLoopRepairsUnissuedToolNotice(t *testing.T) {
	const notice = "工具调用中：list_files, list_files"
	for _, name := range []string{"valid_call_after_correction", "duplicate_notice_pauses", "empty_sse_declared_call_pauses"} {
		t.Run(name, func(t *testing.T) {
			repeated := name != "valid_call_after_correction"
			emptySSE := name == "empty_sse_declared_call_pauses"
			a := testApp(t)
			var requests atomic.Int32
			provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var request struct{ Messages []Message }
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
					t.Error(err)
				}
				count := requests.Add(1)
				msg := Message{Role: "assistant"}
				finish := "stop"
				switch count {
				case 1:
					if emptySSE {
						sseRaw(w, w.(http.Flusher), `{"choices":[{"delta":{},"finish_reason":"tool_calls"}]}`)
						return
					}
					msg.Content = "第一段\n\n" + notice
				case 2:
					last := request.Messages[len(request.Messages)-1]
					if last.Role != "user" || last.Content != unissuedToolCorrection {
						t.Error("missing tool protocol correction")
					}
					for _, item := range request.Messages {
						if strings.Contains(item.Content, notice) {
							t.Error("copied progress label back into the request")
						}
					}
					if emptySSE {
						sseRaw(w, w.(http.Flusher), `{"choices":[{"delta":{},"finish_reason":"tool_calls"}]}`)
						return
					}
					if repeated {
						msg.Content = notice
					} else {
						finish = "tool_calls"
						msg.ToolCalls = []ToolCall{readCall("list_files", `{"path":"."}`)}
					}
				default:
					hasReceipt := false
					for _, item := range request.Messages {
						if item.Role == "tool" && item.ToolCallID != "" {
							hasReceipt = true
						}
					}
					if !hasReceipt {
						t.Error("missing result of actual tool call")
					}
					msg.Content = "已取得目录结果。"
				}
				jsonOut(w, 200, map[string]any{"choices": []any{map[string]any{"message": msg, "finish_reason": finish}}})
			}))
			defer provider.Close()
			task := &Task{ID: newID(), Mode: "chat", Prompt: "读取工作目录", Status: "running", Steps: []Step{{Name: "chat"}}, Steer: make(chan string, 1)}
			registerHarnessFixtureTask(t, a, task)
			out, _, err := a.toolLoop(context.Background(), Settings{BaseURL: provider.URL, Model: "fixture"}, []Message{{Role: "user", Content: task.Prompt}}, ProfileParams{}, builtinTools, task, nil, 0)
			count := requests.Load()
			if repeated {
				if !errors.Is(err, errDeliveryIncomplete) || count != 2 || len(task.ToolUses) != 0 {
					t.Fatalf("repeated notice: %q err=%v requests=%d tools=%d", out, err, count, len(task.ToolUses))
				}
			} else if err != nil || count < 3 || out != "已取得目录结果。" || len(task.ToolUses) != 1 {
				t.Fatalf("real call: %q err=%v requests=%d tools=%d", out, err, count, len(task.ToolUses))
			}
			wantFinish := "stop"
			if emptySSE {
				wantFinish = "tool_calls"
			}
			var chatSnapshot *RequestSnapshot
			for i := range task.RequestSnapshots {
				if task.RequestSnapshots[i].Purpose == "chat#0" {
					chatSnapshot = &task.RequestSnapshots[i]
					break
				}
			}
			if chatSnapshot == nil || chatSnapshot.Outcome != "missing_tool_call" || chatSnapshot.FailureCode != "MISSING_TOOL_CALL" || chatSnapshot.FinishReason != wantFinish {
				t.Fatalf("request snapshot lost the no-call outcome or upstream finish reason: %+v", task.RequestSnapshots)
			}
			segments := task.Steps[0].OutputSegments
			if !emptySSE && (len(segments) == 0 || segments[0].Content != "第一段") {
				t.Fatalf("saved progress = %+v", segments)
			}
			for _, segment := range segments {
				if strings.Contains(segment.Content, notice) {
					t.Fatal("simulated tool activity persisted as output")
				}
			}
		})
	}
}
