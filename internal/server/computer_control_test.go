package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
)

func TestComputerControlToolSchemasEncodeRequiredArrays(t *testing.T) {
	encoded, err := json.Marshal(computerControlToolSchemas())
	if err != nil {
		t.Fatal(err)
	}
	var tools []map[string]any
	if err := json.Unmarshal(encoded, &tools); err != nil {
		t.Fatal(err)
	}
	if len(tools) != 7 {
		t.Fatalf("tool count=%d, want 7", len(tools))
	}
	for _, tool := range tools {
		fn := tool["function"].(map[string]any)
		name := fn["name"].(string)
		parameters := fn["parameters"].(map[string]any)
		required, ok := parameters["required"].([]any)
		if !ok {
			t.Errorf("%s required encoded as %T, want JSON array", name, parameters["required"])
			continue
		}
		if (name == "computer_status" || name == "computer_inspect" || name == "computer_snapshot") && len(required) != 0 {
			t.Errorf("%s required=%v, want empty array", name, required)
		}
		if name == "computer_open_browser" && (len(required) != 1 || required[0] != "browser") {
			t.Errorf("%s required=%v, want browser", name, required)
		}
		if name == "computer_open_browser" {
			properties := parameters["properties"].(map[string]any)
			if _, ok := properties["url"]; !ok {
				t.Error("computer_open_browser is missing the optional url argument")
			}
		}
	}
}

func TestComputerBrowserURL(t *testing.T) {
	for _, tc := range []struct {
		input string
		valid bool
	}{
		{"", true},
		{"https://github.com", true},
		{"http://localhost:9999/path?q=1", true},
		{"https://user:pass@github.com", false},
		{"file:///tmp/private", false},
		{"javascript:alert(1)", false},
		{"https://", false},
		{" https://github.com", false},
		{"https://github.com/" + strings.Repeat("a", 1024), false},
	} {
		got, err := computerBrowserURL(tc.input)
		if (err == nil) != tc.valid {
			t.Errorf("url=%q valid=%v, error=%v", tc.input, tc.valid, err)
		}
		if err == nil && got != tc.input {
			t.Errorf("url=%q normalized unexpectedly to %q", tc.input, got)
		}
	}
}

func TestComputerBrowserConfirmationShowsFullURL(t *testing.T) {
	text := computerControlConfirmationText("computer_open_browser", map[string]any{"browser": "Safari", "url": "https://github.com"})
	if !strings.Contains(text, `"url":"https://github.com"`) {
		t.Fatalf("approval text does not show destination: %s", text)
	}
}

func TestComputerControlDiscoveryFallbackOnlyForUnsupportedRoute(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want bool
	}{
		{&computerControlBridgeHTTPError{Status: http.StatusBadRequest, Message: "old bridge"}, true},
		{&computerControlBridgeHTTPError{Status: http.StatusNotFound, Message: "old bridge"}, true},
		{&computerControlBridgeHTTPError{Status: http.StatusBadGateway, Message: "Aide is foreground"}, false},
		{errors.New("bridge offline"), false},
	} {
		if got := computerControlDiscoveryUnsupported(tc.err); got != tc.want {
			t.Errorf("error=%v: unsupported=%v, want %v", tc.err, got, tc.want)
		}
	}
}
