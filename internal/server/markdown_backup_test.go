package server

import (
	"archive/zip"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestMarkdownBackupCompleteAndVerified(t *testing.T) {
	a := testApp(t)
	dir := a.markdownHistoryDir("workspace:" + a.wsID())
	for i := 0; i < 201; i++ {
		if err := a.captureMarkdown(dir, "page.md", []byte(fmt.Sprintf("# Version %d\n![x](x.svg)\n![missing](gone.svg)", i)), true, func(p string) ([]byte, error) {
			if p == "x.svg" {
				return []byte("<svg>shared</svg>"), nil
			}
			return nil, os.ErrNotExist
		}); err != nil {
			t.Fatal(err)
		}
	}
	data, err := a.markdownBackupData("", "page.md", true)
	if err != nil {
		t.Fatal(err)
	}
	if data["versions"] != 201 || data["objects"] != 202 || data["unavailableAssets"] != 201 || data["integrityChecked"] != true {
		t.Fatalf("summary: %v", data)
	}
	raw, err := base64.StdEncoding.DecodeString(data["base64"].(string))
	if err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		t.Fatal(err)
	}
	if len(zr.File) != 203 {
		t.Fatalf("archive entries: %d", len(zr.File))
	}
	for _, f := range zr.File {
		r, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		b, err := io.ReadAll(r)
		r.Close()
		if err != nil {
			t.Fatal(err)
		}
		if f.Name == "manifest.json" {
			var manifest struct {
				Format  string               `json:"format"`
				History markdownHistoryIndex `json:"history"`
			}
			if err := json.Unmarshal(b, &manifest); err != nil {
				t.Fatal(err)
			}
			if manifest.Format != "aide-markdown-history" || len(manifest.History.Versions) != 201 {
				t.Fatal("incomplete manifest")
			}
		} else if filepath.Base(f.Name) != hash(b) {
			t.Fatalf("object mismatch: %s", f.Name)
		}
	}
	// Deleted live files do not remove archived history or prevent backups.
	requireStatus(t, request(a, "GET", "/api/file/history/backup?path=page.md&archive=1", nil), 200)
	requireStatus(t, request(a, "GET", "/api/file/history/backup?path=../page.md", nil), 400)
	requireStatus(t, request(a, "GET", "/api/file/history/backup?path=page.md&workspaceId=wrong", nil), 409)
	if _, err := a.markdownBackupData("unknown", "page.md", true); err == nil {
		t.Fatal("unknown source accepted")
	}
	if _, err := a.markdownBackupData("", "other.md", true); err != nil {
		t.Fatal(err)
	}
	index, _ := loadMarkdownIndex(dir, "page.md")
	if err := os.WriteFile(filepath.Join(dir, "objects", index.Versions[0].Content), []byte("damaged"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := a.markdownBackupData("", "page.md", true); err == nil {
		t.Fatal("corrupt backup accepted")
	}
}

func TestMarkdownBackupBudget(t *testing.T) {
	a := testApp(t)
	dir := a.markdownHistoryDir("workspace:" + a.wsID())
	digest, err := storeHistoryObject(dir, []byte("small"))
	if err != nil {
		t.Fatal(err)
	}
	index := markdownHistoryIndex{Path: "page.md", Versions: []markdownRevision{{ID: "000001", Content: digest}}}
	if err := atomicJSON(historyIndexPath(dir, "page.md"), index); err != nil {
		t.Fatal(err)
	}
	if err := os.Truncate(filepath.Join(dir, "objects", digest), markdownBackupLimit+1); err != nil {
		t.Fatal(err)
	}
	if _, err := a.markdownBackupData("", "page.md", true); err == nil {
		t.Fatal("oversize accepted")
	}
	stats, err := a.markdownBackupData("", "page.md", false)
	if err != nil {
		t.Fatal(err)
	}
	if stats["integrityChecked"] != false || stats["objectBytes"] != int64(markdownBackupLimit+1) {
		t.Fatalf("stats: %v", stats)
	}
}
