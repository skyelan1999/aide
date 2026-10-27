@echo off
REM Windows lifecycle is implemented by the single native executable.
if not exist "%~dp0aide-launcher.exe" (
  echo Missing aide-launcher.exe. Restore the complete package or build scripts/build-windows-launcher.py.
  exit /b 1
)
"%~dp0aide-launcher.exe" stop %*
exit /b %ERRORLEVEL%
