package server

import (
	"context"
	"encoding/json"
	"math"
	"sort"
	"strings"
)

// Exact typed content and observed controls are review inputs, not durable
// confirmation text. A context value transfers them into the matching approval
// round only after awaitUserAnswer has acquired its approval semaphore.
type surfaceApprovalInputKey struct{}
type surfaceApprovalInput struct {
	Tool                 string                 `json:"tool"`
	Args                 map[string]any         `json:"args"`
	Target               computerControlTarget  `json:"target"`
	Control              map[string]any         `json:"control,omitempty"`
	Visual               *surfaceVisualEvidence `json:"visual,omitempty"`
	Image                *MessageImage          `json:"-"`
	DetailImages         []MessageImage         `json:"-"`
	EvidenceError        string                 `json:"evidenceError,omitempty"`
	CoordinateResolution map[string]any         `json:"coordinateResolution,omitempty"`
}

func surfaceInputFingerprint(input *surfaceApprovalInput) string {
	encoded, _ := json.Marshal(input)
	return approvalFingerprint(string(encoded))
}

func uiNumber(value any) (float64, bool) {
	var number float64
	switch n := value.(type) {
	case float64:
		number = n
	case int:
		number = float64(n)
	default:
		return 0, false
	}
	return number, !math.IsNaN(number) && !math.IsInf(number, 0)
}

func editableUIRole(role string) bool {
	return role == "AXTextField" || role == "AXTextArea" || role == "AXComboBox"
}

func reviewUIControl(tool string, args map[string]any, observation any) map[string]any {
	root, ok := observation.(map[string]any)
	if !ok || root["ok"] != true {
		return nil
	}
	controls, _ := root["controls"].([]any)
	var matches []map[string]any
	for _, raw := range controls {
		control, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		role, _ := control["role"].(string)
		if tool != "computer_click" {
			if control["focused"] == true {
				matches = append(matches, control)
			}
			continue
		}
		// Only hit-tested native controls qualify. Coordinates or an actor's
		// description alone do not tell a reviewer which operation will run.
		if !editableUIRole(role) && role != "AXButton" && role != "AXCheckBox" && role != "AXRadioButton" && role != "AXPopUpButton" && role != "AXLink" && role != "AXTab" && role != "AXMenuItem" {
			continue
		}
		r, _ := control["rect"].(map[string]any)
		x, xOK := uiNumber(args["x"])
		y, yOK := uiNumber(args["y"])
		rx, a := uiNumber(r["x"])
		ry, b := uiNumber(r["y"])
		rw, c := uiNumber(r["width"])
		rh, d := uiNumber(r["height"])
		if xOK && yOK && a && b && c && d && rw > 0 && rh > 0 && x >= rx && x < rx+rw && y >= ry && y < ry+rh {
			matches = append(matches, control)
		}
	}
	sort.SliceStable(matches, func(i, j int) bool {
		area := func(c map[string]any) float64 {
			r, _ := c["rect"].(map[string]any)
			w, _ := uiNumber(r["width"])
			h, _ := uiNumber(r["height"])
			return w * h
		}
		return area(matches[i]) < area(matches[j])
	})
	if len(matches) == 0 {
		return nil
	}
	return matches[0]
}

func reviewUIInput(input *surfaceApprovalInput) bool {
	if input == nil || input.Target.PID <= 0 || input.Target.Rule == "" {
		return false
	}
	if input.Visual != nil {
		return (computerPointerTool(input.Tool) || input.Tool == "computer_key") && input.Image != nil && input.Visual.RegionDigest != ""
	}
	if input.Control == nil {
		return false
	}
	role, _ := input.Control["role"].(string)
	if role == "AXSecureTextField" {
		return false
	}
	if input.Tool == "computer_type" {
		text, ok := input.Args["text"].(string)
		if !editableUIRole(role) || !ok || len([]rune(text)) > 2000 {
			return false
		}
	}
	if input.Tool == "computer_key" {
		key, _ := input.Args["key"].(string)
		if (key == "CMD+A" || key == "DELETE" || key == "BACKSPACE" || strings.HasSuffix(key, "+BACKSPACE") || strings.HasSuffix(key, "+DELETE")) && !editableUIRole(role) {
			return false
		}
	}
	// Password controls are excluded by the native walker too. These exact
	// target labels keep consequential buttons manual even if a reviewer errs.
	label, _ := input.Control["label"].(string)
	label = strings.ToLower(label)
	for _, word := range []string{"删除", "清空数据", "清空回收站", "支付", "付款", "转账", "购买", "发送", "发布", "授权", "密码", "登录", "签署", "删除账户", "同意条款", "delete", "remove", "erase", "pay", "purchase", "buy", "send", "publish", "grant", "password", "sign in", "log in", "login", "transfer", "submit", "accept terms"} {
		if strings.Contains(label, word) {
			return false
		}
	}
	return true
}

func (a *App) computerSurfaceInput(ctx context.Context, task *Task, tool string, args map[string]any, target computerControlTarget, approvedRule string) *surfaceApprovalInput {
	a.mu.Lock()
	enabled := task.AutoReview
	a.mu.Unlock()
	if !enabled || target.PID <= 0 || (!computerPointerTool(tool) && tool != "computer_type" && tool != "computer_key") {
		return nil
	}
	// Only called after normal app authorization. Discovery/allowlists are not
	// replaced by assisted review. Failure leaves this operation manual.
	observation, _ := a.callComputerControlTool(ctx, "computer_inspect", nil, target, approvedRule)
	data, err := json.Marshal(args)
	if err != nil || len(data) > 12000 {
		return nil
	}
	var exact map[string]any
	if json.Unmarshal(data, &exact) != nil {
		return nil
	}
	delete(exact, "approved")
	input := &surfaceApprovalInput{Tool: tool, Args: exact, Target: target, Control: reviewUIControl(tool, exact, observation)}
	if ref, ok := ctx.Value(computerActionReferenceKey{}).(computerSnapshotReference); ok {
		input.CoordinateResolution = map[string]any{"validatedByServer": true, "requestedSpace": "image", "screenshotId": ref.ID, "imagePoints": ref.ImagePoints, "sourceWindow": ref.Window, "pixelWidth": ref.PixelWidth, "pixelHeight": ref.PixelHeight, "executionSpace": "desktop"}
	}
	if tool == "computer_drag" || tool == "computer_scroll" || (input.Control == nil && (tool == "computer_click" || tool == "computer_key")) {
		return a.computerSurfaceVisual(ctx, input, approvedRule)
	}
	if !reviewUIInput(input) {
		return nil
	}
	return input
}

func (a *App) computerSurfaceUnchanged(ctx context.Context, input *surfaceApprovalInput, approvedRule string) bool {
	if input == nil {
		return true
	}
	if input.Visual != nil {
		current := a.computerSurfaceVisual(ctx, input, approvedRule)
		return current != nil && current.Visual.sameTarget(input.Visual)
	}
	observation, err := a.callComputerControlTool(ctx, "computer_inspect", nil, input.Target, approvedRule)
	if err != nil {
		return false
	}
	current := *input
	current.Control = reviewUIControl(input.Tool, input.Args, observation)
	return reviewUIInput(&current) && surfaceInputFingerprint(&current) == surfaceInputFingerprint(input)
}

// Keep reviewer explanations useful without copying the private input or the
// observed field value back into durable approval cards and audit history.
func surfaceReviewReason(reason string, input *surfaceApprovalInput) string {
	if input == nil {
		return reason
	}
	for _, value := range []any{input.Args["text"], input.Control["value"]} {
		if text, ok := value.(string); ok && text != "" {
			reason = strings.ReplaceAll(reason, text, "[input]")
		}
	}
	return reason
}
