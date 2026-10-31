package server

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
)

func TestStreamTextWindowByteSegmentsLongLine(t *testing.T) {
	text := strings.Repeat("a", maxReadToolWindow*2) + "猫尾"
	data := []byte(text)
	wantHash := sha256.Sum256(data)
	var offset int64
	var got strings.Builder
	for offset < int64(len(data)) {
		window, err := streamTextWindow("long.txt", bytes.NewReader(data), 0, 0, true, offset, maxReadToolWindow)
		if err != nil {
			t.Fatal(err)
		}
		if window.TooLarge || window.ByteEnd <= offset {
			t.Fatalf("invalid byte window at %d: %+v", offset, window)
		}
		if window.Hash != hex.EncodeToString(wantHash[:]) {
			t.Fatalf("hash mismatch: got %s", window.Hash)
		}
		got.Write(window.Content)
		offset = window.ByteEnd
	}
	if got.String() != text {
		t.Fatal("byte windows did not reconstruct the UTF-8 source")
	}
}

func TestWorkspaceByteCoverageRequiresCompleteSingleVersion(t *testing.T) {
	app := &App{}
	task := &Task{}
	data := []byte(strings.Repeat("z", maxReadToolWindow+37))
	first, err := streamTextWindow("large.txt", bytes.NewReader(data), 0, 0, true, 0, maxReadToolWindow)
	if err != nil {
		t.Fatal(err)
	}
	app.rememberWorkspaceByteRange(task, "large.txt", first)
	if _, ok := task.readSnapshots["large.txt"]; ok {
		t.Fatal("partial byte coverage created a write baseline")
	}
	second, err := streamTextWindow("large.txt", bytes.NewReader(data), 0, 0, true, first.ByteEnd, maxReadToolWindow)
	if err != nil {
		t.Fatal(err)
	}
	app.rememberWorkspaceByteRange(task, "large.txt", second)
	if got := task.readSnapshots["large.txt"].BaseHash; got != first.Hash {
		t.Fatalf("complete matching byte coverage did not create the expected baseline: %q", got)
	}

	changed := []byte(strings.Repeat("y", maxReadToolWindow+37))
	newFirst, err := streamTextWindow("large.txt", bytes.NewReader(changed), 0, 0, true, 0, maxReadToolWindow)
	if err != nil {
		t.Fatal(err)
	}
	app.rememberWorkspaceByteRange(task, "large.txt", newFirst)
	if task.readCoverage["large.txt"].Hash != newFirst.Hash || len(task.readCoverage["large.txt"].ByteRanges) != 1 {
		t.Fatal("a changed file version reused old byte coverage")
	}
}

func TestStreamTextWindowLineRangeAndOversizedLine(t *testing.T) {
	window, err := streamTextWindow("lines.txt", strings.NewReader("a\nb\n"), 1, 1, false, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if window.TotalLines != 2 || string(window.Content) != "b\n" || window.TooLarge {
		t.Fatalf("unexpected line window: %+v content=%q", window, window.Content)
	}

	longLine := strings.Repeat("x", maxReadToolWindow+1)
	window, err = streamTextWindow("long.txt", strings.NewReader(longLine), 0, 1, false, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !window.TooLarge || window.TotalLines != 1 || window.Hash == "" || len(window.Content) != 0 || window.LineStart != 0 || window.LineEnd != int64(len(longLine)) {
		t.Fatalf("oversized line should require byte windows: %+v", window)
	}
}

func TestStreamTextWindowNormalizesUTF16JSON(t *testing.T) {
	data := []byte{0xff, 0xfe, '{', 0, '"', 0, 'x', 0, '"', 0, ':', 0, '"', 0, 0x2b, 0x73, '"', 0, '}', 0, '\n', 0}
	want := []byte("{\"x\":\"猫\"}\n")
	wantHash := sha256.Sum256(want)
	window, err := streamTextWindow("data.json", bytes.NewReader(data), 0, 0, true, 0, maxReadToolWindow)
	if err != nil {
		t.Fatal(err)
	}
	if string(window.Content) != string(want) || window.Hash != hex.EncodeToString(wantHash[:]) {
		t.Fatalf("unexpected normalized JSON content=%q hash=%s", window.Content, window.Hash)
	}
}
