package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
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

func TestMiMoUnauthorizedGuidance(t *testing.T) {
	const secret = "tp-test-only-secret"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+secret {
			t.Error("request did not use the selected credential as bearer")
		}
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"invalid fixture credential ` + secret + `"}}`))
	}))
	defer srv.Close()

	target, _ := url.Parse(srv.URL)
	original := http.DefaultTransport
	http.DefaultTransport = responsesTestTransport{target, original}
	t.Cleanup(func() { http.DefaultTransport = original })

	a := testApp(t)
	for _, tc := range []struct {
		base string
		want string
	}{
		{"https://api.xiaomimimo.com/v1", "按量计费的 sk- 密钥"},
		{"https://token-plan-cn.xiaomimimo.com/v1", "有效套餐的 tp- 或 ttp- 密钥"},
		{"https://token-plan-sgp.xiaomimimo.com/v1", "有效套餐的 tp- 或 ttp- 密钥"},
		{"https://token-plan-ams.xiaomimimo.com/v1", "有效套餐的 tp- 或 ttp- 密钥"},
	} {
		t.Run(tc.base, func(t *testing.T) {
			check := func(message string) {
				t.Helper()
				if !strings.Contains(message, "HTTP 401") || !strings.Contains(message, tc.want) {
					t.Errorf("missing MiMo credential guidance: %s", message)
				}
				if strings.Contains(message, secret) {
					t.Error("MiMo diagnostic exposed the test credential")
				}
			}
			cfg := Settings{BaseURL: tc.base, Model: "mimo-v2.6-pro", APIKey: secret}
			_, _, _, err := complete(context.Background(), cfg, nil, ProfileParams{}, nil, nil)
			if err == nil {
				t.Fatal("expected authentication failure")
			}
			check(err.Error())
			_, _, _, _, err = completeStream(context.Background(), cfg, nil, ProfileParams{}, nil, nil, nil, nil)
			if err == nil {
				t.Fatal("expected streaming authentication failure")
			}
			check(err.Error())
			discovery := request(a, "POST", "/api/models", map[string]any{"baseURL": tc.base, "apiKey": secret})
			requireStatus(t, discovery, http.StatusBadRequest)
			check(discovery.Body.String())
		})
	}

	for _, base := range []string{"https://api.xiaomimimo.com.example.invalid/v1", "https://example.invalid/v1", "http://api.xiaomimimo.com/v1"} {
		if err := mimoAuthenticationError(base, http.StatusUnauthorized); err != nil {
			t.Errorf("unrelated endpoint %s got MiMo guidance", base)
		}
	}
	if err := mimoAuthenticationError("https://api.xiaomimimo.com/v1", http.StatusForbidden); err != nil {
		t.Error("MiMo HTTP 403 was treated as HTTP 401")
	}
}
