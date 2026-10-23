package server

import (
	"encoding/json"
	"regexp"
	"strings"
	"unicode"
)

// Cheap lexical equivalence, not a semantic embedding score. Normalize wording
// and punctuation; require high bigram overlap to preserve distinct assignments.
func similarParallelTitle(a, b string) bool {
	normalize := func(s string) string {
		for _, v := range []string{"设计并给出", "设计", "给出", "方案", "说明", "design", "provide", "a plan for"} {
			s = strings.ReplaceAll(strings.ToLower(s), v, "")
		}
		return strings.Map(func(r rune) rune {
			if unicode.IsLetter(r) || unicode.IsDigit(r) {
				return r
			}
			return -1
		}, s)
	}
	a, b = normalize(a), normalize(b)
	if a == b {
		return true
	}
	numbers := regexp.MustCompile(`[0-9]+`)
	if strings.Join(numbers.FindAllString(a, -1), ",") != strings.Join(numbers.FindAllString(b, -1), ",") {
		return false
	}
	words := regexp.MustCompile(`[a-z]+`)
	if strings.Join(words.FindAllString(a, -1), ",") != strings.Join(words.FindAllString(b, -1), ",") {
		return false
	}
	grams := func(s string) map[string]bool {
		r := []rune(s)
		out := map[string]bool{}
		for i := 1; i < len(r); i++ {
			out[string(r[i-1:i+1])] = true
		}
		return out
	}
	x, y := grams(a), grams(b)
	if len(x) < 6 || len(y) < 6 {
		return false
	}
	overlap := 0
	for k := range x {
		if y[k] {
			overlap++
		}
	}
	return float64(overlap)/float64(len(x)+len(y)-overlap) >= 0.9
}
func parallelUserScope(input []Message) string {
	var scope []string
	seenAssistant := false
	for _, m := range input {
		if m.Role == "assistant" {
			seenAssistant = true
		}
		if m.Role == "user" && (!seenAssistant || strings.HasPrefix(m.Content, "【你回答过程中用户插话】")) && !strings.HasPrefix(m.Content, "[parallel analysis results") {
			scope = append(scope, m.Content)
		}
	}
	b, _ := json.Marshal(scope)
	return hash(b)
}
