"""Exercise the documented transition and later git pulls with real repositories."""
import shutil
import subprocess
import tempfile
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]

class PluginIsolationTests(unittest.TestCase):
    def test_migrate_then_upgrade_and_pull(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            origin = root / 'origin'
            origin.mkdir()
            def git(where, *args):
                return subprocess.check_output(['git', '-C', str(where), *args], stderr=subprocess.STDOUT, text=True)
            git(origin, 'init', '-b', 'main')
            git(origin, 'config', 'user.name', 'Fixture')
            git(origin, 'config', 'user.email', 'fixture@example.invalid')
            def commit(message):
                git(origin, 'add', '.')
                git(origin, 'commit', '-m', message)
            (origin/'plugins/tool').mkdir(parents=True)
            (origin/'plugins/tool/index.js').write_text('original')
            (origin/'plugins/registry.json').write_text('original registry')
            commit('old tracked plugins')
            clone = root/'clone'
            subprocess.check_output(['git','clone',str(origin),str(clone)],stderr=subprocess.STDOUT)
            git(clone,'config','user.name','Fixture')
            git(clone,'config','user.email','fixture@example.invalid')
            (clone/'plugins/tool/index.js').write_text('user upgrade')
            (clone/'plugins/registry.json').write_text('private settings')
            backup = root/'backup'
            shutil.copytree(clone/'plugins',backup)
            git(clone,'stash','push','-m','plugin-state-before-isolation','--','plugins')
            shutil.copytree(origin/'plugins',origin/'bundled-plugins')
            (origin/'.gitignore').write_text((ROOT/'.gitignore').read_text())
            git(origin,'rm','-r','--cached','plugins')
            commit('isolate runtime plugins')
            git(clone,'pull','--ff-only')
            shutil.copytree(backup,clone/'plugins',dirs_exist_ok=True)
            self.assertEqual((clone/'plugins/tool/index.js').read_text(),'user upgrade')
            self.assertEqual((clone/'plugins/registry.json').read_text(),'private settings')
            self.assertEqual(git(clone,'status','--porcelain'),'')
            # A later marketplace upgrade must neither dirty nor block a pull.
            (clone/'plugins/tool/index.js').write_text('second market upgrade')
            (origin/'bundled-plugins/tool/index.js').write_text('new factory source')
            commit('host update')
            git(clone,'pull','--ff-only')
            self.assertEqual((clone/'plugins/tool/index.js').read_text(),'second market upgrade')
            self.assertEqual(git(clone,'status','--porcelain'),'')

if __name__ == '__main__':
    unittest.main()
