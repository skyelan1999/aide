#!/usr/bin/env python3
"""Exercise the proof writer over loopback with owned server and receipt paths."""
import json
from pathlib import Path
import re
import select
import socket
import subprocess
import sys
import tempfile
import unittest
from urllib.error import HTTPError
from urllib.request import Request, urlopen

ROOT = Path(__file__).resolve().parents[1]


class MermaidProofAuthenticationTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix="aide-proof-auth-")
        self.addCleanup(self.temp.cleanup)
        self.proof = Path(self.temp.name) / "proof.json"
        self.proof.write_text('owned-existing-proof\n')
        with socket.socket() as reserved:
            reserved.bind(("127.0.0.1", 0))
            port = reserved.getsockname()[1]
        self.origin = f"http://127.0.0.1:{port}"
        # Rejection cases log multiple requests before the parent reads output.
        # A regular temporary file cannot block the server on a full stderr pipe.
        self.server_log = tempfile.TemporaryFile(mode="w+t")
        self.addCleanup(self.server_log.close)
        self.server = subprocess.Popen([sys.executable, "-u", "scripts/fixtures/serve-mermaid-csp.py",
                                        "--port", str(port), "--proof", str(self.proof)], cwd=ROOT,
                                       stdout=subprocess.PIPE, stderr=self.server_log, text=True)
        self.addCleanup(self.stop)
        ready, _, _ = select.select([self.server.stdout], [], [], 5)
        self.assertTrue(ready, "owned proof server did not start")
        self.assertEqual(self.server.stdout.readline().strip(), self.origin + "/")

    def stop(self):
        if self.server.poll() is None:
            self.server.terminate()
            try:
                self.server.wait(timeout=3)
            except subprocess.TimeoutExpired:
                self.server.kill()
                self.server.wait(timeout=3)
        self.server.stdout.close()

    def post(self, body=b'{"status":"pass","forged":true}', headers=None):
        try:
            with urlopen(Request(self.origin + "/proof", data=body, headers=headers or {}, method="POST"), timeout=3) as response:
                return response.status, dict(response.headers)
        except HTTPError as error:
            with error:
                return error.code, dict(error.headers)

    def token(self):
        with urlopen(self.origin + "/fixture.js", timeout=3) as response:
            script = response.read().decode()
        match = re.search(r'const proofToken = "([A-Za-z0-9_-]{32,})";', script)
        self.assertIsNotNone(match, "per-run token must be supplied to owned fixture")
        return match[1]

    def test_cross_origin_simple_post_cannot_overwrite_proof(self):
        status, _ = self.post(headers={"Origin": "https://untrusted.invalid", "Content-Type": "text/plain"})
        self.assertEqual(status, 403)
        self.assertEqual(self.proof.read_text(), 'owned-existing-proof\n')

    def test_valid_token_and_same_origin_writes_receipt(self):
        status, _ = self.post(headers={"Origin": self.origin, "Content-Type": "application/json", "X-Aide-Proof-Token": self.token()})
        self.assertEqual(status, 204)
        self.assertEqual(json.loads(self.proof.read_text()), {"status": "pass", "forged": True})

    def test_token_cannot_bypass_origin_or_media_type(self):
        token = self.token()
        for headers, expected in [
            ({"Origin": "https://untrusted.invalid", "Content-Type": "application/json", "X-Aide-Proof-Token": token}, 403),
            ({"Host": "untrusted.invalid", "Origin": self.origin, "Content-Type": "application/json", "X-Aide-Proof-Token": token}, 403),
            ({"Origin": self.origin, "Content-Type": "application/json"}, 403),
            ({"Origin": self.origin, "Content-Type": "application/json", "X-Aide-Proof-Token": "wrong"}, 403),
            ({"Origin": self.origin, "Content-Type": "application/json", "X-Aide-Proof-Token": "é"}, 403),
            ({"Origin": self.origin, "Content-Type": "text/plain", "X-Aide-Proof-Token": token}, 415),
        ]:
            self.assertEqual(self.post(headers=headers)[0], expected)
            self.assertEqual(self.proof.read_text(), 'owned-existing-proof\n')

    def test_malformed_or_oversized_proof_does_not_replace_receipt(self):
        headers = {"Origin": self.origin, "Content-Type": "application/json", "X-Aide-Proof-Token": self.token()}
        for body, expected in [(b"not-json", 400), (b"[]", 400), (b"x" * 16385, 413)]:
            self.assertEqual(self.post(body, headers)[0], expected)
            self.assertEqual(self.proof.read_text(), 'owned-existing-proof\n')

    def test_fixture_response_retains_csp_and_restricts_cross_origin_scripts(self):
        with urlopen(self.origin + "/fixture.js", timeout=3) as response:
            self.assertIn("script-src 'self'", response.headers["Content-Security-Policy"])
            self.assertEqual(response.headers["Cross-Origin-Resource-Policy"], "same-origin")
            self.assertEqual(response.headers["X-Content-Type-Options"], "nosniff")


if __name__ == "__main__":
    unittest.main()
