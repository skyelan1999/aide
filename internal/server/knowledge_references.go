package server

import (
	"net/url"
	"path"
	"regexp"
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
	for _, n := range g.Nodes {
		if n.Kind == "file" {
			pathCounts[n.Path]++
		}
	}
	for _, from := range g.Nodes {
		if from.Text == "" || from.Kind == "symbol" {
			continue
		}
		refs := map[string]knowledgeTextReference{}
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
			if _, exists := refs[rel]; !exists {
				refs[rel] = ref
			}
		}
		for _, to := range g.Nodes {
			if to.Kind != "file" || from.ID == to.ID {
				continue
			}
			stable := "[" + to.ID + "]"
			index := strings.Index(from.Text, stable)
			ref, explicit := knowledgeTextReference{stable, index}, index >= 0
			if !explicit && to.Root == "context" {
				ref, explicit = knowledgeNamespacedReference(from.Text, "/context/"+to.Path)
			}
			if !explicit && to.Root == "source" {
				ref, explicit = knowledgeNamespacedReference(from.Text, to.Source+":"+to.Path)
			}
			localRef, hasLocal := refs[to.Path]
			sameArea := from.Region == to.Region && from.Root == to.Root && from.Source == to.Source
			local := hasLocal && (sameArea || from.Kind == "session" && pathCounts[to.Path] == 1)
			if !explicit && local {
				ref = localRef
			}
			if explicit || local {
				line, column, evidence := knowledgeReferenceLocation(from.Text, ref)
				g.Edges = append(g.Edges, knowledgeEdge{From: from.ID, To: to.ID, Kind: "mention", Line: line, Column: column, Evidence: evidence, Confidence: "explicit_reference"})
				if len(g.Edges) >= 4000 {
					g.Truncated = true
					return
				}
			}
		}
	}
}
