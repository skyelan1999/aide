package server

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func waitPluginResources(t *testing.T, q *pluginResourceQueue, n int) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		q.mu.Lock()
		size := len(q.items)
		q.mu.Unlock()
		if size == n {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("resource waiters did not reach expected size")
}

func TestPluginResourcesAtomicFairnessAndCancellation(t *testing.T) {
	var q pluginResourceQueue
	claim := func(path string, write bool) pluginFootprint {
		return pluginFootprint{resources: []pluginResource{{path, write}}}
	}
	first, err := q.acquire(context.Background(), claim("/workspace/report", false))
	if err != nil {
		t.Fatal(err)
	}
	second, err := q.acquire(context.Background(), claim("/workspace/report", false))
	if err != nil {
		t.Fatal("read/read must overlap", err)
	}
	writer := make(chan func(), 1)
	go func() {
		release, e := q.acquire(context.Background(), pluginFootprint{resources: []pluginResource{{"/workspace/report", true}, {"/workspace/chapters", true}}})
		if e == nil {
			writer <- release
		}
	}()
	waitPluginResources(t, &q, 3)
	ctx, cancel := context.WithCancel(context.Background())
	later := make(chan error, 1)
	go func() {
		release, e := q.acquire(ctx, claim("/workspace/chapters/one.md", false))
		if e == nil {
			release()
		}
		later <- e
	}()
	waitPluginResources(t, &q, 4)
	select {
	case <-later:
		t.Fatal("reader jumped a waiting multi-resource writer")
	default:
	}
	independent, err := q.acquire(context.Background(), claim("/other-workspace/report", true))
	if err != nil {
		t.Fatal(err)
	}
	independent()
	cancel()
	if err := <-later; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	first()
	second()
	select {
	case release := <-writer:
		release()
		release()
	case <-time.After(3 * time.Second):
		t.Fatal("atomic writer did not resume")
	}
	waitPluginResources(t, &q, 0)
}

func TestPluginResourceContractFallbackAndProposalBoundary(t *testing.T) {
	a := testApp(t)
	plugin := filepath.Join(a.pluginsPath, "reader")
	if err := os.MkdirAll(plugin, 0700); err != nil {
		t.Fatal(err)
	}
	set := func(contract string) pluginFootprint {
		a.mu.Lock()
		defer a.mu.Unlock()
		a.pluginSurface = []byte(`{"plugins":[{"id":"reader","tools":[{"name":"inspect","executable":true,"scheduling":` + contract + `}]}]}`)
		return a.pluginFootprintLocked("reader", "inspect", map[string]any{"path": "report.md", "readOnly": true})
	}
	for _, contract := range []string{`null`, `{}`, `{"resources":null}`, `{"resources":[{"root":"workspace","path":"../outside","access":"read"}]}`, `{"resources":[{"root":"workspace","argument":"missing","access":"read"}]}`, `{"resources":[{"root":"ssh","path":"same.md","access":"write"}]}`} {
		if !set(contract).exclusive {
			t.Fatal("invalid declaration allowed parallelism", contract)
		}
	}
	read := set(`{"resources":[{"root":"workspace","argument":"path","access":"read"}]}`)
	if read.exclusive || !read.readOnly {
		t.Fatal("valid read contract not available")
	}
	write := set(`{"resources":[{"root":"workspace","path":"report.md","access":"write"}]}`)
	if !pluginResourceConflict(read, write) {
		t.Fatal("same file read/write did not conflict")
	}
	proposal := func(path string) any {
		return map[string]any{"proposal": map[string]any{"type": "file", "path": path, "content": "draft"}}
	}
	if _, err := pluginResourceResult(read, a.workPath, proposal("report.md"), nil); err == nil {
		t.Fatal("read-only result accepted a write proposal")
	}
	if _, err := pluginResourceResult(write, a.workPath, proposal("other.md"), nil); err == nil {
		t.Fatal("write proposal escaped its claims")
	}
	if _, err := pluginResourceResult(write, a.workPath, proposal("report.md"), nil); err != nil {
		t.Fatal(err)
	}
	if _, err := pluginResourceResult(write, a.workPath, map[string]any{"proposal": map[string]any{"type": "command", "command": "touch other.md"}}, nil); err == nil {
		t.Fatal("file contract allowed command proposal")
	}
	if err := os.Symlink(a.workPath, filepath.Join(a.workPath, "alias")); err != nil {
		t.Fatal(err)
	}
	if !set(`{"resources":[{"root":"workspace","path":"alias/report.md","access":"read"}]}`).exclusive {
		t.Fatal("symlink alias bypassed writer")
	}
	if err := os.WriteFile(filepath.Join(a.workPath, "hard-original"), []byte("evidence"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(filepath.Join(a.workPath, "hard-original"), filepath.Join(a.workPath, "hard-alias")); err != nil {
		t.Fatal(err)
	}
	if !set(`{"resources":[{"root":"workspace","path":"hard-alias","access":"write"}]}`).exclusive {
		t.Fatal("hard link alias bypassed writer")
	}
	a.mu.Lock()
	a.wsConfig.Workspace.Mode = "ssh"
	a.mu.Unlock()
	if !set(`{"resources":[]}`).exclusive {
		t.Fatal("SSH contract claimed unsupported path driver")
	}
}

func TestPluginResourceContractEnforcedAtRealHostResult(t *testing.T) {
	a := testApp(t)
	code := `module.exports={apply(ctx){ctx.tool({name:'read_liar',scheduling:{resources:[{root:'workspace',argument:'path',access:'read'}]},handler:(_,api)=>api.proposeWrite('report.md','unexpected')});ctx.tool({name:'wrong_writer',scheduling:{resources:[{root:'workspace',path:'report.md',access:'write'}]},handler:(_,api)=>api.proposeWrite('other.md','unexpected')});ctx.tool({name:'proper_writer',scheduling:{resources:[{root:'workspace',path:'report.md',access:'write'}]},handler:(_,api)=>api.proposeWrite('report.md','proposal')})}};`
	requireStatus(t, request(a, "POST", "/api/plugins", map[string]any{"id": "contract", "name": "contract", "code": code}), 201)
	for _, tool := range []string{"read_liar", "wrong_writer"} {
		if _, err := a.callPluginToolContext(context.Background(), "contract", tool, map[string]any{"path": "report.md"}); err == nil || !strings.Contains(err.Error(), "契约不一致") {
			t.Fatal("host accepted inconsistent proposal", tool, err)
		}
	}
	task := &Task{}
	result := a.executeToolCall(context.Background(), readCall("read_liar", `{"path":"report.md"}`), task, map[string]Change{})
	if !strings.HasPrefix(result, "错误") || len(task.Files) != 0 {
		t.Fatal("workflow reported a rejected contract as success or recorded its proposal", result)
	}
	raw, err := a.callPluginToolContext(context.Background(), "contract", "proper_writer", nil)
	if err != nil {
		t.Fatal(err)
	}
	_, proposals := normalizePluginResult(raw)
	if len(proposals) != 1 || proposals[0]["path"] != "report.md" {
		t.Fatal("valid proposal not retained", raw)
	}
	if _, err := os.Stat(filepath.Join(a.workPath, "report.md")); !os.IsNotExist(err) {
		t.Fatal("scheduling bypassed file approval", err)
	}
}

func TestPluginResourcesDeclaredCallDoesNotLoadUnrelatedModules(t *testing.T) {
	a := testApp(t)
	known := `module.exports={apply(ctx){ctx.tool({name:'known',scheduling:{resources:[]},handler:()=>({ok:true})})}};`
	other := `const fs=require('fs'),path=require('path');fs.appendFileSync(path.join(__dirname,'loads'),'1');module.exports={apply(ctx){ctx.tool({name:'other',handler:()=>({ok:true})})}};`
	for id, code := range map[string]string{"known": known, "other": other} {
		requireStatus(t, request(a, "POST", "/api/plugins", map[string]any{"id": id, "name": id, "code": code}), 201)
	}
	file := filepath.Join(a.pluginsPath, "other", "loads")
	before, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.callPluginToolContext(context.Background(), "known", "known", nil); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(file)
	if err != nil || string(before) != string(after) {
		t.Fatal("declared call executed an unrelated module outside its footprint", err)
	}
	a.mu.Lock()
	a.pluginSurface = []byte(`{"plugins":[{"id":"known","requires":["other-service"],"tools":[{"name":"known","executable":true,"scheduling":{"resources":[]}}]}]}`)
	footprint := a.pluginFootprintLocked("known", "known", nil)
	a.mu.Unlock()
	if !footprint.exclusive {
		t.Fatal("service dependency escaped resource reservation")
	}
}

func TestPluginResourcesCrossPluginWritesAndUnknownEffects(t *testing.T) {
	a := testApp(t)
	file := filepath.Join(a.workPath, "shared.txt")
	encoded, _ := json.Marshal(file)
	// Both handlers actually read-modify-write one file; entering the first
	// handler is a barrier, so accidental overlap deterministically loses data.
	code := `const fs=require('fs'),path=require('path');module.exports={apply(ctx){ctx.tool({name:'TOOL',CONTRACT handler:async()=>{const p=FILE;const old=fs.existsSync(p)?Number(fs.readFileSync(p,'utf8')):0;fs.writeFileSync(path.join(__dirname,'entered'),'1');while(!fs.existsSync(path.join(__dirname,'release')))await new Promise(r=>setTimeout(r,10));fs.writeFileSync(p,String(old+1));return {value:old+1};}})}};`
	for _, id := range []string{"left", "right", "unknown"} {
		contract := `scheduling:{resources:[{root:'workspace',path:'shared.txt',access:'write'}]},`
		if id == "unknown" {
			contract = ""
		}
		script := strings.NewReplacer("TOOL", id+"_increment", "CONTRACT", contract, "FILE", string(encoded)).Replace(code)
		requireStatus(t, request(a, "POST", "/api/plugins", map[string]any{"id": id, "name": id, "code": script}), 201)
	}
	defer func() {
		for _, id := range []string{"left", "right", "unknown"} {
			_ = os.WriteFile(filepath.Join(a.pluginsPath, id, "release"), []byte("1"), 0600)
		}
	}()
	results := make(chan error, 3)
	start := func(id string) {
		go func() {
			_, err := a.callPluginToolContext(context.Background(), id, id+"_increment", nil)
			results <- err
		}()
	}
	start("left")
	deadline := time.Now().Add(3 * time.Second)
	for {
		if _, err := os.Stat(filepath.Join(a.pluginsPath, "left", "entered")); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("first handler did not enter")
		}
		time.Sleep(time.Millisecond)
	}
	start("right")
	waitPluginResources(t, &a.pluginResources, 2)
	start("unknown")
	waitPluginResources(t, &a.pluginResources, 3)
	for _, id := range []string{"right", "unknown"} {
		if _, err := os.Stat(filepath.Join(a.pluginsPath, id, "entered")); !os.IsNotExist(err) {
			t.Fatal("conflicting handler executed before its turn", id, err)
		}
	}
	for _, id := range []string{"left", "right", "unknown"} {
		if err := os.WriteFile(filepath.Join(a.pluginsPath, id, "release"), []byte("1"), 0600); err != nil {
			t.Fatal(err)
		}
		select {
		case err := <-results:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("handler failed to finish", id)
		}
	}
	b, err := os.ReadFile(file)
	if err != nil || string(b) != "3" {
		t.Fatalf("lost cross-plugin updates: %q %v", b, err)
	}
	waitPluginResources(t, &a.pluginResources, 0)
}

func TestPluginResourceWaitRejectsWorkspaceEpochChange(t *testing.T) {
	a := testApp(t)
	code := `module.exports={apply(ctx){ctx.tool({name:'marker',handler:()=>({ok:true})})}}`
	requireStatus(t, request(a, "POST", "/api/plugins", map[string]any{"id": "marker", "name": "marker", "code": code}), 201)
	release, err := a.pluginResources.acquire(context.Background(), pluginFootprint{exclusive: true})
	if err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() {
		_, err := a.callPluginToolContext(context.Background(), "marker", "marker", nil)
		result <- err
	}()
	waitPluginResources(t, &a.pluginResources, 2)
	a.mu.Lock()
	a.wsRevision++
	a.mu.Unlock()
	release()
	if err := <-result; err == nil || !strings.Contains(err.Error(), "未执行") {
		t.Fatal("workspace epoch not checked", err)
	}
}

func TestPluginProcessCancellationStopsChildWriter(t *testing.T) {
	a := testApp(t)
	code := `const fs=require('fs'),path=require('path'),cp=require('child_process');module.exports={apply(ctx){ctx.tool({name:'launch_child',handler:async()=>{const marker=path.join(__dirname,'late-write');cp.spawn(process.execPath,['-e',"setTimeout(()=>require('fs').writeFileSync("+JSON.stringify(marker)+",'unexpected'),800)"],{stdio:'ignore'});fs.writeFileSync(path.join(__dirname,'entered'),'1');await new Promise(r=>setTimeout(r,5000));return {ok:true};}});ctx.tool({name:'follow_up',handler:async()=>{await new Promise(r=>setTimeout(r,1000));return {ok:true}}})}};`
	requireStatus(t, request(a, "POST", "/api/plugins", map[string]any{"id": "child", "name": "child", "code": code}), 201)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() { _, err := a.callPluginToolContext(ctx, "child", "launch_child", nil); result <- err }()
	deadline := time.Now().Add(3 * time.Second)
	for {
		if _, err := os.Stat(filepath.Join(a.pluginsPath, "child", "entered")); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("child fixture did not start")
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	// Keep a second handler alive beyond the orphan's scheduled write. This
	// proves cancellation ends the process tree before another resource holder.
	if _, err := a.callPluginToolContext(context.Background(), "child", "follow_up", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(a.pluginsPath, "child", "late-write")); !os.IsNotExist(err) {
		t.Fatal("child outlived cancelled resource holder", err)
	}
}

func TestPluginResourcesDaemonCancellationStopsChildrenBeforeRelease(t *testing.T) {
	a := testApp(t)
	installMockDaemon(t, a, "child-daemon", true)
	code := `const fs=require('fs'),path=require('path'),cp=require('child_process');module.exports={apply(ctx){ctx.tool({name:'daemon_child',scheduling:{resources:[]},handler:async()=>{const marker=path.join(__dirname,'late-write');cp.spawn(process.execPath,['-e',"setTimeout(()=>require('fs').writeFileSync("+JSON.stringify(marker)+",'unexpected'),800)"],{stdio:'ignore'});fs.writeFileSync(path.join(__dirname,'entered'),'1');await new Promise(r=>setTimeout(r,5000));return {ok:true}}})},stop(){}};`
	if err := os.WriteFile(filepath.Join(a.pluginsPath, "child-daemon", "index.js"), []byte(code), 0600); err != nil {
		t.Fatal(err)
	}
	a.runPluginHost(context.Background())
	if err := a.daemons.Start("child-daemon"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() { _, err := a.callPluginToolContext(ctx, "child-daemon", "daemon_child", nil); result <- err }()
	deadline := time.Now().Add(3 * time.Second)
	for {
		if _, err := os.Stat(filepath.Join(a.pluginsPath, "child-daemon", "entered")); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("daemon child fixture did not start")
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	if err := <-result; !errors.Is(err, context.Canceled) || !strings.Contains(err.Error(), "执行结果待核实") {
		t.Fatal("cancelled dispatch not marked uncertain", err)
	}
	waitPluginResources(t, &a.pluginResources, 0)
	if _, err := a.daemons.Call("child-daemon", "daemon_child", nil); err == nil {
		t.Fatal("cancelled daemon silently replayed")
	}
	time.Sleep(time.Second)
	if _, err := os.Stat(filepath.Join(a.pluginsPath, "child-daemon", "late-write")); !os.IsNotExist(err) {
		t.Fatal("daemon child outlived resource holder", err)
	}
}
