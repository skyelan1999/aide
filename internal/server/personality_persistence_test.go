package server

import (
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestIssue155PersonalityChangesRejectPersistenceFailure(t *testing.T) {
	for _, action := range []string{"save", "reset", "rollback", "evolve", "voice-adopt", "automatic"} {
		for _, target := range []string{"settings", "state"} {
			if action == "save" && target == "state" {
				continue // A plain prompt save has no history-state update.
			}
			t.Run(action+"/"+target, func(t *testing.T) {
				a := testApp(t)
				id := personaAide
				if action == "voice-adopt" {
					id = personaXiaomi
				}
				old := Personality{Enabled: true, Prompt: "你是 aide 小秘。" + strings.Repeat("旧", 80), Evolutions: 3}
				priorState := personalityStateEntry{CountSinceEvolve: 2, History: []string{"你是 aide 小秘。保留的旧版本"}}
				if action == "evolve" || action == "automatic" {
					srv := evolveMockServer(t, "你是 aide。"+strings.Repeat("新", 25))
					a.settings.BaseURL, a.settings.Model = srv.URL, "test"
					var messages []Message
					for _, line := range strings.Split(strings.TrimSpace(okSample), "\n") {
						messages = append(messages, Message{Role: "user", Content: line})
					}
					a.sessions["owned-evolution-sample"] = &Session{ID: "owned-evolution-sample", Updated: time.Now().UTC().Format(time.RFC3339Nano), Messages: messages}
				}
				a.mu.Lock()
				if a.settings.Personalities == nil {
					a.settings.Personalities = map[string]Personality{}
				}
				a.settings.Personalities[id] = old
				a.personalityState.Entries[id] = priorState
				revision := a.personalityRevisions[id]
				err := atomicJSON(SettingsPath(a.dataPath), a.settings)
				if err == nil {
					err = atomicJSON(PersonalityStatePath(a.dataPath), a.personalityState)
				}
				a.mu.Unlock()
				if err != nil {
					t.Fatal(err)
				}
				path := SettingsPath(a.dataPath)
				if target == "state" {
					path = PersonalityStatePath(a.dataPath)
				}
				backup := path + ".owned-fixture"
				if err := os.Rename(path, backup); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(path, 0700); err != nil {
					t.Fatal(err)
				}
				invoke := func() int {
					switch action {
					case "save":
						return request(a, "PUT", "/api/personality", map[string]any{"id": id, "enabled": true, "prompt": "你是 aide。用户的新提示词"}).Code
					case "voice-adopt":
						return request(a, "POST", "/api/voice-personality/adopt", map[string]string{"prompt": "你是小秘。用户采纳的新提示词"}).Code
					case "automatic":
						a.runAutoEvolve(id, modeRefine, "owned-test", okSample)
						return 0
					default:
						return request(a, "POST", "/api/personality/"+action, map[string]string{"id": id}).Code
					}
				}
				if status := invoke(); action != "automatic" && status != 500 {
					t.Fatalf("failed %s save reported HTTP %d", action, status)
				}
				a.mu.Lock()
				current, state, currentRevision := a.personalityLocked(id), a.personalityStateLocked(id), a.personalityRevisions[id]
				a.mu.Unlock()
				if current != old || !reflect.DeepEqual(state.History, priorState.History) || state.CountSinceEvolve != priorState.CountSinceEvolve || currentRevision != revision {
					t.Fatalf("failed change published prompt/history/revision: promptChanged=%v history=%v count=%d revision=%d", current != old, state.History, state.CountSinceEvolve, currentRevision)
				}
				settingsPath, statePath := SettingsPath(a.dataPath), PersonalityStatePath(a.dataPath)
				if target == "settings" {
					settingsPath = backup
				} else {
					statePath = backup
				}
				var diskSettings Settings
				var diskState personalityStateFile
				b, err := os.ReadFile(settingsPath)
				if err != nil || json.Unmarshal(b, &diskSettings) != nil || diskSettings.Personalities[id] != old {
					t.Fatal("failed change replaced persisted prompt", err)
				}
				b, err = os.ReadFile(statePath)
				if err != nil || json.Unmarshal(b, &diskState) != nil || !reflect.DeepEqual(diskState.Entries[id].History, priorState.History) {
					t.Fatal("failed change replaced persisted rollback history", err)
				}
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				if err := os.Rename(backup, path); err != nil {
					t.Fatal(err)
				}
				if status := invoke(); action != "automatic" && status != 200 {
					t.Fatalf("repaired storage did not permit retry: HTTP %d", status)
				}
				a.mu.Lock()
				changed := a.personalityLocked(id)
				a.mu.Unlock()
				if changed == old {
					t.Fatal("successful retry did not adopt requested change")
				}
				b, err = os.ReadFile(SettingsPath(a.dataPath))
				if err != nil || json.Unmarshal(b, &diskSettings) != nil || (action != "reset" && diskSettings.Personalities[id] != changed) {
					t.Fatal("successful change was not persisted", err)
				}
			})
		}
	}
}

func TestIssue155MissingStateIsRemovedWhenSettingsSaveFails(t *testing.T) {
	a := testApp(t)
	statePath := PersonalityStatePath(a.dataPath)
	if err := os.Remove(statePath); err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	settingsPath := SettingsPath(a.dataPath)
	if err := os.Remove(settingsPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	if err := os.Mkdir(settingsPath, 0700); err != nil {
		t.Fatal(err)
	}
	a.mu.Lock()
	old := a.personalityLocked(personaAide)
	next := old
	next.Prompt = "aide owned candidate"
	state := a.personalityStateLocked(personaAide)
	state.History = []string{"owned prior prompt"}
	err := a.commitPersonalityLocked(personaAide, &next, &state)
	current := a.personalityLocked(personaAide)
	a.mu.Unlock()
	if err == nil || current != old {
		t.Fatal("failed save adopted candidate", err)
	}
	if _, err := os.Stat(statePath); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("failed two-file update left a newly created history file", err)
	}
}

func TestIssue155FailedMetadataSaveKeepsPreviousState(t *testing.T) {
	a := testApp(t)
	path := PersonalityStatePath(a.dataPath)
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	a.mu.Lock()
	before := a.personalityStateLocked(personaAide)
	fire, _ := a.onPersonalityInteractLocked(personaAide)
	afterCounter := a.personalityStateLocked(personaAide)
	personality := a.personalityLocked(personaAide)
	a.mu.Unlock()
	err := a.finishPersonalityAttempt(personaAide, "owned-trigger", "failed", personality, personality, "owned test")
	a.mu.Lock()
	afterAttempt := a.personalityStateLocked(personaAide)
	a.mu.Unlock()
	if fire || err == nil || !reflect.DeepEqual(before, afterCounter) || !reflect.DeepEqual(before, afterAttempt) {
		t.Fatal("unsaved trigger/attempt state became visible", err)
	}
}
