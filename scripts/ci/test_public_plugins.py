"""A stale, incomplete, skipped or wrong-package result must block publishing."""
import copy
import unittest

import public_plugins as qa


class PublicPluginGateTests(unittest.TestCase):
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
