package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path"
)

// Source actions use the same bounded adapters as file transfers. A source ID
// never silently falls back to the workspace, and read-only metadata is allowed.
func (a *App) sourceFileAction(w http.ResponseWriter, root, id, p, operation, newName string) {
	a.sourceFileActionFormat(w, root, id, p, operation, newName, "")
}

func (a *App) sourceFileActionFormat(w http.ResponseWriter, root, id, p, operation, newName, format string) {
	a.sourceFileActionFormatContext(context.Background(), w, root, id, p, operation, newName, format)
}

func (a *App) sourceFileActionContext(ctx context.Context, w http.ResponseWriter, root, id, p, operation, newName string) {
	a.sourceFileActionFormatContext(ctx, w, root, id, p, operation, newName, "")
}

func (a *App) sourceFileActionFormatContext(ctx context.Context, w http.ResponseWriter, root, id, p, operation, newName, format string) {
	if root != "" && root != "context" || id == "workspace" || validTransferPath(p) != nil {
		fail(w, 400, errors.New("引用来源或路径无效"))
		return
	}
	a.filesMu.Lock()
	defer a.filesMu.Unlock()
	loc, err := a.transferLocation(id, operation != "properties")
	if err != nil {
		fail(w, 403, err)
		return
	}
	entry, err := a.transferEntry(loc, p)
	if err != nil {
		fail(w, 400, err)
		return
	}
	if operation == "properties" {
		if loc.source.Type == "local" || loc.source.Type == "skill" {
			local, e := a.localSourceRoot(loc.source)
			if e != nil {
				fail(w, 400, e)
				return
			}
			info, e := local.Stat(p)
			local.Close()
			if e != nil {
				fail(w, 400, e)
				return
			}
			payload := fileInfoPayload(p, info)
			payload["source"], payload["rw"] = id, loc.source.RW
			jsonOut(w, 200, payload)
			return
		}
		typ := "文件"
		if entry["dir"] == true {
			typ = "文件夹"
		}
		jsonOut(w, 200, map[string]any{"name": entry["name"], "path": p, "dir": entry["dir"], "type": typ, "size": entry["size"], "modified": entry["modified"], "source": id, "rw": loc.source.RW})
		return
	}
	dest := p
	switch operation {
	case "delete":
		if err = a.transferRemoveTree(loc, p, entry["dir"] == true, 0); err != nil {
			fail(w, 400, err)
			return
		}
	case "rename":
		name, e := validDirectoryName(newName)
		if e != nil {
			fail(w, 400, e)
			return
		}
		dest = path.Join(path.Dir(p), name)
		if _, e := a.transferEntry(loc, dest); e == nil {
			fail(w, 409, errors.New("已存在同名文件或文件夹"))
			return
		} else if !errors.Is(e, os.ErrNotExist) {
			fail(w, 400, e)
			return
		}
		if err = a.transferRename(loc, p, dest); err != nil {
			fail(w, 400, err)
			return
		}
	case "archive":
		format, e := normalizeArchiveFormat(format)
		if e != nil {
			fail(w, 400, e)
			return
		}
		dest = archiveNameForFormat(p, format)
		if _, e := a.transferEntry(loc, dest); e == nil {
			fail(w, 409, errors.New("压缩目标已存在"))
			return
		} else if !errors.Is(e, os.ErrNotExist) {
			fail(w, 400, e)
			return
		}
		items, e := a.collectArchiveContext(ctx, p, loc.source, true, entry["dir"] == true)
		if e != nil {
			fail(w, 400, e)
			return
		}
		defer cleanupArchiveItems(items)
		fs, e := a.streamTransferFS(ctx, id, true)
		if e != nil {
			fail(w, 400, e)
			return
		}
		defer fs.Close()
		if e = writeArchiveToTransferFS(ctx, fs, dest, items, format); e != nil {
			fail(w, 400, e)
			return
		}
	case "extract":
		if archiveExtension(p) == "" || entry["dir"] == true {
			fail(w, 400, errors.New("不支持此归档格式"))
			return
		}
		fs, e := a.streamTransferFS(ctx, id, true)
		if e != nil {
			fail(w, 400, e)
			return
		}
		defer fs.Close()
		input, e := fs.Open(p, os.O_RDONLY)
		if e != nil {
			fail(w, 400, e)
			return
		}
		info, e := input.Stat()
		if e != nil || !info.Mode().IsRegular() {
			_ = input.Close()
			fail(w, 400, errors.New("归档输入不是普通文件"))
			return
		}
		files, e := readArchiveFilesFromReader(ctx, input, info.Size(), p)
		closeErr := input.Close()
		if e = errors.Join(e, closeErr); e != nil {
			fail(w, 400, e)
			return
		}
		defer cleanupExtractedArchive(files)
		base := archiveBaseForPath(p)
		// Fail on a listing/connection error instead of treating it as absence.
		for n := 0; n < 1000; n++ {
			candidate := base
			if n > 0 {
				candidate = fmt.Sprintf("%s-%d", base, n)
			}
			_, e = a.transferEntry(loc, candidate)
			if errors.Is(e, os.ErrNotExist) {
				dest = candidate
				break
			}
			if e != nil {
				fail(w, 400, e)
				return
			}
			if n == 999 {
				fail(w, 409, errors.New("解压目标名称已用尽"))
				return
			}
		}
		if validTransferPath(dest) != nil {
			fail(w, 400, errors.New("解压目标路径无效"))
			return
		}
		if e = writeExtractedArchiveContext(ctx, fs, dest, files); e != nil {
			fail(w, 400, e)
			return
		}
	default:
		fail(w, 400, errors.New("未知文件操作"))
		return
	}
	jsonOut(w, 200, map[string]string{"path": dest, "name": path.Base(dest), "source": id})
}
