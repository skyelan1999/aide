package server

import (
	"net/url"
	"path"
	"regexp"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Capture the actual target span, rather than searching for its text again.
// Code/name mentions remain explicit text evidence, not runtime references.
var knowledgeReferenceTargets = regexp.MustCompile("`([^`\\r\\n]+)`|\"([^\"\\r\\n]+)\"|'([^'\\r\\n]+)'|\\((?:<([^<>\\r\\n]+)>|([^\\s()<>\"']+))(?:[ \\t]+\"[^\"\\r\\n]*\")?[ \\t]*\\)")

type knowledgeTextReference struct {
	target string
	offset int
}

func knowledgeTextReferences(text string) []knowledgeTextReference {
	out := []knowledgeTextReference{}
	for _, match := range knowledgeReferenceTargets.FindAllStringSubmatchIndex(text, 200) {
		for i := 2; i+1 < len(match); i += 2 {
			if match[i] >= 0 {
				raw := text[match[i]:match[i+1]]
				if len(raw) <= 1024 {
					out = append(out, knowledgeTextReference{raw, match[i]})
				}
				break
			}
		}
	}
	return out
}

func knowledgeLocalReference(raw string) string {
	value := strings.TrimSpace(raw)
	u, err := url.Parse(value)
	if err != nil || u.IsAbs() || u.Host != "" || strings.HasPrefix(value, "/") {
		return ""
	}
	value, _, _ = strings.Cut(value, "#")
	value, _, _ = strings.Cut(value, "?")
	value, err = url.PathUnescape(value)
	if err != nil || value == "" || strings.ContainsRune(value, 0) || !utf8.ValidString(value) || strings.HasPrefix(value, "/") {
		return ""
	}
	return value
}

func knowledgeReferenceBoundary(r rune) bool {
	return unicode.IsSpace(r) || strings.ContainsRune("`'\"()[]<>{},;，；。！？“”‘’", r)
}

// Plain namespaced references must be whole targets, not filename prefixes or
// fragments inside an external URL. Local relative paths require delimiters.
func knowledgeNamespacedReference(text, target string) (knowledgeTextReference, bool) {
	for start := 0; start < len(text); {
		index := strings.Index(text[start:], target)
		if index < 0 {
			break
		}
		index += start
		end := index + len(target)
		before := index == 0
		if !before {
			r, _ := utf8.DecodeLastRuneInString(text[:index])
			before = knowledgeReferenceBoundary(r)
		}
		after := end == len(text)
		if !after {
			r, _ := utf8.DecodeRuneInString(text[end:])
			after = knowledgeReferenceBoundary(r) || r == '#' || r == '?'
		}
		if before && after {
			return knowledgeTextReference{target, index}, true
		}
		start = end
	}
	return knowledgeTextReference{}, false
}

func knowledgeReferenceLocation(text string, ref knowledgeTextReference) (int, int, string) {
	prefix := text[:ref.offset]
	return strings.Count(prefix, "\n") + 1, utf8.RuneCountInString(prefix[strings.LastIndex(prefix, "\n")+1:]) + 1, "显式引用：" + knowledgeClip(ref.target, 160)
}

func appendKnowledgeMentions(g *knowledgeGraph) {
	pathCounts := map[string]int{}
	paths := map[string][]int{}
	ids := map[string][]int{}
	namespaced := map[string][]int{}
	prefixes := map[string]int{}
	for i, n := range g.Nodes {
		if n.Kind == "file" {
			pathCounts[n.Path]++
			paths[n.Path] = append(paths[n.Path], i)
			ids[n.ID] = append(ids[n.ID], i)
			prefix := ""
			if n.Root == "context" {
				prefix = "/context/"
			} else if n.Root == "source" {
				prefix = n.Source + ":"
			}
			if prefix != "" {
				target := prefix + n.Path
				namespaced[target] = append(namespaced[target], i)
				prefixes[prefix] = max(prefixes[prefix], len(target))
			}
		}
	}
	for _, from := range g.Nodes {
		if from.Text == "" || from.Kind == "symbol" {
			continue
		}
		// Resolve actual reference targets through indexes, instead of testing
		// every file against every document (quadratic on large repositories).
		refs := map[int]knowledgeTextReference{}
		put := func(indices []int, ref knowledgeTextReference) {
			for _, i := range indices {
				if from.ID != g.Nodes[i].ID {
					if _, exists := refs[i]; !exists {
						refs[i] = ref
					}
				}
			}
		}
		for _, span := range knowledgeStableReferences.FindAllStringSubmatchIndex(from.Text, -1) {
			put(ids[from.Text[span[2]:span[3]]], knowledgeTextReference{from.Text[span[0]:span[1]], span[0]})
		}
		for prefix, maxLength := range prefixes {
			for start := 0; start < len(from.Text); {
				at := strings.Index(from.Text[start:], prefix)
				if at < 0 {
					break
				}
				at += start
				start = at + len(prefix)
				if at > 0 {
					r, _ := utf8.DecodeLastRuneInString(from.Text[:at])
					if !knowledgeReferenceBoundary(r) {
						continue
					}
				}
				for end := start; end <= min(len(from.Text), at+maxLength); end++ {
					if end < len(from.Text) {
						r, _ := utf8.DecodeRuneInString(from.Text[end:])
						if !knowledgeReferenceBoundary(r) && r != '#' && r != '?' {
							continue
						}
					}
					target := from.Text[at:end]
					put(namespaced[target], knowledgeTextReference{target, at})
				}
			}
		}
		for _, ref := range knowledgeTextReferences(from.Text) {
			rel := knowledgeLocalReference(ref.target)
			if rel == "" {
				continue
			}
			if from.Kind == "file" {
				rel = path.Clean(path.Join(path.Dir(from.Path), rel))
			} else {
				rel = path.Clean(rel)
			}
			for _, i := range paths[rel] {
				to := g.Nodes[i]
				sameArea := from.Region == to.Region && from.Root == to.Root && from.Source == to.Source
				if sameArea || from.Kind == "session" && pathCounts[to.Path] == 1 {
					put([]int{i}, ref)
				}
			}
		}
		order := make([]int, 0, len(refs))
		for i := range refs {
			order = append(order, i)
		}
		sort.Ints(order) // Preserve historical graph order and the bounded edge cap.
		for _, i := range order {
			to, ref := g.Nodes[i], refs[i]
			line, column, evidence := knowledgeReferenceLocation(from.Text, ref)
			g.Edges = append(g.Edges, knowledgeEdge{From: from.ID, To: to.ID, Kind: "mention", Line: line, Column: column, Evidence: evidence, Confidence: "explicit_reference"})
			if len(g.Edges) >= 4000 {
				g.Truncated = true
				return
			}
		}
	}
}

var knowledgeStableReferences = regexp.MustCompile(`\[([^\[\]\r\n]+)\]`)
