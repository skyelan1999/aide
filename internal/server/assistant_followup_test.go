package server

import (
	"context"
	"encoding/json"
	"os"
	"testing"
)

func TestAssistantClearFollowupsIsReversibleAndPreservesTasks(t *testing.T) {
	a := testApp(t)
	s := &Session{ID: "follow-clean", Number: 901, Title: "synthetic", FollowedByAssistant: true, FollowNote: "keep note", Messages: []Message{{Role: "user", Content: "keep message"}}, Runs: []*Task{{ID: "r", Status: "interrupted", CanResume: true}}}
	a.sessions[s.ID] = s
	for _, heard := range []string{"你好", "不要清理全部跟进", "怎么清理全部跟进", "清理跟进"} {
		a.clearAssistantFollowups(context.Background(), map[string]any{"all": true}, heard)
		if !s.FollowedByAssistant {
			t.Fatalf("unauthorized mutation: %s", heard)
		}
	}
	result := a.execAssistantTool(asstToolCall("clear_followups", `{"all":true}`), &assistantDecision{}, "你把你跟进的这部分都清理一下吧")
	var out struct {
		OK    bool `json:"ok"`
		Count int  `json:"count"`
	}
	if json.Unmarshal([]byte(result), &out) != nil || !out.OK || out.Count != 1 {
		t.Fatal(result)
	}
	if s.FollowedByAssistant || s.Runs[0].Status != "interrupted" || !s.Runs[0].CanResume || len(s.Messages) != 1 || s.FollowNote != "keep note" {
		t.Fatal("task or message altered")
	}
	raw, err := os.ReadFile(SessionPath(a.dataPath, s.ID, sessionBucketFor(s)))
	if err != nil {
		t.Fatal(err)
	}
	var saved Session
	if json.Unmarshal(raw, &saved) != nil || saved.FollowedByAssistant {
		t.Fatal("follow flag not persisted")
	}
	if len(a.assistantWorkLedgerLocked()) != 0 {
		t.Fatal("ledger still followed")
	}
	if _, err := a.followSessionTool("#901", "restored"); err != nil || !s.FollowedByAssistant {
		t.Fatal("restore failed", err)
	}
}

func TestAssistantClearFollowupsTargetsCurrentExplicitReferences(t *testing.T) {
	a := testApp(t)
	a.sessions["one"] = &Session{ID: "one", Number: 901, FollowedByAssistant: true}
	a.sessions["two"] = &Session{ID: "two", Number: 902, FollowedByAssistant: true}
	a.clearAssistantFollowups(context.Background(), map[string]any{"all": true, "ref": "#902"}, "把 #901 的跟进都清理一下")
	if a.sessions["one"].FollowedByAssistant || !a.sessions["two"].FollowedByAssistant {
		t.Fatal("ignored current authorization scope")
	}
}
