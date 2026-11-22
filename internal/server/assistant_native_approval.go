package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const nativeApprovalGuidance = "用户指定会话并询问审批时调用 assistant_work。用工具返回的会话编号和待审事项询问用户。只有用户另说“批准本次审批”或“拒绝本次审批”时，才再次调用工具提交这一项决定。"

const spokenSessionDigits = `0-9零〇一二两三四五六七八九幺十百千万`

var nativeApprovalSession = regexp.MustCompile(`(?:[#＃][[:space:]]*|(?:会话|绘画)[[:space:]]*)([` + spokenSessionDigits + `]+)|([` + spokenSessionDigits + `]+)[[:space:]]*(?:号|的)?[[:space:]]*(?:会话|绘画)`)
var nativeApprovalCorrection = regexp.MustCompile(`^(?:不是[` + spokenSessionDigits + `]+[，,、[:space:]]*(?:而?是)|是)?([` + spokenSessionDigits + `]+)[。.!！[:space:]]*$`)

type nativeApprovalOffer struct {
	session, run, workspace, kind string
	question, round, fingerprint  string
	revision                      uint64
	number                        int
	expires                       time.Time
	detail                        string
}

func nativeApprovalTarget(heard string) string {
	return nativeApprovalTargetWithContext(heard, false)
}

// Spoken numbers and ASR's 会话/绘画 confusion are used only for a read-only
// lookup. They never grant authority to submit an approval decision.
func nativeApprovalTargetWithContext(heard string, recentApprovalQuery bool) string {
	if !strings.Contains(heard, "审批") && assistantRunAction.MatchString(strings.TrimSpace(heard)) {
		return ""
	}
	if !strings.Contains(heard, "审批") && !recentApprovalQuery {
		return ""
	}
	for _, phrase := range []string{"不要审批", "别审批", "不需要审批", "不用审批"} {
		if strings.Contains(heard, phrase) {
			return ""
		}
	}
	matches := nativeApprovalSession.FindAllStringSubmatch(heard, 2)
	if len(matches) != 1 {
		if len(matches) == 0 && recentApprovalQuery {
			if corrected := nativeApprovalCorrection.FindStringSubmatch(strings.TrimSpace(heard)); corrected != nil {
				if number, ok := spokenSessionNumber(corrected[1]); ok {
					return "#" + strconv.Itoa(number)
				}
			}
		}
		return ""
	}
	if number, ok := spokenSessionNumber(matches[0][1] + matches[0][2]); ok {
		return "#" + strconv.Itoa(number)
	}
	return ""
}

func spokenSessionNumber(text string) (int, bool) {
	if len([]rune(text)) > 16 {
		return 0, false
	}
	if n, err := strconv.Atoi(text); err == nil {
		return n, n > 0 && n <= 99999999
	}
	digits := map[rune]int{'零': 0, '〇': 0, '一': 1, '幺': 1, '二': 2, '两': 2, '三': 3, '四': 4, '五': 5, '六': 6, '七': 7, '八': 8, '九': 9}
	units := map[rune]int{'十': 10, '百': 100, '千': 1000, '万': 10000}
	total, section, number, previousUnit := 0, 0, 0, 10000
	hasUnit := strings.ContainsAny(text, "十百千万")
	for _, r := range text {
		if d, ok := digits[r]; ok {
			number = number*10 + d
			continue
		}
		unit, ok := units[r]
		if !ok {
			return 0, false
		}
		if unit == 10000 {
			if total != 0 || section+number == 0 {
				return 0, false
			}
			total, section, number, previousUnit = (section+number)*unit, 0, 0, 10000
			continue
		}
		if unit >= previousUnit || number == 0 && unit != 10 {
			return 0, false
		}
		if number == 0 {
			number = 1
		}
		section, number, previousUnit = section+number*unit, 0, unit
	}
	if !hasUnit {
		section = 0
	}
	n := total + section + number
	return n, n > 0 && n <= 99999999
}

func nativeApprovalDecision(heard string) string {
	heard = strings.Trim(strings.TrimSpace(heard), "。！？.!?，, ")
	switch heard {
	case "批准本次审批", "批准这一次审批", "同意本次审批", "确认本次审批":
		return "确认"
	case "拒绝本次审批", "拒绝这一次审批", "不同意本次审批":
		return "拒绝"
	}
	return ""
}

func (a *App) nativeApprovalForSession(workspace, ref string) (*nativeApprovalOffer, string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	s := a.resolveSessionRef(ref)
	if s == nil || s.Archived || s.Kind == assistantSessionKind || a.wsID() != workspace {
		return nil, "没有找到当前工作区的这一个会话，请核对会话编号。"
	}
	pending := 0
	for _, event := range a.humanApprovalEventsLocked() {
		if event.SessionID == s.ID {
			pending++
		}
	}
	if pending > 1 {
		return nil, fmt.Sprintf("会话 #%d 有 %d 项待审批。请在对应会话选择其中一项，避免把决定用错。", s.Number, pending)
	}
	for _, run := range s.Runs {
		if run.WorkspaceID != workspace || run.StopRequested ||
			run.ApprovalState == "reviewing" || run.ApprovalState == "recommended" || run.ApprovalState == "approved" || run.ApprovalState == "applying" {
			continue
		}
		offer := &nativeApprovalOffer{session: s.ID, run: run.ID, workspace: workspace, revision: a.wsRevision, number: s.Number, expires: time.Now().Add(2 * time.Minute)}
		if run.Status == "awaiting_approval" && len(run.Files) > 0 {
			var preview strings.Builder
			for _, file := range run.Files {
				if preview.Len()+len(file.Path)+len(file.Before)+len(file.Content) > 64<<10 {
					return nil, fmt.Sprintf("会话 #%d 的文件提案较大，请在原会话审阅和审批。", s.Number)
				}
				fmt.Fprintf(&preview, "%s\n原内容\n%s\n拟写入\n%s\n\n", file.Path, file.Before, file.Content)
			}
			offer.kind = "files"
			offer.fingerprint = approvalFingerprint(approvalFilesFingerprint(run.Files))
			offer.detail = preview.String()
			return offer, fmt.Sprintf("会话 #%d 有一批 %d 项文件提案待审批。请看通话里的改动，再说“批准本次审批”或“拒绝本次审批”。", s.Number, len(run.Files))
		}
		if run.Status != "awaiting_clarification" || run.AnswerCh == nil || run.approvalAnswered {
			continue
		}
		var question struct {
			ID      string `json:"questionId"`
			Kind    string `json:"approvalKind"`
			Type    string `json:"type"`
			Command string `json:"command"`
		}
		if json.Unmarshal(run.PendingQuestion, &question) != nil || question.ID == "" || question.Kind != "shell" || question.Type != "confirm" || question.Command == "" || len(question.Command) > 64<<10 || approvalCommandProtected(question.Command) {
			continue
		}
		offer.kind = "shell"
		offer.question = question.ID
		offer.round = fmt.Sprint(run.answerRound)
		offer.fingerprint = approvalFingerprint(string(run.PendingQuestion))
		offer.detail = question.Command
		return offer, fmt.Sprintf("会话 #%d 的第 %s 轮命令待确认。请看通话里的完整命令，再说“批准本次审批”或“拒绝本次审批”。", s.Number, offer.round)
	}
	return nil, fmt.Sprintf("会话 #%d 当前没有可通过语音提交决定的命令或文件提案。", s.Number)
}

func nativeApprovalEvent(offer *nativeApprovalOffer) map[string]any {
	event := map[string]any{"type": "aide.approval"}
	if offer != nil {
		event["sessionNumber"], event["kind"], event["detail"] = offer.number, offer.kind, offer.detail
		event["round"], event["expiresAt"] = offer.round, offer.expires.UnixMilli()
	}
	return event
}

func (a *App) submitNativeApproval(ctx context.Context, assistantID string, offer *nativeApprovalOffer, answer string) string {
	if assistantReadOnlyCall(ctx) {
		return assistantReadOnlyCallDenied
	}
	if offer == nil || time.Now().After(offer.expires) {
		return "未提交审批。请先说要查看哪个会话的审批。"
	}
	guard := func(task *Task) error {
		if ctx.Err() != nil || !a.isAssistantUnlocked(assistantID) || a.loadLockStateLocked().Locked ||
			a.wsID() != offer.workspace || a.wsRevision != offer.revision || task.WorkspaceID != offer.workspace ||
			task.StopRequested || task.ApprovalState == "reviewing" || task.ApprovalState == "recommended" ||
			task.ApprovalState == "approved" || task.ApprovalState == "applying" {
			return errors.New("审批上下文已变化")
		}
		fingerprint := approvalFingerprint(string(task.PendingQuestion))
		if offer.kind == "files" && task.Status == "awaiting_approval" {
			fingerprint = approvalFingerprint(approvalFilesFingerprint(task.Files))
		}
		if fingerprint != offer.fingerprint {
			return errors.New("待审内容已变化")
		}
		return nil
	}
	if offer.kind == "files" && answer == "拒绝" {
		a.mu.Lock()
		defer a.mu.Unlock()
		if session := a.sessionLocked(offer.session); session != nil && !session.Deleted {
			for _, task := range session.Runs {
				if task.ID != offer.run || task.Status != "awaiting_approval" || guard(task) != nil {
					continue
				}
				receipt := ApprovalReview{Command: "write_file: explicit file proposal", Status: "declined", Reason: "用户在语音通话中拒绝本次应用", Source: "human", Kind: "files", At: time.Now().UTC().Format(time.RFC3339Nano), Workspace: task.WorkspaceID, Root: task.AgentRoot, Fingerprint: offer.fingerprint}
				if a.persistApprovalReceiptLocked(task, receipt) == nil {
					return fmt.Sprintf("已记录拒绝会话 #%d 的本次文件应用，提案仍可在会话中查看。", offer.number)
				}
			}
		}
		return "未能记录拒绝，请重新查看对应会话的审批卡片。"
	}
	payload, _ := json.Marshal(map[string]string{"answer": answer, "questionId": offer.question, "round": offer.round})
	r := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(payload)).WithContext(ctx)
	r.SetPathValue("id", offer.session)
	r.SetPathValue("run", offer.run)
	w := httptest.NewRecorder()
	if offer.kind == "files" {
		a.applyTaskWithGuard(w, r, guard)
	} else {
		a.answerTaskGuarded(w, r, func(session *Session, task *Task) error {
			if session.Deleted {
				return errors.New("会话已经删除")
			}
			return guard(task)
		})
	}
	if w.Code/100 != 2 {
		return fmt.Sprintf("会话 #%d 的审批提交未成功，请在原会话查看当前状态。", offer.number)
	}
	return fmt.Sprintf("已向会话 #%d 提交本次审批决定：%s。", offer.number, answer)
}
