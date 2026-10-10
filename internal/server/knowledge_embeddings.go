package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Keys are supplied by the caller from SecretVault, never persisted here.
type knowledgeEmbeddingProvider struct {
	BaseURL string
	Model   string
	APIKey  string
}

// Provider indices are local to each batch. Only vectors cross this boundary;
// file identity, hashes and citation locators remain in the source snapshot.
func knowledgeEmbeddings(ctx context.Context, cfg knowledgeEmbeddingProvider, texts []string) ([][]float64, error) {
	u, err := url.Parse(cfg.BaseURL)
	if err != nil || u == nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || strings.TrimSpace(cfg.Model) == "" {
		return nil, errors.New("invalid embedding provider configuration")
	}
	if len(texts) == 0 || len(texts) > 2501 {
		return nil, errors.New("embedding input count exceeds limit")
	}
	total := 0
	for _, text := range texts {
		total += len(text)
		if strings.TrimSpace(text) == "" || len(text) > 32<<10 || total > 16<<20 {
			return nil, errors.New("embedding input size invalid or exceeds limit")
		}
	}
	u.Path = strings.TrimRight(u.Path, "/") + "/embeddings"
	u.RawPath = ""
	client := &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	out := make([][]float64, 0, len(texts))
	dimension := 0
	for start := 0; start < len(texts); start += 32 {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		end := start + 32
		if end > len(texts) {
			end = len(texts)
		}
		body, err := json.Marshal(map[string]any{"model": cfg.Model, "input": texts[start:end], "encoding_format": "float"})
		if err != nil {
			return nil, errors.New("embedding request encoding failed")
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), bytes.NewReader(body))
		if err != nil {
			return nil, errors.New("embedding request creation failed")
		}
		req.Header.Set("Content-Type", "application/json")
		if cfg.APIKey != "" {
			req.Header.Set("Authorization", "Bearer "+cfg.APIKey)
		}
		resp, err := client.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			// Transport errors may include endpoint credentials or query text.
			return nil, errors.New("embedding provider connection failed")
		}
		vectors, nextDimension, err := decodeKnowledgeEmbeddings(resp, end-start, dimension)
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if err != nil {
			return nil, err
		}
		dimension = nextDimension
		out = append(out, vectors...)
	}
	return out, nil
}

func decodeKnowledgeEmbeddings(resp *http.Response, count, dimension int) ([][]float64, int, error) {
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, 0, fmt.Errorf("embedding provider returned HTTP %d", resp.StatusCode)
	}
	const maxResponse = 8 << 20
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxResponse+1))
	if err != nil || len(b) > maxResponse {
		return nil, 0, errors.New("embedding response unreadable or exceeds limit")
	}
	var payload struct {
		Data []struct {
			Index     *int      `json:"index"`
			Embedding []float64 `json:"embedding"`
		} `json:"data"`
	}
	if json.Unmarshal(b, &payload) != nil || len(payload.Data) != count {
		return nil, 0, errors.New("embedding response count or format invalid")
	}
	out := make([][]float64, count)
	for _, item := range payload.Data {
		if item.Index == nil || *item.Index < 0 || *item.Index >= count || out[*item.Index] != nil {
			return nil, 0, errors.New("embedding response indices invalid")
		}
		if dimension == 0 {
			dimension = len(item.Embedding)
		}
		v, err := documentUnitVector(item.Embedding, dimension)
		if err != nil {
			return nil, 0, err
		}
		out[*item.Index] = v
	}
	return out, dimension, nil
}
