package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// A transfer is deliberately bounded by collectArchive (1000 entries / 64 MiB).
// Each top-level item is staged beside its destination and published by rename.
type fileTransferRequest struct {
	Operation       string   `json:"operation"`
	Source          string   `json:"source"`
	Paths           []string `json:"paths"`
	Destination     string   `json:"destination"`
	DestinationPath string   `json:"destinationPath"`
}

type transferLocation struct {
	source    Source
	hasSource bool
}

func validTransferPath(p string) error {
	if strings.ContainsAny(p, "\r\n\x00") {
		return errors.New("路径包含不支持的控制字符")
	}
	return validArchivePath(p)
}

func (a *App) transferLocation(id string, writable bool) (transferLocation, error) {
	if id == "workspace" {
		return transferLocation{}, nil
	}
	a.mu.Lock()
	src, ok := a.findSource(id)
	a.mu.Unlock()
	if !ok || !src.Enabled {
		return transferLocation{}, errors.New("来源不存在或已停用")
	}
	if writable && (!src.RW || (src.Type != "local" && src.Type != "skill" && src.Type != "sftp" && src.Type != "workspace-sftp")) {
		return transferLocation{}, errors.New("目标来源不可写")
	}
	return transferLocation{source: src, hasSource: true}, nil
}

func (a *App) transferChildren(loc transferLocation, p string) ([]map[string]any, error) {
	if loc.hasSource {
		return a.listSourceDir(loc.source, p)
	}
	return a.listWorkspaceDir(p)
}
func (a *App) transferEntry(loc transferLocation, p string) (map[string]any, error) {
	items, err := a.transferChildren(loc, path.Dir(p))
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
func (a *App) transferDirectoryExists(loc transferLocation, p string) error {
	if p == "." {
		_, err := a.transferChildren(loc, p)
		return err
	}
	item, err := a.transferEntry(loc, p)
	if err != nil {
		return err
	}
	if item["dir"] != true {
		return errors.New("目标不是文件夹")
	}
	return nil
}
func (a *App) transferRemote(loc transferLocation, p string) (string, bool) {
	if !loc.hasSource && a.workspaceMode() == "ssh" {
		return a.workspaceRemotePath(p), true
	}
	if loc.hasSource && loc.source.Type == "workspace-sftp" {
		return a.workspaceRemoteSourcePath(loc.source.Config.Path, p), true
	}
	if loc.hasSource && loc.source.Type == "sftp" {
		return pathJoinRemote(loc.source.Config.Path, p), true
	}
	return "", false
}
func (a *App) transferIdentity(loc transferLocation, p string) (string, error) {
	if remote, ok := a.transferRemote(loc, p); ok {
		host := "workspace"
		if loc.hasSource && loc.source.Type == "sftp" {
			host = "source:" + sftpConnIdentity(loc.source)
		}
		return host + ":" + path.Clean(remote), nil
	}
	root, closeRoot, err := a.transferLocalRoot(loc)
	if err != nil {
		return "", err
	}
	if closeRoot {
		defer root.Close()
	}
	return "local:" + filepath.ToSlash(filepath.Clean(filepath.Join(root.Name(), filepath.FromSlash(p)))), nil
}
func (a *App) transferLocalRoot(loc transferLocation) (*os.Root, bool, error) {
	if !loc.hasSource {
		return a.workspace, false, nil
	}
	root, err := a.localSourceRoot(loc.source)
	return root, true, err
}
func (a *App) transferRemoteBatch(loc transferLocation, batch string) error {
	var out string
	var err error
	if loc.hasSource && loc.source.Type == "sftp" {
		out, err = a.sftpBatchSource(loc.source, batch)
	} else {
		out, err = a.sftpBatch(batch)
	}
	if err != nil {
		return err
	}
	if sftpCommandFailed(out) {
		return errors.New(strings.TrimSpace(out))
	}
	return nil
}
func (a *App) transferMkdir(loc transferLocation, p string) error {
	if err := validTransferPath(p); err != nil {
		return err
	}
	if remote, ok := a.transferRemote(loc, p); ok {
		return a.transferRemoteBatch(loc, "mkdir "+shellQuoteRemote(remote)+"\n")
	}
	root, closeRoot, err := a.transferLocalRoot(loc)
	if err != nil {
		return err
	}
	if closeRoot {
		defer root.Close()
	}
	return root.Mkdir(p, 0755)
}
func (a *App) transferWrite(loc transferLocation, p string, b []byte) error {
	if err := validTransferPath(p); err != nil {
		return err
	}
	if remote, ok := a.transferRemote(loc, p); ok {
		if loc.hasSource && loc.source.Type == "sftp" {
			return a.sftpWriteSource(loc.source, p, b)
		}
		return a.sftpWrite(remote, b)
	}
	root, closeRoot, err := a.transferLocalRoot(loc)
	if err != nil {
		return err
	}
	if closeRoot {
		defer root.Close()
	}
	f, err := root.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		return err
	}
	if _, err = f.Write(b); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}
func (a *App) transferRename(loc transferLocation, oldPath, newPath string) error {
	if err := validTransferPath(oldPath); err != nil {
		return err
	}
	if err := validTransferPath(newPath); err != nil {
		return err
	}
	if _, err := a.transferEntry(loc, newPath); err == nil {
		return errors.New("目标已存在，不会覆盖")
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if remoteOld, ok := a.transferRemote(loc, oldPath); ok {
		remoteNew, _ := a.transferRemote(loc, newPath)
		return a.transferRemoteBatch(loc, "rename "+shellQuoteRemote(remoteOld)+" "+shellQuoteRemote(remoteNew)+"\n")
	}
	root, closeRoot, err := a.transferLocalRoot(loc)
	if err != nil {
		return err
	}
	if closeRoot {
		defer root.Close()
	}
	return root.Rename(oldPath, newPath)
}
func (a *App) transferRemoveTree(loc transferLocation, p string, dir bool, depth int) error {
	if depth > maxArchiveDepth {
		return errors.New("目录层级超过限制")
	}
	if dir {
		children, err := a.transferChildren(loc, p)
		if err != nil {
			return err
		}
		for _, item := range children {
			child, ok := item["path"].(string)
			if !ok || validTransferPath(child) != nil {
				return errors.New("目录包含无效路径")
			}
			if err := a.transferRemoveTree(loc, child, item["dir"] == true, depth+1); err != nil {
				return err
			}
		}
	}
	if remote, ok := a.transferRemote(loc, p); ok {
		command := "rm "
		if dir {
			command = "rmdir "
		}
		return a.transferRemoteBatch(loc, command+shellQuoteRemote(remote)+"\n")
	}
	root, closeRoot, err := a.transferLocalRoot(loc)
	if err != nil {
		return err
	}
	if closeRoot {
		defer root.Close()
	}
	return root.Remove(p)
}
func (a *App) transferFiles(w http.ResponseWriter, r *http.Request) {
	var req fileTransferRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 128<<10)).Decode(&req); err != nil {
		fail(w, 400, err)
		return
	}
	if req.Operation != "copy" && req.Operation != "move" {
		fail(w, 400, errors.New("操作只能是复制或移动"))
		return
	}
	if len(req.Paths) == 0 || len(req.Paths) > 100 {
		fail(w, 400, errors.New("请选择 1–100 个文件或文件夹"))
		return
	}
	if req.DestinationPath == "" {
		req.DestinationPath = "."
	}
	if req.DestinationPath != "." && validTransferPath(req.DestinationPath) != nil {
		fail(w, 400, errors.New("目标路径无效"))
		return
	}
	a.filesMu.Lock()
	defer a.filesMu.Unlock()
	src, err := a.transferLocation(req.Source, false)
	if err != nil {
		fail(w, 400, err)
		return
	}
	dst, err := a.transferLocation(req.Destination, true)
	if err != nil {
		fail(w, 400, err)
		return
	}
	if req.Operation == "move" && src.hasSource && !src.source.RW {
		fail(w, 403, errors.New("只读引用不能移动"))
		return
	}
	if err := a.transferDirectoryExists(dst, req.DestinationPath); err != nil {
		fail(w, 400, err)
		return
	}
	type item struct {
		old, target string
		dir         bool
		content     []archiveItem
	}
	planned := make([]item, 0, len(req.Paths))
	seen := map[string]bool{}
	for _, p := range req.Paths {
		if validTransferPath(p) != nil || seen[p] {
			fail(w, 400, errors.New("源路径无效或重复"))
			return
		}
		seen[p] = true
		for other := range seen {
			if other != p && (strings.HasPrefix(p, other+"/") || strings.HasPrefix(other, p+"/")) {
				fail(w, 400, errors.New("不能同时选择文件夹及其子项"))
				return
			}
		}
		entry, err := a.transferEntry(src, p)
		if err != nil {
			fail(w, 400, err)
			return
		}
		target := path.Join(req.DestinationPath, path.Base(p))
		if validTransferPath(target) != nil {
			fail(w, 400, errors.New("目标路径无效"))
			return
		}
		srcIdentity, err := a.transferIdentity(src, p)
		if err != nil {
			fail(w, 400, err)
			return
		}
		dstIdentity, err := a.transferIdentity(dst, target)
		if err != nil {
			fail(w, 400, err)
			return
		}
		if dstIdentity == srcIdentity || (entry["dir"] == true && strings.HasPrefix(dstIdentity, srcIdentity+"/")) {
			fail(w, 400, errors.New("不能复制或移动到自身及其子目录"))
			return
		}
		if _, err := a.transferEntry(dst, target); err == nil {
			fail(w, 409, fmt.Errorf("目标已存在：%s", target))
			return
		} else if !errors.Is(err, os.ErrNotExist) {
			fail(w, 400, err)
			return
		}
		planned = append(planned, item{old: p, target: target, dir: entry["dir"] == true})
	}
	totalBytes, totalEntries := 0, 0
	for i := range planned {
		p := &planned[i]
		content, err := a.collectArchive(p.old, src.source, src.hasSource, p.dir)
		if err != nil {
			fail(w, 400, err)
			return
		}
		p.content = content
		totalEntries += len(content)
		for _, part := range content {
			totalBytes += len(part.data)
		}
		if totalEntries > maxArchiveFiles || totalBytes > maxRawFile {
			fail(w, 400, errors.New("所选文件超过 1000 项或 64 MiB 上限"))
			return
		}
	}
	copied := 0
	for _, p := range planned {
		stage := path.Join(path.Dir(p.target), ".aide-transfer-"+newID())
		var err error
		if p.dir {
			err = a.transferMkdir(dst, stage)
		} else {
			err = a.transferWrite(dst, stage, p.content[0].data)
		}
		if err == nil && p.dir {
			for _, part := range p.content[1:] {
				rel := strings.TrimPrefix(strings.TrimSuffix(part.name, "/"), path.Base(p.old)+"/")
				target := path.Join(stage, rel)
				if part.dir {
					err = a.transferMkdir(dst, target)
				} else {
					err = a.transferWrite(dst, target, part.data)
				}
				if err != nil {
					break
				}
			}
		}
		if err == nil {
			err = a.transferRename(dst, stage, p.target)
		}
		if err != nil {
			_ = a.transferRemoveTree(dst, stage, p.dir, 0)
			fail(w, 500, fmt.Errorf("已复制 %d 项；%s 失败：%w", copied, p.old, err))
			return
		}
		copied++
		if req.Operation == "move" {
			if err := a.transferRemoveTree(src, p.old, p.dir, 0); err != nil {
				fail(w, 500, fmt.Errorf("目标已复制，但删除源 %s 失败，请检查后手动清理：%w", p.old, err))
				return
			}
		}
	}
	jsonOut(w, 200, map[string]any{"count": copied, "operation": req.Operation})
}
