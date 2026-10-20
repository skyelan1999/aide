package server

// Large transfers use random-access files, never the preview []byte APIs.
import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"strings"

	"github.com/pkg/sftp"
)

type transferFile interface {
	io.ReaderAt
	io.WriterAt
	io.Seeker
	io.Reader
	io.Closer
	Stat() (os.FileInfo, error)
	Truncate(int64) error
	Sync() error
}
type transferFS struct {
	root    *os.Root
	client  *sftp.Client
	base    string
	command *exec.Cmd
	cancel  context.CancelFunc
}

func (a *App) transferSourceStreaming(id string) bool {
	if id == "" || id == "workspace" {
		return true
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	src, ok := a.findSource(id)
	return ok && (src.Type == "local" || src.Type == "skill" || src.Type == "sftp" || src.Type == "workspace-sftp")
}

func (a *App) streamTransferFS(ctx context.Context, id string, write bool) (*transferFS, error) {
	loc, err := a.transferLocation(id, write)
	if err != nil {
		return nil, err
	}
	if !a.transferSourceStreaming(id) {
		return nil, errors.New("此引用驱动不支持随机文件读写，请使用其原有下载或复制入口")
	}
	a.mu.Lock()
	remote, ok := a.transferRemote(loc, ".")
	var localName string
	if !ok && !loc.hasSource {
		localName = a.workspace.Name()
	}
	port := a.wsConfig.Workspace.Port
	target := a.sshTarget()
	sock := sshControlSocket
	a.mu.Unlock()
	if !ok {
		var root *os.Root
		if loc.hasSource {
			root, err = a.localSourceRoot(loc.source)
		} else {
			root, err = os.OpenRoot(localName)
		}
		if err != nil {
			return nil, err
		}
		return &transferFS{root: root}, nil
	}
	child, cancel := context.WithCancel(ctx)
	if loc.hasSource && loc.source.Type == "sftp" {
		sock = sourceSocket(loc.source.ID)
		port = loc.source.Config.Port
		target = a.sftpTargetOf(loc.source)
		err = a.ensureSourceSession(child, loc.source)
	} else {
		err = a.ensureSSHSession(child)
	}
	if err != nil {
		cancel()
		return nil, err
	}
	if port == 0 {
		port = 22
	}
	generation := a.sshSessionGeneration(sock)
	cmd := exec.CommandContext(child, a.sshBin, "-o", "ControlPath="+sock, "-o", "ControlMaster=no", "-o", "BatchMode=yes", "-o", "ConnectTimeout=10", "-o", "ServerAliveInterval=15", "-o", "ServerAliveCountMax=2", "-o", "LogLevel=ERROR", "-p", fmt.Sprint(port), "-s", target, "sftp")
	in, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return nil, err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, err
	}
	if err = cmd.Start(); err != nil {
		cancel()
		return nil, err
	}
	client, err := sftp.NewClientPipe(out, in)
	if err != nil {
		cancel()
		_ = cmd.Wait()
		return nil, err
	}
	fs := &transferFS{client: client, command: cmd, cancel: cancel}
	if generation != a.sshSessionGeneration(sock) {
		fs.Close()
		return nil, errors.New("SSH 连接配置已变化")
	}
	fs.base, err = client.RealPath(remote)
	if err == nil {
		err = fs.checkRemoteDirectories(fs.base)
	}
	if err != nil {
		fs.Close()
		return nil, err
	}
	return fs, nil
}

// Some SFTP servers implement REALPATH as lexical normalization only. Check
// each ancestor with LSTAT as well, rather than assuming it resolves symlinks.
func (f *transferFS) checkRemoteDirectories(p string) error {
	current := "/"
	for _, component := range strings.Split(strings.Trim(p, "/"), "/") {
		if component == "" {
			continue
		}
		current = path.Join(current, component)
		info, e := f.client.Lstat(current)
		if e != nil {
			return e
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return errors.New("远端父路径不是普通目录，传输不跟随符号链接")
		}
	}
	return nil
}
func (f *transferFS) Close() {
	if f.root != nil {
		_ = f.root.Close()
	}
	if f.cancel != nil {
		f.cancel()
	}
	if f.client != nil {
		_ = f.client.Close()
	}
	if f.command != nil {
		_ = f.command.Wait()
	}
}
func (f *transferFS) remotePath(p string) (string, error) {
	if p != "." {
		if err := validTransferPath(p); err != nil {
			return "", err
		}
	}
	full := path.Join(f.base, p)
	parent := path.Dir(full)
	if p == "." {
		parent = f.base
	}
	if err := f.checkRemoteDirectories(parent); err != nil {
		return "", err
	}
	resolved, err := f.client.RealPath(parent)
	if err != nil {
		return "", err
	}
	if resolved != f.base && !strings.HasPrefix(resolved, strings.TrimSuffix(f.base, "/")+"/") {
		return "", errors.New("远端路径越界")
	}
	info, err := f.client.Lstat(full)
	if err == nil && info.Mode()&os.ModeSymlink != 0 {
		return "", errors.New("传输不跟随符号链接")
	}
	if err != nil && !os.IsNotExist(err) {
		return "", err
	}
	return full, nil
}
func (f *transferFS) Stat(p string) (os.FileInfo, error) {
	if p != "." {
		if err := validTransferPath(p); err != nil {
			return nil, err
		}
	}
	if f.root != nil {
		return f.root.Lstat(p)
	}
	v, e := f.remotePath(p)
	if e != nil {
		return nil, e
	}
	return f.client.Stat(v)
}
func (f *transferFS) Open(p string, flags int) (transferFile, error) {
	if err := validTransferPath(p); err != nil {
		return nil, err
	}
	if f.root != nil {
		info, e := f.root.Lstat(p)
		if e == nil && info.Mode()&os.ModeSymlink != 0 {
			return nil, errors.New("传输不跟随符号链接")
		}
		return f.root.OpenFile(p, flags, 0600)
	}
	v, e := f.remotePath(p)
	if e != nil {
		return nil, e
	}
	return f.client.OpenFile(v, flags)
}
func (f *transferFS) ReadDir(p string) ([]os.FileInfo, error) {
	if f.root != nil {
		d, e := f.root.Open(p)
		if e != nil {
			return nil, e
		}
		defer d.Close()
		return d.Readdir(-1)
	}
	v, e := f.remotePath(p)
	if e != nil {
		return nil, e
	}
	return f.client.ReadDir(v)
}
func (f *transferFS) Mkdir(p string) error {
	if p == "." {
		return nil
	}
	if err := validTransferPath(p); err != nil {
		return err
	}
	if f.root != nil {
		return f.root.Mkdir(p, 0755)
	}
	v, e := f.remotePath(p)
	if e != nil {
		return e
	}
	return f.client.Mkdir(v)
}
func (f *transferFS) MkdirAll(p string) error {
	if p == "." || p == "" {
		return nil
	}
	for i := range strings.Split(p, "/") {
		part := strings.Join(strings.Split(p, "/")[:i+1], "/")
		info, e := f.Stat(part)
		if e == nil {
			if !info.IsDir() {
				return errors.New("父路径不是目录")
			}
			continue
		}
		if !os.IsNotExist(e) {
			return e
		}
		if e = f.Mkdir(part); e != nil && !os.IsExist(e) {
			return e
		}
	}
	return nil
}
func (f *transferFS) Remove(p string, dir bool) error {
	if err := validTransferPath(p); err != nil {
		return err
	}
	if f.root != nil {
		return f.root.Remove(p)
	}
	v, e := f.remotePath(p)
	if e != nil {
		return e
	}
	if dir {
		return f.client.RemoveDirectory(v)
	}
	return f.client.Remove(v)
}
func (f *transferFS) Publish(old, new string) error {
	if _, e := f.Stat(new); e == nil {
		return os.ErrExist
	} else if !os.IsNotExist(e) {
		return e
	}
	if f.root != nil {
		return transferRenameExclusive(f.root, old, new)
	}
	a, e := f.remotePath(old)
	if e != nil {
		return e
	}
	b, e := f.remotePath(new)
	if e != nil {
		return e
	}
	return f.client.Rename(a, b) // Standard SFTP rename MUST fail if target exists; never PosixRename.
}

// A free-space reserve is a resource guard, not a file-size limit. Remote
// servers without StatVFS still report write/space errors per bounded chunk.
func (f *transferFS) CheckSpace(additional int64) error {
	var available int64
	var e error
	if f.root != nil {
		available, e = transferAvailableBytes(f.root)
	} else {
		if _, supported := f.client.HasExtension("statvfs@openssh.com"); !supported {
			return nil
		}
		stat, err := f.client.StatVFS(f.base)
		if err != nil {
			return err
		}
		if stat.Frsize > 0 && stat.Bavail > uint64(^uint64(0)>>1)/stat.Frsize {
			available = int64(^uint64(0) >> 1)
		} else {
			available = int64(stat.Bavail * stat.Frsize)
		}
	}
	if e != nil {
		return e
	}
	if available >= 0 && (available < 64<<20 || additional > available-(64<<20)) {
		return errors.New("目标磁盘剩余空间不足（保留64MiB用于正常运行），可释放空间后继续")
	}
	return nil
}
