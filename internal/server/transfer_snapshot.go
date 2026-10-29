package server

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
)

// A new preparation gets a durable representation identity, even when a
// paused/crashed job is rebuilt with the same size and filesystem timestamp.
func (m *transferManager) publishDownload(j *transferJob, temporary, final string) error {
	if err := os.Rename(temporary, filepath.Join(m.jobDir(j), final)); err != nil {
		return err
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	j.DownloadVersion = newID()
	return m.save(j)
}

func (m *transferManager) fileSnapshot(ctx context.Context, j *transferJob, src *transferFS) error {
	if len(j.Parts) != 1 || j.Parts[0].Dir {
		return errors.New("无效原文件下载清单")
	}
	p := j.Parts[0]
	if err := checkTransferSource(src, p); err != nil {
		return err
	}
	root, err := os.OpenRoot(m.jobDir(j))
	if err != nil {
		return err
	}
	defer root.Close()
	space := &transferFS{root: root}
	if err := space.CheckSpace(p.Size); err != nil {
		return err
	}
	f, err := src.Open(p.Source, os.O_RDONLY)
	if err != nil {
		return err
	}
	defer f.Close()
	name := filepath.Join(m.jobDir(j), "download.file.part")
	out, err := os.OpenFile(name, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer os.Remove(name)
	defer out.Close()
	j.mu.Lock()
	j.Completed = 0
	j.mu.Unlock()
	buf := make([]byte, 128<<10)
	reader := io.NewSectionReader(f, 0, p.Size)
	var copied int64
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := m.check(j); err != nil {
			return err
		}
		n, readErr := reader.Read(buf)
		if n > 0 {
			if err := space.CheckSpace(int64(n)); err != nil {
				return err
			}
			written, err := out.Write(buf[:n])
			if err != nil {
				return err
			}
			if written != n {
				return io.ErrShortWrite
			}
			copied += int64(n)
			j.mu.Lock()
			j.Completed = copied
			err = m.checkpoint(j)
			j.mu.Unlock()
			if err != nil {
				return err
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return readErr
		}
	}
	if copied != p.Size {
		return io.ErrUnexpectedEOF
	}
	if err := checkTransferSource(src, p); err != nil {
		return err
	}
	if err := out.Sync(); err != nil {
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	if err := m.check(j); err != nil {
		return err
	}
	return m.publishDownload(j, name, "download.file")
}
