package server

// ── 附件扩展（#63 扩展：可解析文档 + 图片多模态）─────────────────────────────
//
// 原 attachmentContext 只接受 UTF-8 文本，docx/pdf/xlsx/pptx 一律报"不支持二进制文件"。
// 本扩展：
//   1. 可解析文档（docx/pdf/xlsx/pptx）由后端调 scripts/office/extract_text.py 提取文本，
//      注入 <untrusted-file> 文本上下文——不依赖模型视觉能力，任何模型都能读。
//   2. 图片（png/jpg/jpeg/gif/webp）：检测当前模型视觉能力，支持则按多模态 image_url 附加，
//      不支持则在发送前给出可操作提示。
//   3. 大文件截断标注；提取失败明确报错；确实无法解析的二进制改友好文案。
//
// 后端只提供提取与发送能力；附件 chip 的保留/删除由前端处理。

import (
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"
)

const (
	// maxDocExtractChars 单个文档提取文本上限（按 rune 计），超出截断并标注。
	maxDocExtractChars = 60000
	// maxAttachmentChars 所有附件注入上下文的总字符上限（UTF-8 字节）。
	maxAttachmentChars = 200000
	// maxImageAttachment 单张图片大小上限。
	maxImageAttachment = 5 << 20
)

// documentAttachmentExts 可由后端提取文本的文档扩展名 → 展示名。
var documentAttachmentExts = map[string]string{
	".docx": "Word",
	".pdf":  "PDF",
	".xlsx": "Excel",
	".pptx": "PowerPoint",
}

// imageAttachmentTypes 图片扩展名 → MIME 类型。
var imageAttachmentTypes = map[string]string{
	".png":  "image/png",
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".gif":  "image/gif",
	".webp": "image/webp",
}

// MessageImage 是随 outgoing 用户消息附带的一张图片（多模态）。
// json:"-" 不持久化、不进 UI 历史：图片是工作区文件，每次 run 重新附加。
type MessageImage struct {
	MediaType string
	DataURL   string
}

// ── 视觉能力检测 ──────────────────────────────────────────────────────────────

// knownVisionFragments 模型未显式声明 vision 时，按模型 id 小写子串粗判支持视觉。
// 仅放高置信片段，避免把普通文本模型误判为视觉模型。
var knownVisionFragments = []string{
	"gpt-4o", "gpt-4.1", "gpt-4-turbo", "gpt-5",
	"claude-3", "claude-4",
	"gemini",
	"qwen-vl", "qvq", "-vl", "_vl",
	"glm-4v", "glm-4.5v", "glm-4.6v",
	"deepseek-vl",
	"step-1v", "step-2v",
	"vision",
}

// modelSupportsVision 判断模型是否支持图片输入：先看用户在模型列表里的显式声明，
// 再按已知视觉模型清单兜底。纯函数，不持锁。
func modelSupportsVision(modelID string, models []ModelRef) bool {
	modelID = strings.TrimSpace(modelID)
	if modelID == "" {
		return false
	}
	for _, m := range models {
		if m.ID == modelID && m.Vision {
			return true
		}
	}
	low := strings.ToLower(modelID)
	for _, frag := range knownVisionFragments {
		if strings.Contains(low, frag) {
			return true
		}
	}
	return false
}

// recommendedVisionModels 不支持视觉时给前端/用户的推荐清单。
func recommendedVisionModels() []string {
	return []string{"gpt-4o", "gpt-4.1", "qwen-vl-max", "qwen-vl-plus", "glm-4v", "deepseek-vl", "gemini-2.0-flash", "claude-3.5-sonnet"}
}

// visionGateLocked 校验图片附件与当前模型视觉能力是否匹配。调用方必须持有 a.mu。
func (a *App) visionGateLocked(images []MessageImage) error {
	if len(images) == 0 {
		return nil
	}
	if !modelSupportsVision(a.settings.Model, a.settings.Models) {
		return fmt.Errorf("当前模型 %q 不支持图片输入，请在模型设置中切换到支持视觉的模型（如 %s）",
			a.settings.Model, strings.Join(recommendedVisionModels(), "、"))
	}
	return nil
}

// ── 提取与读取 ────────────────────────────────────────────────────────────────

// extractDocumentText 调 scripts/office/extract_text.py 提取 docx/pdf/xlsx/pptx 文本。
// 仅支持本地工作区（脚本跑在容器内；SSH 远端无文件可读，参考来源库同理）。
func (a *App) extractDocumentText(att Attachment) (string, error) {
	if a.workspaceMode() == "ssh" {
		return "", errors.New("Office/PDF 文档提取暂不支持 SSH 工作区")
	}
	if att.Root == "source" {
		return "", errors.New("参考来源中的 Office/PDF 文档暂不支持文本提取，请改用文本文件")
	}
	if err := safePath(att.Path); err != nil {
		return "", err
	}
	root, err := a.root(att.Root)
	if err != nil {
		return "", err
	}
	st, err := root.Stat(att.Path)
	if err != nil || !st.Mode().IsRegular() {
		return "", fmt.Errorf("文件不存在或不是普通文件: %s", att.Path)
	}
	script, err := officeScriptPath("extract_text.py")
	if err != nil {
		return "", err
	}
	out, err := runOfficeScript(root.Name(), script, att.Path)
	if err != nil {
		return "", fmt.Errorf("提取文档 %s 失败：%w", path.Base(att.Path), err)
	}
	return out, nil
}

// readLocalImage 读取本地工作区图片原始字节并编码为 data URL。
func (a *App) readLocalImage(att Attachment) (MessageImage, error) {
	if a.workspaceMode() == "ssh" || att.Root == "source" {
		return MessageImage{}, errors.New("图片附件暂仅支持本地工作区文件")
	}
	if err := safePath(att.Path); err != nil {
		return MessageImage{}, err
	}
	root, err := a.root(att.Root)
	if err != nil {
		return MessageImage{}, err
	}
	f, err := root.Open(att.Path)
	if err != nil {
		return MessageImage{}, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return MessageImage{}, err
	}
	if !info.Mode().IsRegular() {
		return MessageImage{}, errors.New("只支持普通图片文件")
	}
	b, err := io.ReadAll(io.LimitReader(f, maxImageAttachment+1))
	if err != nil {
		return MessageImage{}, err
	}
	if len(b) > maxImageAttachment {
		return MessageImage{}, fmt.Errorf("图片超过 %d MB 上限", maxImageAttachment>>20)
	}
	mt := imageAttachmentTypes[strings.ToLower(path.Ext(att.Path))]
	return MessageImage{
		MediaType: mt,
		DataURL:  "data:" + mt + ";base64," + base64.StdEncoding.EncodeToString(b),
	}, nil
}

// truncateExtract 按 rune 截断超长提取文本，并标注"已截断/共 N 字"。
func truncateExtract(text string) string {
	r := []rune(text)
	if len(r) <= maxDocExtractChars {
		return text
	}
	return string(r[:maxDocExtractChars]) +
		fmt.Sprintf("\n[…文档过长已截断，共 %d 字，仅展示前 %d 字]", len(r), maxDocExtractChars)
}

// friendlyAttachError 把"不支持二进制文件"等拒绝改写成可操作文案；其余错误原样返回。
func friendlyAttachError(path string, err error) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	if strings.Contains(msg, "不支持二进制文件") || strings.Contains(msg, "只支持普通文本文件") {
		return fmt.Errorf("文件 %s 为 aide 暂不能解析的二进制格式；可让 aide 直接在工作区打开该文件，或另存为文本后附加", path)
	}
	return err
}

// outgoingMessages 把内部 []Message 转成发给 Chat Completions 的消息数组。
// 携带 Images 的用户消息把 content 从字符串改为多模态 parts 数组；其余消息原样透传。
func outgoingMessages(msgs []Message) []any {
	out := make([]any, len(msgs))
	for i := range msgs {
		m := &msgs[i]
		if len(m.Images) == 0 {
			out[i] = *m
			continue
		}
		parts := []any{map[string]any{"type": "text", "text": m.Content}}
		for _, img := range m.Images {
			parts = append(parts, map[string]any{
				"type": "image_url",
				"image_url": map[string]any{"url": img.DataURL},
			})
		}
		mm := map[string]any{"role": m.Role, "content": parts}
		if m.ToolCallID != "" {
			mm["tool_call_id"] = m.ToolCallID
		}
		out[i] = mm
	}
	return out
}
