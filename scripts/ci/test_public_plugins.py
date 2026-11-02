"""A stale, incomplete, skipped or wrong-package result must block publishing."""
import copy
import hashlib
import json
from pathlib import Path
import tempfile
import unittest
import zipfile

import public_plugins as qa


class PublicPluginGateTests(unittest.TestCase):
    def test_frozen_packages_must_match_current_catalog_identity(self):
        with tempfile.TemporaryDirectory() as directory:
            market = Path(directory)
            rows = []
            for version in ('1.0.0', '2.0.0'):
                package = 'packages/fixture/' + version + '.zip'
                path = market / package
                path.parent.mkdir(parents=True, exist_ok=True)
                with zipfile.ZipFile(path, 'w') as bundle:
                    bundle.writestr('manifest.json', json.dumps({
                        'id': 'fixture', 'version': version, 'main': 'index.js'}))
                    bundle.writestr('index.js', 'module.exports = {};')
                raw = path.read_bytes()
                rows.append({'id': 'fixture', 'version': version, 'package': package,
                             'bytes': len(raw), 'sha256': hashlib.sha256(raw).hexdigest()})
            current = rows[1]
            lock = {'plugins': [dict(current, marketVersion=current['version'])]}

            def catalog(items):
                (market / 'catalog.json').write_text(json.dumps({'plugins': items}))

            catalog([current])
            qa.inspect_packages(market, lock)
            # Both historical and current ZIPs are valid; the advertised one
            # must be the version actually selected for install/function tests.
            with self.assertRaisesRegex(ValueError, 'market version'):
                qa.inspect_packages(market, {'plugins': [dict(rows[0], marketVersion='1.0.0')]})
            for field, value in (('version', '3.0.0'), ('package', rows[0]['package']),
                                 ('bytes', current['bytes'] + 1), ('sha256', 'f' * 64)):
                with self.subTest(field=field), self.assertRaises(ValueError):
                    catalog([dict(current, **{field: value})])
                    qa.inspect_packages(market, lock)
            catalog([current, current])
            with self.assertRaisesRegex(ValueError, 'Duplicate market'):
                qa.inspect_packages(market, lock)

    def fixture(self):
        item = {'id': 'fixture', 'marketVersion': '1.0.0', 'sha256': 'a' * 64}
        lock = {'marketRevision': 'b' * 40, 'plugins': [item]}
        report = {'status': 'pass', 'commit': 'c' * 40, 'architecture': 'arm64',
                  'marketRevision': lock['marketRevision'], 'plugins': [
                      {'id': 'fixture', 'version': '1.0.0', 'sha256': item['sha256'],
                       'inspection': 'pass', 'installation': 'pass', 'function': 'pass',
                       'cases': ['actual operation with independent assertion']}]}
        return lock, report

    def test_only_exact_complete_success_is_accepted(self):
        lock, report = self.fixture()
        qa.verify_report(report, lock, 'c' * 40, 'arm64')
        for field, value in [('status', 'skip'), ('commit', 'd' * 40), ('architecture', 'amd64'),
                             ('marketRevision', 'e' * 40), ('plugins', [])]:
            with self.subTest(field=field), self.assertRaises(ValueError):
                qa.verify_report(dict(report, **{field: value}), lock, 'c' * 40, 'arm64')

    def test_installation_does_not_substitute_for_function_or_coverage(self):
        lock, original = self.fixture()
        for change in ({'function': 'not_run'}, {'function': 'skip'}, {'function': 'fail'},
                       {'cases': []}, {'sha256': 'f' * 64}, {'version': '1.0.1'}):
            report = copy.deepcopy(original)
            report['plugins'][0].update(change)
            with self.subTest(change=change), self.assertRaises(ValueError):
                qa.verify_report(report, lock)
        report = copy.deepcopy(original)
        report['plugins'].append(report['plugins'][0])
        with self.assertRaises(ValueError):
            qa.verify_report(report, lock)


if __name__ == '__main__':
    unittest.main()
