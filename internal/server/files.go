package server

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"sort"
	"strings"
	"unicode/utf8"
)

const maxFile = 256 << 10

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
		return nil, fmt.Errorf("文件超过 %d KB 限制", maxFile/1024)
	}
	if !utf8.Valid(b) || strings.ContainsRune(string(b), 0) {
		return nil, errors.New("不支持二进制文件")
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
	which := r.URL.Query().Get("root")
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

// listLocalDir 列本地目录（目录优先、名称升序；≤2000 项）。
func (a *App) listLocalDir(root *os.Root, p string) ([]map[string]any, error) {
	f, err := root.Open(p)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	entries, err := f.ReadDir(2000)
	if err != nil && err != io.EOF {
		return nil, err
	}
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
	return items, nil
}
func (a *App) readFile(w http.ResponseWriter, r *http.Request) {
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
		jsonOut(w, 200, map[string]string{"content": string(b), "hash": hash(b), "wsId": "source:" + srcID})
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
		b, err = readText(root, r.URL.Query().Get("path"))
	}
	if err != nil {
		fail(w, 400, err)
		return
	}
	which := r.URL.Query().Get("root")
	if which == "workspace" {
		jsonOut(w, 200, map[string]string{"content": string(b), "hash": hash(b), "wsId": a.wsID()})
		return
	}
	jsonOut(w, 200, map[string]string{"content": string(b), "hash": hash(b)})
}
func (a *App) writeFile(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Path    string `json:"path"`
		Content string `json:"content"`
		Hash    string `json:"hash"`
		Source  string `json:"source"`
		WsID    string `json:"wsId"`
	}
	if err := decode(w, r, &in); err != nil {
		fail(w, 400, err)
		return
	}
	if err := safePath(in.Path); err != nil {
		fail(w, 400, err)
		return
	}
	if len(in.Content) > maxFile {
		fail(w, 400, errors.New("文件太大"))
		return
	}
	// R02：保存必须携带读取时的工作区身份；缺失身份仅允许新建文件
	if in.Source == "" {
		if in.WsID != "" && in.WsID != a.wsID() {
			fail(w, 409, errors.New("工作区已切换：该文件属于其他项目，请重新打开后再保存"))
			return
		}
		if in.WsID == "" && in.Hash != "" && a.wsID() != defaultWorkspaceID {
			// 缺失身份仅当工作区从未定制（默认根）时兼容放行；定制后必须显式携带
			fail(w, 409, errors.New("缺少工作区身份：请重新打开文件后再保存"))
			return
		}
	} else if in.WsID != "" && in.WsID != "source:"+in.Source {
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
