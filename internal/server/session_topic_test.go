package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestSessionTopicLocalProjection(t *testing.T) {
	for _, tc := range []struct{ old, prompt, want string }{
		{"好的，我理解。但我无法一次性完成。", "帮我生成一份一百万字的小说，要求有科幻题材，并为其生成对应漫剧改编的剧本", "一百万字小说与漫剧改编的剧本"},
		{"list_files", "只读验收。请先用 list_files 查看工作目录下的 sample-abcdef，再读取文件。", "查看工作目录 · 只读验收"},
		{"继续", "独立验证。先使用 inspect_queue 核对打印队列中的 job-ab12cd，再汇报结果。", "核对打印队列 · 独立验证"},
		{"做了吗", "请帮我制作一套温控方案，同时编写操作手册。", "温控方案与操作手册"},
	} {
		initial := &Task{Prompt: tc.prompt}
		s := &Session{Title: tc.old, Runs: []*Task{initial, {Prompt: "继续"}, {Prompt: "做了吗"}}}
		if got := displaySessionTitle(s); got != tc.want {
			t.Errorf("projection got %q want %q", got, tc.want)
		}
		if displaySessionTitle(sessionCatalogueMetadata(s)) != tc.want || initial.Prompt != tc.prompt {
			t.Fatal("catalogue and full history must agree without mutating prompts")
		}
	}
	s := &Session{Title: "继续", TitleSource: "manual", Runs: []*Task{{Prompt: "设计纸鸢图册"}}}
	if displaySessionTitle(s) != "继续" {
		t.Fatal("user title must survive even when it resembles a progress message")
	}
	s.TitleSource = ""
	s.Kind = assistantSessionKind
	if displaySessionTitle(s) != "继续" {
		t.Fatal("assistant system title must never be derived from private history")
	}
	s.Kind = ""
	s.Title = "自选工作名称"
	if managedTopic(s) {
		t.Fatal("ambiguous descriptive legacy title must be preserved")
	}
}

func TestSessionTopicListDetailsAndSearchShareTitle(t *testing.T) {
	a := testApp(t)
	id := newID()
	s := &Session{ID: id, Number: 291, Title: "list_files", Runs: []*Task{{ID: newID(), Prompt: "整理纸鸢图册", Status: "completed"}, {ID: newID(), Prompt: "继续", Status: "completed"}}}
	a.mu.Lock()
	a.sessions[id] = s
	if err := a.save(s); err != nil {
		t.Fatal(err)
	}
	a.mu.Unlock()
	for _, path := range []string{"/api/sessions", "/api/sessions/" + id + "?limit=1", "/api/search?q=" + url.QueryEscape("#291"), "/api/search?q=" + url.QueryEscape("纸鸢") + "&meta=1"} {
		w := request(a, "GET", path, nil)
		if w.Code != 200 || !strings.Contains(w.Body.String(), `"title":"整理纸鸢图册"`) {
			t.Fatalf("title missing for %s: %d %s", path, w.Code, w.Body.String())
		}
	}
	if s.Title != "list_files" {
		t.Fatal("a read must not rewrite the raw legacy title")
	}
}

func TestSessionTopicSummarizationLifecycle(t *testing.T) {
	a := testApp(t)
	task := &Task{ID: newID(), Prompt: "比较纸鸢的骨架结构"}
	s := &Session{ID: newID(), Title: "新会话", TitleSource: "auto", Runs: []*Task{task}}
	setLocalTopic(s, task.Prompt)
	a.sessions[s.ID] = s
	ch := make(chan string, 8)
	a.eventMu.Lock()
	a.globalSubs[ch] = struct{}{}
	a.eventMu.Unlock()
	var calls atomic.Int32
	var answer atomic.Value
	answer.Store("纸鸢骨架结构比较")
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct{ Messages []Message }
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if len(body.Messages) != 2 || !strings.Contains(body.Messages[1].Content, "纸鸢") {
			t.Error("summary omitted the original intent")
		}
		if strings.Contains(body.Messages[1].Content, "PRIVATE_REPLY") {
			t.Error("leaked assistant history into title request")
		}
		calls.Add(1)
		jsonOut(w, 200, map[string]any{"choices": []any{map[string]any{"message": Message{Role: "assistant", Content: answer.Load().(string)}}}})
	}))
	defer provider.Close()
	cfg := Settings{BaseURL: provider.URL, Model: "fixture"}
	s.Messages = []Message{{Role: "assistant", Content: "PRIVATE_REPLY"}}
	a.summarizeTopic(context.Background(), s, task, cfg, ProfileParams{})
	if s.Title != "纸鸢骨架结构比较" || task.TopicContext == nil {
		t.Fatal(s.Title)
	}
	select {
	case msg := <-ch:
		if msg != "sessions:"+s.ID {
			t.Fatal(msg)
		}
	default:
		t.Fatal("missing title-change broadcast")
	}
	original := s.Title
	for _, prompt := range []string{"继续", "做了吗", "hello", "list_files"} {
		next := &Task{ID: newID(), Prompt: prompt}
		s.Runs = append(s.Runs, next)
		a.summarizeTopic(context.Background(), s, next, cfg, ProfileParams{})
	}
	if s.Title != original || calls.Load() != 1 {
		t.Fatal("fillers triggered a title request")
	}
	pivot := &Task{ID: newID(), Prompt: "换个话题：制作茶盏图鉴"}
	s.Runs = append(s.Runs, pivot)
	setLocalTopic(s, pivot.Prompt)
	if s.Title != "制作茶盏图鉴" {
		t.Fatal("local pivot missing", s.Title)
	}
	for _, invalid := range []string{"好的，我来帮你", "list_files", "主题标题", "深海潜水与装备保养", "纸鸢骨架结构比较"} {
		answer.Store(invalid)
		a.summarizeTopic(context.Background(), s, pivot, cfg, ProfileParams{})
		if s.Title != "制作茶盏图鉴" {
			t.Fatalf("bad title replaced useful local title: %q", s.Title)
		}
	}
	answer.Store("茶盏图鉴制作")
	a.summarizeTopic(context.Background(), s, pivot, cfg, ProfileParams{})
	if s.Title != "茶盏图鉴制作" {
		t.Fatal("model ignored pivot", s.Title)
	}
}

func TestSessionTopicLateReplyPreservesManualOrNewerTitle(t *testing.T) {
	for _, manual := range []bool{false, true} {
		a := testApp(t)
		task := &Task{ID: newID(), Prompt: "设计纸鸢图册"}
		s := &Session{ID: newID(), Title: "图册设计", TitleSource: "auto", Runs: []*Task{task}}
		a.sessions[s.ID] = s
		entered, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
		provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			close(entered)
			select {
			case <-release:
			case <-r.Context().Done():
				return
			}
			jsonOut(w, 200, map[string]any{"choices": []any{map[string]any{"message": Message{Role: "assistant", Content: "纸鸢图册设计"}}}})
		}))
		go func() {
			defer close(done)
			a.summarizeTopic(context.Background(), s, task, Settings{BaseURL: provider.URL, Model: "fixture"}, ProfileParams{})
		}()
		select {
		case <-entered:
		case <-time.After(time.Second * 3):
			provider.Close()
			t.Fatal("summary did not start")
		}
		want := "修订印刷规范"
		if manual {
			want = "我的标题"
			res := request(a, "PATCH", "/api/sessions/"+s.ID, map[string]string{"title": want})
			if res.Code != 200 {
				t.Errorf("rename failed: %d", res.Code)
			}
		} else {
			a.mu.Lock()
			next := &Task{ID: newID(), Prompt: "现在改做修订印刷规范"}
			s.Runs = append(s.Runs, next)
			setLocalTopic(s, next.Prompt)
			a.mu.Unlock()
		}
		close(release)
		<-done
		provider.Close()
		if s.Title != want {
			t.Fatalf("stale result won: %q, wanted %q", s.Title, want)
		}
	}
}
