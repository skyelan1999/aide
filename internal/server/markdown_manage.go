package server

import (
	"archive/zip"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

type markdownRetention struct {
	KeepLatest int `json:"keepLatest"`
	KeepDays   int `json:"keepDays"`
}

func (p markdownRetention) validate() error {
	if p.KeepLatest < 0 || p.KeepLatest > 10000 || p.KeepDays < 0 || p.KeepDays > 36500 {
		return errors.New("历史保留设置超出范围")
	}
	return nil
}
func loadMarkdownRetention(dir, p string) (markdownRetention, error) {
	var policy markdownRetention
	b, err := os.ReadFile(historyIndexPath(dir, p) + ".policy")
	if errors.Is(err, os.ErrNotExist) {
		return policy, nil
	}
	if err != nil {
		return policy, err
	}
	if err = json.Unmarshal(b, &policy); err != nil {
		return policy, err
	}
	return policy, policy.validate()
}
func retainMarkdown(index markdownHistoryIndex, policy markdownRetention, now time.Time) markdownHistoryIndex {
	if policy.KeepLatest == 0 && policy.KeepDays == 0 {
		return index
	}
	retained := make([]markdownRevision, 0, len(index.Versions))
	for i, v := range index.Versions {
		keep := i == len(index.Versions)-1
		withinCount := policy.KeepLatest == 0 || i >= len(index.Versions)-policy.KeepLatest
		created, err := time.Parse(time.RFC3339Nano, v.Created)
		withinDays := policy.KeepDays == 0 || err != nil || !created.Before(now.AddDate(0, 0, -policy.KeepDays))
		if keep || withinCount && withinDays {
			retained = append(retained, v)
		}
	}
	index.Versions = retained
	return index
}
func nextMarkdownRevision(index markdownHistoryIndex) string {
	max := 0
	for _, v := range index.Versions {
		if n, err := strconv.Atoi(v.ID); err == nil && n > max {
			max = n
		}
	}
	return fmt.Sprintf("%06d", max+1)
}

func markdownFingerprint(content string, assets []markdownAsset) string {
	b, _ := json.Marshal(struct {
		Content string
		Assets  []markdownAsset
	}{content, assets})
	return hash(b)
}

type markdownImportedArchive struct {
	History markdownHistoryIndex
	Objects map[string][]byte
	Digest  string
}

// ZIP entry bytes are never extracted to a client-selected path. Both compressed
// and expanded sizes are bounded; duplicate/unreferenced entries are rejected.
func readMarkdownArchive(raw []byte, p string) (markdownImportedArchive, error) {
	out := markdownImportedArchive{Objects: map[string][]byte{}, Digest: hash(raw)}
	if len(raw) > markdownBackupLimit {
		return out, errors.New("历史备份超过 64 MiB")
	}
	zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return out, err
	}
	if len(zr.File) > 20000 {
		return out, errors.New("历史备份条目过多")
	}
	seen := map[string]bool{}
	var expanded int64
	var manifest []byte
	for _, entry := range zr.File {
		if seen[entry.Name] || !entry.Mode().IsRegular() {
			return out, errors.New("历史备份重复或非普通文件条目")
		}
		seen[entry.Name] = true
		if entry.UncompressedSize64 > markdownBackupLimit {
			return out, errors.New("历史备份解压大小超限")
		}
		expanded += int64(entry.UncompressedSize64)
		if expanded > markdownBackupLimit {
			return out, errors.New("历史备份解压大小超限")
		}
		r, err := entry.Open()
		if err != nil {
			return out, err
		}
		b, err := io.ReadAll(io.LimitReader(r, markdownBackupLimit+1))
		r.Close()
		if err != nil || len(b) > markdownBackupLimit {
			return out, errors.New("历史备份条目读取失败")
		}
		if entry.Name == "manifest.json" {
			manifest = b
			continue
		}
		digest := strings.TrimPrefix(entry.Name, "objects/")
		if entry.Name != "objects/"+digest || len(digest) != 64 || hash(b) != digest {
			return out, errors.New("历史对象路径或摘要无效")
		}
		out.Objects[digest] = b
	}
	var m struct {
		Format  string               `json:"format"`
		Schema  int                  `json:"schema"`
		History markdownHistoryIndex `json:"history"`
	}
	if json.Unmarshal(manifest, &m) != nil || m.Format != "aide-markdown-history" || m.Schema != 1 || m.History.Path != p || len(m.History.Versions) > 10000 {
		return out, errors.New("历史备份清单或目标路径不匹配")
	}
	needed := map[string]bool{}
	ids := map[string]bool{}
	for _, v := range m.History.Versions {
		if v.ID == "" || ids[v.ID] || len(v.Assets) > 64 {
			return out, errors.New("历史版本编号或资源数量无效")
		}
		ids[v.ID] = true
		if n, err := strconv.Atoi(v.ID); err != nil || n < 1 || n > 1000000000 {
			return out, errors.New("历史版本编号无效")
		}
		if _, err := time.Parse(time.RFC3339Nano, v.Created); err != nil {
			return out, errors.New("历史版本时间无效")
		}
		if _, ok := out.Objects[v.Content]; !ok {
			return out, errors.New("缺少历史正文对象")
		}
		needed[v.Content] = true
		refs := map[string]bool{}
		for _, ref := range markdownReferences(p, out.Objects[v.Content]) {
			refs[ref] = true
		}
		assets := map[string]bool{}
		total := 0
		for _, asset := range v.Assets {
			if safePath(asset.Path) != nil || !refs[asset.Path] || assets[asset.Path] {
				return out, errors.New("历史资源路径无效或重复")
			}
			assets[asset.Path] = true
			if asset.Status == "stored" {
				b, ok := out.Objects[asset.Digest]
				if !ok || len(b) != asset.Bytes {
					return out, errors.New("缺少历史资源对象或大小不匹配")
				}
				total += len(b)
				needed[asset.Digest] = true
			} else if asset.Status != "unavailable" || asset.Digest != "" || asset.Bytes != 0 {
				return out, errors.New("历史资源状态无效")
			}
		}
		if total > 32<<20 || markdownFingerprint(v.Content, v.Assets) != v.Fingerprint {
			return out, errors.New("历史版本指纹或资源大小无效")
		}
	}
	if len(needed) != len(out.Objects) {
		return out, errors.New("历史备份包含未引用对象")
	}
	out.History = m.History
	return out, nil
}

// Scan every index and restore journal in this identity. Unknown metadata,
// symlinks, malformed JSON or excessive metadata block GC rather than guessing.
func markdownGarbagePlan(dir string) (map[string]any, error) {
	referenced := map[string]bool{}
	var evidence []string
	var scan func(any)
	scan = func(v any) {
		switch x := v.(type) {
		case string:
			if len(x) == 64 && strings.Trim(x, "0123456789abcdef") == "" {
				referenced[x] = true
			}
		case []any:
			for _, a := range x {
				scan(a)
			}
		case map[string]any:
			for _, a := range x {
				scan(a)
			}
		}
	}
	var total int64
	err := filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if errors.Is(err, os.ErrNotExist) && p == dir {
			return nil
		}
		if err != nil {
			return err
		}
		if d.Type()&os.ModeSymlink != 0 {
			return errors.New("历史目录含符号链接，清理已停止")
		}
		if d.IsDir() {
			if d.Name() == "objects" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".json") {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		total += info.Size()
		if total > markdownBackupLimit {
			return errors.New("历史元数据过大，清理已停止")
		}
		f, err := os.Open(p)
		if err != nil {
			return err
		}
		b, err := io.ReadAll(io.LimitReader(f, markdownBackupLimit+1))
		f.Close()
		if err != nil {
			return err
		}
		if len(b) > markdownBackupLimit {
			return errors.New("历史元数据过大")
		}
		rel, _ := filepath.Rel(dir, p)
		if filepath.Dir(rel) == "." {
			var index markdownHistoryIndex
			if json.Unmarshal(b, &index) != nil || index.Path == "" || safePath(index.Path) != nil || filepath.Base(p) != hash([]byte(index.Path))+".json" || index.Versions == nil {
				return errors.New("历史索引结构无效，清理已停止")
			}
			for _, v := range index.Versions {
				if len(v.Content) != 64 || strings.Trim(v.Content, "0123456789abcdef") != "" {
					return errors.New("历史引用摘要无效，清理已停止")
				}
				for _, asset := range v.Assets {
					if asset.Status == "stored" && (len(asset.Digest) != 64 || strings.Trim(asset.Digest, "0123456789abcdef") != "") {
						return errors.New("历史资源摘要无效，清理已停止")
					}
				}
			}
		} else if filepath.Dir(rel) == "restores" {
			var journal markdownRestoreJournal
			if json.Unmarshal(b, &journal) != nil || journal.ID == "" || journal.Plan.Identity == "" || journal.Plan.Files == nil {
				return errors.New("恢复记录结构无效，清理已停止")
			}
			for _, f := range journal.Plan.Files {
				if len(f.After) != 64 || (f.Before != "missing" && len(f.Before) != 64) {
					return errors.New("恢复记录摘要无效，清理已停止")
				}
			}
		} else {
			return errors.New("发现未知历史元数据，清理已停止")
		}
		var value any
		if json.Unmarshal(b, &value) != nil {
			return errors.New("历史元数据损坏，清理已停止")
		}
		scan(value)
		evidence = append(evidence, rel+":"+hash(b))
		return nil
	})
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(filepath.Join(dir, "objects"))
	if errors.Is(err, os.ErrNotExist) {
		entries = nil
	} else if err != nil {
		return nil, err
	}
	garbage := []string{}
	var reclaim int64
	for _, entry := range entries {
		name := entry.Name()
		if len(name) != 64 || strings.Trim(name, "0123456789abcdef") != "" {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return nil, err
		}
		if !info.Mode().IsRegular() {
			return nil, errors.New("历史对象不是普通文件")
		}
		evidence = append(evidence, "objects/"+name+":"+fmt.Sprint(info.Size())+":"+fmt.Sprint(info.ModTime().UnixNano()))
		if !referenced[name] {
			garbage = append(garbage, name)
			reclaim += info.Size()
		}
	}
	sort.Strings(evidence)
	sort.Strings(garbage)
	b, _ := json.Marshal(evidence)
	return map[string]any{"revision": hash(b), "objects": garbage, "objectCount": len(garbage), "reclaimBytes": reclaim}, nil
}

type markdownManageRequest struct {
	Action        string            `json:"action"`
	Path          string            `json:"path"`
	Source        string            `json:"source"`
	WorkspaceID   string            `json:"workspaceId"`
	Identity      string            `json:"identity"`
	Revision      string            `json:"revision"`
	Archive       string            `json:"archive"`
	ArchiveDigest string            `json:"archiveDigest"`
	Policy        markdownRetention `json:"policy"`
}

func (a *App) markdownManageAPI(w http.ResponseWriter, r *http.Request) {
	var in markdownManageRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 90<<20))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&in) != nil {
		fail(w, 400, errors.New("历史管理请求无效"))
		return
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		fail(w, 400, errors.New("历史管理请求必须为单一对象"))
		return
	}
	if !strings.HasSuffix(strings.ToLower(in.Path), ".md") && !strings.HasSuffix(strings.ToLower(in.Path), ".markdown") {
		fail(w, 400, errors.New("只支持 Markdown 历史管理"))
		return
	}
	a.filesMu.Lock()
	defer a.filesMu.Unlock()
	if in.Source == "" && in.WorkspaceID != a.wsID() {
		fail(w, 409, errors.New("工作区已切换"))
		return
	}
	identity, _, err := a.historyTarget(in.Source, in.Path)
	if err != nil {
		fail(w, 400, err)
		return
	}
	identityDigest := hash([]byte(identity))
	dir := a.markdownHistoryDir(identity)
	a.historyMu.Lock()
	defer a.historyMu.Unlock()
	index, err := loadMarkdownIndex(dir, in.Path)
	if err != nil {
		fail(w, 400, err)
		return
	}
	policy, err := loadMarkdownRetention(dir, in.Path)
	if err != nil {
		fail(w, 400, err)
		return
	}
	snapshot, _ := json.Marshal(struct {
		Index  markdownHistoryIndex
		Policy markdownRetention
	}{index, policy})
	revision := hash(snapshot)
	reply := map[string]any{"identity": identityDigest, "revision": revision, "versions": len(index.Versions), "policy": policy}
	mutation := in.Action == "import" || in.Action == "retention" || in.Action == "gc"
	if mutation && (in.Identity != identityDigest || in.Revision != revision) {
		fail(w, 409, errors.New("历史或来源已变化，请重新预览"))
		return
	}
	switch in.Action {
	case "status":
	case "import-preview", "import":
		raw, err := base64.StdEncoding.DecodeString(in.Archive)
		if err != nil {
			fail(w, 400, err)
			return
		}
		archive, err := readMarkdownArchive(raw, in.Path)
		if err != nil {
			fail(w, 400, err)
			return
		}
		if in.Action == "import" && archive.Digest != in.ArchiveDigest {
			fail(w, 409, errors.New("导入备份已变化"))
			return
		}
		merged := index
		merged.Versions = append([]markdownRevision{}, index.Versions...)
		fingerprints := map[string]bool{}
		for _, v := range index.Versions {
			fingerprints[v.Created+"|"+v.Fingerprint] = true
		}
		added := 0
		for _, v := range archive.History.Versions {
			if fingerprints[v.Created+"|"+v.Fingerprint] {
				continue
			}
			v.ID = nextMarkdownRevision(merged)
			merged.Versions = append(merged.Versions, v)
			fingerprints[v.Created+"|"+v.Fingerprint] = true
			added++
		}
		if len(merged.Versions) > 10000 {
			fail(w, 400, errors.New("导入后历史版本超过 10000 条"))
			return
		}
		reply["added"] = added
		reply["archiveDigest"] = archive.Digest
		if in.Action == "import" {
			// Validate existing objects before mutating metadata; immutable object writes
			// precede one atomic index commit. Interrupted orphan writes are GC candidates.
			for digest := range archive.Objects {
				if info, err := os.Lstat(filepath.Join(dir, "objects", digest)); err == nil {
					if !info.Mode().IsRegular() {
						fail(w, 400, errors.New("历史对象不是普通文件"))
						return
					}
					if _, err = readHistoryObject(dir, digest); err != nil {
						fail(w, 400, err)
						return
					}
				} else if !errors.Is(err, os.ErrNotExist) {
					fail(w, 400, err)
					return
				}
			}
			for _, b := range archive.Objects {
				if _, err := storeHistoryObject(dir, b); err != nil {
					fail(w, 500, err)
					return
				}
			}
			if err := atomicJSON(historyIndexPath(dir, in.Path), merged); err != nil {
				fail(w, 500, err)
				return
			}
			reply["versions"] = len(merged.Versions)
		}
	case "retention-preview", "retention":
		if err := in.Policy.validate(); err != nil {
			fail(w, 400, err)
			return
		}
		retained := retainMarkdown(index, in.Policy, time.Now().UTC())
		reply["retained"] = len(retained.Versions)
		reply["removed"] = len(index.Versions) - len(retained.Versions)
		reply["policy"] = in.Policy
		if in.Action == "retention" {
			if err := os.MkdirAll(dir, 0700); err != nil {
				fail(w, 500, err)
				return
			}
			// Persist opt-in policy first. Metadata pruning is a separate atomic commit;
			// an interrupted request cannot lose object bytes or corrupt the index.
			if err := atomicJSON(historyIndexPath(dir, in.Path)+".policy", in.Policy); err != nil {
				fail(w, 500, err)
				return
			}
			if err := atomicJSON(historyIndexPath(dir, in.Path), retained); err != nil {
				if rollbackErr := atomicJSON(historyIndexPath(dir, in.Path)+".policy", policy); rollbackErr != nil {
					fail(w, 500, fmt.Errorf("保留索引提交失败且策略恢复失败: %v; %v", err, rollbackErr))
					return
				}
				fail(w, 500, err)
				return
			}
		}
	case "gc-preview", "gc":
		plan, err := markdownGarbagePlan(dir)
		if err != nil {
			fail(w, 400, err)
			return
		}
		reply["gc"] = plan
		if in.Action == "gc" {
			if in.ArchiveDigest != plan["revision"] {
				fail(w, 409, errors.New("历史对象已变化，请重新预览"))
				return
			}
			removed := 0
			for _, digest := range plan["objects"].([]string) {
				if err := os.Remove(filepath.Join(dir, "objects", digest)); err != nil {
					jsonOut(w, 500, map[string]any{"error": err.Error(), "removedObjects": removed})
					return
				}
				removed++
			}
			reply["removedObjects"] = removed
		}
	default:
		fail(w, 400, errors.New("历史管理操作无效"))
		return
	}
	jsonOut(w, 200, reply)
}
