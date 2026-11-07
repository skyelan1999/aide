package server

import (
	"strings"
	"time"
)

// OrchestrationEvent deliberately contains no prompts, tool arguments, file
// paths or model output. It remains useful when full request snapshots stop at
// their much smaller privacy/storage limit.
type OrchestrationEvent struct {
	Kind          string `json:"kind"`
	Stage         string `json:"stage"`
	StartedAt     string `json:"startedAt"`
	CompletedAt   string `json:"completedAt"`
	ElapsedMillis int64  `json:"elapsedMillis"`
	Outcome       string `json:"outcome"`
}

const maxOrchestrationEvents = 256

func orchestrationStage(kind, stage string) string {
	if kind == "model_request" {
		switch {
		case strings.HasPrefix(stage, "parallel/plan-"):
			return "parallel_plan"
		case strings.HasPrefix(stage, "parallel/wave-"):
			return "parallel_worker"
		case strings.HasPrefix(stage, "pw/"):
			return "professional_workflow"
		}
	}
	switch stage {
	case "chat", "agent", "plan", "propose", "review", "analysis", "environment", "active_context", "shell", "clarification", "run_shell", "read_file", "list_files", "write_file", "browser_read":
		return stage
	default:
		return "other"
	}
}

// Caller holds a.mu. Recording must never force a session write in the hot
// path; the next normal checkpoint persists the bounded ledger.
func (a *App) appendOrchestrationEventLocked(task *Task, kind, stage string, started, completed time.Time, outcome string) {
	if task == nil {
		return
	}
	if len(task.OrchestrationEvents) >= maxOrchestrationEvents {
		copy(task.OrchestrationEvents, task.OrchestrationEvents[1:])
		task.OrchestrationEvents = task.OrchestrationEvents[:maxOrchestrationEvents-1]
		task.OrchestrationDropped++
	}
	task.OrchestrationEvents = append(task.OrchestrationEvents, OrchestrationEvent{
		Kind: kind, Stage: orchestrationStage(kind, stage),
		StartedAt:     started.UTC().Format(time.RFC3339Nano),
		CompletedAt:   completed.UTC().Format(time.RFC3339Nano),
		ElapsedMillis: max(int64(0), completed.Sub(started).Milliseconds()),
		Outcome:       outcome,
	})
}

func (a *App) recordOrchestrationEvent(task *Task, kind, stage string, started time.Time, outcome string) {
	a.mu.Lock()
	a.appendOrchestrationEventLocked(task, kind, stage, started, time.Now(), outcome)
	a.mu.Unlock()
}

// OrchestrationReceipt gives both parallel implementations the same status
// vocabulary. A worker's successful return is never treated as validation.
type OrchestrationReceipt struct {
	Lane          string `json:"lane"`
	ScopeHash     string `json:"scopeHash"`
	Status        string `json:"status"`
	StartedAt     string `json:"startedAt,omitempty"`
	CompletedAt   string `json:"completedAt,omitempty"`
	ElapsedMillis int64  `json:"elapsedMillis,omitempty"`
	Collected     bool   `json:"collected,omitempty"`
	Proposed      bool   `json:"proposed,omitempty"`
	Verified      bool   `json:"verified"`
	ToolCount     int    `json:"toolCount,omitempty"`
	ChangeCount   int    `json:"changeCount,omitempty"`
}

type OrchestrationCost struct {
	Samples       int   `json:"samples"`
	AverageMillis int64 `json:"averageMillis"`
	MaximumMillis int64 `json:"maximumMillis"`
}

// The scheduler sees a small recent aggregate, not an expanding event log.
// Call under a.mu. Durations from overlapping kinds are not additive.
func recentOrchestrationCosts(task *Task) map[string]OrchestrationCost {
	if task == nil {
		return nil
	}
	const recentPerKind = 8
	sums := map[string]int64{}
	costs := map[string]OrchestrationCost{}
	for i := len(task.OrchestrationEvents) - 1; i >= 0; i-- {
		event := task.OrchestrationEvents[i]
		switch event.Kind {
		case "snapshot_copy", "queue_wait", "worker_run", "collect":
		default:
			continue
		}
		cost := costs[event.Kind]
		if cost.Samples >= recentPerKind {
			continue
		}
		cost.Samples++
		sums[event.Kind] += event.ElapsedMillis
		cost.AverageMillis = sums[event.Kind] / int64(cost.Samples)
		cost.MaximumMillis = max(cost.MaximumMillis, event.ElapsedMillis)
		costs[event.Kind] = cost
	}
	return costs
}

// Caller must hold a.mu or operate on an immutable task snapshot.
func orchestrationReceipts(task *Task) []OrchestrationReceipt {
	if task == nil {
		return nil
	}
	var out []OrchestrationReceipt
	if task.Parallel != nil {
		for _, part := range task.Parallel.Parts {
			proposed := false
			for _, change := range task.Files {
				if task.Parallel.DraftProposals > 0 && part.DraftPath != "" && change.Path == part.DraftPath {
					proposed = true
					break
				}
			}
			out = append(out, OrchestrationReceipt{
				Lane: "analysis", ScopeHash: hash([]byte(part.Title)), Status: part.Status,
				StartedAt: part.StartedAt, CompletedAt: part.CompletedAt,
				ElapsedMillis: part.ElapsedMillis,
				Proposed:      proposed,
				Verified:      false,
			})
		}
	}
	if task.Environments != nil {
		for _, result := range task.Environments.Results {
			started, startErr := time.Parse(time.RFC3339Nano, result.StartedAt)
			ended, endErr := time.Parse(time.RFC3339Nano, result.FinishedAt)
			var elapsed int64
			if startErr == nil && endErr == nil {
				elapsed = max(int64(0), ended.Sub(started).Milliseconds())
			}
			out = append(out, OrchestrationReceipt{
				Lane: "environment", ScopeHash: hash([]byte(result.Assignment)), Status: result.Status,
				StartedAt: result.StartedAt, CompletedAt: result.FinishedAt,
				ElapsedMillis: elapsed,
				Collected:     result.Collected, Proposed: len(result.MergedPaths) > 0,
				Verified: false, ToolCount: len(result.Tools), ChangeCount: len(result.Changes),
			})
		}
	}
	return out
}
