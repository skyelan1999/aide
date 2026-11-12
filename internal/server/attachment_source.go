package server

import (
	"context"
	"errors"
	"os"
	"path/filepath"
)

// Reading remote media/documents uses the same per-call, AI-visible source
// capability as plugins. No SSH credential or stale local fallback is exposed.
func (a *App) attachmentSourceBytes(att Attachment) ([]byte, error) {
	source := att.Root
	if att.Root == "source" {
		a.mu.Lock()
		src, ok := a.findSource(att.Source)
		allowed := ok && sourceAIVisible(src, a.wsID())
		var work, ref string
		if a.workspace != nil {
			work = a.workspace.Name()
		}
		if a.reference != nil {
			ref = a.reference.Name()
		}
		areas := a.knowledgeAreasLocked(a.wsID(), work, ref)
		var area *knowledgeArea
		for i := range areas {
			if areas[i].source == att.Source {
				copy := areas[i]
				area = &copy
				break
			}
		}
		a.mu.Unlock()
		if !allowed || area == nil {
			return nil, errors.New("引用来源不可用或对当前工作区 AI 不可见")
		}
		switch src.Type {
		case "local", "skill", "sftp", "workspace-sftp":
		default:
			// Preserve pre-existing document drivers, including relay/FTP,
			// while adding the same identity/visibility checks around their I/O.
			if !a.knowledgeAreaCurrent(*area) {
				return nil, errors.New("引用来源身份已变化")
			}
			b, e := a.readSourceRaw(src, att.Path)
			if e != nil {
				return nil, e
			}
			if !a.knowledgeAreaCurrent(*area) || !a.sourceAIAllowed(src.ID) {
				return nil, errors.New("引用来源读取期间发生变化")
			}
			return b, nil
		}
	}
	if source == "source" {
		if att.Source == "" {
			return nil, errors.New("缺少引用来源 ID")
		}
		source = "source:" + att.Source
	}
	if source == "" {
		source = "workspace"
	}
	a.mu.Lock()
	areas := a.pluginSourceAreasLocked()
	a.mu.Unlock()
	dir, e := os.MkdirTemp("", "aide-attachment-source-*")
	if e != nil {
		return nil, e
	}
	defer os.RemoveAll(dir)
	var total int64
	entries := map[string]*pluginSourceCheckout{}
	out, e := a.pluginCheckoutRequest(context.Background(), areas, dir, &total, entries, pluginSourceRequest{Operation: "checkout", Source: source, Path: att.Path, Slot: "attachment"})
	if e != nil {
		return nil, e
	}
	b, e := os.ReadFile(filepath.Join(dir, out.(map[string]any)["path"].(string)))
	if e != nil {
		return nil, e
	}
	if !a.pluginCheckoutEntryCurrent(entries["attachment"]) {
		return nil, errors.New("附件读取期间工作区、来源或可见性已变化")
	}
	return b, nil
}
