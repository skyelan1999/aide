package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestIssue94FlatMigrationRejectsUnavailableJournalBeforeMutating(t *testing.T) {
	data := t.TempDir()
	if err := EnsureDirs(data); err != nil {
		t.Fatal(err)
	}
	original := filepath.Join(data, SettingsFileName)
	flat := []byte(`{"model":"owned-flat-original"}`)
	current := []byte(`{"model":"owned-preexisting-current"}`)
	if err := os.WriteFile(original, flat, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(SettingsPath(data), current, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(migrationStatePath(data), 0700); err != nil {
		t.Fatal(err)
	}
	if err := MigrateFlatToLayered(data); err == nil {
		t.Error("migration reported success without persistent rollback state")
	}
	if got, err := os.ReadFile(original); err != nil || string(got) != string(flat) {
		t.Error("flat original not preserved on journal failure", err)
	}
	if got, err := os.ReadFile(SettingsPath(data)); err != nil || string(got) != string(current) {
		t.Error("preexisting layered settings changed", err)
	}
}

func TestIssue94InterruptedMigrationRestoresOriginalsAndPriorState(t *testing.T) {
	data := t.TempDir()
	if err := EnsureDirs(data); err != nil {
		t.Fatal(err)
	}
	original := filepath.Join(data, "session-owned.json")
	dest := SessionPath(data, "owned", "active")
	state := migrationStatePath(data)
	old := []byte(`{"id":"owned","runs":[]}`)
	previousState := []byte(`{"stage":"previous-owned-state"}`)
	if err := os.WriteFile(original, old, 0640); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(original, 0640); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(state, previousState, 0600); err != nil {
		t.Fatal(err)
	}
	u, err := beginMigrationUndo(data, "flat-migration", []string{original, dest, state})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dest, []byte("new-owned-copy"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(original); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(state, []byte(`{"stage":"new-state"}`), 0600); err != nil {
		t.Fatal(err)
	}
	// No in-memory undo invocation: recovery reopens the durable journal.
	if err := recoverMigrationUndo(data, "flat-migration"); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(original); err != nil || string(got) != string(old) {
		t.Fatal("source recovery failed", err)
	}
	if got, err := os.ReadFile(state); err != nil || string(got) != string(previousState) {
		t.Fatal("prior marker recovery failed", err)
	}
	if st, err := os.Stat(original); err != nil || st.Mode().Perm() != 0640 {
		t.Fatal("source mode not preserved", err)
	}
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Fatal("new destination retained", err)
	}
	if _, err := os.Stat(filepath.Join(data, u.BackupDir)); !os.IsNotExist(err) {
		t.Fatal("undo copies retained after recovery", err)
	}
	if err := recoverMigrationUndo(data, "flat-migration"); err != nil {
		t.Fatal("recovery not idempotent", err)
	}
}

func TestIssue94FailedRollbackRetainsJournalForRetry(t *testing.T) {
	data := t.TempDir()
	if err := EnsureDirs(data); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(data, "owned.txt")
	if err := os.WriteFile(file, []byte("owned-original"), 0600); err != nil {
		t.Fatal(err)
	}
	u, err := beginMigrationUndo(data, "flat-migration", []string{file})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(file, 0700); err != nil {
		t.Fatal(err)
	}
	if err := recoverMigrationUndo(data, "flat-migration"); err == nil {
		t.Fatal("blocked recovery reported success")
	}
	if _, err := os.Stat(migrationUndoPath(data, "flat-migration")); err != nil {
		t.Fatal("pending journal lost", err)
	}
	if _, err := os.Stat(filepath.Join(data, u.BackupDir)); err != nil {
		t.Fatal("original copies lost", err)
	}
	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	if err := recoverMigrationUndo(data, "flat-migration"); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(file); err != nil || string(got) != "owned-original" {
		t.Fatal("retry did not restore original", err)
	}
}

func TestIssue94CorruptPreimageIsNeverPublished(t *testing.T) {
	data := t.TempDir()
	if err := EnsureDirs(data); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(data, "owned.txt")
	if err := os.WriteFile(file, []byte("owned-original"), 0600); err != nil {
		t.Fatal(err)
	}
	u, err := beginMigrationUndo(data, "flat-migration", []string{file})
	if err != nil {
		t.Fatal(err)
	}
	backup := filepath.Join(data, u.BackupDir, u.Entries[0].Backup)
	if err := os.WriteFile(backup, []byte("corrupted-copy"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte("owned-current"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := recoverMigrationUndo(data, "flat-migration"); err == nil {
		t.Fatal("corrupted preimage accepted")
	}
	if got, err := os.ReadFile(file); err != nil || string(got) != "owned-current" {
		t.Fatal("corrupted preimage published", err)
	}
	if err := os.WriteFile(backup, []byte("owned-original"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := recoverMigrationUndo(data, "flat-migration"); err != nil {
		t.Fatal(err)
	}
}

func TestIssue94CommittedJournalNeverUndoesCompletedMigration(t *testing.T) {
	data := t.TempDir()
	if err := EnsureDirs(data); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(data, "owned.txt")
	if err := os.WriteFile(file, []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}
	u, err := beginMigrationUndo(data, "flat-migration", []string{file})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte("committed"), 0600); err != nil {
		t.Fatal(err)
	}
	u.Stage = "committed"
	if err := atomicMigrationState(data, migrationUndoPath(data, "flat-migration"), u); err != nil {
		t.Fatal(err)
	}
	if err := recoverMigrationUndo(data, "flat-migration"); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(file); err != nil || string(got) != "committed" {
		t.Fatal("committed migration rolled back", err)
	}
}

func TestIssue94RecoveryRejectsEscapingMetadata(t *testing.T) {
	data := t.TempDir()
	if err := EnsureDirs(data); err != nil {
		t.Fatal(err)
	}
	u := migrationUndo{Version: 1, Stage: "pending", BackupDir: ".integrity/.migration-undo-flat-migration-owned", Entries: []migrationUndoEntry{{Path: "../outside"}}}
	b, err := json.Marshal(u)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(migrationUndoPath(data, "flat-migration"), b, 0600); err != nil {
		t.Fatal(err)
	}
	if err := recoverMigrationUndo(data, "flat-migration"); err == nil {
		t.Fatal("escaping undo path accepted")
	}
}

func TestIssue94MigrationEntryPointsRecoverBeforeNoOp(t *testing.T) {
	for _, kind := range []string{"flat-migration", "session-bucket-migration"} {
		t.Run(kind, func(t *testing.T) {
			data := t.TempDir()
			if err := EnsureDirs(data); err != nil {
				t.Fatal(err)
			}
			file := SettingsPath(data)
			if err := os.WriteFile(file, []byte("owned-before"), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := beginMigrationUndo(data, kind, []string{file}); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(file, []byte("interrupted-update"), 0600); err != nil {
				t.Fatal(err)
			}
			var err error
			if kind == "flat-migration" {
				err = MigrateFlatToLayered(data)
			} else {
				err = rebalanceSessionBuckets(data)
			}
			if err != nil {
				t.Fatal(err)
			}
			if got, err := os.ReadFile(file); err != nil || string(got) != "owned-before" {
				t.Fatal("entry point skipped undo recovery", err)
			}
		})
	}
}

func TestIssue94ManualRecoveryDoesNotAlsoRollbackPreviousMigration(t *testing.T) {
	data := t.TempDir()
	if err := EnsureDirs(data); err != nil {
		t.Fatal(err)
	}
	prior := SessionPath(data, "prior", "active")
	if err := os.WriteFile(prior, []byte("previous-completed-result"), 0600); err != nil {
		t.Fatal(err)
	}
	rel, err := filepath.Rel(data, prior)
	if err != nil {
		t.Fatal(err)
	}
	state := sessionRebalanceStatePath(data)
	if err := atomicMigrationState(data, state, sessionRebalanceState{Stage: "done", Added: []string{rel}}); err != nil {
		t.Fatal(err)
	}
	changed := SessionPath(data, "changed", "active")
	if err := os.WriteFile(changed, []byte("before-new-attempt"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := beginMigrationUndo(data, "session-bucket-migration", []string{changed, state}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(changed, []byte("interrupted-new-attempt"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := RollbackSessionBucketMigration(data); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(changed); err != nil || string(got) != "before-new-attempt" {
		t.Fatal("latest attempt not recovered", err)
	}
	if got, err := os.ReadFile(prior); err != nil || string(got) != "previous-completed-result" {
		t.Fatal("previous completed migration was also rolled back", err)
	}
}

func TestIssue94SessionMigrationRejectsUnavailableJournalBeforeMutating(t *testing.T) {
	data := t.TempDir()
	if err := EnsureDirs(data); err != nil {
		t.Fatal(err)
	}
	original := SessionPath(data, "owned-fixture", "active")
	canonical := SessionPath(data, "owned-fixture", "archived")
	latest := []byte(`{"id":"owned-fixture","archived":true,"updated":"2026-10-26T12:00:00Z","runs":[]}`)
	previous := []byte(`{"id":"owned-fixture","archived":true,"updated":"2026-10-25T12:00:00Z","runs":[]}`)
	if err := os.WriteFile(original, latest, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(canonical, previous, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(sessionRebalanceStatePath(data), 0700); err != nil {
		t.Fatal(err)
	}
	if err := rebalanceSessionBuckets(data); err == nil {
		t.Error("session migration reported success without persistent rollback state")
	}
	if got, err := os.ReadFile(original); err != nil || string(got) != string(latest) {
		t.Error("source session not preserved on journal failure", err)
	}
	if got, err := os.ReadFile(canonical); err != nil || string(got) != string(previous) {
		t.Error("preexisting canonical session overwritten on journal failure", err)
	}
}
