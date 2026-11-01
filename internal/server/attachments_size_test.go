package server

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"
)

func TestReadLocalImageHasNoFiveMiBLimit(t *testing.T) {
	a := testApp(t)
	const imageSize = int64(5<<20) + 1
	file, err := os.Create(filepath.Join(a.workPath, "large.png"))
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(imageSize); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	img, err := a.readLocalImage(Attachment{Root: "workspace", Path: "large.png"})
	if err != nil {
		t.Fatalf("image larger than 5 MiB should be accepted: %v", err)
	}
	const prefix = "data:image/png;base64,"
	if len(img.DataURL) != len(prefix)+base64.StdEncoding.EncodedLen(int(imageSize)) {
		t.Fatalf("image payload was truncated: got %d data URL bytes", len(img.DataURL))
	}
}
