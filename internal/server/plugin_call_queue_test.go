package server

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func waitPluginQueue(t *testing.T, q *pluginCallQueues, key string, n int) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		q.mu.Lock()
		size := len(q.queues[key])
		q.mu.Unlock()
		if size == n {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("plugin queue did not reach expected size")
}
func TestPluginCallQueueFIFOAndCancellation(t *testing.T) {
	var q pluginCallQueues
	first, err := q.acquire(context.Background(), "workspace/plugin")
	if err != nil {
		t.Fatal(err)
	}
	order := make(chan int, 3)
	done := make(chan struct{})
	go func() {
		release, e := q.acquire(context.Background(), "workspace/plugin")
		if e == nil {
			order <- 2
			release()
		}
		close(done)
	}()
	waitPluginQueue(t, &q, "workspace/plugin", 2)
	ctx, cancel := context.WithCancel(context.Background())
	cancelled := make(chan error, 1)
	go func() {
		release, e := q.acquire(ctx, "workspace/plugin")
		if e == nil {
			release()
		}
		cancelled <- e
	}()
	waitPluginQueue(t, &q, "workspace/plugin", 3)
	cancel()
	if e := <-cancelled; !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
	waitPluginQueue(t, &q, "workspace/plugin", 2)
	other, e := q.acquire(context.Background(), "workspace/other")
	if e != nil {
		t.Fatal(e)
	}
	other()
	select {
	case <-order:
		t.Fatal("same plugin overlapped")
	default:
	}
	first()
	first()
	<-done
	if got := <-order; got != 2 {
		t.Fatal(got)
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.queues) != 0 {
		t.Fatal("queue not reclaimed")
	}
}
func TestPluginCallsSerializeSharedFileWrites(t *testing.T) {
	a := testApp(t)
	code := `const fs=require('fs'),path=require('path');module.exports={apply(ctx){ctx.tool({name:'counter_increment',handler:async()=>{const p=path.join(__dirname,'counter.txt');const old=fs.existsSync(p)?Number(fs.readFileSync(p,'utf8')):0;await new Promise(r=>setTimeout(r,100));fs.writeFileSync(p,String(old+1));return {value:old+1};}})}};`
	requireStatus(t, request(a, "POST", "/api/plugins", map[string]any{"id": "counter", "name": "counter", "code": code}), 201)
	var wg sync.WaitGroup
	results := make(chan error, 3)
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := a.callPluginToolContext(context.Background(), "counter", "counter_increment", nil)
			results <- err
		}()
	}
	wg.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Fatal(err)
		}
	}
	b, err := os.ReadFile(filepath.Join(a.pluginsPath, "counter", "counter.txt"))
	if err != nil || string(b) != "3" {
		t.Fatalf("lost shared file update: %q %v", b, err)
	}
}

func TestPluginCallsDifferentPluginsOverlap(t *testing.T) {
	a := testApp(t)
	if err := os.WriteFile(filepath.Join(a.workPath, "shared-readonly.txt"), []byte("shared evidence"), 0600); err != nil {
		t.Fatal(err)
	}
	code := `const fs=require('fs'),path=require('path');module.exports={apply(ctx){ctx.tool({name:'TOOL',scheduling:{resources:[{root:'workspace',path:'shared-readonly.txt',access:'read'}]},handler:async()=>{fs.readFileSync(path.join(__dirname,'..','..','shared-readonly.txt'),'utf8');fs.writeFileSync(path.join(__dirname,'entered'),'1');for(let i=0;i<500;i++){if(fs.existsSync(path.join(__dirname,'release')))return {ok:true};await new Promise(r=>setTimeout(r,10));}throw Error('barrier timed out');}})}};`
	for _, id := range []string{"left", "right"} {
		requireStatus(t, request(a, "POST", "/api/plugins", map[string]any{"id": id, "name": id, "code": strings.ReplaceAll(code, "TOOL", id+"_barrier")}), 201)
	}
	results := make(chan error, 2)
	for _, id := range []string{"left", "right"} {
		go func(id string) {
			_, e := a.callPluginToolContext(context.Background(), id, id+"_barrier", nil)
			results <- e
		}(id)
	}
	defer func() {
		for _, id := range []string{"left", "right"} {
			_ = os.WriteFile(filepath.Join(a.pluginsPath, id, "release"), []byte("1"), 0600)
		}
	}()
	deadline := time.Now().Add(4 * time.Second)
	both := false
	for time.Now().Before(deadline) {
		_, e1 := os.Stat(filepath.Join(a.pluginsPath, "left", "entered"))
		_, e2 := os.Stat(filepath.Join(a.pluginsPath, "right", "entered"))
		if e1 == nil && e2 == nil {
			both = true
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	for _, id := range []string{"left", "right"} {
		if e := os.WriteFile(filepath.Join(a.pluginsPath, id, "release"), []byte("1"), 0600); e != nil {
			t.Fatal(e)
		}
	}
	for i := 0; i < 2; i++ {
		if e := <-results; e != nil {
			t.Fatal(e)
		}
	}
	if !both {
		t.Fatal("different real Node plugin handlers did not overlap")
	}
}
