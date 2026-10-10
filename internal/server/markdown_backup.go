package server

import (
	"archive/zip"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

const markdownBackupLimit = 64 << 20

// A document backup includes every indexed revision, even those outside the
// recent-200 UI window. It never includes credentials or unrelated documents.
func (a *App) markdownBackupData(source, p string, archive bool) (map[string]any, error) {
	if !strings.EqualFold(path.Ext(p), ".md") && !strings.EqualFold(path.Ext(p), ".markdown") {
		return nil, errors.New("只支持 Markdown 历史备份")
	}
	identity, _, err := a.historyTarget(source, p)
	if err != nil {
		return nil, err
	}
	a.historyMu.Lock()
	defer a.historyMu.Unlock()
	dir := a.markdownHistoryDir(identity)
	index, err := loadMarkdownIndex(dir, p)
	if err != nil {
		return nil, err
	}
	if index.Path != p {
		return nil, errors.New("历史索引路径不匹配")
	}
	digests := map[string]bool{}
	unavailable := 0
	for _, v := range index.Versions {
		digests[v.Content] = true
		for _, asset := range v.Assets {
			if asset.Status == "stored" {
				digests[asset.Digest] = true
			} else {
				unavailable++
			}
		}
	}
	keys := make([]string, 0, len(digests))
	for digest := range digests {
		keys = append(keys, digest)
	}
	sort.Strings(keys)
	var total int64
	for _, digest := range keys {
		if len(digest) != 64 || strings.Trim(digest, "0123456789abcdef") != "" {
			return nil, errors.New("历史对象摘要无效")
		}
		info, err := os.Stat(filepath.Join(dir, "objects", digest))
		if err != nil {
			return nil, err
		}
		if !info.Mode().IsRegular() {
			return nil, errors.New("历史对象不是普通文件")
		}
		total += info.Size()
	}
	result := map[string]any{"path": p, "versions": len(index.Versions), "objects": len(keys), "objectBytes": total, "unavailableAssets": unavailable, "limitBytes": markdownBackupLimit, "integrityChecked": false}
	if !archive {
		return result, nil
	}
	if total > markdownBackupLimit {
		return nil, errors.New("历史备份超过 64 MiB；请通过服务数据卷备份，不会导出不完整版本")
	}
	manifest, err := json.MarshalIndent(map[string]any{"format": "aide-markdown-history", "schema": 1, "identityDigest": hash([]byte(identity)), "history": index, "unavailableAssets": unavailable}, "", "  ")
	if err != nil {
		return nil, err
	}
	if int64(len(manifest))+total > markdownBackupLimit {
		return nil, errors.New("历史备份超过 64 MiB；请通过服务数据卷备份，不会导出不完整版本")
	}
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	appendObject := func(name string, b []byte) error {
		entry, err := writer.Create(name)
		if err != nil {
			return err
		}
		_, err = entry.Write(b)
		return err
	}
	if err := appendObject("manifest.json", manifest); err != nil {
		return nil, err
	}
	var readBytes int64 = int64(len(manifest))
	for _, digest := range keys {
		b, err := readHistoryObject(dir, digest)
		if err != nil {
			return nil, err
		}
		readBytes += int64(len(b))
		if readBytes > markdownBackupLimit {
			return nil, errors.New("历史备份超过 64 MiB")
		}
		if err := appendObject("objects/"+digest, b); err != nil {
			return nil, err
		}
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	result["integrityChecked"] = true
	result["base64"] = base64.StdEncoding.EncodeToString(buffer.Bytes())
	result["filename"] = path.Base(p) + ".history.zip"
	result["archiveBytes"] = buffer.Len()
	return result, nil
}

func (a *App) markdownBackupAPI(w http.ResponseWriter, r *http.Request) {
	a.filesMu.Lock()
	defer a.filesMu.Unlock()
	if ws := r.URL.Query().Get("workspaceId"); r.URL.Query().Get("source") == "" && ws != "" && ws != a.wsID() {
		fail(w, 409, errors.New("工作区已切换"))
		return
	}
	data, err := a.markdownBackupData(r.URL.Query().Get("source"), r.URL.Query().Get("path"), r.URL.Query().Get("archive") == "1")
	if err != nil {
		fail(w, 400, err)
		return
	}
	jsonOut(w, 200, data)
}
