package server

// Native DOCX comments are part of the document, not the legacy sidecar store.
// Workspace/source access and optimistic concurrency are enforced here; the
// Python helper only transforms bounded OOXML bytes in a temporary file.

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path"
	"strings"
)

func docxCommentRun(b []byte, operation string, spec any) (map[string]any, []byte, error) {
	if err := checkOfficeArchive(b); err != nil {
		return nil, nil, err
	}
	filename, cleanup, err := officeTemp(".docx", b)
	if err != nil {
		return nil, nil, err
	}
	defer cleanup()
	script, err := officeScriptPath("docx_comments.py")
	if err != nil {
		return nil, nil, err
	}
	arg, err := json.Marshal(spec)
	if err != nil || len(arg) > 1<<20 {
		return nil, nil, errors.New("批注参数无效或过大")
	}
	out, err := runOfficeScript(os.TempDir(), script, operation, filename, string(arg))
	if err != nil {
		return nil, nil, err
	}
	var result map[string]any
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		return nil, nil, err
	}
	if operation == "list" {
		return result, nil, nil
	}
	changed, err := os.ReadFile(filename)
	if err != nil {
		return nil, nil, err
	}
	if err := checkOfficeArchive(changed); err != nil {
		return nil, nil, err
	}
	return result, changed, nil
}

func officeCommentIdentity(source string, a *App) string {
	if source != "" {
		return "source:" + source
	}
	return a.wsID()
}

func validDocxPath(p string) error {
	if err := safePath(p); err != nil {
		return err
	}
	if strings.ToLower(path.Ext(p)) != ".docx" {
		return errors.New("仅支持 .docx 文件")
	}
	return nil
}

// GET /api/office/docx/comments?path=...&source=...
func (a *App) officeDocxCommentsGet(w http.ResponseWriter, r *http.Request) {
	p, source := r.URL.Query().Get("path"), r.URL.Query().Get("source")
	if err := validDocxPath(p); err != nil {
		fail(w, 400, err)
		return
	}
	b, src, err := a.officeReadBytes(p, source)
	if err != nil {
		fail(w, 400, err)
		return
	}
	result, _, err := docxCommentRun(b, "list", map[string]any{})
	if err != nil {
		fail(w, 400, err)
		return
	}
	result["hash"] = hash(b)
	result["workspaceId"] = officeCommentIdentity(source, a)
	result["readOnly"] = source != "" && !src.RW
	jsonOut(w, 200, result)
}

// POST /api/office/docx/comments adds a portable comment. PUT edits the
// precisely anchored text; neither silently resolves or removes the comment.
func (a *App) officeDocxCommentsWrite(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Path        string `json:"path"`
		Source      string `json:"source"`
		Hash        string `json:"hash"`
		WorkspaceID string `json:"workspaceId"`
		Quote       string `json:"quote"`
		Text        string `json:"text"`
		Author      string `json:"author"`
		AnchorIndex int    `json:"anchorIndex"`
		ID          int    `json:"id"`
		Expected    string `json:"expectedText"`
		NewText     string `json:"newText"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		fail(w, 400, err)
		return
	}
	if err := validDocxPath(in.Path); err != nil {
		fail(w, 400, err)
		return
	}
	if in.Hash == "" || in.WorkspaceID != officeCommentIdentity(in.Source, a) {
		fail(w, 409, errors.New("文档版本或工作区已变化，请重新打开"))
		return
	}
	a.filesMu.Lock()
	defer a.filesMu.Unlock()
	b, src, err := a.officeReadBytes(in.Path, in.Source)
	if err != nil {
		fail(w, 400, err)
		return
	}
	if in.Source != "" && !src.RW {
		fail(w, 403, errors.New("该引用为只读"))
		return
	}
	if hash(b) != in.Hash {
		fail(w, 409, errors.New("文件已被修改，请重新打开批注"))
		return
	}
	op := "add"
	spec := map[string]any{"quote": in.Quote, "text": in.Text, "author": in.Author, "anchorIndex": in.AnchorIndex}
	if r.Method == http.MethodPut {
		op = "edit"
		spec = map[string]any{"id": in.ID, "expectedText": in.Expected, "newText": in.NewText}
	}
	result, changed, err := docxCommentRun(b, op, spec)
	if err != nil {
		fail(w, 400, err)
		return
	}
	latest, _, err := a.officeReadBytes(in.Path, in.Source)
	if err != nil || hash(latest) != in.Hash {
		fail(w, 409, errors.New("文件在批注处理期间发生变化，请重新打开"))
		return
	}
	if err = a.officeWriteBytes(in.Path, in.Source, src, changed); err != nil {
		fail(w, 400, err)
		return
	}
	result["hash"] = hash(changed)
	jsonOut(w, 200, result)
}

func (a *App) officeCommentTool(wsRoot *os.Root, mode, remoteBase, p, operation string, spec map[string]any) string {
	if err := validDocxPath(p); err != nil {
		return "路径无效: " + err.Error()
	}
	a.filesMu.Lock()
	defer a.filesMu.Unlock()
	var b []byte
	var err error
	if mode == "ssh" {
		b, err = a.sftpRead(pathJoinRemote(remoteBase, p))
	} else {
		b, err = readRawBytes(wsRoot, p)
	}
	if err != nil {
		return "读取 DOCX 失败: " + err.Error()
	}
	result, changed, err := docxCommentRun(b, operation, spec)
	if err != nil {
		return "批注操作失败: " + err.Error()
	}
	if operation != "list" {
		var latest []byte
		if mode == "ssh" {
			latest, err = a.sftpRead(pathJoinRemote(remoteBase, p))
		} else {
			latest, err = readRawBytes(wsRoot, p)
		}
		if err != nil || hash(latest) != hash(b) {
			return "文件在批注处理期间发生变化，请重新读取批注"
		}
		if mode == "ssh" {
			err = a.sftpWrite(pathJoinRemote(remoteBase, p), changed)
		} else {
			err = putText(wsRoot, p, changed)
		}
		if err != nil {
			return "保存 DOCX 失败: " + err.Error()
		}
	}
	out, _ := json.Marshal(result)
	return fmt.Sprintf("%s", out)
}
