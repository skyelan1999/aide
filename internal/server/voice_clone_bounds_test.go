package server

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestIssue99CloneRejectsDuplicatesAndCountBeforeOutbound(t *testing.T) {
	for _, kind := range []string{"duplicate", "count"} {
		t.Run(kind, func(t *testing.T) {
			calls := 0
			backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				io.Copy(io.Discard, r.Body)
				jsonOut(w, 200, map[string]any{"voice_id": "owned-clone"})
			}))
			defer backend.Close()
			a := testApp(t)
			a.unlockPersona(t, "owned-voice-password")
			a.settings.CloneTTSBaseURL = backend.URL
			a.settings.CloneTTSBackend = "gpt-sovits"
			key := deriveKey("owned-voice-password")
			dir := a.voiceSamplesDir(personaXiaomi)
			if err := os.MkdirAll(dir, 0700); err != nil {
				t.Fatal(err)
			}
			ids := []string{}
			for i := 0; i < 17; i++ {
				id := fmt.Sprintf("%064x", i+1)
				if err := encryptSampleFile(key, filepath.Join(dir, id+".enc"), []byte("owned-audio")); err != nil {
					t.Fatal(err)
				}
				ids = append(ids, id)
			}
			if kind == "duplicate" {
				ids = []string{ids[0], ids[0]}
			}
			w := request(a, "POST", "/api/voice-clone/create", map[string]any{"sampleIds": ids})
			if w.Code != 400 {
				t.Errorf("invalid %s clone selection returned%d", kind, w.Code)
			}
			if calls != 0 {
				t.Errorf("invalid selection sent%d outbound requests", calls)
			}
		})
	}
}

func TestIssue99CloneRejectsAggregateAndOversizedStoredSamples(t *testing.T) {
	for _, kind := range []string{"aggregate", "oversized-cipher"} {
		t.Run(kind, func(t *testing.T) {
			a := testApp(t)
			a.unlockPersona(t, "owned-password")
			a.settings.CloneTTSBackend = "gpt-sovits"
			// An invalid selection must fail locally before this unreachable endpoint.
			a.settings.CloneTTSBaseURL = "http://127.0.0.1:1"
			dir := a.voiceSamplesDir(personaXiaomi)
			if err := os.MkdirAll(dir, 0700); err != nil {
				t.Fatal(err)
			}
			key := deriveKey("owned-password")
			ids := []string{}
			for i := 0; i < 3; i++ {
				id := fmt.Sprintf("%064x", i+1)
				ids = append(ids, id)
				if err := encryptSampleFile(key, filepath.Join(dir, id+".enc"), bytes.Repeat([]byte{'a'}, 12<<20)); err != nil {
					t.Fatal(err)
				}
			}
			if kind == "oversized-cipher" {
				ids = ids[:1]
				f, err := os.OpenFile(filepath.Join(dir, ids[0]+".enc"), os.O_WRONLY, 0600)
				if err != nil {
					t.Fatal(err)
				}
				if err := f.Truncate(voiceSampleMaxBytes + voiceSampleCipherOverhead + 1); err != nil {
					t.Fatal(err)
				}
				f.Close()
				if _, err := decryptSampleFile(key, filepath.Join(dir, ids[0]+".enc")); err == nil {
					t.Fatal("oversized encrypted file read without limit")
				}
			}
			requireStatus(t, request(a, "POST", "/api/voice-clone/create", map[string]any{"sampleIds": ids}), 400)
		})
	}
}

func TestIssue99ValidMaximumCountSendsExactSamples(t *testing.T) {
	wantAudio := []byte("owned-voice-sample")
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Samples []struct {
				ID    string `json:"id"`
				Audio string `json:"audioB64"`
			} `json:"samples"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
		}
		if len(payload.Samples) != 16 {
			t.Errorf("outbound count%d, want16", len(payload.Samples))
		}
		for _, sample := range payload.Samples {
			raw, err := base64.StdEncoding.DecodeString(sample.Audio)
			if err != nil || !bytes.Equal(raw, wantAudio) {
				t.Error("audio payload changed", err)
			}
		}
		jsonOut(w, 200, map[string]any{"voice_id": "owned-maximum-clone"})
	}))
	defer backend.Close()
	a := testApp(t)
	a.unlockPersona(t, "owned-password")
	a.settings.CloneTTSBackend = "gpt-sovits"
	a.settings.CloneTTSBaseURL = backend.URL
	dir := a.voiceSamplesDir(personaXiaomi)
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	key := deriveKey("owned-password")
	ids := []string{}
	for i := 0; i < 16; i++ {
		id := fmt.Sprintf("%064x", i+1)
		ids = append(ids, id)
		if err := encryptSampleFile(key, filepath.Join(dir, id+".enc"), wantAudio); err != nil {
			t.Fatal(err)
		}
	}
	requireStatus(t, request(a, "POST", "/api/voice-clone/create", map[string]any{"sampleIds": ids}), 200)
}
