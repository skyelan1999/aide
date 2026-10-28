package server

import (
	"fmt"
	"regexp"
	"strings"
)

func assistantStartedReply(intent string) string {
	if regexp.MustCompile(`查|检索|搜索|天气|几度|归属地`).MatchString(intent) {
		return "好，我去看一下，查到就告诉你。"
	}
	return "好，我来处理，有结果就告诉你。"
}

func assistantUnverifiedActionClaim(text string) bool {
	return regexp.MustCompile(`已(?:经)?(?:转交|交办|派发|启动|安排)|我(?:这就|去)(?:查|看一下)|已经让\s*aide`).MatchString(text)
}

// A short presentation of the executor's public answer. The original report
// stays in its source task; receipts fingerprint that original, not this style.
func assistantResultText(event assistantWatchEvent, run *Task) string {
	if event.Status != "completed" {
		text := event.Text
		if event.Blocker != "" {
			text += "\n" + boundedEvidenceText(event.Blocker, 160)
		}
		return text
	}
	detail := ""
	for i := len(run.Steps) - 1; i >= 0; i-- {
		if run.Steps[i].Name == "chat" || run.Steps[i].Name == "agent" {
			detail = strings.TrimSpace(run.Steps[i].Content)
			break
		}
	}
	if detail == "" {
		return "这项任务已经结束，但还没有拿到可汇总的结果。我先不猜，详情在原任务里。"
	}
	// Prefer the explicit result section; discard execution bookkeeping around it.
	if i := strings.Index(detail, "**结果**"); i >= 0 {
		detail = detail[i+len("**结果**"):]
	} else if i := strings.Index(detail, "## 最终结果"); i >= 0 {
		detail = detail[i+len("## 最终结果"):]
	} else {
		detail = regexp.MustCompile(`^(?:任务已完成[，。][^\n]*|任务已交付[，。][^\n]*|已交付[，。]无需补做。)[\s\n]*`).ReplaceAllString(detail, "")
	}
	for _, marker := range []string{"**依据**", "**缺口**", "## 缺口", "实际数据来源：", "**实际完成的内容**"} {
		if i := strings.Index(detail, marker); i >= 0 {
			detail = detail[:i]
		}
	}
	detail = strings.TrimSpace(strings.TrimLeft(detail, "：: \n"))
	detail = strings.NewReplacer("**", "", "`", "", "\n", " ").Replace(detail)
	detail = strings.TrimSpace(regexp.MustCompile(`\s+`).ReplaceAllString(detail, " "))
	if len([]rune(detail)) > 220 {
		r := []rune(detail)
		cut := 220
		for i := len(r[:220]) - 1; i >= 80; i-- {
			if strings.ContainsRune("。！？；", r[i]) {
				cut = i + 1
				break
			}
		}
		detail = string(r[:cut])
		if cut == 220 {
			detail += "…"
		}
	}
	if detail == "" {
		detail = "结果已经回来了。"
	} else {
		detail = "查到了，" + detail
	}
	if event.Delivery != nil && event.Delivery.State != "delivered" {
		detail += " 但交付还没核验，不能视为已交付。"
	}
	return detail + fmt.Sprintf("\n\n完整结果和来源见任务 #%s。", strings.TrimPrefix(regexp.MustCompile(`任务 #([0-9]+).*`).ReplaceAllString(event.Text, "$1"), "#"))
}
