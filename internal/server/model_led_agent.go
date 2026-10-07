package server

import (
	"encoding/json"
	"fmt"
	"strings"
)

// A plan records the model's working intent, not proof of execution or approval.
type AgentPlanItem struct {
	Step     string `json:"step"`
	Status   string `json:"status"`
	Evidence []int  `json:"evidence,omitempty"`
}

type AgentPlan struct {
	Explanation string          `json:"explanation,omitempty"`
	Items       []AgentPlanItem `json:"items"`
}

var updatePlanTool = map[string]any{"type": "function", "function": map[string]any{
	"name":        "update_plan",
	"description": "Maintain a concise working plan for a multi-step task. Revise it as real tool results arrive. Status is your assessment, not execution proof or user approval. Simple questions need no plan. Evidence contains 1-based indices of existing task tool records. Mark blocked steps with a concrete reason in explanation; never invent a completed action.",
	"parameters": map[string]any{"type": "object", "properties": map[string]any{
		"explanation": map[string]any{"type": "string"},
		"plan": map[string]any{"type": "array", "minItems": 1, "maxItems": 12, "items": map[string]any{"type": "object", "properties": map[string]any{
			"step": map[string]any{"type": "string"}, "status": map[string]any{"type": "string", "enum": []string{"pending", "in_progress", "completed", "blocked"}},
			"evidence": map[string]any{"type": "array", "items": map[string]any{"type": "integer", "minimum": 1}},
		}, "required": []string{"step", "status"}}},
	}, "required": []string{"plan"}},
}}

func modelLedWorkflow(task *Task) bool {
	if task.Mode != "workflow" || task.Strategy != "auto" || (task.WorkflowPhase != "" && task.WorkflowPhase != "auto") {
		return false
	}
	// Older paused tasks must finish the same fixed pipeline they started.
	for _, step := range task.Steps {
		if step.Name == "plan" || step.Name == "propose" || step.Name == "review" {
			return false
		}
	}
	return true
}

func (a *App) updateAgentPlan(task *Task, raw string) string {
	var in struct {
		Explanation string          `json:"explanation"`
		Plan        []AgentPlanItem `json:"plan"`
	}
	if err := json.Unmarshal([]byte(raw), &in); err != nil {
		return "错误：无效的计划参数"
	}
	if len(in.Plan) == 0 || len(in.Plan) > 12 || len([]rune(in.Explanation)) > 2000 {
		return "错误：计划须包含1至12项，说明不超过2000字"
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	active := 0
	for i := range in.Plan {
		item := &in.Plan[i]
		item.Step = strings.TrimSpace(item.Step)
		if item.Step == "" || len([]rune(item.Step)) > 300 || len(item.Evidence) > 20 {
			return "错误：计划步骤或证据数量超限"
		}
		switch item.Status {
		case "in_progress":
			active++
		case "pending", "completed", "blocked":
		default:
			return "错误：无效的计划状态"
		}
		for _, id := range item.Evidence {
			if id < 1 || id > len(task.ToolUses) {
				return "错误：证据必须引用已有工具记录编号"
			}
		}
	}
	if active > 1 {
		return "错误：同一时间最多一个执行中的计划步骤"
	}
	if task.AgentPlan != nil {
		task.AgentPlan.Explanation = in.Explanation
		task.AgentPlan.Items = in.Plan
	} else {
		task.AgentPlan = &AgentPlan{Explanation: in.Explanation, Items: in.Plan}
	}
	// Checkpoint callback persists this alongside the paired tool response.
	encoded, _ := json.Marshal(task.AgentPlan)
	return "计划已更新（模型评估，不代表文件已应用或操作已授权）：" + string(encoded)
}

// Count execution progress, excluding bookkeeping that cannot justify another
// completion review on its own. Called under a.mu with the task fields stable.
func agentExecutionCount(task *Task) int {
	count := 0
	for _, use := range task.ToolUses {
		switch use.Tool {
		case "update_plan", "research_status", "record_research_finding", "avatar_cue":
			continue
		default:
			count++
		}
	}
	return count
}

func agentReviewNeeded(task *Task) bool {
	count := agentExecutionCount(task)
	reviews := task.AgentReviewCount
	if task.AgentReviewDone && reviews == 0 {
		reviews = 1
	}
	return count > 0 && reviews < taskExecutionPolicy(task).CompletionReviews && (!task.AgentReviewDone || count > task.AgentReviewToolCount)
}

// Compact factual ledger for a bounded model-owned completion decision. It does
// not choose a business action, grant permission, or hide failed tool results.
func (a *App) agentCompletionReview(task *Task) string {
	a.mu.Lock()
	defer a.mu.Unlock()
	var b strings.Builder
	b.WriteString(taskExecutionPolicy(task).CompletionInstruction + "\n")
	if task.AgentPlan != nil {
		p, _ := json.Marshal(task.AgentPlan)
		b.WriteString("当前计划：" + string(p) + "\n")
	}
	start := len(task.ToolUses) - 16
	if start < 0 {
		start = 0
	}
	for i := start; i < len(task.ToolUses); i++ {
		use := task.ToolUses[i]
		result := []rune(use.Result)
		if len(result) > 400 {
			result = result[:400]
		}
		fmt.Fprintf(&b, "工具记录%d %s 参数：%s；返回：%s\n", i+1, use.Tool, boundedEvidenceText(use.Args, 1000), string(result))
	}
	b.WriteString("\n实际来源与参数账本（早期记录仍保留，不以最近16条替代覆盖）：" + researchEvidenceReport(task.ToolUses, task.ResearchFindings) + "\n")
	b.WriteString("完整结果已在前文；本账本只展示最近16条的摘要，不代表其他资料已读取。")
	return b.String()
}

func boundedEvidenceText(text string, limit int) string {
	r := []rune(text)
	if len(r) > limit {
		return string(r[:limit]) + "（摘要截断）"
	}
	return text
}

const modelLedInstruction = "你是 aide 的执行智能体。围绕用户目标自主决定是否需要规划、调查、实施、验证及调整顺序；复杂任务使用 update_plan 维护简短计划，简单问题直接回答。不机械执行固定阶段，不为凑流程启动子智能体或要求用户改参数。已授权且必要的工作持续推进；只有确实缺少关键输入、遇到权限边界或用户指定审核关卡才暂停。用户插话修正当前目标时保留已完成工作，不重新起跑。每次工具结果返回后判断下一步；工具失败时选择合理替代路径，保留事实和缺口。用户明确指定阶段或审核关卡时遵守。文件写入仍使用提案和现有批准流程；未应用的提案不得写成已完成。仅在用户要求测试/验证时执行测试。计划完成状态是你的评估，最终结论须依据实际证据。"

const completionInstruction = "【任务收尾自检】请根据用户当前目标、插话、计划和本轮真实工具结果，自行判断任务是否已交付。若仍有授权内可完成的必要步骤，直接调用工具并修订计划；若已经完成或有实际阻塞，给出准确的最终结果与缺口。不得因为计划写completed就宣称执行成功。不得承诺下一步自动执行却结束。没有证据的检查写未验证。已发现、与当前目标相关且可访问的真实来源链接，应在授权范围内现在读取；不得用预判无结果代替访问，也不得把其留作需要用户再次同意的下一轮。区分“已读范围未找到”与“官方不公开”，不能声称全部渠道已查尽。PDF必须依据本轮元数据报告页码、截断和缺页，不根据章节猜测省略未读页；参数测量条件缺失时不得宣称已足够校准仿真。历史助手声称的已读/已执行不是工具证据。收尾检查次数遵守当前执行策略，仅在新增实际执行记录后再次检查；不能为触发检查而凑工具调用。不要求额外测试或泛泛确认。\n"
