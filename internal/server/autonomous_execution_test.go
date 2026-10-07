package server

import (
	"strings"
	"testing"
)

func TestAutonomousExecutionPolicy(t *testing.T) {
	for _, mode := range []string{"chat", "workflow"} {
		instruction := runtimeCapabilityInstruction(nil, "继续，你自己定", mode)
		for _, fragment := range []string{"授权在任务范围内持续有效", "必须调用 ask_user", "不绕过审批", "不要为了设计/计划/测试阶段结束"} {
			if !strings.Contains(instruction, fragment) {
				t.Fatalf("%s lacks %s", mode, fragment)
			}
		}
	}
	if strings.Contains(autoModePrompt, "测试结论在动手/定稿前") {
		t.Fatal("workflow still requires unnecessary phase approval")
	}
}

func TestClarificationOptionsRemainAnswerable(t *testing.T) {
	for _, tc := range []struct {
		input, output string
		count         int
	}{
		{"single", "input", 0}, {"multi", "input", 0}, {"confirm", "confirm", 2}, {"invalid", "input", 0},
	} {
		kind, choices := normalizeClarificationOptions(tc.input, nil)
		if kind != tc.output || len(choices) != tc.count {
			t.Fatalf("%s => %s %v", tc.input, kind, choices)
		}
	}
	kind, choices := normalizeClarificationOptions("single", []string{"方案 A", "方案 B"})
	if kind != "single" || len(choices) != 2 {
		t.Fatal("valid choices were changed")
	}
}

func TestClarificationCardCorrection(t *testing.T) {
	tools := []any{map[string]any{"function": map[string]any{"name": "ask_user"}}}
	for _, out := range []string{"要我继续读吗？", "请补充最关键的一项：报告主题", "你确认后我立刻执行"} {
		if !needsClarificationCard(out, tools) {
			t.Fatalf("missing correction: %s", out)
		}
	}
	if needsClarificationCard("已读取三个页面。参数尚未公开。", tools) || needsClarificationCard("要我继续吗？", nil) {
		t.Fatal("unexpected correction")
	}
}

func TestAutonomousResearchContinuation(t *testing.T) {
	tools := []any{map[string]any{"function": map[string]any{"name": "browser_read"}}}
	if !needsResearchContinuation("下一步（我直接继续，不再问）\n我这就用 fromPage=89 读取剩余部分。", tools) {
		t.Fatal("immediate read commitment was missed")
	}
	for _, out := range []string{"建议下一步读取第89页。", "已完成读取，未公开桨规格。", "用户可自行读取PDF。"} {
		if needsResearchContinuation(out, tools) {
			t.Fatal("recommendation must not execute: " + out)
		}
	}
	if needsResearchContinuation("我这就用 fromPage=89 读取剩余部分。", nil) {
		t.Fatal("missing tool must not continue")
	}
}

func TestAutonomousExplicitPDFReadRequiresAttempt(t *testing.T) {
	tools := []any{map[string]any{"function": map[string]any{"name": "browser_read"}}}
	if !needsWebReadAttempt("请用browser_read fromPage=61补读PDF并核实", "chat", tools, nil) {
		t.Fatal("explicit PDF read must require tool evidence")
	}
	if needsWebReadAttempt("编写代码实现PDF读取", "chat", tools, nil) {
		t.Fatal("implementation is not a research request")
	}
}
