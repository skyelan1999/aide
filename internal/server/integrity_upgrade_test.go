//go:build linux

package server

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func integrityUpgradeFixture(t *testing.T) (string, string, string, []byte, *integrityUpgrade) {
	t.Helper()
	if os.Geteuid() != 0 {
		t.Skip("image checksum fixtures require root-owned build files (run isolated Docker test)")
	}
	data := t.TempDir()
	if err := EnsureDirs(data); err != nil {
		t.Fatal(err)
	}
	digest, err := currentBinarySHA256()
	if err != nil {
		t.Fatal(err)
	}
	old := baselineManifest{Version: "0.1.0", Commit: strings.Repeat("1", 40), BinarySHA256: strings.Repeat("0", 64), BuiltAt: "old"}
	if err := atomicJSON(baselinePath(data), old); err != nil {
		t.Fatal(err)
	}
	previous, _ := os.ReadFile(baselinePath(data))
	checksum := filepath.Join(t.TempDir(), "aide.sha256")
	if err := os.WriteFile(checksum, []byte(digest+"  /usr/local/bin/aide\n"), 0444); err != nil {
		t.Fatal(err)
	}
	u := prepareIntegrityUpgrade(data, checksum, "0.2.0", strings.Repeat("2", 40), digest, VerifyIntegrity(data))
	if u == nil {
		t.Fatal("verified changed build must prepare a candidate")
	}
	return data, checksum, digest, previous, u
}

func TestIntegrityUpgradeReadinessAndDataPreservation(t *testing.T) {
	data, _, _, previous, u := integrityUpgradeFixture(t)
	sentinel := filepath.Join(data, "sessions", "existing-user-data")
	want := []byte("existing project, settings and session bytes")
	if err := os.WriteFile(sentinel, want, 0600); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(baselinePath(data))
	if !bytes.Equal(before, previous) {
		t.Fatal("preparation modified baseline before readiness")
	}
	a := &App{dataPath: data, integrityRep: VerifyIntegrity(data), integrityUpgrade: u}
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/healthz", nil))
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"integrity":"ok"`) {
		t.Fatalf("health: %d %s", w.Code, w.Body.String())
	}
	var next baselineManifest
	b, _ := os.ReadFile(baselinePath(data))
	json.Unmarshal(b, &next)
	if next.Version != "0.2.0" || next.BuiltAt == "old" {
		t.Fatalf("baseline not updated: %+v", next)
	}
	backups, _ := filepath.Glob(filepath.Join(IntegrityDir(data), "baseline-before-*.json"))
	if len(backups) != 1 {
		t.Fatalf("backups: %v", backups)
	}
	backup, _ := os.ReadFile(backups[0])
	actual, _ := os.ReadFile(sentinel)
	if !bytes.Equal(backup, previous) || !bytes.Equal(actual, want) {
		t.Fatal("backup or user data changed")
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); a.confirmImageIntegrityUpgrade() }()
	}
	wg.Wait()
	after, _ := os.ReadFile(baselinePath(data))
	if !bytes.Equal(b, after) {
		t.Fatal("repeated readiness rewrote baseline")
	}
}

func TestIntegrityUpgradeRejectsUnverifiedTransitions(t *testing.T) {
	for _, kind := range []string{"same-identity", "wrong-digest", "missing-checksum", "writable-checksum", "symlink-checksum", "runtime-owned-checksum", "unknown-build", "corrupt-data", "startup-errors", "malformed-baseline"} {
		t.Run(kind, func(t *testing.T) {
			data, checksum, digest, _, _ := integrityUpgradeFixture(t)
			version, commit := "0.2.0", strings.Repeat("2", 40)
			rep := VerifyIntegrity(data)
			switch kind {
			case "same-identity":
				version, commit = "0.1.0", strings.Repeat("1", 40)
			case "wrong-digest":
				digest = strings.Repeat("f", 64)
			case "missing-checksum":
				os.Remove(checksum)
			case "writable-checksum":
				os.Chmod(checksum, 0666)
			case "symlink-checksum":
				p := checksum + ".link"
				os.Symlink(checksum, p)
				checksum = p
			case "runtime-owned-checksum":
				os.Chown(checksum, 1000, 1000)
			case "unknown-build":
				commit = "unknown"
			case "corrupt-data":
				os.WriteFile(SettingsPath(data), []byte("{bad"), 0600)
				rep = VerifyIntegrity(data)
			case "startup-errors":
				rep.Errors = []string{"failed initialization"}
			case "malformed-baseline":
				os.WriteFile(baselinePath(data), []byte("{bad"), 0600)
			}
			before, _ := os.ReadFile(baselinePath(data))
			if prepareIntegrityUpgrade(data, checksum, version, commit, digest, rep) != nil {
				t.Fatal("unverified change was accepted")
			}
			after, _ := os.ReadFile(baselinePath(data))
			if !bytes.Equal(before, after) {
				t.Fatal("rejected transition changed baseline")
			}
		})
	}
}

func TestIntegrityUpgradeRechecksAndPreservesOnFailure(t *testing.T) {
	for _, kind := range []string{"baseline-changed", "checksum-changed", "data-corrupted", "backup-conflict"} {
		t.Run(kind, func(t *testing.T) {
			data, checksum, _, previous, u := integrityUpgradeFixture(t)
			switch kind {
			case "baseline-changed":
				os.WriteFile(baselinePath(data), append(previous, ' '), 0600)
			case "checksum-changed":
				os.Chmod(checksum, 0644)
				os.WriteFile(checksum, []byte(strings.Repeat("f", 64)+"  /usr/local/bin/aide"), 0444)
			case "data-corrupted":
				os.WriteFile(SettingsPath(data), []byte("{bad"), 0600)
			case "backup-conflict":
				sum := sha256.Sum256(previous)
				os.WriteFile(filepath.Join(IntegrityDir(data), "baseline-before-"+hex.EncodeToString(sum[:])+".json"), []byte("conflict"), 0600)
			}
			before, _ := os.ReadFile(baselinePath(data))
			if u.commit(data) == nil {
				t.Fatal("expected refusal")
			}
			after, _ := os.ReadFile(baselinePath(data))
			if !bytes.Equal(before, after) {
				t.Fatal("failed update replaced old baseline")
			}
		})
	}
}

func TestIntegrityUpgradeRollbackAndSubsequentTampering(t *testing.T) {
	data, checksum, digest, _, u := integrityUpgradeFixture(t)
	// A verified older build is allowed to restore its own baseline on rollback.
	old := u.next
	old.Version = "0.3.0"
	old.BinarySHA256 = strings.Repeat("3", 64)
	atomicJSON(baselinePath(data), old)
	u = prepareIntegrityUpgrade(data, checksum, "0.2.0", strings.Repeat("2", 40), digest, VerifyIntegrity(data))
	if u == nil {
		t.Fatal("verified rollback rejected")
	}
	if err := u.commit(data); err != nil {
		t.Fatal(err)
	}
	a := &App{dataPath: data, integrityRep: VerifyIntegrity(data)}
	// No candidate remains after confirmation; later unexpected changes are not blessed.
	b, _ := os.ReadFile(baselinePath(data))
	json.Unmarshal(b, &old)
	old.BinarySHA256 = strings.Repeat("e", 64)
	atomicJSON(baselinePath(data), old)
	a.confirmImageIntegrityUpgrade()
	if VerifyIntegrity(data).Status != IntegrityDegraded {
		t.Fatal("later tampering was hidden")
	}
}
