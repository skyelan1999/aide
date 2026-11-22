package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// A bounded read-only recovery uses the same permission and workspace guards
// as model-generated calls. It can never dispatch or approve work.
func (a *App) recoverAssistantApprovalQuery(ctx context.Context, heard string, dec *assistantDecision) (ToolCall, string, error) {
	call := ToolCall{ID: newID(), Type: "function"}
	call.Function.Name = "aide_component_status"
	call.Function.Arguments = `{"component":"approvals"}`
	if ctx.Err() != nil || !assistantApprovalStatusQuery(heard) {
		return call, "", errors.New("审批查询请求已结束或不匹配")
	}
	result := a.execAssistantToolWithContext(ctx, call, dec, heard)
	if dec.approvalTotal == nil {
		return call, "", errors.New("未取得本轮审批查询回执，未执行或转交任务")
	}
	dec.approvalQueryRecovery = true
	return call, result, nil
}

const assistantApprovalQueryRepair = "本轮只读审批查询已取得上方真实工具回执。请按回执 total 回答当前数量，无需重复查询。不要沿用历史失败或转交话术，不得批准、拒绝、派发或恢复任务。"

func assistantApprovalStatusQuery(heard string) bool {
	// A preservation constraint is not an approval query. Keep the actual
	// requested operation in the other clauses (e.g. read a workspace guide).
	clauses := assistantWorkClauses.Split(heard, -1)
	kept := clauses[:0]
	for _, clause := range clauses {
		if (strings.Contains(clause, "审批") || strings.Contains(clause, "待审")) && assistantWorkNegation.MatchString(strings.TrimSpace(clause)) && !strings.Contains(clause, "查") && !strings.Contains(clause, "看") {
			continue
		}
		kept = append(kept, clause)
	}
	heard = strings.Join(kept, "，")
	if !strings.Contains(heard, "审批") && !strings.Contains(heard, "待审") {
		return false
	}
	for _, s := range []string{"不要查", "不用查", "不需要查", "别查", "无需查", "不要看", "不用看", "别看"} {
		if strings.Contains(heard, s) {
			return false
		}
	}
	for _, s := range []string{"查", "看", "多少", "几项", "几条", "数量", "总数", "状态", "进度", "调用", "能用"} {
		if strings.Contains(heard, s) {
			return true
		}
	}
	return false
}
func assistantApprovalReadOnlyTool(name string) bool {
	switch name {
	case "aide_component_status", "get_session", "search_sessions", "work_status":
		return true
	}
	return false
}
func assistantApprovalQueryTools(tools []any, heard string) []any {
	if !assistantApprovalStatusQuery(heard) {
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
func assistantApprovalCountResult(result string) *int {
	var value struct {
		Total *int `json:"total"`
	}
	if json.Unmarshal([]byte(result), &value) == nil && value.Total != nil && *value.Total >= 0 {
		return value.Total
	}
	return nil
}
func finalizeAssistantApprovalQuery(dec assistantDecision, heard string) assistantDecision {
	if dec.Action != "chat" || dec.approvalTotal == nil || !assistantApprovalStatusQuery(heard) {
		return dec
	}
	for _, word := range []string{"数量", "总数", "多少", "几项", "几条", "几件"} {
		if strings.Contains(heard, word) {
			dec.Reply = fmt.Sprintf("当前有 %d 项待人工审批。", *dec.approvalTotal)
			break
		}
	}
	return dec
}
