#!/usr/bin/env python3
"""Read/write portable Word comments in a DOCX, using JSON on stdin.

Usage: docx_comments.py list|add|edit <file.docx> [JSON spec]. The caller owns
workspace permissions, version checks and atomic persistence.
"""
import copy
import json
import sys

from docx import Document
from docx.oxml.ns import qn
from docx.text.paragraph import Paragraph
from docx.text.run import Run


def paragraphs(doc):
    for element in doc.element.body.iter(qn("w:p")):
        yield Paragraph(element, doc)


def anchors(doc):
    found = {}
    for para in paragraphs(doc):
        starts = {}
        active = set()
        chunks = {}
        for node in para._p:
            if node.tag == qn("w:commentRangeStart"):
                cid = node.get(qn("w:id"))
                starts[cid] = True
                active.add(cid)
                chunks.setdefault(cid, [])
            elif node.tag == qn("w:commentRangeEnd"):
                active.discard(node.get(qn("w:id")))
            elif node.tag == qn("w:r"):
                value = Run(node, para).text
                for cid in active:
                    chunks[cid].append(value)
        for cid in starts:
            # Cross-paragraph ranges cannot be edited safely by this tool.
            if cid not in chunks or any(n.tag == qn("w:commentRangeEnd") and n.get(qn("w:id")) == cid for n in para._p) is False:
                continue
            found[cid] = (para, "".join(chunks[cid]))
    return found


def list_comments(doc):
    anchored = anchors(doc)
    rows = []
    for comment in doc.comments:
        cid = str(comment.comment_id)
        para, quote = anchored.get(cid, (None, ""))
        rows.append({
            "id": comment.comment_id,
            "author": comment.author or "",
            "date": comment.timestamp.isoformat() if comment.timestamp else "",
            "text": comment.text or "",
            "anchorQuote": quote,
            "anchorValid": para is not None and bool(quote),
        })
    return rows


def split_at(para, offset):
    at = 0
    for run in para.runs:
        end = at + len(run.text)
        if at < offset < end:
            if any(child.tag not in {qn("w:rPr"), qn("w:t"), qn("w:tab"), qn("w:br")} for child in run._r):
                raise ValueError("锚点包含复杂对象，不能安全拆分")
            clone = copy.deepcopy(run._r)
            run._r.addnext(clone)
            Run(clone, para).text = run.text[offset - at:]
            run.text = run.text[:offset - at]
            return
        at = end


def add_comment(doc, spec):
    quote = str(spec.get("quote", ""))
    body = str(spec.get("text", ""))
    if not quote.strip() or not body.strip() or len(quote) > 4000 or len(body) > 20000:
        raise ValueError("quote/text 不能为空或过长")
    index = spec.get("anchorIndex", 0)
    if type(index) is not int or index < 0:
        raise ValueError("anchorIndex 必须为非负整数")
    positions = []
    for para in paragraphs(doc):
        start = 0
        while True:
            pos = para.text.find(quote, start)
            if pos < 0:
                break
            positions.append((para, pos))
            start = pos + 1
    if index >= len(positions):
        raise ValueError(f"原文锚点不存在或序号越界（匹配 {len(positions)} 处）")
    para, start = positions[index]
    stop = start + len(quote)
    split_at(para, stop)
    split_at(para, start)
    selected = []
    at = 0
    for run in para.runs:
        end = at + len(run.text)
        if at >= start and end <= stop and end > at:
            selected.append(run)
        at = end
    if not selected or "".join(run.text for run in selected) != quote:
        raise ValueError("无法精确锚定选中文本")
    author = str(spec.get("author") or "aide")[:100]
    comment = doc.add_comment(selected, body, author=author, initials=author[:1].upper())
    return {"id": comment.comment_id, "anchorQuote": quote}


def edit_comment(doc, spec):
    try:
        cid = str(int(spec.get("id")))
    except (TypeError, ValueError):
        raise ValueError("批注 ID 无效") from None
    expected = spec.get("expectedText")
    replacement = spec.get("newText")
    if not isinstance(expected, str) or not expected or not isinstance(replacement, str) or len(replacement) > 20000:
        raise ValueError("必须提供非空 expectedText 和 newText")
    if not any(str(c.comment_id) == cid for c in doc.comments):
        raise ValueError("批注不存在")
    anchor = anchors(doc).get(cid)
    if anchor is None or anchor[1] != expected:
        raise ValueError("锚点已失效或原文与 expectedText 不同，请重新读取批注")
    para, _ = anchor
    nodes = list(para._p)
    start = next((i for i, n in enumerate(nodes) if n.tag == qn("w:commentRangeStart") and n.get(qn("w:id")) == cid), -1)
    end = next((i for i, n in enumerate(nodes) if n.tag == qn("w:commentRangeEnd") and n.get(qn("w:id")) == cid), -1)
    if start < 0 or end <= start:
        raise ValueError("批注锚点无效")
    selected = nodes[start + 1:end]
    if any(n.tag != qn("w:r") for n in selected):
        raise ValueError("锚点含有嵌套批注或复杂对象，拒绝自动修改")
    runs = [Run(n, para) for n in selected]
    if not runs or "".join(r.text for r in runs) != expected:
        raise ValueError("锚点文本已变化，拒绝自动修改")
    if any(any(child.tag not in {qn("w:rPr"), qn("w:t"), qn("w:tab"), qn("w:br")} for child in r._r) for r in runs):
        raise ValueError("锚点包含复杂对象，拒绝自动修改")
    runs[0].text = replacement
    for run in runs[1:]:
        para._p.remove(run._r)
    return {"id": int(cid), "oldText": expected, "newText": replacement}


def main():
    if len(sys.argv) != 4 or sys.argv[1] not in {"list", "add", "edit"} or not sys.argv[2].lower().endswith(".docx"):
        raise ValueError("用法: docx_comments.py list|add|edit file.docx JSON")
    op, filename = sys.argv[1:3]
    spec = json.loads(sys.argv[3])
    doc = Document(filename)
    if op == "list":
        result = {"comments": list_comments(doc)}
    elif op == "add":
        result = add_comment(doc, spec)
        doc.save(filename)
    else:
        result = edit_comment(doc, spec)
        doc.save(filename)
    print(json.dumps(result, ensure_ascii=False))


if __name__ == "__main__":
    try:
        main()
    except (ValueError, KeyError) as exc:
        print(str(exc), file=sys.stderr)
        sys.exit(1)
