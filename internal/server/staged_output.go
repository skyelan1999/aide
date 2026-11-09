package server

import (
	"fmt"
	"strings"
	"time"
)

// OutputSegment is a visible main-chat response, separate from reasoning and
// auxiliary worker output. Content remains the legacy final-answer field.
type OutputSegment struct {
	ID      string `json:"id"`
	Round   int    `json:"round"`
	Content string `json:"content"`
	At      string `json:"at,omitempty"` // 正文落盘时间；旧记录留空
}

func (a *App) saveOutputSegment(task *Task, index, round int, text string) error {
	return a.saveOutputSegmentAt(task, index, round, text, "")
}

// saveOutputSegmentAt 允许调用方指定正文的生成起始时间：插话打断时正文是
// 在打断之前产生的，用它落盘就不能让时间线把这段正文排到插话后面。
func (a *App) saveOutputSegmentAt(task *Task, index, round int, text, at string) error {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	if at == "" {
		at = time.Now().UTC().Format(time.RFC3339Nano)
	}
	a.mu.Lock()
	if task.Mode != "chat" || index < 0 || index >= len(task.Steps) || task.Steps[index].Name != "chat" {
		a.mu.Unlock()
		return nil
	}
	session := a.approvalSessionLocked(task)
	if session == nil || session.Deleted {
		a.mu.Unlock()
		return fmt.Errorf("阶段正文没有有效的所属会话")
	}
	segment := OutputSegment{ID: newID(), Round: round, Content: text, At: at}
	step := &task.Steps[index]
	step.OutputSegments = append(step.OutputSegments, segment)
	if err := a.save(session); err != nil {
		step.OutputSegments = step.OutputSegments[:len(step.OutputSegments)-1]
		a.mu.Unlock()
		return fmt.Errorf("保存阶段正文: %w", err)
	}
	name := step.Name
	a.mu.Unlock()
	// Never advertise an unpersisted conclusion. The GET snapshot owns recovery.
	a.publishStream(task.ID, streamEvent{Event: "segment", Step: name, Round: round, Segment: &segment})
	return nil
}
