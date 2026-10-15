package server

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// Text is an exact, bounded prefix of a persisted assistant message. Bytes is
// the original byte length, not a hash or assertion about an unseen suffix.
type knowledgeAIReplyBody struct {
	Text        string `json:"text"`
	Hash        string `json:"hash"`
	SourceBytes int    `json:"sourceBytes"`
	Complete    bool   `json:"complete"`
}

func knowledgeAIHistoryByteLimit(version int) int {
	if version == 2 {
		// 256 KiB source bytes can require 1.5 MiB of JSON escaping. Existing
		// 64 KiB conclusion metadata and the 8 MiB whole-history limit remain.
		return 2 << 20
	}
	return 72 << 10
}

func validateKnowledgeAIReplyBody(version int, r knowledgeAIConclusionRecord) bool {
	if version == 1 {
		return r.Body == nil
	}
	b := r.Body
	if b == nil || len(b.Text) == 0 || len(b.Text) > knowledgeConclusionScanBytes || !utf8.ValidString(b.Text) || b.SourceBytes < len(b.Text) || b.Hash != hash([]byte(b.Text)) || b.Complete != (b.SourceBytes == len(b.Text) && r.Status == "completed") {
		return false
	}
	if r.Unclassified && r.ReplyExcerpt != conclusionBytePrefix(b.Text, knowledgeConclusionTextBytes) {
		return false
	}
	for _, c := range r.Conclusions.Items {
		if c.SourceHash != b.Hash {
			return false
		}
	}
	return true
}

// Chunks remain exact source substrings. Prefer line boundaries so ordinary
// paragraphs are not split; a single oversized line is split at UTF-8 bounds.
// Chunk offsets are bytes only for stable identity; selection still uses UTF16.
func knowledgeBodyChunks(text, key, heading string, complete bool) []knowledgeConclusion {
	out := []knowledgeConclusion{}
	sourceHash, line := hash([]byte(text)), 1
	for start := 0; start < len(text); {
		chunk := conclusionBytePrefix(text[start:], knowledgeConclusionTextBytes)
		if start+len(chunk) < len(text) {
			if end := strings.LastIndexByte(chunk, '\n'); end >= len(chunk)/2 {
				chunk = chunk[:end+1]
			}
		}
		out = append(out, knowledgeConclusion{Key: fmt.Sprintf("%s:%d", key, start), Heading: fmt.Sprintf("%s · 第%d段", heading, len(out)+1), HeadingLine: line, Line: line, EndLine: line + strings.Count(strings.TrimSuffix(chunk, "\n"), "\n"), Text: chunk, TextHash: hash([]byte(chunk)), SourceHash: sourceHash, Partial: !complete || len(chunk) < len(text)})
		start += len(chunk)
		line += strings.Count(chunk, "\n")
	}
	return out
}
