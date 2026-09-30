package server

import (
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestSystemLogSnapshotFiltersInclusiveTimeRange(t *testing.T) {
	buffer := &systemLogBuffer{entries: []systemLogEntry{
		{Time: "2026-09-30T09:59:59Z", Level: "info", Message: "before"},
		{Time: "2026-09-30T10:00:00Z", Level: "error", Message: "at start"},
		{Time: "2026-09-30T11:00:00Z", Level: "info", Message: "inside"},
		{Time: "2026-09-30T12:00:00Z", Level: "warn", Message: "at end"},
		{Time: "2026-09-30T12:00:01Z", Level: "error", Message: "after"},
	}}
	start, err := time.Parse(time.RFC3339, "2026-09-30T10:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	end, err := time.Parse(time.RFC3339, "2026-09-30T12:00:00Z")
	if err != nil {
		t.Fatal(err)
	}

	got := buffer.snapshot("all", 10, &start, &end)
	if len(got) != 3 {
		t.Fatalf("got %d entries, want 3: %#v", len(got), got)
	}
	if got[0].Message != "at end" || got[1].Message != "inside" || got[2].Message != "at start" {
		t.Fatalf("unexpected newest-first range result: %#v", got)
	}

	got = buffer.snapshot("error", 1, &start, &end)
	if len(got) != 1 || got[0].Message != "at start" {
		t.Fatalf("level and time filters should apply before limit; got %#v", got)
	}
}

func TestSystemLogsHandlerRejectsInvalidTimeRange(t *testing.T) {
	req := httptest.NewRequest("GET", "/api/system-logs?start=2026-09-30T12%3A00%3A00Z&end=2026-09-30T10%3A00%3A00Z", nil)
	res := httptest.NewRecorder()
	(&App{}).systemLogsHandler(res, req)
	if res.Code != 400 {
		t.Fatalf("status = %d, want 400", res.Code)
	}
}

func TestSystemLogsHandlerAppliesTimeFiltersToJSONAndDownload(t *testing.T) {
	previous := systemLogs
	systemLogs = &systemLogBuffer{entries: []systemLogEntry{
		{Time: "2026-09-30T09:59:59Z", Level: "info", Message: "before"},
		{Time: "2026-09-30T10:00:00Z", Level: "info", Message: "at start"},
		{Time: "2026-09-30T11:00:00Z", Level: "error", Message: "inside"},
		{Time: "2026-09-30T12:00:00Z", Level: "info", Message: "at end"},
		{Time: "2026-09-30T12:00:01Z", Level: "info", Message: "after"},
	}}
	defer func() { systemLogs = previous }()

	query := url.Values{
		"level": {"info"},
		"start": {"2026-09-30T10:00:00Z"},
		"end":   {"2026-09-30T12:00:00Z"},
	}
	req := httptest.NewRequest("GET", "/api/system-logs?"+query.Encode(), nil)
	res := httptest.NewRecorder()
	(&App{}).systemLogsHandler(res, req)
	if res.Code != 200 {
		t.Fatalf("status = %d, want 200: %s", res.Code, res.Body.String())
	}
	var result struct {
		Entries []systemLogEntry `json:"entries"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Entries) != 2 || result.Entries[0].Message != "at end" || result.Entries[1].Message != "at start" {
		t.Fatalf("unexpected filtered JSON response: %#v", result.Entries)
	}

	query.Set("download", "1")
	req = httptest.NewRequest("GET", "/api/system-logs?"+query.Encode(), nil)
	res = httptest.NewRecorder()
	(&App{}).systemLogsHandler(res, req)
	if res.Code != 200 {
		t.Fatalf("download status = %d, want 200", res.Code)
	}
	body := strings.TrimSpace(res.Body.String())
	if count := strings.Count(body, "\n") + 1; count != 2 {
		t.Fatalf("download has %d lines, want 2: %s", count, body)
	}
	if strings.Contains(body, "before") || strings.Contains(body, "inside") || strings.Contains(body, "after") {
		t.Fatalf("download contains entries outside active filters: %s", body)
	}
}
