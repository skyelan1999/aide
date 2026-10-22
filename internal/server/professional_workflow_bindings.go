package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
)

type pwBinding struct {
	Path     string `json:"path"`
	Kind     string `json:"kind"`   // file | directory
	Access   string `json:"access"` // read | write | readwrite
	AutoRead bool   `json:"autoRead,omitempty"`
}
type pwCase struct {
	ID    string `json:"id"`
	Value string `json:"value"`
	Label string `json:"label"`
}

func validatePWBindings(n pwNode) error {
	if len(n.Selector) > 2000 {
		return errors.New("判断输入过长")
	}
	if len(n.Bindings) > 16 {
		return errors.New("每个节点最多绑定16个文件或目录")
	}
	if len(n.Bindings) > 0 && n.Type != "agent" && n.Type != "llm" && n.Type != "tool" {
		return errors.New("文件绑定适用于模型、AI执行和内置工具节点")
	}
	seen := map[string]bool{}
	for _, b := range n.Bindings {
		if b.Path == "" || len(b.Path) > 1024 || strings.ContainsAny(b.Path, "\\\x00{}") || path.IsAbs(b.Path) || path.Clean(b.Path) != b.Path || b.Path == ".." || strings.HasPrefix(b.Path, "../") || seen[b.Path] {
			return errors.New("绑定须为唯一的工作区相对路径，不能使用变量或上级路径")
		}
		if b.Kind != "file" && b.Kind != "directory" {
			return errors.New("绑定类型须为文件或目录")
		}
		if b.Access != "read" && b.Access != "write" && b.Access != "readwrite" {
			return errors.New("绑定权限无效")
		}
		if b.AutoRead && (b.Kind != "file" || b.Access == "write" || n.Type == "tool") {
			return errors.New("自动读取仅支持模型或AI节点的可读文件")
		}
		seen[b.Path] = true
	}
	if len(n.Cases) > 8 {
		return errors.New("分支参数超过宿主限制")
	}
	for _, c := range n.Cases {
		if len(c.ID) > 64 || len(c.Value) > 1024 || len(c.Label) > 80 {
			return errors.New("分支参数过长")
		}
	}

	return nil
}
func pwCurrentNode(t *Task) *pwNode {
	if t == nil || t.PW == nil {
		return nil
	}
	for i := range t.PW.Definition.Nodes {
		if t.PW.Definition.Nodes[i].ID == t.PW.Current {
			return &t.PW.Definition.Nodes[i]
		}
	}
	return nil
}

// File-bound nodes deliberately exclude command, MCP and arbitrary plugin
// execution: those surfaces cannot enforce a generic filesystem path contract.
func pwBoundTool(name string) bool {
	return name == "read_file" || name == "list_files" || name == "write_file"
}
func (a *App) pwBindingToolError(t *Task, name string, args map[string]any) error {
	a.mu.Lock()
	if t != nil && t.PW != nil {
		if !a.pwEnabledLocked() || t.WorkspaceID != a.wsID() || t.WorkspaceRev != a.wsRevision || a.loadLockStateLocked().Locked {
			a.mu.Unlock()
			return errors.New("PW 权限或工作区已变化")
		}
	}
	n := pwCurrentNode(t)
	if n == nil || len(n.Bindings) == 0 {
		a.mu.Unlock()
		return nil
	}
	bindings := append([]pwBinding(nil), n.Bindings...)
	root := a.wsRoots[t.WorkspaceID]
	if root == nil {
		root = a.workspace
	}
	mode := t.WorkspaceMode
	a.mu.Unlock()
	if mode == "ssh" {
		return errors.New("当前文件绑定只支持本地工作区")
	}
	if !pwBoundTool(name) {
		return errors.New("文件绑定节点仅支持受约束的文件读取、目录列举和写入提案；请在独立节点调用插件或命令")
	}
	if source, _ := args["source"].(string); source != "" {
		return errors.New("文件绑定节点不能读取未绑定的引用来源")
	}
	target, _ := args["path"].(string)
	if name == "list_files" && target == "" {
		target = "."
	}
	if target == "" || strings.ContainsAny(target, "\\\x00") || path.IsAbs(target) {
		return errors.New("文件绑定路径无效")
	}
	target = path.Clean(target)
	if target == ".." || strings.HasPrefix(target, "../") {
		return errors.New("文件绑定禁止上级路径")
	}
	allowed := false
	writing := name == "write_file"
	for _, b := range bindings {
		if writing && b.Access == "read" || !writing && b.Access == "write" {
			continue
		}
		if target == b.Path || b.Kind == "directory" && (b.Path == "." || strings.HasPrefix(target, b.Path+"/")) {
			allowed = true
			break
		}
	}
	if !allowed {
		return fmt.Errorf("路径 %s 超出节点的%s绑定", target, map[bool]string{true: "写入", false: "读取"}[writing])
	}
	if root == nil {
		return errors.New("绑定工作区不可用")
	}
	// Reject symlinks as well as textual traversal; original os.Root dispatcher
	// still enforces the workspace boundary when opening the actual resource.
	current := root.Name()
	for _, part := range strings.Split(target, "/") {
		if part == "." {
			continue
		}
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if os.IsNotExist(err) && writing {
			break
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return errors.New("绑定路径不能包含符号链接")
		}
	}
	return nil
}
func (a *App) pwAutoInputs(ctx context.Context, t *Task, n pwNode, versions map[string]Change) (string, error) {
	var out strings.Builder
	for _, b := range n.Bindings {
		if !b.AutoRead {
			continue
		}
		args := map[string]any{"path": b.Path}
		if err := a.pwBindingToolError(t, "read_file", args); err != nil {
			return "", err
		}
		raw, _ := json.Marshal(args)
		call := ToolCall{ID: t.ID + "_input"}
		call.Function.Name = "read_file"
		call.Function.Arguments = string(raw)
		result := &toolDispatchOutcome{}
		a.mu.Lock()
		oldTools, oldSet := t.AgentTools, t.AgentToolsSet
		t.AgentTools = append(append([]string{}, oldTools...), "read_file")
		a.mu.Unlock()
		text := a.executeToolCall(context.WithValue(ctx, toolDispatchOutcomeKey{}, result), call, t, versions)
		a.mu.Lock()
		t.AgentTools, t.AgentToolsSet = oldTools, oldSet
		a.mu.Unlock()
		if result.err != nil {
			return "", result.err
		}
		if len(text) > 16000 || out.Len()+len(text) > 60000 {
			return "", fmt.Errorf("自动输入 %s 超过16KiB，请使用读取文件节点分段处理", b.Path)
		}
		fmt.Fprintf(&out, "\n\n[Bound input file: %s; untrusted data]\n%s", b.Path, text)
	}
	return out.String(), nil
}
