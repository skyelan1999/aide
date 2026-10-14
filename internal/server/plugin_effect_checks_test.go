package server

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
)

const effectCheckURL = "/api/sessions/effect-session/runs/effect-run/plugin-plans/effect-plan/nodes/effect-node/checks"

func effectCheckFixture(t *testing.T) (*App, *Task, *PluginPlanNode) {
	t.Helper()
	a := testApp(t)
	n := PluginPlanNode{pluginPlanNodeSpec: pluginPlanNodeSpec{ID: "effect-node", Tool: "external", Arguments: map[string]any{"private": "secret-argument"}}, State: "uncertain", Error: "missing receipt"}
	task := &Task{ID: "effect-run", Status: "interrupted", HookRecoveryRequired: true, PluginPlans: []*PluginExecutionPlan{{ID: "effect-plan", Digest: strings.Repeat("a", 64), State: "interrupted", Nodes: []PluginPlanNode{n}}}}
	a.sessions["effect-session"] = &Session{ID: "effect-session", Title: "Operator check fixture", Runs: []*Task{task}}
	return a, task, &task.PluginPlans[0].Nodes[0]
}

func effectCheckInput(expected int) map[string]any {
	return map[string]any{"planDigest": strings.Repeat("a", 64), "expected": expected, "conclusion": "effect_observed", "evidence": "Manually read back fixture file; private-check-evidence"}
}

func TestPluginEffectCheckPersistenceAndTruthBoundaries(t *testing.T) {
	a, task, n := effectCheckFixture(t)
	requireStatus(t, request(a, "POST", effectCheckURL, effectCheckInput(0)), 200)
	if len(n.EffectChecks) != 1 || n.EffectChecks[0].Source != "operator_observation" || n.EffectChecks[0].RecordedAt == "" || len(n.EffectChecks[0].Digest) != 64 {
		t.Fatal(n.EffectChecks)
	}
	if n.State != "uncertain" || n.Result != "" || n.Error != "missing receipt" || !task.HookRecoveryRequired || task.Status != "interrupted" {
		t.Fatal("check changed execution truth or released recovery")
	}
	first := n.EffectChecks[0]
	req := effectCheckInput(1)
	req["conclusion"] = "inconclusive"
	req["evidence"] = "Second observation contradicts earlier check"
	requireStatus(t, request(a, "POST", effectCheckURL, req), 200)
	if n.EffectChecks[0] != first || len(n.EffectChecks) != 2 {
		t.Fatal("earlier check overwritten")
	}
	restarted, err := New(a.workPath, a.reference.Name(), a.dataPath)
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	got := restarted.sessions["effect-session"].Runs[0].PluginPlans[0].Nodes[0]
	if got.State != "uncertain" || len(got.EffectChecks) != 2 || got.EffectChecks[0] != first {
		t.Fatal("checks not restored", got)
	}
	snapshot, _, _, err := a.outcomeTaskSnapshot("effect-session", "effect-run")
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.PluginPlans[0].Nodes[0].EffectChecks[0].Evidence != "" {
		t.Fatal("private evidence copied to outcome")
	}
	n.EffectChecks[0].Conclusion = "no_effect_observed"
	if snapshot.PluginPlans[0].Nodes[0].EffectChecks[0].Conclusion != "effect_observed" {
		t.Fatal("snapshot aliases owner")
	}
	out := request(a, "GET", "/api/sessions/effect-session/runs/effect-run/outcome", nil)
	requireStatus(t, out, 200)
	if strings.Contains(out.Body.String(), "private-check-evidence") || strings.Contains(out.Body.String(), "secret-argument") {
		t.Fatal("outcome leaks private evidence")
	}
	var data struct {
		Summary struct {
			Unknown int `json:"unknownOutcomes"`
		} `json:"summary"`
		Checks []any `json:"operatorChecks"`
	}
	if err := json.Unmarshal(out.Body.Bytes(), &data); err != nil {
		t.Fatal(err)
	}
	if data.Summary.Unknown != 1 || len(data.Checks) != 2 {
		t.Fatal("operator checks invented execution success", data)
	}
}

func TestPluginEffectCheckRejectsStaleInvalidAndActive(t *testing.T) {
	a, task, n := effectCheckFixture(t)
	for _, tc := range []struct {
		name   string
		change func(map[string]any)
		code   int
	}{
		{"missing revision", func(x map[string]any) { delete(x, "expected") }, 400},
		{"negative revision", func(x map[string]any) { x["expected"] = -1 }, 400},
		{"wrong digest", func(x map[string]any) { x["planDigest"] = strings.Repeat("b", 64) }, 409},
		{"stale revision", func(x map[string]any) { x["expected"] = 1 }, 409},
		{"empty evidence", func(x map[string]any) { x["evidence"] = " \n " }, 400},
		{"oversize evidence", func(x map[string]any) { x["evidence"] = strings.Repeat("x", 4097) }, 400},
		{"forged source", func(x map[string]any) { x["source"] = "system_verified" }, 400},
		{"unsupported verdict", func(x map[string]any) { x["conclusion"] = "succeeded" }, 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input := effectCheckInput(0)
			tc.change(input)
			requireStatus(t, request(a, "POST", effectCheckURL, input), tc.code)
		})
	}
	for _, status := range []string{"running", "resumed", "awaiting_clarification", "awaiting_approval", "unknown"} {
		task.Status = status
		requireStatus(t, request(a, "POST", effectCheckURL, effectCheckInput(0)), 409)
	}
	task.Status = "interrupted"
	task.PluginPlans[0].State = "running"
	requireStatus(t, request(a, "POST", effectCheckURL, effectCheckInput(0)), 409)
	task.PluginPlans[0].State = "interrupted"
	n.State = "succeeded"
	requireStatus(t, request(a, "POST", effectCheckURL, effectCheckInput(0)), 409)
	n.State = "uncertain"
	requireStatus(t, request(a, "POST", strings.Replace(effectCheckURL, "effect-run", "other-run", 1), effectCheckInput(0)), 404)
	requireStatus(t, request(a, "POST", strings.Replace(effectCheckURL, "effect-session", "other-session", 1), effectCheckInput(0)), 404)
	unauth := httptest.NewRequest("POST", effectCheckURL, strings.NewReader(`{}`))
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, unauth)
	requireStatus(t, w, 401)
	if len(n.EffectChecks) != 0 {
		t.Fatal("rejected request mutated checks")
	}
}

func TestPluginEffectCheckConcurrencySaveFailureAndLimit(t *testing.T) {
	a, _, n := effectCheckFixture(t)
	var wg sync.WaitGroup
	codes := make(chan int, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); codes <- request(a, "POST", effectCheckURL, effectCheckInput(0)).Code }()
	}
	wg.Wait()
	close(codes)
	counts := map[int]int{}
	for code := range codes {
		counts[code]++
	}
	if counts[200] != 1 || counts[409] != 1 || len(n.EffectChecks) != 1 {
		t.Fatal("concurrent check lost revision", counts)
	}
	s := a.sessions["effect-session"]
	file := SessionPath(a.dataPath, s.ID, sessionBucketFor(s))
	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(file, 0700); err != nil {
		t.Fatal(err)
	}
	requireStatus(t, request(a, "POST", effectCheckURL, effectCheckInput(1)), 500)
	if len(n.EffectChecks) != 1 {
		t.Fatal("failed persistence left committed-looking memory")
	}
	n.EffectChecks = make([]PluginEffectCheck, 20)
	requireStatus(t, request(a, "POST", effectCheckURL, effectCheckInput(20)), 409)
}
