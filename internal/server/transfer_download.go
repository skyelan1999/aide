package server

import (
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"time"
)

func (a *App) streamFileDownload(w http.ResponseWriter, r *http.Request) {
	// Archives are prepared once on disk, then immutable and Range-downloadable.
	if id := r.URL.Query().Get("job"); id != "" {
		m := a.transfers()
		m.mu.Lock()
		j := m.jobs[id]
		m.mu.Unlock()
		if j == nil {
			fail(w, 404, os.ErrNotExist)
			return
		}
		j.mu.Lock()
		e := m.check(j)
		ready := j.Operation == "download" && j.Status == "complete"
		name := "download.zip"
		stored := "download.zip"
		version := j.DownloadVersion
		if len(j.Tops) == 1 {
			name = path.Base(j.Tops[0].Source) + ".zip"
			if j.RawDownload {
				name = path.Base(j.Tops[0].Source)
				stored = "download.file"
			}
		}
		j.mu.Unlock()
		if e != nil {
			fail(w, 409, e)
			return
		}
		if !ready {
			fail(w, 409, errors.New("归档尚未准备完成"))
			return
		}
		f, e := os.Open(filepath.Join(m.jobDir(j), stored))
		if e != nil {
			fail(w, 500, e)
			return
		}
		defer f.Close()
		info, e := f.Stat()
		if e != nil {
			fail(w, 500, e)
			return
		}
		serveTransferFile(w, r, name, j.ID+":"+version, info, f, true)
		return
	}
	p := r.URL.Query().Get("path")
	if e := validTransferPath(p); e != nil {
		fail(w, 400, e)
		return
	}
	source := r.URL.Query().Get("source")
	if source == "" {
		if r.URL.Query().Get("root") != "workspace" {
			fail(w, 400, errors.New("需要工作区或来源"))
			return
		}
		source = "workspace"
	}
	binding, e := a.transferBinding(source, false)
	if e != nil {
		fail(w, 400, e)
		return
	}
	if expected := r.URL.Query().Get("workspaceId"); expected != "" {
		a.mu.Lock()
		current := a.wsID()
		a.mu.Unlock()
		if expected != current {
			fail(w, 409, errors.New("工作区已切换"))
			return
		}
	}
	fs, e := a.streamTransferFS(r.Context(), source, false)
	if e != nil {
		fail(w, 400, e)
		return
	}
	defer fs.Close()
	f, e := fs.Open(p, os.O_RDONLY)
	if e != nil {
		fail(w, 400, e)
		return
	}
	defer f.Close()
	info, e := f.Stat()
	if e != nil || !info.Mode().IsRegular() {
		fail(w, 400, errors.New("仅普通文件支持分段下载"))
		return
	}
	current, e := a.transferBinding(source, false)
	if e != nil || binding != current {
		fail(w, 409, errors.New("来源配置已变化"))
		return
	}
	serveTransferFile(w, r, path.Base(p), binding+":"+p, info, f, false)
}
func serveTransferFile(w http.ResponseWriter, r *http.Request, name, identity string, info os.FileInfo, reader io.ReaderAt, immutable bool) {
	tag := `"` + hash([]byte(identity+":"+strconv.FormatInt(info.Size(), 10)+":"+strconv.FormatInt(info.ModTime().UnixNano(), 10))) + `"`
	modTime := info.ModTime().Truncate(time.Second)
	if !immutable {
		tag = "W/" + tag
		modTime = time.Time{}
	}
	w.Header().Set("ETag", tag)
	w.Header().Set("Cache-Control", "private, no-cache")
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": name}))
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("X-Transfer-Size", fmt.Sprint(info.Size()))
	// ServeContent applies RFC strong comparison for If-Match/If-Range. Weak
	// metadata validators on mutable files cannot authorize splicing ranges.
	http.ServeContent(w, r, name, modTime, io.NewSectionReader(reader, 0, info.Size()))
}
