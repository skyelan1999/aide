package server

import (
	"errors"
	"strconv"
	"strings"
	"time"
)

// ── 小秘跨会话工具（#30）─────────────────────────────────────────────────────
// 这四个工具只在小秘系统会话（Kind=assistant）的 VoiceAgent 上下文中暴露，
// 普通会话不可见。它们让小秘能：搜索历史会话、按 #N/ID 读取、标记跟进、向指定会话推送备注。
// 所有方法调用方需持有 a.mu（与会话列表/写盘同一把锁）。

// crossSessionSummary 搜索结果：轻量摘要，不含完整消息。
type crossSessionSummary struct {
	Number    int    `json:"number"`
	ID        string `json:"id"`
	Title     string `json:"title"`
	Summary   string `json:"summary"`
	CreatedAt string `json:"createdAt"`
	Pinned    bool   `json:"pinned"`
	Kind      string `json:"kind"`
}

// crossSessionToolNames 小秘会话可用的跨会话工具名清单。
var crossSessionToolNames = []string{"search_sessions", "get_session", "follow_session", "push_to_session"}

// crossSessionToolsFor 返回该会话上下文暴露的跨会话工具名；仅小秘系统会话返回非空，普通会话返回 nil。
func (a *App) crossSessionToolsFor(sess *Session) []string {
	if sess == nil || sess.Kind != assistantSessionKind {
		return nil
	}
	out := make([]string, len(crossSessionToolNames))
	copy(out, crossSessionToolNames)
	return out
}

// resolveSessionRef 按 "#N" 编号或 hash 会话 ID 定位会话；找不到返回 nil。调用方持 a.mu。
func (a *App) resolveSessionRef(ref string) *Session {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return nil
	}
	if strings.HasPrefix(ref, "#") {
		n, err := strconv.Atoi(strings.TrimSpace(ref[1:]))
		if err != nil || n <= 0 {
			return nil
		}
		for _, s := range a.sessions {
			if !s.Deleted && s.Number == n {
				return s
			}
		}
		return nil
	}
	if s := a.sessions[ref]; s != nil && !s.Deleted {
		return s
	}
	return nil
}

// searchSessionsTool 关键词搜索所有会话（含归档，默认），返回匹配摘要列表。调用方持 a.mu。
func (a *App) searchSessionsTool(keyword string, includeArchived bool) []crossSessionSummary {
	lower := strings.ToLower(strings.TrimSpace(keyword))
	out := []crossSessionSummary{}
	for _, s := range a.sessions {
		if s.Deleted {
			continue
		}
		if !includeArchived && s.Archived {
			continue
		}
		if lower != "" &&
			!strings.Contains(strings.ToLower(s.Title), lower) &&
			!strings.Contains(strings.ToLower(s.Compact), lower) {
			matched := false
			for _, m := range s.Messages {
				if strings.Contains(strings.ToLower(m.Content), lower) {
					matched = true
					break
				}
			}
			if !matched {
				continue
			}
		}
		summary := s.Compact
		if summary == "" && len(s.Messages) > 0 {
			summary = s.Messages[0].Content
		}
		out = append(out, crossSessionSummary{
			Number: s.Number, ID: s.ID, Title: s.Title,
			Summary: clip(summary, 120), CreatedAt: s.Created,
			Pinned: s.Pinned, Kind: s.Kind,
		})
	}
	return out
}

// getSessionTool 按 #N 或 hash ID 读取会话完整内容与状态。调用方持 a.mu。
func (a *App) getSessionTool(ref string) (*Session, error) {
	s := a.resolveSessionRef(ref)
	if s == nil {
		return nil, errors.New("会话不存在: " + ref)
	}
	return s, nil
}

// followSessionTool 标记跟进：在目标会话元数据加 followedByAssistant + followNote。调用方持 a.mu。
func (a *App) followSessionTool(ref, note string) (*Session, error) {
	s := a.resolveSessionRef(ref)
	if s == nil {
		return nil, errors.New("会话不存在: " + ref)
	}
	s.FollowedByAssistant = true
	s.FollowNote = strings.TrimSpace(note)
	if err := a.save(s); err != nil {
		return nil, err
	}
	return s, nil
}

// pushToSessionTool 向指定会话推送一条系统备注消息。调用方持 a.mu。
func (a *App) pushToSessionTool(ref, message string) (*Session, error) {
	s := a.resolveSessionRef(ref)
	if s == nil {
		return nil, errors.New("会话不存在: " + ref)
	}
	s.Messages = append(s.Messages, Message{
		Role:    "system",
		Content: "[小秘推送] " + strings.TrimSpace(message),
	})
	s.Updated = time.Now().UTC().Format(time.RFC3339Nano)
	if err := a.save(s); err != nil {
		return nil, err
	}
	return s, nil
}
