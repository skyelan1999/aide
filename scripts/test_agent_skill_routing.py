"""Behavioral regression tests for request-to-skill routing."""
import importlib.util
import json
from pathlib import Path
import subprocess
import sys
import unittest

ROOT = Path(__file__).resolve().parents[1]
spec = importlib.util.spec_from_file_location('agent_route', ROOT / 'scripts/agent-route.py')
agent_route = importlib.util.module_from_spec(spec)
spec.loader.exec_module(agent_route)
CONFIG = json.loads((ROOT / 'docs/agent/router.json').read_text())


class SkillRoutingTests(unittest.TestCase):
    def test_router_limits_matches_and_all_skill_files_exist(self):
        router = CONFIG['skill_router']
        self.assertLessEqual(router['max_specialists'], 3)
        for item in [router['entry'], *(route['skill'] for route in router['routes'])]:
            self.assertTrue((ROOT / item).is_file(), item)

    def test_backend_request_selects_backend_as_primary(self):
        selected = agent_route.select_skills('Go 后端 API 会话工作流', CONFIG)
        self.assertTrue(selected)
        self.assertEqual(selected[0]['id'], 'backend')
        self.assertLessEqual(len(selected), CONFIG['skill_router']['max_specialists'])

    def test_frontend_request_selects_frontend_as_primary(self):
        selected = agent_route.select_skills('网页 UI CSS 样式与浏览器渲染', CONFIG)
        self.assertTrue(selected)
        self.assertEqual(selected[0]['id'], 'frontend')

    def test_each_specialist_route_has_a_representative_request(self):
        examples = {
            'backend': 'Go 后端 API 会话',
            'frontend': '网页 UI CSS 样式',
            'runtime-release': 'Docker 镜像 release 发布升级',
            'security': '鉴权 token vault 密钥加密',
            'verification-docs': '测试 验收 文档 报告 QA',
        }
        for expected, request in examples.items():
            with self.subTest(route=expected):
                selected = agent_route.select_skills(request, CONFIG)
                self.assertTrue(selected)
                self.assertEqual(selected[0]['id'], expected)

    def test_unmatched_request_falls_back_to_coordinator(self):
        selected = agent_route.select_skills('量子海底月球车', CONFIG)
        self.assertEqual(selected, [])
        result = subprocess.run(
            [sys.executable, 'scripts/agent-route.py', 'route', '--request', '量子海底月球车'],
            cwd=ROOT, capture_output=True, text=True, check=True,
        )
        self.assertIn('coordinator owns discovery', result.stdout)

    def test_command_prints_primary_skill_and_does_not_spawn_agents(self):
        result = subprocess.run(
            [sys.executable, 'scripts/agent-route.py', 'route', '--request', 'Go 后端 API'],
            cwd=ROOT, capture_output=True, text=True, check=True,
        )
        self.assertIn('PRIMARY SPECIALIST: Go 后端与产品能力', result.stdout)
        self.assertIn('.agents/skills/aide-backend/SKILL.md', result.stdout)
        self.assertIn('does not spawn agents', result.stdout)

    def test_prompt_request_keeps_client_entry_and_adds_routing(self):
        result = subprocess.run(
            [sys.executable, 'scripts/agent-route.py', 'prompt', 'codex', '--request', '网页 CSS UI'],
            cwd=ROOT, capture_output=True, text=True, check=True,
        )
        self.assertIn('aide workflow v1 / client: codex', result.stdout)
        self.assertIn('SPECIALIST ROUTING', result.stdout)
        self.assertIn('PRIMARY SPECIALIST: 网页界面与交互', result.stdout)


if __name__ == '__main__':
    unittest.main(verbosity=2)
