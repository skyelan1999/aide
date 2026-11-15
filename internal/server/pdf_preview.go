package server

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"image/png"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
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
	cmd.Env, err = pdfFontEnvironment(dir)
	if err != nil {
		return nil, err
	}
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
	fontInfo := q.Get("format") == "fonts"
	page, e1 := strconv.Atoi(q.Get("page"))
	size, e2 := strconv.Atoi(q.Get("size"))
	if effectiveFileExt(q.Get("path")) != ".pdf" || !knowledgeSafePath(q.Get("path")) || (!fontInfo && (e1 != nil || e2 != nil || page < 1 || page > 2000 || size < 256 || size > 4096)) {
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
		if fontInfo {
			output, err = inspectPDFFonts(ctx, input)
		} else {
			output, err = rasterPDFPage(ctx, input, page, size)
		}
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
	if fontInfo {
		w.Header().Set("Content-Type", "application/json")
	} else {
		w.Header().Set("Content-Type", "image/png")
	}
	w.Write(output)
}

// Only a configured local font directory is added; PDF names never become paths.
func pdfFontEnvironment(dir string) ([]string, error) {
	env := []string{"PATH=/usr/bin:/bin", "LANG=C.UTF-8", "HOME=" + dir, "TMPDIR=" + dir, "XDG_CACHE_HOME=" + dir}
	fontDir := cadFontDirectory()
	if info, err := os.Stat(fontDir); err != nil || !info.IsDir() {
		return env, nil
	}
	var escaped bytes.Buffer
	if err := xml.EscapeText(&escaped, []byte(fontDir)); err != nil {
		return nil, err
	}
	config := `<?xml version="1.0"?><!DOCTYPE fontconfig SYSTEM "urn:fontconfig:fonts.dtd"><fontconfig><include ignore_missing="yes">/etc/fonts/fonts.conf</include><dir>` + escaped.String() + `</dir></fontconfig>`
	path := filepath.Join(dir, "fonts.conf")
	if err := os.WriteFile(path, []byte(config), 0600); err != nil {
		return nil, err
	}
	return append(env, "FONTCONFIG_FILE="+path), nil
}

type pdfFontMatch struct {
	Requested string `json:"requested"`
	Family    string `json:"family"`
	Available bool   `json:"available"`
}

type pdfFontInspection struct {
	EmbeddedFonts       int            `json:"embeddedFonts"`
	MissingFonts        []pdfFontMatch `json:"missingFonts"`
	PreferOriginalFonts bool           `json:"preferOriginalFonts"`
}

func parsePDFFontList(data []byte) (pdfFontInspection, error) {
	result := pdfFontInspection{MissingFonts: []pdfFontMatch{}}
	seen := map[string]bool{}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 8 {
			continue
		}
		embedded := fields[len(fields)-5]
		if embedded != "yes" && embedded != "no" {
			continue
		}
		if embedded == "yes" {
			result.EmbeddedFonts++
			continue
		}
		name := fields[0]

		if !seen[name] {
			result.MissingFonts = append(result.MissingFonts, pdfFontMatch{Requested: name})
			seen[name] = true
		}
		if len(result.MissingFonts) > 128 {
			return result, dwgError(413, "PDF 字体数量超过预览上限。")
		}
	}
	return result, nil
}

func canonicalPDFFont(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	if len(name) > 7 && name[6] == '+' {
		subset := true
		for _, char := range name[:6] {
			if char < 'a' || char > 'z' {
				subset = false
			}
		}
		if subset {
			name = name[7:]
		}
	}
	switch name {
	case "宋体":
		return "simsun"
	case "新宋体":
		return "nsimsun"
	case "仿宋", "仿宋_gb2312", "fangsong_gb2312", "simfang":
		return "fangsong"
	case "arialmt":
		return "arial"
	case "timesnewromanpsmt":
		return "times new roman"
	}
	return name
}

func runPDFFontTool(ctx context.Context, dir, tool string, args []string, capBytes int64) ([]byte, error) {
	program, err := exec.LookPath(tool)
	if err != nil {
		return nil, dwgError(503, "PDF 字体预览组件不可用。")
	}
	python, err := exec.LookPath("python3")
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, dwgPreviewTimeout)
	defer cancel()
	arguments := append([]string{"-c", dwgLimitBootstrap, fmt.Sprint(dwgOutputLimit), program}, args...)
	cmd := exec.CommandContext(ctx, python, arguments...)
	cmd.Dir = dir
	cmd.Env, err = pdfFontEnvironment(dir)
	if err != nil {
		return nil, err
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return os.ErrProcessDone
		}
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	cmd.WaitDelay = time.Second
	output := filepath.Join(dir, "font-output")
	file, err := os.OpenFile(output, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		return nil, err
	}
	cmd.Stdout, cmd.Stderr = file, io.Discard
	err = cmd.Run()
	file.Close()
	if ctx.Err() != nil {
		return nil, dwgError(504, "PDF 字体检查已超时或取消。")
	}
	if info, e := os.Stat(output); e == nil && info.Size() >= capBytes {
		return nil, dwgError(413, "PDF 字体信息超过预览上限。")
	}
	if err != nil {
		return nil, dwgError(422, "PDF 字体信息无法读取。")
	}
	data, err := os.ReadFile(output)
	if err != nil {
		return nil, err
	}
	if int64(len(data)) >= capBytes {
		return nil, dwgError(413, "PDF 字体信息超过预览上限。")
	}
	return data, nil
}

func inspectPDFFonts(ctx context.Context, input []byte) ([]byte, error) {
	if len(input) > dwgInputLimit || !bytes.HasPrefix(input, []byte("%PDF-")) {
		return nil, dwgError(400, "PDF 字体预览参数无效。")
	}
	dir, err := os.MkdirTemp("", "aide-pdf-fonts-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "input.pdf")
	if err = os.WriteFile(path, input, 0600); err != nil {
		return nil, err
	}
	data, err := runPDFFontTool(ctx, dir, "pdffonts", []string{path}, 64<<10)
	if err != nil {
		return nil, err
	}
	result, err := parsePDFFontList(data)
	if err != nil {
		return nil, err
	}
	all := len(result.MissingFonts) > 0
	for index, font := range result.MissingFonts {
		if len(font.Requested) > 128 || strings.ContainsAny(font.Requested, ":=\"\\") {
			all = false
			continue
		}
		family := canonicalPDFFont(font.Requested)
		match, matchErr := runPDFFontTool(ctx, dir, "fc-match", []string{"-f", "%{family}", ":family=" + family}, 4<<10)
		if matchErr != nil {
			all = false
			continue
		}
		font.Family = strings.TrimSpace(string(match))
		for _, name := range strings.Split(font.Family, ",") {
			if canonicalPDFFont(name) == family {
				font.Available = true
			}
		}
		result.MissingFonts[index] = font
		all = all && font.Available
	}
	result.PreferOriginalFonts = all
	return json.Marshal(result)
}
