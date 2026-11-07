#!/usr/bin/env python3
"""Add conspicuous notices to files proven different from the pinned WAR.

The before-comparison SHA-256 is checked before every write. Reruns are safe.
"""
from __future__ import annotations

import hashlib
import json
from pathlib import Path

HERE = Path(__file__).resolve().parent
ROOT = HERE.parents[2]
VENDOR = ROOT / "internal/server/web/vendor/drawio"
RECORDS = json.loads((HERE / "drawio-comparison-before.json").read_text())["differences"]
JS_NOTICE = b"/* Modified by Aide; change notice added 2026-11-07. See NOTICE.txt and the pinned upstream comparison. */\n"
HTML_NOTICE = b"<!-- Modified by Aide; change notice added 2026-11-07. See NOTICE.txt and the pinned upstream comparison. -->\n"
MD_NOTICE = b"<!-- Modified by Aide; change notice added 2026-11-07. See NOTICE.txt and the pinned upstream comparison. -->\n"


def main() -> None:
    modified = [row for row in RECORDS if row["status"] == "modified"]
    for row in modified:
        path = VENDOR / row["path"]
        original = path.read_bytes()
        prefix = HTML_NOTICE if path.suffix == ".html" else MD_NOTICE if path.suffix == ".md" else JS_NOTICE
        if original.startswith(prefix):
            continue
        if hashlib.sha256(original).hexdigest() != row["local_sha256"]:
            raise SystemExit(f"Source changed since comparison: {path}")
        if path.suffix not in {".js", ".html", ".md"}:
            raise SystemExit(f"Unreviewed file type: {path}")
        path.write_bytes(prefix + original)
    print(f"Checked and marked {len(modified)} changed upstream files")


if __name__ == "__main__":
    main()
