package server

// Structured Office artifacts and the XLSX grid API. All Office parsing runs
// against bounded temporary files; workspace/source access remains in Go.

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path"
	"strconv"
	"strings"
)

// A small compressed XLSX can still expand into a huge XML payload. Bound
// expansion before handing untrusted workspace/reference bytes to openpyxl.
func checkOfficeArchive(b []byte) error {
	if len(b) == 0 || len(b) > maxRawFile {
		return errors.New("Office 文件为空或超过大小限制")
	}
	z, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		return errors.New("Office 文件不是有效的 OOXML ZIP")
	}
	if len(z.File) > 10000 {
		return errors.New("Office 文件包含过多条目")
	}
	var expanded uint64
	for _, file := range z.File {
		if file.UncompressedSize64 > 128<<20 || expanded > (128<<20)-file.UncompressedSize64 {
			return errors.New("Office 文件解压后超过 128 MiB 限制")
		}
		expanded += file.UncompressedSize64
	}
	return nil
}

func officeTemp(suffix string, b []byte) (string, func(), error) {
	f, err := os.CreateTemp("", "aide-office-*"+suffix)
	if err != nil {
		return "", nil, err
	}
	cleanup := func() { _ = os.Remove(f.Name()) }
	if len(b) > 0 {
		if _, err = f.Write(b); err != nil {
			_ = f.Close()
			cleanup()
			return "", nil, err
		}
	}
	if err = f.Close(); err != nil {
		cleanup()
		return "", nil, err
	}
	return f.Name(), cleanup, nil
}

func officeRun(input []byte, op string, args ...string) ([]byte, error) {
	sp, err := officeScriptPath("office_artifact.py")
	if err != nil {
		return nil, err
	}
	inputPath, cleanInput, err := officeTemp(".json", input)
	if err != nil {
		return nil, err
	}
	defer cleanInput()
	outputPath, cleanOutput, err := officeTemp(".office", nil)
	if err != nil {
		return nil, err
	}
	defer cleanOutput()
	params := append([]string{op}, args...)
	params = append(params, inputPath, outputPath)
	if _, err := runOfficeScript(os.TempDir(), sp, params...); err != nil {
		return nil, err
	}
	b, err := os.ReadFile(outputPath)
	if err != nil {
		return nil, err
	}
	if len(b) == 0 || len(b) > maxRawFile {
		return nil, errors.New("Office 输出为空或超过文件大小限制")
	}
	return b, nil
}

func officeCreateBytes(kind string, spec any) ([]byte, error) {
	if kind != "docx" && kind != "xlsx" && kind != "pptx" {
		return nil, errors.New("仅支持 docx、xlsx、pptx")
	}
	b, err := json.Marshal(spec)
	if err != nil || len(b) > 1<<20 {
		return nil, errors.New("Office 内容无效或过大")
	}
	return officeRun(b, "create", kind)
}

func officeExtractText(b []byte, ext string) (string, error) {
	if strings.EqualFold(ext, ".pdf") {
		if len(b) == 0 || len(b) > maxRawFile {
			return "", errors.New("PDF 文件为空或超过大小限制")
		}
		if !bytes.HasPrefix(bytes.TrimSpace(b[:min(len(b), 1024)]), []byte("%PDF-")) {
			return "", errors.New("文件不是有效的 PDF")
		}
	} else if err := checkOfficeArchive(b); err != nil {
		return "", err
	}
	sp, err := officeScriptPath("extract_text.py")
	if err != nil {
		return "", err
	}
	p, cleanup, err := officeTemp(ext, b)
	if err != nil {
		return "", err
	}
	defer cleanup()
	out, err := runOfficeScript(os.TempDir(), sp, p)
	if err != nil {
		return "", err
	}
	if len(out) > 60<<10 {
		out = out[:60<<10] + "\n…（已截断）"
	}
	return out, nil
}

func (a *App) officeCreateTool(wsRoot *os.Root, mode, remotePath, p, kind string, spec any) string {
	if err := safePath(p); err != nil {
		return "路径无效: " + err.Error()
	}
	if strings.ToLower(path.Ext(p)) != "."+kind {
		return "文件扩展名必须是 ." + kind
	}
	b, err := officeCreateBytes(kind, spec)
	if err != nil {
		return "Office 生成失败: " + err.Error()
	}
	a.filesMu.Lock()
	defer a.filesMu.Unlock()
	if src, bound := a.generatedDocumentSource(); bound {
		if _, readErr := a.readSourceRaw(src, p); readErr == nil {
			return "文件已存在，不会覆盖: " + p
		} else if !errors.Is(readErr, os.ErrNotExist) && !isSFTPNotExistErr(readErr) {
			return "检查自动系统文档失败: " + readErr.Error()
		}
		if err := a.writeSourceText(src, p, b); err != nil {
			return "Office 写入失败: " + err.Error()
		}
		return fmt.Sprintf("已生成 %s（%d 字节）；来源 system-docs，请从自动系统文档打开验证", path.Join(src.Config.Path, p), len(b))
	}
	if mode == "ssh" {
		remoteFile := pathJoinRemote(remotePath, p)
		if a.sftpExists(remoteFile) {
			return "文件已存在，不会覆盖: " + p
		}
		err = a.sftpWrite(remoteFile, b)
	} else {
		if _, statErr := wsRoot.Stat(p); statErr == nil {
			return "文件已存在，不会覆盖: " + p
		} else if !errors.Is(statErr, os.ErrNotExist) {
			return "检查文件失败: " + statErr.Error()
		}
		err = putText(wsRoot, p, b)
	}
	if err != nil {
		return "Office 写入失败: " + err.Error()
	}
	return fmt.Sprintf("已生成 %s（%d 字节）；请从工作目录打开验证", p, len(b))
}

func (a *App) officeReadBytes(p, sourceID string) ([]byte, Source, error) {
	if err := safePath(p); err != nil {
		return nil, Source{}, err
	}
	if sourceID != "" {
		a.mu.Lock()
		src, ok := a.findSource(sourceID)
		a.mu.Unlock()
		if !ok || !src.Enabled {
			return nil, Source{}, errors.New("引用不存在或已停用")
		}
		b, err := a.readSourceRaw(src, p)
		return b, src, err
	}
	if a.workspaceMode() == "ssh" {
		b, err := a.sftpRead(a.workspaceRemotePath(p))
		return b, Source{}, err
	}
	b, err := readRawBytes(a.workspace, p)
	return b, Source{}, err
}

func (a *App) officeWriteBytes(p, sourceID string, src Source, b []byte) error {
	if sourceID != "" {
		if !src.RW {
			return errors.New("该引用为只读")
		}
		return a.writeSourceText(src, p, b)
	}
	return a.writeWorkspaceText(p, b)
}

func (a *App) officeXlsxView(w http.ResponseWriter, r *http.Request) {
	p, sourceID := r.URL.Query().Get("path"), r.URL.Query().Get("source")
	origin, workspace := r.URL.Query().Get("knowledgeOrigin"), r.URL.Query().Get("knowledgeWorkspace")
	knowledgeReadOnly := origin != ""
	// A single URL has a virtual resource.txt path. Only identity-pinned,
	// read-only link/SMB views may use the graph's XLSX format hint.
	virtualXlsx := false
	if knowledgeReadOnly && workspace != "" && sourceID != "" && r.URL.Query().Get("knowledgeFormat") == ".xlsx" {
		a.mu.Lock()
		src, ok := a.findSource(sourceID)
		a.mu.Unlock()
		virtualXlsx = ok && src.Enabled && (src.Type == "link" || src.Type == "smb")
	}
	if effectiveFileExt(p) != ".xlsx" && !virtualXlsx {
		fail(w, 400, errors.New("仅支持 .xlsx"))
		return
	}
	var b []byte
	var src Source
	var err error
	if knowledgeReadOnly {
		b, err = a.knowledgeReadViewer(r.Context(), r.URL.Query().Get("root"), sourceID, p, origin, workspace, 16<<20)
	} else {
		b, src, err = a.officeReadBytes(p, sourceID)
	}
	if err != nil {
		fail(w, 400, err)
		return
	}
	if err := checkOfficeArchive(b); err != nil {
		fail(w, 400, err)
		return
	}
	inputPath, cleanup, err := officeTemp(".xlsx", b)
	if err != nil {
		fail(w, 500, err)
		return
	}
	defer cleanup()
	sp, err := officeScriptPath("office_artifact.py")
	if err != nil {
		fail(w, 500, err)
		return
	}
	row, _ := strconv.Atoi(r.URL.Query().Get("row"))
	col, _ := strconv.Atoi(r.URL.Query().Get("col"))
	if row < 1 {
		row = 1
	}
	if col < 1 {
		col = 1
	}
	if row > 1000000 || col > 16384 {
		fail(w, 400, errors.New("单元格范围无效"))
		return
	}
	out, err := runOfficeScript(os.TempDir(), sp, "xlsx-view", inputPath, r.URL.Query().Get("sheet"), strconv.Itoa(row), strconv.Itoa(col))
	if err != nil {
		fail(w, 400, err)
		return
	}
	var result map[string]any
	if err = json.Unmarshal([]byte(out), &result); err != nil {
		fail(w, 500, err)
		return
	}
	result["hash"] = hash(b)
	result["workspaceId"] = a.wsID()
	result["readOnly"] = knowledgeReadOnly || sourceID != "" && !src.RW
	if sourceID != "" {
		result["workspaceId"] = "source:" + sourceID
	}
	jsonOut(w, 200, result)
}

func (a *App) officeXlsxEdit(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Path        string           `json:"path"`
		Source      string           `json:"source"`
		Hash        string           `json:"hash"`
		WorkspaceID string           `json:"workspaceId"`
		Changes     []map[string]any `json:"changes"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		fail(w, 400, err)
		return
	}
	if strings.ToLower(path.Ext(in.Path)) != ".xlsx" {
		fail(w, 400, errors.New("仅支持 .xlsx"))
		return
	}
	if err := safePath(in.Path); err != nil {
		fail(w, 400, err)
		return
	}
	if in.Hash == "" || len(in.Changes) == 0 || len(in.Changes) > 500 {
		fail(w, 400, errors.New("缺少版本或修改单元格过多"))
		return
	}
	if in.Source == "" && in.WorkspaceID != a.wsID() {
		fail(w, 409, errors.New("工作区已切换，请重新打开"))
		return
	}
	if in.Source != "" && in.WorkspaceID != "source:"+in.Source {
		fail(w, 409, errors.New("引用已切换，请重新打开"))
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
	if err := checkOfficeArchive(b); err != nil {
		fail(w, 400, err)
		return
	}
	if hash(b) != in.Hash {
		fail(w, 409, errors.New("文件已被其他操作修改，请重新打开"))
		return
	}
	inputPath, cleanInput, err := officeTemp(".xlsx", b)
	if err != nil {
		fail(w, 500, err)
		return
	}
	defer cleanInput()
	spec, _ := json.Marshal(map[string]any{"changes": in.Changes})
	changed, err := officeRun(spec, "xlsx-edit", inputPath)
	if err != nil {
		fail(w, 400, err)
		return
	}
	if err = a.officeWriteBytes(in.Path, in.Source, src, changed); err != nil {
		fail(w, 400, err)
		return
	}
	jsonOut(w, 200, map[string]any{"hash": hash(changed), "size": len(changed)})
}
