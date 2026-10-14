package server

import (
	"regexp"
	"strings"
)

// This is deliberately a literal subset, not a general shell parser. Unknown
// syntax goes through the normal approval path, never best-effort execution.
func readOnlyAllowed(command string) bool {
	commands, joins, ok := literalShellCommands(command)
	if !ok {
		return false
	}
	for i, words := range commands {
		if words[0] == "cd" {
			if len(words) != 2 || strings.HasPrefix(words[1], "-") || words[1] == "" || i >= len(joins) || joins[i] != "&&" {
				return false
			}
			continue
		}
		if !literalReadOnlyCommand(words) {
			return false
		}
	}
	return true
}

func literalShellCommands(command string) ([][]string, []string, bool) {
	if len(command) > 16000 || strings.ContainsAny(command, "\x00\n\r") {
		return nil, nil, false
	}
	var commands [][]string
	var joins, words []string
	var word strings.Builder
	started := false
	var quote byte
	flushWord := func() {
		if started {
			words = append(words, word.String())
			word.Reset()
			started = false
		}
	}
	for i := 0; i < len(command); i++ {
		c := command[i]
		if quote == '\'' {
			if c == '\'' {
				quote = 0
			} else {
				word.WriteByte(c)
			}
			continue
		}
		if c == '$' || c == '`' {
			return nil, nil, false
		}
		if c == '\\' {
			if i+1 >= len(command) {
				return nil, nil, false
			}
			next := command[i+1]
			if next != ' ' && next != '\t' && next != '\'' && next != '"' && next != '\\' {
				return nil, nil, false
			}
			word.WriteByte(next)
			started = true
			i++
			continue
		}
		if quote == '"' {
			if c == '"' {
				quote = 0
			} else {
				word.WriteByte(c)
			}
			continue
		}
		switch c {
		case '\'', '"':
			quote = c
			started = true
		case ' ', '\t':
			flushWord()
		case '|', '&':
			flushWord()
			if len(words) == 0 {
				return nil, nil, false
			}
			join := string(c)
			if c == '&' {
				if i+1 >= len(command) || command[i+1] != '&' {
					return nil, nil, false
				}
				join = "&&"
				i++
			} else if i+1 < len(command) && command[i+1] == '|' {
				return nil, nil, false
			}
			commands = append(commands, words)
			words = nil
			joins = append(joins, join)
		case ';', '<', '>', '(', ')', '{', '}', '*', '?', '[', ']', '~', '#':
			return nil, nil, false
		default:
			word.WriteByte(c)
			started = true
		}
	}
	if quote != 0 {
		return nil, nil, false
	}
	flushWord()
	if len(words) == 0 || len(commands) > 15 {
		return nil, nil, false
	}
	return append(commands, words), joins, true
}

var readOnlySedSelection = regexp.MustCompile(`^[0-9]+(,[0-9]+|,\$)?p$`)

func literalReadOnlyCommand(words []string) bool {
	name := words[0]
	// No executable paths, env assignments, interpreters or arbitrary scripts.
	switch name {
	case "ls", "cat", "head", "tail", "wc", "stat", "pwd", "echo", "which", "type", "true", "false", "test", "du", "df", "grep", "tr":
		return true
	case "find", "rg", "file":
		for _, w := range words[1:] {
			option := strings.SplitN(w, "=", 2)[0]
			if name == "find" {
				switch option {
				case "-delete", "-exec", "-execdir", "-ok", "-okdir", "-fprint", "-fprint0", "-fprintf", "-fls":
					return false
				}
			}
			if name == "rg" && len(option) > 2 && strings.HasPrefix(option, "--") && strings.HasPrefix("--pre", option) {
				return false
			}
			if name == "file" && ((len(option) > 2 && strings.HasPrefix(option, "--") && strings.HasPrefix("--compile", option)) || (strings.HasPrefix(option, "-") && !strings.HasPrefix(option, "--") && strings.Contains(option, "C"))) {
				return false
			}
		}
		return true
	case "sed":
		// Only numeric print selections; never -i, w/e commands or arbitrary scripts.
		return len(words) >= 3 && words[1] == "-n" && readOnlySedSelection.MatchString(words[2]) && sedReadOperands(words[3:])
	case "git":
		for _, w := range words[1:] {
			option := strings.SplitN(w, "=", 2)[0]
			for _, forbidden := range []string{"--ext-diff", "--textconv", "--no-index", "--output"} {
				if len(option) > 2 && (strings.HasPrefix(forbidden, option) || strings.HasPrefix(option, forbidden)) {
					return false
				}
			}
		}
	}
	return legacyReadOnlyAllowed(strings.Join(words, " "))
}
func sedReadOperands(words []string) bool {
	for _, w := range words {
		if strings.HasPrefix(w, "-") {
			return false
		}
	}
	return true
}
