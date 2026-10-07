package server

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// The model-supplied approved field must never bypass Aide's confirmation.
// These use real Node plugin handlers and the workflow answer API, without
// operating the user's Safari or desktop.
func TestControlWorkflowConfirmationAndRevocation(t *testing.T) {
	for _, tool := range []string{"browser_navigate", "browser_click", "browser_fill", "computer_click", "computer_type", "computer_key"} {
		for _, action := range []string{"confirm", "decline", "cancel", "revoke"} {
			t.Run(tool+"/"+action, func(t *testing.T) {
				a := testApp(t)
				owner := "browser-control"
				if strings.HasPrefix(tool, "computer_") {
					owner = "computer-control"
				}
				code := `module.exports={name:` + `"` + owner + `"` + `,apply(ctx){ctx.tool({name:"` + tool + `",handler:args=>({text:args.approved===true?"EXECUTED":"UNAPPROVED"})});}};`
				requireStatus(t, request(a, "POST", "/api/plugins", map[string]any{"id": owner, "name": owner, "code": code}), 201)
				task := &Task{ID: "control-task", Mode: "chat", Status: "running", WorkspaceMode: "workspace-write"}
				session := &Session{ID: "control-session", Runs: []*Task{task}}
				a.mu.Lock()
				a.sessions[session.ID] = session
				a.mu.Unlock()
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				result := make(chan string, 1)
				go func() {
					result <- a.executeToolCall(ctx, readCall(tool, `{"url":"https://example.com","selector":"#test","text":"test-input","x":1,"y":1,"key":"ENTER","approved":true}`), task, nil)
				}()
				deadline := time.Now().Add(3 * time.Second)
				for {
					a.mu.Lock()
					status := task.Status
					question := append(json.RawMessage(nil), task.PendingQuestion...)
					a.mu.Unlock()
					if status == "awaiting_clarification" {
						if !strings.Contains(string(question), `"type":"confirm"`) {
							t.Fatalf("missing visible confirmation: %s", question)
						}
						break
					}
					if time.Now().After(deadline) {
						t.Fatal("model approval bypassed confirmation or workflow did not pause")
					}
					time.Sleep(5 * time.Millisecond)
				}
				if action == "cancel" {
					cancel()
				} else {
					if action == "revoke" {
						requireStatus(t, request(a, "PUT", "/api/plugins/"+owner, map[string]any{"enabled": false}), 200)
					}
					answer := "确认"
					if action == "decline" {
						answer = "取消"
					}
					requireStatus(t, request(a, "POST", "/api/sessions/"+session.ID+"/runs/"+task.ID+"/answer", map[string]string{"answer": answer}), 200)
				}
				select {
				case got := <-result:
					if (got == "EXECUTED") != (action == "confirm") {
						t.Fatalf("%s result: %s", action, got)
					}
				case <-time.After(5 * time.Second):
					t.Fatal("control operation failed to finish")
				}
			})
		}
	}
}

func TestControlPluginCanceledContext(t *testing.T) {
	a := testApp(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := a.callPluginToolContext(ctx, "browser-control", "browser_status", nil); err != context.Canceled {
		t.Fatalf("canceled call reached plugin: %v", err)
	}
}

func TestControlStatusResultsReachModel(t *testing.T) {
	got, _ := normalizePluginResult(map[string]any{"ok": true, "service": "aide-safari-bridge", "session": false})
	if !strings.Contains(got, "aide-safari-bridge") || !strings.Contains(got, `"session":false`) {
		t.Fatalf("structured status discarded: %s", got)
	}
	got, _ = normalizePluginResult(map[string]any{"app": "Safari", "imageBase64": "private-image-bytes"})
	if strings.Contains(got, "private-image-bytes") || !strings.Contains(got, "imageNotice") {
		t.Fatalf("screenshot must not masquerade as a visible model image: %s", got)
	}
}
