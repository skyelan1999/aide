package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// Outcome cards are projections of durable task records. A model plan, a file
// proposal, a returned tool result and a verification report remain distinct.
type outcomeFile struct {
	Path          string `json:"path"`
	State         string `json:"state"`
	BeforeHash    string `json:"beforeHash"`
	ProposedHash  string `json:"proposedHash"`
	ProposedBytes int    `json:"proposedBytes"`
}
type outcomeExecution struct {
	ID        string `json:"id"`
	Index     int    `json:"index"`
	Tool      string `json:"tool"`
	Who       string `json:"who,omitempty"`
	State     string `json:"state"`
	Digest    string `json:"digest"`
	Preview   string `json:"preview"`
	Truncated bool   `json:"truncated"`
}
type outcomeReport struct {
	Title     string `json:"title"`
	Content   string `json:"content"`
	Evidence  string `json:"evidence"`
	State     string `json:"state"`
	Truncated bool   `json:"truncated"`
}
type outcomeSystemReceipt struct {
	ID        string `json:"id"`
	State     string `json:"state"`
	Content   string `json:"content"`
	Digest    string `json:"digest"`
	Truncated bool   `json:"truncated"`
}

func outcomeReceiptDigest(step Step) string {
	b, _ := json.Marshal([]string{step.Name, step.Status, step.Content})
	return hash(b)
}

func outcomeDigest(use ToolUse) string {
	b, _ := json.Marshal([]string{use.Tool, use.Args, use.Result})
	return hash(b)
}
func outcomeText(s string, n int) (string, bool) {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n]), true
	}
	return s, false
}
func outcomeOffset(r *http.Request, key string) (int, error) {
	raw := r.URL.Query().Get(key)
	if raw == "" {
		return 0, nil
	}
	n, e := strconv.Atoi(raw)
	if e != nil || n < 0 {
		return 0, errors.New("invalid " + key)
	}
	return n, nil
}
func (a *App) outcomeTaskSnapshot(sessionID, runID string) (*Task, int, string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	s := a.sessions[sessionID]
	if s == nil || s.Deleted {
		return nil, 0, "", errors.New("任务不存在")
	}
	for _, run := range s.Runs {
		if run == nil || run.ID != runID {
			continue
		}
		// Copy only the inputs this projection uses. Never expose request snapshots,
		// checkpoint prompts, live approval channels, or model credentials.
		t := &Task{ID: run.ID, Prompt: run.Prompt, Status: run.Status, Created: run.Created, WorkspaceID: run.WorkspaceID, Error: run.Error, Applied: run.Applied, ExecutionSequence: run.ExecutionSequence}
		t.Files = append([]Change(nil), run.Files...)
		t.Commands = append([]string(nil), run.Commands...)
		t.ToolUses = append([]ToolUse(nil), run.ToolUses...)
		t.Steps = append([]Step(nil), run.Steps...)
		t.ApprovalReviews = append([]ApprovalReview(nil), run.ApprovalReviews...)
		t.ResearchFindings = append([]ResearchFinding(nil), run.ResearchFindings...)
		t.ToolExecutionIntents = append([]ToolExecutionIntent(nil), run.ToolExecutionIntents...)
		if run.AgentPlan != nil {
			b, _ := json.Marshal(run.AgentPlan)
			_ = json.Unmarshal(b, &t.AgentPlan)
		}
		return t, s.Number, s.Title, nil
	}
	return nil, 0, "", errors.New("任务不存在")
}
func taskOutcomeProjection(task *Task, sessionID string, number int, title string, fileOffset, executionOffset, limit int) map[string]any {
	files := []outcomeFile{}
	uses := []outcomeExecution{}
	reports := []outcomeReport{}
	receipts := []outcomeSystemReceipt{}
	gaps := []string{}
	applied := 0
	unknown := []ToolExecutionIntent{}
	for _, intent := range task.ToolExecutionIntents {
		if intent.State != "completed" {
			unknown = append(unknown, intent)
		}
	}
	for _, f := range task.Files {
		if f.Applied || task.Applied {
			applied++
		}
	}
	fEnd := min(len(task.Files), fileOffset+limit)
	for i := min(fileOffset, len(task.Files)); i < fEnd; i++ {
		f := task.Files[i]
		state := "proposed"
		if f.Applied || task.Applied {
			state = "application_recorded"
		}
		files = append(files, outcomeFile{f.Path, state, f.BaseHash, hash([]byte(f.Content)), len(f.Content)})
	}
	eEnd := min(len(task.ToolUses), executionOffset+limit)
	for i := min(executionOffset, len(task.ToolUses)); i < eEnd; i++ {
		u := task.ToolUses[i]
		preview, truncated := outcomeText(u.Result, 1200)
		state := "result_recorded"
		if u.Result == "" {
			state = "no_result"
		}
		uses = append(uses, outcomeExecution{fmt.Sprintf("E%04d", i+1), i + 1, u.Tool, u.Who, state, outcomeDigest(u), preview, truncated})
	}
	for i, u := range task.ToolUses {
		if u.Tool != "record_verification" {
			continue
		}
		var args struct{ Title, Content string }
		_ = json.Unmarshal([]byte(u.Args), &args)
		title, _ := outcomeText(args.Title, 300)
		content, truncated := outcomeText(args.Content, 2400)
		state := "report_submitted"
		if u.Result == "" {
			state = "no_result"
		}
		reports = append(reports, outcomeReport{title, content, fmt.Sprintf("E%04d", i+1), state, truncated})
	}
	for i, step := range task.Steps {
		if step.Name != "file_application_receipt" {
			continue
		}
		content, truncated := outcomeText(step.Content, 2400)
		receipts = append(receipts, outcomeSystemReceipt{fmt.Sprintf("S%04d", i+1), step.Status, content, outcomeReceiptDigest(step), truncated})
	}
	if len(reports) == 0 {
		gaps = append(gaps, "没有验证报告记录；任务完成不代表验收通过")
	}
	if applied > 0 {
		gaps = append(gaps, "已应用标记是历史记录，未重新读取当前文件确认其仍与提案一致")
	}
	if len(task.Commands) > 0 {
		gaps = append(gaps, "建议命令不属于执行证据；实际返回记录见执行列表")
	}
	if len(unknown) > 0 {
		gaps = append(gaps, "存在未取得持久化结果的调用；不得自动重放有副作用操作")
	}
	if task.Error != "" {
		gaps = append(gaps, task.Error)
	}
	goal, goalTruncated := outcomeText(task.Prompt, 20000)
	return map[string]any{
		"version": 1, "taskId": task.ID, "sessionId": sessionID, "sessionNumber": number, "sessionTitle": title, "workspaceId": task.WorkspaceID, "created": task.Created, "status": task.Status, "goal": goal, "goalTruncated": goalTruncated, "goalDigest": hash([]byte(task.Prompt)), "plan": task.AgentPlan,
		"summary":        map[string]any{"files": len(task.Files), "applicationRecords": applied, "executions": len(task.ToolUses), "verificationReports": len(reports), "systemReceipts": len(receipts), "findings": len(task.ResearchFindings), "unknownOutcomes": len(unknown)},
		"systemReceipts": receipts, "systemReceiptNote": "系统应用回执记录应用后的检查及其限制，不等于当前文件复核或完整产品验收；completed也可能记录待人工审批或远程未读回，请查看原文",
		"files": files, "executions": uses, "verification": reports, "findings": task.ResearchFindings, "suggestedCommands": task.Commands, "unknownCalls": unknown, "gaps": gaps,
		"approvals": task.ApprovalReviews, "approvalNote": "审批记录仅证明当时的决策及匹配依据，不证明执行成功；最多保留最近50条，旧记录可能缺少来源字段",
		"release": map[string]string{"state": "not_recorded", "message": "尚无结构化发布验收记录；聊天结论与命令文本不证明远端发布成功"},
		"page":    map[string]any{"fileNext": fEnd, "filesMore": fEnd < len(task.Files), "executionNext": eEnd, "executionsMore": eEnd < len(task.ToolUses), "limit": limit},
		"journal": map[string]any{"sequence": task.ExecutionSequence, "url": "/api/sessions/" + url.PathEscape(sessionID) + "/runs/" + url.PathEscape(task.ID) + "/journal", "note": "执行日志为独立记录；工具结果可由证据编号读取原文与摘要"},
	}
}
func (a *App) taskOutcomeAPI(w http.ResponseWriter, r *http.Request) {
	task, number, title, err := a.outcomeTaskSnapshot(r.PathValue("id"), r.PathValue("run"))
	if err != nil {
		fail(w, 404, err)
		return
	}
	f, e := outcomeOffset(r, "fileOffset")
	if e != nil {
		fail(w, 400, e)
		return
	}
	x, e := outcomeOffset(r, "executionOffset")
	if e != nil {
		fail(w, 400, e)
		return
	}
	limit := 100
	if raw := r.URL.Query().Get("limit"); raw != "" {
		limit, e = strconv.Atoi(raw)
		if e != nil || limit < 1 || limit > 500 {
			fail(w, 400, errors.New("limit 必须为1–500"))
			return
		}
	}
	// Clamp before addition so attacker-controlled offsets cannot overflow.
	f = min(f, len(task.Files))
	x = min(x, len(task.ToolUses))
	jsonOut(w, 200, taskOutcomeProjection(task, r.PathValue("id"), number, title, f, x, limit))
}
func (a *App) taskOutcomeEvidenceAPI(w http.ResponseWriter, r *http.Request) {
	task, _, _, err := a.outcomeTaskSnapshot(r.PathValue("id"), r.PathValue("run"))
	if err != nil {
		fail(w, 404, err)
		return
	}
	if strings.HasPrefix(r.PathValue("evidence"), "S") {
		index, err := strconv.Atoi(strings.TrimPrefix(r.PathValue("evidence"), "S"))
		if err != nil || index < 1 || index > len(task.Steps) || task.Steps[index-1].Name != "file_application_receipt" {
			fail(w, 404, errors.New("系统回执编号不存在"))
			return
		}
		step := task.Steps[index-1]
		digest := outcomeReceiptDigest(step)
		if expected := r.URL.Query().Get("digest"); expected != "" && expected != digest {
			fail(w, 409, errors.New("证据记录已变化，请重新打开成果舱"))
			return
		}
		jsonOut(w, 200, map[string]any{"id": fmt.Sprintf("S%04d", index), "index": index, "tool": step.Name, "source": "system", "state": step.Status, "result": step.Content, "digest": digest, "note": "历史系统应用回执；查看原文中的检查范围，不推断当前文件或产品验收通过"})
		return
	}
	index, err := strconv.Atoi(strings.TrimPrefix(r.PathValue("evidence"), "E"))
	if err != nil || index < 1 || index > len(task.ToolUses) {
		fail(w, 404, errors.New("证据编号不存在"))
		return
	}
	use := task.ToolUses[index-1]
	digest := outcomeDigest(use)
	if expected := r.URL.Query().Get("digest"); expected != "" && expected != digest {
		fail(w, 409, errors.New("证据记录已变化，请重新打开成果舱"))
		return
	}
	jsonOut(w, 200, map[string]any{"id": fmt.Sprintf("E%04d", index), "index": index, "tool": use.Tool, "who": use.Who, "args": use.Args, "result": use.Result, "digest": digest, "note": "原始工具返回，不等于独立验证成功"})
}
