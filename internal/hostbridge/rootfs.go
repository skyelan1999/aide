package hostbridge

// The NFS namespace is backed exclusively by an opened os.Root. The host path
// is never reopened for an operation, including after its parent is renamed.
import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path"
	"sort"
	"strings"
	"sync"
	"time"

	billy "github.com/go-git/go-billy/v5"
	nf "github.com/willscott/go-nfs/file"
)

type rootFS struct {
	root     *os.Root
	ro       bool
	uid, gid uint32
	mu       sync.Mutex
	closed   bool
	files    map[*rootFile]bool
}
type rootFile struct {
	*os.File
	owner *rootFS
	once  sync.Once
}

func (*rootFile) Lock() error   { return errors.ErrUnsupported }
func (*rootFile) Unlock() error { return errors.ErrUnsupported }
func (f *rootFile) Close() error {
	var e error
	f.once.Do(func() { e = f.File.Close(); f.owner.mu.Lock(); delete(f.owner.files, f); f.owner.mu.Unlock() })
	return e
}
func (f *rootFile) Write(b []byte) (int, error) {
	n, e := f.File.Write(b)
	if e == nil && n != len(b) {
		e = io.ErrShortWrite
	}
	if e == nil {
		e = f.File.Sync()
	}
	if e != nil {
		f.Close()
	}
	return n, e
}
func (f *rootFile) Seek(n int64, w int) (int64, error) {
	v, e := f.File.Seek(n, w)
	if e != nil {
		f.Close()
	}
	return v, e
}
func (f *rootFile) Truncate(n int64) error {
	e := f.File.Truncate(n)
	if e == nil {
		e = f.File.Sync()
	}
	if e != nil {
		f.Close()
	}
	return e
}

type projectedInfo struct {
	os.FileInfo
	uid, gid uint32
}

func (i projectedInfo) Sys() any {
	v := nf.GetInfo(i.FileInfo)
	if v == nil {
		v = &nf.FileInfo{Nlink: 1}
	}
	c := *v
	c.UID = i.uid
	c.GID = i.gid
	return c
}
func clean(p string) (string, error) {
	if len(p) > 4096 || strings.ContainsAny(p, "\x00\\") {
		return "", os.ErrPermission
	}
	p = strings.TrimPrefix(p, "/")
	if p == "" {
		return ".", nil
	}
	for _, part := range strings.Split(p, "/") {
		if part == ".." {
			return "", os.ErrPermission
		}
	}
	return path.Clean(p), nil
}
func (f *rootFS) info(v os.FileInfo) os.FileInfo { return projectedInfo{v, f.uid, f.gid} }
func (f *rootFS) OpenFile(p string, flag int, m os.FileMode) (billy.File, error) {
	p, e := clean(p)
	if e != nil {
		return nil, e
	}
	if f.ro && flag&(os.O_WRONLY|os.O_RDWR|os.O_CREATE|os.O_TRUNC|os.O_APPEND) != 0 {
		return nil, os.ErrPermission
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return nil, os.ErrClosed
	}
	if len(f.files) >= 64 {
		return nil, errors.New("bridge open-file budget reached")
	}
	v, e := f.root.OpenFile(p, flag, m.Perm())
	if e != nil {
		return nil, e
	}
	file := &rootFile{File: v, owner: f}
	f.files[file] = true
	return file, nil
}
func (f *rootFS) Open(p string) (billy.File, error) { return f.OpenFile(p, os.O_RDONLY, 0) }
func (f *rootFS) Create(p string) (billy.File, error) {
	return f.OpenFile(p, os.O_CREATE|os.O_RDWR|os.O_TRUNC, 0666)
}
func (f *rootFS) Stat(p string) (os.FileInfo, error) {
	p, e := clean(p)
	if e != nil {
		return nil, e
	}
	v, e := f.root.Stat(p)
	if e != nil {
		return nil, e
	}
	return f.info(v), nil
}
func (f *rootFS) Lstat(p string) (os.FileInfo, error) {
	p, e := clean(p)
	if e != nil {
		return nil, e
	}
	v, e := f.root.Lstat(p)
	if e != nil {
		return nil, e
	}
	return f.info(v), nil
}
func (f *rootFS) ReadDir(p string) ([]os.FileInfo, error) {
	p, e := clean(p)
	if e != nil {
		return nil, e
	}
	v, e := f.root.Open(p)
	if e != nil {
		return nil, e
	}
	defer v.Close()
	rows, e := v.Readdir(10001)
	if e != nil && e != io.EOF {
		return nil, e
	}
	if len(rows) > 10000 {
		return nil, errors.New("bridge directory budget reached")
	}
	for i := range rows {
		rows[i] = f.info(rows[i])
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Name() < rows[j].Name() })
	return rows, nil
}
func (f *rootFS) Rename(a, b string) error {
	if f.ro {
		return os.ErrPermission
	}
	a, e := clean(a)
	if e != nil {
		return e
	}
	b, e = clean(b)
	if e != nil {
		return e
	}
	return f.root.Rename(a, b)
}
func (f *rootFS) Remove(p string) error {
	p, e := clean(p)
	if e != nil {
		return e
	}
	if f.ro {
		return os.ErrPermission
	}
	return f.root.Remove(p)
}
func (f *rootFS) MkdirAll(p string, m os.FileMode) error {
	p, e := clean(p)
	if e != nil {
		return e
	}
	if f.ro {
		return os.ErrPermission
	}
	return f.root.MkdirAll(p, m.Perm())
}
func (f *rootFS) Join(parts ...string) string               { return path.Join(parts...) }
func (f *rootFS) Root() string                              { return "/" }
func (f *rootFS) Chroot(p string) (billy.Filesystem, error) { return nil, errors.ErrUnsupported }
func (f *rootFS) Readlink(p string) (string, error) {
	p, e := clean(p)
	if e != nil {
		return "", e
	}
	return f.root.Readlink(p)
}
func (f *rootFS) Symlink(target, p string) error {
	p, e := clean(p)
	if e != nil {
		return e
	}
	if f.ro {
		return os.ErrPermission
	}
	if strings.ContainsRune(target, 0) {
		return os.ErrInvalid
	}
	return f.root.Symlink(target, p)
}
func (f *rootFS) TempFile(dir, prefix string) (billy.File, error) {
	b := make([]byte, 16)
	if _, e := rand.Read(b); e != nil {
		return nil, e
	}
	return f.OpenFile(path.Join(dir, prefix+hex.EncodeToString(b)), os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
}
func (f *rootFS) Chmod(p string, m os.FileMode) error {
	p, e := clean(p)
	if e != nil {
		return e
	}
	if f.ro {
		return os.ErrPermission
	}
	return f.root.Chmod(p, m.Perm())
}
func (f *rootFS) Chown(p string, u, g int) error {
	if f.ro {
		return os.ErrPermission
	}
	p, e := clean(p)
	if e != nil {
		return e
	}
	if (u != -1 && u != int(f.uid)) || (g != -1 && g != int(f.gid)) {
		return os.ErrPermission
	}
	_, e = f.root.Lstat(p)
	return e
}
func (f *rootFS) Lchown(p string, u, g int) error { return f.Chown(p, u, g) }
func (f *rootFS) Chtimes(p string, a, m time.Time) error {
	p, e := clean(p)
	if e != nil {
		return e
	}
	if f.ro {
		return os.ErrPermission
	}
	return f.root.Chtimes(p, a, m)
}
func (f *rootFS) Capabilities() billy.Capability {
	v := billy.ReadCapability | billy.SeekCapability
	if !f.ro {
		v |= billy.WriteCapability | billy.ReadAndWriteCapability | billy.TruncateCapability
	}
	return v
}
func (f *rootFS) close() {
	f.mu.Lock()
	f.closed = true
	files := make([]*rootFile, 0, len(f.files))
	for v := range f.files {
		files = append(files, v)
	}
	f.mu.Unlock()
	for _, v := range files {
		v.Close()
	}
	f.root.Close()
}
