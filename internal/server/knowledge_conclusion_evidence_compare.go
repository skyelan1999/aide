package server

import "encoding/json"

type knowledgeConclusionEvidenceItem struct {
	identity   string
	node       knowledgeNode
	session    string
	sourceKind string
	conclusion knowledgeConclusion
	bound      bool
	ai         *knowledgeAIConclusionProvenance
	manual     bool
}

// Collection is independent of quote deltas: unchanged words can cite changed
// evidence. All targets come from this snapshot, never the live filesystem.
func knowledgeConclusionEvidenceItems(s knowledgeTimeSnapshot) ([]knowledgeConclusionEvidenceItem, bool) {
	items := []knowledgeConclusionEvidenceItem{}
	used := 0
	partial := s.Graph.Truncated
	seen := map[string]int{}
	blocked := knowledgeConclusionUnavailable(s.Graph)
	for _, n := range s.Graph.Nodes {
		seen[n.ID]++
	}
	for _, n := range s.Graph.Nodes {
		if !knowledgeConclusionDocument(n) {
			continue
		}
		region := n.Region
		if region == "" {
			region = n.Root
			if n.Source != "" {
				region = "source:" + n.Source
			}
		}
		if blocked[region] {
			partial = true
			continue
		}
		if seen[n.ID] != 1 || n.ReadState != "" && n.ReadState != "ready" {
			partial = true
			continue
		}
		available := min(len(n.Text), knowledgeConclusionScanBytes)
		if used+available > 256<<10 {
			partial = true
			break
		}
		used += available
		complete := int64(len(n.Text)) == n.Size && (n.Size > 0 || n.ReadState == "ready")
		set := extractKnowledgeConclusions(n.Text, complete)
		partial = partial || set.Partial
		for _, c := range set.Items {
			if len(items) >= 128 {
				return items, true
			}
			items = append(items, knowledgeConclusionEvidenceItem{identity: n.ID + "\x00document\x00" + c.Key, node: n, sourceKind: "document", conclusion: c, bound: !c.Ambiguous})
		}
	}
	if s.AI != nil {
		partial = partial || s.AI.Partial
		if validateKnowledgeAIHistory(s.AI, s.Graph) != nil {
			return items, true
		}
		for _, r := range s.AI.Records {
			kind := "assistant_conclusion"
			if r.Unclassified {
				kind = "assistant_reply"
			}
			set := knowledgeAIRecordSet(r)
			for _, c := range set.Items {
				if len(items) >= 128 {
					return items, true
				}
				items = append(items, knowledgeConclusionEvidenceItem{identity: knowledgeAIRecordKey(r) + "\x00" + kind + "\x00" + c.Key, node: knowledgeNode{ID: r.Node, Kind: "session", Region: "sessions", Session: r.Session}, session: r.Session, sourceKind: kind, conclusion: c, bound: r.QuestionBound && !c.Ambiguous, ai: knowledgeAIProvenance(r)})
			}
		}
	}
	return items, partial
}

func appendKnowledgeConclusionEvidence(out *knowledgeConclusionComparison, before, after knowledgeTimeSnapshot) {
	out.Evidence = []knowledgeConclusionEvidenceChange{}
	if before.Graph.Workspace != after.Graph.Workspace {
		out.EvidenceState = "workspace_identity_unknown"
		return
	}
	out.EvidenceState = "saved_snapshot_explicit_references"
	out.State = "partial_conclusion_evidence_phase"
	old, oldPartial := knowledgeConclusionEvidenceItems(before)
	next, nextPartial := knowledgeConclusionEvidenceItems(after)
	if oldPartial || nextPartial {
		out.EvidenceState = "partial_saved_snapshot_references"
	}
	oldIndex, nextIndex := newKnowledgeConclusionReferenceIndex(before.Graph), newKnowledgeConclusionReferenceIndex(after.Graph)
	compareKnowledgeEvidenceItems(out, before, after, old, next, oldIndex, nextIndex)
}
func compareKnowledgeEvidenceItems(out *knowledgeConclusionComparison, before, after knowledgeTimeSnapshot, old, next []knowledgeConclusionEvidenceItem, oldIndex, nextIndex *knowledgeConclusionReferenceIndex) {
	previous := map[string]knowledgeConclusionEvidenceItem{}
	for _, item := range old {
		previous[item.identity] = item
	}
	current := map[string]bool{}
	encodedBytes, refs := 0, 0
	appendItem := func(item knowledgeConclusionEvidenceItem, oldItem *knowledgeConclusionEvidenceItem) {
		var a, b *knowledgeConclusionEvidenceSet
		if oldItem != nil {
			s := knowledgeConclusionEvidence(oldItem.conclusion.Text, oldItem.node, before.ID, oldIndex, oldItem.conclusion.Partial)
			a = &s
		}
		if _, exists := current[item.identity]; exists {
			s := knowledgeConclusionEvidence(item.conclusion.Text, item.node, after.ID, nextIndex, item.conclusion.Partial)
			b = &s
		}
		if (a == nil || len(a.References) == 0 && !a.Partial) && (b == nil || len(b.References) == 0 && !b.Partial) {
			return
		}
		r := knowledgeConclusionEvidenceChange{Node: item.node.ID, Path: item.node.Path, Session: item.session, SourceKind: item.sourceKind, ConclusionKey: item.conclusion.Key, Heading: conclusionBytePrefix(item.conclusion.Heading, 256), Before: a, After: b, Kind: "observed"}
		if oldItem != nil {
			q := oldItem.conclusion
			r.BeforeQuote = &q
			r.BeforeAI = oldItem.ai
		}
		if b != nil {
			q := item.conclusion
			r.AfterQuote = &q
			r.AfterAI = item.ai
		}
		if a != nil {
			refs += len(a.References)
			r.Unknown = r.Unknown || a.Partial
		}
		if b != nil {
			refs += len(b.References)
			r.Unknown = r.Unknown || b.Partial
		}
		if refs > 512 {
			out.Truncated = true
			out.EvidenceState = "partial_saved_snapshot_references"
			return
		}
		aligned := oldItem != nil && item.bound && oldItem.bound && (item.manual && oldItem.manual || knowledgeConclusionArea(item.node) == knowledgeConclusionArea(oldItem.node) && item.node.Path == oldItem.node.Path)
		if a == nil || b == nil || !aligned {
			r.Unknown = true
			r.Kind = "unknown"
		} else {
			r.ReferencesChanged = knowledgeConclusionReferenceSignature(a) != knowledgeConclusionReferenceSignature(b)
			byReference := map[string]knowledgeConclusionReference{}
			byTarget := map[string]knowledgeConclusionReference{}
			for _, ref := range a.References {
				byReference[ref.Kind+"\x00"+ref.Reference] = ref
				if ref.Node != "" {
					byTarget[knowledgeConclusionReferenceIdentity(ref)] = ref
				}
			}
			for _, ref := range b.References {
				if prev, ok := byReference[ref.Kind+"\x00"+ref.Reference]; ok {
					if prev.State != ref.State || prev.Complete != ref.Complete {
						r.AvailabilityChanged = true
					}
					if prev.Node != "" && ref.Node != "" && knowledgeConclusionReferenceIdentity(prev) != knowledgeConclusionReferenceIdentity(ref) {
						r.Unknown = true
						r.AvailabilityChanged = true
					}
				}
				if prev, ok := byTarget[knowledgeConclusionReferenceIdentity(ref)]; ok && prev.State == "saved_excerpt" && ref.State == "saved_excerpt" {
					// Different excerpt extents can mean improved coverage, not a
					// content edit. Code hashes are separately labelled index hashes.
					if prev.ExcerptHash != ref.ExcerptHash {
						if prev.Complete && ref.Complete || prev.ExcerptBytes == ref.ExcerptBytes {
							r.ContentChanged = true
						} else {
							r.Unknown = true
						}
					}
					if prev.IndexedCodeHash != "" && ref.IndexedCodeHash != "" && prev.IndexedCodeHash != ref.IndexedCodeHash {
						r.ContentChanged = true
					}
				}
			}
			if r.ReferencesChanged || r.ContentChanged || r.AvailabilityChanged {
				r.Kind = "evidence_changed"
			} else if r.Unknown {
				r.Kind = "unknown"
			} else {
				return
			}
		}
		if r.Unknown {
			out.EvidenceState = "partial_saved_snapshot_references"
		}
		raw, _ := json.Marshal(r)
		encodedBytes += len(raw)
		if len(out.Evidence) >= 128 || encodedBytes > 512<<10 || refs > 512 {
			out.Truncated = true
			out.EvidenceState = "partial_saved_snapshot_references"
			return
		}
		out.Evidence = append(out.Evidence, r)
	}
	for _, item := range next {
		current[item.identity] = true
	}
	for _, item := range next {
		if oldItem, exists := previous[item.identity]; exists {
			appendItem(item, &oldItem)
		} else {
			appendItem(item, nil)
		}
		if out.Truncated && (encodedBytes > 512<<10 || refs > 512) {
			break
		}
	}
	for _, item := range old {
		if !current[item.identity] {
			appendItem(item, &item)
			if encodedBytes > 512<<10 || refs > 512 {
				break
			}
		}
	}
}
