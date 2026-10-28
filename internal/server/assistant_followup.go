package server

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"
)

var followupRefPattern = regexp.MustCompile(`#[0-9]+`)

// Only remove the assistant's follow flag. Never delete, cancel, resume or
// approve the underlying task, message, reminder or session.
func (a *App) clearAssistantFollowups(ctx context.Context, args map[string]any, heard string) string {
	if !strings.Contains(heard, "跟进") && !strings.Contains(heard, "台账") {
		return "未执行：需要用户明确要求清理跟进台账。"
	}
	for _, negative := range []string{"不要", "别清", "不用", "不需要", "怎么", "如何", "是否"} {
		if strings.Contains(heard, negative) {
			return "未执行：本轮没有明确的清理授权。"
		}
	}
	explicit := false
	for _, word := range []string{"清理", "清空", "取消跟进", "不再跟进", "移出"} {
		explicit = explicit || strings.Contains(heard, word)
	}
	if !explicit {
		return "未执行：本轮没有明确的清理授权。"
	}
	all, _ := args["all"].(bool)
	refs := followupRefPattern.FindAllString(heard, -1)
	if len(refs) > 0 {
		all = false
	} // Explicit numbers always narrow a model-generated all flag.
	if all {
		if !strings.Contains(heard, "都") && !strings.Contains(heard, "全部") && !strings.Contains(heard, "所有") && !strings.Contains(heard, "全清") && !strings.Contains(heard, "清空") {
			return "未执行：请明确全部清理还是指定会话编号。"
		}
	} else if len(refs) == 0 {
		return "未执行：请明确全部清理还是指定会话编号。"
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if ctx.Err() != nil {
		return "未执行：请求已取消。"
	}
	if err := a.validateScheduledWorkspaceLocked(ctx); err != nil {
		return "未执行：工作区已变化。"
	}
	selected := map[string]bool{}
	if !all {
		for _, ref := range refs {
			s := a.resolveSessionRef(ref)
			if s == nil {
				return "未执行：指定会话不存在。"
			}
			selected[s.ID] = true
		}
	}
	removed := []int{}
	for _, s := range a.sessions {
		if s.Deleted || s.Kind == assistantSessionKind || !s.FollowedByAssistant || (!all && !selected[s.ID]) {
			continue
		}
		previous := s.FollowedByAssistant
		s.FollowedByAssistant = false
		if err := a.save(s); err != nil {
			s.FollowedByAssistant = previous
			raw, _ := json.Marshal(map[string]any{"ok": false, "removed": removed, "error": "保存失败，部分已清理；失败项保持原状"})
			return string(raw)
		}
		removed = append(removed, s.Number)
	}
	raw, _ := json.Marshal(map[string]any{"ok": true, "removed": removed, "count": len(removed), "remaining": len(a.assistantWorkLedgerLocked()), "effect": "仅取消助理跟进，原任务状态、会话和消息均保留；可重新跟进恢复"})
	return string(raw)
}
