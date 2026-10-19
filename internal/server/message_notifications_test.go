package server

import (
	"aide/internal/messaging"
	"bytes"
	"encoding/json"
	"io"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

type notificationReadGate struct {
	source           io.Reader
	started, release chan struct{}
	entered          bool
}

func (g *notificationReadGate) Read(p []byte) (int, error) {
	if !g.entered {
		g.entered = true
		close(g.started)
		<-g.release
	}
	return g.source.Read(p)
}

func TestMessageNotificationMutationRejectsChangedLockGeneration(t *testing.T) {
	a := testApp(t)
	raw, _ := json.Marshal(map[string]any{"revision": 0, "settings": defaultNotificationConfig().Settings})
	gate := &notificationReadGate{source: bytes.NewReader(raw), started: make(chan struct{}), release: make(chan struct{})}
	r := httptest.NewRequest("PUT", "/api/messages/notifications", gate)
	r.Header.Set("Authorization", "Bearer "+a.token)
	w := httptest.NewRecorder()
	done := make(chan struct{})
	go func() { a.Handler().ServeHTTP(w, r); close(done) }()
	select {
	case <-gate.started:
	case <-time.After(3 * time.Second):
		t.Fatal("request did not reach decoder")
	}
	_, e := a.writeLockState(true)
	if e != nil {
		t.Fatal(e)
	}
	_, e = a.writeLockState(false)
	if e != nil {
		t.Fatal(e)
	}
	close(gate.release)
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("request blocked")
	}
	requireStatus(t, w, 423)
	c, e := a.loadNotificationConfigLocked()
	if e != nil {
		t.Fatal(e)
	}
	if c.Revision != 0 {
		t.Fatal("stale mutation committed")
	}
}

func TestMessageNotificationPreferencesRevisionPersistenceAndLock(t *testing.T) {
	a := testApp(t)
	state := request(a, "GET", "/api/messages/state", nil)
	requireStatus(t, state, 200)
	var initial struct {
		Notifications struct {
			Config  notificationConfig  `json:"config"`
			Summary notificationSummary `json:"summary"`
		} `json:"notifications"`
	}
	if err := json.Unmarshal(state.Body.Bytes(), &initial); err != nil {
		t.Fatal(err)
	}
	c := initial.Notifications.Config
	if c.Version != 1 || c.Settings.Badge != "number" || !c.Settings.Categories.Reminders || c.Settings.Sound || c.Settings.Desktop {
		t.Fatal("bad defaults")
	}
	c.Settings.Badge = "dot"
	saved := request(a, "PUT", "/api/messages/notifications", map[string]any{"revision": 0, "settings": c.Settings})
	requireStatus(t, saved, 200)
	requireStatus(t, request(a, "PUT", "/api/messages/notifications", map[string]any{"revision": 0, "settings": c.Settings}), 409)
	key := strings.Repeat("a", 64)
	requireStatus(t, request(a, "PUT", "/api/messages/notifications", map[string]any{"revision": 1, "conversation": map[string]any{"key": key, "mode": "mute", "until": time.Now().Add(time.Hour).UTC().Format(time.RFC3339)}}), 200)
	requireStatus(t, request(a, "PUT", "/api/messages/notifications", map[string]any{"revision": 2, "conversation": map[string]any{"key": "../invalid", "mode": "hidden"}}), 400)
	fresh := &App{dataPath: a.dataPath}
	reloaded, err := fresh.loadNotificationConfigLocked()
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Revision != 2 || reloaded.Settings.Badge != "dot" || reloaded.Conversations[key].Mode != "mute" {
		t.Fatal("preferences not durably preserved")
	}
	info, err := os.Stat(notificationConfigPath(a.dataPath))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0077 != 0 {
		t.Fatal("notification file is not private")
	}
	if _, err = a.writeLockState(true); err != nil {
		t.Fatal(err)
	}
	requireStatus(t, request(a, "GET", "/api/messages/notifications", nil), 423)
	requireStatus(t, request(a, "PUT", "/api/messages/notifications", map[string]any{"revision": 2, "settings": c.Settings}), 423)
}

func TestMessageNotificationAggregationDedupScopeAndMuteExpiry(t *testing.T) {
	a := testApp(t)
	peer := strings.Repeat("a", 64)
	group := "group:" + strings.Repeat("b", 32)
	hidden := strings.Repeat("c", 64)
	c := defaultNotificationConfig()
	c.Conversations[peer] = conversationNotification{Mode: "mute", Until: time.Now().Add(time.Hour).Format(time.RFC3339)}
	c.Conversations[hidden] = conversationNotification{Mode: "hidden"}
	if err := atomicJSON(notificationConfigPath(a.dataPath), c); err != nil {
		t.Fatal(err)
	}
	a.reminders = []Reminder{{ID: "future", Status: "pending", Area: "global", DueAt: time.Now().Add(time.Hour).Format(time.RFC3339)}, {ID: "due", Status: "pending", Area: "workspace", WorkspaceID: a.wsID(), DueAt: time.Now().Add(-time.Hour).Format(time.RFC3339)}, {ID: "other", Status: "pending", Area: "workspace", WorkspaceID: "another-workspace"}, {ID: "done", Status: "completed", Area: "global"}}
	inv := messaging.Invitation{ID: "file", Peer: strings.Repeat("d", 64), Direction: "incoming", Kind: messaging.File, Status: "pending", Expires: time.Now().Add(time.Hour)}
	consent := messaging.State{Invitations: map[string]messaging.Invitation{"file": inv}, Relationships: map[string]messaging.Relationship{}}
	data := map[string]any{"unread": map[string]int{peer: 3, group: 2, hidden: 4}, "consent": consent, "transfers": map[string]messaging.Transfer{"file": {ID: "file", Peer: inv.Peer, CleanupError: "retry"}}}
	_, summary, err := a.messageNotificationSummary(data)
	if err != nil {
		t.Fatal(err)
	}
	if summary.Total != 5 || summary.Files != 1 || summary.Reminders != 2 || summary.Groups != 2 || summary.Chats != 0 {
		t.Fatalf("bad dedup/mute/scope: %+v", summary)
	}
	inv.Expires = time.Now().Add(-time.Second)
	consent.Invitations["file"] = inv
	_, expiredSummary, e := a.messageNotificationSummary(data)
	if e != nil {
		t.Fatal(e)
	}
	if expiredSummary.Files != 1 {
		t.Fatal("expired invitation hid a pending cleanup error")
	}
	inv.Expires = time.Now().Add(time.Hour)
	consent.Invitations["file"] = inv
	c.Settings.IncludeMuted = true
	c.Settings.ReminderScope = "due"
	if err = atomicJSON(notificationConfigPath(a.dataPath), c); err != nil {
		t.Fatal(err)
	}
	_, summary, err = a.messageNotificationSummary(data)
	if err != nil {
		t.Fatal(err)
	}
	if summary.Total != 7 || summary.Chats != 3 || summary.Reminders != 1 {
		t.Fatalf("include muted overrode hidden or due scope: %+v", summary)
	}
	c.Settings.IncludeMuted = false
	c.Conversations[peer] = conversationNotification{Mode: "mute", Until: time.Now().Add(-time.Second).Format(time.RFC3339)}
	if err = atomicJSON(notificationConfigPath(a.dataPath), c); err != nil {
		t.Fatal(err)
	}
	_, summary, err = a.messageNotificationSummary(data)
	if err != nil {
		t.Fatal(err)
	}
	if summary.Chats != 3 || summary.Conversations[peer].Mode != "default" {
		t.Fatal("mute did not expire")
	}
	c.Settings.Categories = notificationCategories{}
	if err = atomicJSON(notificationConfigPath(a.dataPath), c); err != nil {
		t.Fatal(err)
	}
	_, summary, err = a.messageNotificationSummary(data)
	if err != nil {
		t.Fatal(err)
	}
	if summary.Total != 0 || summary.Conversations[peer].Unread != 3 {
		t.Fatal("category switches changed underlying unread")
	}
}

func TestMessageNotificationGroupVoiceInheritsGroupPolicy(t *testing.T) {
	a := testApp(t)
	group := strings.Repeat("b", 32)
	peer := strings.Repeat("c", 64)
	call := strings.Repeat("d", 32)
	c := defaultNotificationConfig()
	c.Conversations["group:"+group] = conversationNotification{Mode: "mute"}
	if err := atomicJSON(notificationConfigPath(a.dataPath), c); err != nil {
		t.Fatal(err)
	}
	data := map[string]any{"consent": messaging.State{Invitations: map[string]messaging.Invitation{"voice": {ID: "voice", Kind: messaging.Voice, Peer: peer, Resource: call, Status: "pending", Direction: "incoming", Expires: time.Now().Add(time.Hour)}}}, "calls": map[string]messaging.CallState{call: {Group: group}}}
	_, summary, err := a.messageNotificationSummary(data)
	if err != nil {
		t.Fatal(err)
	}
	if summary.Total != 0 {
		t.Fatal("muted group call counted as a direct invitation")
	}
	c.Settings.IncludeMuted = true
	if err = atomicJSON(notificationConfigPath(a.dataPath), c); err != nil {
		t.Fatal(err)
	}
	_, summary, err = a.messageNotificationSummary(data)
	if err != nil {
		t.Fatal(err)
	}
	if summary.Contacts != 1 {
		t.Fatal("include-muted preference ignored for group call")
	}
}
