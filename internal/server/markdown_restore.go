package server

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type markdownRestoreFile struct {
	Path     string `json:"path"`
	Before   string `json:"before"` // "missing" is distinct from an empty file hash.
	After    string `json:"after"`
	Bytes    int    `json:"bytes"`
	Applied  bool   `json:"applied"`
	Verified bool   `json:"verified"`
}
type markdownRestorePlan struct {
	Revision string                `json:"revision"`
	Identity string                `json:"identity"`
	Files    []markdownRestoreFile `json:"files"`
	Payloads [][]byte              `json:"-"`
}
type markdownRestoreJournal struct {
	ID      string              `json:"id"`
	Created string              `json:"created"`
	State   string              `json:"state"`
	Pending string              `json:"pending,omitempty"`
	Error   string              `json:"error,omitempty"`
	Plan    markdownRestorePlan `json:"plan"`
}

func historyCurrentHash(read func(string) ([]byte, error), p string) (string, []byte, error) {
	b, err := read(p)
	if errors.Is(err, os.ErrNotExist) || isSFTPNotExistErr(err) {
		return "missing", nil, nil
	}
	if err != nil {
		return "", nil, err
	}
	return hash(b), b, nil
}

// Build from verified archive objects; never accept paths or bytes from a client.
func (a *App) markdownRestorePlan(source, p, revision string, assets bool) (markdownRestorePlan, error) {
	plan := markdownRestorePlan{Revision: revision, Files: []markdownRestoreFile{}}
	if !strings.HasSuffix(strings.ToLower(p), ".md") && !strings.HasSuffix(strings.ToLower(p), ".markdown") {
		return plan, errors.New("只支持 Markdown 历史恢复")
	}
	identity, read, err := a.historyTarget(source, p)
	if err != nil {
		return plan, err
	}
	plan.Identity = identity
	a.historyMu.Lock()
	defer a.historyMu.Unlock()
	dir := a.markdownHistoryDir(identity)
	index, err := loadMarkdownIndex(dir, p)
	if err != nil {
		return plan, err
	}
	var selected *markdownRevision
	for i := range index.Versions {
		if index.Versions[i].ID == revision {
			selected = &index.Versions[i]
			break
		}
	}
	if selected == nil {
		return plan, errors.New("历史版本不存在")
	}
	appendFile := func(p, digest string) error {
		if err := safePath(p); err != nil {
			return err
		}
		b, err := readHistoryObject(dir, digest)
		if err != nil {
			return err
		}
		before, _, err := historyCurrentHash(read, p)
		if err != nil {
			return err
		}
		plan.Files = append(plan.Files, markdownRestoreFile{Path: p, Before: before, After: digest, Bytes: len(b)})
		plan.Payloads = append(plan.Payloads, b)
		return nil
	}
	if assets {
		for _, asset := range selected.Assets {
			if asset.Status != "stored" {
				return plan, fmt.Errorf("资源 %s 未归档，无法整版恢复；可仅恢复正文", asset.Path)
			}
			if asset.Path == p {
				continue
			}
			if err := appendFile(asset.Path, asset.Digest); err != nil {
				return plan, err
			}
		}
	}
	// Assets first, Markdown last. Existing assets absent in the old revision are
	// preserved; we do not delete unrelated or shared files.
	if err := appendFile(p, selected.Content); err != nil {
		return plan, err
	}
	return plan, nil
}

// Pin the destination. Workspace changes cannot redirect a write to another host.
func (a *App) historyRestoreWriter(source string) (string, func(string, []byte) error, func(), error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	identity := "workspace:" + a.wsID()
	remote := a.wsConfig.Workspace.Mode == "ssh"
	base := a.wsConfig.Workspace.Path
	generation := a.sshSessionGeneration(sshControlSocket)
	localBase := a.workspace.Name()
	if source != "" {
		src, ok := a.findSource(source)
		if !ok || !src.Enabled || !src.RW {
			return "", nil, nil, errors.New("来源不存在、已停用或只读")
		}
		identity = markdownSourceIdentity(src)
		switch src.Type {
		case "local", "skill":
			root, err := a.localSourceRoot(src)
			if err != nil {
				return "", nil, nil, err
			}
			return identity, func(p string, b []byte) error { return putText(root, p, b) }, func() { root.Close() }, nil
		case "workspace-sftp":
			base = a.workspaceRemoteSourcePath(src.Config.Path, ".")
			remote = true
		case "sftp":
			return identity, func(p string, b []byte) error { return a.sftpWriteSource(src, p, b) }, func() {}, nil
		default:
			return "", nil, nil, errors.New("该来源不支持历史恢复")
		}
	}
	if remote {
		return identity, func(p string, b []byte) error {
			if err := safePath(p); err != nil {
				return err
			}
			return a.sftpWriteAtGeneration(pathJoinRemote(base, p), b, generation)
		}, func() {}, nil
	}
	root, err := os.OpenRoot(localBase)
	if err != nil {
		return "", nil, nil, err
	}
	return identity, func(p string, b []byte) error { return putText(root, p, b) }, func() { root.Close() }, nil
}

func (a *App) markdownRestoreAPI(w http.ResponseWriter, r *http.Request) {
	a.filesMu.Lock()
	defer a.filesMu.Unlock()
	var body struct {
		Path        string            `json:"path"`
		Source      string            `json:"source"`
		WorkspaceID string            `json:"workspaceId"`
		Revision    string            `json:"revision"`
		Assets      bool              `json:"assets"`
		Identity    string            `json:"identity"`
		Expected    map[string]string `json:"expected"`
	}
	if err := decode(w, r, &body); err != nil {
		fail(w, 400, err)
		return
	}
	enabled, embedded := a.markdownHistoryConfig()
	if !enabled {
		fail(w, 409, errors.New("请先启用历史追踪，保证恢复前状态可归档"))
		return
	}
	a.mu.Lock()
	workspaceID := a.wsID()
	a.mu.Unlock()
	if body.Source == "" && (body.WorkspaceID == "" || body.WorkspaceID != workspaceID) {
		fail(w, 409, errors.New("工作区已变化，请重新对比"))
		return
	}
	plan, err := a.markdownRestorePlan(body.Source, body.Path, body.Revision, body.Assets)
	if err != nil {
		fail(w, 400, err)
		return
	}
	if body.Identity != plan.Identity {
		fail(w, 409, errors.New("来源已变化，请重新对比"))
		return
	}
	for _, file := range plan.Files {
		if body.Expected[file.Path] != file.Before {
			fail(w, 409, fmt.Errorf("%s 已变化，请重新对比", file.Path))
			return
		}
	}
	identity, write, close, err := a.historyRestoreWriter(body.Source)
	if err != nil {
		fail(w, 400, err)
		return
	}
	defer close()
	if identity != plan.Identity {
		fail(w, 409, errors.New("来源已变化，请重新对比"))
		return
	}
	readIdentity, read, err := a.historyTarget(body.Source, body.Path)
	if err != nil || readIdentity != identity {
		fail(w, 409, errors.New("来源已变化，请重新对比"))
		return
	}
	dir := a.markdownHistoryDir(identity)
	journal := markdownRestoreJournal{ID: newID(), Created: time.Now().UTC().Format(time.RFC3339Nano), State: "prepared", Plan: plan}
	journalPath := filepath.Join(dir, "restores", journal.ID+".json")
	if err := os.MkdirAll(filepath.Dir(journalPath), 0700); err != nil {
		fail(w, 500, err)
		return
	}
	// Back up every overwritten resource, including resources not referenced by
	// the current Markdown, before the first destination write.
	for _, file := range plan.Files {
		before, b, err := historyCurrentHash(read, file.Path)
		if err != nil {
			fail(w, 400, err)
			return
		}
		if before != file.Before {
			fail(w, 409, fmt.Errorf("%s 已变化，请重新对比", file.Path))
			return
		}
		if before != "missing" {
			if _, err := storeHistoryObject(dir, b); err != nil {
				fail(w, 500, err)
				return
			}
		}
	}
	a.historyMu.Lock()
	_, old, err := historyCurrentHash(read, body.Path)
	if err == nil && old != nil {
		err = a.captureMarkdown(dir, body.Path, old, embedded, read)
	}
	a.historyMu.Unlock()
	if err != nil {
		fail(w, 500, err)
		return
	}
	if err := atomicJSON(journalPath, journal); err != nil {
		fail(w, 500, err)
		return
	}
	recordFailure := func(err error) {
		journal.State = "incomplete"
		journal.Error = err.Error()
		persistErr := atomicJSON(journalPath, journal)
		result := map[string]any{"error": err.Error(), "journal": journal.ID, "state": journal.State, "files": journal.Plan.Files}
		if persistErr != nil {
			result["journalError"] = persistErr.Error()
		}
		jsonOut(w, 409, result)
	}
	for i, file := range plan.Files {
		if file.Before == file.After {
			journal.Plan.Files[i].Verified = true
			continue
		}
		if body.Source != "" {
			a.mu.Lock()
			current, ok := a.findSource(body.Source)
			allowed := ok && current.Enabled && current.RW
			a.mu.Unlock()
			if !allowed {
				recordFailure(errors.New("来源写入权限已撤销，恢复停止"))
				return
			}
		}
		currentIdentity, _, err := a.historyTarget(body.Source, body.Path)
		if err != nil || currentIdentity != identity {
			recordFailure(errors.New("来源已变化，恢复停止"))
			return
		}
		before, _, err := historyCurrentHash(read, file.Path)
		if err != nil {
			recordFailure(err)
			return
		}
		if before != file.Before {
			recordFailure(fmt.Errorf("%s 在恢复期间已变化，恢复停止", file.Path))
			return
		}
		journal.Pending = file.Path
		journal.State = "writing"
		if err := atomicJSON(journalPath, journal); err != nil {
			recordFailure(err)
			return
		}
		if err := write(file.Path, plan.Payloads[i]); err != nil {
			recordFailure(err)
			return
		}
		journal.Plan.Files[i].Applied = true
		after, _, err := historyCurrentHash(read, file.Path)
		if err != nil {
			recordFailure(fmt.Errorf("%s 写入已返回成功，但读取确认失败: %w", file.Path, err))
			return
		}
		if after != file.After {
			recordFailure(fmt.Errorf("%s 恢复后内容发生变化，请检查操作记录", file.Path))
			return
		}
		journal.Plan.Files[i].Verified = true
		journal.Pending = ""
		if err := atomicJSON(journalPath, journal); err != nil {
			recordFailure(err)
			return
		}
	}
	a.historyMu.Lock()
	err = a.captureMarkdown(dir, body.Path, plan.Payloads[len(plan.Payloads)-1], embedded, read)
	a.historyMu.Unlock()
	if err != nil {
		recordFailure(fmt.Errorf("文件已恢复，但新历史归档失败: %w", err))
		return
	}
	journal.State = "completed"
	if err := atomicJSON(journalPath, journal); err != nil {
		recordFailure(err)
		return
	}
	jsonOut(w, 200, map[string]any{"state": journal.State, "journal": journal.ID, "files": journal.Plan.Files, "content": string(plan.Payloads[len(plan.Payloads)-1]), "hash": hash(plan.Payloads[len(plan.Payloads)-1])})
}

func (a *App) markdownRestorePreviewAPI(w http.ResponseWriter, r *http.Request) {
	a.filesMu.Lock()
	defer a.filesMu.Unlock()
	if ws := r.URL.Query().Get("workspaceId"); r.URL.Query().Get("source") == "" && ws != "" {
		a.mu.Lock()
		current := a.wsID()
		a.mu.Unlock()
		if current != ws {
			fail(w, 409, errors.New("工作区已变化，请重新对比"))
			return
		}
	}
	plan, err := a.markdownRestorePlan(r.URL.Query().Get("source"), r.URL.Query().Get("path"), r.URL.Query().Get("revision"), r.URL.Query().Get("assets") == "1")
	if err != nil {
		fail(w, 400, err)
		return
	}
	jsonOut(w, 200, plan)
}
