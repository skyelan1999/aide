package server

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"
)

type knowledgeTopicView struct {
	ID             string                         `json:"id"`
	Label          string                         `json:"label"`
	Disabled       bool                           `json:"disabled"`
	Members        []knowledgeConclusionSelection `json:"members"`
	TotalMembers   int                            `json:"totalMembers"`
	ExpiredMembers int                            `json:"expiredMembers"`
	Archived       []knowledgeConclusionSelection `json:"archived"`
}

func knowledgeTopicsView(s knowledgeConclusionTopics, history knowledgeTimeStore, before, after string) []knowledgeTopicView {
	available := map[string]bool{}
	for _, h := range history.Snapshots {
		available[h.ID] = true
	}
	out := []knowledgeTopicView{}
	for _, topic := range s.Topics {
		view := knowledgeTopicView{ID: topic.ID, Label: topic.Label, Disabled: topic.Disabled, Members: []knowledgeConclusionSelection{}, TotalMembers: len(topic.Members)}
		view.Archived = []knowledgeConclusionSelection{}
		for _, m := range topic.Members {
			if !available[m.Pick.Snapshot] {
				view.ExpiredMembers++
				view.Archived = append(view.Archived, m)
			}
			if m.Pick.Snapshot == before || m.Pick.Snapshot == after {
				view.Members = append(view.Members, m)
			}
		}
		out = append(out, view)
	}
	return out
}
func knowledgeTopicSnapshot(history knowledgeTimeStore, id string) (knowledgeTimeSnapshot, bool) {
	for _, s := range history.Snapshots {
		if s.ID == id {
			return s, true
		}
	}
	return knowledgeTimeSnapshot{}, false
}
func (a *App) knowledgeTopicsAPI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	read := r.Method == http.MethodGet || r.Method == http.MethodHead
	if !read && r.Method != http.MethodPut {
		fail(w, 405, fmt.Errorf("不支持的人工主题请求方法"))
		return
	}
	workspace := r.URL.Query().Get("workspace")
	code := r.URL.Query().Get("code") == "1"
	var req struct {
		Expected *int64                  `json:"expected"`
		ID       string                  `json:"id"`
		Label    string                  `json:"label"`
		Action   string                  `json:"action"`
		Before   knowledgeConclusionPick `json:"before"`
		After    knowledgeConclusionPick `json:"after"`
	}
	if r.Method == "PUT" {
		d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
		d.DisallowUnknownFields()
		if d.Decode(&req) != nil || d.Decode(&struct{}{}) != io.EOF || req.Expected == nil || *req.Expected < 0 || len(req.ID) > 32 || len(req.Label) > 256 || !utf8.ValidString(req.Label) || (req.Action != "upsert" && req.Action != "disable") {
			fail(w, 400, fmt.Errorf("主题请求格式无效"))
			return
		}
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.settings.UserPasswordHash != "" && a.loadLockStateLocked().Locked {
		fail(w, 423, fmt.Errorf("请先解锁工作台"))
		return
	}
	if workspace == "" || workspace != knowledgeID(a.wsID(), "scope", "", ".") {
		fail(w, 409, fmt.Errorf("工作区已变化，请重新同步星图"))
		return
	}
	if err := r.Context().Err(); err != nil {
		fail(w, 408, fmt.Errorf("人工主题请求已取消"))
		return
	}
	file := knowledgeTopicFile(a.dataPath, workspace, code)
	key := knowledgeTopicKey(a.token)
	store, err := loadKnowledgeTopics(file, workspace, code, key)
	if err != nil {
		fail(w, 500, err)
		return
	}
	history, err := loadKnowledgeTime(knowledgeTimeFile(a.dataPath, workspace, code), workspace, code, knowledgeTimeKey(a.token))
	if err != nil {
		fail(w, 500, err)
		return
	}
	if read {
		beforeID, afterID := r.URL.Query().Get("before"), r.URL.Query().Get("after")
		before, ok := knowledgeTopicSnapshot(history, beforeID)
		after, ok2 := knowledgeTopicSnapshot(history, afterID)
		if !ok || !ok2 {
			fail(w, 404, fmt.Errorf("历史观察已淘汰，请重新读取列表"))
			return
		}
		bn, an := r.URL.Query().Get("beforeNode"), r.URL.Query().Get("afterNode")
		if len(bn) > 128 || len(an) > 128 {
			fail(w, 400, fmt.Errorf("节点编号过长"))
			return
		}
		bc, bp := knowledgeConclusionCandidates(before, bn, code)
		ac, ap := knowledgeConclusionCandidates(after, an, code)
		jsonOut(w, 200, map[string]any{"revision": store.Revision, "topics": knowledgeTopicsView(store, history, beforeID, afterID), "before": bc, "after": ac, "partial": bp || ap, "limits": map[string]int{"topics": 32, "observationsPerTopic": 32, "bytes": knowledgeTopicBytes}})
		return
	}
	if *req.Expected != store.Revision {
		fail(w, 409, fmt.Errorf("人工主题已被修改，请重新读取后保存"))
		return
	}
	index := -1
	for i, topic := range store.Topics {
		if topic.ID == req.ID {
			index = i
		}
	}
	if req.ID != "" && index < 0 {
		fail(w, 404, fmt.Errorf("人工主题不存在"))
		return
	}
	if req.Action == "disable" {
		if index < 0 {
			fail(w, 400, fmt.Errorf("请选择要停用的主题"))
			return
		}
		store.Topics[index].Disabled = true
	} else {
		if req.Before.Snapshot == req.After.Snapshot {
			fail(w, 400, fmt.Errorf("请选择两个不同的历史观察"))
			return
		}
		before, ok := knowledgeTopicSnapshot(history, req.Before.Snapshot)
		after, ok2 := knowledgeTopicSnapshot(history, req.After.Snapshot)
		if !ok || !ok2 {
			fail(w, 404, fmt.Errorf("历史观察已淘汰，未保存对齐"))
			return
		}
		now := time.Now().UTC().Format(time.RFC3339Nano)
		bm, err := knowledgeConclusionSelect(before, req.Before, now, code)
		if err != nil {
			fail(w, 400, err)
			return
		}
		am, err := knowledgeConclusionSelect(after, req.After, now, code)
		if err != nil {
			fail(w, 400, err)
			return
		}
		label := strings.TrimSpace(req.Label)
		if label == "" {
			fail(w, 400, fmt.Errorf("请填写主题名称"))
			return
		}
		if index < 0 {
			if len(store.Topics) >= 32 {
				fail(w, 409, fmt.Errorf("主题数量已达32个，原标记保留"))
				return
			}
			store.Topics = append(store.Topics, knowledgeConclusionTopic{ID: newID()})
			index = len(store.Topics) - 1
		}
		topic := store.Topics[index]
		topic.Label = label
		topic.Disabled = false
		members := []knowledgeConclusionSelection{}
		for _, m := range topic.Members {
			if m.Pick.Snapshot != bm.Pick.Snapshot && m.Pick.Snapshot != am.Pick.Snapshot {
				members = append(members, m)
			}
		}
		members = append(members, bm, am)
		if len(members) > 32 {
			fail(w, 409, fmt.Errorf("主题观察数量已达32次，原标记保留"))
			return
		}
		topic.Members = members
		store.Topics[index] = topic
	}
	if err := r.Context().Err(); err != nil {
		fail(w, 408, fmt.Errorf("请求已取消，未保存主题"))
		return
	}
	store.Revision++
	if err := saveKnowledgeTopics(file, store, key); err != nil {
		fail(w, 500, err)
		return
	}
	jsonOut(w, 200, map[string]any{"saved": true, "id": store.Topics[index].ID, "revision": store.Revision, "topics": knowledgeTopicsView(store, history, req.Before.Snapshot, req.After.Snapshot)})
}
