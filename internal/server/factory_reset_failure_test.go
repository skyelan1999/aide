package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func issue120BlockPath(t *testing.T, target string) {
	t.Helper()
	if err := os.Remove(target); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if err := os.MkdirAll(target, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "owned-blocker"), []byte("owned failure fixture"), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestIssue120ResetPersistenceFailuresNeverReportSuccess(t *testing.T) {
	for _, tc := range []struct {
		name, scope string
		path        func(*App) string
	}{
		{"settings", "settings", func(a *App) string { return SettingsPath(a.dataPath) }},
		{"session-file", "sessionsAndMemory", func(a *App) string { return filepath.Join(sessionBucketDirs(a.dataPath)[0], "session-000-owned.json") }},
		{"memory", "sessionsAndMemory", func(a *App) string { return a.memoryPath() }},
		{"plugin-experience", "sessionsAndMemory", func(a *App) string { return a.pluginExperiencePath() }},
		{"voice-history", "sessionsAndMemory", func(a *App) string { return VoiceHistoryPath(a.dataPath) }},
		{"personality-state", "sessionsAndMemory", func(a *App) string { return PersonalityStatePath(a.dataPath) }},
		{"source-secret", "credentialsAndKeys", func(a *App) string { return SourcesSecretsPath(a.dataPath) }},
		{"access-token", "credentialsAndKeys", func(a *App) string { return AccessTokenPath(a.dataPath) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := testApp(t)
			a.settings.ToolMaxRounds = 17
			beforeToken := a.token
			issue120BlockPath(t, tc.path(a))
			w := postReset(a, resetBody(map[string]any{tc.scope: true}, "重置", ""))
			if w.Code != 500 {
				t.Errorf("failed %s reset returned%d, want500", tc.name, w.Code)
			}
			var body struct {
				OK          bool   `json:"ok"`
				FailedScope string `json:"failedScope"`
				BackupPath  string `json:"backupPath"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if body.OK || body.FailedScope != tc.scope || body.BackupPath == "" {
				t.Errorf("missing failure/recovery receipt: %s", w.Body.String())
			} else if backup, err := os.ReadFile(body.BackupPath); err != nil || !json.Valid(backup) {
				t.Errorf("unreadable recovery backup: %v", err)
			}
			if tc.scope == "settings" && a.settings.ToolMaxRounds != 17 {
				t.Error("failed settings save changed active settings")
			}
			if tc.name == "access-token" && a.token != beforeToken {
				t.Error("failed token persistence changed active token")
			}
		})
	}
}

func TestIssue120PartialResetReportsCompletedScopeAndBackup(t *testing.T) {
	a := testApp(t)
	a.settings.ToolMaxRounds = 17
	issue120BlockPath(t, SourcesSecretsPath(a.dataPath))
	w := postReset(a, resetBody(map[string]any{"settings": true, "credentialsAndKeys": true}, "重置", ""))
	requireStatus(t, w, 500)
	var body struct {
		OK              bool     `json:"ok"`
		Partial         bool     `json:"partial"`
		FailedScope     string   `json:"failedScope"`
		CompletedScopes []string `json:"completedScopes"`
		BackupPath      string   `json:"backupPath"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.OK || !body.Partial || body.FailedScope != "credentialsAndKeys" || len(body.CompletedScopes) != 1 || body.CompletedScopes[0] != "settings" || body.BackupPath == "" {
		t.Fatal(w.Body.String())
	}
	if a.settings.ToolMaxRounds != defaultSettings().ToolMaxRounds {
		t.Fatal("completed settings scope was not applied")
	}
}

func TestIssue120BackupFailureDoesNotStartReset(t *testing.T) {
	a := testApp(t)
	a.settings.ToolMaxRounds = 17
	if err := os.Remove(ConfigBackupsDir(a.dataPath)); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if err := os.WriteFile(ConfigBackupsDir(a.dataPath), []byte("owned blocker"), 0600); err != nil {
		t.Fatal(err)
	}
	w := postReset(a, resetBody(map[string]any{"settings": true}, "重置", ""))
	requireStatus(t, w, 500)
	var result map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result["failedScope"] != "backup" || result["partial"] != false || a.settings.ToolMaxRounds != 17 {
		t.Fatal(w.Body.String())
	}
}

func TestIssue120AuditFailureAndUniqueRecoveryBackups(t *testing.T) {
	a := testApp(t)
	a.settings.ToolMaxRounds = 17
	first, _, err := a.FactoryReset(ResetScope{Settings: true}, "")
	if err != nil {
		t.Fatal(err)
	}
	second, _, err := a.FactoryReset(ResetScope{Settings: true}, "")
	if err != nil || first == second {
		t.Fatalf("backup collision: %s %s %v", first, second, err)
	}
	for _, backup := range []string{first, second} {
		if data, err := os.ReadFile(backup); err != nil || !json.Valid(data) {
			t.Fatal("backup not retained", err)
		}
	}
	issue120BlockPath(t, factoryResetAuditPath(a.dataPath))
	w := postReset(a, resetBody(map[string]any{"settings": true}, "重置", ""))
	requireStatus(t, w, 500)
	var result map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result["failedScope"] != "audit" || result["partial"] != true {
		t.Fatal(w.Body.String())
	}
}

func TestIssue120UnreadableBucketIsNotAnEmptySuccessfulReset(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "bucket")
	if err := os.WriteFile(dir, []byte("owned non-directory"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := removeGlob(dir, "session-*.json"); err == nil {
		t.Fatal("non-directory bucket treated as empty")
	}
}
