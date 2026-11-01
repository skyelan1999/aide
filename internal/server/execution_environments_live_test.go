package server

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Paid, explicit opt-in. Credentials are read from the local envelope; the
// workload, sessions, plugins, approvals and applied files are synthetic only.
func TestExecutionEnvironmentsLiveLuna(t *testing.T) {
	data := os.Getenv("AIDE_ENVIRONMENT_LIVE_DATA")
	if data == "" {
		t.Skip("explicit Luna live opt-in required")
	}
	b, err := os.ReadFile(filepath.Join(data, "config", "settings.json"))
	if err != nil {
		t.Fatal("model settings unavailable")
	}
	var stored Settings
	if json.Unmarshal(b, &stored) != nil {
		t.Fatal("invalid model settings")
	}
	access, err := os.ReadFile(filepath.Join(data, "auth", "access-token"))
	if err != nil {
		t.Fatal("credential envelope unavailable")
	}
	v := &SecretVault{path: filepath.Join(data, "secrets", "vault.enc"), entries: map[string]VaultEntry{}}
	if v.Load() != nil {
		t.Fatal("vault unavailable")
	}
	key := sha256.Sum256([]byte(strings.TrimSpace(string(access))))
	v.Unlock(key[:])
	defer v.Lock()
	var proof []map[string]any
	for _, capacity := range []int{1, 10} {
		t.Run(fmt.Sprintf("%dX", capacity), func(t *testing.T) {
			a := testApp(t)
			a.vault = v
			a.settings = Settings{Models: cloneExecutionValue(stored.Models), ModelProviders: cloneExecutionValue(stored.ModelProviders), BaseURL: stored.BaseURL, Model: "gpt-6-luna", ActiveModel: "gpt-6-luna", ReasoningEffort: "low", SandboxMode: "workspace-write"}
			for _, schema := range builtinTools {
				n := pluginSchemaName(schema)
				switch n {
				case "list_files", "read_file", "write_file", "environment_submit", "environment_collect", "environment_exchange", "environment_cancel", "environment_load_plugin", "environment_shared_tool":
				default:
					a.settings.DisabledTools = append(a.settings.DisabledTools, n)
				}
			}
			files := map[string]string{
				"sum.js":   "module.exports = xs => 0;\n",
				"slug.js":  "module.exports = text => text;\n",
				"clamp.js": "module.exports = (n,lo,hi) => n;\n",
			}
			for p, c := range files {
				if err := a.workspace.WriteFile(p, []byte(c), 0600); err != nil {
					t.Fatal(err)
				}
			}
			code := `const fs=require('fs'),path=require('path'),assert=require('assert');module.exports={apply(ctx){ctx.tool({name:'verify_candidate',scheduling:{resources:[{root:'workspace',argument:'target',access:'read'}]},description:'Execute deterministic regression assertions for sum.js, slug.js or clamp.js in THIS workspace; returns actual pass/fail and independent plugin instance counter.',parameters:{type:'object',properties:{target:{type:'string',enum:['sum.js','slug.js','clamp.js']}},required:['target']},handler:(args,api)=>{const target=args.target;if(!['sum.js','slug.js','clamp.js'].includes(target))throw Error('unknown target');const file=path.join(api.workspaceRoot,target);delete require.cache[require.resolve(file)];const f=require(file);let passed=false,error='';try{if(target==='sum.js'){assert.strictEqual(f([]),0);assert.strictEqual(f([-2,5,7]),10);assert.strictEqual(f([0.5,0.25]),0.75)}if(target==='slug.js'){assert.strictEqual(f('  Hello   WORLD  '),'hello-world');assert.strictEqual(f('a__b---c'),'a-b-c');assert.strictEqual(f('___'),'')}if(target==='clamp.js'){assert.strictEqual(f(-1,0,10),0);assert.strictEqual(f(12,0,10),10);assert.strictEqual(f(4,0,10),4)}passed=true}catch(e){error=e.message}const state=path.join(__dirname,'calls');const count=fs.existsSync(state)?Number(fs.readFileSync(state,'utf8')):0;fs.writeFileSync(state,String(count+1));return {target,passed,error,instance:__dirname,workspace:api.workspaceRoot,calls:count+1}}})}};`
			planInstall(t, a, "runtime-verify", code)
			s := createSession(t, a)
			prompt := `Fix three independent CommonJS modules in the current project. sum.js exports a function summing an array of numbers including empty and fractional values; slug.js exports a function lowercasing ASCII text, trimming leading/trailing separators, and turning any sequence of whitespace, underscore or hyphen into one hyphen; clamp.js exports a function bounding n inclusively to [lo,hi], assuming lo<=hi. Read before edit. Keep module.exports signatures. Use the actual verify_candidate plugin tool to run each regression against the candidate. With worker capacity available, you are free to choose useful decomposition; use at least two environments for these independent modules to exercise the architecture, without filling every slot. Each worker should load runtime-verify with environment_load_plugin, edit scratch with write_file, then call verify_candidate and publish its result to environment_exchange under a distinct key. The main scheduler collects all jobs, registers their changes as ordinary pending proposals using environment_collect merge=true, resolves conflicts and reports actual validation. At 1X do the work directly with main read_file/write_file and leave changes pending; verify_candidate will still observe old main files until application, so describe that limitation accurately. Do not repeatedly test unapplied main proposals. Do not publish or deploy. Final answer concise.`
			started := time.Now()
			requireStatus(t, request(a, "POST", "/api/sessions/"+s.ID+"/runs", map[string]any{"mode": "chat", "prompt": prompt, "parallelism": capacity, "modelSelection": SessionModelSelection{Strategy: "manual", Profile: "precise", Model: "gpt-6-luna", ReasoningEffort: "low"}}), 202)
			deadline := time.Now().Add(7 * time.Minute)
			var task *Task
			for time.Now().Before(deadline) {
				a.mu.Lock()
				live := a.sessions[s.ID]
				if len(live.Runs) > 0 {
					task = live.Runs[len(live.Runs)-1]
				}
				done := task != nil && task.Status != "running"
				a.mu.Unlock()
				if done {
					break
				}
				time.Sleep(100 * time.Millisecond)
			}
			a.mu.Lock()
			snapshot := cloneExecutionValue(task)
			a.mu.Unlock()
			if snapshot == nil {
				t.Fatal("run absent")
			}
			entry := map[string]any{"capacity": capacity, "elapsedMillis": time.Since(started).Milliseconds(), "status": snapshot.Status, "error": snapshot.Error, "usage": snapshot.Usage, "environments": snapshot.Environments, "tools": snapshot.ToolUses, "files": snapshot.Files}
			proof = append(proof, entry)
			defer func() {
				if path := os.Getenv("AIDE_ENVIRONMENT_LIVE_PROOF"); path != "" {
					if e := atomicJSON(path, proof); e != nil {
						t.Error(e)
					}
				}
			}()
			if snapshot.Status != "awaiting_approval" && snapshot.Status != "completed" {
				t.Fatalf("live task status %s: %s", snapshot.Status, snapshot.Error)
			}
			if len(snapshot.Files) != 3 {
				t.Fatalf("expected three candidate proposals, got %d", len(snapshot.Files))
			}
			for p, c := range files {
				if b, _ := a.workspace.ReadFile(p); string(b) != c {
					t.Fatal("main changed before approval")
				}
			}
			if capacity == 10 {
				if snapshot.Environments == nil || len(snapshot.Environments.Results) < 2 || snapshot.Environments.Peak > 9 {
					t.Fatal("actual environment allocation missing or over capacity")
				}
				for _, r := range snapshot.Environments.Results {
					if r.Status != "completed" || !r.Released || !r.Merged || !r.Collected {
						t.Fatalf("environment incomplete: %+v", r)
					}
					verified := false
					for _, u := range r.Tools {
						if u.Tool == "verify_candidate" && strings.Contains(u.Result, `"passed":true`) {
							verified = true
						}
					}
					if !verified {
						t.Fatalf("environment %s has no real plugin validation", r.ID)
					}
				}
				if _, e := os.Stat(filepath.Join(a.pluginsPath, "runtime-verify", "calls")); !os.IsNotExist(e) {
					t.Fatal("private plugin instance changed main plugin state")
				}
			} else if len(snapshot.Environments.Results) != 0 {
				t.Fatal("1X allocated workers")
			}
			// Real versioned application is authorized only in this disposable fixture.
			requireStatus(t, request(a, "POST", "/api/sessions/"+s.ID+"/runs/"+snapshot.ID+"/apply", nil), 200)
			for target := range files {
				raw, e := a.callPluginTool("runtime-verify", "verify_candidate", map[string]any{"target": target})
				if e != nil {
					t.Fatal(e)
				}
				b, _ := json.Marshal(raw)
				if !strings.Contains(string(b), `"passed":true`) {
					t.Fatal("independent final verification failed", string(b))
				}
			}
			entry["postApplyVerified"] = true
			t.Logf("Luna %dX elapsed=%s tokens=%d environments=%d", capacity, time.Since(started).Round(time.Millisecond), snapshot.Usage.Total, len(snapshot.Environments.Results))
		})
	}
}
