package server

import (
	"context"
	"errors"
)

// Workspace text reads use an independent channel on the authenticated SSH
// master. Callers that need bounded memory use the separate streaming APIs.
func (a *App) readSFTPContent(source, socket string, expected *uint64, open func(*transferFS) (transferFile, error)) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), sftpTimeout)
	defer cancel()
	generation := a.sshSessionGeneration(socket)
	changed := func() bool { return generation != a.sshSessionGeneration(socket) }
	configError := errors.New("SFTP 连接配置已变化，请重新访问")
	if expected != nil && generation != *expected {
		return nil, configError
	}
	fs, err := a.streamTransferFS(ctx, source, false)
	if err != nil {
		return nil, err
	}
	defer fs.Close()
	if fs.client == nil || changed() {
		return nil, configError
	}
	f, err := open(fs)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := readSFTPFile(f)
	if changed() {
		return nil, configError
	}
	return data, err
}

func readSFTPFile(f transferFile) ([]byte, error) {
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() < 0 {
		return nil, errors.New("仅支持普通 SFTP 文件")
	}
	data, err := io.ReadAll(f)
	if err != nil {
		return nil, err
	}
	return data, nil
}
