package server

import "context"

// Caller holds a.mu. Plugin installation state and active project files have
// different roots; remote projects must never fall back to local /workspace.
func (a *App) pluginWorkspaceLocked() string {
	if a.workspaceMode() == "ssh" {
		return ""
	}
	if a.workspace != nil {
		return a.workspace.Name()
	}
	return a.workPath
}

type pluginWorkspaceContextKey struct{}

func pluginWorkspaceFromContext(ctx context.Context) string {
	if root, ok := ctx.Value(pluginWorkspaceContextKey{}).(string); ok {
		return root
	}
	return "/workspace" // legacy daemon callers; App dispatch always binds explicitly
}
