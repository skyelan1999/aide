package server

import (
	"io"
	"os"
	"testing"
)

func TestSpoolGeneratedAssetHasNoFormerVideoLimit(t *testing.T) {
	const size = int64(100<<20 + 1)
	file, header, err := spoolGeneratedAsset(io.LimitReader(zeroReader{}, size))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		name := file.Name()
		_ = file.Close()
		_ = os.Remove(name)
	}()
	if len(header) != 512 {
		t.Fatalf("header length = %d", len(header))
	}
	info, err := file.Stat()
	if err != nil || info.Size() != size {
		t.Fatalf("spooled size = %v, err=%v", info, err)
	}
}
