package server

import (
	"archive/tar"
	"bytes"
	"compress/bzip2"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"path"
	"strings"
	"time"
)

const maxArchiveEncoded = 128 << 20

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

func encodeArchiveFormat(items []archiveItem, format string) ([]byte, error) {
	format, err := normalizeArchiveFormat(format)
	if err != nil {
		return nil, err
	}
	if format == "zip" {
		return encodeArchive(items)
	}
	var tarData bytes.Buffer
	tw := tar.NewWriter(&tarData)
	for _, item := range items {
		mode := int64(0644)
		typ := byte(tar.TypeReg)
		size := int64(len(item.data))
		if item.dir {
			mode, typ, size = 0755, tar.TypeDir, 0
		}
		name := item.name
		if item.dir {
			name = strings.TrimSuffix(name, "/") + "/"
		}
		hdr := &tar.Header{Name: name, Mode: mode, Size: size, Typeflag: typ, ModTime: time.Unix(0, 0).UTC(), Format: tar.FormatPAX}
		if err = tw.WriteHeader(hdr); err != nil {
			return nil, err
		}
		if !item.dir {
			if _, err = tw.Write(item.data); err != nil {
				return nil, err
			}
		}
	}
	if err = tw.Close(); err != nil {
		return nil, err
	}
	if format == "tar" {
		return tarData.Bytes(), nil
	}
	if format == "tar.gz" {
		var out bytes.Buffer
		gz := gzip.NewWriter(&out)
		if _, err = gz.Write(tarData.Bytes()); err == nil {
			err = gz.Close()
		} else {
			_ = gz.Close()
		}
		if err != nil {
			return nil, err
		}
		return out.Bytes(), nil
	}
	codec := "bz2"
	if format == "tar.xz" {
		codec = "xz"
	}
	return pythonArchiveCodec("compress", codec, tarData.Bytes())
}

func pythonArchiveCodec(action, codec string, input []byte) ([]byte, error) {
	if codec != "bz2" && codec != "xz" {
		return nil, errors.New("压缩格式无效")
	}
	// Stream in bounded chunks and reject output above the existing archive cap.
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
 total=0
 while True:
  b=src.read(65536)
  if not b: break
  while b or not d.needs_input:
   out=d.decompress(b,max_length=(67108864-total)+1)
   b=b''
   total+=len(out)
   if total>67108864: raise ValueError('解压内容超过 64 MB 限制')
   sys.stdout.buffer.write(out)
   if d.eof: break
   if not out and d.needs_input: break
`
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "python3", "-I", "-c", script, action, codec)
	cmd.Stdin = bytes.NewReader(input)
	out := &archiveOutputBuffer{limit: maxArchiveEncoded}
	cmd.Stdout = out
	cmd.Stderr = &archiveOutputBuffer{limit: 4096}
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, errors.New("压缩处理超时")
		}
		if out.exceeded {
			return nil, fmt.Errorf("归档内容超过 %d MB 限制", maxArchiveEncoded>>20)
		}
		return nil, fmt.Errorf("压缩处理失败: %w", err)
	}
	if out.exceeded {
		return nil, fmt.Errorf("归档内容超过 %d MB 限制", maxArchiveEncoded>>20)
	}
	return out.buf.Bytes(), nil
}

type archiveOutputBuffer struct {
	buf      bytes.Buffer
	limit    int
	exceeded bool
}

func (w *archiveOutputBuffer) Write(p []byte) (int, error) {
	if len(p) > w.limit-w.buf.Len() {
		w.exceeded = true
		return 0, errors.New("归档输出超出限制")
	}
	return w.buf.Write(p)
}

func readArchiveFilesForPath(data []byte, filePath string) ([]extractedArchiveFile, error) {
	ext := archiveExtension(filePath)
	switch ext {
	case ".zip":
		return readArchiveFiles(data)
	case ".tar", ".tar.gz", ".tgz", ".tar.bz2", ".tbz2", ".tbz", ".tar.xz", ".txz":
		var reader io.Reader = bytes.NewReader(data)
		var closer io.Closer
		var err error
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
			decoded, e := pythonArchiveCodec("decompress", "xz", data)
			if e != nil {
				return nil, e
			}
			reader = bytes.NewReader(decoded)
		}
		if closer != nil {
			defer closer.Close()
		}
		return readTarArchive(reader)
	case ".gz", ".bz2", ".xz":
		var reader io.Reader = bytes.NewReader(data)
		var err error
		switch ext {
		case ".gz":
			gz, e := gzip.NewReader(reader)
			if e != nil {
				return nil, errors.New("不是有效的 GZIP 文件")
			}
			defer gz.Close()
			reader = gz
		case ".bz2":
			reader = bzip2.NewReader(reader)
		case ".xz":
			decoded, e := pythonArchiveCodec("decompress", "xz", data)
			if e != nil {
				return nil, e
			}
			reader = bytes.NewReader(decoded)
		}
		var content []byte
		content, err = io.ReadAll(io.LimitReader(reader, maxRawFile+1))
		if err != nil {
			return nil, fmt.Errorf("解压失败: %w", err)
		}
		if len(content) > maxRawFile {
			return nil, fmt.Errorf("解压内容超过 %d MB 限制", maxRawFile/(1<<20))
		}
		name := path.Base(filePath)
		name = strings.TrimSuffix(name, filePath[len(filePath)-len(ext):])
		clean, err := cleanArchiveEntryPath(name, false)
		if err != nil || clean == "" {
			return nil, errors.New("压缩文件名无效")
		}
		return []extractedArchiveFile{{path: clean, data: content}}, nil
	default:
		return nil, errors.New("支持 ZIP、TAR、TAR.GZ、TAR.BZ2 和 TAR.XZ")
	}
}

func readTarArchive(r io.Reader) ([]extractedArchiveFile, error) {
	tr := tar.NewReader(io.LimitReader(r, maxArchiveEncoded+1))
	files := make([]extractedArchiveFile, 0)
	seen := map[string]bool{}
	total := 0
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, errors.New("TAR 包损坏或格式不支持")
		}
		if len(files) >= maxArchiveFiles {
			return nil, fmt.Errorf("归档最多包含 %d 个条目", maxArchiveFiles)
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
		f := extractedArchiveFile{path: name, dir: isDir}
		if !isDir {
			if hdr.Size < 0 || hdr.Size > int64(maxRawFile-total) {
				return nil, fmt.Errorf("解压内容超过 %d MB 限制", maxRawFile/(1<<20))
			}
			f.data, err = io.ReadAll(io.LimitReader(tr, int64(maxRawFile-total)+1))
			if err != nil {
				return nil, err
			}
			total += len(f.data)
			if total > maxRawFile {
				return nil, fmt.Errorf("解压内容超过 %d MB 限制", maxRawFile/(1<<20))
			}
		}
		files = append(files, f)
	}
	if len(files) == 0 {
		return nil, errors.New("归档为空")
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
