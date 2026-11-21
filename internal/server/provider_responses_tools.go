package server

import (
	"encoding/json"
	"errors"
	"sort"
	"strings"
)

type responsesFunctionItem struct {
	Type      string  `json:"type"`
	CallID    string  `json:"call_id"`
	Name      string  `json:"name"`
	Arguments *string `json:"arguments"`
}

type responsesStreamCall struct {
	item  responsesFunctionItem
	delta strings.Builder
	done  *string
}

type responsesStreamCalls map[int]*responsesStreamCall

func (calls responsesStreamCalls) slot(index int) *responsesStreamCall {
	if calls[index] == nil {
		calls[index] = &responsesStreamCall{}
	}
	return calls[index]
}

func (calls responsesStreamCalls) observe(index int, item responsesFunctionItem, done bool) error {
	c := calls.slot(index)
	if (c.item.CallID != "" && item.CallID != "" && c.item.CallID != item.CallID) ||
		(c.item.Name != "" && item.Name != "" && c.item.Name != item.Name) {
		return errors.New("OpenAI Responses tool identity changed during stream")
	}
	if item.CallID != "" {
		c.item.CallID = item.CallID
	}
	if item.Name != "" {
		c.item.Name = item.Name
	}
	if item.Arguments != nil {
		if done {
			if c.done != nil && *c.done != *item.Arguments {
				return errors.New("OpenAI Responses completed tool arguments disagree")
			}
			c.done = item.Arguments
		} else {
			c.item.Arguments = item.Arguments
		}
	}
	return nil
}

// Hold tool arguments until completion. Some streams carry complete arguments
// in added/done/final items and omit deltas. Never execute the empty placeholder
// or concatenate complete snapshots onto already streamed JSON.
func (calls responsesStreamCalls) finish(response *openAIResponse) ([]ToolCall, error) {
	indices := []int{}
	seen := map[int]bool{}
	for index, raw := range response.Output {
		var item responsesFunctionItem
		if err := json.Unmarshal(raw, &item); err != nil || item.Type != "function_call" {
			continue
		}
		// Final output order may omit earlier non-call items. Match call IDs
		// before using the output position to avoid inventing duplicate calls.
		for previous, c := range calls {
			if item.CallID != "" && c.item.CallID == item.CallID {
				index = previous
				break
			}
		}
		if seen[index] {
			return nil, errors.New("OpenAI Responses duplicated completed tool call")
		}
		if err := calls.observe(index, item, true); err != nil {
			return nil, err
		}
		seen[index] = true
		indices = append(indices, index)
	}
	if len(response.Output) == 0 {
		for index := range calls {
			indices = append(indices, index)
		}
		sort.Ints(indices)
	} else if len(indices) != len(calls) {
		return nil, errors.New("OpenAI Responses final output omitted streamed tool call")
	}
	result := make([]ToolCall, 0, len(indices))
	for _, index := range indices {
		c := calls[index]
		arguments := c.delta.String()
		if c.done != nil {
			arguments = *c.done
			if c.item.Arguments != nil && !strings.HasPrefix(arguments, *c.item.Arguments) {
				return nil, errors.New("OpenAI Responses added and completed tool arguments disagree")
			}
			prefix := c.delta.String()
			if c.item.Arguments != nil && !strings.HasPrefix(arguments, prefix) {
				prefix = *c.item.Arguments + prefix
			}
			if !strings.HasPrefix(arguments, prefix) {
				return nil, errors.New("OpenAI Responses streamed and completed tool arguments disagree")
			}
		} else if c.item.Arguments != nil {
			if arguments == "" {
				arguments = *c.item.Arguments
			} else if !json.Valid([]byte(arguments)) {
				arguments = *c.item.Arguments + arguments
			}
		}
		var object map[string]json.RawMessage
		if c.item.CallID == "" || c.item.Name == "" || json.Unmarshal([]byte(arguments), &object) != nil || object == nil {
			return nil, errors.New("OpenAI Responses tool call lacks identity or complete JSON object arguments")
		}
		call := ToolCall{ID: c.item.CallID, Type: "function"}
		call.Function.Name, call.Function.Arguments = c.item.Name, arguments
		result = append(result, call)
	}
	// Retained Responses history must describe the same calls that executed.
	// Fill absent fields from the validated stream, preserving all other items
	// (including encrypted reasoning) and never replacing conflicting fields.
	position := 0
	for index, raw := range response.Output {
		var item responsesFunctionItem
		if json.Unmarshal(raw, &item) != nil || item.Type != "function_call" {
			continue
		}
		call := result[position]
		position++
		if item.Arguments != nil && item.CallID != "" && item.Name != "" {
			continue
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(raw, &fields); err != nil {
			return nil, err
		}
		fields["arguments"], _ = json.Marshal(call.Function.Arguments)
		fields["call_id"], _ = json.Marshal(call.ID)
		fields["name"], _ = json.Marshal(call.Function.Name)
		encoded, err := json.Marshal(fields)
		if err != nil {
			return nil, err
		}
		response.Output[index] = encoded
	}
	return result, nil
}
