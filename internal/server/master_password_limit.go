package server

import (
	"errors"
	"math"
	"net/http"
	"strconv"
	"sync"
	"time"
)

const masterPasswordFailureLimit = 5
const masterPasswordCooldown = time.Minute

// One bounded budget per app, shared by every master-password verifier. The
// mutex also limits expensive KDF work to one concurrent attempt. No peer IP,
// forwarded header, password, token or unbounded per-client map is retained.
type masterPasswordAttempts struct {
	mu           sync.Mutex
	failures     int
	windowStart  time.Time
	blockedUntil time.Time
	now          func() time.Time
}

type masterPasswordCooldownError struct{ retryAfter time.Duration }

func (e *masterPasswordCooldownError) Error() string {
	return "密码尝试过于频繁，请稍后重试"
}

// This mutex never acquires App.mu, so callers already holding App.mu may use it.
func (a *App) verifyMasterPassword(password, hash string) (valid, needsUpgrade bool, err error) {
	if hash == "" {
		return false, false, nil
	}
	budget := &a.masterPasswordAttempts
	budget.mu.Lock()
	defer budget.mu.Unlock()
	now := time.Now()
	if budget.now != nil {
		now = budget.now()
	}
	if now.Before(budget.blockedUntil) {
		return false, false, &masterPasswordCooldownError{retryAfter: budget.blockedUntil.Sub(now)}
	}
	if !budget.blockedUntil.IsZero() || (!budget.windowStart.IsZero() && now.Sub(budget.windowStart) >= masterPasswordCooldown) {
		budget.failures, budget.windowStart, budget.blockedUntil = 0, time.Time{}, time.Time{}
	}
	valid, needsUpgrade = VerifyPassword(password, hash)
	if valid {
		budget.failures, budget.windowStart = 0, time.Time{}
		return valid, needsUpgrade, nil
	}
	if budget.failures == 0 {
		budget.windowStart = now
	}
	budget.failures++
	if budget.failures >= masterPasswordFailureLimit {
		budget.blockedUntil = now.Add(masterPasswordCooldown)
	}
	return false, false, nil
}

func writeMasterPasswordCooldown(w http.ResponseWriter, err error) bool {
	var cooldown *masterPasswordCooldownError
	if !errors.As(err, &cooldown) {
		return false
	}
	w.Header().Set("Retry-After", strconv.Itoa(max(1, int(math.Ceil(cooldown.retryAfter.Seconds())))))
	fail(w, http.StatusTooManyRequests, cooldown)
	return true
}
