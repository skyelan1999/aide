package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"
	"unicode/utf8"
)

// ExecutionPolicy controls the agent loop, never tool authorization. Each task
// stores its own value so a settings edit cannot change a paused run's contract.
type ExecutionPolicy struct {
	Version               int    `json:"version"`
	ChatBudgetSec         int    `json:"chatBudgetSec"`
	WorkflowBudgetSec     int    `json:"workflowBudgetSec"`
	ToolMaxRounds         int    `json:"toolMaxRounds"`
	CompletionReviews     int    `json:"completionReviews"`
	ResearchCorrections   int    `json:"researchCorrections"`
	AgentInstruction      string `json:"agentInstruction"`
	ResearchInstruction   string `json:"researchInstruction"`
	CompletionInstruction string `json:"completionInstruction"`
}

func defaultExecutionPolicy(rounds int) ExecutionPolicy {
	if rounds < 1 || rounds > 200 {
		rounds = 60
	}
	return ExecutionPolicy{1, 360, 900, rounds, 3, 2, modelLedInstruction, researchPipelineInstruction, completionInstruction}
}

func (p ExecutionPolicy) validate() error {
	if p.Version != 1 {
		return errors.New("execution policy version must be 1")
	}
	if p.ChatBudgetSec < 30 || p.ChatBudgetSec > 7200 || p.WorkflowBudgetSec < 30 || p.WorkflowBudgetSec > 7200 {
		return errors.New("execution budgets must be 30..7200 seconds")
	}
	if p.ToolMaxRounds < 1 || p.ToolMaxRounds > 200 {
		return errors.New("toolMaxRounds must be 1..200")
	}
	if p.CompletionReviews < 0 || p.CompletionReviews > 10 || p.ResearchCorrections < 0 || p.ResearchCorrections > 10 {
		return errors.New("review/correction limits must be 0..10")
	}
	for name, value := range map[string]string{"agentInstruction": p.AgentInstruction, "researchInstruction": p.ResearchInstruction, "completionInstruction": p.CompletionInstruction} {
		if utf8.RuneCountInString(value) > 12000 {
			return fmt.Errorf("%s exceeds 12000 characters", name)
		}
	}
	return nil
}
func decodeExecutionPolicy(b []byte) (ExecutionPolicy, error) {
	var p ExecutionPolicy
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if err := d.Decode(&p); err != nil {
		return p, err
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return p, errors.New("expected one JSON policy object")
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(b, &fields); err != nil {
		return p, err
	}
	for _, key := range []string{"version", "chatBudgetSec", "workflowBudgetSec", "toolMaxRounds", "completionReviews", "researchCorrections", "agentInstruction", "researchInstruction", "completionInstruction"} {
		if value, ok := fields[key]; !ok || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return p, fmt.Errorf("missing policy field: %s", key)
		}
	}
	return p, p.validate()
}
func (a *App) executionPolicyPath() string {
	return filepath.Join(a.dataPath, "config", "execution-policy.json")
}

// Caller holds a.mu; configuration updates and new-task snapshots are serialized.
func (a *App) loadGlobalExecutionPolicy() (ExecutionPolicy, error) {
	p := defaultExecutionPolicy(a.settings.ToolMaxRounds)
	f, err := os.Open(a.executionPolicyPath())
	if os.IsNotExist(err) {
		return p, nil
	}
	if err != nil {
		return p, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, 128*1024+1))
	if err != nil {
		return p, err
	}
	if len(b) > 128*1024 {
		return p, errors.New("execution policy exceeds 128 KiB")
	}
	loaded, err := decodeExecutionPolicy(b)
	if err != nil {
		return p, err
	}
	return loaded, nil
}
func (a *App) currentExecutionPolicy() ExecutionPolicy { p, _ := a.loadExecutionPolicy(); return p }
func taskExecutionPolicy(t *Task) ExecutionPolicy {
	if t.ExecutionPolicy != nil {
		return *t.ExecutionPolicy
	}
	return defaultExecutionPolicy(60) // Old checkpoints retain historical defaults.
}
func taskExecutionTimeout(t *Task) time.Duration {
	p := taskExecutionPolicy(t)
	sec := p.WorkflowBudgetSec
	if t.Mode == "chat" {
		sec = p.ChatBudgetSec
	}
	return time.Duration(sec) * time.Second
}
func (a *App) getExecutionPolicy(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	defer a.mu.Unlock()
	p, err := a.loadGlobalExecutionPolicy()
	warning := ""
	if err != nil {
		warning = err.Error()
	}
	effective, effectiveErr := a.loadExecutionPolicy()
	if effectiveErr != nil {
		warning = effectiveErr.Error()
	}
	jsonOut(w, 200, map[string]any{"policy": p, "effectivePolicy": effective, "workspaceId": a.wsID(), "defaults": defaultExecutionPolicy(a.settings.ToolMaxRounds), "warning": warning, "scope": "global defaults; workspace overrides apply to new tasks; resumed tasks retain their snapshot"})
}
func (a *App) putExecutionPolicy(w http.ResponseWriter, r *http.Request) {
	b, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 128*1024))
	if err != nil {
		fail(w, 400, err)
		return
	}
	p, err := decodeExecutionPolicy(b)
	if err != nil {
		fail(w, 400, err)
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	path := a.executionPolicyPath()
	if err = os.MkdirAll(filepath.Dir(path), 0700); err == nil {
		err = atomicJSON(path, p)
	}
	if err != nil {
		fail(w, 500, err)
		return
	}
	jsonOut(w, 200, map[string]any{"policy": p, "saved": true})
}
