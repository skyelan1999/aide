package server

import (
	"encoding/json"
	"strings"
	"testing"
)

// Existing serialized HTTP profiles must survive a read/save cycle. Test via
// JSON rather than a Go-only struct literal, matching an installation upgrade.
func TestMCPExistingHTTPConfigurationRoundTrip(t *testing.T) {
	wire := `{"transport":"streamable-http","url":"http://host.docker.internal:8811/mcp","dockerGateway":true}`
	var cfg SourceConfig
	if err := json.Unmarshal([]byte(wire), &cfg); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `"dockerGateway":true`) {
		t.Fatal("existing Docker gateway mode lost during config read/save")
	}
	if err := validateMCPConfig(&cfg); err != nil {
		t.Fatalf("existing HTTP profile rejected after read: %v", err)
	}
}
