package server

import (
	"archive/zip"
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"os/exec"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"
	"unicode/utf8"
)

// maxFile 为文本文件单次读/写的上限（UTF-8 文本，编辑器与模型工具共用）。
// 与 maxRawFile(64 MiB) 对齐：原始字节查看器能打开的文件，文本端点也应能读取。
// OOM 防护由两路构成：文件读取用 LimitReader(maxFile+1) 不越界；
// 校验为零拷贝（utf8.Valid 直接扫 []byte，NUL 用 bytes.IndexByte，不再 string(b) 复制一份）。
const maxFile = 64 << 20

// maxProposalTotal 为单次写文件提案（write_file 工具/方案解析）的内容合计上限。
// 远小于单文件上限：提案由模型逐次产出，主要防一次性灌入过大的多文件方案。
const maxProposalTotal = 8 << 20

func safePath(p string) error {
	if p == "" || strings.Contains(p, "\\") || strings.HasPrefix(p, "/") {
		return errors.New("需要工作区内的相对路径")
	}
	for _, part := range strings.Split(p, "/") {
		if part == ".." || part == ".git" || part == ".data" || part == ".aide-data" || part == ".env" || part == "docker-images" {
			return errors.New("路径不可访问")
		}
	}
	return nil
}
func hash(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }

// validateTextContent 统一内容策略（R03）：maxFile 上限、UTF-8、无 NUL。
// 入参均为已落地的 []byte/string 缓冲（HTTP 已解码、文件已读入），此处做零拷贝校验，
// 不再 string(b) 复制一份大缓冲。
func validateTextContent(b []byte) error {
	if len(b) > maxFile {
		return fmt.Errorf("文件超过 %d MiB 限制", maxFile>>20)
	}
	if !utf8.Valid(b) || bytes.IndexByte(b, 0) >= 0 {
		return errors.New("不支持二进制文件")
	}
	return nil
}

// normalizeJSONEncoding accepts BOM-marked UTF-16 JSON as text. JSON is
// ordinarily UTF-8, but Windows tools can emit UTF-16LE/BE; the NUL bytes in
// those files must not make the editor classify them as binary. The returned
// UTF-8 bytes are the canonical editor representation (saving such a file
// writes UTF-8, matching the rest of the text-file contract).
func normalizeJSONEncoding(p string, b []byte) ([]byte, error) {
	ext := strings.ToLower(path.Ext(p))
	if ext != ".json" && ext != ".jsonl" && ext != ".jsonc" {
		return b, nil
	}
	var order binary.ByteOrder
	data := b
	switch {
	case bytes.HasPrefix(b, []byte{0xff, 0xfe}):
		order, data = binary.LittleEndian, b[2:]
	case bytes.HasPrefix(b, []byte{0xfe, 0xff}):
		order, data = binary.BigEndian, b[2:]
	default:
		return b, nil
	}
	if len(data)%2 != 0 {
		return nil, errors.New("JSON 文件的 UTF-16 编码不完整")
	}
	units := make([]uint16, len(data)/2)
	for i := range units {
		units[i] = order.Uint16(data[i*2 : i*2+2])
	}
	for i := 0; i < len(units); i++ {
		u := units[i]
		if u >= 0xd800 && u <= 0xdbff {
			if i+1 >= len(units) || units[i+1] < 0xdc00 || units[i+1] > 0xdfff {
				return nil, errors.New("JSON 文件包含无效的 UTF-16 字符")
			}
			i++
		} else if u >= 0xdc00 && u <= 0xdfff {
			return nil, errors.New("JSON 文件包含无效的 UTF-16 字符")
		}
	}
	decoded := []byte(string(utf16.Decode(units)))
	if !utf8.Valid(decoded) || bytes.IndexByte(decoded, 0) >= 0 {
		return nil, errors.New("JSON 文件包含无效文本")
	}
	return decoded, nil
}

func validateTextFileContent(p string, b []byte) ([]byte, error) {
	b, err := normalizeJSONEncoding(p, b)
	if err != nil {
		return nil, err
	}
	if err := validateTextContent(b); err != nil {
		return nil, err
	}
	return b, nil
}

func readText(root *os.Root, p string) ([]byte, error) {
	return readTextWithOpener(p, root.Open)
}

func readTextWithOpener(p string, openFile func(string) (*os.File, error)) ([]byte, error) {
	if err := safePath(p); err != nil {
		return nil, err
	}
	f, err := openFile(p)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("只支持普通文本文件")
	}
	b, err := io.ReadAll(io.LimitReader(f, maxFile+1))
	if err != nil {
		return nil, err
	}
	if len(b) > maxFile {
		return nil, fmt.Errorf("文件超过 %d MiB 限制", maxFile>>20)
	}
	return validateTextFileContent(p, b)
}

// readTextLines 按行流式读取（模型 read_file 工具的分段模式）。
// offset 为 0 起始行号，limit 为最多返回行数（<=0 表示读到 EOF）。
// 全程只缓冲目标窗口，逐行扫描时同时检测整个文件是否含 NUL（二进制拒绝），
// 对返回窗口做 UTF-8 校验。返回：窗口内容、文件总行数。
func readTextLines(root *os.Root, p string, offset, limit int) ([]byte, int, error) {
	return readTextLinesWithOpener(p, offset, limit, root.Open)
}

func readTextLinesWithOpener(p string, offset, limit int, openFile func(string) (*os.File, error)) ([]byte, int, error) {
	if ext := strings.ToLower(path.Ext(p)); ext == ".json" || ext == ".jsonl" || ext == ".jsonc" {
		b, err := readTextWithOpener(p, openFile)
		if err != nil {
			return nil, 0, err
		}
		lines := bytes.Split(b, []byte{'\n'})
		if len(lines) > 0 && len(lines[len(lines)-1]) == 0 && len(b) > 0 && b[len(b)-1] == '\n' {
			lines = lines[:len(lines)-1]
		}
		start := max(0, offset)
		if start > len(lines) {
			start = len(lines)
		}
		end := len(lines)
		if limit > 0 && start+limit < end {
			end = start + limit
		}
		return bytes.Join(lines[start:end], []byte{'\n'}), len(lines), nil
	}
	if err := safePath(p); err != nil {
		return nil, 0, err
	}
	f, err := openFile(p)
	if err != nil {
		return nil, 0, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, 0, err
	}
	if !info.Mode().IsRegular() {
		return nil, 0, errors.New("只支持普通文本文件")
	}
	if info.Size() > maxFile {
		return nil, 0, fmt.Errorf("文件超过 %d MiB 限制", maxFile>>20)
	}
	if offset < 0 {
		offset = 0
	}
	br := bufio.NewReaderSize(f, 64<<10)
	total := 0
	var out bytes.Buffer
	sawNUL := false
	for {
		line, rerr := br.ReadBytes('\n')
		if len(line) > 0 {
			total++
			if bytes.IndexByte(line, 0) >= 0 {
				sawNUL = true
			}
			if total > offset && (limit <= 0 || total-offset <= limit) {
				out.Write(line)
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			return nil, 0, rerr
		}
	}
	if sawNUL {
		return nil, 0, errors.New("不支持二进制文件")
	}
	if out.Len() > 0 && !utf8.Valid(out.Bytes()) {
		return nil, 0, errors.New("不支持二进制文件")
	}
	return out.Bytes(), total, nil
}

// readTextRange 按字节区间读取（GET /api/file 的懒加载分段模式）。
// off/limit 为字节偏移与长度（limit<=0 表示读到文件尾）。
// 返回窗口内容与文件总字节数；对窗口做 NUL/UTF-8 校验。
func readTextRange(root *os.Root, p string, off, limit int64) ([]byte, int64, error) {
	if ext := strings.ToLower(path.Ext(p)); ext == ".json" || ext == ".jsonl" || ext == ".jsonc" {
		b, err := readText(root, p)
		if err != nil {
			return nil, 0, err
		}
		size := int64(len(b))
		if off < 0 {
			off = 0
		}
		if off > size {
			off = size
		}
		end := size
		if limit > 0 && off+limit < end {
			end = off + limit
		}
		for off > 0 && off < size && !utf8.RuneStart(b[off]) {
			off++
		}
		for end < size && !utf8.RuneStart(b[end]) {
			end++
		}
		return b[off:end], size, nil
	}
	if err := safePath(p); err != nil {
		return nil, 0, err
	}
	f, err := root.Open(p)
	if err != nil {
		return nil, 0, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, 0, err
	}
	if !info.Mode().IsRegular() {
		return nil, 0, errors.New("只支持普通文本文件")
	}
	size := info.Size()
	if size > maxFile {
		return nil, 0, fmt.Errorf("文件超过 %d MiB 限制", maxFile>>20)
	}
	if off < 0 {
		off = 0
	}
	if off > size {
		off = size
	}
	if limit <= 0 || off+limit > size {
		limit = size - off
	}
	b := make([]byte, limit)
	n, rerr := f.ReadAt(b, off)
	if rerr != nil && rerr != io.EOF {
		return nil, 0, rerr
	}
	b = b[:n]
	if bytes.IndexByte(b, 0) >= 0 {
		return nil, 0, errors.New("不支持二进制文件")
	}
	if !utf8.Valid(b) {
		return nil, 0, errors.New("不支持二进制文件")
	}
	return b, size, nil
}

// maxRawFile 为原始字节端点（图片/STL 等二进制查看器）的上限。
const maxRawFile = 64 << 20

const (
	maxArchiveFiles = 1000
	maxArchiveDepth = 32
)

// readRawBytes 返回文件的原始字节，不做 UTF-8/文本校验（供 /api/file/raw 查看器使用）。
func readRawBytes(root *os.Root, p string) ([]byte, error) {
	return readRawBytesWithOpener(p, root.Open)
}

func readRawBytesWithOpener(p string, openFile func(string) (*os.File, error)) ([]byte, error) {
	if err := safePath(p); err != nil {
		return nil, err
	}
	f, err := openFile(p)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("只支持普通文件")
	}
	b, err := io.ReadAll(io.LimitReader(f, maxRawFile+1))
	if err != nil {
		return nil, err
	}
	if len(b) > maxRawFile {
		return nil, fmt.Errorf("文件超过 %d MB 限制", maxRawFile/(1<<20))
	}
	return b, nil
}

type archiveItem struct {
	name string
	data []byte
	dir  bool
}

func validArchivePath(p string) error {
	if p == "" || p == "." || path.Clean(p) != p {
		return errors.New("需要工作区内的文件或文件夹")
	}
	return safePath(p)
}

func archiveContentType(p string) string {
	if ct := mime.TypeByExtension(strings.ToLower(path.Ext(p))); ct != "" {
		return ct
	}
	return "application/octet-stream"
}

func archiveName(p string) string {
	name := path.Base(p)
	if name == "." || name == "/" || name == "" {
		return "download"
	}
	return name
}

func (a *App) sourceEntry(src Source, p string) (map[string]any, error) {
	items, err := a.listSourceDir(src, path.Dir(p))
	if err != nil {
		return nil, err
	}
	for _, item := range items {
		if item["name"] == path.Base(p) {
			return item, nil
		}
	}
	return nil, os.ErrNotExist
}

func checkedRaw(b []byte) ([]byte, error) {
	if len(b) > maxRawFile {
		return nil, fmt.Errorf("文件超过 %d MB 限制", maxRawFile/(1<<20))
	}
	return b, nil
}

// effectiveFileExt recognizes a backup by the type of the original file while
// retaining the actual .bak path for all reads and writes. Only one suffix is
// removed, matching the browser's preview routing.
func effectiveFileExt(p string) string {
	p = strings.ToLower(p)
	if !strings.HasSuffix(p, ".bak") {
		return path.Ext(p)
	}
	p = strings.TrimSuffix(p, ".bak")
	p = backupVersionSuffix.ReplaceAllString(p, "")
	return path.Ext(p)
}

var backupVersionSuffix = regexp.MustCompile(`(?:\.v\d+(?:\.\d+)*|\.pre-[^.]+)+$`)

func (a *App) downloadTarget(r *http.Request) (string, Source, bool, bool, error) {
	p := r.URL.Query().Get("path")
	if err := validArchivePath(p); err != nil {
		return "", Source{}, false, false, err
	}
	if sourceID := r.URL.Query().Get("source"); sourceID != "" {
		a.mu.Lock()
		src, ok := a.findSource(sourceID)
		a.mu.Unlock()
		if !ok || !src.Enabled {
			return "", Source{}, false, false, errors.New("来源不存在或已停用")
		}
		item, err := a.sourceEntry(src, p)
		if err != nil {
			return "", Source{}, false, false, err
		}
		dir, _ := item["dir"].(bool)
		return p, src, true, dir, nil
	}
	if r.URL.Query().Get("root") != "workspace" {
		return "", Source{}, false, false, errors.New("仅工作目录或引用支持下载")
	}
	info, err := a.workspaceFileProperties(p)
	if err != nil {
		return "", Source{}, false, false, err
	}
	dir, _ := info["dir"].(bool)
	return p, Source{}, false, dir, nil
}

func (a *App) downloadRaw(p string, src Source, hasSource bool) ([]byte, error) {
	var b []byte
	var err error
	if hasSource && src.Type == "mcp" {
		b, err = a.readSourceText(src, p)
	} else if hasSource {
		b, err = a.readSourceRaw(src, p)
	} else if a.workspaceMode() == "ssh" {
		b, err = a.sftpRead(a.workspaceRemotePath(p))
	} else {
		b, err = readRawBytes(a.workspace, p)
	}
	if err != nil {
		return nil, err
	}
	return checkedRaw(b)
}

func (a *App) downloadChildren(p string, src Source, hasSource bool) ([]map[string]any, error) {
	if hasSource {
		return a.listSourceDir(src, p)
	}
	return a.listWorkspaceDir(p)
}

func (a *App) collectArchive(p string, src Source, hasSource, dir bool) ([]archiveItem, error) {
	items := make([]archiveItem, 0)
	total := 0
	var walk func(string, string, bool, int) error
	walk = func(actual, entry string, isDir bool, depth int) error {
		if depth > maxArchiveDepth {
			return errors.New("压缩目录层级超过限制")
		}
		if isDir {
			items = append(items, archiveItem{name: strings.TrimSuffix(entry, "/") + "/", dir: true})
			children, err := a.downloadChildren(actual, src, hasSource)
			if err != nil {
				return err
			}
			for _, child := range children {
				childPath, ok := child["path"].(string)
				if !ok || safePath(childPath) != nil {
					return errors.New("目录包含无效路径")
				}
				childName, _ := child["name"].(string)
				childDir, _ := child["dir"].(bool)
				if err := walk(childPath, path.Join(entry, childName), childDir, depth+1); err != nil {
					return err
				}
			}
			return nil
		}
		if len(items) >= maxArchiveFiles {
			return fmt.Errorf("压缩包最多包含 %d 个条目", maxArchiveFiles)
		}
		b, err := a.downloadRaw(actual, src, hasSource)
		if err != nil {
			return err
		}
		total += len(b)
		if total > maxRawFile {
			return fmt.Errorf("压缩内容超过 %d MB 限制", maxRawFile/(1<<20))
		}
		items = append(items, archiveItem{name: entry, data: b})
		return nil
	}
	if err := walk(p, archiveName(p), dir, 0); err != nil {
		return nil, err
	}
	if len(items) > maxArchiveFiles {
		return nil, fmt.Errorf("压缩包最多包含 %d 个条目", maxArchiveFiles)
	}
	return items, nil
}

func (a *App) downloadFile(w http.ResponseWriter, r *http.Request) {
	if !a.transferSourceStreaming(r.URL.Query().Get("source")) {
		a.legacyReferenceDownload(w, r)
		return
	}
	p, _, _, dir, err := a.downloadTarget(r)
	if err != nil {
		fail(w, 400, err)
		return
	}
	if dir || r.URL.Query().Get("archive") == "1" {
		a.legacyLargeArchive(w, r, p)
		return
	}
	a.streamFileDownload(w, r)
}

func (a *App) legacyReferenceDownload(w http.ResponseWriter, r *http.Request) {
	p, src, hasSource, dir, err := a.downloadTarget(r)
	if err != nil {
		fail(w, 400, err)
		return
	}
	archive := r.URL.Query().Get("archive") == "1" || dir
	name := archiveName(p)
	var body []byte
	if archive {
		items, err := a.collectArchive(p, src, hasSource, dir)
		if err != nil {
			fail(w, 400, err)
			return
		}
		var buf bytes.Buffer
		zw := zip.NewWriter(&buf)
		for _, item := range items {
			hdr := &zip.FileHeader{Name: item.name, Method: zip.Deflate}
			if item.dir {
				hdr.SetMode(os.ModeDir | 0755)
			}
			out, e := zw.CreateHeader(hdr)
			if e != nil {
				fail(w, 500, e)
				return
			}
			if !item.dir {
				if _, e = out.Write(item.data); e != nil {
					fail(w, 500, e)
					return
				}
			}
		}
		if err := zw.Close(); err != nil {
			fail(w, 500, err)
			return
		}
		body = buf.Bytes()
		name += ".zip"
		w.Header().Set("Content-Type", "application/zip")
	} else {
		body, err = a.downloadRaw(p, src, hasSource)
		if err != nil {
			fail(w, 400, err)
			return
		}
		w.Header().Set("Content-Type", archiveContentType(p))
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": name}))
	w.Write(body)
}

func encodeArchive(items []archiveItem) ([]byte, error) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, item := range items {
		hdr := &zip.FileHeader{Name: item.name, Method: zip.Deflate}
		if item.dir {
			hdr.SetMode(os.ModeDir | 0755)
		}
		out, err := zw.CreateHeader(hdr)
		if err != nil {
			return nil, err
		}
		if !item.dir {
			if _, err := out.Write(item.data); err != nil {
				return nil, err
			}
		}
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// createWorkspaceArchive writes the generated ZIP alongside its source instead of returning it as a download.
func (a *App) createWorkspaceArchive(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Root   string `json:"root"`
		Source string `json:"source"`
		Path   string `json:"path"`
		Format string `json:"format"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		fail(w, 400, errors.New("请求格式错误"))
		return
	}
	if body.Source != "" {
		a.sourceFileActionFormat(w, body.Root, body.Source, body.Path, "archive", "", body.Format)
		return
	}
	if body.Root != "workspace" || validArchivePath(body.Path) != nil {
		fail(w, 400, errors.New("需要工作目录中的文件或文件夹"))
		return
	}
	a.filesMu.Lock()
	defer a.filesMu.Unlock()
	info, err := a.workspaceFileProperties(body.Path)
	if err != nil {
		fail(w, 400, err)
		return
	}
	dir, _ := info["dir"].(bool)
	items, err := a.collectArchive(body.Path, Source{}, false, dir)
	if err != nil {
		fail(w, 400, err)
		return
	}
	format, err := normalizeArchiveFormat(body.Format)
	if err != nil {
		fail(w, 400, err)
		return
	}
	data, err := encodeArchiveFormat(items, format)
	if err != nil {
		fail(w, 500, err)
		return
	}
	dest := archiveNameForFormat(body.Path, format)
	if a.workspaceMode() == "ssh" {
		remote := a.workspaceRemotePath(dest)
		if a.sftpExists(remote) {
			fail(w, 409, errors.New("压缩目标已存在"))
			return
		}
		if err := a.sftpWrite(remote, data); err != nil {
			fail(w, 500, err)
			return
		}
	} else {
		f, err := a.workspace.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
		if err != nil {
			if errors.Is(err, os.ErrExist) {
				fail(w, 409, errors.New("压缩目标已存在"))
			} else {
				fail(w, 400, err)
			}
			return
		}
		if _, err := f.Write(data); err != nil {
			f.Close()
			_ = a.workspace.Remove(dest)
			fail(w, 500, err)
			return
		}
		if err := f.Close(); err != nil {
			_ = a.workspace.Remove(dest)
			fail(w, 500, err)
			return
		}
	}
	jsonOut(w, 200, map[string]string{"path": dest})
}

type extractedArchiveFile struct {
	path string
	data []byte
	dir  bool
}

func cleanArchiveEntryPath(raw string, isDir bool) (string, error) {
	if strings.ContainsAny(raw, "\x00\r\n") {
		return "", errors.New("ZIP 包含不安全路径")
	}
	// ZIP names should use '/', but archives produced on Windows sometimes use
	// '\\' or prefix entries with './'. Normalize those harmless forms before
	// applying traversal checks; never let a drive, rooted, or parent path through.
	name := strings.ReplaceAll(raw, "\\", "/")
	if strings.HasPrefix(name, "/") {
		return "", errors.New("ZIP 包含不安全路径")
	}
	first := strings.SplitN(name, "/", 2)[0]
	if len(first) >= 2 && ((first[0] >= 'A' && first[0] <= 'Z') || (first[0] >= 'a' && first[0] <= 'z')) && first[1] == ':' {
		return "", errors.New("ZIP 包含不安全路径")
	}
	parts := strings.Split(name, "/")
	cleanParts := make([]string, 0, len(parts))
	for _, part := range parts {
		switch part {
		case "", ".":
			continue
		case "..":
			return "", errors.New("ZIP 包含不安全路径")
		default:
			cleanParts = append(cleanParts, part)
		}
	}
	name = strings.Join(cleanParts, "/")
	if name == "" && isDir {
		return "", nil // Ignore an archive's harmless explicit current-directory entry.
	}
	if name == "" || path.Clean(name) != name || safePath(name) != nil {
		return "", errors.New("ZIP 包含不安全路径")
	}
	return name, nil
}

func availableArchiveDestination(base string, exists func(string) bool) (string, error) {
	if !exists(base) {
		return base, nil
	}
	parent, name := path.Dir(base), path.Base(base)
	for suffix := 2; suffix <= 10000; suffix++ {
		candidate := path.Join(parent, name+" ("+strconv.Itoa(suffix)+")")
		if !exists(candidate) {
			return candidate, nil
		}
	}
	return "", errors.New("找不到可用的解压目标目录")
}

func readArchiveFiles(data []byte) ([]extractedArchiveFile, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, errors.New("不是有效的 ZIP 文件")
	}
	if len(zr.File) == 0 {
		return nil, errors.New("ZIP 文件为空")
	}
	if len(zr.File) > maxArchiveFiles {
		return nil, fmt.Errorf("ZIP 最多包含 %d 个条目", maxArchiveFiles)
	}
	files := make([]extractedArchiveFile, 0, len(zr.File))
	seen := map[string]bool{}
	total := 0
	for _, entry := range zr.File {
		isDir := entry.FileInfo().IsDir() || strings.HasSuffix(entry.Name, "/") || strings.HasSuffix(entry.Name, "\\")
		name, pathErr := cleanArchiveEntryPath(entry.Name, isDir)
		if pathErr != nil {
			return nil, pathErr
		}
		if name == "" && isDir {
			continue
		}
		if seen[name] {
			return nil, errors.New("ZIP 包含重复路径")
		}
		seen[name] = true
		if entry.Mode()&os.ModeSymlink != 0 {
			return nil, errors.New("ZIP 不支持符号链接")
		}
		f := extractedArchiveFile{path: name, dir: isDir}
		if !isDir {
			if entry.UncompressedSize64 > uint64(maxRawFile-total) {
				return nil, fmt.Errorf("解压内容超过 %d MB 限制", maxRawFile/(1<<20))
			}
			rc, e := entry.Open()
			if e != nil {
				return nil, e
			}
			f.data, e = io.ReadAll(io.LimitReader(rc, int64(maxRawFile-total)+1))
			rc.Close()
			if e != nil {
				return nil, e
			}
			total += len(f.data)
			if total > maxRawFile {
				return nil, fmt.Errorf("解压内容超过 %d MB 限制", maxRawFile/(1<<20))
			}
		}
		files = append(files, f)
	}
	if err := validateArchiveLayout(files); err != nil {
		return nil, err
	}
	return files, nil
}

func (a *App) extractArchive(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Root   string `json:"root"`
		Source string `json:"source"`
		Path   string `json:"path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		fail(w, 400, errors.New("请求格式错误"))
		return
	}
	if body.Source != "" {
		a.sourceFileAction(w, body.Root, body.Source, body.Path, "extract", "")
		return
	}
	if body.Root != "workspace" || validArchivePath(body.Path) != nil || archiveExtension(body.Path) == "" {
		fail(w, 400, errors.New("需要工作目录中的受支持归档文件"))
		return
	}
	b, err := a.downloadRaw(body.Path, Source{}, false)
	if err != nil {
		fail(w, 400, err)
		return
	}
	files, err := readArchiveFilesForPath(b, body.Path)
	if err != nil {
		fail(w, 400, err)
		return
	}
	destBase := path.Join(path.Dir(body.Path), archiveDestinationName(body.Path))
	if err := validArchivePath(destBase); err != nil {
		fail(w, 400, err)
		return
	}
	a.filesMu.Lock()
	defer a.filesMu.Unlock()
	fs, err := a.streamTransferFS(r.Context(), "workspace", true)
	if err != nil {
		fail(w, 400, err)
		return
	}
	defer fs.Close()
	var lookupErr error
	dest, err := availableArchiveDestination(destBase, func(candidate string) bool {
		if lookupErr != nil {
			return true
		}
		_, statErr := fs.Stat(candidate)
		if statErr != nil && !os.IsNotExist(statErr) {
			lookupErr = statErr
		}
		return statErr == nil
	})
	if lookupErr != nil {
		fail(w, 400, lookupErr)
		return
	}
	if err != nil {
		fail(w, 409, err)
		return
	}
	if err = writeExtractedArchive(fs, dest, files); err != nil {
		status := 500
		if fs.root == nil {
			status = 400
		}
		if errors.Is(err, os.ErrExist) {
			status = 409
		}
		fail(w, status, err)
		return
	}
	jsonOut(w, 200, map[string]string{"path": dest})
}
func (a *App) root(which string) (*os.Root, error) {
	if strings.HasPrefix(which, "mount:") {
		return a.mountRoot(which)
	}
	if which == "" || which == "workspace" {
		return a.workspace, nil
	}
	if which == "context" {
		return a.reference, nil
	}
	if which == "local" {
		return a.localRoot, nil
	}
	return nil, errors.New("未知根目录")
}
func (a *App) listFiles(w http.ResponseWriter, r *http.Request) {
	p := r.URL.Query().Get("path")
	if p == "" {
		p = "."
	}
	search, err := fileSearchFromRequest(r)
	if err != nil {
		fail(w, 400, err)
		return
	}
	which := r.URL.Query().Get("root")
	if which == "remote" {
		if a.workspaceMode() != "ssh" {
			fail(w, 400, errors.New("仅 SSH/SFTP 工作空间可以浏览远程目录"))
			return
		}
		if err := validateRemoteBrowsePath(p); err != nil {
			fail(w, 400, err)
			return
		}
		items, trunc, err := searchFileTree(p, search, func(dir string) ([]map[string]any, error) { return a.sftpListRemote(dir, dir) })
		if err != nil {
			fail(w, 400, err)
			return
		}
		setSearchTruncationHeaders(w, trunc)
		jsonOut(w, 200, items)
		return
	}
	if err := safePath(p); err != nil {
		fail(w, 400, err)
		return
	}
	if srcID := r.URL.Query().Get("source"); srcID != "" {
		a.mu.Lock()
		src, ok := a.findSource(srcID)
		a.mu.Unlock()
		if !ok || !src.Enabled {
			fail(w, 400, errors.New("来源不存在或已停用"))
			return
		}
		items, trunc, err := searchFileTree(p, search, func(dir string) ([]map[string]any, error) { return a.listSourceDir(src, dir) })
		if err != nil {
			fail(w, 400, err)
			return
		}
		setSearchTruncationHeaders(w, trunc)
		jsonOut(w, 200, items)
		return
	}
	if which == "workspace" && a.workspaceMode() == "ssh" {
		items, trunc, err := searchFileTree(p, search, a.listWorkspaceDir)
		if err != nil {
			fail(w, 400, err)
			return
		}
		setSearchTruncationHeaders(w, trunc)
		jsonOut(w, 200, items)
		return
	}
	root, err := a.root(which)
	if err != nil {
		fail(w, 400, err)
		return
	}
	items, trunc, err := searchFileTree(p, search, func(dir string) ([]map[string]any, error) { return a.listLocalDir(root, dir) })
	if err != nil {
		fail(w, 400, err)
		return
	}
	setSearchTruncationHeaders(w, trunc)
	jsonOut(w, 200, items)
}

const (
	maxFileSearchResults = 500
	maxFileSearchDirs    = 200
)

type fileSearch struct {
	term      string
	recursive bool
	exact     bool
}

func fileSearchFromRequest(r *http.Request) (fileSearch, error) {
	term := strings.TrimSpace(r.URL.Query().Get("search"))
	if term == "" {
		return fileSearch{}, nil
	}
	if len([]rune(term)) > 256 || strings.ContainsAny(term, "\x00\r\n") {
		return fileSearch{}, errors.New("搜索词无效")
	}
	scope := r.URL.Query().Get("scope")
	if scope != "" && scope != "folder" && scope != "recursive" {
		return fileSearch{}, errors.New("搜索范围无效")
	}
	match := r.URL.Query().Get("match")
	if match != "" && match != "fuzzy" && match != "exact" {
		return fileSearch{}, errors.New("匹配方式无效")
	}
	return fileSearch{term: term, recursive: scope == "recursive", exact: match == "exact"}, nil
}

// searchFileTree keeps recursive search inside the already selected safe root.
// It deliberately caps traversed folders and returned rows so a broad search on
// an SSH/SFTP workspace cannot make the file panel unresponsive.
// fileSearchTruncation reports which search caps were hit. It is carried alongside
// the (unchanged) JSON array so the HTTP layer can signal truncation via headers
// without breaking consumers that expect a bare array body.
type fileSearchTruncation struct {
	dirsHit    bool // 达到 maxFileSearchDirs 但仍有未遍历的子目录
	resultsHit bool // 达到 maxFileSearchResults 结果上限
}

func (t fileSearchTruncation) any() bool { return t.dirsHit || t.resultsHit }

// setSearchTruncationHeaders emits the truncation signal only when a cap was hit.
// The body stays a bare JSON array; these headers let the file panel show a notice.
func setSearchTruncationHeaders(w http.ResponseWriter, trunc fileSearchTruncation) {
	if !trunc.any() {
		return
	}
	w.Header().Set("X-Search-Truncated", "1")
	w.Header().Set("X-Search-Dir-Limit", strconv.Itoa(maxFileSearchDirs))
	w.Header().Set("X-Search-Result-Limit", strconv.Itoa(maxFileSearchResults))
}

func searchFileTree(base string, search fileSearch, list func(string) ([]map[string]any, error)) ([]map[string]any, fileSearchTruncation, error) {
	var trunc fileSearchTruncation
	if search.term == "" {
		items, err := list(base)
		return items, trunc, err
	}
	queue := []string{base}
	seen := map[string]bool{base: true}
	items := make([]map[string]any, 0)
	for len(queue) > 0 && len(seen) <= maxFileSearchDirs && len(items) < maxFileSearchResults {
		dir := queue[0]
		queue = queue[1:]
		entries, err := list(dir)
		if err != nil {
			return nil, trunc, err
		}
		for _, entry := range entries {
			name, _ := entry["name"].(string)
			entryPath, _ := entry["path"].(string)
			isDir, _ := entry["dir"].(bool)
			if fileNameMatches(name, search) {
				items = append(items, entry)
				if len(items) >= maxFileSearchResults {
					trunc.resultsHit = true
					break
				}
			}
			if search.recursive && isDir && entryPath != "" && !seen[entryPath] {
				if len(seen) < maxFileSearchDirs {
					seen[entryPath] = true
					queue = append(queue, entryPath)
				} else {
					trunc.dirsHit = true
				}
			}
		}
	}
	return items, trunc, nil
}

func fileNameMatches(name string, search fileSearch) bool {
	if search.exact {
		return strings.EqualFold(name, search.term)
	}
	return strings.Contains(strings.ToLower(name), strings.ToLower(search.term))
}

// validateRemoteBrowsePath keeps a directory-picker path safe for an SFTP
// batch command. Unlike workspace file paths, absolute paths and ".." are
// allowed here: choosing the workspace root must be able to navigate anywhere
// the authenticated remote account itself may access.
func validateRemoteBrowsePath(p string) error {
	if strings.ContainsAny(p, "\x00\r\n") {
		return errors.New("远程目录不能包含反斜杠、换行或空字符")
	}
	p = strings.TrimSpace(p)
	if p == "" {
		return errors.New("远程目录不能为空")
	}
	if strings.Contains(p, "\\") {
		return errors.New("远程目录不能包含反斜杠、换行或空字符")
	}
	return nil
}

type directoryRequest struct {
	Root    string `json:"root"`
	Source  string `json:"source,omitempty"`
	Path    string `json:"path"`
	Parent  string `json:"parentPath"`
	Name    string `json:"name"`
	NewName string `json:"newName"`
}

func validDirectoryName(name string) (string, error) {
	if strings.ContainsAny(name, "\x00\r\n") {
		return "", errors.New("文件夹名称无效")
	}
	name = strings.TrimSpace(name)
	if name == "" || name == "." || name == ".." || strings.ContainsAny(name, `/\\`) {
		return "", errors.New("文件夹名称无效")
	}
	return name, nil
}

func directoryPath(root, p string) error {
	if root == "remote" {
		return validateRemoteBrowsePath(p)
	}
	return safePath(p)
}

func (a *App) remoteDirectory(remotePath string) (bool, error) {
	items, err := a.sftpListRemote(path.Dir(remotePath), path.Dir(remotePath))
	if err != nil {
		return false, err
	}
	name := path.Base(remotePath)
	for _, item := range items {
		if item["name"] == name {
			dir, _ := item["dir"].(bool)
			return dir, nil
		}
	}
	return false, nil
}

func fileInfoPayload(p string, info os.FileInfo) map[string]any {
	typ := "文件"
	if info.IsDir() {
		typ = "文件夹"
	}
	return map[string]any{"name": path.Base(p), "path": p, "type": typ, "dir": info.IsDir(), "size": info.Size(), "modified": info.ModTime().Format(time.RFC3339)}
}

func (a *App) workspaceFileProperties(p string) (map[string]any, error) {
	if err := safePath(p); err != nil {
		return nil, err
	}
	if a.workspaceMode() != "ssh" {
		info, err := a.workspace.Stat(p)
		if err != nil {
			return nil, err
		}
		return fileInfoPayload(p, info), nil
	}
	parent := path.Dir(p)
	items, err := a.sftpListRemote(a.workspaceRemotePath(parent), parent)
	if err != nil {
		return nil, err
	}
	for _, item := range items {
		if item["name"] == path.Base(p) {
			isDir, _ := item["dir"].(bool)
			typ := "文件"
			if isDir {
				typ = "文件夹"
			}
			return map[string]any{"name": item["name"], "path": p, "type": typ, "dir": isDir, "size": item["size"], "modified": item["modified"]}, nil
		}
	}
	return nil, os.ErrNotExist
}

func (a *App) fileProperties(w http.ResponseWriter, r *http.Request) {
	if source := r.URL.Query().Get("source"); source != "" {
		a.sourceFileAction(w, r.URL.Query().Get("root"), source, r.URL.Query().Get("path"), "properties", "")
		return
	}
	if r.URL.Query().Get("root") != "workspace" || r.URL.Query().Get("source") != "" {
		fail(w, 403, errors.New("仅工作目录支持属性查看"))
		return
	}
	info, err := a.workspaceFileProperties(r.URL.Query().Get("path"))
	if err != nil {
		fail(w, 400, err)
		return
	}
	jsonOut(w, 200, info)
}

func (a *App) deleteWorkspaceFile(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Root   string `json:"root"`
		Source string `json:"source"`
		Path   string `json:"path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		fail(w, 400, errors.New("请求格式错误"))
		return
	}
	if body.Source != "" {
		a.sourceFileAction(w, body.Root, body.Source, body.Path, "delete", "")
		return
	}
	if body.Root != "workspace" || body.Path == "." || safePath(body.Path) != nil {
		fail(w, 400, errors.New("需要工作目录内的有效路径"))
		return
	}
	a.filesMu.Lock()
	defer a.filesMu.Unlock()
	info, err := a.workspaceFileProperties(body.Path)
	if err != nil {
		fail(w, 400, err)
		return
	}
	if a.workspaceMode() == "ssh" {
		if err := a.sftpRemoveWorkspacePath(body.Path, info["dir"] == true); err != nil {
			fail(w, 400, err)
			return
		}
	} else if err := a.workspace.RemoveAll(body.Path); err != nil {
		fail(w, 400, err)
		return
	}
	jsonOut(w, 200, map[string]string{"path": body.Path})
}

func (a *App) createDirectory(w http.ResponseWriter, r *http.Request) {
	var body directoryRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		fail(w, 400, errors.New("请求格式错误"))
		return
	}
	if body.Parent == "" {
		body.Parent = "."
	}
	if err := directoryPath(body.Root, body.Parent); err != nil {
		fail(w, 400, err)
		return
	}
	name, err := validDirectoryName(body.Name)
	if err != nil {
		fail(w, 400, err)
		return
	}
	newPath := path.Join(body.Parent, name)
	if body.Source != "" {
		if body.Root != "" && body.Root != "context" {
			fail(w, 400, errors.New("引用来源与目录根不匹配"))
			return
		}
		a.mu.Lock()
		src, ok := a.findSource(body.Source)
		a.mu.Unlock()
		if !ok || !src.Enabled {
			fail(w, 400, errors.New("来源不存在或已停用"))
			return
		}
		if !src.RW {
			fail(w, 403, errors.New("该来源为只读"))
			return
		}
		switch src.Type {
		case "local", "skill":
			root, err := a.localSourceRoot(src)
			if err == nil {
				err = root.Mkdir(newPath, 0755)
				root.Close()
			}
			if err != nil {
				fail(w, 400, err)
				return
			}
		case "relay":
			if err := a.relayMutateSource(src, "mkdir", newPath, ""); err != nil {
				fail(w, 400, err)
				return
			}
		case "sftp":
			if _, err := a.sftpBatchSource(src, "mkdir "+shellQuoteRemote(pathJoinRemote(src.Config.Path, newPath))+"\n"); err != nil {
				fail(w, 400, err)
				return
			}
		case "workspace-sftp":
			if err := a.sftpMakeDirectory(a.workspaceRemoteSourcePath(src.Config.Path, newPath)); err != nil {
				fail(w, 400, err)
				return
			}
		default:
			fail(w, 400, errors.New("该类型来源不支持创建文件夹"))
			return
		}
		jsonOut(w, 200, map[string]string{"path": newPath, "name": name})
		return
	}
	if body.Root == "remote" || (body.Root == "workspace" && a.workspaceMode() == "ssh") {
		remotePath := newPath
		if body.Root == "workspace" {
			remotePath = a.workspaceRemotePath(newPath)
		}
		if err := a.sftpMakeDirectory(remotePath); err != nil {
			fail(w, 400, err)
			return
		}
		jsonOut(w, 200, map[string]string{"path": newPath, "name": name})
		return
	}
	root, err := a.root(body.Root)
	if err != nil {
		fail(w, 400, err)
		return
	}
	if err := root.Mkdir(newPath, 0755); err != nil {
		fail(w, 400, err)
		return
	}
	jsonOut(w, 200, map[string]string{"path": newPath, "name": name})
}

func (a *App) renameDirectory(w http.ResponseWriter, r *http.Request) {
	var body directoryRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		fail(w, 400, errors.New("请求格式错误"))
		return
	}
	if err := directoryPath(body.Root, body.Path); err != nil || body.Path == "." || body.Path == "/" {
		if err == nil {
			err = errors.New("不能重命名根目录")
		}
		fail(w, 400, err)
		return
	}
	name, err := validDirectoryName(body.NewName)
	if err != nil {
		fail(w, 400, err)
		return
	}
	newPath := path.Join(path.Dir(body.Path), name)
	if body.Root == "remote" || (body.Root == "workspace" && a.workspaceMode() == "ssh") {
		oldRemote, newRemote := body.Path, newPath
		if body.Root == "workspace" {
			oldRemote, newRemote = a.workspaceRemotePath(body.Path), a.workspaceRemotePath(newPath)
		}
		isDir, err := a.remoteDirectory(oldRemote)
		if err != nil {
			fail(w, 400, err)
			return
		}
		if !isDir {
			fail(w, 400, errors.New("只能重命名文件夹"))
			return
		}
		if err := a.sftpRenameDirectory(oldRemote, newRemote); err != nil {
			fail(w, 400, err)
			return
		}
		jsonOut(w, 200, map[string]string{"path": newPath, "name": name})
		return
	}
	root, err := a.root(body.Root)
	if err != nil {
		fail(w, 400, err)
		return
	}
	info, err := root.Stat(body.Path)
	if err != nil {
		fail(w, 400, err)
		return
	}
	if !info.IsDir() {
		fail(w, 400, errors.New("只能重命名文件夹"))
		return
	}
	if _, err := root.Stat(newPath); err == nil {
		fail(w, 409, errors.New("已存在同名文件或文件夹"))
		return
	}
	if err := root.Rename(body.Path, newPath); err != nil {
		fail(w, 400, err)
		return
	}
	jsonOut(w, 200, map[string]string{"path": newPath, "name": name})
}

// listLocalDir 列本地目录（目录优先、名称升序；≤2000 项）。
func (a *App) listLocalDir(root *os.Root, p string) ([]map[string]any, error) {
	f, err := root.Open(p)
	if err != nil {
		// Docker Desktop 的 Windows 盘符 bind mount 在 Go Root.Open(".") 上可能
		// 返回 fstatat permission denied，即使该挂载中的子目录可正常访问。
		// 根目录本身已在 os.OpenRoot 时固定；这里仅回退为读取该根，不扩展可访问范围。
		if p == "." && root == a.localRoot {
			// os.ReadDir("/local") 在同一种 drvfs 上也会走 Go 的 fstatat，仍会失败。
			// GNU find 直接读取目录流可用；仅在已固定的 /local 根目录降级使用，
			// 不接收任何用户提供的命令或路径，因此不扩大访问范围。
			if items, readErr := listDockerDesktopLocalRoot(); readErr == nil {
				return items, nil
			}
		}
		return nil, err
	}
	defer f.Close()
	entries, err := f.ReadDir(2000)
	if err != nil && err != io.EOF {
		if p == "." && root == a.localRoot {
			if items, readErr := listDockerDesktopLocalRoot(); readErr == nil {
				return items, nil
			}
		}
		return nil, err
	}
	return localDirItems(entries, p), nil
}

// listDockerDesktopLocalRoot 是 Windows Docker Desktop drvfs 根目录的兼容降级。
// 镜像运行于 Linux，find 来自基础系统；NUL 分隔避免文件名中的空格或制表符破坏解析。
func listDockerDesktopLocalRoot() ([]map[string]any, error) {
	out, err := exec.Command("find", "/local", "-mindepth", "1", "-maxdepth", "1", "-printf", "%y\\000%f\\000").Output()
	if err != nil {
		return nil, err
	}
	parts := bytes.Split(out, []byte{0})
	items := []map[string]any{}
	for i := 0; i+1 < len(parts) && len(items) < 2000; i += 2 {
		kind, name := string(parts[i]), string(parts[i+1])
		if kind != "d" || name == "" || safePath(name) != nil || strings.HasPrefix(name, ".DS_Store") {
			continue
		}
		items = append(items, map[string]any{"name": name, "path": name, "dir": true})
	}
	sort.Slice(items, func(i, j int) bool { return items[i]["name"].(string) < items[j]["name"].(string) })
	return items, nil
}

// localDirItems 将目录项转换为前端可用、安全的列表。
func localDirItems(entries []os.DirEntry, p string) []map[string]any {
	items := []map[string]any{}
	readUnix := newFileUnixReader()
	for _, e := range entries {
		ep := path.Join(p, e.Name())
		if safePath(ep) != nil || strings.HasPrefix(e.Name(), ".DS_Store") {
			continue
		}
		if e.Type()&os.ModeSymlink != 0 {
			continue
		}
		item := map[string]any{"name": e.Name(), "path": ep, "dir": e.IsDir()}
		if metadata := readUnix(e); metadata != nil {
			item["unix"] = metadata
		}
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i]["dir"] != items[j]["dir"] {
			return items[i]["dir"].(bool)
		}
		return items[i]["name"].(string) < items[j]["name"].(string)
	})
	return items
}
func (a *App) readFileRaw(w http.ResponseWriter, r *http.Request) {
	if ct, limit := generatedMediaRawType(r.URL.Query().Get("path")); ct != "" && a.streamViewerRaw(w, r, ct, limit) {
		return
	}

	if isStepRaw(r.URL.Query().Get("path")) && a.streamStepRaw(w, r) {
		return
	}
	limit := rawViewerLimit(r.URL.Query().Get("path"))
	if isStepRaw(r.URL.Query().Get("path")) {
		select {
		case stepRemotePreviewGate <- struct{}{}:
			defer func() { <-stepRemotePreviewGate }()
		default:
			fail(w, 429, errors.New("已有远程 STEP 文件正在读取，请稍后重试"))
			return
		}
	}

	// #63：旧版 .doc 二进制在线查看不支持，尽早返回明确提示
	if pth := r.URL.Query().Get("path"); r.URL.Query().Get("source") == "" && isLegacyDoc(pth) {
		fail(w, 400, legacyDocError(pth))
		return
	}
	var b []byte
	var err error
	if r.URL.Query().Get("knowledgeOrigin") != "" || r.URL.Query().Get("source") == "" && r.URL.Query().Get("root") == "workspace" && a.workspaceMode() == "ssh" {
		q := r.URL.Query()
		b, err = a.knowledgeReadViewer(r.Context(), q.Get("root"), q.Get("source"), q.Get("path"), q.Get("knowledgeOrigin"), q.Get("knowledgeWorkspace"), limit)
	} else if srcID := r.URL.Query().Get("source"); srcID != "" {
		a.mu.Lock()
		src, ok := a.findSource(srcID)
		a.mu.Unlock()
		if !ok || !src.Enabled {
			fail(w, 400, errors.New("来源不存在或已停用"))
			return
		}
		if src.Type == "mcp" {
			b, err = a.readSourceRaw(src, r.URL.Query().Get("path"))
		} else {
			b, err = a.knowledgeReadSource(r.Context(), src, r.URL.Query().Get("path"), limit)
		}
	} else {
		root, rootErr := a.root(r.URL.Query().Get("root"))
		if rootErr != nil {
			fail(w, 400, rootErr)
			return
		}
		b, err = readRawBytes(root, r.URL.Query().Get("path"))
	}
	if err != nil {
		fail(w, 400, err)
		return
	}
	if int64(len(b)) > limit {
		err = fmt.Errorf("文件超过 %d MiB 限制", limit>>20)
		fail(w, 400, err)
		return
	}
	ext := effectiveFileExt(r.URL.Query().Get("path"))
	ct := map[string]string{
		".png": "image/png", ".jpg": "image/jpeg", ".jpeg": "image/jpeg",
		".gif": "image/gif", ".svg": "image/svg+xml", ".webp": "image/webp",
		".ico": "image/x-icon", ".bmp": "image/bmp",
		".mp3": "audio/mpeg", ".wav": "audio/wav", ".mp4": "video/mp4",
		".pdf": "application/pdf", ".html": "text/html; charset=utf-8",
		".css": "text/css; charset=utf-8", ".js": "application/javascript",
		".json": "application/json", ".txt": "text/plain; charset=utf-8",
		".csv": "text/csv; charset=utf-8", ".tsv": "text/tab-separated-values; charset=utf-8",
		".md": "text/markdown; charset=utf-8", ".drawio": "application/xml; charset=utf-8",
		".stl":  "model/stl",
		".step": "application/step", ".stp": "application/step",
	}[ext]
	if ct == "" {
		ct = "application/octet-stream"
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Cache-Control", "no-store")
	if strings.HasPrefix(ct, "audio/") || strings.HasPrefix(ct, "video/") {
		http.ServeContent(w, r, path.Base(r.URL.Query().Get("path")), time.Time{}, bytes.NewReader(b))
		return
	}
	w.Write(b)
}
func (a *App) readFile(w http.ResponseWriter, r *http.Request) {
	// #63：旧版 .doc 二进制在线查看不支持，尽早返回明确提示
	if pth := r.URL.Query().Get("path"); r.URL.Query().Get("source") == "" && isLegacyDoc(pth) {
		fail(w, 400, legacyDocError(pth))
		return
	}
	var b []byte
	var err error
	if origin := r.URL.Query().Get("knowledgeOrigin"); origin != "" {
		q := r.URL.Query()
		b, err = a.knowledgeReadViewer(r.Context(), q.Get("root"), q.Get("source"), q.Get("path"), origin, q.Get("knowledgeWorkspace"), maxFile)
		if err == nil {
			b, err = validateTextFileContent(q.Get("path"), b)
		}
		if err != nil {
			fail(w, 409, err)
			return
		}
		jsonOut(w, 200, map[string]string{"content": string(b), "hash": hash(b)})
		return
	}
	if srcID := r.URL.Query().Get("source"); srcID != "" {
		a.mu.Lock()
		src, ok := a.findSource(srcID)
		a.mu.Unlock()
		if !ok || !src.Enabled {
			fail(w, 400, errors.New("来源不存在或已停用"))
			return
		}
		b, err = a.readSourceText(src, r.URL.Query().Get("path"))
		if err != nil {
			fail(w, 400, err)
			return
		}
		jsonOut(w, 200, map[string]string{"content": string(b), "hash": hash(b), "workspaceId": "source:" + srcID})
		return
	}
	if r.URL.Query().Get("root") == "workspace" && a.workspaceMode() == "ssh" {
		b, err = a.readWorkspaceText(r.URL.Query().Get("path"))
	} else {
		root, rootErr := a.root(r.URL.Query().Get("root"))
		if rootErr != nil {
			fail(w, 400, rootErr)
			return
		}
		pth := r.URL.Query().Get("path")
		// 懒加载分段：带 offset 参数时按字节窗口返回（前端编辑器后续配合）。
		if _, hasOff := r.URL.Query()["offset"]; hasOff {
			off, _ := strconv.ParseInt(r.URL.Query().Get("offset"), 10, 64)
			lim, _ := strconv.ParseInt(r.URL.Query().Get("limit"), 10, 64)
			var window []byte
			var size int64
			window, size, err = readTextRange(root, pth, off, lim)
			if err != nil {
				fail(w, 400, err)
				return
			}
			jsonOut(w, 200, map[string]any{
				"content": string(window), "offset": off, "limit": len(window),
				"size": size, "partial": true,
				"workspaceId": a.wsID(),
			})
			return
		}
		b, err = readText(root, pth)
	}
	if err != nil {
		fail(w, 400, err)
		return
	}
	which := r.URL.Query().Get("root")
	if which == "workspace" {
		jsonOut(w, 200, map[string]string{"content": string(b), "hash": hash(b), "workspaceId": a.wsID()})
		return
	}
	jsonOut(w, 200, map[string]string{"content": string(b), "hash": hash(b)})
}
func fileWriteFailure(w http.ResponseWriter, err error, content string) {
	var archived *markdownPostWriteError
	if errors.As(err, &archived) {
		jsonOut(w, 400, map[string]any{"error": err.Error(), "saved": true, "hash": hash([]byte(content)), "historySaved": false})
		return
	}
	fail(w, 400, err)
}

func (a *App) writeFile(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Path        string `json:"path"`
		Content     string `json:"content"`
		Hash        string `json:"hash"`
		Source      string `json:"source"`
		WorkspaceID string `json:"workspaceId"`
	}
	// writeFile 允许大文本：body 上限放宽到 maxFile + JSON 信封开销（默认 decode 仅 1 MiB）。
	r.Body = http.MaxBytesReader(w, r.Body, maxFile+1<<20)
	d := json.NewDecoder(r.Body)
	if err := d.Decode(&in); err != nil {
		fail(w, 400, err)
		return
	}
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		fail(w, 400, errors.New("请求必须是单个 JSON 对象"))
		return
	}
	if err := safePath(in.Path); err != nil {
		fail(w, 400, err)
		return
	}
	if len(in.Content) > maxFile {
		fail(w, 400, fmt.Errorf("文件超过 %d MiB 限制", maxFile>>20))
		return
	}
	// R02：保存必须携带读取时的工作区身份；缺失身份仅允许新建文件
	if in.Source == "" {
		if in.WorkspaceID != "" && in.WorkspaceID != a.wsID() {
			fail(w, 409, errors.New("工作区已切换：该文件属于其他项目，请重新打开后再保存"))
			return
		}
		if in.WorkspaceID == "" && in.Hash != "" && a.wsID() != defaultWorkspaceID {
			// 缺失身份仅当工作区从未定制（默认根）时兼容放行；定制后必须显式携带
			fail(w, 409, errors.New("缺少工作区身份：请重新打开文件后再保存"))
			return
		}
	} else if in.WorkspaceID != "" && in.WorkspaceID != "source:"+in.Source {
		fail(w, 409, errors.New("来源已切换：请重新打开文件后再保存"))
		return
	}
	a.filesMu.Lock()
	defer a.filesMu.Unlock()
	if in.Source != "" {
		a.mu.Lock()
		src, ok := a.findSource(in.Source)
		a.mu.Unlock()
		if !ok || !src.Enabled {
			fail(w, 400, errors.New("来源不存在或已停用"))
			return
		}
		if !src.RW {
			fail(w, 403, errors.New("该来源为只读"))
			return
		}
		current, readErr := a.readSourceText(src, in.Path)
		if (readErr == nil && hash(current) != in.Hash) || (readErr != nil && (in.Hash != "" || (!errors.Is(readErr, os.ErrNotExist) && !isSFTPNotExistErr(readErr)))) {
			fail(w, 409, errors.New("来源文件已改变或无法确认版本，请重新打开后再保存"))
			return
		}
		if err := a.writeSourceText(src, in.Path, []byte(in.Content)); err != nil {
			fileWriteFailure(w, err, in.Content)
			return
		}
		jsonOut(w, 200, map[string]string{"hash": hash([]byte(in.Content))})
		return
	}
	if a.workspaceMode() == "ssh" {
		// 与本地 checkVersion 对齐区分“新建/覆盖”，但不能用 sftpExists(ls -l)：
		// sftp 的 `ls -l <file>` 在远端/桩里常退化为目录列表，无法可靠判断具体文件存在性。
		// 改为依据 `get` 的结果文本分类：
		//   - get 报 “No such file / Couldn't stat” → 远端确无此文件，允许新建；
		//   - get 成功但 validateTextContent 判为二进制 → 文件存在、无可比对内容，409；
		//   - get 成功且为文本 → 比对哈希，不一致 409。
		current, readErr := a.readWorkspaceText(in.Path)
		switch {
		case readErr == nil:
			if hash(current) != in.Hash {
				fail(w, 409, errors.New("文件已改变或已存在，请重新打开后再保存"))
				return
			}
		case isSFTPNotExistErr(readErr):
			// 远端确无此文件：新建。若客户端却带了旧哈希，说明已被删，409。
			if in.Hash != "" {
				fail(w, 409, errors.New("文件已被删除，请重新打开"))
				return
			}
		default:
			// 文件存在但无法作为文本读取（二进制等）：不绕过冲突检测，409。
			fail(w, 409, errors.New("文件已改变或已存在，请重新打开后再保存"))
			return
		}
		if err := a.writeWorkspaceText(in.Path, []byte(in.Content)); err != nil {
			fileWriteFailure(w, err, in.Content)
			return
		}
		jsonOut(w, 200, map[string]string{"hash": hash([]byte(in.Content))})
		return
	}
	if err := checkVersion(a.workspace, in.Path, in.Hash); err != nil {
		fail(w, 409, err)
		return
	}
	if err := a.writeWorkspaceText(in.Path, []byte(in.Content)); err != nil {
		fileWriteFailure(w, err, in.Content)
		return
	}
	jsonOut(w, 200, map[string]string{"hash": hash([]byte(in.Content))})
}

// uploadFile writes one browser-selected raw file to the current writable workspace
// or reference source. It intentionally rejects replacement: a dropped file must not
// silently overwrite a project artifact. Local, SSH workspace, and writable source
// destinations all reuse their existing atomic write channels.
func (a *App) uploadFile(w http.ResponseWriter, r *http.Request) { a.legacyLargeUpload(w, r) }

// isSFTPNotExistErr 判断 sftp 读取失败是否因“远端文件不存在”。
// sftpBatch 把 stdout+stderr 合并进错误文本；OpenSSH sftp 对缺失文件报
// “Couldn't stat remote file: No such file” 或 “File \"...\" not found.”。
// 只接受明确的不存在诊断；Couldn't stat 本身也可能表示权限不足。
func isSFTPNotExistErr(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, os.ErrNotExist) {
		return true
	}
	for _, line := range strings.Split(err.Error(), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "sftp>") {
			continue
		}
		if strings.HasSuffix(line, ": No such file") || strings.HasSuffix(line, ": No such file or directory") {
			return true
		}
		if strings.Contains(line, `File "`) && strings.HasSuffix(line, `" not found.`) {
			return true
		}
	}
	return false
}

func checkVersion(root *os.Root, p, expected string) error {
	b, err := readText(root, p)
	return checkReadVersion(b, err, expected)
}

func checkReadVersion(b []byte, err error, expected string) error {
	if errors.Is(err, os.ErrNotExist) {
		if expected == "" {
			return nil
		}
		return errors.New("文件已被删除，请重新打开")
	}
	if err != nil {
		return err
	}
	if hash(b) != expected {
		return errors.New("文件已改变或已存在，请重新打开后再保存")
	}
	return nil
}
func putText(root *os.Root, p string, b []byte) error {
	if err := safePath(p); err != nil {
		return err
	}
	if err := root.MkdirAll(path.Dir(p), 0755); err != nil {
		return err
	}
	mode := os.FileMode(0644)
	if st, err := root.Stat(p); err == nil {
		mode = st.Mode().Perm()
	}
	return putAtomicFile(root, p, b, mode)
}

func putAtomicFile(root *os.Root, p string, b []byte, mode os.FileMode) error {
	tmp := path.Join(path.Dir(p), ".aide-write-"+newID())
	f, err := root.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return err
	}
	defer root.Remove(tmp)
	_, err = f.Write(b)
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return root.Rename(tmp, p)
}

// renameFile 在同一目录内重命名文件/文件夹（不允许跨目录移动、不允许覆盖已存在目标）。
func (a *App) renameFile(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Root    string `json:"root"`
		Source  string `json:"source"`
		Path    string `json:"path"`
		NewName string `json:"newName"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		fail(w, 400, errors.New("请求格式错误"))
		return
	}
	if err := safePath(body.Path); err != nil {
		fail(w, 400, err)
		return
	}
	if body.Source != "" {
		a.sourceFileAction(w, body.Root, body.Source, body.Path, "rename", body.NewName)
		return
	}
	if body.Root != "workspace" || body.Source != "" {
		fail(w, 403, errors.New("仅工作目录支持重命名"))
		return
	}
	if strings.ContainsAny(body.NewName, "\x00\r\n") {
		fail(w, 400, errors.New("文件名无效"))
		return
	}
	newName := strings.TrimSpace(body.NewName)
	if newName == "" || newName == "." || newName == ".." ||
		strings.ContainsAny(newName, `/\`+"\x00") {
		fail(w, 400, errors.New("文件名无效"))
		return
	}
	dir := path.Dir(body.Path)
	newPath := path.Join(dir, newName)
	if err := safePath(newPath); err != nil {
		fail(w, 400, err)
		return
	}
	a.filesMu.Lock()
	defer a.filesMu.Unlock()
	if a.workspaceMode() == "ssh" {
		if _, err := a.workspaceFileProperties(body.Path); err != nil {
			fail(w, 400, err)
			return
		}
		if a.sftpExists(a.workspaceRemotePath(newPath)) {
			fail(w, 409, errors.New("已存在同名文件或文件夹"))
			return
		}
		out, err := a.sftpBatch("rename " + shellQuoteRemote(a.workspaceRemotePath(body.Path)) + " " + shellQuoteRemote(a.workspaceRemotePath(newPath)) + "\n")
		if err != nil || sftpCommandFailed(out) {
			if err == nil {
				err = errors.New(strings.TrimSpace(out))
			}
			fail(w, 400, err)
			return
		}
	} else {
		if _, err := a.workspace.Stat(newPath); err == nil {
			fail(w, 409, errors.New("已存在同名文件或文件夹"))
			return
		}
		if err := a.workspace.Rename(body.Path, newPath); err != nil {
			fail(w, 500, err)
			return
		}
	}
	jsonOut(w, 200, map[string]any{"path": newPath, "name": newName})
}

// ===== SQLite 查看器 API =====

// isSqlitePath 判断是否为 SQLite 数据库文件
func isSqlitePath(pth string) bool {
	return strings.HasSuffix(strings.ToLower(pth), ".db") ||
		strings.HasSuffix(strings.ToLower(pth), ".sqlite") ||
		strings.HasSuffix(strings.ToLower(pth), ".sqlite3")
}

// resolveSqlitePath 解析 sqlite 文件，读取内容并写到临时文件，返回临时文件路径
func (a *App) resolveSqlitePath(r *http.Request) (string, error) {
	pth := r.URL.Query().Get("path")
	if err := safePath(pth); err != nil {
		return "", err
	}
	if srcID := r.URL.Query().Get("source"); srcID != "" {
		return "", errors.New("引用暂不支持 SQLite")
	}
	root, err := a.root(r.URL.Query().Get("root"))
	if err != nil {
		return "", err
	}
	b, err := readRawBytes(root, pth)
	if err != nil {
		return "", err
	}
	tmp, err := os.CreateTemp("", "aide-sqlite-*.db")
	if err != nil {
		return "", err
	}
	defer tmp.Close()
	if _, err := tmp.Write(b); err != nil {
		// 提前 return 时调用方的 defer os.Remove(tmpPath) 不会执行，需在此清理，
		// 避免磁盘满/配额失败时临时 db 残留在系统 temp 目录。
		os.Remove(tmp.Name())
		return "", err
	}
	return tmp.Name(), nil
}

// sqliteListTables 返回 sqlite 数据库的表列表
func (a *App) sqliteListTables(w http.ResponseWriter, r *http.Request) {
	tmpPath, err := a.resolveSqlitePath(r)
	if err != nil {
		fail(w, 400, err)
		return
	}
	defer os.Remove(tmpPath)
	// 用 python3 sqlite3 读取表列表
	cmd := exec.Command("python3", "-c", `
import sqlite3, sys, json
db = sqlite3.connect(sys.argv[1])
cur = db.cursor()
cur.execute("SELECT name FROM sqlite_master WHERE type='table' ORDER BY name")
tables = [r[0] for r in cur.fetchall()]
result = []
for t in tables:
    cur.execute(f"SELECT COUNT(*) FROM [{t}]")
    count = cur.fetchone()[0]
    result.append({"name": t, "rows": count})
db.close()
print(json.dumps(result))
`, tmpPath)
	out, err := cmd.Output()
	if err != nil {
		fail(w, 500, fmt.Errorf("读取 sqlite 失败: %w", err))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(out)
}

// sqliteQueryData 返回指定表的数据
func (a *App) sqliteQueryData(w http.ResponseWriter, r *http.Request) {
	table := r.URL.Query().Get("table")
	if table == "" {
		fail(w, 400, errors.New("缺少 table 参数"))
		return
	}
	limit := r.URL.Query().Get("limit")
	if limit == "" {
		limit = "100"
	}
	offset := r.URL.Query().Get("offset")
	if offset == "" {
		offset = "0"
	}
	pageSize, limitErr := strconv.Atoi(limit)
	pageOffset, offsetErr := strconv.ParseInt(offset, 10, 64)
	if limitErr != nil || pageSize < 1 || pageSize > 1000 || offsetErr != nil || pageOffset < 0 {
		fail(w, http.StatusBadRequest, errors.New("invalid sqlite pagination: limit must be 1..1000 and offset a non-negative integer"))
		return
	}
	limit = strconv.Itoa(pageSize)
	offset = strconv.FormatInt(pageOffset, 10)
	tmpPath, err := a.resolveSqlitePath(r)
	if err != nil {
		fail(w, 400, err)
		return
	}
	defer os.Remove(tmpPath)
	// 用 python3 sqlite3 读取表数据
	cmd := exec.Command("python3", "-c", `
import sqlite3, sys, json
db = sqlite3.connect(sys.argv[1])
db.row_factory = sqlite3.Row
cur = db.cursor()
# 获取列名
cur.execute(f"PRAGMA table_info([{sys.argv[2]}])")
cols = [{"name": r[1], "type": r[2]} for r in cur.fetchall()]
# 获取数据
cur.execute(f"SELECT * FROM [{sys.argv[2]}] LIMIT ? OFFSET ?", (int(sys.argv[3]), int(sys.argv[4])))
rows = [dict(r) for r in cur.fetchall()]
# 获取总行数
cur.execute(f"SELECT COUNT(*) FROM [{sys.argv[2]}]")
total = cur.fetchone()[0]
db.close()
print(json.dumps({"columns": cols, "rows": rows, "total": total}))
`, tmpPath, table, limit, offset)
	out, err := cmd.Output()
	if err != nil {
		fail(w, 500, fmt.Errorf("查询 sqlite 失败: %w", err))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(out)
}
