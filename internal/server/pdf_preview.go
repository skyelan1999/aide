package server

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image/png"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"time"
)

// Raster compatibility is confined to one page and a private copy. It shares
// the CAD concurrency/CPU/address-space/output limits, never runs in the server.
func rasterPDFPage(ctx context.Context, input []byte, page, size int) ([]byte, error) {
	if len(input) > dwgInputLimit || !bytes.HasPrefix(input, []byte("%PDF-")) || page < 1 || page > 2000 || size < 256 || size > 4096 {
		return nil, dwgError(400, "PDF 兼容预览参数无效或文件超过 16 MiB。")
	}
	converter, err := exec.LookPath("pdftoppm")
	if err != nil {
		return nil, dwgError(503, "PDF 兼容预览组件未安装，请更新标准 Aide 构建。")
	}
	python, err := exec.LookPath("python3")
	if err != nil {
		return nil, dwgError(503, "PDF 兼容预览组件不可用。")
	}
	dir, err := os.MkdirTemp("", "aide-pdf-preview-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	inPath, outPath := filepath.Join(dir, "input.pdf"), filepath.Join(dir, "page")
	if err = os.WriteFile(inPath, input, 0600); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, dwgPreviewTimeout)
	defer cancel()
	n := strconv.Itoa(page)
	cmd := exec.CommandContext(ctx, python, "-c", dwgLimitBootstrap, fmt.Sprint(dwgOutputLimit), converter, "-f", n, "-l", n, "-singlefile", "-scale-to", strconv.Itoa(size), "-png", inPath, outPath)
	cmd.Dir = dir
	cmd.Env = []string{"PATH=/usr/bin:/bin", "LANG=C.UTF-8", "HOME=" + dir, "TMPDIR=" + dir}
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
	if ctx.Err() != nil {
		return nil, dwgError(504, "PDF 兼容预览已超时或取消。")
	}
	if info, e := os.Stat(outPath + ".png"); e == nil && info.Size() >= dwgOutputLimit {
		return nil, dwgError(413, "PDF 页面超过兼容预览资源上限。")
	}
	if err != nil {
		return nil, dwgError(422, "PDF 页面无法生成兼容预览，请下载原文件查看。")
	}
	f, err := os.Open(outPath + ".png")
	if err != nil {
		return nil, dwgError(422, "PDF 未生成页面预览。")
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, dwgOutputLimit+1))
	if err != nil || len(b) > dwgOutputLimit {
		return nil, dwgError(413, "PDF 页面超过兼容预览资源上限。")
	}
	config, err := png.DecodeConfig(bytes.NewReader(b))
	if err != nil || config.Width < 1 || config.Height < 1 || config.Width > size || config.Height > size {
		return nil, dwgError(422, "PDF 页面预览无效。")
	}
	return b, nil
}

func (a *App) readPDFPreview(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	q := r.URL.Query()
	page, e1 := strconv.Atoi(q.Get("page"))
	size, e2 := strconv.Atoi(q.Get("size"))
	if effectiveFileExt(q.Get("path")) != ".pdf" || !knowledgeSafePath(q.Get("path")) || e1 != nil || e2 != nil || page < 1 || page > 2000 || size < 256 || size > 4096 {
		fail(w, 400, fmt.Errorf("请选择有效的 PDF 页面"))
		return
	}
	select {
	case dwgPreviewSlots <- struct{}{}:
		defer func() { <-dwgPreviewSlots }()
	default:
		fail(w, 429, fmt.Errorf("图纸预览正在处理其他页面，请稍后重试。"))
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	input, current, err := a.readBoundedPreviewInput(ctx, r)
	if err == nil && !current() {
		err = dwgError(409, "文件来源已变化，请重新打开。")
	}
	var output []byte
	if err == nil {
		output, err = rasterPDFPage(ctx, input, page, size)
	}
	if err == nil && !current() {
		err = dwgError(409, "文件来源已变化，请重新打开。")
	}
	if err != nil {
		var pe *dwgPreviewError
		if errors.As(err, &pe) {
			fail(w, pe.status, pe)
			return
		}
		fail(w, 400, fmt.Errorf("无法读取 PDF 文件，请确认文件大小、目录权限和来源连接。"))
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Write(output)
}
