package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCompleteIncludesSafeProviderErrorDetail(t *testing.T) {
	const secret = "test-secret-key"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"invalid model configuration"}}`))
	}))
	defer srv.Close()
	_, _, _, err := complete(context.Background(), Settings{BaseURL: srv.URL, Model: "test", APIKey: secret}, nil, ProfileParams{}, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "HTTP 400") || !strings.Contains(err.Error(), "invalid model configuration") {
		t.Fatalf("expected provider diagnostic, got %v", err)
	}
	if strings.Contains(err.Error(), secret) || strings.Contains(err.Error(), srv.URL) {
		t.Fatalf("provider diagnostic leaked credentials or endpoint: %v", err)
	}
}
