package server

import (
	"context"
	"testing"
	"time"
)

func TestKnowledgeWebParserTransport(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	text := "throw new Error('source must not execute');\nasync function save(){return fetch('/api/save', {method:'POST'});}\ndocument.getElementById('save').addEventListener('click',save);\nfunction dynamic(url){fetch(url);}\n"
	units, err := codeParseBatch(ctx, "JavaScript", []knowledgeCodeFile{{ID: "F", Text: text}})
	if err != nil {
		t.Fatal(err)
	}
	if len(units) != 1 {
		t.Fatal(units)
	}
	u := units[0]
	if len(u.Diagnostics) != 0 || len(u.Events) != 1 || len(u.Requests) != 1 || len(u.UnresolvedWeb) != 1 {
		t.Fatalf("source records lost in parser transport: %+v", u)
	}
	e, r := u.Events[0], u.Requests[0]
	if e.Target != "save" || e.Callback != "save" || e.Line != 3 || e.Column != 1 || e.Event != "click" {
		t.Fatalf("event identity/position lost: %+v", e)
	}
	if r.Caller != e.Callback || r.URL != "/api/save" || r.Method != "POST" || r.Line != 2 || r.Column < 1 {
		t.Fatalf("request identity/position lost: %+v", r)
	}
	if u.UnresolvedWeb[0].Reason != "dynamic_url" || u.UnresolvedWeb[0].Line != 4 {
		t.Fatal(u.UnresolvedWeb)
	}
}
