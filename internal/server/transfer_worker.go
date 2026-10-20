package server

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sync"

	"github.com/pkg/sftp"
)

func (m *transferManager) run(ctx context.Context, j *transferJob) error {
	if e := m.check(j); e != nil {
		return e
	}
	if j.Operation == "upload" {
		return m.finishUpload(ctx, j)
	}
	src, err := m.app.streamTransferFS(ctx, j.Source, false)
	if err != nil {
		return err
	}
	defer src.Close()
	if j.Operation == "download" {
		return m.archive(ctx, j, src)
	}
	dst, err := m.app.streamTransferFS(ctx, j.Destination, true)
	if err != nil {
		return err
	}
	defer dst.Close()
	for _, top := range j.Tops {
		if !top.Published {
			if e := dst.CheckSpace(max(0, j.Size-j.Completed)); e != nil {
				return e
			}
			break
		}
	}
	// A resumed job retains complete top-level publications. Never write through
	// final paths; only its own private staging names are writable.
	for _, top := range j.Tops {
		if top.Published {
			continue
		}
		if _, e := dst.Stat(top.Stage); os.IsNotExist(e) {
			if _, e = dst.Stat(top.Target); e == nil {
				if !top.Publishing {
					return errors.New("目标已存在，不会覆盖")
				}
				// Crash after rename but before journal update: identify the complete tree
				// by its durable chunk hashes before accepting it as this publication.
				if e = m.verifyTop(ctx, j, dst, top, true); e != nil {
					return fmt.Errorf("目标已存在，无法确认是本任务发布：%w", e)
				}
				j.mu.Lock()
				top.Published = true
				top.Publishing = false
				err = m.save(j)
				j.mu.Unlock()
				if err != nil {
					return err
				}
				continue
			}
		}
	}
	for _, part := range j.Parts {
		if m.published(j, part) {
			continue
		}
		if e := ctx.Err(); e != nil {
			return e
		}
		if part.Dir {
			if err = dst.MkdirAll(part.Target); err != nil {
				return err
			}
			continue
		}
		if err = dst.MkdirAll(path.Dir(part.Target)); err != nil {
			return err
		}
		f, e := dst.Open(part.Target, os.O_RDWR|os.O_CREATE|os.O_EXCL)
		if os.IsExist(e) {
			f, e = dst.Open(part.Target, os.O_RDWR)
		}
		if e != nil {
			return e
		}
		info, e := f.Stat()
		if e == nil && info.Size() != part.Size {
			e = f.Truncate(part.Size)
		}
		if e == nil {
			e = syncTransferFile(f)
		}
		_ = f.Close()
		if e != nil {
			return e
		}
	}
	workerCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	type chunk struct {
		p     *transferPart
		index int64
	}
	queue := make(chan chunk)
	var wg sync.WaitGroup
	var first error
	var errorMu sync.Mutex
	report := func(e error) {
		errorMu.Lock()
		if first == nil {
			first = e
		}
		errorMu.Unlock()
		cancel()
	}
	for n := 0; n < j.Concurrency; n++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for c := range queue {
				select {
				case m.slots <- struct{}{}:
				case <-workerCtx.Done():
					return
				}
				e := m.copyChunk(workerCtx, j, src, dst, c.p, c.index)
				<-m.slots
				if e != nil {
					report(e)
					return
				}
			}
		}()
	}
produce:
	for _, p := range j.Parts {
		if p.Dir || m.published(j, p) {
			continue
		}
		for n := int64(0); n < p.Size; n += transferChunkSize {
			select {
			case queue <- chunk{p, n / transferChunkSize}:
			case <-workerCtx.Done():
				break produce
			}
		}
	}
	close(queue)
	wg.Wait()
	if first != nil {
		return first
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if err = m.check(j); err != nil {
		return err
	}
	hasDeleted := false
	for _, p := range j.Parts {
		if p.Deleted {
			hasDeleted = true
		}
	}
	for _, p := range j.Parts {
		if j.Operation == "move" && (p.Deleted || p.Dir && hasDeleted) {
			continue
		}
		if err = checkTransferSource(src, p); err != nil {
			if j.Operation == "move" && m.published(j, p) && os.IsNotExist(err) {
				continue
			}
			return err
		}
		if !p.Dir {
			f, e := src.Open(p.Source, os.O_RDONLY)
			if e != nil {
				return e
			}
			for off := int64(0); off < p.Size; off += transferChunkSize {
				digest, e := transferDigestAt(f, off, min(transferChunkSize, p.Size-off))
				if e != nil || digest != p.Chunks[off/transferChunkSize] {
					f.Close()
					return errors.New("发布前源文件校验不匹配")
				}
			}
			f.Close()
			if err = checkTransferSource(src, p); err != nil {
				return err
			}
		}
	}
	for _, top := range j.Tops {
		if !top.Published {
			if err = m.verifyTop(ctx, j, dst, top, false); err != nil {
				return err
			}
			if err = m.check(j); err != nil {
				return err
			}
			j.mu.Lock()
			top.Publishing = true
			err = m.save(j)
			j.mu.Unlock()
			if err != nil {
				return err
			}
			if err = dst.Publish(top.Stage, top.Target); err != nil {
				return fmt.Errorf("发布失败（不会覆盖现有目标）：%w", err)
			}
			j.mu.Lock()
			top.Published = true
			top.Publishing = false
			err = m.save(j)
			j.mu.Unlock()
			if err != nil {
				return err
			}
		}
	}
	// Move deletion is a separate journalled phase. Only captured entries are
	// removed; new siblings/files are never traversed or recursively deleted.
	if j.Operation == "move" {
		for _, top := range j.Tops {
			if err = m.verifyTop(ctx, j, dst, top, true); err != nil {
				return err
			}
		}
		for i := len(j.Parts) - 1; i >= 0; i-- {
			p := j.Parts[i]
			if p.Deleted {
				continue
			}
			if err = ctx.Err(); err != nil {
				return err
			}
			if err = m.check(j); err != nil {
				return err
			}
			if p.Dir {
				info, e := src.Stat(p.Source)
				err = e
				if err == nil && !info.IsDir() {
					err = errors.New("源目录类型变化")
				}
			} else {
				err = checkTransferSource(src, p)
			}
			if err != nil {
				if os.IsNotExist(err) {
					j.mu.Lock()
					p.Deleted = true
					err = m.save(j)
					j.mu.Unlock()
					if err != nil {
						return err
					}
					continue
				}
				return err
			}
			if !p.Dir {
				f, e := src.Open(p.Source, os.O_RDONLY)
				if e != nil {
					return e
				}
				for off := int64(0); off < p.Size; off += transferChunkSize {
					digest, e := transferDigestAt(f, off, min(transferChunkSize, p.Size-off))
					if e != nil || digest != p.Chunks[off/transferChunkSize] {
						f.Close()
						return errors.New("源文件已变化，目标已保留，源文件未删除")
					}
				}
				_ = f.Close()
			}
			if err = src.Remove(p.Source, p.Dir); err != nil {
				return fmt.Errorf("目标已完整发布，删除源失败：%w", err)
			}
			j.mu.Lock()
			p.Deleted = true
			err = m.save(j)
			j.mu.Unlock()
			if err != nil {
				return err
			}
		}
	}
	return nil
}
func (m *transferManager) published(j *transferJob, p *transferPart) bool {
	for _, t := range j.Tops {
		if t.Published && (p.Target == t.Stage || len(p.Target) > len(t.Stage) && p.Target[:len(t.Stage)+1] == t.Stage+"/") {
			return true
		}
	}
	return false
}
func checkTransferSource(src *transferFS, p *transferPart) error {
	info, e := src.Stat(p.Source)
	if e != nil {
		return e
	}
	if info.IsDir() != p.Dir || (!p.Dir && info.Size() != p.Size) || info.ModTime().UnixNano() != p.Mod {
		return errors.New("源文件或目录已变化，请重新创建传输")
	}
	return nil
}
func syncTransferFile(f transferFile) error {
	e := f.Sync()
	if e != nil && errors.Is(e, os.ErrInvalid) {
		return nil
	}
	var status *sftp.StatusError
	if errors.As(e, &status) && status.FxCode() == sftp.ErrSSHFxOpUnsupported {
		return nil
	}
	return e
}
func (m *transferManager) copyChunk(ctx context.Context, j *transferJob, src, dst *transferFS, p *transferPart, index int64) error {
	if e := ctx.Err(); e != nil {
		return e
	}
	if e := m.check(j); e != nil {
		return e
	}
	if e := checkTransferSource(src, p); e != nil {
		return e
	}
	from, e := src.Open(p.Source, os.O_RDONLY)
	if e != nil {
		return e
	}
	defer from.Close()
	to, e := dst.Open(p.Target, os.O_RDWR)
	if e != nil {
		return e
	}
	defer to.Close()
	offset := index * transferChunkSize
	size := min(transferChunkSize, p.Size-offset)
	j.mu.Lock()
	previous := p.Chunks[index]
	j.mu.Unlock()
	if previous != "" {
		sourceHash, e := transferDigestAt(from, offset, size)
		if e != nil {
			return e
		}
		if previous != sourceHash {
			return errors.New("已传输的源分块内容发生变化")
		}
		targetHash, e := transferDigestAt(to, offset, size)
		if e == nil && targetHash == previous {
			return nil
		}
	}
	b := make([]byte, int(size))
	if _, e = io.ReadFull(io.NewSectionReader(from, offset, size), b); e != nil {
		return e
	}
	if e = ctx.Err(); e != nil {
		return e
	}
	if e = dst.CheckSpace(int64(len(b))); e != nil {
		return e
	}
	if _, e = to.WriteAt(b, offset); e != nil {
		return e
	}
	if e = syncTransferFile(to); e != nil {
		return e
	}
	digest := hash(b)

	j.mu.Lock()
	defer j.mu.Unlock()
	if e = m.acknowledge(j, indexOfTransferPart(j, p), index, digest, false); e != nil {
		return e
	}
	if p.Chunks[index] == "" {
		j.Completed += size
	}
	p.Chunks[index] = digest
	return m.checkpoint(j)
}
func (m *transferManager) verifyTop(ctx context.Context, j *transferJob, fs *transferFS, t *transferTop, final bool) error {
	for _, p := range j.Parts {
		if p.Target != t.Stage && !(len(p.Target) > len(t.Stage) && p.Target[:len(t.Stage)+1] == t.Stage+"/") {
			continue
		}
		if e := ctx.Err(); e != nil {
			return e
		}
		name := p.Target
		if final {
			name = t.Target + p.Target[len(t.Stage):]
		}
		info, e := fs.Stat(name)
		if e != nil {
			return e
		}
		if info.IsDir() != p.Dir || (!p.Dir && info.Size() != p.Size) {
			return errors.New("发布目标类型或长度不匹配")
		}
		if p.Dir {
			continue
		}
		f, e := fs.Open(name, os.O_RDONLY)
		if e != nil {
			return e
		}
		for n := int64(0); n < p.Size; n += transferChunkSize {
			digest, e := transferDigestAt(f, n, min(transferChunkSize, p.Size-n))
			if e != nil || digest != p.Chunks[n/transferChunkSize] {
				f.Close()
				return errors.New("目标文件完整性校验失败")
			}
		}
		_ = f.Close()
	}
	return nil
}
func (m *transferManager) archive(ctx context.Context, j *transferJob, src *transferFS) error {
	// Preparation is streaming ZIP64 to disk. A prepared archive is immutable and
	// Range-downloadable; interrupted preparation is safely rebuilt on resume.
	j.mu.Lock()
	j.Completed = 0
	_ = m.save(j)
	j.mu.Unlock()
	root, e := os.OpenRoot(m.jobDir(j))
	if e != nil {
		return e
	}
	defer root.Close()
	space := &transferFS{root: root}
	if e = space.CheckSpace(j.Size); e != nil {
		return e
	}
	name := filepath.Join(m.jobDir(j), "download.zip.part")
	out, e := os.OpenFile(name, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if e != nil {
		return e
	}
	zw := zip.NewWriter(out)
	var err error
	for _, p := range j.Parts {
		if err = ctx.Err(); err != nil {
			break
		}
		if err = m.check(j); err != nil {
			break
		}
		if err = checkTransferSource(src, p); err != nil {
			break
		}
		relative := ""
		for _, top := range j.Tops {
			if p.Target == top.Stage || len(p.Target) > len(top.Stage) && p.Target[:len(top.Stage)+1] == top.Stage+"/" {
				relative = path.Base(top.Source) + p.Target[len(top.Stage):]
				break
			}
		}
		if relative == "" {
			err = errors.New("归档路径无效")
			break
		}
		hdr := &zip.FileHeader{Name: relative, Method: zip.Store}
		if p.Dir {
			hdr.Name += "/"
			hdr.SetMode(os.ModeDir | 0755)
		} else {
			hdr.SetMode(0644)
		}
		var w io.Writer
		w, err = zw.CreateHeader(hdr)
		if err != nil {
			break
		}
		if p.Dir {
			continue
		}
		f, e := src.Open(p.Source, os.O_RDONLY)
		if e != nil {
			err = e
			break
		}
		buf := make([]byte, 128<<10)
		reader := io.NewSectionReader(f, 0, p.Size)
		var copied int64
		for {
			if err = ctx.Err(); err != nil {
				break
			}
			n, e := reader.Read(buf)
			if n > 0 {
				if err = space.CheckSpace(int64(n)); err != nil {
					break
				}
				if _, err = w.Write(buf[:n]); err != nil {
					break
				}
				copied += int64(n)
			}
			if e == io.EOF {
				break
			}
			if e != nil {
				err = e
				break
			}
		}
		f.Close()
		if err == nil && copied != p.Size {
			err = io.ErrUnexpectedEOF
		}
		if err != nil {
			break
		}
		if err = checkTransferSource(src, p); err != nil {
			break
		}
		j.mu.Lock()
		j.Completed += p.Size
		e = m.save(j)
		j.mu.Unlock()
		if e != nil {
			err = e
			break
		}
	}
	if e = zw.Close(); err == nil {
		err = e
	}
	if e = out.Sync(); err == nil {
		err = e
	}
	if e = out.Close(); err == nil {
		err = e
	}
	if err != nil {
		return err
	}
	if err = m.check(j); err != nil {
		return err
	}
	return os.Rename(name, filepath.Join(m.jobDir(j), "download.zip"))
}

func indexOfTransferPart(j *transferJob, p *transferPart) int {
	for i, v := range j.Parts {
		if v == p {
			return i
		}
	}
	return -1
}

func (m *transferManager) finishUpload(ctx context.Context, j *transferJob) error {
	fs, e := m.app.streamTransferFS(ctx, j.Destination, true)
	if e != nil {
		return e
	}
	defer fs.Close()
	top := j.Tops[0]
	if top.Published {
		return m.verifyTop(ctx, j, fs, top, true)
	}
	f, e := fs.Open(top.Stage, os.O_RDWR)
	if os.IsNotExist(e) {
		if _, targetErr := fs.Stat(top.Target); targetErr == nil {
			if !top.Publishing {
				return errors.New("目标已存在，不会覆盖")
			}
			if e = m.verifyTop(ctx, j, fs, top, true); e != nil {
				return e
			}
			j.mu.Lock()
			top.Published = true
			top.Publishing = false
			e = m.save(j)
			j.mu.Unlock()
			return e
		}
		f, e = fs.Open(top.Stage, os.O_CREATE|os.O_EXCL|os.O_RDWR)
		if e == nil {
			e = f.Truncate(j.Size)
		}
	}
	if e != nil {
		return e
	}
	defer f.Close()
	bad := []int64{}
	for offset := int64(0); offset < j.Size; offset += transferChunkSize {
		if e = ctx.Err(); e != nil {
			return e
		}
		digest, err := transferDigestAt(f, offset, min(transferChunkSize, j.Size-offset))
		if err != nil || digest != j.UploadChunks[offset/transferChunkSize] {
			bad = append(bad, offset/transferChunkSize)
		}
	}
	if len(bad) > 0 {
		j.mu.Lock()
		defer j.mu.Unlock()
		for _, n := range bad {
			if e = m.acknowledge(j, 0, n, "", true); e != nil {
				return e
			}
			delete(j.UploadChunks, n)
			delete(j.Parts[0].Chunks, n)
		}
		j.Completed = 0
		for n := range j.UploadChunks {
			j.Completed += min(transferChunkSize, j.Size-n*transferChunkSize)
		}
		j.UploadCommitted = false
		_ = m.save(j)
		return errors.New("部分已确认分块缺失或损坏，请重新选择原文件继续上传")
	}
	if e = f.Truncate(j.Size); e != nil {
		return e
	}
	if e = syncTransferFile(f); e != nil {
		return e
	}
	f.Close()
	if e = m.check(j); e != nil {
		return e
	}
	j.mu.Lock()
	top.Publishing = true
	e = m.save(j)
	j.mu.Unlock()
	if e != nil {
		return e
	}
	if e = fs.Publish(top.Stage, top.Target); e != nil {
		return e
	}
	j.mu.Lock()
	top.Published = true
	top.Publishing = false
	e = m.save(j)
	j.mu.Unlock()
	return e
}
