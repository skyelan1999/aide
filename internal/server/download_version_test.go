package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestIssue78MutableFileDoesNotOfferStrongRangeValidator(t *testing.T) {
	a := testApp(t)
	original := []byte("AAAACCCC")
	full := filepath.Join(a.workPath, "version.bin")
	if err := os.WriteFile(full, original, 0600); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(full)
	if err != nil {
		t.Fatal(err)
	}
	endpoint := "/api/file/stream?root=workspace&path=version.bin"
	head := request(a, "HEAD", endpoint, nil)
	requireStatus(t, head, 200)
	tag := head.Header().Get("ETag")
	if !strings.HasPrefix(tag, "W/") {
		t.Errorf("mutable metadata ETag claims strong identity: %s", tag)
	}
	if err := os.WriteFile(full, []byte("BBBBEEEE"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(full, st.ModTime(), st.ModTime()); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("GET", endpoint, nil)
	r.Header.Set("Authorization", "Bearer "+a.token)
	r.Header.Set("Range", "bytes=4-7")
	r.Header.Set("If-Match", tag)
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, r)
	if w.Code != 412 {
		t.Fatalf("accepted a conditional range from a different representation: %d %q", w.Code, w.Body.String())
	}
}

func TestIssue78RawDownloadSnapshotPersistsAndStaysImmutable(t *testing.T) {
	a := testApp(t)
	original := bytes.Repeat([]byte("old-version-"), 8000)
	name := "snapshot.bin"
	full := filepath.Join(a.workPath, name)
	if err := os.WriteFile(full, original, 0600); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(full)
	if err != nil {
		t.Fatal(err)
	}
	w := request(a, "POST", "/api/file/transfers", map[string]any{"operation": "download", "source": "workspace", "paths": []string{name}, "rawDownload": true})
	requireStatus(t, w, 201)
	var receipt struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &receipt); err != nil {
		t.Fatal(err)
	}
	m := a.transfers()
	m.mu.Lock()
	job := m.jobs[receipt.ID]
	m.mu.Unlock()
	if err := m.wait(context.Background(), job); err != nil {
		t.Fatal(err)
	}
	endpoint := "/api/file/stream?job=" + receipt.ID
	head := request(a, "HEAD", endpoint, nil)
	requireStatus(t, head, 200)
	tag := head.Header().Get("ETag")
	if strings.HasPrefix(tag, "W/") || tag == "" {
		t.Fatal("immutable snapshot needs a strong validator")
	}
	getRange := func(first, last string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", endpoint, nil)
		r.Header.Set("Authorization", "Bearer "+a.token)
		r.Header.Set("Range", "bytes="+first+"-"+last)
		r.Header.Set("If-Match", tag)
		out := httptest.NewRecorder()
		a.Handler().ServeHTTP(out, r)
		return out
	}
	first := getRange("0", "11")
	requireStatus(t, first, 206)
	if first.Body.String() != string(original[:12]) {
		t.Fatalf("raw snapshot returned archive/other bytes: %q", first.Body.String())
	}
	replacement := bytes.Repeat([]byte("new-version-"), 8000)
	if err := os.WriteFile(full, replacement, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(full, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	testTransferRestart(a)
	second := getRange("12", "23")
	requireStatus(t, second, 206)
	if second.Body.String() != string(original[12:24]) || second.Header().Get("ETag") != tag {
		t.Fatal("snapshot mixed source versions after replacement/restart")
	}
	if !strings.Contains(second.Header().Get("Content-Disposition"), name) {
		t.Fatal("snapshot changed the original download filename")
	}
	requireStatus(t, request(a, "DELETE", "/api/file/transfers/"+receipt.ID, nil), 200)
	current, err := os.ReadFile(full)
	if err != nil || !bytes.Equal(current, replacement) {
		t.Fatal("snapshot removal changed source", err)
	}
	requireStatus(t, request(a, "GET", endpoint, nil), 404)
}

// Simulate a crash after exposing a complete in-memory job but before its
// terminal status reaches disk. Repreparing must get a new durable identity.
func TestIssue78RebuiltSnapshotChangesValidator(t *testing.T) {
	a := testApp(t)
	full := filepath.Join(a.workPath, "rebuild.bin")
	if err := os.WriteFile(full, []byte("AAAA"), 0600); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(full)
	if err != nil {
		t.Fatal(err)
	}
	m := a.transfers()
	j, err := m.create(context.Background(), transferCreate{Operation: "download", Paths: []string{"rebuild.bin"}, RawDownload: true})
	if err != nil {
		t.Fatal(err)
	}
	originalParts := append([]*transferPart(nil), j.Parts...)
	if err := m.start(j); err != nil {
		t.Fatal(err)
	}
	testTransferWait(t, m, j)
	endpoint := "/api/file/stream?job=" + j.ID
	old := request(a, "HEAD", endpoint, nil)
	requireStatus(t, old, 200)
	snapshot := filepath.Join(m.jobDir(j), "download.file")
	oldInfo, err := os.Stat(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	diskPath := filepath.Join(m.jobDir(j), "job.json")
	data, err := os.ReadFile(diskPath)
	if err != nil {
		t.Fatal(err)
	}
	var saved map[string]any
	if err := json.Unmarshal(data, &saved); err != nil {
		t.Fatal(err)
	}
	saved["status"] = "running"
	saved["parts"] = originalParts
	encoded, err := json.Marshal(saved)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(diskPath, encoded, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte("BBBB"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(full, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	testTransferRestart(a)
	m = a.transfers()
	j = m.jobs[j.ID]
	if err := m.start(j); err != nil {
		t.Fatal(err)
	}
	testTransferWait(t, m, j)
	if err := os.Chtimes(snapshot, oldInfo.ModTime(), oldInfo.ModTime()); err != nil {
		t.Fatal(err)
	}
	current := request(a, "HEAD", endpoint, nil)
	requireStatus(t, current, 200)
	if current.Header().Get("ETag") == old.Header().Get("ETag") {
		t.Fatal("rebuilt representation reused its strong validator")
	}
	r := httptest.NewRequest("GET", endpoint, nil)
	r.Header.Set("Authorization", "Bearer "+a.token)
	r.Header.Set("Range", "bytes=0-3")
	r.Header.Set("If-Match", old.Header().Get("ETag"))
	out := httptest.NewRecorder()
	a.Handler().ServeHTTP(out, r)
	requireStatus(t, out, 412)
}

func TestIssue78SnapshotRejectsInvalidSelection(t *testing.T) {
	a := testApp(t)
	if err := os.Mkdir(filepath.Join(a.workPath, "directory"), 0700); err != nil {
		t.Fatal(err)
	}
	for _, in := range []transferCreate{
		{Operation: "download", Paths: []string{"directory"}, RawDownload: true},
		{Operation: "download", Paths: []string{"a", "b"}, RawDownload: true},
		{Operation: "upload", Name: "file", RawDownload: true},
	} {
		if _, err := a.transfers().create(context.Background(), in); err == nil {
			t.Fatalf("invalid raw snapshot selection accepted: %+v", in)
		}
	}
}

func TestIssue78SnapshotExceedsPreviewByteLimit(t *testing.T) {
	a := testApp(t)
	f, err := os.Create(filepath.Join(a.workPath, "large.bin"))
	if err != nil {
		t.Fatal(err)
	}
	const offset = int64(65 << 20)
	if err = f.Truncate(offset + 4); err != nil {
		t.Fatal(err)
	}
	if _, err = f.WriteAt([]byte("tail"), offset); err != nil {
		t.Fatal(err)
	}
	if err = f.Close(); err != nil {
		t.Fatal(err)
	}
	m := a.transfers()
	j, err := m.create(context.Background(), transferCreate{Operation: "download", Paths: []string{"large.bin"}, RawDownload: true})
	if err != nil {
		t.Fatal(err)
	}
	if err = m.start(j); err != nil {
		t.Fatal(err)
	}
	testTransferWait(t, m, j)
	r := httptest.NewRequest("GET", "/api/file/stream?job="+j.ID, nil)
	r.Header.Set("Authorization", "Bearer "+a.token)
	r.Header.Set("Range", "bytes=68157440-")
	out := httptest.NewRecorder()
	a.Handler().ServeHTTP(out, r)
	requireStatus(t, out, 206)
	if out.Body.String() != "tail" {
		t.Fatal("raw snapshot applied preview byte limit")
	}
}

func TestIssue78SFTPFileSnapshot(t *testing.T) {
	for _, source := range []bool{false, true} {
		name := "workspace"
		if source {
			name = "independent-source"
		}
		t.Run(name, func(t *testing.T) {
			a, src, remote := issue79SFTPFixture(t, source)
			original := []byte("owned SSH snapshot version")
			full := filepath.Join(remote, "device.bin")
			if err := os.WriteFile(full, original, 0600); err != nil {
				t.Fatal(err)
			}
			id := "workspace"
			if source {
				id = src.ID
			}
			m := a.transfers()
			j, err := m.create(context.Background(), transferCreate{Operation: "download", Source: id, Paths: []string{"device.bin"}, RawDownload: true})
			if err != nil {
				t.Fatal(err)
			}
			if err := m.start(j); err != nil {
				t.Fatal(err)
			}
			testTransferWait(t, m, j)
			if err := os.WriteFile(full, bytes.Repeat([]byte("x"), len(original)), 0600); err != nil {
				t.Fatal(err)
			}
			w := request(a, "GET", "/api/file/stream?job="+j.ID, nil)
			requireStatus(t, w, 200)
			if !bytes.Equal(w.Body.Bytes(), original) {
				t.Fatal("SFTP snapshot followed changed source content")
			}
		})
	}
}
