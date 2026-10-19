package server

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path"
	"regexp"
	"strings"
	"unicode/utf8"
)

// Structure is an aid to discovery, never evidence of a call or runtime edge.
// Only short declarations survive this streaming read, not the scanned body.
const knowledgeOutlineReadBytes = 256 << 10
const knowledgeOutlineMaxBytes = 4096
const knowledgeOutlineMaxEntries = 32
const knowledgeOutlineEntryBytes = 240

type knowledgeOutlineEntry struct {
	Kind string `json:"kind"`
	Line int    `json:"line"`
	Text string `json:"text"`
}
type knowledgeOutline struct {
	ContentHash string                  `json:"contentHash,omitempty"`
	Entries     []knowledgeOutlineEntry `json:"entries,omitempty"`
	Truncated   bool                    `json:"truncated,omitempty"`
}

var outlineGo = regexp.MustCompile(`^func\s+(\([^)]*\)\s*)?[\pL_][\pL\pN_]*(\[|\s*\()`)
var outlinePython = regexp.MustCompile(`^(async\s+)?(def|class)\s+[\pL_][\pL\pN_]*`)
var outlineJS = regexp.MustCompile(`^(export\s+)?(default\s+)?(async\s+)?(function\s*\*?\s+[\pL_$]|class\s+[\pL_$]|(const|let|var)\s+[\pL_$][\pL\pN_$]*\s*=\s*(async\s+)?(function\b|\([^;]*\)\s*=>|[\pL_$][\pL\pN_$]*\s*=>))`)

func knowledgeOutlineExtension(p string) string {
	ext := strings.ToLower(path.Ext(p))
	// Explicit source exports retain their declaration syntax (e.g. foo.go.txt).
	if ext == ".txt" {
		inner := strings.ToLower(path.Ext(strings.TrimSuffix(p, path.Ext(p))))
		switch inner {
		case ".go", ".py", ".js", ".mjs", ".cjs", ".ts", ".tsx", ".jsx":
			return inner
		}
	}
	return ext
}
func knowledgeOutlineSupported(p string) bool {
	switch knowledgeOutlineExtension(p) {
	case ".md", ".markdown", ".rst", ".go", ".py", ".js", ".mjs", ".cjs", ".ts", ".tsx", ".jsx":
		return true
	}
	return false
}

func knowledgeReadOutline(root *os.Root, p string) knowledgeOutline {
	if !knowledgeOutlineSupported(p) {
		return knowledgeOutline{}
	}
	f, err := root.Open(p)
	if err != nil {
		return knowledgeOutline{Truncated: true}
	}
	defer f.Close()
	return knowledgeExtractOutline(f, p)
}

func knowledgeExtractOutline(r io.Reader, p string) knowledgeOutline {
	out := knowledgeOutline{}
	if !knowledgeOutlineSupported(p) {
		return out
	}
	lr := &io.LimitedReader{R: r, N: knowledgeOutlineReadBytes + 1}
	digest := sha256.New()
	reader := io.TeeReader(lr, digest)
	s := bufio.NewScanner(reader)
	s.Buffer(make([]byte, 4096), 16<<10)
	ext := knowledgeOutlineExtension(p)
	doc := ext == ".md" || ext == ".markdown" || ext == ".rst"
	line, used, consumed := 0, 0, 0
	previous, previousLine, fence := "", 0, ""
	blockComment := false
	for s.Scan() {
		line++
		consumed += len(s.Bytes()) + 1
		if consumed > knowledgeOutlineReadBytes {
			out.Truncated = true
			break
		}
		raw := s.Text()
		if !utf8.ValidString(raw) || strings.ContainsRune(raw, 0) {
			out.Truncated = true
			break
		}
		t := strings.TrimSpace(raw)
		kind, value, at := "", "", line
		if doc {
			if strings.HasPrefix(t, "```") || strings.HasPrefix(t, "~~~") {
				mark := t[:3]
				if fence == "" {
					fence = mark
				} else if strings.HasPrefix(t, fence) {
					fence = ""
				}
				previous = ""
				continue
			}
			if fence != "" {
				continue
			}
			if strings.HasPrefix(t, "#") {
				n := len(t) - len(strings.TrimLeft(t, "#"))
				if n <= 6 && len(t) > n && (t[n] == ' ' || t[n] == '\t') {
					kind, value = "heading", t
				}
			} else if previous != "" && len(t) >= 3 && (strings.Trim(t, "=") == "" || strings.Trim(t, "-") == "") {
				kind, value, at = "heading", previous, previousLine
			}
			if kind == "heading" {
				previous = ""
			} else {
				previous, previousLine = t, line
			}
		} else {
			// Conservative declaration candidates; do not claim parser/type resolution.
			if blockComment {
				if end := strings.Index(t, "*/"); end >= 0 {
					blockComment = false
				}
				continue
			}
			if strings.HasPrefix(t, "/*") {
				blockComment = !strings.Contains(t[2:], "*/")
				continue
			}
			matched := ext == ".go" && outlineGo.MatchString(t) || ext == ".py" && outlinePython.MatchString(t) || ext != ".go" && ext != ".py" && outlineJS.MatchString(t)
			if matched {
				kind, value = "declaration-candidate", t
				if i := strings.Index(value, "{"); i >= 0 {
					value = strings.TrimSpace(value[:i])
				}
			}
		}
		if kind == "" {
			continue
		}
		if len(value) > knowledgeOutlineEntryBytes {
			value = value[:knowledgeOutlineEntryBytes]
			for !utf8.ValidString(value) {
				value = value[:len(value)-1]
			}
			out.Truncated = true
		}
		if len(out.Entries) >= knowledgeOutlineMaxEntries || used+len(value) > knowledgeOutlineMaxBytes {
			out.Truncated = true
			break
		}
		used += len(value)
		out.Entries = append(out.Entries, knowledgeOutlineEntry{Kind: kind, Line: at, Text: strings.Clone(value)})
	}
	if s.Err() != nil || lr.N == 0 {
		out.Truncated = true
	}
	// Complete a bounded digest even if enough declarations were already found.
	// A partial prefix must never masquerade as a whole-file position proof.
	_, drainErr := io.Copy(io.Discard, reader)
	if lr.N == 0 {
		out.Truncated = true
	}
	if s.Err() == nil && drainErr == nil && lr.N > 0 {
		out.ContentHash = hex.EncodeToString(digest.Sum(nil))
	}
	return out
}

func knowledgeOutlineValid(o knowledgeOutline) bool {
	if o.ContentHash != "" {
		b, err := hex.DecodeString(o.ContentHash)
		if err != nil || len(b) != 32 {
			return false
		}
	}
	if len(o.Entries) > knowledgeOutlineMaxEntries {
		return false
	}
	bytes, prev := 0, 0
	for _, e := range o.Entries {
		if e.Line <= prev || e.Line > knowledgeOutlineReadBytes || e.Kind != "heading" && e.Kind != "declaration-candidate" || len(e.Text) > knowledgeOutlineEntryBytes || !utf8.ValidString(e.Text) || strings.ContainsAny(e.Text, "\x00\n\r") {
			return false
		}
		prev = e.Line
		bytes += len(e.Text)
	}
	return bytes <= knowledgeOutlineMaxBytes
}

// AI receives only matching structural snippets, never the complete outline.
func knowledgeOutlineMatches(o knowledgeOutline, query string) knowledgeOutline {
	out := knowledgeOutline{Truncated: o.Truncated}
	terms := strings.Fields(strings.ToLower(query))
	for _, e := range o.Entries {
		for _, term := range terms {
			if term != "" && strings.Contains(strings.ToLower(e.Text), term) {
				out.Entries = append(out.Entries, e)
				break
			}
		}
		if len(out.Entries) == 3 {
			break
		}
	}
	return out
}

// Bound retained structure across a graph view, including entry overhead.
const knowledgeOutlineViewBudget = 8 << 20

func knowledgeOutlineCost(o knowledgeOutline) int {
	bytes := 0
	for _, e := range o.Entries {
		bytes += len(e.Text) + 64
	}
	return bytes
}
func (c *knowledgeScanCache) keepMaterial(key string, memo knowledgeFileMemo) knowledgeFileMemo {
	if old, ok := c.files[key]; ok {
		c.outlineBytes -= knowledgeOutlineCost(old.outline)
	}
	available := knowledgeOutlineViewBudget - c.outlineBytes
	used, n := 0, 0
	for _, e := range memo.outline.Entries {
		cost := len(e.Text) + 64
		if used+cost > available {
			break
		}
		used += cost
		n++
	}
	if n < len(memo.outline.Entries) {
		memo.outline.Entries = append([]knowledgeOutlineEntry(nil), memo.outline.Entries[:n]...)
		memo.outline.Truncated = true
	}
	c.outlineBytes += used
	c.files[key] = memo
	return memo
}
