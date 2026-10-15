package server

import "encoding/json"

type knowledgeManualConclusionComparison struct {
	ID              string                              `json:"id"`
	Label           string                              `json:"label"`
	Alignment       string                              `json:"alignment"`
	State           string                              `json:"state"`
	TextChanged     bool                                `json:"textChanged"`
	HeadingChanged  bool                                `json:"headingChanged"`
	PositionChanged bool                                `json:"positionChanged"`
	SourceChanged   bool                                `json:"sourceChanged"`
	Before          *knowledgeConclusionSelection       `json:"before,omitempty"`
	After           *knowledgeConclusionSelection       `json:"after,omitempty"`
	Evidence        []knowledgeConclusionEvidenceChange `json:"evidence"`
}

func knowledgeSelectionEvidenceItem(id string, m knowledgeConclusionSelection) knowledgeConclusionEvidenceItem {
	c := m.Candidate
	kind := "file"
	if c.Session != "" {
		kind = "session"
	}
	return knowledgeConclusionEvidenceItem{identity: id, node: knowledgeNode{ID: c.Node, Kind: kind, Root: c.Root, Source: c.Source, Region: c.Region, Path: c.Path, Session: c.Session}, session: c.Session, sourceKind: c.SourceKind, conclusion: c.Quote, bound: true, manual: true, ai: c.AI, selection: &m}
}
func appendKnowledgeManualTopics(out *knowledgeConclusionComparison, before, after knowledgeTimeSnapshot, topics knowledgeConclusionTopics) {
	out.Manual = []knowledgeManualConclusionComparison{}
	if before.Graph.Workspace != after.Graph.Workspace || topics.Workspace != before.Graph.Workspace {
		out.ManualState = "workspace_identity_unknown"
		return
	}
	out.ManualState = "explicit_user_alignment"
	oldIndex, nextIndex := newKnowledgeConclusionReferenceIndex(before.Graph), newKnowledgeConclusionReferenceIndex(after.Graph)
	used := 0
	for _, topic := range topics.Topics {
		if topic.Disabled {
			continue
		}
		r := knowledgeManualConclusionComparison{ID: topic.ID, Label: topic.Label, Alignment: "explicit_user", State: "not_aligned_for_pair", Evidence: []knowledgeConclusionEvidenceChange{}}
		for _, member := range topic.Members {
			m := member
			if m.Pick.Snapshot == before.ID {
				r.Before = &m
			}
			if m.Pick.Snapshot == after.ID {
				r.After = &m
			}
		}
		if r.Before == nil && r.After == nil {
			continue
		}
		if r.Before != nil && r.After != nil {
			if !knowledgeTopicSelectionCurrent(before, *r.Before) || !knowledgeTopicSelectionCurrent(after, *r.After) {
				r.State = "anchor_mismatch"
			} else {
				r.State = "selected_excerpts_compared"
				a, b := r.Before.Candidate, r.After.Candidate
				r.TextChanged = a.Quote.TextHash != b.Quote.TextHash
				aHead, _ := json.Marshal(a.Quote.HeadingPath)
				bHead, _ := json.Marshal(b.Quote.HeadingPath)
				r.HeadingChanged = a.Quote.Heading != b.Quote.Heading || string(aHead) != string(bHead)
				r.PositionChanged = a.Quote.Line != b.Quote.Line || a.Quote.EndLine != b.Quote.EndLine || r.Before.Pick.Start != r.After.Pick.Start || r.Before.Pick.End != r.After.Pick.End
				r.SourceChanged = a.Node != b.Node || a.Root != b.Root || a.Source != b.Source || a.Region != b.Region || a.Path != b.Path || a.Session != b.Session || a.SourceKind != b.SourceKind
				if a.AI != nil && b.AI != nil && (a.AI.Run != b.AI.Run || a.AI.QuestionHash != b.AI.QuestionHash) {
					r.SourceChanged = true
				}
				evidence := knowledgeConclusionComparison{Evidence: []knowledgeConclusionEvidenceChange{}}
				compareKnowledgeEvidenceItems(&evidence, before, after, []knowledgeConclusionEvidenceItem{knowledgeSelectionEvidenceItem(topic.ID, *r.Before)}, []knowledgeConclusionEvidenceItem{knowledgeSelectionEvidenceItem(topic.ID, *r.After)}, oldIndex, nextIndex)
				r.Evidence = evidence.Evidence
				if evidence.Truncated {
					out.ManualState = "partial_explicit_user_alignment"
				}
			}
		}
		raw, _ := json.Marshal(r)
		used += len(raw)
		if used > 512<<10 {
			out.ManualState = "partial_explicit_user_alignment"
			out.Truncated = true
			break
		}
		out.Manual = append(out.Manual, r)
	}
}
