package server

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path"
)

type archiveContextReader struct {
	ctx context.Context
	r   io.Reader
}

func (r *archiveContextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.r.Read(p)
}

// Archive payloads are spooled to ordinary temporary files. Only metadata is
// retained in memory, so removing a size cap does not turn a large archive into
// a correspondingly large Go heap allocation.
func spoolArchiveReader(dir string, input io.Reader) (name string, size int64, err error) {
	return spoolArchiveReaderContext(context.Background(), dir, input)
}

func spoolArchiveReaderContext(ctx context.Context, dir string, input io.Reader) (name string, size int64, err error) {
	f, err := os.CreateTemp(dir, "payload-*")
	if err != nil {
		return "", 0, err
	}
	name = f.Name()
	defer func() {
		if err != nil {
			_ = os.Remove(name)
		}
	}()
	size, err = io.Copy(f, &archiveContextReader{ctx: ctx, r: input})
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return "", 0, err
	}
	return name, size, nil
}

func cleanupArchiveItems(items []archiveItem) {
	if len(items) > 0 && items[0].spoolDir != "" {
		_ = os.RemoveAll(items[0].spoolDir)
	}
}

func cleanupExtractedArchive(files []extractedArchiveFile) {
	if len(files) > 0 && files[0].spoolDir != "" {
		_ = os.RemoveAll(files[0].spoolDir)
	}
}

func copyArchiveItem(ctx context.Context, w io.Writer, item archiveItem) error {
	if item.dir {
		return nil
	}
	if item.filePath == "" {
		_, err := io.Copy(w, &archiveContextReader{ctx: ctx, r: bytes.NewReader(item.data)})
		return err
	}
	f, err := os.Open(item.filePath)
	if err != nil {
		return err
	}
	n, copyErr := io.Copy(w, &archiveContextReader{ctx: ctx, r: f})
	closeErr := f.Close()
	if copyErr == nil && n != item.size {
		copyErr = io.ErrUnexpectedEOF
	}
	return errors.Join(copyErr, closeErr)
}

func writeExtractedFile(ctx context.Context, out transferFile, file extractedArchiveFile) error {
	if file.filePath == "" {
		n, err := io.Copy(io.NewOffsetWriter(out, 0), &archiveContextReader{ctx: ctx, r: bytes.NewReader(file.data)})
		if err == nil && n != int64(len(file.data)) {
			err = io.ErrShortWrite
		}
		return err
	}
	input, err := os.Open(file.filePath)
	if err != nil {
		return err
	}
	n, copyErr := io.Copy(io.NewOffsetWriter(out, 0), &archiveContextReader{ctx: ctx, r: input})
	closeInputErr := input.Close()
	if copyErr == nil && n != file.size {
		copyErr = io.ErrUnexpectedEOF
	}
	return errors.Join(copyErr, closeInputErr)
}

// writeStreamToTransfer stages and publishes a stream without a fixed byte
// limit. transferFS.Publish retains the existing no-overwrite guarantee.
func writeStreamToTransfer(ctx context.Context, a *App, sourceID, dest string, input io.Reader) (err error) {
	fs, err := a.streamTransferFS(ctx, sourceID, true)
	if err != nil {
		return err
	}
	defer fs.Close()
	if err = fs.MkdirAll(path.Dir(dest)); err != nil {
		return err
	}
	return writeStreamToTransferFSContext(ctx, fs, dest, input)
}

func writeArchiveToTransferFS(ctx context.Context, fs *transferFS, dest string, items []archiveItem, format string) (err error) {
	stage := path.Join(path.Dir(dest), ".aide-archive-"+newID())
	out, err := fs.Open(stage, os.O_WRONLY|os.O_CREATE|os.O_EXCL)
	if err != nil {
		return err
	}
	published := false
	defer func() {
		if !published {
			err = errors.Join(err, fs.Remove(stage, false))
		}
	}()
	encodeErr := encodeArchiveFormatTo(ctx, items, format, io.NewOffsetWriter(out, 0))
	if encodeErr == nil && fs.root != nil {
		if chmod, ok := out.(interface{ Chmod(os.FileMode) error }); ok {
			encodeErr = chmod.Chmod(0644)
		}
	}
	if encodeErr == nil {
		encodeErr = syncTransferFile(out)
	}
	if err = errors.Join(encodeErr, out.Close()); err != nil {
		return err
	}
	if err = fs.Publish(stage, dest); err != nil {
		return err
	}
	published = true
	return nil
}

func writeArchiveItemToLocation(ctx context.Context, a *App, loc transferLocation, dest string, item archiveItem) error {
	sourceID := "workspace"
	if loc.hasSource {
		sourceID = loc.source.ID
	}
	if a.transferSourceStreaming(sourceID) {
		if item.filePath != "" {
			f, err := os.Open(item.filePath)
			if err != nil {
				return err
			}
			defer f.Close()
			return writeStreamToTransfer(ctx, a, sourceID, dest, f)
		}
		return writeStreamToTransfer(ctx, a, sourceID, dest, bytes.NewReader(item.data))
	}
	if item.filePath != "" {
		data, err := os.ReadFile(item.filePath)
		if err != nil {
			return err
		}
		return a.transferWrite(loc, dest, data)
	}
	return a.transferWrite(loc, dest, item.data)
}

func writeStreamToTransferFS(fs *transferFS, dest string, input io.Reader) error {
	return writeStreamToTransferFSContext(context.Background(), fs, dest, input)
}

func writeStreamToTransferFSContext(ctx context.Context, fs *transferFS, dest string, input io.Reader) (err error) {
	stage := path.Join(path.Dir(dest), ".aide-stream-"+newID())
	out, err := fs.Open(stage, os.O_WRONLY|os.O_CREATE|os.O_EXCL)
	if err != nil {
		return err
	}
	published := false
	defer func() {
		if !published {
			err = errors.Join(err, fs.Remove(stage, false))
		}
	}()
	_, copyErr := io.Copy(io.NewOffsetWriter(out, 0), &archiveContextReader{ctx: ctx, r: input})
	if copyErr == nil && fs.root != nil {
		if chmod, ok := out.(interface{ Chmod(os.FileMode) error }); ok {
			copyErr = chmod.Chmod(0644)
		}
	}
	if copyErr == nil {
		copyErr = syncTransferFile(out)
	}
	if err = errors.Join(copyErr, out.Close()); err != nil {
		return err
	}
	if err = fs.Publish(stage, dest); err != nil {
		return err
	}
	published = true
	return nil
}
