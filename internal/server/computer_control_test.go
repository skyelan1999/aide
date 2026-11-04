package server

import (
	"encoding/json"
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
