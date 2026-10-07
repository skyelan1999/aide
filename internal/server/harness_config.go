package server

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"unicode/utf8"
)

type HarnessSkill struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Instruction string `json:"instruction,omitempty"`
	Path        string `json:"path,omitempty"`
}
type HarnessAgent struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Instruction string   `json:"instruction"`
	Profile     string   `json:"profile,omitempty"`
	Model       string   `json:"model,omitempty"`
	Tools       []string `json:"tools"`
}
type HarnessHook struct {
	Name       string `json:"name"`
	Event      string `json:"event"`
	Tool       string `json:"tool"`
	Command    string `json:"command"`
	Enabled    bool   `json:"enabled"`
	TimeoutSec int    `json:"timeoutSec"`
}
type HarnessConfig struct {
	Version       int            `json:"version"`
	Skills        []HarnessSkill `json:"skills"`
	Agents        []HarnessAgent `json:"agents"`
	Hooks         []HarnessHook  `json:"hooks"`
	DenyTools     []string       `json:"denyTools"`
	MaxAgentDepth int            `json:"maxAgentDepth"`
}

func defaultHarnessConfig() HarnessConfig {
	return HarnessConfig{Version: 1, Skills: []HarnessSkill{}, Agents: []HarnessAgent{}, Hooks: []HarnessHook{}, DenyTools: []string{}, MaxAgentDepth: 3}
}

var harnessName = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,63}$`)
var harnessToolName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.:-]{0,127}$`)

func (c HarnessConfig) validate() error {
	if c.Version != 1 || c.MaxAgentDepth < 0 || c.MaxAgentDepth > 6 {
		return errors.New("version must be 1; maxAgentDepth must be 0..6")
	}
	if len(c.Skills) > 64 || len(c.Agents) > 32 || len(c.Hooks) > 32 || len(c.DenyTools) > 256 {
		return errors.New("extension count exceeds limit")
	}
	seen := map[string]bool{}
	check := func(kind, name, description, content string) error {
		key := kind + ":" + name
		if !harnessName.MatchString(name) || seen[key] {
			return fmt.Errorf("invalid or duplicate %s name: %s", kind, name)
		}
		seen[key] = true
		if utf8.RuneCountInString(description) > 1000 || utf8.RuneCountInString(content) > 16000 {
			return errors.New("extension text exceeds limit")
		}
		return nil
	}
	for _, s := range c.Skills {
		if err := check("skill", s.Name, s.Description, s.Instruction); err != nil {
			return err
		}
		if (s.Instruction == "") == (s.Path == "") {
			return errors.New("skill requires exactly one instruction or workspace path")
		}
		if s.Path != "" {
			if err := safePath(s.Path); err != nil {
				return err
			}
		}
	}
	for _, a := range c.Agents {
		if err := check("agent", a.Name, a.Description, a.Instruction); err != nil {
			return err
		}
		if len(a.Tools) > 256 || len(a.Model) > 200 || len(a.Profile) > 100 {
			return errors.New("agent configuration exceeds limit")
		}
		for _, tool := range a.Tools {
			if !harnessToolName.MatchString(tool) {
				return errors.New("invalid agent tool name")
			}
		}
	}
	for _, h := range c.Hooks {
		if err := check("hook", h.Name, "", h.Command); err != nil {
			return err
		}
		if h.Event != "before_tool" && h.Event != "after_tool" {
			return errors.New("hook event must be before_tool or after_tool")
		}
		if h.Tool != "*" && !harnessToolName.MatchString(h.Tool) {
			return errors.New("invalid hook tool matcher")
		}
		if h.Command == "" || h.TimeoutSec < 1 || h.TimeoutSec > 120 {
			return errors.New("hook requires command and timeoutSec 1..120")
		}
	}
	for _, tool := range c.DenyTools {
		if !harnessToolName.MatchString(tool) {
			return errors.New("invalid denied tool name")
		}
	}
	return nil
}
func (a *App) harnessConfigPath(workspace bool) string {
	if !workspace {
		return filepath.Join(a.dataPath, "config", "harness.json")
	}
	sum := sha256.Sum256([]byte(a.wsID()))
	return filepath.Join(a.dataPath, "config", "workspace-harness", hex.EncodeToString(sum[:])+".json")
}
func readHarnessPatch(path string) (map[string]json.RawMessage, error) {
	f, err := os.Open(path)
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
		return nil, errors.New("harness config exceeds 128 KiB")
	}
	return decodeWorkspacePolicy(b)
}
func mergeHarnessConfig(base HarnessConfig, patch map[string]json.RawMessage) (HarnessConfig, error) {
	b, _ := json.Marshal(base)
	var fields map[string]json.RawMessage
	_ = json.Unmarshal(b, &fields)
	for key, value := range patch {
		if _, ok := fields[key]; !ok {
			return base, errors.New("unknown harness field: " + key)
		}
		if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return base, errors.New("null harness field: " + key)
		}
		fields[key] = value
	}
	b, _ = json.Marshal(fields)
	var c HarnessConfig
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if err := d.Decode(&c); err != nil {
		return base, err
	}
	return c, c.validate()
}

// Caller holds a.mu. Arrays replace the lower layer, rather than concatenate.
func (a *App) loadHarnessConfig() (HarnessConfig, error) {
	c := defaultHarnessConfig()
	for _, workspace := range []bool{false, true} {
		patch, err := readHarnessPatch(a.harnessConfigPath(workspace))
		if err != nil {
			return c, err
		}
		c, err = mergeHarnessConfig(c, patch)
		if err != nil {
			return c, err
		}
	}
	return c, nil
}
func (a *App) harnessConfigHandler(w http.ResponseWriter, r *http.Request) {
	workspace := r.URL.Path == "/api/harness-config/workspace"
	var patch map[string]json.RawMessage
	if r.Method == http.MethodPut {
		b, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 128*1024))
		if err != nil {
			fail(w, 400, err)
			return
		}
		patch, err = decodeWorkspacePolicy(b)
		if err != nil {
			fail(w, 400, err)
			return
		}
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if workspace && r.Method != http.MethodGet && r.URL.Query().Get("workspaceId") != "" && r.URL.Query().Get("workspaceId") != a.wsID() {
		fail(w, 409, errors.New("工作区已切换，请重新加载配置"))
		return
	}
	path := a.harnessConfigPath(workspace)
	if r.Method == http.MethodDelete {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			fail(w, 500, err)
			return
		}
	}
	if r.Method == http.MethodPut {
		base := defaultHarnessConfig()
		if workspace {
			global, err := readHarnessPatch(a.harnessConfigPath(false))
			if err != nil {
				fail(w, 400, err)
				return
			}
			base, err = mergeHarnessConfig(base, global)
			if err != nil {
				fail(w, 400, err)
				return
			}
		}
		if _, err := mergeHarnessConfig(base, patch); err != nil {
			fail(w, 400, err)
			return
		}
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			fail(w, 500, err)
			return
		}
		if err := atomicJSON(path, patch); err != nil {
			fail(w, 500, err)
			return
		}
	}
	stored, err := readHarnessPatch(path)
	if err != nil {
		fail(w, 400, err)
		return
	}
	effective, err := a.loadHarnessConfig()
	warning := ""
	if err != nil {
		warning = err.Error()
	}
	jsonOut(w, 200, map[string]any{"config": stored, "effectiveConfig": effective, "defaults": defaultHarnessConfig(), "workspaceId": a.wsID(), "warning": warning, "saved": r.Method == http.MethodPut})
}
