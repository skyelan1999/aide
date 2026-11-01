package server

import (
	"context"
	"errors"
	"io"
	"os"
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
	return readBoundedSFTPFile(f)
}

// Raw SFTP previews share the raw-file API's bounded lookahead. Workspace text
// reads use readSFTPTextWindow instead and therefore have no whole-file cap.
func readBoundedSFTPFile(f transferFile) ([]byte, error) {
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() < 0 {
		return nil, errors.New("仅支持普通 SFTP 文件")
	}
	data, err := io.ReadAll(io.LimitReader(f, maxRawFile+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxRawFile {
		return nil, errors.New("SFTP 文件超过原始文件预览上限")
	}
	return data, nil
}

// readSFTPTextWindow streams and fingerprints a workspace text file while
// retaining only the requested line or UTF-8 byte window.
func (a *App) readSFTPTextWindow(p string, lineOffset, lineLimit int, byteMode bool, byteOffset, byteLimit int64) (streamedTextWindow, error) {
	ctx, cancel := context.WithTimeout(context.Background(), sftpTimeout)
	defer cancel()
	generation := a.sshSessionGeneration(sshControlSocket)
	changed := func() bool { return generation != a.sshSessionGeneration(sshControlSocket) }
	fs, err := a.streamTransferFS(ctx, "workspace", false)
	if err != nil {
		return streamedTextWindow{}, err
	}
	defer fs.Close()
	if fs.client == nil || changed() {
		return streamedTextWindow{}, errors.New("SSH 连接配置已变化，请重新访问")
	}
	f, err := fs.Open(p, os.O_RDONLY)
	if err != nil {
		return streamedTextWindow{}, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return streamedTextWindow{}, err
	}
	if !info.Mode().IsRegular() || info.Size() < 0 {
		return streamedTextWindow{}, errors.New("仅支持普通 SFTP 文件")
	}
	window, err := streamTextWindow(p, f, lineOffset, lineLimit, byteMode, byteOffset, byteLimit)
	if changed() {
		return streamedTextWindow{}, errors.New("SSH 连接配置已变化，请重新访问")
	}
	if err != nil {
		return streamedTextWindow{}, err
	}
	after, err := f.Stat()
	if err != nil || info.Size() != after.Size() || !info.ModTime().Equal(after.ModTime()) {
		return streamedTextWindow{}, errors.New("读取期间远端文件发生变化，请重新读取")
	}
	if changed() {
		return streamedTextWindow{}, errors.New("SSH 连接配置已变化，请重新访问")
	}
	return window, nil
}
