#!/usr/bin/env python3
"""Compile and run the SAPI bridge session boundary without activating audio."""
from pathlib import Path
import shutil
import subprocess
import tempfile

ROOT = Path(__file__).resolve().parents[1]


def main():
    if shutil.which('cl.exe'):
        with tempfile.TemporaryDirectory(prefix='aide-sapi-smoke-') as directory:
            output = Path(directory) / 'windows-speech-bridge-smoke.exe'
            command = ['cl.exe', '/nologo', '/EHsc', '/std:c++17',
                   str(ROOT / 'scripts/windows_speech_bridge_smoke.cpp'),
                   f'/Fo:{Path(directory) / "windows-speech-bridge-smoke.obj"}',
                   f'/Fd:{Path(directory) / "windows-speech-bridge-smoke.pdb"}',
                   f'/Fe:{output}', '/link', 'sapi.lib', 'ole32.lib']
            # Keep the compiler and smoke process' working directory outside the
            # temporary output tree. MSVC's background telemetry process can
            # retain its current directory after cl.exe exits on hosted runners.
            subprocess.run(command, check=True, cwd=ROOT)
            subprocess.run([str(output)], check=True, timeout=15, cwd=ROOT)
        return

    vswhere = Path(r'C:\Program Files (x86)\Microsoft Visual Studio\Installer\vswhere.exe')
    if not vswhere.exists():
        raise SystemExit('Windows MSVC is required for the native SAPI bridge smoke test')
    installation = subprocess.check_output(
        [str(vswhere), '-latest', '-products', '*', '-property', 'installationPath'], text=True
    ).strip()
    devcmd = Path(installation) / 'Common7/Tools/VsDevCmd.bat'
    with tempfile.TemporaryDirectory(prefix='aide-sapi-smoke-') as directory:
        output = Path(directory) / 'windows-speech-bridge-smoke.exe'
        command_file = Path(directory) / 'run-sapi-smoke.cmd'
        command_file.write_text(
            '@echo off\n'
            f'call "{devcmd}" -arch=x64\n'
            'if errorlevel 1 exit /b 1\n'
            f'cl /nologo /utf-8 /EHsc /std:c++17 "{ROOT / "scripts/windows_speech_bridge_smoke.cpp"}" '
            f'/Fo:"{Path(directory) / "windows-speech-bridge-smoke.obj"}" '
            f'/Fd:"{Path(directory) / "windows-speech-bridge-smoke.pdb"}" '
            f'/Fe:"{output}" /link sapi.lib ole32.lib\n'
            'if errorlevel 1 exit /b 1\n'
            f'"{output}"\n', encoding='utf-8'
        )
        subprocess.run(['cmd.exe', '/d', '/c', str(command_file)], check=True, cwd=ROOT, timeout=30)


if __name__ == '__main__':
    main()
