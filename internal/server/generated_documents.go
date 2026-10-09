package server

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// The binding, rather than the reference registry's empty-path fallback, owns
// generated documents. An unconfigured binding preserves the legacy defaults.
func (a *App) generatedDocumentSource() (Source, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	docs := a.wsConfig.Docs
	if strings.TrimSpace(docs.Path) == "" {
		return Source{}, false
	}
	src := Source{ID: systemDocsSource, Name: "自动系统文档", Type: "local", Enabled: true, RW: true, Builtin: true}
	src.Config.Path = docs.Path
	if docs.Location == "workspace" && a.wsConfig.Workspace.Mode == "ssh" {
		src.Type = "workspace-sftp"
		// Pin an absolute base so a relative binding cannot be resolved against a
		// different workspace between directory enumeration and publication.
		src.Config.Path = a.workspaceRemoteConfiguredDir(docs.Path)
	}
	return src, true
}

// prepareGeneratedDocDir gives the existing numbering/index code a temporary
// working copy. Only the explicitly created document and index are published;
// temporary copies never become the authoritative document location.
// Caller holds filesMu.
func (a *App) prepareGeneratedDocDir(subdir string) (string, string, func(...string) error, func(), error) {
	src, bound := a.generatedDocumentSource()
	if !bound {
		rel := "system-docs/" + subdir
		if err := a.pullProjectCacheDir(rel); err != nil {
			return "", "", nil, nil, err
		}
		return filepath.Join(a.cacheContainer, filepath.FromSlash(rel)), a.projectCacheDisplayPath(rel), func(names ...string) error { return a.pushProjectCacheFiles(rel, names...) }, func() {}, nil
	}
	var root *os.Root
	var err error
	if src.Type == "local" {
		root, err = a.localSourceRoot(src)
		if err != nil {
			return "", "", nil, nil, err
		}
		if err = root.MkdirAll(subdir, 0755); err != nil {
			root.Close()
			return "", "", nil, nil, err
		}
	} else if err = a.sftpEnsureParents(path.Join(src.Config.Path, subdir, "placeholder")); err != nil {
		return "", "", nil, nil, err
	}
	staging, err := os.MkdirTemp("", "aide-generated-docs-*")
	if err != nil {
		if root != nil {
			root.Close()
		}
		return "", "", nil, nil, err
	}
	cleanup := func() {
		if root != nil {
			root.Close()
		}
		_ = os.RemoveAll(staging)
	}
	var entries []map[string]any
	if root != nil {
		entries, err = a.listLocalDir(root, subdir)
	} else {
		entries, err = a.listSourceDir(src, subdir)
	}
	if err != nil {
		cleanup()
		return "", "", nil, nil, err
	}
	for _, entry := range entries {
		name, _ := entry["name"].(string)
		if entry["dir"] == true || path.Base(name) != name || safePath(name) != nil || !strings.HasSuffix(name, ".md") {
			continue
		}
		var b []byte
		if root != nil {
			b, err = readRawBytes(root, path.Join(subdir, name))
		} else {
			b, err = a.readSourceRaw(src, path.Join(subdir, name))
		}
		if err != nil {
			cleanup()
			return "", "", nil, nil, err
		}
		if err = os.WriteFile(filepath.Join(staging, name), b, 0600); err != nil {
			cleanup()
			return "", "", nil, nil, err
		}
	}
	publish := func(names ...string) error {
		for _, name := range names {
			if path.Base(name) != name || safePath(name) != nil {
				return fmt.Errorf("invalid document filename")
			}
			b, err := os.ReadFile(filepath.Join(staging, name))
			if err != nil {
				return err
			}
			if root != nil {
				err = putText(root, path.Join(subdir, name), b)
			} else {
				err = a.writeSourceText(src, path.Join(subdir, name), b)
			}
			if err != nil {
				return err
			}
		}
		return nil
	}
	return staging, path.Join(src.Config.Path, subdir), publish, cleanup, nil
}
