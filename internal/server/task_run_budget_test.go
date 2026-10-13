package server

import (
	"context"
	"encoding/json"
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

func TestAwaitUserAnswerPausesTaskRunBudget(t *testing.T) {
	a, _, task := seedClarifySession(t, "running")
	ctx, cancel := withTaskRunBudget(context.Background(), 140*time.Millisecond)
	defer cancel()

	answerResult := make(chan string, 1)
	go func() {
		answerResult <- a.awaitUserAnswer(ctx, task, json.RawMessage(`{"type":"clarify","prompt":"continue?"}`))
	}()
	deadline := time.Now().Add(time.Second)
	for {
		a.mu.Lock()
		status := task.Status
		a.mu.Unlock()
		if status == "awaiting_clarification" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("clarification did not enter the human-wait state")
		}
		time.Sleep(time.Millisecond)
	}

	time.Sleep(180 * time.Millisecond)
	if err := ctx.Err(); err != nil {
		t.Fatalf("awaitUserAnswer consumed the execution budget while waiting: %v", err)
	}
	w := request(a, "POST", "/api/sessions/sess1/runs/run1/answer", map[string]string{"answer": "continue"})
	requireStatus(t, w, 200)
	select {
	case got := <-answerResult:
		if got != "continue" {
			t.Fatalf("awaitUserAnswer returned %q, want the submitted answer", got)
		}
	case <-time.After(time.Second):
		t.Fatal("awaitUserAnswer did not resume after the answer")
	}

	select {
	case <-ctx.Done():
		if context.Cause(ctx) != context.DeadlineExceeded {
			t.Fatalf("resumed execution ended with cause %v, want deadline exceeded", context.Cause(ctx))
		}
	case <-time.After(250 * time.Millisecond):
		t.Fatal("remaining execution budget did not resume after the answer")
	}
}
