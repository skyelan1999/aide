package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestSurfaceReviewableIsLimitedToBoundedActions(t *testing.T) {
	for _, tc := range []struct {
		kind, tool, url, appRule string
		args                     map[string]any
		want                     bool
	}{
		{"browser-site", "", "https://example.com/docs", "", nil, true},
		{"browser-site", "", "http://127.0.0.1/admin", "", nil, false},
		{"browser-action", "browser_navigate", "", "", map[string]any{"url": "https://example.com/docs"}, true},
		{"browser-action", "browser_click", "", "", map[string]any{"selector": "button"}, false},
		{"computer-app", "", "", "bundle:com.apple.Safari", nil, true},
		{"computer-action", "computer_open_browser", "", "", map[string]any{"browser": "Safari"}, true},
		{"computer-action", "computer_open_browser", "", "", map[string]any{"browser": "Safari", "url": "https://example.com/"}, true},
		{"computer-action", "computer_key", "", "", map[string]any{"app": "Safari浏览器", "key": "CMD+L"}, true},
		{"computer-action", "computer_key", "", "", map[string]any{"app": "Safari浏览器", "key": "ENTER"}, true},
		{"computer-action", "computer_type", "", "", map[string]any{"app": "Safari浏览器"}, true},
		{"computer-action", "computer_click", "", "", map[string]any{"app": "Fixture", "x": -120, "y": 40}, true},
		{"computer-action", "computer_click", "", "", map[string]any{"app": "Fixture", "x": -2000000, "y": 40}, false},
	} {
		if got := surfaceReviewable(tc.kind, tc.tool, tc.args, tc.url, tc.appRule); got != tc.want {
			t.Errorf("%s/%s reviewable=%v, want %v", tc.kind, tc.tool, got, tc.want)
		}
	}
}

func TestAssistedBrowserSiteApprovalIsOneShot(t *testing.T) {
	a := testApp(t)
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		jsonOut(w, 200, map[string]any{"choices": []any{map[string]any{"message": Message{Role: "assistant", Content: `{"decision":"approve","risk":"low","withinWorkspace":false,"authorized":true,"reason":"用户明确要求读取该公开网址"}`}}}})
	}))
	defer model.Close()
	a.settings.BaseURL = model.URL
	a.settings.Model = "test"
	task := &Task{ID: "browser-auto", Prompt: "读取 https://example.org/ 的公开文档", Status: "running", AutoReview: true, WorkspaceID: a.wsID()}
	session := &Session{ID: "browser-auto-session", Runs: []*Task{task}, Messages: []Message{{Role: "user", Content: task.Prompt}}}
	a.sessions[session.ID] = session
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	approved, denial := a.browserReadApproval(ctx, task, map[string]any{"url": "https://example.org/"})
	if denial != "" || approved.Value(browserReadApprovalKey{}) != "https://example.org/" {
		t.Fatalf("browser read not approved: %q, %v", denial, approved.Value(browserReadApprovalKey{}))
	}
	if len(task.ApprovalReviews) != 1 || task.ApprovalReviews[0].Status != "approved" || task.ApprovalReviews[0].Kind != "browser-site" {
		t.Fatalf("missing one-shot audit: %+v", task.ApprovalReviews)
	}
	for _, plugin := range a.pluginRegistry.Plugins {
		if plugin.ID == "browser-control" && browserHostAllowed("example.org", plugin.Settings) {
			t.Fatal("assisted approval must not remember the domain")
		}
	}
	if strings.Contains(string(task.PendingQuestion), "example.org") {
		t.Fatal("approval card should have resolved")
	}
}
