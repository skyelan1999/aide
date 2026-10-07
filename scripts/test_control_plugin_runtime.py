#!/usr/bin/env python3
"""Smoke test a disposable Aide container; never point at user data."""
import argparse
import json
import ssl
import time
from pathlib import Path
from urllib.request import Request, urlopen


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--check", action="store_true")
    args = parser.parse_args()
    deadline = time.monotonic() + 30
    while True:
        try:
            context = ssl.create_default_context(cafile="/data/certs/cert.pem")
            token = Path("/data/auth/access-token").read_text().strip()
            break
        except (OSError, ssl.SSLError):
            if time.monotonic() >= deadline:
                raise
            time.sleep(0.2)

    def call(path, method="GET", body=None):
        data = None if body is None else json.dumps(body).encode()
        request = Request("https://localhost:8080" + path, data=data, method=method,
                          headers={"Authorization": "Bearer " + token,
                                   "Content-Type": "application/json"})
        with urlopen(request, context=context, timeout=10) as response:
            return json.load(response)

    while True:
        try:
            plugins = call("/api/plugins")
            break
        except OSError:
            if time.monotonic() >= deadline:
                raise
            time.sleep(0.2)
    if isinstance(plugins, dict):
        plugins = plugins["plugins"]
    by_id = {p["id"]: p for p in plugins}
    assert len(plugins) == len(by_id) == 19, "expected old 17 plus two controls"
    assert not by_id["computer-control"]["enabled"]
    if not args.check:
        assert not by_id["browser-control"]["enabled"]
        call("/api/plugins/browser-control/settings", "PUT",
             {"settings": {"allowedHosts": ["example.com"]}})
        call("/api/plugins/browser-control", "PUT", {"enabled": True})
    else:
        browser = by_id["browser-control"]
        assert browser["enabled"]
        assert browser["settings"]["allowedHosts"] == ["example.com"]
    surface = call("/api/plugin-surface")
    tools = [t for p in surface["plugins"] if p["id"] == "browser-control" for t in p["tools"]]
    names = {t["name"] for t in tools if t.get("executable")}
    assert names == {"browser_status", "browser_read", "browser_snapshot", "browser_navigate", "browser_click", "browser_fill", "browser_scroll"}, names
    print("PASS: 17 -> 19 plugins; seven executable browser tools; " +
          ("settings survived restart" if args.check else "controls default disabled, activation works"))


if __name__ == "__main__":
    main()
