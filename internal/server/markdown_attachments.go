package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path"
	"strings"
)

// These are inline-context budgets, not file/upload limits. Large Markdown
// remains on disk and is available through read_file's UTF-8 byte windows.
const markdownInlineBudget = 16 << 10
const markdownReferencePreview = 768

func markdownAttachmentPath(p string) bool {
	switch strings.ToLower(path.Ext(p)) {
	case ".md", ".markdown", ".mdown":
		return true
	}
	return false
}

func (a *App) sourceMarkdownWindow(src Source, p string, off, limit int64) (streamedTextWindow, error) {
	if err := safePath(p); err != nil {
		return streamedTextWindow{}, err
	}
	if src.Type == "local" || src.Type == "skill" {
		root, err := a.localSourceRoot(src)
		if err != nil {
			return streamedTextWindow{}, err
		}
		defer root.Close()
		return readTextWindowWithOpener(p, root.Open, 0, 0, true, off, limit)
	}
	// Other source transports retain their existing transfer/permission budgets.
	b, err := a.readSourceText(src, p)
	if err != nil {
		return streamedTextWindow{}, err
	}
	return streamTextWindow(p, bytes.NewReader(b), 0, 0, true, off, limit)
}

func (a *App) markdownAttachmentWindow(att Attachment) (streamedTextWindow, error) {
	if att.Root == "source" {
		a.mu.Lock()
		src, ok := a.findSource(att.Source)
		a.mu.Unlock()
		if !ok || !src.Enabled {
			return streamedTextWindow{}, fmt.Errorf("来源不存在或已停用")
		}
		return a.sourceMarkdownWindow(src, att.Path, 0, markdownInlineBudget)
	}
	if (att.Root == "workspace" || att.Root == "") && a.workspaceMode() == "ssh" {
		return a.readSFTPTextWindow(att.Path, 0, 0, true, 0, markdownInlineBudget)
	}
	root, err := a.root(att.Root)
	if err != nil {
		return streamedTextWindow{}, err
	}
	return readTextWindowWithOpener(att.Path, root.Open, 0, 0, true, 0, markdownInlineBudget)
}

func markdownReference(att Attachment, window streamedTextWindow) string {
	source := ""
	if att.Root == "source" {
		source = att.Source
	}
	if att.Root == "context" {
		source = contextSource
	}
	end := min(len(window.Content), markdownReferencePreview)
	// Trim only an incomplete UTF-8 rune from the bounded preview, never the file.
	for end > 0 && end < len(window.Content) && window.Content[end]&0xc0 == 0x80 {
		end--
	}
	read := map[string]any{"path": att.Path, "byteOffset": 0, "byteLimit": 8192}
	if source != "" {
		read["source"] = source
	}
	metadata, _ := json.Marshal(map[string]any{
		"mode": "file-reference", "root": att.Root, "source": source, "path": att.Path,
		"bytes": window.Size, "lines": window.TotalLines, "sha256": window.Hash,
		"previewByteStart": 0, "previewByteEnd": end, "read_file": read,
	})
	return fmt.Sprintf("[大 Markdown 附件按文件引用附加；原文件完整保留，未将全文加入上下文。以下仅为开头节选，不代表已读取全文，也不构成完整文件修改基线。请调用 read_file 按字节分段读取相关内容，后续从返回的 byteEnd 继续；需要完整审阅时覆盖所有字节范围后才能声称读完。]\n%s\n<markdown-preview>\n%s\n</markdown-preview>", metadata, window.Content[:end])
}
