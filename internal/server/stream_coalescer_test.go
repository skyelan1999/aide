package server

import (
	"strings"
	"testing"
	"time"
)

func TestStreamDeltaCoalescerPreservesContentAndOrder(t *testing.T) {
	a := testApp(t)
	taskID := "coalesced-stream"
	a.liveTaskSess = map[string]string{taskID: "session"}
	events, unsubscribe := a.subscribeStream(taskID)
	defer unsubscribe()

	coalescer := newStreamDeltaCoalescer(a, taskID, 3)
	coalescer.delta("a")
	coalescer.delta("b")
	coalescer.delta("c")
	coalescer.reasoning("r1")
	coalescer.reasoning("r2")
	coalescer.delta("d")
	coalescer.close()

	var deltas, reasoning []string
	var deltaText, reasoningText strings.Builder
	for {
		select {
		case event := <-events:
			switch event.Event {
			case "delta":
				if len(reasoning) > 0 && deltaText.Len() == 3 && event.Text == "d" {
					// The final answer delta must follow the intervening reasoning.
					deltas = append(deltas, "after-reasoning")
				}
				deltaText.WriteString(event.Text)
			case "reasoning":
				reasoning = append(reasoning, event.Reasoning)
				reasoningText.WriteString(event.Reasoning)
			}
		default:
			goto drained
		}
	}

drained:
	if deltaText.String() != "abcd" || reasoningText.String() != "r1r2" {
		t.Fatalf("coalesced content = %q / %q", deltaText.String(), reasoningText.String())
	}
	if len(deltas) != 1 {
		t.Fatal("delta/reasoning/delta order was not preserved")
	}
	live, _, found := a.liveBroker.GetSessionLiveOutput("session")
	if !found || live != "abcd" {
		t.Fatalf("live mirror = %q, found=%v", live, found)
	}
}

func TestStreamDeltaCoalescerTimerFlushesWithoutAnotherChunk(t *testing.T) {
	a := testApp(t)
	events, unsubscribe := a.subscribeStream("timer-flush")
	defer unsubscribe()
	coalescer := newStreamDeltaCoalescer(a, "timer-flush", 0)
	defer coalescer.close()
	coalescer.delta("a")
	coalescer.delta("b")
	var text string
	deadline := time.After(50 * time.Millisecond)
	for len(text) < 2 {
		select {
		case event := <-events:
			text += event.Text
		case <-deadline:
			t.Fatalf("timer did not flush pending text: %q", text)
		}
	}
	if text != "ab" {
		t.Fatalf("timer flushed %q", text)
	}
}

func TestStreamDeltaCoalescerFirstChunkIsImmediate(t *testing.T) {
	a := testApp(t)
	events, unsubscribe := a.subscribeStream("first-chunk")
	defer unsubscribe()
	coalescer := newStreamDeltaCoalescer(a, "first-chunk", 0)
	defer coalescer.close()
	coalescer.delta("first")
	select {
	case event := <-events:
		if event.Text != "first" {
			t.Fatalf("first event = %+v", event)
		}
	case <-time.After(20 * time.Millisecond):
		t.Fatal("first chunk was delayed by the coalescing window")
	}
}
