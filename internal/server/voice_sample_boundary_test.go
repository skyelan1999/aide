package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestIssue150HTTPHistoryTransitionsReportSaveFailure(t *testing.T) {
	for _, action := range []string{"enable", "change-password", "disable"} {
		t.Run(action, func(t *testing.T) {
			a := testApp(t)
			va := a.voiceAgent
			va.recordFallback("owned-history", "fixture")
			if action != "enable" {
				if err := va.enable("owned-old-secret"); err != nil {
					t.Fatal(err)
				}
			}
			p := VoiceHistoryPath(a.dataPath)
			original, err := os.ReadFile(p)
			if err != nil {
				t.Fatal(err)
			}
			backup := p + ".owned-fixture"
			if err := os.Rename(p, backup); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(p, 0700); err != nil {
				t.Fatal(err)
			}
			key, cipher, encrypted := append([]byte(nil), va.key...), va.cachedCipher, va.encrypted
			history := va.historyDesc()
			body := map[string]string{"password": "owned-old-secret", "oldPassword": "owned-old-secret", "newPassword": "owned-new-secret"}
			w := request(a, "POST", "/api/voice-history/"+action, body)
			if w.Code != 400 {
				t.Fatalf("failed save reported status %d: %s", w.Code, w.Body.String())
			}
			preserved, err := os.ReadFile(backup)
			if err != nil || !bytes.Equal(original, preserved) || va.encrypted != encrypted || va.cachedCipher != cipher || !bytes.Equal(va.key, key) || !reflect.DeepEqual(history, va.historyDesc()) {
				t.Fatal("failed transition modified previous state", err)
			}
			if err := os.Remove(p); err != nil {
				t.Fatal(err)
			}
			if err := os.Rename(backup, p); err != nil {
				t.Fatal(err)
			}
			requireStatus(t, request(a, "POST", "/api/voice-history/"+action, body), 200)
		})
	}
}

func ownedVoiceSample(t *testing.T, a *App) string {
	t.Helper()
	a.unlockPersona(t, "owned-sample-secret")
	w := rawUpload(a, makeWAV(8000, 1), "consented=1&transcript=owned-fixture")
	requireStatus(t, w, 200)
	var out struct{ ID string }
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil || out.ID == "" {
		t.Fatal("sample upload failed", err)
	}
	return out.ID
}

func TestIssue151LockedAppRejectsVoiceSampleOperations(t *testing.T) {
	a := testApp(t)
	id := ownedVoiceSample(t, a)
	var calls atomic.Int32
	clone := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		jsonOut(w, 200, map[string]string{"voice_id": "owned-fixture-voice"})
	}))
	defer clone.Close()
	a.mu.Lock()
	a.settings.CloneTTSBaseURL = clone.URL
	a.mu.Unlock()
	if _, err := a.writeLockState(true); err != nil {
		t.Fatal(err)
	}
	for _, operation := range []string{"audio", "upload", "clone", "infer"} {
		t.Run(operation, func(t *testing.T) {
			var w *httptest.ResponseRecorder
			switch operation {
			case "audio":
				w = request(a, "GET", "/api/voice-sample/"+id+"/audio", nil)
			case "upload":
				w = rawUpload(a, []byte("owned-audio"), "consented=1")
			case "clone":
				w = request(a, "POST", "/api/voice-clone/create", map[string]any{"sampleIds": []string{id}})
			case "infer":
				w = request(a, "POST", "/api/voice-personality/infer", map[string]any{"sampleIds": []string{id}})
			}
			if w.Code != 403 {
				t.Fatalf("locked %s status=%d, body length=%d", operation, w.Code, w.Body.Len())
			}
		})
	}
	if calls.Load() != 0 || len(a.loadVoiceSampleIndex(personaXiaomi).Items) != 1 {
		t.Error("locked request reached clone service or changed samples")
	}
	if _, err := a.writeLockState(false); err != nil {
		t.Fatal(err)
	}
	requireStatus(t, request(a, "GET", "/api/voice-sample/"+id+"/audio", nil), 200)
}

func blockOwnedSampleRemoval(t *testing.T, a *App, id string) string {
	t.Helper()
	p := filepath.Join(a.voiceSamplesDir(personaXiaomi), id+".enc")
	if err := os.Remove(p); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(p, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(p, "owned-blocker"), []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestIssue152FailedDeleteRetainsIndex(t *testing.T) {
	a := testApp(t)
	id := ownedVoiceSample(t, a)
	blockOwnedSampleRemoval(t, a, id)
	before, err := os.ReadFile(a.voiceSampleIndexPath(personaXiaomi))
	if err != nil {
		t.Fatal(err)
	}
	requireStatus(t, request(a, "DELETE", "/api/voice-sample/"+id, nil), 500)
	after, err := os.ReadFile(a.voiceSampleIndexPath(personaXiaomi))
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("failed deletion removed metadata", err)
	}
}

func TestIssue152WipeReportsPartialDeletionAndSupportsRetry(t *testing.T) {
	a := testApp(t)
	blocked, removed := ownedVoiceSample(t, a), ownedVoiceSample(t, a)
	p := blockOwnedSampleRemoval(t, a, blocked)
	w := request(a, "POST", "/api/voice-samples/wipe", nil)
	requireStatus(t, w, 500)
	var result struct {
		Removed int `json:"removed"`
		Failed  int `json:"failed"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil || result.Removed != 1 || result.Failed != 1 {
		t.Fatal("inaccurate partial deletion receipt", w.Body.String(), err)
	}
	idx := a.loadVoiceSampleIndex(personaXiaomi)
	if len(idx.Items) != 1 || idx.Items[0].ID != blocked {
		t.Fatal("failed sample disappeared from index", idx)
	}
	if _, err := os.Stat(filepath.Join(a.voiceSamplesDir(personaXiaomi), removed+".enc")); !os.IsNotExist(err) {
		t.Fatal("successful deletion left sample", err)
	}
	if err := os.Remove(filepath.Join(p, "owned-blocker")); err != nil {
		t.Fatal(err)
	}
	requireStatus(t, request(a, "POST", "/api/voice-samples/wipe", nil), 200)
	requireStatus(t, request(a, "POST", "/api/voice-samples/wipe", nil), 200)
}

func TestIssue152WipeRejectsDirectoryAndIndexIOFailures(t *testing.T) {
	for _, kind := range []string{"directory", "index"} {
		t.Run(kind, func(t *testing.T) {
			a := testApp(t)
			id := ownedVoiceSample(t, a)
			p := a.voiceSamplesDir(personaXiaomi)
			if kind == "index" {
				p = a.voiceSampleIndexPath(personaXiaomi)
			}
			if err := os.Rename(p, p+".owned-fixture"); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(p, []byte("owned-blocker"), 0600); err != nil {
				t.Fatal(err)
			}
			requireStatus(t, request(a, "POST", "/api/voice-samples/wipe", nil), 500)
			if kind == "index" {
				if _, err := os.Stat(filepath.Join(a.voiceSamplesDir(personaXiaomi), id+".enc")); err != nil {
					t.Fatal("index read failure still deleted sample", err)
				}
			}
		})
	}
}

func TestIssue152UnindexedAudioCannotBeDecrypted(t *testing.T) {
	a := testApp(t)
	id := ownedVoiceSample(t, a)
	if err := a.saveVoiceSampleIndex(personaXiaomi, voiceSampleIndexFile{Profile: personaXiaomi}); err != nil {
		t.Fatal(err)
	}
	w := request(a, "GET", "/api/voice-sample/"+id+"/audio", nil)
	if w.Code != 404 {
		t.Fatalf("unindexed audio status=%d, body length=%d", w.Code, w.Body.Len())
	}
	if strings.Contains(w.Body.String(), "RIFF") {
		t.Fatal("unindexed sample bytes disclosed")
	}
}

func TestIssue151QueuedSampleOperationsRecheckLock(t *testing.T) {
	for _, operation := range []string{"audio", "upload", "clone", "infer"} {
		t.Run(operation, func(t *testing.T) {
			a := testApp(t)
			id := ownedVoiceSample(t, a)
			a.mu.Lock()
			a.settings.CloneTTSBaseURL = "http://127.0.0.1:1"
			a.settings.CloneTTSBackend = "gpt-sovits"
			a.mu.Unlock()
			release, err := a.voiceSampleMutations.acquire(context.Background(), personaXiaomi)
			if err != nil {
				t.Fatal(err)
			}
			defer release()
			done := make(chan *httptest.ResponseRecorder, 1)
			go func() {
				switch operation {
				case "audio":
					done <- request(a, "GET", "/api/voice-sample/"+id+"/audio", nil)
				case "upload":
					done <- rawUpload(a, []byte("owned-audio"), "consented=1")
				case "clone":
					done <- request(a, "POST", "/api/voice-clone/create", map[string]any{"sampleIds": []string{id}})
				case "infer":
					done <- request(a, "POST", "/api/voice-personality/infer", map[string]any{"sampleIds": []string{id}})
				}
			}()
			deadline := time.Now().Add(3 * time.Second)
			for {
				a.voiceSampleMutations.mu.Lock()
				queued := len(a.voiceSampleMutations.queues[personaXiaomi]) > 1
				a.voiceSampleMutations.mu.Unlock()
				if queued {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("sample operation did not enter profile queue")
				}
				time.Sleep(time.Millisecond)
			}
			if _, err := a.writeLockState(true); err != nil {
				t.Fatal(err)
			}
			release()
			select {
			case w := <-done:
				if w.Code != 403 {
					t.Fatalf("queued %s bypassed lock: status=%d", operation, w.Code)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("queued sample request did not exit")
			}
			if len(a.loadVoiceSampleIndex(personaXiaomi).Items) != 1 {
				t.Fatal("queued upload changed samples while locked")
			}
		})
	}
}

func TestIssue152WipeReportsIndexSaveFailure(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("uses read-only Linux proc fd directory as a synthetic write failure")
	}
	a := testApp(t)
	dir := a.voiceSamplesDir(personaXiaomi)
	if err := os.MkdirAll(filepath.Dir(dir), 0700); err != nil {
		t.Fatal(err)
	}
	// Numeric descriptor entries cannot match *.enc; proc rejects index writes.
	if err := os.Symlink("/proc/self/fd", dir); err != nil {
		t.Fatal(err)
	}
	w := request(a, "POST", "/api/voice-samples/wipe", nil)
	requireStatus(t, w, 500)
	var out struct {
		IndexSaved bool `json:"indexSaved"`
		Removed    int  `json:"removed"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil || out.IndexSaved || out.Removed != 0 {
		t.Fatal("index save failure claimed success", err)
	}
}

func TestIssue151CorruptLockStateFailsClosed(t *testing.T) {
	a := testApp(t)
	id := ownedVoiceSample(t, a)
	a.mu.Lock()
	a.settings.UserPasswordHash = "owned-configured-hash-marker"
	a.mu.Unlock()
	if err := os.WriteFile(lockStatePath(a.dataPath), []byte("invalid-json"), 0600); err != nil {
		t.Fatal(err)
	}
	w := request(a, "GET", "/api/voice-sample/"+id+"/audio", nil)
	if w.Code != 403 {
		t.Fatalf("corrupt lock state accepted retained key: status=%d", w.Code)
	}
}
