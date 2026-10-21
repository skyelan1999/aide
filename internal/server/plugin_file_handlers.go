package server

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path"
	"regexp"
	"sort"
	"strings"
)

type PluginFileHandler struct {
	Extensions []string `json:"extensions"`
	Priority   int      `json:"priority,omitempty"`
	Editable   bool     `json:"editable,omitempty"`
}
type pluginDocumentSelection struct {
	Path               string `json:"path"`
	Root               string `json:"root"`
	Source             string `json:"source,omitempty"`
	ReadOnly           bool   `json:"readOnly,omitempty"`
	Origin             string `json:"origin,omitempty"`
	KnowledgeWorkspace string `json:"knowledgeWorkspace,omitempty"`
}
type pluginDocumentBinding struct {
	Selection pluginDocumentSelection
	Identity  string
	Editable  bool
	Stage     *pluginDocumentStage
}

var pluginFileExtension = regexp.MustCompile(`^\.[a-z0-9][a-z0-9_+.-]{0,23}$`)

func validatePluginFileHandler(h *PluginFileHandler) error {
	if h == nil {
		return nil
	}
	if len(h.Extensions) == 0 || len(h.Extensions) > 32 || h.Priority < -100 || h.Priority > 100 {
		return errors.New("invalid file handler declaration")
	}
	seen := map[string]bool{}
	for _, ext := range h.Extensions {
		if !pluginFileExtension.MatchString(ext) || strings.Contains(ext, "..") || seen[ext] {
			return errors.New("file extensions must be unique lowercase suffixes such as .stl")
		}
		seen[ext] = true
	}
	return nil
}
func pluginFileMatches(p PluginManifest, name string) bool {
	if !p.Enabled || p.Workbench == nil || p.Workbench.FileHandler == nil || validatePluginWorkbench(p.Workbench) != nil {
		return false
	}
	name = strings.ToLower(name)
	if strings.HasSuffix(name, ".bak") {
		name = strings.TrimSuffix(name, ".bak")
	}
	for _, ext := range p.Workbench.FileHandler.Extensions {
		if strings.HasSuffix(path.Base(name), ext) {
			return true
		}
	}
	return false
}
func (a *App) pluginDocumentIdentity(s pluginDocumentSelection, write bool) (string, error) {
	if err := validTransferPath(s.Path); err != nil {
		return "", err
	}
	if s.Origin != "" || s.KnowledgeWorkspace != "" {
		return "", errors.New("knowledge snapshots use the native read-only viewer")
	}
	if s.Source != "" {
		return a.transferBinding(s.Source, write)
	}
	if s.Root == "workspace" {
		return a.transferBinding("workspace", write)
	}
	if s.Root == "context" && !write {
		a.mu.Lock()
		defer a.mu.Unlock()
		return hash([]byte("context:" + a.reference.Name())), nil
	}
	return "", errors.New("file root is read-only or unsupported")
}
func (a *App) pluginDocumentFS(ctx context.Context, s pluginDocumentSelection, write bool) (*transferFS, error) {
	if s.Source != "" {
		return a.streamTransferFS(ctx, s.Source, write)
	}
	if s.Root == "workspace" {
		return a.streamTransferFS(ctx, "workspace", write)
	}
	if s.Root == "context" && !write {
		a.mu.Lock()
		name := a.reference.Name()
		a.mu.Unlock()
		root, e := os.OpenRoot(name)
		if e != nil {
			return nil, e
		}
		return &transferFS{root: root}, nil
	}
	return nil, errors.New("unsupported file root")
}
func (a *App) bindPluginDocument(ctx context.Context, id string, s pluginDocumentSelection) (*pluginDocumentBinding, error) {
	a.mu.Lock()
	var spec *PluginFileHandler
	for _, p := range a.pluginRegistry.Plugins {
		if p.ID == id && pluginFileMatches(p, s.Path) {
			spec = p.Workbench.FileHandler
			break
		}
	}
	a.mu.Unlock()
	if spec == nil {
		return nil, errors.New("plugin does not handle this file extension")
	}
	identity, e := a.pluginDocumentIdentity(s, false)
	if e != nil {
		return nil, e
	}
	fs, e := a.pluginDocumentFS(ctx, s, false)
	if e != nil {
		return nil, e
	}
	defer fs.Close()
	if e = pluginDocumentSafePath(fs, s.Path); e != nil {
		return nil, e
	}
	info, e := fs.Stat(s.Path)
	if e != nil {
		return nil, e
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("only regular files can be opened")
	}
	writable := false
	if spec.Editable && !s.ReadOnly && !strings.HasSuffix(strings.ToLower(s.Path), ".bak") {
		_, e = a.pluginDocumentIdentity(s, true)
		writable = e == nil
	}
	return &pluginDocumentBinding{Selection: s, Identity: identity, Editable: writable}, nil
}
func (a *App) listPluginFileHandlers(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	s := pluginDocumentSelection{Path: q.Get("path"), Root: q.Get("root"), Source: q.Get("source"), ReadOnly: q.Get("readOnly") == "true", Origin: q.Get("origin"), KnowledgeWorkspace: q.Get("knowledgeWorkspace")}
	if s.Root == "" {
		s.Root = "workspace"
	}
	a.mu.Lock()
	candidates := []PluginManifest{}
	for _, p := range a.pluginRegistry.Plugins {
		if pluginFileMatches(p, s.Path) {
			candidates = append(candidates, p)
		}
	}
	a.mu.Unlock()
	out := []PluginManifest{}
	for _, p := range candidates {
		if _, e := a.bindPluginDocument(r.Context(), p.ID, s); e == nil {
			a.mu.Lock()
			_, e = a.workbenchPageLocked(p.ID)
			a.mu.Unlock()
			if e == nil {
				out = append(out, PluginManifest{ID: p.ID, Name: p.Name, Enabled: true, Version: p.Version, Workbench: p.Workbench})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Workbench.FileHandler.Priority != b.Workbench.FileHandler.Priority {
			return a.Workbench.FileHandler.Priority > b.Workbench.FileHandler.Priority
		}
		return a.ID < b.ID
	})
	jsonOut(w, 200, map[string]any{"handlers": out})
}

// No symlinks in the selected path or its assets, including intermediate
// directories. os.Root contains traversal; this check also preserves the
// selected file's smaller asset-directory boundary.
func pluginDocumentSafePath(fs *transferFS, p string) error {
	if err := validTransferPath(p); err != nil {
		return err
	}
	cur := ""
	for _, part := range strings.Split(p, "/") {
		cur = path.Join(cur, part)
		info, e := fs.Stat(cur)
		if e != nil {
			return e
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return errors.New("plugin document paths cannot follow symlinks")
		}
	}
	return nil
}
