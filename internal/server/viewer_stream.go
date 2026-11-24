package server

import (
	"fmt"
	"net/http"
	"os"
	"strings"
)

// Large binary downloads use a bounded streaming adapter; no CAD parser is bundled.
var stepRemotePreviewGate = make(chan struct{}, 1)

func isStepRaw(p string) bool {
	ext := effectiveFileExt(p)
	return ext == ".stp" || ext == ".step"
}

func rawViewerLimit(p string) int64 {
	return 0 // Zero selects the unbounded streaming viewer contract.
}

func rawViewerContentType(p string) string {
	ext := effectiveFileExt(p)
	if ct := map[string]string{
		".png": "image/png", ".jpg": "image/jpeg", ".jpeg": "image/jpeg",
		".gif": "image/gif", ".svg": "image/svg+xml", ".webp": "image/webp",
		".ico": "image/x-icon", ".bmp": "image/bmp",
		".mp3": "audio/mpeg", ".wav": "audio/wav", ".mp4": "video/mp4",
		".pdf": "application/pdf", ".html": "text/html; charset=utf-8",
		".css": "text/css; charset=utf-8", ".js": "application/javascript",
		".json": "application/json", ".txt": "text/plain; charset=utf-8",
		".csv": "text/csv; charset=utf-8", ".tsv": "text/tab-separated-values; charset=utf-8",
		".md": "text/markdown; charset=utf-8", ".drawio": "application/xml; charset=utf-8",
		".stl": "model/stl", ".step": "application/step", ".stp": "application/step",
	}[ext]; ct != "" {
		return ct
	}
	return "application/octet-stream"
}

// Local and random-access remote reads are streamed, including Range requests.
// Source identity and path checks are the same as the knowledge viewer.
func (a *App) streamStepRaw(w http.ResponseWriter, r *http.Request) bool {
	return a.streamViewerRaw(w, r, "application/step", 0)
}
func (a *App) streamViewerRaw(w http.ResponseWriter, r *http.Request, contentType string, limit int64) bool {
	q := r.URL.Query()
	var ar knowledgeArea
	var root *os.Root
	var err error
	directLocal := q.Get("root") == "local" && q.Get("source") == "" && q.Get("knowledgeOrigin") == "" && q.Get("knowledgeWorkspace") == ""
	if directLocal {
		root, err = a.root("local")
		if err != nil || root == nil {
			fail(w, 400, fmt.Errorf("本机目录不可用"))
			return true
		}
	} else {
		ar, err = a.knowledgeViewerArea(q.Get("root"), q.Get("source"), q.Get("knowledgeOrigin"), q.Get("knowledgeWorkspace"))
		if err != nil {
			fail(w, 400, err)
			return true
		}
		if !ar.local() {
			switch ar.kind {
			case "ssh", "workspace-sftp", "sftp", "relay":
				return a.streamTransferViewerRaw(w, r, ar, contentType, limit)
			default:
				return false
			}
		}
		if ar.unavailableReason != "" || !a.knowledgeAreaCurrent(ar) {
			fail(w, 400, fmt.Errorf("引用来源已变化"))
			return true
		}
		root, err = a.openLocalSourceRoot(r.Context(), ar.location)
		if err != nil {
			fail(w, 400, err)
			return true
		}
		defer root.Close()
	}
	p := q.Get("path")
	if !knowledgeSafePath(p) {
		fail(w, 400, fmt.Errorf("路径无效"))
		return true
	}
	parts := strings.Split(p, "/")
	for i := range parts {
		info, e := root.Lstat(strings.Join(parts[:i+1], "/"))
		if e != nil {
			fail(w, 400, e)
			return true
		}
		if info.Mode()&os.ModeSymlink != 0 {
			fail(w, 400, fmt.Errorf("不预览符号链接"))
			return true
		}
		// Reject FIFO/device leaves before Open, which could otherwise block.
		if i == len(parts)-1 && !info.Mode().IsRegular() {
			fail(w, 400, fmt.Errorf("文件预览仅支持普通文件"))
			return true
		}
	}
	f, err := root.Open(p)
	if err != nil {
		fail(w, 400, err)
		return true
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		fail(w, 400, err)
		return true
	}
	if !info.Mode().IsRegular() || limit > 0 && info.Size() > limit {
		if limit > 0 {
			fail(w, 400, fmt.Errorf("文件预览需要普通文件，且不超过 %d MiB", limit>>20))
		} else {
			fail(w, 400, fmt.Errorf("文件预览仅支持普通文件"))
		}
		return true
	}
	if !directLocal && !a.knowledgeAreaCurrent(ar) {
		fail(w, 400, fmt.Errorf("引用来源已变化"))
		return true
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "no-store")
	http.ServeContent(w, r, info.Name(), info.ModTime(), f)
	return true
}

func (a *App) streamTransferViewerRaw(w http.ResponseWriter, r *http.Request, ar knowledgeArea, contentType string, limit int64) bool {
	p := r.URL.Query().Get("path")
	if !knowledgeSafePath(p) {
		fail(w, 400, fmt.Errorf("路径无效"))
		return true
	}
	if isStepRaw(p) {
		select {
		case stepRemotePreviewGate <- struct{}{}:
			defer func() { <-stepRemotePreviewGate }()
		case <-r.Context().Done():
			fail(w, 429, fmt.Errorf("上一个大文件远程预览仍在处理"))
			return true
		}
	}
	fs, err := a.streamTransferFS(r.Context(), checkoutSource(ar), false)
	if err != nil {
		fail(w, 400, err)
		return true
	}
	defer fs.Close()
	f, err := fs.Open(p, os.O_RDONLY)
	if err != nil {
		fail(w, 400, err)
		return true
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		fail(w, 400, err)
		return true
	}
	if !info.Mode().IsRegular() || limit > 0 && info.Size() > limit {
		fail(w, 400, fmt.Errorf("文件预览仅支持普通文件"))
		return true
	}
	if !a.knowledgeAreaCurrent(ar) {
		fail(w, 400, fmt.Errorf("引用来源已变化"))
		return true
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "no-store")
	http.ServeContent(w, r, info.Name(), info.ModTime(), f)
	return true
}
