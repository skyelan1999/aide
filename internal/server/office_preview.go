package server

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

//go:embed office_preview.py
var officePreviewScript string

var officePreviewSlots = make(chan struct{}, 2)

type officePreviewResult struct{ PDF, Meta []byte }

// Content-only, bounded cache. Every request still reads through the current
// authorized source and checks its identity before returning any cached bytes.
var officePreviewCache = struct {
	sync.Mutex
	keys   []string
	values map[string]officePreviewResult
}{values: make(map[string]officePreviewResult)}

func officePreviewFontKey() string {
	entries, _ := os.ReadDir(cadFontDirectory())
	var parts []string
	for _, entry := range entries {
		if info, err := entry.Info(); err == nil && info.Mode().IsRegular() {
			parts = append(parts, fmt.Sprintf("%s:%d:%d", entry.Name(), info.Size(), info.ModTime().UnixNano()))
		}
	}
	sort.Strings(parts)
	return cadFontDirectory() + strings.Join(parts, "|")
}

func convertOfficePreview(ctx context.Context, input []byte, ext string) (officePreviewResult, error) {
	var result officePreviewResult
	converter, err := exec.LookPath("soffice")
	if err != nil {
		return result, dwgError(503, "Office 页面预览组件未安装，请更新标准 Aide 构建。")
	}
	python, err := exec.LookPath("python3")
	if err != nil {
		return result, err
	}
	dir, err := os.MkdirTemp("", "aide-office-preview-*")
	if err != nil {
		return result, err
	}
	defer os.RemoveAll(dir)
	in := filepath.Join(dir, "input"+ext)
	if err = os.WriteFile(in, input, 0600); err != nil {
		return result, err
	}
	cmd := exec.CommandContext(ctx, python, "-c", officePreviewScript, converter, in, dir)
	cmd.Dir = dir
	cmd.Env, err = pdfFontEnvironment(dir)
	if err != nil {
		return result, err
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
		return result, dwgError(504, "Office 页面预览已超时或取消，请重试。")
	}
	if err != nil {
		return result, dwgError(422, "Office 文件无法生成页面预览，请检查文件是否损坏、加密或超过预览上限。")
	}
	read := func(name string, limit int64) ([]byte, error) {
		f, e := os.Open(filepath.Join(dir, name))
		if e != nil {
			return nil, e
		}
		defer f.Close()
		b, e := io.ReadAll(io.LimitReader(f, limit+1))
		if int64(len(b)) > limit {
			return nil, errors.New("preview output limit")
		}
		return b, e
	}
	result.PDF, err = read("input.pdf", 16<<20)
	if err != nil || !bytes.HasPrefix(result.PDF, []byte("%PDF-")) {
		return officePreviewResult{}, dwgError(422, "Office 页面预览无效。")
	}
	result.Meta, err = read("meta.json", 64<<10)
	var meta struct {
		Pages int `json:"pages"`
	}
	if err != nil || json.Unmarshal(result.Meta, &meta) != nil || meta.Pages < 1 || meta.Pages > 2000 {
		return officePreviewResult{}, dwgError(422, "Office 页面预览无效。")
	}
	return result, nil
}

var officePreviewConvert = convertOfficePreview

func (a *App) officePagePreview(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	q := r.URL.Query()
	ext := effectiveFileExt(q.Get("path"))
	format := q.Get("format")
	if q.Get("knowledgeOrigin") != "" && q.Get("knowledgeWorkspace") != "" && q.Get("source") != "" {
		a.mu.Lock()
		src, ok := a.findSource(q.Get("source"))
		a.mu.Unlock()
		hint := q.Get("officeFormat")
		if ok && src.Enabled && (src.Type == "link" || src.Type == "smb") && (hint == ".xlsx" || hint == ".ppt" || hint == ".pptx") {
			ext = hint
		}
	}
	if (ext != ".xlsx" && ext != ".ppt" && ext != ".pptx") || !knowledgeSafePath(q.Get("path")) || (format != "pdf" && format != "meta" && format != "page") {
		fail(w, 400, errors.New("请选择有效的 XLSX、PPT 或 PPTX 文件"))
		return
	}
	page, e1 := strconv.Atoi(q.Get("page"))
	size, e2 := strconv.Atoi(q.Get("size"))
	if format == "page" && (e1 != nil || e2 != nil || page < 1 || page > 2000 || size < 256 || size > 4096) {
		fail(w, 400, errors.New("请选择有效的页面"))
		return
	}
	select {
	case officePreviewSlots <- struct{}{}:
		defer func() { <-officePreviewSlots }()
	default:
		fail(w, 429, errors.New("Office 预览正在处理其他文件，请稍后重试。"))
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 75*time.Second)
	defer cancel()
	input, current, err := a.readBoundedPreviewInput(ctx, r)
	if err == nil && !current() {
		err = dwgError(409, "文件来源已变化，请重新打开。")
	}
	if err == nil {
		if ext == ".ppt" {
			if !bytes.HasPrefix(input, []byte{0xd0, 0xcf, 0x11, 0xe0, 0xa1, 0xb1, 0x1a, 0xe1}) {
				err = dwgError(400, "PPT 文件格式无效。")
			}
		} else {
			err = checkOfficeArchive(input)
		}
	}
	var result officePreviewResult
	if err == nil {
		key := hash(input) + ext + officePreviewFontKey()
		officePreviewCache.Lock()
		cached, ok := officePreviewCache.values[key]
		officePreviewCache.Unlock()
		if ok {
			result = cached
		} else {
			result, err = officePreviewConvert(ctx, input, ext)
			if err == nil && current() {
				officePreviewCache.Lock()
				if _, exists := officePreviewCache.values[key]; !exists {
					if len(officePreviewCache.keys) >= 4 {
						delete(officePreviewCache.values, officePreviewCache.keys[0])
						officePreviewCache.keys = officePreviewCache.keys[1:]
					}
					officePreviewCache.keys = append(officePreviewCache.keys, key)
					officePreviewCache.values[key] = result
				}
				officePreviewCache.Unlock()
			}
		}
	}
	var output []byte
	if err == nil {
		switch format {
		case "meta":
			output = result.Meta
			w.Header().Set("Content-Type", "application/json")
		case "pdf":
			output = result.PDF
			w.Header().Set("Content-Type", "application/pdf")
		case "page":
			output, err = rasterPDFPage(ctx, result.PDF, page, size)
			w.Header().Set("Content-Type", "image/png")
		}
	}
	if err == nil && !current() {
		err = dwgError(409, "文件来源已变化，请重新打开。")
	}
	if err != nil {
		var pe *dwgPreviewError
		if errors.As(err, &pe) {
			fail(w, pe.status, pe)
		} else {
			fail(w, 400, errors.New("无法读取 Office 文件，请检查文件大小、目录权限和来源连接。"))
		}
		return
	}
	w.Write(output)
}
