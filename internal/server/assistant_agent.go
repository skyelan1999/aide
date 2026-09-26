package server

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ── #62：小秘 agentic 自主决策管线 ────────────────────────────────────────────
// 此前小秘的动作选择写死在 voice_agent.analyze() 的固定分类器里：模型只回一个
// {action: send|ignore|standby|ask} JSON，后端据此机械派发。这里把"动作选择"
// 交还给小秘自己——给它一组可调用的工具，由它结合用户意图与上下文自行决定：
//   - 直接陪聊回复（不调任何派发工具，只输出正文，留在小秘会话）
//   - dispatch_to_aide：把工作意图转达给 aide（语音由前端送当前主会话；文字由后端新建会话）
//   - push_to_session / follow_session / search_sessions / get_session：跨会话调度与跟进
//   - remember：写入小秘私有记忆
//   - be_silent：背景声/与他人对话，静默不动作
// analyze() 保留为兜底/既有甄别能力（不删除）；agentic 循环失败时回落。

// assistantDecision 小秘 agentic 循环产出的统一决策（文字 assistant-message 与
// 语音 voice-filter 共用）。Handler 再把它映射到各自前端契约。
type assistantDecision struct {
	Action       string // dispatch | chat | ask | silent
	Reply        string // 小秘对用户说的话（陪聊正文 / 转交说明 / 追问文案）
	DispatchText string // Action=dispatch：总结后要转达给 aide 的清晰意图
	Mode         string // queue | insert（dispatch 时）
	Stop         bool   // 中止/止损类（dispatch 时，始终插队）
	Ask          string // Action=ask：单个追问
	Reason       string // 一句话理由
	ToolsUsed    []string // 本次小秘实际调用了哪些工具（透明可审计）
}

// assistantAgentPrinciples 写进小秘 system prompt 的动作原则。
// 系统侧只给原则，不写死分支；具体调哪个工具由小秘自行判断。
const assistantAgentPrinciples = `【你现在怎么决定动作】不要再只回一个分类 JSON——你手上有一组工具，像真人秘书一样边判断边调工具：
- 用户只是在跟你闲聊、倾诉、问生活问题、表达情绪：什么工具都不用调，直接用一句简短、温暖、口语的话回复他（这就是"陪聊"，回复只留在本会话，绝不打扰 aide）。
- 用户明确要做一件工作（写代码、改文件、跑命令、查资料、产出正式内容）：调用 dispatch_to_aide，把啰嗦/重复/口头禅去掉，总结成一句清晰可执行的意图放进 summary；mode 默认 queue，只有"马上/立刻/现在就要"或中止止损（停/取消/不对/等一下）才用 insert；中止止损把 urgent 设为 true。
- 听上去是电视/广播/视频的背景声，或用户在跟身边真人打电话、闲聊：调用 be_silent，不要转达、不要搭话。
- 意图不清楚（缺对象、缺要做什么）：不要乱猜，直接用一句话追问最关键的一个问题（不调工具，正文就是那个问题）。
- 用户提到某个历史任务/会话、要你跟进或把结论写过去：先用 search_sessions 找到它，必要时 get_session 读一下，再 follow_session 标记或 push_to_session 推送；新独立任务用 push_to_session（ref 留空/"new"）新建会话承接。
- 想记住用户的习惯/偏好/待办：调用 remember 写进你自己的私有记忆。

【铁律】
- 涉及工作、且用户明确要转达，才 dispatch_to_aide / push_to_session；私人陪聊、情绪、背景声绝不打扰 aide 主会话。
- 你不替 aide 写代码或造正式内容；你负责听懂、转交、跟进、用大白话讲结果。
- 删除/覆盖/发布等不可逆动作，先在回复里向用户确认，不要直接派发。
- 先在心里判断，再调工具或回复；工具结果回来后继续判断，必要时再调，最后用一句话收尾（告诉用户你做了什么）。`

// assistantAgentToolSchemas 小秘 agentic 循环可用的工具 schema（ curated，
// 不含 run_shell/write_file 等高危工具——小秘不直接操作工作区，只做调度与陪聊）。
func assistantAgentToolSchemas() []any {
	base := crossSessionToolSchemas() // search_sessions/get_session/follow_session/push_to_session
	extra := []any{
		map[string]any{"type": "function", "function": map[string]any{
			"name":        "dispatch_to_aide",
			"description": "把用户的一个工作意图转达给 aide 去做。summary 要去掉啰嗦/口头禅，保留对象、动作和约束，写成一句可直接执行的话。仅在判断这是工作任务、需要 aide 接手时调用；闲聊/情绪/背景声不要调用。",
			"parameters": map[string]any{"type": "object", "properties": map[string]any{
				"summary": map[string]any{"type": "string", "description": "总结后的清晰意图（给 aide 的任务说明）"},
				"mode":    map[string]any{"type": "string", "enum": []string{"queue", "insert"}, "description": "queue=排队等当前做完（默认）；insert=插队立即打断当前回答（仅紧急/中止止损）"},
				"urgent":  map[string]any{"type": "boolean", "description": "是否为中止/止损类（停/取消/不对/等一下），是则始终插队"},
			}, "required": []string{"summary"}}}},
		map[string]any{"type": "function", "function": map[string]any{
			"name":        "be_silent",
			"description": "判断这句是电视/广播/视频的背景声，或用户在跟身边真人打电话/闲聊：不转达、不搭话、不做任何动作。",
			"parameters": map[string]any{"type": "object", "properties": map[string]any{
				"reason": map[string]any{"type": "string", "description": "一句话说明为什么静默"},
			}, "required": []string{"reason"}}}},
		map[string]any{"type": "function", "function": map[string]any{
			"name":        "remember",
			"description": "把用户的习惯/偏好/待办记进你自己的私有长期记忆（与 aide 的记忆分开，你可读 aide 记忆但这里写的是你自己的）。",
			"parameters": map[string]any{"type": "object", "properties": map[string]any{
				"note": map[string]any{"type": "string", "description": "要记下的一句话"},
			}, "required": []string{"note"}}}},
	}
	return append(base, extra...)
}

// runAssistantAgenticLoop 小秘 agentic 决策主循环。
// heard=用户原话/语音转写；aideCtx=aide 主会话最近上下文（可选）。
// 调用方不持 a.mu（循环内部按需加锁）；cfg 已注入 APIKey。
func (a *App) runAssistantAgenticLoop(ctx context.Context, cfg Settings, heard, aideCtx string) (assistantDecision, error) {
	va := a.voiceAgent
	// 组装 system prompt：身份核心前置 + 动作原则 + aide 记忆（只读）+ 小秘私有记忆
	a.mu.Lock()
	mem := va.memory
	a.mu.Unlock()
	aideMem := ""
	if va != nil {
		aideMem = va.readAideMemory()
	}
	aideSection := ""
	if strings.TrimSpace(aideCtx) != "" {
		aideSection = "aide 主工作台最近与用户的对话如下（用户可能让你讲解/总结/接着讨论；据此回答，不要说看不到）：\n" + aideCtx + "\n\n"
	}
	system := voiceIdentityPrompt(cfg) + "\n\n" + assistantAgentPrinciples + "\n\n" +
		aideSection +
		fmt.Sprintf("【aide 的长期记忆（只读参考、绝不修改；它不是你自己的记忆）】\n%s\n\n", aideMem) +
		fmt.Sprintf("【你自己的私有长期记忆】%s\n", voiceMemorySummary(mem))

	messages := []Message{
		{Role: "system", Content: system},
		{Role: "user", Content: heard},
	}
	tools := assistantAgentToolSchemas()
	params := ProfileParams{MaxTokens: 800, Temperature: fp(0.3)}

	dec := assistantDecision{Action: "chat"}
	var toolsUsed []string
	maxRounds := 8
	for round := 0; round < maxRounds; round++ {
		out, calls, _, err := complete(ctx, cfg, messages, params, tools, nil)
		if err != nil {
			// 空响应/上游错误：带已收集的结论返回，不硬失败
			if strings.TrimSpace(dec.Reply) != "" || dec.Action != "chat" {
				return dec, nil
			}
			return dec, err
		}
		if len(calls) == 0 {
			// 没有工具调用 → 正文就是小秘的收尾回复
			reply := strings.TrimSpace(out)
			if dec.Action == "chat" {
				dec.Reply = reply
			}
			if dec.Reply == "" {
				dec.Reply = reply
			}
			break
		}
		messages = append(messages, Message{Role: "assistant", Content: out, ToolCalls: calls})
		for _, call := range calls {
			name := call.Function.Name
			toolsUsed = append(toolsUsed, name)
			result := a.execAssistantTool(call, &dec)
			messages = append(messages, Message{Role: "tool", ToolCallID: call.ID, Content: result})
		}
	}
	dec.ToolsUsed = toolsUsed
	// 收尾：dispatch_to_aide 已被调用 → action=dispatch；be_silent 已调 → silent。
	// 否则若 Reply 像一个向用户提的问题 → ask；其余按 chat。
	if dec.Action == "dispatch" && strings.TrimSpace(dec.DispatchText) == "" {
		dec.DispatchText = strings.TrimSpace(heard)
	}
	if dec.Action == "chat" && strings.TrimSpace(dec.Reply) == "" {
		dec.Reply = "我在听，你想做点什么？"
	}
	return dec, nil
}

// execAssistantTool 执行小秘 agentic 循环里的单个工具调用。
// 与 executeToolCall 不同：不绑定 Task、不流转发流、不做文件/shell——只做调度与记忆。
// 调用方不持 a.mu；内部按需加锁。结果作为 tool 消息回灌给模型。
func (a *App) execAssistantTool(call ToolCall, dec *assistantDecision) string {
	var args map[string]any
	_ = json.Unmarshal([]byte(call.Function.Arguments), &args)
	if args == nil {
		args = map[string]any{}
	}
	str := func(k string) string { v, _ := args[k].(string); return strings.TrimSpace(v) }
	rawStr := func(k string) string { v, _ := args[k].(string); return v }
	switch call.Function.Name {
	case "dispatch_to_aide":
		summary := strings.TrimSpace(rawStr("summary"))
		if summary == "" {
			return "缺少 summary：请把要转达给 aide 的意图写清楚。"
		}
		mode := strings.ToLower(str("mode"))
		if mode != "insert" {
			mode = "queue"
		}
		urgent, _ := args["urgent"].(bool)
		dec.Action = "dispatch"
		dec.DispatchText = summary
		dec.Mode = mode
		dec.Stop = urgent
		if urgent {
			dec.Mode = "insert"
		}
		return fmt.Sprintf("已记录要转达给 aide 的意图：%s（mode=%s，urgent=%v）。请在收尾回复里用一句话告诉用户你已经转交。", summary, dec.Mode, urgent)
	case "be_silent":
		dec.Action = "silent"
		dec.Reason = str("reason")
		dec.Reply = ""
		return "好的，已静默：" + dec.Reason
	case "remember":
		note := strings.TrimSpace(rawStr("note"))
		if note == "" {
			return "缺少 note。"
		}
		a.mu.Lock()
		a.voiceAgent.memory.Habits = append(a.voiceAgent.memory.Habits, note)
		if len(a.voiceAgent.memory.Habits) > 50 {
			a.voiceAgent.memory.Habits = a.voiceAgent.memory.Habits[len(a.voiceAgent.memory.Habits)-50:]
		}
		b, _ := json.Marshal(a.voiceAgent.memory)
		mp := VoiceMemoryPath(a.dataPath)
		a.mu.Unlock()
		_ = os.MkdirAll(filepath.Dir(mp), 0o700)
		_ = os.WriteFile(mp, b, 0o600)
		return "已记入你的私有记忆：" + note
	case "search_sessions":
		incArchived := true
		if v, ok := args["includeArchived"].(bool); ok {
			incArchived = v
		}
		a.mu.Lock()
		res := a.searchSessionsTool(str("keyword"), incArchived)
		a.mu.Unlock()
		b, _ := json.Marshal(res)
		return string(b)
	case "get_session":
		a.mu.Lock()
		gs, gerr := a.getSessionTool(str("ref"))
		a.mu.Unlock()
		if gerr != nil {
			return gerr.Error()
		}
		recent := gs.Messages
		if len(recent) > 10 {
			recent = recent[len(recent)-10:]
		}
		rb, _ := json.Marshal(map[string]any{
			"number": gs.Number, "id": gs.ID, "title": gs.Title,
			"pinned": gs.Pinned, "archived": gs.Archived, "kind": gs.Kind,
			"followed": gs.FollowedByAssistant, "followNote": gs.FollowNote,
			"recentMessages": recent,
		})
		return string(rb)
	case "follow_session":
		a.mu.Lock()
		fs, ferr := a.followSessionTool(str("ref"), str("note"))
		a.mu.Unlock()
		if ferr != nil {
			return ferr.Error()
		}
		return fmt.Sprintf("已标记跟进会话 #%d %s（备注：%s）。", fs.Number, fs.Title, fs.FollowNote)
	case "push_to_session":
		msg := strings.TrimSpace(rawStr("message"))
		if msg == "" {
			return "缺少 message。"
		}
		ref := str("ref")
		a.mu.Lock()
		var target *Session
		var created bool
		if ref == "" || ref == "new" {
			now := time.Now().UTC().Format(time.RFC3339Nano)
			titleRunes := []rune(msg)
			if len(titleRunes) > 24 {
				titleRunes = titleRunes[:24]
			}
			target = &Session{
				ID: newID(), Title: string(titleRunes), Created: now, Updated: now,
				Messages: []Message{{Role: "user", Content: msg}},
				Runs:     []*Task{},
			}
			a.assignSessionNumber(target)
			a.sessions[target.ID] = target
			created = true
			_ = a.save(target)
			a.broadcastSessionsChanged(target.ID)
		} else {
			target = a.resolveSessionRef(ref)
			if target == nil {
				a.mu.Unlock()
				return "会话不存在: " + ref
			}
			target.Messages = append(target.Messages, Message{
				Role:    "user",
				Content: "[小秘转交] " + msg,
			})
			target.Updated = time.Now().UTC().Format(time.RFC3339Nano)
			_ = a.save(target)
		}
		a.mu.Unlock()
		if created {
			return fmt.Sprintf("已新建会话 #%d（%s）并写入任务：%s。", target.Number, target.Title, msg)
		}
		return fmt.Sprintf("已向会话 #%d（%s）推送：%s", target.Number, target.Title, msg)
	default:
		return "未知工具: " + call.Function.Name
	}
}

// decisionToVoiceEntry 把 agentic 决策映射回既有 VoiceHistoryEntry 契约
// （voice-filter 前端仍按 action=send/ask/standby/ignore 驱动语音面板）。
func decisionToVoiceEntry(d assistantDecision, heard string) VoiceHistoryEntry {
	e := VoiceHistoryEntry{
		Time:   time.Now().Format("2006-01-02 15:04:05"),
		Heard:  heard,
		Reason: d.Reason,
	}
	switch d.Action {
	case "dispatch":
		e.Action = "send"
		e.Text = d.DispatchText
		e.Mode = d.Mode
		e.Stop = d.Stop
		if e.Mode == "" {
			e.Mode = "queue"
		}
	case "ask":
		e.Action = "ask"
		e.Ask = d.Ask
	case "silent":
		e.Action = "ignore"
	default: // chat：语音后台不主动搭话，按 ignore 处理（回复已落 assistant 会话）
		e.Action = "ignore"
		e.Text = ""
	}
	return e
}
