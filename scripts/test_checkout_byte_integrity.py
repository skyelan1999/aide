#!/usr/bin/env python3
"""Exercise real autocrlf checkout of hash-pinned vendor and license inputs."""
import hashlib
import json
from pathlib import Path
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parent.parent


def git(root, *args):
    return subprocess.run(["git", "-C", str(root), *args], check=True,
                          stdout=subprocess.PIPE, stderr=subprocess.PIPE).stdout


class CheckoutByteIntegrityTests(unittest.TestCase):
    def test_autocrlf_preserves_pinned_sources_and_generated_prefix(self):
        paths = {item["path"] for item in json.loads(
            (ROOT / "legal/third_party/sources.json").read_text())}
        for item in json.loads((ROOT / "internal/server/web/vendor/urdf-loader/provenance.json").read_text()):
            paths.update("internal/server/web/vendor/" + name for name in item["files"])
        paths.update({"internal/server/web/vendor/drawio/js/plantuml/re2js.min.js",
                      "internal/server/web/vendor/drawio/js/plantuml/drawio-plantuml.min.js",
                      "internal/server/web/plantuml-resource-helpers.js"})
        with tempfile.TemporaryDirectory(prefix="aide-checkout-") as temp:
            source, checkout = Path(temp) / "source", Path(temp) / "checkout"
            source.mkdir()
            git(source, "init", "-q")
            git(source, "config", "core.autocrlf", "false")
            for relative in paths | {".gitattributes"}:
                target = source / relative
                target.parent.mkdir(parents=True, exist_ok=True)
                target.write_bytes((ROOT / relative).read_bytes())
            # Also cover upstream text that intentionally contains CRLF.
            crlf = "internal/server/web/vendor/fixture/LICENSE"
            (source / crlf).parent.mkdir(parents=True)
            (source / crlf).write_bytes(b"Upstream notice\r\nOriginal bytes\r\n")
            git(source, "add", ".")
            git(source, "-c", "user.name=Checkout test", "-c", "user.email=checkout@example.invalid",
                "commit", "-qm", "fixture")
            git(Path(temp), "clone", "-q", "--no-checkout", str(source), str(checkout))
            git(checkout, "config", "core.autocrlf", "true")
            git(checkout, "checkout", "-q", "HEAD")
            for relative in paths | {crlf}:
                with self.subTest(path=relative):
                    self.assertEqual((checkout / relative).read_bytes(),
                                     (source / relative).read_bytes())
            for item in json.loads((ROOT / "legal/third_party/sources.json").read_text()):
                self.assertEqual(hashlib.sha256((checkout / item["path"]).read_bytes()).hexdigest(),
                                 item["sha256"])
            print(f"Verified {len(paths)} real inputs and original-CRLF control under core.autocrlf=true")


if __name__ == "__main__":
    unittest.main()
