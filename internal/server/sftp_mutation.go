package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path"
	"strings"
)

func (a *App) mutateSFTP(source, socket string, expected *uint64, action func(*transferFS) error) error {
	ctx, cancel := context.WithTimeout(context.Background(), sftpTimeout)
	defer cancel()
	generation := a.sshSessionGeneration(socket)
	changed := func() bool { return generation != a.sshSessionGeneration(socket) }
	configError := errors.New("SFTP 连接配置已变化，请重新访问")
	if expected != nil && generation != *expected {
		return configError
	}
	fs, err := a.streamTransferFS(ctx, source, true)
	if err != nil {
		return err
	}
	defer fs.Close()
	if fs.client == nil || changed() {
		return configError
	}
	if err := action(fs); err != nil {
		return err
	}
	if changed() {
		return configError
	}
	return nil
}

// Resolve and LSTAT every path at the operation, including staging and final
// destination. SFTP v3 has no openat/O_NOFOLLOW: protection against an independent
// server-side path swap additionally requires a server-side chroot/account root.
func (f *transferFS) writeAtomic(p string, data []byte, parents bool) error {
	return f.writeAtomicChecked(p, data, parents, nil)
}
func (f *transferFS) writeAtomicChecked(p string, data []byte, parents bool, beforePublish func() error) error {
	if err := validTransferPath(p); err != nil {
		return err
	}
	if parents {
		if err := f.MkdirAll(path.Dir(p)); err != nil {
			return err
		}
	}
	if f.root != nil {
		return f.writeLocalAtomic(p, data, beforePublish)
	}
	if _, err := f.remotePath(p); err != nil {
		return err
	}
	nonce := make([]byte, 16)
	if _, err := rand.Read(nonce); err != nil {
		return err
	}
	temporary := path.Join(path.Dir(p), ".aide-write-"+hex.EncodeToString(nonce))
	file, err := f.Open(temporary, os.O_WRONLY|os.O_CREATE|os.O_EXCL)
	if err != nil {
		return err
	}
	published := false
	defer func() {
		if !published {
			_ = f.Remove(temporary, false)
		}
	}()
	if n, err := file.WriteAt(data, 0); err != nil || n != len(data) {
		file.Close()
		if err == nil {
			err = io.ErrShortWrite
		}
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	from, err := f.remotePath(temporary)
	if err != nil {
		return err
	}
	to, err := f.remotePath(p)
	if err != nil {
		return err
	}
	if beforePublish != nil {
		if e := beforePublish(); e != nil {
			return e
		}
	}
	if _, supported := f.client.HasExtension("posix-rename@openssh.com"); supported {
		err = f.client.PosixRename(from, to)
	} else {
		err = f.client.Rename(from, to)
	}
	published = err == nil
	return err
}

// os.Root keeps both publication paths attached to the admitted local directory.
func (f *transferFS) writeLocalAtomic(p string, data []byte, beforePublish func() error) error {
	mode := os.FileMode(0600)
	parts := strings.Split(p, "/")
	for i := range parts {
		name := strings.Join(parts[:i+1], "/")
		info, err := f.root.Lstat(name)
		if os.IsNotExist(err) && name == p {
			break
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return errors.New("写入不跟随符号链接")
		}
		if name == p && info.Mode().IsRegular() {
			mode = info.Mode().Perm()
		}
	}
	nonce := make([]byte, 16)
	if _, err := rand.Read(nonce); err != nil {
		return err
	}
	temporary := path.Join(path.Dir(p), ".aide-write-"+hex.EncodeToString(nonce))
	file, err := f.root.OpenFile(temporary, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return err
	}
	published := false
	defer func() {
		if !published {
			_ = f.root.Remove(temporary)
		}
	}()
	if n, err := file.Write(data); err != nil || n != len(data) {
		file.Close()
		if err == nil {
			err = io.ErrShortWrite
		}
		return err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if beforePublish != nil {
		if e := beforePublish(); e != nil {
			return e
		}
	}
	if err := f.root.Rename(temporary, p); err != nil {
		return err
	}
	published = true
	return nil
}
