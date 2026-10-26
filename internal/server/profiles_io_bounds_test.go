package server

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestIssue147RoutingPolicyRejectsLinksAndOversizedInput(t *testing.T) {
	for _, name := range []string{routingPolicyJSON, routingPolicyMD} {
		for _, oversized := range []bool{false, true} {
			t.Run(name+map[bool]string{false: "/symlink", true: "/oversized"}[oversized], func(t *testing.T) {
				a := testApp(t)
				data := `{"default":"precise"}`
				if name == routingPolicyMD {
					data = "```json\n" + data + "\n```\n"
				}
				if oversized {
					data += strings.Repeat(" ", (1<<20)+1)
					if err := os.WriteFile(filepath.Join(a.workPath, name), []byte(data), 0600); err != nil {
						t.Fatal(err)
					}
				} else {
					target := filepath.Join(t.TempDir(), "owned-policy")
					if err := os.WriteFile(target, []byte(data), 0600); err != nil {
						t.Fatal(err)
					}
					if err := os.Symlink(target, filepath.Join(a.workPath, name)); err != nil {
						t.Skip("symlinks unavailable:", err)
					}
				}
				if got := a.readRoutingPolicy(); got != nil {
					t.Fatalf("unsafe workspace routing input accepted: %+v", got)
				}
			})
		}
	}
}

func TestIssue148RejectedProfileSavePreservesMemoryAndDisk(t *testing.T) {
	a := testApp(t)
	if err := a.saveProfiles(); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(a.profilesPath)
	if err != nil {
		t.Fatal(err)
	}
	prior := a.profileState
	originalPath := a.profilesPath
	blocker := filepath.Join(t.TempDir(), "owned-blocker")
	if err := os.WriteFile(blocker, []byte("not a directory"), 0600); err != nil {
		t.Fatal(err)
	}
	a.profilesPath = filepath.Join(blocker, "profiles.json")
	candidate := map[string]any{"strategy": "manual", "activeProfile": "creative", "profiles": []any{}}
	requireStatus(t, request(a, "PUT", "/api/profiles", candidate), 500)
	afterMemory := a.profileState
	if !reflect.DeepEqual(afterMemory, prior) {
		t.Fatalf("rejected configuration became active: before=%+v after=%+v", prior, afterMemory)
	}
	a.profilesPath = originalPath
	after, err := os.ReadFile(originalPath)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("rejected update changed prior persisted data", err)
	}
	get := request(a, "GET", "/api/profiles", nil)
	var visible map[string]any
	if err := json.Unmarshal(get.Body.Bytes(), &visible); err != nil || visible["strategy"] != prior.Strategy || visible["activeProfile"] != prior.ActiveProfile {
		t.Fatal("GET exposed rejected profile", get.Body.String(), err)
	}
	requireStatus(t, request(a, "PUT", "/api/profiles", candidate), 200)
	if a.profileState.Strategy != "manual" || a.profileState.ActiveProfile != "creative" {
		t.Fatal("valid retry did not update profile")
	}
}

func TestIssue147PolicyPriorityFallbackAndExactLimit(t *testing.T) {
	a := testApp(t)
	jsonPath, mdPath := filepath.Join(a.workPath, routingPolicyJSON), filepath.Join(a.workPath, routingPolicyMD)
	md := "```json\n{\"default\":\"creative\"}\n```"
	if err := os.WriteFile(mdPath, []byte(md), 0600); err != nil {
		t.Fatal(err)
	}
	jsonPolicy := `{"default":"precise"}`
	jsonPolicy += strings.Repeat(" ", routingPolicyMaxBytes-len(jsonPolicy))
	if err := os.WriteFile(jsonPath, []byte(jsonPolicy), 0600); err != nil {
		t.Fatal(err)
	}
	if got := a.readRoutingPolicy(); got == nil || got.Default != "precise" {
		t.Fatal("valid exact-limit JSON lost priority", got)
	}
	if err := os.Remove(jsonPath); err != nil {
		t.Fatal(err)
	}
	if got := a.readRoutingPolicy(); got == nil || got.Default != "creative" {
		t.Fatal("ordinary Markdown fallback failed", got)
	}
	if err := os.Mkdir(jsonPath, 0700); err != nil {
		t.Fatal(err)
	}
	if got := a.readRoutingPolicy(); got == nil || got.Default != "creative" {
		t.Fatal("non-regular JSON did not use safe fallback", got)
	}
}
