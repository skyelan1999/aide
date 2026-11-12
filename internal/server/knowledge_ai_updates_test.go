package server

import (
	"encoding/json"
	"net/url"
	"strings"
	"testing"
)

func TestIssue796AIUpdatesVisibilityAndCursorReset(t *testing.T) {
	a := sourceActionFixture(t)
	get := func(endpoint string) string {
		t.Helper()
		r := request(a, "GET", endpoint, nil)
		requireStatus(t, r, 200)
		return r.Body.String()
	}
	decode := func(raw string) knowledgeUpdateResponse {
		t.Helper()
		var d knowledgeUpdateResponse
		if err := json.Unmarshal([]byte(raw), &d); err != nil {
			t.Fatal(err)
		}
		return d
	}
	manual := get("/api/knowledge-map/updates")
	if !strings.Contains(manual, "readonly-actions") {
		t.Fatal("fixture source missing")
	}
	first := decode(get("/api/knowledge-map/updates?scope=ai"))
	if first.Scope != "ai" {
		t.Fatal("host did not acknowledge AI scope")
	}
	if !first.Reset {
		t.Fatal("AI first snapshot must reset")
	}
	before := get("/api/knowledge-map/identity?scope=ai")
	humanBefore := get("/api/knowledge-map/identity")
	requireStatus(t, request(a, "PUT", "/api/sources/ai-visibility", map[string]any{"source": "readonly-actions", "workspaceId": a.wsID(), "visible": false}), 200)
	if after := get("/api/knowledge-map/identity?scope=ai"); after == before {
		t.Fatal("AI identity did not invalidate")
	}
	if after := get("/api/knowledge-map/identity"); after != humanBefore {
		t.Fatal("manual identity unexpectedly changed")
	}
	raw := get("/api/knowledge-map/updates?scope=ai&cursor=" + url.QueryEscape(first.Revision))
	hidden := decode(raw)
	if !hidden.Reset || hidden.Revision == first.Revision || strings.Contains(raw, "readonly-actions") {
		t.Fatalf("revoked data retained: %s", raw)
	}
	delta := decode(get("/api/knowledge-map/updates?scope=ai&cursor=" + hidden.Revision))
	if delta.Reset || len(delta.Nodes) != 0 || len(delta.Edges) != 0 {
		t.Fatal("unchanged scoped cursor not reused")
	}
	if !strings.Contains(get("/api/knowledge-map/updates"), "readonly-actions") {
		t.Fatal("manual browsing lost hidden source")
	}
	requireStatus(t, request(a, "PUT", "/api/sources/ai-visibility", map[string]any{"source": "readonly-actions", "workspaceId": a.wsID(), "visible": true}), 200)
	restored := get("/api/knowledge-map/updates?scope=ai&cursor=" + hidden.Revision)
	if !decode(restored).Reset || !strings.Contains(restored, "readonly-actions") {
		t.Fatal("restored source missing")
	}
}
