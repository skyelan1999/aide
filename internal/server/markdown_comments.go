package server

import (
	"encoding/hex"
	"errors"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// Keep each review bound to the file's resolved workspace or source. The
// legacy Office endpoint still uses its original sidecar directory.
func (a *App) markdownCommentTarget(root, source, file, workspaceID string) (*commentStore, string, error) {
	a.mu.Lock()
	currentWorkspace := a.wsID()
	a.mu.Unlock()
	if source == "" && (root == "" || root == "workspace") && workspaceID != "" && workspaceID != currentWorkspace {
		return nil, "", errors.New("工作区已变化，请重新打开文档")
	}
	var identity string
	var read func(string) ([]byte, error)
	if source != "" || root == "" || root == "workspace" {
		var err error
		identity, read, err = a.historyTarget(source, file)
		if err != nil {
			return nil, "", err
		}
		if source == "" && identity != "workspace:"+currentWorkspace {
			return nil, "", errors.New("工作区已变化，请重新打开文档")
		}
	} else {
		a.mu.Lock()
		resolved, err := a.root(root)
		var base string
		if err == nil && resolved != nil {
			base = resolved.Name()
		}
		a.mu.Unlock()
		if base == "" {
			return nil, "", errors.New("文档来源不可用")
		}
		identity = root + ":" + base
		read = func(file string) ([]byte, error) {
			opened, err := os.OpenRoot(base)
			if err != nil {
				return nil, err
			}
			defer opened.Close()
			return readRawBytes(opened, file)
		}
	}
	bytes, err := read(file)
	if err != nil {
		return nil, "", err
	}
	if err := validateTextContent(bytes); err != nil {
		return nil, "", err
	}
	dir := filepath.Join(CommentsDir(a.dataPath), "markdown", hash([]byte(currentWorkspace+"\x00"+identity)))
	return &commentStore{dir: dir}, hash(bytes), nil
}

func (a *App) markdownComments(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	file := query.Get("path")
	if err := safePath(file); err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	file = path.Clean(file)
	extension := path.Ext(strings.TrimSuffix(strings.ToLower(file), ".bak"))
	if extension != ".md" && extension != ".markdown" {
		fail(w, http.StatusBadRequest, errors.New("此文件不支持 Markdown 批注"))
		return
	}
	store, version, err := a.markdownCommentTarget(query.Get("root"), query.Get("source"), file, query.Get("workspaceId"))
	if err != nil {
		fail(w, http.StatusConflict, err)
		return
	}
	switch r.Method {
	case http.MethodGet:
		comments, err := store.list(file)
		if err != nil {
			fail(w, http.StatusInternalServerError, err)
			return
		}
		markStale(comments, version)
		jsonOut(w, http.StatusOK, map[string]any{"comments": comments})
	case http.MethodPost:
		var in createInput
		if err := decode(w, r, &in); err != nil {
			fail(w, http.StatusBadRequest, err)
			return
		}
		if in.DocHash != version {
			fail(w, http.StatusConflict, errors.New("文档已变化，请保存或重新打开后添加批注"))
			return
		}
		if strings.TrimSpace(in.AnchorQuote) == "" {
			fail(w, http.StatusBadRequest, errors.New("请选中要批注的文字"))
			return
		}
		in.DocPath = file
		comment, err := store.create(in)
		if err != nil {
			fail(w, http.StatusBadRequest, err)
			return
		}
		a.commentsAudit("markdown-comment-create", comment.ID, file)
		jsonOut(w, http.StatusCreated, comment)
	case http.MethodPut, http.MethodDelete:
		id := r.PathValue("id")
		if decoded, err := hex.DecodeString(id); err != nil || len(decoded) != 16 {
			fail(w, http.StatusNotFound, errors.New("批注不存在"))
			return
		}
		if _, err := readCommentFile(filepath.Join(store.docDir(file), id+".json")); err != nil {
			fail(w, http.StatusNotFound, errors.New("批注不存在"))
			return
		}
		if r.Method == http.MethodDelete {
			if err := store.remove(id); err != nil {
				fail(w, http.StatusInternalServerError, err)
				return
			}
			a.commentsAudit("markdown-comment-delete", id, file)
			jsonOut(w, http.StatusOK, map[string]bool{"ok": true})
			return
		}
		var in updateInput
		if err := decode(w, r, &in); err != nil {
			fail(w, http.StatusBadRequest, err)
			return
		}
		comment, err := store.update(id, in)
		if err != nil {
			fail(w, http.StatusBadRequest, err)
			return
		}
		a.commentsAudit("markdown-comment-update", id, file)
		markStale([]*Comment{comment}, version)
		jsonOut(w, http.StatusOK, comment)
	}
}
