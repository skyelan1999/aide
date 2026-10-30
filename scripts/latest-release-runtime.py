#!/usr/bin/env python3
"""Fetch and safely import the current official full Linux release package."""
from __future__ import annotations

import argparse
import hashlib
import json
import os
import re
import subprocess
import sys
import tempfile
import urllib.error
import urllib.request
import zipfile
from pathlib import Path

API = "https://api.github.com/repos/skyelan1999/aide/releases?per_page=100"
DOWNLOAD = re.compile(r"https://github\.com/skyelan1999/aide/releases/download/([A-Za-z0-9._-]+)/([A-Za-z0-9._-]+\.zip)\Z")
TAG = re.compile(r"v[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+(?:-RC[0-9]+)?\Z")
IMAGE = re.compile(r"aide-[A-Za-z0-9._-]+-linux-(amd64|arm64)-image\.tar\.gz\Z")
MAX_PACKAGE = 2 * 1024**3
MAX_IMAGE = 2 * 1024**3


def request(url: str, timeout: int = 30):
    req = urllib.request.Request(url, headers={"User-Agent": "aide-startup-updater", "Accept": "application/vnd.github+json"})
    return urllib.request.urlopen(req, timeout=timeout)


def sha256(path: Path) -> str:
    h = hashlib.sha256()
    with path.open("rb") as f:
        for block in iter(lambda: f.read(1024 * 1024), b""):
            h.update(block)
    return h.hexdigest()


def inspect(docker: str, ref: str) -> dict | None:
    p = subprocess.run([docker, "image", "inspect", ref], text=True, capture_output=True)
    if p.returncode:
        return None
    try:
        return json.loads(p.stdout)[0]
    except (ValueError, IndexError):
        return None


def safe_zip(zf: zipfile.ZipFile):
    infos = zf.infolist()
    if len(infos) > 10000 or sum(x.file_size for x in infos) > 4 * 1024**3:
        raise ValueError("Release ZIP exceeds safe extraction limits")
    for info in infos:
        p = Path(info.filename)
        mode = (info.external_attr >> 16) & 0o170000
        if p.is_absolute() or ".." in p.parts or mode == 0o120000:
            raise ValueError("Release ZIP contains an unsafe path or link")
    bad = zf.testzip()
    if bad:
        raise ValueError("Release ZIP CRC check failed")


def prepare(args):
    arch = args.platform.removeprefix("linux/")
    if args.platform not in ("linux/amd64", "linux/arm64"):
        raise ValueError("Unsupported Docker platform")
    with request(API) as r:
        releases = json.load(r)
    candidates = []
    for release in releases if isinstance(releases, list) else []:
        candidate_tag = release.get("tag_name", "")
        if not TAG.fullmatch(candidate_tag) or release.get("draft") or not release.get("published_at"):
            continue
        candidate_assets = [a for a in release.get("assets", []) if a.get("name") == f"aide-{candidate_tag}-full-linux-{arch}.zip"]
        if len(candidate_assets) == 1:
            candidates.append((release, candidate_assets[0]))
    if not candidates:
        raise ValueError(f"No published full package is available for {args.platform}")
    release, asset = max(candidates, key=lambda pair: pair[0]["published_at"])
    tag = release["tag_name"]
    name = asset["name"]
    match = DOWNLOAD.fullmatch(asset.get("browser_download_url", ""))
    digest = asset.get("digest", "")
    if not match or match.group(1) != tag or not re.fullmatch(r"sha256:[0-9a-f]{64}", digest):
        raise ValueError("Latest Release asset URL or SHA256 metadata is invalid")
    expected = digest.split(":", 1)[1]
    current = inspect(args.docker, args.target)
    state_path = Path(args.state)
    try:
        state = json.loads(state_path.read_text())
    except (OSError, ValueError):
        state = {}
    if state.get("tag") == tag and state.get("imageId") and current and current.get("Id") == state["imageId"]:
        print(f"{tag}|{state['imageId']}||unchanged")
        return

    cache = Path(args.cache)
    cache.mkdir(parents=True, exist_ok=True)
    package = cache / name
    if not package.exists() or sha256(package) != expected:
        partial = package.with_suffix(package.suffix + ".partial")
        partial.unlink(missing_ok=True)
        print(f"Downloading official Aide runtime {tag} ({args.platform})", file=sys.stderr)
        try:
            with request(asset["browser_download_url"], timeout=120) as r, partial.open("wb") as out:
                total = 0
                while True:
                    block = r.read(1024 * 1024)
                    if not block:
                        break
                    total += len(block)
                    if total > MAX_PACKAGE:
                        raise ValueError("Release package exceeds 2 GiB")
                    out.write(block)
            if sha256(partial) != expected:
                raise ValueError("Official Release SHA256 mismatch")
            os.replace(partial, package)
        except Exception:
            partial.unlink(missing_ok=True)
            raise

    with zipfile.ZipFile(package) as zf:
        safe_zip(zf)
        roots = {Path(n).parts[0] for n in zf.namelist() if Path(n).parts}
        if len(roots) != 1:
            raise ValueError("Full package must contain exactly one root directory")
        root = roots.pop()
        manifest_name = f"{root}/docker-images/manifest.json"
        sums_name = f"{root}/docker-images/SHA256SUMS"
        manifest = json.loads(zf.read(manifest_name))
        if (manifest.get("format") != "aide-update-package" or manifest.get("version") != 1
                or manifest.get("releaseTag") != tag or manifest.get("platform") != args.platform):
            raise ValueError("Release image manifest does not match the selected Release/platform")
        image_name = manifest.get("imageArchive", "")
        if not IMAGE.fullmatch(image_name) or image_name != f"aide-{tag}-linux-{arch}-image.tar.gz":
            raise ValueError("Release image archive name is invalid")
        if not isinstance(manifest.get("imageId"), str) or not re.fullmatch(r"sha256:[0-9a-f]{64}", manifest["imageId"]):
            raise ValueError("Release image ID is invalid")
        image_path = f"{root}/docker-images/{image_name}"
        info = zf.getinfo(image_path)
        if info.file_size <= 0 or info.file_size > MAX_IMAGE:
            raise ValueError("Release image archive size is invalid")
        sums = zf.read(sums_name).decode("ascii")
        matches = [line.split() for line in sums.splitlines() if line.split() and line.split()[-1] == image_name]
        if len(matches) != 1 or len(matches[0]) != 2 or not re.fullmatch(r"[0-9a-f]{64}", matches[0][0]):
            raise ValueError("Release image SHA256SUMS is invalid")
        expected_image = matches[0][0]
        with tempfile.NamedTemporaryFile(prefix="aide-release-image-", suffix=".tar.gz", delete=False) as f:
            image_tmp = Path(f.name)
            h = hashlib.sha256()
            with zf.open(image_path) as src:
                for block in iter(lambda: src.read(1024 * 1024), b""):
                    h.update(block)
                    f.write(block)
        if h.hexdigest() != expected_image:
            image_tmp.unlink(missing_ok=True)
            raise ValueError("Release image archive SHA256 mismatch")

    image_cache = cache / "current-image.tar.gz"
    os.replace(image_tmp, image_cache)
    loaded = subprocess.run([args.docker, "image", "load", "--input", str(image_cache)], text=True, capture_output=True)
    if loaded.returncode:
        raise RuntimeError("Docker image load failed: " + (loaded.stderr[-1200:] or loaded.stdout[-1200:]))
    image = inspect(args.docker, manifest["imageId"])
    if not image:
        raise ValueError("Loaded Release image ID could not be inspected")
    config = image.get("Config", {})
    labels = config.get("Labels") or {}
    actual_platform = f"{image.get('Os')}/{image.get('Architecture')}"
    src = labels.get("aide.srcsha", "")
    if (actual_platform != args.platform or not re.fullmatch(r"[0-9a-f]{64}", src)
            or labels.get("aide.cad-runtime") != "8.0.1.1.0"):
        raise ValueError("Loaded image platform or runtime identity is invalid")
    previous = current.get("Id", "") if current else ""
    if previous and previous != manifest["imageId"]:
        rollback = f"aide:rollback-start-{hashlib.sha256(previous.encode()).hexdigest()[:16]}"
        subprocess.run([args.docker, "image", "tag", previous, rollback], check=True)
    subprocess.run([args.docker, "image", "tag", manifest["imageId"], args.target], check=True)
    print(f"{tag}|{manifest['imageId']}|{previous}|updated")


def commit(args):
    if not TAG.fullmatch(args.tag) or not re.fullmatch(r"sha256:[0-9a-f]{64}", args.image_id):
        raise ValueError("Invalid activation receipt")
    path = Path(args.state)
    path.parent.mkdir(parents=True, exist_ok=True)
    tmp = path.with_suffix(".tmp")
    tmp.write_text(json.dumps({"tag": args.tag, "imageId": args.image_id}, separators=(",", ":")) + "\n")
    os.replace(tmp, path)
    for package in Path(args.cache).glob(f"aide-{args.tag}-full-linux-*.zip"):
        package.unlink(missing_ok=True)


def main():
    p = argparse.ArgumentParser()
    sub = p.add_subparsers(dest="command", required=True)
    q = sub.add_parser("prepare")
    for flag in ("docker", "platform", "target", "cache", "state"):
        q.add_argument("--" + flag, required=True)
    q.set_defaults(func=prepare)
    q = sub.add_parser("commit")
    q.add_argument("--tag", required=True); q.add_argument("--image-id", required=True); q.add_argument("--state", required=True)
    q.add_argument("--cache", required=True)
    q.set_defaults(func=commit)
    args = p.parse_args()
    try:
        args.func(args)
    except (OSError, ValueError, RuntimeError, urllib.error.URLError, zipfile.BadZipFile, subprocess.CalledProcessError) as exc:
        print(f"Latest Release runtime unavailable: {exc}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
