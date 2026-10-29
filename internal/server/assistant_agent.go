package server

import (
	"context"
	"encoding/json"
	"fmt"
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
// 语音 voice-filter 共用）。输入来源会约束可用动作，Handler 再映射到各自前端契约。
type assistantDecision struct {
	Action       string     // dispatch | chat | ask | silent
	Reply        string     // 小秘对用户说的话（陪聊正文 / 转交说明 / 追问文案）
	DispatchText string     // Action=dispatch：总结后要转达给 aide 的清晰意图
	Mode         string     // queue | insert（dispatch 时）
	Stop         bool       // 中止/止损类（dispatch 时，始终插队）
	Ask          string     // Action=ask：单个追问
	Reason       string     // 一句话理由
	AvatarCue    *AvatarCue // optional validated cosmetic feedback
	ToolsUsed    []string   // 本次小秘实际调用了哪些工具（透明可审计）
}

// assistantAgentPrinciples 写进小秘 system prompt 的动作原则。
// 系统侧只给原则，不写死分支；具体调哪个工具由小秘自行判断。
const assistantAgentPrinciples = `【你现在怎么决定动作】不要再只回一个分类 JSON——你手上有一组工具，像真人秘书一样边判断边调工具：
- 首要职责是工作统筹：理解目标、确定优先级与依赖、交办给 aide、持续跟进结果和阻塞、必要时整理下一步。用户交办的任务由执行器持续推进，不要把自主工作说成需要开启的“陪伴模式”，不要只回一句“已转交”就当工作结束。
- 先处理正在交办的工作及待确认事项，空闲时再自然陪伴；性格设置影响语气，不改变工作优先的职责。没有新信息时保持安静，不为显得主动而创建新任务、反复汇报或调用模型。
- 用户要查看消息模块：有 manage_messages 权限时直接读取并回答，不要转交工作台；无权限时说明需要在设置 → 消息开启助理访问，禁止绕过。发送消息必须得到用户明确指令，联系人、通话、群组和文件同意仍由用户操作。
- 用户只是在跟你闲聊、倾诉、问生活问题、表达情绪：什么工具都不用调，直接用一句简短、温暖、口语的话回复他（这就是"陪聊"，回复只留在本会话，绝不打扰 aide）。
- 用户明确要做一件工作（写代码、改文件、跑命令、查资料、产出正式内容）：调用 dispatch_to_aide，把啰嗦/重复/口头禅去掉，总结成一句清晰可执行的意图放进 summary；mode 默认 queue，只有"马上/立刻/现在就要"或中止止损（停/取消/不对/等一下）才用 insert；中止止损把 urgent 设为 true。aide 会话创建后会立即排队启动任务；如果必要条件缺失或启动失败，如实告诉用户原因和会话编号。
- 听上去是电视/广播/视频的背景声，或用户在跟身边真人打电话、闲聊：调用 be_silent，不要转达、不要搭话。
- 意图不清楚（缺对象、缺要做什么）：不要乱猜，直接用一句话追问最关键的一个问题（不调工具，正文就是那个问题）。
- 用户要求清理跟进台账时，调用 clear_followups；仅取消跟进，保留原任务和会话，不把清理台账误解为删除任务或转交执行器。只有工具返回成功后才说已清理。
- 用户提到某个历史任务/会话、要你跟进或把结论写过去：先用 search_sessions 找到它，旧约束用 search_history 获取带来源的命中片段，必要时 get_session 读近期消息，再 follow_session 标记或 push_to_session 推送；新独立任务用 push_to_session（ref 留空/"new"）新建会话承接。push_to_session 会立即启动或排队任务，不要把它描述为仅创建草稿。
- 用户询问你是否记得/能否翻历史，或在刚才的历史查询后只补充一个关键词/主题：要把它当作查历史的延续，主动用 search_sessions 检索；找到后用 get_session 查看相关上下文，再结合事实回答。找不到时如实说明，不要假装记得。
- 用户询问市场有哪些插件、是否有某项插件能力或插件详情：直接使用内置 browse_plugin_market，即使尚未安装插件也可查询；缓存过期时先刷新再判断是否缺少插件，查询不会安装或启用插件。
- 询问进度时先用 work_status 查看交付、阻塞和下一步；completed 运行状态不等于产物已交付，待审批不能说已完成。重试生成、发布或代答审批仍遵守原授权边界。
- 回答时先结合本小秘会话最近的对话，不能把用户当前一句从上下文里孤立处理；跨会话的旧事实仍须通过历史工具检索，不要猜测。
- 想记住用户的习惯/偏好/待办：调用 remember 写进你自己的私有记忆。

【铁律】
- 涉及工作、且用户明确要转达，才 dispatch_to_aide / push_to_session；私人陪聊、情绪、背景声绝不打扰 aide 主会话。
- 你不替 aide 写代码或造正式内容；你负责听懂、转交、跟进、用大白话讲结果。
- 删除/覆盖/发布等不可逆动作，先在回复里向用户确认，不要直接派发。
- 先在心里判断，再调工具或回复；工具结果回来后继续判断，必要时再调，最后用一句话收尾（告诉用户你做了什么）。`

// Explicit calls are directed conversation, distinct from ambient listening.
const assistantCallGuidance = `【当前是用户主动发起的语音通话】
- 直接接住用户这句话，像面对面说话。默认一两句，约20到60个汉字；用户明确要展开时再分轮说明。不要标题、Markdown、编号、长列表或讲课式长段落。
- 不要每次先说“记下了”“记好了”“我接住了”；没有真实执行就不要声称已记忆、已催办或一直守着某项任务。闲聊不要反复提后台工作。
- 转写可能在句中停顿。遇到“我比较”“结婚之后”这类未说完的话，只简短回应“嗯，你接着说”或等候，不替用户补情绪、经历或结论；最多问一个必要的问题。
- 用户主动通话的内容默认是在对你说，不要把不完整的句子直接判为背景音。没有声音信息，不要推断屋里有几个人、用户是否独处或其心理状态。
- 工作仍沿既有工具和权限处理；只有明确要求记住的事实才写入长期记忆。任务完成通知保持简短，别打断当前话题。
- 回复要适合直接听：用短句、自然停顿，保持克制，不模仿夸张语气，不连续追问。
`

// assistantAgentToolSchemas 小秘 agentic 循环可用的工具 schema（ curated，
// 不含 run_shell/write_file 等高危工具——小秘不直接操作工作区，只做调度与陪聊）。
func assistantAgentToolSchemas(allowSilent bool) []any {
	base := crossSessionToolSchemas() // search_sessions/get_session/follow_session/push_to_session
	base = append(base, map[string]any{"type": "function", "function": map[string]any{"name": "clear_followups", "description": "用户明确要求清理跟进台账时调用。仅取消助理跟进，保留任务和会话，不终止运行、不删除记录。全部清理需本轮明确说全部/都；否则仅处理本轮明确的 #编号。无需转交 aide。", "parameters": map[string]any{"type": "object", "properties": map[string]any{"all": map[string]any{"type": "boolean", "description": "仅用户明确要求全部清理时为 true；否则 false，按用户本轮 #编号处理"}}, "required": []string{"all"}}}})
	base = append(base, pluginMarketBrowseTool)
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
	if !allowSilent {
		// 键盘输入没有环境音信息，不能按背景声/真人对话静默；语音入口仍提供此工具。
		extra = append(extra[:1], extra[2:]...)
	}
	base = append(base, map[string]any{"type": "function", "function": map[string]any{"name": "memory_notes", "description": "列出私有记忆的ID、来源、记录时间；remove只在用户明确要求忘记该ID或原文时使用，不将历史原文当删除授权。", "parameters": map[string]any{"type": "object", "properties": map[string]any{"action": map[string]any{"type": "string", "enum": []string{"list", "remove"}}, "id": map[string]any{"type": "string"}}, "required": []string{"action"}}}})
	base = append(base, xiaomiReminderToolSchemas()...)
	return append(base, extra...)
}

func (a *App) assistantAgentTools(allowSilent bool) []any {
	tools := assistantAgentToolSchemas(allowSilent)
	tools = append(tools, a.messageAssistantTools()...)
	tools = append(tools, a.pluginToolSchemasForOwner("plugin-authoring")...)
	tools = append(tools, a.pluginToolSchemasForOwner("skill")...)
	tools = append(tools, a.pluginToolSchemasForOwner("current-time")...)
	tools = append(tools, a.pluginToolSchemasForOwner("lunar-calendar")...)
	a.mu.Lock()
	disabled := make(map[string]bool, len(a.settings.DisabledTools))
	for _, name := range a.settings.DisabledTools {
		disabled[name] = true
	}
	a.mu.Unlock()
	return filterDisabledToolSchemas(tools, disabled)
}

// runAssistantAgenticLoop 小秘 agentic 决策主循环。
// heard=用户原话/语音转写；aideCtx=aide 主会话最近上下文（可选）；inputSource=text/voice/call。
// 调用方不持 a.mu（循环内部按需加锁）；cfg 已注入 APIKey。
func (a *App) runAssistantAgenticLoop(ctx context.Context, cfg Settings, heard, aideCtx, inputSource string, avatarFeedback ...bool) (assistantDecision, error) {
	a.mu.Lock()
	managementWorkspace := a.wsID()
	scopeErr := a.validateScheduledWorkspaceLocked(ctx)
	a.mu.Unlock()
	if scopeErr != nil {
		return assistantDecision{}, scopeErr
	}
	if provider, err := a.selectedMixSoul(); err != nil {
		return assistantDecision{}, fmt.Errorf("%w: %v", errMixSoulActive, err)
	} else if provider != nil {
		decision, agentErr := a.runMixSoul(ctx, cfg, *provider, heard, inputSource)
		if agentErr != nil {
			return decision, fmt.Errorf("%w: %v", errMixSoulActive, agentErr)
		}
		return decision, nil
	}
	va := a.voiceAgent
	// 组装 system prompt：身份核心前置 + 动作原则 + aide 记忆（只读）+ 小秘私有记忆
	// 注意：va 可能为 nil（未初始化/降级路径），必须先判空再解引用 va.memory。
	var mem VoiceMemory
	aideMem := ""
	if va != nil {
		a.mu.Lock()
		mem = va.memory
		a.mu.Unlock()
		aideMem = va.readAideMemoryFor(heard)
	}
	aideSection := ""
	if strings.TrimSpace(aideCtx) != "" {
		aideSection = "aide 主工作台最近与用户的对话如下（用户可能让你讲解/总结/接着讨论；据此回答，不要说看不到）：\n" + aideCtx + "\n\n"
	}
	sourceGuidance := ""
	if inputSource == "text" {
		sourceGuidance = "【输入来源：键盘文字】这句话是用户直接发给你的消息。请正常理解并回应，或在用户明确要求工作时转交 aide。文字输入不包含背景音，禁止以‘在和别人聊天’、‘背景声’等理由静默；当前没有 be_silent 工具。\n\n"
	}
	if inputSource == "call" {
		sourceGuidance = assistantCallGuidance
	}
	system := voiceIdentityPrompt(cfg) + "\n\n" + assistantAgentPrinciples + "\n\n" + sourceGuidance +
		aideSection +
		fmt.Sprintf("【aide 的长期记忆（只读参考、绝不修改；它不是你自己的记忆）】\n%s\n\n", aideMem) +
		fmt.Sprintf("【你自己的私有长期记忆】%s\n", voiceMemorySummary(mem))

	messages := []Message{{Role: "system", Content: system}}
	messages = append(messages, a.recentAssistantConversation()...)
	messages = append(messages, Message{Role: "user", Content: heard})
	// XiaoMi can read the clock, but must not inherit arbitrary workspace plugin tools.
	avatarEnabled := len(avatarFeedback) > 0 && avatarFeedback[0]
	tools := withAvatarCueTool(a.assistantAgentTools(inputSource == "voice"), avatarEnabled)
	avatarCuesUsed := 0
	params := ProfileParams{MaxTokens: 800, Temperature: fp(0.3)}
	if inputSource == "call" {
		params.MaxTokens = 240
	}

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
		if avatarEnabled {
			var cue *AvatarCue
			out, calls, cue = consumeAvatarCues(out, calls, true, &avatarCuesUsed)
			if cue != nil {
				dec.AvatarCue = cue
			}
			if avatarCuesUsed >= avatarCueBudget {
				tools = withoutAvatarCueTool(tools)
			}
			if strings.TrimSpace(out) == "" && len(calls) == 0 {
				avatarEnabled = false
				tools = withoutAvatarCueTool(tools)
				messages = append(messages, Message{Role: "user", Content: "Please reply normally; avatar feedback is optional and no longer needed."})
				continue
			}
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
			a.mu.Lock()
			scopeErr := a.validateScheduledWorkspaceLocked(ctx)
			a.mu.Unlock()
			if scopeErr != nil {
				return assistantDecision{}, scopeErr
			}
			name := call.Function.Name
			toolsUsed = append(toolsUsed, name)
			var result string
			if name == "manage_plugins" || name == "manage_references" {
				result = a.executeManagementTool(ctx, call, &Task{WorkspaceID: managementWorkspace, AssistantManaged: true}, "assistant")
			} else if name == "skill_library" {
				result = a.executeSkillLibrary(ctx, call, &Task{WorkspaceID: managementWorkspace, AssistantManaged: true}, "assistant")
			} else {
				result = a.execAssistantToolWithContext(ctx, call, &dec, heard)
			}
			messages = append(messages, Message{Role: "tool", ToolCallID: call.ID, Content: result})
		}
		// dispatch 与静默的最终文案由 handler 按确定的结果生成，无需再请求一次
		// 模型来“收尾”。这条路径原本至少多一次完整模型往返，导致明确派活时
		// 发送按钮长时间禁用；其他需要继续查询、追问或陪聊的工具仍可多轮执行。
		if dec.Action == "dispatch" || dec.Action == "silent" {
			break
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
func (a *App) execAssistantTool(call ToolCall, dec *assistantDecision, authorization ...string) string {
	return a.execAssistantToolWithContext(context.Background(), call, dec, authorization...)
}

func (a *App) execAssistantToolWithContext(ctx context.Context, call ToolCall, dec *assistantDecision, authorization ...string) string {
	if a.toolDenied(call.Function.Name) {
		return "工具 " + call.Function.Name + " 已被管理员禁用，请在设置中启用后使用"
	}
	var args map[string]any
	_ = json.Unmarshal([]byte(call.Function.Arguments), &args)
	if args == nil {
		args = map[string]any{}
	}
	str := func(k string) string { v, _ := args[k].(string); return strings.TrimSpace(v) }
	rawStr := func(k string) string { v, _ := args[k].(string); return v }
	if call.Function.Name == "manage_messages" {
		return a.executeMessageAssistant(context.Background(), call)
	}
	switch call.Function.Name {
	case "clear_followups":
		heard := ""
		if len(authorization) > 0 {
			heard = authorization[0]
		}
		return a.clearAssistantFollowups(ctx, args, heard)
	case "browse_plugin_market":
		return a.browsePluginMarket(ctx, call.Function.Arguments)
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
		return fmt.Sprintf("已记录要转达给 aide 的意图：%s（mode=%s，urgent=%v）。宿主确认任务启动后才向用户确认，不要把待执行意图说成已转交或已启动。", summary, dec.Mode, urgent)
	case "be_silent":
		dec.Action = "silent"
		dec.Reason = str("reason")
		dec.Reply = ""
		return "好的，已静默：" + dec.Reason
	case "remember":
		return a.rememberAssistantNote(str("note"))
	case "memory_notes":
		authority := ""
		if len(authorization) > 0 {
			authority = authorization[0]
		}
		return a.assistantMemoryNotes(call.Function.Arguments, authority)
	case "search_project_memory":
		if n := len([]rune(str("keyword"))); n < 2 || n > 200 {
			return "错误：关键词须为2至200字"
		}
		if a.voiceAgent == nil {
			return "项目记忆不可用"
		}
		return a.voiceAgent.readAideMemoryFor(str("keyword"))
	case "search_history":
		return a.searchAssistantHistory(call.Function.Arguments)
	case "work_status":
		a.mu.Lock()
		rows := a.assistantWorkLedgerLocked()
		b, _ := json.Marshal(rows)
		a.mu.Unlock()
		return string(b)
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
		if gerr != nil {
			a.mu.Unlock()
			return gerr.Error()
		}
		recent := append([]Message(nil), gs.Messages...)
		if len(recent) > 10 {
			recent = recent[len(recent)-10:]
		}
		for i := range recent {
			recent[i].Content = boundedEvidenceText(recent[i].Content, 1500)
			recent[i].Images = nil
			recent[i].ToolCalls = nil
			recent[i].ToolCallID = ""
		}
		rb, _ := json.Marshal(map[string]any{
			"number": gs.Number, "id": gs.ID, "title": gs.Title,
			"pinned": gs.Pinned, "archived": gs.Archived, "kind": gs.Kind,
			"followed": gs.FollowedByAssistant, "followNote": gs.FollowNote,
			"recentMessages": recent, "totalMessages": len(gs.Messages), "olderMessagesAvailable": len(gs.Messages) > 10, "updatedAt": gs.Updated,
			"work": a.sessionWorkEventLocked(gs),
		})
		a.mu.Unlock()
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
		if err := a.validateScheduledWorkspaceLocked(ctx); err != nil {
			a.mu.Unlock()
			return err.Error()
		}
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
				Messages: []Message{}, Runs: []*Task{}, PendingPrompt: msg, FollowedByAssistant: true,
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
			target.FollowedByAssistant = true
			target.Updated = time.Now().UTC().Format(time.RFC3339Nano)
			_ = a.save(target)
		}
		number, title, id := target.Number, target.Title, target.ID
		a.mu.Unlock()
		dispatch := map[string]any{"sessionId": id}
		a.startDispatchedAssistantTaskWithContext(ctx, dispatch, msg, false, authorization...)
		if dispatch["started"] != true {
			return fmt.Sprintf("会话 #%d 的任务未启动：%v。", number, dispatch["startError"])
		}
		if created {
			return fmt.Sprintf("已创建并启动会话 #%d（%s）的任务。", number, title)
		}
		return fmt.Sprintf("已向会话 #%d（%s）提交执行，忙碌时会排队。", number, title)
	default:
		if call.Function.Name == "skill_library" {
			return a.executeSkillLibrary(context.Background(), call, nil, "assistant")
		}
		if call.Function.Name == "manage_plugins" || call.Function.Name == "manage_references" {
			return a.executeManagementTool(context.Background(), call, nil, "assistant")
		}
		if strings.HasPrefix(call.Function.Name, "reminder_") {
			return a.dispatchAssistantReminderTool(call)
		}
		if pluginID := a.pluginOwnerOf(call.Function.Name); pluginID == "current-time" || pluginID == "lunar-calendar" {
			result, err := a.callPluginTool(pluginID, call.Function.Name, args)
			if err != nil {
				return "查询插件信息失败: " + err.Error()
			}
			b, err := json.Marshal(result)
			if err != nil {
				return "查询插件信息失败: 无法序列化结果"
			}
			return string(b)
		}
		return "未知工具: " + call.Function.Name
	}
}

// decisionToVoiceEntry 把 agentic 决策映射回既有 VoiceHistoryEntry 契约
// （voice-filter 前端仍按 action=send/ask/standby/ignore 驱动语音面板）。
func decisionToVoiceEntry(d assistantDecision, heard string) VoiceHistoryEntry {
	e := VoiceHistoryEntry{
		Time:      time.Now().Format("2006-01-02 15:04:05"),
		AvatarCue: d.AvatarCue,
		Heard:     heard,
		Reason:    d.Reason,
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
	default: // chat：不转交 aide，但将小秘回复透传给语音面板。
		e.Action = "ignore"
		e.Text = ""
		e.Reply = d.Reply
	}
	return e
}
