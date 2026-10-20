package server

import (
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// Isolated Aide page with disposable workspace/plugin data; no live user data.
func TestLargeTransferBrowser(t *testing.T) {
	if os.Getenv("AIDE_LARGE_TRANSFER_BROWSER") != "1" {
		t.Skip("opt-in browser regression")
	}
	out := os.Getenv("AIDE_EVIDENCE_DIR")
	if out == "" {
		t.Fatal("set AIDE_EVIDENCE_DIR")
	}
	a := testApp(t)
	input := filepath.Join(t.TempDir(), "large-browser.bin")
	file, err := os.Create(input)
	if err != nil {
		t.Fatal(err)
	}
	if err = file.Truncate(67<<20 + 39); err != nil {
		t.Fatal(err)
	}
	_, _ = file.WriteAt([]byte("large-browser-transfer"), 0)
	_, _ = file.WriteAt([]byte("last-bytes"), (67<<20)+29)
	file.Close()
	for _, name := range []string{"copies", "moved"} {
		if err = os.Mkdir(filepath.Join(a.workPath, name), 0755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(a.dataPath, "auth"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(a.dataPath, "auth/access-token"), []byte(a.token), 0600); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(a.Handler())
	defer server.Close()
	script, err := filepath.Abs("../../scripts/browser-large-transfers.cjs")
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command("node", script)
	command.Env = append(os.Environ(), "AIDE_TEST_URL="+server.URL, "AIDE_TEST_DATA="+a.dataPath, "AIDE_TRANSFER_INPUT="+input)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("large transfer browser: %v\n%s", err, output)
	}
	t.Log(string(output))
	if testFileDigest(t, input) != testFileDigest(t, filepath.Join(a.workPath, "large-browser.bin")) {
		t.Fatal("browser upload checksum")
	}
	if testFileDigest(t, input) != testFileDigest(t, filepath.Join(a.workPath, "moved/large-browser.bin")) {
		t.Fatal("browser copy/move checksum")
	}
	if _, err = os.Stat(filepath.Join(a.workPath, "copies/large-browser.bin")); !os.IsNotExist(err) {
		t.Fatal("move did not remove copied source")
	}

}
