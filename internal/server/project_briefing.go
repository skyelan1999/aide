package server

import (
	"errors"
	"fmt"
	"path"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

type projectBriefMessage struct {
	instant   time.Time
	Day       string `json:"day"`
	At        string `json:"at"`
	Source    string `json:"source"`
	SessionID string `json:"sessionId"`
	RunID     string `json:"runId"`
	Role      string `json:"role"`
	Content   string `json:"content"`
	Match     string `json:"match"`
}

// Caller holds App.mu, keeping extraction bound to the checked workspace and scope.
func (a *App) projectMessagesForBridgeLocked(req workbenchBridgeRequest, id string, b *pluginWorkbenchBridge) (map[string]any, int, error) {
	allowed := false
	for _, p := range a.pluginRegistry.Plugins {
		if p.ID == id && p.Enabled && p.Workbench != nil {
			allowed = p.Workbench.ProjectMessages
		}
	}
	if !allowed {
		return nil, 403, errors.New("project messages capability not declared")
	}
	if b.Busy || b.Seen[req.RequestID] || len(b.Seen) >= 128 {
		return nil, 409, errors.New("busy, replayed or exhausted workbench request")
	}
	for key := range req.Arguments {
		if key != "projectId" && key != "projectName" && key != "path" && key != "fromDate" && key != "toDate" {
			return nil, 400, errors.New("unknown project message argument")
		}
	}
	name := strings.TrimSpace(stringValue(req.Arguments["projectName"]))
	pid := strings.TrimSpace(stringValue(req.Arguments["projectId"]))
	file := stringValue(req.Arguments["path"])
	if utf8.RuneCountInString(name) < 2 || len(name) > 600 || len(pid) > 80 || strings.ContainsAny(name+pid, "\r\n\x00") || len(file) > 1000 || (file != "" && (safePath(file) != nil || !strings.EqualFold(path.Ext(file), ".pp"))) {
		return nil, 400, errors.New("valid project identity and optional PP path required")
	}
	zone := time.FixedZone("Asia/Shanghai", 8*3600)
	start, e1 := time.ParseInLocation("2006-01-02", stringValue(req.Arguments["fromDate"]), zone)
	last, e2 := time.ParseInLocation("2006-01-02", stringValue(req.Arguments["toDate"]), zone)
	if e1 != nil || e2 != nil || last.Before(start) || last.Sub(start) > 30*24*time.Hour {
		return nil, 400, errors.New("project message range must contain 1 to 31 days")
	}
	b.Seen[req.RequestID] = true
	rows, omitted := a.projectMessagesLocked(name, pid, file, start, last.AddDate(0, 0, 1))
	return map[string]any{"requestId": req.RequestID, "status": "result", "result": map[string]any{"messages": rows, "omitted": omitted, "timeBasis": "Message creation time; legacy messages use source task creation time and are labeled (Asia/Shanghai)", "fromDate": start.Format("2006-01-02"), "toDate": last.Format("2006-01-02")}}, 200, nil
}

func (a *App) projectMessagesLocked(name, pid, file string, start, end time.Time) ([]projectBriefMessage, int) {
	rows := []projectBriefMessage{}
	// A run must belong to this workspace and explicitly refer to this project.
	// A matching conversation title alone never grants access to unrelated runs.
	for _, s := range a.sessions {
		if s == nil || s.Deleted || s.Kind == assistantSessionKind {
			continue
		}
		runs := map[string]*Task{}
		matches := map[string]string{}
		for _, run := range s.Runs {
			if run == nil || run.WorkspaceID != a.wsID() {
				continue
			}
			match := explicitProjectReference(run.Prompt, name, pid)
			if file != "" {
				for _, attachment := range run.Attachments {
					if attachment.Root == "workspace" && attachment.Source == "" && !attachment.Directory && attachment.Path == file {
						match = "projectFile"
						break
					}
				}
			}
			if match != "" {
				runs[run.ID] = run
				matches[run.ID] = match
			}
		}
		for _, message := range s.Messages {
			run := runs[message.SourceRunID]
			if run == nil || (message.Role != "user" && message.Role != "assistant") || strings.TrimSpace(message.Content) == "" {
				continue
			}
			atText := message.CreatedAt
			source := fmt.Sprintf("会话 #%d · %s · %s", s.Number, s.Title, message.Role)
			if atText == "" {
				atText = run.Created
				source += "（按任务时间）"
			}
			at, err := time.Parse(time.RFC3339Nano, atText)
			if err != nil || at.Before(start) || !at.Before(end) {
				continue
			}
			rows = append(rows, projectBriefMessage{instant: at, Day: at.In(start.Location()).Format("2006-01-02"), At: atText, Source: source, SessionID: s.ID, RunID: run.ID, Role: message.Role, Content: message.Content, Match: matches[run.ID]})
		}
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if !rows[i].instant.Equal(rows[j].instant) {
			return rows[i].instant.After(rows[j].instant)
		}
		if rows[i].SessionID != rows[j].SessionID {
			return rows[i].SessionID < rows[j].SessionID
		}
		return rows[i].RunID < rows[j].RunID
	})
	omitted := 0
	bytes := 0
	out := []projectBriefMessage{}
	for _, row := range rows {
		if len(out) >= 100 || bytes >= 48000 {
			omitted++
			continue
		}
		chars := []rune(row.Content)
		if len(chars) > 1200 {
			row.Content = string(chars[:1200]) + "…（原消息较长，已截取）"
		}
		if bytes+len(row.Content) > 48000 {
			omitted++
			continue
		}
		bytes += len(row.Content)
		out = append(out, row)
	}
	return out, omitted
}

// A plain word or ID substring is not evidence of project membership. Text
// references use explicit, complete markers; exact PP attachments remain an
// independent selection source. Scan without materializing all prompt matches.
func explicitProjectReference(prompt, name, pid string) string {
	match := ""
	for {
		start := strings.IndexByte(prompt, '[')
		if start < 0 {
			return match
		}
		prompt = prompt[start+1:]
		end := strings.IndexByte(prompt, ']')
		if end < 0 {
			return match
		}
		reference := prompt[:end]
		prompt = prompt[end+1:]
		if strings.ContainsAny(reference, "\r\n") {
			continue
		}
		kind, value, ok := strings.Cut(reference, ":")
		if !ok {
			continue
		}
		value = strings.TrimSpace(value)
		if kind == "project-id" && len(pid) >= 8 && value == pid {
			return "projectId"
		}
		if kind == "project-name" && name != "" && strings.EqualFold(value, name) {
			match = "projectName"
		}
	}
}
