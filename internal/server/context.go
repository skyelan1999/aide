package server

// R08-04 上下文预览：与真实请求共用同一构建器。
// 预览展示组成/估算口径（4 字符 ≈ 1 token，非精确 tokenizer），
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

// ContextBreakdown 上下文组成明细（字符数按 UTF-8 字节）。
type ContextBreakdown struct {
	SystemChars      int `json:"systemChars"`
	SummaryChars     int `json:"summaryChars"`
	HistoryChars     int `json:"historyChars"`
	HistoryMessages  int `json:"historyMessages"`
	PromptChars      int `json:"promptChars"`
	AttachmentChars  int `json:"attachmentChars"`
	AttachmentFiles  int `json:"attachmentFiles"`
	InstructionChars int `json:"instructionChars"`
	ToolSchemaChars  int `json:"toolSchemaChars"`
	ToolCount        int `json:"toolCount"`
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
func (a *App) buildContextPreview(s *Session, prompt, mode, contextText string, images []MessageImage, cfg Settings, params ProfileParams, includeBody bool) *ContextPreview {
	// 按会话 Kind 选基础 system 设定（小秘系统会话恒为小秘人格；普通会话跟随全局活动人格）
	history := []Message{{Role: "system", Content: a.systemPromptForSession(s) + "\n" + a.cwdPromptLineLocked() + "\n可用工具: " + a.toolListHint()}}
	if guide := a.environmentGuide(); guide != "" {
		history[0].Content += "\n" + guide
	}
	// 注入持久记忆
	if mem := a.readMemory(); mem != "" && !strings.HasPrefix(mem, "(记忆文件为空") {
		history[0].Content += "\n\n## 持久记忆\n以下是你之前记下的用户偏好和项目约定，请在回答中参考：\n" + mem
	}
	// 注入 aide 性格（仅影响对话风格；可演化、只作用于基本聊天）
	if pa := a.personalityLocked(personaAide); pa.Enabled && strings.TrimSpace(pa.Prompt) != "" {
		history[0].Content += "\n\n## 你的性格（仅影响对话风格）\n" + strings.TrimSpace(pa.Prompt)
	}
	var bd ContextBreakdown
	bd.SystemChars = len(history[0].Content)
	if s != nil && s.Compact != "" {
		history = append(history, Message{Role: "system", Content: "历史摘要（已压缩 " + fmt.Sprint(s.CompactedMessages) + " 条消息）:\n" + s.Compact})
		bd.SummaryChars = len(history[1].Content)
	}
	historyCount := 0
	if s != nil {
		start, total := len(s.Messages), 0
		for start > 0 && total+len(s.Messages[start-1].Content) < 60000 {
			start--
			total += len(s.Messages[start].Content)
		}
		history = append(history, s.Messages[start:]...)
		historyCount = len(s.Messages) - start
	}
	historyStart := len(history) // 回放段起点（含摘要之后的第一条历史）
	history = append(history, Message{Role: "user", Content: prompt + contextText, Images: images})
	instruction := chatInstruction
	if mode == "workflow" {
		instruction = planInstruction
	}
	first := append(append([]Message{}, history...), Message{Role: "user", Content: instruction})
	tools := a.contextToolsFor(s)

	bd.HistoryChars = 0
	for i := historyStart; i < len(history)-1; i++ {
		bd.HistoryChars += len(history[i].Content)
	}
	bd.HistoryMessages = historyCount
	bd.PromptChars = len(prompt)
	bd.AttachmentChars = len(contextText)
	if contextText != "" {
		bd.AttachmentFiles = strings.Count(contextText, "<untrusted-file")
	}
	bd.InstructionChars = len(instruction)
	tb, _ := json.Marshal(tools)
	bd.ToolSchemaChars = len(tb)
	bd.ToolCount = len(tools)

	inputChars := bd.SystemChars + bd.SummaryChars + bd.HistoryChars + bd.PromptChars + bd.AttachmentChars + bd.InstructionChars + bd.ToolSchemaChars
	inputEstimate := inputChars / 4
	outputReserve := params.MaxTokens
	window := a.modelWindow(cfg.Model)
	total := inputEstimate + outputReserve

	fp := sha256.New()
	fp.Write([]byte(history[0].Content))
	fp.Write([]byte(cfg.Model))
	fp.Write([]byte(prompt))
	fp.Write([]byte(mode))
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
		EstimationNote: "输入用量为估算：4 字符 ≈ 1 token（按 UTF-8 字节），非精确 tokenizer；输出预留为 max_tokens 采样参数。",
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

// contextPreviewHandler POST /api/context-preview：按草稿构造预览（不产生副作用）。
func (a *App) contextPreviewHandler(w http.ResponseWriter, r *http.Request) {
	var in struct {
		SessionID   string       `json:"sessionId"`
		Prompt      string       `json:"prompt"`
		Mode        string       `json:"mode"`
		Attachments []Attachment `json:"attachments"`
	}
	if err := decode(w, r, &in); err != nil {
		fail(w, 400, err)
		return
	}
	in.Prompt = strings.TrimSpace(in.Prompt)
	if in.Prompt == "" || len(in.Prompt) > 20000 {
		fail(w, 400, errors.New("请输入 1–20000 字节的任务"))
		return
	}
	if in.Mode != "chat" && in.Mode != "workflow" {
		fail(w, 400, errors.New("未知工作模式"))
		return
	}
	if len(in.Attachments) > 8 {
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
	profileID, params, err := a.resolveProfile("manual", "", in.Prompt, in.Mode)
	if err != nil {
		fail(w, 400, err)
		return
	}
	_ = profileID
	preview := a.buildContextPreview(s, in.Prompt, in.Mode, contextText, images, a.settings, params, true)
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
