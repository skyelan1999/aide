package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Managed worktrees are ordinary selectable workspaces. Switching does not
// retarget running tasks; the existing workspace/AgentRoot snapshots still own
// file reads, command CWD and proposal identity. Archive retains all files.
type ManagedWorktree struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	WorkspaceID   string `json:"workspaceId"`
	BasePath      string `json:"basePath"`
	Path          string `json:"path"`
	WorkspacePath string `json:"workspacePath"`
	BaseCommit    string `json:"baseCommit"`
	State         string `json:"state"` // creating, ready, outcome_unknown, archived
	Created       string `json:"created"`
	Error         string `json:"error,omitempty"`
}

func (a *App) managedWorktreesPath() string {
	return filepath.Join(a.dataPath, "config", "worktrees.json")
}
func (a *App) loadManagedWorktrees() ([]ManagedWorktree, error) {
	b, err := os.ReadFile(a.managedWorktreesPath())
	if os.IsNotExist(err) {
		return []ManagedWorktree{}, nil
	}
	if err != nil {
		return nil, err
	}
	if len(b) > 128*1024 {
		return nil, errors.New("工作树登记超过128KiB")
	}
	var entries []ManagedWorktree
	err = json.Unmarshal(b, &entries)
	for i := range entries {
		if entries[i].State == "creating" {
			entries[i].State = "outcome_unknown"
			entries[i].Error = "创建结果尚未持久化，先检查Git工作树状态；不要直接重复创建。"
		}
	}
	return entries, err
}
func (a *App) currentWorktreeIDLocked() string {
	entries, err := a.loadManagedWorktrees()
	if err != nil {
		return ""
	}
	for _, entry := range entries {
		if entry.State == "ready" && filepath.Clean(entry.Path) == filepath.Clean(a.containerAbs) {
			return entry.ID
		}
	}
	return ""
}
func worktreeGit(ctx context.Context, dir string, args ...string) (string, error) {
	flags := []string{"-C", dir, "-c", "core.hooksPath=/dev/null", "-c", "core.fsmonitor=false", "-c", "submodule.recurse=false"}
	cmd := exec.CommandContext(ctx, "git", append(flags, args...)...)
	out := &limitedBytesWriter{limit: 128 << 10}
	cmd.Stdout = out
	cmd.Stderr = out
	err := cmd.Run()
	if err != nil {
		return out.String(), fmt.Errorf("Git 操作失败: %w: %s", err, out.String())
	}
	return out.String(), nil
}
func (a *App) managedWorktrees(w http.ResponseWriter, r *http.Request) {
	a.worktreeMu.Lock()
	defer a.worktreeMu.Unlock()
	a.mu.Lock()
	entries, err := a.loadManagedWorktrees()
	if err != nil {
		a.mu.Unlock()
		fail(w, 500, err)
		return
	}
	workspaceID := a.wsID()
	mode := a.workspaceMode()
	base := a.containerAbs
	display := a.wsConfig.Workspace.Path
	sandbox := a.settings.SandboxMode
	a.mu.Unlock()
	visible := func(entry ManagedWorktree) bool {
		return entry.WorkspaceID == workspaceID || (mode != "ssh" && base != "" &&
			(filepath.Clean(entry.Path) == filepath.Clean(base) || filepath.Clean(entry.BasePath) == filepath.Clean(base)))
	}
	if r.Method == http.MethodGet {
		out := []ManagedWorktree{}
		for _, entry := range entries {
			if visible(entry) {
				out = append(out, entry)
			}
		}
		jsonOut(w, 200, map[string]any{"worktrees": out, "workspaceId": workspaceID, "supported": mode != "ssh"})
		return
	}
	if mode == "ssh" {
		fail(w, 400, errors.New("托管工作树仅支持本地Git工作区"))
		return
	}
	if sandbox == "read-only" {
		fail(w, 403, errors.New("只读沙箱不创建或归档工作树"))
		return
	}
	if r.PathValue("worktree") != "" {
		for i := range entries {
			if entries[i].ID == r.PathValue("worktree") && visible(entries[i]) {
				if filepath.Clean(entries[i].Path) == filepath.Clean(base) {
					fail(w, 409, errors.New("先切换到原工作区，再归档当前工作树"))
					return
				}
				entries[i].State = "archived"
				if err := atomicJSON(a.managedWorktreesPath(), entries); err != nil {
					fail(w, 500, err)
					return
				}
				jsonOut(w, 200, map[string]any{"worktree": entries[i], "filesRetained": true})
				return
			}
		}
		fail(w, 404, errors.New("工作树不存在"))
		return
	}
	var in struct {
		Name        string `json:"name"`
		Ref         string `json:"ref"`
		WorkspaceID string `json:"workspaceId"`
	}
	if err := decode(w, r, &in); err != nil {
		fail(w, 400, err)
		return
	}
	if in.WorkspaceID != workspaceID {
		fail(w, 409, errors.New("工作区已变化，请重新加载"))
		return
	}
	if !harnessName.MatchString(in.Name) {
		fail(w, 400, errors.New("名称须为小写字母开头，最多64个字母数字连字符"))
		return
	}
	if len(entries) >= 64 {
		fail(w, 409, errors.New("工作树登记最多64项；归档保留原文件和登记"))
		return
	}
	if in.Ref == "" {
		in.Ref = "HEAD"
	}
	if len(in.Ref) > 200 || strings.HasPrefix(in.Ref, "-") {
		fail(w, 400, errors.New("无效Git版本"))
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	base, err = filepath.EvalSymlinks(base)
	if err != nil {
		fail(w, 400, err)
		return
	}
	// Checkout filters can execute repository-defined code. Require the operator
	// to handle those repositories explicitly rather than silently running them.
	if out, _ := worktreeGit(ctx, base, "config", "--get-regexp", "^filter\\."); strings.TrimSpace(out) != "" {
		fail(w, 409, errors.New("仓库配置了checkout过滤器，请在终端审阅后手动创建工作树"))
		return
	}
	top, err := worktreeGit(ctx, base, "rev-parse", "--show-toplevel")
	if err != nil {
		fail(w, 400, err)
		return
	}
	if filepath.Clean(strings.TrimSpace(top)) != base {
		fail(w, 400, errors.New("请先选择Git仓库根目录"))
		return
	}
	commit, err := worktreeGit(ctx, base, "rev-parse", "--verify", in.Ref+"^{commit}")
	if err != nil {
		fail(w, 400, err)
		return
	}
	commit = strings.TrimSpace(commit)
	rel := filepath.Join(".cache", "aide", "worktrees")
	if _, err := worktreeGit(ctx, base, "check-ignore", "--quiet", "--no-index", filepath.Join(rel, "probe")); err != nil {
		fail(w, 409, errors.New("请先在Git忽略规则中排除 .cache/aide/worktrees/，再创建工作树"))
		return
	}
	parent := base
	for _, part := range []string{".cache", "aide", "worktrees"} {
		parent = filepath.Join(parent, part)
		st, e := os.Lstat(parent)
		if e == nil && (!st.IsDir() || st.Mode()&os.ModeSymlink != 0) {
			fail(w, 409, errors.New("工作树缓存路径不可含符号链接或普通文件"))
			return
		}
		if e != nil && !os.IsNotExist(e) {
			fail(w, 500, e)
			return
		}
	}
	if err := os.MkdirAll(parent, 0700); err != nil {
		fail(w, 500, err)
		return
	}
	id := newID()
	path := filepath.Join(parent, id)
	if display == "" {
		display = "/workspace"
	}
	entry := ManagedWorktree{ID: id, Name: in.Name, WorkspaceID: workspaceID, BasePath: base, Path: path, WorkspacePath: strings.TrimRight(display, "/") + "/" + filepath.ToSlash(filepath.Join(rel, id)), BaseCommit: commit, State: "creating", Created: time.Now().UTC().Format(time.RFC3339Nano)}
	entries = append(entries, entry)
	if err := os.MkdirAll(filepath.Dir(a.managedWorktreesPath()), 0700); err != nil {
		fail(w, 500, err)
		return
	}
	if err := atomicJSON(a.managedWorktreesPath(), entries); err != nil {
		fail(w, 500, err)
		return
	}
	_, err = worktreeGit(ctx, base, "worktree", "add", "--detach", "--", path, commit)
	if err != nil {
		entry.State = "outcome_unknown"
		entry.Error = err.Error()
	} else {
		entry.State = "ready"
	}
	entries[len(entries)-1] = entry
	if saveErr := atomicJSON(a.managedWorktreesPath(), entries); saveErr != nil {
		fail(w, 500, fmt.Errorf("Git操作已尝试，登记结果保存失败；请先检查 %s: %w", path, saveErr))
		return
	}
	if err != nil {
		jsonOut(w, 409, map[string]any{"error": err.Error(), "worktree": entry})
		return
	}
	jsonOut(w, 200, map[string]any{"worktree": entry, "note": "以固定提交创建detached HEAD；不复制原工作区未提交修改。请切换工作区后创建新任务。"})
}
