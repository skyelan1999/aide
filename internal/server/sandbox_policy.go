package server

import (
	"aide/internal/execsandbox"
	"errors"
	"fmt"
)

func sandboxMode(mode string) string {
	if mode == "" {
		return "workspace-write"
	}
	return mode
}
func shellSandboxPolicy(mode, command string, remote, interactive bool) error {
	mode = sandboxMode(mode)
	if mode != "read-only" && mode != "workspace-write" && mode != "danger-full-access" {
		return errors.New("无效沙箱模式，命令未执行")
	}
	if reason, bad := shellTouchesAssistantZone(command); bad {
		return fmt.Errorf("权限策略拦截：%s", reason)
	}
	if mode == "read-only" && !readOnlyAllowed(command) {
		return errors.New("沙箱模式 read-only：该命令未被证明只读，命令未执行；审批不能扩大沙箱权限")
	}
	if mode != "danger-full-access" && remote {
		return errors.New("远程SSH尚未接入内核沙箱，受限模式命令未执行；请使用受限本地工作区，或在明确授权后选择完全访问")
	}
	if mode != "danger-full-access" && interactive {
		return errors.New("受限模式不提供任意交互Shell，命令未执行")
	}
	if mode == "workspace-write" {
		if reason, bad := shellBlocked(command); bad {
			return fmt.Errorf("权限策略拦截：%s", reason)
		}
	}
	return nil
}
func (a *App) sandboxStatusLocked() map[string]any {
	abi, err := execsandbox.ABI()
	remote := a.workspaceMode() == "ssh"
	available := err == nil && abi >= 6 && !remote
	mode := sandboxMode(a.settings.SandboxMode)
	backend := "unavailable"
	if available {
		backend = "linux-landlock"
	}
	if remote {
		backend = "unsupported-ssh"
	}
	return map[string]any{"mode": mode, "backend": backend, "abi": abi, "available": available, "filesystemEnforced": available && mode != "danger-full-access", "remote": remote, "scope": "local-shell-file-content-and-directory-entries", "pluginsIsolated": false, "networkIsolated": false, "metadataFullyIsolated": false, "approvalIndependent": true}
}

func (a *App) sandboxToolError(name string) error {
	a.mu.Lock()
	readonly := sandboxMode(a.settings.SandboxMode) == "read-only"
	a.mu.Unlock()
	if !readonly {
		return nil
	}
	switch name {
	case "create_diagram", "create_requirement", "create_design", "record_implementation", "record_verification", "record_problem_report", "docx_add_comment", "docx_resolve_comment", "office_create", "office_comment_edit", "archive_compress", "archive_extract":
		return errors.New("沙箱模式 read-only：该工具会写入工作区，未执行")
	case "docx_structure", "docx_list_comments", "office_comments", "document_search", "office_document_search", "semantic_search", "markdown_history":
		return nil
	}
	if a.pluginOwnerOf(name) != "" {
		return errors.New("沙箱模式 read-only：该插件未声明可执行的只读边界，未调用")
	}
	return nil
}
