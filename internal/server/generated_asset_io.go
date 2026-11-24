package server

import (
	"io"
	"os"
)

// spoolGeneratedAsset bounds memory without imposing a file-size ceiling. The
// caller validates the returned header, rewinds the file, and publishes it via
// the same no-overwrite streaming path as other workspace artifacts.
func spoolGeneratedAsset(input io.Reader) (file *os.File, header []byte, err error) {
	file, err = os.CreateTemp("", "aide-generated-*")
	if err != nil {
		return nil, nil, err
	}
	keep := false
	defer func() {
		if !keep {
			name := file.Name()
			_ = file.Close()
			_ = os.Remove(name)
		}
	}()
	if _, err = io.Copy(file, input); err != nil {
		return nil, nil, err
	}
	if _, err = file.Seek(0, io.SeekStart); err != nil {
		return nil, nil, err
	}
	header = make([]byte, 512)
	n, readErr := file.Read(header)
	if readErr != nil && readErr != io.EOF {
		return nil, nil, readErr
	}
	header = header[:n]
	if _, err = file.Seek(0, io.SeekStart); err != nil {
		return nil, nil, err
	}
	keep = true
	return file, header, nil
}
