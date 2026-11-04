package server

import (
	"encoding/json"
	"testing"
)

func TestComputerControlBridgeUsesRuntimeLocalAddress(t *testing.T) {
	t.Run("native package ignores Docker gateway", func(t *testing.T) {
		t.Setenv("AIDE_RUNTIME_MODE", "native")
		t.Setenv("AIDE_COMPUTER_BRIDGE_URL", computerControlDefaultBridge)
		if got := computerControlBridgeURL(); got != computerControlLoopbackBridge {
			t.Fatalf("native bridge URL=%q, want %q", got, computerControlLoopbackBridge)
		}
	})
	t.Run("container keeps Docker gateway", func(t *testing.T) {
		t.Setenv("AIDE_RUNTIME_MODE", "container")
		t.Setenv("AIDE_COMPUTER_BRIDGE_URL", "")
		if got := computerControlBridgeURL(); got != computerControlDefaultBridge {
			t.Fatalf("container bridge URL=%q, want %q", got, computerControlDefaultBridge)
		}
	})
	t.Run("native never accepts remote override", func(t *testing.T) {
		t.Setenv("AIDE_RUNTIME_MODE", "native")
		t.Setenv("AIDE_COMPUTER_BRIDGE_URL", "https://example.invalid:17778")
		if got := computerControlBridgeURL(); got != computerControlLoopbackBridge {
			t.Fatalf("native bridge URL=%q, want pinned loopback %q", got, computerControlLoopbackBridge)
		}
	})
}

func TestComputerControlToolSchemasEncodeRequiredArrays(t *testing.T) {
	encoded, err := json.Marshal(computerControlToolSchemas())
	if err != nil {
		t.Fatal(err)
	}
	var tools []map[string]any
	if err := json.Unmarshal(encoded, &tools); err != nil {
		t.Fatal(err)
	}
	if len(tools) != 6 {
		t.Fatalf("tool count=%d, want 6", len(tools))
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
	}
}
