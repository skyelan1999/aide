package server

import (
	"testing"
	"time"
)

func TestTaskRunTimeout(t *testing.T) {
	if got := taskRunTimeout("workflow"); got != 15*time.Minute {
		t.Fatalf("workflow timeout = %s, want 15m", got)
	}
	if got := taskRunTimeout("chat"); got != 6*time.Minute {
		t.Fatalf("chat timeout = %s, want 6m", got)
	}
}
