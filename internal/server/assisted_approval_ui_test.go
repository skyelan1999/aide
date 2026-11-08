package server

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func uiTestControl(role, label string, focused bool, x, width float64) map[string]any {
	return map[string]any{"role": role, "label": label, "focused": focused, "rect": map[string]any{"x": x, "y": float64(0), "width": width, "height": float64(50)}}
}

func TestUIReviewRequiresActualHitOrFocus(t *testing.T) {
	field := uiTestControl("AXTextArea", "Query", true, 10, 100)
	button := uiTestControl("AXButton", "Translate", false, 120, 80)
	observation := map[string]any{"ok": true, "controls": []any{field, button}}
	for _, tc := range []struct {
		tool  string
		args  map[string]any
		label string
	}{
		{"computer_click", map[string]any{"x": 20, "y": 20}, "Query"},
		{"computer_click", map[string]any{"x": 130, "y": 20}, "Translate"},
		{"computer_click", map[string]any{"x": 110, "y": 20}, ""},
		{"computer_click", map[string]any{"x": 999, "y": 20}, ""},
		{"computer_type", map[string]any{"text": "hello"}, "Query"},
	} {
		control := reviewUIControl(tc.tool, tc.args, observation)
		label, _ := control["label"].(string)
		if label != tc.label {
			t.Fatalf("%s %v target %q want %q", tc.tool, tc.args, label, tc.label)
		}
	}
	if reviewUIControl("computer_click", map[string]any{"x": 20, "y": 20}, map[string]any{"ok": false, "controls": []any{field}}) != nil {
		t.Fatal("failed inspection was trusted")
	}
	if reviewUIControl("computer_type", nil, map[string]any{"ok": true, "controls": []any{button}}) != nil {
		t.Fatal("guessed focus")
	}
}

func TestUIReviewRejectsConsequentialAndNoneditableTargets(t *testing.T) {
	for _, label := range []string{"Delete account", "Pay now", "发送消息", "授权访问", "清空回收站", "Login", "Password"} {
		input := &surfaceApprovalInput{Tool: "computer_click", Target: computerControlTarget{Name: "App", PID: 12, Rule: "bundle:app"}, Control: uiTestControl("AXButton", label, false, 0, 100)}
		if reviewUIInput(input) {
			t.Fatalf("consequential target reviewable: %s", label)
		}
	}
	for _, role := range []string{"AXButton", "AXOutline", "AXSecureTextField"} {
		input := &surfaceApprovalInput{Tool: "computer_type", Args: map[string]any{"text": "hello"}, Target: computerControlTarget{Name: "App", PID: 12, Rule: "bundle:app"}, Control: uiTestControl(role, "", true, 0, 100)}
		if reviewUIInput(input) {
			t.Fatalf("type into %s reviewable", role)
		}
		input.Tool = "computer_key"
		input.Args = map[string]any{"key": "CMD+A"}
		if reviewUIInput(input) {
			t.Fatalf("select all in noneditable %s reviewable", role)
		}
	}
}

func TestUIReviewFingerprintBindsContentTargetAndControl(t *testing.T) {
	base := &surfaceApprovalInput{Tool: "computer_type", Args: map[string]any{"text": "hello"}, Target: computerControlTarget{Name: "App", PID: 12, Rule: "bundle:app"}, Control: uiTestControl("AXTextArea", "Query", true, 0, 100)}
	hash := surfaceInputFingerprint(base)
	base.Args["text"] = "another"
	if hash == surfaceInputFingerprint(base) {
		t.Fatal("text not bound")
	}
	base.Args["text"] = "hello"
	base.Target.PID++
	if hash == surfaceInputFingerprint(base) {
		t.Fatal("PID not bound")
	}
	base.Target.PID--
	base.Control["label"] = "Password"
	if hash == surfaceInputFingerprint(base) {
		t.Fatal("control not bound")
	}
}

func TestAssistedUIExactTextIsReviewedButNotPersistedInCard(t *testing.T) {
	const exact = "ordinary translation sample"
	a := testApp(t)
	var calls atomic.Int32
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		var request struct {
			Messages []Message
			Tools    []any
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
		}
		if len(request.Tools) != 0 {
			t.Error("reviewer received tools")
		}
		var payload struct {
			ExactInput surfaceApprovalInput `json:"exactInput"`
		}
		if len(request.Messages) < 2 || json.Unmarshal([]byte(request.Messages[1].Content), &payload) != nil || payload.ExactInput.Args["text"] != exact {
			t.Error("reviewer did not receive exact input")
		}
		jsonOut(w, 200, map[string]any{"choices": []any{map[string]any{"message": Message{Role: "assistant", Content: `{"decision":"approve","risk":"low","authorized":true,"reason":"ordinary translation sample is task input"}`}}}})
	}))
	defer model.Close()
	a.settings.BaseURL = model.URL
	a.settings.Model = "test"
	task := &Task{ID: "ui-auto", Prompt: "Translate the sample using TextEdit", Status: "running", AutoReview: true, WorkspaceID: a.wsID()}
	session := &Session{ID: "ui-auto-session", Runs: []*Task{task}, Messages: []Message{{Role: "user", Content: task.Prompt}}}
	a.sessions[session.ID] = session
	input := &surfaceApprovalInput{Tool: "computer_type", Args: map[string]any{"text": exact}, Target: computerControlTarget{Name: "TextEdit", PID: 12, Rule: "bundle:com.apple.TextEdit"}, Control: uiTestControl("AXTextArea", "Query", true, 0, 100)}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	ctx = context.WithValue(ctx, surfaceApprovalInputKey{}, input)
	q := json.RawMessage(`{"question":"Type into TextEdit","type":"confirm","approvalKind":"computer-action","tool":"computer_type","actionArgs":{"app":"TextEdit","characterCount":27}}`)
	if got := a.awaitUserAnswer(ctx, task, q); got != "确认" {
		t.Fatalf("not approved: %q", got)
	}
	if calls.Load() != 1 || len(task.ApprovalReviews) != 1 || task.ApprovalReviews[0].Status != "approved" {
		t.Fatalf("missing review: %+v", task.ApprovalReviews)
	}
	encoded, _ := json.Marshal(task)
	if strings.Contains(string(encoded), exact) || task.surfaceApprovalInput != nil {
		t.Fatal("memory-only text leaked into durable task state")
	}
}

func TestAssistedUIWithoutEvidenceOrInManualModeNeverCallsReviewer(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		a := testApp(t)
		var calls atomic.Int32
		model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); t.Error("review called without evidence") }))
		a.settings.BaseURL = model.URL
		a.settings.Model = "test"
		task := &Task{ID: "ui-no-evidence", Prompt: "Translate using TextEdit", Status: "running", AutoReview: enabled, WorkspaceID: a.wsID()}
		session := &Session{ID: "ui-no-evidence-session", Runs: []*Task{task}, Messages: []Message{{Role: "user", Content: task.Prompt}}}
		a.sessions[session.ID] = session
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		answer := a.awaitUserAnswer(ctx, task, json.RawMessage(`{"type":"confirm","approvalKind":"computer-action","tool":"computer_click","actionArgs":{"app":"TextEdit","x":20,"y":20}}`))
		cancel()
		model.Close()
		if answer == "确认" || calls.Load() != 0 {
			t.Fatal("manual/no-evidence operation automatically approved")
		}
	}
}

func TestAssistedCanvasClickReviewerReceivesFreshRaster(t *testing.T) {
	a := testApp(t)
	input := &surfaceApprovalInput{Tool: "computer_click", Args: map[string]any{"x": 5, "y": 5}, Target: computerControlTarget{Name: "Fixture", Rule: "bundle:fixture", PID: 123}}
	im := image.NewGray(image.Rect(0, 0, 120, 80))
	input = surfaceVisualInput(input, snapshotFixture(t, im, map[string]any{"x": 0, "y": 0, "width": 120, "height": 80}))
	if input == nil {
		t.Fatal("fixture image missing")
	}
	var calls atomic.Int32
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		data, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
		}
		var request struct {
			MaxTokens int `json:"max_tokens"`
			Tools     []any
		}
		if json.Unmarshal(data, &request) != nil || request.MaxTokens != 1024 || len(request.Tools) != 0 {
			t.Error("incorrect visual review budget/tools")
		}
		if !bytes.Contains(data, []byte(input.Image.DataURL)) || !bytes.Contains(data, []byte("ImageX")) {
			t.Error("visual reviewer did not receive image and mapped click")
		}
		jsonOut(w, 200, map[string]any{"choices": []any{map[string]any{"message": Message{Role: "assistant", Content: `{"decision":"approve","risk":"low","authorized":true,"reason":"ordinary visible task button"}`}}}})
	}))
	defer model.Close()
	a.settings.BaseURL = model.URL
	a.settings.Model = "test"
	task := &Task{ID: "canvas-review", Prompt: "Click the visible ordinary toolbar button in Fixture", Status: "running", AutoReview: true, WorkspaceID: a.wsID()}
	session := &Session{ID: "canvas-review-session", Runs: []*Task{task}, Messages: []Message{{Role: "user", Content: task.Prompt}}}
	a.sessions[session.ID] = session
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	ctx = context.WithValue(ctx, surfaceApprovalInputKey{}, input)
	answer := a.awaitUserAnswer(ctx, task, json.RawMessage(`{"type":"confirm","approvalKind":"computer-action","tool":"computer_click","actionArgs":{"app":"Fixture","x":5,"y":5}}`))
	if answer != "确认" || calls.Load() != 1 {
		t.Fatalf("visual action not reviewed: %q", answer)
	}
	stored, _ := json.Marshal(task)
	if bytes.Contains(stored, []byte(input.Image.DataURL)) || task.surfaceApprovalInput != nil {
		t.Fatal("review screenshot leaked into durable task")
	}
}
