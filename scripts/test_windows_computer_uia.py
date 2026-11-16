#!/usr/bin/env python3
"""Build and run a bounded, local Win32 UI Automation contract smoke test."""
import os
from pathlib import Path
import subprocess
import tempfile

ROOT = Path(__file__).resolve().parents[1]


def main():
    if os.name != 'nt':
        raise SystemExit('NOT_RUN: Windows UI Automation requires a Windows desktop')
    vswhere = Path(os.environ['ProgramFiles(x86)']) / 'Microsoft Visual Studio/Installer/vswhere.exe'
    installation = subprocess.check_output([str(vswhere), '-latest', '-products', '*',
        '-requires', 'Microsoft.VisualStudio.Component.VC.Tools.x86.x64', '-property', 'installationPath'], text=True).strip()
    if not installation:
        raise SystemExit('Windows C++ compiler unavailable')
    (ROOT / '.agent-state').mkdir(exist_ok=True)
    with tempfile.TemporaryDirectory(prefix='aide-windows-uia-', dir=ROOT / '.agent-state') as temporary:
        folder = Path(temporary)
        binary = folder / 'uia-smoke.exe'
        command = folder / 'build-and-run.cmd'
        command.write_text('@echo off\ncall "' + installation + '\\Common7\\Tools\\VsDevCmd.bat" -arch=x64\n'
            'if errorlevel 1 exit /b 1\ncl /nologo /utf-8 /std:c++17 /EHsc /DUNICODE /D_UNICODE '
            '/I"' + str(ROOT / 'native/windows-launcher') + '" '
            '"' + str(ROOT / 'scripts/fixtures/windows_computer_uia.cpp') + '" '
            '/Fe:"' + str(binary) + '" /link ole32.lib oleaut32.lib uuid.lib uiautomationcore.lib user32.lib gdi32.lib gdiplus.lib bcrypt.lib crypt32.lib ws2_32.lib\n'
            'if errorlevel 1 exit /b 1\n"' + str(binary) + '"\n', encoding='utf-8')
        subprocess.run(['cmd.exe', '/d', '/c', str(command)], check=True, cwd=folder, timeout=90)
    print('PASS Windows UIA smoke: editable value/button found; password value excluded')


if __name__ == '__main__':
    main()
