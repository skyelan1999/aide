package server

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"os"
	"path"
	"strings"
)

//go:embed office_tools.json
var officeToolsJSON []byte

func builtinOfficeTools() []any {
	var tools []any
	if err := json.Unmarshal(officeToolsJSON, &tools); err != nil {
		panic(err)
	}
	return tools
}

type officeDocumentRequest struct {
	Path      string           `json:"path"`
	Source    string           `json:"source"`
	Root      string           `json:"root"`
	ReadOnly  bool             `json:"readOnly"`
	Origin    string           `json:"knowledgeOrigin"`
	Workspace string           `json:"knowledgeWorkspace"`
	Hash      string           `json:"hash"`
	Changes   []map[string]any `json:"changes"`
	Offset    int              `json:"offset"`
}

func (a *App) officeDocumentArea(in officeDocumentRequest) (knowledgeArea, error) {
	if safePath(in.Path) != nil || !officeStructuredExtension(path.Ext(in.Path)) {
		return knowledgeArea{}, errors.New("Office 文件路径无效或为备份文件")
	}
	return a.knowledgeViewerArea(in.Root, in.Source, in.Origin, in.Workspace)
}
func officeAreaWritable(ar knowledgeArea) bool {
	return (ar.source == "" && ar.root == "workspace") || ar.source != "" && ar.src.RW && (ar.kind == "local" || ar.kind == "ssh" || ar.kind == "sftp" || ar.kind == "workspace-sftp" || ar.kind == "relay")
}
func officeStructuredExtension(ext string) bool {
	switch strings.ToLower(ext) {
	case ".docx", ".xlsx", ".pptx":
		return true
	}
	return false
}

func officeDocumentTransform(b []byte, p, operation string, spec any) ([]byte, error) {
	if err := checkOfficeArchive(b); err != nil {
		return nil, err
	}
	ext := strings.ToLower(path.Ext(p))
	if !officeStructuredExtension(ext) {
		return nil, errors.New("AI 结构化读写支持 DOCX/XLSX/PPTX；其他格式可用版式预览")
	}
	input, clean, e := officeTemp(ext, b)
	if e != nil {
		return nil, e
	}
	defer clean()
	output, cleanOutput, e := officeTemp(".office", nil)
	if e != nil {
		return nil, e
	}
	defer cleanOutput()
	script, e := officeScriptPath("office_document.py")
	if e != nil {
		return nil, e
	}
	raw, e := json.Marshal(spec)
	if e != nil || len(raw) > 1<<20 {
		return nil, errors.New("Office 参数无效或过大")
	}
	if _, e = runOfficeScript(os.TempDir(), script, operation, input, strings.TrimPrefix(ext, "."), string(raw), output); e != nil {
		return nil, e
	}
	result, e := os.ReadFile(output)
	if e != nil || len(result) > maxRawFile {
		return nil, errors.New("Office 输出无效或过大")
	}
	if operation == "edit" {
		if e = checkOfficeArchive(result); e != nil {
			return nil, e
		}
	}
	return result, nil
}

// The bounded native executor is called inside Aide by its built-in AI tools.
// AI additionally requires a current task and AI-visible source. It cannot use
// a viewer token or a writable reference to bypass those checks.
func (a *App) officeDocumentOperation(ctx context.Context, in officeDocumentRequest, write, ai bool, expectedWorkspace string) (map[string]any, error) {
	ar, e := a.officeDocumentArea(in)
	if e != nil {
		return nil, e
	}
	if ai {
		a.mu.Lock()
		allowed := expectedWorkspace == "" || a.wsID() == expectedWorkspace
		if ar.source != "" {
			allowed = allowed && sourceAIVisible(ar.src, a.wsID())
		}
		a.mu.Unlock()
		if !allowed {
			return nil, errors.New("工作区已变化或来源对 AI 不可见")
		}
	}
	lock := a.readLockState()
	if lock.Locked {
		return nil, errors.New("工作台已锁定")
	}
	if write && (in.ReadOnly || in.Origin != "" || !officeAreaWritable(ar) || in.Hash == "") {
		return nil, errors.New("来源只读或缺少文件版本")
	}
	if write {
		a.filesMu.Lock()
		defer a.filesMu.Unlock()
	}
	b, e := a.knowledgeAreaRead(ctx, ar, in.Path, maxRawFile)
	if e != nil {
		return nil, e
	}
	if write && hash(b) != in.Hash {
		return nil, errors.New("Office 文件版本冲突，请重新读取")
	}
	op := "read"
	if write {
		op = "edit"
	}
	result, e := officeDocumentTransform(b, in.Path, op, map[string]any{"offset": in.Offset, "changes": in.Changes})
	if e != nil {
		return nil, e
	}
	if !a.knowledgeAreaCurrent(ar) {
		return nil, errors.New("Office 来源已变化")
	}
	if !write {
		var out map[string]any
		if json.Unmarshal(result, &out) != nil {
			return nil, errors.New("Office 解析结果无效")
		}
		out["hash"] = hash(b)
		out["readOnly"] = !officeAreaWritable(ar)
		out["path"] = in.Path
		return out, nil
	}
	mutation, finish, e := a.beginMessageMutation(ctx, lock.Gen)
	if e != nil {
		return nil, e
	}
	defer finish()
	fs, e := a.streamTransferFS(mutation, checkoutSource(ar), true)
	if e != nil {
		return nil, e
	}
	defer fs.Close()
	guard := func() error {
		if !a.knowledgeAreaCurrent(ar) {
			return errors.New("Office 来源已变化")
		}
		current, exists, e := checkoutRead(fs, in.Path, false)
		if e != nil || !exists || hash(current) != in.Hash {
			return errors.New("Office 文件版本冲突")
		}
		return nil
	}
	if e = fs.writeAtomicChecked(in.Path, result, false, guard); e != nil {
		return nil, e
	}
	return map[string]any{"saved": true, "hash": hash(result), "path": in.Path}, nil
}
func (a *App) officeDocumentTool(ctx context.Context, task *Task, args map[string]any, write bool) string {
	raw, e := json.Marshal(args)
	if e != nil {
		return e.Error()
	}
	var in officeDocumentRequest
	if e = json.Unmarshal(raw, &in); e != nil {
		return e.Error()
	}
	in.Root = "workspace"
	in.Source = strings.TrimPrefix(in.Source, "source:")
	out, e := a.officeDocumentOperation(ctx, in, write, true, task.WorkspaceID)
	if e != nil {
		return "Office 操作失败: " + e.Error()
	}
	raw, _ = json.Marshal(out)
	return string(raw)
}
