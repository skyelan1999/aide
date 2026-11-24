package server

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestOfficeDocumentReadEditChecksVersion(t *testing.T) {
	scripts, _ := filepath.Abs("../../scripts/office")
	t.Setenv("AIDE_OFFICE_SCRIPTS", scripts)
	for _, kind := range []string{"docx", "xlsx", "pptx"} {
		t.Run(kind, func(t *testing.T) {
			a := testApp(t)
			spec := map[string]any{"blocks": []any{map[string]any{"type": "paragraph", "text": "Original"}}}
			if kind == "xlsx" {
				spec = map[string]any{"sheets": []any{map[string]any{"name": "Data", "rows": []any{[]any{"Original", 42}}}}}
			}
			if kind == "pptx" {
				spec = map[string]any{"slides": []any{map[string]any{"title": "Original", "body": "Body"}}}
			}
			b, e := officeCreateBytes(kind, spec)
			if e != nil {
				t.Fatal(e)
			}
			p := "test." + kind
			dwgWrite(t, a, p, b)
			in := officeDocumentRequest{Path: p}
			out, e := a.officeDocumentOperation(context.Background(), in, false, true, a.wsID())
			if e != nil {
				t.Fatal(e)
			}
			blocks := out["blocks"].([]any)
			block := blocks[0].(map[string]any)
			in.Hash = out["hash"].(string)
			in.Changes = []map[string]any{{"id": block["id"], "expectedText": block["text"], "newText": "Updated by AI"}}
			if _, e = a.officeDocumentOperation(context.Background(), in, true, true, a.wsID()); e != nil {
				t.Fatal(e)
			}
			after, _ := a.workspace.ReadFile(p)
			text, e := officeExtractText(after, "."+kind)
			if e != nil || !strings.Contains(text, "Updated by AI") {
				t.Fatal(text, e)
			}
			if _, e = a.officeDocumentOperation(context.Background(), in, true, true, a.wsID()); e == nil {
				t.Fatal("stale hash accepted")
			}
			out, e = a.officeDocumentOperation(context.Background(), officeDocumentRequest{Path: p}, false, true, a.wsID())
			if e != nil {
				t.Fatal(e)
			}
			in.Hash = out["hash"].(string)
			if _, e = a.officeDocumentOperation(context.Background(), in, true, true, "old-workspace"); e == nil {
				t.Fatal("stale workspace accepted")
			}
		})
	}
}
func TestOfficeDocumentNativeToolDispatchAndSandbox(t *testing.T) {
	useSourceOfficeScripts(t)
	a := testApp(t)
	b, e := officeCreateBytes("docx", map[string]any{"blocks": []any{map[string]any{"type": "paragraph", "text": "Native dispatch"}}})
	if e != nil {
		t.Fatal(e)
	}
	dwgWrite(t, a, "dispatch.docx", b)
	out := releasePublicNative(t, a, "office_document_read", map[string]any{"path": "dispatch.docx"})
	var result map[string]any
	if json.Unmarshal([]byte(out), &result) != nil {
		t.Fatal(out)
	}
	block := result["blocks"].([]any)[0].(map[string]any)
	out = releasePublicNative(t, a, "office_document_edit", map[string]any{"path": "dispatch.docx", "hash": result["hash"], "changes": []any{map[string]any{"id": block["id"], "expectedText": block["text"], "newText": "Native AI edited"}}})
	if !strings.Contains(out, `"saved":true`) {
		t.Fatal(out)
	}
	a.mu.Lock()
	a.settings.SandboxMode = "read-only"
	a.mu.Unlock()
	if a.sandboxToolError("office_document_read") != nil || a.sandboxToolError("office_document_edit") == nil {
		t.Fatal("read-only sandbox boundary wrong")
	}
}

func TestOfficeDocumentReadLargeArchiveAndPagination(t *testing.T) {
	useSourceOfficeScripts(t)
	a := testApp(t)
	blocks := make([]any, 201)
	for i := range blocks {
		blocks[i] = map[string]any{"type": "paragraph", "text": fmt.Sprintf("Paragraph %d", i)}
	}
	small, err := officeCreateBytes("docx", map[string]any{"blocks": blocks})
	if err != nil {
		t.Fatal(err)
	}
	dwgWrite(t, a, "small.docx", small)
	const name = "large.docx"
	checksum := writeOfficeDocumentWithLargeMedia(t, a.workspace.Name(), name, small)
	for _, offset := range []int{0, 200} {
		large, err := a.officeDocumentOperation(context.Background(), officeDocumentRequest{Path: name, Offset: offset}, false, true, a.wsID())
		if err != nil {
			t.Fatal(err)
		}
		regular, err := a.officeDocumentOperation(context.Background(), officeDocumentRequest{Path: "small.docx", Offset: offset}, false, true, a.wsID())
		if err != nil {
			t.Fatal(err)
		}
		if large["hash"] != checksum || large["readOnly"] != false || regular["readOnly"] != false {
			t.Fatal("file version or write eligibility changed", large["hash"], large["readOnly"], regular["readOnly"])
		}
		for _, field := range []string{"format", "blocks", "offset", "nextOffset", "truncated"} {
			got, _ := json.Marshal(large[field])
			want, _ := json.Marshal(regular[field])
			if !bytes.Equal(got, want) {
				t.Fatalf("offset %d, field %s differs", offset, field)
			}
		}
		if offset == 0 && large["nextOffset"] != float64(200) || offset == 200 && (large["nextOffset"] != nil || len(large["blocks"].([]any)) != 1) {
			t.Fatal("incorrect pagination", offset, large["nextOffset"])
		}
	}
	if _, err = a.officeDocumentOperation(context.Background(), officeDocumentRequest{Path: name, Hash: checksum}, true, true, a.wsID()); err == nil {
		t.Fatal("large Office write was accepted")
	}
	file, err := a.workspace.Open(name)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	h := sha256.New()
	if _, err := io.Copy(h, file); err != nil || hex.EncodeToString(h.Sum(nil)) != checksum {
		t.Fatal("source file changed", err)
	}
}

func writeOfficeDocumentWithLargeMedia(t *testing.T, dir, name string, small []byte) string {
	t.Helper()
	original, err := zip.NewReader(bytes.NewReader(small), int64(len(small)))
	if err != nil {
		t.Fatal(err)
	}
	file, err := os.Create(filepath.Join(dir, name))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	checksum := sha256.New()
	archive := zip.NewWriter(io.MultiWriter(file, checksum))
	for _, entry := range original.File {
		if entry.Name != "[Content_Types].xml" {
			if err := archive.Copy(entry); err != nil {
				t.Fatal(err)
			}
			continue
		}
		content, err := original.Open(entry.Name)
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(content)
		content.Close()
		if err != nil {
			t.Fatal(err)
		}
		part, err := archive.Create(entry.Name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = part.Write(bytes.Replace(data, []byte("</Types>"), []byte(`<Default Extension="bmp" ContentType="image/bmp"/></Types>`), 1)); err != nil {
			t.Fatal(err)
		}
	}
	// A valid 24-bit bitmap exceeds both the previous source and expansion limits.
	const width, height = 4096, 11000
	const pixels = width * height * 3
	var header [54]byte
	copy(header[:], "BM")
	for offset, value := range map[int]uint32{2: pixels + 54, 10: 54, 14: 40, 18: width, 22: height, 34: pixels} {
		binary.LittleEndian.PutUint32(header[offset:], value)
	}
	binary.LittleEndian.PutUint16(header[26:], 1)
	binary.LittleEndian.PutUint16(header[28:], 24)
	media, err := archive.CreateHeader(&zip.FileHeader{Name: "word/media/unused.bmp", Method: zip.Store})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = media.Write(header[:]); err != nil {
		t.Fatal(err)
	}
	row := make([]byte, width*3)
	for range height {
		if _, err = media.Write(row); err != nil {
			t.Fatal(err)
		}
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	if info, err := file.Stat(); err != nil || info.Size() <= 128<<20 {
		t.Fatal("expected an actual Office archive larger than 128 MiB", err)
	}
	return hex.EncodeToString(checksum.Sum(nil))
}

func TestOfficeDocumentReadChecksParsedXML(t *testing.T) {
	useSourceOfficeScripts(t)
	a := testApp(t)
	large := issue77Archive(t, 129<<20)
	dwgWrite(t, a, "oversized.docx", large)
	_, err := a.officeDocumentOperation(context.Background(), officeDocumentRequest{Path: "oversized.docx"}, false, true, a.wsID())
	if err == nil || !strings.Contains(err.Error(), "Office XML 超过") {
		t.Fatal("oversized XML was not rejected by the expansion guard", err)
	}
	corrupt := issue77Archive(t, 1)
	central := bytes.Index(corrupt, []byte{'P', 'K', 1, 2})
	if central < 0 {
		t.Fatal("missing central ZIP directory")
	}
	binary.LittleEndian.PutUint32(corrupt[central+16:], 0)
	dwgWrite(t, a, "corrupt.docx", corrupt)
	_, err = a.officeDocumentOperation(context.Background(), officeDocumentRequest{Path: "corrupt.docx"}, false, true, a.wsID())
	if err == nil || !strings.Contains(err.Error(), "Bad CRC-32") {
		t.Fatal("corrupt XML was not rejected by the CRC check", err)
	}
}

func TestOfficeDocumentReadAdmissionWhileParsing(t *testing.T) {
	for _, scenario := range []string{"cancellation", "lock-generation", "source-visibility"} {
		t.Run(scenario, func(t *testing.T) { testOfficeDocumentReadAdmission(t, scenario) })
	}
}

func testOfficeDocumentReadAdmission(t *testing.T, scenario string) {
	t.Helper()
	a := testApp(t)
	in := officeDocumentRequest{Path: "fixture.docx"}
	if scenario == "source-visibility" {
		in.Source = "fixture-source"
		a.sourceRegistry.Sources = append(a.sourceRegistry.Sources, Source{ID: in.Source, Name: "Fixture", Type: "local", Enabled: true})
		if err := a.reference.WriteFile(in.Path, []byte("fixture"), 0600); err != nil {
			t.Fatal(err)
		}
	} else {
		dwgWrite(t, a, in.Path, []byte("fixture"))
	}
	scripts, scratch := t.TempDir(), t.TempDir()
	t.Setenv("AIDE_OFFICE_SCRIPTS", scripts)
	t.Setenv("TMPDIR", scratch)
	marker := filepath.Join(scratch, "parser-started")
	t.Setenv("AIDE_TEST_OFFICE_PARSER_MARKER", marker)
	script := `import os, pathlib, sys, time
marker = pathlib.Path(os.environ['AIDE_TEST_OFFICE_PARSER_MARKER'])
marker.touch()
while not marker.with_name('parser-release').exists():
    time.sleep(0.01)
pathlib.Path(sys.argv[-1]).write_text('{"blocks": [{"text": "test data"}]}')
`
	if err := os.WriteFile(filepath.Join(scripts, "office_document.py"), []byte(script), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() {
		out, err := a.officeDocumentOperation(ctx, in, false, true, a.wsID())
		if out != nil && err != nil {
			err = fmt.Errorf("read exposed output after losing admission: %v", out)
		}
		result <- err
	}()
	deadline := time.After(5 * time.Second)
	for {
		if _, err := os.Stat(marker); err == nil {
			break
		}
		select {
		case err := <-result:
			t.Fatal("Office parser did not start", err)
		case <-deadline:
			t.Fatal("Office parser did not start within five seconds")
		case <-time.After(10 * time.Millisecond):
		}
	}
	var want string
	switch scenario {
	case "cancellation":
		cancel()
	case "lock-generation":
		if _, err := a.writeLockState(true); err != nil {
			t.Fatal(err)
		}
		if _, err := a.writeLockState(false); err != nil {
			t.Fatal(err)
		}
		want = "锁屏状态已变化"
	case "source-visibility":
		a.mu.Lock()
		for i := range a.sourceRegistry.Sources {
			if a.sourceRegistry.Sources[i].ID == in.Source {
				a.sourceRegistry.Sources[i].AIHiddenWorkspaces = []string{a.wsID()}
			}
		}
		a.mu.Unlock()
		want = "来源"
	}
	if want != "" {
		if err := os.WriteFile(filepath.Join(scratch, "parser-release"), nil, 0600); err != nil {
			t.Fatal(err)
		}
	}
	select {
	case err := <-result:
		if want == "" && !errors.Is(err, context.Canceled) || want != "" && (err == nil || !strings.Contains(err.Error(), want)) {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Office parser did not stop after cancellation")
	}
	if leftover, err := filepath.Glob(filepath.Join(scratch, "aide-office-*")); err != nil || len(leftover) != 0 {
		t.Fatal("private Office snapshot was not removed", leftover, err)
	}
}
