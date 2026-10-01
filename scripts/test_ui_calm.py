"""Regression checks for the low-stimulation UI styles and theme token contract."""
from pathlib import Path
import re
import subprocess
import unittest

ROOT = Path(__file__).resolve().parents[1]
CSS_PATH = 'internal/server/web/macos.css'


def custom_properties(text):
    return set(re.findall(r'(?m)^\s*(--[\w-]+)\s*:', text))


def blocks(text):
    text = re.sub(r'/\*.*?\*/', '', text, flags=re.S)
    return [(selector.strip(), body) for selector, body in re.findall(r'([^{}]+)\{([^{}]*)\}', text)]


class CalmUIStyleTests(unittest.TestCase):
    def setUp(self):
        self.css = (ROOT / CSS_PATH).read_text()

    def test_wide_settings_sheet_gets_later_lower_blur_override(self):
        legacy = self.css.index('blur(22px)')
        calm_section = self.css.index('/* Calm work surfaces:')
        self.assertGreater(calm_section, legacy)
        matching = [
            (selector, body) for selector, body in blocks(self.css[calm_section:])
            if 'backdrop-filter:' in body and 'blur(12px)' in body
        ]
        self.assertTrue(matching)
        selectors = ' '.join(selector for selector, _ in matching)
        self.assertIn('.settings-sheet.sheet-wide', selectors)

    def test_reduced_transparency_covers_wide_settings_sheet(self):
        media = self.css.split('@media (prefers-reduced-transparency: reduce)', 1)[1]
        matching = [
            (selector, body) for selector, body in blocks(media)
            if 'backdrop-filter: none' in body
        ]
        self.assertTrue(matching)
        self.assertIn('.settings-sheet.sheet-wide', ' '.join(selector for selector, _ in matching))

    def test_focus_keyboard_reduced_motion_and_desktop_layout_remain_present(self):
        self.assertIn('button:focus-visible', self.css)
        self.assertIn('@media (prefers-reduced-motion: reduce)', self.css)
        self.assertIn('@media (min-width: 1151px)', self.css)
        self.assertRegex(self.css, r'\.starter-grid\s*\{[^}]*repeat\(3, minmax\(0, 1fr\)\)')

    def test_theme_custom_property_contracts_are_unchanged(self):
        for theme in ('light', 'dark'):
            with self.subTest(theme=theme):
                path = f'internal/server/web/themes/{theme}/tokens.css'
                current = custom_properties((ROOT / path).read_text())
                previous = subprocess.check_output(['git', 'show', f'HEAD:{path}'], cwd=ROOT, text=True)
                self.assertTrue(current)
                self.assertEqual(current, custom_properties(previous), theme)

    def test_html_cache_version_advances_with_macos_stylesheet(self):
        html = (ROOT / 'internal/server/web/index.html').read_text()
        current = re.search(r'href="/macos\.css\?v=(\d+)"', html)
        self.assertIsNotNone(current)
        previous = subprocess.check_output(
            ['git', 'show', 'HEAD:internal/server/web/index.html'], cwd=ROOT, text=True,
        )
        previous_match = re.search(r'href="/macos\.css\?v=(\d+)"', previous)
        self.assertIsNotNone(previous_match)
        self.assertGreater(int(current.group(1)), int(previous_match.group(1)))


if __name__ == '__main__':
    unittest.main(verbosity=2)
