#!/usr/bin/env python3
"""docx_add_comment <path> <quote> <text> [author] [anchorIndex]：
给 .docx 添加原生批注。锚点策略：找到第 anchorIndex 个包含 quote 的段落，
把批注挂到该段落的全部 runs 上（python-docx 1.2.0 支持 add_comment）。
输出 {"ok": true, "id": <新批注 id>}。
"""
import json
import os
import sys


def main() -> int:
    if len(sys.argv) < 4:
        print("用法: docx_add_comment <path> <quote> <text> [author] [anchorIndex]", file=sys.stderr)
        return 1
    path, quote, text = sys.argv[1], sys.argv[2], sys.argv[3]
    author = sys.argv[4] if len(sys.argv) > 4 and sys.argv[4] else "aide"
    try:
        anchor_index = int(sys.argv[5]) if len(sys.argv) > 5 else 0
    except ValueError:
        anchor_index = 0
    if not path.lower().endswith(".docx"):
        print("仅支持 .docx 文件", file=sys.stderr)
        return 1
    if not os.path.isfile(path):
        print(f"文件不存在: {path}", file=sys.stderr)
        return 1
    if not quote.strip() or not text.strip():
        print("quote 和 text 不能为空", file=sys.stderr)
        return 1

    from docx import Document  # noqa: WPS433

    d = Document(path)
    matches = [p for p in d.paragraphs if quote in (p.text or "")]
    if not matches:
        print(f"未找到包含片段的段落: {quote!r}", file=sys.stderr)
        return 1
    if anchor_index >= len(matches):
        print(f"anchorIndex={anchor_index} 超出范围（共 {len(matches)} 处匹配）", file=sys.stderr)
        return 1
    target = matches[anchor_index]
    runs = target.runs
    if not runs:
        print("目标段落没有可锚定的 run（空段落）", file=sys.stderr)
        return 1
    c = d.add_comment(runs=runs, text=text, author=author, initials=author[:1].upper() or "A")
    d.save(path)
    print(json.dumps({"ok": True, "id": c.comment_id}, ensure_ascii=False))
    return 0


if __name__ == "__main__":
    sys.exit(main())
