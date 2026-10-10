package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMemoryTokensLegacyAndExternalEdit(t *testing.T) {
	a := testApp(t)
	dir := a.projectCacheDir()
	os.MkdirAll(dir, 0700)
	os.WriteFile(a.memoryPath(), []byte("旧记录🙂\n"), 0600)
	if got := a.writeMemory("新增记录"); !strings.Contains(got, "已写入") {
		t.Fatal(got)
	}
	s, _, err := loadMemoryTokenStore(dir)
	if err != nil || len(s.Records) != 2 || s.Records[0].Origin != "legacy-text" {
		t.Fatalf("%+v %v", s, err)
	}
	os.WriteFile(a.memoryPath(), []byte("替换记忆"), 0600)
	s, warning, err := loadMemoryTokenStore(dir)
	if err != nil || warning == "" || s.text() != "替换记忆" {
		t.Fatalf("%+v %s %v", s, warning, err)
	}
	if got := a.writeMemory("后续"); !strings.Contains(got, "已写入") {
		t.Fatal(got)
	}
	if strings.Contains(a.readMemory(), "旧记录") {
		t.Fatal("deleted memory resurrected")
	}
	// Missing compatibility projection keeps JSON as source.
	os.Remove(a.memoryPath())
	s, _, err = loadMemoryTokenStore(dir)
	if err != nil || !strings.Contains(s.text(), "后续") {
		t.Fatal(err)
	}
}
func TestMemoryTokensCorruptionAndLimits(t *testing.T) {
	a := testApp(t)
	a.writeMemory("正常")
	before, _ := os.ReadFile(a.memoryPath())
	if got := a.writeMemory(strings.Repeat("a", memoryTextLimit)); !strings.Contains(got, "超过") {
		t.Fatal(got)
	}
	after, _ := os.ReadFile(a.memoryPath())
	if string(after) != string(before) {
		t.Fatal("over limit mutated")
	}
	os.WriteFile(filepath.Join(a.projectCacheDir(), memoryTokenFile), []byte("bad"), 0600)
	if got := a.writeMemory("不得覆盖"); !strings.Contains(got, "失败") {
		t.Fatal(got)
	}
	s := newMemoryTokenStore()
	s.Tokenizer = "unregistered"
	if s.validate() == nil {
		t.Fatal("unsupported tokenizer")
	}
	s = newMemoryTokenStore()
	s.append("abcd", "test")
	delete(s.Vocabulary, s.Records[0].Tokens[0])
	if s.validate() == nil {
		t.Fatal("missing token accepted")
	}
}
func TestMemoryTokensStableLegacyAndUnicode(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "memory.md"), []byte("中文🙂\n不要删除\n"), 0600)
	first, _, err := loadMemoryTokenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	second, _, err := loadMemoryTokenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if first.Records[0].ID != second.Records[0].ID || first.Records[0].Created != second.Records[0].Created {
		t.Fatal("unstable legacy identity")
	}
	if len(first.Records[0].Tokens) != 4 || first.text() != "中文🙂\n不要删除\n" {
		t.Fatal(first)
	}
}
func TestMemoryTokensRejectsLinkedMemory(t *testing.T) {
	a := testApp(t)
	os.MkdirAll(a.projectCacheDir(), 0700)
	target := filepath.Join(t.TempDir(), "private")
	os.WriteFile(target, []byte("private"), 0600)
	if err := os.Symlink(target, a.memoryPath()); err != nil {
		t.Skip(err)
	}
	if got := a.writeMemory("safe"); !strings.Contains(got, "失败") {
		t.Fatal(got)
	}
}
