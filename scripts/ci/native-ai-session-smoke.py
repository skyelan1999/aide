#!/usr/bin/env python3
"""Exercise an installed Aide session through a local OpenAI-compatible model fixture."""
import json
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import ssl
import threading
import time
import urllib.error
import urllib.request

MODEL = "aide-native-install-smoke"
API_KEY = "aide-native-install-smoke-key"
ANSWER = "AIDE_NATIVE_AI_SESSION_OK"
received = []


class MockModel(BaseHTTPRequestHandler):
    def do_POST(self):
        length = int(self.headers.get("Content-Length", "0"))
        request_body = json.loads(self.rfile.read(length))
        received.append((self.path, self.headers.get("Authorization"), request_body))
        if self.path != "/v1/chat/completions" or request_body.get("model") != MODEL:
            self.send_error(404)
            return
        body = json.dumps({
            "id": "chatcmpl-aide-native-smoke",
            "object": "chat.completion",
            "choices": [{"index": 0, "message": {"role": "assistant", "content": ANSWER}, "finish_reason": "stop"}],
            "usage": {"prompt_tokens": 8, "completion_tokens": 2, "total_tokens": 10},
        }).encode()
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def log_message(self, *_args):
        pass


def main():
    import argparse
    parser = argparse.ArgumentParser()
    parser.add_argument("--base", required=True)
    parser.add_argument("--token", required=True)
    args = parser.parse_args()
    server = ThreadingHTTPServer(("127.0.0.1", 0), MockModel)
    thread = threading.Thread(target=server.serve_forever, daemon=True)
    thread.start()
    context = ssl._create_unverified_context() if args.base.startswith("https://") else None

    def request(method, path, payload=None):
        data = None if payload is None else json.dumps(payload).encode()
        req = urllib.request.Request(
            args.base.rstrip("/") + path, data=data, method=method,
            headers={"Authorization": "Bearer " + args.token, "Accept": "application/json",
                     **({"Content-Type": "application/json"} if data is not None else {})},
        )
        try:
            with urllib.request.urlopen(req, timeout=12, context=context) as response:
                return json.load(response)
        except urllib.error.HTTPError as exc:
            detail = exc.read(1024).decode("utf-8", errors="replace").strip()
            raise RuntimeError(f"Aide API {method} {path} returned HTTP {exc.code}: {detail}") from None

    try:
        provider_url = f"http://127.0.0.1:{server.server_address[1]}/v1"
        request("PUT", "/api/settings", {
            "model": MODEL,
            "activeModel": MODEL,
            "baseURL": provider_url,
            "models": [{"id": MODEL, "name": "Native install smoke", "providerId": "default", "contextWindow": 32768}],
            "modelProviders": [{
                "id": "default", "name": "Native install smoke", "baseURL": provider_url,
                "protocol": "openai-compatible", "keys": [{"id": "smoke", "label": "CI fixture"}], "activeKeyId": "smoke",
            }],
            "providerKeyWrites": [{"providerId": "default", "keyId": "smoke", "apiKey": API_KEY}],
        })
        session = request("POST", "/api/sessions", {"title": "Native install AI smoke"})
        session_id = session.get("id")
        if not session_id:
            raise RuntimeError("Aide did not create an AI smoke session")
        run = request("POST", f"/api/sessions/{session_id}/runs", {
            "prompt": "Return exactly AIDE_NATIVE_AI_SESSION_OK and do not call tools.",
            "mode": "chat", "strategy": "auto",
        })
        if not run.get("id"):
            raise RuntimeError("Aide did not create an AI session run")
        deadline = time.monotonic() + 35
        final = None
        while time.monotonic() < deadline:
            final = request("GET", f"/api/sessions/{session_id}")
            messages = final.get("messages", [])
            if any(item.get("role") == "assistant" and ANSWER in str(item.get("content", "")) for item in messages):
                break
            time.sleep(0.4)
        else:
            statuses = [item.get("status") for item in (final or {}).get("runs", [])]
            raise RuntimeError(f"AI session did not receive the expected answer; run states={statuses}")
        if not received or received[0][0] != "/v1/chat/completions" or received[0][1] != "Bearer " + API_KEY:
            raise RuntimeError("Installed Aide did not reach the local mock model with its configured credential")
        if received[0][2].get("model") != MODEL:
            raise RuntimeError("Installed Aide sent the wrong model to the local mock provider")
        print("Native installed Aide AI session/provider/auth/response smoke: PASS")
    finally:
        server.shutdown()
        server.server_close()


if __name__ == "__main__":
    main()
