#!/usr/bin/env python3
"""Install a tiny fixture plugin in an isolated workspace and exercise its Node daemon."""
import argparse
import json
from pathlib import Path
import ssl
import urllib.error
import urllib.request


def request(base: str, token: str, method: str, path: str):
    req = urllib.request.Request(
        base.rstrip("/") + path,
        method=method,
        headers={"Authorization": f"Bearer {token}", "Accept": "application/json"},
    )
    context = ssl._create_unverified_context() if base.startswith("https://") else None
    with urllib.request.urlopen(req, timeout=20, context=context) as response:
        if response.status != 200:
            raise RuntimeError(f"{method} {path}: unexpected HTTP {response.status}")
        return json.load(response)


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--base", required=True)
    parser.add_argument("--token", required=True)
    parser.add_argument("--workspace", required=True, type=Path)
    parser.add_argument("--fixture", required=True, type=Path)
    args = parser.parse_args()

    plugin_id = "native-runtime-smoke-daemon"
    plugin_dir = args.workspace / "plugins" / plugin_id
    plugin_dir.mkdir(parents=True, exist_ok=True)
    source = args.fixture / "index.js"
    if not source.is_file():
        raise FileNotFoundError(f"plugin daemon fixture is missing: {source}")
    (plugin_dir / "index.js").write_bytes(source.read_bytes())
    (plugin_dir / "manifest.json").write_text(json.dumps({
        "id": plugin_id, "name": "Native Runtime Smoke Daemon", "version": "0.0.1",
        "main": "index.js", "daemon": True, "enabled": False,
    }), encoding="utf-8")
    registry = {
        "version": 1,
        "plugins": [{
            "id": plugin_id, "name": "Native Runtime Smoke Daemon", "main": "index.js",
            "version": "0.0.1", "daemon": True, "enabled": False,
        }],
    }
    (args.workspace / "plugins" / "registry.json").write_text(json.dumps(registry), encoding="utf-8")

    endpoint = f"/api/plugins/daemons/{plugin_id}"
    started = False
    try:
        result = request(args.base, args.token, "POST", endpoint + "/start")
        started = True
        if result.get("status") != "running":
            raise AssertionError(f"daemon start did not report running: {result}")
        states = request(args.base, args.token, "GET", "/api/plugins/daemons").get("daemons", [])
        state = next((item for item in states if item.get("id") == plugin_id), None)
        if not state or state.get("status") != "running":
            raise AssertionError(f"daemon registry did not report running: {state}")
    finally:
        if started:
            stopped = request(args.base, args.token, "POST", endpoint + "/stop")
            if stopped.get("status") != "stopped":
                raise AssertionError(f"daemon stop did not report stopped: {stopped}")
    states = request(args.base, args.token, "GET", "/api/plugins/daemons").get("daemons", [])
    state = next((item for item in states if item.get("id") == plugin_id), None)
    if not state or state.get("status") != "stopped":
        raise AssertionError(f"daemon process did not stop: {state}")
    print("Bundled Node plugin daemon start/IPC-ready/status/stop smoke: PASS")


if __name__ == "__main__":
    main()
