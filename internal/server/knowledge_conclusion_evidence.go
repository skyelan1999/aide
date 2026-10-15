package server

import (
	"encoding/json"
	"net/url"
	"path"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"
)

// These are explicit textual references reconstructed from saved snapshots.
// Neither a resolved target nor a matching hash proves that a model read it.
type knowledgeConclusionReference struct {
	Reference       string `json:"reference"`
	Kind            string `json:"kind"`
	Line            int    `json:"line"`
	Column          int    `json:"column"`
	State           string `json:"state"`
	Node            string `json:"node,omitempty"`
	Path            string `json:"path,omitempty"`
	Root            string `json:"root,omitempty"`
	Source          string `json:"source,omitempty"`
	Region          string `json:"region,omitempty"`
	Excerpt         string `json:"excerpt,omitempty"`
	ExcerptHash     string `json:"excerptHash,omitempty"`
	ExcerptBytes    int    `json:"excerptBytes,omitempty"`
	IndexedCodeHash string `json:"indexedCodeHash,omitempty"`
	Complete        bool   `json:"complete"`
}
type knowledgeConclusionEvidenceSet struct {
	Snapshot   string                         `json:"snapshot"`
	Historical bool                           `json:"historical"`
	Confidence string                         `json:"confidence"`
	ModelUse   string                         `json:"modelUse"`
	References []knowledgeConclusionReference `json:"references"`
	Partial    bool                           `json:"partial"`
}
type knowledgeConclusionEvidenceChange struct {
	Node                string                           `json:"node"`
	Path                string                           `json:"path,omitempty"`
	Session             string                           `json:"session,omitempty"`
	SourceKind          string                           `json:"sourceKind"`
	ConclusionKey       string                           `json:"conclusionKey"`
	Heading             string                           `json:"heading"`
	Kind                string                           `json:"kind"`
	ReferencesChanged   bool                             `json:"referencesChanged"`
	ContentChanged      bool                             `json:"contentChanged"`
	AvailabilityChanged bool                             `json:"availabilityChanged"`
	Unknown             bool                             `json:"unknown"`
	BeforeQuote         *knowledgeConclusion             `json:"beforeQuote,omitempty"`
	AfterQuote          *knowledgeConclusion             `json:"afterQuote,omitempty"`
	BeforeAI            *knowledgeAIConclusionProvenance `json:"beforeAI,omitempty"`
	AfterAI             *knowledgeAIConclusionProvenance `json:"afterAI,omitempty"`
	Before              *knowledgeConclusionEvidenceSet  `json:"before,omitempty"`
	After               *knowledgeConclusionEvidenceSet  `json:"after,omitempty"`
}

var knowledgeConclusionIDRef = regexp.MustCompile(`\[((?:F|C)[0-9a-f]{12}|S(?:[0-9]{1,16}|[0-9a-f]{12}))\]`)
var knowledgeConclusionIndirectRef = regexp.MustCompile(`\[[^\]\n]+\]\[[^\]\n]*\]`)
var knowledgeConclusionInlineRef = regexp.MustCompile(`!?\[[^\]\n]*\]\([ \t]*(<[^<>\r\n]+>|[^\s()<>"']+)(?:[ \t]+(?:"[^"\r\n]*"|'[^'\r\n]*'))?[ \t]*\)`)

// Mask fenced/inline code and comments without moving byte positions. Complex
// HTML/reference-style syntax remains partial rather than being guessed.
func knowledgeConclusionReferenceText(text string) (string, bool) {
	b := []byte(text)
	mask := func(a, bound int) {
		for i := a; i < bound; i++ {
			if b[i] != '\n' {
				b[i] = ' '
			}
		}
	}
	fence, run := byte(0), 0
	comment, partial := false, false
	for start := 0; start < len(text); {
		end := strings.IndexByte(text[start:], '\n')
		if end < 0 {
			end = len(text)
		} else {
			end += start + 1
		}
		line := text[start:end]
		trim := strings.TrimSpace(line)
		indent := len(line) - len(strings.TrimLeft(line, " "))
		if fence != 0 {
			if indent <= 3 && strings.HasPrefix(trim, string(fence)) {
				n := len(trim) - len(strings.TrimLeft(trim, string(fence)))
				if n >= run && strings.TrimSpace(trim[n:]) == "" {
					fence = 0
				}
			}
			mask(start, end)
			start = end
			continue
		}
		if indent <= 3 && len(trim) >= 3 && (trim[0] == '`' || trim[0] == '~') {
			n := len(trim) - len(strings.TrimLeft(trim, string(trim[0])))
			if n >= 3 && (trim[0] != '`' || !strings.Contains(trim[n:], "`")) {
				fence, run = trim[0], n
				mask(start, end)
				start = end
				continue
			}
		}
		if indent > 3 || strings.HasPrefix(line, "\t") {
			mask(start, end)
			start = end
			continue
		}
		for i := start; i < end; {
			if comment {
				j := strings.Index(text[i:end], "-->")
				if j < 0 {
					mask(i, end)
					break
				}
				mask(i, i+j+3)
				i += j + 3
				comment = false
				continue
			}
			if strings.HasPrefix(text[i:], "<!--") {
				comment = true
				continue
			}
			if text[i] == '`' {
				j := i
				for j < end && text[j] == '`' {
					j++
				}
				closeEnd := 0
				for cursor := j; cursor < end; {
					if text[cursor] != '`' {
						cursor++
						continue
					}
					last := cursor
					for last < end && text[last] == '`' {
						last++
					}
					if last-cursor == j-i {
						closeEnd = last
						break
					}
					cursor = last
				}
				if closeEnd > 0 {
					mask(i, closeEnd)
					i = closeEnd
					continue
				}
				partial = true
				mask(i, end)
				break
			}
			i++
		}
		if strings.HasPrefix(trim, "<") && !strings.HasPrefix(trim, "<!--") {
			partial = true
			mask(start, end)
		}
		lower := strings.ToLower(line)
		if strings.Contains(lower, "<a ") || strings.Contains(lower, "<img ") {
			partial = true
		}
		start = end
	}
	return string(b), partial || comment || fence != 0
}

type knowledgeConclusionReferenceIndex struct {
	ids     map[string][]knowledgeNode
	paths   map[string][]knowledgeNode
	areas   map[string][]knowledgeNode
	blocked map[string]bool
	hashes  map[string]string
	budget  int
}

func knowledgeConclusionArea(n knowledgeNode) string {
	return n.Root + "\x00" + n.Source + "\x00" + n.Region
}
func knowledgeConclusionUnavailable(g knowledgeGraph) map[string]bool {
	blocked := map[string]bool{}
	for _, s := range g.Sources {
		if s.State == "unavailable" || s.State == "disabled" {
			region := s.Region
			if region == "" {
				region = s.ID
			}
			blocked[region] = true
		}
	}
	return blocked
}
func newKnowledgeConclusionReferenceIndex(g knowledgeGraph) *knowledgeConclusionReferenceIndex {
	x := &knowledgeConclusionReferenceIndex{ids: map[string][]knowledgeNode{}, paths: map[string][]knowledgeNode{}, areas: map[string][]knowledgeNode{}, blocked: knowledgeConclusionUnavailable(g), hashes: map[string]string{}, budget: 1 << 20}
	for _, n := range g.Nodes {
		x.ids[n.ID] = append(x.ids[n.ID], n)
		if n.Kind == "file" {
			x.paths[n.Path] = append(x.paths[n.Path], n)
			key := knowledgeConclusionArea(n) + "\x00" + n.Path
			x.areas[key] = append(x.areas[key], n)
		}
	}
	return x
}
func (x *knowledgeConclusionReferenceIndex) resolve(from knowledgeNode, ref knowledgeConclusionReference) knowledgeConclusionReference {
	var candidates []knowledgeNode
	if ref.Kind == "node_id" {
		candidates = x.ids[ref.Reference]
	} else {
		raw := ref.Reference
		if strings.HasPrefix(raw, "/context/") {
			raw = strings.TrimPrefix(raw, "/context/")
			rel := knowledgeLocalReference(raw)
			for _, n := range x.paths[path.Clean(rel)] {
				if n.Root == "context" && rel != "" {
					candidates = append(candidates, n)
				}
			}
		} else if before, after, ok := strings.Cut(raw, ":"); ok && !strings.Contains(before, "/") && !strings.Contains(after, "//") {
			for _, n := range x.paths[path.Clean(knowledgeLocalReference(after))] {
				if n.Root == "source" && n.Source == before {
					candidates = append(candidates, n)
				}
			}
			if len(candidates) == 0 {
				ref.State = "namespace_not_in_snapshot"
				return ref
			}
		} else {
			u, err := url.Parse(raw)
			if err != nil {
				ref.State = "invalid_reference"
				return ref
			}
			if u.Scheme != "" || u.Host != "" {
				ref.State = "external_unfetched"
				return ref
			}
			if strings.HasPrefix(raw, "#") {
				ref.State = "fragment_not_resolved"
				return ref
			}
			rel := knowledgeLocalReference(raw)
			if rel == "" {
				ref.State = "unsupported_reference"
				return ref
			}
			if from.Kind == "file" {
				rel = path.Clean(path.Join(path.Dir(from.Path), rel))
				candidates = x.areas[knowledgeConclusionArea(from)+"\x00"+rel]
			} else {
				rel = path.Clean(rel)
				candidates = x.paths[rel]
			}
			if safePath(rel) != nil {
				ref.State = "outside_source"
				return ref
			}
		}
	}
	if len(candidates) == 0 {
		ref.State = "not_in_saved_index"
		return ref
	}
	if len(candidates) != 1 {
		ref.State = "ambiguous_target"
		return ref
	}
	n := candidates[0]
	if n.Kind == "file" && safePath(n.Path) != nil {
		ref.State = "outside_source"
		return ref
	}
	ref.Node, ref.Path, ref.Root, ref.Source, ref.Region = n.ID, n.Path, n.Root, n.Source, n.Region
	region := n.Region
	if region == "" {
		region = n.Root
		if n.Source != "" {
			region = "source:" + n.Source
		}
	}
	if x.blocked[region] {
		ref.State = "source_unavailable"
		return ref
	}
	if n.ReadState != "" && n.ReadState != "ready" {
		ref.State = "target_not_ready"
		return ref
	}
	if n.Kind != "file" && n.Kind != "symbol" {
		ref.State = "target_not_document"
		return ref
	}
	if n.Text == "" && !(n.Size == 0 && n.ReadState == "ready") {
		ref.State = "body_not_captured"
		return ref
	}
	if !utf8.ValidString(n.Text) {
		ref.State = "invalid_saved_text"
		return ref
	}
	key := n.ID + "\x00" + knowledgeConclusionArea(n) + "\x00" + n.Path
	digest, exists := x.hashes[key]
	if !exists {
		if len(n.Text) > x.budget {
			ref.State = "comparison_budget"
			return ref
		}
		x.budget -= len(n.Text)
		digest = hash([]byte(n.Text))
		x.hashes[key] = digest
	}
	ref.State = "saved_excerpt"
	ref.ExcerptHash = digest
	ref.ExcerptBytes = len(n.Text)
	ref.Excerpt = conclusionBytePrefix(n.Text, 256)
	if len(n.ContentHash) == 64 {
		ref.IndexedCodeHash = n.ContentHash
	}
	ref.Complete = n.Kind == "file" && n.Size == int64(len(n.Text)) && (n.Size > 0 || n.ReadState == "ready")
	return ref
}

func knowledgeConclusionEvidence(text string, from knowledgeNode, snapshot string, x *knowledgeConclusionReferenceIndex, partial bool) knowledgeConclusionEvidenceSet {
	out := knowledgeConclusionEvidenceSet{Snapshot: snapshot, Historical: true, Confidence: "explicit_text_reference", ModelUse: "not_proven", References: []knowledgeConclusionReference{}, Partial: partial}
	if len(text) > knowledgeConclusionTextBytes {
		text = conclusionBytePrefix(text, knowledgeConclusionTextBytes)
		out.Partial = true
	}
	masked, unsupported := knowledgeConclusionReferenceText(text)
	out.Partial = out.Partial || unsupported || knowledgeConclusionIndirectRef.MatchString(masked)
	type found struct {
		start, end int
		kind       string
	}
	refs := []found{}
	for _, m := range knowledgeConclusionIDRef.FindAllStringSubmatchIndex(masked, 65) {
		refs = append(refs, found{m[2], m[3], "node_id"})
	}
	// A destination prefix alone is not a complete Markdown link. Incomplete
	// or nested syntax must not accidentally bind to a similarly named file.
	if len(markdownInlineRef.FindAllStringIndex(masked, 65)) != len(knowledgeConclusionInlineRef.FindAllStringIndex(masked, 65)) {
		out.Partial = true
	}
	for _, re := range []*regexp.Regexp{knowledgeConclusionInlineRef, markdownDefinitionRef} {
		for _, m := range re.FindAllStringSubmatchIndex(masked, 65) {
			a, b := m[2], m[3]
			if text[a] == '<' {
				a++
				if b > a && text[b-1] == '>' {
					b--
				}
			}
			refs = append(refs, found{a, b, "markdown_target"})
		}
	}
	sort.SliceStable(refs, func(i, j int) bool { return refs[i].start < refs[j].start })
	seen := map[string]bool{}
	for _, r := range refs {
		// A backslash-escaped bracket is an example, not a citation.
		bracket := r.start - 1
		if r.kind != "node_id" {
			bracket = strings.LastIndexByte(masked[:r.start], '[')
		}
		escapes := 0
		for j := bracket - 1; j >= 0 && masked[j] == '\\'; j-- {
			escapes++
		}
		if escapes%2 != 0 {
			continue
		}
		raw := text[r.start:r.end]
		if len(raw) > 1024 {
			out.Partial = true
			continue
		}
		key := r.kind + "\x00" + raw
		if seen[key] {
			continue
		}
		seen[key] = true
		if len(out.References) >= 16 {
			out.Partial = true
			break
		}
		prefix := text[:r.start]
		line := strings.Count(prefix, "\n") + 1
		column := utf8.RuneCountInString(prefix[strings.LastIndex(prefix, "\n")+1:]) + 1
		ref := x.resolve(from, knowledgeConclusionReference{Reference: raw, Kind: r.kind, Line: line, Column: column})
		if ref.State != "saved_excerpt" || !ref.Complete {
			out.Partial = true
		}
		out.References = append(out.References, ref)
	}
	return out
}

func knowledgeConclusionReferenceIdentity(r knowledgeConclusionReference) string {
	return r.Node + "\x00" + r.Root + "\x00" + r.Source + "\x00" + r.Region + "\x00" + r.Path
}
func knowledgeConclusionReferenceSignature(s *knowledgeConclusionEvidenceSet) string {
	if s == nil {
		return ""
	}
	refs := []string{}
	for _, r := range s.References {
		refs = append(refs, r.Kind+"\x00"+r.Reference)
	}
	sort.Strings(refs)
	b, _ := json.Marshal(refs)
	return hash(b)
}
