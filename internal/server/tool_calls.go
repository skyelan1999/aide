package server

import (
	"errors"
	"net/http"
	"sort"
	"strings"
)

// 调用记录聚合（#45）：子会话执行过程中的工具调用上报并关联到父会话，
// 在父会话「调用记录」中与主 Agent 调用统一聚合展示、筛选、统计与导出。
//
// 归属口径：
//   - spawn_subagent 本身是主 Agent 在其 run 里的一次工具调用，who="main"；
//   - 子会话内部的所有工具调用 who="sub"，携带子会话 ID/编号/标题；
//   - 子会话完成后自动归档（Archived=true）但仍保留在 a.sessions 中，
//     故归档后父会话轨迹仍可聚合到这些子调用。
//
// 实现采用「关联查询聚合」而非双写：主会话 runs + 按 ParentID 关联子会话 runs，
// 不复制数据、无迁移、归档安全。ToolUse.Who/Child* 在记录时落盘，供导出直接携带归属。

// toolCallRecord 调用记录聚合项（主/子统一视图）。
type toolCallRecord struct {
	Who            string `json:"who"`                      // "main" | "sub"
	ChildSessionID string `json:"childSessionId,omitempty"` // who=sub 时的子会话 ID
	ChildNumber    int    `json:"childNumber,omitempty"`    // 子会话编号 #N
	ChildTitle     string `json:"childTitle,omitempty"`     // 子会话标题
	Time           string `json:"time,omitempty"`           // 所属 run 的创建时间（相对时间线）
	Tool           string `json:"tool"`
	Args           string `json:"args,omitempty"`
	Result         string `json:"result,omitempty"` // 展示用预览
	OK             bool   `json:"ok"`               // 是否未命中失败启发式
}

// toolCallStats 调用统计（随筛选结果计算）：主 N 子 M 失败 X。
type toolCallStats struct {
	MainCount  int `json:"mainCount"`
	ChildCount int `json:"childCount"`
	FailCount  int `json:"failCount"`
}

type toolCallsResponse struct {
	Calls []toolCallRecord `json:"calls"`
	Stats toolCallStats    `json:"stats"`
}

// isToolCallFailed 与前端「调用记录」失败判定保持一致：结果含 失败/error/拒绝/fail（忽略大小写）。
func isToolCallFailed(result string) bool {
	l := strings.ToLower(result)
	return strings.Contains(l, "失败") ||
		strings.Contains(l, "error") ||
		strings.Contains(l, "拒绝") ||
		strings.Contains(l, "fail")
}

// collectToolCallsLocked 聚合某会话的调用记录：本会话 runs（主 Agent，含 spawn_subagent）
// + 所有以本会话为父的子会话 runs（子 Agent）。调用方必须持有 a.mu。
func (a *App) collectToolCallsLocked(sess *Session) []toolCallRecord {
	out := []toolCallRecord{}
	// 主 Agent：本会话全部 runs 的工具调用（spawn_subagent 自然落在这里）
	for _, run := range sess.Runs {
		for _, tu := range run.ToolUses {
			display := tu.Preview
			if display == "" {
				display = tu.Result
			}
			out = append(out, toolCallRecord{
				Who: "main", Time: run.Created, Tool: tu.Tool, Args: tu.Args, Result: display,
				OK: !isToolCallFailed(display),
			})
		}
	}
	// 子 Agent：所有 ParentID == sess.ID 的子会话（含已自动归档、未删除者）
	for _, child := range a.sessions {
		if child.ParentID != sess.ID || child.Deleted {
			continue
		}
		for _, run := range child.Runs {
			for _, tu := range run.ToolUses {
				display := tu.Preview
				if display == "" {
					display = tu.Result
				}
				out = append(out, toolCallRecord{
					Who: "sub", ChildSessionID: child.ID, ChildNumber: child.Number, ChildTitle: child.Title,
					Time: run.Created, Tool: tu.Tool, Args: tu.Args, Result: display,
					OK: !isToolCallFailed(display),
				})
			}
		}
	}
	// 时间线按时间升序（稳定排序：同一 run 内保持追加顺序），便于观察子 agent 在父流程中的时点
	sort.SliceStable(out, func(i, j int) bool { return out[i].Time < out[j].Time })
	return out
}

// sessionToolCalls 调用记录查询端点：GET /api/sessions/{id}/tool-calls
// 过滤参数：
//   - who=all|main|sub（默认 all）
//   - childSessionId=xxx（who=sub 时按具体子会话过滤；多子会话区分）
//
// 响应：{ calls: [...], stats: { mainCount, childCount, failCount } }，
// stats 随当前筛选结果计算（主 N 子 M 失败 X）。
func (a *App) sessionToolCalls(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	a.mu.Lock()
	sess := a.sessions[id]
	if sess == nil || sess.Deleted {
		a.mu.Unlock()
		fail(w, 404, errors.New("会话不存在"))
		return
	}
	calls := a.collectToolCallsLocked(sess)
	a.mu.Unlock()

	who := r.URL.Query().Get("who")
	childID := r.URL.Query().Get("childSessionId")
	filtered := []toolCallRecord{}
	for _, c := range calls {
		if who == "main" && c.Who != "main" {
			continue
		}
		if who == "sub" && c.Who != "sub" {
			continue
		}
		if childID != "" && c.Who == "sub" && c.ChildSessionID != childID {
			continue
		}
		filtered = append(filtered, c)
	}
	stats := toolCallStats{}
	for _, c := range filtered {
		switch c.Who {
		case "main":
			stats.MainCount++
		case "sub":
			stats.ChildCount++
		}
		if !c.OK {
			stats.FailCount++
		}
	}
	jsonOut(w, 200, toolCallsResponse{Calls: filtered, Stats: stats})
}
