package server

import (
	"regexp"
	"strings"

	"github.com/yuin/goldmark/v2/ast"
	"github.com/yuin/goldmark/v2/parser"
)

// This is the workbench's progress label. It carries no arguments or call IDs.
const toolNoticePrefix = "工具调用中："

var toolNoticeNames = regexp.MustCompile(`^[a-zA-Z0-9_-]+(?:, *[a-zA-Z0-9_-]+)*$`)

func splitUnissuedToolNotice(text string) (string, []string) {
	end := strings.TrimRight(text, " \t\r\n")
	line := strings.LastIndexByte(end, '\n') + 1
	names, found := strings.CutPrefix(end[line:], toolNoticePrefix)
	if !found || !toolNoticeNames.MatchString(names) {
		return text, nil
	}
	// A quoted example, code block, or list continuation is ordinary Markdown.
	last := parser.New().Parse([]byte(end)).LastChild()
	if last == nil || last.Kind() != ast.KindParagraph {
		return text, nil
	}
	return strings.TrimRight(end[:line], " \t\r\n"), strings.Split(strings.ReplaceAll(names, " ", ""), ",")
}

func quotedToolNotice(prompt, text string) bool {
	prompt = strings.ToLower(prompt)
	line := strings.TrimSpace(text)
	if i := strings.LastIndexByte(line, '\n'); i >= 0 {
		line = line[i+1:]
	}
	if !strings.Contains(prompt, strings.ToLower(line)) && !strings.Contains(prompt, "工具调用") && !strings.Contains(prompt, "tool call") {
		return false
	}
	for _, intent := range []string{"引用", "原样", "照读", "复述", "原文输出", "只输出", "只回答", "示例", "举例", "格式说明", "quote", "verbatim", "example", "exactly"} {
		if strings.Contains(prompt, intent) {
			return true
		}
	}
	return false
}

func toolCallFinish(finish string) bool {
	return finish == "tool_calls" || finish == "function_call"
}

// Require an actual tool-capable response and an offered name. A malformed
// provider finish_reason can independently declare a call that never arrived.
func toolResponseProse(prompt, out, finish string, tools []any, calls []ToolCall, err error) (string, bool) {
	if err != nil || len(calls) > 0 || len(tools) == 0 || finish == "length" {
		return out, false
	}
	prose, names := splitUnissuedToolNotice(out)
	if len(names) > 0 && !quotedToolNotice(prompt, out) {
		offered := requestToolNames(tools)
		for _, name := range names {
			if offered[name] {
				return prose, true
			}
		}
	}
	return out, toolCallFinish(finish)
}

const unissuedToolCorrection = "【工具协议纠正】上一次响应没有提供结构化工具调用，进度文字没有执行任何工具。若完成当前任务仍需工具，请通过本次请求提供的 tools 返回 tool_calls 和真实参数；依据实际工具结果继续。若无需工具，可以直接回答。"
