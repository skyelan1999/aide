package server

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestResearchCoverageClaimScope(t *testing.T) {
	cases := []struct {
		name, out, claim string
		want             bool
	}{
		{"plain", "PDF文档全篇已读。", "全篇已读", true},
		{"historical", "此前不能说全篇已读。", "全篇已读", false},
		{"comma contrast", "此前不能说全篇已读，但现在可以说全篇已读。", "全篇已读", true},
		{"unpunctuated contrast", "此前不能说全篇已读但现在可以说全篇已读。", "全篇已读", true},
		{"different claims", "此前不能说全篇已读但现在全覆盖。", "全覆盖", true},
		{"two negatives", "不能说全篇已读，也不能说全篇已读。", "全篇已读", false},
		{"unrelated later negative", "PDF文档全篇已读，但没有检查其他渠道。", "全篇已读", true},
		{"suffix retraction", "“全篇已读”这一说法不准确。", "全篇已读", false},
		{"channel contrast", "此前不能说全部渠道已查尽，但现在全部渠道已查尽。", "全部渠道已查尽", true},
		{"negative implication", "部分阅读不等于全篇已读。", "全篇已读", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := affirmativeResearchClaim(tc.out, tc.claim); got != tc.want {
				t.Fatalf("claim classification=%v want %v", got, tc.want)
			}
		})
	}
	report := "PDF文档：此前不能说全篇已读，但现在可以说全篇已读。"
	receipt := func(last int) []ToolUse {
		return []ToolUse{{Tool: "read_pdf", Result: sourceReceipt(map[string]any{"url": "https://example.test/owned.pdf", "pages": 10, "fromPage": 1, "toPage": last, "lastPageComplete": true})}}
	}
	if !sourceCoverageCorrection(report, receipt(2)) {
		t.Fatal("incomplete real page receipts must require correction")
	}
	if sourceCoverageCorrection(report, receipt(10)) {
		t.Fatal("complete real page receipts must remain sufficient")
	}
	if !sourceCoverageCorrection(report, nil) {
		t.Fatal("a PDF coverage claim without receipts requires correction")
	}
}

func TestSystemLogQuotedCredentials(t *testing.T) {
	cases := []struct {
		name, line string
		forbidden  []string
	}{
		{"double assignment", `password="ownedalpha ownedbeta" status=ok`, []string{"ownedalpha", "ownedbeta"}},
		{"single assignment", `secret='ownedalpha ownedbeta' status=ok`, []string{"ownedalpha", "ownedbeta"}},
		{"json field", `{"password": "ownedalpha ownedbeta", "status":"ok"}`, []string{"ownedalpha", "ownedbeta"}},
		{"escaped quotes", `password="ownedalpha \"ownedquote\" ownedbeta" status=ok`, []string{"ownedalpha", "ownedquote", "ownedbeta"}},
		{"unterminated quote", `password="ownedalpha ownedbeta`, []string{"ownedalpha", "ownedbeta"}},
		{"multiple", `password="ownedalpha ownedbeta" api_key='ownedgamma owneddelta' status=ok`, []string{"ownedalpha", "ownedbeta", "ownedgamma", "owneddelta"}},
		{"unquoted", `password=ownedalpha status=ok`, []string{"ownedalpha"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := redactSystemLog(tc.line)
			for _, secret := range tc.forbidden {
				if strings.Contains(got, secret) {
					t.Fatal("credential fragment retained")
				}
			}
			if !strings.Contains(got, "[REDACTED]") {
				t.Fatal("missing redaction marker")
			}
			if strings.Contains(tc.line, "status=ok") && !strings.Contains(got, "status=ok") {
				t.Fatal("unrelated status text lost")
			}
		})
	}
	old := systemLogs
	systemLogs = &systemLogBuffer{}
	defer func() { systemLogs = old }()
	systemLogs.add(`provider error: password="ownedalpha ownedbeta" status=ok`)
	for _, suffix := range []string{"", "?download=1"} {
		res := httptest.NewRecorder()
		(&App{}).systemLogsHandler(res, httptest.NewRequest("GET", "/api/system-logs"+suffix, nil))
		if res.Code != 200 {
			t.Fatal("log endpoint failed")
		}
		if strings.Contains(res.Body.String(), "ownedalpha") || strings.Contains(res.Body.String(), "ownedbeta") {
			t.Fatal("credential fragment exposed by stored log endpoint")
		}
		if suffix == "" {
			var data map[string]any
			if json.Unmarshal(res.Body.Bytes(), &data) != nil {
				t.Fatal("invalid endpoint JSON")
			}
		}
	}
}
