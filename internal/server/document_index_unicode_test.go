package server

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestGeneratedDocumentIndexesPreserveUnicodeTitles(t *testing.T) {
	for _, prefix := range []string{"REQ", "DESIGN", "IMPL", "TEST", "RCA"} {
		t.Run(prefix, func(t *testing.T) {
			dir := t.TempDir()
			titles := []string{"接口隔离与恢复", "ASCII isolation", "混合 Unicode · 第二节"}
			for i, title := range titles {
				name := fmt.Sprintf("%s-%03d-document.md", prefix, i+1)
				body := fmt.Sprintf("# %s-%03d · %s\n\n- 状态：待评审\n", prefix, i+1, title)
				if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0600); err != nil {
					t.Fatal(err)
				}
			}
			index := "index.md"
			var err error
			if prefix == "REQ" {
				index = "requirements-index.md"
				err = (&App{}).rewriteRequirementsIndex(dir)
			} else {
				err = rewriteDocIndex(dir, prefix, index, "文档索引")
			}
			if err != nil {
				t.Fatal(err)
			}
			body, err := os.ReadFile(filepath.Join(dir, index))
			if err != nil || !utf8.Valid(body) || strings.ContainsRune(string(body), utf8.RuneError) {
				t.Fatalf("index is not valid Unicode: %v %q", err, body)
			}
			for i, title := range titles {
				want := fmt.Sprintf("| %s-%03d | %s |", prefix, i+1, title)
				if !strings.Contains(string(body), want) {
					t.Fatalf("missing exact title %q in %s", want, body)
				}
			}
		})
	}
}
