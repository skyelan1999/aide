package server

// Host operations are a manual workbench capability, never a model tool or a
// generic shell/proxy API. Only the launcher chooses the component and endpoint.
import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type hostComponentConnection struct {
	Protocol int    `json:"protocol"`
	Service  string `json:"service"`
	Version  string `json:"version"`
	Token    string `json:"token"`
	Port     int    `json:"port"`
}

func readHostComponentConnection(dataPath string) (hostComponentConnection, error) {
	var c hostComponentConnection
	dir := filepath.Join(dataPath, "host-components")
	// Reject link redirection of either the metadata directory or the file.
	for _, p := range []string{dir, filepath.Join(dir, "codex-bridge.json")} {
		info, e := os.Lstat(p)
		if e != nil || info.Mode()&os.ModeSymlink != 0 {
			return c, errors.New("host metadata unavailable")
		}
	}
	file := filepath.Join(dir, "codex-bridge.json")
	info, e := os.Stat(file)
	if e != nil || !info.Mode().IsRegular() || info.Size() > 4096 {
		return c, errors.New("invalid host metadata")
	}
	raw, e := os.ReadFile(file)
	if e != nil {
		return c, errors.New("host metadata unreadable")
	}
	if json.Unmarshal(raw, &c) != nil || c.Protocol != 1 || c.Service != "codex-bridge" || c.Port != 17888 {
		return c, errors.New("invalid host metadata")
	}
	decoded, e := hex.DecodeString(c.Token)
	if e != nil || len(decoded) != 32 {
		return c, errors.New("invalid host credential")
	}
	return c, nil
}
func hostComponentRequest(ctx context.Context, client *http.Client, c hostComponentConnection, action string) (map[string]any, error) {
	route, method, body := "/api/action", "POST", map[string]string{"action": action}
	switch action {
	case "status", "models", "usage":
		route = "/api/" + action
		method = "GET"
		body = nil
	case "key":
		route = "/api/key"
		body = map[string]string{}
	case "install", "start", "stop", "uninstall":
	default:
		return nil, errors.New("unsupported host action")
	}
	raw, _ := json.Marshal(body)
	base := "http://host.docker.internal:17888"
	// A native Aide process shares the launcher's loopback interface.
	if _, e := os.Stat("/.dockerenv"); os.IsNotExist(e) {
		base = "http://127.0.0.1:17888"
	}
	req, e := http.NewRequestWithContext(ctx, method, base+route, bytes.NewReader(raw))
	if e != nil {
		return nil, errors.New("invalid host request")
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Manager-Owner-UI", "1")
	resp, e := client.Do(req)
	if e != nil {
		return nil, errors.New("Aide host component unavailable; restart Aide with the updated launcher / 请使用新版启动器重新启动 Aide")
	}
	defer resp.Body.Close()
	data, e := io.ReadAll(io.LimitReader(resp.Body, 65537))
	if e != nil || len(data) > 65536 {
		return nil, errors.New("invalid host response")
	}
	var value map[string]any
	if json.Unmarshal(data, &value) != nil {
		return nil, errors.New("invalid host response")
	}
	if resp.StatusCode != 200 {
		message, _ := value["error"].(string)
		message = strings.ReplaceAll(message, c.Token, "[redacted]")
		if len(message) > 500 {
			message = message[:500]
		}
		if message == "" {
			message = "Host operation failed"
		}
		return nil, errors.New(message)
	}
	// Explicit projection: never return a control token, path or arbitrary host
	// response field into the frame. Only an explicit key action returns its key.
	if action == "key" {
		key, _ := value["bridgeKey"].(string)
		decoded, e := hex.DecodeString(key)
		if e != nil || len(decoded) != 32 {
			return nil, errors.New("invalid model key response")
		}
		return map[string]any{"bridgeKey": key, "created": value["created"]}, nil
	}
	if value["service"] != "aide-codex-bridge-manager" {
		return nil, errors.New("unexpected host service")
	}
	result := map[string]any{}
	if action == "usage" {
		usage, ok := value["usage"].(map[string]any)
		if !ok {
			return nil, errors.New("invalid quota response")
		}
		buckets, _ := usage["buckets"].([]any)
		if len(buckets) > 32 {
			return nil, errors.New("invalid quota response")
		}
		safeBuckets := []any{}
		available := false
		for _, entry := range buckets {
			b, ok := entry.(map[string]any)
			if !ok {
				continue
			}
			safe := map[string]any{}
			for _, name := range []string{"id", "label"} {
				if v, ok := b[name].(string); ok && len(v) <= 200 {
					safe[name] = v
				}
			}
			for _, name := range []string{"primary", "secondary"} {
				window, ok := b[name].(map[string]any)
				if !ok {
					continue
				}
				used, ok := window["usedPercent"].(float64)
				if !ok || used < 0 {
					continue
				}
				remaining := 100 - used
				if remaining < 0 {
					remaining = 0
				}
				if remaining > 100 {
					remaining = 100
				}
				w := map[string]any{"usedPercent": used, "remainingPercent": remaining}
				for _, field := range []string{"windowDurationMins", "resetsAt"} {
					if v, ok := window[field].(float64); ok && v > 0 {
						w[field] = v
					}
				}
				safe[name] = w
				available = true
			}
			safeBuckets = append(safeBuckets, safe)
		}
		result["available"] = available
		result["buckets"] = safeBuckets
		if v, ok := usage["checkedAt"].(string); ok && len(v) < 64 {
			result["checkedAt"] = v
		}
		return result, nil
	}
	if action == "models" {
		items, ok := value["models"].([]any)
		if !ok || len(items) > 200 {
			return nil, errors.New("invalid model catalog")
		}
		models := []any{}
		for _, item := range items {
			m, ok := item.(map[string]any)
			if !ok {
				continue
			}
			id, ok := m["id"].(string)
			if ok && len(id) <= 200 {
				models = append(models, map[string]string{"id": id})
			}
		}
		result["models"] = models
		return result, nil
	}
	for _, k := range []string{"installed", "running", "externalBridge", "nodeFound", "codexFound", "platform", "arch", "port", "version", "ok"} {
		if v, ok := value[k]; ok {
			result[k] = v
		}
	}
	result["available"] = true
	result["paired"] = true
	result["managerVersion"] = value["version"]
	phase := "ready"
	if value["externalBridge"] == true {
		phase = "external_bridge"
	} else if value["installed"] != true {
		phase = "host_missing"
	} else if value["running"] != true {
		phase = "host_stopped"
	}
	result["phase"] = phase
	return result, nil
}
func (a *App) callWorkbenchHostComponent(w http.ResponseWriter, r *http.Request, req workbenchBridgeRequest) {
	w.Header().Set("Cache-Control", "no-store")
	id, key := r.PathValue("id"), r.PathValue("instance")
	action, _ := req.Arguments["action"].(string)
	if len(req.Arguments) != 1 || len(req.Attachments) != 0 || req.Tool != "" || req.ProposalID != "" || !map[string]bool{"status": true, "install": true, "start": true, "stop": true, "uninstall": true, "key": true, "models": true, "usage": true}[action] {
		fail(w, 400, errors.New("invalid host action"))
		return
	}
	a.mu.Lock()
	b, e := a.workbenchBridgeLocked(id, key, true)
	allowed := false
	for _, p := range a.pluginRegistry.Plugins {
		if p.ID == id && id == "codex-bridge" && p.Enabled && p.Workbench != nil && p.Workbench.HostComponent == "codex-bridge" {
			allowed = true
		}
	}
	if e == nil && !allowed {
		e = errors.New("host capability denied")
	}
	if e == nil && (b.Busy || b.Seen[req.RequestID] || len(b.Seen) >= 128) {
		e = errors.New("workbench busy or replayed request")
	}
	if e != nil {
		a.mu.Unlock()
		fail(w, 403, e)
		return
	}
	b.Seen[req.RequestID] = true
	b.Busy = true
	ctx, cancel := context.WithTimeout(r.Context(), 40*time.Second)
	b.Cancel = cancel
	dataPath := a.dataPath
	a.mu.Unlock()
	defer func() { cancel(); a.mu.Lock(); b.Busy = false; b.Cancel = nil; a.mu.Unlock() }()
	c, e := readHostComponentConnection(dataPath)
	var result map[string]any
	if e == nil {
		transport := http.DefaultTransport.(*http.Transport).Clone()
		transport.Proxy = nil
		defer transport.CloseIdleConnections()
		client := &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
		result, e = hostComponentRequest(ctx, client, c, action)
	}
	if e != nil && action == "status" {
		result = map[string]any{"available": false, "paired": false, "phase": "unavailable", "message": "宿主组件未就绪，请使用新版 Aide 完整发行包启动。No separate manager is required; restart with the updated full Aide bundle."}
		e = nil
	}
	a.mu.Lock()
	_, scopeErr := a.workbenchBridgeLocked(id, key, true)
	a.mu.Unlock()
	if scopeErr != nil {
		fail(w, 403, scopeErr)
		return
	}
	if e != nil {
		fail(w, 503, e)
		return
	}
	jsonOut(w, 200, map[string]any{"requestId": req.RequestID, "status": "result", "result": result})
}
