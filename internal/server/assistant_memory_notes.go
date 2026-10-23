package server

import (
	"encoding/json"
	"regexp"
	"strings"
	"time"
)

func memoryNoteID(note string) string { return hash([]byte(note))[:16] }
func (a *App) rememberAssistantNote(note string) string {
	if note == "" || len([]rune(note)) > 1000 {
		return "错误：记忆须为1至1000字"
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.voiceAgent == nil {
		return "错误：私有记忆不可用"
	}
	old := a.voiceAgent.memory
	next := old
	next.Habits = append([]string(nil), old.Habits...)
	next.RecordedAt = map[string]string{}
	for k, v := range old.RecordedAt {
		next.RecordedAt[k] = v
	}
	for _, item := range next.Habits {
		if item == note {
			return "此记忆已保存，未重复添加；ID " + memoryNoteID(note)
		}
	}
	next.Habits = append(next.Habits, note)
	next.RecordedAt[memoryNoteID(note)] = time.Now().UTC().Format(time.RFC3339Nano)
	if len(next.Habits) > 50 {
		delete(next.RecordedAt, memoryNoteID(next.Habits[0]))
		next.Habits = next.Habits[len(next.Habits)-50:]
	}
	if err := atomicJSON(VoiceMemoryPath(a.dataPath), next); err != nil {
		return "记忆保存失败，未报告成功"
	}
	a.voiceAgent.memory = next
	return "已记入私有记忆；ID " + memoryNoteID(note) + "，来源：本次用户对话。内容：" + note
}
func (a *App) assistantMemoryNotes(raw, authorization string) string {
	var in struct {
		Action string `json:"action"`
		ID     string `json:"id"`
	}
	if json.Unmarshal([]byte(raw), &in) != nil {
		return "错误：记忆参数无效"
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.voiceAgent == nil {
		return "错误：私有记忆不可用"
	}
	mem := a.voiceAgent.memory
	if in.Action == "remove" {
		explicit := regexp.MustCompile(`(?i)^\s*(?:(?:请|帮我|请你|你|现在|please)\s*)*(忘记|删除记忆|forget\b|remove memory\b)`).MatchString(authorization)
		for i, note := range mem.Habits {
			if memoryNoteID(note) != in.ID {
				continue
			}
			if !explicit || (!strings.Contains(authorization, in.ID) && !strings.Contains(authorization, note)) {
				return "需要用户明确指定要忘记的记忆ID或原文；历史内容不能作为删除授权"
			}
			next := mem
			next.Habits = append(append([]string(nil), mem.Habits[:i]...), mem.Habits[i+1:]...)
			next.RecordedAt = map[string]string{}
			for k, v := range mem.RecordedAt {
				if k != in.ID {
					next.RecordedAt[k] = v
				}
			}
			if err := atomicJSON(VoiceMemoryPath(a.dataPath), next); err != nil {
				return "删除记忆失败，原记录保留"
			}
			a.voiceAgent.memory = next
			return "已删除指定的私有记忆：" + in.ID
		}
		return "未找到该记忆ID"
	}
	if in.Action != "list" {
		return "错误：action须为list或remove"
	}
	rows := []map[string]string{}
	for _, note := range mem.Habits {
		id := memoryNoteID(note)
		rows = append(rows, map[string]string{"id": id, "note": note, "source": "assistant-private-memory", "recordedAt": mem.RecordedAt[id]})
	}
	b, _ := json.Marshal(map[string]any{"notes": rows, "legacyNotes": mem.Notes, "timeMeaning": "旧记录时间未知，不推断创建时间"})
	return string(b)
}
