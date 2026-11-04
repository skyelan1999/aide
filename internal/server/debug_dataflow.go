package server

import (
	"bufio"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

const debugDataflowJournalLimit = 5000

type debugDataflowDump struct {
	SchemaVersion int                       `json:"schemaVersion"`
	GeneratedAt   string                    `json:"generatedAt"`
	Session       debugDataflowSession      `json:"session"`
	Run           debugDataflowRun          `json:"run"`
	ProviderCalls []debugDataflowProvider   `json:"providerCalls"`
	Steps         []debugDataflowStep       `json:"steps"`
	ToolUses      []debugDataflowToolUse    `json:"toolUses"`
	ToolIntents   []debugDataflowToolIntent `json:"unresolvedToolIntents"`
	Journal       debugDataflowJournal      `json:"executionJournal"`
	Incidents     []debugDataflowIncident   `json:"incidents"`
	Timeline      []debugDataflowTimeline   `json:"timeline"`
	Anomalies     []debugDataflowAnomaly    `json:"anomalies"`
}

type debugDataflowSession struct {
	ID                  string `json:"id"`
	Updated             string `json:"updated,omitempty"`
	RunCount            int    `json:"runCount"`
	MessageCount        int    `json:"messageCount"`
	MessageContentBytes int64  `json:"messageContentBytes"`
	ToolArgumentBytes   int64  `json:"toolArgumentBytes"`
}

type debugDataflowRun struct {
	ID                 string             `json:"id"`
	RequestID          string             `json:"requestId,omitempty"`
	Status             string             `json:"status"`
	Mode               string             `json:"mode,omitempty"`
	Model              string             `json:"model,omitempty"`
	Created            string             `json:"created,omitempty"`
	Failures           int                `json:"failures"`
	ErrorPresent       bool               `json:"errorPresent"`
	Usage              debugDataflowUsage `json:"usage,omitempty"`
	ExecutionSequence  uint64             `json:"executionSequence"`
	SnapshotsTruncated bool               `json:"snapshotsTruncated"`
	CheckpointPresent  bool               `json:"checkpointPresent"`
}

type debugDataflowUsage struct {
	Prompt         int  `json:"prompt"`
	CacheHit       int  `json:"cacheHit,omitempty"`
	CacheMiss      int  `json:"cacheMiss,omitempty"`
	Completion     int  `json:"completion"`
	Total          int  `json:"total"`
	Estimated      bool `json:"estimated,omitempty"`
	ReasoningKnown bool `json:"reasoningKnown,omitempty"`
	Reasoning      bool `json:"reasoning,omitempty"`
}

type debugDataflowProvider struct {
	Index               int    `json:"index"`
	Purpose             string `json:"purpose,omitempty"`
	Step                string `json:"step,omitempty"`
	Round               int    `json:"round,omitempty"`
	Model               string `json:"model,omitempty"`
	StartedAt           string `json:"startedAt,omitempty"`
	CompletedAt         string `json:"completedAt,omitempty"`
	ElapsedMillis       int64  `json:"elapsedMillis,omitempty"`
	Status              string `json:"status"`
	FailureCode         string `json:"failureCode,omitempty"`
	MessageCount        int    `json:"messageCount"`
	MessageContentBytes int64  `json:"messageContentBytes"`
	ToolArgumentBytes   int64  `json:"toolArgumentBytes"`
	ToolCount           int    `json:"toolCount"`
	BodyBytes           int    `json:"bodyBytes"`
}

type debugDataflowStep struct {
	Index          int    `json:"index"`
	Label          string `json:"label,omitempty"`
	Status         string `json:"status"`
	ContentBytes   int64  `json:"contentBytes"`
	ReasoningBytes int64  `json:"reasoningBytes"`
}

type debugDataflowToolUse struct {
	Index          int    `json:"index"`
	Tool           string `json:"tool"`
	Origin         string `json:"origin,omitempty"`
	ChildSessionID string `json:"childSessionId,omitempty"`
	ChildNumber    int    `json:"childNumber,omitempty"`
	ArgsBytes      int    `json:"argsBytes"`
	ResultBytes    int    `json:"resultBytes"`
	HasResult      bool   `json:"hasResult"`
}

type debugDataflowToolIntent struct {
	CallID string `json:"callId,omitempty"`
	Tool   string `json:"tool"`
	State  string `json:"state"`
}

type debugDataflowJournal struct {
	Available bool                        `json:"available"`
	Truncated bool                        `json:"truncated"`
	Warning   string                      `json:"warning,omitempty"`
	Events    []debugDataflowJournalEvent `json:"events"`
}

type debugDataflowJournalEvent struct {
	Sequence  uint64 `json:"sequence"`
	Kind      string `json:"kind"`
	At        string `json:"at,omitempty"`
	Step      string `json:"step,omitempty"`
	CallID    string `json:"callId,omitempty"`
	Tool      string `json:"tool,omitempty"`
	HasDigest bool   `json:"hasDigest,omitempty"`
}

type debugDataflowIncident struct {
	ID            string                 `json:"incidentId"`
	Time          string                 `json:"time"`
	Module        string                 `json:"module"`
	Phase         string                 `json:"phase"`
	Code          string                 `json:"code"`
	Severity      string                 `json:"severity"`
	Retryable     bool                   `json:"retryable"`
	CauseTypes    []string               `json:"causeTypes,omitempty"`
	Locations     []string               `json:"locations,omitempty"`
	Plugin        string                 `json:"pluginId,omitempty"`
	PluginVersion string                 `json:"pluginVersion,omitempty"`
	Tool          string                 `json:"tool,omitempty"`
	PanicType     string                 `json:"panicType,omitempty"`
	DurationMS    int64                  `json:"durationMs,omitempty"`
	HTTPStatus    int                    `json:"httpStatus,omitempty"`
	PluginFault   *pluginFailureMetadata `json:"pluginFault,omitempty"`
}

type debugDataflowTimeline struct {
	At           string `json:"at,omitempty"`
	Source       string `json:"source"`
	Kind         string `json:"kind"`
	Status       string `json:"status,omitempty"`
	Sequence     uint64 `json:"sequence,omitempty"`
	Index        int    `json:"index,omitempty"`
	Step         string `json:"step,omitempty"`
	Model        string `json:"model,omitempty"`
	Tool         string `json:"tool,omitempty"`
	CallID       string `json:"callId,omitempty"`
	IncidentCode string `json:"incidentCode,omitempty"`
	OutcomeCode  string `json:"outcomeCode,omitempty"`
}

type debugDataflowAnomaly struct {
	Code     string `json:"code"`
	Severity string `json:"severity"`
	Evidence string `json:"evidence"`
}

func debugDataflowLabel(value string) string {
	if value == "" {
		return ""
	}
	if len(value) > 128 {
		return "invalid-identifier"
	}
	for _, c := range value {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune("-_.:/#*", c)) {
			return "invalid-identifier"
		}
	}
	return value
}

func debugDataflowStatus(value string) string {
	switch value {
	case "pending", "queued", "running", "awaiting_clarification", "complete", "completed", "done", "failed", "cancelled", "interrupted", "skipped", "paused", "error", "dispatched":
		return value
	default:
		return "unknown"
	}
}

func debugDataflowOutcome(value string) string {
	switch value {
	case "completed", "failed", "empty_response":
		return value
	default:
		return "unknown"
	}
}

func debugDataflowFailureCode(value string) string {
	switch value {
	case "CANCELED", "TIMEOUT", "ACCESS_DENIED", "STATE_CONFLICT", "INVALID_REQUEST", "TLS_CERTIFICATE", "NOT_FOUND", "CONNECTION_REFUSED", "DNS_FAILED", "INVALID_JSON", "CONNECTION_FAILED", "OPERATION_FAILED", "EMPTY_RESPONSE":
		return value
	default:
		if value == "" {
			return ""
		}
		return "OTHER"
	}
}

func debugDataflowCallRef(refs map[string]string, callID string) string {
	if callID == "" {
		return ""
	}
	if ref, ok := refs[callID]; ok {
		return ref
	}
	ref := "call-" + strconv.Itoa(len(refs)+1)
	refs[callID] = ref
	return ref
}

func debugDataflowTerminal(status string) bool {
	switch status {
	case "completed", "complete", "done", "failed", "cancelled", "interrupted", "paused", "error":
		return true
	default:
		return false
	}
}

func (a *App) debugSessionDataflowDump(w http.ResponseWriter, r *http.Request) {
	sessionID, runID := r.PathValue("id"), r.PathValue("run")
	out := debugDataflowDump{
		SchemaVersion: 1,
		GeneratedAt:   time.Now().UTC().Format(time.RFC3339Nano),
		ProviderCalls: []debugDataflowProvider{},
		Steps:         []debugDataflowStep{},
		ToolUses:      []debugDataflowToolUse{},
		ToolIntents:   []debugDataflowToolIntent{},
		Journal:       debugDataflowJournal{Events: []debugDataflowJournalEvent{}},
		Incidents:     []debugDataflowIncident{},
		Timeline:      []debugDataflowTimeline{},
		Anomalies:     []debugDataflowAnomaly{},
	}
	var journalID string
	callRefs := map[string]string{}
	a.mu.Lock()
	session := a.sessions[sessionID]
	if session == nil || session.Deleted {
		a.mu.Unlock()
		fail(w, http.StatusNotFound, errors.New("会话不存在"))
		return
	}
	var task *Task
	for _, candidate := range session.Runs {
		if candidate != nil && candidate.ID == runID {
			task = candidate
			break
		}
	}
	if task == nil {
		a.mu.Unlock()
		fail(w, http.StatusNotFound, errors.New("run 不存在"))
		return
	}
	if task.ID == "" || filepath.Base(task.ID) != task.ID || task.ID == "." || task.ID == ".." {
		a.mu.Unlock()
		fail(w, http.StatusBadRequest, errors.New("无效 run 标识"))
		return
	}
	journalID = task.ID
	stepLabels := make(map[string]string, len(task.Steps))
	for i, step := range task.Steps {
		if step.Name != "" {
			stepLabels[step.Name] = "step-" + strconv.Itoa(i+1)
		}
	}
	out.Session = debugDataflowSession{ID: debugDataflowLabel(session.ID), Updated: session.Updated, RunCount: len(session.Runs), MessageCount: len(session.Messages)}
	for _, message := range session.Messages {
		out.Session.MessageContentBytes += int64(len(message.Content))
		for _, call := range message.ToolCalls {
			out.Session.ToolArgumentBytes += int64(len(call.Function.Arguments))
		}
	}
	out.Run = debugDataflowRun{
		ID: debugDataflowLabel(task.ID), RequestID: debugDataflowLabel(task.DiagnosticTraceID),
		Status: debugDataflowStatus(task.Status), Mode: debugDataflowLabel(task.Mode),
		Model: debugDataflowLabel(task.Model), Created: task.Created, Failures: task.Failures,
		ErrorPresent: task.Error != "", Usage: debugDataflowUsage{
			Prompt: task.Usage.Prompt, CacheHit: task.Usage.CacheHit, CacheMiss: task.Usage.CacheMiss,
			Completion: task.Usage.Completion, Total: task.Usage.Total, Estimated: task.Usage.Estimated,
			ReasoningKnown: task.Usage.ReasoningKnown, Reasoning: task.Usage.Reasoning,
		}, ExecutionSequence: task.ExecutionSequence,
		SnapshotsTruncated: task.SnapshotsTruncated, CheckpointPresent: len(task.CheckpointMessages) > 0,
	}
	for i, snapshot := range task.RequestSnapshots {
		stepName, roundText, _ := strings.Cut(snapshot.Purpose, "#")
		stepLabel := stepLabels[stepName]
		if stepLabel == "" && stepName == "image" {
			stepLabel = "media"
		}
		round, _ := strconv.Atoi(roundText)
		entry := debugDataflowProvider{
			Index: i + 1, Purpose: "call-" + strconv.Itoa(i+1), Step: stepLabel, Round: round, Model: debugDataflowLabel(snapshot.Model),
			StartedAt: snapshot.At, CompletedAt: snapshot.CompletedAt, ElapsedMillis: snapshot.ElapsedMillis,
			MessageCount: len(snapshot.Messages), ToolCount: len(snapshot.Tools), BodyBytes: len(snapshot.Body),
			Status:      "returned_legacy",
			FailureCode: debugDataflowFailureCode(snapshot.FailureCode),
		}
		switch snapshot.Outcome {
		case "completed", "failed", "empty_response":
			entry.Status = debugDataflowOutcome(snapshot.Outcome)
		default:
			if snapshot.CompletedAt == "" {
				entry.Status = "incomplete"
			}
		}
		for _, message := range snapshot.Messages {
			entry.MessageContentBytes += int64(len(message.Content))
			for _, call := range message.ToolCalls {
				entry.ToolArgumentBytes += int64(len(call.Function.Arguments))
			}
		}
		out.ProviderCalls = append(out.ProviderCalls, entry)
		if entry.StartedAt != "" {
			out.Timeline = append(out.Timeline, debugDataflowTimeline{At: entry.StartedAt, Source: "provider", Kind: "request", Status: "started", Index: entry.Index, Step: entry.Step, Model: entry.Model})
		}
		if entry.CompletedAt != "" {
			out.Timeline = append(out.Timeline, debugDataflowTimeline{At: entry.CompletedAt, Source: "provider", Kind: "response", Status: entry.Status, Index: entry.Index, Step: entry.Step, Model: entry.Model, OutcomeCode: entry.FailureCode})
		}
	}
	for i, step := range task.Steps {
		out.Steps = append(out.Steps, debugDataflowStep{
			Index: i + 1, Label: "step-" + strconv.Itoa(i+1), Status: debugDataflowStatus(step.Status),
			ContentBytes: int64(len(step.Content)), ReasoningBytes: int64(len(step.Reasoning)),
		})
	}
	const maxToolUses = 2000
	toolCount := len(task.ToolUses)
	if toolCount > maxToolUses {
		toolCount = maxToolUses
		out.Anomalies = append(out.Anomalies, debugDataflowAnomaly{Code: "tool_uses_truncated", Severity: "warn", Evidence: "tool-use metadata exceeds the dump limit"})
	}
	for i, use := range task.ToolUses[:toolCount] {
		out.ToolUses = append(out.ToolUses, debugDataflowToolUse{
			Index: i + 1, Tool: debugDataflowLabel(use.Tool), Origin: debugDataflowLabel(use.Who),
			ChildSessionID: debugDataflowLabel(use.ChildSessionID), ChildNumber: use.ChildNumber,
			ArgsBytes: len(use.Args), ResultBytes: len(use.Result), HasResult: use.Result != "",
		})
	}
	for _, intent := range task.ToolExecutionIntents {
		out.ToolIntents = append(out.ToolIntents, debugDataflowToolIntent{
			CallID: intent.CallID, Tool: debugDataflowLabel(intent.Tool), State: debugDataflowStatus(intent.State),
		})
	}
	runStatus := task.Status
	a.mu.Unlock()

	out.Journal = readDebugDataflowJournal(a.dataPath, journalID, stepLabels, callRefs)
	for i := range out.ToolIntents {
		out.ToolIntents[i].CallID = debugDataflowCallRef(callRefs, out.ToolIntents[i].CallID)
	}
	incidents, _ := a.incidentSnapshot()
	for _, event := range incidents {
		if event.Session != sessionID && event.Task != runID {
			continue
		}
		out.Incidents = append(out.Incidents, debugDataflowIncident{
			ID: debugDataflowLabel(event.ID), Time: event.Time, Module: debugDataflowLabel(event.Module), Phase: debugDataflowLabel(event.Phase),
			Code: debugDataflowLabel(event.Code), Severity: debugDataflowLabel(event.Severity), Retryable: event.Retryable,
			CauseTypes: debugDataflowLabels(event.CauseTypes), Locations: debugDataflowLocations(event.Locations),
			Plugin: debugDataflowLabel(event.Plugin), PluginVersion: debugDataflowLabel(event.PluginVersion), Tool: debugDataflowLabel(event.Tool),
			PanicType: debugDataflowLabel(event.PanicType), DurationMS: event.DurationMS, HTTPStatus: event.Status,
			PluginFault: sanitizedPluginFault(event.PluginFault),
		})
		out.Timeline = append(out.Timeline, debugDataflowTimeline{At: event.Time, Source: "incident", Kind: debugDataflowLabel(event.Phase), Status: debugDataflowLabel(event.Severity), IncidentCode: debugDataflowLabel(event.Code), Tool: debugDataflowLabel(event.Tool)})
	}
	for _, event := range out.Journal.Events {
		status := ""
		switch event.Kind {
		case "model_request":
			status = "started"
		case "tool_dispatch":
			status = "dispatched"
		case "tool_result":
			status = "completed"
		}
		out.Timeline = append(out.Timeline, debugDataflowTimeline{
			At: event.At, Source: "execution_journal", Kind: event.Kind, Status: status,
			Sequence: event.Sequence, Step: event.Step, Tool: event.Tool, CallID: event.CallID,
		})
	}
	out.Anomalies = append(out.Anomalies, analyzeDebugDataflow(out, runStatus)...)
	sort.SliceStable(out.Incidents, func(i, j int) bool { return out.Incidents[i].Time < out.Incidents[j].Time })
	sort.SliceStable(out.Timeline, func(i, j int) bool { return out.Timeline[i].At < out.Timeline[j].At })
	// This endpoint is an explicit download. The payload contains only operational
	// metadata; prompts, provider bodies, tool arguments/results, and secrets stay out.
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="aide-session-dataflow.json"`)
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(out)
}

func debugDataflowLabels(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		out = append(out, debugDataflowLabel(value))
	}
	return out
}

func debugDataflowLocations(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		first := strings.Fields(value)
		if len(first) == 0 {
			continue
		}
		location := filepath.Base(first[0])
		parts := strings.Split(location, ":")
		if len(parts) != 2 || !strings.HasSuffix(parts[0], ".go") {
			continue
		}
		line := true
		for _, c := range parts[1] {
			if c < '0' || c > '9' {
				line = false
				break
			}
		}
		if line && parts[1] != "" {
			out = append(out, location)
		}
	}
	return out
}

func readDebugDataflowJournal(dataPath, taskID string, stepLabels, callRefs map[string]string) debugDataflowJournal {
	out := debugDataflowJournal{Events: []debugDataflowJournalEvent{}}
	path := filepath.Join(dataPath, "execution-events", taskID+".jsonl")
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return out
	}
	if err != nil {
		out.Warning = "journal metadata could not be read"
		return out
	}
	if !info.Mode().IsRegular() || info.Size() > 64<<20 {
		out.Warning = "journal is not a regular file or exceeds the 64 MiB read bound"
		return out
	}
	f, err := os.Open(path)
	if err != nil {
		out.Warning = "journal could not be opened"
		return out
	}
	defer f.Close()
	out.Available = true
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 4096), 64*1024)
	for scanner.Scan() {
		if len(out.Events) >= debugDataflowJournalLimit {
			out.Truncated = true
			out.Warning = "journal event count exceeds the 5000-event dump bound"
			break
		}
		var event ExecutionEvent
		if json.Unmarshal(scanner.Bytes(), &event) != nil || event.TaskID != taskID || event.Version != 1 {
			out.Warning = "journal contains an incomplete or invalid record"
			break
		}
		stepLabel := stepLabels[event.Step]
		if stepLabel == "" && event.Step != "" {
			stepLabel = "step-unknown"
		}
		out.Events = append(out.Events, debugDataflowJournalEvent{
			Sequence: event.Sequence, Kind: debugDataflowEventKind(event.Kind), At: event.At,
			Step: stepLabel, CallID: debugDataflowCallRef(callRefs, event.CallID),
			Tool: debugDataflowLabel(event.Tool), HasDigest: event.Digest != "",
		})
	}
	if scanner.Err() != nil {
		out.Warning = "journal scan failed or contains a record exceeding 64 KiB"
	}
	return out
}

func debugDataflowEventKind(value string) string {
	switch value {
	case "model_request", "model_retry", "tool_dispatch", "tool_result", "model_continuation":
		return value
	default:
		return "unknown"
	}
}

func analyzeDebugDataflow(dump debugDataflowDump, runStatus string) []debugDataflowAnomaly {
	out := []debugDataflowAnomaly{}
	terminal := debugDataflowTerminal(runStatus)
	for _, call := range dump.ProviderCalls {
		if terminal && call.Status == "incomplete" {
			out = append(out, debugDataflowAnomaly{Code: "provider_request_incomplete", Severity: "error", Evidence: "a persisted provider request has no completion timestamp although the run is terminal"})
		}
		if call.Status == "failed" {
			severity := "warn"
			if dump.Run.Status == "failed" {
				severity = "error"
			}
			if call.FailureCode == "CANCELED" {
				severity = "info"
			}
			out = append(out, debugDataflowAnomaly{Code: "provider_request_failed", Severity: severity, Evidence: "provider request failed with classification " + call.FailureCode})
		}
		if call.Status == "empty_response" {
			out = append(out, debugDataflowAnomaly{Code: "provider_empty_response", Severity: "warn", Evidence: "provider returned an empty response"})
		}
	}
	if dump.Run.Status == "failed" {
		out = append(out, debugDataflowAnomaly{Code: "run_failed", Severity: "error", Evidence: "run status is failed; inspect linked incident classifications"})
	}
	if dump.Journal.Warning != "" {
		out = append(out, debugDataflowAnomaly{Code: "execution_journal_incomplete", Severity: "warn", Evidence: dump.Journal.Warning})
	}
	if !dump.Journal.Available && dump.Run.ExecutionSequence > 0 {
		out = append(out, debugDataflowAnomaly{Code: "execution_journal_missing", Severity: "warn", Evidence: "execution sequence is nonzero but no journal file is available"})
	}
	dispatched, results := map[string]bool{}, map[string]bool{}
	var previousSequence uint64
	modelRequests := 0
	for index, event := range dump.Journal.Events {
		if index > 0 && event.Sequence != previousSequence+1 {
			out = append(out, debugDataflowAnomaly{Code: "execution_journal_sequence_gap", Severity: "warn", Evidence: "execution journal event sequence is not contiguous"})
		}
		previousSequence = event.Sequence
		if event.CallID == "" {
			if event.Kind == "model_request" {
				modelRequests++
			}
			continue
		}
		switch event.Kind {
		case "model_request":
			modelRequests++
		case "tool_dispatch":
			dispatched[event.CallID] = true
		case "tool_result":
			results[event.CallID] = true
		}
	}
	if !dump.Run.SnapshotsTruncated && modelRequests > len(dump.ProviderCalls) {
		out = append(out, debugDataflowAnomaly{Code: "provider_snapshot_count_mismatch", Severity: "warn", Evidence: "execution journal has more model requests than persisted provider snapshots"})
	}
	if terminal && dump.Journal.Available && dump.Journal.Warning == "" && !dump.Journal.Truncated && previousSequence != dump.Run.ExecutionSequence {
		out = append(out, debugDataflowAnomaly{Code: "execution_journal_sequence_mismatch", Severity: "warn", Evidence: "terminal run sequence does not match the last persisted journal event"})
	}
	if terminal {
		callIDs := make([]string, 0, len(dispatched))
		for callID := range dispatched {
			callIDs = append(callIDs, callID)
		}
		sort.Strings(callIDs)
		for _, callID := range callIDs {
			if !results[callID] {
				out = append(out, debugDataflowAnomaly{Code: "tool_result_missing", Severity: "error", Evidence: "a dispatched tool call has no persisted result"})
			}
		}
		for _, intent := range dump.ToolIntents {
			if intent.State == "dispatched" && !results[intent.CallID] {
				out = append(out, debugDataflowAnomaly{Code: "tool_outcome_unknown", Severity: "error", Evidence: "a tool intent remains dispatched without a confirmed completion"})
			}
		}
	}
	for _, event := range dump.Incidents {
		if event.Severity == "error" {
			out = append(out, debugDataflowAnomaly{Code: "incident_error", Severity: "error", Evidence: "linked incident " + event.Code + " in " + event.Module + "/" + event.Phase})
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Code == out[j].Code {
			return out[i].Evidence < out[j].Evidence
		}
		return out[i].Code < out[j].Code
	})
	return out
}
