package server

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"
)

type recoveryCall struct {
	ID        string `json:"id"`
	Tool      string `json:"tool"`
	Stamp     string `json:"stamp"`
	Operation string `json:"operation,omitempty"`
}

func recoveryOperation(tool string, args map[string]any) string {
	return recoveryHash([]any{tool, args})
}
func (a *App) recoveryOperationError(task *Task, tool string, args map[string]any, skipPlan *PluginExecutionPlan) error {
	// Built-in read observations are needed to verify external state.
	switch tool {
	case "read_file", "list_files", "list_sources", "document_search", "semantic_search":
		return nil
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	op := recoveryOperation(tool, args)
	for _, p := range task.PluginPlans {
		if p == nil {
			continue
		}
		for _, n := range p.Nodes {
			if p == skipPlan {
				continue
			}
			if (n.State == "uncertain" || n.State == "dispatched" && p.State != "running") && recoveryOperation(n.Tool, n.Arguments) == op {
				return errors.New("previous unresolved operation cannot be repeated under a new call ID")
			}
		}
	}
	if recoveryReceiptValid(task.RecoveryContinuation) {
		for _, c := range task.RecoveryContinuation.Calls {
			if c.Operation != "" && c.Operation == op {
				return errors.New("continuation does not authorize retry of an unresolved operation")
			}
		}
	}
	return nil
}

type recoveryNode struct {
	ID         string `json:"id"`
	Tool       string `json:"tool"`
	State      string `json:"state"`
	Check      string `json:"check,omitempty"`
	Conclusion string `json:"conclusion,omitempty"`
}
type recoveryPlan struct {
	ID    string         `json:"id"`
	Nodes []recoveryNode `json:"nodes"`
	Stamp string         `json:"stamp"`
}
type taskRecoveryReview struct {
	Required    bool           `json:"required"`
	Ready       bool           `json:"ready"`
	Snapshot    string         `json:"snapshot"`
	Plans       []recoveryPlan `json:"plans"`
	Calls       []recoveryCall `json:"calls"`
	HookWarning bool           `json:"hookWarning"`
}
type taskRecoveryDecision struct {
	Snapshot string `json:"snapshot"`
	Decision string `json:"decision"`
	Note     string `json:"note"`
}

// A continuation permits future work, never claims prior effects succeeded.
// There is no model tool for submitting this operator-attributed decision.
type TaskRecoveryContinuation struct {
	Snapshot    string            `json:"snapshot"`
	Decision    string            `json:"decision"`
	Note        string            `json:"note,omitempty"`
	SourceRun   string            `json:"sourceRun"`
	Source      string            `json:"source"`
	RecordedAt  string            `json:"recordedAt"`
	Plans       map[string]string `json:"plans"`
	Calls       []recoveryCall    `json:"calls"`
	HookWarning bool              `json:"hookWarning"`
	Digest      string            `json:"digest"`
}

func recoveryHash(v any) string { b, _ := json.Marshal(v); return hash(b) }
func recoveryReceiptValid(v *TaskRecoveryContinuation) bool {
	if v == nil || v.Source != "operator_decision" || v.Decision != "continue_without_replay" {
		return false
	}
	copy := *v
	copy.Digest = ""
	return v.Digest != "" && recoveryHash(copy) == v.Digest
}
func recoveryCheckValid(v PluginEffectCheck) bool {
	copy := v
	copy.Digest = ""
	return v.Source == "operator_observation" && v.Evidence != "" && v.RecordedAt != "" && v.Digest != "" && recoveryHash(copy) == v.Digest && (v.Conclusion == "effect_observed" || v.Conclusion == "no_effect_observed" || v.Conclusion == "inconclusive")
}
func normalizedRecoveryPlan(p *PluginExecutionPlan) *PluginExecutionPlan {
	if p == nil {
		return nil
	}
	b, _ := json.Marshal(p)
	var copy PluginExecutionPlan
	_ = json.Unmarshal(b, &copy)
	recoverPluginPlans(&Task{PluginPlans: []*PluginExecutionPlan{&copy}})
	return &copy
}
func pluginPlanContinued(task *Task, p *PluginExecutionPlan) bool {
	return recoveryReceiptValid(task.RecoveryContinuation) && task.RecoveryContinuation.Plans[p.ID] == recoveryHash(normalizedRecoveryPlan(p))
}
func recoveryRetryUnknown(task *Task) bool {
	for _, p := range task.PluginPlans {
		if p != nil {
			for _, n := range p.Nodes {
				if n.State == "uncertain" || n.State == "dispatched" {
					return true
				}
			}
		}
	}
	return recoveryReceiptValid(task.RecoveryContinuation) && (len(task.RecoveryContinuation.Calls) > 0 || task.RecoveryContinuation.HookWarning)
}

// Caller holds App.mu. Hash private task inputs, expose only identities and
// check provenance; notes, arguments and checkpoint text never enter preview.
func taskRecoveryPreview(task *Task) taskRecoveryReview {
	r := taskRecoveryReview{Ready: true, Plans: []recoveryPlan{}, Calls: []recoveryCall{}, HookWarning: task.HookRecoveryRequired}
	r.Snapshot = recoveryHash([]any{task.ID, task.Status, task.WorkspaceID, task.CanResume, task.ExecutionSequence, task.CheckpointStep, task.CheckpointMessages, task.PluginPlans, task.ToolExecutionIntents, task.HookRecoveryRequired, task.RecoveryContinuation})
	graphCalls := map[string]bool{}
	for _, raw := range task.PluginPlans {
		if raw == nil {
			continue
		}
		p := normalizedRecoveryPlan(raw)
		graphCalls[p.ID] = true
		pending := p.State != "completed"
		for _, n := range p.Nodes {
			graphCalls[p.ID+"/"+n.ID] = true
			pending = pending || n.State == "uncertain" || n.State == "dispatched"
		}
		if !pending || pluginPlanContinued(task, p) {
			continue
		}
		plan := recoveryPlan{ID: p.ID, Stamp: recoveryHash(p), Nodes: []recoveryNode{}}
		for _, n := range p.Nodes {
			if n.State != "uncertain" && n.State != "dispatched" {
				continue
			}
			if n.State == "dispatched" {
				r.Ready = false
			}
			row := recoveryNode{ID: n.ID, Tool: n.Tool, State: n.State}
			if len(n.EffectChecks) == 0 {
				r.Ready = false
			} else {
				last := n.EffectChecks[len(n.EffectChecks)-1]
				row.Check = last.Digest
				row.Conclusion = last.Conclusion
				if !recoveryCheckValid(last) {
					r.Ready = false
				}
			}
			plan.Nodes = append(plan.Nodes, row)
		}
		r.Plans = append(r.Plans, plan)
	}
	known := map[string]bool{}
	if recoveryReceiptValid(task.RecoveryContinuation) {
		for _, c := range task.RecoveryContinuation.Calls {
			known[c.Stamp] = true
		}
	}
	seen := map[string]bool{}
	states := map[string]string{}
	protocol := map[string]ToolCall{}
	for _, msg := range task.CheckpointMessages {
		for _, call := range msg.ToolCalls {
			protocol[call.ID] = call
		}
	}
	add := func(id, tool, stamp, operation string) {
		if graphCalls[id] || seen[id] {
			return
		}
		seen[id] = true
		r.Calls = append(r.Calls, recoveryCall{ID: id, Tool: tool, Stamp: stamp, Operation: operation})
	}
	for _, intent := range task.ToolExecutionIntents {
		states[intent.CallID] = intent.State
		if intent.State != "completed" {
			stamp := recoveryHash(intent)
			operation := ""
			if call, ok := protocol[intent.CallID]; ok {
				stamp = recoveryHash(call)
				var args map[string]any
				if json.Unmarshal([]byte(call.Function.Arguments), &args) == nil {
					operation = recoveryOperation(call.Function.Name, args)
				}
			}
			add(intent.CallID, intent.Tool, stamp, operation)
		}
	}
	for i := 0; i < len(task.CheckpointMessages); i++ {
		msg := task.CheckpointMessages[i]
		if msg.Role != "assistant" {
			continue
		}
		answers := map[string]string{}
		for j := i + 1; j < len(task.CheckpointMessages) && task.CheckpointMessages[j].Role == "tool"; j++ {
			v := task.CheckpointMessages[j]
			answers[v.ToolCallID] = v.Content
		}
		for _, call := range msg.ToolCalls {
			answer, answered := answers[call.ID]
			stamp := recoveryHash(call)
			unknown := !answered && (task.ExecutionSequence == 0 || states[call.ID] == "dispatched")
			unknown = unknown || answered && strings.HasPrefix(answer, "TOOL_OUTCOME_UNKNOWN") && !known[stamp]
			if unknown {
				var args map[string]any
				operation := ""
				if json.Unmarshal([]byte(call.Function.Arguments), &args) == nil {
					operation = recoveryOperation(call.Function.Name, args)
				}
				add(call.ID, call.Function.Name, stamp, operation)
			}
		}
	}
	r.Required = len(r.Plans) > 0 || len(r.Calls) > 0 || r.HookWarning
	return r
}

func (a *App) taskRecoveryReviewLocked(task *Task) taskRecoveryReview {
	r := taskRecoveryPreview(task)
	r.Snapshot = recoveryHash([]any{r.Snapshot, a.wsID(), a.wsRevision})
	return r
}

func (a *App) taskRecoveryPreviewAPI(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	defer a.mu.Unlock()
	s := a.sessions[r.PathValue("id")]
	if s == nil || s.Deleted {
		fail(w, 404, errors.New("session not found"))
		return
	}
	for _, task := range s.Runs {
		if task.ID != r.PathValue("run") {
			continue
		}
		if (task.Status != "paused" && task.Status != "interrupted") || !task.CanResume || len(task.CheckpointMessages) == 0 {
			fail(w, 409, errors.New("no resumable checkpoint"))
			return
		}
		if task.WorkspaceID != "" && task.WorkspaceID != a.wsID() {
			fail(w, 409, errors.New("return to the original workspace before recovery"))
			return
		}
		if _, active := a.cancels[task.ID]; active {
			fail(w, 409, errors.New("task is still stopping"))
			return
		}
		jsonOut(w, 200, a.taskRecoveryReviewLocked(task))
		return
	}
	fail(w, 404, errors.New("task not found"))
}

func readTaskResumeRequest(w http.ResponseWriter, r *http.Request) (*bool, *taskRecoveryDecision, error) {
	if r.Body == nil || r.ContentLength == 0 {
		return nil, nil, nil
	}
	var req struct {
		AvatarFeedback *bool                 `json:"avatarFeedback,omitempty"`
		Recovery       *taskRecoveryDecision `json:"recovery,omitempty"`
	}
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10))
	d.DisallowUnknownFields()
	if err := d.Decode(&req); err != nil {
		return nil, nil, err
	}
	if err := d.Decode(&struct{}{}); err != io.EOF {
		return nil, nil, errors.New("expected one JSON object")
	}
	if req.Recovery != nil {
		x := req.Recovery
		x.Note = strings.TrimSpace(x.Note)
		if x.Decision != "continue_without_replay" || len(x.Snapshot) != 64 || x.Note == "" || len(x.Note) > 4096 {
			return nil, nil, errors.New("explicit continuation decision, snapshot and 1-4096 note bytes required")
		}
	}
	return req.AvatarFeedback, req.Recovery, nil
}

func applyRecoveryContinuation(task *Task, sourceID string, review taskRecoveryReview, decision *taskRecoveryDecision) {
	receipt := &TaskRecoveryContinuation{Snapshot: review.Snapshot, Decision: decision.Decision, Note: decision.Note, SourceRun: sourceID, Source: "operator_decision", RecordedAt: time.Now().UTC().Format(time.RFC3339Nano), Plans: map[string]string{}, Calls: []recoveryCall{}, HookWarning: review.HookWarning}
	if recoveryReceiptValid(task.RecoveryContinuation) {
		for k, v := range task.RecoveryContinuation.Plans {
			receipt.Plans[k] = v
		}
		receipt.Calls = append(receipt.Calls, task.RecoveryContinuation.Calls...)
	}
	for _, p := range review.Plans {
		receipt.Plans[p.ID] = p.Stamp
	}
	receipt.Calls = append(receipt.Calls, review.Calls...)
	receipt.Digest = recoveryHash(receipt)
	task.RecoveryContinuation = receipt
	task.HookRecoveryRequired = false
	// A server-authored summary records operator intent without sending the
	// private note or node evidence to the provider. It does not fabricate tools.
	summary, _ := json.Marshal(map[string]any{"plans": review.Plans, "calls": review.Calls, "hookWarning": review.HookWarning, "receipt": receipt.Digest})
	task.CheckpointMessages = append(task.CheckpointMessages, Message{Role: "user", Content: "OPERATOR_CONTINUATION: The operator explicitly chose to continue future work without replaying previous unresolved calls. Prior uncertain/proposed/skipped nodes remain unverified. Do not repeat their effects or invent results. New operations still require current policy, approvals and task hooks. Reviewed identities: " + string(summary)})
}
