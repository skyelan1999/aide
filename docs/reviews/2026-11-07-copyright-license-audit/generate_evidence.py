#!/usr/bin/env python3
"""Read-only inventory of the checked-out source and currently running image."""
from __future__ import annotations

import hashlib
import json
from pathlib import Path
import re
import subprocess
from zipfile import ZipFile

ROOT = Path(__file__).resolve().parents[3]
OUT = Path(__file__).resolve().parent
WAR = ROOT.parent / "aide-audit-snapshots/draw-v31.5.2.war"


def sha(path: Path) -> str:
    return hashlib.sha256(path.read_bytes()).hexdigest()


def run(*args: str) -> str:
    return subprocess.run(args, cwd=ROOT, check=True, text=True, capture_output=True).stdout


def write(name: str, value: object) -> None:
    (OUT / name).write_text(json.dumps(value, ensure_ascii=False, indent=2) + "\n")


def drawio() -> None:
    base = ROOT / "internal/server/web/vendor/drawio"
    with ZipFile(WAR) as upstream:
        names = {name for name in upstream.namelist() if not name.endswith("/")}
        rows = []
        for path in sorted(base.rglob("*")):
            if not path.is_file():
                continue
            name = path.relative_to(base).as_posix()
            local_hash = sha(path)
            upstream_hash = hashlib.sha256(upstream.read(name)).hexdigest() if name in names else None
            if upstream_hash == local_hash:
                continue
            rows.append({"path": name, "status": "modified" if upstream_hash else "vendor_only",
                         "upstream_sha256": upstream_hash, "local_sha256": local_hash})
    write("drawio-comparison-before.json", {
        "source": "https://github.com/jgraph/drawio/releases/download/v31.5.2/draw.war",
        "war_sha256": sha(WAR), "vendor_root": "internal/server/web/vendor/drawio",
        "upstream_file_count": len(names), "vendored_file_count": sum(p.is_file() for p in base.rglob("*")),
        "differences": rows,
    })


def inventory() -> None:
    go_mod = (ROOT / "go.mod").read_text()
    modules = []
    for module, version in re.findall(r"^\s*([^\s()]+)\s+(v[^\s]+)", go_mod, re.M):
        vendor = ROOT / "vendor" / module
        licenses = [p.relative_to(ROOT).as_posix() for p in sorted(vendor.rglob("*"))
                    if p.is_file() and p.name.lower().startswith(("license", "copying", "notice"))]
        modules.append({"name": module, "version": version, "license_text_paths": licenses})
    web = ROOT / "internal/server/web/vendor"
    web_items = []
    for entry in sorted(web.iterdir()):
        if not entry.is_dir() and not entry.is_file():
            continue
        license_files = [p.relative_to(ROOT).as_posix() for p in entry.rglob("*")
                         if p.is_file() and ("license" in p.name.lower() or "notice" in p.name.lower())] if entry.is_dir() else []
        web_items.append({"path": entry.relative_to(ROOT).as_posix(), "license_text_paths": license_files})
    image = run("docker", "inspect", "aide-aide-1", "--format", "{{.Image}} {{.Config.Image}}").strip()
    packages = run("docker", "exec", "aide-aide-1", "sh", "-lc", "dpkg-query -W -f='${Package}\\t${Version}\\n'").splitlines()
    packages = [line.replace("\\t", "\t").split("\t", 1) for line in packages]
    py_script = """import importlib.metadata as m,json
for d in sorted(m.distributions(),key=lambda d:d.metadata.get('Name','')):
 n=d.metadata.get('Name'); files=[str(p) for p in (d.files or []) if 'LICENSE' in str(p).upper() or 'COPYING' in str(p).upper()]
 print(json.dumps({'name':n,'version':d.version,'declared_license':d.metadata.get('License-Expression') or d.metadata.get('License') or None,'license_files':files}))
"""
    py = [json.loads(line) for line in run("docker", "exec", "aide-aide-1", "python", "-c", py_script).splitlines()]
    run_license_paths = run("docker", "exec", "aide-aide-1", "sh", "-lc",
                            "find /opt/aide -type f | grep -Ei '/licenses/|/license|/notice' | sort").splitlines()
    lock = json.loads((ROOT / "scripts/browser-runtime/package-lock.json").read_text())
    write("distribution-inventory.json", {
        "source_head": run("git", "rev-parse", "HEAD").strip(),
        "observed_container": image,
        "go_modules": modules,
        "web_vendor_entries": web_items,
        "python_installed_in_observed_container": py,
        "node_installed_in_observed_container": run("docker", "exec", "aide-aide-1", "node", "--version").strip(),
        "optional_browser_runtime_lock": [{"name": k, "version": v.get("version"), "declared_license": v.get("license")}
                                          for k, v in lock["packages"].items() if k],
        "debian_packages_in_observed_container": [{"name": x[0], "version": x[1] if len(x) > 1 else ""} for x in packages],
        "explicit_license_files_in_observed_container_opt_aide": run_license_paths,
        "native_helper_source_paths": ["native/macos-desktop/main.swift", "native/macos-desktop/computer_control.swift",
                                       "scripts/native-computer-bridge.swift", "native/windows-launcher/third_party"],
        "conditional_runtime_components": ["sherpa-onnx 1.13.8", "onnxruntime shared library", "Codex Bridge 0.4.0"],
    })


if __name__ == "__main__":
    drawio()
    inventory()
