package server

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/bzip2"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"strings"
	"time"
)

func normalizeArchiveFormat(format string) (string, error) {
	switch strings.ToLower(strings.TrimPrefix(strings.TrimSpace(format), ".")) {
	case "", "zip":
		return "zip", nil
	case "tar":
		return "tar", nil
	case "tar.gz", "tgz":
		return "tar.gz", nil
	case "tar.bz2", "tbz2", "tbz":
		return "tar.bz2", nil
	case "tar.xz", "txz":
		return "tar.xz", nil
	default:
		return "", errors.New("支持 ZIP、TAR、TAR.GZ、TAR.BZ2 和 TAR.XZ")
	}
}

func archiveExtension(p string) string {
	lower := strings.ToLower(p)
	for _, ext := range []string{".tar.gz", ".tar.bz2", ".tar.xz", ".tgz", ".tbz2", ".tbz", ".txz", ".zip", ".tar", ".gz", ".bz2", ".xz"} {
		if strings.HasSuffix(lower, ext) {
			return ext
		}
	}
	return ""
}

func archiveDestinationName(p string) string {
	name := path.Base(p)
	if ext := archiveExtension(name); ext != "" {
		return strings.TrimSuffix(name, name[len(name)-len(ext):])
	}
	return strings.TrimSuffix(name, path.Ext(name))
}

func writeZipArchive(ctx context.Context, items []archiveItem, output io.Writer) error {
	zw := zip.NewWriter(output)
	for _, item := range items {
		if err := ctx.Err(); err != nil {
			return err
		}
		hdr := &zip.FileHeader{Name: item.name, Method: zip.Deflate}
		if item.dir {
			hdr.SetMode(os.ModeDir | 0755)
		}
		out, err := zw.CreateHeader(hdr)
		if err != nil {
			return err
		}
		if err = copyArchiveItem(ctx, out, item); err != nil {
			return err
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return errors.Join(zw.Close(), ctx.Err())
}

func writeTarArchive(ctx context.Context, items []archiveItem, output io.Writer) error {
	tw := tar.NewWriter(output)
	for _, item := range items {
		if err := ctx.Err(); err != nil {
			return err
		}
		mode := int64(0644)
		typ := byte(tar.TypeReg)
		size := item.size
		if item.filePath == "" {
			size = int64(len(item.data))
		}
		if item.dir {
			mode, typ, size = 0755, tar.TypeDir, 0
		}
		name := item.name
		if item.dir {
			name = strings.TrimSuffix(name, "/") + "/"
		}
		hdr := &tar.Header{Name: name, Mode: mode, Size: size, Typeflag: typ, ModTime: time.Unix(0, 0).UTC(), Format: tar.FormatPAX}
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		if err := copyArchiveItem(ctx, tw, item); err != nil {
			return err
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return errors.Join(tw.Close(), ctx.Err())
}

// encodeArchiveFormatTo streams an archive directly to its destination. The
// request context controls cancellation; no wall-clock or byte ceiling is used.
func encodeArchiveFormatTo(ctx context.Context, items []archiveItem, format string, output io.Writer) error {
	format, err := normalizeArchiveFormat(format)
	if err != nil {
		return err
	}
	if format == "zip" {
		return writeZipArchive(ctx, items, output)
	}
	produce := func(w io.Writer) error { return writeTarArchive(ctx, items, w) }
	switch format {
	case "tar":
		return produce(output)
	case "tar.gz":
		gz := gzip.NewWriter(output)
		produceErr := produce(gz)
		closeErr := gz.Close()
		return errors.Join(produceErr, closeErr, ctx.Err())
	case "tar.bz2", "tar.xz":
		codec := "bz2"
		if format == "tar.xz" {
			codec = "xz"
		}
		reader, writer := io.Pipe()
		producerDone := make(chan error, 1)
		go func() {
			producerDone <- errors.Join(produce(writer), writer.Close())
		}()
		decodeErr := pythonArchiveCodecStream(ctx, "compress", codec, reader, output)
		_ = reader.CloseWithError(decodeErr)
		produceErr := <-producerDone
		return errors.Join(decodeErr, produceErr, ctx.Err())
	default:
		return errors.New("压缩格式无效")
	}
}

func encodeArchiveFormat(items []archiveItem, format string) ([]byte, error) {
	var out bytes.Buffer
	err := encodeArchiveFormatTo(context.Background(), items, format, &out)
	return out.Bytes(), err
}

func encodeArchive(items []archiveItem) ([]byte, error) {
	return encodeArchiveFormat(items, "zip")
}

func pythonArchiveCodec(action, codec string, input []byte) ([]byte, error) {
	var out bytes.Buffer
	err := pythonArchiveCodecStream(context.Background(), action, codec, bytes.NewReader(input), &out)
	return out.Bytes(), err
}

func pythonArchiveCodecStream(ctx context.Context, action, codec string, input io.Reader, output io.Writer) error {
	if codec != "bz2" && codec != "xz" {
		return errors.New("压缩格式无效")
	}
	// Codec input and output stay bounded to small chunks. The request context,
	// rather than a fixed timeout, determines how long a large archive may run.
	script := `import bz2,lzma,sys
mode,kind=sys.argv[1:3]
src=sys.stdin.buffer
if mode=='compress':
 c=(bz2.BZ2Compressor() if kind=='bz2' else lzma.LZMACompressor())
 while True:
  b=src.read(65536)
  if not b: break
  sys.stdout.buffer.write(c.compress(b))
 sys.stdout.buffer.write(c.flush())
else:
 d=(bz2.BZ2Decompressor() if kind=='bz2' else lzma.LZMADecompressor())
 while True:
  b=src.read(65536)
  if not b: break
  while b or not d.needs_input:
   out=d.decompress(b,max_length=1048576)
   b=b''
   sys.stdout.buffer.write(out)
   if d.eof: break
   if not out and d.needs_input: break
`
	cmd := exec.CommandContext(ctx, "python3", "-I", "-c", script, action, codec)
	cmd.Stdin = input
	cmd.Stdout = output
	cmd.Stderr = &archiveOutputBuffer{limit: 4096}
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("压缩处理已取消: %w", ctx.Err())
		}
		return fmt.Errorf("压缩处理失败: %w", err)
	}
	return nil
}

// archiveOutputBuffer is now used only for bounded subprocess diagnostics.
type archiveOutputBuffer struct {
	buf      bytes.Buffer
	limit    int
	exceeded bool
}

func (w *archiveOutputBuffer) Write(p []byte) (int, error) {
	originalLen := len(p)
	remaining := w.limit - w.buf.Len()
	if remaining <= 0 {
		w.exceeded = true
		return originalLen, nil
	}
	if len(p) > remaining {
		p = p[:remaining]
		w.exceeded = true
	}
	_, _ = w.buf.Write(p)
	return originalLen, nil
}

func spoolArchiveCodec(ctx context.Context, codec string, input io.Reader, spoolDir string) (*os.File, error) {
	f, err := os.CreateTemp(spoolDir, "decoded-*")
	if err != nil {
		return nil, err
	}
	name := f.Name()
	if err = pythonArchiveCodecStream(ctx, "decompress", codec, input, f); err == nil {
		_, err = f.Seek(0, io.SeekStart)
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		_ = os.Remove(name)
		return nil, err
	}
	return os.Open(name)
}

func readArchiveFilesForPath(data []byte, filePath string) ([]extractedArchiveFile, error) {
	return readArchiveFilesFromReader(context.Background(), bytes.NewReader(data), int64(len(data)), filePath)
}

func readArchiveFilesFromReader(ctx context.Context, input io.ReaderAt, size int64, filePath string) (files []extractedArchiveFile, err error) {
	spoolDir, err := os.MkdirTemp("", "aide-archive-output-*")
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			_ = os.RemoveAll(spoolDir)
		}
	}()
	ext := archiveExtension(filePath)
	switch ext {
	case ".zip":
		zr, zipErr := zip.NewReader(input, size)
		if zipErr != nil {
			return nil, errors.New("不是有效的 ZIP 文件")
		}
		return readZipArchive(ctx, zr, spoolDir)
	case ".tar", ".tar.gz", ".tgz", ".tar.bz2", ".tbz2", ".tbz", ".tar.xz", ".txz":
		var reader io.Reader = io.NewSectionReader(input, 0, size)
		var closer io.Closer
		switch ext {
		case ".tar.gz", ".tgz":
			var gz *gzip.Reader
			gz, err = gzip.NewReader(reader)
			if err != nil {
				return nil, errors.New("不是有效的 TAR.GZ 文件")
			}
			reader, closer = gz, gz
		case ".tar.bz2", ".tbz2", ".tbz":
			reader = bzip2.NewReader(reader)
		case ".tar.xz", ".txz":
			var decoded *os.File
			decoded, err = spoolArchiveCodec(ctx, "xz", reader, spoolDir)
			if err != nil {
				return nil, err
			}
			reader, closer = decoded, decoded
		}
		if closer != nil {
			defer func() { err = errors.Join(err, closer.Close()) }()
		}
		return readTarArchiveContext(ctx, reader, spoolDir)
	case ".gz", ".bz2", ".xz":
		var reader io.Reader = io.NewSectionReader(input, 0, size)
		var closer io.Closer
		switch ext {
		case ".gz":
			var gz *gzip.Reader
			gz, err = gzip.NewReader(reader)
			if err != nil {
				return nil, errors.New("不是有效的 GZIP 文件")
			}
			reader, closer = gz, gz
		case ".bz2":
			reader = bzip2.NewReader(reader)
		case ".xz":
			var decoded *os.File
			decoded, err = spoolArchiveCodec(ctx, "xz", reader, spoolDir)
			if err != nil {
				return nil, err
			}
			reader, closer = decoded, decoded
		}
		if closer != nil {
			defer func() { err = errors.Join(err, closer.Close()) }()
		}
		contentPath, contentSize, spoolErr := spoolArchiveReaderContext(ctx, spoolDir, reader)
		if spoolErr != nil {
			return nil, fmt.Errorf("解压失败: %w", spoolErr)
		}
		name := path.Base(filePath)
		name = strings.TrimSuffix(name, filePath[len(filePath)-len(ext):])
		clean, cleanErr := cleanArchiveEntryPath(name, false)
		if cleanErr != nil || clean == "" {
			return nil, errors.New("压缩文件名无效")
		}
		return []extractedArchiveFile{{path: clean, filePath: contentPath, size: contentSize, spoolDir: spoolDir}}, nil
	default:
		return nil, errors.New("支持 ZIP、TAR、TAR.GZ、TAR.BZ2 和 TAR.XZ")
	}
}

func readArchiveFiles(data []byte) (files []extractedArchiveFile, err error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, errors.New("不是有效的 ZIP 文件")
	}
	spoolDir, err := os.MkdirTemp("", "aide-archive-output-*")
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			_ = os.RemoveAll(spoolDir)
		}
	}()
	return readZipArchive(context.Background(), zr, spoolDir)
}

func readZipArchive(ctx context.Context, zr *zip.Reader, spoolDir string) ([]extractedArchiveFile, error) {
	if len(zr.File) == 0 {
		return nil, errors.New("ZIP 文件为空")
	}
	files := make([]extractedArchiveFile, 0, len(zr.File))
	seen := map[string]bool{}
	for _, entry := range zr.File {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		isDir := entry.FileInfo().IsDir() || strings.HasSuffix(entry.Name, "/") || strings.HasSuffix(entry.Name, "\\")
		name, pathErr := cleanArchiveEntryPath(entry.Name, isDir)
		if pathErr != nil {
			return nil, pathErr
		}
		if name == "" && isDir {
			continue
		}
		if seen[name] {
			return nil, errors.New("ZIP 包含重复路径")
		}
		seen[name] = true
		if entry.Mode()&os.ModeSymlink != 0 {
			return nil, errors.New("ZIP 不支持符号链接")
		}
		f := extractedArchiveFile{path: name, dir: isDir, spoolDir: spoolDir}
		if !isDir {
			rc, err := entry.Open()
			if err != nil {
				return nil, err
			}
			f.filePath, f.size, err = spoolArchiveReaderContext(ctx, spoolDir, rc)
			closeErr := rc.Close()
			if err = errors.Join(err, closeErr); err != nil {
				return nil, err
			}
		}
		files = append(files, f)
	}
	if err := validateArchiveLayout(files); err != nil {
		return nil, err
	}
	return files, nil
}

func readTarArchive(r io.Reader) (files []extractedArchiveFile, err error) {
	spoolDir, err := os.MkdirTemp("", "aide-archive-output-*")
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			_ = os.RemoveAll(spoolDir)
		}
	}()
	return readTarArchiveContext(context.Background(), r, spoolDir)
}

func readTarArchiveContext(ctx context.Context, r io.Reader, spoolDir string) ([]extractedArchiveFile, error) {
	tr := tar.NewReader(r)
	files := make([]extractedArchiveFile, 0)
	seen := map[string]bool{}
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, errors.New("TAR 包损坏或格式不支持")
		}
		isDir := hdr.Typeflag == tar.TypeDir
		if hdr.Typeflag != tar.TypeReg && hdr.Typeflag != tar.TypeRegA && !isDir {
			return nil, errors.New("TAR 不支持链接或特殊文件")
		}
		name, err := cleanArchiveEntryPath(hdr.Name, isDir)
		if err != nil {
			return nil, err
		}
		if name == "" && isDir {
			continue
		}
		if seen[name] {
			return nil, errors.New("归档包含重复路径")
		}
		seen[name] = true
		f := extractedArchiveFile{path: name, dir: isDir, spoolDir: spoolDir}
		if !isDir {
			if hdr.Size < 0 {
				return nil, errors.New("TAR 条目大小无效")
			}
			f.filePath, f.size, err = spoolArchiveReaderContext(ctx, spoolDir, tr)
			if err != nil {
				return nil, err
			}
		}
		files = append(files, f)
	}
	if len(files) == 0 {
		return nil, errors.New("归档为空")
	}
	if err := validateArchiveLayout(files); err != nil {
		return nil, err
	}
	return files, nil
}

func archiveNameForFormat(p, format string) string {
	format, err := normalizeArchiveFormat(format)
	if err != nil {
		format = "zip"
	}
	return p + "." + format
}

func archiveBaseForPath(p string) string {
	if ext := archiveExtension(p); ext != "" {
		return strings.TrimSuffix(p, p[len(p)-len(ext):])
	}
	return strings.TrimSuffix(p, path.Ext(p))
}
