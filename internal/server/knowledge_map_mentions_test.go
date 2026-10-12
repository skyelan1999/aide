package server

import "testing"

func TestKnowledgeMentionLocationUsesUTF8LineAndColumn(t *testing.T) {
	text := "# 报告\n参考 [F0123456789ab] 继续。\n"
	line, column, evidence := knowledgeMentionLocation(text, "[F0123456789ab]")
	if line != 2 || column != 4 {
		t.Fatalf("locator = %d:%d, want 2:4", line, column)
	}
	if evidence != "显式引用：[F0123456789ab]" {
		t.Fatalf("evidence = %q", evidence)
	}
}

func TestKnowledgeMentionLocationOmitsMissingToken(t *testing.T) {
	line, column, evidence := knowledgeMentionLocation("report without a link", "other.md")
	if line != 0 || column != 0 || evidence != "" {
		t.Fatalf("unexpected locator for absent token: %d:%d %q", line, column, evidence)
	}
}
