package server

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"
)

const knowledgeConclusionScanBytes = 64 << 10
const knowledgeConclusionTextBytes = 4 << 10
const knowledgeConclusionItems = 16

// Hashes describe saved excerpts, never an unobserved complete source.
type knowledgeConclusion struct {
	Key         string   `json:"key"`
	Heading     string   `json:"heading"`
	HeadingPath []string `json:"headingPath"`
	HeadingLine int      `json:"headingLine"`
	Line        int      `json:"line"`
	EndLine     int      `json:"endLine"`
	Text        string   `json:"text"`
	TextHash    string   `json:"textHash"`
	SourceHash  string   `json:"sourceExcerptHash"`
	Partial     bool     `json:"partial,omitempty"`
	Ambiguous   bool     `json:"ambiguous,omitempty"`
}
type knowledgeConclusionSet struct {
	Version  int                   `json:"version"`
	Items    []knowledgeConclusion `json:"items"`
	Partial  bool                  `json:"partial"`
	Warnings []string              `json:"warnings"`
}
type knowledgeConclusionChange struct {
	Kind   string               `json:"kind"`
	Reason string               `json:"reason,omitempty"`
	Before *knowledgeConclusion `json:"before,omitempty"`
	After  *knowledgeConclusion `json:"after,omitempty"`
}

func conclusionBytePrefix(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	for limit > 0 && !utf8.RuneStart(text[limit]) {
		limit--
	}
	return text[:limit]
}

func conclusionTitle(title string) string {
	title = strings.TrimSpace(title)
	without := strings.TrimRight(title, "#")
	if len(without) < len(title) && (strings.HasSuffix(without, " ") || strings.HasSuffix(without, "\t")) {
		title = strings.TrimSpace(without)
	}
	return title
}

func explicitConclusionTitle(title string) bool {
	switch strings.ToLower(title) {
	case "结论", "主要结论", "最终结论", "结论与依据", "结论与证据", "结论与建议", "总结", "conclusion", "conclusions", "summary":
		return true
	}
	return false
}

func conclusionATX(line string) (int, string, bool) {
	indent := len(line) - len(strings.TrimLeft(line, " "))
	if indent > 3 || strings.HasPrefix(line, "\t") {
		return 0, "", false
	}
	line = line[indent:]
	count := len(line) - len(strings.TrimLeft(line, "#"))
	if count < 1 || count > 6 || len(line) > count && line[count] != ' ' && line[count] != '\t' {
		return 0, "", false
	}
	return count, conclusionTitle(line[count:]), true
}

func conclusionSetext(line string) int {
	if len(line)-len(strings.TrimLeft(line, " ")) > 3 || strings.HasPrefix(line, "\t") {
		return 0
	}
	line = strings.TrimSpace(line)
	if line != "" && strings.Trim(line, "=") == "" {
		return 1
	}
	if line != "" && strings.Trim(line, "-") == "" {
		return 2
	}
	return 0
}

// This is bounded explicit-section extraction, not semantic inference. HTML
// blocks are conservatively omitted and make capture partial.
func extractKnowledgeConclusions(text string, sourceComplete bool) knowledgeConclusionSet {
	out := knowledgeConclusionSet{Version: 1, Items: []knowledgeConclusion{}, Warnings: []string{}, Partial: !sourceComplete}
	if len(text) > knowledgeConclusionScanBytes {
		text = conclusionBytePrefix(text, knowledgeConclusionScanBytes)
		out.Partial = true
		out.Warnings = append(out.Warnings, "source_scan_limit")
	}
	if !utf8.ValidString(text) {
		out.Partial = true
		out.Warnings = append(out.Warnings, "invalid_utf8")
		return out
	}
	sourceHash := hash([]byte(text))
	lines := strings.Split(text, "\n")
	for i := range lines {
		lines[i] = strings.TrimSuffix(lines[i], "\r")
	}
	type heading struct {
		title             string
		level, line, body int
		path              []string
	}
	headings := []heading{}
	stack := []heading{}
	paragraph := []string{}
	paragraphLine := 0
	fence := byte(0)
	fenceLength := 0
	comment := false
	htmlBlock := false
	rawHTML := ""
	for i, line := range lines {
		if i == 0 {
			line = strings.TrimPrefix(line, "\uFEFF")
		}
		trim := strings.TrimSpace(line)
		indent := len(line) - len(strings.TrimLeft(line, " "))
		if fence != 0 {
			if indent <= 3 && strings.HasPrefix(trim, string(fence)) {
				run := len(trim) - len(strings.TrimLeft(trim, string(fence)))
				if run >= fenceLength && strings.TrimSpace(trim[run:]) == "" {
					fence = 0
				}
			}
			continue
		}
		if comment {
			if strings.Contains(line, "-->") {
				comment = false
			}
			paragraph = nil
			continue
		}
		if strings.Contains(line, "<!--") {
			_, _, headingComment := conclusionATX(line)
			if headingComment || strings.Contains(line, "`") {
				out.Partial = true
				found := false
				for _, warning := range out.Warnings {
					if warning == "commented_heading_or_inline_code_unresolved" {
						found = true
					}
				}
				if !found {
					out.Warnings = append(out.Warnings, "commented_heading_or_inline_code_unresolved")
				}
			}
			comment = !strings.Contains(strings.SplitN(line, "<!--", 2)[1], "-->")
			paragraph = nil
			continue
		}
		if rawHTML != "" {
			if strings.Contains(strings.ToLower(line), "</"+rawHTML+">") {
				rawHTML = ""
			}
			paragraph = nil
			continue
		}
		if htmlBlock {
			if trim == "" {
				htmlBlock = false
			}
			paragraph = nil
			continue
		}
		if indent <= 3 && len(trim) >= 3 && (trim[0] == '`' || trim[0] == '~') {
			run := len(trim) - len(strings.TrimLeft(trim, string(trim[0])))
			if run >= 3 && (trim[0] != '`' || !strings.Contains(trim[run:], "`")) {
				fence, fenceLength = trim[0], run
				paragraph = nil
				continue
			}
		}
		if strings.HasPrefix(trim, "<") {
			out.Partial = true
			found := false
			for _, warning := range out.Warnings {
				if warning == "html_block_omitted" {
					found = true
				}
			}
			if !found {
				out.Warnings = append(out.Warnings, "html_block_omitted")
			}
			htmlBlock = true
			lower := strings.ToLower(trim)
			for _, name := range []string{"script", "style", "pre", "textarea"} {
				if (strings.HasPrefix(lower, "<"+name+">") || strings.HasPrefix(lower, "<"+name+" ")) && !strings.Contains(lower, "</"+name+">") {
					rawHTML = name
					htmlBlock = false
				}
			}
			paragraph = nil
			continue
		}
		if indent > 3 || strings.HasPrefix(line, "\t") || strings.HasPrefix(trim, ">") || trim == "" {
			paragraph = nil
			continue
		}
		level, title, ok := conclusionATX(line)
		headingLine, body := i+1, i+1
		if !ok && len(paragraph) > 0 {
			if level = conclusionSetext(line); level > 0 {
				title = strings.Join(paragraph, " ")
				headingLine = paragraphLine
				ok = true
			}
		}
		if ok {
			paragraph = nil
			if len(headings) >= 256 {
				out.Partial = true
				out.Warnings = append(out.Warnings, "heading_limit")
				lines = lines[:i]
				break
			}
			for len(stack) > 0 && stack[len(stack)-1].level >= level {
				stack = stack[:len(stack)-1]
			}
			path := []string{}
			for _, h := range stack {
				path = append(path, h.title)
			}
			path = append(path, title)
			h := heading{title, level, headingLine, body, path}
			headings = append(headings, h)
			stack = append(stack, h)
		} else {
			if len(paragraph) == 0 {
				paragraphLine = i + 1
			}
			paragraph = append(paragraph, trim)
		}
	}
	if comment {
		out.Partial = true
		out.Warnings = append(out.Warnings, "unterminated_comment")
	}
	counts := map[string]int{}
	for i, h := range headings {
		if !explicitConclusionTitle(h.title) {
			continue
		}
		if len(out.Items) >= knowledgeConclusionItems {
			out.Partial = true
			out.Warnings = append(out.Warnings, "conclusion_item_limit")
			break
		}
		end := len(lines)
		for _, next := range headings[i+1:] {
			if next.level <= h.level {
				end = next.line - 1
				break
			}
		}
		start := h.body
		for start < end && strings.TrimSpace(lines[start]) == "" {
			start++
		}
		for end > start && strings.TrimSpace(lines[end-1]) == "" {
			end--
		}
		body := strings.Join(lines[start:end], "\n")
		clipped := conclusionBytePrefix(body, knowledgeConclusionTextBytes)
		encoded, _ := json.Marshal(h.path)
		base := hash(encoded)
		counts[base]++
		line, last := start+1, end
		if body == "" {
			line, last = h.line, h.line
		}
		out.Items = append(out.Items, knowledgeConclusion{Key: base + fmt.Sprintf(":%d", counts[base]), Heading: h.title, HeadingPath: h.path, HeadingLine: h.line, Line: line, EndLine: last, Text: clipped, TextHash: hash([]byte(clipped)), SourceHash: sourceHash, Partial: out.Partial || len(clipped) < len(body)})
	}
	for i := range out.Items {
		item := &out.Items[i]
		base := strings.Split(item.Key, ":")[0]
		item.Ambiguous = counts[base] > 1
		item.Partial = item.Partial || out.Partial
	}
	return out
}

func compareKnowledgeConclusionSets(before, after *knowledgeConclusionSet) []knowledgeConclusionChange {
	changes := []knowledgeConclusionChange{}
	old, current := map[string]knowledgeConclusion{}, map[string]knowledgeConclusion{}
	if before != nil && before.Version == 1 {
		for _, c := range before.Items {
			old[c.Key] = c
		}
	}
	if after != nil && after.Version == 1 {
		for _, c := range after.Items {
			current[c.Key] = c
		}
	}
	if after != nil {
		for _, c := range after.Items {
			previous, exists := old[c.Key]
			copy := c
			if c.Ambiguous || exists && previous.Ambiguous {
				changes = append(changes, knowledgeConclusionChange{Kind: "unknown", Reason: "duplicate_heading_identity", After: &copy})
				continue
			}
			if !exists {
				kind, reason := "added", "explicit_section_added"
				if before == nil || before.Version != 1 || before.Partial || after.Partial || c.Partial {
					kind, reason = "observed", "previous_capture_unknown"
				}
				changes = append(changes, knowledgeConclusionChange{Kind: kind, Reason: reason, After: &copy})
				continue
			}
			if previous.TextHash != c.TextHash {
				prev := previous
				kind, reason := "changed", "captured_text_changed"
				if previous.Partial || c.Partial {
					kind, reason = "unknown", "partial_capture_not_aligned"
				}
				changes = append(changes, knowledgeConclusionChange{Kind: kind, Reason: reason, Before: &prev, After: &copy})
			}
		}
	}
	if before != nil {
		for _, c := range before.Items {
			if _, exists := current[c.Key]; !exists {
				copy := c
				kind, reason := "removed", "explicit_section_missing"
				if c.Ambiguous || after == nil || after.Version != 1 || after.Partial {
					kind, reason = "unknown", "current_capture_incomplete"
				}
				changes = append(changes, knowledgeConclusionChange{Kind: kind, Reason: reason, Before: &copy})
			}
		}
	}
	return changes
}
