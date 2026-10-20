package server

import (
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// Isolated Aide page with disposable workspace/plugin data; no live user data.
func TestButtonFeedbackBrowser(t *testing.T) {
	if os.Getenv("AIDE_BUTTON_FEEDBACK_BROWSER") != "1" {
		t.Skip("opt-in browser regression")
	}
	out := os.Getenv("AIDE_EVIDENCE_DIR")
	if out == "" {
		t.Fatal("set AIDE_EVIDENCE_DIR")
	}
	a := testApp(t)
	for _, id := range []string{"feedback-fixture"} {
		requireStatus(t, request(a, "POST", "/api/plugins", map[string]any{"id": id, "name": "Upgrade fixture " + id, "version": "1.0.0", "code": "module.exports={apply(){}}"}), 201)
	}
	if err := os.MkdirAll(filepath.Join(a.dataPath, "auth"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(a.dataPath, "auth/access-token"), []byte(a.token), 0600); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(a.Handler())
	defer server.Close()
	script, err := filepath.Abs("../../scripts/browser-button-feedback.cjs")
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command("node", script)
	command.Env = append(os.Environ(), "AIDE_TEST_URL="+server.URL, "AIDE_TEST_DATA="+a.dataPath)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("button feedback browser: %v\n%s", err, output)
	}
	t.Log(string(output))
}
