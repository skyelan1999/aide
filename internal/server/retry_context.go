package server

import (
	"errors"
	"fmt"
	"strings"
)

// Retry replays the selected turn, not requests added after it. This view is
// private to context construction; the live session and current policies remain
// untouched. Call with a.mu held.
func retryConversation(s *Session, original *Task) (*Session, error) {
	if s == nil || original == nil || original.Prompt == "" {
		return nil, errors.New("原任务请求缺失，无法建立重试上下文")
	}
	index := -1
	for i, run := range s.Runs {
		if run != nil && run.ID == original.ID {
			index = i
			break
		}
	}
	if index < 0 {
		return nil, errors.New("原任务已不存在")
	}
	view := *s
	view.Runs = append([]*Task(nil), s.Runs[:index]...)
	for i, message := range s.Messages {
		if message.Role == "user" && message.SourceRunID == original.ID {
			view.Messages = append([]Message(nil), s.Messages[:i]...)
			return &view, nil
		}
	}
	// Match old untagged messages only when their occurrences correspond
	// unambiguously to the untagged turns, as in conversationHistory.
	anchored := map[string]bool{}
	var messages []int
	for i, message := range s.Messages {
		if message.Role != "user" {
			continue
		}
		if message.SourceRunID != "" {
			anchored[message.SourceRunID] = true
		} else if message.Content == original.Prompt {
			messages = append(messages, i)
		}
	}
	var turns []*Task
	for _, run := range s.Runs {
		if run != nil && !anchored[run.ID] && run.ResumedFrom == "" && run.Prompt == original.Prompt {
			turns = append(turns, run)
		}
	}
	if len(turns) == len(messages) {
		for i, run := range turns {
			if run.ID == original.ID {
				view.Messages = append([]Message(nil), s.Messages[:messages[i]]...)
				return &view, nil
			}
		}
	}
	// Compaction can remove an old input anchor. Recover its preceding
	// conversation from the original recorded request, rebuilding the system
	// header/tools/attachments using today's policies in the normal builder.
	if len(original.RequestSnapshots) > 0 {
		input := original.RequestSnapshots[0].Messages
		for i := len(input) - 1; i >= 0; i-- {
			if input[i].Role != "user" || !strings.HasPrefix(input[i].Content, original.Prompt) {
				continue
			}
			view.Messages = nil
			// Recorded provider messages already contain recovered partial output.
			// Do not synthesize it a second time from live prior-run objects.
			view.Runs = nil
			view.Compact, view.CompactedMessages = "", 0
			for _, message := range input[:i] {
				if message.Role != "system" {
					view.Messages = append(view.Messages, message)
				} else if strings.HasPrefix(message.Content, "历史摘要（已压缩 ") {
					if _, summary, ok := strings.Cut(message.Content, "\n"); ok {
						view.Compact = summary
						fmt.Sscanf(message.Content, "历史摘要（已压缩 %d 条消息）:", &view.CompactedMessages)
					}
				}
			}
			return &view, nil
		}
	}
	if index == 0 && len(s.Messages) == 0 && s.Compact == "" {
		view.Messages = nil
		return &view, nil
	}
	return nil, errors.New("原任务的输入历史已压缩或缺失，无法安全重试；请新建任务并明确所需上下文")
}
