"""Reject crossed identities, incomplete checks, tampering and stale release tags."""
import copy
import json
import os
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

import delivery_gate as gate
from build_candidate import verification_plan


class DeliveryGateTests(unittest.TestCase):
    tag = 'v0.1.17.5-RC3'
    sha = 'a' * 40

    def fixture(self, root):
        for arch in ('amd64', 'arm64'):
            folder = root / ('aide-candidate-' + arch)
            folder.mkdir()
            name = f'aide-{self.tag}-full-linux-{arch}.zip'
            path = folder / name
            path.write_bytes(b'local synthetic artifact ' + arch.encode())
            image = f'aide:ci-{self.sha[:12]}-{arch}'
            proof = dict(status='pass', candidateOnly=True, published=False, pluginsBundled=False,
                         tag=self.tag, commit=self.sha, platform='linux/' + arch,
                         sourceSHA256='b' * 64, image=image,
                         verification=[dict(command=c, environmentOverrides=e, status='pass') for c, e in verification_plan(image)],
                         smoke=dict(status='pass', installedPlugins=0), privacy=dict(status='pass', pluginPayloads=0),
                         packageValidation=dict(status='pass'), importValidation=dict(status='pass'),
                         asset=dict(name=name, bytes=path.stat().st_size, sha256=gate.digest(path)))
            (folder / 'candidate-proof.json').write_text(json.dumps(proof))
        return root / 'aide-candidate-amd64/candidate-proof.json'

    def test_both_architectures_required_and_digest_tampering_rejected(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            self.fixture(root)
            assets, source = gate.validate_candidates(root, self.tag, self.sha)
            self.assertEqual(len(assets), 2)
            self.assertEqual(source, 'b' * 64)
            next((root / 'aide-candidate-amd64').glob('*.zip')).write_bytes(b'tampered')
            with self.assertRaisesRegex(ValueError, 'digest mismatch'):
                gate.validate_candidates(root, self.tag, self.sha)

    def test_crossed_or_incomplete_candidate_fails_closed(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            p = self.fixture(root)
            original = json.loads(p.read_text())
            mutations = [dict(commit='c' * 40), dict(platform='linux/arm64'), dict(pluginsBundled=True),
                         dict(verification=[]), dict(sourceSHA256='d' * 64),
                         dict(asset={**original['asset'], 'name': '../outside.zip'})]
            for change in mutations:
                with self.subTest(change=change):
                    p.write_text(json.dumps({**copy.deepcopy(original), **change}))
                    with self.assertRaises(ValueError):
                        gate.validate_candidates(root, self.tag, self.sha)
            p.write_text(json.dumps(original))
            (p.parent / 'extra.zip').write_bytes(b'unsolicited')
            with self.assertRaisesRegex(ValueError, 'Unexpected candidate files'):
                gate.validate_candidates(root, self.tag, self.sha)

    def test_malicious_release_ref_rejected_before_remote_operations(self):
        with tempfile.TemporaryDirectory() as tmp:
            with self.assertRaises(ValueError):
                gate.validate_candidates(Path(tmp), '../../main;bad', self.sha)

    def test_manual_tag_must_name_current_main_commit(self):
        def fake(args):
            if args == ['git', 'rev-parse', 'HEAD']:
                return self.sha
            if args == ['git', 'status', '--porcelain']:
                return ''
            if args[0] == 'bash':
                return '0.1.17.5 RC3'
            return 'd' * 40
        with patch.dict(os.environ, {'RELEASE_TAG': self.tag}), patch.object(gate, 'capture', side_effect=fake), patch.object(gate.subprocess, 'run'):
            with self.assertRaisesRegex(ValueError, 'must identify'):
                gate.source()


if __name__ == '__main__':
    unittest.main()
