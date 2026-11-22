package server

import (
	"regexp"
	"strings"
	"time"
)

// Full-utterance allowlist: quoted/hypothetical speech, negation and stopping a
// task cannot become authority to hang up. Only the current call ASR is used.
var nativeHangupWords = regexp.MustCompile(`^(?:好了|好的|好|行|那|我们|咱们|我先|先|现在|你|给|我|帮忙|帮我|请|麻烦|把|这次|这个|这通|电话|通话|回头聊|改天聊|一会再聊|一会儿再聊|晚点聊|稍后再聊|下次聊|就这样|挂断|挂掉|挂了|挂机|挂|结束|就|可以|吧|了|啊|嗯|呃|呀|啦|一下|咯|喽|再见|拜拜|拜|[\s，,。.!！?？…])+$`)
var nativeHangupAction = regexp.MustCompile(`挂(?:断|掉|了|吧|一下|电话|机)|结束(?:这次|这通)?通话|再见|拜拜`)

func nativeUserRequestsHangup(text string) bool {
	text = strings.TrimSpace(text)
	lower := strings.Trim(strings.ToLower(text), " .!?，。！？")
	if lower == "bye" || lower == "goodbye" || lower == "hang up" || lower == "please hang up" {
		return true
	}
	return nativeHangupWords.MatchString(text) && nativeHangupAction.MatchString(text)
}

type nativePhoneHangup struct {
	turn     string
	deadline time.Time
}

func (h *nativePhoneHangup) observe(l *nativeTurnLedger, turn, text string, speaking bool, now time.Time) map[string]any {
	if turn == "" || turn != l.latest || speaking {
		return nil
	}
	if !nativeUserRequestsHangup(text) {
		if h.turn != "" {
			old := h.turn
			h.turn = ""
			h.deadline = time.Time{}
			return map[string]any{"type": "aide.call_end_cancel", "turnId": old}
		}
		return nil
	}
	if h.turn == turn {
		return nil
	}
	h.turn = turn
	h.deadline = now.Add(8 * time.Second)
	event := map[string]any{"type": "aide.call_end", "turnId": turn, "reason": "user-request"}
	for i := len(l.order) - 1; i >= 0; i-- {
		reply := l.replies[l.order[i]]
		if reply != nil && reply.turn == turn && !reply.canceled && !reply.proactive {
			event["responseId"] = reply.id
			event["responseDone"] = reply.done
			break
		}
	}
	return event
}
func (h *nativePhoneHangup) expired(now time.Time) bool {
	return h.turn != "" && !now.Before(h.deadline)
}
