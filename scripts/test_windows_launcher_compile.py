#!/usr/bin/env python3
"""Compile and self-test the real Win32 launcher with the existing Windows SDK.

Resource declarations are placeholders for this isolated test only. The temporary
executable is never installed or published and cannot provide host helpers.
"""
import os
from pathlib import Path
import re
import subprocess
import tempfile

ROOT = Path(__file__).resolve().parents[1]


def main():
    if os.name != 'nt':
        raise SystemExit('NOT_RUN: Windows SDK compiler required')
    vswhere = Path(os.environ['ProgramFiles(x86)']) / 'Microsoft Visual Studio/Installer/vswhere.exe'
    installation = subprocess.check_output([str(vswhere), '-latest', '-products', '*',
        '-requires', 'Microsoft.VisualStudio.Component.VC.Tools.x86.x64', '-property', 'installationPath'], text=True).strip()
    if not installation:
        raise SystemExit('Windows C++ compiler unavailable')
    with tempfile.TemporaryDirectory(prefix='aide-native-syntax-') as temporary:
        folder = Path(temporary)
        sources = '\n'.join(p.read_text(encoding='utf-8') for p in (ROOT / 'native/windows-launcher').glob('*') if p.suffix in ('.cpp', '.hpp'))
        declarations = ['#pragma once']
        declarations += [f'inline const char* {name}="";' for name in sorted(set(re.findall(r'\bembedded_[a-z_0-9]+\b', sources)))]
        declarations += [f'extern "C" const unsigned char {name}[]={{0}};' for name in sorted(set(re.findall(r'\baide_(?:local_mounts|message_lan)_(?:begin|end)\b', sources)))]
        (folder / 'embedded.hpp').write_text('\n'.join(declarations), encoding='utf-8')
        command = folder / 'check.cmd'
        command.write_text('@echo off\ncall "' + installation + '\\Common7\\Tools\\VsDevCmd.bat" -arch=x64\n'
            'if errorlevel 1 exit /b 1\ncl /nologo /utf-8 /std:c++17 /EHsc /I"' + str(folder) + '" "' + str(ROOT / 'native/windows-launcher/main.cpp') + '" "' + str(ROOT / 'native/windows-launcher/third_party/miniz/miniz_tinfl.c') + '" /Fe:launcher-test.exe /link advapi32.lib bcrypt.lib ws2_32.lib shell32.lib ole32.lib uuid.lib winhttp.lib user32.lib\n'
            'if errorlevel 1 exit /b 1\nlauncher-test.exe --self-test\n')
        subprocess.run(['cmd.exe', '/d', '/c', str(command)], check=True, cwd=folder)
        binary = folder / 'launcher-test.exe'
        for source in (False, True):
            root = folder / ('source fixture' if source else 'release fixture')
            root.mkdir()
            if source:
                (root / '.git').mkdir()
            for action in ('start', 'stop', 'build', 'clean', 'docker_clean', 'release', 'verify'):
                result = subprocess.run([str(binary), action, '--root', str(root), '--dry-run'],
                    cwd=folder, input='', capture_output=True, text=True, timeout=15)
                allowed = source or action in ('start', 'stop', 'docker_clean', 'verify')
                assert (result.returncode == 0) == allowed, (source, action, result.stdout, result.stderr)
                assert not (root / '.aide-data').exists(), 'dry run mutated launcher state'
                if allowed:
                    assert 'Dry run:' in result.stdout
        from test_windows_bat_entrypoints import check
        check(binary, folder, installation)
    print('PASS real Win32 launcher compile/link and native self-test; fixture executable removed')


if __name__ == '__main__':
    main()
