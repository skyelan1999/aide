package server

import (
	"context"
	"errors"
)

// Optional structured failure reporting for manual callers. The model-facing
// text protocol remains unchanged, and file content is never classified by text.
type toolDispatchOutcomeKey struct{}
type toolDispatchOutcome struct{ err error }

func recordToolDispatchFailure(ctx context.Context, message string) string {
	if outcome, ok := ctx.Value(toolDispatchOutcomeKey{}).(*toolDispatchOutcome); ok {
		outcome.err = errors.New(message)
	}
	return message
}
