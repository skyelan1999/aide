package server

import "time"

// compactionCircuitKey keeps failures isolated between sessions and models.
// State is process-local: session files and their schema remain unchanged.
type compactionCircuitKey struct {
	sessionID string
	model     string
}

type compactionCircuitState struct {
	failures   int
	retryAfter time.Time
	updated    time.Time
}

const (
	compactionCircuitMaxAge  = 24 * time.Hour
	compactionCircuitMaxKeys = 128
)

// autoCompactionRetryAfterLocked returns zero for closed or half-open circuits.
// Only automatic background compaction consults this gate; manual and preflight
// compaction always remain available as explicit recovery paths.
func (a *App) autoCompactionRetryAfterLocked(key compactionCircuitKey, now time.Time) time.Duration {
	state, ok := a.contextCompactionCircuits[key]
	if !ok {
		return 0
	}
	if !now.Before(state.retryAfter) {
		delete(a.contextCompactionCircuits, key)
		return 0
	}
	return state.retryAfter.Sub(now)
}

// recordAutoCompactionFailureLocked applies bounded exponential cooldowns:
// 30 seconds, two minutes, then five minutes. The next automatic attempt after
// the deadline is the half-open probe; a success clears all failure history.
func (a *App) recordAutoCompactionFailureLocked(key compactionCircuitKey, now time.Time) time.Duration {
	if a.contextCompactionCircuits == nil {
		a.contextCompactionCircuits = make(map[compactionCircuitKey]compactionCircuitState)
	}
	for existing, state := range a.contextCompactionCircuits {
		if now.Sub(state.updated) >= compactionCircuitMaxAge {
			delete(a.contextCompactionCircuits, existing)
		}
	}
	if len(a.contextCompactionCircuits) >= compactionCircuitMaxKeys {
		var oldestKey compactionCircuitKey
		var oldest time.Time
		for existing, state := range a.contextCompactionCircuits {
			if oldest.IsZero() || state.updated.Before(oldest) {
				oldestKey, oldest = existing, state.updated
			}
		}
		delete(a.contextCompactionCircuits, oldestKey)
	}
	state := a.contextCompactionCircuits[key]
	state.failures++
	state.updated = now
	cooldown := 5 * time.Minute
	switch state.failures {
	case 1:
		cooldown = 30 * time.Second
	case 2:
		cooldown = 2 * time.Minute
	}
	state.retryAfter = now.Add(cooldown)
	a.contextCompactionCircuits[key] = state
	return cooldown
}

func (a *App) resetCompactionCircuitLocked(key compactionCircuitKey) {
	delete(a.contextCompactionCircuits, key)
}
