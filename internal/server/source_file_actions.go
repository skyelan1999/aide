package server

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"path"
	"sort"
	"strings"
)

// Source actions use the same bounded adapters as file transfers. A source ID
// never silently falls back to the workspace, and read-only metadata is allowed.
func (a *App) sourceFileAction(w http.ResponseWriter, root, id, p, operation, newName string) {
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
		dest = p + ".zip"
		if _, e := a.transferEntry(loc, dest); e == nil {
			fail(w, 409, errors.New("压缩目标已存在"))
			return
		} else if !errors.Is(e, os.ErrNotExist) {
			fail(w, 400, e)
			return
		}
		items, e := a.collectArchive(p, loc.source, true, entry["dir"] == true)
		if e != nil {
			fail(w, 400, e)
			return
		}
		data, e := encodeArchive(items)
		if e != nil {
			fail(w, 500, e)
			return
		}
		stage := path.Join(path.Dir(dest), ".aide-archive-"+newID())
		if e = a.transferWrite(loc, stage, data); e == nil {
			e = a.transferRename(loc, stage, dest)
		}
		if e != nil {
			_ = a.transferRemoveTree(loc, stage, false, 0)
			fail(w, 400, e)
			return
		}
	case "extract":
		if !strings.HasSuffix(strings.ToLower(p), ".zip") || entry["dir"] == true {
			fail(w, 400, errors.New("仅支持 ZIP 文件"))
			return
		}
		data, e := a.downloadRaw(p, loc.source, true)
		if e != nil {
			fail(w, 400, e)
			return
		}
		files, e := readArchiveFiles(data)
		if e != nil {
			fail(w, 400, e)
			return
		}
		base := strings.TrimSuffix(p, path.Ext(p))
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
		stage := path.Join(path.Dir(dest), ".aide-unpack-"+newID())
		if e = a.transferMkdir(loc, stage); e != nil {
			fail(w, 400, e)
			return
		}
		dirs := map[string]bool{}
		for _, f := range files {
			d := f.path
			if !f.dir {
				d = path.Dir(d)
			}
			for d != "." && d != "" {
				dirs[d] = true
				d = path.Dir(d)
			}
		}
		ordered := make([]string, 0, len(dirs))
		for d := range dirs {
			ordered = append(ordered, d)
		}
		sort.Slice(ordered, func(i, j int) bool { return strings.Count(ordered[i], "/") < strings.Count(ordered[j], "/") })
		for _, d := range ordered {
			if e = a.transferMkdir(loc, path.Join(stage, d)); e != nil {
				break
			}
		}
		if e == nil {
			for _, f := range files {
				if !f.dir {
					e = a.transferWrite(loc, path.Join(stage, f.path), f.data)
					if e != nil {
						break
					}
				}
			}
		}
		if e == nil {
			e = a.transferRename(loc, stage, dest)
		}
		if e != nil {
			_ = a.transferRemoveTree(loc, stage, true, 0)
			fail(w, 400, e)
			return
		}
	default:
		fail(w, 400, errors.New("未知文件操作"))
		return
	}
	jsonOut(w, 200, map[string]string{"path": dest, "name": path.Base(dest), "source": id})
}
