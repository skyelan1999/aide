package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type voiceUploadBarrier struct {
	io.Reader
	once  sync.Once
	ready chan<- struct{}
	start <-chan struct{}
}

func (b *voiceUploadBarrier) Read(p []byte) (int, error) {
	b.once.Do(func() { b.ready <- struct{}{}; <-b.start })
	return b.Reader.Read(p)
}

func TestIssue98ConcurrentUploadsKeepEverySuccessfulSample(t *testing.T) {
	a := testApp(t)
	a.unlockPersona(t, "owned-voice-password")
	seed := voiceSampleIndexFile{Profile: personaXiaomi}
	for i := 0; i < 5000; i++ {
		seed.Items = append(seed.Items, VoiceSampleMeta{ID: fmt.Sprintf("%064x", i), Note: strings.Repeat("x", 256)})
	}
	if err := a.saveVoiceSampleIndex(personaXiaomi, seed); err != nil {
		t.Fatal(err)
	}
	const n = 4
	ready, start := make(chan struct{}, n), make(chan struct{})
	results := make(chan *httptest.ResponseRecorder, n)
	for i := 0; i < n; i++ {
		go func() {
			r := httptest.NewRequest("POST", "/api/voice-sample/upload?consented=1", nil)
			r.Body = io.NopCloser(&voiceUploadBarrier{Reader: bytes.NewReader([]byte("owned-audio")), ready: ready, start: start})
			r.Header.Set("Authorization", "Bearer "+a.token)
			w := httptest.NewRecorder()
			a.Handler().ServeHTTP(w, r)
			results <- w
		}()
	}
	for i := 0; i < n; i++ {
		<-ready
	}
	close(start)
	ids := []string{}
	for i := 0; i < n; i++ {
		w := <-results
		requireStatus(t, w, 200)
		var out struct{ ID string }
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, out.ID)
	}
	idx := a.loadVoiceSampleIndex(personaXiaomi)
	seen := map[string]bool{}
	for _, sample := range idx.Items {
		seen[sample.ID] = true
	}
	for _, id := range ids {
		if !seen[id] {
			t.Errorf("successful uploaded sample lost from index: %s", id)
		}
		if _, err := os.Stat(filepath.Join(a.voiceSamplesDir(personaXiaomi), id+".enc")); err != nil {
			t.Fatal(err)
		}
	}
	if len(idx.Items) != len(seed.Items)+n {
		t.Errorf("index has%d items, want%d", len(idx.Items), len(seed.Items)+n)
	}
	deleted := make(chan *httptest.ResponseRecorder, n)
	for _, id := range ids {
		go func(id string) { deleted <- request(a, "DELETE", "/api/voice-sample/"+id, nil) }(id)
	}
	for i := 0; i < n; i++ {
		requireStatus(t, <-deleted, 200)
	}
	if got := len(a.loadVoiceSampleIndex(personaXiaomi).Items); got != len(seed.Items) {
		t.Errorf("concurrent deletes retained%d rows, want%d", got, len(seed.Items))
	}
	for _, id := range ids {
		if _, err := os.Stat(filepath.Join(a.voiceSamplesDir(personaXiaomi), id+".enc")); !os.IsNotExist(err) {
			t.Fatal("deleted sample remains", err)
		}
	}
}

func TestIssue98CanceledMutationsPreserveSamplesAndOtherProfilesProceed(t *testing.T) {
	for _, action := range []string{"delete", "wipe"} {
		t.Run(action, func(t *testing.T) {
			a := testApp(t)
			id := strings.Repeat("a", 64)
			if err := a.saveVoiceSampleIndex(personaXiaomi, voiceSampleIndexFile{Profile: personaXiaomi, Items: []VoiceSampleMeta{{ID: id}}}); err != nil {
				t.Fatal(err)
			}
			file := filepath.Join(a.voiceSamplesDir(personaXiaomi), id+".enc")
			if err := os.WriteFile(file, []byte("owned-opaque-encrypted-file"), 0600); err != nil {
				t.Fatal(err)
			}
			release, err := a.voiceSampleMutations.acquire(context.Background(), personaXiaomi)
			if err != nil {
				t.Fatal(err)
			}
			defer release()
			method, route := "DELETE", "/api/voice-sample/"+id
			if action == "wipe" {
				method, route = "POST", "/api/voice-samples/wipe"
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			r := httptest.NewRequest(method, route, nil).WithContext(ctx)
			r.Header.Set("Authorization", "Bearer "+a.token)
			done := make(chan *httptest.ResponseRecorder, 1)
			go func() { w := httptest.NewRecorder(); a.Handler().ServeHTTP(w, r); done <- w }()
			deadline := time.Now().Add(2 * time.Second)
			for {
				a.voiceSampleMutations.mu.Lock()
				queued := len(a.voiceSampleMutations.queues[personaXiaomi]) > 1
				a.voiceSampleMutations.mu.Unlock()
				if queued {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("mutation did not wait behind profile gate")
				}
				time.Sleep(time.Millisecond)
			}
			requireStatus(t, request(a, "POST", "/api/voice-samples/wipe?profile=other", nil), 200)
			cancel()
			requireStatus(t, <-done, 409)
			if len(a.loadVoiceSampleIndex(personaXiaomi).Items) != 1 {
				t.Error("canceled mutation altered index")
			}
			if _, err := os.Stat(file); err != nil {
				t.Fatal("canceled mutation removed sample", err)
			}
		})
	}
}
