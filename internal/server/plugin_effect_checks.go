package server

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"
)

// Operator observations never replace a missing execution receipt. Keeping the
// original uncertain state also prevents a new check from enabling DAG replay.
type PluginEffectCheck struct {
	Conclusion string `json:"conclusion"`
	Evidence   string `json:"evidence,omitempty"`
	RecordedAt string `json:"recordedAt"`
	Source     string `json:"source"`
	Digest     string `json:"digest"`
}

func pluginEffectTaskStopped(status string) bool {
	switch status {
	case "paused", "interrupted", "completed", "failed", "cancelled":
		return true
	}
	return false
}

func (a *App) pluginEffectCheckAPI(w http.ResponseWriter, r *http.Request) {
	var req struct {
		PlanDigest string `json:"planDigest"`
		Expected   *int   `json:"expected"`
		Conclusion string `json:"conclusion"`
		Evidence   string `json:"evidence"`
	}
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10))
	d.DisallowUnknownFields()
	if err := d.Decode(&req); err != nil {
		fail(w, 400, err)
		return
	}
	if err := d.Decode(&struct{}{}); err != io.EOF {
		fail(w, 400, errors.New("expected one JSON object"))
		return
	}
	req.Evidence = strings.TrimSpace(req.Evidence)
	if req.Expected == nil || *req.Expected < 0 || len(req.PlanDigest) != 64 || len(req.Evidence) == 0 || len(req.Evidence) > 4096 {
		fail(w, 400, errors.New("plan digest, revision and 1-4096 evidence bytes required"))
		return
	}
	if req.Conclusion != "effect_observed" && req.Conclusion != "no_effect_observed" && req.Conclusion != "inconclusive" {
		fail(w, 400, errors.New("invalid operator conclusion"))
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	s := a.sessions[r.PathValue("id")]
	if s == nil || s.Deleted {
		fail(w, 404, errors.New("session not found"))
		return
	}
	for _, task := range s.Runs {
		if task == nil || task.ID != r.PathValue("run") {
			continue
		}
		for _, plan := range task.PluginPlans {
			if plan == nil || plan.ID != r.PathValue("plan") {
				continue
			}
			for i := range plan.Nodes {
				n := &plan.Nodes[i]
				if n.ID != r.PathValue("node") {
					continue
				}
				if !pluginEffectTaskStopped(task.Status) || plan.State == "running" || n.State != "uncertain" {
					fail(w, 409, errors.New("only stopped uncertain calls can be checked"))
					return
				}
				if req.PlanDigest != plan.Digest || *req.Expected != len(n.EffectChecks) {
					fail(w, 409, errors.New("node record changed; reload before recording"))
					return
				}
				if len(n.EffectChecks) >= 20 {
					fail(w, 409, errors.New("operator check history limit reached"))
					return
				}
				record := PluginEffectCheck{Conclusion: req.Conclusion, Evidence: req.Evidence, RecordedAt: time.Now().UTC().Format(time.RFC3339Nano), Source: "operator_observation"}
				b, _ := json.Marshal(record)
				record.Digest = hash(b)
				old := n.EffectChecks
				n.EffectChecks = append(append([]PluginEffectCheck(nil), old...), record)
				if err := a.save(s); err != nil {
					n.EffectChecks = old
					fail(w, 500, err)
					return
				}
				jsonOut(w, 200, map[string]any{"recorded": true, "revision": len(n.EffectChecks), "check": record})
				return
			}
		}
	}
	fail(w, 404, errors.New("plugin plan node not found"))
}
