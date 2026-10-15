package server

import (
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/yuin/goldmark/v2/ast"
	"github.com/yuin/goldmark/v2/parser"
	"github.com/yuin/goldmark/v2/text"
)

type knowledgeEvidenceSource struct {
	raw      string
	complete bool
}
type knowledgeMarkdownOccurrence struct {
	start, end int
	ref        knowledgeConclusionReference
}
type knowledgeMarkdownSource struct {
	text        string
	hash        string
	occurrences []knowledgeMarkdownOccurrence
	unknown     [][2]int
	partial     bool
}

func knowledgeMarkdownNormalized(s string) string { return strings.ReplaceAll(s, "\r\n", "\n") }

// Parser offsets use normalized newlines; displayed usage/definition excerpts
// and their hashes retain the exact historical bytes, including internal CRLF.
func knowledgeMarkdownRawOffsets(raw string) []int {
	out := make([]int, 0, len(raw)+1)
	for i := 0; i < len(raw); i++ {
		out = append(out, i)
		if raw[i] == '\r' && i+1 < len(raw) && raw[i+1] == '\n' {
			i++
		}
	}
	return append(out, len(raw))
}

// The source is the same saved document or attributed assistant record. A
// session's mixed graph excerpt is never a Markdown document for an AI reply.
func knowledgeEvidenceSourceFor(s knowledgeTimeSnapshot, item knowledgeConclusionEvidenceItem) *knowledgeEvidenceSource {
	if item.ai != nil {
		if s.AI == nil || validateKnowledgeAIHistory(s.AI, s.Graph) != nil {
			return nil
		}
		var found *knowledgeEvidenceSource
		for _, r := range s.AI.Records {
			if r.Node != item.node.ID || r.Session != item.session || r.Run != item.ai.Run || r.QuestionHash != item.ai.QuestionHash || r.Body == nil {
				continue
			}
			if item.conclusion.SourceHash != "" && item.conclusion.SourceHash != r.Body.Hash {
				return nil
			}
			if found != nil {
				return nil
			}
			found = &knowledgeEvidenceSource{r.Body.Text, r.Body.Complete}
		}
		return found
	}
	var found *knowledgeEvidenceSource
	for _, n := range s.Graph.Nodes {
		if n.ID != item.node.ID || n.Kind != "file" || knowledgeConclusionArea(n) != knowledgeConclusionArea(item.node) || n.Path != item.node.Path {
			continue
		}
		if found != nil || n.ReadState != "" && n.ReadState != "ready" || !utf8.ValidString(n.Text) {
			return nil
		}
		body := conclusionBytePrefix(n.Text, knowledgeConclusionScanBytes)
		if item.conclusion.SourceHash != hash([]byte(body)) && item.conclusion.SourceHash != hash([]byte(n.Text)) {
			return nil
		}
		complete := len(body) == len(n.Text) && int64(len(n.Text)) == n.Size && (n.Size > 0 || n.ReadState == "ready")
		found = &knowledgeEvidenceSource{body, complete}
	}
	return found
}

// Locate a saved quote inside its own source. Line anchors constrain repeated
// prose; byte-offset chunk keys disambiguate repeated text on a single line.
func knowledgeMarkdownQuoteSpan(source knowledgeEvidenceSource, q knowledgeConclusion) (int, int, bool) {
	normalized, quote := knowledgeMarkdownNormalized(source.raw), knowledgeMarkdownNormalized(q.Text)
	for _, prefix := range []string{"saved_assistant_body:", "saved_document_body:"} {
		if suffix, ok := strings.CutPrefix(q.Key, prefix); ok {
			offset, err := strconv.Atoi(suffix)
			if err != nil || offset < 0 || offset > len(source.raw) {
				return 0, 0, false
			}
			start := len(knowledgeMarkdownNormalized(source.raw[:offset]))
			if strings.HasPrefix(normalized[start:], quote) {
				return start, start + len(quote), true
			}
			return 0, 0, false
		}
	}
	if q.Key == "unclassified_reply" || q.Key == "saved_document_excerpt" {
		if strings.HasPrefix(normalized, quote) {
			return 0, len(quote), true
		}
		return 0, 0, false
	}
	line, start, end := 1, 0, len(normalized)
	for i, c := range normalized {
		if c != '\n' {
			continue
		}
		line++
		if line == q.Line {
			start = i + 1
		}
		if q.EndLine > 0 && line == q.EndLine+1 {
			end = i
			break
		}
	}
	if q.Line < 1 || q.Line > line || start > end || quote == "" {
		return 0, 0, false
	}
	pos := strings.Index(normalized[start:end], quote)
	if pos < 0 {
		return 0, 0, false
	}
	a := start + pos
	if strings.Contains(normalized[a+1:end], quote) {
		return 0, 0, false
	}
	return a, a + len(quote), true
}

func knowledgeMarkdownItemSpan(source knowledgeEvidenceSource, s knowledgeTimeSnapshot, item knowledgeConclusionEvidenceItem) (int, int, bool) {
	if item.selection == nil {
		return knowledgeMarkdownQuoteSpan(source, item.conclusion)
	}
	m := item.selection
	candidates, _ := knowledgeConclusionCandidates(s, m.Pick.Node, m.Candidate.Code)
	for _, c := range candidates {
		if c.Token != m.Pick.Candidate {
			continue
		}
		a, _, ok := knowledgeMarkdownQuoteSpan(source, c.Quote)
		if !ok {
			return 0, 0, false
		}
		prefix := ""
		if m.Pick.Start > 0 {
			prefix, ok = knowledgeConclusionUTF16(c.Quote.Text, 0, m.Pick.Start)
			if !ok {
				return 0, 0, false
			}
		}
		start := a + len(knowledgeMarkdownNormalized(prefix))
		return start, start + len(knowledgeMarkdownNormalized(item.conclusion.Text)), true
	}
	return 0, 0, false
}

func knowledgeMarkdownValueEnd(v text.Value) int {
	end := -1
	if v.IsOwned() {
		return end
	}
	for _, index := range v.Indices() {
		end = max(end, index.Stop)
	}
	return end
}

func knowledgeMarkdownBracketEnd(s string, start int) int {
	if start < 0 || start >= len(s) || s[start] != '[' {
		return -1
	}
	depth := 1
	for i := start + 1; i < len(s); i++ {
		if s[i] == '\\' {
			i++
			continue
		}
		if s[i] == '`' {
			end := i + 1
			for end < len(s) && s[end] == '`' {
				end++
			}
			run := s[i:end]
			if close := strings.Index(s[end:], run); close >= 0 {
				i = end + close + len(run) - 1
				continue
			}
		}
		if s[i] == '[' {
			depth++
		}
		if s[i] == ']' {
			depth--
			if depth == 0 {
				return i + 1
			}
		}
	}
	return -1
}

func knowledgeMarkdownLinkEnd(s string, start int, destination text.SingleLineValue, title text.MultiLineValue, reference *ast.ReferenceLink) int {
	label := start
	if label >= 0 && label < len(s) && s[label] == '!' {
		label++
	}
	end := knowledgeMarkdownBracketEnd(s, label)
	if end < 0 {
		return -1
	}
	if reference != nil {
		if reference.ReferenceLinkKind == ast.ReferenceLinkKindShortcut {
			return end
		}
		return knowledgeMarkdownBracketEnd(s, end)
	}
	if end >= len(s) || s[end] != '(' {
		return -1
	}
	end = max(end+1, knowledgeMarkdownValueEnd(&destination))
	if !title.IsEmpty() {
		end = max(end, knowledgeMarkdownValueEnd(&title))
		if end < len(s) && (s[end] == '\'' || s[end] == '"' || s[end] == ')') {
			end++
		}
	} else if end < len(s) && s[end] == '>' {
		end++
	}
	for end < len(s) && (s[end] == ' ' || s[end] == '\t' || s[end] == '\n') {
		end++
	}
	if end < len(s) && s[end] == ')' {
		return end + 1
	}
	return -1
}

func parseKnowledgeMarkdownSource(raw string) *knowledgeMarkdownSource {
	s := &knowledgeMarkdownSource{text: knowledgeMarkdownNormalized(raw), hash: hash([]byte(raw))}
	src := []byte(s.text)
	rawOffsets := knowledgeMarkdownRawOffsets(raw)
	plain := []byte(strings.Repeat(" ", len(src)))
	for i, c := range src {
		if c == '\n' {
			plain[i] = c
		}
	}
	doc := parser.New().Parse(src)
	definitions := map[int]knowledgeConclusionReference{}
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		d, ok := n.(*ast.LinkReferenceDefinition)
		if !ok {
			return ast.WalkContinue, nil
		}
		if len(definitions) >= 256 {
			s.partial = true
			return ast.WalkContinue, nil
		}
		start, end := d.Pos(), knowledgeMarkdownValueEnd(&d.Destination)
		end = max(end, knowledgeMarkdownValueEnd(&d.Title))
		if start < 0 || end < start || end > len(src) || d.Destination.IsOwned() {
			s.partial = true
			return ast.WalkContinue, nil
		}
		if next := strings.IndexByte(s.text[end:], '\n'); next >= 0 {
			end += next
		} else {
			end = len(src)
		}
		definition := raw[rawOffsets[start]:rawOffsets[end]]
		definitions[d.Destination.Index().Start] = knowledgeConclusionReference{DefinitionLine: strings.Count(s.text[:start], "\n") + 1, Definition: conclusionBytePrefix(definition, 2048), DefinitionHash: hash([]byte(definition))}
		return ast.WalkContinue, nil
	})
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		if len(s.occurrences) >= 1024 {
			s.partial = true
			return ast.WalkStop, nil
		}
		var dest text.SingleLineValue
		var title text.MultiLineValue
		var reference *ast.ReferenceLink
		switch v := n.(type) {
		case *ast.Link:
			dest, title, reference = v.Destination, v.Title, v.Reference
		case *ast.Image:
			dest, title, reference = v.Destination, v.Title, v.Reference
		case *ast.HTMLBlock:
			segments := v.Source()
			if len(segments) > 0 {
				s.unknown = append(s.unknown, [2]int{segments[0].Start, segments[len(segments)-1].Stop})
			}
			return ast.WalkSkipChildren, nil
		case *ast.RawHTML:
			if end := knowledgeMarkdownValueEnd(&v.Value); v.Pos() >= 0 && end >= v.Pos() {
				s.unknown = append(s.unknown, [2]int{v.Pos(), end})
			}
			return ast.WalkSkipChildren, nil
		case *ast.CodeSpan, *ast.CodeBlock, *ast.LinkReferenceDefinition:
			return ast.WalkSkipChildren, nil
		case *ast.Text:
			if v.Value.IsOwned() {
				return ast.WalkContinue, nil
			}
			index := v.Value.Index()
			if index.Start < 0 || index.Stop > len(src) {
				s.partial = true
				return ast.WalkContinue, nil
			}
			// Unresolved brackets can span adjacent AST text nodes. Preserve
			// only source-backed text ranges and scan their contiguous union.
			copy(plain[index.Start:index.Stop], src[index.Start:index.Stop])
			return ast.WalkContinue, nil
		default:
			return ast.WalkContinue, nil
		}
		start, end := n.Pos(), knowledgeMarkdownLinkEnd(s.text, n.Pos(), dest, title, reference)
		if start < 0 || end <= start || end > len(src) {
			s.partial = true
			return ast.WalkSkipChildren, nil
		}
		r := knowledgeConclusionReference{Reference: dest.Value(src), Kind: "markdown_target"}
		if reference != nil {
			r = definitions[dest.Index().Start]
			r.Reference = raw[rawOffsets[start]:rawOffsets[end]]
			r.Target = dest.Value(src)
			r.Kind = "markdown_reference"
			if r.DefinitionHash == "" {
				r.State = "reference_definition_unavailable"
				s.partial = true
			}
		}
		if len(r.Reference) > 1024 || len(r.Target) > 1024 {
			s.partial = true
			return ast.WalkSkipChildren, nil
		}
		s.occurrences = append(s.occurrences, knowledgeMarkdownOccurrence{start, end, r})
		return ast.WalkSkipChildren, nil
	})
	for _, m := range knowledgeConclusionIDRef.FindAllStringSubmatchIndex(string(plain), 1025) {
		if len(s.occurrences) >= 1024 {
			s.partial = true
			break
		}
		bracket, escapes := m[0], 0
		for i := bracket - 1; i >= 0 && s.text[i] == '\\'; i-- {
			escapes++
		}
		if escapes%2 == 0 {
			s.occurrences = append(s.occurrences, knowledgeMarkdownOccurrence{m[2], m[3], knowledgeConclusionReference{Reference: s.text[m[2]:m[3]], Kind: "node_id"}})
		}
	}
	for _, span := range knowledgeConclusionIndirectRef.FindAllStringIndex(string(plain), 1025) {
		s.unknown = append(s.unknown, [2]int{span[0], span[1]})
	}
	sort.SliceStable(s.occurrences, func(i, j int) bool { return s.occurrences[i].start < s.occurrences[j].start })
	return s
}

func knowledgeItemEvidence(item knowledgeConclusionEvidenceItem, snapshot knowledgeTimeSnapshot, x *knowledgeConclusionReferenceIndex) knowledgeConclusionEvidenceSet {
	source := knowledgeEvidenceSourceFor(snapshot, item)
	context, baseLine := "saved_source", 0
	start, end, ok := 0, 0, false
	if source == nil {
		// A legacy manual selection must retain the syntax surrounding its
		// original candidate. Parsing the selected substring alone could turn
		// an example inside a code fence into a false citation.
		context = "selected_excerpt_only"
		q := item.conclusion
		if m := item.selection; m != nil {
			candidates, _ := knowledgeConclusionCandidates(snapshot, m.Pick.Node, m.Candidate.Code)
			for _, c := range candidates {
				if c.Token != m.Pick.Candidate {
					continue
				}
				q = c.Quote
				prefix := ""
				if m.Pick.Start > 0 {
					prefix, ok = knowledgeConclusionUTF16(q.Text, 0, m.Pick.Start)
					if !ok {
						break
					}
				}
				start = len(knowledgeMarkdownNormalized(prefix))
				end = start + len(knowledgeMarkdownNormalized(item.conclusion.Text))
				normalized := knowledgeMarkdownNormalized(q.Text)
				ok = end <= len(normalized) && normalized[start:end] == knowledgeMarkdownNormalized(item.conclusion.Text)
				break
			}
		} else {
			end = len(knowledgeMarkdownNormalized(q.Text))
			ok = true
		}
		source = &knowledgeEvidenceSource{q.Text, false}
		baseLine = max(1, q.Line) - 1
	} else {
		start, end, ok = knowledgeMarkdownItemSpan(*source, snapshot, item)
	}
	out := knowledgeConclusionEvidenceSet{Snapshot: snapshot.ID, Historical: true, Confidence: "explicit_text_reference", ModelUse: "not_proven", References: []knowledgeConclusionReference{}, Partial: item.conclusion.Partial || !source.complete, Context: context}
	if !ok {
		out.Partial = true
		out.Context = "source_anchor_unknown"
		return out
	}
	key := hash([]byte(source.raw))
	out.SourceExcerptHash = key
	parsed := x.markdown[key]
	if parsed == nil {
		if len(source.raw) > x.parseBudget {
			out.Partial = true
			out.Context = "source_parse_budget"
			return out
		}
		x.parseBudget -= len(source.raw)
		parsed = parseKnowledgeMarkdownSource(source.raw)
		x.markdown[key] = parsed
	}
	out.Partial = out.Partial || parsed.partial
	for _, span := range parsed.unknown {
		if span[0] < end && span[1] > start {
			out.Partial = true
			break
		}
	}
	seen := map[string]bool{}
	for _, occurrence := range parsed.occurrences {
		if occurrence.end <= start || occurrence.start >= end {
			continue
		}
		if occurrence.start < start || occurrence.end > end {
			out.Partial = true
			continue
		}
		r := occurrence.ref
		identity := r.Kind + "\x00" + r.Reference + "\x00" + r.Target + "\x00" + r.DefinitionHash
		if seen[identity] {
			continue
		}
		seen[identity] = true
		if len(out.References) >= 16 {
			out.Partial = true
			break
		}
		prefix := parsed.text[:occurrence.start]
		r.SourceLine = baseLine + strings.Count(prefix, "\n") + 1
		r.SourceColumn = utf8.RuneCountInString(prefix[strings.LastIndexByte(prefix, '\n')+1:]) + 1
		r.Line = r.SourceLine - max(1, item.conclusion.Line) + 1
		r.Column = r.SourceColumn
		if r.State == "" {
			r = x.resolve(item.node, r)
		}
		if r.State != "saved_excerpt" || !r.Complete {
			out.Partial = true
		}
		out.References = append(out.References, r)
	}
	return out
}
