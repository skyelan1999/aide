package server

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A writer waiting for its plugin must already be visible to the shared
// resource queue, otherwise a later reader in another plugin can overtake it.
func TestPluginResourceAdmissionBeforePluginFIFO(t *testing.T) {
	a := testApp(t)
	shared := filepath.Join(a.workPath, "shared.txt")
	if err := os.WriteFile(shared, []byte("initial"), 0600); err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(shared)
	common := `const fs=require('fs'),path=require('path');module.exports={apply(ctx){
	function register(name,access,target){ctx.tool({name,scheduling:{resources:[{root:'workspace',path:target,access}]},handler:async()=>{
	const mark=path.join(__dirname,name);fs.writeFileSync(mark+'.entered','1');
	for(let i=0;!fs.existsSync(mark+'.release');i++){if(i>1000)throw Error('barrier timeout');await new Promise(r=>setTimeout(r,10));}
	if(access==='write')fs.writeFileSync(FILE,'written');return {value:target==='shared.txt'?fs.readFileSync(FILE,'utf8'):'independent'};
	}});}
	TOOLS
	}};`
	for id, tools := range map[string]string{
		"alpha": `register('alpha_read','read','shared.txt');register('alpha_write','write','shared.txt');`,
		"beta":  `register('beta_read','read','shared.txt');`,
		"gamma": `register('gamma_read','read','independent.txt');`,
	} {
		code := strings.NewReplacer("FILE", string(encoded), "TOOLS", tools).Replace(common)
		requireStatus(t, request(a, "POST", "/api/plugins", map[string]any{"id": id, "name": id, "code": code}), 201)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	marker := func(id, tool, suffix string) string { return filepath.Join(a.pluginsPath, id, tool+suffix) }
	exists := func(p string) bool { _, err := os.Stat(p); return err == nil }
	await := func(label string, ready func() bool) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for !ready() {
			if time.Now().After(deadline) {
				t.Fatal("timed out waiting for " + label)
			}
			time.Sleep(time.Millisecond)
		}
	}
	release := func(id, tool string) {
		t.Helper()
		if err := os.WriteFile(marker(id, tool, ".release"), []byte("1"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	defer func() {
		for _, pair := range [][2]string{{"alpha", "alpha_read"}, {"alpha", "alpha_write"}, {"beta", "beta_read"}, {"gamma", "gamma_read"}} {
			_ = os.WriteFile(marker(pair[0], pair[1], ".release"), []byte("1"), 0600)
		}
	}()
	type outcome struct {
		raw any
		err error
	}
	start := func(id, tool string) <-chan outcome {
		ch := make(chan outcome, 1)
		go func() { raw, err := a.callPluginToolContext(ctx, id, tool, nil); ch <- outcome{raw, err} }()
		return ch
	}
	finish := func(ch <-chan outcome) any {
		t.Helper()
		select {
		case r := <-ch:
			if r.err != nil {
				t.Fatal(r.err)
			}
			return r.raw
		case <-time.After(5 * time.Second):
			t.Fatal("handler did not finish")
			return nil
		}
	}
	first := start("alpha", "alpha_read")
	await("first reader", func() bool { return exists(marker("alpha", "alpha_read", ".entered")) })
	writer := start("alpha", "alpha_write")
	// Accept either queue here so this regression deterministically demonstrates
	// the former implementation's overtaking, rather than failing on an internal
	// queue-count assertion alone.
	await("writer admission", func() bool {
		a.pluginResources.mu.Lock()
		n := len(a.pluginResources.items)
		a.pluginResources.mu.Unlock()
		a.pluginCalls.mu.Lock()
		local := len(a.pluginCalls.queues[a.pluginsPath+"\x00alpha"])
		a.pluginCalls.mu.Unlock()
		return n == 2 || local == 2
	})
	later := start("beta", "beta_read")
	await("later reader admission", func() bool {
		a.pluginResources.mu.Lock()
		n := len(a.pluginResources.items)
		a.pluginResources.mu.Unlock()
		return n == 3 || exists(marker("beta", "beta_read", ".entered"))
	})
	if exists(marker("beta", "beta_read", ".entered")) {
		t.Fatal("later reader overtook the queued writer")
	}
	independent := start("gamma", "gamma_read")
	await("independent reader", func() bool { return exists(marker("gamma", "gamma_read", ".entered")) })
	release("gamma", "gamma_read")
	finish(independent)
	release("alpha", "alpha_read")
	finish(first)
	await("writer", func() bool { return exists(marker("alpha", "alpha_write", ".entered")) })
	if exists(marker("beta", "beta_read", ".entered")) {
		t.Fatal("reader overlapped the writer")
	}
	release("alpha", "alpha_write")
	finish(writer)
	await("later reader", func() bool { return exists(marker("beta", "beta_read", ".entered")) })
	release("beta", "beta_read")
	raw := finish(later)
	result, ok := raw.(map[string]any)
	if !ok || result["value"] != "written" {
		t.Fatalf("reader did not observe ordered write: %#v", raw)
	}
	waitPluginResources(t, &a.pluginResources, 0)
}
