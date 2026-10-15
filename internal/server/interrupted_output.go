package server

import (
	"fmt"
	"strings"
)

// Only public response text belongs in conversation history, never reasoning,
// proposal JSON, tool arguments/results or auxiliary worker output.
func visibleTaskOutput(task *Task) string {
	var parts []string
	for _, step := range task.Steps {
		if step.Name != "chat" && step.Name != "agent" && step.Name != "review" {
			continue
		}
		represented := false
		for _, segment := range step.OutputSegments {
			if strings.TrimSpace(segment.Content) != "" {
				parts = append(parts, segment.Content)
			}
			if segment.Content == step.Content {
				represented = true
			}
		}
		if !represented && strings.TrimSpace(step.Content) != "" {
			parts = append(parts, step.Content)
		}
	}
	return strings.Join(parts, "\n\n")
}
func interruptedOutputMessage(task *Task) (Message, bool) {
	switch task.Status {
	case "cancelled", "paused", "interrupted", "failed":
	default:
		return Message{}, false
	}
	text := visibleTaskOutput(task)
	if strings.TrimSpace(text) == "" {
		return Message{}, false
	}
	return Message{Role: "assistant", SourceRunID: task.ID, Content: fmt.Sprintf("[中断前的部分输出 · 任务 %s · 状态 %s]\n以下是已生成的正文，可能未写完，不代表任务完成、操作成功或文件已应用。\n\n%s", task.ID, task.Status, text)}, true
}

// Caller holds a.mu. Associate durable assistant text with its owning run so
// legacy recovery and repeated finalization cannot duplicate that response.
func (a *App) rememberTaskOutputLocked(s *Session, task *Task, answer string) {
	if s == nil || s.Deleted || a.sessions[s.ID] != s {
		return
	}
	owned := false
	for _, run := range s.Runs {
		if run == task {
			owned = true
			break
		}
	}
	if !owned {
		return
	}
	s.Messages = conversationHistory(s)
	message, interrupted := interruptedOutputMessage(task)
	if !interrupted {
		if task.Status != "completed" && task.Status != "awaiting_approval" {
			return
		}
		text := visibleTaskOutput(task)
		if strings.TrimSpace(text) == "" {
			text = answer
		}
		if strings.TrimSpace(text) == "" {
			return
		}
		message = Message{Role: "assistant", Content: text, SourceRunID: task.ID}
	}
	for _, previous := range s.Messages {
		if previous.Role == "assistant" && previous.SourceRunID == task.ID {
			return
		}
	}
	s.Messages = append(s.Messages, message)
}

// A read-only projection for old sessions whose stopped output was saved only
// in Runs. Insert it at a retained user-turn boundary, before the next run.
// Never resurrect compacted turns or guess ownership for ambiguous old prompts.
func conversationHistory(s *Session) []Message {
	if s == nil || s.Deleted {
		return nil
	}
	tasks := map[string]*Task{}
	for _, task := range s.Runs {
		if task != nil {
			tasks[task.ID] = task
		}
	}
	anchors := map[int][]*Task{}
	anchored := map[string]bool{}
	represented := map[string]bool{}
	legacyMessages := map[string][]int{}
	for i, message := range s.Messages {
		if message.Role == "assistant" && message.SourceRunID != "" {
			represented[message.SourceRunID] = true
		}
		if message.Role != "user" {
			continue
		}
		if message.SourceRunID != "" {
			anchors[i] = []*Task{tasks[message.SourceRunID]}
			anchored[message.SourceRunID] = true
		} else {
			legacyMessages[message.Content] = append(legacyMessages[message.Content], i)
		}
	}
	legacyTasks := map[string][]*Task{}
	for _, task := range s.Runs {
		if task == nil || anchored[task.ID] || task.ResumedFrom != "" {
			continue
		}
		legacyTasks[task.Prompt] = append(legacyTasks[task.Prompt], task)
	}
	for prompt, tasks := range legacyTasks {
		indices := legacyMessages[prompt]
		if len(indices) != len(tasks) {
			continue
		}
		for j, index := range indices {
			anchors[index] = []*Task{tasks[j]}
		}
	}
	anchoredAt := map[string]int{}
	for index, turns := range anchors {
		for _, turn := range turns {
			if turn != nil {
				anchoredAt[turn.ID] = index
			}
		}
	}
	for _, task := range s.Runs {
		if task == nil || task.ResumedFrom == "" {
			continue
		}
		if _, exists := anchoredAt[task.ID]; exists {
			continue
		}
		seen := map[string]bool{}
		for parent := task.ResumedFrom; parent != "" && !seen[parent]; {
			seen[parent] = true
			if index, exists := anchoredAt[parent]; exists {
				anchors[index] = append(anchors[index], task)
				anchoredAt[task.ID] = index
				break
			}
			if ancestor := tasks[parent]; ancestor != nil {
				parent = ancestor.ResumedFrom
			} else {
				break
			}
		}
	}
	history := make([]Message, 0, len(s.Messages))
	var previous []*Task
	flush := func() {
		for _, task := range previous {
			if task != nil && !represented[task.ID] {
				if message, ok := interruptedOutputMessage(task); ok {
					history = append(history, message)
					represented[task.ID] = true
				}
			}
		}
	}
	for i, message := range s.Messages {
		if task, boundary := anchors[i]; boundary {
			flush()
			previous = task
		}
		history = append(history, message)
	}
	flush()

	return history
}

// Used only by writes/compaction, never a read-only context preview. Save the
// recovered history before allowing compression to remove its retained anchor.
func (a *App) materializeConversationLocked(s *Session) error {
	if s == nil || s.Deleted || a.sessions[s.ID] != s {
		return fmt.Errorf("会话已变化")
	}
	history := conversationHistory(s)
	if len(history) == len(s.Messages) {
		return nil
	}
	candidate := *s
	candidate.Messages = history
	if err := a.save(&candidate); err != nil {
		return err
	}
	s.Messages = history
	return nil
}
