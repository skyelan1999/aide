#!/usr/bin/env python3
"""Keep external GitHub Actions pinned to reviewable, immutable commits."""

import pathlib
import re
import unittest


WORKFLOWS = pathlib.Path(__file__).resolve().parent.parent / ".github" / "workflows"
USES = re.compile(r"^\s*(?:-\s*)?uses:\s*(\S+)(?:\s+#\s*(\S+))?\s*$", re.MULTILINE)
PINNED = re.compile(r"^[^\s@]+/[^\s@]+@[0-9a-f]{40}$")
VERSION = re.compile(r"^v\d+(?:\.\d+)*$")


def is_reviewable_pin(ref: str, version: str) -> bool:
    return bool(PINNED.fullmatch(ref) and VERSION.fullmatch(version))


class WorkflowActionPinsTest(unittest.TestCase):
    def test_movable_and_undocumented_refs_are_rejected(self):
        self.assertFalse(is_reviewable_pin("actions/checkout@v4", "v4"))
        self.assertFalse(is_reviewable_pin("actions/checkout@" + "a" * 39, "v4"))
        self.assertFalse(is_reviewable_pin("actions/checkout@" + "a" * 40, ""))
        self.assertTrue(is_reviewable_pin("actions/checkout@" + "a" * 40, "v4"))

    def test_external_actions_have_sha_and_version_comment(self):
        seen = 0
        for path in sorted(WORKFLOWS.glob("*.yml")):
            with self.subTest(workflow=path.name):
                for ref, version in USES.findall(path.read_text()):
                    if ref.startswith("./"):
                        continue
                    seen += 1
                    self.assertTrue(is_reviewable_pin(ref, version), (ref, version))
        self.assertGreater(seen, 0)


if __name__ == "__main__":
    unittest.main()
