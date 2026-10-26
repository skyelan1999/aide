package server

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestIssue103LargePassphraseNeverBlocksBeforeChildStart(t *testing.T) {
	for _, mode := range []string{"read", "no-read", "never-start", "canceled-start"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestIssue103AskpassHelperProcess$")
			cmd.Env = append(os.Environ(), "AIDE_ISSUE103_MODE="+mode)
			out, err := cmd.CombinedOutput()
			if ctx.Err() != nil {
				t.Fatal("large passphrase blocked attach/start/cleanup before the child could finish")
			}
			if err != nil {
				t.Fatalf("helper failed: %v %s", err, out)
			}
		})
	}
}

func TestIssue103AskpassHelperProcess(t *testing.T) {
	mode := os.Getenv("AIDE_ISSUE103_MODE")
	if mode == "" {
		t.Skip("subprocess fixture only")
	}
	passphrase := strings.Repeat("s", 256<<10)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	command := "exit 0"
	if mode == "read" {
		command = "wc -c <&3"
	}
	if mode == "canceled-start" {
		cancel()
	}
	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	cleanup, err := attachPassphraseAskpass(cmd, passphrase)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	askPath := ""
	for _, entry := range cmd.Env {
		if strings.HasPrefix(entry, "SSH_ASKPASS=") {
			askPath = strings.TrimPrefix(entry, "SSH_ASKPASS=")
		}
		if strings.Contains(entry, passphrase) {
			t.Fatal("passphrase leaked into environment")
		}
	}
	for _, arg := range cmd.Args {
		if strings.Contains(arg, passphrase) {
			t.Fatal("passphrase leaked into arguments")
		}
	}
	helper, err := os.ReadFile(askPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(helper), passphrase) {
		t.Fatal("passphrase persisted in helper script")
	}
	defer func() {
		cleanup()
		if _, err := os.Stat(filepath.Dir(askPath)); !os.IsNotExist(err) {
			t.Error("askpass helper directory retained")
		}
		if _, err := cmd.ExtraFiles[0].Stat(); err == nil {
			t.Error("parent passphrase read descriptor retained")
		}
	}()
	if mode == "never-start" {
		cleanup()
		return
	}
	out, err := cmd.CombinedOutput()
	if mode == "canceled-start" {
		if err == nil {
			t.Fatal("canceled command started")
		}
		cleanup()
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	if mode == "read" {
		size, err := strconv.Atoi(strings.TrimSpace(string(out)))
		if err != nil || size != len(passphrase) {
			t.Fatal("child did not receive complete passphrase length")
		}
	}
	cleanup()
}
