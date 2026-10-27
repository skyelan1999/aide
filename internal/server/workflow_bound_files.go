package server

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"path"
	"strings"
)

// Each component is opened relative to the already pinned parent. Verify the
// directory before/after opening and against the opened handle; subsequent I/O
// uses that handle rather than resolving the workspace path again.
func boundDirectoryRoot(root *os.Root, p string, create bool) (*os.Root, func(), error) {
	if root == nil {
		return nil, nil, errors.New("绑定工作区不可用")
	}
	if err := safePath(p); err != nil {
		return nil, nil, err
	}
	current, owned := root, false
	closeRoot := func() {
		if owned {
			owned = false
			current.Close()
		}
	}
	for _, part := range strings.Split(path.Clean(p), "/") {
		if part == "." {
			continue
		}
		before, err := current.Lstat(part)
		if errors.Is(err, os.ErrNotExist) && create {
			if err = current.Mkdir(part, 0755); err != nil && !errors.Is(err, os.ErrExist) {
				closeRoot()
				return nil, nil, err
			}
			before, err = current.Lstat(part)
		}
		if err != nil {
			closeRoot()
			return nil, nil, err
		}
		if !before.IsDir() || before.Mode()&os.ModeSymlink != 0 {
			closeRoot()
			return nil, nil, errors.New("绑定目录不能包含符号链接或特殊文件")
		}
		next, err := current.OpenRoot(part)
		if err != nil {
			closeRoot()
			return nil, nil, err
		}
		after, linkErr := current.Lstat(part)
		opened, statErr := next.Stat(".")
		if linkErr != nil || statErr != nil || !after.IsDir() || after.Mode()&os.ModeSymlink != 0 ||
			!os.SameFile(before, after) || !os.SameFile(before, opened) {
			next.Close()
			closeRoot()
			return nil, nil, errors.New("绑定目录在打开期间已变化")
		}
		if owned {
			current.Close()
		}
		current, owned = next, true
	}
	return current, closeRoot, nil
}

func boundPathExists(root *os.Root, p string) (bool, error) {
	if err := safePath(p); err != nil {
		return false, err
	}
	parent, closeRoot, err := boundDirectoryRoot(root, path.Dir(p), false)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	defer closeRoot()
	info, err := parent.Lstat(path.Base(p))
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return false, errors.New("绑定路径不能包含符号链接")
	}
	return true, nil
}

func boundFileOpener(root *os.Root) func(string) (*os.File, error) {
	return func(p string) (*os.File, error) {
		if err := safePath(p); err != nil {
			return nil, err
		}
		parent, closeRoot, err := boundDirectoryRoot(root, path.Dir(p), false)
		if err != nil {
			return nil, err
		}
		defer closeRoot()
		// Preserve initial ENOENT for new-file version checks. A disappearance
		// during the verified nonblocking open remains a changed-file error.
		info, err := parent.Lstat(path.Base(p))
		if err != nil {
			return nil, err
		}
		if !info.Mode().IsRegular() {
			return nil, errors.New("绑定目标只支持普通文件，不能是符号链接")
		}
		f, _, err := openDeliveryFile(parent, path.Base(p))
		if err != nil {
			return nil, errors.New("绑定文件在打开期间变化或不可读取")
		}
		return f, err
	}
}

func readBoundText(root *os.Root, p string) ([]byte, error) {
	return readTextWithOpener(p, boundFileOpener(root))
}

func readBoundRawBytes(root *os.Root, p string) ([]byte, error) {
	return readRawBytesWithOpener(p, boundFileOpener(root))
}

func checkBoundVersion(root *os.Root, p, expected string) error {
	b, err := readBoundText(root, p)
	return checkReadVersion(b, err, expected)
}

func putBoundText(root *os.Root, p string, b []byte) error {
	if err := safePath(p); err != nil {
		return err
	}
	parent, closeRoot, err := boundDirectoryRoot(root, path.Dir(p), true)
	if err != nil {
		return err
	}
	defer closeRoot()
	name, mode := path.Base(p), os.FileMode(0644)
	if st, err := parent.Lstat(name); err == nil {
		if !st.Mode().IsRegular() {
			return errors.New("绑定目标必须是普通文件，不能是符号链接")
		}
		mode = st.Mode().Perm()
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return putAtomicFile(parent, name, b, mode)
}

func (a *App) listBoundLocalDir(root *os.Root, p string) ([]map[string]any, error) {
	dir, closeRoot, err := boundDirectoryRoot(root, p, false)
	if err != nil {
		return nil, err
	}
	defer closeRoot()
	f, err := dir.Open(".")
	if err != nil {
		return nil, err
	}
	defer f.Close()
	entries, err := f.ReadDir(2000)
	if err != nil && err != io.EOF {
		return nil, err
	}
	// DirEntry.Info may resolve its original pathname after a rename. Snapshot
	// metadata using the pinned directory, then reuse the ordinary formatter.
	stable := make([]os.DirEntry, 0, len(entries))
	for _, entry := range entries {
		info, err := dir.Lstat(entry.Name())
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		stable = append(stable, fs.FileInfoToDirEntry(info))
	}
	return localDirItems(stable, p), nil
}
