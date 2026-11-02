"""Reject crossed identities, incomplete checks, tampering and stale release tags."""
import copy
import datetime
import json
import os
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

import delivery_gate as gate
from build_candidate import verification_plan


class DeliveryGateTests(unittest.TestCase):
    def setUp(self):
        # Test inputs must not inherit the real branch/dispatch/tag that runs CI.
        environment = patch.dict(os.environ, {'GITHUB_REF': 'refs/heads/main',
            'GH_EVENT_NAME': 'workflow_dispatch', 'RELEASE_TAG': '',
            'RELEASE_CADENCE_EXCEPTION_REASON': '', 'VALIDATION_SCOPE': 'full'})
        environment.start()
        self.addCleanup(environment.stop)

    def test_branch_candidate_is_read_only_and_cannot_publish(self):
        import tempfile
        from pathlib import Path
        with tempfile.TemporaryDirectory() as temporary:
            output = str(Path(temporary) / 'outputs')
            def capture(args):
                return self.sha if args == ['git', 'rev-parse', 'HEAD'] else ''
            with patch.dict(os.environ, {'RELEASE_TAG': '', 'RELEASE_CADENCE_EXCEPTION_REASON': '',
                    'GITHUB_REF': 'refs/heads/release/qa', 'GH_EVENT_NAME': 'workflow_dispatch', 'GITHUB_OUTPUT': output}), \
                    patch.object(gate, 'capture', side_effect=capture), patch.object(gate.subprocess, 'run') as run:
                gate.source()
                self.assertIn('publish=false', Path(output).read_text())
                self.assertEqual(run.call_args.args[0], ['git', 'merge-base', '--is-ancestor', 'origin/main', self.sha])
                with patch.dict(os.environ, {'RELEASE_TAG': self.tag}), self.assertRaisesRegex(ValueError, 'requires main'):
                    gate.source()

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
                         smoke=dict(status='pass', installedPlugins=0, xauth=dict(status='pass', version='xauth 1.1.2')), privacy=dict(status='pass', pluginPayloads=0),
                         packageValidation=dict(status='pass'), importValidation=dict(status='pass', smoke=dict(xauth=dict(status='pass', version='xauth 1.1.2'))),
                         asset=dict(name=name, bytes=path.stat().st_size, sha256=gate.digest(path)))
            sdk_name = f'aide-{self.tag}-devtools-linux-{arch}.zip'
            sdk_path = folder / sdk_name
            sdk_path.write_bytes(b'local synthetic SDK ' + arch.encode())
            proof['toolbox'] = dict(status='pass', image=image + '-test-tools',
                platform='linux/' + arch, sourceSHA256='b' * 64, commit=self.sha,
                checks=dict(status='pass'), privacy=dict(status='pass', pluginPayloads=0),
                importValidation=dict(status='pass'), packageValidation=dict(status='pass'),
                asset=dict(name=sdk_name, bytes=sdk_path.stat().st_size, sha256=gate.digest(sdk_path)))
            lock = gate.load_lock()
            proof['publicPlugins'] = dict(status='pass', commit=self.sha, architecture=arch, marketRevision=lock['marketRevision'],
                plugins=[dict(id=p['id'], version=p['marketVersion'], sha256=p['sha256'], inspection='pass', installation='pass', function='pass', cases=['synthetic independent positive assertion']) for p in lock['plugins']])
            proof['goSuite'] = dict(status='pass', server=dict(status='pass', inventory=['TestFixture'], inventoryCount=1, passed=['TestFixture'], skipped=[], shards=[dict(exit=0, missing=[], selected=['TestFixture'], results={'TestFixture':'pass'})]))
            (folder / 'candidate-proof.json').write_text(json.dumps(proof))
        return root / 'aide-candidate-amd64/candidate-proof.json'

    def test_both_architectures_required_and_digest_tampering_rejected(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            self.fixture(root)
            assets, source = gate.validate_candidates(root, self.tag, self.sha)
            self.assertEqual(len(assets), 4)
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

    def test_runtime_and_reimport_require_actual_xauth_smoke(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            p = self.fixture(root)
            original = json.loads(p.read_text())
            for section in ('smoke', 'importValidation'):
                for xauth in ({}, {'status': 'fail', 'version': 'xauth'},
                              {'status': 'pass', 'version': '   '}):
                    proof = copy.deepcopy(original)
                    smoke = proof[section] if section == 'smoke' else proof[section]['smoke']
                    smoke['xauth'] = xauth
                    p.write_text(json.dumps(proof))
                    with self.subTest(section=section, xauth=xauth):
                        with self.assertRaisesRegex(ValueError, 'runtime xauth'):
                            gate.validate_candidates(root, self.tag, self.sha)

    def test_sdk_is_required_and_tampering_rejected(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            p = self.fixture(root)
            proof = json.loads(p.read_text())
            original = copy.deepcopy(proof)
            for change in ({'toolbox': {}}, {'toolbox': {**proof['toolbox'], 'sourceSHA256': 'x' * 64}},
                           {'toolbox': {**proof['toolbox'], 'importValidation': {}}}):
                p.write_text(json.dumps({**original, **change}))
                with self.assertRaises(ValueError):
                    gate.validate_candidates(root, self.tag, self.sha)
            p.write_text(json.dumps(original))
            (p.parent / proof['toolbox']['asset']['name']).write_bytes(b'bad SDK')
            with self.assertRaisesRegex(ValueError, 'SDK asset size or digest'):
                gate.validate_candidates(root, self.tag, self.sha)

    def test_malicious_release_ref_rejected_before_remote_operations(self):
        with tempfile.TemporaryDirectory() as tmp:
            with self.assertRaises(ValueError):
                gate.validate_candidates(Path(tmp), '../../main;bad', self.sha)

    def test_remote_draft_uses_id_and_rejects_wrong_identity_or_digest(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            self.fixture(root)
            for name in ('SHA256SUMS', 'RELEASE-ASSETS.txt', 'RELEASE-VALIDATION.json'):
                (root / name).write_text('synthetic manifest')
            expected, _ = gate.validate_candidates(root, self.tag, self.sha)
            for name in ('SHA256SUMS', 'RELEASE-ASSETS.txt', 'RELEASE-VALIDATION.json'):
                expected[name] = dict(bytes=(root / name).stat().st_size, sha256=gate.digest(root / name))
            (root / 'RELEASE-NOTES.md').write_text('Reviewed human release notes')
            release = dict(id=123, tag_name=self.tag, body='Reviewed human release notes', draft=True, assets=[dict(name=n, size=v['bytes'], digest='sha256:' + v['sha256']) for n, v in expected.items()])
            def run(identity, record):
                def fake(args):
                    if args == ['gh', 'api', f'repos/owner/repo/git/ref/tags/{self.tag}']:
                        return json.dumps(dict(object=dict(type='commit', sha=self.sha)))
                    if args == ['gh', 'release', 'view', self.tag, '--repo', 'owner/repo', '--json', 'databaseId']:
                        return json.dumps(dict(databaseId=identity))
                    if args == ['gh', 'api', 'repos/owner/repo/releases/123']:
                        return json.dumps(record)
                    if args == ['gh', 'api', 'repos/' + gate.load_lock()['marketRepository'] + '/commits/main']:
                        return json.dumps(dict(sha=gate.load_lock()['marketRevision']))
                    raise AssertionError('Unexpected remote operation: ' + str(args))
                with patch.dict(os.environ, dict(RELEASE_TAG=self.tag, RELEASE_SHA=self.sha, GH_REPO='owner/repo')), patch.object(gate, 'capture', side_effect=fake):
                    gate.remote(root)
            run(123, release)
            for record in ({**release, 'id':124}, {**release, 'tag_name':'wrong'}, {**release, 'draft':False}, {**release, 'body':'wrong notes'}, {**release, 'assets':[]}):
                with self.subTest(record=record), self.assertRaises(ValueError):
                    run(123, record)
            for identity in ('123', True, -1):
                with self.subTest(identity=identity), self.assertRaisesRegex(ValueError, 'database ID'):
                    run(identity, release)

    def test_weekly_cadence_counts_rcs_ignores_drafts_and_has_exact_boundary(self):
        now = datetime.datetime(2026, 10, 30, 12, tzinfo=datetime.timezone.utc)
        gate.check_weekly_cadence([], now)
        gate.check_weekly_cadence([dict(draft=True, published_at='2026-10-30T12:00:00Z')], now)
        gate.check_weekly_cadence([dict(draft=False, prerelease=True, published_at='2026-10-23T12:00:00Z')], now)
        with self.assertRaisesRegex(ValueError, 'lacks timestamp'):
            gate.check_weekly_cadence([dict(draft=False)], now)
        with self.assertRaisesRegex(ValueError, 'Weekly release limit'):
            gate.check_weekly_cadence([dict(draft=False, prerelease=True, published_at='2026-10-23T12:00:01Z')], now)

    def test_one_off_cadence_exception_requires_a_manual_dispatch_and_a_reason(self):
        reason = 'Owner-authorized Codex Bridge integration release on 2026-10-29; preserve weekly cadence afterward.'
        with patch.dict(os.environ, {'GH_REPO': 'owner/repo', 'RELEASE_CADENCE_EXCEPTION_REASON': reason,
                                     'GH_EVENT_NAME': 'workflow_dispatch'}), patch.object(gate, 'capture', side_effect=AssertionError('exception must not fetch releases')):
            gate.cadence()
        for event, invalid_reason in (('push', reason), ('workflow_dispatch', 'too short'),
                                      ('workflow_dispatch', reason + '\nforged log line')):
            with self.subTest(event=event, reason=invalid_reason), patch.dict(os.environ, {
                    'GH_REPO': 'owner/repo', 'RELEASE_CADENCE_EXCEPTION_REASON': invalid_reason,
                    'GH_EVENT_NAME': event}), self.assertRaises(ValueError):
                gate.cadence()

    def test_cadence_exception_cannot_be_supplied_for_candidate_only(self):
        def fake(args):
            if args == ['git', 'rev-parse', 'HEAD']:
                return self.sha
            if args == ['git', 'status', '--porcelain']:
                return ''
            return '0.1.17.5 RC3'
        with patch.dict(os.environ, {'RELEASE_CADENCE_EXCEPTION_REASON': 'A sufficiently detailed release exception reason.',
                                     'RELEASE_TAG': ''}), patch.object(gate, 'capture', side_effect=fake), \
                patch.object(gate.subprocess, 'run'):
            with self.assertRaisesRegex(ValueError, 'requires a release tag'):
                gate.source()

    def test_notes_require_exact_tag_and_user_facing_sections(self):
        with tempfile.TemporaryDirectory() as temporary, patch.object(gate, 'ROOT', Path(temporary)):
            notes = Path(temporary) / 'docs/releases'
            notes.mkdir(parents=True)
            path = notes / (self.tag + '.md')
            with self.assertRaisesRegex(ValueError, 'Missing reviewed'):
                gate.release_notes(self.tag)
            path.write_text('# aide ' + self.tag + '\n' + ''.join('## ' + title + '\n' + 'Verified release detail. ' * 4 + '\n' for title in ('新增与改进', '修复', '升级影响', '已知问题')))
            self.assertTrue(gate.release_notes(self.tag).startswith('# aide ' + self.tag))
            path.write_text(path.read_text().replace('## 升级影响', '## TODO'))
            with self.assertRaises(ValueError):
                gate.release_notes(self.tag)

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

    def test_host_only_repairs_cannot_publish_a_release(self):
        for scope in ('hosts', 'browser'):
            with self.subTest(scope=scope), patch.dict(os.environ, {'RELEASE_TAG': self.tag, 'VALIDATION_SCOPE': scope}), \
                    patch.object(gate, 'capture', side_effect=lambda command: '' if 'status' in command else self.sha), \
                    patch.object(gate.subprocess, 'run') as run:
                with self.assertRaisesRegex(ValueError, 'full validation scope'):
                    gate.source()
                run.assert_not_called()


if __name__ == '__main__':
    unittest.main()
