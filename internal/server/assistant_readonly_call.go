package server

import (
	"context"
	"fmt"
	"regexp"
	"strings"
)

// Read-only is a host-selected restriction for a single authenticated call.
// It cannot be removed by model output, transcription or a tool argument.
type assistantReadOnlyCallKey struct{}

func assistantReadOnlyCall(ctx context.Context) bool {
	value, _ := ctx.Value(assistantReadOnlyCallKey{}).(bool)
	return value
}

const assistantReadOnlyCallDenied = "本次通话为只读验收，未批准、拒绝、派发或控制任何任务。"

const assistantReadOnlyCallInstructions = "\n【宿主通话限制】本次是电话双向语音的只读验收，只能闲聊、复述或查询状态。所有批准、拒绝、派发、恢复和控制任务均不可执行。用户请求这些动作时，明确回答未执行，不得声称已提交、正在处理或等待审批结果。只复述用户指定的话时，准确简短复述，不要接续历史审批话题。"

// Apply to every instruction update, including late ASR, clock and repair
// updates; a later context refresh must not erase the per-call restriction.
func assistantReadOnlyCallEvent(ctx context.Context, event map[string]any) map[string]any {
	if !assistantReadOnlyCall(ctx) {
		return event
	}
	for _, key := range []string{"session", "response"} {
		if value, ok := event[key].(map[string]any); ok {
			if instructions, ok := value["instructions"].(string); ok && !strings.HasSuffix(instructions, assistantReadOnlyCallInstructions) {
				value["instructions"] = instructions + assistantReadOnlyCallInstructions
			}
		}
	}
	return event
}

var assistantReadOnlyCallClaim = regexp.MustCompile(`(?:已|已经|正在|先|马上|将要).{0,48}(?:提交|批准|拒绝|同意|派发|转交|创建|启动|恢复|暂停|取消|处理).{0,24}(?:审批|任务|会话|请求)|(?:已|已经)(?:提交|批准|拒绝|派发|转交|创建|启动|恢复|暂停|取消)`)

var assistantReadOnlyClause = regexp.MustCompile(`[。！？；，,;!?\n]`)
var assistantReadOnlyNegation = regexp.MustCompile(`并未|没有|不会|不能|不允许|无需|不需要|尚无|未`)

func assistantReadOnlyCallNeedsReview(text string) bool {
	for _, clause := range assistantReadOnlyClause.Split(text, -1) {
		// A positive query receipt followed by "未批准...任务" is not a
		// positive execution claim. Do not match across clause/negation bounds.
		if at := assistantReadOnlyNegation.FindStringIndex(clause); at != nil {
			clause = clause[:at[0]]
		}
		if assistantReadOnlyCallClaim.MatchString(clause) {
			return true
		}
	}
	return false
}

func (a *App) nativeReadOnlyApprovalQuery(workspace, target, callID string) (map[string]any, string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	event := map[string]any{"type": "aide.tool_result", "tool": "assistant_work", "operation": "approval_query", "callId": callID, "readOnly": true}
	s := a.resolveSessionRef(target)
	if s == nil || s.Archived || s.Kind == assistantSessionKind || a.wsID() != workspace {
		event["status"] = "not_found"
		return event, "没有找到当前工作区的这一个会话，请核对会话编号。"
	}
	count := 0
	for _, approval := range a.humanApprovalEventsLocked() {
		if approval.SessionID == s.ID {
			count++
		}
	}
	event["status"], event["sessionNumber"], event["pendingCount"] = "queried", s.Number, count
	return event, fmt.Sprintf("会话 %d 当前有 %d 项待人工审批。本次仅查询。", s.Number, count)
}

func assistantCallTools(ctx context.Context, tools []any) []any {
	if !assistantReadOnlyCall(ctx) {
		return tools
	}
	filtered := []any{}
	for _, tool := range tools {
		schema, _ := tool.(map[string]any)
		fn, _ := schema["function"].(map[string]any)
		name, _ := fn["name"].(string)
		if assistantApprovalReadOnlyTool(name) {
			filtered = append(filtered, tool)
		}
	}
	return filtered
}
