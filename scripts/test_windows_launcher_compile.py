#!/usr/bin/env python3
"""Compile-check the real Win32 launcher with the Windows runner's existing SDK.

Resource declarations are placeholders for syntax checking only. No executable
or distributable payload is produced.
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
        sources = '\n'.join(p.read_text() for p in (ROOT / 'native/windows-launcher').glob('*') if p.suffix in ('.cpp', '.hpp'))
        declarations = ['#pragma once']
        declarations += [f'inline const char* {name}="";' for name in sorted(set(re.findall(r'\bembedded_[a-z_]+\b', sources)))]
        declarations += [f'extern "C" const unsigned char {name}[];' for name in sorted(set(re.findall(r'\baide_(?:local_mounts|message_lan)_(?:begin|end)\b', sources)))]
        (folder / 'embedded.hpp').write_text('\n'.join(declarations))
        command = folder / 'check.cmd'
        command.write_text('@echo off\ncall "' + installation + '\\Common7\\Tools\\VsDevCmd.bat" -arch=x64\n'
            'if errorlevel 1 exit /b 1\ncl /nologo /std:c++17 /EHsc /Zs /I"' + str(folder) + '" "' + str(ROOT / 'native/windows-launcher/main.cpp') + '"\n')
        subprocess.run(['cmd.exe', '/d', '/c', str(command)], check=True, cwd=folder)
    print('PASS real Win32 launcher C++/SDK syntax; no executable generated')


if __name__ == '__main__':
    main()
