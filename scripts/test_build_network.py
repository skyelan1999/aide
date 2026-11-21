import copy
import importlib.util
import unittest
from pathlib import Path
from unittest.mock import patch

spec = importlib.util.spec_from_file_location('build_network', Path(__file__).with_name('build-network.py'))
network = importlib.util.module_from_spec(spec)
spec.loader.exec_module(network)


class SelectionTests(unittest.TestCase):
    def setUp(self):
        self.healthy = {region: {kind: {'ok': True} for kind in ('registry', 'pip', 'apt')}
                        for region in ('standard', 'china')}

    def test_standard_wins_when_reachable(self):
        self.assertEqual(set(network.select(self.healthy, self.healthy).values()), {'standard'})

    def test_container_failures_override_host_package_results(self):
        container = copy.deepcopy(self.healthy)
        container['standard']['apt']['ok'] = False
        result = network.select(self.healthy, container)
        self.assertEqual(result, {'registry': 'standard', 'pip': 'standard', 'apt': 'china'})

    def test_no_network_is_recorded_without_claiming_a_working_source(self):
        failed = {r: {k: {'ok': False} for k in self.healthy[r]} for r in self.healthy}
        self.assertEqual(set(network.select(failed, None).values()), {'unavailable'})

    def test_explicit_profiles_and_disabled_fallback(self):
        self.assertEqual(set(network.select({}, None, 'china').values()), {'china'})
        self.assertEqual(set(network.select({}, None, 'standard').values()), {'standard'})
        with self.assertRaises(ValueError):
            network.select({}, None, 'unknown')
        failed = copy.deepcopy(self.healthy)
        for value in failed['standard'].values():
            value['ok'] = False
        self.assertEqual(set(network.select(failed, None, fallback=False).values()), {'standard'})

    def test_repository_body_is_validated(self):
        class Response:
            headers = {}
            def __enter__(self): return self
            def __exit__(self, *args): pass
            def read(self, limit): return b'<html>captive login portal</html>'
        with patch.object(network.urllib.request, 'urlopen', return_value=Response()):
            for kind in ('pip', 'apt', 'registry'):
                self.assertFalse(network.probe(kind, 'https://example.test/')['ok'])



class WiringTests(unittest.TestCase):
    def test_preflight_precedes_fingerprint_and_builder(self):
        import inspect
        from test_launcher_contract import module
        source = inspect.getsource(module.Launcher.build)
        self.assertLess(source.index('select_build_sources'), source.index('source-sha'))
        self.assertLess(source.index('select_build_sources'), source.index('ensure_builder'))

    def test_private_index_offline_and_custom_image_are_preserved(self):
        import tempfile
        from test_launcher_contract import module
        healthy = {r: {k: {'ok': False} for k in ('registry', 'pip', 'apt')} for r in ('standard', 'china')}
        for v in healthy['china'].values():
            v['ok'] = True
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            (root/'Dockerfile').write_text('ARG PYTHON_BASE=python:3.12\nARG NODE_BASE=node:24\nARG GO_BASE=golang:1.26\n')
            launcher = module.Launcher(root)
            with patch.object(launcher, 'd', return_value=None), patch.object(network, 'assess', return_value=healthy), patch.object(module, 'build_network_module', return_value=network):
                result = launcher.select_build_sources({}, {'PYTHON_BASE':'private.example/python:v1','PIP_INDEX_URL':'https://private.example/simple/'})
                self.assertEqual(result['PYTHON_BASE'], 'private.example/python:v1')
                self.assertEqual(result['PIP_INDEX_URL'], 'https://private.example/simple/')
                result = launcher.select_build_sources({}, {'PIP_OFFLINE':'1','PIP_INDEX_URL':network.PIP['standard']})
                self.assertEqual(result['PIP_INDEX_URL'], network.PIP['standard'])


if __name__ == '__main__':
    unittest.main()
