#!/usr/bin/env python3
"""docx_resolve_comment <path> <commentId>：把 .docx 原生批注标记为已解决。

python-docx 1.2.0 本身不暴露"已解决"状态；Word 2016+ 用 word/commentsExtended.xml
中的 <w15:commentEx w15:paraId="..." w15:done="1"/> 表达。本脚本直接改包内 XML：
  1) 给 comments.xml 中目标批注的 <w:p> 补 w15:paraId（缺失则生成随机 8 位 hex）；
  2) 写/更新 commentsExtended.xml；
  3) 同步 [Content_Types].xml 与 document.xml.rels。
输出 {"ok": true, "id": <commentId>, "resolved": true}。
"""
import json
import os
import random
import re
import shutil
import sys
import zipfile
from lxml import etree

W = "http://schemas.openxmlformats.org/wordprocessingml/2006/main"
W15 = "http://schemas.microsoft.com/office/word/2012/wordml"
NS = {"w": W, "w15": W15}

CT_OVERRIDE = (
    '<Override PartName="/word/commentsExtended.xml" '
    'ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.commentsExtended+xml"/>'
)
REL_TYPE = "http://schemas.microsoft.com/office/2011/relationships/commentsExtended"


def qn(prefix: str, tag: str) -> str:
    return f"{{{NS[prefix]}}}{tag}"


def main() -> int:
    if len(sys.argv) < 3:
        print("用法: docx_resolve_comment <path> <commentId>", file=sys.stderr)
        return 1
    path, comment_id = sys.argv[1], sys.argv[2]
    try:
        target_id = str(int(comment_id))
    except ValueError:
        print("commentId 必须是整数", file=sys.stderr)
        return 1
    if not path.lower().endswith(".docx"):
        print("仅支持 .docx 文件", file=sys.stderr)
        return 1
    if not os.path.isfile(path):
        print(f"文件不存在: {path}", file=sys.stderr)
        return 1

    tmp = path + ".aide-tmp.docx"
    with zipfile.ZipFile(path, "r") as zin:
        names = zin.namelist()
        data = {n: zin.read(n) for n in names}

    if "word/comments.xml" not in data:
        print("文档没有批注", file=sys.stderr)
        return 1

    comments = etree.fromstring(data["word/comments.xml"])
    found = None
    for c in comments.findall(f"{{{W}}}comment"):
        if c.get(qn("w", "id")) == target_id:
            found = c
            break
    if found is None:
        print(f"批注不存在: {target_id}", file=sys.stderr)
        return 1

    p = found.find(f"{{{W}}}p")
    if p is None:
        print("目标批注缺少段落", file=sys.stderr)
        return 1
    para_id = p.get(qn("w15", "paraId"))
    if not para_id:
        para_id = f"{random.getrandbits(32):08X}"
        p.set(qn("w15", "paraId"), para_id)
    data["word/comments.xml"] = etree.tostring(
        comments, xml_declaration=True, encoding="UTF-8", standalone=True
    )

    # commentsExtended.xml：存在则解析更新，不存在则新建
    ext_name = "word/commentsExtended.xml"
    if ext_name in data:
        ext = etree.fromstring(data[ext_name])
    else:
        ext = etree.fromstring(
            f'<w15:commentsEx xmlns:w15="{W15}"></w15:commentsEx>'.encode("utf-8")
        )
    existing = None
    for ex in ext.findall(f"{{{W15}}}commentEx"):
        if ex.get(qn("w15", "paraId")) == para_id:
            existing = ex
            break
    if existing is None:
        existing = etree.SubElement(ext, qn("w15", "commentEx"))
    existing.set(qn("w15", "paraId"), para_id)
    existing.set(qn("w15", "done"), "1")
    data[ext_name] = etree.tostring(
        ext, xml_declaration=True, encoding="UTF-8", standalone=True
    )

    # [Content_Types].xml：补 Override
    ct = data["[Content_Types].xml"].decode("utf-8")
    if "commentsExtended.xml" not in ct:
        ct = ct.replace("</Types>", CT_OVERRIDE + "</Types>")
        data["[Content_Types].xml"] = ct.encode("utf-8")

    # document.xml.rels：补关系
    rels_name = "word/_rels/document.xml.rels"
    rels = data[rels_name].decode("utf-8")
    if "commentsExtended" not in rels:
        used = set(re.findall(r'Id="(rId\d+)"', rels))
        n = 1
        while f"rId{n}" in used:
            n += 1
        rel = (
            f'<Relationship Id="rId{n}" Type="{REL_TYPE}" Target="commentsExtended.xml"/>'
        )
        rels = rels.replace("</Relationships>", rel + "</Relationships>")
        data[rels_name] = rels.encode("utf-8")

    with zipfile.ZipFile(tmp, "w", zipfile.ZIP_DEFLATED) as zout:
        for n, b in data.items():
            zout.writestr(n, b)
    shutil.move(tmp, path)
    print(json.dumps({"ok": True, "id": target_id, "resolved": True}, ensure_ascii=False))
    return 0


if __name__ == "__main__":
    sys.exit(main())
