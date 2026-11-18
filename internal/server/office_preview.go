package server

import (
	"archive/zip"
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

//go:embed office_preview.py
var officePreviewScript string

var officePreviewSlots = make(chan struct{}, 2)

const officePreviewLifetime = 15 * time.Minute

type officePreview struct {
	dir, version string
	request      [6]string
	meta         []byte
	current      func() bool
	expires      time.Time
	expiration   *time.Timer
	users        int
	evicted      bool
}

// Keep a few immutable render snapshots on disk. A Range request leases the
// snapshot until it finishes, so eviction cannot unlink a PDF being served.
type officePreviewCache struct {
	sync.Mutex
	entries []*officePreview
	closed  bool
}

func (c *officePreviewCache) discard(p *officePreview) {
	p.evicted = true
	p.expiration.Stop()
	if p.users == 0 {
		_ = os.RemoveAll(p.dir)
	}
}

func (c *officePreviewCache) close() {
	c.Lock()
	defer c.Unlock()
	c.closed = true
	for _, p := range c.entries {
		c.discard(p)
	}
	c.entries = nil
}

func (c *officePreviewCache) release(p *officePreview) {
	c.Lock()
	defer c.Unlock()
	p.users--
	if p.evicted && p.users == 0 {
		_ = os.RemoveAll(p.dir)
	}
}

func (c *officePreviewCache) expire(p *officePreview) {
	c.Lock()
	defer c.Unlock()
	if p.evicted {
		return
	}
	if remaining := time.Until(p.expires); remaining > 0 {
		p.expiration.Reset(remaining)
		return
	}
	for i, entry := range c.entries {
		if entry == p {
			c.discard(p)
			c.entries = append(c.entries[:i], c.entries[i+1:]...)
			return
		}
	}
}

func (c *officePreviewCache) add(p *officePreview) bool {
	c.Lock()
	defer c.Unlock()
	if c.closed {
		return false
	}
	if len(c.entries) == 4 {
		c.discard(c.entries[0])
		c.entries = c.entries[1:]
	}
	p.users = 1
	p.expiration = time.AfterFunc(officePreviewLifetime, func() { c.expire(p) })
	c.entries = append(c.entries, p)
	return true
}

func (c *officePreviewCache) acquire(request [6]string, version string) *officePreview {
	c.Lock()
	defer c.Unlock()
	now := time.Now()
	kept := c.entries[:0]
	var match *officePreview
	for _, p := range c.entries {
		if !now.Before(p.expires) {
			c.discard(p)
			continue
		}
		kept = append(kept, p)
		if p.version == version && p.request == request {
			p.users++
			p.expires = now.Add(officePreviewLifetime)
			p.expiration.Reset(officePreviewLifetime)
			match = p
		}
	}
	c.entries = kept
	return match
}

func officePreviewExtension(ext string) bool {
	switch ext {
	case ".doc", ".docx", ".odt", ".xls", ".xlsx", ".ods", ".ppt", ".pptx", ".odp":
		return true
	}
	return false
}

type officeCopyReader struct {
	context.Context
	io.Reader
}

func (r officeCopyReader) Read(p []byte) (int, error) {
	if err := r.Err(); err != nil {
		return 0, err
	}
	return r.Reader.Read(p)
}

type officeSnapshotWriter struct {
	target    *os.File
	space     *transferFS
	remaining int64
}

func (w *officeSnapshotWriter) Write(p []byte) (int, error) {
	if w.remaining < int64(len(p)) {
		if err := w.space.CheckSpace(int64(len(p))); err != nil {
			return 0, dwgError(507, "Office 预览暂存空间不足，请释放磁盘空间后重试。")
		}
		w.remaining = 4 << 20
	}
	n, err := w.target.Write(p)
	w.remaining -= int64(n)
	return n, err
}

// Capture a private disk snapshot using the ordinary source credentials and
// identity checks. Office source bytes are streamed without a file-size cutoff.
func (a *App) captureOfficeSource(ctx context.Context, r *http.Request, dest string) (func() bool, error) {
	q := r.URL.Query()
	p, rootName := q.Get("path"), q.Get("root")
	target, err := os.OpenFile(dest, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return nil, err
	}
	defer target.Close()
	tempRoot, err := os.OpenRoot(filepath.Dir(dest))
	if err != nil {
		return nil, err
	}
	defer tempRoot.Close()
	space := &transferFS{root: tempRoot}
	writer := &officeSnapshotWriter{target: target, space: space}
	copyFile := func(f io.Reader, stat func() (os.FileInfo, error)) error {
		before, err := stat()
		if err != nil || !before.Mode().IsRegular() {
			return errors.New("预览仅支持普通文件")
		}
		if err := space.CheckSpace(before.Size()); err != nil {
			return dwgError(507, "Office 预览暂存空间不足，请释放磁盘空间后重试。")
		}
		n, err := io.Copy(writer, officeCopyReader{Context: ctx, Reader: f})
		if err != nil {
			return err
		}
		after, err := stat()
		if err != nil || after.Size() != n || before.Size() != n || before.ModTime() != after.ModTime() {
			return dwgError(409, "Office 文件已变化，请重新打开预览。")
		}
		return nil
	}
	copyLocal := func(root *os.Root) error {
		file, err := openPreviewLocal(root, p)
		if err != nil {
			return err
		}
		defer file.Close()
		return copyFile(file, file.Stat)
	}
	if (rootName == "local" || strings.HasPrefix(rootName, "mount:")) && q.Get("source") == "" && q.Get("knowledgeOrigin") == "" && q.Get("knowledgeWorkspace") == "" {
		a.mu.Lock()
		root, err := a.root(rootName)
		a.mu.Unlock()
		if err != nil {
			return nil, err
		}
		current := func() bool {
			a.mu.Lock()
			defer a.mu.Unlock()
			active, err := a.root(rootName)
			return err == nil && root == active
		}
		return current, copyLocal(root)
	}
	ar, err := a.knowledgeViewerArea(rootName, q.Get("source"), q.Get("knowledgeOrigin"), q.Get("knowledgeWorkspace"))
	if err != nil {
		return nil, err
	}
	current := func() bool { return a.knowledgeAreaCurrent(ar) }
	if ar.unavailableReason != "" || !current() {
		return nil, dwgError(409, "文件来源已变化，请重新打开。")
	}
	switch ar.kind {
	case "local", "skill":
		root, err := a.openLocalSourceRoot(ctx, ar.location)
		if err != nil {
			return nil, err
		}
		defer root.Close()
		return current, copyLocal(root)
	case "ssh", "workspace-sftp", "sftp", "relay":
		fs, err := a.streamTransferFS(ctx, checkoutSource(ar), false)
		if err != nil {
			return nil, err
		}
		defer fs.Close()
		if !current() {
			return nil, dwgError(409, "文件来源已变化，请重新打开。")
		}
		file, err := fs.Open(p, os.O_RDONLY)
		if err != nil {
			return nil, err
		}
		defer file.Close()
		return current, copyFile(file, file.Stat)
	case "link", "smb", "ftp", "ftps":
		if (ar.kind == "link" || ar.kind == "smb") && p != "resource.txt" {
			return nil, errors.New("未知单资源路径")
		}
		cmd, err := a.knowledgeCurlCommand(ctx, ar, p, false, nil)
		if err != nil {
			return nil, err
		}
		cmd.Stdout, cmd.Stderr = writer, io.Discard
		return current, cmd.Run()
	default:
		return nil, errors.New("不支持的引用来源类型")
	}
}

func validateOfficePreviewInput(input, ext string) error {
	switch ext {
	case ".doc", ".xls", ".ppt":
		file, err := os.Open(input)
		if err != nil {
			return err
		}
		defer file.Close()
		var header [8]byte
		if _, err = io.ReadFull(file, header[:]); err != nil || header != [8]byte{0xd0, 0xcf, 0x11, 0xe0, 0xa1, 0xb1, 0x1a, 0xe1} {
			return dwgError(400, "旧版 Office 文件格式无效。")
		}
	default:
		archive, err := zip.OpenReader(input)
		if err != nil {
			return dwgError(400, "Office 文件格式无效或已损坏。")
		}
		archive.Close()
	}
	return nil
}

func convertOfficePreview(ctx context.Context, input, dir string) ([]byte, error) {
	converter, err := exec.LookPath("soffice")
	if err != nil {
		return nil, dwgError(503, "Office 页面预览组件未安装，请更新标准 Aide 构建。")
	}
	python, err := exec.LookPath("python3")
	if err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, python, "-c", officePreviewScript, converter, input, dir)
	cmd.Dir = dir
	cmd.Env, err = pdfFontEnvironment(dir)
	if err != nil {
		return nil, err
	}
	cmd.Env = append(cmd.Env, "SAL_USE_VCLPLUGIN=svp", "OMP_NUM_THREADS=1")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return os.ErrProcessDone
		}
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	cmd.WaitDelay = time.Second
	cmd.Stdout, cmd.Stderr = io.Discard, io.Discard
	err = cmd.Run()
	if cmd.Process != nil {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	if ctx.Err() != nil {
		return nil, dwgError(504, "Office 页面预览已超时或取消，请重试。")
	}
	if err != nil {
		return nil, dwgError(422, "Office 文件无法生成页面预览，请检查文件格式或加密状态。")
	}
	pdf, err := os.Open(filepath.Join(dir, "input.pdf"))
	if err != nil {
		return nil, dwgError(422, "Office 页面预览无效。")
	}
	var header [5]byte
	_, err = io.ReadFull(pdf, header[:])
	pdf.Close()
	if err != nil || !bytes.Equal(header[:], []byte("%PDF-")) {
		return nil, dwgError(422, "Office 页面预览无效。")
	}
	meta, err := os.ReadFile(filepath.Join(dir, "meta.json"))
	var parsed struct{ Pages int }
	if err != nil || len(meta) > 64<<10 || json.Unmarshal(meta, &parsed) != nil || parsed.Pages < 1 || parsed.Pages > 2000 {
		return nil, dwgError(422, "Office 页面预览无效。")
	}
	return meta, nil
}

var officePreviewConvert = convertOfficePreview

func (a *App) newOfficePreview(ctx context.Context, r *http.Request, key [6]string, ext string) (*officePreview, error) {
	select {
	case officePreviewSlots <- struct{}{}:
		defer func() { <-officePreviewSlots }()
	default:
		return nil, dwgError(429, "Office 预览正在处理其他文件，请稍后重试。")
	}
	dir, err := os.MkdirTemp("", "aide-office-preview-*")
	if err != nil {
		return nil, err
	}
	keep := false
	defer func() {
		if !keep {
			_ = os.RemoveAll(dir)
		}
	}()
	input := filepath.Join(dir, "input"+ext)
	current, err := a.captureOfficeSource(ctx, r, input)
	if err != nil {
		return nil, err
	}
	if !current() {
		return nil, dwgError(409, "文件来源已变化，请重新打开。")
	}
	if err = validateOfficePreviewInput(input, ext); err != nil {
		return nil, err
	}
	meta, err := officePreviewConvert(ctx, input, dir)
	if err != nil {
		return nil, err
	}
	if !current() {
		return nil, dwgError(409, "文件来源已变化，请重新打开。")
	}
	// Retain only the PDF needed for range reads, never the private source copy.
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		if entry.Name() != "input.pdf" {
			if err = os.RemoveAll(filepath.Join(dir, entry.Name())); err != nil {
				return nil, err
			}
		}
	}
	p := &officePreview{dir: dir, version: newID(), request: key, meta: meta, current: current, expires: time.Now().Add(officePreviewLifetime)}
	if !a.officePreviews.add(p) {
		return nil, dwgError(503, "Aide 正在退出")
	}
	keep = true
	return p, nil
}

func (a *App) officePagePreview(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	q := r.URL.Query()
	ext := effectiveFileExt(q.Get("path"))
	if q.Get("knowledgeOrigin") != "" && q.Get("knowledgeWorkspace") != "" && q.Get("source") != "" {
		a.mu.Lock()
		src, ok := a.findSource(q.Get("source"))
		a.mu.Unlock()
		hint := q.Get("officeFormat")
		if ok && src.Enabled && (src.Type == "link" || src.Type == "smb") && officePreviewExtension(hint) {
			ext = hint
		}
	}
	format := q.Get("format")
	if !officePreviewExtension(ext) || !knowledgeSafePath(q.Get("path")) || (format != "pdf" && format != "meta" && format != "page") {
		fail(w, 400, errors.New("请选择支持的 Word、Excel、PowerPoint 或 OpenDocument 文件"))
		return
	}
	page, e1 := strconv.Atoi(q.Get("page"))
	size, e2 := strconv.Atoi(q.Get("size"))
	if format == "page" && (e1 != nil || e2 != nil || page < 1 || page > 2000 || size < 256 || size > 4096) {
		fail(w, 400, errors.New("请选择有效的页面"))
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()
	key := [6]string{q.Get("path"), ext, q.Get("root"), q.Get("source"), q.Get("knowledgeOrigin"), q.Get("knowledgeWorkspace")}
	version := q.Get("version")
	var preview *officePreview
	var err error
	if version == "" {
		preview, err = a.newOfficePreview(ctx, r, key, ext)
	} else {
		preview = a.officePreviews.acquire(key, version)
		if preview == nil {
			err = dwgError(409, "Office 预览已过期，请重新打开。")
		}
	}
	if preview != nil {
		defer a.officePreviews.release(preview)
		if !preview.current() {
			err = dwgError(409, "文件来源已变化，请重新打开。")
		}
	}
	if err == nil {
		w.Header().Set("X-Aide-Office-Version", preview.version)
		path := filepath.Join(preview.dir, "input.pdf")
		switch format {
		case "meta":
			w.Header().Set("Content-Type", "application/json")
			w.Write(preview.meta)
			return
		case "pdf":
			var f *os.File
			f, err = os.Open(path)
			if err == nil {
				defer f.Close()
				w.Header().Set("Content-Type", "application/pdf")
				http.ServeContent(w, r, "preview.pdf", time.Time{}, f)
				return
			}
		case "page":
			var output []byte
			output, err = rasterPDFFile(ctx, path, page, size)
			if err == nil && !preview.current() {
				err = dwgError(409, "文件来源已变化，请重新打开。")
			}
			if err == nil {
				w.Header().Set("Content-Type", "image/png")
				w.Write(output)
				return
			}
		}
	}
	var pe *dwgPreviewError
	if errors.As(err, &pe) {
		fail(w, pe.status, pe)
	} else if ctx.Err() != nil {
		fail(w, 504, errors.New("Office 页面预览已超时或取消，请重试。"))
	} else {
		fail(w, 400, errors.New("无法读取 Office 文件，请检查目录权限、磁盘空间和来源连接。"))
	}
}
