package server

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 旧上限 256 KiB 已提升到 64 MiB：这里用 300 KiB 文本验证“超过旧限制但低于新上限”可正常读写。
func TestTextFileAboveOld256KiBNowOK(t *testing.T) {
	a := testApp(t)
	content := strings.Repeat("abcdefgh\n", 30000) // ~300 KB
	if len(content) <= 256<<10 {
		t.Fatalf("fixture must exceed old 256 KiB, got %d", len(content))
	}
	if len(content) > maxFile {
		t.Fatalf("fixture must stay under new cap, got %d", len(content))
	}
	body := map[string]string{"path": "big.txt", "content": content, "hash": ""}
	requireStatus(t, request(a, "PUT", "/api/file", body), 200)
	w := request(a, "GET", "/api/file?path=big.txt", nil)
	requireStatus(t, w, 200)
	var resp struct {
		Content string `json:"content"`
		Hash    string `json:"hash"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Content != content {
		t.Fatalf("round-trip mismatch: len got %d want %d", len(resp.Content), len(content))
	}
	if resp.Hash != hash([]byte(content)) {
		t.Fatal("hash mismatch after big write")
	}
}

// 超过新上限必须明确 400 错误，不静默截断、不 OOM。
func TestTextFileOverNewCapRejected(t *testing.T) {
	a := testApp(t)
	// 单字节超过 maxFile：触发 writeFile 的大小兜底（body 上限 maxFile+1MiB 仍容纳此请求）
	big := strings.Repeat("a", maxFile+1)
	body := map[string]string{"path": "huge.txt", "content": big, "hash": ""}
	w := request(a, "PUT", "/api/file", body)
	requireStatus(t, w, 400)
	if !strings.Contains(w.Body.String(), "MiB") {
		t.Fatalf("error should state MiB cap, got: %s", w.Body.String())
	}
	// 不应落盘
	if _, err := os.Stat(filepath.Join(a.workPath, "huge.txt")); !os.IsNotExist(err) {
		t.Fatal("oversized file must not be written")
	}
}

// validateTextContent 边界：恰好 maxFile 通过，maxFile+1 拒绝；NUL/二进制拒绝。
func TestValidateTextContentBoundary(t *testing.T) {
	atCap := bytes.Repeat([]byte("a"), maxFile) // 全 'a'：合法 UTF-8、无 NUL
	if err := validateTextContent(atCap); err != nil {
		t.Fatalf("at cap should pass: %v", err)
	}
	overCap := bytes.Repeat([]byte("a"), maxFile+1)
	if err := validateTextContent(overCap); err == nil {
		t.Fatal("over cap should fail")
	}
	if err := validateTextContent([]byte("hello\x00world")); err == nil {
		t.Fatal("NUL byte must be rejected")
	}
	if err := validateTextContent([]byte{0xff, 0xfe, 0xfd}); err == nil {
		t.Fatal("invalid UTF-8 must be rejected")
	}
	if err := validateTextContent([]byte("中文 UTF-8 正常")); err != nil {
		t.Fatalf("valid utf8 should pass: %v", err)
	}
}

// readTextLines 行分段：offset/limit 只回窗口，并报告总行数；整文件 NUL 检测。
func TestReadTextLinesPaging(t *testing.T) {
	a := testApp(t)
	var sb strings.Builder
	for i := 0; i < 10; i++ {
		sb.WriteString("line-" + itoa(i) + "\n")
	}
	if err := a.workspace.WriteFile("paged.txt", []byte(sb.String()), 0644); err != nil {
		t.Fatal(err)
	}
	// offset=2(第3行起), limit=3 → 第3,4,5行
	got, total, err := readTextLines(a.workspace, "paged.txt", 2, 3)
	if err != nil {
		t.Fatal(err)
	}
	if total != 10 {
		t.Fatalf("total lines = %d want 10", total)
	}
	want := "line-2\nline-3\nline-4\n"
	if string(got) != want {
		t.Fatalf("window = %q want %q", got, want)
	}
	// limit=0 表示从 offset 到 EOF
	got, _, err = readTextLines(a.workspace, "paged.txt", 8, 0)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "line-8\nline-9\n" {
		t.Fatalf("tail window = %q", got)
	}
	// 路径安全不回归
	if _, _, err := readTextLines(a.workspace, "../etc", 0, 0); err == nil {
		t.Fatal("unsafe path must be rejected")
	}
}

// readTextLines 对整文件 NUL 仍拒绝（即使 NUL 不在返回窗口内）。
func TestReadTextLinesRejectsBinaryAnywhere(t *testing.T) {
	a := testApp(t)
	content := "safe line one\nsafe line two\n\x00binary\n"
	if err := a.workspace.WriteFile("bin.txt", []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	// 只取前 2 行（不含 NUL 行），仍应因整文件含 NUL 被拒
	if _, _, err := readTextLines(a.workspace, "bin.txt", 0, 2); err == nil {
		t.Fatal("NUL outside window must still be rejected")
	}
}

// GET /api/file 字节分段窗口：offset/limit 返回 partial 载荷。
func TestHTTPFileRangeWindow(t *testing.T) {
	a := testApp(t)
	content := "0123456789ABCDEFGHIJ" // 20 bytes
	if err := a.workspace.WriteFile("win.txt", []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	w := request(a, "GET", "/api/file?path=win.txt&offset=5&limit=3", nil)
	requireStatus(t, w, 200)
	var resp struct {
		Content string `json:"content"`
		Offset  int64  `json:"offset"`
		Limit   int    `json:"limit"`
		Size    int64  `json:"size"`
		Partial bool   `json:"partial"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Content != "567" || resp.Offset != 5 || resp.Size != 20 || !resp.Partial {
		t.Fatalf("range resp = %+v", resp)
	}
	// 无 offset：保持原有整文件 + hash 行为
	w = request(a, "GET", "/api/file?path=win.txt", nil)
	requireStatus(t, w, 200)
	var full struct {
		Content string `json:"content"`
		Hash    string `json:"hash"`
		Partial bool   `json:"partial"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &full); err != nil {
		t.Fatal(err)
	}
	if full.Content != content || full.Hash != hash([]byte(content)) || full.Partial {
		t.Fatalf("full resp = %+v", full)
	}
}

// 二进制文件经文本端点仍 400，路径安全不回归（既有行为）。
func TestTextRejectBinaryAndUnsafePath(t *testing.T) {
	a := testApp(t)
	if err := a.workspace.WriteFile("nul.bin", []byte("a\x00b"), 0644); err != nil {
		t.Fatal(err)
	}
	requireStatus(t, request(a, "GET", "/api/file?path=nul.bin", nil), 400)
	for _, p := range []string{"../etc", ".git/config", ".env"} {
		requireStatus(t, request(a, "GET", "/api/file?path="+p, nil), 400)
	}
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b [20]byte
	pos := len(b)
	for i > 0 {
		pos--
		b[pos] = byte('0' + i%10)
		i /= 10
	}
	return string(b[pos:])
}
