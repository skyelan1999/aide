package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestMCPHTTPSourceDiscoveryAndReadOnlyCall(t *testing.T) {
	var calls, closed atomic.Int32
	fixture := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer mcp-test-token" {
			w.WriteHeader(401)
			return
		}
		if r.Method == http.MethodDelete {
			closed.Add(1)
			w.WriteHeader(204)
			return
		}
		var in struct {
			ID     int             `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if json.NewDecoder(r.Body).Decode(&in) != nil {
			w.WriteHeader(400)
			return
		}
		if in.Method != "initialize" && r.Header.Get("Mcp-Session-Id") != "test-session" {
			w.WriteHeader(400)
			return
		}
		w.Header().Set("Mcp-Session-Id", "test-session")
		if in.Method == "notifications/initialized" {
			w.WriteHeader(202)
			return
		}
		var result any
		switch in.Method {
		case "initialize":
			result = map[string]any{"protocolVersion": "2025-03-26", "capabilities": map[string]any{}}
		case "tools/list":
			result = map[string]any{"tools": []any{map[string]any{"name": "read_time", "description": "clock", "annotations": map[string]bool{"readOnlyHint": true}}, map[string]any{"name": "unknown_permission"}}}
		case "tools/call":
			calls.Add(1)
			result = map[string]any{"content": []any{map[string]any{"type": "text", "text": "clock reached through AIDE"}}}
		default:
			w.WriteHeader(400)
			return
		}
		wire, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": in.ID, "result": result})
		if in.Method == "tools/list" {
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprintf(w, ": keepalive\n\nevent: message\ndata: %s\n\n", wire)
		} else {
			w.Header().Set("Content-Type", "application/json")
			w.Write(wire)
		}
	}))
	defer fixture.Close()
	a := testApp(t)
	requireStatus(t, request(a, "PUT", "/api/sources", map[string]any{"sources": []any{sourceBody("http-mcp", "gateway", "mcp", map[string]any{"transport": "streamable-http", "url": fixture.URL + "/mcp"}, false)}, "secrets": map[string]any{"http-mcp": map[string]any{"password": "mcp-test-token"}}}), 200)
	discovery := request(a, "POST", "/api/sources/http-mcp/test", nil)
	requireStatus(t, discovery, 200)
	if !strings.Contains(discovery.Body.String(), `"readOnly":true`) || strings.Contains(discovery.Body.String(), "mcp-test-token") {
		t.Fatalf("invalid discovery %s", discovery.Body.String())
	}
	result := a.executeToolCall(context.Background(), readCall("mcp_call", `{"source":"http-mcp","tool":"read_time","arguments":{}}`), &Task{}, nil)
	if !strings.Contains(result, "clock reached through AIDE") {
		t.Fatalf("call failed %s", result)
	}
	denied := a.executeToolCall(context.Background(), readCall("mcp_call", `{"source":"http-mcp","tool":"unknown_permission","arguments":{}}`), &Task{}, nil)
	if !strings.Contains(denied, "只读") || calls.Load() != 1 {
		t.Fatalf("unknown permission was executed: %s", denied)
	}
	if closed.Load() != 3 {
		t.Fatalf("sessions not closed: %d", closed.Load())
	}
}

func TestMCPHTTPRejectsUnsafeURLAndMixedCommand(t *testing.T) {
	for _, raw := range []string{"ftp://localhost/a", "http://public.example/mcp", "https://user:token@example.com/mcp", "https://example.com/mcp?token=secret", "https://example.com/mcp#secret", "/mcp"} {
		if validateMCPHTTPURL(raw) == nil {
			t.Errorf("accepted %q", raw)
		}
	}
	for _, raw := range []string{"http://127.0.0.1:123/mcp", "http://[::1]:123/mcp", "http://host.docker.internal:123/mcp", "https://example.com/mcp"} {
		if err := validateMCPHTTPURL(raw); err != nil {
			t.Errorf("rejected safe endpoint %q: %v", raw, err)
		}
	}
	cfg := SourceConfig{Transport: "streamable-http", URL: "http://localhost/mcp", Command: "docker"}
	if validateMCPConfig(&cfg) == nil {
		t.Fatal("mixed local command/HTTP accepted")
	}
}

func TestMCPHTTPAuthRedirectAndBoundedResponses(t *testing.T) {
	var destinationHits atomic.Int32
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { destinationHits.Add(1) }))
	defer destination.Close()
	for _, test := range []struct {
		name              string
		status            int
		contentType, body string
	}{
		{"unauthorized", 401, "application/json", "secret-token-must-not-leak"},
		{"redirect", 302, "application/json", ""},
		{"wrong-id", 200, "application/json", `{"jsonrpc":"2.0","id":99,"result":{}}`},
		{"oversized", 200, "application/json", strings.Repeat("x", mcpLineLimit+2)},
		{"bad-type", 200, "text/html", "<html>login</html>"},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", test.contentType)
				if test.status == 302 {
					w.Header().Set("Location", destination.URL)
				}
				w.WriteHeader(test.status)
				fmt.Fprint(w, test.body)
			}))
			defer fixture.Close()
			_, err := openMCPHTTP(context.Background(), SourceConfig{URL: fixture.URL}, "secret-token-must-not-leak")
			if err == nil || strings.Contains(err.Error(), "secret-token-must-not-leak") {
				t.Fatalf("unsafe error: %v", err)
			}
		})
	}
	if destinationHits.Load() != 0 {
		t.Fatal("followed redirect and potentially leaked token")
	}
}

func TestMCPHTTPCancellationDoesNotWaitForGateway(t *testing.T) {
	fixture := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		select {
		case <-r.Context().Done():
		case <-time.After(time.Second):
		}
	}))
	defer fixture.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := openMCPHTTP(ctx, SourceConfig{URL: fixture.URL}, "")
	if err == nil || time.Since(start) > time.Second {
		t.Fatalf("cancel not honored: %v", err)
	}
}

func TestDockerMCPGatewayVirtualHostRequiresPrivateEndpointAndToken(t *testing.T) {
	if _, err := openMCPHTTP(context.Background(), SourceConfig{URL: "http://host.docker.internal:8811/mcp", DockerGateway: true}, ""); err == nil {
		t.Fatal("Docker gateway without bearer token accepted")
	}
	cfg := SourceConfig{Transport: "streamable-http", URL: "https://remote.example/mcp", DockerGateway: true}
	if validateMCPConfig(&cfg) == nil {
		t.Fatal("Docker virtual-host mode allowed remote server")
	}
	request, _ := http.NewRequest(http.MethodPost, "http://host.docker.internal:8811/mcp", nil)
	client := &mcpHTTP{dockerGateway: true, version: "2025-03-26", token: "fixture-token"}
	client.headers(request)
	if request.Host != "localhost:8811" || request.Header.Get("Authorization") != "Bearer fixture-token" {
		t.Fatal("Docker private alias did not retain authenticated loopback virtual host")
	}
	plain, _ := http.NewRequest(http.MethodPost, "https://remote.example/mcp", nil)
	(&mcpHTTP{}).headers(plain)
	if plain.Host != "remote.example" {
		t.Fatal("ordinary MCP endpoint host changed")
	}
}

// A delayed source snapshot must never pair an old URL with newly saved credentials.
func TestMCPHTTPRejectsStaleConnectionBeforeSending(t *testing.T) {
	var hits atomic.Int32
	fixture := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits.Add(1); w.WriteHeader(500) }))
	defer fixture.Close()
	a := testApp(t)
	put := func(endpoint, token string) {
		t.Helper()
		requireStatus(t, request(a, "PUT", "/api/sources", map[string]any{"sources": []any{sourceBody("stale-mcp", "gateway", "mcp", map[string]any{"transport": "streamable-http", "url": endpoint}, false)}, "secrets": map[string]any{"stale-mcp": map[string]any{"password": token}}}), 200)
	}
	put(fixture.URL+"/old", "old-token")
	a.mu.Lock()
	stale, _ := a.findSource("stale-mcp")
	a.mu.Unlock()
	put(fixture.URL+"/new", "new-token")
	if client, err := a.openMCPSource(context.Background(), stale); err == nil {
		client.close()
		t.Fatal("stale connection accepted")
	}
	if hits.Load() != 0 {
		t.Fatal("stale endpoint received a request")
	}
}

func TestMCPHTTPRevocationAndDiscoveryCompareAndSwap(t *testing.T) {
	for _, changeAt := range []string{"initialize", "tools/list"} {
		t.Run(changeAt, func(t *testing.T) {
			a := testApp(t)
			var notified, called atomic.Int32
			fixture := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodDelete {
					w.WriteHeader(204)
					return
				}
				var in struct {
					ID     int    `json:"id"`
					Method string `json:"method"`
				}
				if json.NewDecoder(r.Body).Decode(&in) != nil {
					w.WriteHeader(400)
					return
				}
				if in.Method == changeAt {
					a.mu.Lock()
					for i := range a.sourceRegistry.Sources {
						if a.sourceRegistry.Sources[i].ID == "changing-mcp" {
							a.sourceRegistry.Sources[i].Enabled = false
						}
					}
					a.mu.Unlock()
				}
				if in.Method == "notifications/initialized" {
					notified.Add(1)
					w.WriteHeader(202)
					return
				}
				if in.Method == "tools/call" {
					called.Add(1)
				}
				var result any = map[string]any{"protocolVersion": "2025-03-26"}
				if in.Method == "tools/list" {
					result = map[string]any{"tools": []any{map[string]any{"name": "stale_tool"}}}
				}
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": in.ID, "result": result})
			}))
			defer fixture.Close()
			requireStatus(t, request(a, "PUT", "/api/sources", map[string]any{"sources": []any{sourceBody("changing-mcp", "gateway", "mcp", map[string]any{"transport": "streamable-http", "url": fixture.URL}, false)}}), 200)
			response := request(a, "POST", "/api/sources/changing-mcp/test", nil)
			expected := 409
			if changeAt == "initialize" {
				expected = 502
			}
			requireStatus(t, response, expected)
			a.mu.Lock()
			current, _ := a.findSource("changing-mcp")
			a.mu.Unlock()
			if len(current.Config.MCPTools) != 0 || called.Load() != 0 {
				t.Fatal("revoked discovery or tool call was accepted")
			}
			if changeAt == "initialize" && notified.Load() != 0 {
				t.Fatal("request sent after revocation")
			}
		})
	}
}
