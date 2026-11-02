package server

import (
	"bytes"
	"context"
	"io"
	"os"
	"unicode/utf8"
)

// Do not expose File.WriteTo: every copy chunk must observe cancellation.
type executionContextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r executionContextReader) Read(b []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(b)
}

// Apply the ordinary UTF-8/no-NUL text contract with bounded memory, including
// code points split across chunks. Invalid/binary artifacts are never ReadAll'd.
func executionTextArtifact(ctx context.Context, path string) (bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer f.Close()
	buf := make([]byte, 64<<10)
	carry := 0
	for {
		n, err := (executionContextReader{ctx, f}).Read(buf[carry:])
		n += carry
		chunk := buf[:n]
		if bytes.IndexByte(chunk, 0) >= 0 {
			return false, nil
		}
		carry = 0
		if !utf8.Valid(chunk) {
			for tail := 1; tail < utf8.UTFMax && tail <= n; tail++ {
				if !utf8.FullRune(chunk[n-tail:]) && utf8.Valid(chunk[:n-tail]) {
					carry = copy(buf, chunk[n-tail:])
					break
				}
			}
			if carry == 0 {
				return false, nil
			}
		}
		if err == io.EOF {
			return carry == 0, nil
		}
		if err != nil {
			return false, err
		}
	}
}
