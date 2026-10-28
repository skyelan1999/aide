package server

import "testing"

func TestNativeReplyLateASRAndNextInput(t *testing.T) {
	l := newNativeTurnLedger()
	l.commit("first")
	l.created("reply-first")
	r := l.replies["reply-first"]
	r.text = "first advice"
	r.done = true
	l.commit("second")
	l.transcript("second", "another project")
	if len(l.ready()) != 0 {
		t.Fatal("reply rebound to next ASR")
	}
	l.transcript("first", "first project")
	if len(l.ready()) != 1 || l.ready()[0].turn != "first" {
		t.Fatal("late ASR lost")
	}
	if len(r.inputs) != 1 || r.inputs[0] != "first" {
		t.Fatal("future evidence attached")
	}
	r.saved = true
	if len(l.ready()) != 0 {
		t.Fatal("duplicate save")
	}
}

func TestNativeCanceledAndAmbiguousRepliesFailClosed(t *testing.T) {
	l := newNativeTurnLedger()
	l.commit("one")
	l.transcript("one", "user")
	l.created("canceled")
	r := l.replies["canceled"]
	r.text = "unheard promise"
	r.done = true
	r.canceled = true
	if len(l.ready()) != 0 {
		t.Fatal("canceled speech became memory")
	}
	l = newNativeTurnLedger()
	l.created("a")
	l.created("b")
	l.transcript("late", "unknown turn")
	if l.replies["a"].turn != "" || l.replies["b"].turn != "" {
		t.Fatal("ambiguous reply adopted an arbitrary source")
	}
}
