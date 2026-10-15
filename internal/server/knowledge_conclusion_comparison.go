package server

import (
	"path"
	"strings"
)

type knowledgeNodeConclusionChange struct {
	Node   string                    `json:"node"`
	Path   string                    `json:"path,omitempty"`
	Root   string                    `json:"root,omitempty"`
	Source string                    `json:"source,omitempty"`
	Region string                    `json:"region,omitempty"`
	Change knowledgeConclusionChange `json:"change"`
}
type knowledgeConclusionComparison struct {
	Version        int                             `json:"version"`
	State          string                          `json:"state"`
	Changes        []knowledgeNodeConclusionChange `json:"changes"`
	Sources        int                             `json:"sources"`
	UnknownSources int                             `json:"unknownSources"`
	Truncated      bool                            `json:"truncated"`
	AIState        string                          `json:"aiState"`
	EvidenceState  string                          `json:"evidenceState"`
}

func knowledgeConclusionDocument(n knowledgeNode) bool {
	ext := strings.ToLower(n.Format)
	if ext == "" {
		ext = strings.ToLower(path.Ext(n.Path))
	}
	return n.Kind == "file" && (ext == ".md" || ext == ".markdown")
}

// The initial document phase reuses historical saved text only. It performs
// no current-source reads and never attributes mixed session text to an AI.
func compareKnowledgeConclusions(before, after knowledgeGraph) knowledgeConclusionComparison {
	out := knowledgeConclusionComparison{Version: 1, State: "partial_document_phase", Changes: []knowledgeNodeConclusionChange{}, AIState: "not_captured", EvidenceState: "not_captured"}
	old, current := map[string]knowledgeNode{}, map[string]knowledgeNode{}
	counts := map[string]int{}
	for _, n := range before.Nodes {
		old[n.ID] = n
		counts["before:"+n.ID]++
	}
	for _, n := range after.Nodes {
		current[n.ID] = n
		counts["after:"+n.ID]++
	}
	used := 0
	blockedSources := func(g knowledgeGraph) map[string]bool {
		blocked := map[string]bool{}
		for _, source := range g.Sources {
			region := source.Region
			if region == "" {
				region = source.ID
			}
			if source.State == "unavailable" || source.State == "disabled" {
				blocked[region] = true
			}
		}
		return blocked
	}
	oldBlocked, newBlocked := blockedSources(before), blockedSources(after)
	capture := func(n knowledgeNode, exists bool, blocked map[string]bool) *knowledgeConclusionSet {
		if !exists || !knowledgeConclusionDocument(n) {
			return nil
		}
		region := n.Region
		if region == "" {
			region = n.Root
			if n.Source != "" {
				region = "source:" + n.Source
			}
		}
		if blocked[region] {
			out.UnknownSources++
			return &knowledgeConclusionSet{Version: 1, Items: []knowledgeConclusion{}, Partial: true, Warnings: []string{"source_unavailable"}}
		}
		if n.ReadState != "" && n.ReadState != "ready" {
			out.UnknownSources++
			return &knowledgeConclusionSet{Version: 1, Items: []knowledgeConclusion{}, Partial: true, Warnings: []string{"source_not_ready"}}
		}
		bytes := min(len(n.Text), knowledgeConclusionScanBytes)
		if used+bytes > 256<<10 {
			out.UnknownSources++
			out.Truncated = true
			return nil
		}
		used += bytes
		complete := int64(len(n.Text)) == n.Size && (n.Size > 0 || n.ReadState == "ready")
		set := extractKnowledgeConclusions(n.Text, complete)
		if set.Partial {
			out.UnknownSources++
		}
		return &set
	}
	appendChanges := func(n knowledgeNode, changes []knowledgeConclusionChange) {
		for _, change := range changes {
			if len(out.Changes) >= 128 {
				out.Truncated = true
				return
			}
			out.Changes = append(out.Changes, knowledgeNodeConclusionChange{Node: n.ID, Path: n.Path, Root: n.Root, Source: n.Source, Region: n.Region, Change: change})
		}
	}
	for _, n := range after.Nodes {
		previous, exists := old[n.ID]
		if !knowledgeConclusionDocument(n) && !knowledgeConclusionDocument(previous) {
			continue
		}
		out.Sources++
		if counts["after:"+n.ID] > 1 || counts["before:"+n.ID] > 1 || exists && (n.Root != previous.Root || n.Source != previous.Source || n.Region != previous.Region || n.Path != previous.Path) {
			out.UnknownSources++
			appendChanges(n, []knowledgeConclusionChange{{Kind: "unknown", Reason: "source_identity_ambiguous"}})
			continue
		}
		appendChanges(n, compareKnowledgeConclusionSets(capture(previous, exists, oldBlocked), capture(n, true, newBlocked)))
	}
	for _, n := range before.Nodes {
		if _, exists := current[n.ID]; !exists && knowledgeConclusionDocument(n) {
			out.Sources++
			appendChanges(n, compareKnowledgeConclusionSets(capture(n, true, oldBlocked), nil))
		}
	}
	return out
}
