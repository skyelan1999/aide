package server

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"
)

type knowledgeConclusionCandidate struct {
	Code       bool                             `json:"code"`
	Token      string                           `json:"token"`
	Node       string                           `json:"node"`
	Path       string                           `json:"path,omitempty"`
	Root       string                           `json:"root,omitempty"`
	Source     string                           `json:"source,omitempty"`
	Region     string                           `json:"region,omitempty"`
	Session    string                           `json:"session,omitempty"`
	SourceKind string                           `json:"sourceKind"`
	AI         *knowledgeAIConclusionProvenance `json:"ai,omitempty"`
	Quote      knowledgeConclusion              `json:"quote"`
}
type knowledgeConclusionPick struct {
	Snapshot  string `json:"snapshot"`
	Node      string `json:"node"`
	Candidate string `json:"candidate"`
	Start     int    `json:"start"`
	End       int    `json:"end"`
}
type knowledgeConclusionSelection struct {
	Pick           knowledgeConclusionPick      `json:"pick"`
	Candidate      knowledgeConclusionCandidate `json:"candidate"`
	SourceTextHash string                       `json:"sourceTextHash"`
	Recorded       string                       `json:"recorded"`
}

func knowledgeCandidateToken(s knowledgeTimeSnapshot, c knowledgeConclusionCandidate) string {
	c.Token = ""
	raw, _ := json.Marshal(struct {
		Workspace, Snapshot, Digest string
		Candidate                   knowledgeConclusionCandidate
	}{s.Graph.Workspace, s.ID, s.Digest, c})
	return hash(raw)
}

// Return only historical excerpts. A node filter allows a source outside the
// first bounded page to be selected without reading any current file.
func knowledgeConclusionCandidates(s knowledgeTimeSnapshot, node string, code ...bool) ([]knowledgeConclusionCandidate, bool) {
	view := len(code) > 0 && code[0]
	filtered := s
	if node != "" {
		filtered.Graph.Nodes = nil
		for _, n := range s.Graph.Nodes {
			if n.ID == node {
				filtered.Graph.Nodes = append(filtered.Graph.Nodes, n)
			}
		}
		if s.AI != nil {
			ai := *s.AI
			ai.Records = nil
			for _, r := range s.AI.Records {
				if r.Node == node {
					ai.Records = append(ai.Records, r)
				}
			}
			filtered.AI = &ai
		}
	}
	items, partial := knowledgeConclusionEvidenceItems(filtered)
	// A source body is selectable even when it has no explicit conclusion.
	blocked := knowledgeConclusionUnavailable(filtered.Graph)
	counts := map[string]int{}
	for _, n := range filtered.Graph.Nodes {
		counts[n.ID]++
	}
	for _, n := range filtered.Graph.Nodes {
		if n.Kind != "file" {
			continue
		}
		if counts[n.ID] != 1 || n.Path == "" || !utf8.ValidString(n.Text) || n.ReadState != "" && n.ReadState != "ready" {
			partial = true
			continue
		}
		if n.Text == "" {
			if n.Size != 0 || n.ReadState != "ready" {
				partial = true
			}
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
		if len(items) >= 256 {
			partial = true
			break
		}
		text := conclusionBytePrefix(n.Text, knowledgeConclusionTextBytes)
		complete := int64(len(n.Text)) == n.Size && (n.Size > 0 || n.ReadState == "ready")
		heading := "文档摘录（未标记结论）"
		if !knowledgeConclusionDocument(n) {
			heading = "已保存文本摘录（非Markdown，行号为保存文本行）"
		}
		q := knowledgeConclusion{Key: "saved_document_excerpt", Heading: heading, HeadingLine: 1, Line: 1, EndLine: strings.Count(text, "\n") + 1, Text: text, TextHash: hash([]byte(text)), SourceHash: hash([]byte(n.Text)), Partial: !complete || len(text) < len(n.Text)}
		items = append(items, knowledgeConclusionEvidenceItem{node: n, sourceKind: "document_excerpt", conclusion: q})
		// Preserve the original first-excerpt identity for existing selections.
		// Additional chunks expose only bytes already present in this snapshot.
		body := conclusionBytePrefix(n.Text, knowledgeConclusionScanBytes)
		heading = fmt.Sprintf("文档历史正文（已保存%d字节）", len(body))
		if !knowledgeConclusionDocument(n) {
			heading = fmt.Sprintf("已保存文本正文（非Markdown，已保存%d字节，文本行号）", len(body))
		}
		if !complete || len(body) != len(n.Text) {
			heading += "（非完整记录）"
		}
		for _, chunk := range knowledgeBodyChunks(body, "saved_document_body", heading, complete && len(body) == len(n.Text)) {
			items = append(items, knowledgeConclusionEvidenceItem{node: n, sourceKind: "document_excerpt", conclusion: chunk})
		}
	}
	if filtered.AI != nil && validateKnowledgeAIHistory(filtered.AI, filtered.Graph) == nil {
		for _, r := range filtered.AI.Records {
			if r.Body == nil {
				continue // Legacy observations never acquire today's text.
			}
			heading := fmt.Sprintf("AI历史正文（已保存%d/%d字节）", len(r.Body.Text), r.Body.SourceBytes)
			if !r.Body.Complete {
				heading += "（非完整记录）"
			}
			for _, chunk := range knowledgeBodyChunks(r.Body.Text, "saved_assistant_body", heading, r.Body.Complete) {
				items = append(items, knowledgeConclusionEvidenceItem{node: knowledgeNode{ID: r.Node, Kind: "session", Region: "sessions", Session: r.Session}, session: r.Session, sourceKind: "assistant_reply", conclusion: chunk, ai: knowledgeAIProvenance(r)})
			}
		}
	}
	out := []knowledgeConclusionCandidate{}
	used := 0
	for _, item := range items {
		q := item.conclusion
		q.Heading = conclusionBytePrefix(q.Heading, 256)
		q.HeadingPath = append([]string(nil), q.HeadingPath...)
		for i := range q.HeadingPath {
			q.HeadingPath[i] = conclusionBytePrefix(q.HeadingPath[i], 256)
		}
		c := knowledgeConclusionCandidate{Node: item.node.ID, Path: item.node.Path, Root: item.node.Root, Source: item.node.Source, Region: item.node.Region, Session: item.session, SourceKind: item.sourceKind, AI: item.ai, Quote: q}
		invalid := false
		for _, value := range []string{c.Node, c.Path, c.Root, c.Source, c.Region, c.Session} {
			invalid = invalid || !utf8.ValidString(value)
		}
		if invalid {
			partial = true
			continue
		}
		if len(c.Path) > 4096 || len(c.Node) > 128 || len(c.Source) > 128 || len(c.Region) > 256 || len(c.Session) > 128 {
			partial = true
			continue
		}
		c.Code = view
		c.Token = knowledgeCandidateToken(s, c)
		raw, _ := json.Marshal(c)
		if len(out) >= 128 || used+len(raw) > 256<<10 {
			partial = true
			break
		}
		out = append(out, c)
		used += len(raw)
	}
	return out, partial
}

// Browser textarea offsets are UTF-16 units. Reject offsets inside a surrogate
// pair instead of rounding into another user's selected character.
func knowledgeConclusionUTF16(text string, start, end int) (string, bool) {
	if start < 0 || end <= start {
		return "", false
	}
	units, a, b := 0, -1, -1
	for i, r := range text {
		if units == start {
			a = i
		}
		if units == end {
			b = i
			break
		}
		if r > 0xffff {
			units += 2
		} else {
			units++
		}
	}
	if units == end && b < 0 {
		b = len(text)
	}
	if a < 0 || b < a || b > len(text) {
		return "", false
	}
	return text[a:b], true
}
func knowledgeConclusionSelect(s knowledgeTimeSnapshot, p knowledgeConclusionPick, recorded string, code ...bool) (knowledgeConclusionSelection, error) {
	view := len(code) > 0 && code[0]
	bad := func() (knowledgeConclusionSelection, error) {
		return knowledgeConclusionSelection{}, fmt.Errorf("片段身份或选择范围已变化，请重新读取")
	}
	if p.Snapshot != s.ID || p.Node == "" || len(p.Node) > 128 || len(p.Candidate) != 64 {
		return bad()
	}
	candidates, _ := knowledgeConclusionCandidates(s, p.Node, view)
	for _, c := range candidates {
		if c.Token == p.Candidate {
			text, ok := knowledgeConclusionUTF16(c.Quote.Text, p.Start, p.End)
			if !ok || strings.TrimSpace(text) == "" {
				return bad()
			}
			original := c.Quote.Text
			// Locate source lines within the retained excerpt; never claim an unseen line.
			prefix := ""
			units := 0
			for i, r := range original {
				if units == p.Start {
					prefix = original[:i]
					break
				}
				if r > 0xffff {
					units += 2
				} else {
					units++
				}
			}
			q := c.Quote
			q.Line += strings.Count(prefix, "\n")
			if q.Line < 1 {
				q.Line = 1
			}
			q.EndLine = q.Line + strings.Count(strings.TrimSuffix(text, "\n"), "\n")
			q.Text = text
			q.TextHash = hash([]byte(text))
			c.Quote = q
			return knowledgeConclusionSelection{Pick: p, Candidate: c, SourceTextHash: hash([]byte(original)), Recorded: recorded}, nil
		}
	}
	return bad()
}
