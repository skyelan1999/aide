package server

import "testing"

func TestShellApprovalExecutionIntentKeepsUnknownsExplicit(t *testing.T) {
	intent := shellApprovalExecutionIntentFor("/workspace/project", "workspace-write", "local")
	if intent.Action == "" || intent.WorkingDirectory != "/workspace/project" || intent.TargetScopeWarning == "" || intent.Impact == "" {
		t.Fatalf("incomplete shell intent: %+v", intent)
	}
	if intent.SandboxMode != "workspace-write" || intent.WorkspaceMode != "local" || intent.ContainerIsolationNote == "" {
		t.Fatalf("execution environment omitted from intent: %+v", intent)
	}
	if shellApprovalExecutionIntent("/workspace/project").SandboxMode != "" {
		t.Fatal("compatibility intent unexpectedly claims environment facts")
	}
}

func TestShellApprovalIntentDescribesOnlyProvableGitRemoval(t *testing.T) {
	intent := shellApprovalExecutionIntentForCommand("git rm -- docs/old.md", "/workspace/project", "workspace-write", "local")
	if len(intent.CommandSteps) != 1 || intent.CommandSteps[0] != "git rm" {
		t.Fatalf("command not summarized: %+v", intent)
	}
	if len(intent.Targets) != 1 || intent.Targets[0].Path != "docs/old.md" || intent.ParseStatus == "" {
		t.Fatalf("literal target not summarized: %+v", intent)
	}
	unknown := shellApprovalExecutionIntentForCommand("git rm -- $(cat target)", "/workspace/project", "workspace-write", "local")
	if len(unknown.Targets) != 0 || unknown.ParseStatus == "" || unknown.TargetScopeWarning == "" {
		t.Fatalf("dynamic target was presented as known: %+v", unknown)
	}
}

func TestFileApprovalExecutionIntentMatchesPendingTargets(t *testing.T) {
	files := []Change{
		{Path: "new.md", Content: "new"},
		{Path: "existing.md", Content: "updated", Before: "old", BaseHash: "sha"},
		{Path: "done.md", Content: "done", Applied: true},
	}
	intent := fileApprovalExecutionIntent(files)
	if len(intent.Targets) != 2 {
		t.Fatalf("intent includes applied or omits pending files: %+v", intent.Targets)
	}
	if intent.Targets[0].Path != "new.md" || intent.Targets[0].Action != "创建新文件" {
		t.Fatalf("wrong new-file intent: %+v", intent.Targets[0])
	}
	if intent.Targets[1].Path != "existing.md" || intent.Targets[1].Action != "写入/修改文件" {
		t.Fatalf("wrong modification intent: %+v", intent.Targets[1])
	}
	if intent.Impact == "" {
		t.Fatal("missing write impact")
	}
}

func TestApprovalCommandNeedsManualUsesParsedExecutable(t *testing.T) {
	for _, command := range []string{"rm file.txt", "env X=1 sudo id", "echo safe && chmod 600 file", "git rm -r -- old-dir", "git rm -- .git/config"} {
		if reason, blocked := approvalCommandNeedsManual(command); !blocked || reason == "" {
			t.Errorf("dangerous command was not blocked: %q", command)
		}
	}
	for _, command := range []string{`echo "rm is a tool name"`, "cat ./chown-notes.txt", "printf 'sudo is prohibited'", "git status -- .gitignore", "git rm -- docs/old.md"} {
		if reason, blocked := approvalCommandNeedsManual(command); blocked {
			t.Errorf("safe parsed command was falsely blocked: %q (%s)", command, reason)
		}
	}
	if reason, blocked := approvalCommandNeedsManual("echo safe; rm file.txt"); !blocked || reason == "" {
		t.Fatalf("dangerous token in unsupported syntax was not kept manual: %q %v", reason, blocked)
	}
}

func TestApprovalGitRmTargetIsSingleSafeRelativePath(t *testing.T) {
	for _, command := range []string{"git rm file.txt", "git rm -- 'docs/old file.md'"} {
		if _, ok := approvalGitRmTarget(command); !ok {
			t.Errorf("valid literal Git removal rejected: %q", command)
		}
	}
	for _, command := range []string{"git rm -r -- dir", "git rm -- .git/config", "git rm -- ../outside", "git rm -- a b", "git rm -- file && git status", "git rm -- $(cat target)"} {
		if target, ok := approvalGitRmTarget(command); ok {
			t.Errorf("unsafe or ambiguous Git removal accepted: %q -> %q", command, target)
		}
	}
}
