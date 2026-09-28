package server

// #63 扩展：可解析文档附件提取 + 图片多模态视觉能力 测试。

import (
	"encoding/base64"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// makeFixture 在测试工作区用 python 生成一个文档；失败则 skip（容器缺库）。
func makeFixture(t *testing.T, a *App, name, gen string) {
	t.Helper()
	full := filepath.Join(a.workPath, name)
	cmd := exec.Command("python3", "-c", gen, full)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("跳过：无法生成夹具 %s（容器缺库?）: %v %s", name, err, out)
	}
}

func TestTruncateExtract(t *testing.T) {
	// 短文本原样返回
	if got := truncateExtract("短文本"); got != "短文本" {
		t.Fatalf("短文本不应截断: %q", got)
	}
	// 超长按 rune 截断并标注
	long := strings.Repeat("字", maxDocExtractChars+500)
	got := truncateExtract(long)
	if !strings.Contains(got, "已截断") || !strings.Contains(got, "仅展示前") {
		t.Fatalf("缺少截断标注: %q", got[:120])
	}
	if strings.Count(got, "字") > maxDocExtractChars+20 {
		t.Fatalf("截断后仍过长: %d", len([]rune(got)))
	}
}

func TestModelSupportsVision(t *testing.T) {
	// 显式声明优先
	models := []ModelRef{{ID: "my-text-model", Vision: true}}
	if !modelSupportsVision("my-text-model", models) {
		t.Fatal("显式 Vision=true 应支持")
	}
	// 已知视觉模型清单兜底
	known := []string{"gpt-4o", "gpt-4.1-2025", "qwen-vl-max", "glm-4v", "deepseek-vl", "gemini-2.0-flash", "claude-3.5-sonnet"}
	for _, m := range known {
		if !modelSupportsVision(m, nil) {
			t.Fatalf("已知视觉模型应识别: %s", m)
		}
	}
	// 普通文本模型不支持
	plain := []string{"deepseek-chat", "deepseek-reasoner", "qwen-plus", "gpt-3.5-turbo", "my-custom-llm", ""}
	for _, m := range plain {
		if modelSupportsVision(m, nil) {
			t.Fatalf("普通文本模型不应被判为视觉: %s", m)
		}
	}
}

func TestOutgoingMessagesMultimodal(t *testing.T) {
	imgs := []MessageImage{{MediaType: "image/png", DataURL: "data:image/png;base64," + base64.StdEncoding.EncodeToString([]byte{1, 2, 3})}}
	in := []Message{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "看图", Images: imgs},
		{Role: "assistant", Content: "你好"},
	}
	out := outgoingMessages(in)
	if len(out) != 3 {
		t.Fatalf("消息数异常: %d", len(out))
	}
	// 系统/助手消息原样透传（Content 仍为字符串）
	if out[0].(Message).Content != "sys" {
		t.Fatal("system 消息应原样透传")
	}
	// 用户消息转为 parts 数组
	mm, ok := out[1].(map[string]any)
	if !ok {
		t.Fatal("带图用户消息应为 map")
	}
	parts, ok := mm["content"].([]any)
	if !ok || len(parts) != 2 {
		t.Fatalf("content 应为 2 段 parts: %+v", mm["content"])
	}
	textPart := parts[0].(map[string]any)
	if textPart["type"] != "text" || textPart["text"] != "看图" {
		t.Fatalf("text part 异常: %+v", textPart)
	}
	imgPart := parts[1].(map[string]any)
	if imgPart["type"] != "image_url" {
		t.Fatalf("image part 异常: %+v", imgPart)
	}
}

func TestAttachmentContextDocx(t *testing.T) {
	a := testApp(t)
	t.Setenv("AIDE_OFFICE_SCRIPTS", "/src/scripts/office")
	makeFixture(t, a, "d.docx", `
import sys
from docx import Document
d=Document(); d.add_heading("设计说明",level=1); d.add_paragraph("背景段落唯一标记XYZ")
d.save(sys.argv[1])
`)
	ctx, imgs, _, err := a.attachmentContext([]Attachment{{Root: "workspace", Path: "d.docx"}})
	if err != nil {
		t.Fatalf("docx 提取失败: %v", err)
	}
	if !strings.Contains(ctx, "设计说明") || !strings.Contains(ctx, "背景段落唯一标记XYZ") {
		t.Fatalf("未提取到 docx 正文: %s", ctx)
	}
	if len(imgs) != 0 {
		t.Fatal("docx 不应产生图片")
	}
}

func TestAttachmentContextXlsx(t *testing.T) {
	a := testApp(t)
	t.Setenv("AIDE_OFFICE_SCRIPTS", "/src/scripts/office")
	makeFixture(t, a, "s.xlsx", `
import sys
from openpyxl import Workbook
wb=Workbook(); ws=wb.active; ws.title="Sheet1"; ws["A1"]="姓名"; ws["B1"]="唯一标记张三"; wb.save(sys.argv[1])
`)
	ctx, _, _, err := a.attachmentContext([]Attachment{{Root: "workspace", Path: "s.xlsx"}})
	if err != nil {
		t.Fatalf("xlsx 提取失败: %v", err)
	}
	if !strings.Contains(ctx, "Sheet1") || !strings.Contains(ctx, "唯一标记张三") {
		t.Fatalf("未提取到 xlsx 内容: %s", ctx)
	}
}

func TestAttachmentContextPdf(t *testing.T) {
	a := testApp(t)
	t.Setenv("AIDE_OFFICE_SCRIPTS", "/src/scripts/office")
	// 手写最小单页 PDF（Helvetica + Tj）
	gen := `
import sys
objs=[b"<< /Type /Catalog /Pages 2 0 R >>",
b"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
b"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 300 144] /Contents 4 0 R /Resources << /Font << /F1 5 0 R >> >> >>"]
stream=b"BT /F1 18 Tf 20 100 Td (PDFMARKHELLO) Tj ET"
objs.append(b"<< /Length "+str(len(stream)).encode()+b" >>\nstream\n"+stream+b"\nendstream")
objs.append(b"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>")
out=b"%PDF-1.4\n"; offs=[]
for i,o in enumerate(objs,1):
    offs.append(len(out)); out+=str(i).encode()+b" 0 obj\n"+o+b"\nendobj\n"
xref=len(out); out+=b"xref\n0 "+str(len(objs)+1).encode()+b"\n0000000000 65535 f \n"
for off in offs: out+=("%010d 00000 n \n"%off).encode()
out+=b"trailer\n<< /Size "+str(len(objs)+1).encode()+b" /Root 1 0 R >>\nstartxref\n"+str(xref).encode()+b"\n%%EOF\n"
open(sys.argv[1],"wb").write(out)
`
	makeFixture(t, a, "p.pdf", gen)
	ctx, _, _, err := a.attachmentContext([]Attachment{{Root: "workspace", Path: "p.pdf"}})
	if err != nil {
		t.Skipf("PDF 提取不可用（容器可能未装 pypdf）: %v", err)
	}
	if !strings.Contains(ctx, "PDFMARKHELLO") {
		t.Fatalf("未提取到 PDF 文本: %s", ctx)
	}
}

func TestAttachmentContextUnsupportedBinary(t *testing.T) {
	a := testApp(t)
	// 写一个含 NUL 的二进制文件
	if err := os.WriteFile(filepath.Join(a.workPath, "bad.bin"), []byte{0x00, 0x01, 0x02, 0x00}, 0600); err != nil {
		t.Fatal(err)
	}
	_, _, _, err := a.attachmentContext([]Attachment{{Root: "workspace", Path: "bad.bin"}})
	if err == nil {
		t.Fatal("二进制应报错")
	}
	if strings.Contains(err.Error(), "不支持二进制文件") {
		t.Fatalf("旧的笼统文案应被替换: %v", err)
	}
	if !strings.Contains(err.Error(), "工作区") {
		t.Fatalf("应给可操作文案: %v", err)
	}
}

func TestVisionGateLocked(t *testing.T) {
	a := testApp(t)
	a.mu.Lock()
	a.settings.Model = "deepseek-chat" // 非视觉
	a.mu.Unlock()
	imgs := []MessageImage{{MediaType: "image/png", DataURL: "data:..."}}
	a.mu.Lock()
	err := a.visionGateLocked(imgs)
	a.mu.Unlock()
	if err == nil || !strings.Contains(err.Error(), "不支持图片") {
		t.Fatalf("非视觉模型+图片应报错: %v", err)
	}
	// 切到视觉模型
	a.mu.Lock()
	a.settings.Model = "gpt-4o"
	err = a.visionGateLocked(imgs)
	a.mu.Unlock()
	if err != nil {
		t.Fatalf("视觉模型应放行: %v", err)
	}
	// 无图片直接放行
	a.mu.Lock()
	a.settings.Model = "deepseek-chat"
	err = a.visionGateLocked(nil)
	a.mu.Unlock()
	if err != nil {
		t.Fatalf("无图片应放行: %v", err)
	}
}

func TestReadLocalImageDataURL(t *testing.T) {
	a := testApp(t)
	png := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, 0x00, 0x01, 0x02} // 任意字节当 png
	if err := os.WriteFile(filepath.Join(a.workPath, "pic.png"), png, 0600); err != nil {
		t.Fatal(err)
	}
	img, err := a.readLocalImage(Attachment{Root: "workspace", Path: "pic.png"})
	if err != nil {
		t.Fatalf("读图失败: %v", err)
	}
	if img.MediaType != "image/png" || !strings.HasPrefix(img.DataURL, "data:image/png;base64,") {
		t.Fatalf("data URL 异常: %+v", img)
	}
	// 解码应还原原字节
	raw, _ := base64.StdEncoding.DecodeString(strings.TrimPrefix(img.DataURL, "data:image/png;base64,"))
	if string(raw) != string(png) {
		t.Fatal("base64 往返不一致")
	}
}
