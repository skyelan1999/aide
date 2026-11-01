package server

import (
	"aide/internal/execsandbox"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type largeBenchGroup struct {
	ID          string           `json:"id"`
	Integration bool             `json:"integration"`
	Passed      int              `json:"passed"`
	Total       int              `json:"total"`
	Complete    bool             `json:"complete"`
	Failures    []map[string]any `json:"failures"`
}
type largeBenchScore struct {
	SyntaxPassed int               `json:"syntaxPassed"`
	SyntaxTotal  int               `json:"syntaxTotal"`
	SyntaxMillis int64             `json:"syntaxMillis"`
	Passed       int               `json:"passed"`
	Total        int               `json:"total"`
	Complete     bool              `json:"complete"`
	Groups       []largeBenchGroup `json:"groups"`
}
type largeBenchCheck struct {
	Suite         string          `json:"suite"`
	Environment   string          `json:"environment"`
	ElapsedMillis int64           `json:"elapsedMillis"`
	Score         largeBenchScore `json:"score"`
}
type largeBenchController struct {
	mu        sync.Mutex
	a         *App
	sessionID string
	scale     string
	oracle    []byte
	key       string
	checks    []largeBenchCheck
	root      string
}

// Optional test-only timing observers. Normal product code has no dependency
// on benchmark tracing, and no prompts, credentials or tool payloads are logged.
var largeBenchAppObserver func(*App)
var largeBenchVerifyObserver func(*App, string, string, bool) func()

func largeBenchFixturePath(t *testing.T) string {
	t.Helper()
	dir, _ := os.Getwd()
	for i := 0; i < 10; i++ {
		p := filepath.Join(dir, "scripts", "benchmarks", "luna-large-workload.cjs")
		if _, e := os.Stat(p); e == nil {
			return p
		}
		dir = filepath.Dir(dir)
	}
	t.Fatal("large workload fixture missing")
	return ""
}
func largeBenchNode(ctx context.Context, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "node", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("node verifier: %w: %s", err, out)
	}
	return out, nil
}

// The checker runs candidate code in an isolated temporary tree and restricted
// VM module loader. Main proposals are overlaid without applying user files.
// Workers use their actual scratch files. Both modes use the exact same oracle.
func (c *largeBenchController) verify(ctx context.Context, source, suite string, holdout bool, overlay []Change) (largeBenchScore, error) {
	if largeBenchVerifyObserver != nil {
		defer largeBenchVerifyObserver(c.a, source, suite, holdout)()
	}
	var score largeBenchScore
	dir, err := os.MkdirTemp(c.root, "check-")
	if err != nil {
		return score, err
	}
	defer os.RemoveAll(dir)
	project := filepath.Join(dir, "project")
	if err = os.MkdirAll(filepath.Join(project, "src"), 0700); err != nil {
		return score, err
	}
	root, err := os.OpenRoot(source)
	if err != nil {
		return score, err
	}
	defer root.Close()
	entries, err := fsReadBenchDir(root, "src")
	if err != nil {
		return score, err
	}
	for _, name := range entries {
		if !strings.HasSuffix(name, ".js") {
			continue
		}
		b, e := root.ReadFile("src/" + name)
		if e != nil {
			return score, e
		}
		if e = os.WriteFile(filepath.Join(project, "src", name), b, 0600); e != nil {
			return score, e
		}
	}
	for _, f := range overlay {
		if f.Source != "" || !strings.HasPrefix(f.Path, "src/") || filepath.Dir(f.Path) != "src" || !strings.HasSuffix(f.Path, ".js") {
			continue
		}
		if err = os.WriteFile(filepath.Join(project, filepath.FromSlash(f.Path)), []byte(f.Content), 0600); err != nil {
			return score, err
		}
	}
	if err = os.WriteFile(filepath.Join(dir, "oracle.cjs"), c.oracle, 0600); err != nil {
		return score, err
	}
	mode := "standard"
	if holdout {
		mode = "holdout"
	}
	command := "node " + shellQuote(filepath.Join(dir, "oracle.cjs")) + " verify " + shellQuote(project) + " " + shellQuote(c.scale) + " " + shellQuote(suite) + " " + mode
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	cmd, cleanup, err := execsandbox.Command(ctx, "workspace-write", dir, command)
	if err != nil {
		return score, err
	}
	defer cleanup()
	configurePluginProcess(cmd, true)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return score, fmt.Errorf("isolated checker: %w: %.1500s", err, out)
	}
	err = json.Unmarshal(out, &score)
	return score, err
}
func fsReadBenchDir(root *os.Root, dir string) ([]string, error) {
	f, e := root.Open(dir)
	if e != nil {
		return nil, e
	}
	defer f.Close()
	entries, e := f.ReadDir(-1)
	var out []string
	for _, d := range entries {
		if d.Type().IsRegular() {
			out = append(out, d.Name())
		}
	}
	return out, e
}

func (c *largeBenchController) serve(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" || r.Header.Get("X-Benchmark-Key") != c.key {
		http.Error(w, "not authorized", 403)
		return
	}
	var q struct {
		Workspace string `json:"workspace"`
		Suite     string `json:"suite"`
	}
	if json.NewDecoder(io.LimitReader(r.Body, 16384)).Decode(&q) != nil {
		http.Error(w, "invalid request", 400)
		return
	}
	_, _ = io.Copy(io.Discard, r.Body)
	if q.Suite == "" {
		q.Suite = "all"
	}
	if q.Suite != "all" && (strings.ContainsAny(q.Suite, "/\\.\x00") || len(q.Suite) > 64) {
		http.Error(w, "invalid suite", 400)
		return
	}
	var overlay []Change
	kind := "main-candidates"
	c.a.mu.Lock()
	session := c.a.sessions[c.sessionID]
	allowed := q.Workspace == c.a.workPath
	if allowed && session != nil {
		for _, task := range session.Runs {
			overlay = append(overlay, cloneExecutionValue(task.Files)...)
		}
	}
	if !allowed && session != nil {
		// A checker cannot turn a model-supplied arbitrary host path into read access.
		for _, task := range session.Runs {
			if task.Environments == nil {
				continue
			}
			for _, env := range task.Environments.Results {
				if filepath.Base(q.Workspace) == "work" && filepath.Base(filepath.Dir(q.Workspace)) == env.ID && strings.HasPrefix(filepath.Base(filepath.Dir(filepath.Dir(q.Workspace))), "aide-environments-") && filepath.Dir(filepath.Dir(filepath.Dir(q.Workspace))) == os.TempDir() {
					allowed = true
					kind = env.ID
				}
			}
		}
	}
	c.a.mu.Unlock()
	if !allowed {
		http.Error(w, "workspace is outside this benchmark run", 403)
		return
	}
	start := time.Now()
	score, err := c.verify(r.Context(), q.Workspace, q.Suite, false, overlay)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	c.mu.Lock()
	c.checks = append(c.checks, largeBenchCheck{Suite: q.Suite, Environment: kind, ElapsedMillis: time.Since(start).Milliseconds(), Score: score})
	c.mu.Unlock()
	jsonOut(w, 200, score)
}

func TestLargeEngineeringFixtureOracle(t *testing.T) {
	fixture := largeBenchFixturePath(t)
	oracle, e := os.ReadFile(fixture)
	if e != nil {
		t.Fatal(e)
	}
	for _, scale := range []string{"medium", "large"} {
		t.Run(scale, func(t *testing.T) {
			dir := t.TempDir()
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if _, e := largeBenchNode(ctx, fixture, "self-test"); e != nil {
				t.Fatal(e)
			}
			if _, e := largeBenchNode(ctx, fixture, "setup", dir, scale); e != nil {
				t.Fatal(e)
			}
			c := &largeBenchController{root: t.TempDir(), oracle: oracle, scale: scale}
			broken, e := c.verify(ctx, dir, "all", false, nil)
			if e != nil || broken.Passed != 0 {
				t.Fatal("baseline did not expose all missing functionality", broken, e)
			}
			code := `const f=require(process.argv[1]),fs=require('fs'),path=require('path');for(const m of f.chosen(process.argv[3]))fs.writeFileSync(path.join(process.argv[2],'src',m.id+'.js'),'module.exports = '+m.source+';');`
			if _, e := largeBenchNode(ctx, "-e", code, fixture, dir, scale); e != nil {
				t.Fatal(e)
			}
			fixed, e := c.verify(ctx, dir, "all", false, nil)
			if e != nil || !fixed.Complete {
				t.Fatal("reference failed", fixed, e)
			}
			holdout, e := c.verify(ctx, dir, "all", true, nil)
			if e != nil || !holdout.Complete {
				t.Fatal("holdout reference failed", holdout, e)
			}
			t.Logf("%s modules=%d behavior=%d holdout=%d", scale, fixed.SyntaxTotal, fixed.Total, holdout.Total)
		})
	}
}

func TestLargeEngineeringVerifierUsesCommonJSRealm(t *testing.T) {
	fixture := largeBenchFixturePath(t)
	oracle, e := os.ReadFile(fixture)
	if e != nil {
		t.Fatal(e)
	}
	dir := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if _, e := largeBenchNode(ctx, fixture, "setup", dir, "medium"); e != nil {
		t.Fatal(e)
	}
	code := `const f=require(process.argv[1]),fs=require('fs'),path=require('path');for(const m of f.chosen('medium')){let s=m.source;if(['inventory','idempotency'].includes(m.id))s=s.replace('function(x){','function(x){if(Object.getPrototypeOf(x)!==Object.prototype)throw Error("wrong realm");');fs.writeFileSync(path.join(process.argv[2],'src',m.id+'.js'),'module.exports = '+s+';');}`
	if _, e := largeBenchNode(ctx, "-e", code, fixture, dir); e != nil {
		t.Fatal(e)
	}
	c := &largeBenchController{root: t.TempDir(), oracle: oracle, scale: "medium"}
	score, e := c.verify(ctx, dir, "all", false, nil)
	if e != nil || !score.Complete {
		t.Fatal("valid native JSON/prototype checks falsely rejected", score, e)
	}
	// The realm fix must retain mutation detection, including integration calls.
	mutate := `const f=require(process.argv[1]),fs=require('fs'),path=require('path');const m=f.modules.find(m=>m.id==='decimal');fs.writeFileSync(path.join(process.argv[2],'src','decimal.js'),'module.exports = '+m.source.replace('function(x){','function(x){x.mutated=true;')+';');`
	if _, e := largeBenchNode(ctx, "-e", mutate, fixture, dir); e != nil {
		t.Fatal(e)
	}
	bad, e := c.verify(ctx, dir, "decimal", false, nil)
	if e != nil || bad.Passed != 0 {
		t.Fatal("input mutation was hidden by JSON realm marshalling", bad, e)
	}
}

func TestLargeEngineeringReportProposal(t *testing.T) {
	a := testApp(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if _, e := largeBenchNode(ctx, largeBenchFixturePath(t), "setup", a.workPath, "medium"); e != nil {
		t.Fatal(e)
	}
	s := createSession(t, a)
	task := &Task{ID: newID(), Status: "awaiting_approval", WorkspaceID: a.wsID(), WorkspaceRev: a.wsRevision, WorkspaceMode: "local"}
	a.mu.Lock()
	a.sessions[s.ID].Runs = append(a.sessions[s.ID].Runs, task)
	a.mu.Unlock()
	versions := map[string]Change{}
	a.executeToolCall(ctx, readCall("read_file", `{"path":"REPORT.md"}`), task, versions)
	result := a.executeToolCall(ctx, readCall("write_file", `{"path":"REPORT.md","content":"# Report\nTested project implementation."}`), task, versions)
	if len(task.Files) != 1 || task.Files[0].Source != "" || task.Files[0].Applied {
		t.Fatal("project report did not become an ordinary pending proposal", result)
	}
	if b, e := a.workspace.ReadFile("REPORT.md"); e != nil || len(b) != 0 {
		t.Fatal("empty baseline report was applied early", e)
	}
	requireStatus(t, request(a, "POST", "/api/sessions/"+s.ID+"/runs/"+task.ID+"/apply", nil), 200)
	if b, e := a.workspace.ReadFile("REPORT.md"); e != nil || !strings.Contains(string(b), "Tested project") {
		t.Fatal("normal apply did not produce project report", e)
	}
}

func TestLargeEngineeringLiveLuna(t *testing.T) {
	data := os.Getenv("AIDE_LARGE_LIVE_DATA")
	if data == "" {
		t.Skip("explicit paid Luna opt-in required")
	}
	fixture := largeBenchFixturePath(t)
	oracle, e := os.ReadFile(fixture)
	if e != nil {
		t.Fatal(e)
	}
	digest := sha256.Sum256(oracle)
	b, e := os.ReadFile(filepath.Join(data, "config", "settings.json"))
	if e != nil {
		t.Fatal("settings unavailable")
	}
	var stored Settings
	if json.Unmarshal(b, &stored) != nil {
		t.Fatal("settings invalid")
	}
	access, e := os.ReadFile(filepath.Join(data, "auth", "access-token"))
	if e != nil {
		t.Fatal("credential envelope unavailable")
	}
	v := &SecretVault{path: filepath.Join(data, "secrets", "vault.enc"), entries: map[string]VaultEntry{}}
	if v.Load() != nil {
		t.Fatal("vault unavailable")
	}
	key := sha256.Sum256([]byte(strings.TrimSpace(string(access))))
	v.Unlock(key[:])
	defer v.Lock()
	type choice struct {
		scale    string
		capacity int
		minutes  int
	}
	var proof []map[string]any
	choices := []choice{{"medium", 1, 25}, {"medium", 5, 25}, {"large", 1, 65}, {"large", 10, 65}}
	if strings.Contains(","+os.Getenv("AIDE_LARGE_LIVE_ONLY")+",", ",medium-10X,") {
		choices = append(choices, choice{"medium", 10, 25})
	}
	for _, selection := range choices {
		name := fmt.Sprintf("%s-%dX", selection.scale, selection.capacity)
		if only := os.Getenv("AIDE_LARGE_LIVE_ONLY"); only != "" && !strings.Contains(","+only+",", ","+name+",") {
			continue
		}
		t.Run(name, func(t *testing.T) {
			a := testApp(t)
			if largeBenchAppObserver != nil {
				largeBenchAppObserver(a)
			}
			a.vault = v
			a.profileState.Profiles = append(a.profileState.Profiles, Profile{ID: "large-bench", Name: "Large benchmark", Params: ProfileParams{Temperature: fp(0.2), TopP: fp(0.9), MaxTokens: 32768, ResponseFormat: "text"}})
			a.settings = Settings{Models: cloneExecutionValue(stored.Models), ModelProviders: cloneExecutionValue(stored.ModelProviders), BaseURL: stored.BaseURL, Model: "gpt-6-luna", ActiveModel: "gpt-6-luna", ReasoningEffort: "high", SandboxMode: "workspace-write", ToolMaxRounds: 200}
			for _, schema := range builtinTools {
				n := pluginSchemaName(schema)
				switch n {
				case "read_file", "list_files", "write_file", "update_plan":
				default:
					a.settings.DisabledTools = append(a.settings.DisabledTools, n)
				}
			}
			policy := defaultExecutionPolicy(200)
			policy.ChatBudgetSec = selection.minutes * 60
			if e := atomicJSON(a.executionPolicyPath(), policy); e != nil {
				t.Fatal(e)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			setup, e := largeBenchNode(ctx, fixture, "setup", a.workPath, selection.scale)
			if e != nil {
				t.Fatal(e)
			}
			initialSources, _ := fsReadBenchDir(a.workspace, "src")
			allowedSources := map[string]bool{}
			for _, name := range initialSources {
				allowedSources["src/"+name] = true
			}
			s := createSession(t, a)
			c := &largeBenchController{a: a, sessionID: s.ID, scale: selection.scale, oracle: oracle, key: newID() + newID(), root: t.TempDir()}
			verifier := httptest.NewServer(http.HandlerFunc(c.serve))
			defer verifier.Close()
			plugin := `module.exports={apply(ctx){ctx.tool({name:'bench_check',description:'Run independent syntax and behavioral regression against a module id or all. On main this tests ALL current pending write_file proposals over baseline WITHOUT applying them. In a temporary environment tests its actual private files. Returns exact passed/total, failures and cross-module integration. Verifier is immutable. No artificial delay.',scheduling:{resources:[{root:'workspace',path:'src',access:'read'}]},parameters:{type:'object',properties:{suite:{type:'string'}},required:['suite']},handler:async(args,api)=>{const r=await fetch(ctx.settings.url,{method:'POST',headers:{'Content-Type':'application/json','X-Benchmark-Key':ctx.settings.key},body:JSON.stringify({workspace:api.workspaceRoot,suite:args.suite||'all'})});const s=await r.text();if(!r.ok)throw Error(s);return JSON.parse(s)}})}};`
			planInstall(t, a, "engineering-checker", plugin)
			a.mu.Lock()
			for i := range a.pluginRegistry.Plugins {
				if a.pluginRegistry.Plugins[i].ID == "engineering-checker" {
					a.pluginRegistry.Plugins[i].Settings = map[string]any{"url": verifier.URL, "key": c.key}
				}
			}
			a.mu.Unlock()
			a.runPluginHost(ctx)
			prompt := `Implement the complete order fulfilment, settlement and operational consistency library in this workspace. Read README.md and the existing empty project file REPORT.md, then each relevant contracts/*.md and existing src/*.js. Implement every required module, including cross-module integration. Preserve contracts/README and export APIs. Pure synchronous functions, no external I/O, time or third-party imports; never mutate inputs. Existing source files must be read before write_file. Use bench_check (suite=module id or all) for REAL syntax and behavioral regression; on main it tests pending proposals without approving/applying them, so you CAN iterate using write_file. In temporary environments it tests their actual files. Tests and oracle cannot be edited. Do not return until all required contracts are implemented and the whole library passes, or give precise remaining failures if blocked. Use coherent dynamic decomposition and useful independent environments where capacity permits; do not pad to fill slots or fix a role per slot. Workers may load enabled engineering-checker with environment_load_plugin and exchange evidence. Main owns integration, conflicts, collection and normal pending-proposal registration. Leave main changes pending for the harness to apply through the normal API. Update the existing project file REPORT.md using write_file WITHOUT source, describing actual evidence, coverage and unresolved limitations, with no invented test claims. REPORT.md belongs in this project; it is not a new system-docs deliverable. Do not publish or deploy.`
			if os.Getenv("AIDE_LARGE_LIVE_WORK_QUEUE") == "1" && selection.capacity > 1 {
				// Explicit experimental strategy, labelled separately from free-choice
				// runs. Same code contracts and hidden correctness checks, no truth leak.
				prompt += "\nFor this work-queue strategy experiment, enqueue the independent non-integration modules as separate small assignments using environment_submit_batch, rather than one multi-module shard per slot. More jobs than worker slots are intentional; slots refill automatically. Choose the batches and integration order yourself. Main owns cross-module integration/REPORT and checks; collect whichever jobs finish first with environment_collect_ready and resolve errors individually. This is not a requirement for other user projects."
			}
			if os.Getenv("AIDE_LARGE_LIVE_ALL_SLOTS") == "1" && selection.capacity > 1 {
				prompt += "\nFor this explicit all-slots queue experiment, use environment_run_queue with order=random and merge=true to run the independent non-integration modules as separate assignments in one useful backlog. Main inference pauses while code dispatches and collects the queue using all X slots. Worker read_file evidence covers their assigned files; main should inspect integration/conflict scopes and validate all candidates with bench_check, rather than re-read original stubs merely to reconstruct progress. Keep integration modules and REPORT on main after the queue. This is a labelled strategy experiment, not a fixed decomposition policy for other projects."
			}
			started := time.Now()
			deadline := started.Add(time.Duration(selection.minutes) * time.Minute)
			entry := map[string]any{"name": name, "scale": selection.scale, "capacity": selection.capacity, "model": "gpt-6-luna", "reasoning": "high", "maxTokens": 32768, "runtimeVariant": "scheduler-receipt-compact-collect", "fixtureSHA256": hex.EncodeToString(digest[:]), "setup": json.RawMessage(setup), "startedAt": started.UTC().Format(time.RFC3339Nano)}
			proof = append(proof, entry)
			saveProof := func() {
				if p := os.Getenv("AIDE_LARGE_LIVE_PROOF"); p != "" {
					if e := atomicJSON(p, proof); e != nil {
						t.Error(e)
					}
				}
			}
			defer saveProof()
			saveProof()
			var iterations []map[string]any
			var snapshots []*Task
			for attempt := 0; attempt < 3 && time.Now().Before(deadline); attempt++ {
				w := request(a, "POST", "/api/sessions/"+s.ID+"/runs", map[string]any{"mode": "chat", "prompt": prompt, "parallelism": selection.capacity, "modelSelection": SessionModelSelection{Strategy: "manual", Profile: "large-bench", Model: "gpt-6-luna", ReasoningEffort: "high"}})
				if w.Code != 202 {
					entry["startError"] = w.Body.String()
					break
				}
				lastProgress := time.Time{}
				var task *Task
				for time.Now().Before(deadline) {
					a.mu.Lock()
					live := a.sessions[s.ID]
					task = live.Runs[len(live.Runs)-1]
					state := task.Status
					files := len(task.Files)
					tools := len(task.ToolUses)
					envs := cloneExecutionValue(task.Environments)
					a.mu.Unlock()
					if time.Since(lastProgress) >= 30*time.Second {
						lastProgress = time.Now()
						c.mu.Lock()
						checks := len(c.checks)
						c.mu.Unlock()
						t.Logf("PROGRESS %s attempt=%d elapsed=%s state=%s proposals=%d tools=%d checkerCalls=%d", name, attempt+1, time.Since(started).Round(time.Second), state, files, tools, checks)
						entry["progress"] = map[string]any{"elapsedMillis": time.Since(started).Milliseconds(), "state": state, "proposals": files, "tools": tools, "checkerCalls": checks, "environments": envs}
						saveProof()
					}
					if state != "running" {
						break
					}
					time.Sleep(200 * time.Millisecond)
				}
				a.mu.Lock()
				snapshot := cloneExecutionValue(task)
				if task != nil && task.Status == "running" {
					if stop := a.cancels[task.ID]; stop != nil {
						stop()
					}
				}
				a.mu.Unlock()
				if snapshot == nil {
					break
				}
				snapshots = append(snapshots, snapshot)
				iteration := map[string]any{"attempt": attempt + 1, "elapsedMillis": time.Since(started).Milliseconds(), "status": snapshot.Status, "error": snapshot.Error, "usage": snapshot.Usage, "environments": snapshot.Environments, "proposals": len(snapshot.Files)}
				if len(snapshot.Steps) > 0 {
					iteration["answer"] = snapshot.Steps[len(snapshot.Steps)-1].Content
				}
				iterations = append(iterations, iteration)
				entry["iterations"] = iterations
				saveProof()
				if snapshot.Status != "awaiting_approval" && snapshot.Status != "completed" {
					break
				}
				invalid := []string{}
				for _, f := range snapshot.Files {
					if f.Source != "" || f.Path != "REPORT.md" && !allowedSources[f.Path] {
						invalid = append(invalid, f.Path)
					}
				}
				if len(invalid) > 0 {
					iteration["scopeViolations"] = invalid
					break
				}
				candidate, e := c.verify(context.Background(), a.workPath, "all", false, snapshot.Files)
				if e != nil {
					iteration["verificationError"] = e.Error()
					break
				}
				iteration["candidateScore"] = candidate
				if snapshot.Status == "awaiting_approval" {
					applied := request(a, "POST", "/api/sessions/"+s.ID+"/runs/"+snapshot.ID+"/apply", nil)
					if applied.Code != 200 {
						iteration["applyError"] = applied.Body.String()
						break
					}
				}
				holdout, e := c.verify(context.Background(), a.workPath, "all", true, nil)
				if e != nil {
					iteration["verificationError"] = e.Error()
					break
				}
				iteration["holdoutScore"] = holdout
				entry["finalScore"] = holdout
				report, reportErr := a.workspace.ReadFile("REPORT.md")
				reportPresent := reportErr == nil && len(strings.TrimSpace(string(report))) > 0
				entry["reportPresent"] = reportPresent
				entry["verifiedComplete"] = holdout.Complete && reportPresent
				if holdout.Complete && reportPresent {
					break
				}
				failed := []largeBenchGroup{}
				for _, g := range holdout.Groups {
					if !g.Complete {
						failed = append(failed, g)
					}
				}
				detail, _ := json.Marshal(failed)
				prompt = "Continue the same project. The harness applied your prior candidates ONLY in this disposable fixture, then independent holdout validation found remaining failures below. Read current files and repair missing/incorrect modules, preserve good work. Use bench_check for regression. Produce or update REPORT.md with actual evidence; it is a required deliverable. Keep final changes pending. No publishing. Actual remaining evidence:\n" + string(detail)
			}
			entry["elapsedMillis"] = time.Since(started).Milliseconds()
			entry["finishedAt"] = time.Now().UTC().Format(time.RFC3339Nano)
			entry["iterations"] = iterations
			var usage TokenUsage
			for _, task := range snapshots {
				usage = addUsage(usage, task.Usage)
			}
			entry["usage"] = usage
			c.mu.Lock()
			entry["checks"] = append([]largeBenchCheck(nil), c.checks...)
			c.mu.Unlock()
			saveProof()
			// Retain the actual generated candidate/report/source independently of
			// the ephemeral App so a long-run failure is still inspectable.
			if proofPath := os.Getenv("AIDE_LARGE_LIVE_PROOF"); proofPath != "" {
				outDir := strings.TrimSuffix(proofPath, filepath.Ext(proofPath)) + "-" + name
				_ = os.MkdirAll(outDir, 0700)
				for _, task := range snapshots {
					_ = atomicJSON(filepath.Join(outDir, task.ID+".json"), task)
				}
				// A failed connection may have already retained a baseline under
				// this case name. Never silently reuse/overwrite its O_EXCL tree.
				archiveID := newID()
				if len(snapshots) > 0 {
					archiveID = snapshots[len(snapshots)-1].ID
				}
				project := filepath.Join(outDir, "project-"+archiveID)
				if _, _, err := copyExecutionTree(context.Background(), a.workPath, project, true); err != nil {
					entry["artifactCopyError"] = err.Error()
					t.Error("candidate archive failed", err)
				} else {
					entry["artifactProject"] = project
				}
			}
			t.Logf("RESULT %s elapsed=%s verified=%v tokens=%d", name, time.Since(started).Round(time.Millisecond), entry["verifiedComplete"], usage.Total)
			if entry["verifiedComplete"] != true {
				t.Errorf("%s did not satisfy independent completion/accuracy acceptance; partial evidence retained", name)
			}
		})
	}
}
