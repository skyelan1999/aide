package server

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func issue77Archive(t *testing.T, expanded int) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	f, err := w.Create("word/document.xml")
	if err != nil {
		t.Fatal(err)
	}
	block := bytes.Repeat([]byte("x"), 32<<10)
	for expanded > 0 {
		n := min(len(block), expanded)
		if _, err = f.Write(block[:n]); err != nil {
			t.Fatal(err)
		}
		expanded -= n
	}
	if err = w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}
func TestIssue77LegacyOfficeAdmission(t *testing.T) {
	for _, kind := range []string{"raw-size", "expanded-size", "invalid-member"} {
		t.Run(kind, func(t *testing.T) {
			a := testApp(t)
			scripts := t.TempDir()
			for _, name := range []string{"docx_structure.py", "extract_text.py"} {
				if err := os.WriteFile(filepath.Join(scripts, name), []byte("print('parser-reached')\n"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			t.Setenv("AIDE_OFFICE_SCRIPTS", scripts)
			full := filepath.Join(a.workPath, "oversized.docx")
			if kind == "raw-size" {
				f, err := os.Create(full)
				if err != nil {
					t.Fatal(err)
				}
				err = f.Truncate(maxRawFile + 1)
				_ = f.Close()
				if err != nil {
					t.Fatal(err)
				}
			} else {
				size := 129 << 20
				if kind == "invalid-member" {
					size = 1 << 20
				}
				data := issue77Archive(t, size)
				if kind == "invalid-member" {
					offset := bytes.Index(data, []byte{'P', 'K', 1, 2})
					if offset < 0 {
						t.Fatal("missing central directory")
					}
					binary.LittleEndian.PutUint32(data[offset+24:offset+28], 1)
				}
				if err := os.WriteFile(full, data, 0600); err != nil {
					t.Fatal(err)
				}
			}
			if out := a.docxTool(a.workspace, "", "oversized.docx", "docx_structure.py"); strings.Contains(out, "parser-reached") {
				t.Fatal("DOCX parser ran before input admission")
			}
			if out, err := a.extractDocumentText(Attachment{Root: "workspace", Path: "oversized.docx"}); err == nil || strings.Contains(out, "parser-reached") {
				t.Fatalf("attachment parser ran before admission: %q %v", out, err)
			}
		})
	}
}
func TestIssue77OfficeParserMemoryLimit(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux parser resource limit")
	}
	dir := t.TempDir()
	script := filepath.Join(dir, "resource-probe.py")
	source := "import json, resource, sys\nprint(json.dumps({'limit':resource.getrlimit(resource.RLIMIT_AS),'arg':sys.argv[1]}))\n"
	if err := os.WriteFile(script, []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	out, err := runOfficeScript(dir, script, "argument-control")
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Limit []int64 `json:"limit"`
		Arg   string  `json:"arg"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatal(err, out)
	}
	if len(got.Limit) != 2 || got.Limit[0] < 0 || got.Limit[0] > 512<<20 || got.Limit[1] < 0 || got.Limit[1] > 512<<20 {
		t.Fatalf("parser has unbounded address space: %+v", got)
	}
	if got.Arg != "argument-control" {
		t.Fatal("parser arguments changed")
	}
}
