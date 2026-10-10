package server

import (
	"crypto/sha256"
	"fmt"
	"path"
	"strings"
)

// A pending proposal is not a failed disk write. Never return its proposed
// contents as if read_file had read them from disk.
func (a *App) pendingNewFileRead(task *Task, p string) string {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, f := range task.Files {
		if f.Path == path.Clean(p) && !f.Applied && f.BaseHash == "" && f.Before == "" {
			return "文件状态：待审批的新文件提案，尚未写入磁盘。审批在本轮模型结束后执行；这不是读取失败。不要重复读取或改用其他工具绕过审批，也不要断言最终写入失败。请结束本轮并说明待审批，最终应用结果由系统回执补充。"
		}
	}
	return ""
}

// The model's final answer precedes independent review. Preserve that answer
// and append a distinctly identified, verified system receipt after review.
// Lock in writer order and pin the workspace before reading actual bytes.
func (a *App) recordReviewedFileResult(task *Task) {
	a.filesMu.Lock()
	defer a.filesMu.Unlock()
	a.mu.Lock()
	defer a.mu.Unlock()
	s := a.approvalSessionLocked(task)
	if s == nil || !task.AutoReview || len(task.Files) == 0 {
		return
	}
	for _, step := range task.Steps {
		if step.Name == "file_application_receipt" {
			return
		}
	}
	var lines []string
	status := "completed"
	if !task.Applied {
		lines = append(lines, "文件提案尚未全部应用。需要人工确认或处理审批记录中的原因；本回执不证明写入成功。")
	} else if task.WorkspaceID != a.wsID() || a.workspaceMode() == "ssh" {
		lines = append(lines, "应用记录显示已写入；当前工作区已变化或为远程环境，未进行本地字节读回验证。")
	} else {
		for _, f := range task.Files {
			b, err := readRawBytes(a.workspace, f.Path)
			if err != nil {
				lines = append(lines, fmt.Sprintf("- `%s`：应用后读回失败：%v。", f.Path, err))
				status = "failed"
			} else if string(b) != f.Content {
				lines = append(lines, fmt.Sprintf("- `%s`：应用后内容已变化，当前字节与提案不一致。", f.Path))
				status = "failed"
			} else {
				lines = append(lines, fmt.Sprintf("- `%s`：已实际写入并读回一致，%d 字节，SHA-256 `%x`。", f.Path, len(b), sha256.Sum256(b)))
			}
		}
	}
	content := "### 系统应用回执（模型回复后的实际状态）\n\n" + strings.Join(lines, "\n")
	step := Step{Name: "file_application_receipt", Status: status, Content: content}
	task.Steps = append(task.Steps, step)
	s.Messages = append(s.Messages, Message{Role: "assistant", Content: content})
	if err := a.save(s); err != nil {
		task.Error = "应用回执保存失败：" + err.Error()
	}
}
