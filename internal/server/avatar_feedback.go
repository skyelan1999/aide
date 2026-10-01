package server

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
)

// AvatarCue is cosmetic metadata. It never changes task state, permissions or tools.
type AvatarCue struct {
	Scene     string `json:"scene"`
	Variant   int    `json:"variant"`
	Emotion   string `json:"emotion"`
	Intensity int    `json:"intensity"`
}

var avatarSceneVariants = map[string]int{
	"idle": 3, "listening": 2, "thinking": 3, "searching": 2,
	"reading": 2, "creating": 2, "executing": 2, "generating": 3,
	"speaking": 2, "awaiting_user": 2, "waiting": 2, "completed": 3,
	"error": 3, "paused": 2, "sleeping": 3, "notification": 2,
}

const avatarCueToolName = "aide_avatar_cue"
const avatarCueBudget = 3

func avatarCueFormatAllowed(params ProfileParams) bool {
	return params.ResponseFormat == "" || params.ResponseFormat == "text"
}

func readAvatarFeedbackOverride(w http.ResponseWriter, r *http.Request) (*bool, error) {
	if r.Body == nil || r.ContentLength == 0 {
		return nil, nil
	}
	var in struct {
		AvatarFeedback *bool `json:"avatarFeedback,omitempty"`
	}
	if err := decode(w, r, &in); err != nil {
		return nil, err
	}
	return in.AvatarFeedback, nil
}

// Enabled only for requests from a client displaying the avatar. Piggybacks the
// existing model response, with no classifier request, timer or per-frame calls.
func avatarCueTool() any {
	return map[string]any{"type": "function", "function": map[string]any{
		"name":        avatarCueToolName,
		"description": "Optional cosmetic avatar expression, at most once per response and three times per turn. Select a scene matching your actual activity; this cannot change task status. variant is 1 or 2, also 3 for idle/thinking/generating/completed/error/sleeping. If no work tool is needed, put the complete user-facing answer in reply so no extra model round is needed. Never call solely to animate or repeat an unchanged expression.",
		"parameters": map[string]any{"type": "object", "additionalProperties": false, "properties": map[string]any{
			"scene":     map[string]any{"type": "string", "enum": []string{"idle", "listening", "thinking", "searching", "reading", "creating", "executing", "generating", "speaking", "awaiting_user", "waiting", "completed", "error", "paused", "sleeping", "notification"}},
			"variant":   map[string]any{"type": "integer", "minimum": 1, "maximum": 3, "description": "Ordered actions by scene: idle blink/perch/tilt; listening lean-ear/hold-cheeks; thinking chin/scratch-idea/hug-whale; searching magnifier/file-box; reading book/desk; creating keyboard/draw; executing repair/checklist; generating coin/bowl/chase-token; speaking gesture/pointer; awaiting_user question-sign/beckon; waiting hourglass/clock; completed clap/card/jump-wave; error pout/tears/stomp; paused sit/stop-hands; sleeping belly/curl/yawn; notification stretch/bell."},
			"emotion":   map[string]any{"type": "string", "enum": []string{"calm", "curious", "happy", "frustrated", "sleepy"}},
			"intensity": map[string]any{"type": "integer", "minimum": 1, "maximum": 3},
			"reply":     map[string]any{"type": "string", "description": "User-facing reply when this is the only call; otherwise omit."},
		}, "required": []string{"scene", "variant", "emotion", "intensity"}},
	}}
}

func withAvatarCueTool(tools []any, enabled bool) []any {
	if !enabled {
		return tools
	}
	return append(append([]any(nil), tools...), avatarCueTool())
}

func withoutAvatarCueTool(tools []any) []any {
	result := make([]any, 0, len(tools))
	for _, tool := range tools {
		definition, _ := tool.(map[string]any)
		function, _ := definition["function"].(map[string]any)
		if function["name"] != avatarCueToolName {
			result = append(result, tool)
		}
	}
	return result
}

func parseAvatarCue(args string) (*AvatarCue, string) {
	if len(args) > 64000 {
		return nil, ""
	}
	var value struct {
		AvatarCue
		Reply string `json:"reply"`
	}
	decoder := json.NewDecoder(strings.NewReader(args))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&value) != nil || decoder.Decode(new(any)) != io.EOF {
		return nil, ""
	}
	if value.Variant < 1 || value.Variant > avatarSceneVariants[value.Scene] || value.Intensity < 1 || value.Intensity > 3 {
		return nil, ""
	}
	switch value.Emotion {
	case "calm", "curious", "happy", "frustrated", "sleepy":
	default:
		return nil, ""
	}
	return &value.AvatarCue, strings.TrimSpace(value.Reply)
}

// Consume only the dedicated structured call. Prose and tool output cannot emit
// cues. Remove the side-channel call before recording the ordinary tool exchange,
// so every remaining tool_call still receives exactly one tool result.
func consumeAvatarCues(out string, calls []ToolCall, enabled bool, used *int) (string, []ToolCall, *AvatarCue) {
	if !enabled {
		return out, calls, nil
	}
	kept := make([]ToolCall, 0, len(calls))
	var cue *AvatarCue
	for _, call := range calls {
		if call.Function.Name != avatarCueToolName {
			kept = append(kept, call)
			continue
		}
		candidate, reply := parseAvatarCue(call.Function.Arguments)
		if candidate == nil {
			continue
		}
		if strings.TrimSpace(out) == "" && reply != "" {
			out = reply
		}
		if cue == nil && *used < avatarCueBudget {
			cue = candidate
			(*used)++
		}
	}
	return out, kept, cue
}
