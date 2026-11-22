package server

import (
	"crypto/sha256"
	"encoding/json"
	"log"
	"time"
)

// Per-boot typed decoding reuse, never a persistent file/mtime cache. Callers
// still read the complete current bytes; even same-size/timestamp replacements
// must match SHA256 before a previous typed validation can be reused.
type startupSessionEntry struct {
	digest  [sha256.Size]byte
	session *Session
}

type startupSessionCache struct {
	catalogue       *lazySessionStore
	entries         map[string]startupSessionEntry
	decoded, reused int
}

func (c *startupSessionCache) decode(path string, data []byte) (*Session, error) {
	var digest [sha256.Size]byte
	if c != nil {
		digest = sha256.Sum256(data)
		if previous, ok := c.entries[path]; ok && previous.digest == digest {
			c.reused++
			return previous.session, nil
		}
	}
	var session Session
	if err := json.Unmarshal(data, &session); err != nil {
		return nil, err
	}
	if c != nil {
		if c.entries == nil {
			c.entries = make(map[string]startupSessionEntry)
		}
		c.decoded++
		c.entries[path] = startupSessionEntry{digest: digest, session: &session}
		c.catalogue.record(path, data, &session)
	}
	return &session, nil
}

// Stage timings contain only aggregate counts, never session content or paths.
func startupStageTimer() func(string) {
	started, previous := time.Now(), time.Now()
	return func(stage string) {
		now := time.Now()
		log.Printf("startup: stage=%s elapsed=%s total=%s", stage, now.Sub(previous).Round(time.Millisecond), now.Sub(started).Round(time.Millisecond))
		previous = now
	}
}
