package server

import "strings"

// Only explicit requests for a reply are corrected. Ordinary questions quoted
// from source material and optional recommendations must not pause a task.
func needsClarificationCard(out string, tools []any) bool {
	available := false
	for _, tool := range tools {
		definition, _ := tool.(map[string]any)
		fn, _ := definition["function"].(map[string]any)
		if fn["name"] == "ask_user" {
			available = true
		}
	}
	if !available {
		return false
	}
	for _, cue := range []string{"要我继续", "要我现在", "你回一个", "回一个词", "请补充最关键", "需要你确认一件事", "你确认后我", "请确认是否", "请选择一项", "请告诉我你选择", "要我走 A"} {
		if strings.Contains(out, cue) {
			return true
		}
	}
	return false
}

const clarificationCardCorrection = "【执行策略纠正】上一段正文在索取用户回答，但没有调用 ask_user。不要重复正文。若这是已授权的常规步骤，直接执行可用工具并交付；若确实缺少阻止执行的输入或需要新的授权，立即调用 ask_user 显示询问卡片，有路径选择时提供2至3个具体选项。不能把未执行的下一步说成将自动继续。"

// Repair explicit immediate research commitments at the end of a final answer.
// Recommendations and source quotations alone must not trigger another round.
func needsResearchContinuation(out string, tools []any) bool {
	available := false
	for _, tool := range tools {
		definition, _ := tool.(map[string]any)
		fn, _ := definition["function"].(map[string]any)
		if fn["name"] == "browser_read" {
			available = true
		}
	}
	if !available {
		return false
	}
	tail := []rune(out)
	if len(tail) > 1200 {
		tail = tail[len(tail)-1200:]
	}
	text := string(tail)
	for _, cue := range []string{"我这就用", "我现在就读取", "我接着自定执行", "下一步（我直接继续", "下一步（我自定执行"} {
		if strings.Contains(text, cue) && (strings.Contains(text, "读取") || strings.Contains(text, "继续读")) {
			return true
		}
	}
	return false
}

const researchContinuationCorrection = "【执行策略纠正】上一段末尾承诺立即继续读取，却结束了回答。请现在调用 browser_read 执行已经承诺的只读步骤；PDF截断时使用真实nextPage/fromPage继续。不得伪造页码内容或把未执行步骤称为完成。若实际工具预算或权限阻塞，说明具体停止原因和未完成项，不再写将自动继续。"

const autonomousExecutionInstruction = `【执行与询问策略】用户已授权的任务持续执行到可交付结果；“继续”“可以”“按建议”“你自己定”的授权在任务范围内持续有效。按合理默认值选择可逆的常规步骤，简短说明假设并执行。查看网页、沿来源链接查证、读取资料等只读步骤不逐次询问“要我继续吗”。不要把资料未知当成授权缺失：先用可用工具核实；确实无对应工具时说明具体限制和已完成结果，不提出无法执行的下一步来索取许可。
只在缺少会实质改变结果且无法合理推断的关键输入、用户明确设置审核关卡、需要扩大访问范围或产生未获授权的不可逆/对外影响时暂停。既有工具权限、网站范围和操作确认仍须遵守，不绕过审批。下载公开资料本身不等于向外上传资料；是否能下载以真实工具和权限为准，不编造“下载必须逐次批准”的规则。
需要用户回答时必须调用 ask_user，让界面显示询问卡片，不在普通回复末尾留下裸问题。每次一个自包含问题，解释缺少什么及其影响；有可选路径时用 single 提供2至3个互斥选项，推荐项在前；确认具体操作用 confirm 并说明操作对象与影响，避免确认泛泛的计划。没有可选答案的关键参数用 input。不能把“其他”当作唯一选项。已回答的问题不得重复询问；收到回答后执行，不只重述计划。不要为了设计/计划/测试阶段结束而自动索取确认。
`

func normalizeClarificationOptions(qtype string, options []string) (string, []string) {
	if qtype != "single" && qtype != "multi" && qtype != "input" && qtype != "confirm" {
		qtype = "input"
	}
	if qtype == "confirm" && len(options) == 0 {
		options = []string{"确认", "需要调整"}
	}
	if (qtype == "single" || qtype == "multi") && len(options) == 0 {
		qtype = "input" // Missing choices must remain answerable, never invent approval.
	}
	return qtype, options
}
