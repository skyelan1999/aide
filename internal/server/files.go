package server

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"
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
		if part == ".." || part == ".git" || part == ".data" || part == ".env" || part == "docker-images" {
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
func readText(root *os.Root, p string) ([]byte, error) {
	if err := safePath(p); err != nil {
		return nil, err
	}
	f, err := root.Open(p)
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
	if !utf8.Valid(b) || bytes.IndexByte(b, 0) >= 0 {
		return nil, errors.New("不支持二进制文件")
	}
	return b, nil
}

// readTextLines 按行流式读取（模型 read_file 工具的分段模式）。
// offset 为 0 起始行号，limit 为最多返回行数（<=0 表示读到 EOF）。
// 全程只缓冲目标窗口，逐行扫描时同时检测整个文件是否含 NUL（二进制拒绝），
// 对返回窗口做 UTF-8 校验。返回：窗口内容、文件总行数。
func readTextLines(root *os.Root, p string, offset, limit int) ([]byte, int, error) {
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

// readRawBytes 返回文件的原始字节，不做 UTF-8/文本校验（供 /api/file/raw 查看器使用）。
func readRawBytes(root *os.Root, p string) ([]byte, error) {
	if err := safePath(p); err != nil {
		return nil, err
	}
	f, err := root.Open(p)
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
func (a *App) root(which string) (*os.Root, error) {
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
		items, err := a.sftpListRemote(p, p)
		if err != nil {
			fail(w, 400, err)
			return
		}
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
		items, err := a.listSourceDir(src, p)
		if err != nil {
			fail(w, 400, err)
			return
		}
		jsonOut(w, 200, items)
		return
	}
	if which == "workspace" && a.workspaceMode() == "ssh" {
		items, err := a.listWorkspaceDir(p)
		if err != nil {
			fail(w, 400, err)
			return
		}
		jsonOut(w, 200, items)
		return
	}
	root, err := a.root(which)
	if err != nil {
		fail(w, 400, err)
		return
	}
	items, err := a.listLocalDir(root, p)
	if err != nil {
		fail(w, 400, err)
		return
	}
	jsonOut(w, 200, items)
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
	Root     string `json:"root"`
	Path     string `json:"path"`
	Parent   string `json:"parentPath"`
	Name     string `json:"name"`
	NewName  string `json:"newName"`
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
		Root string `json:"root"`
		Path string `json:"path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Root != "workspace" || safePath(body.Path) != nil {
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
		if err := a.sftpRemove(a.workspaceRemotePath(body.Path), info["dir"] == true); err != nil {
			fail(w, 400, err)
			return
		}
	} else if err := a.workspace.Remove(body.Path); err != nil {
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
	for _, e := range entries {
		ep := path.Join(p, e.Name())
		if safePath(ep) != nil || strings.HasPrefix(e.Name(), ".DS_Store") {
			continue
		}
		if e.Type()&os.ModeSymlink != 0 {
			continue
		}
		items = append(items, map[string]any{"name": e.Name(), "path": ep, "dir": e.IsDir()})
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
	// #63：旧版 .doc 二进制在线查看不支持，尽早返回明确提示
	if pth := r.URL.Query().Get("path"); r.URL.Query().Get("source") == "" && isLegacyDoc(pth) {
		fail(w, 400, legacyDocError(pth))
		return
	}
	var b []byte
	var err error
	if srcID := r.URL.Query().Get("source"); srcID != "" {
		a.mu.Lock()
		src, ok := a.findSource(srcID)
		a.mu.Unlock()
		if !ok || !src.Enabled {
			fail(w, 400, errors.New("来源不存在或已停用"))
			return
		}
		b, err = a.readSourceRaw(src, r.URL.Query().Get("path"))
	} else if r.URL.Query().Get("root") == "workspace" && a.workspaceMode() == "ssh" {
		pth := r.URL.Query().Get("path")
		if err := safePath(pth); err != nil {
			fail(w, 400, err)
			return
		}
		b, err = a.sftpRead(a.workspaceRemotePath(pth))
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
	ext := strings.ToLower(path.Ext(r.URL.Query().Get("path")))
	ct := map[string]string{
		".png": "image/png", ".jpg": "image/jpeg", ".jpeg": "image/jpeg",
		".gif": "image/gif", ".svg": "image/svg+xml", ".webp": "image/webp",
		".ico": "image/x-icon", ".bmp": "image/bmp",
		".pdf": "application/pdf", ".html": "text/html; charset=utf-8",
		".css": "text/css; charset=utf-8", ".js": "application/javascript",
		".json": "application/json", ".txt": "text/plain; charset=utf-8",
		".md": "text/markdown; charset=utf-8", ".drawio": "application/xml; charset=utf-8",
		".stl": "model/stl",
	}[ext]
	if ct == "" {
		ct = "application/octet-stream"
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Cache-Control", "no-store")
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
		if err := a.writeSourceText(src, in.Path, []byte(in.Content)); err != nil {
			fail(w, 400, err)
			return
		}
		jsonOut(w, 200, map[string]string{"hash": hash([]byte(in.Content))})
		return
	}
	a.filesMu.Lock()
	defer a.filesMu.Unlock()
	if a.workspaceMode() == "ssh" {
		current, readErr := a.readWorkspaceText(in.Path)
		if readErr == nil && hash(current) != in.Hash {
			fail(w, 409, errors.New("文件已改变或已存在，请重新打开后再保存"))
			return
		}
		if readErr != nil && in.Hash != "" {
			fail(w, 409, errors.New("文件已被删除，请重新打开"))
			return
		}
		if err := a.writeWorkspaceText(in.Path, []byte(in.Content)); err != nil {
			fail(w, 400, err)
			return
		}
		jsonOut(w, 200, map[string]string{"hash": hash([]byte(in.Content))})
		return
	}
	if err := checkVersion(a.workspace, in.Path, in.Hash); err != nil {
		fail(w, 409, err)
		return
	}
	if err := putText(a.workspace, in.Path, []byte(in.Content)); err != nil {
		fail(w, 400, err)
		return
	}
	jsonOut(w, 200, map[string]string{"hash": hash([]byte(in.Content))})
}
func checkVersion(root *os.Root, p, expected string) error {
	b, err := readText(root, p)
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
			if err == nil { err = errors.New(strings.TrimSpace(out)) }
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
