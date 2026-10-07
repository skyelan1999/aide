package server

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
)

// Overrides are trusted operator configuration in the data directory. We do
// not execute hooks or import policy automatically from untrusted repositories.
func (a *App) workspaceExecutionPolicyPath() string {
	sum := sha256.Sum256([]byte(a.wsID()))
	return filepath.Join(a.dataPath, "config", "workspace-policies", hex.EncodeToString(sum[:])+".json")
}

func mergeExecutionPolicy(base ExecutionPolicy, patch map[string]json.RawMessage) (ExecutionPolicy, error) {
	b, err := json.Marshal(base)
	if err != nil {
		return base, err
	}
	var fields map[string]json.RawMessage
	if err = json.Unmarshal(b, &fields); err != nil {
		return base, err
	}
	for name, value := range patch {
		if _, ok := fields[name]; !ok {
			return base, errors.New("unknown policy field: " + name)
		}
		if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return base, errors.New("null policy field: " + name)
		}
		fields[name] = value
	}
	b, err = json.Marshal(fields)
	if err != nil {
		return base, err
	}
	return decodeExecutionPolicy(b)
}

func decodeWorkspacePolicy(b []byte) (map[string]json.RawMessage, error) {
	var patch map[string]json.RawMessage
	d := json.NewDecoder(bytes.NewReader(b))
	if err := d.Decode(&patch); err != nil {
		return nil, err
	}
	if patch == nil {
		return nil, errors.New("workspace policy must be an object")
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return nil, errors.New("expected one policy object")
	}
	return patch, nil
}

func (a *App) readWorkspacePolicy() (map[string]json.RawMessage, error) {
	f, err := os.Open(a.workspaceExecutionPolicyPath())
	if os.IsNotExist(err) {
		return map[string]json.RawMessage{}, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, 128*1024+1))
	if err != nil {
		return nil, err
	}
	if len(b) > 128*1024 {
		return nil, errors.New("workspace policy exceeds 128 KiB")
	}
	return decodeWorkspacePolicy(b)
}

// Caller holds a.mu, so global and workspace layers form one task snapshot.
func (a *App) loadExecutionPolicy() (ExecutionPolicy, error) {
	base, err := a.loadGlobalExecutionPolicy()
	if err != nil {
		return base, err
	}
	patch, err := a.readWorkspacePolicy()
	if err != nil {
		return base, err
	}
	return mergeExecutionPolicy(base, patch)
}

func (a *App) getWorkspaceExecutionPolicy(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	defer a.mu.Unlock()
	patch, err := a.readWorkspacePolicy()
	if err != nil {
		fail(w, 400, err)
		return
	}
	effective, err := a.loadExecutionPolicy()
	if err != nil {
		fail(w, 400, err)
		return
	}
	jsonOut(w, 200, map[string]any{"workspaceId": a.wsID(), "overrides": patch, "effectivePolicy": effective, "precedence": []string{"defaults", "global", "workspace", "task snapshot"}})
}

func (a *App) putWorkspaceExecutionPolicy(w http.ResponseWriter, r *http.Request) {
	b, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 128*1024))
	if err != nil {
		fail(w, 400, err)
		return
	}
	patch, err := decodeWorkspacePolicy(b)
	if err != nil {
		fail(w, 400, err)
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if r.URL.Query().Get("workspaceId") != "" && r.URL.Query().Get("workspaceId") != a.wsID() {
		fail(w, 409, errors.New("工作区已切换，请重新加载配置"))
		return
	}
	base, err := a.loadGlobalExecutionPolicy()
	if err != nil {
		fail(w, 400, err)
		return
	}
	effective, err := mergeExecutionPolicy(base, patch)
	if err != nil {
		fail(w, 400, err)
		return
	}
	path := a.workspaceExecutionPolicyPath()
	if err = os.MkdirAll(filepath.Dir(path), 0700); err == nil {
		err = atomicJSON(path, patch)
	}
	if err != nil {
		fail(w, 500, err)
		return
	}
	jsonOut(w, 200, map[string]any{"saved": true, "workspaceId": a.wsID(), "overrides": patch, "effectivePolicy": effective})
}

func (a *App) clearWorkspaceExecutionPolicy(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if r.URL.Query().Get("workspaceId") != "" && r.URL.Query().Get("workspaceId") != a.wsID() {
		fail(w, 409, errors.New("工作区已切换，请重新加载配置"))
		return
	}
	if err := os.Remove(a.workspaceExecutionPolicyPath()); err != nil && !os.IsNotExist(err) {
		fail(w, 500, err)
		return
	}
	jsonOut(w, 200, map[string]any{"cleared": true, "workspaceId": a.wsID()})
}
