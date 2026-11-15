#!/usr/bin/env python3
"""Build and run an isolated Win32 integration fixture for the native Aide bridge."""
import os
from pathlib import Path
import subprocess
import tempfile

ROOT = Path(__file__).resolve().parents[2]


def main():
    if os.name != "nt":
        raise SystemExit("NOT_RUN: Win32 desktop required")
    program_files = Path(os.environ["ProgramFiles(x86)"])
    vswhere = program_files / "Microsoft Visual Studio/Installer/vswhere.exe"
    installation = subprocess.check_output([
        str(vswhere), "-latest", "-products", "*", "-requires",
        "Microsoft.VisualStudio.Component.VC.Tools.x86.x64", "-property", "installationPath",
    ], text=True).strip()
    if not installation:
        raise SystemExit("Windows C++ compiler unavailable")

    with tempfile.TemporaryDirectory(prefix="aide-computer-bridge-") as temporary:
        folder = Path(temporary)
        executable = folder / "windows-computer-bridge-test.exe"
        command = folder / "run.cmd"
        command.write_text(
            "@echo off\n"
            f'call "{installation}\\Common7\\Tools\\VsDevCmd.bat" -arch=x64\n'
            "if errorlevel 1 exit /b 1\n"
            f'cl /nologo /utf-8 /std:c++17 /EHsc /DUNICODE /D_UNICODE "{ROOT / "scripts/ci/windows_computer_bridge.cpp"}" '
            f'/Fe:"{executable}" /link bcrypt.lib ws2_32.lib crypt32.lib ole32.lib gdiplus.lib gdi32.lib user32.lib\n'
            "if errorlevel 1 exit /b 1\n"
            f'"{executable}"\n',
            encoding="utf-8",
        )
        subprocess.run(["cmd.exe", "/d", "/c", str(command)], cwd=folder, check=True, timeout=90)


if __name__ == "__main__":
    main()
