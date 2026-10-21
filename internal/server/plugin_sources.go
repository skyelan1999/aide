package server

// Per-call, credential-free file-source capability for trusted plugin handlers.
// The socket and snapshots live only as long as the admitted tool invocation.
import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

type pluginSourceRequest struct {
	Operation string   `json:"operation"`
	Source    string   `json:"source"`
	Path      string   `json:"path"`
	Slot      string   `json:"slot"`
	Recursive bool     `json:"recursive"`
	MaxBytes  int64    `json:"maxBytes"`
	MaxFiles  int      `json:"maxFiles"`
	MaxDepth  int      `json:"maxDepth"`
	Exclude   []string `json:"exclude"`
}

var pluginSourceProtected = []string{".git", ".data", ".aide-data", ".env", "docker-images"}
var pluginSourceLongEntry = regexp.MustCompile(`^([bcdlps-][^ ]*)\s+\S+\s+\S+\s+\S+\s+\d+\s+\S+\s+\S+\s+\S+\s(.+)$`)

// Comparison listings include dotfiles and fail on unparseable entries instead
// of silently claiming equality for a partially discovered directory.
func (a *App) pluginSourceRemoteList(ctx context.Context, ar knowledgeArea, p string) ([]map[string]any, error) {
	if !knowledgeSafePath(p) || !a.knowledgeAreaCurrent(ar) {
		return nil, errors.New("来源路径或配置已变化")
	}
	base := knowledgeRemoteBase(ar)
	if strings.ContainsAny(base, "\x00\r\n\t\\") {
		return nil, errors.New("远端根路径无效")
	}
	out, e := a.knowledgeSFTP(ctx, ar, "cd "+shellQuoteRemote(pathJoinRemote(base, p))+"\nls -la\n", 256<<10)
	if e != nil {
		return nil, e
	}
	rows := []map[string]any{}
	for _, line := range strings.Split(string(out), "\n") {
		if strings.TrimSpace(line) == "" || strings.HasPrefix(line, "sftp>") || strings.HasPrefix(line, "total ") {
			continue
		}
		match := pluginSourceLongEntry.FindStringSubmatch(line)
		if match == nil {
			return nil, errors.New("远端目录包含无法无损解析的条目；请缩小比较范围")
		}
		name := match[2]
		link := match[1][0] == 'l'
		if link {
			if i := strings.Index(name, " -> "); i >= 0 {
				name = name[:i]
			}
		}
		name = decodeSFTPName(name)
		if name == "." || name == ".." {
			continue
		}
		if name == "" || path.Base(name) != name || strings.ContainsAny(name, "\x00\r\n\t\\") {
			return nil, errors.New("不支持的远端文件名")
		}
		rows = append(rows, map[string]any{"name": name, "path": path.Join(p, name), "dir": match[1][0] == 'd', "symlink": link, "special": !strings.ContainsRune("d-l", rune(match[1][0]))})
		if len(rows) > 5000 {
			return nil, errors.New("远端目录超过 5000 条目")
		}
	}
	return rows, nil
}

func (a *App) pluginSourceAreasLocked() []knowledgeArea {
	var work, ref string
	if a.workspace != nil {
		work = a.workspace.Name()
	}
	if a.reference != nil {
		ref = a.reference.Name()
	}
	all := a.knowledgeAreasLocked(a.wsID(), work, ref)
	areas := []knowledgeArea{}
	for _, ar := range all {
		if ar.root == "context" && ar.source == "" {
			src, ok := a.findSource(contextSource)
			if !ok || !sourceAIVisible(src, a.wsID()) {
				continue
			}
		}
		if !ar.enabled || ar.source != "" && !sourceAIVisible(ar.src, a.wsID()) {
			continue
		}
		switch ar.kind {
		case "local", "skill", "ssh", "sftp", "workspace-sftp":
			areas = append(areas, ar)
		}
	}
	return areas
}

func pluginSourceID(ar knowledgeArea) string {
	if ar.source != "" {
		return "source:" + ar.source
	}
	return ar.root
}
func pluginSourceLabel(ar knowledgeArea) map[string]any {
	label := map[string]any{"id": pluginSourceID(ar), "name": ar.name, "type": ar.kind, "origin": ar.origin}
	if ar.root == "workspace" && ar.source == "" {
		// Durable cache identity excludes per-process origin, lease epoch and secrets.
		w := ar.ws.Workspace
		identity, _ := json.Marshal([]any{ar.scope, ar.kind, ar.location, w.Mode, w.Path, w.Host, w.Port, w.Username})
		label["workspaceKey"] = "W" + hash(identity)[:32]
	}
	return label
}

func (a *App) startPluginSources(ctx context.Context) (string, func(), func() bool, error) {
	ctx, cancelSources := context.WithCancel(ctx)
	dir, err := os.MkdirTemp("", "aide-plugin-sources-*")
	if err != nil {
		cancelSources()
		return "", nil, nil, err
	}
	cleanup := func() { cancelSources(); _ = os.RemoveAll(dir) }
	listener, err := net.Listen("unix", filepath.Join(dir, "source.sock"))
	if err != nil {
		cleanup()
		return "", nil, nil, err
	}
	a.mu.Lock()
	areas := a.pluginSourceAreasLocked()
	a.mu.Unlock()
	var mu sync.Mutex
	var total int64
	used := map[string]bool{}
	server := &http.Server{ReadHeaderTimeout: 5 * time.Second}
	server.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if r.Method != "POST" || r.URL.Path != "/" {
			fail(w, 400, errors.New("invalid source request"))
			return
		}
		var in pluginSourceRequest
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&in) != nil {
			fail(w, 400, errors.New("invalid source request"))
			return
		}
		callCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
		defer cancel()
		result, e := a.pluginSourceRequest(callCtx, areas, dir, &total, used, in)
		if e != nil {
			fail(w, 400, e)
			return
		}
		jsonOut(w, 200, result)
	})
	go func() { _ = server.Serve(listener) }()
	validate := func() bool {
		a.mu.Lock()
		now := a.pluginSourceAreasLocked()
		a.mu.Unlock()
		if len(now) != len(areas) {
			return false
		}
		for i, ar := range areas {
			if ar.origin != now[i].origin {
				return false
			}
		}
		return true
	}
	return filepath.Join(dir, "source.sock"), func() { cancelSources(); _ = server.Close(); mu.Lock(); defer mu.Unlock(); cleanup() }, validate, nil
}

func (a *App) pluginSourceRequest(ctx context.Context, areas []knowledgeArea, temp string, total *int64, used map[string]bool, in pluginSourceRequest) (any, error) {
	// Revalidate all pinned sources, including AI visibility, before and after I/O.
	current := func(ar knowledgeArea) bool {
		a.mu.Lock()
		now := a.pluginSourceAreasLocked()
		a.mu.Unlock()
		for _, n := range now {
			if pluginSourceID(n) == pluginSourceID(ar) && n.origin == ar.origin {
				return true
			}
		}
		return false
	}
	if in.Operation == "sources" {
		items := []map[string]any{}
		for _, ar := range areas {
			if current(ar) {
				items = append(items, pluginSourceLabel(ar))
			}
		}
		return map[string]any{"sources": items}, nil
	}
	if in.Source == "" {
		in.Source = "workspace"
	}
	var ar knowledgeArea
	found := false
	for _, candidate := range areas {
		if pluginSourceID(candidate) == in.Source {
			ar = candidate
			found = true
			break
		}
	}
	if !found || !current(ar) {
		return nil, errors.New("来源不存在、不可供 AI 使用或配置已变化")
	}
	if in.Path == "" {
		in.Path = "."
	}
	if !knowledgeSafePath(in.Path) || path.Clean(in.Path) != in.Path {
		return nil, errors.New("需要来源内规范相对路径")
	}
	list := func(p string) ([]map[string]any, error) {
		if !current(ar) {
			return nil, errors.New("来源配置已变化")
		}
		if ar.local() {
			root, e := a.openLocalSourceRoot(ctx, ar.location)
			if e != nil {
				return nil, e
			}
			defer root.Close()
			// Reject symlink ancestors, even when they remain inside the root.
			parts := []string{}
			for _, part := range splitSourcePath(p) {
				parts = append(parts, part)
				info, e := root.Lstat(path.Join(parts...))
				if e != nil {
					return nil, e
				}
				if info.Mode()&os.ModeSymlink != 0 {
					return nil, errors.New("不跟随符号链接")
				}
			}
			f, e := root.Open(p)
			if e != nil {
				return nil, e
			}
			defer f.Close()
			items, e := f.ReadDir(5001)
			if e != nil && e != io.EOF {
				return nil, e
			}
			if len(items) > 5000 {
				return nil, errors.New("目录列表超过 5000 条目，请缩小范围")
			}
			rows := []map[string]any{}
			for _, item := range items {
				rows = append(rows, map[string]any{"name": item.Name(), "path": path.Join(p, item.Name()), "dir": item.IsDir(), "symlink": item.Type()&os.ModeSymlink != 0, "special": !item.IsDir() && !item.Type().IsRegular() && item.Type()&os.ModeSymlink == 0})
			}
			return rows, nil
		}
		parent := "."
		for _, part := range splitSourcePath(p) {
			rows, e := a.pluginSourceRemoteList(ctx, ar, parent)
			if e != nil {
				return nil, e
			}
			found := false
			for _, row := range rows {
				if row["name"] == part {
					if row["symlink"] == true || row["dir"] != true {
						return nil, errors.New("不跟随目录符号链接")
					}
					found = true
					break
				}
			}
			if !found {
				return nil, os.ErrNotExist
			}
			parent = path.Join(parent, part)
		}
		return a.pluginSourceRemoteList(ctx, ar, p)
	}
	if in.Operation == "localRoot" {
		if !ar.local() {
			return nil, errors.New("该来源不是本地 Git 工作目录；请使用文件或目录比较")
		}
		return map[string]any{"workspace": ar.location, "source": pluginSourceLabel(ar)}, nil
	}
	if in.Operation == "browse" {
		rows, e := list(in.Path)
		if e != nil {
			return nil, e
		}
		if !current(ar) {
			return nil, errors.New("来源配置已变化")
		}
		entries := []map[string]any{}
		truncated := len(rows) > 5000
		for i, row := range rows {
			if i >= 5000 {
				break
			}
			kind := "file"
			if row["dir"] == true {
				kind = "directory"
			}
			if row["symlink"] == true {
				kind = "link"
			}
			if row["special"] == true {
				kind = "special"
			}
			entries = append(entries, map[string]any{"path": row["path"], "kind": kind, "bytes": row["size"]})
		}
		return map[string]any{"path": in.Path, "entries": entries, "truncated": truncated, "source": pluginSourceLabel(ar)}, nil
	}
	if in.Operation != "stage" || in.Slot != "left" && in.Slot != "right" && in.Slot != "path" || used[in.Slot] {
		return nil, errors.New("invalid or repeated source snapshot slot")
	}
	used[in.Slot] = true
	maxBytes := in.MaxBytes
	if maxBytes <= 0 || maxBytes > 64<<20 {
		maxBytes = 64 << 20
	}
	maxFiles := in.MaxFiles
	if maxFiles <= 0 || maxFiles > 5000 {
		maxFiles = 5000
	}
	maxDepth := in.MaxDepth
	if maxDepth <= 0 || maxDepth > 32 {
		maxDepth = 32
	}
	if len(in.Exclude) > 64 {
		return nil, errors.New("too many exclusion patterns")
	}
	for _, pattern := range in.Exclude {
		if _, e := path.Match(pattern, ""); e != nil {
			return nil, e
		}
	}
	base := path.Join(in.Slot, path.Base(in.Path))
	if in.Path == "." {
		base = in.Slot
	}
	files, entries := 0, 0
	var bytesRead int64
	copyFile := func(p, dest string) error {
		remaining := maxBytes - bytesRead
		if remaining > 64<<20-*total {
			remaining = 64<<20 - *total
		}
		if remaining > 8<<20 {
			remaining = 8 << 20
		}
		if remaining <= 0 {
			return errors.New("来源快照超过字节预算")
		}
		read := func() ([]byte, error) {
			if ar.local() {
				return a.knowledgeAreaRead(ctx, ar, p, remaining)
			}
			if !knowledgeSafePath(p) {
				return nil, errors.New("来源路径不可访问")
			}
			parent := "."
			parts := splitSourcePath(p)
			for i, name := range parts {
				rows, e := list(parent)
				if e != nil {
					return nil, e
				}
				found := false
				for _, row := range rows {
					if row["name"] == name {
						if row["symlink"] == true || row["special"] == true || row["dir"] != (i < len(parts)-1) {
							return nil, errors.New("来源文件不是普通文件或含符号链接")
						}
						found = true
						break
					}
				}
				if !found {
					return nil, os.ErrNotExist
				}
				parent = path.Join(parent, name)
			}
			f, e := os.CreateTemp("", "aide-plugin-source-read-*")
			if e != nil {
				return nil, e
			}
			filename := f.Name()
			f.Close()
			defer os.Remove(filename)
			_, e = a.knowledgeSFTP(ctx, ar, "get "+shellQuoteRemote(pathJoinRemote(knowledgeRemoteBase(ar), p))+" "+shellQuoteRemote(filename)+"\n", remaining)
			if e != nil {
				return nil, e
			}
			f, e = os.Open(filename)
			if e != nil {
				return nil, e
			}
			defer f.Close()
			b, e := io.ReadAll(io.LimitReader(f, remaining+1))
			if int64(len(b)) > remaining {
				return nil, errors.New("来源文件超过字节预算")
			}
			return b, e
		}
		b, e := read()
		if e != nil {
			return e
		}
		if !current(ar) {
			return errors.New("来源配置已变化")
		}
		files++
		if files > maxFiles {
			return errors.New("来源快照超过文件预算")
		}
		bytesRead += int64(len(b))
		*total += int64(len(b))
		if e = os.MkdirAll(filepath.Dir(filepath.Join(temp, dest)), 0700); e != nil {
			return e
		}
		if e = os.WriteFile(filepath.Join(temp, dest), b, 0600); e != nil {
			return e
		}
		// Stage times are fixed, so repeat comparisons have stable identities.
		return os.Chtimes(filepath.Join(temp, dest), time.Unix(0, 0), time.Unix(0, 0))
	}
	var walk func(string, string, int) error
	walk = func(p, dest string, depth int) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if depth > maxDepth {
			return errors.New("来源快照超过目录深度预算")
		}
		if in.Recursive {
			if err := os.MkdirAll(filepath.Join(temp, dest), 0700); err != nil {
				return err
			}
			rows, e := list(p)
			if e != nil {
				return e
			}
			sort.Slice(rows, func(i, j int) bool { return fmt.Sprint(rows[i]["name"]) < fmt.Sprint(rows[j]["name"]) })
			for _, row := range rows {
				name, ok := row["name"].(string)
				if !ok || name == "." || name == ".." || path.Base(name) != name || strings.ContainsAny(name, "\\\x00\r\n\t") {
					return errors.New("来源返回无效文件名")
				}
				// Match native directory exclusions before reading file bytes.
				skip := false
				for _, protected := range pluginSourceProtected {
					if name == protected {
						skip = true
					}
				}
				rel := path.Join(p, name)
				patterns := in.Exclude
				if patterns == nil {
					patterns = []string{".git"}
				}
				relative := strings.TrimPrefix(rel, in.Path+"/")
				if in.Path == "." {
					relative = rel
				}
				for _, pattern := range patterns {
					candidate := relative
					if !strings.Contains(pattern, "/") {
						candidate = name
					}
					m, _ := path.Match(pattern, candidate)
					prefix := strings.TrimSuffix(pattern, "/**")
					skip = skip || m || (prefix != pattern && (relative == prefix || strings.HasPrefix(relative, prefix+"/")))
				}
				if skip {
					continue
				}
				entries++
				if entries > maxFiles {
					return errors.New("来源快照超过条目预算；请缩小目录范围")
				}
				if row["symlink"] == true || row["special"] == true {
					return errors.New("快照包含符号链接；请排除后重试")
				}
				if row["dir"] == true {
					if e := walk(rel, path.Join(dest, name), depth+1); e != nil {
						return e
					}
				} else {
					if e := copyFile(rel, path.Join(dest, name)); e != nil {
						return e
					}
				}
			}
			return nil
		}
		return copyFile(p, dest)
	}
	if e := walk(in.Path, base, 0); e != nil {
		return nil, e
	}
	if !current(ar) {
		return nil, errors.New("来源配置已变化")
	}
	label := pluginSourceLabel(ar)
	label["path"] = in.Path
	label["snapshotPath"] = base
	label["bytes"] = bytesRead
	label["files"] = files
	label["originalTimestamps"] = false
	label["excludedByHostPolicy"] = pluginSourceProtected
	return map[string]any{"workspace": temp, "path": base, "source": label}, nil
}

func splitSourcePath(p string) []string {
	if p == "." {
		return nil
	}
	return strings.Split(p, "/")
}
