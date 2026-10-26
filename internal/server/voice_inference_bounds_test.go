package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func inferenceFixture(t *testing.T, sizes []int, transcript string) (*App, []string, *atomic.Int32, <-chan string) {
	t.Helper()
	a := testApp(t)
	a.unlockPersona(t, "owned-inference-secret")
	var calls atomic.Int32
	requests := make(chan string, 4)
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		var body struct {
			Messages []Message `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if len(body.Messages) > 0 {
			select {
			case requests <- body.Messages[len(body.Messages)-1].Content:
			default:
			}
		}
		jsonOut(w, 200, map[string]any{"choices": []any{map[string]any{"message": Message{Role: "assistant", Content: `{"profile":{},"promptDraft":"你是小秘。fixture"}`}}}})
	}))
	t.Cleanup(backend.Close)
	a.mu.Lock()
	a.settings.BaseURL, a.settings.Model = backend.URL, "owned-test-model"
	a.mu.Unlock()
	dir := a.voiceSamplesDir(personaXiaomi)
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	key := deriveKey("owned-inference-secret")
	idx := voiceSampleIndexFile{Profile: personaXiaomi}
	var ids []string
	for i, size := range sizes {
		id := fmt.Sprintf("%064x", i+1)
		if err := encryptSampleFile(key, filepath.Join(dir, id+".enc"), bytes.Repeat([]byte{'a'}, size)); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
		idx.Items = append(idx.Items, VoiceSampleMeta{ID: id, Bytes: size, DurationSec: 1, Transcript: transcript})
	}
	if err := a.saveVoiceSampleIndex(personaXiaomi, idx); err != nil {
		t.Fatal(err)
	}
	return a, ids, &calls, requests
}

func TestIssue156InferenceRejectsExcessWorkBeforeModel(t *testing.T) {
	for _, kind := range []string{"count", "aggregate", "oversized", "corrupt", "invalid-id", "unknown-id", "special-file"} {
		t.Run(kind, func(t *testing.T) {
			sizes := []int{64}
			if kind == "aggregate" {
				sizes = []int{12 << 20, 12 << 20, 12 << 20}
			}
			a, ids, calls, _ := inferenceFixture(t, sizes, "owned transcript")
			switch kind {
			case "count":
				for len(ids) < 17 {
					ids = append(ids, ids[0])
				}
			case "oversized":
				f, err := os.OpenFile(sampleEncPath(a, personaXiaomi, ids[0]), os.O_WRONLY, 0600)
				if err != nil {
					t.Fatal(err)
				}
				if err := f.Truncate(voiceSampleMaxBytes + voiceSampleCipherOverhead + 1); err != nil {
					t.Fatal(err)
				}
				f.Close()
			case "corrupt":
				if err := os.WriteFile(sampleEncPath(a, personaXiaomi, ids[0]), bytes.Repeat([]byte{'x'}, 64), 0600); err != nil {
					t.Fatal(err)
				}
			case "invalid-id":
				ids = append(ids, "../unbound")
			case "unknown-id":
				ids = append(ids, strings.Repeat("f", 64))
			case "special-file":
				path := sampleEncPath(a, personaXiaomi, ids[0])
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(path, 0700); err != nil {
					t.Fatal(err)
				}
			}
			w := request(a, "POST", "/api/voice-personality/infer", map[string]any{"sampleIds": ids})
			status := 400
			if kind == "unknown-id" {
				status = 404
			}
			if w.Code != status || calls.Load() != 0 {
				t.Fatalf("invalid %s inference status=%d, outbound calls=%d", kind, w.Code, calls.Load())
			}
		})
	}
}

func TestIssue156CanceledQueuedInferenceDoesNotCallModel(t *testing.T) {
	a, ids, calls, _ := inferenceFixture(t, []int{64}, "owned transcript")
	release, err := a.voiceSampleMutations.acquire(context.Background(), personaXiaomi)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	body, err := json.Marshal(map[string]any{"sampleIds": ids})
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("POST", "/api/voice-personality/infer", bytes.NewReader(body)).WithContext(ctx)
	r.Header.Set("Authorization", "Bearer "+a.token)
	done := make(chan int, 1)
	go func() { w := httptest.NewRecorder(); a.Handler().ServeHTTP(w, r); done <- w.Code }()
	deadline := time.Now().Add(3 * time.Second)
	for {
		a.voiceSampleMutations.mu.Lock()
		queued := len(a.voiceSampleMutations.queues[personaXiaomi]) > 1
		a.voiceSampleMutations.mu.Unlock()
		if queued {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("inference did not wait for profile queue")
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	select {
	case status := <-done:
		if status != 409 || calls.Load() != 0 {
			t.Fatalf("canceled inference status=%d outbound=%d", status, calls.Load())
		}
	case <-time.After(3 * time.Second):
		t.Fatal("canceled inference did not leave queue")
	}
}

func TestIssue156InferenceDeduplicatesSelectedSamples(t *testing.T) {
	a, ids, calls, _ := inferenceFixture(t, []int{64}, "owned transcript")
	w := request(a, "POST", "/api/voice-personality/infer", map[string]any{"sampleIds": []string{ids[0], ids[0]}})
	requireStatus(t, w, 200)
	var out struct {
		Features voiceAcousticFeatures `json:"features"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil || out.Features.SampleCount != 1 || out.Features.DurationSec != 1 || calls.Load() != 1 {
		t.Fatal("duplicate selection counted/decrypted repeatedly", out, err)
	}
}

func TestIssue156InferenceAllowsExactAudioAndCountLimits(t *testing.T) {
	for _, kind := range []string{"audio", "count"} {
		t.Run(kind, func(t *testing.T) {
			sizes := []int{15 << 20, 15 << 20, 2 << 20}
			if kind == "count" {
				sizes = make([]int, 16)
				for i := range sizes {
					sizes[i] = 64
				}
			}
			a, ids, calls, _ := inferenceFixture(t, sizes, "owned transcript")
			w := request(a, "POST", "/api/voice-personality/infer", map[string]any{"sampleIds": ids})
			requireStatus(t, w, 200)
			var out struct {
				Features voiceAcousticFeatures `json:"features"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil || out.Features.SampleCount != len(ids) || calls.Load() != 1 {
				t.Fatal("valid boundary rejected or changed sample count", out, err)
			}
		})
	}
}

func TestIssue156InferenceBoundsTranscriptBeforeFeatureAndPromptWork(t *testing.T) {
	a, ids, _, requests := inferenceFixture(t, []int{64}, strings.Repeat("界", 100000))
	w := request(a, "POST", "/api/voice-personality/infer", map[string]any{"sampleIds": ids})
	requireStatus(t, w, 200)
	var out struct {
		Features voiceAcousticFeatures `json:"features"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil || out.Features.CharsPerSec > 800 {
		t.Fatal("full transcript processed before truncation", out, err)
	}
	select {
	case prompt := <-requests:
		parts := strings.SplitN(prompt, "转写片段：\n\"\"\"\n", 2)
		if len(parts) != 2 || strings.Count(strings.SplitN(parts[1], "\n\"\"\"", 2)[0], "界") != 800 {
			t.Fatal("model transcript excerpt changed its character boundary")
		}
	default:
		t.Fatal("owned model did not receive bounded inference")
	}
}
