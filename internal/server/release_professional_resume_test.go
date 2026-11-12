package server

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Exercise the exact market package through the installed handler, never a
// rewritten adapter or a paid model. All career data here is fictional.
func releasePublicProfessionalResume(t *testing.T, a *App) []string {
	const plugin = "professional-resume"
	guide := releasePublicCall(t, a, plugin, "pr_guide", map[string]any{})
	if !strings.Contains(guide, "resultSchema") {
		t.Fatal("missing installed guide", guide)
	}
	marker := "FICTIONAL-RELEASE-RESUME-PRIVATE-MARKER"
	packet := map[string]any{"protocol": 1, "databaseId": "release-db", "profileId": "release-profile", "targetId": "release-target", "candidate": map[string]any{"headline": "Synthetic engineer"}, "target": map[string]any{"company": "Fictional release company", "role": "Synthetic engineer", "jd": "Synthetic fixture only"}, "evidence": []any{map[string]any{"id": "e1", "kind": "project", "title": "Fictional project", "description": marker, "contribution": "Synthetic validation", "verified": true}}}
	if _, err := a.callPluginToolContext(context.Background(), plugin, "pr_tasks", map[string]any{"action": "create", "packet": packet}); err == nil {
		t.Fatal("task accepted without consent")
	}
	created := releasePublicObject(t, releasePublicCall(t, a, plugin, "pr_tasks", map[string]any{"action": "create", "consent": true, "packet": packet}))
	id, _ := created["id"].(string)
	packetHash, _ := created["packetHash"].(string)
	if len(id) != 48 || len(packetHash) != 64 {
		t.Fatal(created)
	}
	got := releasePublicCall(t, a, plugin, "pr_tasks", map[string]any{"action": "get", "id": id})
	if !strings.Contains(got, marker) || !strings.Contains(got, packetHash) {
		t.Fatal("admitted packet not recovered", got)
	}
	encrypted := filepath.Join(a.pluginsPath, ".professional-resume-state")
	found := false
	if err := filepath.WalkDir(encrypted, func(p string, d os.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if !d.IsDir() && d.Name() == id {
			b, e := os.ReadFile(p)
			if e != nil {
				return e
			}
			if bytes.Contains(b, []byte(marker)) {
				t.Error("task plaintext persisted")
			}
			found = true
		}
		return nil
	}); err != nil || !found {
		t.Fatal("encrypted task cache missing", err)
	}
	result := map[string]any{"protocol": 1, "profileId": "release-profile", "targetId": "release-target", "resume": map[string]any{"headline": "Synthetic engineer", "summary": "Fictional fixture only", "sections": []any{map[string]any{"title": "Synthetic evidence", "items": []any{map[string]any{"text": "Synthetic validation", "evidenceIds": []string{"e1"}, "status": "supported"}}}}}, "research": map[string]any{"status": "unverified", "overview": "No real company research performed", "businessDirection": "Unknown", "roleAnalysis": "Synthetic fixture only", "sources": []any{}, "findings": []any{}, "risks": []any{}, "unknowns": []string{"No live research"}}, "questions": []string{}}
	valid := releasePublicObject(t, releasePublicCall(t, a, plugin, "pr_validate", map[string]any{"packet": packet, "result": result}))
	if valid["valid"] != true {
		t.Fatal(valid)
	}
	invalid := map[string]any{"protocol": 1, "profileId": "different-profile", "targetId": "release-target"}
	if _, err := a.callPluginToolContext(context.Background(), plugin, "pr_validate", map[string]any{"packet": packet, "result": invalid}); err == nil {
		t.Fatal("mismatched result accepted")
	}
	completed := releasePublicObject(t, releasePublicCall(t, a, plugin, "pr_tasks", map[string]any{"action": "complete", "id": id, "packetHash": packetHash, "result": result}))
	if completed["status"] != "complete" {
		t.Fatal(completed)
	}
	retrieved := releasePublicObject(t, releasePublicCall(t, a, plugin, "pr_tasks", map[string]any{"action": "result", "id": id}))
	if retrieved["status"] != "complete" || retrieved["result"] == nil {
		t.Fatal(retrieved)
	}
	if _, err := a.callPluginToolContext(context.Background(), plugin, "pr_tasks", map[string]any{"action": "complete", "id": id, "packetHash": packetHash, "result": result}); err == nil {
		t.Fatal("completed task overwritten")
	}
	deleted := releasePublicObject(t, releasePublicCall(t, a, plugin, "pr_tasks", map[string]any{"action": "delete", "id": id}))
	if deleted["deleted"] != true {
		t.Fatal(deleted)
	}
	if _, err := a.callPluginToolContext(context.Background(), plugin, "pr_tasks", map[string]any{"action": "get", "id": id}); err == nil {
		t.Fatal("deleted task still available")
	}
	return []string{"installed guide and consent-required fictional task", "encrypted task cache contains no plaintext fixture", "evidence-bound result validation and mismatch refusal", "complete/result lifecycle, immutable completion and deletion; no live research or model call"}
}
