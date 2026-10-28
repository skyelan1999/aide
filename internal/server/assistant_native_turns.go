package server

// This ledger is private to one native socket reader. Provider item IDs bind
// responses even when ASR arrives after response.created or response.done.
type nativeReplyEvidence struct {
	id, turn, text        string
	inputs                []string
	done, canceled, saved bool
	proactive             bool
	review                *nativeSpeechReview
	quotation             nativeQuotation
}

type nativeTurnLedger struct {
	latest  string
	inputs  []string
	heard   map[string]string
	replies map[string]*nativeReplyEvidence
	order   []string
}

func newNativeTurnLedger() *nativeTurnLedger {
	return &nativeTurnLedger{heard: map[string]string{}, replies: map[string]*nativeReplyEvidence{}}
}

func (l *nativeTurnLedger) commit(id string) {
	if id == "" {
		return
	}
	for _, old := range l.inputs {
		if old == id {
			l.latest = id
			return
		}
	}
	if len(l.inputs) >= 128 {
		return
	}
	l.inputs = append(l.inputs, id)
	l.latest = id
}

func (l *nativeTurnLedger) transcript(id, text string) {
	if id == "" || len(l.heard) >= 128 {
		return
	}
	l.heard[id] = text
	found := false
	for _, old := range l.inputs {
		if old == id {
			found = true
			break
		}
	}
	if !found {
		l.commit(id)
	}
	// Some providers omit item_id on speech_stopped. Only unresolved replies
	// may bind to the arriving ASR; an existing binding is never overwritten.
	var unresolved []*nativeReplyEvidence
	for _, r := range l.replies {
		if r.turn == "" && !r.saved {
			unresolved = append(unresolved, r)
		}
	}
	if len(unresolved) == 1 {
		unresolved[0].turn = id
		unresolved[0].inputs = append(unresolved[0].inputs, id)
	}
}

func (l *nativeTurnLedger) created(id string) {
	if id == "" || len(l.replies) >= 128 {
		return
	}
	if _, exists := l.replies[id]; exists {
		return
	}
	l.replies[id] = &nativeReplyEvidence{id: id, turn: l.latest, inputs: append([]string{}, l.inputs...)}
	l.order = append(l.order, id)
}

func (l *nativeTurnLedger) ready() []*nativeReplyEvidence {
	var out []*nativeReplyEvidence
	for _, id := range l.order {
		r := l.replies[id]
		if r.done && !r.canceled && !r.saved && (r.review == nil || !r.review.enabled) && r.text != "" && l.heard[r.turn] != "" {
			out = append(out, r)
		}
	}
	return out
}
