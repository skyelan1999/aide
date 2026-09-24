package server

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Opt-in: requires scripts/fixtures/sources real protocol servers, not mocks.
func TestSourcesLiveProtocols(t *testing.T) {
	host := os.Getenv("AIDE_SOURCE_FIXTURE_HOST")
	if host == "" {
		t.Skip("Docker source fixtures not started")
	}
	for _, typ := range []string{"local", "skill", "link", "ftp", "ftps", "sftp", "smb"} {
		t.Run(typ, func(t *testing.T) {
			a := testApp(t)
			config := map[string]any{}
			switch typ {
			case "local", "skill":
				config["path"] = "refs"
				os.MkdirAll(filepath.Join(a.workPath, "refs", "nested"), 0755)
				os.WriteFile(filepath.Join(a.workPath, "refs", "nested", "design notes.md"), []byte("real protocol reference"), 0600)
			case "link":
				config["url"] = "http://" + host + ":8080/nested/design%20notes.md"
			case "ftp", "ftps":
				config["url"] = "ftp://" + host
				config["username"] = "fixture"
			case "smb":
				config["url"] = "smb://" + host + "/refs/nested/design%20notes.md"
				config["username"] = "fixture"
			case "sftp":
				config = map[string]any{"host": host, "port": 22, "username": "fixture", "auth": "password", "path": "/srv/refs"}
			}
			id := "live-" + typ
			requireStatus(t, request(a, "PUT", "/api/sources", map[string]any{"sources": []any{sourceBody(id, typ, typ, config, (typ == "sftp" || typ == "local" || typ == "skill"))}, "secrets": map[string]any{id: map[string]any{"password": "fixture-pass"}}}), 200)
			w := request(a, "GET", "/api/files?source="+id+"&path=.", nil)
			requireStatus(t, w, 200)
			p := "resource.txt"
			if typ == "ftp" || typ == "ftps" || typ == "sftp" || typ == "local" || typ == "skill" {
				if !strings.Contains(w.Body.String(), "nested") {
					t.Fatal("directory missing:", w.Body.String())
				}
				w = request(a, "GET", "/api/files?source="+id+"&path=nested", nil)
				requireStatus(t, w, 200)
				if !strings.Contains(w.Body.String(), "nested/design notes.md") {
					t.Fatal("nested path missing:", w.Body.String())
				}
				p = "nested/design%20notes.md"
			}
			w = request(a, "GET", "/api/file?source="+id+"&path="+p, nil)
			requireStatus(t, w, 200)
			if !strings.Contains(w.Body.String(), "real protocol reference") {
				t.Fatal("file body mismatch:", w.Body.String())
			}
			if typ == "sftp" || typ == "local" || typ == "skill" {
				requireStatus(t, request(a, "PUT", "/api/file", map[string]string{"source": id, "path": "written.md", "content": "written through aide"}), 200)
				w = request(a, "GET", "/api/file?source="+id+"&path=written.md", nil)
				requireStatus(t, w, 200)
				if !strings.Contains(w.Body.String(), "written through aide") {
					t.Fatal("write roundtrip failed")
				}
			} else {
				requireStatus(t, request(a, "PUT", "/api/file", map[string]string{"source": id, "path": "write.md", "content": "must reject"}), 403)
			}
			var call ToolCall
			raw, _ := json.Marshal(map[string]any{"id": "source-read", "type": "function", "function": map[string]string{"name": "read_file", "arguments": `{"source":"` + id + `","path":"` + strings.ReplaceAll(p, "%20", " ") + `"}`}})
			json.Unmarshal(raw, &call)
			result := a.executeToolCall(context.Background(), call, &Task{}, map[string]Change{})
			if !strings.Contains(result, "real protocol reference") {
				t.Fatal("AI tool did not receive actual source body:", result)
			}
			provider := newToolProvider(t, []func() (string, []ToolCall){
				func() (string, []ToolCall) { return "", []ToolCall{call} },
				func() (string, []ToolCall) { return "Reference received", nil },
			})
			defer provider.Close()
			a.settings = Settings{BaseURL: provider.URL, Model: "test"}
			session := createSession(t, a)
			requireStatus(t, request(a, "POST", "/api/sessions/"+session.ID+"/runs", map[string]any{"mode": "chat", "prompt": "Read reference source"}), 202)
			waitTaskDone(t, a, session.ID)
			found := false
			for _, messages := range provider.requests {
				for _, message := range messages {
					if message.Role == "tool" && strings.Contains(message.Content, "real protocol reference") {
						found = true
					}
				}
			}
			if !found {
				t.Fatal("source body not delivered to model request")
			}
			t.Log("PASS: registration, real list/read, write policy, AI tool and model roundtrip body")
		})
	}
}
