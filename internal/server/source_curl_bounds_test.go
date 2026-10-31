package server

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestIssue76CurlOutputProcess(t *testing.T) {
	mode := os.Getenv("AIDE_CURL_BOUNDS_HELPER")
	if mode == "" {
		t.Skip("subprocess output fixture")
	}
	if mode == "stderr" {
		fmt.Fprintln(os.Stderr, "owned-credential-canary")
		os.Exit(9)
	}
	amount := int64(12 << 20)
	if mode == "file" {
		amount = maxExternalTextFile + (8 << 20)
	}
	buf := []byte(strings.Repeat("x", 32<<10))
	for amount > 0 {
		n := min(int64(len(buf)), amount)
		if _, err := os.Stdout.Write(buf[:n]); err != nil {
			os.Exit(4)
		}
		amount -= n
	}
	if err := os.WriteFile(os.Getenv("AIDE_CURL_COMPLETED"), []byte("fully produced"), 0600); err != nil {
		os.Exit(5)
	}
	os.Exit(0)
}

func TestIssue76CurlOutputIsBoundedDuringCapture(t *testing.T) {
	for _, mode := range []string{"listing", "file", "stderr"} {
		t.Run(mode, func(t *testing.T) {
			a := testApp(t)
			bin, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			marker := filepath.Join(t.TempDir(), "producer-completed")
			stub := filepath.Join(t.TempDir(), "owned-curl")
			script := "#!/bin/sh\nexec env AIDE_CURL_BOUNDS_HELPER=" + shellQuote(mode) + " AIDE_CURL_COMPLETED=" + shellQuote(marker) + " " + shellQuote(bin) + " -test.run='^TestIssue76CurlOutputProcess$'\n"
			if err := os.WriteFile(stub, []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			a.curlBin = stub
			src := Source{ID: "owned-source", Type: "ftp", Config: SourceConfig{URL: "ftp://owned.invalid"}}
			if mode == "file" {
				_, err = a.curlReadSource(src, "readme.txt")
			} else {
				_, err = a.curlListSource(src, ".")
			}
			if err == nil {
				t.Fatal("oversized or failed source was accepted")
			}
			if strings.Contains(err.Error(), "owned-credential-canary") {
				t.Fatal("curl stderr leaked into diagnostic response")
			}
			if mode != "stderr" {
				if _, e := os.Stat(marker); !os.IsNotExist(e) {
					t.Fatalf("producer finished all bytes before capture was bounded: %v", e)
				}
			}
		})
	}
}
