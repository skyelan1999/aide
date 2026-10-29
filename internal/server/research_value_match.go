package server

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Classification only: numbers remain strings, with no rounding, locale
// conversion, unit conversion or implied arithmetic evaluation.
var researchScalarLiteral = regexp.MustCompile(`^[+-]?[\t\p{Zs}]*(?:0[xX][0-9a-fA-F]+|0[bB][01]+|0[oO][0-7]+|(?:[0-9]+(?:(?:[.,][0-9]+)|(?:[\t\p{Zs}]+[0-9]{3}))*|[.,][0-9]+)(?:[eE][+-]?[0-9]+)?|∞)$`)

func researchValueVariants(value string) ([]string, bool) {
	canonical := strings.ReplaceAll(value, "−", "-")
	if !researchScalarLiteral.MatchString(canonical) {
		return []string{value}, false
	}
	parts := strings.Split(canonical, "-")
	variants := []string{parts[0]}
	for _, part := range parts[1:] {
		next := make([]string, 0, len(variants)*2)
		for _, prefix := range variants {
			next = append(next, prefix+"-"+part, prefix+"−"+part)
		}
		variants = next
	}
	unsigned := strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(canonical, "+"), "-"))
	if strings.ContainsAny(canonical, "eE") && !strings.HasPrefix(strings.ToLower(unsigned), "0x") {
		next := make([]string, 0, len(variants)*2)
		for _, variant := range variants {
			next = append(next, strings.ReplaceAll(variant, "E", "e"), strings.ReplaceAll(variant, "e", "E"))
		}
		variants = next
	}
	return variants, true
}

// A value must occur inside the exact quote and retain its boundaries in the
// original tool result. In particular, quote="5" cannot launder result="150".
func researchFindingValueMatches(result, quote, value string) bool {
	value = strings.TrimSpace(value)
	if quote == "" || value == "" {
		return false
	}
	variants, numeric := researchValueVariants(value)
	type span struct{ start, end int }
	var candidates []span
	for _, variant := range variants {
		for offset := 0; offset <= len(quote)-len(variant); {
			rel := strings.Index(quote[offset:], variant)
			if rel < 0 {
				break
			}
			start := offset + rel
			end := start + len(variant)
			if researchValueBoundaries(quote, start, end, numeric) {
				candidates = append(candidates, span{start, end})
			}
			offset = start + 1
		}
	}
	if len(candidates) == 0 {
		return false
	}
	// KMP retains overlapping quote occurrences without repeatedly comparing a
	// long periodic quote against the entire result or allocating result tokens.
	prefix := make([]int, len(quote))
	for i, matched := 1, 0; i < len(quote); i++ {
		for matched > 0 && quote[i] != quote[matched] {
			matched = prefix[matched-1]
		}
		if quote[i] == quote[matched] {
			matched++
		}
		prefix[i] = matched
	}
	matched := 0
	for i := 0; i < len(result); i++ {
		for matched > 0 && result[i] != quote[matched] {
			matched = prefix[matched-1]
		}
		if result[i] == quote[matched] {
			matched++
		}
		if matched == len(quote) {
			start := i + 1 - len(quote)
			for _, candidate := range candidates {
				if researchValueBoundaries(result, start+candidate.start, start+candidate.end, numeric) {
					return true
				}
			}
			matched = prefix[matched-1]
		}
	}
	return false
}

func researchIdentifierRune(r rune) bool {
	// Han labels adjoining a value have natural boundaries; this is not a
	// natural-language word segmenter or a semantic claim validator.
	return r == '_' || unicode.IsNumber(r) || unicode.IsMark(r) || unicode.IsLetter(r) && !unicode.Is(unicode.Han, r)
}

func researchSignRune(r rune) bool {
	return r == '+' || r == '＋' || r == '−' || r == '±' || unicode.Is(unicode.Dash, r)
}

func researchHorizontalSpace(r rune) bool { return r == '\t' || unicode.Is(unicode.Zs, r) }

func researchPreviousNonspace(s string, end int) (rune, int) {
	for end > 0 {
		r, n := utf8.DecodeLastRuneInString(s[:end])
		end -= n
		if !researchHorizontalSpace(r) {
			return r, end
		}
	}
	return 0, 0
}

func researchNextNonspace(s string) string {
	for len(s) > 0 {
		r, n := utf8.DecodeRuneInString(s)
		if !researchHorizontalSpace(r) {
			break
		}
		s = s[n:]
	}
	return s
}

func researchBeginsNumber(s string) bool {
	r, n := utf8.DecodeRuneInString(s)
	if researchSignRune(r) {
		s = researchNextNonspace(s[n:])
		r, n = utf8.DecodeRuneInString(s)
	}
	if unicode.IsDigit(r) || r == '∞' {
		return true
	}
	if r == '.' || r == ',' {
		next, _ := utf8.DecodeRuneInString(s[n:])
		return unicode.IsDigit(next)
	}
	return false
}

func researchLeadingDigits(s string) int {
	count := 0
	for _, r := range s {
		if !unicode.IsDigit(r) {
			break
		}
		count++
		if count > 3 {
			break
		}
	}
	return count
}

func researchBulletAt(s string, index int, r rune) bool {
	if r != '-' && r != '+' {
		return false
	}
	for index > 0 {
		previous, n := utf8.DecodeLastRuneInString(s[:index])
		if previous == '\n' || previous == '\r' {
			return true
		}
		if !researchHorizontalSpace(previous) {
			return false
		}
		index -= n
	}
	return true
}

func researchExponentSuffix(s string) bool {
	r, n := utf8.DecodeRuneInString(s)
	if r != 'e' && r != 'E' {
		return false
	}
	s = researchNextNonspace(s[n:])
	r, n = utf8.DecodeRuneInString(s)
	if researchSignRune(r) {
		s = researchNextNonspace(s[n:])
	}
	r, _ = utf8.DecodeRuneInString(s)
	return unicode.IsDigit(r)
}

func researchValueBoundaries(s string, start, end int, numeric bool) bool {
	value := s[start:end]
	first, firstSize := utf8.DecodeRuneInString(value)
	last, _ := utf8.DecodeLastRuneInString(value)
	beginsNumber := researchBeginsNumber(value)
	if start > 0 {
		before, _ := utf8.DecodeLastRuneInString(s[:start])
		if researchIdentifierRune(first) && researchIdentifierRune(before) ||
			beginsNumber && (researchIdentifierRune(before) || researchSignRune(before)) {
			return false
		}
		if beginsNumber && !researchSignRune(first) && (before == '.' || before == ',') {
			return false
		}
		if (first == '.' || first == ',') && unicode.IsDigit(before) && researchBeginsNumber(value[firstSize:]) {
			return false
		}
		if beginsNumber && researchHorizontalSpace(before) {
			previous, index := researchPreviousNonspace(s, start)
			if researchSignRune(previous) && !researchBulletAt(s, index, previous) {
				return false
			}
			if unicode.IsDigit(first) && researchLeadingDigits(value) == 3 && unicode.IsDigit(previous) {
				return false
			}
			if previous == 'e' || previous == 'E' {
				beforeExponent, _ := utf8.DecodeLastRuneInString(s[:index])
				if unicode.IsDigit(beforeExponent) {
					return false
				}
			}
		}
	}
	if end < len(s) {
		after, nextSize := utf8.DecodeRuneInString(s[end:])
		next, _ := utf8.DecodeRuneInString(s[end+nextSize:])
		if (last == '.' || last == ',') && unicode.IsDigit(after) {
			return false
		}
		if unicode.IsDigit(last) || last == '∞' {
			if unicode.IsDigit(after) || (after == '.' || after == ',') && unicode.IsDigit(next) || researchExponentSuffix(s[end:]) {
				return false
			}
			if researchHorizontalSpace(after) && researchLeadingDigits(researchNextNonspace(s[end:])) == 3 {
				return false
			}
			zero := strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(strings.ReplaceAll(value, "−", "-"), "+"), "-"))
			if zero == "0" && (after == 'x' || after == 'X') && strings.ContainsRune("0123456789abcdefABCDEF", next) ||
				zero == "0" && (after == 'b' || after == 'B') && (next == '0' || next == '1') ||
				zero == "0" && (after == 'o' || after == 'O') && next >= '0' && next <= '7' {
				return false
			}
		}
		if numeric && (after == '_' || unicode.IsMark(after)) {
			return false
		}
		if researchIdentifierRune(last) && researchIdentifierRune(after) && !(numeric && unicode.IsLetter(after)) {
			return false
		}
	}
	return true
}
