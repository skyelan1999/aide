package server

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// Remote project artifacts use SFTP as their destination. The local directory is
// only a per-project working copy under AIDE_DATA, never the aide source tree.
func (a *App) projectCacheDisplayPath(rel string) string {
	if remote := a.workspaceRemoteCachePath(); remote != "" {
		return path.Join(remote, rel)
	}
	return filepath.Join(a.cacheContainer, filepath.FromSlash(rel))
}

func (a *App) pullProjectCacheDir(rel string) error {
	remote := a.workspaceRemoteCachePath()
	if remote == "" {
		return nil
	}
	dir := path.Join(remote, rel)
	// Make the selected destination before listing; don't silently fall back on a
	// failed remote read, which could restart document numbering and overwrite it.
	if err := a.sftpEnsureParents(path.Join(dir, "placeholder")); err != nil {
		return err
	}
	entries, err := a.sftpListRemote(dir, rel)
	if err != nil {
		return err
	}
	local := filepath.Join(a.cacheContainer, filepath.FromSlash(rel))
	if err := os.MkdirAll(local, 0700); err != nil {
		return err
	}
	remoteNames := map[string]bool{}
	for _, entry := range entries {
		if name, ok := entry["name"].(string); ok {
			remoteNames[name] = true
		}
	}
	// Remove only stale Markdown working copies; the remote source is authoritative.
	old, err := os.ReadDir(local)
	if err != nil {
		return err
	}
	for _, entry := range old {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".md") && !remoteNames[entry.Name()] {
			if err := os.Remove(filepath.Join(local, entry.Name())); err != nil {
				return err
			}
		}
	}
	for _, entry := range entries {
		name, _ := entry["name"].(string)
		if entry["dir"] == true || !strings.HasSuffix(name, ".md") || path.Base(name) != name || safePath(name) != nil {
			continue
		}
		data, err := a.sftpRead(path.Join(dir, name))
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(local, name), data, 0600); err != nil {
			return err
		}
	}
	return nil
}

func (a *App) pushProjectCacheFiles(rel string, names ...string) error {
	remote := a.workspaceRemoteCachePath()
	if remote == "" {
		return nil
	}
	for _, name := range names {
		if path.Base(name) != name || safePath(name) != nil {
			return fmt.Errorf("invalid cache filename")
		}
		data, err := os.ReadFile(filepath.Join(a.cacheContainer, filepath.FromSlash(rel), name))
		if err != nil {
			return err
		}
		if err := a.sftpWrite(path.Join(remote, rel, name), data); err != nil {
			return err
		}
	}
	return nil
}
