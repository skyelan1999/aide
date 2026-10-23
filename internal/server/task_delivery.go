package server

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// Delivery is evidence about an output, separate from the executor's status.
// Plans, proposed writes and model assertions never create verified artifacts.
var errDeliveryIncomplete = errors.New("交付未完成")

type TaskDelivery struct {
	State     string   `json:"state"` // delivered, pending_approval, missing
	Kind      string   `json:"kind"`
	Artifacts []string `json:"artifacts,omitempty"`
	Reason    string   `json:"reason,omitempty"`
	CheckedAt string   `json:"checkedAt"`
}

var artifactLink = regexp.MustCompile(`!?\[[^\]\n]*\]\(<?([^\s<>\)]+)>?\)`)
var artifactExtension = regexp.MustCompile(`(?i)\b(png|jpe?g|webp|mp4|mp3|wav|pdf|docx|xlsx|pptx|dxf|stl|step|stp|zip)\b`)
var fileDeliveryAction = regexp.MustCompile(`(?i)(生成|制作|创建|导出|保存|输出|交付|generate|create|export|save|deliver).{0,60}\b(png|jpe?g|webp|mp4|mp3|wav|pdf|docx|xlsx|pptx|dxf|stl|step|stp|zip)\b`)

func requiredDeliveryKind(prompt string) string {
	for _, prefix := range []string{"不要", "不用", "停止", "取消", "别生成", "do not", "don't", "stop", "cancel"} {
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(prompt)), prefix) {
			return ""
		}
	}
	if kind := generationIntent(prompt); kind != "" {
		return kind
	}
	if fileDeliveryAction.MatchString(prompt) && !mediaCodeRequest.MatchString(prompt) && artifactExtension.MatchString(prompt) {
		return "file"
	}
	return ""
}

// Caller holds a.mu, so the workspace identity and proposals cannot change.
func (a *App) inspectTaskDeliveryLocked(task *Task, out string) *TaskDelivery {
	prompt := task.Prompt
	if task.MediaPrompt != "" {
		prompt = task.MediaPrompt
	}
	kind := requiredDeliveryKind(prompt)
	if task.MediaGeneration != nil {
		kind = task.MediaGeneration.Kind
	} else if native := generationModelOutput(task.Model); native != "" {
		kind = native
	}
	if kind == "" {
		return nil
	}
	d := &TaskDelivery{State: "missing", Kind: kind, CheckedAt: time.Now().UTC().Format(time.RFC3339Nano), Reason: "尚无可访问且格式匹配的交付文件"}
	seen := map[string]bool{}
	for _, m := range artifactLink.FindAllStringSubmatch(out, -1) {
		u, err := url.Parse(m[1])
		if err != nil || u.IsAbs() || u.Host != "" || u.RawQuery != "" || u.Fragment != "" {
			continue
		}
		p := u.Path
		if safePath(p) != nil || a.workspace == nil || a.workspaceMode() == "ssh" {
			continue
		}
		if ok, _ := canAccessMemory(a.dataPath, callerAide, filepath.Join(a.workspace.Name(), p), opRead); !ok {
			continue
		}
		pre, err := a.workspace.Lstat(p)
		if err != nil || !pre.Mode().IsRegular() {
			continue
		}
		f, err := a.workspace.Open(p)
		if err != nil {
			continue
		}
		info, statErr := f.Stat()
		b, readErr := io.ReadAll(io.LimitReader(f, 512))
		f.Close()
		if statErr != nil || readErr != nil || !info.Mode().IsRegular() || info.Size() == 0 {
			continue
		}
		valid := true
		switch kind {
		case "image":
			valid = strings.HasPrefix(http.DetectContentType(b), "image/")
		case "video":
			valid = strings.EqualFold(filepath.Ext(p), ".mp4") && mediaFileMatches(b, "mp4")
		case "file":
			ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(p)), ".")
			valid = false
			for _, requested := range artifactExtension.FindAllString(strings.ToLower(prompt), -1) {
				if requested == ext {
					valid = true
				}
			}
		case "speech":
			ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(p)), ".")
			valid = (ext == "mp3" || ext == "wav") && mediaFileMatches(b, ext)
		}
		if valid && !seen[p] {
			d.Artifacts = append(d.Artifacts, p)
			seen[p] = true
		}
	}
	if len(d.Artifacts) > 0 {
		d.State = "delivered"
		d.Reason = ""
		return d
	}
	for _, f := range task.Files {
		if !f.Applied && strings.Contains(strings.ToLower(prompt), strings.TrimPrefix(strings.ToLower(filepath.Ext(f.Path)), ".")) && filepath.Ext(f.Path) != "" {
			d.State = "pending_approval"
			d.Reason = "文件修改仍是待审批提案"
			return d
		}
	}
	return d
}

func (a *App) checkTaskDelivery(task *Task, out string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	d := a.inspectTaskDeliveryLocked(task, out)
	task.Delivery = d
	if d != nil && d.State == "missing" {
		return fmt.Errorf("%w：%s；已有记录保留，请检查产物或继续当前任务，不能把执行承诺作为完成", errDeliveryIncomplete, d.Reason)
	}
	return nil
}

// Called by the existing proposal application path, not by model assertions.
func (a *App) finalizeProposalDeliveryLocked(task *Task) {
	if task.Delivery == nil || task.Delivery.State != "pending_approval" || !task.Applied {
		return
	}
	d := &TaskDelivery{State: "missing", Kind: task.Delivery.Kind, Reason: "提案已应用，尚未验证交付文件", CheckedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	for _, f := range task.Files {
		b, err := a.readAppliedProposalLocked(f)
		if err == nil && len(b) > 0 && string(b) == f.Content {
			d.Artifacts = append(d.Artifacts, proposalDisplayPath(f))
		}
	}
	if len(d.Artifacts) > 0 {
		d.State = "delivered"
		d.Reason = ""
	}
	task.Delivery = d
}
