package server

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestApprovalPurposeForFileProposalAndReceipt(t *testing.T) {
	a := testApp(t)
	docs := bindProposalDocs(t, a, "purpose-docs")
	s, task := documentProposalTask(a)
	task.AutoReview = false
	purpose := map[string]string{"goal": "生成运维说明", "reason": "交付用户要求的处理流程"}
	for _, source := range []string{"", systemDocsSource} {
		args, _ := json.Marshal(map[string]any{"source": source, "path": "purpose.md", "content": "# Instructions\n", approvalPurposeField: purpose})
		if got := a.executeToolCall(context.Background(), readCall("write_file", string(args)), task, nil); !strings.Contains(got, "提案") {
			t.Fatal(got)
		}
	}
	if len(task.Files) != 2 {
		t.Fatal(task.Files)
	}
	for _, file := range task.Files {
		if file.ApprovalPurpose == nil || file.ApprovalPurpose.Goal != purpose["goal"] || file.ApprovalPurpose.Reason != purpose["reason"] {
			t.Fatalf("missing purpose for %q", proposalDisplayPath(file))
		}
	}
	for _, dir := range []string{docs, a.workPath} {
		if _, err := os.Stat(filepath.Join(dir, "purpose.md")); !os.IsNotExist(err) {
			t.Fatalf("a model purpose authorized a write: %v", err)
		}
	}
	original := approvalFilesFingerprint(task.Files)
	noPurpose := append([]Change(nil), task.Files...)
	for i := range noPurpose {
		noPurpose[i].ApprovalPurpose = nil
	}
	if original != approvalFilesFingerprint(noPurpose) {
		t.Fatal("explanation changed exact authorization fingerprints")
	}
	requireStatus(t, request(a, "POST", "/api/sessions/"+s.ID+"/runs/"+task.ID+"/apply", nil), 200)
	raw, _ := json.Marshal(s)
	var restored Session
	if err := json.Unmarshal(raw, &restored); err != nil {
		t.Fatal(err)
	}
	records := restored.Runs[0].ApprovalReviews
	last := records[len(records)-1]
	if last.Fingerprint != approvalFingerprint(original) || len(last.FilePurposes) != 2 ||
		last.FilePurposes[0].Path == last.FilePurposes[1].Path || last.FilePurposes[0].Purpose.Goal != purpose["goal"] {
		t.Fatalf("incorrect persisted receipt: %+v", last)
	}
}

func TestApprovalPurposeIsBoundedAndDoesNotInventLegacyText(t *testing.T) {
	purpose := parseApprovalPurpose(map[string]any{"goal": " \u202e 备份\t 输出 ", "reason": strings.Repeat("需", 500)})
	if purpose.Goal != "备份 输出" || utf8.RuneCountInString(purpose.Reason) != approvalPurposeTextLimit {
		t.Fatalf("unexpected normalization: %+v", purpose)
	}
	if parseApprovalPurpose(map[string]any{"goal": "目的只有一半"}) != nil {
		t.Fatal("incomplete explanation was invented")
	}
	var old struct {
		Files   []Change         `json:"files"`
		Reviews []ApprovalReview `json:"reviews"`
	}
	if err := json.Unmarshal([]byte(`{"files":[{"path":"legacy.md","content":"old"}],"reviews":[{"command":"mkdir reports","status":"confirmed"}]}`), &old); err != nil {
		t.Fatal(err)
	}
	if old.Files[0].ApprovalPurpose != nil || old.Reviews[0].Purpose != nil {
		t.Fatal("old records must not acquire a fabricated explanation")
	}
}
