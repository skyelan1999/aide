package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
)

// personalityStateCandidate copies the map and changed history under a.mu.
func (a *App) personalityStateCandidate(id string, entry personalityStateEntry) personalityStateFile {
	next := personalityStateFile{Entries: maps.Clone(a.personalityState.Entries)}
	if next.Entries == nil {
		next.Entries = map[string]personalityStateEntry{}
	}
	entry.History = append([]string(nil), entry.History...)
	next.Entries[id] = entry
	return next
}

// commitPersonalityStateLocked publishes counters/attempt metadata only after saving.
// The caller holds a.mu. Prompt changes use commitPersonalityLocked instead.
func (a *App) commitPersonalityStateLocked(id string, entry personalityStateEntry) error {
	next := a.personalityStateCandidate(id, entry)
	if err := atomicJSON(PersonalityStatePath(a.dataPath), next); err != nil {
		return err
	}
	a.personalityState = next
	return nil
}

// commitPersonalityLocked saves candidates before publishing memory or revisions.
// For a two-file update, save history first and restore its preimage if settings
// fails. This is a compensating write, not a crash-atomic multi-file transaction.
// A failed compensation is returned explicitly; no rejected candidate is published.
// The caller holds a.mu throughout the operation.
func (a *App) commitPersonalityLocked(id string, personality *Personality, entry *personalityStateEntry) error {
	settings := a.settings
	settings.Personalities = maps.Clone(a.settings.Personalities)
	if settings.Personalities == nil {
		settings.Personalities = map[string]Personality{}
	}
	if personality == nil {
		delete(settings.Personalities, id)
	} else {
		settings.Personalities[id] = *personality
	}
	var nextState personalityStateFile
	var preimage []byte
	stateExisted := false
	if entry != nil {
		var err error
		preimage, err = os.ReadFile(PersonalityStatePath(a.dataPath))
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		stateExisted = err == nil
		if stateExisted && !json.Valid(preimage) {
			return errors.New("性格状态文件损坏，请修复后重试")
		}
		nextState = a.personalityStateCandidate(id, *entry)
		if err := atomicJSON(PersonalityStatePath(a.dataPath), nextState); err != nil {
			return err
		}
	}
	if err := atomicJSON(SettingsPath(a.dataPath), settings); err != nil {
		if entry == nil {
			return err
		}
		var rollbackErr error
		if stateExisted {
			rollbackErr = atomicJSON(PersonalityStatePath(a.dataPath), json.RawMessage(preimage))
		} else {
			rollbackErr = os.Remove(PersonalityStatePath(a.dataPath))
			if errors.Is(rollbackErr, os.ErrNotExist) {
				rollbackErr = nil
			}
		}
		if rollbackErr != nil {
			return errors.Join(err, fmt.Errorf("性格历史恢复失败，内存保留原配置；请检查磁盘状态: %w", rollbackErr))
		}
		return err
	}
	a.settings.Personalities = settings.Personalities
	if entry != nil {
		a.personalityState = nextState
	}
	if a.personalityRevisions == nil {
		a.personalityRevisions = map[string]uint64{}
	}
	a.personalityRevisions[id]++
	return nil
}
