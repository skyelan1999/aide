package server

// Execution environments are task-local runtimes, not additional user sessions
// and not VMs. Tools retain their ordinary sandbox/approval boundaries. In
// particular trusted Node plugins do not acquire an OS security boundary here.
import (
	"aide/internal/execsandbox"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type executionPoolKey struct{}
type executionApprovalHeldKey struct{}

type ExecutionEnvironmentResult struct {
	ID                string     `json:"id"`
	Assignment        string     `json:"assignment"`
	Key               string     `json:"key,omitempty"`
	DependsOn         []string   `json:"dependsOn,omitempty"`
	Status            string     `json:"status"`
	CreatedAt         string     `json:"createdAt,omitempty"`
	StartedAt         string     `json:"startedAt,omitempty"`
	FinishedAt        string     `json:"finishedAt,omitempty"`
	Output            string     `json:"output,omitempty"`
	Error             string     `json:"error,omitempty"`
	Changes           []Change   `json:"changes,omitempty"`
	Unsupported       []string   `json:"unsupported,omitempty"`
	Tools             []ToolUse  `json:"tools,omitempty"`
	Usage             TokenUsage `json:"usage"`
	ArtifactDirectory string     `json:"artifactDirectory,omitempty"`
	Released          bool       `json:"released"`
	Collected         bool       `json:"collected"`
	Merged            bool       `json:"merged"`
	MergedPaths       []string   `json:"mergedPaths,omitempty"`
}

type ExecutionEnvironmentProgress struct {
	Capacity       int                          `json:"capacity"`
	Active         int                          `json:"active"`
	Peak           int                          `json:"peak"`
	MainPaused     bool                         `json:"mainPaused,omitempty"`
	WorkerCapacity int                          `json:"workerCapacity,omitempty"`
	Results        []ExecutionEnvironmentResult `json:"results,omitempty"`
}

type executionExchangeEntry struct {
	Version int    `json:"version"`
	Author  string `json:"author"`
	Value   string `json:"value"`
	// Shared content is never promoted to a user instruction or verified fact.
	Verified bool `json:"verified"`
}

type executionJob struct {
	result       ExecutionEnvironmentResult
	done         chan struct{}
	dependencies []string
	base         map[string]string
	ctx          context.Context
	cancel       context.CancelFunc
}

type executionPool struct {
	mu          sync.Mutex
	closeOnce   sync.Once
	approval    chan struct{}
	mergeMu     sync.Mutex
	mergedCalls int
	toolBudget  int
	persistErr  error
	parent      *App
	task        *Task
	cfg         Settings
	params      ProfileParams
	ctx         context.Context
	cancel      context.CancelFunc
	jobs        map[string]*executionJob
	queue       []string
	changed     chan struct{}
	draining    bool
	randomReady bool
	exchange    map[string]executionExchangeEntry
	slots       chan struct{}
	wg          sync.WaitGroup
	root        string
	source      string
	workspaceID string
	revision    uint64
	closed      bool
}

type executionEnvironment struct {
	pool *executionPool
	id   string
	root string
	ctx  context.Context
}

func environmentTool(name, description string, properties map[string]any, required ...string) any {
	if required == nil {
		required = []string{}
	}
	return map[string]any{"type": "function", "function": map[string]any{"name": name, "description": description, "parameters": map[string]any{"type": "object", "properties": properties, "required": required}}}
}

var executionEnvironmentTools = []any{
	environmentTool("environment_run_queue", "Run a work batch using ALL X work slots. During this synchronous tool the main model pauses inference, so no slot is reserved for a manager. Code dispatches ready assignments, refills slots and collects results without per-job model polling. order defaults to random among ready jobs; fifo is available. Optional jobs uses the same atomic batch validation and batch-local keys as environment_submit_batch; omitted jobs drains the existing queue. merge=true registers ordinary pending proposals only. Dependencies order execution, not copy predecessor edits. Cancellation cancels/joins the pool before returning the borrowed slot. Returns a compact manifest; independently verify integration and inspect retained evidence as needed, without re-reading every original file solely to remember progress.", map[string]any{"jobs": map[string]any{"type": "array", "items": map[string]any{"type": "object", "properties": map[string]any{"key": map[string]any{"type": "string"}, "assignment": map[string]any{"type": "string"}, "dependsOn": map[string]any{"type": "array", "items": map[string]any{"type": "string"}}}, "required": []string{"assignment"}}}, "order": map[string]any{"type": "string", "enum": []string{"random", "fifo"}}, "merge": map[string]any{"type": "boolean"}}),
	environmentTool("environment_submit_batch", "Atomically enqueue small, independently verifiable assignments. jobs may outnumber slots; an idle slot automatically starts the next ready job without another model turn. Each optional key is unique within this batch; dependsOn may name batch keys (including later jobs) or existing job IDs. Rejects missing dependencies/cycles before any job starts. Dependencies order execution only: they do NOT apply or copy predecessor candidates. Keep integration of edits on main. Choose useful granularity, not a fixed job count; do not split trivial/sequential work. Returns stable IDs for every accepted job.", map[string]any{"jobs": map[string]any{"type": "array", "items": map[string]any{"type": "object", "properties": map[string]any{"key": map[string]any{"type": "string"}, "assignment": map[string]any{"type": "string"}, "dependsOn": map[string]any{"type": "array", "items": map[string]any{"type": "string"}}}, "required": []string{"assignment"}}}}, "jobs"),
	environmentTool("environment_queue", "Read task-local job states without loading source or worker history. limit controls presentation only (default 32); pass nextAfter as after to continue. Counts cover ALL jobs, including omitted rows. queued dependencies do not occupy worker slots. completed is not independently verified; mergedAsPending is not applied.", map[string]any{"after": map[string]any{"type": "string"}, "limit": map[string]any{"type": "integer"}}),
	environmentTool("environment_collect_ready", "Collect whichever jobs have finished, rather than waiting for a specific slow job. wait=true waits cancelably for ANY completion; limit defaults to 8 receipts. merge=true uses the same ordinary pending-proposal version/conflict checks, never applies files. Each result has its own error so one conflict cannot hide other results. Resolve conflicts with environment_collect paths/detail; do not blindly repeat a failed merge. Collect all jobs before final response.", map[string]any{"wait": map[string]any{"type": "boolean"}, "merge": map[string]any{"type": "boolean"}, "limit": map[string]any{"type": "integer"}}),
	environmentTool("environment_submit", "Schedule an independent assignment in an isolated scratch workspace. X includes this main runtime: at most X-1 workers. Create only useful assignments; never pad slots. Dependencies reference existing job IDs. Worker tools/plugins run normally; results must be collected and reconciled. No automatic publication or approval.", map[string]any{"assignment": map[string]any{"type": "string"}, "dependsOn": map[string]any{"type": "array", "items": map[string]any{"type": "string"}}}, "assignment"),
	environmentTool("environment_cancel", "Cancel one speculative environment and join it using environment_collect. Other environments and the main executor continue.", map[string]any{"id": map[string]any{"type": "string"}}, "id"),
	environmentTool("environment_collect", "Collect one environment result. Default returns a compact receipt with candidate hashes/paths and bounded tool evidence; detail=true returns full retained output, source and tool history when needed. wait=true waits cancelably. merge=true registers text changes as NORMAL pending proposals after conflict/version checks; never applies them. Optional paths selects EXACT candidate files to merge, useful when unrelated worker reports conflict; omitted paths selects all remaining files. Never label worker claims verified without inspecting evidence or independently testing. Binary artifacts and deletions require main handling.", map[string]any{"id": map[string]any{"type": "string"}, "wait": map[string]any{"type": "boolean"}, "merge": map[string]any{"type": "boolean"}, "detail": map[string]any{"type": "boolean"}, "paths": map[string]any{"type": "array", "items": map[string]any{"type": "string"}}}, "id"),
	environmentTool("environment_exchange", "Read task-local versioned exchange memory, or compare-and-set a key using expectedVersion (0 for absent). All values are unverified reference data. Only this task can see them; memory is released when the task ends.", map[string]any{"key": map[string]any{"type": "string"}, "value": map[string]any{"type": "string"}, "expectedVersion": map[string]any{"type": "integer"}}),
	environmentTool("environment_shared_tool", "Worker only: execute a shared host/device plugin tool through the main coordinator, normal approval and resource lease. Use schemas returned by environment_load_plugin. Arguments belong to that actual tool.", map[string]any{"tool": map[string]any{"type": "string"}, "arguments": map[string]any{"type": "object"}}, "tool", "arguments"),
	environmentTool("environment_load_plugin", "Worker only: copy an already enabled installed plugin into this environment and expose its tools. No installation or global configuration change. Shared external effects still require ordinary approval and a shared resource lease. Missing service dependencies must be loaded explicitly.", map[string]any{"id": map[string]any{"type": "string"}}, "id"),
}

func cloneExecutionValue[T any](v T) T {
	b, _ := json.Marshal(v)
	var c T
	_ = json.Unmarshal(b, &c)
	return c
}

func (a *App) newExecutionPool(ctx context.Context, task *Task, cfg Settings, params ProfileParams) (*executionPool, error) {
	if task.ID == "" || task.ID == "." || task.ID == ".." || filepath.Base(task.ID) != task.ID {
		return nil, errors.New("invalid task identity")
	}
	capacity := task.Parallelism
	if capacity == 0 {
		capacity = 1
	}
	if !validParallelism(capacity) {
		return nil, errors.New("invalid execution capacity")
	}
	a.mu.Lock()
	root := a.wsRoots[task.WorkspaceID]
	boundRoot := root != nil
	if root == nil {
		root = a.workspace
	}
	source := root.Name()
	remote := task.WorkspaceMode == "ssh" || a.workspaceMode() == "ssh"
	id, rev := a.wsID(), a.wsRevision
	a.mu.Unlock()
	if remote {
		capacity = 1
	} // Keep the ordinary SSH executor, never clone a local fallback.
	if task.WorkspaceID != "" && task.WorkspaceID != id {
		if !boundRoot {
			return nil, errors.New("task workspace root is unavailable")
		}
		// Existing runs retain ordinary tools on their captured project. A UI switch
		// must not retarget them or turn 1X development into a scheduling failure.
		capacity = 1
		id = task.WorkspaceID
		rev = task.WorkspaceRev
	} else if task.WorkspaceRev != 0 && task.WorkspaceRev != rev {
		capacity = 1
	}
	ctx, cancel := context.WithCancel(ctx)
	slotCapacity := capacity
	if capacity == 1 {
		slotCapacity = 0 // Ordinary 1X never creates a child runtime.
	}
	p := &executionPool{parent: a, task: task, cfg: cfg, params: params, ctx: ctx, cancel: cancel, jobs: map[string]*executionJob{}, exchange: map[string]executionExchangeEntry{}, slots: make(chan struct{}, slotCapacity), approval: make(chan struct{}, 1), source: source, workspaceID: id, revision: rev}
	a.mu.Lock()
	if previous := task.Environments; previous != nil {
		for _, result := range previous.Results {
			if result.Status == "running" || result.Status == "queued" {
				result.Status = "interrupted"
				result.Error = "Prior runtime ended; inspect retained evidence before resubmitting. No automatic replay."
			}
			j := &executionJob{result: cloneExecutionValue(result), done: make(chan struct{}), ctx: ctx, cancel: func() {}}
			close(j.done)
			p.jobs[result.ID] = j
		}
	}
	task.Environments = &ExecutionEnvironmentProgress{Capacity: capacity}
	a.mu.Unlock()
	p.mu.Lock()
	p.publishLocked()
	p.mu.Unlock()
	return p, nil
}

func (p *executionPool) publishLocked() {
	p.parent.mu.Lock()
	progress := p.task.Environments
	progress.MainPaused = p.draining
	progress.WorkerCapacity = p.workerLimitLocked()
	progress.Results = nil
	for _, j := range p.jobs {
		progress.Results = append(progress.Results, cloneExecutionValue(j.result))
	}
	sort.Slice(progress.Results, func(i, j int) bool { return progress.Results[i].ID < progress.Results[j].ID })
	for _, s := range p.parent.sessions {
		for _, t := range s.Runs {
			if t == p.task {
				if err := p.parent.save(s); err != nil {
					p.persistErr = err
				}
			}
		}
	}
	p.parent.mu.Unlock()
	p.parent.publishStream(p.task.ID, streamEvent{Event: "parallel", Text: "environments"})
}

func (p *executionPool) close() {
	p.closeOnce.Do(func() {
		p.mu.Lock()
		p.closed = true
		p.cancel()
		p.dispatchReadyLocked()
		p.mu.Unlock()
		p.wg.Wait()
		p.mu.Lock()
		defer p.mu.Unlock()
		if p.root != "" {
			if err := os.RemoveAll(p.root); err != nil {
				p.persistErr = errors.Join(p.persistErr, err)
			}
		}
		p.exchange = nil
		p.publishLocked()
	})
}

// Snapshot regular files, including uncommitted and untracked source. Never
// follow a link into another project or recursively clone runtime/cache data.
func copyExecutionTree(ctx context.Context, source, destination string, skipRuntime bool) (map[string]string, []string, error) {
	root, err := os.OpenRoot(source)
	if err != nil {
		return nil, nil, err
	}
	defer root.Close()
	hashes := map[string]string{}
	omitted := []string{}
	err = fs.WalkDir(root.FS(), ".", func(rel string, d fs.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if e = ctx.Err(); e != nil {
			return e
		}
		if rel == "." {
			return os.MkdirAll(destination, 0700)
		}
		if d.Name() == ".git" {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if skipRuntime && d.IsDir() {
			switch d.Name() {
			case ".git", ".aide-data", ".cache", ".agent-state", "node_modules":
				omitted = append(omitted, rel+"/")
				return fs.SkipDir
			}
		}
		info, e := d.Info()
		if e != nil {
			return e
		}
		if info.Mode()&os.ModeSymlink != 0 || (!d.IsDir() && !info.Mode().IsRegular()) {
			omitted = append(omitted, rel)
			return nil
		}
		out := filepath.Join(destination, filepath.FromSlash(rel))
		if d.IsDir() {
			return os.MkdirAll(out, 0700)
		}
		in, e := root.Open(rel)
		if e != nil {
			return e
		}
		f, e := os.OpenFile(out, os.O_CREATE|os.O_EXCL|os.O_WRONLY, info.Mode().Perm()&0777)
		if e != nil {
			in.Close()
			return e
		}
		digest := sha256.New()
		_, e = io.Copy(io.MultiWriter(f, digest), in)
		_ = in.Close()
		closeErr := f.Close()
		if e != nil {
			return e
		}
		if closeErr != nil {
			return closeErr
		}
		hashes[rel] = hex.EncodeToString(digest.Sum(nil))
		return nil
	})
	return hashes, omitted, err
}

func (p *executionPool) submit(assignment string, dependencies []string) (string, error) {
	jobs, err := p.submitBatch([]executionAssignment{{Assignment: assignment, DependsOn: dependencies}})
	if err != nil {
		return "", err
	}
	return jobs[0].ID, nil
}

func (p *executionPool) run(j *executionJob) {
	var err error
	defer j.cancel()
	result := cloneExecutionValue(j.result)
	defer func() {
		p.mu.Lock()
		defer p.mu.Unlock()
		if err != nil {
			result.Status = "failed"
			result.Error = err.Error()
			if errors.Is(err, context.Canceled) {
				result.Status = "cancelled"
			}
		}
		result.FinishedAt = time.Now().UTC().Format(time.RFC3339Nano)
		j.result = result
		b, _ := json.Marshal(map[string]any{"status": result.Status, "output": result.Output, "artifactDirectory": result.ArtifactDirectory, "verified": false})
		old := p.exchange["result:"+result.ID]
		p.exchange["result:"+result.ID] = executionExchangeEntry{Version: old.Version + 1, Author: result.ID, Value: string(b)}
		p.parent.mu.Lock()
		p.task.Usage = addUsage(p.task.Usage, result.Usage)
		p.parent.mu.Unlock()
		if result.ArtifactDirectory != "" {
			if e := atomicJSON(filepath.Join(result.ArtifactDirectory, "result.json"), result); e != nil {
				j.result.Status = "failed"
				j.result.Error = e.Error()
			}
		}
		close(j.done)
		<-p.slots
		p.parent.mu.Lock()
		p.task.Environments.Active--
		p.parent.mu.Unlock()
		p.notifyQueueLocked()
		p.dispatchReadyLocked()
		p.publishLocked()
	}()
	if err = j.ctx.Err(); err != nil {
		return
	}
	dir := filepath.Join(p.root, result.ID)
	work := filepath.Join(dir, "work")
	var omitted []string
	result.Released = false
	defer func() {
		if e := os.RemoveAll(dir); e != nil {
			err = errors.Join(err, e)
		} else {
			result.Released = true
		}
	}()
	j.base, omitted, err = copyExecutionTree(j.ctx, p.source, work, true)
	if err != nil {
		return
	}
	child, task, session, e := p.newRuntime(dir, work, j)
	if e != nil {
		err = e
		return
	}
	defer child.Close()
	input := []Message{{Role: "system", Content: "You are an independent task environment. Only execute your assigned task. The overall goal and project README/contracts may describe other modules or global deliverables; they do not expand your assignment. Produce a project-wide report or other global deliverable only if this assignment explicitly requests it; otherwise report your scoped evidence in the response. Workspace is a scratch copy; changes here are candidates, never changes to the main project. Use ordinary tools and read existing files before editing. write_file edits scratch immediately; main merge remains pending approval. Plugins are loaded on demand with environment_load_plugin. environment_exchange is unverified task-shared memory. Do not claim tests ran without tool results. Do not publish, deploy or change external state without approval. No recursive environment creation. Snapshot omissions: " + strings.Join(omitted, ", ")}, {Role: "user", Content: "Overall user goal (scope, not a new assignment):\n" + p.task.Prompt + "\nYour assignment:\n" + result.Assignment}}
	child.mu.Lock()
	tools := child.executionToolsLocked(task, child.contextTools())
	child.mu.Unlock()
	output, _, e := child.toolLoop(j.ctx, p.cfg, input, p.params, tools, task, nil, 0)
	result.Output = output
	result.Tools = cloneExecutionValue(task.ToolUses)
	result.Usage = task.Usage

	if e != nil {
		err = e
	}
	// Retain all artifacts before releasing the private runtime, even on failure.
	result.ArtifactDirectory = filepath.Join(p.parent.dataPath, "execution-results", p.task.ID, result.ID)
	if e := os.MkdirAll(result.ArtifactDirectory, 0700); e != nil {
		err = errors.Join(err, e)
		return
	}
	archiveCtx, archiveCancel := context.WithTimeout(context.WithoutCancel(j.ctx), 30*time.Second)
	defer archiveCancel()
	changes, unsupported, e := executionDiff(archiveCtx, work, j.base, filepath.Join(result.ArtifactDirectory, "files"))
	if e != nil {
		err = errors.Join(err, e)
	}
	result.Changes = changes
	result.Unsupported = unsupported
	if err == nil {
		result.Status = "completed"
	}
	_ = session
	if e := atomicJSON(filepath.Join(result.ArtifactDirectory, "result.json"), result); e != nil {
		err = errors.Join(err, e)
	}
}

func (p *executionPool) newRuntime(dir, work string, j *executionJob) (*App, *Task, *Session, error) {
	if j.ctx == nil {
		j.ctx = p.ctx
	}
	data, ref := filepath.Join(dir, "data"), filepath.Join(dir, "ref")
	if e := EnsureDirs(data); e != nil {
		return nil, nil, nil, e
	}
	if e := os.MkdirAll(ref, 0700); e != nil {
		return nil, nil, nil, e
	}
	w, e := os.OpenRoot(work)
	if e != nil {
		return nil, nil, nil, e
	}
	r, e := os.OpenRoot(ref)
	if e != nil {
		w.Close()
		return nil, nil, nil, e
	}
	p.parent.mu.Lock()
	settings := cloneExecutionValue(p.parent.settings)
	profiles := cloneExecutionValue(p.parent.profileState)
	window := p.parent.modelWindow(p.cfg.Model)
	p.parent.mu.Unlock()
	// No startup workers, external mounts, real history, sources, credentials on
	// disk, or user workspace selection. Only the selected provider key is used.
	a := &App{workspace: w, reference: r, workPath: work, refPath: ref, dataPath: data, containerAbs: work, settings: settings, profileState: profiles, sessions: map[string]*Session{}, cancels: map[string]context.CancelFunc{}, commands: make(chan struct{}, 4), wsRoots: map[string]*os.Root{defaultWorkspaceID: w}, eventSubs: map[string]map[chan streamEvent]struct{}{}, globalSubs: map[chan string]struct{}{}, liveBroker: NewStreamBroker(), liveTaskSess: map[string]string{}, compactingSessions: map[string]bool{}, contextCompactionProgress: map[string]contextCompactionProgress{}, tokenStats: map[string]TokenDay{}, pluginsPath: filepath.Join(dir, "plugins"), executionEnvironment: &executionEnvironment{pool: p, id: j.result.ID, root: work, ctx: j.ctx}}
	a.settings.BaseURL = p.cfg.BaseURL
	a.settings.APIKey = p.cfg.APIKey
	a.settings.Model = p.cfg.Model
	a.settings.ActiveModel = p.cfg.Model
	// Worker routing cannot fetch another credential or silently change providers.
	if sandboxMode(a.settings.SandboxMode) != "read-only" {
		a.settings.SandboxMode = "workspace-write"
	}
	a.settings.Models = []ModelRef{{ID: p.cfg.Model, ContextWindow: window}}
	a.settings.ModelProviders = nil
	a.wsConfig = defaultWorkspaceConfig()
	a.bgCtx, a.bgCancel = context.WithCancel(p.ctx)
	a.pluginRegistry = pluginRegistry{Version: 1}
	a.daemons = newDaemonManager(a)
	t := &Task{ID: newID(), Mode: "chat", Prompt: j.result.Assignment, Model: p.cfg.Model, Status: "running", Parallelism: 1, WorkspaceID: a.wsID(), WorkspaceRev: a.wsRevision, WorkspaceMode: "local", AgentRoot: AgentRoot{ContainerAbs: work, DisplayHost: work, ID: a.wsID()}, ExecutionPolicy: cloneExecutionValue(p.task.ExecutionPolicy), HarnessConfig: cloneExecutionValue(p.task.HarnessConfig), AutoReview: p.task.AutoReview, Steer: make(chan string, 4), Steps: []Step{{Name: "chat", Status: "running"}}}
	// Restrict only worker capabilities; never narrow the user's main executor.
	t.AgentToolsSet = true
	for _, schema := range append(append([]any{}, builtinTools...), executionEnvironmentTools...) {
		name := pluginSchemaName(schema)
		if environmentLocalTool(name) && !taskToolDenied(p.task, name) && !toolDeniedIn(settings.DisabledTools, name) {
			t.AgentTools = append(t.AgentTools, name)
		}
	}
	s := &Session{ID: newID(), Runs: []*Task{t}, Messages: inputForEnvironment(t.Prompt)}
	a.sessions[s.ID] = s
	return a, t, s, nil
}

func inputForEnvironment(prompt string) []Message { return []Message{{Role: "user", Content: prompt}} }
func environmentLocalTool(name string) bool {
	switch name {
	case "list_files", "read_file", "write_file", "run_shell", "semantic_search", "document_search", "office_document_search", "docx_structure", "docx_list_comments", "research_status", "record_research_finding", "update_plan", "environment_exchange", "environment_load_plugin", "environment_shared_tool":
		return true
	}
	return false
}

func executionDiff(ctx context.Context, work string, base map[string]string, artifact string) ([]Change, []string, error) {
	root, err := os.OpenRoot(work)
	if err != nil {
		return nil, nil, err
	}
	defer root.Close()
	seen := map[string]bool{}
	changes := []Change{}
	unsupported := []string{}
	err = fs.WalkDir(root.FS(), ".", func(rel string, d fs.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if e = ctx.Err(); e != nil {
			return e
		}
		if rel == "." {
			return nil
		}
		if executionPrivatePath(rel) {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		info, e := d.Info()
		if e != nil {
			return e
		}
		if !info.Mode().IsRegular() {
			unsupported = append(unsupported, rel+": link/special artifact omitted")
			return nil
		}
		f, e := root.Open(rel)
		if e != nil {
			return e
		}
		defer f.Close()
		digest := sha256.New()
		if _, e = io.Copy(digest, executionContextReader{ctx, f}); e != nil {
			return e
		}
		seen[rel] = true
		if base[rel] == hex.EncodeToString(digest.Sum(nil)) {
			return nil
		}
		dest := filepath.Join(artifact, filepath.FromSlash(rel))
		if e = os.MkdirAll(filepath.Dir(dest), 0700); e != nil {
			return e
		}
		if _, e = f.Seek(0, io.SeekStart); e != nil {
			return e
		}
		out, e := os.OpenFile(dest, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, info.Mode().Perm())
		if e != nil {
			return e
		}
		_, copyErr := io.Copy(out, executionContextReader{ctx, f})
		if e = errors.Join(copyErr, out.Close()); e != nil {
			return e
		}
		// Binary artifacts stay on disk. Only changed, validated text needs to
		// enter the in-memory proposal contract; unchanged files are hash-only.
		text, e := executionTextArtifact(ctx, dest)
		if e != nil {
			return e
		}
		if !text {
			unsupported = append(unsupported, rel+": binary artifact retained")
			return nil
		}
		b, e := os.ReadFile(dest)
		if e != nil {
			return e
		}
		changes = append(changes, Change{Path: rel, Content: string(b), BaseHash: base[rel], NoSymlinks: true})
		return nil
	})
	for rel := range base {
		if !seen[rel] {
			unsupported = append(unsupported, rel+": deletion requires main handling")
		}
	}
	sort.Slice(changes, func(i, j int) bool { return changes[i].Path < changes[j].Path })
	sort.Strings(unsupported)
	return changes, unsupported, err
}

func (p *executionPool) collect(ctx context.Context, id string, wait, merge bool, versions map[string]Change) (ExecutionEnvironmentResult, error) {
	return p.collectPaths(ctx, id, wait, merge, versions, nil)
}

func (p *executionPool) collectPaths(ctx context.Context, id string, wait, merge bool, versions map[string]Change, paths []string) (ExecutionEnvironmentResult, error) {
	p.mu.Lock()
	j := p.jobs[id]
	p.mu.Unlock()
	if j == nil {
		return ExecutionEnvironmentResult{}, errors.New("unknown environment")
	}
	if wait {
		select {
		case <-ctx.Done():
			return ExecutionEnvironmentResult{}, ctx.Err()
		case <-j.done:
		}
	}
	select {
	case <-j.done:
	default:
		p.mu.Lock()
		defer p.mu.Unlock()
		return cloneExecutionValue(j.result), nil
	}
	p.mergeMu.Lock()
	defer p.mergeMu.Unlock()
	p.mu.Lock()
	resultCopy := cloneExecutionValue(j.result)
	p.mu.Unlock()
	result := &resultCopy
	wanted := map[string]bool{}
	for _, path := range paths {
		found := false
		for _, f := range result.Changes {
			found = found || f.Path == path
		}
		if !found {
			return *result, fmt.Errorf("unknown candidate path: %s", path)
		}
		wanted[path] = true
	}
	merged := map[string]bool{}
	for _, path := range result.MergedPaths {
		merged[path] = true
	}
	var candidates []Change
	for _, f := range result.Changes {
		if !merged[f.Path] && (len(paths) == 0 || wanted[f.Path]) {
			candidates = append(candidates, f)
		}
	}
	if merge && !result.Merged {
		if result.Status != "completed" {
			return *result, errors.New("failed or cancelled result cannot merge")
		}
		p.parent.mu.Lock()
		stable := p.parent.wsID() == p.workspaceID && p.parent.wsRevision == p.revision
		pending := cloneExecutionValue(p.task.Files)
		p.parent.mu.Unlock()
		if !stable {
			return *result, errors.New("main workspace changed; result retained without merge")
		}
		if taskToolDenied(p.task, "write_file") || p.parent.toolDenied("write_file") {
			return *result, errors.New("main write_file denied")
		}
		// Validate the entire batch before registering any candidate. Two workers
		// changing the same path never silently overwrite each other's proposal.
		root, e := os.OpenRoot(p.source)
		if e != nil {
			return *result, e
		}
		defer root.Close()
		for _, f := range candidates {
			if safePath(f.Path) != nil || executionPrivatePath(f.Path) {
				return *result, errors.New("unsafe candidate path")
			}
			for _, v := range pending {
				if v.Path == f.Path && !v.Applied {
					return *result, fmt.Errorf("proposal conflict: %s", f.Path)
				}
			}
			b, e := root.ReadFile(f.Path)
			if e != nil && !os.IsNotExist(e) {
				return *result, e
			}
			if (e == nil && f.BaseHash == "") || (e == nil && hash(b) != f.BaseHash) || (os.IsNotExist(e) && f.BaseHash != "") {
				return *result, fmt.Errorf("version conflict: %s", f.Path)
			}
		}
		if p.toolBudget > 0 && p.mergedCalls+len(candidates) > p.toolBudget {
			return *result, errors.New("main tool budget insufficient for candidate batch")
		}
		for _, f := range candidates {
			b, _ := root.ReadFile(f.Path)
			p.parent.mu.Lock()
			if p.task.readSnapshots == nil {
				p.task.readSnapshots = map[string]Change{}
			}
			if f.BaseHash != "" {
				p.task.readSnapshots[f.Path] = Change{Path: f.Path, Before: string(b), BaseHash: f.BaseHash}
			}
			p.parent.mu.Unlock()
			args, _ := json.Marshal(map[string]string{"path": f.Path, "content": f.Content})
			call := ToolCall{ID: newID()}
			call.Function.Name = "write_file"
			call.Function.Arguments = string(args)
			// Reuse the main policy/recovery/proposal path. No disk apply here.
			if err := ctx.Err(); err != nil {
				return *result, err
			}
			p.parent.mu.Lock()
			chain := append([]Message(nil), p.task.CheckpointMessages...)
			p.parent.mu.Unlock()
			if err := p.parent.runHarnessHooks(ctx, p.task, "chat", chain, "before_tool", "write_file"); err != nil {
				return *result, err
			}
			if err := p.parent.checkpointExecution(p.task, "chat", chain, "tool_dispatch", &call, ""); err != nil {
				return *result, err
			}
			if p.toolBudget > 0 && p.mergedCalls >= p.toolBudget {
				return *result, errors.New("main tool budget exhausted before merge")
			}
			p.mergedCalls++
			outcome := &toolDispatchOutcome{}
			receipt := p.parent.executeToolCall(context.WithValue(ctx, toolDispatchOutcomeKey{}, outcome), call, p.task, versions)
			p.parent.mu.Lock()
			p.task.ToolUses = append(p.task.ToolUses, ToolUse{Tool: "write_file", Args: call.Function.Arguments, Result: receipt, Preview: "environment merge proposal", Who: "main"})
			p.parent.mu.Unlock()
			if outcome.err == nil {
				// Registration already happened. Publish its exact successful subset
				// before any fallible result checkpoint/hook, so retries skip it even
				// when the rest of this batch is cancelled or fails. No disk apply.
				merged[f.Path] = true
				result.MergedPaths = nil
				for path := range merged {
					result.MergedPaths = append(result.MergedPaths, path)
				}
				sort.Strings(result.MergedPaths)
				result.Merged = len(merged) == len(result.Changes)
				if err := p.persistCollection(j, *result); err != nil {
					return *result, err
				}
			}
			if err := p.parent.checkpointExecution(p.task, "chat", chain, "tool_result", &call, receipt); err != nil {
				return resultCopy, err
			}
			if err := p.parent.runHarnessHooks(ctx, p.task, "chat", chain, "after_tool", "write_file"); err != nil {
				return resultCopy, err
			}
			if outcome.err != nil {
				return *result, fmt.Errorf("merge proposal rejected: %s", receipt)
			}
		}
		result.Merged = len(merged) == len(result.Changes)
	}
	result.Collected = true
	if err := p.persistCollection(j, *result); err != nil {
		return *result, err
	}
	return cloneExecutionValue(*result), nil
}

func (p *executionPool) persistCollection(j *executionJob, result ExecutionEnvironmentResult) error {
	p.mu.Lock()
	j.result = cloneExecutionValue(result)
	p.publishLocked()
	persistErr := p.persistErr
	p.mu.Unlock()
	if persistErr != nil {
		return persistErr
	}

	if result.ArtifactDirectory != "" {
		if e := atomicJSON(filepath.Join(result.ArtifactDirectory, "result.json"), result); e != nil {
			return e
		}
	}
	return nil
}

func (a *App) executeEnvironmentTool(ctx context.Context, call ToolCall, task *Task, versions map[string]Change) string {
	var args struct {
		Jobs            []executionAssignment `json:"jobs"`
		After           string                `json:"after"`
		Limit           int                   `json:"limit"`
		Order           string                `json:"order"`
		Assignment      string                `json:"assignment"`
		DependsOn       []string              `json:"dependsOn"`
		ID              string                `json:"id"`
		Wait            bool                  `json:"wait"`
		Merge           bool                  `json:"merge"`
		Detail          bool                  `json:"detail"`
		Paths           []string              `json:"paths"`
		Key             string                `json:"key"`
		Value           *string               `json:"value"`
		ExpectedVersion int                   `json:"expectedVersion"`
		Tool            string                `json:"tool"`
		Arguments       map[string]any        `json:"arguments"`
	}
	if e := json.Unmarshal([]byte(call.Function.Arguments), &args); e != nil {
		return recordToolDispatchFailure(ctx, e.Error())
	}
	p, _ := ctx.Value(executionPoolKey{}).(*executionPool)
	author := "main"
	if a.executionEnvironment != nil {
		p = a.executionEnvironment.pool
		author = a.executionEnvironment.id
	}
	if p == nil {
		return recordToolDispatchFailure(ctx, "environment runtime is unavailable in this phase")
	}
	var value any
	var err error
	switch call.Function.Name {
	case "environment_run_queue":
		if author != "main" {
			err = errors.New("only main can pause itself and lend its slot")
		} else if !executionBlockingQueueEnabled(task) {
			err = errors.New("blocking queue is not enabled for this task; use environment_submit_batch and continue useful main work, then environment_collect_ready")
		} else {
			value, err = p.runQueue(ctx, args.Jobs, args.Order, args.Merge, versions)
		}
	case "environment_submit_batch":
		if author != "main" {
			err = errors.New("only main scheduler can allocate environments")
		} else {
			value, err = p.submitBatch(args.Jobs)
		}
	case "environment_queue":
		if author != "main" {
			err = errors.New("only main scheduler reads the queue")
		} else {
			value, err = p.queueStatus(args.After, args.Limit)
		}
	case "environment_collect_ready":
		if author != "main" {
			err = errors.New("only main scheduler can collect environments")
		} else {
			value, err = p.collectReady(ctx, args.Wait, args.Merge, args.Limit, versions)
		}
	case "environment_submit":
		if author != "main" {
			err = errors.New("only main scheduler can allocate environments")
		} else {
			value, err = p.submit(args.Assignment, args.DependsOn)
		}
	case "environment_cancel":
		if author != "main" {
			err = errors.New("only main scheduler cancels environments")
		} else {
			err = p.cancelJob(args.ID)
			value = "cancellation requested"
		}
	case "environment_shared_tool":
		value, err = a.executeSharedEnvironmentTool(ctx, task, args.Tool, args.Arguments)
	case "environment_collect":
		if author != "main" {
			err = errors.New("only main scheduler can merge environments")
		} else {
			var result ExecutionEnvironmentResult
			result, err = p.collectPaths(ctx, args.ID, args.Wait, args.Merge, versions, args.Paths)
			if args.Detail {
				value = result
			} else {
				value = compactExecutionResult(result)
			}
		}
	case "environment_exchange":
		p.mu.Lock()
		if p.closed {
			err = errors.New("exchange closed")
		} else if args.Key == "" {
			value = cloneExecutionValue(p.exchange)
		} else {
			v := p.exchange[args.Key]
			if args.Value != nil {
				if v.Version != args.ExpectedVersion {
					err = errors.New("exchange version conflict")
				} else {
					v = executionExchangeEntry{Version: v.Version + 1, Author: author, Value: *args.Value}
					p.exchange[args.Key] = v
				}
			}
			value = v
		}
		p.mu.Unlock()
	case "environment_load_plugin":
		err = a.loadEnvironmentPlugin(ctx, task, args.ID)
		value = "plugin loaded; private tool schemas are available next round. For shared host/device capabilities read environment_exchange key shared-plugin:" + args.ID + " and use environment_shared_tool; normal main approval applies."
	}
	if err != nil {
		return recordToolDispatchFailure(ctx, err.Error())
	}
	b, _ := json.Marshal(value)
	return string(b)
}

func (a *App) loadEnvironmentPlugin(ctx context.Context, task *Task, id string) error {
	if a.executionEnvironment == nil {
		return errors.New("load plugins only inside an environment; main uses normal routing")
	}
	if !pluginIDPattern.MatchString(id) {
		return errors.New("invalid plugin ID")
	}
	p := a.executionEnvironment.pool
	p.parent.mu.Lock()
	var selected *PluginManifest
	for _, v := range p.parent.pluginRegistry.Plugins {
		if v.ID == id && v.Enabled {
			v := cloneExecutionValue(v)
			selected = &v
			break
		}
	}
	source := filepath.Join(p.parent.pluginsPath, id)
	p.parent.mu.Unlock()
	if selected == nil {
		return errors.New("plugin is not enabled in the parent project")
	}
	// Host/device adapters are singleton resources. Route them to main rather
	// than cloning bridge tokens, browser sessions or hardware handles.
	if id == "browser-control" || id == "computer-control" || id == "terminal" || selected.Workbench != nil && selected.Workbench.HostComponent != "" {
		p.parent.mu.Lock()
		schemas := cloneExecutionValue(p.parent.pluginToolSchemasForOwner(id))
		p.parent.mu.Unlock()
		raw, _ := json.Marshal(schemas)
		p.mu.Lock()
		old := p.exchange["shared-plugin:"+id]
		p.exchange["shared-plugin:"+id] = executionExchangeEntry{Version: old.Version + 1, Author: "runtime", Value: string(raw)}
		p.mu.Unlock()
		return nil
	}
	for _, v := range a.pluginRegistry.Plugins {
		if v.ID == id {
			return nil
		}
	}
	if _, _, err := copyExecutionTree(ctx, source, filepath.Join(a.pluginsPath, id), false); err != nil {
		return err
	}
	a.mu.Lock()
	a.pluginRegistry.Plugins = append(a.pluginRegistry.Plugins, *selected)
	a.mu.Unlock()
	a.runPluginHost(ctx)
	a.mu.Lock()
	loadError := a.pluginError(id)
	available := len(a.pluginSurface) > 0
	a.mu.Unlock()
	if !available {
		return errors.New("private plugin host did not publish a surface; kernel sandbox or runtime unavailable")
	}
	if loadError != "" {
		return errors.New(loadError)
	}
	if selected.Daemon {
		if err := a.daemons.Start(id); err != nil {
			return err
		}
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, schema := range a.pluginToolSchemas() {
		name := pluginSchemaName(schema)
		if !taskToolDenied(p.task, name) && !toolDeniedIn(a.settings.DisabledTools, name) {
			task.AgentTools = append(task.AgentTools, name)
		}
	}
	return nil
}

// An approval remains a real main-task question, including the private CWD.
// Serialize child questions so late answers cannot approve another worker.
func (a *App) environmentQuestion(ctx context.Context, q json.RawMessage) string {
	p := a.executionEnvironment.pool
	select {
	case <-ctx.Done():
		return "未批准：环境已取消"
	case p.approval <- struct{}{}:
	}
	defer func() { <-p.approval }()
	var v map[string]any
	_ = json.Unmarshal(q, &v)
	v["environmentId"] = a.executionEnvironment.id
	// Auxiliary shell review would otherwise review the main CWD. Always let
	// the user see and approve this environment's original question verbatim.
	v["approvalKind"] = "environment"
	q, _ = json.Marshal(v)
	return p.parent.awaitUserAnswer(context.WithValue(ctx, executionApprovalHeldKey{}, true), p.task, q)
}

func (p *executionPool) uncollected() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	var ids []string
	for id, j := range p.jobs {
		if !j.result.Collected {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	return ids
}

func (a *App) stageEnvironmentProposal(task *Task, p, content string) error {
	if a.executionEnvironment == nil {
		return nil
	}
	if sandboxMode(a.settings.SandboxMode) == "read-only" {
		return errors.New("read-only environment")
	}
	if err := safePath(p); err != nil {
		return err
	}
	if err := a.workspace.MkdirAll(filepath.Dir(p), 0700); err != nil {
		return err
	}
	if err := a.workspace.WriteFile(p, []byte(content), 0600); err != nil {
		return err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	for i := range task.Files {
		if task.Files[i].Path == p {
			task.Files[i].Applied = true
		}
	}
	return nil
}

func executionPrivatePath(p string) bool {
	for _, part := range strings.Split(filepath.ToSlash(p), "/") {
		switch part {
		case ".git", ".aide-data", ".agent-state":
			return true
		}
	}
	return false
}

// Only private runtimes use this wrapper. Existing project/plugin processes
// keep their current behavior. Fail closed if kernel enforcement is unavailable.
func (e *executionEnvironment) command(ctx context.Context, args, environment []string) (*exec.Cmd, func(), error) {
	runtimeRoot := filepath.Dir(e.root)
	script := filepath.Join(runtimeRoot, "plugin-host.js")
	if err := os.WriteFile(script, []byte("process.argv.splice(1,1);\n"+pluginHostJS), 0600); err != nil {
		return nil, nil, err
	}
	var command strings.Builder
	for _, item := range environment {
		key, value, ok := strings.Cut(item, "=")
		if !ok || key == "HOME" || key == "PATH" {
			continue
		}
		command.WriteString("export " + key + "=" + shellQuote(value) + "; ")
	}
	command.WriteString("cd " + shellQuote(e.root) + " && exec node " + shellQuote(script))
	for _, arg := range args {
		command.WriteString(" " + shellQuote(arg))
	}
	return execsandbox.Command(ctx, "workspace-write", runtimeRoot, command.String())
}

func (a *App) executeSharedEnvironmentTool(ctx context.Context, worker *Task, name string, args map[string]any) (any, error) {
	if a.executionEnvironment == nil {
		return nil, errors.New("shared proxy is worker-only")
	}
	p := a.executionEnvironment.pool
	owner := p.parent.pluginOwnerOf(name)
	if owner != "browser-control" && owner != "computer-control" && owner != "terminal" {
		return nil, errors.New("only shared host/device capabilities use the coordinator proxy")
	}
	if taskToolDenied(p.task, name) || p.parent.toolDenied(name) {
		return nil, errors.New("shared tool denied in main task")
	}
	p.parent.mu.Lock()
	stable := p.parent.wsID() == p.workspaceID && p.parent.wsRevision == p.revision
	p.parent.mu.Unlock()
	if !stable {
		return nil, errors.New("workspace changed before shared tool dispatch")
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case p.approval <- struct{}{}:
	}
	defer func() { <-p.approval }()
	ctx = context.WithValue(ctx, executionApprovalHeldKey{}, true)
	raw, _ := json.Marshal(args)
	call := readCallForEnvironment(name, string(raw))
	p.parent.mu.Lock()
	chain := append([]Message(nil), p.task.CheckpointMessages...)
	p.parent.mu.Unlock()
	if err := p.parent.runHarnessHooks(ctx, p.task, "chat", chain, "before_tool", name); err != nil {
		return nil, err
	}
	if err := p.parent.checkpointExecution(p.task, "chat", chain, "tool_dispatch", &call, ""); err != nil {
		return nil, err
	}
	outcome := &toolDispatchOutcome{}
	result := p.parent.executeToolCall(context.WithValue(ctx, toolDispatchOutcomeKey{}, outcome), call, p.task, nil)
	p.parent.mu.Lock()
	p.task.ToolUses = append(p.task.ToolUses, ToolUse{Tool: name, Args: call.Function.Arguments, Result: result, Who: "environment:" + a.executionEnvironment.id})
	p.parent.mu.Unlock()
	if err := p.parent.checkpointExecution(p.task, "chat", chain, "tool_result", &call, result); err != nil {
		return nil, err
	}
	if err := p.parent.runHarnessHooks(ctx, p.task, "chat", chain, "after_tool", name); err != nil {
		return nil, err
	}
	return result, outcome.err
}
func readCallForEnvironment(name, args string) ToolCall {
	c := ToolCall{ID: newID()}
	c.Function.Name = name
	c.Function.Arguments = args
	return c
}

// Caller holds a.mu. Normal 1X projects pay no extra tool-schema cost.
func (a *App) executionToolsLocked(task *Task, tools []any) []any {
	if a.executionEnvironment == nil && (task == nil || task.Mode != "chat" || task.Parallelism <= 1 || task.Parallel != nil || task.WorkspaceMode == "ssh") {
		return tools
	}
	seen := map[string]bool{}
	for _, schema := range tools {
		seen[pluginSchemaName(schema)] = true
	}
	for _, schema := range executionEnvironmentTools {
		n := pluginSchemaName(schema)
		if a.executionEnvironment != nil {
			if n != "environment_exchange" && n != "environment_load_plugin" && n != "environment_shared_tool" {
				continue
			}
		} else if n == "environment_load_plugin" || n == "environment_shared_tool" || (n == "environment_run_queue" && !executionBlockingQueueEnabled(task)) {
			continue
		}
		if !seen[n] && !toolDeniedIn(a.settings.DisabledTools, n) {
			tools = append(tools, schema)
		}
	}
	return tools
}

// The blocking all-slot experiment remains available to explicitly scoped agents,
// but ordinary multi-X chats always expose asynchronous collaboration instead.
func executionBlockingQueueEnabled(task *Task) bool {
	if task == nil || !task.AgentToolsSet || taskToolDenied(task, "environment_run_queue") {
		return false
	}
	for _, name := range task.AgentTools {
		if name == "environment_run_queue" {
			return true
		}
	}
	return false
}

func executionSchedulerInstruction(task *Task) string {
	if task == nil || task.Mode != "chat" || task.Parallel != nil || task.Parallelism <= 1 || task.WorkspaceMode == "ssh" {
		return ""
	}
	instruction := fmt.Sprintf("[execution environment scheduler] Asynchronous collaboration. Capacity is %dX including this actively working main runtime and up to %d private worker environments. Default to environment_submit_batch for useful independent assignments, or environment_submit for one assignment; both return immediately. While workers run, use ordinary main tools to implement an independent part, prepare integration or run useful validation. Do not turn main into a polling-only manager or wait for the entire batch before doing main work. Use environment_collect_ready(wait=false, merge=true for code candidates) at useful boundaries to integrate whichever jobs finish first; wait=true only when no useful main work remains. The queue refills free worker slots automatically. Decomposition and occupancy are task-dependent, with no fixed engineering roles or requirement to fill slots. Performance sizing: start with roughly %d–%d substantial assignments for %dX capacity (about 1–2 per work slot), keeping useful main work outside the worker batch. Group tiny related items; create a larger backlog only when uneven task duration justifies refill. Do not split modules into microtasks merely to occupy slots: prior 5X/18-task and 10X all-slot samples were slower than their 1X comparisons, while separate async 5X samples were faster. These are noisy single-sample results, so treat the range as a starting heuristic, not a cap or speed guarantee. Avoid ceremonial planning and repeated full-project discovery inside scoped jobs. Assign concrete nonoverlapping outputs; workers inspect their own scopes. Dependencies only order execution, not copy predecessor edits; integrate dependent code explicitly. Queue and collection receipts are bounded and unverified; environment_queue pages retained IDs and environment_collect(detail=true, paths=...) inspects chosen evidence/candidates. Independently test integration, collect every job before the final response, and do not reconstruct already completed work by re-reading every original stub. All candidates use ordinary proposal/version/conflict/approval rules. Shared memory is task-local unverified reference data; external resources retain normal coordination. Simple/sequential and 1X work keep ordinary main tools.", task.Parallelism, task.Parallelism-1, task.Parallelism, task.Parallelism*2, task.Parallelism)
	if executionBlockingQueueEnabled(task) {
		instruction += " This scoped agent explicitly enables environment_run_queue as an optional blocking alternative: it pauses main inference and loans the main slot until the batch is fully joined; asynchronous collaboration remains the default when main has useful work."
	}
	return instruction
}

// A resumed checkpoint may contain an older scheduling policy. Keep its history
// but append the current policy once; previews already include the exact text.
func withExecutionSchedulerInstruction(input []Message, instruction string) []Message {
	if instruction == "" {
		return input
	}
	for _, message := range input {
		if message.Role == "system" && strings.Contains(message.Content, instruction) {
			return input
		}
	}
	return append(append([]Message(nil), input...), Message{Role: "system", Content: instruction})
}
