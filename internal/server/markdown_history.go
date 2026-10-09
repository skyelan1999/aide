package server

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// History is service data, isolated by workspace/source identity. Objects are
// immutable content-addressed bytes; version metadata is atomically replaced.
type markdownAsset struct {
	Path   string `json:"path"`
	Digest string `json:"digest,omitempty"`
	Status string `json:"status"`
	Bytes  int    `json:"bytes,omitempty"`
}
type markdownRevision struct {
	ID          string          `json:"id"`
	Created     string          `json:"created"`
	Content     string          `json:"contentDigest"`
	Fingerprint string          `json:"fingerprint"`
	Assets      []markdownAsset `json:"assets"`
}
type markdownHistoryIndex struct {
	Path     string             `json:"path"`
	Versions []markdownRevision `json:"versions"`
}

func (a *App) markdownHistoryConfig() (bool, bool) {
	// Read the atomically persisted registry: callers may already hold a.mu.
	b, err := os.ReadFile(filepath.Join(a.workPath, pluginsDirName, "registry.json"))
	if err != nil {
		return false, true
	}
	var reg pluginRegistry
	if json.Unmarshal(b, &reg) != nil {
		return false, true
	}
	for _, p := range reg.Plugins {
		if p.ID == "markdown-history" {
			embedded := true
			if v, ok := p.Settings["trackEmbedded"].(bool); ok {
				embedded = v
			}
			return p.Enabled, embedded
		}
	}
	return false, true
}
func markdownSourceIdentity(src Source) string {
	config, _ := json.Marshal(map[string]any{"type": src.Type, "config": src.Config})
	return "source:" + src.ID + ":" + hash(config)
}

func (a *App) markdownHistoryDir(identity string) string {
	return filepath.Join(a.dataPath, "markdown-history", hash([]byte(identity)))
}
func historyIndexPath(dir, p string) string { return filepath.Join(dir, hash([]byte(p))+".json") }
func loadMarkdownIndex(dir, p string) (markdownHistoryIndex, error) {
	index := markdownHistoryIndex{Path: p, Versions: []markdownRevision{}}
	b, err := os.ReadFile(historyIndexPath(dir, p))
	if errors.Is(err, os.ErrNotExist) {
		return index, nil
	}
	if err != nil {
		return index, err
	}
	err = json.Unmarshal(b, &index)
	return index, err
}
func storeHistoryObject(dir string, b []byte) (string, error) {
	digest := hash(b)
	objects := filepath.Join(dir, "objects")
	if err := os.MkdirAll(objects, 0700); err != nil {
		return "", err
	}
	dest := filepath.Join(objects, digest)
	if _, err := os.Stat(dest); err == nil {
		return digest, nil
	}
	f, err := os.CreateTemp(objects, ".snapshot-")
	if err != nil {
		return "", err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if err = f.Chmod(0600); err == nil {
		_, err = f.Write(b)
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(tmp, dest)
	}
	return digest, err
}

// Never serve damaged objects, even if their metadata is valid JSON.
func readHistoryObject(dir, digest string) ([]byte, error) {
	if len(digest) != 64 || strings.Trim(digest, "0123456789abcdef") != "" {
		return nil, errors.New("历史对象摘要无效")
	}
	b, err := os.ReadFile(filepath.Join(dir, "objects", digest))
	if err != nil {
		return nil, err
	}
	if hash(b) != digest {
		return nil, errors.New("历史对象完整性校验失败")
	}
	return b, nil
}

var markdownInlineRef = regexp.MustCompile(`!?\[[^\]\n]*\]\(\s*(<[^>]+>|[^\s)]+)`)
var markdownDefinitionRef = regexp.MustCompile(`(?m)^\s{0,3}\[[^\]\n]+\]:\s*(<[^>]+>|[^\s]+)`)
var markdownHTMLRef = regexp.MustCompile(`(?i)(?:src|href)\s*=\s*["']([^"']+)["']`)
var markdownFence = regexp.MustCompile("(?ms)^\\s{0,3}(?:```|~~~).*?^\\s{0,3}(?:```|~~~)[^\\n]*")

func markdownReferences(p string, b []byte) []string {
	text := markdownFence.ReplaceAllString(string(b), "")
	refs := map[string]bool{}
	for _, re := range []*regexp.Regexp{markdownInlineRef, markdownDefinitionRef, markdownHTMLRef} {
		for _, m := range re.FindAllStringSubmatch(text, -1) {
			raw := strings.Trim(m[1], "<>")
			u, err := url.Parse(raw)
			if err != nil || u.Scheme != "" || u.Host != "" || u.Path == "" || strings.HasPrefix(u.Path, "/") {
				continue
			}
			rel := path.Clean(path.Join(path.Dir(p), u.Path))
			if safePath(rel) != nil || rel == p {
				continue
			}
			refs[rel] = true
		}
	}
	result := make([]string, 0, len(refs))
	for ref := range refs {
		result = append(result, ref)
	}
	sort.Strings(result)
	return result
}
func (a *App) captureMarkdown(dir, p string, b []byte, embedded bool, read func(string) ([]byte, error)) error {
	index, err := loadMarkdownIndex(dir, p)
	if err != nil {
		return err
	}
	digest, err := storeHistoryObject(dir, b)
	if err != nil {
		return err
	}
	if !embedded && len(index.Versions) > 0 && index.Versions[len(index.Versions)-1].Content == digest {
		return nil
	}
	assets := []markdownAsset{}
	total := 0
	if embedded {
		refs := markdownReferences(p, b)
		if len(refs) > 64 {
			return errors.New("Markdown 引用资源超过 64 项，历史追踪未完成")
		}
		for _, ref := range refs {
			data, readErr := read(ref)
			item := markdownAsset{Path: ref, Status: "unavailable"}
			if readErr == nil {
				total += len(data)
				if total > 32<<20 {
					return errors.New("Markdown 引用资源超过 32 MiB，历史追踪未完成")
				}
				item.Digest, err = storeHistoryObject(dir, data)
				if err != nil {
					return err
				}
				item.Status = "stored"
				item.Bytes = len(data)
			}
			assets = append(assets, item)
		}
	}
	raw, _ := json.Marshal(struct {
		Content string
		Assets  []markdownAsset
	}{digest, assets})
	fingerprint := hash(raw)
	if len(index.Versions) > 0 && index.Versions[len(index.Versions)-1].Fingerprint == fingerprint {
		return nil
	}
	index.Versions = append(index.Versions, markdownRevision{ID: fmt.Sprintf("%06d", len(index.Versions)+1), Created: time.Now().UTC().Format(time.RFC3339Nano), Content: digest, Fingerprint: fingerprint, Assets: assets})
	return atomicJSON(historyIndexPath(dir, p), index)
}
func (a *App) withMarkdownHistory(identity, p string, b []byte, read func(string) ([]byte, error), write func() error) error {
	enabled, embedded := a.markdownHistoryConfig()
	if !enabled {
		return write()
	}
	a.historyMu.Lock()
	defer a.historyMu.Unlock()
	dir := a.markdownHistoryDir(identity)
	md := strings.EqualFold(path.Ext(p), ".md") || strings.EqualFold(path.Ext(p), ".markdown")
	if md {
		old, err := read(p)
		if err == nil {
			if err = a.captureMarkdown(dir, p, old, embedded, read); err != nil {
				return fmt.Errorf("保存前历史归档失败: %w", err)
			}
		} else if !errors.Is(err, os.ErrNotExist) && !isSFTPNotExistErr(err) {
			return fmt.Errorf("无法读取待归档原文: %w", err)
		}
	}
	if err := write(); err != nil {
		return err
	}
	if md {
		if err := a.captureMarkdown(dir, p, b, embedded, read); err != nil {
			return fmt.Errorf("文件已保存，但历史归档失败: %w", err)
		}
		return nil
	}
	// Embedded assets updated through Aide also advance their indexed parent MD.
	if embedded {
		entries, err := os.ReadDir(dir)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
				continue
			}
			raw, err := os.ReadFile(filepath.Join(dir, entry.Name()))
			if err != nil {
				return err
			}
			var index markdownHistoryIndex
			if json.Unmarshal(raw, &index) != nil || len(index.Versions) == 0 {
				continue
			}
			for _, asset := range index.Versions[len(index.Versions)-1].Assets {
				if asset.Path == p {
					text, err := read(index.Path)
					if err != nil {
						return fmt.Errorf("资源已保存，但无法读取关联 Markdown: %w", err)
					}
					if err = a.captureMarkdown(dir, index.Path, text, true, read); err != nil {
						return fmt.Errorf("资源已保存，但关联历史归档失败: %w", err)
					}
					break
				}
			}
		}
	}
	return nil
}
func (a *App) historyTarget(source, p string) (string, func(string) ([]byte, error), error) {
	if err := safePath(p); err != nil {
		return "", nil, err
	}
	if source == "" {
		a.mu.Lock()
		identity := "workspace:" + a.wsID()
		remote := a.wsConfig.Workspace.Mode == "ssh"
		remoteBase := a.wsConfig.Workspace.Path
		generation := a.sshSessionGeneration(sshControlSocket)
		localBase := a.workspace.Name()
		a.mu.Unlock()
		return identity, func(p string) ([]byte, error) {
			if err := safePath(p); err != nil {
				return nil, err
			}
			if remote {
				b, err := a.sftpReadAtGeneration(pathJoinRemote(remoteBase, p), generation)
				if err != nil {
					return nil, err
				}
				return checkedRaw(b)
			}
			root, err := os.OpenRoot(localBase)
			if err != nil {
				return nil, err
			}
			defer root.Close()
			return readRawBytes(root, p)
		}, nil
	}
	a.mu.Lock()
	src, ok := a.findSource(source)
	remoteBase := ""
	generation := uint64(0)
	if ok && src.Type == "workspace-sftp" {
		remoteBase = a.workspaceRemoteSourcePath(src.Config.Path, ".")
		generation = a.sshSessionGeneration(sshControlSocket)
	}
	a.mu.Unlock()
	if !ok || !src.Enabled {
		return "", nil, errors.New("来源不存在或已停用")
	}
	if src.Type == "mcp" {
		return "", nil, errors.New("MCP 来源无文件历史")
	}
	return markdownSourceIdentity(src), func(p string) ([]byte, error) {
		if src.Type == "workspace-sftp" {
			if err := safePath(p); err != nil {
				return nil, err
			}
			b, err := a.sftpReadAtGeneration(pathJoinRemote(remoteBase, p), generation)
			if err != nil {
				return nil, err
			}
			return checkedRaw(b)
		}
		return a.readSourceRaw(src, p)
	}, nil
}
func (a *App) markdownHistoryData(source, p, revision, asset string) (map[string]any, error) {
	enabled, embedded := a.markdownHistoryConfig()
	if p == "" {
		return map[string]any{"enabled": enabled, "trackEmbedded": embedded}, nil
	}
	identity, read, err := a.historyTarget(source, p)
	if err != nil {
		return nil, err
	}
	a.historyMu.Lock()
	defer a.historyMu.Unlock()
	dir := a.markdownHistoryDir(identity)
	// Observe external edits only for list requests. Looking up an old revision
	// must remain usable when the live file is unavailable.
	observation := "disabled"
	var observationError string
	if enabled && revision == "" && (strings.EqualFold(path.Ext(p), ".md") || strings.EqualFold(path.Ext(p), ".markdown")) {
		observation = "checked"
		current, readErr := read(p)
		if readErr == nil {
			readErr = a.captureMarkdown(dir, p, current, embedded, read)
		}
		if readErr != nil {
			observation = "unavailable"
			observationError = readErr.Error()
		}
	}
	index, err := loadMarkdownIndex(dir, p)
	if err != nil {
		return nil, err
	}
	visible := index.Versions
	if len(visible) > 200 {
		visible = visible[len(visible)-200:]
	}
	result := map[string]any{"enabled": enabled, "trackEmbedded": embedded, "path": p, "versions": visible, "total": len(index.Versions), "observation": observation, "observationError": observationError}
	if revision != "" {
		for _, v := range index.Versions {
			if v.ID == revision {
				if asset != "" {
					for _, item := range v.Assets {
						if item.Path == asset && item.Status == "stored" {
							raw, err := readHistoryObject(dir, item.Digest)
							if err != nil {
								return nil, err
							}
							return map[string]any{"asset": item, "base64": base64.StdEncoding.EncodeToString(raw)}, nil
						}
					}
					return nil, errors.New("此版本没有该资源快照")
				}
				b, err := readHistoryObject(dir, v.Content)
				if err != nil {
					return nil, err
				}
				result["content"] = string(b)
				result["revision"] = v
				return result, nil
			}
		}
		return nil, errors.New("历史版本不存在")
	}
	return result, nil
}
func (a *App) markdownHistoryAPI(w http.ResponseWriter, r *http.Request) {
	a.filesMu.Lock()
	defer a.filesMu.Unlock()
	if ws := r.URL.Query().Get("workspaceId"); r.URL.Query().Get("source") == "" && ws != "" && ws != a.wsID() {
		fail(w, 409, errors.New("工作区已切换"))
		return
	}
	result, err := a.markdownHistoryData(r.URL.Query().Get("source"), r.URL.Query().Get("path"), r.URL.Query().Get("revision"), r.URL.Query().Get("asset"))
	if err != nil {
		fail(w, 400, err)
		return
	}
	jsonOut(w, 200, result)
}
