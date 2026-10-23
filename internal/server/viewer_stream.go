package server

import (
	"fmt"
	"net/http"
	"os"
	"strings"
)

const maxStepPreview = 384 << 20

// Large binary downloads use a bounded streaming adapter; no CAD parser is bundled.
var stepRemotePreviewGate = make(chan struct{}, 1)

func isStepRaw(p string) bool {
	ext := effectiveFileExt(p)
	return ext == ".stp" || ext == ".step"
}

func rawViewerLimit(p string) int64 {
	if isStepRaw(p) {
		return maxStepPreview
	}
	return maxRawFile
}

// Local CAD reads are streamed, including Range requests, instead of allocating
// hundreds of MiB per HTTP request. Source identity and path checks are the same
// as the knowledge viewer; remote sources retain their bounded read adapters.
func (a *App) streamStepRaw(w http.ResponseWriter, r *http.Request) bool {
	return a.streamViewerRaw(w, r, "application/step", maxStepPreview)
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
			return false
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
	if !info.Mode().IsRegular() || info.Size() > limit {
		fail(w, 400, fmt.Errorf("文件预览需要普通文件，且不超过 %d MiB", limit>>20))
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
