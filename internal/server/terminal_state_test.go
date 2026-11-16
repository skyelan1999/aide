package server

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestTerminalStateRealDaemonRestartAcrossMappings(t *testing.T) {
	source := externalPluginSource(t, "terminal", "sessions.js")
	data, oldWork, newWork := t.TempDir(), t.TempDir(), t.TempDir()
	config := defaultWorkspaceConfig()
	config.Workspace.Path = "/host/persistent-project"
	first, err := terminalStateDirectory(data, config, oldWork)
	if err != nil {
		t.Fatal(err)
	}
	second, err := terminalStateDirectory(data, config, newWork)
	if err != nil {
		t.Fatal(err)
	}
	contexts, _ := json.Marshal([]map[string]any{
		{"storageRoot": oldWork, "stateRoot": first, "workbench": true},
		{"storageRoot": newWork, "stateRoot": second, "workbench": true},
	})
	// Exercise the actual plugin with a real shell, then a fresh daemon against
	// the rebuilt host context. Saved disconnected sessions must survive too.
	script := `
const assert=require('node:assert/strict');
const {Sessions}=require(process.argv[1]);
const [before,after]=JSON.parse(process.argv[2]);
(async()=>{
 const manager=new Sessions(); let restarted;
 try {
  const rec=(await manager.create(before,{name:'persistent shell'})).session;
  manager.configure(before,{id:rec.id,displayLineNumbers:true,displayTimestamps:true});
  manager.send(before,{id:rec.id,data:"printf '\\nPERSIST_REBUILD\\n'\r"});
  const deadline=Date.now()+5000;
  while(!Buffer.from(manager.poll(before,{id:rec.id}).output.data,'base64').toString().includes('\r\nPERSIST_REBUILD\r\n')){
   if(Date.now()>deadline)throw Error('shell output missing');
   await new Promise(r=>setTimeout(r,20));
  }
  const saved=(await manager.create(before,{copyFrom:rec.id,name:'saved configuration',connect:false})).session;
  await manager.stop();
  restarted=new Sessions();
  const records=restarted.list(after).sessions;
  assert.equal(records.length,2);
  const restored=records.find(s=>s.id===rec.id);
  assert.equal(restored.name,'persistent shell');
  assert.equal(restored.displayLineNumbers,true);assert.equal(restored.displayTimestamps,true);
  assert.notEqual(restored.status,'running');
  assert.equal(records.find(s=>s.id===saved.id).status,'saved');
  assert(Buffer.from(restarted.poll(after,{id:rec.id}).output.data,'base64').toString().includes('PERSIST_REBUILD'));
  assert(restarted.logs(after,{id:rec.id}).files.length);
  console.log('PASS: real PTY history, display settings and disconnected configuration survived host mapping change and daemon restart');
 } finally {await manager.stop();if(restarted)await restarted.stop();}
})().catch(e=>{console.error(e);process.exitCode=1;});`
	output, err := exec.Command("node", "-e", script, source, string(contexts)).CombinedOutput()
	if err != nil {
		t.Fatalf("real daemon restart: %v\n%s", err, output)
	}
	t.Log(string(output))
}

func TestTerminalStateSurvivesRebuildAndLocalMappingChanges(t *testing.T) {
	data := t.TempDir()
	config := defaultWorkspaceConfig()
	config.Workspace.Path = "/host/project"
	base := filepath.Join(data, "terminal-private")
	legacy := filepath.Join(base, terminalStateHash([]any{"local|/host/project|", "local", 22, "", "/local/host/project"}))
	logs := filepath.Join(legacy, ".aide", "terminal-workbench")
	if err := os.MkdirAll(logs, 0700); err != nil {
		t.Fatal(err)
	}
	// Include private relay material as well as saved sessions and output. The
	// locator must preserve all bytes, without copying or deleting the source.
	for _, name := range []string{"saved.json", "output.log", "relay.private.json", "known_hosts"} {
		if err := os.WriteFile(filepath.Join(logs, name), []byte(name), 0600); err != nil {
			t.Fatal(err)
		}
	}
	first, err := terminalStateDirectory(data, config, "/new-mount/project")
	if err != nil || first != legacy {
		t.Fatalf("legacy mapping not recovered: %s %v", first, err)
	}
	config.Workspace.Port = 2200
	config.Workspace.Username = "unused-local-ssh-setting"
	for _, mapping := range []string{"/workspace", "/another-mount/project", "/host/project"} {
		restarted, err := terminalStateDirectory(data, config, mapping)
		if err != nil || restarted != first {
			t.Fatalf("rebuild changed state directory: %s %v", restarted, err)
		}
	}
	for _, name := range []string{"saved.json", "output.log", "relay.private.json", "known_hosts"} {
		raw, err := os.ReadFile(filepath.Join(logs, name))
		if err != nil || string(raw) != name {
			t.Fatalf("existing state changed: %s %v", name, err)
		}
	}
}

func TestTerminalStateWorkspaceAndRemoteAccountIsolation(t *testing.T) {
	data := t.TempDir()
	config := defaultWorkspaceConfig()
	config.Workspace.Path = "/project-a"
	seen := map[string]bool{}
	check := func() {
		t.Helper()
		dir, err := terminalStateDirectory(data, config, "/workspace")
		if err != nil || seen[dir] {
			t.Fatalf("workspace state collided: %s %v", dir, err)
		}
		seen[dir] = true
	}
	check()
	config.Workspace.Path = "/project-b"
	check()
	config.Workspace.Mode, config.Workspace.Host = "ssh", "host-a"
	config.Workspace.Username = "alice"
	check()
	config.Workspace.Username = "bob"
	check()
	config.Workspace.Port = 2200
	check()
	config.Workspace.Host = "host-b"
	check()
}

func TestTerminalStateConcurrentFirstAccessKeepsOneDirectory(t *testing.T) {
	data := t.TempDir()
	config := defaultWorkspaceConfig()
	var wg sync.WaitGroup
	results := make(chan string, 12)
	for range 12 {
		wg.Go(func() {
			dir, err := terminalStateDirectory(data, config, "/workspace")
			if err != nil {
				t.Error(err)
			}
			results <- dir
		})
	}
	wg.Wait()
	close(results)
	first := <-results
	for dir := range results {
		if dir != first {
			t.Fatalf("concurrent state split: %s != %s", dir, first)
		}
	}
}

func TestTerminalStateRejectsUnsafeLocatorAndLegacyDirectory(t *testing.T) {
	for _, unsafe := range []string{"locator-link", "locator-traversal", "legacy-link"} {
		t.Run(unsafe, func(t *testing.T) {
			data, outside := t.TempDir(), t.TempDir()
			config := defaultWorkspaceConfig()
			base := filepath.Join(data, "terminal-private")
			if err := os.Mkdir(base, 0700); err != nil {
				t.Fatal(err)
			}
			locator := filepath.Join(base, terminalStateHash([]any{"terminal-state-v2", "local", "."})+".root.json")
			var err error
			switch unsafe {
			case "locator-link":
				err = os.Symlink(filepath.Join(outside, "secret"), locator)
			case "locator-traversal":
				err = os.WriteFile(locator, []byte(`"`+strings.Repeat("../", 21)+`x"`), 0600)
			case "legacy-link":
				legacy := terminalStateHash([]any{"local||", "local", 22, "", "/workspace"})
				err = os.Symlink(outside, filepath.Join(base, legacy))
			}
			if err != nil {
				t.Fatal(err)
			}
			if _, err := terminalStateDirectory(data, config, "/workspace"); err == nil {
				t.Fatal("accepted unsafe state")
			}
		})
	}
}
