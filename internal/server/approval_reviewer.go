package server

import (
	"context"
	"encoding/json"
	"strings"
)

type approvalVerdict struct {
	Decision        string `json:"decision"`
	Risk            string `json:"risk"`
	WithinWorkspace bool   `json:"withinWorkspace"`
	Authorized      bool   `json:"authorized"`
	Reason          string `json:"reason"`
}

// Retry empty/format recovery once, never retry an explicit manual verdict
// into approval. Both calls share the original review deadline and no tools.
func reviewApproval(ctx context.Context, cfg Settings, messages []Message) (bool, string, TokenUsage) {
	var usage TokenUsage
	reason := "审核响应无效，请手动确认"
	for attempt, budget := range []int{512, 2048} {
		if ctx.Err() != nil {
			return false, "审核已取消或超时，请手动确认", usage
		}
		out, calls, u, err := complete(ctx, cfg, messages, ProfileParams{MaxTokens: budget, Temperature: fp(0)}, nil, nil)
		usage = addUsage(usage, u)
		if ctx.Err() != nil {
			return false, "审核已取消或超时，请手动确认", usage
		}
		if err != nil {
			reason = "审核模型请求失败，请检查模型连接后重试或手动确认"
			if isEmptyCompletionErr(err) {
				reason = "审核模型未返回决定，可能耗尽输出预算，请手动确认"
			} else {
				return false, reason, usage
			}
		} else {
			if len(calls) > 0 {
				return false, "审核模型返回了工具调用而非审批决定，请手动确认", usage
			}
			var v approvalVerdict
			parsed := json.Unmarshal(approvalDecisionJSON(out), &v) == nil
			if parsed && v.Decision == "manual" {
				if strings.TrimSpace(v.Reason) == "" {
					v.Reason = "审核器要求手动确认"
				}
				return false, v.Reason, usage
			}
			if parsed && strings.TrimSpace(v.Reason) != "" && v.Decision == "approve" && (v.Risk == "low" || v.Risk == "medium" || v.Risk == "high") {
				return v.Decision == "approve" && (v.Risk == "low" || v.Risk == "medium") && v.WithinWorkspace && v.Authorized, v.Reason, usage
			}
			reason = "审核模型返回的审批格式无效，请手动确认"
		}
		if attempt == 0 {
			messages = append(append([]Message(nil), messages...), Message{Role: "user", Content: "上一轮未返回有效审批JSON。本轮只返回策略要求的完整JSON对象；缺证据返回manual，不得因重试降低风险标准。"})
		}
	}
	return false, reason, usage
}
