package server

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestIssue158FindingRequiresWholeValueInOriginalResult(t *testing.T) {
	cases := []struct {
		name, result, quote, value string
		want                       bool
	}{
		{"digit-fragment", "diameter: 150 mm", "diameter: 150 mm", "5", false},
		{"narrow-quote-fragment", "diameter: 150 mm", "5", "5", false},
		{"decimal-tail", "value=1.5 mm", "5", "5", false},
		{"decimal-prefix", "value=1.50 mm", "1.5", "1.5", false},
		{"decimal-dot-prefix", "value=1.50 mm", "1.", "1.", false},
		{"negative-sign", "offset=-5 mm", "5", "5", false},
		{"positive-sign", "offset=+5 mm", "5", "5", false},
		{"spaced-sign", "offset=-   5 mm", "5", "5", false},
		{"exponent-tail", "tolerance=1e-5 mm", "-5", "-5", false},
		{"exponent-prefix", "tolerance=1e-5 mm", "1", "1", false},
		{"group-comma-tail", "length=1,500 mm", "500", "500", false},
		{"group-comma-prefix", "length=1,500 mm", "1", "1", false},
		{"group-space-tail", "length=1 500 mm", "500", "500", false},
		{"group-space-prefix", "length=1 500 mm", "1", "1", false},
		{"group-narrow-space", "length=1\u202f500 mm", "500", "500", false},
		{"identifier-prefix", "revision=rev5", "5", "5", false},
		{"identifier-suffix", "revision=5_rev", "5", "5", false},
		{"text-fragment", "material=stainlesssteel", "steel", "steel", false},
		{"material-grade-fragment", "material=Q235B", "Q235", "Q235", false},
		{"unit-phrase-fragment", "length=150 mm", "5 mm", "5 mm", false},
		{"hex-prefix", "register=0x15", "0", "0", false},
		{"unsigned-infinity", "limit=-∞", "∞", "∞", false},
		{"exact-integer", "diameter: 150 mm", "150 mm", "150", true},
		{"unit-adjacent", "diameter: 5mm", "5mm", "5", true},
		{"chinese-label", "直径150毫米", "150毫米", "150", true},
		{"exact-decimal", "diameter: 1.50 mm", "1.50 mm", "1.50", true},
		{"leading-decimal", "ratio=.5", ".5", ".5", true},
		{"exact-negative", "offset=-5 mm", "-5 mm", "-5", true},
		{"exact-plus", "offset=+5 mm", "+5 mm", "+5", true},
		{"math-minus-format", "offset=−5 mm", "−5 mm", "-5", true},
		{"exponent-format", "epsilon=1E−5", "1E−5", "1e-5", true},
		{"exact-group-comma", "length=1,500 mm", "1,500 mm", "1,500", true},
		{"exact-group-space", "length=1 500 mm", "1 500 mm", "1 500", true},
		{"comma-list-space", "values: 1, 5 mm", "5 mm", "5", true},
		{"sentence-period", "value is 5.", "5.", "5", true},
		{"exact-material", "材料为Q235钢", "Q235钢", "Q235", true},
		{"exact-hex", "register=0x15", "0x15", "0x15", true},
		{"large-integer", "id=9007199254740993", "9007199254740993", "9007199254740993", true},
		{"whole-unit-phrase", "length=150 mm", "150 mm", "150 mm", true},
		{"group-adjacent-unit", "length=1 500mm", "1 500mm", "1 500", true},
		{"tab-group-adjacent-unit", "length=1\t500mm", "1\t500mm", "1\t500", true},
		{"narrow-group-adjacent-unit", "length=1\u202f500mm", "1\u202f500mm", "1\u202f500", true},
		{"spaced-signed-literal", "offset=- 5mm", "- 5mm", "- 5", true},
		{"markdown-list", "- 5 mm", "5 mm", "5", true},
		{"markdown-list-negative", "- -5 mm", "-5 mm", "-5", true},
		{"doubled-sign", "offset=--5", "-5", "-5", false},
		{"exponent-digit-fragment", "tolerance=1e-50", "1e-5", "1e-5", false},
		{"signed-exponent-format", "offset=−1E−5 mm", "−1E−5 mm", "-1e-5", true},
		{"overlapping-quote", "zab ab ab", "ab ab", "ab ab", true},
		{"literal-quote-required", "diameter: 5 mm", "diameter = 5 mm", "5", false},
	}
	for _, kind := range []string{"source", "calculation"} {
		for _, tc := range cases {
			t.Run(kind+"/"+tc.name, func(t *testing.T) {
				a := &App{}
				task := &Task{ToolUses: []ToolUse{{Tool: "read_file", Result: tc.result}}}
				f := ResearchFinding{Name: "owned fixture", Kind: kind, Evidence: 1, Value: tc.value, Quote: tc.quote}
				raw, err := json.Marshal(f)
				if err != nil {
					t.Fatal(err)
				}
				out := a.recordResearchFinding(task, string(raw))
				accepted := len(task.ResearchFindings) == 1 && strings.HasPrefix(out, "参数已记录")
				if accepted != tc.want {
					t.Fatalf("whole-value policy accepted=%v want=%v: %s", accepted, tc.want, out)
				}
			})
		}
	}
}

func TestIssue158NarrowQuoteCanFindLaterWholeOccurrence(t *testing.T) {
	for _, suffix := range []string{"", "; 5 mm"} {
		a := &App{}
		task := &Task{ToolUses: []ToolUse{{Tool: "read_file", Result: strings.Repeat("5", 32<<10) + suffix}}}
		raw, _ := json.Marshal(ResearchFinding{Name: "owned long result", Kind: "source", Evidence: 1, Quote: "5", Value: "5"})
		out := a.recordResearchFinding(task, string(raw))
		if (len(task.ResearchFindings) == 1) != (suffix != "") {
			t.Fatal("narrow quote was not checked against its original numeric context", out)
		}
	}
}

func TestIssue158RejectedRevisionKeepsPriorFinding(t *testing.T) {
	a := &App{}
	old := ResearchFinding{Name: "diameter", Kind: "source", Evidence: 1, Quote: "diameter: 150 mm", Value: "150", Unit: "mm"}
	task := &Task{ToolUses: []ToolUse{{Tool: "read_file", Result: old.Quote}}, ResearchFindings: []ResearchFinding{old}}
	bad := old
	bad.Value = "5"
	raw, _ := json.Marshal(bad)
	if out := a.recordResearchFinding(task, string(raw)); !strings.HasPrefix(out, "错误") || len(task.ResearchFindings) != 1 || task.ResearchFindings[0] != old {
		t.Fatal("invalid revision replaced a valid finding", out)
	}
}

func TestIssue158PeriodicQuoteKeepsOriginalBoundaries(t *testing.T) {
	quote := strings.Repeat("steelX ", 200) + "steel"
	result := strings.Repeat("steelX ", 16<<10)
	for _, suffix := range []string{"", "steel"} {
		a := &App{}
		task := &Task{ToolUses: []ToolUse{{Tool: "read_file", Result: result + suffix}}}
		raw, _ := json.Marshal(ResearchFinding{Name: "owned periodic result", Kind: "source", Evidence: 1, Quote: quote, Value: "steel"})
		out := a.recordResearchFinding(task, string(raw))
		if (len(task.ResearchFindings) == 1) != (suffix != "") {
			t.Fatal("periodic quote lost original token boundaries or later whole occurrence", out)
		}
	}
}
