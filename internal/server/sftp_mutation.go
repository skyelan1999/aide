package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path"
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
	if err := validTransferPath(p); err != nil {
		return err
	}
	if parents {
		if err := f.MkdirAll(path.Dir(p)); err != nil {
			return err
		}
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
	if _, supported := f.client.HasExtension("posix-rename@openssh.com"); supported {
		err = f.client.PosixRename(from, to)
	} else {
		err = f.client.Rename(from, to)
	}
	published = err == nil
	return err
}
