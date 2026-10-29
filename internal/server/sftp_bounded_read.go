package server

import (
	"context"
	"errors"
	"fmt"
	"io"
)

// Preview/legacy reads use an independent channel on the authenticated SSH
// master, bounded in memory. Large transfers keep their separate streaming API.
func (a *App) readSFTPBounded(source, socket string, expected *uint64, open func(*transferFS) (transferFile, error)) ([]byte, error) {
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
	data, err := readBoundedSFTPFile(f)
	if changed() {
		return nil, configError
	}
	return data, err
}

func readBoundedSFTPFile(f transferFile) ([]byte, error) {
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() < 0 {
		return nil, errors.New("仅支持普通 SFTP 文件")
	}
	if info.Size() > maxRawFile {
		return nil, fmt.Errorf("SFTP 文件超过 %d MiB 限制", maxRawFile>>20)
	}
	// The extra byte also catches files that grow after STAT or report a false
	// size. LimitReader prevents io.ReadAll from invoking unbounded fast paths.
	data, err := io.ReadAll(io.LimitReader(f, maxRawFile+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxRawFile {
		return nil, fmt.Errorf("SFTP 文件超过 %d MiB 限制", maxRawFile>>20)
	}
	return data, nil
}
