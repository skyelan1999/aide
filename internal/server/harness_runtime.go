package server

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

func taskHarnessConfig(task *Task) HarnessConfig {
	if task.HarnessConfig != nil {
		return *task.HarnessConfig
	}
	return defaultHarnessConfig()
}
func taskToolDenied(task *Task, name string) bool {
	for _, tool := range taskHarnessConfig(task).DenyTools {
		if tool == name {
			return true
		}
	}
	if task.AgentToolsSet {
		for _, tool := range task.AgentTools {
			if tool == name {
				return false
			}
		}
		return true
	}
	return false
}
func filterTaskTools(task *Task, tools []any) []any {
	filtered := make([]any, 0, len(tools))
	for _, tool := range tools {
		m, ok := tool.(map[string]any)
		if !ok {
			continue
		}
		fn, ok := m["function"].(map[string]any)
		if !ok {
			continue
		}
		name, _ := fn["name"].(string)
		if !taskToolDenied(task, name) {
			filtered = append(filtered, tool)
		}
	}
	return filtered
}
func harnessInstruction(c HarnessConfig) string {
	var b strings.Builder
	if len(c.Skills) > 0 {
		b.WriteString("\n## 已配置 Skills\n按任务需要调用 read_skill 获取说明；说明不授予工具权限。\n")
		for _, s := range c.Skills {
			fmt.Fprintf(&b, "- %s: %s\n", s.Name, s.Description)
		}
	}
	if len(c.Agents) > 0 {
		b.WriteString("\n## 已配置子代理\nspawn_subagent 的 agent 参数选择已定义的角色。\n")
		for _, a := range c.Agents {
			fmt.Fprintf(&b, "- %s: %s\n", a.Name, a.Description)
		}
	}
	return b.String()
}
func (a *App) readHarnessSkill(task *Task, name string) string {
	for _, skill := range taskHarnessConfig(task).Skills {
		if skill.Name != name {
			continue
		}
		if skill.Instruction != "" {
			return skill.Instruction
		}
		a.mu.Lock()
		root := a.wsRoots[task.WorkspaceID]
		if root == nil && task.WorkspaceID == a.wsID() {
			root = a.workspace
		}
		mode := task.WorkspaceMode
		remote := task.WorkspaceRemotePath
		a.mu.Unlock()
		var b []byte
		var err error
		if err = safePath(skill.Path); err != nil {
			return "错误：无效 Skill 路径"
		}
		if mode != "ssh" && root == nil {
			return "错误：Skill 所属工作区不可用"
		}
		if mode == "ssh" {
			b, err = a.sftpRead(pathJoinRemote(remote, skill.Path))
		} else {
			b, err = readText(root, skill.Path)
		}
		if err != nil {
			return "错误：读取 Skill 失败: " + err.Error()
		}
		if err := validateTextContent(b); err != nil {
			return "错误：Skill 必须是有效文本: " + err.Error()
		}
		if len(b) > 64*1024 {
			return "错误：Skill 超过64KiB，请拆分说明"
		}
		return string(b)
	}
	return "错误：Skill 未配置"
}

// Same approval and shell executor as run_shell; Hooks cannot bypass either.
func (a *App) approvedShell(ctx context.Context, task *Task, command string, timeout time.Duration) (string, bool) {
	if command == "" {
		return "缺少 command 参数", false
	}
	if a.toolDenied("run_shell") || taskToolDenied(task, "run_shell") {
		return "工具 run_shell 已禁用", false
	}
	remembered := false
	if !readOnlyAllowed(command) {
		var err error
		remembered, err = a.approveRememberedShell(task, command)
		if err != nil {
			return "审批记录保存失败，命令未运行: " + err.Error(), false
		}
	}
	if !readOnlyAllowed(command) && !remembered {
		q, _ := json.Marshal(map[string]any{"question": "即将执行可能修改文件、改变外部状态或访问网络的命令。请检查完整命令后决定是否继续：\n\n" + command, "type": "confirm", "approvalKind": "shell", "command": command})
		if a.awaitUserAnswer(ctx, task, q) != "确认" {
			return "用户未批准执行该命令；命令没有运行。", false
		}
	}
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	out, code, err := a.execShellCommand(ctx, task, command)
	if err != nil || code != 0 {
		fb := analyzeShellFailure(command, out, code, err)
		if strings.TrimSpace(out) != "" {
			fb += "\n\n--- 原始输出 ---\n" + out
		}
		return fb, false
	}
	res := "exit code: " + fmt.Sprint(code)
	if strings.TrimSpace(out) != "" {
		res += "\n" + out
	}
	return res, true
}
func (a *App) runHarnessHooks(ctx context.Context, task *Task, step string, chain []Message, event, tool string) error {
	if a.toolDenied(tool) || taskToolDenied(task, tool) {
		return nil
	}
	for _, hook := range taskHarnessConfig(task).Hooks {
		if !hook.Enabled || hook.Event != event || (hook.Tool != "*" && hook.Tool != tool) {
			continue
		}
		if task.HookRecoveryRequired {
			return fmt.Errorf("Hook 结果未知：请先检查外部状态，确认后重试任务；续跑不会重复执行 Hook")
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		args, _ := json.Marshal(map[string]string{"command": hook.Command})
		call := ToolCall{ID: "hook-" + newID()}
		call.Function.Name = "run_shell"
		call.Function.Arguments = string(args)
		if err := a.checkpointExecution(task, step, chain, "tool_dispatch", &call, ""); err != nil {
			return err
		}
		a.publishStream(task.ID, streamEvent{Event: "note", Text: "执行 Hook: " + hook.Name})
		result, ok := a.approvedShell(ctx, task, hook.Command, time.Duration(hook.TimeoutSec)*time.Second)
		if err := a.checkpointExecution(task, step, chain, "tool_result", &call, result); err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("Hook %s 未完成：%s", hook.Name, result)
		}
	}
	return nil
}
