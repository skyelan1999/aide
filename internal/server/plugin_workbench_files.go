package server

// Credential-free manual file selection for explicitly opted-in workbenches.
// This channel does not expose a model tool or permit the iframe to commit saves.
import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path"
	"sort"
	"strings"
	"time"
)

type PluginWorkbenchFileAccess struct {
	Protocol   int      `json:"protocol"`
	Extensions []string `json:"extensions"`
}

func validateWorkbenchFileAccess(f *PluginWorkbenchFileAccess) error {
	if f == nil {
		return nil
	}
	if f.Protocol != 1 {
		return errors.New("unsupported workbench file access protocol")
	}
	return validatePluginFileHandler(&PluginFileHandler{Extensions: f.Extensions})
}

func workbenchFileAccessMatches(f *PluginWorkbenchFileAccess, name string) bool {
	if f == nil || validateWorkbenchFileAccess(f) != nil {
		return false
	}
	for _, ext := range f.Extensions {
		if strings.HasSuffix(strings.ToLower(path.Base(name)), ext) {
			return true
		}
	}
	return false
}

func workbenchFileSelection(source, name string) (pluginDocumentSelection, error) {
	s := pluginDocumentSelection{Root: "workspace", Path: name}
	if source == "workspace" {
		return s, nil
	}
	if !strings.HasPrefix(source, "source:") || len(source) <= 7 {
		return s, errors.New("invalid file source")
	}
	s.Root, s.Source = "context", strings.TrimPrefix(source, "source:")
	return s, nil
}

func (a *App) workbenchFileSourceCurrent(source string) bool {
	if source == "workspace" {
		return true
	}
	if !strings.HasPrefix(source, "source:") {
		return false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	s, ok := a.findSource(strings.TrimPrefix(source, "source:"))
	return ok && s.Enabled && (strings.TrimSpace(s.Config.Path) != "" || s.ID == contextSource && s.Builtin) && (s.Type == "local" || s.Type == "skill" || s.Type == "sftp" || s.Type == "workspace-sftp")
}

func pluginDocumentTargetSafePath(fs *transferFS, name string, missing bool) error {
	if !missing {
		return pluginDocumentSafePath(fs, name)
	}
	if err := validTransferPath(name); err != nil || name == "." {
		return errors.New("invalid document target")
	}
	if parent := path.Dir(name); parent != "." {
		if err := pluginDocumentSafePath(fs, parent); err != nil {
			return err
		}
		info, err := fs.Stat(parent)
		if err != nil || !info.IsDir() {
			return errors.New("target parent is not a directory")
		}
	}
	_, err := fs.Stat(name)
	if errors.Is(err, os.ErrNotExist) || isSFTPNotExistErr(err) {
		return nil
	}
	if err != nil {
		return err
	}
	return errors.New("target already exists; select it again before replacing")
}

func pluginDocumentTargetVersion(ctx context.Context, fs *transferFS, name string, missing bool) (string, error) {
	if missing {
		return "", pluginDocumentTargetSafePath(fs, name, true)
	}
	return pluginDocumentVersion(ctx, fs, name)
}

func (a *App) callWorkbenchFiles(w http.ResponseWriter, r *http.Request, req workbenchBridgeRequest) {
	var args struct {
		Action  string  `json:"action"`
		Source  string  `json:"source,omitempty"`
		Path    string  `json:"path,omitempty"`
		Mode    string  `json:"mode,omitempty"`
		Offset  int     `json:"offset,omitempty"`
		Limit   int     `json:"limit,omitempty"`
		Query   string  `json:"query,omitempty"`
		Version *string `json:"version,omitempty"`
	}
	raw, _ := json.Marshal(req.Arguments)
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&args) != nil || (args.Action != "sources" && args.Action != "browse" && args.Action != "bind" && args.Action != "target" && args.Action != "clear") {
		fail(w, 400, errors.New("invalid file selection request"))
		return
	}
	id, key := r.PathValue("id"), r.PathValue("instance")
	a.mu.Lock()
	b, err := a.workbenchBridgeLocked(id, key, true)
	var access *PluginWorkbenchFileAccess
	for _, p := range a.pluginRegistry.Plugins {
		if p.ID == id && p.Workbench != nil {
			access = p.Workbench.FileAccess
		}
	}
	if err == nil && (access == nil || validateWorkbenchFileAccess(access) != nil) {
		err = errors.New("workbench file access not declared")
	}
	if err == nil && (b.Busy || b.Seen[req.RequestID] || len(b.Seen) >= 128) {
		err = errors.New("file selection busy or replayed")
	}
	if err == nil && (args.Action == "bind" || args.Action == "clear") && b.Document != nil && b.Document.Stage != nil {
		err = errors.New("save or discard the pending document draft first")
	}
	if err != nil {
		a.mu.Unlock()
		fail(w, 403, err)
		return
	}
	b.Busy = true
	b.Seen[req.RequestID] = true
	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	b.Cancel = cancel
	a.mu.Unlock()
	defer func() {
		cancel()
		a.mu.Lock()
		if a.pluginWorkbenchBridges[key] == b {
			b.Busy = false
			b.Cancel = nil
		}
		a.mu.Unlock()
	}()
	admit := func() error {
		a.mu.Lock()
		defer a.mu.Unlock()
		live, err := a.workbenchBridgeLocked(id, key, true)
		if err != nil {
			return err
		}
		if live != b {
			return errors.New("workbench file scope changed")
		}
		return ctx.Err()
	}
	result := map[string]any{}
	if args.Action == "clear" {
		a.mu.Lock()
		disposePluginDocument(b.Document)
		b.Document = nil
		a.mu.Unlock()
		result["cleared"] = true
	} else if args.Action == "sources" {
		rows := []map[string]any{{"id": "workspace", "name": "工作区", "type": "workspace"}}
		a.mu.Lock()
		for _, s := range a.sourceRegistry.Sources {
			if s.Enabled && (strings.TrimSpace(s.Config.Path) != "" || s.ID == contextSource && s.Builtin) && (s.Type == "local" || s.Type == "skill" || s.Type == "sftp" || s.Type == "workspace-sftp") {
				rows = append(rows, map[string]any{"id": "source:" + s.ID, "name": s.Name, "type": s.Type})
			}
		}
		a.mu.Unlock()
		visible := []map[string]any{}
		for _, row := range rows {
			s, _ := workbenchFileSelection(row["id"].(string), "file.pr")
			if _, e := a.pluginDocumentIdentity(s, false); e != nil {
				continue
			}
			_, e := a.pluginDocumentIdentity(s, true)
			row["writable"] = e == nil
			visible = append(visible, row)
		}
		result["sources"] = visible
	} else {
		if args.Path == "" {
			args.Path = "."
		}
		if len(args.Path) > 2048 || (args.Path != "." && (validTransferPath(args.Path) != nil || path.Clean(args.Path) != args.Path)) || !a.workbenchFileSourceCurrent(args.Source) {
			fail(w, 400, errors.New("invalid or disabled file source/path"))
			return
		}
		s, e := workbenchFileSelection(args.Source, args.Path)
		if e != nil {
			fail(w, 400, e)
			return
		}
		if s.Path == "." {
			s.Path = "source.pr"
		}
		identity, e := a.pluginDocumentIdentity(s, false)
		if e != nil {
			fail(w, 403, e)
			return
		}
		current := func() error {
			if e := admit(); e != nil {
				return e
			}
			if !a.workbenchFileSourceCurrent(args.Source) {
				return errors.New("file source disabled")
			}
			v, e := a.pluginDocumentIdentity(s, false)
			if e != nil {
				return e
			}
			if v != identity {
				return errors.New("file source configuration changed")
			}
			return nil
		}
		a.filesMu.Lock()
		defer a.filesMu.Unlock()
		fs, e := a.pluginDocumentFS(ctx, s, false)
		if e != nil {
			fail(w, 403, e)
			return
		}
		defer fs.Close()
		if args.Action == "browse" {
			if args.Offset < 0 || args.Offset > 5000 || args.Limit < 0 || args.Limit > 500 || len(args.Query) > 120 {
				fail(w, 400, errors.New("invalid directory page"))
				return
			}
			if args.Path != "." {
				if e = pluginDocumentSafePath(fs, args.Path); e != nil {
					fail(w, 403, e)
					return
				}
			}
			rows := []map[string]any{}
			scanned := 0
			e = fs.readDirEach(ctx, args.Path, func(info os.FileInfo) error {
				scanned++
				if scanned > 5000 {
					return errors.New("directory exceeds 5000 entries; choose a smaller directory")
				}
				name := info.Name()
				if strings.HasPrefix(name, ".") || info.Mode()&os.ModeSymlink != 0 || (!info.IsDir() && !info.Mode().IsRegular()) {
					return nil
				}
				if !info.IsDir() && !workbenchFileAccessMatches(access, name) {
					return nil
				}
				if args.Query != "" && !strings.Contains(strings.ToLower(name), strings.ToLower(args.Query)) {
					return nil
				}
				rows = append(rows, map[string]any{"name": name, "path": path.Join(args.Path, name), "directory": info.IsDir(), "size": info.Size()})
				return nil
			})
			if e != nil {
				fail(w, 400, e)
				return
			}
			sort.Slice(rows, func(i, j int) bool {
				di, dj := rows[i]["directory"].(bool), rows[j]["directory"].(bool)
				if di != dj {
					return di
				}
				return rows[i]["name"].(string) < rows[j]["name"].(string)
			})
			limit := args.Limit
			if limit == 0 {
				limit = 200
			}
			start := args.Offset
			if start > len(rows) {
				start = len(rows)
			}
			end := start + limit
			if end > len(rows) {
				end = len(rows)
			}
			result = map[string]any{"entries": rows[start:end], "total": len(rows), "offset": start, "nextOffset": end, "hasMore": end < len(rows), "source": args.Source, "path": args.Path}
		} else {
			if (args.Mode != "open" && args.Mode != "save") || args.Path == "." || !workbenchFileAccessMatches(access, args.Path) {
				fail(w, 400, errors.New("file extension or selection mode not declared"))
				return
			}
			_, writeErr := a.pluginDocumentIdentity(s, true)
			editable := writeErr == nil
			if args.Mode == "save" && !editable {
				fail(w, 403, errors.New("selected source is read-only"))
				return
			}
			info, statErr := fs.Stat(args.Path)
			missing := errors.Is(statErr, os.ErrNotExist) || isSFTPNotExistErr(statErr)
			if missing && args.Mode != "save" {
				fail(w, 404, errors.New("selected file does not exist"))
				return
			}
			if !missing && (statErr != nil || !info.Mode().IsRegular()) {
				fail(w, 400, errors.New("selected file is not a regular document"))
				return
			}
			if e = pluginDocumentTargetSafePath(fs, args.Path, missing); e != nil {
				fail(w, 403, e)
				return
			}
			version, e := pluginDocumentTargetVersion(ctx, fs, args.Path, missing)
			if e != nil {
				fail(w, 400, e)
				return
			}
			if args.Action == "bind" && ((args.Mode == "save" && args.Version == nil) || (args.Version != nil && *args.Version != version)) {
				fail(w, 409, errors.New("file target changed since selection; select it again"))
				return
			}
			if e = current(); e != nil {
				fail(w, 409, e)
				return
			}
			size := int64(0)
			if info != nil {
				size = info.Size()
			}
			if args.Action == "bind" {
				a.mu.Lock()
				live, e := a.workbenchBridgeLocked(id, key, true)
				if e == nil && live == b {
					disposePluginDocument(b.Document)
					b.Document = &pluginDocumentBinding{Selection: s, Identity: identity, Editable: editable, Missing: missing, FileAccess: true}
				} else if e == nil {
					e = errors.New("file scope changed")
				}
				a.mu.Unlock()
				if e != nil {
					fail(w, 409, e)
					return
				}
			}
			result = map[string]any{"path": args.Path, "source": args.Source, "editable": editable, "exists": !missing, "size": size, "version": version, "chunkSize": pluginDocumentChunk}
		}
		if e = current(); e != nil {
			fail(w, 409, e)
			return
		}
	}
	if err = admit(); err != nil {
		fail(w, 409, err)
		return
	}
	jsonOut(w, 200, map[string]any{"requestId": req.RequestID, "status": "result", "result": result})
}

func pluginDocumentSourceID(s pluginDocumentSelection) string {
	if s.Source != "" {
		return "source:" + s.Source
	}
	if s.Root == "workspace" {
		return "workspace"
	}
	return "source:context"
}
