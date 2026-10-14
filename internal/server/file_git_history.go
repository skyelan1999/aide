package server

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const gitHistoryLimit = 50
const gitHistoryTextLimit = 2 << 20

var gitHistoryObject = regexp.MustCompile(`^(?:[0-9a-f]{40}|[0-9a-f]{64})$`)

type gitHistoryVersion struct {
	ID      string `json:"id"`
	Short   string `json:"short"`
	Created string `json:"created"`
	Subject string `json:"subject"`
}
type gitHistorySide struct {
	Ref     string `json:"ref"`
	Exists  bool   `json:"exists"`
	Hash    string `json:"hash"`
	Content string `json:"content"`
}
type gitHistoryRepo struct{ bin, base, dir, path string }

// Only fixed read operations are used. No shell, hooks, textconv, external diff,
// inherited Git routing variables, optional index writes, or lazy network fetch.
func (g gitHistoryRepo) command(ctx context.Context, limit int64, args ...string) ([]byte, error) {
	fixed := []string{"--no-pager", "--literal-pathspecs", "-c", "safe.directory=" + g.dir, "-c", "core.fsmonitor=false", "-c", "core.hooksPath=/dev/null", "-C", g.dir}
	cmd := exec.CommandContext(ctx, g.bin, append(fixed, args...)...)
	for _, value := range os.Environ() {
		if !strings.HasPrefix(value, "GIT_") {
			cmd.Env = append(cmd.Env, value)
		}
	}
	cmd.Env = append(cmd.Env, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_TERMINAL_PROMPT=0", "GIT_OPTIONAL_LOCKS=0", "GIT_NO_LAZY_FETCH=1", "GIT_NO_REPLACE_OBJECTS=1", "GIT_ALLOW_PROTOCOL=", "LC_ALL=C")
	output := &knowledgeLimitWriter{max: limit, stop: func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
	}}
	cmd.Stdout = output
	cmd.WaitDelay = time.Second
	err := cmd.Run()
	if output.overflow {
		return nil, errors.New("Git 比较超过读取上限")
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	return output.buf.Bytes(), err
}
func gitHistoryInside(base, target string) bool {
	rel, err := filepath.Rel(base, target)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func (a *App) gitHistoryRoot(source, root string) (*os.Root, string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if source != "" {
		src, ok := a.findSource(source)
		if !ok || !src.Enabled {
			return nil, "", errors.New("来源不存在或已停用")
		}
		if src.Type != "local" && src.Type != "skill" {
			return nil, "此来源暂不支持 Git 比较；不会使用本机缓存代替远端仓库。", nil
		}
		r, err := a.localSourceRoot(src)
		return r, "", err
	}
	if root != "" && root != "workspace" {
		return nil, "此文件位置暂不支持 Git 比较。", nil
	}
	if a.wsConfig.Workspace.Mode == "ssh" {
		return nil, "SSH 工作区暂不支持 Git 比较；不会使用本机缓存代替远端仓库。", nil
	}
	r, err := os.OpenRoot(a.workspace.Name())
	return r, "", err
}
func findGitHistoryRepo(ctx context.Context, root *os.Root, p string) (*gitHistoryRepo, string, error) {
	bin, err := exec.LookPath("git")
	if err != nil {
		return nil, "运行环境未安装 Git。", nil
	}
	base, err := filepath.EvalSymlinks(root.Name())
	if err != nil {
		return nil, "", err
	}
	// A Git tree path and the actual file path must refer to the same location.
	current := base
	for _, part := range strings.Split(filepath.FromSlash(p), string(filepath.Separator)) {
		current = filepath.Join(current, part)
		info, e := os.Lstat(current)
		if errors.Is(e, os.ErrNotExist) {
			break
		}
		if e != nil {
			return nil, "", e
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil, "", errors.New("Git 比较不支持符号链接路径")
		}
	}
	dir := filepath.Dir(filepath.Join(base, filepath.FromSlash(p)))
	for gitHistoryInside(base, dir) {
		if _, e := os.Lstat(filepath.Join(dir, ".git")); e == nil {
			g := &gitHistoryRepo{bin: bin, base: base, dir: dir}
			relative, e := filepath.Rel(dir, filepath.Join(base, filepath.FromSlash(p)))
			if e != nil {
				return nil, "", e
			}
			g.path = filepath.ToSlash(relative)
			top, e := g.command(ctx, 8192, "rev-parse", "--show-toplevel")
			if e != nil {
				return nil, "无法读取此 Git 仓库。", nil
			}
			actual, e := filepath.EvalSymlinks(strings.TrimSpace(string(top)))
			if e != nil || actual != dir {
				return nil, "Git 工作树与当前目录不一致。", nil
			}
			return g, "", nil
		}
		if dir == base {
			break
		}
		dir = filepath.Dir(dir)
	}
	return nil, "当前目录不属于工作区内的 Git 仓库。", nil
}
func (g gitHistoryRepo) resolve(ctx context.Context, ref string) (string, error) {
	if ref != "HEAD" && !gitHistoryObject.MatchString(ref) {
		return "", errors.New("请选择 HEAD 或完整提交编号")
	}
	raw, err := g.command(ctx, 256, "rev-parse", "--verify", ref+"^{commit}")
	if err != nil {
		return "", errors.New("Git 提交不存在或不可读取")
	}
	oid := strings.TrimSpace(string(raw))
	if !gitHistoryObject.MatchString(oid) {
		return "", errors.New("Git 提交编号无效")
	}
	return oid, nil
}
func (g gitHistoryRepo) side(ctx context.Context, root *os.Root, p, ref string) (gitHistorySide, error) {
	result := gitHistorySide{Ref: ref}
	var raw []byte
	var err error
	if ref == "worktree" {
		file, openErr := root.Open(p)
		if errors.Is(openErr, os.ErrNotExist) {
			return result, nil
		}
		if openErr != nil {
			return result, openErr
		}
		defer file.Close()
		info, statErr := file.Stat()
		if statErr != nil {
			return result, statErr
		}
		if !info.Mode().IsRegular() {
			return result, errors.New("只支持普通文本文件")
		}
		raw, err = io.ReadAll(io.LimitReader(file, gitHistoryTextLimit+1))
	} else {
		var entry []byte
		if ref == "index" {
			entry, err = g.command(ctx, 16384, "ls-files", "--stage", "-z", "--", g.path)
		} else {
			oid, e := g.resolve(ctx, ref)
			if e != nil {
				return result, e
			}
			result.Ref = oid
			entry, err = g.command(ctx, 16384, "ls-tree", "-z", oid, "--", g.path)
		}
		if err != nil {
			return result, errors.New("无法读取 Git 文件条目")
		}
		if len(entry) == 0 {
			return result, nil
		}
		records := bytes.Split(bytes.TrimSuffix(entry, []byte{0}), []byte{0})
		if len(records) != 1 {
			return result, errors.New("文件存在未解决的 Git 冲突")
		}
		fields := strings.Fields(strings.SplitN(string(records[0]), "\t", 2)[0])
		if len(fields) != 3 {
			return result, errors.New("Git 文件条目无效")
		}
		if fields[0] != "100644" && fields[0] != "100755" {
			return result, errors.New("仅支持普通 Git 文本文件，不支持符号链接或子模块条目")
		}
		oid := fields[2]
		if ref == "index" {
			if fields[2] != "0" {
				return result, errors.New("文件存在未解决的 Git 冲突")
			}
			oid = fields[1]
		}
		if !gitHistoryObject.MatchString(oid) {
			return result, errors.New("Git 对象编号无效")
		}
		size, e := g.command(ctx, 64, "cat-file", "-s", oid)
		if e != nil {
			return result, errors.New("Git 文件对象不可读取")
		}
		var n int64
		if _, e = fmt.Sscan(string(size), &n); e != nil || n > gitHistoryTextLimit {
			return result, errors.New("Git 比较仅支持不超过 2 MiB 的文本版本")
		}
		raw, err = g.command(ctx, gitHistoryTextLimit, "cat-file", "blob", oid)
	}
	if err != nil {
		return result, err
	}
	if len(raw) > gitHistoryTextLimit {
		return result, errors.New("Git 比较仅支持不超过 2 MiB 的文本版本")
	}
	if err = validateTextContent(raw); err != nil {
		return result, err
	}
	result.Exists = true
	result.Hash = hash(raw)
	result.Content = string(raw)
	return result, nil
}
func (g gitHistoryRepo) compare(ctx context.Context, before, after gitHistorySide) (string, error) {
	dir, err := os.MkdirTemp("", "aide-git-compare-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(dir)
	left, right := filepath.Join(dir, "before"), filepath.Join(dir, "after")
	if err = os.WriteFile(left, []byte(before.Content), 0600); err != nil {
		return "", err
	}
	if err = os.WriteFile(right, []byte(after.Content), 0600); err != nil {
		return "", err
	}
	// Run outside the project so repository attributes cannot affect the patch.
	isolated := g
	isolated.dir = dir
	patch, err := isolated.command(ctx, 8<<20, "diff", "--no-index", "--no-ext-diff", "--no-textconv", "--no-color", "--unified=3", "--", left, right)
	if err != nil {
		var exit *exec.ExitError
		if !errors.As(err, &exit) || exit.ExitCode() != 1 {
			return "", errors.New("Git 差异生成失败或超过限制")
		}
	}
	lines := strings.Split(string(patch), "\n")
	if len(lines) > 3 && strings.HasPrefix(lines[0], "diff --git ") {
		lines[0] = "diff --git a/" + g.path + " b/" + g.path
		for i, line := range lines {
			if strings.HasPrefix(line, "--- ") {
				if before.Exists {
					lines[i] = "--- a/" + g.path
				} else {
					lines[i] = "--- /dev/null"
				}
			}
			if strings.HasPrefix(line, "+++ ") {
				if after.Exists {
					lines[i] = "+++ b/" + g.path
				} else {
					lines[i] = "+++ /dev/null"
				}
			}
		}
	}
	return strings.Join(lines, "\n"), nil
}
func (a *App) fileGitHistoryAPI(w http.ResponseWriter, r *http.Request) {
	a.filesMu.Lock()
	defer a.filesMu.Unlock()
	q := r.URL.Query()
	p, source := q.Get("path"), q.Get("source")
	if err := safePath(p); err != nil {
		fail(w, 400, err)
		return
	}
	if source == "" && q.Get("workspaceId") != "" && q.Get("workspaceId") != a.wsID() {
		fail(w, 409, errors.New("工作区已切换"))
		return
	}
	root, reason, err := a.gitHistoryRoot(source, q.Get("root"))
	if err != nil {
		fail(w, 400, err)
		return
	}
	if root == nil {
		jsonOut(w, 200, map[string]any{"available": false, "reason": reason})
		return
	}
	defer root.Close()
	ctx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
	defer cancel()
	repo, reason, err := findGitHistoryRepo(ctx, root, p)
	if err != nil {
		fail(w, 400, err)
		return
	}
	if repo == nil {
		jsonOut(w, 200, map[string]any{"available": false, "reason": reason})
		return
	}
	if q.Get("probe") == "1" {
		jsonOut(w, 200, map[string]any{"available": true})
		return
	}
	head, _ := repo.resolve(ctx, "HEAD")
	result := map[string]any{"available": true, "path": p, "head": head, "versions": []gitHistoryVersion{}, "limit": gitHistoryLimit, "truncated": false}
	if q.Get("from") != "" || q.Get("to") != "" {
		from, to := q.Get("from"), q.Get("to")
		if from == "" {
			from = "HEAD"
		}
		if to == "" {
			to = "worktree"
		}
		if from == "HEAD" && head != "" {
			from = head
		}
		if to == "HEAD" && head != "" {
			to = head
		}
		before, e := repo.side(ctx, root, p, from)
		if e != nil {
			fail(w, 400, e)
			return
		}
		after, e := repo.side(ctx, root, p, to)
		if e != nil {
			fail(w, 400, e)
			return
		}
		patch, e := repo.compare(ctx, before, after)
		if e != nil {
			fail(w, 400, e)
			return
		}
		result["before"], result["after"], result["diff"] = before, after, patch
		jsonOut(w, 200, result)
		return
	}
	if head != "" {
		raw, e := repo.command(ctx, 512<<10, "log", "-n51", "--format=%H%x00%h%x00%aI%x00%s%x00", head, "--", repo.path)
		if e != nil {
			fail(w, 400, errors.New("Git 文件历史读取失败"))
			return
		}
		fields := strings.Split(string(raw), "\x00")
		versions := []gitHistoryVersion{}
		for i := 0; i+3 < len(fields); i += 4 {
			versions = append(versions, gitHistoryVersion{ID: strings.TrimSpace(fields[i]), Short: fields[i+1], Created: fields[i+2], Subject: fields[i+3]})
		}
		if len(versions) > gitHistoryLimit {
			versions = versions[:gitHistoryLimit]
			result["truncated"] = true
		}
		result["versions"] = versions
	}
	jsonOut(w, 200, result)
}
