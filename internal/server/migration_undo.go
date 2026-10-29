package server

// Startup migrations run before the data volume is admitted to the application.
// Save verified preimages before any file mutation, including the result marker.
// An interrupted rollback retains this journal for the next startup to retry.
import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

type migrationUndoEntry struct {
	Path   string      `json:"path"`
	Exists bool        `json:"exists"`
	Mode   os.FileMode `json:"mode,omitempty"`
	SHA256 string      `json:"sha256,omitempty"`
	Backup string      `json:"backup,omitempty"`
}

type migrationUndo struct {
	Version    int                  `json:"version"`
	Stage      string               `json:"stage"`
	BackupDir  string               `json:"backupDir"`
	Entries    []migrationUndoEntry `json:"entries"`
	data, kind string
}

func migrationUndoPath(data, kind string) string {
	return filepath.Join(IntegrityDir(data), kind+"-undo.json")
}

func migrationUndoRelative(p string) bool {
	return filepath.IsLocal(p) && filepath.Clean(p) == p && p != "." && !strings.ContainsAny(p, "\\\x00\r\n")
}

func recoverMigrationUndo(data, kind string) error {
	_, err := recoverMigrationUndoResult(data, kind)
	return err
}

func recoverMigrationUndoResult(data, kind string) (bool, error) {
	root, err := os.OpenRoot(data)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	defer root.Close()
	b, err := root.ReadFile(filepath.Join(filepath.Base(IntegrityDir(data)), kind+"-undo.json"))
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var undo migrationUndo
	if err := json.Unmarshal(b, &undo); err != nil {
		return false, fmt.Errorf("迁移恢复日志损坏: %w", err)
	}
	undo.data, undo.kind = data, kind
	if err := undo.validate(); err != nil {
		return false, err
	}
	if undo.Stage == "committed" {
		undo.cleanup()
		return false, nil
	}
	return true, undo.rollback()
}

func (u *migrationUndo) validate() error {
	if u.Version != 1 || (u.Stage != "pending" && u.Stage != "committed") || filepath.Dir(u.BackupDir) != filepath.Base(IntegrityDir(u.data)) || !strings.HasPrefix(filepath.Base(u.BackupDir), ".migration-undo-"+u.kind+"-") {
		return errors.New("迁移恢复日志身份无效")
	}
	seen := map[string]bool{}
	for i, e := range u.Entries {
		if !migrationUndoRelative(e.Path) || seen[e.Path] || (e.Exists && (e.Backup != strconv.Itoa(i)+".bin" || len(e.SHA256) != 64)) {
			return errors.New("迁移恢复日志路径无效")
		}
		if e.Path == filepath.Join(filepath.Base(IntegrityDir(u.data)), u.kind+"-undo.json") {
			return errors.New("迁移恢复日志不能覆盖自身")
		}
		seen[e.Path] = true
	}
	return nil
}

func beginMigrationUndo(data, kind string, paths []string) (_ *migrationUndo, err error) {
	root, err := os.OpenRoot(data)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	relDir := filepath.Join(filepath.Base(IntegrityDir(data)), ".migration-undo-"+kind+"-"+newID())
	if err := root.Mkdir(relDir, 0700); err != nil {
		return nil, err
	}
	// This directory was created here and has no pre-existing user content.
	defer func() {
		if err != nil {
			_ = root.RemoveAll(relDir)
		}
	}()
	u := &migrationUndo{Version: 1, Stage: "pending", BackupDir: relDir, data: data, kind: kind}
	unique := map[string]bool{}
	for _, p := range paths {
		rel, e := filepath.Rel(data, p)
		if e != nil || !migrationUndoRelative(rel) {
			return nil, errors.New("迁移目标不在数据目录内")
		}
		unique[rel] = true
	}
	ordered := make([]string, 0, len(unique))
	for p := range unique {
		ordered = append(ordered, p)
	}
	sort.Strings(ordered)
	for i, p := range ordered {
		entry := migrationUndoEntry{Path: p}
		st, e := root.Lstat(p)
		if os.IsNotExist(e) {
			u.Entries = append(u.Entries, entry)
			continue
		}
		if e != nil {
			return nil, e
		}
		if !st.Mode().IsRegular() {
			return nil, fmt.Errorf("迁移目标不是普通文件: %s", p)
		}
		entry.Exists, entry.Mode, entry.Backup = true, st.Mode().Perm(), strconv.Itoa(i)+".bin"
		src, e := root.Open(p)
		if e != nil {
			return nil, e
		}
		dst, e := root.OpenFile(filepath.Join(relDir, entry.Backup), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if e != nil {
			src.Close()
			return nil, e
		}
		digest := sha256.New()
		_, e = io.Copy(io.MultiWriter(dst, digest), src)
		if e == nil {
			e = dst.Sync()
		}
		e = errors.Join(e, src.Close(), dst.Close())
		if e != nil {
			return nil, e
		}
		entry.SHA256 = hex.EncodeToString(digest.Sum(nil))
		u.Entries = append(u.Entries, entry)
	}
	if err = u.validate(); err != nil {
		return nil, err
	}
	if err = atomicMigrationState(data, migrationUndoPath(data, kind), u); err != nil {
		return nil, fmt.Errorf("保存迁移恢复日志失败: %w", err)
	}
	return u, nil
}

func (u *migrationUndo) cleanup() {
	root, err := os.OpenRoot(u.data)
	if err != nil {
		return
	}
	defer root.Close()
	// Never discard preimages if a pending journal could still reference them.
	if err := root.Remove(filepath.Join(filepath.Base(IntegrityDir(u.data)), u.kind+"-undo.json")); err != nil && !os.IsNotExist(err) {
		return
	}
	_ = root.RemoveAll(u.BackupDir)
}

func (u *migrationUndo) commit() error {
	u.Stage = "committed"
	if err := atomicMigrationState(u.data, migrationUndoPath(u.data, u.kind), u); err != nil {
		u.Stage = "pending"
		return fmt.Errorf("提交迁移恢复日志失败: %w", err)
	}
	u.cleanup()
	return nil
}

func atomicMigrationState(data, name string, value any) error {
	rel, err := filepath.Rel(data, name)
	if err != nil || !migrationUndoRelative(rel) {
		return errors.New("迁移状态路径无效")
	}
	b, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	root, err := os.OpenRoot(data)
	if err != nil {
		return err
	}
	defer root.Close()
	tmp := filepath.Join(filepath.Dir(rel), ".aide-migration-state-"+newID())
	f, err := root.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer root.Remove(tmp)
	_, err = f.Write(b)
	if err == nil {
		err = f.Sync()
	}
	err = errors.Join(err, f.Close())
	if err != nil {
		return err
	}
	return root.Rename(tmp, rel)
}

func (u *migrationUndo) rollback() error {
	if err := u.validate(); err != nil {
		return err
	}
	root, err := os.OpenRoot(u.data)
	if err != nil {
		return err
	}
	defer root.Close()
	var failures []error
	for _, e := range u.Entries {
		if !e.Exists {
			st, err := root.Lstat(e.Path)
			if os.IsNotExist(err) {
				continue
			}
			if err != nil {
				failures = append(failures, err)
				continue
			}
			if !st.Mode().IsRegular() {
				failures = append(failures, fmt.Errorf("迁移恢复目标不是普通文件: %s", e.Path))
				continue
			}
			if err := root.Remove(e.Path); err != nil {
				failures = append(failures, err)
			}
			continue
		}
		if err := restoreMigrationPreimage(root, u.BackupDir, e); err != nil {
			failures = append(failures, err)
		}
	}
	if err := errors.Join(failures...); err != nil {
		return fmt.Errorf("迁移恢复未完成，已保留恢复日志和原件: %w", err)
	}
	u.cleanup()
	return nil
}

func restoreMigrationPreimage(root *os.Root, backupDir string, e migrationUndoEntry) error {
	src, err := root.Open(filepath.Join(backupDir, e.Backup))
	if err != nil {
		return err
	}
	defer src.Close()
	tmp := filepath.Join(filepath.Dir(e.Path), ".aide-migration-restore-"+newID())
	dst, err := root.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, e.Mode.Perm())
	if err != nil {
		return err
	}
	defer root.Remove(tmp)
	digest := sha256.New()
	_, err = io.Copy(io.MultiWriter(dst, digest), src)
	if err == nil && hex.EncodeToString(digest.Sum(nil)) != e.SHA256 {
		err = errors.New("迁移恢复原件校验失败")
	}
	if err == nil {
		err = dst.Chmod(e.Mode.Perm())
	}
	if err == nil {
		err = dst.Sync()
	}
	err = errors.Join(err, dst.Close())
	if err != nil {
		return err
	}
	return root.Rename(tmp, e.Path)
}
