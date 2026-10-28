package server

import (
	"context"
	"crypto/sha256"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"
)

// Result readiness is derived from persisted executor state. No model requests,
// approval decisions, task retries or browser presence are needed to detect it.
func (a *App) assistantResultLoop(ctx context.Context) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.publishAssistantResults()
		case <-a.assistantResultsWake:
			a.publishAssistantResults()
		}
	}
}

func assistantResultEvidenceText(event assistantWatchEvent, run *Task) string {
	text := event.Text
	if event.Status == "completed" {
		detail := ""
		for i := len(run.Steps) - 1; i >= 0; i-- {
			if run.Steps[i].Name == "chat" || run.Steps[i].Name == "agent" {
				detail = strings.TrimSpace(run.Steps[i].Content)
				break
			}
		}
		if detail != "" {
			text += "\n\n" + boundedEvidenceText(detail, 6000)
		} else {
			text += "\n\n暂未收到可汇总的结果正文，请打开原任务核对。"
		}
	}
	if event.Blocker != "" {
		text += "\n\n需要注意：" + boundedEvidenceText(event.Blocker, 600)
	}
	if event.Delivery != nil && event.Delivery.State != "delivered" {
		text += "\n\n尚未核验交付，不能视为已交付。"
	}
	return text
}

func (a *App) publishAssistantResults() {
	if a.readLockState().Locked {
		return
	}
	a.mu.Lock()
	assistant := a.findAssistantSessionLocked()
	if assistant == nil || !a.isAssistantUnlocked(assistant.ID) {
		a.mu.Unlock()
		return
	}
	oldMessages, oldUpdated := append([]Message(nil), assistant.Messages...), assistant.Updated
	oldReceipts := assistant.AssistantResultNotices
	receipts := make(map[string]string, len(oldReceipts))
	for k, v := range oldReceipts {
		receipts[k] = v
	}
	// Scan each persisted run, so a new run cannot hide the preceding result
	// before the worker handles its wake-up. Unfollowed sessions stay excluded.
	type result struct {
		event assistantWatchEvent
		run   *Task
	}
	pending := []result{}
	for _, source := range a.sessions {
		if source.Deleted || !source.FollowedByAssistant || source.Kind == assistantSessionKind {
			continue
		}
		for _, run := range source.Runs {
			if run == nil || run.WorkspaceID != "" && run.WorkspaceID != a.wsID() {
				continue
			}
			switch run.Status {
			case "completed", "failed", "awaiting_approval", "awaiting_clarification", "interrupted":
			default:
				continue
			}
			view := *source
			view.Runs = []*Task{run}
			if event := a.sessionWorkEventLocked(&view); event != nil {
				pending = append(pending, result{*event, run})
			}
		}
	}
	sort.Slice(pending, func(i, j int) bool {
		if pending[i].event.UpdatedAt != pending[j].event.UpdatedAt {
			return pending[i].event.UpdatedAt < pending[j].event.UpdatedAt
		}
		return pending[i].event.ID < pending[j].event.ID
	})
	changed := false
	for _, p := range pending {
		event, run := p.event, p.run
		text := assistantResultText(event, run)
		stableEvent := event
		stableEvent.Text = "" // Titles/session numbering may be repaired without a new result.
		digest := fmt.Sprintf("%x", sha256.Sum256([]byte(assistantResultEvidenceText(stableEvent, run))))
		if receipts[event.ID] == digest {
			// Refresh only our exact old auto-generated report, never user messages or
			// edited assistant text. The full evidence remains in the source task.
			legacy := assistantResultEvidenceText(event, run)
			for i := len(assistant.Messages) - 1; i >= 0; i-- {
				m := &assistant.Messages[i]
				if m.Type == "task-result" && m.SourceRunID == run.ID && m.Content == legacy && m.Content != text {
					m.Content = text
					changed = true
					break
				}
			}
			continue
		}
		assistant.Messages = append(assistant.Messages, Message{Role: "assistant", Type: "task-result", Content: text, SourceRunID: run.ID, CreatedAt: time.Now().UTC().Format(time.RFC3339Nano)})
		receipts[event.ID] = digest
		changed = true
	}
	if !changed {
		a.mu.Unlock()
		return
	}
	assistant.AssistantResultNotices = receipts
	assistant.Updated = time.Now().UTC().Format(time.RFC3339Nano)
	err := a.save(assistant)
	if err != nil {
		assistant.Messages = oldMessages
		assistant.Updated = oldUpdated
		assistant.AssistantResultNotices = oldReceipts
	}
	id := assistant.ID
	a.mu.Unlock()
	if err != nil {
		log.Printf("assistant result notification save failed: %v", err)
		return
	}
	a.broadcastSessionsChanged(id)
}
