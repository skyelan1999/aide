package server

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func TestKnowledgeConclusionComparisonWire(t *testing.T) {
	before := knowledgeTimeSnapshot{ID: "old", Graph: knowledgeGraph{Workspace: "w", Nodes: []knowledgeNode{{ID: "F", Kind: "file", Root: "workspace", Path: "report.md", Text: "# 报告\n## 结论\n采用方案甲。\n"}}}}
	after := knowledgeTimeSnapshot{ID: "new", Graph: knowledgeGraph{Workspace: "w", Nodes: []knowledgeNode{{ID: "F", Kind: "file", Root: "workspace", Path: "report.md", Text: "# 报告\n## 结论\n采用方案乙。\n"}}}}
	b, err := json.Marshal(compareKnowledgeTime(before, after))
	if err != nil {
		t.Fatal(err)
	}
	var data map[string]json.RawMessage
	if err := json.Unmarshal(b, &data); err != nil {
		t.Fatal(err)
	}
	if _, ok := data["conclusions"]; !ok {
		t.Fatal("timeline only reports node changes; explicit conclusion changes are missing")
	}
}

func conclusionDocument(text string) knowledgeNode {
	return knowledgeNode{ID: "F", Kind: "file", Root: "workspace", Region: "workspace", Path: "report.md", Text: text, Size: int64(len(text)), ReadState: "ready"}
}

func TestKnowledgeConclusionExplicitCapture(t *testing.T) {
	text := "# 报告\n## 结论 ###\n采用方案甲。\n### 依据\n[来源](source.md)\n## 后续\n其他内容。\n"
	s := extractKnowledgeConclusions(text, true)
	if len(s.Items) != 1 || s.Partial {
		t.Fatalf("%+v", s)
	}
	c := s.Items[0]
	if c.Heading != "结论" || c.Line != 3 || c.EndLine != 5 || c.HeadingLine != 2 || c.Text != "采用方案甲。\n### 依据\n[来源](source.md)" || c.TextHash != hash([]byte(c.Text)) || c.SourceHash != hash([]byte(text)) || len(c.HeadingPath) != 2 {
		t.Fatalf("lost exact excerpt/provenance: %+v", c)
	}
	setext := extractKnowledgeConclusions("结论\n----\n中文结论。\n", true)
	if len(setext.Items) != 1 || setext.Items[0].Line != 3 || setext.Items[0].HeadingLine != 1 {
		t.Fatal(setext)
	}
	bom := extractKnowledgeConclusions("\uFEFF##\t结论\t###\r\n中文结论。\r\n", true)
	if len(bom.Items) != 1 || bom.Items[0].Text != "中文结论。" || bom.Items[0].Line != 2 {
		t.Fatalf("BOM/CRLF/tab source failed: %+v", bom)
	}
	for _, text := range []string{
		"```md\n## 结论\n伪结论\n```\n", "~~~\n## 结论\n~~~\n",
		"> ## 结论\n> 引用内容\n", "    ## 结论\n    代码内容\n",
		"<!--\n## 结论\n-->\n", "<script>\n## 结论\n</script>\n",
		"不是单行标题\n结论\n---\n正文", "##结论\n缺少空格\n", "## 尚无结论\n不属于明确结论标题\n",
	} {
		if got := extractKnowledgeConclusions(text, true); len(got.Items) != 0 {
			t.Fatalf("non-section became conclusion: %q %+v", text, got)
		}
	}
}

func TestKnowledgeConclusionChangesAreScoped(t *testing.T) {
	old := extractKnowledgeConclusions("前言甲\n\n## 结论\n采用方案甲。\n", true)
	unchanged := extractKnowledgeConclusions("前言乙\n\n\n## 结论\n采用方案甲。\n", true)
	if changes := compareKnowledgeConclusionSets(&old, &unchanged); len(changes) != 0 {
		t.Fatalf("position/outside text changed the conclusion: %+v", changes)
	}
	next := extractKnowledgeConclusions("## 结论\n采用方案乙。\n", true)
	changes := compareKnowledgeConclusionSets(&old, &next)
	if len(changes) != 1 || changes[0].Kind != "changed" || changes[0].Before.Text != "采用方案甲。" || changes[0].After.Text != "采用方案乙。" {
		t.Fatal(changes)
	}
	partial := extractKnowledgeConclusions("前言仍在，结论未覆盖。", false)
	if got := compareKnowledgeConclusionSets(&old, &partial); len(got) != 1 || got[0].Kind != "unknown" {
		t.Fatalf("partial capture inferred retraction: %+v", got)
	}
	if got := compareKnowledgeConclusionSets(nil, &next); len(got) != 1 || got[0].Kind != "observed" {
		t.Fatalf("legacy missing capture inferred new claim: %+v", got)
	}
	commented := extractKnowledgeConclusions("## 结论 <!--注释-->\n采用方案甲。", true)
	if got := compareKnowledgeConclusionSets(&old, &commented); len(got) != 1 || got[0].Kind != "unknown" {
		t.Fatalf("unsupported heading format became a retraction: %+v", got)
	}
	plain := extractKnowledgeConclusions("没有结论段。", true)
	if got := compareKnowledgeConclusionSets(&plain, &partial); len(got) != 0 {
		t.Fatal(got)
	}
	duplicate := extractKnowledgeConclusions("## 结论\n甲\n## 结论\n乙\n", true)
	if len(duplicate.Items) != 2 || !duplicate.Items[0].Ambiguous || !duplicate.Items[1].Ambiguous {
		t.Fatal(duplicate)
	}
	for _, change := range compareKnowledgeConclusionSets(&duplicate, &next) {
		if change.Kind == "changed" || change.Kind == "removed" {
			t.Fatal("duplicate topics were aligned", change)
		}
	}
}

func TestKnowledgeConclusionBounds(t *testing.T) {
	s := extractKnowledgeConclusions("## 结论\n"+strings.Repeat("中文", 5000), true)
	if len(s.Items) != 1 || !s.Items[0].Partial || len(s.Items[0].Text) > knowledgeConclusionTextBytes || !utf8.ValidString(s.Items[0].Text) {
		t.Fatal("quote budget invalid", s)
	}
	s = extractKnowledgeConclusions(strings.Repeat("x", knowledgeConclusionScanBytes)+"\n## 结论\n不可见", true)
	if !s.Partial || len(s.Items) != 0 {
		t.Fatal("read beyond scan budget", s)
	}
	s = extractKnowledgeConclusions(strings.Repeat("## 结论\n文字\n", 20), true)
	if len(s.Items) != knowledgeConclusionItems || !s.Partial {
		t.Fatal("item budget invalid", s)
	}
	s = extractKnowledgeConclusions("## 结论\n"+string([]byte{255}), true)
	if !s.Partial || len(s.Items) != 0 {
		t.Fatal("invalid UTF8 quoted", s)
	}
}

func TestKnowledgeConclusionHistoricalAPI(t *testing.T) {
	a := testApp(t)
	workspace, _ := a.knowledgeUpdatesWorkspace()
	store := knowledgeTimeStore{Version: 1, Workspace: workspace, Snapshots: []knowledgeTimeSnapshot{}}
	for i, text := range []string{"## 结论\n采用方案甲。\n", "## 结论\n采用方案乙。\n"} {
		var err error
		store, _, err = appendKnowledgeTime(store, knowledgeGraph{Workspace: workspace, Nodes: []knowledgeNode{conclusionDocument(text)}}, time.Unix(int64(i+1), 0))
		if err != nil {
			t.Fatal(err)
		}
	}
	file := knowledgeTimeFile(a.dataPath, workspace, false)
	if err := saveKnowledgeTime(file, store, knowledgeTimeKey(a.token)); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(file)
	if err != nil || strings.Contains(string(raw), "采用方案") {
		t.Fatal("conclusion plaintext on disk", err)
	}
	loaded, err := loadKnowledgeTime(file, workspace, false, knowledgeTimeKey(a.token))
	if err != nil {
		t.Fatal(err)
	}
	w := request(a, "POST", "/api/knowledge-map/timeline/compare?workspace="+workspace, map[string]string{"before": loaded.Snapshots[0].ID, "after": loaded.Snapshots[1].ID})
	requireStatus(t, w, 200)
	var result knowledgeTimeComparison
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Conclusions.Changes) != 1 || result.Conclusions.Changes[0].Change.Kind != "changed" || result.Conclusions.AIState != "not_captured" || result.Conclusions.EvidenceState != "saved_snapshot_explicit_references" {
		t.Fatalf("document phase missing/overclaimed: %+v", result.Conclusions)
	}
	requireStatus(t, request(a, "POST", "/api/knowledge-map/timeline/compare?workspace=other", map[string]string{"before": loaded.Snapshots[0].ID, "after": loaded.Snapshots[1].ID}), 409)
	a.mu.Lock()
	a.settings.UserPasswordHash = "configured"
	a.mu.Unlock()
	if _, err := a.writeLockState(true); err != nil {
		t.Fatal(err)
	}
	requireStatus(t, request(a, "POST", "/api/knowledge-map/timeline/compare?workspace="+workspace, map[string]string{"before": loaded.Snapshots[0].ID, "after": loaded.Snapshots[1].ID}), 423)
}

func TestKnowledgeConclusionSourceIdentity(t *testing.T) {
	old := conclusionDocument("## 结论\n甲\n")
	next := conclusionDocument("## 结论\n乙\n")
	next.Root = "context"
	result := compareKnowledgeConclusions(knowledgeGraph{Nodes: []knowledgeNode{old}}, knowledgeGraph{Nodes: []knowledgeNode{next}})
	if len(result.Changes) != 1 || result.Changes[0].Change.Kind != "unknown" || result.Changes[0].Change.Reason != "source_identity_ambiguous" {
		t.Fatal(result)
	}
	next.Root = old.Root
	result = compareKnowledgeConclusions(knowledgeGraph{Nodes: []knowledgeNode{old}}, knowledgeGraph{Nodes: []knowledgeNode{next}, Sources: []knowledgeSource{{ID: "workspace", Region: "workspace", State: "unavailable"}}})
	if len(result.Changes) != 1 || result.Changes[0].Change.Kind != "unknown" {
		t.Fatalf("unavailable source claimed a new conclusion: %+v", result)
	}
}

func TestKnowledgeConclusionActualIndexHistory(t *testing.T) {
	a := testApp(t)
	workspace, epoch := a.knowledgeUpdatesWorkspace()
	file := filepath.Join(a.workPath, "report.md")
	for _, text := range []string{"# 报告\n## 结论\n采用方案甲。\n", "# 报告\n## 结论\n采用方案乙。\n"} {
		if err := os.WriteFile(file, []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
		graph := a.knowledgeSnapshotModeCached(context.Background(), false, nil)
		if err := a.recordKnowledgeTime(graph, false, epoch); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	store, err := loadKnowledgeTime(knowledgeTimeFile(a.dataPath, workspace, false), workspace, false, knowledgeTimeKey(a.token))
	if err != nil {
		t.Fatal(err)
	}
	if len(store.Snapshots) != 2 {
		t.Fatalf("unexpected history observations %d", len(store.Snapshots))
	}
	w := request(a, "POST", "/api/knowledge-map/timeline/compare?workspace="+workspace, map[string]string{"before": store.Snapshots[0].ID, "after": store.Snapshots[1].ID})
	requireStatus(t, w, 200)
	var result knowledgeTimeComparison
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, change := range result.Conclusions.Changes {
		if change.Path == "report.md" {
			found = true
			if change.Change.Kind != "changed" || change.Change.Before.Text != "采用方案甲。" || change.Change.After.Text != "采用方案乙。" || change.Change.Before.Line != 3 || change.Change.After.Line != 3 {
				t.Fatalf("actual saved source provenance lost: %+v", change)
			}
		}
	}
	if !found {
		t.Fatalf("actual index/history/API missed conclusion: %+v", result.Conclusions)
	}
}

func TestKnowledgeConclusionNoMixedSessionAttribution(t *testing.T) {
	before := knowledgeGraph{Nodes: []knowledgeNode{{ID: "S", Kind: "session", Text: "## 结论\n用户文字或混合摘要"}}}
	after := knowledgeGraph{Nodes: []knowledgeNode{{ID: "S", Kind: "session", Text: "## 结论\n工具文字或混合摘要"}}}
	if got := compareKnowledgeConclusions(before, after); len(got.Changes) != 0 || got.AIState != "not_captured" {
		t.Fatalf("mixed text became AI conclusion: %+v", got)
	}
}
