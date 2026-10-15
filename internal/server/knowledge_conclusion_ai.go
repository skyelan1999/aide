package server

import (
	"encoding/json"
	"fmt"
	"sort"
	"unicode/utf8"
)

type knowledgeAIConclusionRecord struct {
	Origin          string                 `json:"origin"`
	Node            string                 `json:"node"`
	Session         string                 `json:"session"`
	SessionTitle    string                 `json:"sessionTitle"`
	Run             string                 `json:"run"`
	QuestionHash    string                 `json:"questionHash,omitempty"`
	QuestionBound   bool                   `json:"questionBound"`
	QuestionPreview string                 `json:"questionPreview"`
	Status          string                 `json:"status"`
	Model           string                 `json:"model,omitempty"`
	Unclassified    bool                   `json:"unclassified,omitempty"`
	ReplyExcerpt    string                 `json:"replyExcerpt,omitempty"`
	ReplyHash       string                 `json:"replyHash,omitempty"`
	Conclusions     knowledgeConclusionSet `json:"conclusions"`
	Body            *knowledgeAIReplyBody  `json:"body,omitempty"`
}
type knowledgeAIConclusionSnapshot struct {
	Version      int                           `json:"version"`
	Workspace    string                        `json:"workspace"`
	Records      []knowledgeAIConclusionRecord `json:"records"`
	Partial      bool                          `json:"partial"`
	Unattributed int                           `json:"unattributed"`
}

// Caller owns the App/session lock. This sidecar is stored only in encrypted
// history, never attached to graph nodes used for model requests or map caches.
func captureKnowledgeAIConclusions(g knowledgeGraph, sessions map[string]*Session, scope string) *knowledgeAIConclusionSnapshot {
	out := &knowledgeAIConclusionSnapshot{Version: 2, Workspace: g.Workspace, Records: []knowledgeAIConclusionRecord{}}
	type source struct {
		node    knowledgeNode
		session *Session
	}
	sources := []source{}
	for _, n := range g.Nodes {
		if n.Kind == "session" {
			if len(n.Session) > 128 {
				out.Partial = true
				continue
			}
			if s := sessions[n.Session]; s != nil && !s.Deleted && s.Kind != "assistant" {
				sources = append(sources, source{n, s})
			}
		}
	}
	sort.Slice(sources, func(i, j int) bool {
		a, b := conclusionBytePrefix(sources[i].session.Updated, 64), conclusionBytePrefix(sources[j].session.Updated, 64)
		if a != b {
			return a > b
		}
		return sources[i].node.ID < sources[j].node.ID
	})
	for _, s := range g.Sources {
		if s.Region == "sessions" && s.State != "ready" {
			out.Partial = true
		}
	}
	inputBytes, outputBytes, messageScans, runScans := 0, 0, 0, 0
	for _, source := range sources {
		if len(out.Records) >= 32 || outputBytes >= 64<<10 || inputBytes >= 256<<10 || messageScans >= 4096 || runScans >= 4096 {
			out.Partial = true
			break
		}
		s := source.session
		if len(s.ID) > 128 || !utf8.ValidString(s.ID) {
			out.Partial = true
			continue
		}
		if s.Compact != "" || s.CompactedMessages > 0 {
			out.Partial = true
		}
		start := max(0, len(s.Messages)-256)
		if start > 0 {
			out.Partial = true
		}
		wanted := map[string]bool{}
		for _, m := range s.Messages[start:] {
			if m.SourceRunID != "" && len(m.SourceRunID) <= 128 {
				wanted[m.SourceRunID] = true
			}
		}
		runs := map[string]*Task{}
		duplicates := map[string]bool{}
		scanComplete := true
		for i := len(s.Runs) - 1; i >= 0; i-- {
			if runScans >= 4096 {
				scanComplete = false
				out.Partial = true
				break
			}
			runScans++
			r := s.Runs[i]
			if r == nil || len(r.ID) > 128 || !wanted[r.ID] {
				continue
			}
			if _, exists := runs[r.ID]; exists {
				duplicates[r.ID] = true
			}
			runs[r.ID] = r
		}
		if !scanComplete {
			continue
		}
		type questionInput struct {
			Content string
			Type    string
		}
		inputs := map[string][]questionInput{}
		anchored := map[string]bool{}
		seenUser := map[string]bool{}
		unknown := map[string]bool{}
		type candidate struct {
			message  Message
			run      *Task
			question string
			bound    bool
		}
		candidates := []candidate{}
		active := ""
		for _, m := range s.Messages[start:] {
			if messageScans >= 4096 {
				out.Partial = true
				break
			}
			messageScans++
			if len(m.SourceRunID) > 128 {
				out.Partial = true
				if m.Role == "assistant" {
					out.Unattributed++
				}
				continue
			}
			r := runs[m.SourceRunID]
			if m.Role == "user" {
				if m.SourceRunID == "" {
					if active != "" {
						unknown[active] = true
					}
					continue
				}
				active = ""
				if r == nil || r.WorkspaceID != scope || duplicates[r.ID] {
					continue
				}
				active = r.ID
				if !seenUser[r.ID] {
					anchored[r.ID] = m.Content == r.Prompt && r.Prompt != ""
					seenUser[r.ID] = true
				}
				if len(m.Content) > 16<<10 || len(m.Type) > 64 || inputBytes+len(m.Content)+len(m.Type) > 256<<10 || !utf8.ValidString(m.Content) || !utf8.ValidString(m.Type) {
					unknown[r.ID] = true
					out.Partial = true
					continue
				}
				inputBytes += len(m.Content) + len(m.Type)
				inputs[r.ID] = append(inputs[r.ID], questionInput{m.Content, m.Type})
				continue
			}
			if m.Role != "assistant" || m.Content == "" || m.Type != "" || len(m.ToolCalls) > 0 || m.ToolCallID != "" {
				continue
			}
			if r == nil || duplicates[m.SourceRunID] || len(r.ID) > 128 || !utf8.ValidString(r.ID) || len(r.Status) > 64 || !utf8.ValidString(r.Status) {
				out.Unattributed++
				out.Partial = true
				continue
			}
			if r.WorkspaceID != scope {
				continue
			}
			bound := anchored[r.ID] && !unknown[r.ID]
			question := ""
			if bound {
				raw, _ := json.Marshal(struct {
					Workspace string
					Inputs    []questionInput
				}{g.Workspace, inputs[r.ID]})
				question = hash(raw)
			}
			candidates = append(candidates, candidate{m, r, question, bound})
		}
		seenRuns, seenTopics := map[string]bool{}, map[string]bool{}
		perSession := 0
		for i := len(candidates) - 1; i >= 0; i-- {
			c := candidates[i]
			r := c.run
			if seenRuns[r.ID] {
				continue
			}
			seenRuns[r.ID] = true
			if c.bound && seenTopics[c.question] {
				continue
			}
			if c.bound {
				seenTopics[c.question] = true
			}
			if perSession >= 4 || len(out.Records) >= 32 {
				out.Partial = true
				break
			}
			available := min(knowledgeConclusionScanBytes, (256<<10)-inputBytes)
			if available <= 0 {
				out.Partial = true
				break
			}
			text := conclusionBytePrefix(c.message.Content, available)
			inputBytes += len(text)
			if !utf8.ValidString(text) {
				out.Partial = true
				out.Unattributed++
				continue
			}
			complete := len(text) == len(c.message.Content) && r.Status == "completed"
			set := extractKnowledgeConclusions(text, complete)
			if len(set.Items) > 4 {
				set.Items = set.Items[:4]
				set.Partial = true
				set.Warnings = append(set.Warnings, "ai_item_limit")
			}
			record := knowledgeAIConclusionRecord{Origin: "persisted_assistant", Node: source.node.ID, Session: s.ID, SessionTitle: knowledgeClip(s.Title, 80), Run: r.ID, QuestionHash: c.question, QuestionBound: c.bound, QuestionPreview: conclusionBytePrefix(r.Prompt, 512), Status: r.Status, Model: conclusionBytePrefix(r.Model, 256), Conclusions: set}
			if !c.bound {
				out.Partial = true
			}
			if !utf8.ValidString(record.QuestionPreview) {
				record.QuestionPreview = ""
			}
			if !utf8.ValidString(record.Model) {
				record.Model = ""
			}
			if !utf8.ValidString(record.SessionTitle) {
				record.SessionTitle = ""
			}
			if len(set.Items) == 0 {
				record.Unclassified = true
				record.ReplyExcerpt = conclusionBytePrefix(text, knowledgeConclusionTextBytes)
				record.ReplyHash = hash([]byte(record.ReplyExcerpt))
				if len(record.ReplyExcerpt) < len(text) {
					record.Conclusions.Partial = true
				}
			}
			// Keep the bounded original independently of the small conclusion
			// excerpts. This never reads a live file or enters ordinary graph data.
			record.Body = &knowledgeAIReplyBody{Text: text, Hash: hash([]byte(text)), SourceBytes: len(c.message.Content), Complete: complete}
			body := record.Body
			record.Body = nil
			encoded, _ := json.Marshal(record)
			if outputBytes+len(encoded) > 64<<10 {
				out.Partial = true
				break
			}
			outputBytes += len(encoded)
			record.Body = body
			out.Records = append(out.Records, record)
			perSession++
		}
	}
	sort.Slice(out.Records, func(i, j int) bool {
		a, b := out.Records[i], out.Records[j]
		if a.Node != b.Node {
			return a.Node < b.Node
		}
		if a.QuestionHash != b.QuestionHash {
			return a.QuestionHash < b.QuestionHash
		}
		return a.Run < b.Run
	})
	return out
}

func validateKnowledgeAIHistory(ai *knowledgeAIConclusionSnapshot, g knowledgeGraph) error {
	if ai == nil {
		return nil
	}
	invalid := func() error { return fmt.Errorf("AI结论历史身份或格式无效；保留原文件") }
	if (ai.Version != 1 && ai.Version != 2) || ai.Workspace != g.Workspace || len(ai.Records) > 32 || ai.Unattributed < 0 {
		return invalid()
	}
	nodes := map[string]knowledgeNode{}
	counts := map[string]int{}
	for _, n := range g.Nodes {
		nodes[n.ID] = n
		counts[n.ID]++
	}
	seen := map[string]bool{}
	rawBytes := len(ai.Workspace)
	for _, r := range ai.Records {
		if !validateKnowledgeAIReplyBody(ai.Version, r) {
			return invalid()
		}
		n := nodes[r.Node]
		key := knowledgeAIRecordKey(r)
		if seen[key] || counts[r.Node] != 1 || n.Kind != "session" || n.Session != r.Session || r.Origin != "persisted_assistant" || r.Run == "" || len(r.Run) > 128 || len(r.Status) > 64 || r.Conclusions.Version != 1 || len(r.Conclusions.Items) > 4 || len(r.Conclusions.Warnings) > 16 || len(r.QuestionPreview) > 512 || len(r.Model) > 256 || len(r.ReplyExcerpt) > knowledgeConclusionTextBytes {
			return invalid()
		}
		seen[key] = true
		rawBytes += len(r.Node) + len(r.Session) + len(r.SessionTitle) + len(r.Run) + len(r.QuestionHash) + len(r.QuestionPreview) + len(r.Status) + len(r.Model) + len(r.ReplyExcerpt) + len(r.ReplyHash)
		for _, warning := range r.Conclusions.Warnings {
			rawBytes += len(warning)
		}
		if r.QuestionBound && len(r.QuestionHash) != 64 || !r.QuestionBound && r.QuestionHash != "" || r.Status != "completed" && !r.Conclusions.Partial {
			return invalid()
		}
		if r.Unclassified {
			if len(r.Conclusions.Items) != 0 || len(r.ReplyExcerpt) > knowledgeConclusionTextBytes || !utf8.ValidString(r.ReplyExcerpt) || r.ReplyHash != hash([]byte(r.ReplyExcerpt)) {
				return invalid()
			}
		}
		for _, c := range r.Conclusions.Items {
			rawBytes += len(c.Text) + len(c.Key) + len(c.Heading) + len(c.TextHash) + len(c.SourceHash)
			if len(c.HeadingPath) > 6 {
				return invalid()
			}
			for _, heading := range c.HeadingPath {
				rawBytes += len(heading)
			}
			if rawBytes > 72<<10 || len(c.Text) > knowledgeConclusionTextBytes || !utf8.ValidString(c.Text) || c.TextHash != hash([]byte(c.Text)) || len(c.SourceHash) != 64 || len(c.Key) > 80 || c.HeadingLine < 1 || c.Line < 1 || c.EndLine < c.Line {
				return invalid()
			}
		}
		if rawBytes > 72<<10 {
			return invalid()
		}
	}
	if knowledgeAIHistoryBytes(ai) > knowledgeAIHistoryByteLimit(ai.Version) {
		return invalid()
	}
	if ai.Version == 2 {
		bodyBytes := 0
		for _, r := range ai.Records {
			bodyBytes += len(r.Body.Text)
		}
		if bodyBytes > 256<<10 {
			return invalid()
		}
	}
	return nil
}
