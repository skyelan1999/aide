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
	root, err := a.root(r.URL.Query().Get("root"))
	if err != nil {
		fail(w, 400, err)
		return
	}
	f, err := root.Open(p)
	if err != nil {
		fail(w, 400, err)
		return
	}
	defer f.Close()
	entries, err := f.ReadDir(2000)
	if err != nil && err != io.EOF {
		fail(w, 400, err)
		return
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
	jsonOut(w, 200, items)
}
func (a *App) readFile(w http.ResponseWriter, r *http.Request) {
	root, err := a.root(r.URL.Query().Get("root"))
	if err != nil {
		fail(w, 400, err)
		return
	}
	b, err := readText(root, r.URL.Query().Get("path"))
	if err != nil {
		fail(w, 400, err)
		return
	}
	jsonOut(w, 200, map[string]string{"content": string(b), "hash": hash(b)})
}
func (a *App) writeFile(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Path    string `json:"path"`
		Content string `json:"content"`
		Hash    string `json:"hash"`
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
	a.filesMu.Lock()
	defer a.filesMu.Unlock()
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
