package server

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// Native Office parsers receive a temporary authorized file, not SSH credentials.
// Source identity is checked before staging and after transformation/writeback.
func (a *App) remoteOfficeSnapshot(ctx context.Context, expectedID, remoteBase, source, p string, write bool, transform func(string) (string, error)) (string, error) {
	if err := validDocxPath(p); err != nil {
		return "", err
	}
	a.mu.Lock()
	valid := a.wsID() == expectedID && (source != "" || a.workspaceMode() == "ssh" && a.wsConfig.Workspace.Path == remoteBase)
	areas := a.pluginSourceAreasLocked()
	a.mu.Unlock()
	if !valid {
		return "", errors.New("Office 调用绑定的 SSH 工作区已变化")
	}
	dir, e := os.MkdirTemp("", "aide-remote-office-*")
	if e != nil {
		return "", e
	}
	defer os.RemoveAll(dir)
	var total int64
	entries := map[string]*pluginSourceCheckout{}
	result, e := a.pluginCheckoutRequest(ctx, areas, dir, &total, entries, pluginSourceRequest{Operation: "checkout", Source: source, Path: p, Slot: "office", Write: write})
	if e != nil {
		return "", e
	}
	local := filepath.Join(dir, result.(map[string]any)["path"].(string))
	raw, e := os.ReadFile(local)
	if e != nil {
		return "", e
	}
	if e = checkOfficeArchive(raw); e != nil {
		return "", e
	}
	out, e := transform(local)
	if e != nil {
		return "", e
	}
	entry := entries["office"]
	if !a.pluginCheckoutEntryCurrent(entry) {
		return "", errors.New("Office 处理期间工作区或来源配置已变化，结果丢弃")
	}
	if write {
		raw, e = os.ReadFile(local)
		if e != nil {
			return "", e
		}
		if e = checkOfficeArchive(raw); e != nil {
			return "", e
		}
		if _, e = a.pluginCheckoutRequest(ctx, areas, dir, &total, entries, pluginSourceRequest{Operation: "commit", Slot: "office"}); e != nil {
			return "", e
		}
	}
	return out, nil
}

func (a *App) docxToolBound(ctx context.Context, wsRoot *os.Root, mode, expectedID, remoteBase, p, script string, extra ...string) string {
	return a.docxToolSourceBound(ctx, wsRoot, mode, expectedID, remoteBase, "", p, script, extra...)
}
func (a *App) docxToolSourceBound(ctx context.Context, wsRoot *os.Root, mode, expectedID, remoteBase, source, p, script string, extra ...string) string {
	if mode != "ssh" && source == "" {
		return a.docxTool(wsRoot, mode, p, script, extra...)
	}
	out, e := a.remoteOfficeSnapshot(ctx, expectedID, remoteBase, officeSourceID(source), p, script == "docx_resolve_comment.py", func(local string) (string, error) {
		sp, e := officeScriptPath(script)
		if e != nil {
			return "", e
		}
		return runOfficeScript(filepath.Dir(local), sp, append([]string{filepath.Base(local)}, extra...)...)
	})
	if e != nil {
		return "Office 远端操作失败: " + e.Error()
	}
	return strings.TrimSpace(out)
}

func (a *App) officeCommentToolBound(ctx context.Context, wsRoot *os.Root, mode, expectedID, remoteBase, p, operation string, spec map[string]any) string {
	return a.officeCommentToolSourceBound(ctx, wsRoot, mode, expectedID, remoteBase, "", p, operation, spec)
}
func (a *App) officeCommentToolSourceBound(ctx context.Context, wsRoot *os.Root, mode, expectedID, remoteBase, source, p, operation string, spec map[string]any) string {
	if mode != "ssh" && source == "" {
		return a.officeCommentTool(wsRoot, mode, remoteBase, p, operation, spec)
	}
	out, e := a.remoteOfficeSnapshot(ctx, expectedID, remoteBase, officeSourceID(source), p, operation != "list", func(local string) (string, error) {
		raw, e := os.ReadFile(local)
		if e != nil {
			return "", e
		}
		result, changed, e := docxCommentRun(raw, operation, spec)
		if e != nil {
			return "", e
		}
		if operation != "list" {
			if e = os.WriteFile(local, changed, 0600); e != nil {
				return "", e
			}
		}
		b, e := json.Marshal(result)
		return string(b), e
	})
	if e != nil {
		return "Office 远端批注失败: " + e.Error()
	}
	return out
}

func officeSourceID(source string) string {
	if source == "" {
		return ""
	}
	return "source:" + strings.TrimPrefix(source, "source:")
}
