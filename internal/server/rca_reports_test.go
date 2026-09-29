package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRCAValidWorkflowPhase 校验后端工作流阶段白名单接受 "problem-solving"。
func TestRCAValidWorkflowPhase(t *testing.T) {
	if !validWorkflowPhase("problem-solving") {
		t.Fatal("validWorkflowPhase(\"problem-solving\") = false, want true")
	}
	for _, ok := range []string{"", "auto", "requirement", "design", "implementation", "verify"} {
		if !validWorkflowPhase(ok) {
			t.Errorf("validWorkflowPhase(%q) = false, want true", ok)
		}
	}
	for _, bad := range []string{"problem_solving", "problemsolving", "RCA", "problem-solving ", "rca", "verify2"} {
		if validWorkflowPhase(bad) {
			t.Errorf("validWorkflowPhase(%q) = true, want false", bad)
		}
	}
}

// TestRCANumberingAndIndex 在 t.TempDir() 里模拟 createDoc 的产物：
// 写入两个 RCA-*.md，跑 nextDocNumber + rewriteDocIndex，再用 ParseRCAIndex 回读。
func TestRCANumberingAndIndex(t *testing.T) {
	dir := t.TempDir()
	reports := map[string]string{
		"RCA-001-登录超时.md": "# RCA-001 · 登录超时\n\n- 编号：RCA-001\n- 创建时间：2026-09-29 10:00:00\n- 状态：待评审\n- 关联：draw.io: login-timeout.drawio\n",
		"RCA-002-支付重试.md": "# RCA-002 · 支付重试\n\n- 编号：RCA-002\n- 创建时间：2026-09-30 11:30:00\n- 状态：已确认\n- 关联：draw.io: retry.drawio.svg\n",
	}
	for name, body := range reports {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}
	// 编号连续性：已有 001/002，下一号应为 003
	if n := nextDocNumber(dir, "RCA"); n != 3 {
		t.Fatalf("nextDocNumber = %d, want 3", n)
	}
	// 索引重写
	if err := rewriteDocIndex(dir, "RCA", "problem-reports-index.md", "问题解决报告索引"); err != nil {
		t.Fatal(err)
	}
	idx, err := os.ReadFile(filepath.Join(dir, "problem-reports-index.md"))
	if err != nil {
		t.Fatal(err)
	}
	entries := ParseRCAIndex(string(idx))
	if len(entries) != 2 {
		t.Fatalf("parsed %d index rows, want 2; body=\n%s", len(entries), string(idx))
	}
	// 按编号升序
	if entries[0].ID != "RCA-001" || entries[1].ID != "RCA-002" {
		t.Fatalf("index order wrong: %+v", entries)
	}
	if entries[0].Name != "登录超时" || entries[1].Name != "支付重试" {
		t.Fatalf("index names wrong: %+v", entries)
	}
	if entries[0].Status != "待评审" || entries[1].Status != "已确认" {
		t.Fatalf("index status wrong: %+v", entries)
	}
	if !strings.Contains(entries[1].Related, "retry.drawio.svg") {
		t.Fatalf("index related for RCA-002 wrong: %q", entries[1].Related)
	}
	// 列表辅助函数
	files, err := rcaListReportFiles(dir)
	if err != nil || len(files) != 2 {
		t.Fatalf("rcaListReportFiles = %v, err=%v", files, err)
	}
}

func rcaFixtureReport() string {
	return "# RCA-001 · 登录超时\n\n" +
		"- 编号：RCA-001\n- 创建时间：2026-09-29 10:00:00\n- 状态：待评审\n- 关联：draw.io: login-timeout.drawio\n\n" +
		"## 问题\n用户登录偶发 504。\n\n" +
		"## 背景\n高峰时段出现。\n\n" +
		"## 排查方向\n查网关超时与下游连接池。\n\n" +
		"## RCA 图\nlogin-timeout.drawio\n\n" +
		"## 测试\n压测复现。\n\n" +
		"## 结论\n连接池耗尽。\n\n" +
		"## 建议\n调大连接池并加限流。\n"
}

// TestRCADiagramExistence 校验 .drawio 存在性，以及 .drawio.svg/.png 导出图缺失的关联修正。
func TestRCADiagramExistence(t *testing.T) {
	root := t.TempDir()
	// 存在的源图
	if err := os.WriteFile(filepath.Join(root, "login-timeout.drawio"), []byte("<mxfile/>"), 0644); err != nil {
		t.Fatal(err)
	}

	// 1) 直接引用 .drawio：存在，无导出引用
	ok, exp, resolved, err := ValidateRCADiagramExists(root, "login-timeout.drawio")
	if err != nil || !ok {
		t.Fatalf("existing drawio: ok=%v exp=%v resolved=%q err=%v", ok, exp, resolved, err)
	}
	if resolved != "login-timeout.drawio" || exp != nil {
		t.Fatalf("unexpected resolve: resolved=%q exp=%v", resolved, exp)
	}

	// 2) 引用 .drawio.svg 导出：源图存在，但导出文件缺失 → exportExists=false
	ok, exp, resolved, err = ValidateRCADiagramExists(root, "login-timeout.drawio.svg")
	if err != nil || !ok {
		t.Fatalf("export ref resolve: ok=%v err=%v", ok, err)
	}
	if resolved != "login-timeout.drawio" {
		t.Fatalf("export resolve should strip .svg, got %q", resolved)
	}
	if exp == nil || *exp != false {
		t.Fatalf("expected exportExists=false (svg missing), got %v", exp)
	}

	// 3) 补上导出图后：exportExists=true
	if err := os.WriteFile(filepath.Join(root, "login-timeout.drawio.svg"), []byte("<svg/>"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, exp2, _, err := ValidateRCADiagramExists(root, "login-timeout.drawio.svg"); err != nil || exp2 == nil || *exp2 != true {
		t.Fatalf("after writing svg, exportExists should be true, got %v err=%v", exp2, err)
	}

	// 4) 完全不存在的 drawio → ok=false
	ok, _, _, err = ValidateRCADiagramExists(root, "nope.drawio")
	if err != nil || ok {
		t.Fatalf("missing drawio: ok=%v err=%v, want ok=false", ok, err)
	}

	// 5) 路径逃逸被拒绝
	if _, _, _, err := ValidateRCADiagramExists(root, "../etc/passwd"); err == nil {
		t.Fatal("path escape should be rejected")
	}

	// 6) ResolveRCADiagram 纯函数
	if d, e := ResolveRCADiagram("a.drawio.png"); d != "a.drawio" || e != "a.drawio.png" {
		t.Fatalf("ResolveRCADiagram png = %q,%q", d, e)
	}
	if d, e := ResolveRCADiagram("a.drawio"); d != "a.drawio" || e != "" {
		t.Fatalf("ResolveRCADiagram drawio = %q,%q", d, e)
	}
}

// TestRCARequiredSections 校验必备二级标题完整性。
func TestRCARequiredSections(t *testing.T) {
	if miss := CheckRCAReportSections(rcaFixtureReport()); len(miss) != 0 {
		t.Fatalf("complete report should have no missing sections, got %v", miss)
	}
	// 删掉两个必备小节
	bad := strings.Replace(rcaFixtureReport(), "## 测试\n压测复现。\n\n", "", 1)
	bad = strings.Replace(bad, "## 建议\n调大连接池并加限流。\n", "", 1)
	miss := CheckRCAReportSections(bad)
	if len(miss) != 2 {
		t.Fatalf("want 2 missing sections, got %v", miss)
	}
	for _, want := range []string{"测试", "建议"} {
		found := false
		for _, m := range miss {
			if m == want {
				found = true
			}
		}
		if !found {
			t.Fatalf("expected missing %q in %v", want, miss)
		}
	}
}

// TestExtractRCADiagramPath 校验从报告正文提取 drawio 路径。
func TestExtractRCADiagramPath(t *testing.T) {
	if got := ExtractRCADiagramPath(rcaFixtureReport()); got != "login-timeout.drawio" {
		t.Fatalf("ExtractRCADiagramPath = %q", got)
	}
	// 仅靠 ## RCA 图 小节
	onlySec := "# x\n\n## RCA 图\nfallback.drawio\n\n## 问题\nhi\n"
	if got := ExtractRCADiagramPath(onlySec); got != "fallback.drawio" {
		t.Fatalf("fallback extract = %q", got)
	}
	if got := ExtractRCADiagramPath("# nothing\n"); got != "" {
		t.Fatalf("empty extract = %q", got)
	}
}
