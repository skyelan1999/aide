package server

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestIssue83CommentReplyFieldsBoundBeforePersistence(t *testing.T) {
	a := testApp(t)
	cs := a.commentStore()
	c, err := cs.create(createInput{DocPath: "doc.docx", Text: "original", Author: "owner"})
	if err != nil {
		t.Fatal(err)
	}
	filename := filepath.Join(cs.docDir(c.DocPath), c.ID+".json")
	before, err := os.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"text", "author"} {
		reply := &struct {
			Text   string `json:"text"`
			Author string `json:"author"`
		}{Text: "reply", Author: "owner"}
		if field == "text" {
			reply.Text = strings.Repeat("界", maxCommentText+1)
		} else {
			reply.Author = strings.Repeat("界", maxCommentAuthor+1)
		}
		changed := "must not persist"
		if _, err := cs.update(c.ID, updateInput{Text: &changed, Reply: reply}); err == nil {
			t.Errorf("oversized reply %s accepted", field)
		}
		after, err := os.ReadFile(filename)
		if err != nil || !bytes.Equal(before, after) {
			t.Errorf("failed reply %s changed saved comment: %v", field, err)
		}
	}
}
func TestIssue83CommentReplyUnicodeBoundary(t *testing.T) {
	a := testApp(t)
	cs := a.commentStore()
	c, err := cs.create(createInput{DocPath: "doc.docx", Text: "control"})
	if err != nil {
		t.Fatal(err)
	}
	text, author := strings.Repeat("界", maxCommentText), strings.Repeat("名", maxCommentAuthor)
	got, err := cs.update(c.ID, updateInput{Reply: &struct {
		Text   string `json:"text"`
		Author string `json:"author"`
	}{text, author}})
	if err != nil || len(got.Replies) != 1 || got.Replies[0].Text != text || got.Replies[0].Author != author {
		t.Fatal("Unicode boundary reply rejected/changed", err)
	}
}
