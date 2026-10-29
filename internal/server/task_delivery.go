package server

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
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

type deliveryWorkspace struct {
	root     *os.Root
	id       string
	revision uint64
	dataPath string
	remote   bool
}

func (a *App) deliveryWorkspaceLocked() deliveryWorkspace {
	return deliveryWorkspace{a.workspace, a.wsID(), a.wsRevision, a.dataPath, a.workspaceMode() == "ssh"}
}

// Caller holds a.mu. Runtime checkTaskDelivery uses an immutable snapshot so
// bounded hashing does not hold the application's global lock.
func (a *App) inspectTaskDeliveryLocked(task *Task, out string) *TaskDelivery {
	return a.deliveryWorkspaceLocked().inspect(task, out)
}

func (scope deliveryWorkspace) inspect(task *Task, out string) *TaskDelivery {
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
	d := &TaskDelivery{State: "missing", Kind: kind, CheckedAt: time.Now().UTC().Format(time.RFC3339Nano), Reason: "尚无格式匹配且能验证由本任务新建、修改或写入的交付文件"}
	seen := map[string]bool{}
	remaining := int64(deliveryBaselineReadBytes)
	pathEntries := deliveryBaselineEntries
	for _, m := range artifactLink.FindAllStringSubmatch(out, 32) {
		u, err := url.Parse(m[1])
		if err != nil || u.IsAbs() || u.Host != "" || u.RawQuery != "" || u.Fragment != "" {
			continue
		}
		p := u.Path
		if safePath(p) != nil || !utf8.ValidString(p) || scope.root == nil || scope.remote {
			continue
		}
		if ok, _ := canAccessMemory(scope.dataPath, callerAide, filepath.Join(scope.root.Name(), p), opRead); !ok {
			continue
		}
		f, info, err := openDeliveryFile(scope.root, p)
		if err != nil {
			continue
		}
		b, readErr := io.ReadAll(io.LimitReader(f, 512))
		if readErr != nil || info.Size() == 0 {
			f.Close()
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
		if valid && !seen[p] && scope.deliveryIsCurrent(task, p, f, info, &remaining, &pathEntries) {
			after, statErr := f.Stat()
			linked, linkErr := scope.root.Lstat(p)
			if statErr == nil && linkErr == nil && os.SameFile(info, after) && os.SameFile(info, linked) &&
				after.Size() == info.Size() && after.ModTime().Equal(info.ModTime()) {
				d.Artifacts = append(d.Artifacts, p)
				seen[p] = true
			}
		}
		f.Close()
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
	a.filesMu.Lock()
	defer a.filesMu.Unlock()
	a.mu.Lock()
	scope := a.deliveryWorkspaceLocked()
	owner := a.approvalSessionLocked(task)
	snapshot := &Task{Prompt: task.Prompt, MediaPrompt: task.MediaPrompt, Model: task.Model,
		WorkspaceID: task.WorkspaceID, WorkspaceRev: task.WorkspaceRev, DeliveryBaseline: task.DeliveryBaseline,
		Files: append([]Change(nil), task.Files...)}
	if task.MediaGeneration != nil {
		generation := *task.MediaGeneration
		snapshot.MediaGeneration = &generation
	}
	a.mu.Unlock()
	d := scope.inspect(snapshot, out)
	a.mu.Lock()
	defer a.mu.Unlock()
	stable := scope.root == a.workspace && scope.id == a.wsID() && scope.revision == a.wsRevision && scope.dataPath == a.dataPath && scope.remote == (a.workspaceMode() == "ssh") &&
		task.WorkspaceID == snapshot.WorkspaceID && task.WorkspaceRev == snapshot.WorkspaceRev &&
		task.Prompt == snapshot.Prompt && task.MediaPrompt == snapshot.MediaPrompt && task.DeliveryBaseline == snapshot.DeliveryBaseline &&
		generationModelOutput(task.Model) == generationModelOutput(snapshot.Model) && len(task.Files) == len(snapshot.Files)
	if stable {
		for i := range task.Files {
			stable = stable && task.Files[i] == snapshot.Files[i]
		}
	}
	if task.MediaGeneration == nil || snapshot.MediaGeneration == nil {
		stable = stable && task.MediaGeneration == nil && snapshot.MediaGeneration == nil
	} else {
		left, right := task.MediaGeneration, snapshot.MediaGeneration
		stable = stable && left.Kind == right.Kind && left.Asset == right.Asset && left.Identity == right.Identity && left.RequestHash == right.RequestHash
	}
	if owner != nil {
		stable = stable && !owner.Deleted && a.approvalSessionLocked(task) == owner
	}
	if !stable {
		return fmt.Errorf("%w：工作区或任务证据已变化，请重新检查交付", errDeliveryIncomplete)
	}
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
		b, err := a.readAppliedProposalLocked(task, f)
		if err == nil && len(b) > 0 && string(b) == f.Content {
			d.Artifacts = append(d.Artifacts, proposalDisplayPath(f))
		}
	}
	if len(d.Artifacts) > 0 {
		d.State = "delivered"
		d.Reason = ""
	}
	task.Delivery = d
	if d.State == "delivered" {
		task.DeliveryBaseline = nil
	}
}
