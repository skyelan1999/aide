package server

// Compatibility entry points now stream through the same durable engine.
import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

func (m *transferManager) wait(ctx context.Context, j *transferJob) error {
	ticker := time.NewTicker(30 * time.Millisecond)
	defer ticker.Stop()
	for {
		j.mu.Lock()
		state, message := j.Status, j.Error
		j.mu.Unlock()
		switch state {
		case "complete":
			return nil
		case "failed":
			return errors.New(message)
		case "cancelled", "paused":
			return errors.New("传输已暂停或取消")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}
func (a *App) legacyLargeCopy(w http.ResponseWriter, r *http.Request) {
	var req fileTransferRequest
	if e := decode(w, r, &req); e != nil {
		fail(w, 400, e)
		return
	}
	if !a.transferSourceStreaming(req.Source) {
		a.legacyReferenceTransfer(w, r, req)
		return
	}
	if req.Operation != "copy" && req.Operation != "move" {
		fail(w, 400, errors.New("操作只能是复制或移动"))
		return
	}
	if req.Operation == "move" && req.Source != "workspace" {
		loc, e := a.transferLocation(req.Source, false)
		if e == nil && loc.hasSource && !loc.source.RW {
			fail(w, 403, errors.New("只读引用不能移动"))
			return
		}
	}
	m := a.transfers()
	j, e := m.create(r.Context(), transferCreate{Operation: req.Operation, Source: req.Source, Destination: req.Destination, Paths: req.Paths, DestinationPath: req.DestinationPath})
	if e != nil {
		code := 400
		if os.IsExist(e) || containsConflict(e) {
			code = 409
		}
		fail(w, code, e)
		return
	}
	if e = m.start(j); e == nil {
		e = m.wait(r.Context(), j)
	}
	if e != nil {
		fail(w, 500, e)
		return
	}
	jsonOut(w, 200, map[string]any{"count": len(j.Tops), "operation": j.Operation, "transferId": j.ID})
}
func containsConflict(e error) bool { return e != nil && strings.Contains(e.Error(), "已存在") }
func (a *App) legacyLargeUpload(w http.ResponseWriter, r *http.Request) {
	// Compatibility raw requests retain their request budget. Large/resumable
	// uploads use the declared-size, 4MiB chunk endpoints instead.
	if r.ContentLength > maxRawFile {
		fail(w, 413, errors.New("请使用文件传输中的分块上传通道"))
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxRawFile)
	p := r.URL.Query().Get("path")
	if e := validTransferPath(p); e != nil {
		fail(w, 400, e)
		return
	}
	source := r.URL.Query().Get("source")
	if source == "" {
		source = "workspace"
		a.mu.Lock()
		ws := a.wsID()
		a.mu.Unlock()
		if expected := r.URL.Query().Get("workspaceId"); expected != "" && expected != ws {
			fail(w, 409, errors.New("工作区已切换"))
			return
		}
	} else {
		a.mu.Lock()
		src, ok := a.findSource(source)
		a.mu.Unlock()
		if ok && !src.RW {
			fail(w, 403, errors.New("该来源为只读"))
			return
		}
	}
	m := a.transfers()
	j, e := m.create(r.Context(), transferCreate{Operation: "upload", Destination: source, Name: p, Size: max(0, r.ContentLength)})
	if e != nil {
		code := 400
		if containsConflict(e) {
			code = 409
		}
		fail(w, code, e)
		return
	}
	held := false
	select {
	case m.slots <- struct{}{}:
		held = true
		defer func() {
			if held {
				<-m.slots
			}
		}()
	case <-r.Context().Done():
		return
	}
	buf := make([]byte, int(transferChunkSize))
	size := int64(0)
	for {
		n, end, readErr := readTransferBodyChunk(r.Body, buf)
		if readErr != nil {
			e = readErr
			break
		}
		if r.ContentLength >= 0 && ((end && size+int64(n) != r.ContentLength) || size+int64(n) > r.ContentLength) {
			e = errors.New("上传长度与声明不匹配")
			break
		}
		if n > 0 {
			j.mu.Lock()
			if r.ContentLength < 0 {
				j.Size = size + int64(n)
				j.Parts[0].Size = j.Size
			}
			j.mu.Unlock()
			e = m.writeUploadChunk(r.Context(), j, size/transferChunkSize, buf[:n], hash(buf[:n]))
			if e != nil {
				break
			}
			size += int64(n)
		}
		if end {
			break
		}
	}
	j.mu.Lock()
	if e != nil {
		j.Status = "failed"
		j.Error = e.Error()
	}
	saveErr := m.save(j)
	j.mu.Unlock()
	if e == nil {
		e = saveErr
	}
	if e != nil {
		fail(w, 500, e)
		return
	}

	<-m.slots
	held = false

	if e = m.start(j); e == nil {
		e = m.wait(r.Context(), j)
	}
	if e != nil {
		fail(w, 500, e)
		return
	}
	jsonOut(w, 201, map[string]any{"path": p, "size": size, "transferId": j.ID})
}
func (a *App) legacyLargeArchive(w http.ResponseWriter, r *http.Request, p string) {
	source := r.URL.Query().Get("source")
	if source == "" {
		source = "workspace"
	}
	m := a.transfers()
	j, e := m.create(r.Context(), transferCreate{Operation: "download", Source: source, Paths: []string{p}})
	if e == nil {
		e = m.start(j)
	}
	if e == nil {
		e = m.wait(r.Context(), j)
	}
	if e != nil {
		fail(w, 400, e)
		return
	}
	clone := r.Clone(r.Context())
	urlCopy := *r.URL
	query := urlCopy.Query()
	query.Set("job", j.ID)
	urlCopy.RawQuery = query.Encode()
	clone.URL = &urlCopy
	a.streamFileDownload(w, clone)
}

func readTransferBodyChunk(r io.Reader, b []byte) (int, bool, error) {
	n := 0
	for n < len(b) {
		got, e := r.Read(b[n:])
		n += got
		if e == io.EOF {
			return n, true, nil
		}
		if e != nil {
			return n, false, e
		}
		if got == 0 {
			return n, false, io.ErrNoProgress
		}
	}
	return n, false, nil
}
