package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func responsesEvent(value any) string {
	b, _ := json.Marshal(value)
	return "data: " + string(b) + "\n\n"
}

func TestOpenAIResponsesFullToolArgumentsWithoutDeltas(t *testing.T) {
	arguments := `{"path":"图纸/源/preview.png","crop":{"x":10,"y":20,"width":30,"height":40}}`
	for _, variant := range []string{"added", "itemDone", "argumentsDone", "completedOnly", "delta", "partialDelta", "addedPartial", "addedAndDelta", "legacyFinalFields"} {
		t.Run(variant, func(t *testing.T) {
			item := map[string]any{"type": "function_call", "call_id": "call_fixture", "name": "view_image"}
			var stream strings.Builder
			if variant != "completedOnly" {
				added := map[string]any{"type": "function_call", "call_id": "call_fixture", "name": "view_image"}
				if variant == "added" || variant == "addedAndDelta" {
					added["arguments"] = arguments
				} else if variant == "addedPartial" {
					added["arguments"] = arguments[:8]
				}
				stream.WriteString(responsesEvent(map[string]any{"type": "response.output_item.added", "output_index": 3, "item": added}))
			}
			if variant == "itemDone" {
				item["arguments"] = arguments
				stream.WriteString(responsesEvent(map[string]any{"type": "response.output_item.done", "output_index": 3, "item": item}))
			}
			if variant == "argumentsDone" {
				stream.WriteString(responsesEvent(map[string]any{"type": "response.function_call_arguments.done", "output_index": 3, "arguments": arguments}))
			}
			if variant == "delta" || variant == "partialDelta" || variant == "addedPartial" || variant == "addedAndDelta" || variant == "legacyFinalFields" {
				delta := arguments
				if variant == "partialDelta" {
					delta = arguments[:8]
				} else if variant == "addedPartial" {
					delta = arguments[8:]
				}
				stream.WriteString(responsesEvent(map[string]any{"type": "response.function_call_arguments.delta", "output_index": 3, "delta": delta}))
			}
			if variant != "legacyFinalFields" {
				item["arguments"] = arguments
			}
			stream.WriteString(responsesEvent(map[string]any{"type": "response.completed", "response": map[string]any{
				"status": "completed", "output": []any{map[string]any{"type": "reasoning", "encrypted_content": "fixture-cipher"}, item},
				"usage": map[string]any{"input_tokens": 20, "output_tokens": 5, "total_tokens": 25},
			}}))
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				fmt.Fprint(w, stream.String())
			}))
			defer srv.Close()
			target, _ := url.Parse(srv.URL)
			previous := http.DefaultTransport
			http.DefaultTransport = responsesTestTransport{target, previous}
			defer func() { http.DefaultTransport = previous }()
			cfg := Settings{BaseURL: "https://api.openai.com/v1", Model: "gpt-6-luna"}
			text, calls, usage, _, err := completeStream(context.Background(), cfg, []Message{{Role: "user", Content: "fixture"}}, ProfileParams{}, nil, nil, nil, nil)
			if err != nil || len(calls) != 1 || calls[0].ID != "call_fixture" || calls[0].Function.Name != "view_image" || calls[0].Function.Arguments != arguments {
				t.Fatalf("full arguments lost or duplicated: calls=%v err=%v", calls, err)
			}
			message := providerAssistantMessage(cfg, text, calls, usage)
			body := buildResponsesBody(cfg, []Message{message}, ProfileParams{}, nil, false)
			encoded, _ := json.Marshal(body["input"])
			if !bytes.Contains(encoded, []byte("fixture-cipher")) {
				t.Fatal("reasoning continuity lost")
			}
			var history []map[string]any
			if err := json.Unmarshal(encoded, &history); err != nil || history[1]["arguments"] != arguments {
				t.Fatal("executed call and retained provider history differ", string(encoded), err)
			}
		})
	}
}

func TestOpenAIResponsesRejectsIncompleteOrConflictingToolArguments(t *testing.T) {
	for _, completed := range []string{"", `{"path":"other"}`, `[]`, `null`, `{"path":`} {
		var stream strings.Builder
		stream.WriteString(responsesEvent(map[string]any{"type": "response.output_item.added", "output_index": 0, "item": map[string]any{"type": "function_call", "call_id": "c", "name": "read_file"}}))
		stream.WriteString(responsesEvent(map[string]any{"type": "response.function_call_arguments.delta", "output_index": 0, "delta": `{"path":"first"}`}))
		stream.WriteString(responsesEvent(map[string]any{"type": "response.completed", "response": map[string]any{"status": "completed", "output": []any{map[string]any{"type": "function_call", "call_id": "c", "name": "read_file", "arguments": completed}}}}))
		var out bytes.Buffer
		if err := translateResponsesStream(strings.NewReader(stream.String()), &out); err == nil || strings.Contains(out.String(), "tool_calls") || strings.Contains(out.String(), "[DONE]") {
			t.Fatal("bad arguments reached execution stream", completed, out.String(), err)
		}
	}
	var out bytes.Buffer
	truncated := responsesEvent(map[string]any{"type": "response.output_item.added", "output_index": 0, "item": map[string]any{"type": "function_call", "call_id": "c", "name": "read_file", "arguments": `{"path":"first"}`}})
	if err := translateResponsesStream(strings.NewReader(truncated), &out); err == nil || strings.Contains(out.String(), "tool_calls") {
		t.Fatal("uncompleted call emitted", err, out.String())
	}
}

func TestOpenAIResponsesMultipleCallsKeepIdentityAndOrder(t *testing.T) {
	calls := responsesStreamCalls{}
	a, b := `{"path":"甲"}`, `{"path":"乙"}`
	if err := calls.observe(3, responsesFunctionItem{CallID: "a", Name: "read_file", Arguments: &a}, false); err != nil {
		t.Fatal(err)
	}
	if err := calls.observe(8, responsesFunctionItem{CallID: "b", Name: "read_file", Arguments: &b}, false); err != nil {
		t.Fatal(err)
	}
	response := &openAIResponse{}
	for _, item := range []responsesFunctionItem{{Type: "function_call", CallID: "b", Name: "read_file", Arguments: &b}, {Type: "function_call", CallID: "a", Name: "read_file", Arguments: &a}} {
		raw, _ := json.Marshal(item)
		response.Output = append(response.Output, raw)
	}
	result, err := calls.finish(response)
	if err != nil || len(result) != 2 || result[0].ID != "b" || result[0].Function.Arguments != b || result[1].ID != "a" || result[1].Function.Arguments != a {
		t.Fatal("calls merged/reordered", result, err)
	}
	if err := calls.observe(3, responsesFunctionItem{CallID: "different", Name: "read_file"}, false); err == nil {
		t.Fatal("changed identity accepted")
	}
}
