package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestKnowledgeEmbeddingsBatchOrder(t *testing.T) {
	var calls atomic.Int32
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/embeddings" || r.Header.Get("Authorization") != "Bearer fixture-key" {
			t.Error("request path/auth mismatch")
		}
		var body struct {
			Model    string   `json:"model"`
			Input    []string `json:"input"`
			Encoding string   `json:"encoding_format"`
		}
		if json.NewDecoder(r.Body).Decode(&body) != nil || body.Model != "fixture" || body.Encoding != "float" {
			t.Error("invalid request")
		}
		batch := int(calls.Add(1)) - 1
		if len(body.Input) > 32 {
			t.Error("oversized batch")
		}
		data := []map[string]any{}
		for i := len(body.Input) - 1; i >= 0; i-- {
			if body.Input[i] != fmt.Sprint(batch*32+i) {
				t.Error("input order changed")
			}
			data = append(data, map[string]any{"index": i, "embedding": []float64{float64(batch*32 + i + 1), 1}})
		}
		json.NewEncoder(w).Encode(map[string]any{"data": data})
	}))
	defer s.Close()
	texts := make([]string, 33)
	for i := range texts {
		texts[i] = fmt.Sprint(i)
	}
	v, err := knowledgeEmbeddings(context.Background(), knowledgeEmbeddingProvider{s.URL + "/v1", "fixture", "fixture-key"}, texts)
	if err != nil || len(v) != 33 || calls.Load() != 2 {
		t.Fatalf("batch result: %d %v", len(v), err)
	}
	for i := range v {
		if diff := v[i][0]/v[i][1] - float64(i+1); diff < -1e-10 || diff > 1e-10 {
			t.Fatal("provider index order lost")
		}
	}
}

func TestKnowledgeEmbeddingsMalformed(t *testing.T) {
	for _, body := range []string{
		`{"data":[]}`,
		`{"data":[{"embedding":[1]}]}`,
		`{"data":[{"index":2,"embedding":[1]}]}`,
		`{"data":[{"index":0,"embedding":[]}]}`,
		`{"data":[{"index":0,"embedding":[1e999]}]}`,
		`{"data":[{"index":0,"embedding":[1]}]} trailing`,
	} {
		t.Run(body, func(t *testing.T) {
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, body) }))
			defer s.Close()
			if _, err := knowledgeEmbeddings(context.Background(), knowledgeEmbeddingProvider{s.URL, "fixture", ""}, []string{"text"}); err == nil {
				t.Fatal("accepted malformed provider response")
			}
		})
	}
}

func TestKnowledgeEmbeddingsCancelAndRedirect(t *testing.T) {
	started := make(chan struct{})
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		close(started)
		<-r.Context().Done()
	}))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := knowledgeEmbeddings(ctx, knowledgeEmbeddingProvider{s.URL, "fixture", ""}, []string{"text"})
		done <- err
	}()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("request did not start")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancel: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("cancel did not stop request")
	}
	s.Close()
	var forwarded atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { forwarded.Add(1) }))
	defer target.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer redirect.Close()
	_, err := knowledgeEmbeddings(context.Background(), knowledgeEmbeddingProvider{redirect.URL, "fixture", "secret-fixture"}, []string{"private-text"})
	if err == nil || forwarded.Load() != 0 || strings.Contains(err.Error(), "secret-fixture") || strings.Contains(err.Error(), "private-text") {
		t.Fatalf("redirect/error boundary: %v", err)
	}
}

func TestKnowledgeEmbeddingsDimensionAndDuplicateIndices(t *testing.T) {
	for _, body := range []string{
		`{"data":[{"index":0,"embedding":[1,0]},{"index":0,"embedding":[0,1]}]}`,
		`{"data":[{"index":0,"embedding":[1,0]},{"index":1,"embedding":[1]}]}`,
	} {
		response := &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}
		if _, _, err := decodeKnowledgeEmbeddings(response, 2, 0); err == nil {
			t.Fatal("invalid dimensions/duplicates accepted")
		}
	}
	response := &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"data":[{"index":0,"embedding":[1]}]}`))}
	if _, _, err := decodeKnowledgeEmbeddings(response, 1, 2); err == nil {
		t.Fatal("dimension changed across batches")
	}
	for _, endpoint := range []string{"ftp://host", "https://user:password@host", "https://host?token=secret", "https://host#fragment", "https://"} {
		if _, err := knowledgeEmbeddings(context.Background(), knowledgeEmbeddingProvider{endpoint, "fixture", ""}, []string{"text"}); err == nil {
			t.Fatal("invalid endpoint accepted")
		}
	}
}
