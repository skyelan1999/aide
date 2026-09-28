#!/usr/bin/env python3
"""extract_text <path>：把 Office/PDF 文档提取为纯文本，打到 stdout。

供 aide 把 .docx/.pdf/.xlsx/.pptx 作为附件时，将正文随消息发给模型
（不依赖模型视觉能力）。失败时 stderr 报错并 exit 1。

分派：
  .docx  → python-docx（段落 + 表格）
  .xlsx  → openpyxl（按 sheet 逐行，单元格以 tab 分隔）
  .pptx  → python-pptx（逐幻灯片文本框）
  .pdf   → pypdf（逐页 extract_text）

超长截断由 Go 侧统一处理（标注「已截断/共 N 字」），本脚本只负责完整提取。
"""
import os
import sys


def extract_docx(path: str) -> str:
    from docx import Document  # noqa: WPS433（延迟导入，错误信息更清晰）

    d = Document(path)
    parts = []
    for p in d.paragraphs:
        t = (p.text or "").strip()
        if t:
            parts.append(t)
    for ti, table in enumerate(d.tables):
        parts.append(f"[表格 {ti + 1}]")
        for row in table.rows:
            cells = [(c.text or "").strip().replace("\n", " ") for c in row.cells]
            if any(cells):
                parts.append("\t".join(cells))
    return "\n".join(parts)


def extract_xlsx(path: str) -> str:
    from openpyxl import load_workbook  # noqa: WPS433

    wb = load_workbook(path, read_only=True, data_only=True)
    parts = []
    for ws in wb.worksheets:
        parts.append(f"[Sheet: {ws.title}]")
        for row in ws.iter_rows(values_only=True):
            cells = ["" if v is None else str(v).strip() for v in row]
            # 去掉行尾空单元格，保留中间空列的 tab
            while cells and cells[-1] == "":
                cells.pop()
            if any(cells):
                parts.append("\t".join(cells))
    wb.close()
    return "\n".join(parts)


def extract_pptx(path: str) -> str:
    from pptx import Presentation  # noqa: WPS433

    prs = Presentation(path)
    parts = []
    for si, slide in enumerate(prs.slides):
        parts.append(f"[幻灯片 {si + 1}]")
        for shape in slide.shapes:
            if not shape.has_text_frame:
                continue
            for para in shape.text_frame.paragraphs:
                t = "".join(run.text for run in para.runs).strip()
                if t:
                    parts.append(t)
    return "\n".join(parts)


def extract_pdf(path: str) -> str:
    try:
        from pypdf import PdfReader  # noqa: WPS433
    except ImportError:
        print("容器未安装 pypdf，无法提取 PDF 文本（请在 Dockerfile pip 依赖中加入 pypdf）",
              file=sys.stderr)
        return ""  # 触发下方"无文本"错误
    reader = PdfReader(path)
    parts = []
    for i, page in enumerate(reader.pages):
        t = (page.extract_text() or "").strip()
        if t:
            parts.append(f"[第 {i + 1} 页]\n{t}")
    return "\n".join(parts)


DISPATCH = {
    ".docx": extract_docx,
    ".xlsx": extract_xlsx,
    ".pptx": extract_pptx,
    ".pdf": extract_pdf,
}


def main() -> int:
    if len(sys.argv) < 2:
        print("用法: extract_text.py <path>", file=sys.stderr)
        return 1
    path = sys.argv[1]
    ext = os.path.splitext(path)[1].lower()
    fn = DISPATCH.get(ext)
    if fn is None:
        print(f"不支持的文档类型: {ext}", file=sys.stderr)
        return 1
    if not os.path.isfile(path):
        print(f"文件不存在: {path}", file=sys.stderr)
        return 1
    try:
        text = fn(path)
    except Exception as e:  # 提取库任何异常都明确上报，不静默丢内容
        print(f"提取 {ext} 失败: {type(e).__name__}: {e}", file=sys.stderr)
        return 1
    if not text.strip():
        print(f"未能从 {ext} 提取到文本（可能是扫描件/图片型文档）: {path}", file=sys.stderr)
        return 1
    sys.stdout.write(text)
    return 0


if __name__ == "__main__":
    sys.exit(main())
