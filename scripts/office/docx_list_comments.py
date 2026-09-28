#!/usr/bin/env python3
"""docx_list_comments <path>：列出 .docx 原生批注（comments.xml）。

python-docx 1.2.0 支持读取 comments。输出 {"comments":[{id,author,date,text}]}。
"""
import json
import os
import sys


def main() -> int:
    path = sys.argv[1]
    if not path.lower().endswith(".docx"):
        print("仅支持 .docx 文件", file=sys.stderr)
        return 1
    if not os.path.isfile(path):
        print(f"文件不存在: {path}", file=sys.stderr)
        return 1

    from docx import Document  # noqa: WPS433

    d = Document(path)
    rows = []
    for c in d.comments:
        ts = ""
        if getattr(c, "timestamp", None) is not None:
            ts = c.timestamp.isoformat()
        rows.append({
            "id": c.comment_id,
            "author": c.author or "",
            "date": ts,
            "text": c.text or "",
        })
    print(json.dumps({"comments": rows}, ensure_ascii=False))
    return 0


if __name__ == "__main__":
    sys.exit(main())
