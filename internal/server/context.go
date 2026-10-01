package server

// R08-04 上下文预览：与真实请求共用同一构建器。
// 预览展示组成/估算口径（对齐 DSH 的 4 UTF-16 字符 ≈ 1 token 启发式，非精确 tokenizer），
// 并在发送前可解释地拦截超限请求（Provider 日志不得出现被拦截的主调用）。

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path"
	"strings"
	"time"
)

// 各阶段指令常量：预览与 execute 共用，避免两处文案漂移。
const (
	chatInstruction = "请直接回答用户的问题，并明确未验证的内容。需要查看文件时使用工具。"
	planInstruction = "请针对用户任务制定简短的实施计划。可用工具查看工作目录与文件，列出步骤、需要修改的路径和验证命令；缺失信息明确说明。此阶段不执行任何破坏性操作。"
)

// ContextComponent 是一个可解释的上下文组成部分。Bytes 是实际会发给
// provider 的文本 UTF-8 字节数；Characters 是 DSH 口径的 UTF-16 code units；
// Tokens 是估算值，不是 provider 的账单 usage。协议开销和图片没有可比较的
// 文本，因此 Bytes/Characters 可以为 0 而 Tokens 非 0。
type ContextComponent struct {
	Bytes      int   `json:"bytes"`
	Characters int   `json:"characters"` // DSH 口径：UTF-16 code units
	Tokens     int   `json:"tokens"`
	tokenParts []int // 保留每个文本块/字段的独立取整边界
}

// ContextUsageAnchor 保存一次真实上游调用及其 DSH 估算锚点，用来校准
// 后续相同请求头下的会话上下文压力。
type ContextUsageAnchor struct {
	Usage              TokenUsage `json:"usage"`
	PromptEstimate     int        `json:"promptEstimate"`
	CompletionEstimate int        `json:"completionEstimate"`
	HeaderFingerprint  string     `json:"headerFingerprint"`
}

func utf16CodeUnits(s string) int {
	n := 0
	for _, r := range s {
		if r > 0xffff {
			n += 2
		} else {
			n++
		}
	}
	return n
}

func (c *ContextComponent) addText(s string) {
	c.Bytes += len(s)
	units := utf16CodeUnits(s)
	c.Characters += units
	c.tokenParts = append(c.tokenParts, units)
}

// ContextBreakdown 上下文组成明细。各 Component.Tokens 的总和严格等于
// ContextPreview.InputEstimate，避免前端分项和标题出现不同口径。
type ContextBreakdown struct {
	System           ContextComponent `json:"system"`
	Summary          ContextComponent `json:"summary"`
	HistoryUser      ContextComponent `json:"historyUser"`
	HistoryAssistant ContextComponent `json:"historyAssistant"`
	ToolCalls        ContextComponent `json:"toolCalls"`
	ToolResults      ContextComponent `json:"toolResults"`
	Prompt           ContextComponent `json:"prompt"`
	Attachments      ContextComponent `json:"attachments"`
	Instruction      ContextComponent `json:"instruction"`
	ToolSchemas      ContextComponent `json:"toolSchemas"`
	Images           ContextComponent `json:"images"`
	Protocol         ContextComponent `json:"protocol"`
	UsageAdjustment  ContextComponent `json:"usageAdjustment"`

	HistoryMessages  int `json:"historyMessages"`
	AttachmentFiles  int `json:"attachmentFiles"`
	ImageFiles       int `json:"imageFiles"`
	ToolCount        int `json:"toolCount"`
	ToolCallCount    int `json:"toolCallCount"`
	MessageCount     int `json:"messageCount"`
	RoleFrames       int `json:"roleFrames"`
	ContentBlocks    int `json:"contentBlocks"`
	UsageCalibration int `json:"usageCalibration,omitempty"`

	// 兼容 R08-04 API 客户端；新界面使用上面的 disjoint components。
	// Deprecated: use Component.Bytes/Component.Tokens instead.
	SystemChars      int `json:"systemChars,omitempty"`
	SummaryChars     int `json:"summaryChars,omitempty"`
	HistoryChars     int `json:"historyChars,omitempty"`
	PromptChars      int `json:"promptChars,omitempty"`
	AttachmentChars  int `json:"attachmentChars,omitempty"`
	InstructionChars int `json:"instructionChars,omitempty"`
	ToolSchemaChars  int `json:"toolSchemaChars,omitempty"`
}

const (
	contextCharsPerToken        = 4
	contextMessageOverhead      = 4
	contextContentBlockOverhead = 4
	contextImageUpperBoundCost  = 384
	contextHistoryMaxTokens     = 15000
)

func estimateContextTextTokens(characters int) int {
	if characters <= 0 {
		return 0
	}
	return (characters + contextCharsPerToken - 1) / contextCharsPerToken
}

func (c *ContextComponent) estimatedTextTokens() int {
	total := 0
	for _, units := range c.tokenParts {
		total += estimateContextTextTokens(units)
	}
	return total
}

func (b *ContextBreakdown) components() []*ContextComponent {
	return []*ContextComponent{
		&b.System, &b.Summary, &b.HistoryUser, &b.HistoryAssistant,
		&b.ToolCalls, &b.ToolResults, &b.Prompt, &b.Attachments,
		&b.Instruction, &b.ToolSchemas, &b.Images, &b.Protocol, &b.UsageAdjustment,
	}
}

// price 按 DSH token-meter 的固定密度规则重新计价：UTF-16 字符数/4，
// 每个消息角色和内容块各加 4；工具 schema 加 4。图片继续使用本地保守上界。
func (b *ContextBreakdown) price() int {
	for _, component := range b.components() {
		component.Tokens = component.estimatedTextTokens()
	}
	b.Images.Tokens = b.ImageFiles * contextImageUpperBoundCost
	if b.ToolCount > 0 {
		b.ToolSchemas.Tokens += contextContentBlockOverhead
	}
	b.Protocol.Tokens = b.RoleFrames*contextMessageOverhead + b.ContentBlocks*contextContentBlockOverhead
	b.UsageAdjustment.Tokens = b.UsageCalibration
	total := 0
	for _, component := range b.components() {
		total += component.Tokens
	}
	return total
}

// ContextPreview 一次任务首轮请求的构建预览（R08-04）。
type ContextPreview struct {
	Model          string           `json:"model"`
	ContextWindow  int              `json:"contextWindow"`
	OutputReserve  int              `json:"outputReserve"`
	InputEstimate  int              `json:"inputEstimate"`
	TotalEstimate  int              `json:"totalEstimate"`
	OverLimit      bool             `json:"overLimit"`
	EstimationNote string           `json:"estimationNote"`
	Instruction    string           `json:"instruction"`
	Breakdown      ContextBreakdown `json:"breakdown"`
	Messages       []Message        `json:"messages,omitempty"`
	Tools          []any            `json:"tools,omitempty"`
	Fingerprint    string           `json:"fingerprint"`
	WorkspaceID    string           `json:"workspaceId"`
	SessionID      string           `json:"sessionId,omitempty"`
	GeneratedAt    string           `json:"generatedAt"`
}

// RequestSnapshot 实际发出的 Provider 请求快照（含工具续跑轮次）。
type RequestSnapshot struct {
	Purpose   string          `json:"purpose"`
	Model     string          `json:"model"`
	MaxTokens int             `json:"maxTokens,omitempty"`
	Messages  []Message       `json:"messages"`
	Tools     []any           `json:"tools,omitempty"`
	Body      json.RawMessage `json:"body"`
	SHA256    string          `json:"sha256"`
	At        string          `json:"at"`
}

// contextTools 任务可用工具（与 toolLoop 使用的完全一致）；按设置过滤被禁用的工具。
// 调用方必须持有 a.mu（startTask/contextPreviewHandler 均在持锁状态调用；step 函数调用处自行加锁）。
func (a *App) contextTools() []any {
	disabled := make(map[string]bool, len(a.settings.DisabledTools))
	for _, dt := range a.settings.DisabledTools {
		disabled[dt] = true
	}
	tools := make([]any, 0, len(builtinTools))
	for _, t := range builtinTools {
		if fn, ok := t.(map[string]any)["function"].(map[string]any); ok {
			if name, _ := fn["name"].(string); disabled[name] {
				continue
			}
		}
		tools = append(tools, t)
	}
	tools = append(tools, a.pluginToolSchemas()...)
	return tools
}

// modelWindow 返回活跃模型的上下文窗口（未配置/缺省 → defaultContextWindow）。
func (a *App) modelWindow(modelID string) int {
	for _, m := range a.settings.Models {
		if m.ID == modelID && m.ContextWindow > 0 {
			return m.ContextWindow
		}
	}
	return defaultContextWindow
}

// attachmentContext 读取任务附件（与 startTask 实际使用同一条路径）。
// 返回注入上下文的文本、需随消息发送的多模态图片、workspace 版本快照。
// 可解析文档（docx/pdf/xlsx/pptx）由后端提取文本，不依赖模型视觉；
// 图片作为多模态附件返回，视觉能力门禁由调用方持锁后调 visionGateLocked 完成
// （不在此加锁，避免与已持锁的 retryTask 死锁）。
func (a *App) attachmentContext(atts []Attachment) (string, []MessageImage, map[string]Change, error) {
	contextText := ""
	var images []MessageImage
	versions := map[string]Change{}
	addText := func(root, apath, body string) error {
		contextText += fmt.Sprintf("\n<untrusted-file root=%q path=%q>\n%s\n</untrusted-file>\n", root, apath, body)
		if len(contextText) > maxAttachmentChars {
			return fmt.Errorf("附件总量超过 %d KB，请减少附件或缩短文件", maxAttachmentChars>>10)
		}
		return nil
	}
	for _, att := range atts {
		ext := strings.ToLower(path.Ext(att.Path))

		// 1) 图片 → 多模态（是否可发送由调用方 visionGateLocked 判定）
		if _, isImg := imageAttachmentTypes[ext]; isImg {
			img, err := a.readLocalImage(att)
			if err != nil {
				return "", nil, nil, err
			}
			images = append(images, img)
			if err := addText(att.Root, att.Path, "[图片附件 "+path.Base(att.Path)+"，已作为视觉输入随消息发送]"); err != nil {
				return "", nil, nil, err
			}
			continue
		}

		// 2) 可解析文档 → 后端提取文本，不依赖模型视觉
		if _, isDoc := documentAttachmentExts[ext]; isDoc {
			text, err := a.extractDocumentText(att)
			if err != nil {
				return "", nil, nil, err
			}
			text = truncateExtract(text)
			if err := addText(att.Root, att.Path, text); err != nil {
				return "", nil, nil, err
			}
			continue
		}

		// 3) 普通文本文件（原逻辑）
		var b []byte
		var err error
		if att.Root == "source" {
			a.mu.Lock()
			src, ok := a.findSource(att.Source)
			a.mu.Unlock()
			if !ok || !src.Enabled {
				return "", nil, nil, errors.New("来源不存在或已停用")
			}
			b, err = a.readSourceText(src, att.Path)
		} else if (att.Root == "workspace" || att.Root == "") && a.workspaceMode() == "ssh" {
			b, err = a.readWorkspaceText(att.Path)
		} else {
			var r *os.Root
			r, err = a.root(att.Root)
			if err == nil {
				b, err = readText(r, att.Path)
			}
		}
		if err != nil {
			return "", nil, nil, friendlyAttachError(att.Path, err)
		}
		if err := addText(att.Root, att.Path, string(b)); err != nil {
			return "", nil, nil, err
		}
		if att.Root == "workspace" || att.Root == "" {
			versions[path.Clean(att.Path)] = Change{BaseHash: hash(b), Before: string(b)}
		}
	}
	return contextText, images, versions, nil
}

// systemPromptForSession 按会话 Kind 选 system 设定：
// 小秘系统会话(Kind=assistant)永远用小秘人格（不随全局 ActivePersona 漂移）；
// 普通会话用全局活动人格(aide 工作 / 小秘 生活)。调用方持 a.mu。
func (a *App) systemPromptForSession(s *Session) string {
	if s != nil && s.Kind == assistantSessionKind {
		p := voiceIdentityPrompt(a.settings) + "\n\n" + fmt.Sprintf(xiaomiMainPrompt, a.personaDisplayName(personaXiaomi))
		if a.voiceAgent != nil {
			p += "\n\n【aide 的长期记忆（你只读参考、绝不修改；它是 aide 记下的用户偏好/项目约定，不是你自己的记忆）】\n" + a.voiceAgent.readAideMemory()
		}
		return p
	}
	return a.baseSystemPrompt()
}

// buildContextPreview 构造与真实首轮请求一致的消息/工具并给出预算估算。
// s 为 nil 时表示新会话（无历史与摘要）。调用方需持有 a.mu。
func (a *App) buildContextPreview(s *Session, prompt, mode, contextText string, images []MessageImage, cfg Settings, params ProfileParams, includeBody bool, avatarFeedback ...bool) *ContextPreview {
	// 按会话 Kind 选基础 system 设定（小秘系统会话恒为小秘人格；普通会话跟随全局活动人格）
	history := []Message{{Role: "system", Content: a.systemPromptForSession(s) + "\n" + a.cwdPromptLineLocked() + "\n可用工具: " + a.toolListHint()}}
	if guide := a.environmentGuide(); guide != "" {
		history[0].Content += "\n" + guide
	}
	// 注入持久记忆
	if mem := a.readCachedProjectMemory(); mem != "" && !strings.HasPrefix(mem, "(记忆文件为空") {
		history[0].Content += "\n\n## 持久记忆\n以下是你之前记下的用户偏好和项目约定，请在回答中参考：\n" + mem
	}
	// 注入 aide 性格（仅影响对话风格；可演化、只作用于基本聊天）
	if pa := a.personalityLocked(personaAide); pa.Enabled && strings.TrimSpace(pa.Prompt) != "" {
		history[0].Content += "\n\n## 你的性格（仅影响对话风格）\n" + strings.TrimSpace(pa.Prompt)
	}
	// MM-05：注入已启用插件的能力域感知与使用经验（小秘会话不直接持有插件工具，跳过）
	if s == nil || s.Kind != assistantSessionKind {
		if capHint := a.pluginCapabilityHint(); capHint != "" {
			history[0].Content += "\n\n" + capHint
		}
		if expHint := a.pluginExperienceHint(); expHint != "" {
			history[0].Content += "\n\n" + expHint
		}
	}
	var bd ContextBreakdown
	bd.System.addText(history[0].Content)
	if history[0].Content != "" {
		bd.RoleFrames++
	}
	bd.SystemChars = bd.System.Bytes
	if s != nil && s.Compact != "" {
		history = append(history, Message{Role: "system", Content: "历史摘要（已压缩 " + fmt.Sprint(s.CompactedMessages) + " 条消息）:\n" + s.Compact})
		bd.Summary.addText(history[1].Content)
		if history[1].Content != "" {
			bd.RoleFrames++
		}
		bd.SummaryChars = bd.Summary.Bytes
	}
	// 摘要之后才是持久化会话回放；它们要按角色拆分，而不是混成“历史正文”。
	historyStart := len(history)
	historyCount := 0
	if s != nil {
		start, total := len(s.Messages), 0
		for start > 0 && total+contextMessageTokens(s.Messages[start-1]) < contextHistoryMaxTokens {
			start--
			total += contextMessageTokens(s.Messages[start])
		}
		history = append(history, s.Messages[start:]...)
		historyCount = len(s.Messages) - start
	}
	history = append(history, Message{Role: "user", Content: prompt + contextText, Images: images})
	instruction := chatInstruction
	if mode == "workflow" {
		instruction = planInstruction
	}
	first := append(append([]Message{}, history...), Message{Role: "user", Content: instruction})
	avatarEnabled := len(avatarFeedback) > 0 && avatarFeedback[0] && avatarCueFormatAllowed(params)
	tools := withAvatarCueTool(a.contextToolsFor(s), avatarEnabled)

	for i := historyStart; i < len(history)-1; i++ {
		addHistoryComponent(&bd, history[i])
	}
	bd.HistoryMessages = historyCount
	bd.Prompt.addText(prompt)
	bd.PromptChars = bd.Prompt.Bytes
	bd.Attachments.addText(contextText)
	bd.AttachmentChars = bd.Attachments.Bytes
	if prompt != "" || contextText != "" {
		bd.ContentBlocks++ // 新用户消息的正文与附件文本合为一个内容块
	}
	bd.RoleFrames++ // 即使 baseline 正文为空，请求仍会包含该 user 消息
	if contextText != "" {
		bd.AttachmentFiles = strings.Count(contextText, "<untrusted-file")
	}
	bd.ImageFiles = len(images)
	bd.ContentBlocks += len(images)
	bd.Instruction.addText(instruction)
	if instruction != "" {
		bd.ContentBlocks++
	}
	bd.RoleFrames++
	bd.InstructionChars = bd.Instruction.Bytes
	tb, _ := json.Marshal(tools)
	if len(tools) > 0 {
		bd.ToolSchemas.addText(string(tb))
	}
	bd.ToolSchemaChars = bd.ToolSchemas.Bytes
	bd.ToolCount = len(tools)
	bd.MessageCount = len(first)
	bd.HistoryChars = bd.HistoryUser.Bytes + bd.HistoryAssistant.Bytes + bd.ToolCalls.Bytes + bd.ToolResults.Bytes
	if s != nil && len(history) > 0 {
		fingerprint := contextHeaderFingerprint(cfg, history[0].Content, tools)
		for i := len(s.Runs) - 1; i >= 0; i-- {
			anchor := s.Runs[i].ContextAnchor
			if anchor == nil || anchor.Usage.Estimated || anchor.Usage.Prompt <= 0 || anchor.Usage.Completion < 0 {
				continue
			}
			if anchor.HeaderFingerprint != fingerprint {
				break // header 已换到其他模型/系统提示/工具集合，不复用旧 usage
			}
			estimatedAnchor := anchor.PromptEstimate + anchor.CompletionEstimate
			actualAnchor := anchor.Usage.Prompt + anchor.Usage.Completion
			if actualAnchor >= estimatedAnchor {
				bd.UsageCalibration = actualAnchor - estimatedAnchor
			}
			break
		}
	}
	inputEstimate := bd.price()
	outputReserve := params.MaxTokens
	window := a.modelWindow(cfg.Model)
	total := inputEstimate + outputReserve

	fp := sha256.New()
	fp.Write([]byte(history[0].Content))
	fp.Write([]byte(cfg.Model))
	fp.Write([]byte(prompt))
	fp.Write([]byte(mode))
	if avatarEnabled {
		fp.Write([]byte("avatar-feedback"))
	}
	fp.Write([]byte(contextText))
	fp.Write([]byte(a.wsID()))
	if s != nil {
		fp.Write([]byte(s.ID))
		fp.Write([]byte(s.Compact))
		fmt.Fprintf(fp, "%d", s.CompactedMessages)
		fmt.Fprintf(fp, "%d", len(s.Messages))
		if len(s.Messages) > 0 {
			fp.Write([]byte(s.Messages[len(s.Messages)-1].Content))
		}
	}
	paramsJSON, _ := json.Marshal(params)
	fp.Write(paramsJSON)

	preview := &ContextPreview{
		Model:          cfg.Model,
		ContextWindow:  window,
		OutputReserve:  outputReserve,
		InputEstimate:  inputEstimate,
		TotalEstimate:  total,
		OverLimit:      window > 0 && total > window,
		EstimationNote: "组成来自即将发送的首轮消息与工具定义，估算口径对齐 DeepSeek Harness：按 UTF-16 字符数每 4 个约 1 token，并按消息/内容块计结构开销；图片按每张 384 token 的本地保守上界估算。若最近一次模型调用有真实 usage 且模型/系统提示/工具定义相同，会用其校准当前估算；其余情况仍是估算，不是 tokenizer 实测。最近任务累计 usage 另行显示。",
		Instruction:    instruction,
		Breakdown:      bd,
		Fingerprint:    hex.EncodeToString(fp.Sum(nil)),
		WorkspaceID:    a.wsID(),
		GeneratedAt:    time.Now().UTC().Format(time.RFC3339Nano),
	}
	if s != nil {
		preview.SessionID = s.ID
	}
	if includeBody {
		preview.Messages = first
		preview.Tools = tools
	}
	return preview
}

// contextMessageTokens 用与预览相同的 DSH 启发式估算一条回放消息。
func contextMessageTokens(m Message) int {
	tokens := 0
	if m.Role == "system" {
		if m.Content != "" {
			tokens = contextMessageOverhead + estimateContextTextTokens(utf16CodeUnits(m.Content))
		}
	} else {
		tokens = contextMessageOverhead
		if m.Content != "" {
			tokens += estimateContextTextTokens(utf16CodeUnits(m.Content)) + contextContentBlockOverhead
		}
	}
	for _, call := range m.ToolCalls {
		if tokens == 0 {
			tokens = contextMessageOverhead
		}
		tokens += estimateContextTextTokens(utf16CodeUnits(call.Function.Name))
		tokens += estimateContextTextTokens(utf16CodeUnits(call.Function.Arguments))
		tokens += contextContentBlockOverhead
	}
	if len(m.Images) > 0 && tokens == 0 {
		tokens = contextMessageOverhead
	}
	tokens += len(m.Images) * (contextImageUpperBoundCost + contextContentBlockOverhead)
	return tokens
}

func addHistoryComponent(b *ContextBreakdown, m Message) {
	if m.Role != "system" || m.Content != "" || len(m.ToolCalls) > 0 || len(m.Images) > 0 {
		b.RoleFrames++
	}
	switch m.Role {
	case "tool":
		if m.Content != "" {
			b.ToolResults.addText(m.Content)
			b.ContentBlocks++
		}
	case "assistant":
		if m.Content != "" {
			b.HistoryAssistant.addText(m.Content)
			b.ContentBlocks++
		}
		for _, call := range m.ToolCalls {
			b.ToolCalls.addText(call.Function.Name)
			b.ToolCalls.addText(call.Function.Arguments)
			b.ContentBlocks++
		}
		b.ToolCallCount += len(m.ToolCalls)
	case "user":
		if m.Content != "" {
			b.HistoryUser.addText(m.Content)
			b.ContentBlocks++
		}
	default:
		// 历史中的 system/developer 消息不能静默消失；与首条系统提示同类展示。
		if m.Content != "" {
			b.System.addText(m.Content)
		}
	}
	b.ImageFiles += len(m.Images)
	b.ContentBlocks += len(m.Images)
	if m.Role != "assistant" {
		for _, call := range m.ToolCalls {
			b.ToolCalls.addText(call.Function.Name)
			b.ToolCalls.addText(call.Function.Arguments)
			b.ToolCallCount++
			b.ContentBlocks++
		}
	}
}

// estimateProviderPromptTokens 为上游未返回 usage 的回退路径复用同一计量
// 规则。这里不把消息称为“历史”，但按 role 分类可确保工具 schema、调用参数
// 与工具结果都不会在本地估算里漏算。
func estimateProviderPromptTokens(messages []Message, tools []any) int {
	var breakdown ContextBreakdown
	for _, message := range messages {
		addHistoryComponent(&breakdown, message)
	}
	if len(tools) > 0 {
		toolBytes, _ := json.Marshal(tools)
		breakdown.ToolSchemas.addText(string(toolBytes))
	}
	breakdown.ToolCount = len(tools)
	breakdown.MessageCount = len(messages)
	return breakdown.price()
}

func contextHeaderFingerprint(cfg Settings, system string, tools []any) string {
	h := sha256.New()
	h.Write([]byte(cfg.BaseURL))
	h.Write([]byte{0})
	h.Write([]byte(cfg.Model))
	h.Write([]byte{0})
	h.Write([]byte(system))
	b, _ := json.Marshal(tools)
	h.Write([]byte{0})
	h.Write(b)
	return hex.EncodeToString(h.Sum(nil))
}

// applyWorkflowContext 把工作流阶段提示纳入首条 system 消息，并立即重算
// 组成预算。它必须在超限检查前调用，保证预览、拦截与真实请求同口径。
// 调用方已持有 a.mu。
func (a *App) applyWorkflowContext(preview *ContextPreview, mode, phase string) {
	if preview == nil || mode != "workflow" {
		return
	}
	addition := ""
	switch phase {
	case "requirement":
		addition = requirementPhasePrompt
	case "design":
		addition = designPhasePrompt
	case "implementation":
		addition = implementationPhasePrompt
	case "verify":
		addition = verifyPhasePrompt
	case "problem-solving":
		addition = problemSolvingPhasePrompt
	case "", "auto":
		addition = autoModePrompt + a.profileInventoryPrompt()
	}
	if addition == "" {
		return
	}
	if len(preview.Messages) > 0 {
		preview.Messages[0].Content += addition
	}
	preview.Breakdown.System.addText(addition)
	preview.Breakdown.SystemChars = preview.Breakdown.System.Bytes
	preview.InputEstimate = preview.Breakdown.price()
	preview.TotalEstimate = preview.InputEstimate + preview.OutputReserve
	preview.OverLimit = preview.ContextWindow > 0 && preview.TotalEstimate > preview.ContextWindow
	// 指纹用于草稿失效判断；阶段切换即使用户正文不变也必须失效。
	h := sha256.New()
	h.Write([]byte(preview.Fingerprint))
	h.Write([]byte(addition))
	preview.Fingerprint = hex.EncodeToString(h.Sum(nil))
}

func validWorkflowPhase(phase string) bool {
	switch phase {
	case "", "auto", "requirement", "design", "implementation", "verify", "problem-solving":
		return true
	}
	return false
}

// contextPreviewHandler POST /api/context-preview：按草稿构造预览（不产生副作用）。
func (a *App) contextPreviewHandler(w http.ResponseWriter, r *http.Request) {
	var in struct {
		SessionID      string       `json:"sessionId"`
		Prompt         string       `json:"prompt"`
		Mode           string       `json:"mode"`
		Strategy       string       `json:"strategy,omitempty"`
		Profile        string       `json:"profile,omitempty"`
		WorkflowPhase  string       `json:"workflowPhase,omitempty"`
		Baseline       bool         `json:"baseline,omitempty"`
		AvatarFeedback bool         `json:"avatarFeedback,omitempty"`
		Attachments    []Attachment `json:"attachments"`
	}
	if err := decode(w, r, &in); err != nil {
		fail(w, 400, err)
		return
	}
	in.Prompt = strings.TrimSpace(in.Prompt)
	if (in.Prompt == "" && !in.Baseline) || len(in.Prompt) > 20000 {
		fail(w, 400, errors.New("请输入 1–20000 字节的任务"))
		return
	}
	if in.Mode != "chat" && in.Mode != "workflow" {
		fail(w, 400, errors.New("未知工作模式"))
		return
	}
	if in.Mode == "workflow" && !validWorkflowPhase(in.WorkflowPhase) {
		fail(w, 400, errors.New("未知工作流阶段"))
		return
	}
	if len(in.Attachments) > 8 || (in.Baseline && len(in.Attachments) != 0) {
		fail(w, 400, errors.New("最多附加 8 个文件"))
		return
	}
	contextText, images, _, err := a.attachmentContext(in.Attachments)
	if err != nil {
		fail(w, 400, err)
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.settings.Model == "" {
		fail(w, 400, errors.New("请先打开模型设置，配置 API 和模型"))
		return
	}
	if err := a.visionGateLocked(images); err != nil {
		fail(w, 400, err)
		return
	}
	var s *Session
	if in.SessionID != "" {
		s = a.sessions[in.SessionID]
		if s == nil {
			fail(w, 404, errors.New("会话不存在"))
			return
		}
	}
	strategy := in.Strategy
	if strategy == "" {
		strategy = "manual"
	}
	if strategy != "manual" && strategy != "auto" {
		fail(w, 400, errors.New("策略只支持 manual 或 auto"))
		return
	}
	profileID, params, err := a.resolveProfile(strategy, in.Profile, in.Prompt, in.Mode)
	if err != nil {
		fail(w, 400, err)
		return
	}
	_ = profileID
	preview := a.buildContextPreview(s, in.Prompt, in.Mode, contextText, images, a.settings, params, !in.Baseline, in.AvatarFeedback)
	a.applyWorkflowContext(preview, in.Mode, in.WorkflowPhase)
	jsonOut(w, 200, preview)
}

// runRequestsHandler GET /api/sessions/{id}/runs/{rid}/requests：实际请求快照（证据）。
func (a *App) runRequestsHandler(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	defer a.mu.Unlock()
	s := a.sessions[r.PathValue("id")]
	if s == nil {
		fail(w, 404, errors.New("会话不存在"))
		return
	}
	for _, t := range s.Runs {
		if t.ID == r.PathValue("run") {
			jsonOut(w, 200, map[string]any{
				"run":       t.ID,
				"snapshots": t.RequestSnapshots,
				"truncated": t.SnapshotsTruncated,
				"model":     t.Model,
			})
			return
		}
	}
	fail(w, 404, errors.New("任务不存在"))
}
