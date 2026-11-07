#!/usr/bin/env python3
"""Stage source-backed third-party license texts for images and offline bundles."""

import argparse
import hashlib
import json
from pathlib import Path
import shutil


def stage(source: Path, target: Path) -> dict:
    source = source.resolve()
    target.mkdir(parents=True, exist_ok=True)
    candidates = [source / "LICENSE", source / "internal/server/code_analysis/ACORN-LICENSE"]
    for root in ("vendor", "internal/server/web/vendor", "native/windows-launcher/third_party"):
        folder = source / root
        candidates.extend(
            path for path in folder.rglob("*")
            if path.is_file() and path.suffix.lower() != ".svg"
            and ("license" in path.name.lower() or "notice" in path.name.lower())
        )
    provenance = json.loads((source / "legal/third_party/sources.json").read_text())
    for item in provenance:
        path = source / item["path"]
        if not path.is_file() or hashlib.sha256(path.read_bytes()).hexdigest() != item["sha256"]:
            raise RuntimeError(f"Third-party source text changed or missing: {item['path']}")
    candidates.extend(
        path for path in (source / "legal/third_party").rglob("*")
        if path.is_file() and path.name != "sources.json" and "reference-only" not in path.name
    )
    records = []
    for path in sorted(set(candidates)):
        if not path.is_file():
            raise FileNotFoundError(path)
        relative = path.relative_to(source)
        destination = target / relative
        destination.parent.mkdir(parents=True, exist_ok=True)
        shutil.copyfile(path, destination)
        records.append({
            "source_path": str(relative),
            "sha256": hashlib.sha256(path.read_bytes()).hexdigest(),
        })
    if not any(record["source_path"] == "internal/server/web/vendor/drawio/LICENSE" for record in records):
        raise RuntimeError("draw.io Apache-2.0 license is missing")
    if not any(record["source_path"] == "LICENSE" for record in records):
        raise RuntimeError("Aide MIT license is missing")
    (target / "SOURCES.json").write_text(json.dumps(provenance, ensure_ascii=False, indent=2) + "\n")
    manifest = {"scope": "source-backed license and notice texts; see audit for unresolved components", "files": records}
    (target / "MANIFEST.json").write_text(json.dumps(manifest, ensure_ascii=False, indent=2) + "\n")
    return manifest


if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument("target", type=Path)
    parser.add_argument("--source", type=Path, default=Path(__file__).resolve().parent.parent)
    args = parser.parse_args()
    print(f"Staged {len(stage(args.source, args.target)['files'])} license/notice texts")
