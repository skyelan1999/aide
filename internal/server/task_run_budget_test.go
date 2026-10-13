package server

import (
	"context"
	"testing"
	"time"
)

func TestTaskRunBudgetPausesDuringHumanWait(t *testing.T) {
	ctx, cancel := withTaskRunBudget(context.Background(), 180*time.Millisecond)
	defer cancel()

	time.Sleep(40 * time.Millisecond)
	resume := pauseTaskRunBudget(ctx)
	time.Sleep(220 * time.Millisecond)
	if err := ctx.Err(); err != nil {
		t.Fatalf("human wait exhausted active execution budget: %v", err)
	}

	resume()
	select {
	case <-ctx.Done():
		if context.Cause(ctx) != context.DeadlineExceeded {
			t.Fatalf("resumed budget ended with cause %v, want deadline exceeded", context.Cause(ctx))
		}
	case <-time.After(250 * time.Millisecond):
		t.Fatal("active execution budget did not resume after human wait")
	}
}

func TestTaskRunBudgetNestedPauseWaitsForEveryResume(t *testing.T) {
	ctx, cancel := withTaskRunBudget(context.Background(), 120*time.Millisecond)
	defer cancel()

	firstResume := pauseTaskRunBudget(ctx)
	secondResume := pauseTaskRunBudget(ctx)
	time.Sleep(150 * time.Millisecond)
	firstResume()
	time.Sleep(40 * time.Millisecond)
	if err := ctx.Err(); err != nil {
		t.Fatalf("nested pause resumed too early: %v", err)
	}
	secondResume()

	select {
	case <-ctx.Done():
		if context.Cause(ctx) != context.DeadlineExceeded {
			t.Fatalf("resumed budget ended with cause %v, want deadline exceeded", context.Cause(ctx))
		}
	case <-time.After(180 * time.Millisecond):
		t.Fatal("budget did not resume after the final pause was released")
	}
}

func TestTaskRunBudgetParentCancellationSurvivesPause(t *testing.T) {
	parent, cancelParent := context.WithCancel(context.Background())
	ctx, cancel := withTaskRunBudget(parent, time.Second)
	defer cancel()

	resume := pauseTaskRunBudget(ctx)
	cancelParent()
	select {
	case <-ctx.Done():
		if context.Cause(ctx) != context.Canceled {
			t.Fatalf("parent cancellation cause = %v, want canceled", context.Cause(ctx))
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("parent cancellation was ignored during human wait")
	}
	resume()
}
