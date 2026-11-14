package server

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

const dwgInputLimit = 16 << 20
const dwgOutputLimit = 4 << 20 // Same bound as the existing DXF browser parser.
const dwgPreviewTimeout = 20 * time.Second

var dwgPreviewSlots = make(chan struct{}, 2)

type dwgPreviewError struct {
	status  int
	message string
}

func (e *dwgPreviewError) Error() string        { return e.message }
func dwgError(status int, message string) error { return &dwgPreviewError{status, message} }

// The converter receives only a private copy, never a workspace path. Limits
// apply before exec and are inherited by the separate LibreDWG process.
const dwgLimitBootstrap = `import os, resource, sys
resource.setrlimit(resource.RLIMIT_AS, (512*1024*1024, 512*1024*1024))
resource.setrlimit(resource.RLIMIT_CPU, (15, 16))
resource.setrlimit(resource.RLIMIT_FSIZE, (4*1024*1024, 4*1024*1024))
resource.setrlimit(resource.RLIMIT_NOFILE, (64, 64))
resource.setrlimit(resource.RLIMIT_CORE, (0, 0))
os.execv(sys.argv[1], sys.argv[1:])
`

func dwgConverter() (string, error) {
	p := os.Getenv("AIDE_DWG_CONVERTER")
	if p == "" {
		p = "/opt/aide/dwg/bin/dwg2dxf"
	}
	resolved, err := exec.LookPath(p)
	if err != nil {
		return "", dwgError(503, "DWG 预览组件未安装，请更新标准 Aide 构建；也可下载原文件用 CAD 打开。")
	}
	return resolved, nil
}

func convertDWG(ctx context.Context, input []byte) ([]byte, error) {
	if len(input) > dwgInputLimit {
		return nil, dwgError(413, "DWG 文件超过 16 MiB 预览上限，请下载原文件用 CAD 打开。")
	}
	if len(input) < 24 || !bytes.HasPrefix(input, []byte("AC10")) || input[4] < '0' || input[4] > '9' || input[5] < '0' || input[5] > '9' {
		return nil, dwgError(422, "无法识别 DWG 文件，请确认文件完整且未加密；可下载原文件用 CAD 检查。")
	}
	converter, err := dwgConverter()
	if err != nil {
		return nil, err
	}
	python, err := exec.LookPath("python3")
	if err != nil {
		return nil, dwgError(503, "DWG 预览组件不完整，请更新标准 Aide 构建。")
	}
	dir, err := os.MkdirTemp("", "aide-dwg-preview-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir) // Only our private, newly created conversion directory.
	inPath, outPath := filepath.Join(dir, "input.dwg"), filepath.Join(dir, "preview.dxf")
	if err = os.WriteFile(inPath, input, 0600); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, dwgPreviewTimeout)
	defer cancel()
	// Preserve the source DXF version: forced version changes can silently lose entities.
	cmd := exec.CommandContext(ctx, python, "-c", dwgLimitBootstrap, converter, "-o", outPath, inPath)
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
		return nil, dwgError(504, "DWG 预览已超时或取消。可重试，或下载原文件用 CAD 打开。")
	}
	if err != nil {
		return nil, dwgError(422, "DWG 转换失败：文件可能损坏、包含不支持的内容或超过预览资源上限。可下载原文件用 CAD 打开。")
	}
	f, err := os.Open(outPath)
	if err != nil {
		return nil, dwgError(422, "DWG 未生成可显示的二维内容，请下载原文件用 CAD 打开。")
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > dwgOutputLimit {
		return nil, dwgError(413, "DWG 转换结果超过 4 MiB 预览上限，请下载原文件用 CAD 打开。")
	}
	output, err := io.ReadAll(io.LimitReader(f, dwgOutputLimit+1))
	if err != nil {
		return nil, err
	}
	trimmed := bytes.TrimSpace(output)
	if len(output) > dwgOutputLimit || !bytes.Contains(trimmed, []byte("SECTION")) || !bytes.HasSuffix(trimmed, []byte("EOF")) {
		return nil, dwgError(422, "DWG 未生成完整的二维预览，请下载原文件用 CAD 打开。")
	}
	return output, nil
}

// O_NONBLOCK prevents a replaced FIFO from blocking between Lstat and Open.
func readDWGLocal(root *os.Root, p string) ([]byte, error) {
	if root == nil || !knowledgeSafePath(p) {
		return nil, fmt.Errorf("文件路径无效或目录不可用")
	}
	parts := strings.Split(p, "/")
	for i := range parts {
		info, err := root.Lstat(strings.Join(parts[:i+1], "/"))
		if err != nil {
			return nil, err
		}
		if info.Mode()&os.ModeSymlink != 0 || (i == len(parts)-1 && !info.Mode().IsRegular()) {
			return nil, fmt.Errorf("DWG 预览仅支持普通文件，不读取符号链接")
		}
	}
	f, err := root.OpenFile(p, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return nil, fmt.Errorf("文件状态已变化，请重新打开")
	}
	if info.Size() > dwgInputLimit {
		return nil, dwgError(413, "DWG 文件超过 16 MiB 预览上限，请下载原文件用 CAD 打开。")
	}
	b, err := io.ReadAll(io.LimitReader(f, dwgInputLimit+1))
	if len(b) > dwgInputLimit {
		return nil, dwgError(413, "DWG 文件超过 16 MiB 预览上限，请下载原文件用 CAD 打开。")
	}
	return b, err
}

func (a *App) readDWGPreview(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	q := r.URL.Query()
	p := q.Get("path")
	if effectiveFileExt(p) != ".dwg" || !knowledgeSafePath(p) {
		fail(w, 400, fmt.Errorf("请选择有效的 DWG 文件"))
		return
	}
	select {
	case dwgPreviewSlots <- struct{}{}:
		defer func() { <-dwgPreviewSlots }()
	default:
		fail(w, 429, fmt.Errorf("DWG 预览正在处理其他图纸，请稍后重试。"))
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	var input []byte
	var err error
	var current func() bool
	if (q.Get("root") == "local" || strings.HasPrefix(q.Get("root"), "mount:")) && q.Get("source") == "" && q.Get("knowledgeOrigin") == "" && q.Get("knowledgeWorkspace") == "" {
		a.mu.Lock()
		root, rootErr := a.root(q.Get("root"))
		a.mu.Unlock()
		if rootErr != nil {
			err = rootErr
		} else {
			input, err = readDWGLocal(root, p)
		}
		current = func() bool {
			a.mu.Lock()
			defer a.mu.Unlock()
			now, e := a.root(q.Get("root"))
			return e == nil && root != nil && root == now
		}
	} else {
		var ar knowledgeArea
		ar, err = a.knowledgeViewerArea(q.Get("root"), q.Get("source"), q.Get("knowledgeOrigin"), q.Get("knowledgeWorkspace"))
		if err == nil && (ar.unavailableReason != "" || !a.knowledgeAreaCurrent(ar)) {
			err = dwgError(409, "文件来源已变化，请从当前目录重新打开图纸。")
		}
		if err == nil {
			if ar.local() {
				var root *os.Root
				root, err = a.openLocalSourceRoot(ctx, ar.location)
				if err == nil {
					input, err = readDWGLocal(root, p)
					root.Close()
				}
			} else {
				input, err = a.knowledgeAreaRead(ctx, ar, p, dwgInputLimit)
			}
		}
		current = func() bool { return a.knowledgeAreaCurrent(ar) }
	}
	if err == nil && !current() {
		err = dwgError(409, "文件来源已变化，请从当前目录重新打开图纸。")
	}
	var output []byte
	if err == nil {
		output, err = convertDWG(ctx, input)
	}
	if err == nil && !current() {
		err = dwgError(409, "文件来源已变化，请从当前目录重新打开图纸。")
	}
	if err != nil {
		var previewErr *dwgPreviewError
		if errors.As(err, &previewErr) {
			fail(w, previewErr.status, previewErr)
			return
		}
		fail(w, 400, fmt.Errorf("无法读取 DWG 文件，请确认目录权限、文件大小和来源连接后重试。"))
		return
	}
	w.Header().Set("Content-Type", "application/dxf")
	w.Write(output)
}
