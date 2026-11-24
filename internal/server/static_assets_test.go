package server

import (
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func TestStaticAssetServerGzipNegotiationAndValidators(t *testing.T) {
	source := "function repeated(){return 'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa';}\n" + strings.Repeat("const value = 'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa';\n", 300)
	handler := staticAssetServer(fstest.MapFS{
		"index.html": {Data: []byte(`<script src="/app.js?v=299" defer></script>`)},
		"app.js":     {Data: []byte(source)},
	})

	plain := httptest.NewRecorder()
	handler.ServeHTTP(plain, httptest.NewRequest(http.MethodGet, "/app.js", nil))
	if plain.Code != http.StatusOK || plain.Header().Get("Content-Encoding") != "" || plain.Body.String() != source {
		t.Fatalf("plain response changed: status=%d headers=%v", plain.Code, plain.Header())
	}
	plainTag := plain.Header().Get("ETag")
	if plainTag == "" || plain.Header().Get("Cache-Control") == "" {
		t.Fatal("plain validator or cache policy missing")
	}

	request := httptest.NewRequest(http.MethodGet, "/app.js", nil)
	request.Header.Set("Accept-Encoding", "br, gzip")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || response.Header().Get("Content-Encoding") != "gzip" {
		t.Fatalf("gzip response invalid: status=%d headers=%v", response.Code, response.Header())
	}
	if response.Header().Get("Vary") != "Accept-Encoding" || response.Header().Get("ETag") == plainTag {
		t.Fatalf("representation validators invalid: %v", response.Header())
	}
	reader, err := gzip.NewReader(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := io.ReadAll(reader)
	if err != nil || string(decoded) != source {
		t.Fatalf("gzip round trip failed: %v", err)
	}
	if response.Body.Len() >= len(source) {
		t.Fatalf("gzip did not reduce response: %d >= %d", response.Body.Len(), len(source))
	}

	revalidate := httptest.NewRequest(http.MethodGet, "/app.js", nil)
	revalidate.Header.Set("Accept-Encoding", "gzip")
	revalidate.Header.Set("If-None-Match", response.Header().Get("ETag"))
	notModified := httptest.NewRecorder()
	handler.ServeHTTP(notModified, revalidate)
	if notModified.Code != http.StatusNotModified {
		t.Fatalf("gzip validator not honored: %d", notModified.Code)
	}

	headRequest := httptest.NewRequest(http.MethodHead, "/app.js", nil)
	headRequest.Header.Set("Accept-Encoding", "gzip")
	head := httptest.NewRecorder()
	handler.ServeHTTP(head, headRequest)
	if head.Code != http.StatusOK || head.Body.Len() != 0 || head.Header().Get("Content-Encoding") != "gzip" || head.Header().Get("Content-Length") == "" {
		t.Fatalf("compressed HEAD invalid: status=%d headers=%v bytes=%d", head.Code, head.Header(), head.Body.Len())
	}

	rangeRequest := httptest.NewRequest(http.MethodGet, "/app.js", nil)
	rangeRequest.Header.Set("Accept-Encoding", "gzip")
	rangeRequest.Header.Set("Range", "bytes=0-8")
	rangeResponse := httptest.NewRecorder()
	handler.ServeHTTP(rangeResponse, rangeRequest)
	if rangeResponse.Code != http.StatusPartialContent || rangeResponse.Header().Get("Content-Encoding") != "" || rangeResponse.Body.String() != source[:9] {
		t.Fatalf("range must remain uncompressed: status=%d headers=%v body=%q", rangeResponse.Code, rangeResponse.Header(), rangeResponse.Body.String())
	}

	disabled := httptest.NewRequest(http.MethodGet, "/app.js", nil)
	disabled.Header.Set("Accept-Encoding", "gzip;q=0, br")
	disabledResponse := httptest.NewRecorder()
	handler.ServeHTTP(disabledResponse, disabled)
	if disabledResponse.Header().Get("Content-Encoding") != "" || disabledResponse.Body.String() != source {
		t.Fatal("gzip;q=0 must retain the original representation")
	}
}

func TestStaticAcceptsGzip(t *testing.T) {
	for header, expected := range map[string]bool{"": false, "br": false, "gzip": true, "GZip ; q=0.5": true, "gzip;q=0": false, "*;q=0.2": true, "xgzip": false} {
		if actual := staticAcceptsGzip(header); actual != expected {
			t.Errorf("staticAcceptsGzip(%q)=%v, want %v", header, actual, expected)
		}
	}
}
