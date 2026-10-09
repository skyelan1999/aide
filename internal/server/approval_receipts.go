package server

import (
	"errors"
	"time"
)

func rememberedApprovalReceipt(rule ApprovalRule, command string) ApprovalReview {
	return ApprovalReview{Command: command, Status: "approved", Reason: "命中同一工作区、根目录与完整载荷摘要的有效授权；审批通过不代表执行成功", At: time.Now().UTC().Format(time.RFC3339Nano), Source: "remembered", Kind: rule.Kind, Workspace: rule.Workspace, Root: rule.Root, Fingerprint: rule.Fingerprint, RuleID: approvalRuleID(rule), ExpiresAt: rule.ExpiresAt}
}

// Caller holds a.mu. Receipt must be durable before an operation is released.
func (a *App) persistApprovalReceiptLocked(task *Task, record ApprovalReview) error {
	s := a.approvalSessionLocked(task)
	if s == nil || s.Deleted {
		return errors.New("审批任务会话不可用，未放行")
	}
	previous := task.ApprovalReviews
	next := append([]ApprovalReview(nil), previous...)
	if len(next) >= 50 {
		next = next[len(next)-49:]
	}
	task.ApprovalReviews = append(next, record)
	if err := a.save(s); err != nil {
		task.ApprovalReviews = previous
		return err
	}
	return nil
}
func (a *App) approveRememberedShell(task *Task, command string) (bool, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if approvalCommandProtected(command) {
		return false, nil
	}
	rule, matched := a.matchApprovalRuleLocked(task, "shell", command)
	if !matched {
		return false, nil
	}
	if err := a.persistApprovalReceiptLocked(task, rememberedApprovalReceipt(rule, command)); err != nil {
		return false, err
	}
	return true, nil
}
