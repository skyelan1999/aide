package server

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestBrowserReadTargetAndHostRules(t *testing.T) {
	for _, raw := range []string{"", "file:///tmp/test", "https://user:pass@example.com/", "https://*.example.com/"} {
		if _, _, err := browserReadTarget(raw); err == nil {
			t.Errorf("accepted invalid approval target %q", raw)
		}
	}
	for _, host := range []string{"localhost", "sub.localhost", "127.0.0.1", "10.0.0.1", "169.254.169.254"} {
		if browserHostMayBeApproved(host) {
			t.Errorf("private host could request approval: %s", host)
		}
	}
	urlText, host, err := browserReadTarget("https://Sub.Example.com:443/path?q=1")
	if err != nil || urlText != "https://Sub.Example.com:443/path?q=1" || host != "sub.example.com" {
		t.Fatalf("target: %q %q %v", urlText, host, err)
	}
	settings := map[string]any{"allowedHosts": []any{"*.example.com"}}
	if !browserHostAllowed(host, settings) || browserHostAllowed("example.com", settings) || browserHostAllowed("notexample.com", settings) {
		t.Fatal("wildcard must match only subdomains")
	}
	copySettings := browserHostsWith(settings, "another.example.net")
	if browserHostAllowed("another.example.net", settings) || !browserHostAllowed("another.example.net", copySettings) {
		t.Fatal("single-call host extension mutated persistent settings")
	}
}

func TestBrowserReadUnknownSiteApproval(t *testing.T) {
	for _, tc := range []struct {
		name, answer     string
		read, remembered bool
	}{
		{"allow-once", "确认", true, false},
		{"remember", "记住并允许", true, true},
		{"deny", "拒绝", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := testApp(t)
			fixture := `module.exports={name:"browser-control",apply(ctx){ctx.tool({name:"browser_read",handler:()=>({text:"READ:"+JSON.stringify(ctx.settings.allowedHosts||[])})});}};`
			pluginDir := filepath.Join(a.pluginsPath, "browser-control")
			if err := os.MkdirAll(pluginDir, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(pluginDir, "manifest.json"), []byte(`{"id":"browser-control","name":"Browser fixture","version":"1.0.0","main":"index.js"}`), 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(pluginDir, "index.js"), []byte(fixture), 0600); err != nil {
				t.Fatal(err)
			}
			a.mu.Lock()
			a.pluginRegistry.Plugins = append(a.pluginRegistry.Plugins, PluginManifest{ID: "browser-control", Name: "Browser fixture", Main: "index.js", Version: "1.0.0", Enabled: true, Settings: map[string]any{"allowedHosts": []any{"example.com"}}})
			if err := a.savePluginRegistry(); err != nil {
				a.mu.Unlock()
				t.Fatal(err)
			}
			a.mu.Unlock()
			a.runPluginHost(context.Background())
			task := &Task{ID: "browser-task", Mode: "chat", Status: "running", WorkspaceMode: "workspace-write"}
			session := &Session{ID: "browser-session", Runs: []*Task{task}}
			a.mu.Lock()
			a.sessions[session.ID] = session
			a.mu.Unlock()
			result := make(chan string, 1)
			go func() {
				result <- a.executeToolCall(context.Background(), readCall("browser_read", `{"url":"https://github.com/"}`), task, nil)
			}()
			deadline := time.Now().Add(3 * time.Second)
			for {
				a.mu.Lock()
				status := task.Status
				question := append(json.RawMessage(nil), task.PendingQuestion...)
				a.mu.Unlock()
				if status == "awaiting_clarification" {
					if !strings.Contains(string(question), `"approvalKind":"browser-site"`) || !strings.Contains(string(question), `https://github.com/`) {
						t.Fatalf("missing exact URL in approval card: %s", question)
					}
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("browser read did not pause for approval")
				}
				time.Sleep(5 * time.Millisecond)
			}
			requireStatus(t, request(a, "POST", "/api/sessions/"+session.ID+"/runs/"+task.ID+"/answer", boundQuestionAnswerForTest(t, a, task, tc.answer)), 200)
			select {
			case got := <-result:
				if strings.Contains(got, "READ:") != tc.read {
					t.Fatalf("read mismatch: %s", got)
				}
				if tc.read && !strings.Contains(got, "github.com") {
					t.Fatalf("approved host missing from plugin call: %s", got)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("approval did not resolve")
			}
			a.mu.Lock()
			remembered := false
			for _, p := range a.pluginRegistry.Plugins {
				if p.ID == "browser-control" {
					remembered = browserHostAllowed("github.com", p.Settings)
				}
			}
			a.mu.Unlock()
			if remembered != tc.remembered {
				t.Fatalf("remembered=%t, want %t", remembered, tc.remembered)
			}
			stored, err := os.ReadFile(filepath.Join(a.pluginsPath, "registry.json"))
			if err != nil {
				t.Fatal(err)
			}
			var persisted pluginRegistry
			if err := json.Unmarshal(stored, &persisted); err != nil {
				t.Fatal(err)
			}
			persistedHost := false
			for _, p := range persisted.Plugins {
				if p.ID == "browser-control" {
					persistedHost = browserHostAllowed("github.com", p.Settings)
				}
			}
			if persistedHost != tc.remembered {
				t.Fatalf("persisted host=%t, want %t", persistedHost, tc.remembered)
			}
		})
	}
}
