package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestExecutionDiffLargeArtifactsUseBoundedMemory(t *testing.T) {
	for _, unchanged := range []bool{true, false} {
		t.Run(map[bool]string{true: "unchanged", false: "changed-binary"}[unchanged], func(t *testing.T) {
			work, artifact := t.TempDir(), t.TempDir()
			const size = 64 << 20
			f, err := os.Create(filepath.Join(work, "large.bin"))
			if err != nil {
				t.Fatal(err)
			}
			if err = f.Truncate(size); err != nil {
				t.Fatal(err)
			}
			if err = f.Close(); err != nil {
				t.Fatal(err)
			}
			f, err = os.Open(filepath.Join(work, "large.bin"))
			if err != nil {
				t.Fatal(err)
			}
			h := sha256.New()
			if _, err = io.Copy(h, f); err != nil {
				t.Fatal(err)
			}
			f.Close()
			digest := hex.EncodeToString(h.Sum(nil))
			base := map[string]string{}
			if unchanged {
				base["large.bin"] = digest
			}
			runtime.GC()
			var before, after runtime.MemStats
			runtime.ReadMemStats(&before)
			changes, unsupported, err := executionDiff(context.Background(), work, base, artifact)
			runtime.ReadMemStats(&after)
			if err != nil || len(changes) != 0 {
				t.Fatalf("changes=%v err=%v", changes, err)
			}
			allocated := after.TotalAlloc - before.TotalAlloc
			t.Logf("64 MiB artifact allocated %.2f MiB", float64(allocated)/(1<<20))
			if allocated > 8<<20 {
				t.Fatalf("whole-file allocation: %d bytes", allocated)
			}
			retained, err := os.Open(filepath.Join(artifact, "large.bin"))
			if unchanged {
				if !os.IsNotExist(err) || len(unsupported) != 0 {
					t.Fatal("unchanged artifact was copied", err, unsupported)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			defer retained.Close()
			h.Reset()
			n, err := io.Copy(h, retained)
			if err != nil || n != size || hex.EncodeToString(h.Sum(nil)) != digest || len(unsupported) != 1 {
				t.Fatal("binary artifact was truncated or altered", n, err, unsupported)
			}
		})
	}
}

func TestExecutionTextArtifactMatchesProposalTextContract(t *testing.T) {
	cases := []string{"", "plain text", "\x00", "bad\xff", "truncated\xe4\xb8", strings.Repeat("a", (64<<10)-1) + "中", strings.Repeat("a", (64<<10)-2) + "😀", strings.Repeat("a", (64<<10)-1) + "é", strings.Repeat("a", 64<<10) + "\x00"}
	for i, content := range cases {
		path := filepath.Join(t.TempDir(), "artifact")
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
		text, err := executionTextArtifact(context.Background(), path)
		if err != nil || text != (validateTextContent([]byte(content)) == nil) {
			t.Fatalf("case %d: text=%v err=%v", i, text, err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	path := filepath.Join(t.TempDir(), "cancelled")
	os.WriteFile(path, []byte("valid"), 0600)
	if _, err := executionTextArtifact(ctx, path); err != context.Canceled {
		t.Fatal(err)
	}
}
