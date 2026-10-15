package server

import (
	"encoding/json"
)

type knowledgeAIConclusionProvenance struct {
	Run             string `json:"run"`
	QuestionHash    string `json:"questionHash,omitempty"`
	QuestionBound   bool   `json:"questionBound"`
	QuestionPreview string `json:"questionPreview"`
	Status          string `json:"status"`
	Model           string `json:"model,omitempty"`
}

func knowledgeAIRecordKey(r knowledgeAIConclusionRecord) string {
	if r.QuestionBound {
		return r.Node + "\x00" + r.Session + "\x00question:" + r.QuestionHash
	}
	return r.Node + "\x00" + r.Session + "\x00run:" + r.Run
}
func knowledgeAIProvenance(r knowledgeAIConclusionRecord) *knowledgeAIConclusionProvenance {
	return &knowledgeAIConclusionProvenance{r.Run, r.QuestionHash, r.QuestionBound, r.QuestionPreview, r.Status, r.Model}
}
func knowledgeAIRecordSet(r knowledgeAIConclusionRecord) knowledgeConclusionSet {
	if !r.Unclassified {
		return r.Conclusions
	}
	return knowledgeConclusionSet{Version: 1, Partial: r.Conclusions.Partial, Warnings: r.Conclusions.Warnings, Items: []knowledgeConclusion{{Key: "unclassified_reply", Heading: "AI答复（未标记结论）", Text: r.ReplyExcerpt, TextHash: r.ReplyHash, Partial: r.Conclusions.Partial}}}
}

func appendKnowledgeAIComparison(out *knowledgeConclusionComparison, before, after *knowledgeAIConclusionSnapshot, workspace string) {
	if before == nil && after == nil {
		return
	}
	valid := func(s *knowledgeAIConclusionSnapshot) bool {
		return s == nil || (s.Version == 1 || s.Version == 2) && s.Workspace == workspace
	}
	if !valid(before) || !valid(after) {
		out.AIState = "identity_unavailable"
		out.UnknownSources++
		return
	}
	out.State = "partial_ai_document_phase"
	out.AIState = "captured_scoped"
	if before == nil || after == nil {
		out.AIState = "legacy_capture_unknown"
	} else if before.Partial || after.Partial {
		out.AIState = "partial"
	}
	old, current := map[string]knowledgeAIConclusionRecord{}, map[string]knowledgeAIConclusionRecord{}
	if before != nil {
		for _, r := range before.Records {
			old[knowledgeAIRecordKey(r)] = r
		}
	}
	if after != nil {
		for _, r := range after.Records {
			current[knowledgeAIRecordKey(r)] = r
		}
	}
	appendChange := func(r knowledgeAIConclusionRecord, previous *knowledgeAIConclusionRecord, change knowledgeConclusionChange) {
		if len(out.Changes) >= 128 {
			out.Truncated = true
			return
		}
		kind := "assistant_conclusion"
		if r.Unclassified {
			kind = "assistant_reply"
		}
		item := knowledgeNodeConclusionChange{Node: r.Node, SourceKind: kind, Session: r.Session, Region: "sessions", Change: change, AfterAI: knowledgeAIProvenance(r)}
		if previous != nil {
			item.BeforeAI = knowledgeAIProvenance(*previous)
		}
		out.Changes = append(out.Changes, item)
	}
	if after != nil {
		for _, r := range after.Records {
			out.Sources++
			prev, exists := old[knowledgeAIRecordKey(r)]
			set := knowledgeAIRecordSet(r)
			if !exists {
				for _, c := range set.Items {
					copy := c
					appendChange(r, nil, knowledgeConclusionChange{Kind: "observed", Reason: "new_question_or_previous_ai_unknown", After: &copy})
				}
				continue
			}
			if prev.Unclassified != r.Unclassified {
				appendChange(r, &prev, knowledgeConclusionChange{Kind: "unknown", Reason: "ai_reply_classification_changed"})
				continue
			}
			previous := knowledgeAIRecordSet(prev)
			changes := compareKnowledgeConclusionSets(&previous, &set)
			for _, change := range changes {
				if !r.QuestionBound || !prev.QuestionBound {
					change.Kind = "unknown"
					change.Reason = "question_not_bound"
				}
				if r.Unclassified && change.Kind == "changed" {
					change.Kind = "reply_changed"
					change.Reason = "unclassified_assistant_text_changed"
				}
				appendChange(r, &prev, change)
			}
			if len(changes) == 0 && (prev.Status != r.Status || prev.Model != r.Model) {
				appendChange(r, &prev, knowledgeConclusionChange{Kind: "state_changed", Reason: "assistant_record_state_changed"})
			}
		}
	}
	if before != nil {
		for _, r := range before.Records {
			if _, exists := current[knowledgeAIRecordKey(r)]; !exists {
				out.Sources++
				if len(out.Changes) >= 128 {
					out.Truncated = true
					break
				}
				out.Changes = append(out.Changes, knowledgeNodeConclusionChange{Node: r.Node, Session: r.Session, SourceKind: "assistant_conclusion", Region: "sessions", BeforeAI: knowledgeAIProvenance(r), Change: knowledgeConclusionChange{Kind: "unknown", Reason: "ai_record_not_in_current_capture"}})
			}
		}
	}
}

// AI capture data must not escape through ordinary graph/model payloads.
func knowledgeAIHistoryBytes(ai *knowledgeAIConclusionSnapshot) int {
	b, _ := json.Marshal(ai)
	return len(b)
}
