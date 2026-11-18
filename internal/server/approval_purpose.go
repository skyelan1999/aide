package server

import (
	"context"
	"encoding/json"
	"strings"
	"unicode"
)

const approvalPurposeField = "approvalPurpose"
const approvalPurposeTextLimit = 240

// approvalPurpose is the model's explanation, separate from the exact action
// and the reviewer's decision. It cannot grant permission.
type approvalPurpose struct {
	Goal   string `json:"goal"`
	Reason string `json:"reason"`
}

type approvalPurposeKey struct{}

type approvalFilePurpose struct {
	Path    string           `json:"path"`
	Purpose *approvalPurpose `json:"purpose"`
}

func approvalPurposeSchema() map[string]any {
	return map[string]any{
		"type":                 "object",
		"description":          "审批说明：具体结果及任务关系。勿含敏感内容。",
		"additionalProperties": false,
		"required":             []string{"goal", "reason"},
		"properties": map[string]any{
			"goal":   map[string]any{"type": "string", "maxLength": approvalPurposeTextLimit},
			"reason": map[string]any{"type": "string", "maxLength": approvalPurposeTextLimit},
		},
	}
}

func browserApprovalTool(tool string) bool {
	return tool == "browser_read" || browserToolRequiresConfirmation(tool)
}

func toolAcceptsApprovalPurpose(tool string) bool {
	switch tool {
	case "run_shell", "write_file", "computer_inspect", "computer_snapshot":
		return true
	default:
		return computerControlNeedsConfirmation(tool)
	}
}

func parseApprovalPurpose(value any) *approvalPurpose {
	data, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	goal, _ := data["goal"].(string)
	reason, _ := data["reason"].(string)
	return cleanApprovalPurpose(&approvalPurpose{Goal: goal, Reason: reason})
}

func cleanApprovalPurpose(p *approvalPurpose) *approvalPurpose {
	if p == nil {
		return nil
	}
	goal, reason := approvalPurposeText(p.Goal), approvalPurposeText(p.Reason)
	if goal == "" || reason == "" {
		return nil
	}
	return &approvalPurpose{Goal: goal, Reason: reason}
}

func approvalPurposeText(value string) string {
	var out strings.Builder
	space := false
	n := 0
	for _, r := range value {
		if unicode.IsSpace(r) {
			space = out.Len() > 0
			continue
		}
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			continue
		}
		needed := 1
		if space {
			needed++
		}
		if n+needed > approvalPurposeTextLimit {
			break
		}
		if space {
			out.WriteByte(' ')
			space = false
		}
		out.WriteRune(r)
		n += needed
	}
	return out.String()
}

func approvalPurposeFromContext(ctx context.Context) *approvalPurpose {
	p, _ := ctx.Value(approvalPurposeKey{}).(*approvalPurpose)
	return p
}

func approvalQuestionWithPurpose(ctx context.Context, question json.RawMessage) json.RawMessage {
	purpose := approvalPurposeFromContext(ctx)
	if purpose == nil {
		return question
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(question, &fields) != nil {
		return question
	}
	var kind string
	_ = json.Unmarshal(fields["approvalKind"], &kind)
	if kind == "" {
		return question
	}
	fields[approvalPurposeField], _ = json.Marshal(purpose)
	updated, err := json.Marshal(fields)
	if err != nil {
		return question
	}
	return updated
}

func proposedFilePurposes(files []Change) []approvalFilePurpose {
	var result []approvalFilePurpose
	for _, f := range files {
		if !f.Applied && f.ApprovalPurpose != nil {
			result = append(result, approvalFilePurpose{Path: proposalDisplayPath(f), Purpose: f.ApprovalPurpose})
		}
	}
	return result
}

func approvalSurfaceReceiptAction(question json.RawMessage) string {
	// Preserve bounded action fields without including copy, question
	// identifiers or hidden input.
	var action struct {
		Kind    string          `json:"approvalKind"`
		Tool    string          `json:"tool,omitempty"`
		Args    json.RawMessage `json:"actionArgs,omitempty"`
		URL     string          `json:"url,omitempty"`
		App     string          `json:"app,omitempty"`
		AppRule string          `json:"appRule,omitempty"`
	}
	if json.Unmarshal(question, &action) != nil {
		return ""
	}
	raw, _ := json.Marshal(action)
	return string(raw)
}
