package server

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// Findings belong to one task. Tool receipts, not assistant prose, establish
// source coverage; quoted matches establish traceability, not truth/authority.
type ResearchFinding struct {
	Name       string `json:"name"`
	Value      string `json:"value"`
	Unit       string `json:"unit,omitempty"`
	Kind       string `json:"kind"`
	Evidence   int    `json:"evidence,omitempty"`
	Quote      string `json:"quote,omitempty"`
	Limitation string `json:"limitation,omitempty"`
}

var researchStatusTool = map[string]any{"type": "function", "function": map[string]any{
	"name": "research_status", "description": "Read the task's actual source receipts, PDF coverage and unresolved candidate links, plus recorded source/calculation/assumption parameters. Historical assistant claims are never coverage evidence. Read-only; no browsing, permission changes or file writes.",
	"parameters": map[string]any{"type": "object", "properties": map[string]any{}},
}}
var recordResearchTool = map[string]any{"type": "function", "function": map[string]any{
	"name": "record_research_finding", "description": "Record one parameter before modeling. source/calculation requires an existing successful tool record, an exact quote and value present in that quote. assumption must state its limitation. This checks traceability, not scientific accuracy or official authority. Conversion requires actual calculation evidence; does not write workspace files.",
	"parameters": map[string]any{"type": "object", "properties": map[string]any{
		"name": map[string]any{"type": "string"}, "value": map[string]any{"type": "string"}, "unit": map[string]any{"type": "string"},
		"kind":     map[string]any{"type": "string", "enum": []string{"source", "calculation", "assumption"}},
		"evidence": map[string]any{"type": "integer", "minimum": 1}, "quote": map[string]any{"type": "string"}, "limitation": map[string]any{"type": "string"},
	}, "required": []string{"name", "value", "kind"}},
}}

const sourceReceiptPrefix = "【读取证据】"

func sourceReceipt(raw any) string {
	m, ok := raw.(map[string]any)
	if !ok {
		return ""
	}
	u, _ := m["url"].(string)
	if u == "" {
		return ""
	}
	meta := map[string]any{}
	for _, k := range []string{"url", "title", "pages", "fromPage", "toPage", "nextPage", "lastPageComplete", "truncated", "textChars", "linksTruncated", "links"} {
		if v, exists := m[k]; exists {
			meta[k] = v
		}
	}
	b, err := json.Marshal(meta)
	if err != nil {
		return ""
	}
	return sourceReceiptPrefix + string(b) + "\n"
}

func readSourceReceipt(s string) map[string]any {
	prefix := sourceReceiptPrefix
	if !strings.HasPrefix(s, prefix) {
		prefix = "【PDF读取元数据】"
		if !strings.HasPrefix(s, prefix) {
			return nil
		}
	}
	line := strings.SplitN(strings.TrimPrefix(s, prefix), "\n", 2)[0]
	var m map[string]any
	if json.Unmarshal([]byte(line), &m) != nil {
		return nil
	}
	return m
}

func evidenceFailure(s string) bool {
	for _, prefix := range []string{"错误", "插件工具失败", "未执行", "搜索失败", "离线环境", "执行失败"} {
		if strings.HasPrefix(strings.TrimSpace(s), prefix) {
			return true
		}
	}
	var m map[string]any
	if json.Unmarshal([]byte(s), &m) == nil {
		if ok, exists := m["ok"].(bool); exists && !ok {
			return true
		}
		if status, _ := m["status"].(string); status == "unconfigured" || status == "failed" {
			return true
		}
	}
	return false
}

func (a *App) recordResearchFinding(task *Task, raw string) string {
	var f ResearchFinding
	if json.Unmarshal([]byte(raw), &f) != nil {
		return "错误：参数记录格式无效"
	}
	if strings.TrimSpace(f.Name) == "" || strings.TrimSpace(f.Value) == "" || len([]rune(raw)) > 3500 || len([]rune(f.Name)) > 160 || len([]rune(f.Value)) > 300 || len([]rune(f.Unit)) > 80 {
		return "错误：参数名称和值必填，记录过长"
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	switch f.Kind {
	case "assumption":
		if strings.TrimSpace(f.Limitation) == "" {
			return "错误：假设必须注明依据缺口和适用限制"
		}
		f.Evidence = 0
		f.Quote = ""
	case "source", "calculation":
		if f.Evidence < 1 || f.Evidence > len(task.ToolUses) || len([]rune(f.Quote)) > 1600 || strings.TrimSpace(f.Quote) == "" {
			return "错误：须引用已有工具记录和精确原文片段"
		}
		use := task.ToolUses[f.Evidence-1]
		// A plan update or another ledger entry is not independent measurement.
		if use.Tool == "update_plan" || use.Tool == "research_status" || use.Tool == "record_research_finding" || use.Tool == "ask_user" || use.Tool == "write_file" || evidenceFailure(use.Result) {
			return "错误：该记录不是成功读取或计算的证据"
		}
		if !strings.Contains(use.Result, f.Quote) || !strings.Contains(f.Quote, f.Value) {
			return "错误：工具结果中没有此精确片段或参数值，不可记为已核实；换算或推断须另列并实际计算"
		}
	default:
		return "错误：kind仅允许source/calculation/assumption"
	}
	for i, v := range task.ResearchFindings {
		if v.Name == f.Name {
			task.ResearchFindings[i] = f
			b, _ := json.Marshal(f)
			return "参数已修订（可追溯，不代表独立校准）：" + string(b)
		}
	}
	if len(task.ResearchFindings) >= 60 {
		return "错误：参数账本最多60项"
	}
	task.ResearchFindings = append(task.ResearchFindings, f)
	b, _ := json.Marshal(f)
	return "参数已记录（可追溯，不代表独立校准）：" + string(b)
}

type researchPDF struct {
	URL           string       `json:"url"`
	Pages         int          `json:"pages"`
	CompletePages int          `json:"completePages"`
	MissingRanges []string     `json:"missingRanges"`
	Covered       map[int]bool `json:"-"`
}

func receiptInt(m map[string]any, k string) int { n, _ := m[k].(float64); return int(n) }
func researchEvidenceReport(uses []ToolUse, findings []ResearchFinding) string {
	sources := []map[string]any{}
	pdfs := map[string]*researchPDF{}
	seen := map[string]bool{}
	candidates := map[string]map[string]any{}
	for i, use := range uses {
		if use.Who == "sub" {
			continue
		} // parent receipt indices must remain unambiguous
		m := readSourceReceipt(use.Result)
		if m == nil {
			continue
		}
		u, _ := m["url"].(string)
		seen[u] = true
		r := map[string]any{"evidence": i + 1, "url": u, "title": m["title"], "truncated": m["truncated"], "textChars": m["textChars"], "linksTruncated": m["linksTruncated"]}
		if n := receiptInt(m, "pages"); n > 0 && n <= 10000 {
			p := pdfs[u]
			if p == nil {
				p = &researchPDF{URL: u, Pages: n, Covered: map[int]bool{}}
				pdfs[u] = p
			}
			end := receiptInt(m, "toPage")
			if complete, ok := m["lastPageComplete"].(bool); !ok || !complete {
				end--
			}
			for j := receiptInt(m, "fromPage"); j >= 1 && j <= end && j <= n; j++ {
				p.Covered[j] = true
			}
			r["fromPage"] = m["fromPage"]
			r["toPage"] = m["toPage"]
			r["lastPageComplete"] = m["lastPageComplete"]
			r["nextPage"] = m["nextPage"]
		} else if chars, ok := m["textChars"].(float64); ok && chars < 600 {
			r["contentStatus"] = "short_or_framework; not evidence of absence"
		}
		sources = append(sources, r)
		links, _ := m["links"].([]any)
		for _, link := range links {
			l, ok := link.(map[string]any)
			if !ok {
				continue
			}
			target, _ := l["url"].(string)
			label, _ := l["text"].(string)
			if target == "" {
				continue
			}
			// Candidate classification is generic navigation guidance, not product data.
			for _, cue := range []string{"配件", "维修", "支持", "下载", "规格", "螺旋桨", "accessor", "propeller", "support", "repair", "download", "spec", ".pdf"} {
				if strings.Contains(strings.ToLower(label+" "+target), cue) {
					candidates[target] = map[string]any{"url": target, "label": label, "discoveredBy": i + 1}
					break
				}
			}
		}
	}
	keys := []string{}
	for u := range pdfs {
		keys = append(keys, u)
	}
	sort.Strings(keys)
	coverage := []*researchPDF{}
	for _, u := range keys {
		p := pdfs[u]
		p.CompletePages = len(p.Covered)
		for j := 1; j <= p.Pages; j++ {
			if p.Covered[j] {
				continue
			}
			start := j
			for j+1 <= p.Pages && !p.Covered[j+1] {
				j++
			}
			p.MissingRanges = append(p.MissingRanges, fmt.Sprintf("%d-%d", start, j))
		}
		coverage = append(coverage, p)
	}
	keys = nil
	for u := range candidates {
		if !seen[u] {
			keys = append(keys, u)
		}
	}
	sort.Strings(keys)
	unread := []map[string]any{}
	for _, u := range keys {
		if len(unread) >= 32 {
			break
		}
		unread = append(unread, candidates[u])
	}
	omitted := 0
	if len(sources) > 80 {
		omitted = len(sources) - 80
		sources = sources[len(sources)-80:]
	}
	compactFindings := append([]ResearchFinding(nil), findings...)
	for i := range compactFindings {
		compactFindings[i].Quote = boundedEvidenceText(compactFindings[i].Quote, 240)
		compactFindings[i].Limitation = boundedEvidenceText(compactFindings[i].Limitation, 400)
	}
	b, _ := json.Marshal(map[string]any{"sources": sources, "sourceReceiptsOmitted": omitted, "pdfCoverage": coverage, "unreadCandidates": unread, "unreadCandidateCount": len(keys), "findings": compactFindings, "limitations": "Coverage is computed only from real tool metadata for this task. Candidates may be irrelevant or outside permission scope; follow only relevant authorized links. Traceable source quotes are not independent validation. No global exhaustiveness claim."})
	return string(b)
}
func (a *App) researchStatus(task *Task) string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return researchEvidenceReport(task.ToolUses, task.ResearchFindings)
}

var researchFullCoverageClaim = regexp.MustCompile(`(?:完整\s*\d+\s*页(?:手册|文档)?(?:已读|已覆盖|全部读完)|全篇已读|全篇已查尽|全覆盖|全部页已读)`)

func affirmativeResearchClaim(out, claim string) bool {
	for _, clause := range strings.FieldsFunc(out, func(r rune) bool { return strings.ContainsRune("。；;！!\n", r) }) {
		if !strings.Contains(clause, claim) {
			continue
		}
		negated := false
		for _, cue := range []string{"不能说", "不可称", "不支持", "未", "没有", "撤回", "错误", "不准确", "此前", "上轮", "并非", "不等于"} {
			negated = negated || strings.Contains(clause, cue)
		}
		if !negated {
			return true
		}
	}
	return false
}

func sourceCoverageCorrection(out string, uses []ToolUse) bool {
	absoluteChannel := affirmativeResearchClaim(out, "全部渠道已查尽") || affirmativeResearchClaim(out, "官方系统性不公开")
	absolutePDF := false
	for _, claim := range researchFullCoverageClaim.FindAllString(out, -1) {
		absolutePDF = absolutePDF || affirmativeResearchClaim(out, claim)
	}
	if absoluteChannel {
		return true
	}
	if !absolutePDF {
		return false
	}
	// Do not infer PDF coverage from model words; parse the ledger we generated.
	var ledger struct {
		PDFs []researchPDF `json:"pdfCoverage"`
	}
	_ = json.Unmarshal([]byte(researchEvidenceReport(uses, nil)), &ledger)
	for _, p := range ledger.PDFs {
		if p.CompletePages < p.Pages {
			return true
		}
	}
	// A PDF claim requires at least one real PDF reading receipt.
	return len(ledger.PDFs) == 0 && (strings.Contains(strings.ToLower(out), "pdf") || strings.Contains(out, "手册") || strings.Contains(out, "文档"))
}

const researchPipelineInstruction = `【资料到工程交付】依据当前目标决定流程，不为凑步骤重复全篇下载。先列需要的参数和可接受的结果精度；已有本地来源记录、下载文档与旧报告先读取作为线索，区分原始来源和旧模型结论，关键数值回到原始来源核实；公开产品/配件参数与必须实测的翼型/接口分开。优先官方产品规格、独立配件商品页、维修支持尺寸/型号表及真实下载文档；产品规格页没有零件尺寸不能说明配件商品/维修表也没有。通过真实链接或可用检索定位，不编造URL或将该案例的数值套到其他产品。
web_search若返回unconfigured只是检索服务未配置，不代表浏览器或外网不可用；不得把它称为检索无结果。JS空框架不证明没有商品，沿已返回的购买/产品链接或站内搜索入口换路径；不要反复读取同一空页。搜索结果是线索，参数须打开来源核实。截图或照片必须经过当前模型实际支持的视觉输入；文本工具不含像素时不能宣称看到四叶/几何。无视觉能力则说明形状未核实，仍完成文字参数调查。
使用research_status查看全部实际阅读范围/缺页和来源候选；PDF截断按nextPage补读相关部分，需求不要求全篇时明确范围即可。用record_research_finding记录重要参数的值、单位、source/calculation/assumption和真实工具编号/精确片段；quote不匹配就不可作为事实。来源片段可追溯不等于来源权威或模型已校准。遇冲突保留版本、工况与来源，不混用型号。
输入足够做近似模型时透明采用假设继续交付；精确复刻仍缺关键实测尺寸时说明已完成部分和确切缺口，再问必要问题。不要因为未知数据就只索取照片/上传公开PDF。参数到几何、到计算、到图表依次保持同一输入；发现旧脚本基线与新资料冲突先更正，不沿用旧结论。模型输出必须实际运行；记录工具返回的推力/扭矩/功率、方法、工况、收敛和局限，不把BEMT叫CFD或把轴功率当电池功率。只在用户要求时运行测试。propeller工具若提供可用于尺寸约束模型，默认值只是该工具的概念基线，当前型号须重新查证与传参。
终稿给可打开的交付物、来源和剩余缺口，文字简洁；没有应用的文件提案是待审批，不是已写入。结论应随新证据修订，不保证不同模型数值或语句完全一致。`

// Engineering discovery also applies when the user does not spell out "官网".
// It does not force browsing for offline-only or code repair requests.
func requestsEngineeringResearch(prompt string) bool {
	p := strings.ToLower(prompt)
	for _, s := range []string{"不要联网", "离线", "offline", "不要访问", "修复", "插件", "写代码", "实现流程", "debug", "implement"} {
		if strings.Contains(p, s) {
			return false
		}
	}
	for _, s := range []string{"复刻", "气动模型", "原厂参数", "螺旋桨模型", "逆向建模", "reconstruct", "replicate", "aerodynamic model", "manufacturer dimensions"} {
		if strings.Contains(p, s) {
			return true
		}
	}
	return false
}
func (a *App) researchQuestionPreflight(task *Task, question, qtype string) string {
	if qtype == "confirm" || !requestsEngineeringResearch(task.Prompt) {
		return ""
	}
	publicMissing := false
	for _, s := range []string{"官方参数", "官网资料", "公开资料", "上传", "实物照片", "实测数字", "manufacturer", "official parameters"} {
		publicMissing = publicMissing || strings.Contains(question, s)
	}
	if !publicMissing || a.pluginOwnerOf("browser_read") == "" {
		return ""
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if toolDeniedIn(a.settings.DisabledTools, "browser_read") {
		return ""
	}
	for _, u := range task.ToolUses {
		if u.Tool == "browser_read" || u.Tool == "browser_snapshot" {
			return ""
		}
	}
	refusals := 0
	for _, u := range task.ToolUses {
		if u.Tool == "ask_user" && strings.HasPrefix(u.Result, "【调查尚未执行】") {
			refusals++
		}
	}
	if refusals >= taskExecutionPolicy(task).ResearchCorrections {
		return ""
	} // avoid trapping a confused model indefinitely
	return "【调查尚未执行】当前问题把未调查的公开参数混同于必须用户提供的实测尺寸。先使用已启用的只读browser_read调查相关官方产品/配件/维修支持来源，或报告真实工具/权限失败。对无法公开取得的翼型/接口保持未知；可做近似重建时标注假设交付，不要求上传同一份公开PDF。此返回不是用户回答或新授权。"
}
