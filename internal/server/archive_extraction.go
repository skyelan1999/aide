package server

import (
	"errors"
	"io"
	"os"
	"path"
	"sort"
	"strings"
)

// Adding a separator to the sort key keeps a parent immediately before its
// descendants, even when a sibling such as "a-unrelated" also exists.
func validateArchiveLayout(files []extractedArchiveFile) error {
	type entry struct {
		key string
		dir bool
	}
	entries := make([]entry, len(files))
	for i, file := range files {
		entries[i] = entry{file.path + "/", file.dir}
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].key < entries[j].key })
	for i := 0; i+1 < len(entries); i++ {
		if !entries[i].dir && strings.HasPrefix(entries[i+1].key, entries[i].key) {
			return errors.New("归档文件路径与目录冲突")
		}
	}
	return nil
}

// Cleanup uses the same pinned filesystem as writes, never a newly selected
// workspace. Remote symlinks are rejected by transferFS before traversal.
func removeArchiveStage(fs *transferFS, stage string) error {
	if fs.root != nil {
		return fs.root.RemoveAll(stage)
	}
	items, err := fs.ReadDir(stage)
	if err != nil {
		return err
	}
	var cleanup error
	for _, item := range items {
		child := path.Join(stage, item.Name())
		if item.IsDir() {
			cleanup = errors.Join(cleanup, removeArchiveStage(fs, child))
		} else {
			cleanup = errors.Join(cleanup, fs.Remove(child, false))
		}
	}
	if cleanup != nil {
		return cleanup
	}
	return fs.Remove(stage, true)
}

func writeExtractedArchive(fs *transferFS, dest string, files []extractedArchiveFile) (err error) {
	stage := path.Join(path.Dir(dest), ".aide-unpack-"+newID())
	if err = fs.Mkdir(stage); err != nil {
		return err
	}
	published := false
	defer func() {
		if !published {
			err = errors.Join(err, removeArchiveStage(fs, stage))
		}
	}()
	for _, file := range files {
		target := path.Join(stage, file.path)
		if file.dir {
			if err = fs.MkdirAll(target); err != nil {
				return err
			}
			continue
		}
		if err = fs.MkdirAll(path.Dir(target)); err != nil {
			return err
		}
		out, openErr := fs.Open(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL)
		if openErr != nil {
			return openErr
		}
		n, writeErr := out.WriteAt(file.data, 0)
		if writeErr == nil && n != len(file.data) {
			writeErr = io.ErrShortWrite
		}
		if writeErr == nil && fs.root != nil {
			if chmod, ok := out.(interface{ Chmod(os.FileMode) error }); ok {
				writeErr = chmod.Chmod(0644)
			}
		}
		if writeErr == nil {
			writeErr = syncTransferFile(out)
		}
		if err = errors.Join(writeErr, out.Close()); err != nil {
			return err
		}
	}
	if err = fs.Publish(stage, dest); err != nil {
		return err
	}
	published = true
	return nil
}
