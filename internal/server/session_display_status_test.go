package server

import "testing"

func TestSessionDisplayStatusPrioritizesActiveRuns(t *testing.T) {
	tests := []struct {
		name string
		runs []*Task
		want string
	}{
		{name: "empty session", want: ""},
		{name: "latest terminal", runs: []*Task{{Status: "completed"}, {Status: "failed"}}, want: "failed"},
		{name: "running supersedes later failure", runs: []*Task{{Status: "running"}, {Status: "failed"}}, want: "running"},
		{name: "queued supersedes later failure", runs: []*Task{{Status: "queued"}, {Status: "failed"}}, want: "queued"},
		{name: "paused supersedes later terminal", runs: []*Task{{Status: "paused"}, {Status: "cancelled"}}, want: "paused"},
		{name: "waiting supersedes running", runs: []*Task{{Status: "awaiting_clarification"}, {Status: "running"}}, want: "awaiting_clarification"},
		{name: "latest waiting kind wins", runs: []*Task{{Status: "awaiting_approval"}, {Status: "awaiting_clarification"}, {Status: "running"}}, want: "awaiting_clarification"},
		{name: "approval supersedes later failure", runs: []*Task{{Status: "awaiting_approval"}, {Status: "failed"}}, want: "awaiting_approval"},
		{name: "nil runs are ignored", runs: []*Task{nil, {Status: "completed"}, nil}, want: "completed"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := sessionDisplayStatus(&Session{Runs: test.runs}); got != test.want {
				t.Fatalf("sessionDisplayStatus() = %q, want %q", got, test.want)
			}
		})
	}
	if got := sessionDisplayStatus(nil); got != "" {
		t.Fatalf("nil session status = %q", got)
	}
}
