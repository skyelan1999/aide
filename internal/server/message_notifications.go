package server

import (
	"aide/internal/messaging"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type notificationCategories struct {
	Chats     bool `json:"chats"`
	Groups    bool `json:"groups"`
	Contacts  bool `json:"contacts"`
	Files     bool `json:"files"`
	Reminders bool `json:"reminders"`
}
type notificationSettings struct {
	Badge         string                 `json:"badge"`
	Categories    notificationCategories `json:"categories"`
	ReminderScope string                 `json:"reminderScope"`
	Popup         bool                   `json:"popup"`
	Sound         bool                   `json:"sound"`
	Desktop       bool                   `json:"desktop"`
	IncludeMuted  bool                   `json:"includeMuted"`
}
type conversationNotification struct {
	Mode  string `json:"mode"`
	Until string `json:"until,omitempty"`
}
type notificationConfig struct {
	Version       int                                 `json:"version"`
	Revision      int64                               `json:"revision"`
	Settings      notificationSettings                `json:"settings"`
	Conversations map[string]conversationNotification `json:"conversations"`
}
type notificationConversationCount struct {
	Unread  int    `json:"unread"`
	Mode    string `json:"mode"`
	Until   string `json:"until,omitempty"`
	Counted bool   `json:"counted"`
}
type notificationSummary struct {
	Total         int                                      `json:"total"`
	Chats         int                                      `json:"chats"`
	Groups        int                                      `json:"groups"`
	Contacts      int                                      `json:"contacts"`
	Files         int                                      `json:"files"`
	Reminders     int                                      `json:"reminders"`
	Conversations map[string]notificationConversationCount `json:"conversations"`
}

func defaultNotificationConfig() notificationConfig {
	return notificationConfig{Version: 1, Settings: notificationSettings{Badge: "number", ReminderScope: "all", Popup: true, Categories: notificationCategories{true, true, true, true, true}}, Conversations: map[string]conversationNotification{}}
}
func notificationConfigPath(data string) string {
	return filepath.Join(ConfigDir(data), "notifications.json")
}
func (a *App) loadNotificationConfigLocked() (notificationConfig, error) {
	c := defaultNotificationConfig()
	raw, err := os.ReadFile(notificationConfigPath(a.dataPath))
	if errors.Is(err, os.ErrNotExist) {
		return c, nil
	}
	if err != nil {
		return c, err
	}
	if err = json.Unmarshal(raw, &c); err != nil {
		return c, err
	}
	if c.Version != 1 {
		return c, errors.New("unsupported notification configuration")
	}
	if c.Conversations == nil {
		c.Conversations = map[string]conversationNotification{}
	}
	return c, nil
}
func validNotificationKey(key string) bool {
	n := 64
	if strings.HasPrefix(key, "group:") {
		key = strings.TrimPrefix(key, "group:")
		n = 32
	}
	b, err := hex.DecodeString(key)
	return err == nil && len(key) == n && hex.EncodeToString(b) == key
}
func notificationMode(c notificationConfig, key string, now time.Time) string {
	r, ok := c.Conversations[key]
	if !ok {
		return "default"
	}
	if r.Until != "" {
		until, err := time.Parse(time.RFC3339, r.Until)
		if err != nil || !until.After(now) {
			return "default"
		}
	}
	return r.Mode
}
func notificationCounted(c notificationConfig, key string, now time.Time) bool {
	mode := notificationMode(c, key, now)
	return mode != "hidden" && (mode != "mute" || c.Settings.IncludeMuted)
}

// These preferences have their own revision and durable config file. Updating a
// conversation does not restart networking or replace other conversation rules.
func (a *App) messageNotificationsAPI(w http.ResponseWriter, r *http.Request, gen int64) {
	var in struct {
		Revision     int64                 `json:"revision"`
		Settings     *notificationSettings `json:"settings"`
		Conversation *struct {
			Key   string `json:"key"`
			Mode  string `json:"mode"`
			Until string `json:"until"`
		} `json:"conversation"`
	}
	if r.Method != http.MethodGet && r.Method != http.MethodPut {
		fail(w, 405, errors.New("method not allowed"))
		return
	}
	if r.Method == http.MethodPut {
		if err := decode(w, r, &in); err != nil {
			fail(w, 400, err)
			return
		}
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	lock := a.loadLockStateLocked()
	if lock.Locked || lock.Gen != gen {
		fail(w, 423, errors.New("请先解锁工作台"))
		return
	}
	c, err := a.loadNotificationConfigLocked()
	if err != nil {
		fail(w, 500, err)
		return
	}
	if r.Method == http.MethodGet {
		jsonOut(w, 200, c)
		return
	}
	if in.Revision != c.Revision {
		fail(w, 409, errors.New("通知设置已在其他窗口修改，请刷新后重试"))
		return
	}
	if (in.Settings == nil) == (in.Conversation == nil) {
		fail(w, 400, errors.New("invalid notification update"))
		return
	}
	if in.Settings != nil {
		s := *in.Settings
		if (s.Badge != "number" && s.Badge != "dot" && s.Badge != "off") || (s.ReminderScope != "all" && s.ReminderScope != "due") {
			fail(w, 400, errors.New("invalid notification settings"))
			return
		}
		c.Settings = s
	} else {
		rule := in.Conversation
		if !validNotificationKey(rule.Key) || (rule.Mode != "default" && rule.Mode != "mute" && rule.Mode != "hidden") {
			fail(w, 400, errors.New("invalid conversation notification rule"))
			return
		}
		if rule.Until != "" {
			until, e := time.Parse(time.RFC3339, rule.Until)
			if e != nil || !until.After(time.Now()) || until.After(time.Now().Add(366*24*time.Hour)) {
				fail(w, 400, errors.New("invalid mute expiry"))
				return
			}
		}
		if rule.Mode == "default" {
			delete(c.Conversations, rule.Key)
		} else {
			if len(c.Conversations) >= 4096 {
				if _, exists := c.Conversations[rule.Key]; !exists {
					fail(w, 400, errors.New("notification rule capacity reached"))
					return
				}
			}
			c.Conversations[rule.Key] = conversationNotification{Mode: rule.Mode, Until: rule.Until}
		}
	}
	c.Revision++
	if err = atomicJSON(notificationConfigPath(a.dataPath), c); err != nil {
		fail(w, 500, err)
		return
	}
	jsonOut(w, 200, c)
}

func (a *App) messageNotificationSummary(data map[string]any) (notificationConfig, notificationSummary, error) {
	a.mu.Lock()
	c, err := a.loadNotificationConfigLocked()
	workspace := a.wsID()
	reminders := append([]Reminder(nil), a.reminders...)
	a.mu.Unlock()
	out := notificationSummary{Conversations: map[string]notificationConversationCount{}}
	if err != nil {
		return c, out, err
	}
	now := time.Now()
	counts, _ := data["unread"].(map[string]int)
	for key, count := range counts {
		mode := notificationMode(c, key, now)
		counted := notificationCounted(c, key, now)
		out.Conversations[key] = notificationConversationCount{Unread: count, Mode: mode, Until: c.Conversations[key].Until, Counted: counted}
		if !counted {
			continue
		}
		if strings.HasPrefix(key, "group:") {
			if c.Settings.Categories.Groups {
				out.Groups += count
			}
		} else if c.Settings.Categories.Chats {
			out.Chats += count
		}
	}
	// Keep rule controls available even when the conversation has no unread.
	for key, rule := range c.Conversations {
		if _, ok := out.Conversations[key]; !ok {
			out.Conversations[key] = notificationConversationCount{Mode: notificationMode(c, key, now), Until: rule.Until, Counted: notificationCounted(c, key, now)}
		}
	}
	consent, _ := data["consent"].(messaging.State)
	transfers, _ := data["transfers"].(map[string]messaging.Transfer)
	for _, inv := range consent.Invitations {
		if inv.Direction != "incoming" || inv.Status != "pending" || !inv.Expires.After(now) || consent.Relationships[inv.Peer].Blocked {
			continue
		}
		key := inv.Peer
		if inv.Kind == messaging.Group {
			key = "group:" + inv.Resource
		}
		if !notificationCounted(c, key, now) {
			continue
		}
		if inv.Kind == messaging.File {
			if c.Settings.Categories.Files {
				out.Files++
			}
		} else if c.Settings.Categories.Contacts {
			out.Contacts++
		}
	}
	for id, transfer := range transfers {
		inv := consent.Invitations[id]
		if transfer.CleanupError != "" && inv.Status != "pending" && !consent.Relationships[transfer.Peer].Blocked && c.Settings.Categories.Files && notificationCounted(c, transfer.Peer, now) {
			out.Files++
		}
	}
	for _, r := range reminders {
		if !c.Settings.Categories.Reminders || r.Status != "pending" || (r.Area == "workspace" && r.WorkspaceID != workspace) {
			continue
		}
		if c.Settings.ReminderScope == "due" {
			due, e := time.Parse(time.RFC3339, r.DueAt)
			if e != nil || due.After(now) {
				continue
			}
		}
		out.Reminders++
	}
	out.Total = out.Chats + out.Groups + out.Contacts + out.Files + out.Reminders
	return c, out, nil
}
