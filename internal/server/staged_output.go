package server

import (
	"fmt"
	"strings"
)

// OutputSegment is a visible main-chat response, separate from reasoning and
// auxiliary worker output. Content remains the legacy final-answer field.
type OutputSegment struct {
	ID      string `json:"id"`
	Round   int    `json:"round"`
	Content string `json:"content"`
}

func (a *App) saveOutputSegment(task *Task, index, round int, text string) error {
	if strings.TrimSpace(text) == "" {
		return nil
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
	segment := OutputSegment{ID: newID(), Round: round, Content: text}
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
