package server

import "context"

// An origin supplied only by the internal delegation path, never by run JSON.
type assistantDispatchKey struct{}

const assistantTaskMessageType = "assistant-task"

func assistantMessageType(managed bool) string {
	if managed {
		return assistantTaskMessageType
	}
	return ""
}

func (a *App) taskAutoReviewLocked(task *Task) bool {
	return a.approvalPolicy.Enabled || task.AssistantManaged && a.settings.AssistantAutoReview
}

// Caller holds a.mu. Revoke old generations before scheduling new reviews.
func (a *App) refreshAssistantReviewsLocked() {
	for _, session := range a.sessions {
		if session.Deleted {
			continue
		}
		for _, task := range session.Runs {
			if !task.AssistantManaged {
				continue
			}
			switch task.Status {
			case "running", "awaiting_approval", "awaiting_clarification", "queued", "paused":
			default:
				continue
			}
			task.AutoReview = a.taskAutoReviewLocked(task)
			task.approvalGeneration++
			if task.approvalCancel != nil {
				task.approvalCancel()
			}
			task.ApprovalState = "manual"
			if err := a.save(session); err != nil {
				task.AutoReview = false
				continue
			}
			if task.AutoReview && hasPendingApproval(task) {
				go func(t *Task) { a.reviewPendingCommand(t); a.reviewPendingFiles(context.Background(), t) }(task)
			}
		}
	}
	go a.broadcastSessionsChanged("")
}
